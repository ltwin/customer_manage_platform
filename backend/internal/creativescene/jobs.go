package creativescene

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Client jobs: work the server hands to an open creative workspace tab (the
// render host). See the C3 design, "客户端任务".

type JobKind string

const (
	KindBakeModel   JobKind = "bake_model"
	KindRenderView  JobKind = "render_view"
	KindExportShots JobKind = "export_shots"
)

const (
	JobQueued    = "queued"
	JobClaimed   = "claimed"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
	JobExpired   = "expired"
	JobCancelled = "cancelled"
)

const (
	jobLease       = 30 * time.Second
	jobDeadline    = 30 * time.Minute
	maxJobAttempts = 3
	maxActiveJobs  = 50
)

// knownKinds are the kinds a claim may ask for; executableKinds are the ones a
// render host can run today (export_shots arrives with C4).
var (
	knownKinds      = []string{string(KindBakeModel), string(KindRenderView), string(KindExportShots)}
	executableKinds = []string{string(KindBakeModel), string(KindRenderView)}
	editorSession   = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)
)

var (
	// ErrJobLeaseLost is a claim that no longer holds: the job is not claimed,
	// the token is someone else's or the lease ran out. The host drops its work.
	ErrJobLeaseLost = errors.New("creative scene: job lease lost")
	// ErrRunGate is a run that may no longer write: the account lost the
	// capability, the run passed its deadline or was cancelled, a skill was
	// disabled or the consent withdrawn.
	ErrRunGate = errors.New("creative scene: run may no longer write")
	// ErrJobNotFound is a job that does not exist in this account. It matches
	// creativedocument.ErrNotFound, so it maps to 404 like a missing scene.
	ErrJobNotFound = fmt.Errorf("%w: scene job", creativedocument.ErrNotFound)
)

// Job is one creative_scene_jobs row.
type Job struct {
	ID         string
	DocumentID string
	Kind       JobKind
	State      string
	Request    json.RawMessage
	Attempt    int
	RunID      *string
	ToolCallID *string
	Deadline   time.Time
	Result     json.RawMessage
}

// BakeRequest is a bake_model job's request.
type BakeRequest struct {
	Code       string         `json:"code"`
	Params     map[string]any `json:"params"`
	LibVersion int            `json:"lib_version"`
	NameHint   string         `json:"name_hint,omitempty"`
	Target     BakeTarget     `json:"target"`
	// Expected freezes the replaced instances' content units when the job is
	// created; an add expects nothing and writes at the revision current then.
	Expected map[string]*creativeops.Revision `json:"expected"`
}

// BakeTarget says where the baked model goes: a new object, or the model of
// existing instances. ObjectIDs lists every instance changed by all_instances,
// fixed when the job is created.
type BakeTarget struct {
	Op        string          `json:"op"`
	Object    json.RawMessage `json:"object,omitempty"`
	ObjectID  string          `json:"object_id,omitempty"`
	Scope     string          `json:"scope,omitempty"`
	ObjectIDs []string        `json:"object_ids,omitempty"`
	// BaseContentID appends the new revision to the replaced model's content
	// (R4: unshared, or all instances); empty forks a new content.
	BaseContentID string `json:"base_content_id,omitempty"`
}

// RenderRequest is a render_view job's request: one of camera, viewpoint or preset.
type RenderRequest struct {
	CameraObjectID string     `json:"camera_object_id,omitempty"`
	Viewpoint      *Viewpoint `json:"viewpoint,omitempty"`
	Preset         string     `json:"preset,omitempty"`
	Mode           string     `json:"mode"`
}

type Viewpoint struct {
	Position [3]float64 `json:"position"`
	Target   [3]float64 `json:"target"`
	FocalMM  float64    `json:"focal_mm"`
}

// BakeResult is a successful bake: the committed model and where it went.
type BakeResult struct {
	ContentID         string               `json:"content_id"`
	ContentRevisionID string               `json:"content_revision_id"`
	ObjectIDs         []string             `json:"object_ids"`
	Revision          creativeops.Revision `json:"revision"`
}

