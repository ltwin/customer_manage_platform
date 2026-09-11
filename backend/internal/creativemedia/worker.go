package creativemedia

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Lease budgets match the job attempt timeouts registered in Handlers: a
// lease must outlive the longest work its holder can still perform, or a
// concurrent claim would treat a live worker as dead.
const (
	leaseDuration = 2 * time.Minute // queue default attempt timeout
	completeLease = 5 * time.Minute
	verifyLease   = 20 * time.Minute
)

// LeaseBudget exposes the per-stage lease so tests can hold the invariant
// "lease covers the job attempt timeout" against Handlers.
func LeaseBudget(kind string) time.Duration {
	switch kind {
	case jobComplete:
		return completeLease
	case jobVerify:
		return verifyLease
	default:
		return leaseDuration
	}
}

type jobPayload struct {
	UploadID string `json:"upload_id"`
}

func decodeJob(job jobs.Request) (string, error) {
	var p jobPayload
	if err := creativeops.Decode(job.Payload, &p); err != nil {
		return "", err
	}
	if p.UploadID == "" {
		return "", creativeops.ErrValidation
	}
	return p.UploadID, nil
}

// claim takes execution authority for one external stage: the state/phase
// must match, no live lease may exist and the session must not be expired.
// Every later write checks the returned epoch.
func (s *Service) claim(ctx context.Context, scope store.AccountScope, id, state, phase string, budget time.Duration) (upload, bool, error) {
	var u upload
	claimed := false
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "media_write"); err != nil {
			return err
		}
		var err error
		u, err = lockUpload(ctx, tx, id)
		if err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		// A promotion whose worker died holds a preallocated blob and a lapsed
		// lease. Nothing can resume an external write it cannot verify, so it
		// fails explicitly; the deleting blob rows keep the objects for
		// reconciliation instead of leaking a silent 24h stall.
		if u.State == "verifying" && u.IOPhase == "promoting" && (u.Lease == nil || !now.Before(*u.Lease)) {
			s.logger.Warn("creative media promotion interrupted; failing session for reconciliation",
				slog.String("upload_id", u.ID), slog.String("account_id", scope.AccountID()), slog.Int64("epoch", u.Epoch))
			return s.finish(ctx, tx, u, "failed", "promote_interrupted")
		}
		if u.State != state || u.IOPhase != phase {
			return nil
		}
		if !now.Before(u.ExpiresAt) {
			return s.finish(ctx, tx, u, "expired", "expired")
		}
		if u.Lease != nil && now.Before(*u.Lease) {
			return ErrEpoch
		}
		u.Epoch++
		lease := now.Add(budget)
		u.Lease = &lease
		if _, err = tx.Update(ctx, "creative_uploads", "execution_epoch=$2,lease_until=$3,updated_at=clock_timestamp()", "id=$4", u.Epoch, lease, u.ID); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return u, claimed, err
}

// withEpoch applies a state transition only while this worker still owns the
// session; a stale epoch is reported, never silently overwritten.
func (s *Service) withEpoch(ctx context.Context, scope store.AccountScope, u upload, apply func(store.TxAccountScope, upload) error) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "media_write"); err != nil {
			return err
		}
		current, err := lockUpload(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		if current.Epoch != u.Epoch || current.State != u.State || current.IOPhase != u.IOPhase {
			return ErrEpoch
		}
		return apply(tx, current)
	})
}
func (s *Service) fail(ctx context.Context, scope store.AccountScope, u upload, code string, cause error) error {
	s.logger.Warn("creative media upload failed",
		slog.String("upload_id", u.ID), slog.String("account_id", scope.AccountID()),
		slog.String("state", u.State), slog.String("io_phase", u.IOPhase), slog.Int64("epoch", u.Epoch),
		slog.String("kind", u.Kind), slog.Int64("size", u.Size), slog.String("driver", s.adapter.Driver()),
		slog.String("code", code), slog.String("error", cause.Error()))
	return s.withEpoch(ctx, scope, u, func(tx store.TxAccountScope, current upload) error {
		return s.finish(ctx, tx, current, "failed", failureCode(code))
	})
}

