package creativescene

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	KindModelInstance = "model_instance"

	modelKind          = "model"
	maxCodeBytes       = 64 << 10
	maxModelParams     = 32
	maxSceneModels     = 300
	maxCommitsPerBatch = 16
	maxNameHintRunes   = 80
	maxEnumOptions     = 32
	maxOptionRunes     = 64
	maxParamUnitRunes  = 16
)

var (
	// ErrUploadUnavailable is a staged upload that is missing, someone
	// else's, expired, not ready or already consumed (409
	// creative_upload_unavailable). It is never a document-level 404 or 409.
	ErrUploadUnavailable = errors.New("creative scene: staged upload unavailable")
	// ErrModelUnavailable is a referenced model revision that does not exist,
	// is not a complete model, or cannot be displayed (400
	// creative_model_unavailable).
	ErrModelUnavailable = errors.New("creative scene: model unavailable")

	refPattern        = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	paramNamePattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	revisionIDPattern = regexp.MustCompile(`^ccrv_[A-Za-z0-9_-]{1,64}$`)
	mounts            = []string{"floor", "wall", "ceiling", "free"}
)

// ModelRef points a model instance at an immutable model revision. In a
// stored snapshot only ContentRevisionID is set; Ref names a commit_model of
// the same batch and is resolved before the snapshot is validated.
type ModelRef struct {
	ContentRevisionID string `json:"content_revision_id,omitempty"`
	Ref               string `json:"ref,omitempty"`
}

// Generator is how a model was made: the geometry module and the values it was
// baked with. Code is left out of read views.
type Generator struct {
	Language        string               `json:"language"`
	ContractVersion int                  `json:"contract_version"`
	LibVersion      int                  `json:"lib_version"`
	Code            string               `json:"code,omitempty"`
	CodeSHA256      string               `json:"code_sha256"`
	ParamsSchema    map[string]paramSpec `json:"params_schema"`
	Params          map[string]any       `json:"params"`
	Mount           string               `json:"mount"`
}

// ModelPayload is payload.model of model content. The staged form (written by
// the media worker) has no generator; scenes reference only the complete form.
type ModelPayload struct {
	Format    string               `json:"format"`
	Generator *Generator           `json:"generator,omitempty"`
	Manifest  creativeglb.Manifest `json:"manifest"`
	NameHint  string               `json:"name_hint,omitempty"`
}

func decodeModelPayload(raw json.RawMessage) (ModelPayload, bool) {
	var p ModelPayload
	if len(raw) == 0 || json.Unmarshal(raw, &p) != nil || p.Format != "glb" {
		return ModelPayload{}, false
	}
	return p, true
}

// checkGenerator validates a submitted generator and returns it in canonical
// form: the parameter definitions checked like the builtin catalog and every
// parameter filled in. The definitions are the code's own declaration; the
// server cannot match them against the code, which is fine because the GLB,
// not the code, is the geometry.
func checkGenerator(g Generator) (Generator, error) {
	if g.Language != "three-module" || g.ContractVersion != 1 || g.LibVersion != 1 {
		return Generator{}, invalid("generator 须为 three-module，contract_version 与 lib_version 均为 1")
	}
	if g.Code == "" || len(g.Code) > maxCodeBytes || !utf8.ValidString(g.Code) {
		return Generator{}, invalid("代码须为 1 字节到 64 KiB 的 UTF-8 文本")
	}
	sum := sha256.Sum256([]byte(g.Code))
	if g.CodeSHA256 != "sha256-"+hex.EncodeToString(sum[:]) {
		return Generator{}, invalid("code_sha256 与代码不符")
	}
	if !slices.Contains(mounts, g.Mount) {
		return Generator{}, invalid("mount 须为 floor、wall、ceiling 或 free")
	}
	schema, err := checkParamsSchema(g.ParamsSchema)
	if err != nil {
		return Generator{}, err
	}
	params, err := fillParams(schema, g.Params)
	if err != nil {
		return Generator{}, err
	}
	g.ParamsSchema, g.Params = schema, params
	return g, nil
}

func checkParamsSchema(in map[string]paramSpec) (map[string]paramSpec, error) {
	if len(in) > maxModelParams {
		return nil, invalid("参数不能超过 %d 个", maxModelParams)
	}
	out := make(map[string]paramSpec, len(in))
	for name, p := range in {
		if !paramNamePattern.MatchString(name) {
			return nil, invalid("参数名 %q 不合法", name)
		}
		checked, err := p.check()
		if err != nil {
			return nil, invalid("参数 %s：%v", name, err)
		}
		out[name] = checked
	}
	return out, nil
}

