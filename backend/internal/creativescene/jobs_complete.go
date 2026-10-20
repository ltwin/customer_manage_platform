package creativescene

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativestaging"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const completeOperationKey = "creative.scene_job.complete"

// Host-side failure codes a render host may report (api: 完成场景客户端任务).
var hostErrorCodes = []string{"compile_error", "compile_timeout", "limit_exceeded", "render_error", "host_error"}

// CompleteInput is the complete request's payload; JobID comes from the path.
type CompleteInput struct {
	JobID      string        `json:"job_id"`
	ClaimToken string        `json:"claim_token"`
	Outcome    string        `json:"outcome"`
	Error      *HostError    `json:"error,omitempty"`
	Bake       *BakeOutput   `json:"bake,omitempty"`
	Render     *RenderOutput `json:"render,omitempty"`
	Export     *ExportOutput `json:"export,omitempty"`
	// operationID is the completion's operation, recorded on the canvas
	// change an export makes.
	operationID string
}

type HostError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// BakeOutput is what the host baked: two staged uploads, the manifest it
// computed and the normalized module information the sandbox returned.
type BakeOutput struct {
	GLBUploadID       string          `json:"glb_upload_id"`
	ThumbnailUploadID string          `json:"thumbnail_upload_id"`
	Manifest          json.RawMessage `json:"manifest"`
	Module            json.RawMessage `json:"module"`
}

type RenderOutput struct {
	ImageUploadID string               `json:"image_upload_id"`
	SceneRevision creativeops.Revision `json:"scene_revision"`
}

// ExportOutput is what the host exported: the revision it rendered and the
// staged images, shaped as in the export request.
type ExportOutput struct {
	SceneRevision creativeops.Revision `json:"scene_revision"`
	Shots         []ExportShot         `json:"shots"`
}

// moduleInfo is the sandbox's module, as the host checked it.
type moduleInfo struct {
	ParamsSchema json.RawMessage `json:"params_schema"`
	Params       map[string]any  `json:"params"`
	Mount        string          `json:"mount"`
}

// CompleteResponse is the receipt: the job's final state and result.
type CompleteResponse struct {
	JobID  string    `json:"job_id"`
	State  string    `json:"state"`
	Result JobResult `json:"result"`
}

// runGateError carries a run that failed Admit out of the rolled-back
// transaction, so Complete can end it in a transaction of its own.
type runGateError struct {
	runID string
	cause error
}

func (e *runGateError) Error() string { return fmt.Sprintf("run %s: %v", e.runID, e.cause) }
func (e *runGateError) Unwrap() error { return ErrRunGate }

// Complete records a render host's result. In one transaction it admits the
// run the job belongs to, checks the claim, writes the result (a bake through
// the scene command in a savepoint), finishes the job and settles the run.
func Complete(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	var in CompleteInput
	receipt, err := (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: completeOperationKey, Capability: "manual_write",
		Validate: func(raw json.RawMessage) error {
			if err := creativeops.Decode(raw, &in); err != nil {
				return err
			}
			in.operationID = c.OperationID
			return in.validate()
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			resp, err := complete(ctx, tx, in)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			body, err := json.Marshal(resp)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			id := resp.JobID
			return creativeops.Outcome{HTTPStatus: 200, Response: body, ResultKind: "scene_job", ResultID: &id}, nil
		}}, c)
	var gate *runGateError
	if errors.As(err, &gate) {
		if w := currentWaiter(); w != nil {
			if abort := w.Abort(ctx, scope, gate.runID, gate.cause); abort != nil {
				return creativeops.Receipt{}, errors.Join(err, abort)
			}
		}
		return creativeops.Receipt{}, creativeops.Explain(ErrJobLeaseLost, "助手运行已不能继续写入，任务已取消")
	}
	return receipt, err
}

