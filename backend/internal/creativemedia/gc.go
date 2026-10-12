package creativemedia

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativereference"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// GCMode 决定媒体回收做到哪一步（media.md §6.5）：先只读扫描，恢复演练通过后
// 才开启删除。
type GCMode string

const (
	GCOff    GCMode = "off"
	GCScan   GCMode = "scan"
	GCDelete GCMode = "delete"
)

// ParseGCMode 未配置时只读：开启删除必须是一次显式决定。
func ParseGCMode(v string) (GCMode, error) {
	switch m := GCMode(v); m {
	case "":
		return GCScan, nil
	case GCOff, GCScan, GCDelete:
		return m, nil
	}
	return "", fmt.Errorf("creative media gc mode %q: want off, scan or delete", v)
}

const (
	gcBatch    = 100
	gcInterval = 10 * time.Minute
	// 已计额度的 blob 曾绑定内容，宽限兜底误删并覆盖票据与读 pin；从未计额度的
	// 只需覆盖 10 分钟票据与 120 秒读 pin。
	gcBookedGrace   = 24 * time.Hour
	gcUnbookedGrace = 15 * time.Minute
	// 生成恢复每次续 24 小时；超过两倍仍无进展的导入不再等待。
	gcStaleImport = 48 * time.Hour
)

// GCReport 是一个账号一轮回收的结果；只读模式下是「本应回收」的统计。
type GCReport struct {
	StaleImports     int64
	UnheldRevisions  int64
	ReclaimableBlobs int64
	// ReclaimableBytes 与 DeletingBytes 只计已计额度的字节，即释放后
	// stored_bytes 会减少的量。
	ReclaimableBytes int64
	DeletingBytes    int64
	DeletedBlobs     int64
	ReleasedBytes    int64
	// SkippedBlobs 的存储驱动或 bucket 与当前配置不符，本进程无权删除。
	SkippedBlobs int64
}

const unheldMediaRevision = `state='ready' AND content_id IN (SELECT id FROM creative_contents WHERE account_id=$1 AND kind IN ('image','video','audio')) AND NOT (` + creativereference.LiveOwnerCondition + `)`

// blobHolders 对应 media.md §6.2 的 blob 持有方。绑定到无持有方修订的 blob 视为
// 未持有：这样的修订已不可用，也不会重新获得持有方。
const blobHolders = `EXISTS (SELECT 1 FROM creative_content_objects o JOIN creative_content_revisions ON creative_content_revisions.account_id=o.account_id AND creative_content_revisions.id=o.content_revision_id WHERE o.account_id=$1 AND o.blob_id=creative_blobs.id AND creative_content_revisions.state<>'deleted' AND (` + creativereference.LiveOwnerCondition + `))
OR EXISTS (SELECT 1 FROM creative_uploads WHERE account_id=$1 AND blob_id=creative_blobs.id AND state IN ('created','uploading','verifying'))
OR EXISTS (SELECT 1 FROM creative_generation_imports WHERE account_id=$1 AND state IN ('writing','stored') AND creative_blobs.id IN (original_blob,display_blob))
OR EXISTS (SELECT 1 FROM creative_blob_read_pins WHERE account_id=$1 AND blob_id=creative_blobs.id AND expires_at>clock_timestamp())`

const unheldBlob = `state='ready' AND NOT (` + blobHolders + `)`

const staleImport = `state IN ('writing','stored') AND (lease_until IS NULL OR lease_until<=clock_timestamp()) AND updated_at<=$2`

// CollectGarbage 对一个账号执行一轮回收。只读模式只统计、不写任何行；删除模式按
// media.md §6.3 依次重置陈旧导入、删除无持有方的修订、标记 blob、删除到期对象并
// 释放额度。每步在自己的短事务里先取账号写屏障，再锁行重查。
func (s *Service) CollectGarbage(ctx context.Context, scope store.AccountScope, mode GCMode) (GCReport, error) {
	var r GCReport
	var err error
	switch mode {
	case GCScan:
		return s.scanGarbage(ctx, scope)
	case GCDelete:
		if r.StaleImports, err = s.resetStaleImports(ctx, scope); err != nil {
			return r, err
		}
		if r.UnheldRevisions, err = s.deleteUnheldRevisions(ctx, scope); err != nil {
			return r, err
		}
		if r.ReclaimableBlobs, err = s.markUnheldBlobs(ctx, scope); err != nil {
			return r, err
		}
		err = s.deleteDueBlobs(ctx, scope, &r)
	}
	return r, err
}

