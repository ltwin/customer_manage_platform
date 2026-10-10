package creativemedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const generationImportLease = 5 * time.Minute

// PreparedMediaImport 的私有字段防止调用方伪造对象/版本或跨账号移交。
type PreparedMediaImport struct {
	account, id, identity, state                                 string
	epoch                                                        int64
	original, display, originalVersion, displayVersion, revision string
	reserved                                                     int64
	verified                                                     Verified
}

const importColumns = "identity_hash,state,epoch,original_blob,display_blob,original_version,display_version,verified,content_revision_id,reserved_bytes"

func readGeneratedImport(ctx context.Context, tx store.TxAccountScope, id string) (PreparedMediaImport, error) {
	p := PreparedMediaImport{account: tx.AccountID(), id: id}
	var raw []byte
	err := tx.QueryRowForUpdate(ctx, "creative_generation_imports", importColumns, "id=$2", id).Scan(&p.identity, &p.state, &p.epoch, &p.original, &p.display, &p.originalVersion, &p.displayVersion, &raw, &p.revision, &p.reserved)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(raw, &p.verified)
	return p, err
}
func generatedObjectKey(account, id, role string) string {
	return path.Join("creative-v2", account, "blobs", id, role)
}

// PrepareGeneratedMedia 有界地获取、校验并保存对象。重试只复用输出身份，
// stored阶段按精确版本和摘要复核，不依赖临时URL，不重复保存ready修订。
// exact为false时size只是取件上限（供应商不给字节数），实际字节以校验结果为准。
func (s *Service) PrepareGeneratedMedia(ctx context.Context, scope store.AccountScope, identity, kind, mime string, size int64, exact bool, fetch func(context.Context) (io.ReadCloser, error)) (PreparedMediaImport, error) {
	if identity == "" || len(identity) > 1024 || !creativecontent.IsMediaKind(kind) || size < 1 || size > s.verifier.MaxBytes(kind) || fetch == nil {
		return PreparedMediaImport{}, ErrSizeLimit
	}
	sum := sha256.Sum256([]byte(identity))
	id := "ccgi_" + hex.EncodeToString(sum[:])
	hash := creativegraph.Hash(creativegraph.Canonical([]any{identity, kind, mime, size}))
	var p PreparedMediaImport
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		if _, err := creativelibrary.LockLibraryInTx(ctx, tx); err != nil {
			return err
		}
		var err error
		p, err = readGeneratedImport(ctx, tx, id)
		if errors.Is(err, store.ErrNoRows) {
			if err = tx.Insert(ctx, "creative_generation_imports", []string{"id", "identity_hash", "state"}, id, hash, "writing"); err != nil {
				return err
			}
			p = PreparedMediaImport{account: scope.AccountID(), id: id, identity: hash, state: "writing", epoch: 1}
		} else if err != nil {
			return err
		}
		if p.identity != hash {
			return ErrState
		}
		if p.state == "stored" || p.state == "ready" {
			return nil
		}
		var lease *time.Time
		if err = tx.QueryRow(ctx, "creative_generation_imports", "lease_until", "id=$2", id).Scan(&lease); err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if lease != nil && now.Before(*lease) {
			return ErrEpoch
		}
		// 前次写入可能成功但回执丢失：旧身份仅供对象清理，绝不覆盖或复用。
		for _, blob := range []string{p.original, p.display} {
			if blob != "" {
				if _, err = tx.Update(ctx, "creative_blobs", "state='deleting',delete_after=$3", "id=$2 AND state='verifying'", blob, now.Add(48*time.Hour)); err != nil {
					return err
				}
			}
		}
		if p.reserved > 0 {
			if _, err = tx.Update(ctx, "creative_media_quotas", "reserved_bytes=reserved_bytes-$2,revision=revision+1", "", p.reserved); err != nil {
				return err
			}
		}
		p.epoch++
		_, err = tx.Update(ctx, "creative_generation_imports", "epoch=$3,lease_until=$4,original_blob='',display_blob='',original_version='',display_version='',verified='{}',reserved_bytes=0", "id=$2", id, p.epoch, now.Add(generationImportLease))
		return err
	})
	if err != nil {
		return p, err
	}
	if p.state == "ready" {
		return p, nil
	}
	if p.state == "stored" {
		return p, s.verifyStoredImport(ctx, p)
	}
	callCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	err = s.writeGeneratedMedia(callCtx, scope, &p, kind, mime, size, exact, fetch)
	if err != nil {
		// 仅释放自己的lease；失败详情不包含签名URL。崩溃由lease到期恢复。
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer finishCancel()
		cleanupErr := scope.WithTxScope(finishCtx, func(tx store.TxAccountScope) error {
			_, e := tx.Update(finishCtx, "creative_generation_imports", "lease_until=NULL", "id=$2 AND epoch=$3 AND state='writing'", p.id, p.epoch)
			return e
		})
		return p, errors.Join(err, cleanupErr)
	}
	return p, nil
}
func (s *Service) writeGeneratedMedia(ctx context.Context, scope store.AccountScope, p *PreparedMediaImport, kind, mime string, size int64, exact bool, fetch func(context.Context) (io.ReadCloser, error)) error {
	body, err := fetch(ctx)
	if err != nil {
		return err
	}
	verified, tmp, err := s.verifier.Verify(ctx, body, kind, mime, size)
	_ = body.Close()
	if err != nil {
		return err
	}
	displayPath := ""
	if verified.Display != nil {
		displayPath = verified.Display.Path
	}
	defer func() {
		_ = os.Remove(tmp)
		if displayPath != "" {
			_ = os.Remove(displayPath)
		}
	}()
	if verified.DurationMs != nil && (s.cfg.GeneratedMaxDuration <= 0 || *verified.DurationMs > s.cfg.GeneratedMaxDuration.Milliseconds()) {
		return ErrSizeLimit
	}
	if exact && verified.Size != size || verified.Size > size || verified.Mime != mime {
		return ErrSizeLimit
	}
	p.verified = verified
	p.original = "ccbl_" + uuid.NewString()
	p.display = ""
	p.originalVersion = ""
	p.displayVersion = ""
	if verified.Display != nil {
		p.display = "ccbl_" + uuid.NewString()
	}
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		current, err := readGeneratedImport(ctx, tx, p.id)
		if err != nil {
			return err
		}
		if current.epoch != p.epoch || current.state != "writing" {
			return ErrEpoch
		}
		total := verified.Size
		if verified.Display != nil {
			total += verified.Display.Size
		}
		if err = s.reserveQuota(ctx, tx, total); err != nil {
			return err
		}
		if _, err = tx.Update(ctx, "creative_generation_imports", "reserved_bytes=$3", "id=$2", p.id, total); err != nil {
			return err
		}
		if err = tx.Insert(ctx, "creative_blobs", []string{"id", "storage_driver", "bucket", "object_key", "sha256", "byte_size", "mime", "width", "height", "duration_ms", "codec_metadata", "state"}, p.original, s.adapter.Driver(), s.adapter.Bucket(), generatedObjectKey(p.account, p.original, "original"), verified.SHA256, verified.Size, verified.Mime, verified.Width, verified.Height, verified.DurationMs, verified.Codecs, "verifying"); err != nil {
			return err
		}
		if d := verified.Display; d != nil {
			if err = tx.Insert(ctx, "creative_blobs", []string{"id", "storage_driver", "bucket", "object_key", "sha256", "byte_size", "mime", "width", "height", "state"}, p.display, s.adapter.Driver(), s.adapter.Bucket(), generatedObjectKey(p.account, p.display, "display"), d.SHA256, d.Size, d.Mime, d.Width, d.Height, "verifying"); err != nil {
				return err
			}
		}
		_, err = tx.Update(ctx, "creative_generation_imports", "original_blob=$3,display_blob=$4,verified=$5", "id=$2", p.id, p.original, p.display, creativegraph.Canonical(verified))
		return err
	})
	if err != nil {
		return err
	}
	publish := func(blob, role, file, mime string, size int64) (string, error) {
		f, err := os.Open(file)
		if err != nil {
			return "", err
		}
		defer func() { _ = f.Close() }()
		version, err := s.adapter.PublishVerified(ctx, generatedObjectKey(p.account, blob, role), f, size, mime)
		if err != nil {
			return "", err
		}
		err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			current, err := readGeneratedImport(ctx, tx, p.id)
			if err != nil {
				return err
			}
			if current.epoch != p.epoch || current.state != "writing" {
				return ErrEpoch
			}
			_, err = tx.Update(ctx, "creative_blobs", "object_version=$3", "id=$2 AND state='verifying'", blob, version)
			return err
		})
		return version, err
	}
	p.originalVersion, err = publish(p.original, "original", tmp, verified.Mime, verified.Size)
	if err != nil {
		return err
	}
	if d := verified.Display; d != nil {
		p.displayVersion, err = publish(p.display, "display", d.Path, d.Mime, d.Size)
		if err != nil {
			return err
		}
		d.Path = ""
	}
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		n, err := tx.Update(ctx, "creative_generation_imports", "state='stored',original_version=$4,display_version=$5,verified=$6,lease_until=NULL", "id=$2 AND epoch=$3 AND state='writing'", p.id, p.epoch, p.originalVersion, p.displayVersion, creativegraph.Canonical(p.verified))
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrEpoch
		}
		return nil
	})
	if err == nil {
		p.state = "stored"
	}
	return err
}
func (s *Service) verifyStoredImport(ctx context.Context, p PreparedMediaImport) error {
	check := func(blob, role, version, digest string, size int64) error {
		body, stat, err := s.adapter.OpenVersion(ctx, generatedObjectKey(p.account, blob, role), version, nil)
		if err != nil {
			return err
		}
		defer func() { _ = body.Close() }()
		if stat.Version != version || stat.Size != size {
			return ErrState
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(body, size+1))
		if err != nil {
			return err
		}
		if n != size || "sha256-"+hex.EncodeToString(h.Sum(nil)) != digest {
			return ErrState
		}
		return nil
	}
	if err := check(p.original, "original", p.originalVersion, p.verified.SHA256, p.verified.Size); err != nil {
		return err
	}
	if d := p.verified.Display; d != nil {
		return check(p.display, "display", p.displayVersion, d.SHA256, d.Size)
	}
	return nil
}

