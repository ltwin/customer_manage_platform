package creativeskill

import (
	"context"
	"fmt"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Publish runs the whole import protocol for one package. Both the admin
// command and the deployment's seed import need the same loop, and writing it
// twice would put its ordering rules — every upload outside a transaction, one
// operation producing one version — in two places that can drift.
//
// Replaying one operation id is the supported way to make publication
// idempotent: BeginImport hands back the record the first attempt created, a
// file that is already staged is not staged again, and a finalized import
// returns the version it already froze.
func (s *Service) Publish(ctx context.Context, scope store.AccountScope, pkg Package, intent PublishIntent) (Version, error) {
	record, err := s.BeginImport(ctx, scope, pkg.Request(intent))
	if err != nil {
		return Version{}, err
	}
	for _, name := range record.Pending {
		body, ok := pkg.Body(name)
		if !ok {
			// Unreachable today, and kept as an assertion rather than a claim:
			// the request hash covers the declarations, so an import that
			// replays at all was built from a package with these same paths.
			return Version{}, fmt.Errorf("%w: %s is declared but not in this package", creativeops.ErrValidation, name)
		}
		if _, err := s.StageResource(ctx, scope, record.ID, name, body); err != nil {
			return Version{}, err
		}
	}
	return s.FinalizeImport(ctx, scope, record.ID)
}

// Reclaimed reports what one sweep did, so an operator reading the command's
// output can tell "nothing was due" apart from "nothing could be deleted".
// None of the counts are omitted when zero: a missing field is exactly the
// reading this type exists to prevent.
type Reclaimed struct {
	Imports int `json:"imports"`
	Objects int `json:"objects"`
	// Failed counts imports this sweep marked but could not finish. They keep
	// their object list and come back on the next run.
	Failed int `json:"failed"`
}

// ReclaimExpiredImports drops the objects of imports whose window has closed.
//
// The order is the point. Each import is marked expired in its own short
// transaction under the row lock, and only then are its objects deleted.
// Deleting first would race a finalize that is about to succeed and leave a
// frozen version pointing at bytes that no longer exist.
//
// Two facts close that race rather than one. Expired is terminal — nothing
// writes an import out of it — so an import this sweep marked can never go on
// to freeze a version. And objectKey puts the import id in every key, so one
// import's objects are never another's: a version frozen by a different import
// cannot reference anything deleted here.
//
// It reclaims what the import recorded. Bytes published by a staging call that
// died before its recording transaction are not in that list and are not found
// here; nothing in the object port can enumerate a key prefix, so finding those
// waits for the general collection that FND-10 brings. Frozen versions are
// never touched — retention for those is that they are not deleted at all.
func (s *Service) ReclaimExpiredImports(ctx context.Context, scope store.AccountScope, limit int) (Reclaimed, error) {
	if limit < 1 {
		return Reclaimed{}, creativeops.ErrValidation
	}
	var due []string
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		// Already-expired rows are included: a sweep that died between the mark
		// and the deletes has to be able to finish on the next run.
		//
		// Least-recently-touched first, not oldest-expiry first, and that is what
		// keeps the queue moving. Every attempt marks its import inside the row
		// lock and writes updated_at, so an import that cannot be deleted drifts
		// behind the ones still waiting. Ordering by expires_at instead would pin
		// the same unreclaimable rows to the head of every future page, and once
		// enough of them accumulated to fill one page, nothing behind them would
		// ever be selected again.
		rows, err := tx.QueryPage(ctx, "creative_skill_imports", "id",
			"(state IN ('preparing','ready') AND expires_at<=$2) OR (state='expired' AND staged_objects<>'[]'::jsonb)",
			[]store.OrderBy{{Column: "updated_at"}}, limit, 0, now)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			due = append(due, id)
		}
		return rows.Err()
	}); err != nil {
		return Reclaimed{}, err
	}
	var report Reclaimed
	var firstFailure error
	for _, id := range due {
		objects, reclaimed, err := s.reclaimOne(ctx, scope, id)
		if err != nil {
			// One import that can never be deleted must not shadow the rest of
			// this page; the ordering above is what keeps it from shadowing the
			// pages behind it.
			report.Failed++
			if firstFailure == nil {
				firstFailure = fmt.Errorf("import %s: %w", id, err)
			}
			continue
		}
		if reclaimed {
			report.Imports++
			report.Objects += objects
		}
	}
	return report, firstFailure
}

// reclaimOne reports whether this import was actually reclaimed, separately
// from the error: an import that a finalize won in the meantime is neither a
// success to count nor a failure to report.
func (s *Service) reclaimOne(ctx context.Context, scope store.AccountScope, importID string) (int, bool, error) {
	var staged []stagedObject
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		row, err := loadImport(ctx, tx, true, "id=$2", importID)
		if err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		// Re-read under the lock: a finalize may have won the race between the
		// listing query and here, and a finalized import owns its objects.
		if !row.outOfWindow(now) {
			return nil
		}
		staged = row.Staged
		// The list survives this transaction on purpose. It is the only record
		// of which objects to delete, so clearing it before the deletes succeed
		// would turn every interrupted sweep into permanent orphans.
		_, err = tx.Update(ctx, "creative_skill_imports",
			"state='expired', revision=revision+1, updated_at=$3", "id=$2", importID, now)
		return err
	}); err != nil {
		return 0, false, err
	}
	if len(staged) == 0 {
		return 0, true, nil
	}
	for _, object := range staged {
		// An object staged into another store is not this command's to delete.
		// The bucket matters as much as the driver, and for a worse reason:
		// handing an OSS version id to the local adapter merely fails, while a
		// delete aimed at the wrong bucket of the same driver *succeeds* —
		// deleting an absent object is not an error — and this sweep would then
		// clear the only record of where the real bytes are.
		if object.Driver != s.objects.Driver() || object.Bucket != s.objects.Bucket() {
			return 0, false, fmt.Errorf("%w: %s was staged into %s/%s, not %s/%s",
				ErrResourceUnavailable, object.Path,
				object.Driver, object.Bucket, s.objects.Driver(), s.objects.Bucket())
		}
		if err := s.objects.DeleteExact(ctx, object.Key, object.Version); err != nil {
			return 0, false, fmt.Errorf("%w: delete %s: %v", ErrResourceUnavailable, object.Path, err)
		}
	}
	// The state guard is redundant today — expired is terminal — and it is here
	// so that a future "resume an expired import" cannot silently drop the list
	// of objects that import still owns.
	return len(staged), true, scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, err := tx.Update(ctx, "creative_skill_imports",
			"staged_objects='[]'::jsonb, revision=revision+1, updated_at=clock_timestamp()",
			"id=$2 AND state='expired'", importID)
		return err
	})
}
