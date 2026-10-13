package creativeimageedit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	JobKind = "creative_image_edit_render"
	// Queue is the River queue with a single worker: one encode at a time
	// per worker process (backend design §10).
	Queue = "image_edit"

	// renderDeadline bounds queueing plus encoding; readyTTL is how long a
	// finished result waits to be applied.
	renderDeadline = 2 * time.Minute
	readyTTL       = 30 * time.Minute
	// An account may have one candidate encoding and one waiting.
	accountActiveLimit = 2
	createWindow       = 10 * time.Minute
	createLimit        = 60
	sweepInterval      = 5 * time.Minute
)

var (
	// ErrNodeBusy: the node already has an unfinished candidate (409).
	ErrNodeBusy = errors.New("creative image edit node busy")
	// ErrBusy: the account's concurrent or recent candidates are at the limit (429).
	ErrBusy = errors.New("creative image edit capacity busy")
	// ErrIdempotency: an operation id reused with a different request (409).
	ErrIdempotency = errors.New("creative image edit operation reused with a different request")
	// ErrSourceChanged: the node no longer shows the revision the edit started from (409).
	ErrSourceChanged = errors.New("creative image edit source changed")
	ErrNotFound      = errors.New("creative image edit render not found")
)

// Renderer is the codec seam; the worker uses *Codec, tests a fake.
type Renderer interface {
	Render(context.Context, Request) (Result, error)
}

// Service owns edit candidates. The API process composes it without a
// renderer: it only accepts, reads and discards; the worker renders.
type Service struct {
	media    *creativemedia.Service
	renderer Renderer
	runtime  jobs.Runtime
	tempRoot string
}

// tempRoot is shared with the worker's Codec so startup sweeping also owns
// source files left behind by a killed worker.
func NewService(media *creativemedia.Service, renderer Renderer, tempRoot string) *Service {
	return &Service{media: media, renderer: renderer, tempRoot: tempRoot}
}

func (s *Service) SetRuntime(r jobs.Runtime) { s.runtime = r }

func (s *Service) Handlers() []store.JobHandler {
	return []store.JobHandler{{Kind: JobKind, Queue: Queue, Timeout: renderDeadline + 30*time.Second, Work: s.Work}}
}

// CreateInput is one click on apply. The operation id makes the click
// idempotent; the source revision is what the panel was opened on.
type CreateInput struct {
	OperationID      string
	CanvasID         string
	NodeID           string
	SourceRevisionID string
	Action           string
	Quality          int
	Crop             Crop
	CallerKind       string
}

// Render is a candidate as callers see it. State is effective: an
// unfinished candidate past its deadline reads as expired before the sweep
// records it.
type Render struct {
	ID               string          `json:"id"`
	OperationID      string          `json:"operation_id"`
	CanvasID         string          `json:"canvas_id"`
	NodeID           string          `json:"node_id"`
	SourceRevisionID string          `json:"source_revision_id"`
	Action           string          `json:"action"`
	Parameters       json.RawMessage `json:"parameters"`
	State            string          `json:"state"`
	ExpiresAt        time.Time       `json:"expires_at"`
	ErrorCode        string          `json:"error_code,omitempty"`
	Output           *Output         `json:"output,omitempty"`
	ResultRevisionID *string         `json:"result_revision_id,omitempty"`
	AppliedVersionID *string         `json:"applied_version_id,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`

	requestHash string
	callerKind  string
	original    *string
	display     *string
}

// Output is what the encode actually produced; SourceSize lets callers
// report the change without trusting a client's number.
type Output struct {
	creativemedia.EditFacts
	SourceSize int64 `json:"source_size"`
}

func (r Render) active() bool {
	return r.State == "queued" || r.State == "running" || r.State == "ready"
}

func parameters(in CreateInput) (json.RawMessage, error) {
	switch in.Action {
	case ActionCompress:
		if in.Quality < 1 || in.Quality > 100 {
			return nil, creativeops.ErrValidation
		}
		return creativegraph.Canonical(map[string]int{"quality": in.Quality}), nil
	case ActionCrop:
		c := in.Crop
		if c.Left < 0 || c.Top < 0 || c.Width < 1 || c.Height < 1 {
			return nil, creativeops.ErrValidation
		}
		return creativegraph.Canonical(map[string]int{"left": c.Left, "top": c.Top, "width": c.Width, "height": c.Height}), nil
	}
	return nil, creativeops.ErrValidation
}

