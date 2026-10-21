package creativescene

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/creativestaging"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// opEnv is what operations touching content need inside the command
// transaction. A nil tx means a pure replay with no content operations.
type opEnv struct {
	ctx      context.Context
	tx       store.TxAccountScope
	retain   creativedocument.Retain
	document string
	revision int64 // the revision this batch produces

	refs     map[string]creativecontent.Revision // commit_model ref → new revision
	order    []string                            // refs in batch order
	held     map[string]bool                     // revisions the current snapshot references
	verified map[string]bool                     // existing revisions checked this batch
}

// newOpEnv prepares the operations of a batch on cur; the batch produces the
// next revision.
func newOpEnv(ctx context.Context, tx store.TxAccountScope, retain creativedocument.Retain, cur creativedocument.Current) *opEnv {
	held := make(map[string]bool, len(cur.Refs))
	for _, id := range cur.Refs {
		held[id] = true
	}
	return &opEnv{ctx: ctx, tx: tx, retain: retain, document: cur.Document.ID, revision: cur.Revision + 1,
		refs: map[string]creativecontent.Revision{}, held: held, verified: map[string]bool{}}
}

// verifyExisting checks every existing model revision the batch points at.
// Revisions the current snapshot already references (copies of placed
// instances) are kept alive by its reference rows, under the document lock,
// so they are read together without row locks. New ones are locked one by
// one in sorted order, so concurrent batches take content locks in one order.
func (env *opEnv) verifyExisting(ops []sceneOp) error {
	seen := map[string]bool{}
	var held, fresh []string
	add := func(m *ModelRef) {
		if m == nil || m.Ref != "" || m.ContentRevisionID == "" || seen[m.ContentRevisionID] {
			return
		}
		id := m.ContentRevisionID
		seen[id] = true
		if env.held[id] {
			held = append(held, id)
		} else {
			fresh = append(fresh, id)
		}
	}
	for _, op := range ops {
		switch o := op.(type) {
		case *addObjectsOp:
			for i := range o.Objects {
				if o.Objects[i].Kind == KindModelInstance {
					add(o.Objects[i].Model)
				}
			}
		case *setInstanceModelOp:
			add(&o.Model)
		}
	}
	if len(held) > 0 {
		batch, err := creativecontent.ReadDisplayBatchInTx(env.ctx, env.tx, held)
		if err != nil {
			return err
		}
		for _, id := range held {
			res := batch[id]
			if _, ok := completeModel(res.Revision); res.Err != nil || !ok {
				return modelUnavailable(id)
			}
			env.verified[id] = true
		}
	}
	sort.Strings(fresh)
	for _, id := range fresh {
		if err := requireModel(env.ctx, env.tx, id); err != nil {
			return err
		}
		env.verified[id] = true
	}
	return nil
}

// resolve turns {ref} into the revision committed earlier in the batch and
// checks {content_revision_id}; afterwards m holds only a revision id.
func (env *opEnv) resolve(m *ModelRef) error {
	switch {
	case m.Ref != "" && m.ContentRevisionID != "":
		return invalid("model 只能写 ref 或 content_revision_id 其中之一")
	case m.Ref != "":
		r, ok := env.refs[m.Ref]
		if !ok {
			return invalid("model 引用的 ref %q 没有在它之前的 commit_model 中定义", m.Ref)
		}
		*m = ModelRef{ContentRevisionID: r.ID}
		return nil
	case m.ContentRevisionID == "":
		return invalid("model 缺少 content_revision_id 或 ref")
	case !env.verified[m.ContentRevisionID]:
		if err := requireModel(env.ctx, env.tx, m.ContentRevisionID); err != nil {
			return err
		}
		env.verified[m.ContentRevisionID] = true
	}
	return nil
}

// finish requires every committed model to stay referenced by the resulting
// scene s, so a commit never creates content the snapshot does not hold (the
// document layer would report that as a type contract violation), and returns
// the receipt's models.
func (env *opEnv) finish(s Scene) (map[string]json.RawMessage, error) {
	if len(env.refs) == 0 {
		return nil, nil
	}
	type committed struct {
		ContentID         string `json:"content_id"`
		ContentRevisionID string `json:"content_revision_id"`
	}
	inScene := map[string]bool{}
	for _, id := range modelRefs(s.Objects) {
		inScene[id] = true
	}
	models := make(map[string]committed, len(env.refs))
	for _, ref := range env.order {
		r := env.refs[ref]
		if !inScene[r.ID] {
			return nil, invalid("commit_model %s 写入的模型没有被场景中的物件引用", ref)
		}
		models[ref] = committed{ContentID: r.ContentID, ContentRevisionID: r.ID}
	}
	raw, err := json.Marshal(models)
	if err != nil {
		return nil, err
	}
	return map[string]json.RawMessage{"models": raw}, nil
}

