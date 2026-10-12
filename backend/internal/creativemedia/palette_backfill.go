package creativemedia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"log/slog"
	"time"

	"github.com/disintegration/imaging"
	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativepalette"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

const PaletteBackfillKind = "media.palette_backfill"
const paletteBatch = 50
const paletteTimeout = 10 * time.Minute

// The original binding excludes display/thumbnail derivatives from the scan.
const missingPalette = `state='ready' AND mime IN ('image/jpeg','image/png','image/webp')
 AND EXISTS(SELECT 1 FROM creative_content_objects o WHERE o.account_id=$1 AND o.blob_id=creative_blobs.id AND o.role='original')
 AND NOT EXISTS(SELECT 1 FROM creative_blob_palettes p WHERE p.account_id=$1 AND p.blob_id=creative_blobs.id AND p.algorithm=$2)`

type palettePayload struct {
	Algorithm  string `json:"algorithm"`
	AfterBatch string `json:"after_batch,omitempty"`
}

func paletteRequest(after string) jobs.Request {
	raw, _ := json.Marshal(palettePayload{Algorithm: creativepalette.Algorithm, AfterBatch: after})
	return jobs.Request{Kind: PaletteBackfillKind, OperationID: uuid.NewString(), CreatedAt: time.Now().UTC(), Payload: raw, UniqueByArgs: true}
}
func validatePalette(p palettePayload) error {
	if p.Algorithm != creativepalette.Algorithm {
		return creativeops.ErrValidation
	}
	return nil
}

type paletteRead struct {
	id, key, version, pin string
	result                creativepalette.Result
}

func (s *Service) workPalette(ctx context.Context, scope store.AccountScope, job jobs.Request) (err error) {
	var payload palettePayload
	if err = creativeops.Decode(job.Payload, &payload); err != nil {
		return err
	}
	if err = validatePalette(payload); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, paletteTimeout)
	defer cancel()
	var ids []string
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if e := tx.RequireCreativeCapability(ctx, "media_write"); e != nil {
			return e
		}
		rows, e := tx.QueryPage(ctx, "creative_blobs", "id", missingPalette, []store.OrderBy{{Column: "id"}}, paletteBatch, 0, creativepalette.Algorithm)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				return e
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	var reads []paletteRead
	var pins []string
	defer func() {
		// An uncertain pin commit can leave only a bounded lease; all known pins are
		// released even when storage, decoding, or the final transaction fails.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if len(pins) > 0 {
			err = errors.Join(err, scope.WithTxScope(cleanup, func(tx store.TxAccountScope) error {
				_, e := tx.Delete(cleanup, "creative_blob_read_pins", "id=ANY($2::text[])", pins)
				return e
			}))
		}
	}()
	for _, id := range ids {
		read, ok, e := s.pinPalette(ctx, scope, id, &pins)
		if e != nil {
			return e
		}
		if !ok {
			continue
		}
		read.result, e = s.readPalette(ctx, read)
		if e != nil {
			return e
		}
		reads = append(reads, read)
	}
	// A receipt prevents a replay from writing another batch or continuing twice.
	_, err = run(ctx, scope, PaletteBackfillKind, creativeops.Command{OperationID: job.OperationID, CreatedAt: job.CreatedAt, Payload: job.Payload}, validatePalette, func(ctx context.Context, tx store.TxAccountScope, _ palettePayload) (creativeops.Outcome, error) {
		for _, r := range reads {
			alive, e := tx.Exists(ctx, "creative_blob_read_pins", "id=$2 AND expires_at>clock_timestamp()", r.pin)
			if e != nil {
				return creativeops.Outcome{}, e
			}
			if !alive {
				return creativeops.Outcome{}, context.DeadlineExceeded
			}
			// The writer barrier serializes this recheck with deletion and rebuild.
			missing, e := tx.Exists(ctx, "creative_blobs", missingPalette+" AND id=$3", creativepalette.Algorithm, r.id)
			if e != nil {
				return creativeops.Outcome{}, e
			}
			if missing {
				if e = creativepalette.WriteInTx(ctx, tx, r.id, r.result); e != nil {
					return creativeops.Outcome{}, e
				}
			}
		}
		more, e := tx.Exists(ctx, "creative_blobs", missingPalette, creativepalette.Algorithm)
		if e != nil {
			return creativeops.Outcome{}, e
		}
		if more {
			if _, e = jobs.EnqueueInTx(ctx, tx.Jobs(s.runtime), paletteRequest(job.OperationID)); e != nil {
				return creativeops.Outcome{}, e
			}
		}
		return outcome(200, "palette", job.OperationID, 1, map[string]int{"processed": len(reads)})
	})
	return err
}

