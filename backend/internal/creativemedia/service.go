package creativemedia

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

const (
	jobInit     = "media.upload_init"
	jobComplete = "media.upload_complete"
	jobVerify   = "media.upload_verify"
	jobAbort    = "media.upload_abort"
	batchLimit  = 50
)

// Service is the media application. The API process enqueues; the worker
// process executes external I/O and publishes through the same code.
type Service struct {
	cfg      Config
	adapter  versionedfs.Adapter
	verifier *Verifier
	runtime  jobs.Runtime
	tickets  ticketSigner
	logger   *slog.Logger
}

func NewService(cfg Config, adapter versionedfs.Adapter, verifier *Verifier, ticketKey []byte, ticketBase string) (*Service, error) {
	if adapter == nil || verifier == nil || len(ticketKey) < 32 || ticketBase == "" {
		return nil, errors.New("creative media service requires adapter, verifier, ticket key and base path")
	}
	return &Service{cfg: cfg, adapter: adapter, verifier: verifier, tickets: ticketSigner{key: ticketKey, base: ticketBase}, logger: slog.Default()}, nil
}

// SetRuntime binds the queue used for same-transaction enqueue.
func (s *Service) SetRuntime(runtime jobs.Runtime) { s.runtime = runtime }

// SetLogger receives the process logger; failure details go here, never to
// the client-visible error_code.
func (s *Service) SetLogger(logger *slog.Logger) {
	if logger != nil {
		s.logger = logger
	}
}

// Handlers registers the worker stages. Verification streams up to the
// audio/video limit twice (staging read, final write) and probes it, so its
// attempt budget is sized for large files instead of the queue default.
func (s *Service) Handlers() []store.JobHandler {
	return []store.JobHandler{
		{Kind: jobInit, Work: s.workInit},
		{Kind: jobComplete, Work: s.workComplete, Timeout: 5 * time.Minute},
		{Kind: jobVerify, Work: s.workVerify, Timeout: 20 * time.Minute},
		{Kind: jobAbort, Work: s.workAbort},
	}
}
func (s *Service) Capabilities() Capabilities {
	return Capabilities{SchemaVersion: 1, Formats: s.verifier.Formats(), ImageMaxBytes: s.cfg.ImageMaxBytes, AVMaxBytes: s.cfg.AVMaxBytes, PartSize: s.cfg.PartSize, BatchLimit: batchLimit}
}
func (s *Service) partCount(size int64) int {
	return int(math.Ceil(float64(size) / float64(s.cfg.PartSize)))
}

func run[T any](ctx context.Context, scope store.AccountScope, key string, c creativeops.Command, validate func(T) error, apply func(context.Context, store.TxAccountScope, T) (creativeops.Outcome, error)) (creativeops.Receipt, error) {
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: key, Capability: "media_write", Validate: func(raw json.RawMessage) error {
		var v T
		if err := creativeops.Decode(raw, &v); err != nil {
			return err
		}
		return validate(v)
	}, Apply: func(ctx context.Context, tx store.TxAccountScope, raw json.RawMessage) (creativeops.Outcome, error) {
		var v T
		if err := creativeops.Decode(raw, &v); err != nil {
			return creativeops.Outcome{}, err
		}
		return apply(ctx, tx, v)
	}}, c)
}
func outcome(status int, kind, id string, revision creativeops.Revision, value any) (creativeops.Outcome, error) {
	body, err := json.Marshal(value)
	return creativeops.Outcome{HTTPStatus: status, ResultKind: kind, ResultID: &id, ResultRevision: &revision, Response: body}, err
}

func (t Target) validate() error {
	switch t.Kind {
	case "asset":
		if t.Asset == nil || t.Node != nil {
			return creativeops.ErrValidation
		}
		return creativelibrary.ValidateImportTarget(t.Asset.library())
	case "node":
		if t.Node == nil || t.Asset != nil || t.Node.CanvasID == "" || t.Node.NodeID == "" || t.Node.ExpectedDataRevision < 1 {
			return creativeops.ErrValidation
		}
		return nil
	}
	return creativeops.ErrValidation
}
func (a AssetTarget) library() creativelibrary.ImportTarget {
	return creativelibrary.ImportTarget{Title: a.Title, Description: a.Description, GroupIDs: a.GroupIDs, TagIDs: a.TagIDs, NewTags: a.NewTags, Favorite: a.Favorite}
}
func (n NodeTarget) canvas() creativecanvas.NodeTarget {
	return creativecanvas.NodeTarget{CanvasID: n.CanvasID, NodeID: n.NodeID, ExpectedDataRevision: n.ExpectedDataRevision}
}