func (in CompleteInput) validate() error {
	if in.JobID == "" || in.ClaimToken == "" {
		return creativeops.ErrValidation
	}
	results := 0
	for _, set := range []bool{in.Bake != nil, in.Render != nil, in.Export != nil} {
		if set {
			results++
		}
	}
	switch in.Outcome {
	case JobSucceeded:
		if in.Error != nil || results != 1 {
			return invalid("成功时只带一个与任务类型对应的结果")
		}
	case JobFailed:
		if in.Error == nil || results != 0 {
			return invalid("失败时只带 error")
		}
		if !slices.Contains(hostErrorCodes, in.Error.Code) {
			return invalid("错误码 %q 不合法", in.Error.Code)
		}
		if in.Error.Message == "" || utf8.RuneCountInString(in.Error.Message) > 4000 {
			return invalid("错误信息为 1 到 4000 个字符")
		}
	default:
		return invalid("outcome 只能是 succeeded 或 failed")
	}
	if in.Bake != nil && (in.Bake.GLBUploadID == "" || in.Bake.ThumbnailUploadID == "" || len(in.Bake.Manifest) == 0 || len(in.Bake.Module) == 0) {
		return invalid("bake 缺少上传、manifest 或 module")
	}
	if in.Render != nil && (in.Render.ImageUploadID == "" || in.Render.SceneRevision < 1) {
		return invalid("render 缺少截图上传或 scene_revision")
	}
	if in.Export != nil {
		if in.Export.SceneRevision < 1 {
			return invalid("export 缺少 scene_revision")
		}
		return validateShots(in.Export.Shots)
	}
	return nil
}

func complete(ctx context.Context, tx store.TxAccountScope, in CompleteInput) (CompleteResponse, error) {
	job, created, err := scanJob(tx.QueryRow(ctx, "creative_scene_jobs", jobColumns, "id=$2", in.JobID))
	if err != nil {
		return CompleteResponse{}, err
	}
	w := currentWaiter()
	var gate RunGate
	if job.RunID != nil {
		if w == nil {
			return CompleteResponse{}, errors.New("creative scene: no job waiter registered")
		}
		gate, err = w.Admit(ctx, tx, *job.RunID)
		if errors.Is(err, ErrRunGate) {
			return CompleteResponse{}, &runGateError{runID: *job.RunID, cause: err}
		}
		if err != nil {
			return CompleteResponse{}, err
		}
	}
	job, err = lockClaim(ctx, tx, in.JobID, in.ClaimToken)
	if err != nil {
		return CompleteResponse{}, err
	}
	var res JobResult
	switch {
	case in.Outcome == JobFailed:
		res = JobResult{Failure: &JobFailure{Code: in.Error.Code, Message: in.Error.Message}}
	case job.Kind == KindBakeModel && in.Bake != nil:
		res, err = completeBake(ctx, tx, job, *in.Bake)
	case job.Kind == KindRenderView && in.Render != nil:
		res, err = completeRender(ctx, tx, job, *in.Render)
	case job.Kind == KindExportShots && in.Export != nil:
		res, err = completeExport(ctx, tx, job, *in.Export, in.operationID)
	default:
		return CompleteResponse{}, invalid("结果与任务类型 %s 不对应", job.Kind)
	}
	if err != nil {
		return CompleteResponse{}, err
	}
	if err := finishJob(ctx, tx, job, res, false, false, created); err != nil {
		return CompleteResponse{}, err
	}
	state := res.state(false, false)
	if gate != nil {
		done := job
		done.State = state
		done.Result, _ = json.Marshal(res)
		if err := w.Settle(ctx, tx, gate, done, res); err != nil {
			return CompleteResponse{}, err
		}
	}
	return CompleteResponse{JobID: job.ID, State: state, Result: res}, nil
}

// completeBake writes the baked model the way the job asked: commit_model plus
// add_objects or set_instance_model, as the run's agent. The scene command runs
// in a savepoint, so a conflict or a rejected result rolls back only the write
// and the job is recorded as failed for the assistant to redo.
func completeBake(ctx context.Context, tx store.TxAccountScope, job Job, out BakeOutput) (JobResult, error) {
	var req BakeRequest
	if err := json.Unmarshal(job.Request, &req); err != nil {
		return JobResult{}, err
	}
	actor := creativedocument.Photographer
	if job.RunID != nil {
		actor = creativedocument.Actor{Kind: "agent", RunID: job.RunID}
	}
	return writeBake(ctx, tx, job.DocumentID, actor, req, out)
}