// commitModelOp writes the complete form of a model from two consumed staged
// uploads: the GLB (whose staged form carries the server's manifest) and a PNG
// thumbnail. It changes no scene unit; a later operation places the model.
type commitModelOp struct {
	Op                string               `json:"op"`
	Ref               string               `json:"ref"`
	GLBUploadID       string               `json:"glb_upload_id"`
	ThumbnailUploadID string               `json:"thumbnail_upload_id"`
	Generator         Generator            `json:"generator"`
	Manifest          creativeglb.Manifest `json:"manifest"`
	NameHint          string               `json:"name_hint,omitempty"`
	BaseContentID     string               `json:"base_content_id,omitempty"`
}

func (op *commitModelOp) touched(Scene, map[string]int) ([]string, error) { return nil, nil }

func (op *commitModelOp) apply(env *opEnv, _ *Scene, _ *changeSet) error {
	if !refPattern.MatchString(op.Ref) {
		return invalid("ref %q 不合法", op.Ref)
	}
	if _, dup := env.refs[op.Ref]; dup {
		return invalid("ref %s 在本批中重复", op.Ref)
	}
	if utf8.RuneCountInString(op.NameHint) > maxNameHintRunes {
		return invalid("name_hint 不能超过 %d 个字", maxNameHintRunes)
	}
	generator, err := checkGenerator(op.Generator)
	if err != nil {
		return err
	}
	ctx, tx := env.ctx, env.tx
	glb, glbStaged, err := lockStaged(ctx, tx, op.GLBUploadID, modelKind)
	if err != nil {
		return err
	}
	staged, ok := decodeModelPayload(glbStaged.Payload.Model)
	if glbStaged.Kind != modelKind || !ok || staged.Generator != nil {
		return invalid("上传 %s 不是模型", op.GLBUploadID)
	}
	thumb, thumbStaged, err := lockStaged(ctx, tx, op.ThumbnailUploadID, "image")
	if err != nil {
		return err
	}
	glbBlob, glbOK := original(glbStaged)
	thumbBlob, thumbOK := original(thumbStaged)
	if !glbOK || !thumbOK || thumbBlob.Mime != "image/png" {
		return invalid("缩略图须为 PNG 图片")
	}
	if !op.Manifest.Matches(staged.Manifest) {
		return invalid("manifest 与上传的 GLB 不符")
	}
	payload, err := json.Marshal(ModelPayload{Format: "glb", Generator: &generator, Manifest: staged.Manifest, NameHint: op.NameHint})
	if err != nil {
		return err
	}
	base := op.BaseContentID
	if base != "" {
		// A library copy belongs to the library: an edit of an instance placed
		// from it forks, whoever makes it.
		copied, err := isLibraryCopy(ctx, tx, base)
		if err != nil {
			return err
		}
		if copied {
			base = ""
		}
	}
	draft := creativecontent.Draft{Kind: modelKind, DeclarationID: glbStaged.DeclarationID, Payload: creativecontent.Payload{Model: payload}}
	objects := []creativecontent.ObjectBinding{{Role: "original", BlobID: glbBlob.BlobID}, {Role: "thumbnail", BlobID: thumbBlob.BlobID}}
	r, err := creativecontent.WriteModelAndRetain(ctx, tx, draft, base, objects, env.retain)
	if base != "" && (errors.Is(err, creativecontent.ErrNotFound) || errors.Is(err, creativeops.ErrValidation)) {
		return invalid("base_content_id %s 不是本账号的模型", op.BaseContentID)
	}
	if err != nil {
		return err
	}
	if err := creativestaging.Consume(ctx, tx, glb, env.document, env.revision); err != nil {
		return err
	}
	if err := creativestaging.Consume(ctx, tx, thumb, env.document, env.revision); err != nil {
		return err
	}
	env.refs[op.Ref] = r
	env.order = append(env.order, op.Ref)
	return nil
}

// isLibraryCopy reports whether a content is a library copy, the only kind
// whose revision carries source_revision_id. Copies never gain revisions and
// nothing becomes one, so the answer needs no lock.
func isLibraryCopy(ctx context.Context, tx store.TxAccountScope, contentID string) (bool, error) {
	return tx.Exists(ctx, "creative_content_revisions", "content_id=$2 AND payload->'model'->>'source_revision_id' IS NOT NULL", contentID)
}