// validateTargetInTx takes the target's own locks in the shared order and
// reports a classifiable conflict for changed targets.
func validateTargetInTx(ctx context.Context, tx store.TxAccountScope, t Target, kind string) error {
	if t.Kind == "asset" {
		err := creativelibrary.ValidateImportTargetInTx(ctx, tx, t.Asset.library())
		if errors.Is(err, creativelibrary.ErrNotFound) {
			return creativecanvas.ErrTargetChanged
		}
		return err
	}
	return creativecanvas.ValidateNodeTargetInTx(ctx, tx, t.Node.canvas(), kind)
}

func (s *Service) validateCreate(v CreateUploadInput) error {
	if utf8.RuneCountInString(v.FileName) < 1 || utf8.RuneCountInString(v.FileName) > 255 || strings.ContainsAny(v.FileName, "/\\\x00") || v.Size < 1 {
		return creativeops.ErrValidation
	}
	f, ok := s.verifier.format(v.Mime)
	if !ok || f.Kind != v.Kind {
		return ErrUnsupported
	}
	if v.Size > s.verifier.MaxBytes(v.Kind) || s.partCount(v.Size) > s.cfg.MaxParts {
		return ErrSizeLimit
	}
	rights := v.Rights.OrDefault()
	if utf8.RuneCountInString(rights.EvidenceSummary) > 500 {
		return creativeops.ErrValidation
	}
	if planningmedia.ValidatePurpose(planningmedia.RightsDeclarationInput{SourceClass: rights.SourceClass, RightsBasis: rights.RightsBasis, EvidenceSummary: rights.EvidenceSummary}, planningmedia.PurposeMoodboardDisplay) != nil {
		return creativecontent.ErrUsageDenied
	}
	return v.Target.validate()
}