// workInit: created/initializing → uploading/none. The multipart id is fixed
// in the database before any client can request part signatures.
func (s *Service) workInit(ctx context.Context, scope store.AccountScope, job jobs.Request) error {
	id, err := decodeJob(job)
	if err != nil {
		return err
	}
	u, claimed, err := s.claim(ctx, scope, id, "created", "none", leaseDuration)
	if err != nil || !claimed {
		return err
	}
	if err := s.withEpoch(ctx, scope, u, func(tx store.TxAccountScope, _ upload) error {
		_, err := tx.Update(ctx, "creative_uploads", "io_phase='initializing',updated_at=clock_timestamp()", "id=$2", u.ID)
		return err
	}); err != nil {
		return err
	}
	u.IOPhase = "initializing"
	session, err := s.adapter.InitMultipart(ctx, u.StagingKey, u.Mime)
	if err != nil {
		return s.fail(ctx, scope, u, "init_failed", err)
	}
	return s.withEpoch(ctx, scope, u, func(tx store.TxAccountScope, _ upload) error {
		_, err := tx.Update(ctx, "creative_uploads", "state='uploading',io_phase='none',multipart_id=$2,lease_until=NULL,revision=revision+1,updated_at=clock_timestamp()", "id=$3", session, u.ID)
		return err
	})
}

// workComplete: uploading/completing → verifying/none. The server lists the
// parts the storage really holds; client-reported etags are never trusted.
func (s *Service) workComplete(ctx context.Context, scope store.AccountScope, job jobs.Request) error {
	id, err := decodeJob(job)
	if err != nil {
		return err
	}
	u, claimed, err := s.claim(ctx, scope, id, "uploading", "completing", completeLease)
	if err != nil || !claimed {
		return err
	}
	if u.MultipartID == nil {
		return s.fail(ctx, scope, u, "complete_failed", errors.New("missing multipart session"))
	}
	parts, err := s.adapter.ListParts(ctx, u.StagingKey, *u.MultipartID)
	if err != nil {
		return s.fail(ctx, scope, u, "complete_failed", err)
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Number < parts[j].Number })
	var total int64
	for i, p := range parts {
		if p.Number != i+1 {
			return s.fail(ctx, scope, u, "parts_incomplete", fmt.Errorf("part %d missing", i+1))
		}
		total += p.Size
	}
	if len(parts) != s.partCount(u.Size) || total != u.Size {
		return s.fail(ctx, scope, u, "parts_incomplete", fmt.Errorf("%d parts / %d bytes", len(parts), total))
	}
	version, err := s.adapter.CompleteMultipart(ctx, u.StagingKey, *u.MultipartID, parts)
	if err != nil {
		return s.fail(ctx, scope, u, "complete_failed", err)
	}
	return s.withEpoch(ctx, scope, u, func(tx store.TxAccountScope, _ upload) error {
		for _, p := range parts {
			if err := tx.Upsert(ctx, "creative_upload_parts", []string{"upload_id", "part_number", "etag", "byte_size"}, []string{"account_id", "upload_id", "part_number"}, []string{"etag", "byte_size"}, u.ID, p.Number, p.ETag, p.Size); err != nil {
				return err
			}
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Update(ctx, "creative_uploads", "state='verifying',io_phase='none',staging_version=$2,lease_until=NULL,revision=revision+1,updated_at=clock_timestamp()", "id=$3", version, u.ID); err != nil {
			return err
		}
		return s.enqueue(ctx, tx, jobVerify, job.OperationID, now, u.ID)
	})
}