// lockStaged locks a staged upload of the given declared kind and reads the
// content revision it holds.
func lockStaged(ctx context.Context, tx store.TxAccountScope, uploadID, kind string) (creativestaging.Staged, creativecontent.Revision, error) {
	s, err := creativestaging.Lock(ctx, tx, uploadID)
	if errors.Is(err, creativestaging.ErrUnavailable) {
		return s, creativecontent.Revision{}, creativeops.Explain(ErrUploadUnavailable, "上传 %s 不可用，请重新生成", uploadID)
	}
	if err != nil {
		return s, creativecontent.Revision{}, err
	}
	if s.Kind != kind {
		return s, creativecontent.Revision{}, invalid("上传 %s 的类型须为 %s", uploadID, kind)
	}
	r, err := creativecontent.RequireUsable(ctx, tx, s.ContentRevisionID, "display")
	if errors.Is(err, creativecontent.ErrNotFound) || errors.Is(err, creativecontent.ErrUsageDenied) || errors.Is(err, creativecontent.ErrMissingRoot) {
		return s, creativecontent.Revision{}, creativeops.Explain(ErrUploadUnavailable, "上传 %s 不可用，请重新生成", uploadID)
	}
	return s, r, err
}

func original(r creativecontent.Revision) (creativecontent.MediaObject, bool) {
	for _, m := range r.Media {
		if m.Role == "original" {
			return m, true
		}
	}
	return creativecontent.MediaObject{}, false
}

// setInstanceModelOp points model instances at another model revision. Only
// content units change, so moving the same object concurrently never
// conflicts. all_instances lists the instances explicitly: expanding them on
// the server would miss units for instances added concurrently.
type setInstanceModelOp struct {
	Op        string   `json:"op"`
	ObjectID  string   `json:"object_id"`
	Model     ModelRef `json:"model"`
	Scope     string   `json:"scope,omitempty"`
	ObjectIDs []string `json:"object_ids,omitempty"`
}

func (op *setInstanceModelOp) checkScope() error {
	switch op.Scope {
	case "", "this":
		if len(op.ObjectIDs) != 0 {
			return invalid("只改当前实例时不能带 object_ids")
		}
	case "all_instances":
		if len(op.ObjectIDs) == 0 || len(op.ObjectIDs) > maxBatchItems || !slices.Contains(op.ObjectIDs, op.ObjectID) {
			return invalid("all_instances 须在 object_ids 中列出全部共用实例，并包含 object_id")
		}
	default:
		return invalid("scope 须为 this 或 all_instances")
	}
	return nil
}

// targets are the instances to change; with all_instances each must reference
// the same revision as object_id in s.
func (op *setInstanceModelOp) targets(s Scene, byID map[string]int) ([]string, error) {
	instance := func(id string) (*Object, error) {
		i, ok := byID[id]
		if !ok {
			return nil, invalid("物件 %s 不存在", id)
		}
		if s.Objects[i].Kind != KindModelInstance || s.Objects[i].Model == nil {
			return nil, invalid("物件 %s 不是模型实例", id)
		}
		return &s.Objects[i], nil
	}
	base, err := instance(op.ObjectID)
	if err != nil {
		return nil, err
	}
	if op.Scope != "all_instances" {
		return []string{op.ObjectID}, nil
	}
	seen := make(map[string]bool, len(op.ObjectIDs))
	for _, id := range op.ObjectIDs {
		if seen[id] {
			return nil, invalid("物件 %s 在 object_ids 中出现了两次", id)
		}
		seen[id] = true
		o, err := instance(id)
		if err != nil {
			return nil, err
		}
		if o.Model.ContentRevisionID != base.Model.ContentRevisionID {
			return nil, invalid("物件 %s 与 %s 引用的不是同一个模型修订", id, op.ObjectID)
		}
	}
	return op.ObjectIDs, nil
}

func (op *setInstanceModelOp) touched(s Scene, byID map[string]int) ([]string, error) {
	ids, err := op.targets(s, byID)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = contentUnit(id)
	}
	return out, nil
}

func (op *setInstanceModelOp) apply(env *opEnv, s *Scene, c *changeSet) error {
	byID := indexObjects(s.Objects)
	ids, err := op.targets(*s, byID)
	if err != nil {
		return err
	}
	model := op.Model
	if err := env.resolve(&model); err != nil {
		return err
	}
	for _, id := range ids {
		m := model
		s.Objects[byID[id]].Model = &m
		c.change(contentUnit(id))
	}
	return nil
}
