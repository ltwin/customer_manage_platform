package creativelibrary

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const trashExpiryKind = "library.trash_expiry"
const trashExpiryBatch = 100

// TrashExpiry removes only expired library roots. Content, nodes and run roots
// remain owned by their domains; this job never deletes an object-store file.
type TrashExpiry struct{ runtime jobs.Runtime }

func NewTrashExpiry(runtime jobs.Runtime) *TrashExpiry { return &TrashExpiry{runtime: runtime} }
func (s *TrashExpiry) SetRuntime(runtime jobs.Runtime) { s.runtime = runtime }
func (s *TrashExpiry) Handler() store.JobHandler {
	return store.JobHandler{Kind: trashExpiryKind, Queue: "maintenance", Work: s.work}
}
func expiryRequest() jobs.Request {
	return jobs.Request{Kind: trashExpiryKind, OperationID: uuid.NewString(), CreatedAt: time.Now().UTC(), Payload: json.RawMessage(`{}`), UniqueByArgs: true}
}
func (s *TrashExpiry) work(ctx context.Context, scope store.AccountScope, job jobs.Request) error {
	// The receipt and next batch commit with the deletion. Replaying an attempt
	// cannot delete an additional batch or enqueue the continuation twice.
	_, err := libraryCommand(ctx, scope, creativeops.Command{OperationID: job.OperationID, CreatedAt: job.CreatedAt, Payload: job.Payload}, "trash_expiry", func(map[string]string) error { return nil }, func(ctx context.Context, tx store.TxAccountScope, _ map[string]string) (EmptyTrashResult, string, creativeops.Revision, error) {
		rows, err := tx.QueryPage(ctx, "creative_assets", "id", "deleted_at IS NOT NULL AND purge_after <= clock_timestamp()", []store.OrderBy{{Column: "id"}}, trashExpiryBatch, 0)
		if err != nil {
			return EmptyTrashResult{}, "", 0, err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return EmptyTrashResult{}, "", 0, err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return EmptyTrashResult{}, "", 0, err
		}
		for _, id := range ids {
			if err := purgeAsset(ctx, tx, id); err != nil {
				return EmptyTrashResult{}, "", 0, err
			}
		}
		if _, err := tx.Delete(ctx, "creative_asset_group_deletions", "created_at <= clock_timestamp()-interval '30 days'"); err != nil {
			return EmptyTrashResult{}, "", 0, err
		}
		result, err := libraryResult(ctx, tx, "trash", 1, len(ids) > 0, false)
		if err != nil {
			return EmptyTrashResult{}, "", 0, err
		}
		if len(ids) == trashExpiryBatch {
			next := expiryRequest()
			// Distinguish the continuation from the running scanner job's unique args.
			next.Payload = json.RawMessage(`{"after_batch":"` + job.OperationID + `"}`)
			if _, err := jobs.EnqueueInTx(ctx, tx.Jobs(s.runtime), next); err != nil {
				return EmptyTrashResult{}, "", 0, err
			}
		}
		return EmptyTrashResult{PurgedCount: len(ids), LibraryRevision: result.LibraryRevision}, result.ID, result.LibraryRevision, nil
	})
	return err
}

// SchedulePage traverses all accounts without a fixed total cap, including
// accounts with only expired group snapshots. The queue rejects inactive ones.
func (s *TrashExpiry) SchedulePage(ctx context.Context, db *store.Store, after string) (string, error) {
	ids, err := db.MaintenanceAccountIDs(ctx, after, 100)
	if err != nil {
		return after, err
	}
	for _, id := range ids {
		err := db.ScopeFor(auth.AccountContext{AccountID: id}).WithTxScope(ctx, func(tx store.TxAccountScope) error {
			due, err := tx.Exists(ctx, "creative_assets", "deleted_at IS NOT NULL AND purge_after <= clock_timestamp()")
			if err != nil {
				return err
			}
			if !due {
				due, err = tx.Exists(ctx, "creative_asset_group_deletions", "created_at <= clock_timestamp()-interval '30 days'")
				if err != nil {
					return err
				}
			}
			if !due {
				return nil
			}
			_, err = jobs.EnqueueInTx(ctx, tx.Jobs(s.runtime), expiryRequest())
			return err
		})
		if err != nil {
			return after, err
		}
		after = id
	}
	if len(ids) < 100 {
		return "", nil
	}
	return after, nil
}

// Run schedules on startup and hourly, independently of any page or API call.
// The worker process owns and joins this loop during shutdown.
func (s *TrashExpiry) Run(ctx context.Context, db *store.Store, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		cursor := ""
		for {
			pageCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			next, err := s.SchedulePage(pageCtx, db, cursor)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					logger.Warn("library trash expiry schedule", slog.String("error", err.Error()))
				}
				break
			}
			if next == "" || ctx.Err() != nil {
				break
			}
			cursor = next
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