// check validates one parameter definition from untrusted code. Numeric
// parameters need a range so the parameter panel can bound them.
func (p paramSpec) check() (paramSpec, error) {
	switch p.Type {
	case "number", "integer":
		if p.Min == nil || p.Max == nil || !finite(*p.Min) || !finite(*p.Max) || *p.Min > *p.Max {
			return p, errors.New("须有 min ≤ max")
		}
		if p.Step != nil && (!finite(*p.Step) || *p.Step <= 0) {
			return p, errors.New("step 须大于 0")
		}
		if p.Options != nil {
			return p, errors.New("数值参数不能有 options")
		}
	case "enum":
		if len(p.Options) == 0 || len(p.Options) > maxEnumOptions {
			return p, fmt.Errorf("options 须为 1 到 %d 项", maxEnumOptions)
		}
		seen := map[string]bool{}
		for _, o := range p.Options {
			if n := utf8.RuneCountInString(o); n == 0 || n > maxOptionRunes || seen[o] {
				return p, errors.New("options 不能为空、过长或重复")
			}
			seen[o] = true
		}
		fallthrough
	case "boolean", "color":
		if p.Min != nil || p.Max != nil || p.Step != nil || p.Unit != "" || p.Type != "enum" && p.Options != nil {
			return p, fmt.Errorf("%s 参数只能有 default 和 label", p.Type)
		}
	default:
		return p, fmt.Errorf("未知参数类型 %q", p.Type)
	}
	if utf8.RuneCountInString(p.Label) > maxLabelRunes || utf8.RuneCountInString(p.Unit) > maxParamUnitRunes {
		return p, errors.New("label 或 unit 过长")
	}
	d, err := p.value(p.Default)
	if err != nil {
		return p, fmt.Errorf("默认值%w", err)
	}
	p.Default = d
	return p, nil
}

// fillParams checks every given value against its definition and fills the
// missing ones with defaults; a key without a definition is rejected.
func fillParams(schema map[string]paramSpec, in map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(schema))
	for k, v := range in {
		p, ok := schema[k]
		if !ok {
			return nil, invalid("参数 %q 没有定义", k)
		}
		canonical, err := p.value(v)
		if err != nil {
			return nil, invalid("参数 %s %v", k, err)
		}
		out[k] = canonical
	}
	for k, p := range schema {
		if _, ok := out[k]; !ok {
			out[k] = p.Default
		}
	}
	return out, nil
}

// requireModel checks that a referenced revision is a complete model this
// account can display.
func requireModel(ctx context.Context, tx store.TxAccountScope, id string) error {
	if !revisionIDPattern.MatchString(id) {
		return modelUnavailable(id)
	}
	r, err := creativecontent.RequireUsable(ctx, tx, id, "display")
	if errors.Is(err, creativecontent.ErrNotFound) || errors.Is(err, creativecontent.ErrUsageDenied) || errors.Is(err, creativecontent.ErrMissingRoot) {
		return modelUnavailable(id)
	}
	if err != nil {
		return err
	}
	if _, ok := completeModel(r); !ok {
		return modelUnavailable(id)
	}
	return nil
}

// completeModel decodes a revision a scene may reference: model content in
// its complete form, with a generator.
func completeModel(r creativecontent.Revision) (ModelPayload, bool) {
	p, ok := decodeModelPayload(r.Payload.Model)
	return p, ok && r.Kind == modelKind && p.Generator != nil
}

func modelUnavailable(id string) error {
	return creativeops.Explain(ErrModelUnavailable, "模型 %s 不可用", id)
}

// modelRefs are the distinct model revisions a scene references, sorted. They
// are the snapshot's references.
func modelRefs(objects []Object) []string {
	seen := map[string]bool{}
	var out []string
	for _, o := range objects {
		if o.Model != nil && o.Model.ContentRevisionID != "" && !seen[o.Model.ContentRevisionID] {
			seen[o.Model.ContentRevisionID] = true
			out = append(out, o.Model.ContentRevisionID)
		}
	}
	sort.Strings(out)
	return out
}

// modelView is one entry of a read's models: what the editor needs to place
// and re-bake a model, never its code or a storage address.
type modelView struct {
	ContentID string                `json:"content_id"`
	Usable    bool                  `json:"usable"`
	Manifest  *creativeglb.Manifest `json:"manifest"`
	Generator *Generator            `json:"generator"`
	NameHint  *string               `json:"name_hint"`
}

// readModels reads the listed revisions in the caller's snapshot with a fixed
// number of queries, however many models there are.
func readModels(ctx context.Context, tx store.ReadTxAccountScope, ids []string) (map[string]modelView, error) {
	out := make(map[string]modelView, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	batch, err := creativecontent.ReadDisplayBatch(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		res := batch[id]
		v := modelView{ContentID: res.Revision.ContentID}
		if p, ok := completeModel(res.Revision); res.Err == nil && ok {
			g := *p.Generator
			g.Code = ""
			manifest := p.Manifest
			v.Usable, v.Manifest, v.Generator = true, &manifest, &g
			if p.NameHint != "" {
				hint := p.NameHint
				v.NameHint = &hint
			}
		}
		out[id] = v
	}
	return out, nil
}

// validateModelRef is the stored-snapshot check of a model instance's model.
func validateModelRef(m *ModelRef) error {
	if m == nil {
		return errors.New("缺少 model")
	}
	if m.Ref != "" || !revisionIDPattern.MatchString(m.ContentRevisionID) {
		return errors.New("model 须为合法的 content_revision_id")
	}
	return nil
}