// writeBake writes a baked model as req asks, in a savepoint: a conflict or a
// refused result rolls back only the write and comes back as the failure.
func writeBake(ctx context.Context, tx store.TxAccountScope, documentID string, actor creativedocument.Actor, req BakeRequest, out BakeOutput) (JobResult, error) {
	var module moduleInfo
	if err := creativeops.Decode(out.Module, &module); err != nil {
		return JobResult{}, invalid("module 格式不正确")
	}
	sum := sha256.Sum256([]byte(req.Code))
	commit := map[string]any{
		"op": "commit_model", "ref": "m", "glb_upload_id": out.GLBUploadID, "thumbnail_upload_id": out.ThumbnailUploadID,
		"generator": map[string]any{
			"language": "three-module", "contract_version": 1, "lib_version": req.LibVersion,
			"code": req.Code, "code_sha256": "sha256-" + hex.EncodeToString(sum[:]),
			"params_schema": module.ParamsSchema, "params": module.Params, "mount": module.Mount,
		},
		"manifest": out.Manifest,
	}
	if req.NameHint != "" {
		commit["name_hint"] = req.NameHint
	}
	if req.Target.BaseContentID != "" {
		commit["base_content_id"] = req.Target.BaseContentID
	}
	var place map[string]any
	expected := map[string]*creativeops.Revision{}
	var objectIDs []string
	switch req.Target.Op {
	case "add":
		cur, err := creativedocument.ReadCurrentInTx(ctx, tx, documentID, TypeKey)
		if err != nil {
			return failedWrite(err, out)
		}
		scene, err := decodeScene(cur.Content.Payload)
		if err != nil {
			return JobResult{}, err
		}
		object := map[string]any{}
		if err := json.Unmarshal(req.Target.Object, &object); err != nil {
			return JobResult{}, err
		}
		id, _ := object["id"].(string)
		var parent *string
		if p, ok := object["parent_id"].(string); ok {
			parent = &p
		}
		object["kind"] = KindModelInstance
		object["order"] = nextOrder(scene, parent)
		object["model"] = map[string]any{"ref": "m"}
		object["material_slots"] = map[string]any{}
		if _, ok := object["visible"]; !ok {
			object["visible"] = true
		}
		if _, ok := object["locked"]; !ok {
			object["locked"] = false
		}
		place = map[string]any{"op": "add_objects", "objects": []any{object}}
		r := creativeops.Revision(cur.UnitRevs[unitObjects])
		expected[unitObjects] = &r
		objectIDs = []string{id}
	case "replace":
		place = map[string]any{"op": "set_instance_model", "object_id": req.Target.ObjectID, "model": map[string]any{"ref": "m"}, "scope": req.Target.Scope}
		objectIDs = []string{req.Target.ObjectID}
		if req.Target.Scope == "all_instances" {
			place["object_ids"] = req.Target.ObjectIDs
			objectIDs = req.Target.ObjectIDs
		}
		expected = req.Expected
	default:
		return JobResult{}, fmt.Errorf("bake for %s: target %q", documentID, req.Target.Op)
	}
	payload, err := json.Marshal(map[string]any{"document_id": documentID, "expected": expected, "operations": []any{commit, place}})
	if err != nil {
		return JobResult{}, err
	}
	var written creativedocument.CommandResult
	err = tx.Savepoint(ctx, "scene_job_write", func() error {
		var err error
		written, err = creativedocument.CommandInTx(ctx, tx, actor, payload)
		return err
	})
	if err != nil {
		return failedWrite(err, out)
	}
	var models map[string]struct {
		ContentID         string `json:"content_id"`
		ContentRevisionID string `json:"content_revision_id"`
	}
	if err := json.Unmarshal(written.Extra["models"], &models); err != nil || models["m"].ContentRevisionID == "" {
		return JobResult{}, fmt.Errorf("bake for %s: receipt models %s", documentID, written.Extra["models"])
	}
	return JobResult{Bake: &BakeResult{ContentID: models["m"].ContentID, ContentRevisionID: models["m"].ContentRevisionID,
		ObjectIDs: objectIDs, Revision: creativeops.Revision(written.Revision)}}, nil
}

// failedWrite turns a refused scene write into the job's failure. A staged
// upload that is gone is the host's to fix (re-upload), so it is returned and
// rolls the whole completion back.
func failedWrite(err error, out BakeOutput) (JobResult, error) {
	if errors.Is(err, ErrUploadUnavailable) {
		return JobResult{}, err
	}
	if c, ok := creativedocument.IsPublicConflict(err); ok {
		conflicts := make([]Conflict, 0, len(c.Conflicts))
		for _, u := range c.Conflicts {
			conflicts = append(conflicts, Conflict{Unit: u.Unit, Expected: u.ExpectedRev, Current: u.CurrentRev})
		}
		return JobResult{Failure: &JobFailure{Code: "conflict", Message: "烘焙期间这些单元被改过，没有写入",
			Conflicts: conflicts, Bake: &BakeArtifacts{GLBUploadID: out.GLBUploadID, ThumbnailUploadID: out.ThumbnailUploadID, Module: out.Module, Manifest: out.Manifest}}}, nil
	}
	for _, refused := range []error{creativeops.ErrValidation, creativedocument.ErrSizeLimit, ErrModelUnavailable, creativedocument.ErrTrashed, creativedocument.ErrNotFound} {
		if errors.Is(err, refused) {
			return JobResult{Failure: &JobFailure{Code: "invalid_result", Message: reasonOf(err)}}, nil
		}
	}
	return JobResult{}, err
}