// RenderResult is a successful screenshot, kept staged.
type RenderResult struct {
	ImageUploadID     string               `json:"image_upload_id"`
	ContentRevisionID string               `json:"content_revision_id"`
	SceneRevision     creativeops.Revision `json:"scene_revision"`
}

// BakeArtifacts is a bake that could not be written; a retry reuses it.
type BakeArtifacts struct {
	GLBUploadID       string          `json:"glb_upload_id"`
	ThumbnailUploadID string          `json:"thumbnail_upload_id"`
	Module            json.RawMessage `json:"module"`
	Manifest          json.RawMessage `json:"manifest"`
}

// JobFailure is a failed, expired or cancelled job's result.
type JobFailure struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Conflicts []Conflict     `json:"conflicts,omitempty"`
	Bake      *BakeArtifacts `json:"bake,omitempty"`
}

// Conflict is one unit another writer changed after the baseline.
type Conflict struct {
	Unit     string                `json:"unit"`
	Expected *creativeops.Revision `json:"expected_rev"`
	Current  *creativeops.Revision `json:"current_rev"`
}

// JobResult is what a terminal job holds in result and what the waiter turns
// into the tool result. Exactly one field is set.
type JobResult struct {
	Bake    *BakeResult
	Render  *RenderResult
	Failure *JobFailure
}

func (r JobResult) state(expired, cancelled bool) string {
	switch {
	case r.Failure == nil:
		return JobSucceeded
	case expired:
		return JobExpired
	case cancelled:
		return JobCancelled
	}
	return JobFailed
}

func (r JobResult) MarshalJSON() ([]byte, error) {
	switch {
	case r.Bake != nil:
		return json.Marshal(r.Bake)
	case r.Render != nil:
		return json.Marshal(r.Render)
	case r.Failure != nil:
		return json.Marshal(r.Failure)
	}
	return nil, errors.New("empty job result")
}

// RunGate is the waiter's handle on a run admitted in this transaction.
type RunGate any

// JobWaiter is how a job reaches the assistant run that waits on it. The agent
// package implements it and registers it at start-up, so this package never
// imports the agent runtime.
type JobWaiter interface {
	// Admit locks the run in the run lock order (project, slot, gateway group,
	// run, skill, consent) and rechecks what an agent write needs: the
	// agent_start capability, the run's deadline, no cancellation, active
	// skills and a standing consent. A run that fails it returns ErrRunGate.
	Admit(ctx context.Context, tx store.TxAccountScope, runID string) (RunGate, error)
	// Settle writes the job's tool result into the run and, when the run waits
	// on nothing else, takes the slot and queues it.
	Settle(ctx context.Context, tx store.TxAccountScope, gate RunGate, job Job, res JobResult) error
	// Abort ends a run that failed Admit, in its own transaction; ending a run
	// cancels its jobs.
	Abort(ctx context.Context, scope store.AccountScope, runID string, cause error) error
}

var (
	waiterMu sync.RWMutex
	waiter   JobWaiter
	logger   = slog.Default()
)

// SetJobWaiter registers the agent runtime's waiter. The server and the
// creative worker call it once at start-up.
func SetJobWaiter(w JobWaiter) {
	waiterMu.Lock()
	defer waiterMu.Unlock()
	waiter = w
}

// SetJobWaiterForTest swaps the waiter for one test.
func SetJobWaiterForTest(tb creativedocument.Cleaner, w JobWaiter) {
	tb.Helper()
	waiterMu.Lock()
	old := waiter
	waiter = w
	waiterMu.Unlock()
	tb.Cleanup(func() { SetJobWaiter(old) })
}

func currentWaiter() JobWaiter {
	waiterMu.RLock()
	defer waiterMu.RUnlock()
	return waiter
}

// SetLogger receives the process logger for job transitions.
func SetLogger(l *slog.Logger) {
	if l != nil {
		logger = l
	}
}