// CreateUpload reserves quota and accepts the session; initialization runs in
// the worker. Same-transaction enqueue guarantees no accepted session is lost.
func (s *Service) CreateUpload(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "media.create_upload", c, s.validateCreate, func(ctx context.Context, tx store.TxAccountScope, v CreateUploadInput) (creativeops.Outcome, error) {
		if s.runtime == nil {
			return creativeops.Outcome{}, jobs.ErrNoHandlers
		}
		if err := validateTargetInTx(ctx, tx, v.Target, v.Kind); err != nil {
			if errors.Is(err, creativecanvas.ErrTargetChanged) {
				return creativeops.Outcome{}, creativecanvas.ErrVersionConflict
			}
			return creativeops.Outcome{}, err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if err := s.reserveQuota(ctx, tx, v.Size); err != nil {
			return creativeops.Outcome{}, err
		}
		declaration := "ccrd_" + uuid.NewString()
		rights := v.Rights.OrDefault()
		if err := tx.Insert(ctx, "creative_rights_declarations", []string{"id", "source_class", "rights_basis", "evidence_summary"}, declaration, rights.SourceClass, rights.RightsBasis, rights.EvidenceSummary); err != nil {
			return creativeops.Outcome{}, err
		}
		if err := tx.Insert(ctx, "creative_usage_grants", []string{"id", "declaration_id", "purpose", "evidence"}, "ccug_"+uuid.NewString(), declaration, "display", json.RawMessage(`{"source":"upload_declaration"}`)); err != nil {
			return creativeops.Outcome{}, err
		}
		u := upload{ID: "ccup_" + uuid.NewString(), DeclarationID: declaration, PublishOperation: uuid.NewString(), State: "created", IOPhase: "none", Kind: v.Kind, Mime: v.Mime, Name: v.FileName, TargetKind: v.Target.Kind, Size: v.Size, Reserved: v.Size, Revision: 1, ExpiresAt: now.Add(s.cfg.SessionTTL), CreatedAt: now}
		u.StagingKey = path.Join("creative-v2", tx.AccountID(), "staging", u.ID, "original")
		u.Target = creativegraph.Canonical(v.Target)
		if err := tx.Insert(ctx, "creative_uploads", []string{"id", "operation_id", "rights_declaration_id", "publish_operation_id", "state", "io_phase", "declared_kind", "declared_mime", "declared_size", "reserved_bytes", "original_name", "staging_key", "expires_at", "target_kind", "target_snapshot", "created_at", "updated_at"}, u.ID, c.OperationID, declaration, u.PublishOperation, "created", "none", v.Kind, v.Mime, v.Size, v.Size, v.FileName, u.StagingKey, u.ExpiresAt, v.Target.Kind, u.Target, now, now); err != nil {
			return creativeops.Outcome{}, err
		}
		if err := s.enqueue(ctx, tx, jobInit, c.OperationID, now, u.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		return outcome(202, "upload", u.ID, 1, s.view(u, nil, nil))
	})
}
func (s *Service) enqueue(ctx context.Context, tx store.TxAccountScope, kind, operation string, at time.Time, uploadID string) error {
	// Each stage uses its own deterministic operation identity per upload.
	id := uuid.NewSHA1(uuid.MustParse(operation), []byte(kind+":"+uploadID)).String()
	_, err := jobs.EnqueueInTx(ctx, tx.Jobs(s.runtime), jobs.Request{Kind: kind, OperationID: id, CreatedAt: at, Payload: creativegraph.Canonical(map[string]string{"upload_id": uploadID})})
	return err
}
func (s *Service) reserveQuota(ctx context.Context, tx store.TxAccountScope, size int64) error {
	exists, err := tx.Exists(ctx, "creative_media_quotas", "")
	if err != nil {
		return err
	}
	if !exists {
		if err := tx.Insert(ctx, "creative_media_quotas", []string{"limit_bytes"}, s.cfg.QuotaLimitBytes); err != nil {
			return err
		}
	}
	var reserved, stored, limit int64
	if err := tx.QueryRowForUpdate(ctx, "creative_media_quotas", "reserved_bytes,stored_bytes,limit_bytes", "").Scan(&reserved, &stored, &limit); err != nil {
		return err
	}
	if reserved+stored+size > limit {
		return ErrQuota
	}
	_, err = tx.Update(ctx, "creative_media_quotas", "reserved_bytes=reserved_bytes+$2,revision=revision+1", "", size)
	return err
}

func scanUpload(row store.Row) (upload, error) {
	var u upload
	err := row.Scan(&u.ID, &u.DeclarationID, &u.PublishOperation, &u.State, &u.IOPhase, &u.Epoch, &u.Lease, &u.Revision, &u.Kind, &u.Mime, &u.Size, &u.Reserved, &u.QuotaSettled, &u.Name, &u.StagingKey, &u.StagingVersion, &u.MultipartID, &u.ExpiresAt, &u.TargetKind, &u.Target, &u.BlobID, &u.HandedOff, &u.PublishedBlob, &u.ResultKind, &u.ResultID, &u.ResultRevision, &u.ErrorCode, &u.CreatedAt)
	if errors.Is(err, store.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}
func lockUpload(ctx context.Context, tx store.TxAccountScope, id string) (upload, error) {
	return scanUpload(tx.QueryRowForUpdate(ctx, "creative_uploads", uploadColumns, "id=$2", id))
}
func (s *Service) parts(ctx context.Context, tx interface {
	QueryPage(context.Context, string, string, string, []store.OrderBy, int, int, ...any) (store.Rows, error)
}, id string) ([]int, error) {
	rows, err := tx.QueryPage(ctx, "creative_upload_parts", "part_number", "upload_id=$2", []store.OrderBy{{Column: "part_number"}}, 10000, 0, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	numbers := []int{}
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		numbers = append(numbers, n)
	}
	return numbers, rows.Err()
}
func (s *Service) view(u upload, parts []int, candidate *Candidate) UploadView {
	v := UploadView{ID: u.ID, State: u.State, IOPhase: u.IOPhase, Revision: creativeops.Revision(u.Revision), Kind: u.Kind, Mime: u.Mime, Size: u.Size, FileName: u.Name, PartSize: s.cfg.PartSize, PartCount: s.partCount(u.Size), UploadedParts: parts, TargetKind: u.TargetKind, ExpiresAt: u.ExpiresAt, ErrorCode: u.ErrorCode, CreatedAt: u.CreatedAt}
	if v.UploadedParts == nil {
		v.UploadedParts = []int{}
	}
	if u.ResultKind != nil && u.ResultID != nil {
		p := Publication{Kind: *u.ResultKind, ID: *u.ResultID, Revision: 1}
		if u.ResultRevision != nil {
			p.Revision = creativeops.Revision(*u.ResultRevision)
		}
		v.Publication = &p
		if p.Kind != "candidate" {
			kind, id := p.Kind, p.ID
			v.Binding = &Binding{Status: "applied", TargetKind: &kind, TargetID: &id}
		} else if candidate != nil {
			v.Binding = candidateBinding(*candidate)
		}
	}
	return v
}
func candidateBinding(c Candidate) *Binding {
	b := &Binding{Status: "needs_review", CandidateID: &c.ID}
	expires := c.ExpiresAt
	b.ExpiresAt = &expires
	switch c.State {
	case "applied":
		b.Status = "applied"
		if c.Adopted != nil {
			kind, id := c.Adopted.Kind, c.Adopted.ID
			b.TargetKind, b.TargetID = &kind, &id
		}
	case "discarded", "expired":
		b.Status = c.State
	}
	return b
}

func (s *Service) GetUpload(ctx context.Context, scope store.AccountScope, id string) (UploadView, error) {
	var result UploadView
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		u, err := scanUpload(tx.QueryRow(ctx, "creative_uploads", uploadColumns, "id=$2", id))
		if err != nil {
			return err
		}
		parts, err := s.parts(ctx, tx, id)
		if err != nil {
			return err
		}
		var candidate *Candidate
		if u.ResultKind != nil && *u.ResultKind == "candidate" {
			c, err := scanCandidate(tx.QueryRow(ctx, "creative_upload_candidates", candidateColumns, "id=$2", *u.ResultID))
			if err != nil {
				return err
			}
			candidate = &c
		}
		result = s.view(u, parts, candidate)
		return nil
	})
	return result, err
}