func reasonOf(err error) string {
	var public interface{ PublicMessage() string }
	if errors.As(err, &public) {
		return public.PublicMessage()
	}
	switch {
	case errors.Is(err, creativedocument.ErrTrashed):
		return "场景已在回收站"
	case errors.Is(err, creativedocument.ErrNotFound):
		return "场景已删除"
	case errors.Is(err, ErrModelUnavailable):
		return "引用的模型不可用"
	}
	return "结果不合规"
}

// nextOrder puts a new object after its siblings at the moment it is written.
func nextOrder(s Scene, parent *string) int {
	next := 0
	for _, o := range s.Objects {
		if (o.ParentID == nil) != (parent == nil) || parent != nil && *o.ParentID != *parent {
			continue
		}
		if o.Order >= next {
			next = o.Order + 1
		}
	}
	return next
}

// completeRender keeps the screenshot staged and records which revision it shows.
func completeRender(ctx context.Context, tx store.TxAccountScope, job Job, out RenderOutput) (JobResult, error) {
	head, err := creativedocument.ReadHeadInTx(ctx, tx, job.DocumentID, TypeKey)
	if err != nil {
		return JobResult{}, err
	}
	if int64(out.SceneRevision) > head.Revision {
		return JobResult{}, invalid("scene_revision %d 超过当前修订 %d", out.SceneRevision, head.Revision)
	}
	staged, err := creativestaging.Lock(ctx, tx, out.ImageUploadID)
	if errors.Is(err, creativestaging.ErrUnavailable) {
		return JobResult{}, ErrUploadUnavailable
	}
	if err != nil {
		return JobResult{}, err
	}
	if staged.Kind != "image" {
		return JobResult{}, invalid("截图必须是图片上传")
	}
	return JobResult{Render: &RenderResult{ImageUploadID: out.ImageUploadID, ContentRevisionID: staged.ContentRevisionID, SceneRevision: out.SceneRevision}}, nil
}

// completeExport places the exported images as the run, in a savepoint (E4):
// the nodes exist only if the job succeeds. The shots must be exactly the
// cameras and passes the job asked for; rows follow the job's camera order.
// An export that cannot be placed fails the job and the run goes on; a
// staged image that is gone rolls the completion back for the host to
// upload again.
func completeExport(ctx context.Context, tx store.TxAccountScope, job Job, out ExportOutput, operation string) (JobResult, error) {
	var req ExportRequest
	if err := json.Unmarshal(job.Request, &req); err != nil {
		return JobResult{}, err
	}
	byCamera := make(map[string]ExportShot, len(out.Shots))
	for _, s := range out.Shots {
		byCamera[s.CameraObjectID] = s
	}
	mismatch := invalid("export.shots 须正好是任务请求的摄像机 %v 和导出方式 %v", req.CameraObjectIDs, req.Passes)
	if len(out.Shots) != len(req.CameraObjectIDs) {
		return JobResult{}, mismatch
	}
	shots := make([]ExportShot, 0, len(req.CameraObjectIDs))
	for _, id := range req.CameraObjectIDs {
		s, ok := byCamera[id]
		if !ok || len(s.Images) != len(req.Passes) {
			return JobResult{}, mismatch
		}
		for _, img := range s.Images {
			if !slices.Contains(req.Passes, img.Pass) {
				return JobResult{}, mismatch
			}
		}
		shots = append(shots, s)
	}
	actor := creativedocument.Photographer
	if job.RunID != nil {
		actor = creativedocument.Actor{Kind: "agent", RunID: job.RunID}
	}
	in := ExportInput{DocumentID: job.DocumentID, CanvasID: req.CanvasID, SceneNodeID: req.SceneNodeID, SceneRevision: out.SceneRevision, Shots: shots}
	var placed ExportResult
	err := tx.Savepoint(ctx, "scene_job_export", func() error {
		var err error
		placed, err = placeExportsInTx(ctx, tx, in, actor, operation)
		return err
	})
	switch {
	case err == nil:
		return JobResult{Export: &placed}, nil
	case errors.Is(err, ErrUploadUnavailable):
		return JobResult{}, err
	}
	for _, refused := range []error{creativeops.ErrValidation, creativedocument.ErrSizeLimit, creativedocument.ErrTrashed, creativedocument.ErrNotFound,
		creativecanvas.ErrArchived, creativecanvas.ErrNotFound, creativecanvas.ErrLimit} {
		if errors.Is(err, refused) {
			return JobResult{Failure: &JobFailure{Code: "invalid_result", Message: exportReason(err)}}, nil
		}
	}
	return JobResult{}, err
}