// logTransition records one state change: enough to follow a job across tabs
// and retries without reading the database.
func logTransition(job Job, to string, code string, started time.Time) {
	attrs := []any{
		slog.String("job_id", job.ID), slog.String("document_id", job.DocumentID), slog.String("kind", string(job.Kind)),
		slog.String("from", job.State), slog.String("to", to), slog.Int("attempt", job.Attempt),
	}
	if job.RunID != nil {
		attrs = append(attrs, slog.String("run_id", *job.RunID))
	}
	if code != "" {
		attrs = append(attrs, slog.String("code", code))
	}
	if !started.IsZero() {
		attrs = append(attrs, slog.Duration("since_created", time.Since(started)))
	}
	logger.Info("creative scene job", attrs...)
}

const jobColumns = "id,document_id,kind,state,request,attempt,run_id,tool_call_id,deadline,result,created_at"

func scanJob(row store.Row) (Job, time.Time, error) {
	var j Job
	var kind string
	var created time.Time
	err := row.Scan(&j.ID, &j.DocumentID, &kind, &j.State, &j.Request, &j.Attempt, &j.RunID, &j.ToolCallID, &j.Deadline, &j.Result, &created)
	j.Kind = JobKind(kind)
	if errors.Is(err, store.ErrNoRows) {
		return Job{}, time.Time{}, ErrJobNotFound
	}
	return j, created, err
}

// requireScene is the document check every job entry makes: it exists in this
// account and is a scene.
func requireScene(ctx context.Context, tx store.TxAccountScope, id string) (creativedocument.Document, error) {
	return creativedocument.ReadHeadInTx(ctx, tx, id, TypeKey)
}

// NewJob is a job a tool asks for. RunID and ToolCallID identify the tool call;
// the same call never makes a second job.
type NewJob struct {
	DocumentID string
	Kind       JobKind
	Request    any
	RunID      string
	ToolCallID string
}

// CreateJob queues a job in the caller's transaction, or returns the job this
// tool call already created (a run replayed from its checkpoint calls the tool
// again).
func CreateJob(ctx context.Context, tx store.TxAccountScope, in NewJob) (Job, error) {
	if !slices.Contains(executableKinds, string(in.Kind)) || in.RunID == "" || in.ToolCallID == "" {
		return Job{}, creativeops.ErrValidation
	}
	if _, err := requireScene(ctx, tx, in.DocumentID); err != nil {
		return Job{}, err
	}
	existing, _, err := scanJob(tx.QueryRow(ctx, "creative_scene_jobs", jobColumns, "run_id=$2 AND tool_call_id=$3", in.RunID, in.ToolCallID))
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, creativedocument.ErrNotFound) {
		return Job{}, err
	}
	request, err := json.Marshal(in.Request)
	if err != nil {
		return Job{}, err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return Job{}, err
	}
	job := Job{ID: "csjob_" + uuid.NewString(), DocumentID: in.DocumentID, Kind: in.Kind, State: JobQueued, Request: request,
		RunID: &in.RunID, ToolCallID: &in.ToolCallID, Deadline: now.Add(jobDeadline)}
	if err := tx.Insert(ctx, "creative_scene_jobs", []string{"id", "document_id", "kind", "request", "deadline", "run_id", "tool_call_id"},
		job.ID, job.DocumentID, string(job.Kind), request, job.Deadline, in.RunID, in.ToolCallID); err != nil {
		return Job{}, err
	}
	logTransition(Job{ID: job.ID, DocumentID: job.DocumentID, Kind: job.Kind, RunID: job.RunID}, JobQueued, "", time.Time{})
	return job, nil
}

// ReadRunJob reads the job one tool call created, if any.
func ReadRunJob(ctx context.Context, tx store.TxAccountScope, runID, toolCallID string) (Job, error) {
	job, _, err := scanJob(tx.QueryRow(ctx, "creative_scene_jobs", jobColumns, "run_id=$2 AND tool_call_id=$3", runID, toolCallID))
	return job, err
}