// CommitGeneratedMediaInTx 由外层Gateway请求锁保护，调用方先锁library/canvas。
// ready修订、配额和真实候选/执行根与Gateway交接同事务；对象网络I/O已结束。
func (s *Service) CommitGeneratedMediaInTx(ctx context.Context, tx store.TxAccountScope, p PreparedMediaImport, retain func(creativecontent.Revision) error) (creativecontent.Revision, error) {
	if p.account != tx.AccountID() || retain == nil {
		return creativecontent.Revision{}, ErrState
	}
	current, err := readGeneratedImport(ctx, tx, p.id)
	if err != nil {
		return creativecontent.Revision{}, err
	}
	if current.identity != p.identity || current.epoch != p.epoch {
		return creativecontent.Revision{}, ErrEpoch
	}
	if current.state == "ready" {
		return creativecontent.RequireUsable(ctx, tx, current.revision, "display")
	}
	if current.state != "stored" {
		return creativecontent.Revision{}, ErrState
	}
	total := current.verified.Size
	if d := current.verified.Display; d != nil {
		total += d.Size
	}
	if current.reserved != total {
		return creativecontent.Revision{}, ErrState
	}
	if _, err = tx.Update(ctx, "creative_media_quotas", "reserved_bytes=reserved_bytes-$2,stored_bytes=stored_bytes+$2,revision=revision+1", "", total); err != nil {
		return creativecontent.Revision{}, err
	}
	objects := []creativecontent.ObjectBinding{{Role: "original", BlobID: current.original}}
	for _, v := range []struct{ id, version, role string }{{current.original, current.originalVersion, "original"}, {current.display, current.displayVersion, "display"}} {
		if v.id == "" {
			continue
		}
		n, e := tx.Update(ctx, "creative_blobs", "state='ready',verified_at=clock_timestamp(),quota_booked_at=clock_timestamp()", "id=$2 AND object_version=$3 AND state='verifying'", v.id, v.version)
		if e != nil {
			return creativecontent.Revision{}, e
		}
		if n != 1 {
			return creativecontent.Revision{}, ErrState
		}
		if v.role == "display" {
			objects = append(objects, creativecontent.ObjectBinding{Role: v.role, BlobID: v.id})
		}
	}
	revision, err := creativecontent.WriteMediaAndRetain(ctx, tx, creativecontent.Draft{Kind: current.verified.Kind}, objects, retain)
	if err != nil {
		return creativecontent.Revision{}, err
	}
	_, err = tx.Update(ctx, "creative_generation_imports", "state='ready',content_revision_id=$3,reserved_bytes=0", "id=$2", p.id, revision.ID)
	return revision, err
}