// AuthorizeParts signs part writes. It is repeatable and changes no business
// state; the session must be in uploading/none and not expired.
func (s *Service) AuthorizeParts(ctx context.Context, scope store.AccountScope, id string, numbers []int) (AuthorizedParts, error) {
	if len(numbers) < 1 || len(numbers) > s.cfg.MaxParts {
		return AuthorizedParts{}, creativeops.ErrValidation
	}
	var u upload
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "media_write"); err != nil {
			return err
		}
		var err error
		u, err = scanUpload(tx.QueryRow(ctx, "creative_uploads", uploadColumns, "id=$2", id))
		if err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if !now.Before(u.ExpiresAt) {
			return ErrExpired
		}
		if u.State != "uploading" || u.IOPhase != "none" || u.MultipartID == nil {
			return ErrState
		}
		return nil
	})
	if err != nil {
		return AuthorizedParts{}, err
	}
	result := AuthorizedParts{UploadID: u.ID, Parts: []versionedfs.PartAuthorization{}}
	expires := time.Now().Add(s.cfg.SignatureTTL)
	seen := map[int]bool{}
	for _, n := range numbers {
		if n < 1 || n > s.partCount(u.Size) || seen[n] {
			return AuthorizedParts{}, creativeops.ErrValidation
		}
		seen[n] = true
		a, err := s.adapter.AuthorizePart(ctx, u.StagingKey, *u.MultipartID, n, expires)
		if err != nil {
			return AuthorizedParts{}, err
		}
		result.Parts = append(result.Parts, a)
	}
	return result, nil
}