// LockJobs locks the listed jobs (in id order, so concurrent callers agree)
// and returns them; missing ids are skipped.
func LockJobs(ctx context.Context, tx store.TxAccountScope, ids []string) ([]Job, error) {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	out := make([]Job, 0, len(sorted))
	for _, id := range slices.Compact(sorted) {
		job, _, err := scanJob(tx.QueryRowForUpdate(ctx, "creative_scene_jobs", jobColumns, "id=$2", id))
		if errors.Is(err, creativedocument.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, nil
}

// Terminal reports whether a job state is final.
func Terminal(state string) bool {
	return state == JobSucceeded || state == JobFailed || state == JobExpired || state == JobCancelled
}

// ClaimInput is a render host asking for work.
type ClaimInput struct {
	EditorSessionID string   `json:"editor_session_id"`
	DocumentID      string   `json:"document_id,omitempty"`
	Kinds           []string `json:"kinds,omitempty"`
}

// ClaimedJob is what the host gets back: enough to run the job and hold it.
type ClaimedJob struct {
	ID         string          `json:"id"`
	DocumentID string          `json:"document_id"`
	Kind       string          `json:"kind"`
	Request    json.RawMessage `json:"request"`
	Attempt    int             `json:"attempt"`
	ClaimToken string          `json:"claim_token"`
	LeaseUntil time.Time       `json:"lease_until"`
	Deadline   time.Time       `json:"deadline"`
}

// Claim gives the host the oldest queued job it can run, or nil.
func Claim(ctx context.Context, scope store.AccountScope, in ClaimInput) (*ClaimedJob, error) {
	if !editorSession.MatchString(in.EditorSessionID) {
		return nil, invalid("editor_session_id 格式不正确")
	}
	kinds := in.Kinds
	if len(kinds) == 0 {
		kinds = knownKinds
	}
	for _, k := range kinds {
		if !slices.Contains(knownKinds, k) {
			return nil, invalid("未知的任务类型 %q", k)
		}
	}
	var out *ClaimedJob
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		if in.DocumentID != "" {
			if _, err := requireScene(ctx, tx, in.DocumentID); err != nil {
				return err
			}
		}
		token, err := claimToken()
		if err != nil {
			return err
		}
		job, ok, err := tx.ClaimCreativeSceneJob(ctx, store.SceneJobClaim{DocumentID: in.DocumentID, Kinds: kinds, Token: token, Session: in.EditorSessionID, Lease: jobLease})
		if err != nil || !ok {
			return err
		}
		out = &ClaimedJob{ID: job.ID, DocumentID: job.DocumentID, Kind: job.Kind, Request: job.Request, Attempt: job.Attempt,
			ClaimToken: token, LeaseUntil: job.LeaseUntil, Deadline: job.Deadline}
		logTransition(Job{ID: job.ID, DocumentID: job.DocumentID, Kind: JobKind(job.Kind), State: JobQueued, Attempt: job.Attempt, RunID: job.RunID}, JobClaimed, "", time.Time{})
		return nil
	})
	return out, err
}

func claimToken() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "clm_" + hex.EncodeToString(b), nil
}

// Lease extends a held claim by the lease length, never past the deadline.
func Lease(ctx context.Context, scope store.AccountScope, jobID, token string) (time.Time, error) {
	var until time.Time
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		_, err := lockClaim(ctx, tx, jobID, token)
		if err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "creative_scene_jobs",
			"lease_until=LEAST(clock_timestamp()+interval '30 seconds', deadline), updated_at=clock_timestamp()", "id=$2", jobID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "creative_scene_jobs", "lease_until", "id=$2", jobID).Scan(&until)
	})
	return until, err
}

// lockClaim locks a job and checks that this token still holds it.
func lockClaim(ctx context.Context, tx store.TxAccountScope, jobID, token string) (Job, error) {
	var claimToken *string
	var leaseUntil *time.Time
	job, _, err := scanJob(tx.QueryRowForUpdate(ctx, "creative_scene_jobs", jobColumns, "id=$2", jobID))
	if err != nil {
		return Job{}, err
	}
	if err := tx.QueryRow(ctx, "creative_scene_jobs", "claim_token,lease_until", "id=$2", jobID).Scan(&claimToken, &leaseUntil); err != nil {
		return Job{}, err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return Job{}, err
	}
	if job.State != JobClaimed || claimToken == nil || *claimToken != token || leaseUntil == nil || !now.Before(*leaseUntil) {
		return Job{}, ErrJobLeaseLost
	}
	return job, nil
}