// Create accepts one edit: it fixes the source, checks the node and the
// account's limits and queues the encode, all in one transaction. Lock
// order: account write barrier, then project/canvas, then candidates.
func (s *Service) Create(ctx context.Context, scope store.AccountScope, in CreateInput) (Render, error) {
	params, err := parameters(in)
	if err != nil {
		return Render{}, err
	}
	if id, perr := uuid.Parse(in.OperationID); perr != nil || id == uuid.Nil || in.SourceRevisionID == "" || (in.CallerKind != "photographer" && in.CallerKind != "agent") {
		return Render{}, creativeops.ErrValidation
	}
	if s.runtime == nil {
		return Render{}, jobs.ErrNoHandlers
	}
	hash := creativegraph.Hash(creativegraph.Canonical([]any{in.CanvasID, in.NodeID, in.SourceRevisionID, in.Action, params, in.CallerKind}))
	var out Render
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "media_write"); err != nil {
			return err
		}
		existing, err := readRender(ctx, tx, false, "operation_id=$2", in.OperationID)
		if err == nil {
			if existing.requestHash != hash {
				return ErrIdempotency
			}
			out = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		current, err := creativecanvas.ImageEditSourceInTx(ctx, tx, in.CanvasID, in.NodeID)
		if err != nil {
			return err
		}
		if current != in.SourceRevisionID {
			return ErrSourceChanged
		}
		if _, err := creativecontent.RequireUsable(ctx, tx, in.SourceRevisionID, "display"); err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if _, err := expireOverdueInTx(ctx, tx, now); err != nil {
			return err
		}
		busy, err := tx.Exists(ctx, "creative_image_edit_renders", "canvas_id=$2 AND node_id=$3 AND state IN ('queued','running','ready')", in.CanvasID, in.NodeID)
		if err != nil {
			return err
		}
		if busy {
			return ErrNodeBusy
		}
		working, err := tx.Count(ctx, "creative_image_edit_renders", "state IN ('queued','running')")
		if err != nil {
			return err
		}
		recent, err := tx.Count(ctx, "creative_image_edit_renders", "created_at>$2", now.Add(-createWindow))
		if err != nil {
			return err
		}
		if working >= accountActiveLimit || recent >= createLimit {
			return ErrBusy
		}
		id := "cier_" + uuid.NewString()
		if err := tx.Insert(ctx, "creative_image_edit_renders", []string{"id", "operation_id", "request_hash", "canvas_id", "node_id", "source_revision_id", "action", "parameters", "encoder_policy", "caller_kind", "state", "expires_at"}, id, in.OperationID, hash, in.CanvasID, in.NodeID, in.SourceRevisionID, in.Action, params, PolicyVersion, in.CallerKind, "queued", now.Add(renderDeadline)); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"render_id": id})
		if _, err := jobs.EnqueueInTx(ctx, tx.Jobs(s.runtime), jobs.Request{Kind: JobKind, OperationID: uuid.NewString(), CreatedAt: now, Payload: payload}); err != nil {
			return err
		}
		out, err = readRender(ctx, tx, false, "id=$2", id)
		return err
	})
	if err != nil {
		return Render{}, err
	}
	return out.effective(time.Now()), nil
}

// Get reads one candidate.
func (s *Service) Get(ctx context.Context, scope store.AccountScope, id string) (Render, error) {
	var out Render
	var now time.Time
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		var err error
		if now, err = tx.CreativeNow(ctx); err != nil {
			return err
		}
		out, err = readRender(ctx, tx, false, "id=$2", id)
		return err
	})
	return out.effective(now), err
}

// Discard releases an unfinished candidate. It is idempotent and never
// undoes an applied one: that returns the applied fact.
func (s *Service) Discard(ctx context.Context, scope store.AccountScope, id string) (Render, error) {
	var out Render
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "media_write"); err != nil {
			return err
		}
		r, err := readRender(ctx, tx, true, "id=$2", id)
		if err != nil {
			return err
		}
		if r.active() {
			if err := finishInTx(ctx, tx, r, "discarded", "", nil); err != nil {
				return err
			}
			r, err = readRender(ctx, tx, false, "id=$2", id)
			if err != nil {
				return err
			}
		}
		out = r
		return nil
	})
	return out, err
}

func (r Render) effective(now time.Time) Render {
	if r.active() && !now.Before(r.ExpiresAt) {
		r.State = "expired"
	}
	return r
}

const renderColumns = "id,operation_id,request_hash,canvas_id,node_id,source_revision_id,action,parameters,caller_kind,state,expires_at,error_code,original_blob_id,display_blob_id,output,result_revision_id,applied_version_id,created_at"