// CompleteUpload accepts the completion request; the worker lists real parts.
func (s *Service) CompleteUpload(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "media.complete_upload", c, func(v CompleteUploadInput) error {
		if v.UploadID == "" || v.ExpectedRevision < 1 || len(v.Parts) > 10000 {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v CompleteUploadInput) (creativeops.Outcome, error) {
		if s.runtime == nil {
			return creativeops.Outcome{}, jobs.ErrNoHandlers
		}
		u, err := lockUpload(ctx, tx, v.UploadID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if u.Revision != int64(v.ExpectedRevision) {
			return creativeops.Outcome{}, creativecanvas.ErrVersionConflict
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if !now.Before(u.ExpiresAt) {
			return creativeops.Outcome{}, ErrExpired
		}
		if u.State != "uploading" || u.IOPhase != "none" {
			return creativeops.Outcome{}, ErrState
		}
		if _, err := tx.Update(ctx, "creative_uploads", "io_phase='completing',revision=revision+1,updated_at=clock_timestamp()", "id=$2", u.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		if err := s.enqueue(ctx, tx, jobComplete, c.OperationID, now, u.ID); err != nil {
			return creativeops.Outcome{}, err
		}
		u.IOPhase = "completing"
		u.Revision++
		return outcome(202, "upload", u.ID, creativeops.Revision(u.Revision), s.view(u, nil, nil))
	})
}

// CancelUpload stops a session before publication; cleanup is asynchronous.
func (s *Service) CancelUpload(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "media.cancel_upload", c, func(v CancelUploadInput) error {
		if v.UploadID == "" || v.ExpectedRevision < 1 {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v CancelUploadInput) (creativeops.Outcome, error) {
		u, err := lockUpload(ctx, tx, v.UploadID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if u.Revision != int64(v.ExpectedRevision) {
			return creativeops.Outcome{}, creativecanvas.ErrVersionConflict
		}
		switch u.State {
		case "created", "uploading", "verifying":
		default:
			return creativeops.Outcome{}, ErrState
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if err := s.finish(ctx, tx, u, "cancelled", "cancelled"); err != nil {
			return creativeops.Outcome{}, err
		}
		if s.runtime != nil && u.MultipartID != nil {
			if err := s.enqueue(ctx, tx, jobAbort, c.OperationID, now, u.ID); err != nil {
				return creativeops.Outcome{}, err
			}
		}
		u.State, u.IOPhase, u.ErrorCode = "cancelled", "none", "cancelled"
		u.Revision++
		return outcome(200, "upload", u.ID, creativeops.Revision(u.Revision), s.view(u, nil, nil))
	})
}

// finish moves a session to a terminal non-ready state, invalidating any
// worker epoch and releasing the reservation. Staged objects stay for the
// sweep; a blob preallocated during promotion is marked deleting.
func (s *Service) finish(ctx context.Context, tx store.TxAccountScope, u upload, state, code string) error {
	if _, err := tx.Update(ctx, "creative_uploads", "state=$2,io_phase='none',error_code=$3,execution_epoch=execution_epoch+1,lease_until=NULL,revision=revision+1,updated_at=clock_timestamp()", "id=$4", state, code, u.ID); err != nil {
		return err
	}
	if u.QuotaSettled == nil && u.Reserved > 0 {
		if _, err := tx.Update(ctx, "creative_media_quotas", "reserved_bytes=GREATEST(reserved_bytes-$2,0),revision=revision+1", "", u.Reserved); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "creative_uploads", "reserved_bytes=0,quota_settled_at=clock_timestamp()", "id=$2", u.ID); err != nil {
			return err
		}
	}
	if u.BlobID != nil {
		// The original and any rendition derived from it in this session.
		if _, err := tx.Update(ctx, "creative_blobs", "state='deleting',delete_after=clock_timestamp()+interval '48 hours'", "(id=$2 OR codec_metadata->>'derived_from'=$2) AND state='verifying'", *u.BlobID); err != nil {
			return err
		}
	}
	return nil
}
func targetOf(u upload) (Target, error) {
	var t Target
	err := json.Unmarshal(u.Target, &t)
	return t, err
}

// failureCode is the client-visible reason class; the underlying error is
// logged by the worker and never persisted or returned.
func failureCode(code string) string { return strings.TrimSpace(code) }