// workVerify: verifying/none → verifying/promoting → ready (or failed). The
// blob identity and final key are fixed before the object write so an
// unknown result can be reconciled against a known location.
func (s *Service) workVerify(ctx context.Context, scope store.AccountScope, job jobs.Request) error {
	id, err := decodeJob(job)
	if err != nil {
		return err
	}
	u, claimed, err := s.claim(ctx, scope, id, "verifying", "none", verifyLease)
	if err != nil || !claimed {
		return err
	}
	if u.StagingVersion == nil {
		return s.fail(ctx, scope, u, "verify_failed", errors.New("missing staging version"))
	}
	body, _, err := s.adapter.OpenVersion(ctx, u.StagingKey, *u.StagingVersion, nil)
	if err != nil {
		return s.fail(ctx, scope, u, "verify_failed", err)
	}
	verified, tmp, err := s.verifier.Verify(ctx, body, u.Kind, "", s.verifier.MaxBytes(u.Kind))
	_ = body.Close()
	if err != nil {
		code := "verify_failed"
		if errors.Is(err, ErrUnsupported) {
			code = "media_unsupported"
		} else if errors.Is(err, ErrSizeLimit) {
			code = "size_limit"
		}
		return s.fail(ctx, scope, u, code, err)
	}
	defer func() {
		_ = os.Remove(tmp)
		if verified.Display != nil {
			_ = os.Remove(verified.Display.Path)
		}
	}()
	if verified.Size != u.Size {
		return s.fail(ctx, scope, u, "size_mismatch", fmt.Errorf("declared %d actual %d", u.Size, verified.Size))
	}
	// Preallocate blob identities and final keys inside the database first.
	blobID := "ccbl_" + uuid.NewString()
	var displayID string
	if verified.Display != nil {
		displayID = "ccbl_" + uuid.NewString()
	}
	account := scope.AccountID()
	key := func(id, rendition string) string {
		return path.Join("creative-v2", account, "blobs", id, rendition)
	}
	if err := s.withEpoch(ctx, scope, u, func(tx store.TxAccountScope, _ upload) error {
		if err := tx.Insert(ctx, "creative_blobs", []string{"id", "storage_driver", "bucket", "object_key", "sha256", "byte_size", "mime", "width", "height", "duration_ms", "codec_metadata", "state"}, blobID, s.adapter.Driver(), s.adapter.Bucket(), key(blobID, "original"), verified.SHA256, verified.Size, verified.Mime, verified.Width, verified.Height, verified.DurationMs, verified.Codecs, "verifying"); err != nil {
			return err
		}
		if displayID != "" {
			d := verified.Display
			if err := tx.Insert(ctx, "creative_blobs", []string{"id", "storage_driver", "bucket", "object_key", "sha256", "byte_size", "mime", "width", "height", "codec_metadata", "state"}, displayID, s.adapter.Driver(), s.adapter.Bucket(), key(displayID, "display"), d.SHA256, d.Size, d.Mime, d.Width, d.Height, creativegraph.Canonical(map[string]string{"derived_from": blobID, "recipe": "display-v1"}), "verifying"); err != nil {
				return err
			}
		}
		_, err := tx.Update(ctx, "creative_uploads", "io_phase='promoting',blob_id=$2,updated_at=clock_timestamp()", "id=$3", blobID, u.ID)
		return err
	}); err != nil {
		return err
	}
	u.IOPhase = "promoting"
	u.BlobID = &blobID
	publish := func(id, rendition, filePath, mime string, size int64) (string, error) {
		f, err := os.Open(filePath)
		if err != nil {
			return "", err
		}
		defer func() { _ = f.Close() }()
		return s.adapter.PublishVerified(ctx, key(id, rendition), f, size, mime)
	}
	originalVersion, err := publish(blobID, "original", tmp, verified.Mime, verified.Size)
	if err != nil {
		return s.fail(ctx, scope, u, "promote_failed", err)
	}
	var displayVersion string
	if displayID != "" {
		displayVersion, err = publish(displayID, "display", verified.Display.Path, verified.Display.Mime, verified.Display.Size)
		if err != nil {
			return s.fail(ctx, scope, u, "promote_failed", err)
		}
	}
	return s.publish(ctx, scope, u, blobID, originalVersion, displayID, displayVersion, verified)
}