func readRender(ctx context.Context, tx store.TxAccountScope, lock bool, cond string, args ...any) (Render, error) {
	read := tx.QueryRow
	if lock {
		read = tx.QueryRowForUpdate
	}
	var r Render
	var output []byte
	err := read(ctx, "creative_image_edit_renders", renderColumns, cond, args...).Scan(&r.ID, &r.OperationID, &r.requestHash, &r.CanvasID, &r.NodeID, &r.SourceRevisionID, &r.Action, &r.Parameters, &r.callerKind, &r.State, &r.ExpiresAt, &r.ErrorCode, &r.original, &r.display, &output, &r.ResultRevisionID, &r.AppliedVersionID, &r.CreatedAt)
	if errors.Is(err, store.ErrNoRows) {
		return Render{}, ErrNotFound
	}
	if err != nil {
		return Render{}, err
	}
	if len(output) > 2 {
		r.Output = &Output{}
		if err := json.Unmarshal(output, r.Output); err != nil {
			return Render{}, err
		}
	}
	return r, nil
}

// finishInTx moves an unfinished candidate to a terminal state. Blobs still
// verifying are handed to GC; ready ones lose their holder with this row.
func finishInTx(ctx context.Context, tx store.TxAccountScope, r Render, state, code string, output *Output) error {
	set, args := "state=$3,error_code=$4,revision=revision+1,updated_at=clock_timestamp()", []any{r.ID, state, code}
	if output != nil {
		set += ",output=$5"
		args = append(args, creativegraph.Canonical(output))
	}
	if _, err := tx.Update(ctx, "creative_image_edit_renders", set, "id=$2 AND state IN ('queued','running','ready')", args...); err != nil {
		return err
	}
	return creativemedia.AbandonEditBlobsInTx(ctx, tx, deref(r.original), deref(r.display))
}

// expireOverdueInTx records the expiry of every unfinished candidate past
// its deadline; the caller holds the write barrier.
func expireOverdueInTx(ctx context.Context, tx store.TxAccountScope, now time.Time) (int64, error) {
	rows, err := tx.QueryPage(ctx, "creative_image_edit_renders", "id", "state IN ('queued','running','ready') AND expires_at<=$2", []store.OrderBy{{Column: "id"}}, 100, 0, now)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		r, err := readRender(ctx, tx, true, "id=$2", id)
		if err != nil {
			return 0, err
		}
		if err := finishInTx(ctx, tx, r, "expired", "", nil); err != nil {
			return 0, err
		}
	}
	return int64(len(ids)), nil
}

// SweepExpired records expiries for one account so abandoned candidates
// stop holding their source and blobs even if nobody reads them again.
func (s *Service) SweepExpired(ctx context.Context, scope store.AccountScope) (int64, error) {
	var n int64
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		n, err = expireOverdueInTx(ctx, tx, now)
		return err
	})
	return n, err
}