func (s *Service) scanGarbage(ctx context.Context, scope store.AccountScope) (GCReport, error) {
	var r GCReport
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.CheckCreativeTextReferences(ctx); err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if r.StaleImports, err = tx.Count(ctx, "creative_generation_imports", staleImport, now.Add(-gcStaleImport)); err != nil {
			return err
		}
		if r.UnheldRevisions, err = tx.Count(ctx, "creative_content_revisions", unheldMediaRevision); err != nil {
			return err
		}
		if r.ReclaimableBlobs, err = tx.Count(ctx, "creative_blobs", unheldBlob); err != nil {
			return err
		}
		if err = tx.ScalarAggregate(ctx, "creative_blobs", store.AggregateSum, "byte_size", "quota_booked_at IS NOT NULL AND "+unheldBlob).Scan(&r.ReclaimableBytes); err != nil {
			return err
		}
		return tx.ScalarAggregate(ctx, "creative_blobs", store.AggregateSum, "byte_size", "state='deleting' AND quota_booked_at IS NOT NULL AND quota_released_at IS NULL").Scan(&r.DeletingBytes)
	})
	return r, err
}

func scanIDs(ctx context.Context, tx store.TxAccountScope, table, cond string, args ...any) ([]string, error) {
	rows, err := tx.QueryPage(ctx, table, "id", cond, []store.OrderBy{{Column: "id"}}, gcBatch, 0, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// resetStaleImports 走与 PrepareGeneratedMedia 相同的重置：之后迟到的提交因 epoch
// 不符而失败，重试会重新拉取。
func (s *Service) resetStaleImports(ctx context.Context, scope store.AccountScope) (int64, error) {
	var n int64
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.CheckCreativeTextReferences(ctx); err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		cutoff := now.Add(-gcStaleImport)
		ids, err := scanIDs(ctx, tx, "creative_generation_imports", staleImport, cutoff)
		if err != nil {
			return err
		}
		for _, id := range ids {
			p, err := readGeneratedImport(ctx, tx, id)
			if err != nil {
				return err
			}
			stale, err := tx.Exists(ctx, "creative_generation_imports", "id=$3 AND "+staleImport, cutoff, id)
			if err != nil {
				return err
			}
			if !stale {
				continue
			}
			if err := resetImportInTx(ctx, tx, &p, now, nil); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// deleteUnheldRevisions 只改库内状态，不碰外部存储，所以不需要中间态。
func (s *Service) deleteUnheldRevisions(ctx context.Context, scope store.AccountScope) (int64, error) {
	var n int64
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.CheckCreativeTextReferences(ctx); err != nil {
			return err
		}
		ids, err := scanIDs(ctx, tx, "creative_content_revisions", unheldMediaRevision)
		if err != nil {
			return err
		}
		for _, id := range ids {
			var state string
			if err := tx.QueryRowForUpdate(ctx, "creative_content_revisions", "state", "id=$2", id).Scan(&state); err != nil {
				return err
			}
			if state != "ready" {
				continue
			}
			live, err := creativereference.Live(ctx, tx, []string{id})
			if err != nil {
				return err
			}
			if live[id] {
				continue
			}
			if _, err := tx.Update(ctx, "creative_content_revisions", "state='deleted'", "id=$2", id); err != nil {
				return err
			}
			if _, err := tx.Delete(ctx, "creative_content_objects", "content_revision_id=$2", id); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

func (s *Service) markUnheldBlobs(ctx context.Context, scope store.AccountScope) (int64, error) {
	var n int64
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.CheckCreativeTextReferences(ctx); err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		ids, err := scanIDs(ctx, tx, "creative_blobs", unheldBlob)
		if err != nil {
			return err
		}
		for _, id := range ids {
			// 读取不取写屏障，但会锁 blob 行并要求 ready 才写读 pin；锁住后重查即可。
			var booked *time.Time
			if err := tx.QueryRowForUpdate(ctx, "creative_blobs", "quota_booked_at", "id=$2", id).Scan(&booked); err != nil {
				return err
			}
			grace := gcUnbookedGrace
			if booked != nil {
				grace = gcBookedGrace
			}
			marked, err := tx.Update(ctx, "creative_blobs", "state='deleting',delete_after=$3", "id=$2 AND "+unheldBlob, id, now.Add(grace))
			if err != nil {
				return err
			}
			n += marked
		}
		return nil
	})
	return n, err
}

type dueBlob struct {
	id, key string
}

// deleteDueBlobs 在事务外删除对象，确认后在事务内置 deleted 并恰好释放一次额度。
// 删除失败或结果未知时保持 deleting，下一轮重试；额度不会提前释放。
func (s *Service) deleteDueBlobs(ctx context.Context, scope store.AccountScope, r *GCReport) error {
	var due []dueBlob
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.CheckCreativeTextReferences(ctx); err != nil {
			return err
		}
		const dueCondition = "state='deleting' AND delete_after<=clock_timestamp()"
		// Count mismatches separately: they must never consume this store's batch.
		var err error
		r.SkippedBlobs, err = tx.Count(ctx, "creative_blobs", dueCondition+" AND (storage_driver<>$2 OR bucket<>$3)", s.adapter.Driver(), s.adapter.Bucket())
		if err != nil {
			return err
		}
		rows, err := tx.QueryPage(ctx, "creative_blobs", "id,object_key", dueCondition+" AND storage_driver=$2 AND bucket=$3", []store.OrderBy{{Column: "delete_after"}, {Column: "id"}}, gcBatch, 0, s.adapter.Driver(), s.adapter.Bucket())
		if err != nil {
			return err
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var b dueBlob
			if err := rows.Scan(&b.id, &b.key); err != nil {
				return err
			}
			due = append(due, b)
			ids = append(ids, b.id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		// Advance the batch before I/O: even a timeout or process crash must
		// not leave the same oldest failures blocking all later due objects.
		// The write barrier also prevents concurrent collectors claiming it.
		_, err = tx.Update(ctx, "creative_blobs", "delete_after=$3", "id=ANY($2::text[]) AND state='deleting'", ids, now.Add(gcInterval))
		return err
	})
	if err != nil {
		return err
	}
	var failed error
	for _, b := range due {
		if err := s.adapter.DeleteCurrent(ctx, b.key); err != nil {
			failed = errors.Join(failed, fmt.Errorf("delete blob %s: %w", b.id, err))
			continue
		}
		released, err := finishBlobDeletion(ctx, scope, b.id)
		if err != nil {
			failed = errors.Join(failed, fmt.Errorf("finish blob %s: %w", b.id, err))
			continue
		}
		r.DeletedBlobs++
		r.ReleasedBytes += released
	}
	return failed
}

func finishBlobDeletion(ctx context.Context, scope store.AccountScope, id string) (int64, error) {
	var released int64
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		var state string
		var size int64
		var booked, freed *time.Time
		if err := tx.QueryRowForUpdate(ctx, "creative_blobs", "state,byte_size,quota_booked_at,quota_released_at", "id=$2", id).Scan(&state, &size, &booked, &freed); err != nil {
			return err
		}
		if state != "deleting" {
			return nil
		}
		if booked != nil && freed == nil {
			if _, err := tx.Update(ctx, "creative_media_quotas", "stored_bytes=stored_bytes-$2,revision=revision+1", "", size); err != nil {
				return err
			}
			released = size
			if _, err := tx.Update(ctx, "creative_blobs", "state='deleted',quota_released_at=clock_timestamp()", "id=$2", id); err != nil {
				return err
			}
		} else if _, err := tx.Update(ctx, "creative_blobs", "state='deleted'", "id=$2", id); err != nil {
			return err
		}
		// 派生事实随 blob 删除。
		for _, table := range []string{"creative_blob_palettes", "creative_media_facts", "creative_media_probes", "creative_blob_read_pins"} {
			if _, err := tx.Delete(ctx, table, "blob_id=$2", id); err != nil {
				return err
			}
		}
		return nil
	})
	return released, err
}

// RunGC 每 10 分钟逐账号回收一轮，包括已停用的账号。只读模式下每个有可回收内容的
// 账号输出一行统计，这就是上线前核对用的报告。
func (s *Service) RunGC(ctx context.Context, db *store.Store, mode GCMode, logger *slog.Logger) {
	if mode == GCOff {
		return
	}
	ticker := time.NewTicker(gcInterval)
	defer ticker.Stop()
	for {
		s.collectAllAccounts(ctx, db, mode, logger)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) collectAllAccounts(ctx context.Context, db *store.Store, mode GCMode, logger *slog.Logger) {
	after := ""
	for ctx.Err() == nil {
		ids, err := db.MaintenanceAccountIDs(ctx, after, 100)
		if err != nil {
			logger.Warn("creative media gc accounts", slog.String("error", err.Error()))
			return
		}
		for _, id := range ids {
			accountCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			r, err := s.CollectGarbage(accountCtx, db.ScopeFor(auth.AccountContext{AccountID: id}), mode)
			cancel()
			if err != nil && ctx.Err() == nil {
				logger.Warn("creative media gc", slog.String("account_id", id), slog.String("error", err.Error()))
			}
			if r != (GCReport{}) {
				logger.Info("creative media gc", slog.String("account_id", id), slog.String("mode", string(mode)),
					slog.Int64("stale_imports", r.StaleImports), slog.Int64("unheld_revisions", r.UnheldRevisions),
					slog.Int64("reclaimable_blobs", r.ReclaimableBlobs), slog.Int64("reclaimable_bytes", r.ReclaimableBytes),
					slog.Int64("deleting_bytes", r.DeletingBytes), slog.Int64("deleted_blobs", r.DeletedBlobs),
					slog.Int64("released_bytes", r.ReleasedBytes), slog.Int64("skipped_blobs", r.SkippedBlobs))
			}
			after = id
		}
		if len(ids) < 100 {
			return
		}
	}
}