// ActiveJob is a running job as the editor sees it: no code, params or claim.
type ActiveJob struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	State     string          `json:"state"`
	RunID     *string         `json:"run_id"`
	CreatedAt time.Time       `json:"created_at"`
	Target    json.RawMessage `json:"target"`
}

// ListActive lists a scene's queued and claimed jobs, oldest first.
func ListActive(ctx context.Context, scope store.AccountScope, documentID string) ([]ActiveJob, error) {
	out := []ActiveJob{}
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		if _, err := requireScene(ctx, tx, documentID); err != nil {
			return err
		}
		rows, err := tx.QueryPage(ctx, "creative_scene_jobs", jobColumns, "document_id=$2 AND state IN ('queued','claimed')",
			[]store.OrderBy{{Column: "created_at"}, {Column: "id"}}, maxActiveJobs, 0, documentID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			job, created, err := scanJob(rows)
			if err != nil {
				return err
			}
			out = append(out, ActiveJob{ID: job.ID, Kind: string(job.Kind), State: job.State, RunID: job.RunID, CreatedAt: created, Target: activeTarget(job)})
		}
		return rows.Err()
	})
	return out, err
}

// activeTarget is what the editor highlights: the replaced instances, or where
// a new object will appear. Code and params stay out.
func activeTarget(job Job) json.RawMessage {
	if job.Kind != KindBakeModel {
		return json.RawMessage("null")
	}
	var req BakeRequest
	if json.Unmarshal(job.Request, &req) != nil {
		return json.RawMessage("null")
	}
	var target any
	switch req.Target.Op {
	case "add":
		var o struct {
			Name      string          `json:"name"`
			ParentID  *string         `json:"parent_id"`
			Transform json.RawMessage `json:"transform"`
		}
		_ = json.Unmarshal(req.Target.Object, &o)
		target = map[string]any{"op": "add", "object": o}
	case "replace":
		ids := req.Target.ObjectIDs
		if len(ids) == 0 {
			ids = []string{req.Target.ObjectID}
		}
		target = map[string]any{"op": "replace", "object_ids": ids}
	default:
		return json.RawMessage("null")
	}
	b, err := json.Marshal(target)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

// CancelRunJobs cancels a run's unfinished jobs, in the transaction that ends
// the run (the run row is already locked, so jobs come after it).
func CancelRunJobs(ctx context.Context, tx store.TxAccountScope, runID string) error {
	rows, err := tx.Query(ctx, "creative_scene_jobs", "id", "run_id=$2 AND state IN ('queued','claimed')", runID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	jobs, err := LockJobs(ctx, tx, ids)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if Terminal(job.State) {
			continue // finished between the read and the lock
		}
		if err := finishJob(ctx, tx, job, JobResult{Failure: &JobFailure{Code: JobCancelled, Message: "运行已结束，任务已取消"}}, false, true, time.Time{}); err != nil {
			return err
		}
	}
	return nil
}

// finishJob moves a locked job to its terminal state with its result.
func finishJob(ctx context.Context, tx store.TxAccountScope, job Job, res JobResult, expired, cancelled bool, created time.Time) error {
	body, err := json.Marshal(res)
	if err != nil {
		return err
	}
	to := res.state(expired, cancelled)
	if _, err := tx.Update(ctx, "creative_scene_jobs",
		"state=$3, result=$4, claim_token=NULL, editor_session_id=NULL, lease_until=NULL, updated_at=clock_timestamp()",
		"id=$2", job.ID, to, body); err != nil {
		return err
	}
	code := ""
	if res.Failure != nil {
		code = res.Failure.Code
	}
	logTransition(job, to, code, created)
	return nil
}