// RunSweep expires overdue candidates for every account, inactive ones too.
func (s *Service) RunSweep(ctx context.Context, db *store.Store, logger *slog.Logger) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		after := ""
		for ctx.Err() == nil {
			ids, err := db.MaintenanceAccountIDs(ctx, after, 100)
			if err != nil {
				logger.Warn("creative image edit sweep accounts", slog.String("error", err.Error()))
				break
			}
			for _, id := range ids {
				if _, err := s.SweepExpired(ctx, db.ScopeFor(auth.AccountContext{AccountID: id})); err != nil && ctx.Err() == nil {
					logger.Warn("creative image edit sweep", slog.String("account_id", id), slog.String("error", err.Error()))
				}
				after = id
			}
			if len(ids) < 100 {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Work renders one candidate. Publication is fenced by its blob identity:
// discarding, expiring or replacing a claim prevents a late writer from
// making its result ready.
func (s *Service) Work(ctx context.Context, scope store.AccountScope, req jobs.Request) error {
	var payload struct {
		RenderID string `json:"render_id"`
	}
	if err := json.Unmarshal(req.Payload, &payload); err != nil || payload.RenderID == "" {
		return jobs.ErrInvalidTask
	}
	if s.renderer == nil {
		return errors.New("creative image edit renderer is not configured in this process")
	}
	r, ok, err := s.start(ctx, scope, payload.RenderID)
	if err != nil || !ok {
		return err
	}
	ctx, cancel := context.WithDeadline(ctx, r.ExpiresAt)
	defer cancel()
	source, src, err := s.stageSource(ctx, scope, r.SourceRevisionID)
	if err != nil {
		return s.settle(ctx, scope, r, err, "source_unavailable", nil)
	}
	defer func() { _ = os.Remove(source) }()
	var params struct {
		Quality int `json:"quality"`
		Crop
	}
	if err := json.Unmarshal(r.Parameters, &params); err != nil {
		return s.settle(ctx, scope, r, err, "invalid_parameters", nil)
	}
	result, err := s.renderer.Render(ctx, Request{Source: source, Mime: src.Mime, Action: r.Action, Quality: params.Quality, Crop: params.Crop})
	if err != nil {
		return s.settle(ctx, scope, r, err, renderErrorCode(err), &Output{SourceSize: src.Size})
	}
	defer func() { _ = result.Close() }()
	out, err := s.media.VerifyEditOutput(ctx, scope, result.Path, result.Mime)
	if err != nil {
		return s.settle(ctx, scope, r, fmt.Errorf("%w: %v", errPermanent, err), "output_invalid", nil)
	}
	defer out.Close()
	if f := out.Facts(); f.Mime != result.Mime || f.Width != result.Width || f.Height != result.Height {
		return s.settle(ctx, scope, r, fmt.Errorf("%w: verifier saw %+v", errPermanent, f), "output_invalid", nil)
	}
	return s.store(ctx, scope, r, out, src.Size)
}

var errPermanent = errors.New("permanent render failure")

// start moves a queued candidate to running and recovers unfinished claims.
// A redelivery abandons the previous attempt's blobs before clearing its
// claim. A still-live writer is fenced by those blob states and identities;
// each attempt allocates fresh blobs, so no lease or ambiguous object overwrite
// is needed. Ready candidates (including an unknown-but-committed outcome)
// are never reclaimed.
func (s *Service) start(ctx context.Context, scope store.AccountScope, id string) (Render, bool, error) {
	var r Render
	ok := false
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		var err error
		if r, err = readRender(ctx, tx, true, "id=$2", id); err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		switch {
		case !r.active() || r.State == "ready":
			return nil
		case !now.Before(r.ExpiresAt):
			return finishInTx(ctx, tx, r, "expired", "", nil)
		case r.State == "running" && r.original != nil:
			if err := creativemedia.AbandonEditBlobsInTx(ctx, tx, deref(r.original), deref(r.display)); err != nil {
				return err
			}
			if _, err := tx.Update(ctx, "creative_image_edit_renders", "original_blob_id=NULL,display_blob_id=NULL,revision=revision+1,updated_at=clock_timestamp()", "id=$2", id); err != nil {
				return err
			}
			r.original, r.display = nil, nil
		}
		if r.State == "queued" {
			if _, err := tx.Update(ctx, "creative_image_edit_renders", "state='running',revision=revision+1,updated_at=clock_timestamp()", "id=$2 AND state='queued'", id); err != nil {
				return err
			}
			r.State = "running"
		}
		ok = true
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return Render{}, false, nil
	}
	return r, ok, err
}

// stageSource copies the exact source object to a private file and checks
// its digest, so the codec never reads a partially fetched object.
func (s *Service) stageSource(ctx context.Context, scope store.AccountScope, revisionID string) (string, creativemedia.EditSource, error) {
	body, src, err := s.media.OpenEditSource(ctx, scope, revisionID)
	if err != nil {
		return "", src, err
	}
	defer func() { _ = body.Close() }()
	path, err := stageSourceBody(body, src, s.tempRoot)
	return path, src, err
}

func stageSourceBody(body io.Reader, src creativemedia.EditSource, tempRoot string) (string, error) {
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(tempRoot, sourceTempPrefix)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hasher), io.LimitReader(body, src.Size+1))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil && (n != src.Size || "sha256-"+hex.EncodeToString(hasher.Sum(nil)) != src.SHA256) {
		err = creativemedia.ErrState
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func renderErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrNotReduced):
		return ""
	case errors.Is(err, ErrColour):
		return "colour_unknown"
	case errors.Is(err, ErrParameters):
		return "invalid_parameters"
	case errors.Is(err, creativemedia.ErrSizeLimit):
		return "too_large"
	case errors.Is(err, creativemedia.ErrUnsupported):
		return "unsupported"
	}
	return "render_failed"
}