// publish runs the publication as a command under the server-generated
// publish_operation_id: exactly one receipt, target re-validation under the
// shared lock order, content creation and the ready handoff in one transaction.
func (s *Service) publish(ctx context.Context, scope store.AccountScope, u upload, blobID, originalVersion, displayID, displayVersion string, verified Verified) error {
	command := creativeops.Command{OperationID: u.PublishOperation, CreatedAt: u.CreatedAt, Payload: creativegraph.Canonical(map[string]any{"action": "publish_upload", "upload_id": u.ID, "blob_id": blobID})}
	_, err := run(ctx, scope, "media.publish_upload", command, func(map[string]any) error { return nil }, func(ctx context.Context, tx store.TxAccountScope, _ map[string]any) (creativeops.Outcome, error) {
		// Library root first: asset targets and node targets share this order.
		if _, err := creativelibrary.LockLibraryInTx(ctx, tx); err != nil {
			return creativeops.Outcome{}, err
		}
		current, err := lockUpload(ctx, tx, u.ID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if current.Epoch != u.Epoch || current.State != "verifying" || current.IOPhase != "promoting" || current.BlobID == nil || *current.BlobID != blobID {
			return creativeops.Outcome{}, ErrEpoch
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if !now.Before(current.ExpiresAt) {
			if err := s.finish(ctx, tx, current, "expired", "expired"); err != nil {
				return creativeops.Outcome{}, err
			}
			return outcome(200, "upload", u.ID, creativeops.Revision(current.Revision+1), map[string]string{"state": "expired"})
		}
		if _, err := tx.Update(ctx, "creative_blobs", "state='ready',object_version=$2,verified_at=$3,quota_booked_at=$3", "id=$4 AND state='verifying'", originalVersion, now, blobID); err != nil {
			return creativeops.Outcome{}, err
		}
		objects := []creativecontent.ObjectBinding{{Role: "original", BlobID: blobID}}
		if displayID != "" {
			if _, err := tx.Update(ctx, "creative_blobs", "state='ready',object_version=$2,verified_at=$3,quota_booked_at=$3", "id=$4 AND state='verifying'", displayVersion, now, displayID); err != nil {
				return creativeops.Outcome{}, err
			}
			objects = append(objects, creativecontent.ObjectBinding{Role: "display", BlobID: displayID})
		}
		stored := verified.Size
		if verified.Display != nil {
			stored += verified.Display.Size
		}
		// Renditions were not part of the reservation; settle only when the
		// total still fits, otherwise fail the session instead of tripping the
		// quota constraint mid-transaction.
		var reserved, storedNow, limit int64
		if err := tx.QueryRowForUpdate(ctx, "creative_media_quotas", "reserved_bytes,stored_bytes,limit_bytes", "").Scan(&reserved, &storedNow, &limit); err != nil {
			return creativeops.Outcome{}, err
		}
		if reserved-current.Reserved+storedNow+stored > limit {
			if err := s.finish(ctx, tx, current, "failed", "quota_exceeded"); err != nil {
				return creativeops.Outcome{}, err
			}
			return outcome(200, "upload", u.ID, creativeops.Revision(current.Revision+1), map[string]string{"state": "failed"})
		}
		if _, err := tx.Update(ctx, "creative_media_quotas", "reserved_bytes=GREATEST(reserved_bytes-$2,0),stored_bytes=stored_bytes+$3,revision=revision+1", "", current.Reserved, stored); err != nil {
			return creativeops.Outcome{}, err
		}
		target, err := targetOf(current)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		draft := creativecontent.Draft{Kind: current.Kind, DeclarationID: current.DeclarationID}
		publication, err := s.bindOrCandidate(ctx, tx, current, target, draft, objects, command.OperationID, now)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if _, err := tx.Update(ctx, "creative_uploads", "state='ready',io_phase='none',blob_id=NULL,handed_off_at=$2,published_blob_id_snapshot=$3,publication_result_kind=$4,publication_result_id=$5,publication_result_revision=$6,reserved_bytes=0,quota_settled_at=$2,lease_until=NULL,execution_epoch=execution_epoch+1,revision=revision+1,updated_at=clock_timestamp()", "id=$7", now, blobID, publication.Kind, publication.ID, int64(publication.Revision), current.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		return outcome(200, "upload", current.ID, creativeops.Revision(current.Revision+1), publication)
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, creativeops.ErrConflict):
		// The receipt exists only if the ready handoff committed with it.
		return nil
	case errors.Is(err, ErrEpoch), errors.Is(err, store.ErrCommitOutcomeUnknown), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// Someone else owns the session, or the commit outcome is unknown. The
		// error is surfaced as-is; a later claim finds the session still in
		// promoting and fails it explicitly (publication is never re-run), and
		// the written objects stay behind deleting blob rows for reconciliation.
		return err
	default:
		// A definite business failure lands as failed with its reason; the
		// written objects stay behind deleting blob rows for reconciliation.
		if failErr := s.fail(ctx, scope, u, "publish_failed", err); failErr != nil {
			return failErr
		}
		return nil
	}
}

// bindOrCandidate publishes the verified content to the intended target, or,
// when the target changed since the session was created, into a pending
// candidate. Either way the revision gets a real root in this transaction.
func (s *Service) bindOrCandidate(ctx context.Context, tx store.TxAccountScope, current upload, target Target, draft creativecontent.Draft, objects []creativecontent.ObjectBinding, operation string, now time.Time) (Publication, error) {
	targetErr := validateTargetInTx(ctx, tx, target, current.Kind)
	if targetErr != nil && !errors.Is(targetErr, creativecanvas.ErrTargetChanged) {
		return Publication{}, targetErr
	}
	if targetErr == nil && target.Kind == "asset" {
		var asset creativelibrary.AssetResult
		if _, err := creativecontent.WriteMediaAndRetain(ctx, tx, draft, objects, func(r creativecontent.Revision) error {
			var e error
			asset, e = creativelibrary.CreateFromRevisionInTx(ctx, tx, r, target.Asset.library())
			return e
		}); err != nil {
			return Publication{}, err
		}
		return Publication{Kind: "asset", ID: asset.ID, Revision: asset.Revision}, nil
	}
	if targetErr == nil {
		var change creativecanvas.ChangeResult
		_, err := creativecontent.WriteMediaAndRetain(ctx, tx, draft, objects, func(r creativecontent.Revision) error {
			var e error
			change, e = creativecanvas.BindNodeRevisionInTx(ctx, tx, target.Node.canvas(), r, operation)
			return e
		})
		if err == nil {
			return Publication{Kind: "node", ID: target.Node.NodeID, Revision: change.ResultRevision}, nil
		}
		if !errors.Is(err, creativecanvas.ErrTargetChanged) {
			return Publication{}, err
		}
	}
	candidateID := "ccuc_" + uuid.NewString()
	if _, err := creativecontent.WriteMediaAndRetain(ctx, tx, draft, objects, func(r creativecontent.Revision) error {
		return tx.Insert(ctx, "creative_upload_candidates", []string{"id", "upload_id", "content_id", "content_revision_id", "state", "reason", "target_snapshot", "original_name", "declared_kind", "expires_at"}, candidateID, current.ID, r.ContentID, r.ID, "pending", "target_changed", current.Target, current.Name, current.Kind, now.Add(s.cfg.CandidateTTL))
	}); err != nil {
		return Publication{}, err
	}
	return Publication{Kind: "candidate", ID: candidateID, Revision: 1}, nil
}

// workAbort removes staging parts of a cancelled/failed session. It is safe
// to repeat and never touches a session that is still active.
func (s *Service) workAbort(ctx context.Context, scope store.AccountScope, job jobs.Request) error {
	id, err := decodeJob(job)
	if err != nil {
		return err
	}
	var u upload
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		var err error
		u, err = scanUpload(tx.QueryRow(ctx, "creative_uploads", uploadColumns, "id=$2", id))
		return err
	}); err != nil {
		return err
	}
	switch u.State {
	case "cancelled", "failed", "expired":
	default:
		return nil
	}
	if u.MultipartID != nil {
		if err := s.adapter.AbortMultipart(ctx, u.StagingKey, *u.MultipartID); err != nil {
			return err
		}
	}
	if u.StagingVersion != nil {
		if err := s.adapter.DeleteExact(ctx, u.StagingKey, *u.StagingVersion); err != nil {
			return err
		}
	}
	return nil
}

// SweepExpired marks overdue sessions expired and releases reservations. It
// never deletes objects; physical cleanup belongs to the retention feature.
func (s *Service) SweepExpired(ctx context.Context, scope store.AccountScope, limit int) (int, error) {
	swept := 0
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "media_write"); err != nil {
			return err
		}
		rows, err := tx.QueryPage(ctx, "creative_uploads", "id", "state IN ('created','uploading','verifying') AND expires_at<=clock_timestamp() AND (lease_until IS NULL OR lease_until<=clock_timestamp())", []store.OrderBy{{Column: "expires_at"}}, limit, 0)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			u, err := lockUpload(ctx, tx, id)
			if err != nil {
				return err
			}
			if err := s.finish(ctx, tx, u, "expired", "expired"); err != nil {
				return err
			}
			swept++
		}
		_, err = tx.Update(ctx, "creative_upload_candidates", "state='expired',content_id=NULL,content_revision_id=NULL,revision=revision+1,updated_at=clock_timestamp()", "state='pending' AND expires_at<=clock_timestamp()")
		return err
	})
	return swept, err
}