func exportReason(err error) string {
	switch {
	case errors.Is(err, creativecanvas.ErrArchived):
		return "画布已归档，导出图没有放置"
	case errors.Is(err, creativecanvas.ErrNotFound):
		return "场景节点已不在画布上，导出图没有放置"
	}
	return "导出图没有放置：" + reasonOf(err)
}

// RetryInput is write_model's retry_job_id: a bake of this conversation that
// failed on a conflict, written again at a new baseline and target from what
// it already baked.
type RetryInput struct {
	DocumentID       string
	RunID            string
	ConversationRuns []string
	RetryJobID       string
	BasedOn          int64
	Add              *AddTarget
	Replace          *ReplaceTarget
}

// RetryBakeInTx writes a conflicted bake's artifacts in the caller's
// transaction, without a job: the code is not compiled again. A replace meets
// the baseline check again. Two retries of one job serialize on the job row,
// and the second finds the staged uploads consumed.
func RetryBakeInTx(ctx context.Context, tx store.TxAccountScope, in RetryInput) (*BakeResult, *WriteConflict, error) {
	if (in.Add == nil) == (in.Replace == nil) {
		return nil, nil, invalid("target 须为 add 或 replace 其中之一")
	}
	refused := invalid("retry_job_id 只能指向本会话在同一场景里因冲突失败的烘焙任务")
	jobs, err := LockJobs(ctx, tx, []string{in.RetryJobID})
	if err != nil {
		return nil, nil, err
	}
	if len(jobs) != 1 {
		return nil, nil, refused
	}
	job := jobs[0]
	if job.Kind != KindBakeModel || job.State != JobFailed || job.DocumentID != in.DocumentID || job.RunID == nil ||
		*job.RunID != in.RunID && !slices.Contains(in.ConversationRuns, *job.RunID) {
		return nil, nil, refused
	}
	var failure JobFailure
	if err := json.Unmarshal(job.Result, &failure); err != nil || failure.Code != "conflict" || failure.Bake == nil {
		return nil, nil, refused
	}
	var original BakeRequest
	if err := json.Unmarshal(job.Request, &original); err != nil {
		return nil, nil, err
	}
	cur, err := creativedocument.LockCurrentInTx(ctx, tx, in.DocumentID, TypeKey)
	if err != nil {
		return nil, nil, err
	}
	if cur.Document.TrashedAt != nil {
		return nil, nil, creativedocument.ErrTrashed
	}
	if in.BasedOn < 1 || in.BasedOn > cur.Revision {
		return nil, nil, invalid("based_on_revision 须在 1 到当前修订 %d 之间", cur.Revision)
	}
	scene, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return nil, nil, err
	}
	req := BakeRequest{Code: original.Code, Params: original.Params, LibVersion: original.LibVersion, NameHint: original.NameHint, Expected: map[string]*creativeops.Revision{}}
	idx := indexObjects(scene.Objects)
	if in.Add != nil {
		if req.Target, err = addTarget(scene, idx, *in.Add, req.NameHint); err != nil {
			return nil, nil, err
		}
	} else {
		target, units, err := replaceTarget(ctx, tx, scene, idx, *in.Replace)
		if err != nil {
			return nil, nil, err
		}
		conflict, err := checkBaseline(ctx, tx, cur, in.BasedOn, in.RunID, units)
		if err != nil {
			return nil, nil, err
		}
		if conflict != nil {
			conflict.RetryJobID = job.ID
			return nil, conflict, nil
		}
		for _, u := range units {
			req.Expected[u] = currentRev(cur.UnitRevs, u)
		}
		req.Target = target
	}
	artifacts := failure.Bake
	res, err := writeBake(ctx, tx, in.DocumentID, creativedocument.Actor{Kind: "agent", RunID: &in.RunID}, req,
		BakeOutput{GLBUploadID: artifacts.GLBUploadID, ThumbnailUploadID: artifacts.ThumbnailUploadID, Manifest: artifacts.Manifest, Module: artifacts.Module})
	if errors.Is(err, ErrUploadUnavailable) {
		return nil, nil, invalid("这份烘焙结果已被使用或已过期，请重新调用 write_model 生成")
	}
	if err != nil {
		return nil, nil, err
	}
	if res.Failure != nil {
		return nil, nil, invalid("没有写入：%s", res.Failure.Message)
	}
	return res.Bake, nil, nil
}