// permanent reports whether retrying the same candidate cannot help.
func permanent(err error) bool {
	for _, target := range []error{errPermanent, ErrNotReduced, ErrColour, ErrParameters, creativemedia.ErrSizeLimit, creativemedia.ErrUnsupported, creativecontent.ErrNotFound, creativecontent.ErrUsageDenied} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// settle turns a failed step into the candidate's outcome. Cancellation is
// retryable until the candidate's own deadline, even when wrapped as a
// permanent verification error. Only database time under the candidate lock
// can establish expiry; a cancelled old attempt must not stop its successor.
func (s *Service) settle(ctx context.Context, scope store.AccountScope, r Render, cause error, code string, output *Output) error {
	state := ""
	cancelled := ctx.Err() != nil
	switch {
	case cancelled:
		// Inspect the current deadline below before deciding whether to expire.
	case errors.Is(cause, ErrNotReduced):
		state = "not_reduced"
	case permanent(cause):
		state = "failed"
	default:
		return cause
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	return scope.WithTxScope(finishCtx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(finishCtx); err != nil {
			return err
		}
		current, err := readRender(finishCtx, tx, true, "id=$2", r.ID)
		if err != nil {
			return err
		}
		if current.State != "running" {
			return nil
		}
		now, err := tx.CreativeNow(finishCtx)
		if err != nil {
			return err
		}
		if !now.Before(current.ExpiresAt) {
			return finishInTx(finishCtx, tx, current, "expired", "", nil)
		}
		if cancelled {
			return errors.Join(ctx.Err(), cause)
		}
		return finishInTx(finishCtx, tx, current, state, code, output)
	})
}

// store claims the candidate for this attempt, writes the objects and makes
// the result ready. The blob rows exist before any object does, so every
// interruption leaves something GC can find.
func (s *Service) store(ctx context.Context, scope store.AccountScope, r Render, out *creativemedia.EditOutput, sourceSize int64) error {
	claimed := false
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		current, err := readRender(ctx, tx, true, "id=$2", r.ID)
		if err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if current.State != "running" || current.original != nil || !now.Before(current.ExpiresAt) {
			return nil
		}
		if err := s.media.InsertEditBlobsInTx(ctx, tx, out); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "creative_image_edit_renders", "original_blob_id=$3,display_blob_id=$4,updated_at=clock_timestamp()", "id=$2", r.ID, out.OriginalBlob(), nullable(out.DisplayBlob())); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	if err != nil || !claimed {
		return err
	}
	if err := s.media.PublishEditBlobs(ctx, scope, out); err != nil {
		return s.release(ctx, scope, r.ID, out, err)
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	return scope.WithTxScope(finishCtx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(finishCtx); err != nil {
			return err
		}
		current, err := readRender(finishCtx, tx, true, "id=$2", r.ID)
		if err != nil {
			return err
		}
		now, err := tx.CreativeNow(finishCtx)
		if err != nil {
			return err
		}
		if current.State != "running" || deref(current.original) != out.OriginalBlob() || !now.Before(current.ExpiresAt) {
			// Discarded or expired while writing: the result is nobody's.
			return creativemedia.AbandonEditBlobsInTx(finishCtx, tx, out.OriginalBlob(), out.DisplayBlob())
		}
		if err := s.media.ReadyEditBlobsInTx(finishCtx, tx, out); err != nil {
			return err
		}
		output := creativegraph.Canonical(Output{EditFacts: out.Facts(), SourceSize: sourceSize})
		_, err = tx.Update(finishCtx, "creative_image_edit_renders", "state='ready',expires_at=$3,output=$4,revision=revision+1,updated_at=clock_timestamp()", "id=$2 AND state='running'", r.ID, now.Add(readyTTL), output)
		return err
	})
}

// release undoes this attempt's claim after a failed write so a retry can
// claim again, and hands the half-written blobs to GC.
func (s *Service) release(ctx context.Context, scope store.AccountScope, id string, out *creativemedia.EditOutput, cause error) error {
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	err := scope.WithTxScope(finishCtx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(finishCtx); err != nil {
			return err
		}
		if _, err := tx.Update(finishCtx, "creative_image_edit_renders", "original_blob_id=NULL,display_blob_id=NULL,updated_at=clock_timestamp()", "id=$2 AND state='running' AND original_blob_id=$3", id, out.OriginalBlob()); err != nil {
			return err
		}
		return creativemedia.AbandonEditBlobsInTx(finishCtx, tx, out.OriginalBlob(), out.DisplayBlob())
	})
	if errors.Is(cause, creativemedia.ErrState) {
		// The candidate abandoned the blobs first; there is nothing to retry.
		return err
	}
	return errors.Join(cause, err)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