func (s *Service) pinPalette(ctx context.Context, scope store.AccountScope, id string, pins *[]string) (paletteRead, bool, error) {
	read := paletteRead{id: id}
	ready := false
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		var state, mime string
		err := tx.QueryRowForUpdate(ctx, "creative_blobs", "state,mime,object_key,object_version", "id=$2", id).Scan(&state, &mime, &read.key, &read.version)
		if errors.Is(err, store.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if state != "ready" {
			return nil
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		pin := func(blob string) (string, error) {
			p := "ccpn_" + uuid.NewString()
			*pins = append(*pins, p)
			return p, tx.Insert(ctx, "creative_blob_read_pins", []string{"id", "blob_id", "owner_kind", "expires_at"}, p, blob, "processing", now.Add(paletteTimeout+time.Minute))
		}
		read.pin, err = pin(id)
		if err != nil {
			return err
		}
		// Existing displays are JPEG and discard alpha. Only JPEG originals may
		// use them; PNG/WebP must retain the original's transparency semantics.
		if mime != "image/jpeg" {
			ready = true
			return nil
		}
		// Pin the display alongside the original before reading its fixed version.
		var display, key, version string
		err = tx.QueryRow(ctx, "creative_blobs", "id,object_key,object_version", `state='ready' AND id IN (SELECT d.blob_id FROM creative_content_objects o JOIN creative_content_objects d ON d.account_id=o.account_id AND d.content_revision_id=o.content_revision_id AND d.role='display' WHERE o.account_id=$1 AND o.blob_id=$2 AND o.role='original')`, id).Scan(&display, &key, &version)
		if err != nil && !errors.Is(err, store.ErrNoRows) {
			return err
		}
		if err == nil {
			if _, err = pin(display); err != nil {
				return err
			}
			read.key, read.version = key, version
		}
		ready = true
		return nil
	})
	return read, ready, err
}
func (s *Service) readPalette(ctx context.Context, r paletteRead) (creativepalette.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, stat, err := s.adapter.OpenVersion(ctx, r.key, r.version, nil)
	// A version that no longer exists cannot recover through queue retries.
	// Record it so one missing object cannot starve every later batch.
	if errors.Is(err, versionedfs.ErrNotFound) {
		return creativepalette.Failed("object_missing"), nil
	}
	if err != nil {
		return creativepalette.Result{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(body, s.cfg.ImageMaxBytes+1))
	closeErr := body.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return creativepalette.Result{}, err
	}
	if ctx.Err() != nil {
		return creativepalette.Result{}, ctx.Err()
	}
	if int64(len(raw)) > s.cfg.ImageMaxBytes {
		return creativepalette.Failed("size_limit"), nil
	}
	// A short successful stream is an incomplete storage read, not proof of
	// corrupt image bytes. Retry the entire batch instead of persisting failure.
	if stat.Size <= 0 || int64(len(raw)) != stat.Size {
		return creativepalette.Result{}, errors.New("incomplete palette object read")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return creativepalette.Failed("image_decode"), nil
	}
	if !s.verifier.validDimensions(cfg.Width, cfg.Height) {
		return creativepalette.Failed("invalid_dimensions"), nil
	}
	img, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return creativepalette.Failed("image_decode"), nil
	}
	return creativepalette.Extract(img), nil
}

// RebuildPalettes is an account-scoped operator command. Unsupported algorithm
// versions are refused before deleting anything. Queue insertion is atomic.
func RebuildPalettes(ctx context.Context, scope store.AccountScope, runtime jobs.Runtime, algorithm string) error {
	if algorithm != creativepalette.Algorithm {
		return creativeops.ErrValidation
	}
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		if _, err := tx.Delete(ctx, "creative_blob_palettes", "algorithm=$2", algorithm); err != nil {
			return err
		}
		_, err := jobs.EnqueueInTx(ctx, tx.Jobs(runtime), paletteRequest("rebuild-"+uuid.NewString()))
		return err
	})
}

// SchedulePalettes schedules every account page on worker startup. Restart or
// rebuild picks up missing records after retries are exhausted, without a cap.
func (s *Service) SchedulePalettes(ctx context.Context, db *store.Store) error {
	cursor := ""
	for {
		ids, err := db.MaintenanceAccountIDs(ctx, cursor, 100)
		if err != nil {
			return err
		}
		for _, id := range ids {
			err = db.ScopeFor(auth.AccountContext{AccountID: id}).WithTxScope(ctx, func(tx store.TxAccountScope) error {
				missing, e := tx.Exists(ctx, "creative_blobs", missingPalette, creativepalette.Algorithm)
				if e != nil || !missing {
					return e
				}
				_, e = jobs.EnqueueInTx(ctx, tx.Jobs(s.runtime), paletteRequest(""))
				return e
			})
			if err != nil {
				return err
			}
			cursor = id
		}
		if len(ids) < 100 {
			return nil
		}
	}
}

// RunPaletteBackfill retries startup scheduling on transient failure. The worker
// owns this finite goroutine and joins it on shutdown.
func (s *Service) RunPaletteBackfill(ctx context.Context, db *store.Store, logger *slog.Logger) {
	for {
		if err := s.SchedulePalettes(ctx, db); err == nil {
			return
		} else if ctx.Err() == nil {
			logger.Warn("palette backfill schedule", slog.String("error", err.Error()))
		}
		timer := time.NewTimer(time.Minute)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
