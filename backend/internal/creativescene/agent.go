package creativescene

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	// maxSummaryLines bounds a change summary; the rest is counted.
	maxSummaryLines = 30
	// maxSummaryRows bounds the history one summary reads.
	maxSummaryRows = 2000
)

// ChangeSummary is what others changed in a scene after a revision, in words
// the model reads: who changed which objects, what was added or deleted, and
// what was restored. Revisions the assistant reads are plain integers; the
// API's decimal strings are for browsers, and a model copies what it sees.
type ChangeSummary struct {
	DocumentID    string   `json:"document_id"`
	Title         string   `json:"title"`
	SinceRevision int64    `json:"since_revision"`
	Revision      int64    `json:"revision"`
	Lines         []string `json:"lines"`
	More          int      `json:"more,omitempty"`
}

// ChangesSinceInTx summarizes what others changed since the conversation's
// runs last wrote the scene. It is nil when they never wrote it.
func ChangesSinceInTx(ctx context.Context, tx store.TxAccountScope, documentID string, conversationRuns []string) (*ChangeSummary, error) {
	last, err := creativedocument.LastRunRevision(ctx, tx, documentID, conversationRuns)
	if err != nil || last == 0 {
		return nil, err
	}
	s, err := changesAfter(ctx, tx, func(revision int64) (creativedocument.Current, error) {
		return creativedocument.ReadAtInTx(ctx, tx, documentID, TypeKey, revision)
	}, documentID, last, conversationRuns)
	return &s, err
}

// ChangesAfterInTx summarizes what others than the run changed after base:
// the gap between a stale baseline and now, which a conflict reports.
func ChangesAfterInTx(ctx context.Context, tx store.TxAccountScope, documentID string, base int64, runID string) (ChangeSummary, error) {
	return changesAfter(ctx, tx, func(revision int64) (creativedocument.Current, error) {
		return creativedocument.ReadAtInTx(ctx, tx, documentID, TypeKey, revision)
	}, documentID, base, []string{runID})
}

func changesAfter(ctx context.Context, pages creativedocument.PageReader, at func(int64) (creativedocument.Current, error), documentID string, base int64, excluded []string) (ChangeSummary, error) {
	head, err := at(0)
	if err != nil {
		return ChangeSummary{}, err
	}
	out := ChangeSummary{DocumentID: documentID, Title: head.Title, SinceRevision: base, Revision: head.Revision, Lines: []string{}}
	if base >= head.Revision {
		return out, nil
	}
	history, err := creativedocument.History(ctx, pages, documentID, base, maxSummaryRows)
	if err != nil {
		return ChangeSummary{}, err
	}
	var rows []creativedocument.HistoryRow
	for _, row := range history {
		if row.RunID == "" || !slices.Contains(excluded, row.RunID) {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		return out, nil
	}
	old, err := at(base)
	if err != nil {
		return ChangeSummary{}, err
	}
	before, err := decodeScene(old.Content.Payload)
	if err != nil {
		return ChangeSummary{}, err
	}
	now, err := decodeScene(head.Content.Payload)
	if err != nil {
		return ChangeSummary{}, err
	}
	lines := summarizeChanges(rows, before, now)
	if len(history) == maxSummaryRows && history[len(history)-1].Revision < head.Revision {
		lines = append(lines, fmt.Sprintf("第 %d 版之后还有改动未列出", history[len(history)-1].Revision))
	}
	if len(lines) > maxSummaryLines {
		out.More = len(lines) - maxSummaryLines
		lines = lines[:maxSummaryLines]
	}
	out.Lines = lines
	return out, nil
}

// Prompt words the summary for a run's input: which scene, since when, and
// what changed. It is empty when nothing did.
func (s ChangeSummary) Prompt() string {
	if len(s.Lines) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "场景「%s」(%s)在本会话上次写入（第 %d 版）之后被改过，现在是第 %d 版：", s.Title, s.DocumentID, s.SinceRevision, s.Revision)
	for _, l := range s.Lines {
		b.WriteString("\n- ")
		b.WriteString(l)
	}
	if s.More > 0 {
		fmt.Fprintf(&b, "\n另有 %d 处改动未列出。", s.More)
	}
	b.WriteString("\n改这个场景前先用 read_scene 读当前状态。")
	return b.String()
}

// EditorContext is the scene open in the photographer's editor when they
// asked, and the objects they had selected there that are still in it.
type EditorContext struct {
	DocumentID string   `json:"document_id"`
	Title      string   `json:"title"`
	Revision   int64    `json:"revision"`
	Selected   []string `json:"selected_object_ids"`
	// Objects are the selected objects with the names they had when asked,
	// in Selected's order, for the photographer's message to show.
	Objects []EditorObject `json:"-"`
	Prompt  string         `json:"-"`
}

// EditorObject is one selected object as the editor showed it.
type EditorObject struct {
	ID   string
	Name string
}

// EditorContextInTx reads the scene being edited and words it for a run's
// input. Selected objects no longer in the scene are left out; a scene in the
// trash is not found.
func EditorContextInTx(ctx context.Context, tx store.TxAccountScope, documentID string, selected []string) (EditorContext, error) {
	cur, err := creativedocument.ReadCurrentInTx(ctx, tx, documentID, TypeKey)
	if err != nil {
		return EditorContext{}, err
	}
	if cur.Document.TrashedAt != nil {
		return EditorContext{}, creativedocument.ErrNotFound
	}
	scene, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return EditorContext{}, err
	}
	idx := indexObjects(scene.Objects)
	out := EditorContext{DocumentID: documentID, Title: cur.Title, Revision: cur.Revision, Selected: []string{}}
	var names []string
	for _, id := range selected {
		i, ok := idx[id]
		if !ok || slices.Contains(out.Selected, id) {
			continue
		}
		out.Selected = append(out.Selected, id)
		out.Objects = append(out.Objects, EditorObject{ID: id, Name: scene.Objects[i].Name})
		if name := scene.Objects[i].Name; name != "" {
			names = append(names, fmt.Sprintf("「%s」(%s)", name, id))
		} else {
			names = append(names, id)
		}
	}
	out.Prompt = fmt.Sprintf("创作者正在编辑场景「%s」(%s，第 %d 版)，", out.Title, documentID, cur.Revision)
	if len(names) == 0 {
		out.Prompt += "没有选中物件。"
	} else {
		out.Prompt += "选中了：" + strings.Join(names, "、") + "。说「这个」「它们」时指的是选中的物件。"
	}
	return out, nil
}

var actorLabels = map[string]string{"photographer": "创作者", "agent": "另一次助手对话", "system": "系统"}

func actorLabel(kind string) string {
	if l, ok := actorLabels[kind]; ok {
		return l
	}
	return kind
}

// summarizeChanges words other writers' revisions over before → now, in the
// order they happened: restores one line each, and ordinary edits one line per
// writer and object (added, deleted, or which parts changed).
func summarizeChanges(rows []creativedocument.HistoryRow, before, now Scene) []string {
	beforeIdx, nowIdx := indexObjects(before.Objects), indexObjects(now.Objects)
	label := func(id string) string {
		name := ""
		if i, ok := nowIdx[id]; ok {
			name = now.Objects[i].Name
		} else if i, ok := beforeIdx[id]; ok {
			name = before.Objects[i].Name
		}
		if name == "" {
			return id
		}
		return fmt.Sprintf("「%s」(%s)", name, id)
	}
	// reparentedTo words a parent that differs between before and now. Only
	// edits that changed placement say it: a keep-world reparent always
	// does, and an object's mere order is not news.
	reparentedTo := func(id string) string {
		bi, ok1 := beforeIdx[id]
		ni, ok2 := nowIdx[id]
		if !ok1 || !ok2 {
			return ""
		}
		was, is := before.Objects[bi].ParentID, now.Objects[ni].ParentID
		switch {
		case is == nil && was == nil, is != nil && was != nil && *is == *was:
			return ""
		case is == nil:
			return "，并改挂到根"
		}
		return "，并改挂到" + label(*is)
	}
	type edit struct {
		at                 int64
		actor, target      string
		object             bool
		placement, content bool
		added, removed     bool
	}
	var order []string
	edits := map[string]*edit{}
	type line struct {
		at   int64
		text string
	}
	var restores []line
	for _, row := range rows {
		actor := actorLabel(row.ActorKind)
		switch {
		case row.WholeRestore():
			restores = append(restores, line{row.Revision, fmt.Sprintf("%s把整个场景恢复到第 %d 版", actor, row.RestoredFrom)})
			continue
		case row.RestoredFrom > 0:
			var names []string
			for _, target := range restoredTargets(append(slices.Clone(row.Changed), row.Removed...)) {
				if strings.HasPrefix(target, "sobj_") {
					names = append(names, label(target))
				} else {
					names = append(names, unitLabels[target])
				}
			}
			restores = append(restores, line{row.Revision, fmt.Sprintf("%s把%s恢复到第 %d 版", actor, strings.Join(names, "、"), row.RestoredFrom)})
			continue
		}
		note := func(target string, object bool) *edit {
			key := actor + "\x00" + target
			e := edits[key]
			if e == nil {
				e = &edit{at: row.Revision, actor: actor, target: target, object: object}
				edits[key] = e
				order = append(order, key)
			}
			return e
		}
		for _, u := range row.Changed {
			if id, isContent, ok := objectOfUnit(u); ok {
				e := note(id, true)
				_, existed := beforeIdx[id]
				switch {
				case !existed && !isContent:
					e.added = true
				case isContent:
					e.content = true
				default:
					e.placement = true
				}
			} else if u != unitObjects {
				note(u, false)
			}
		}
		for _, u := range row.Removed {
			if id, _, ok := objectOfUnit(u); ok {
				note(id, true).removed = true
			}
		}
	}
	for _, key := range order {
		e := edits[key]
		var text string
		_, inNow := nowIdx[e.target]
		switch {
		case !e.object:
			text = fmt.Sprintf("%s改了%s", e.actor, unitLabels[e.target])
		case e.added && e.removed || e.added && !inNow:
			continue // came and went after the baseline
		case e.added:
			text = fmt.Sprintf("%s新增了%s", e.actor, label(e.target))
		case e.removed:
			text = fmt.Sprintf("%s删除了%s", e.actor, label(e.target))
		default:
			var parts []string
			if e.placement {
				parts = append(parts, "摆放")
			}
			if e.content {
				parts = append(parts, "内容")
			}
			text = fmt.Sprintf("%s改了%s的%s", e.actor, label(e.target), strings.Join(parts, "和"))
			if e.placement {
				text += reparentedTo(e.target)
			}
		}
		restores = append(restores, line{e.at, text})
	}
	sort.SliceStable(restores, func(i, j int) bool { return restores[i].at < restores[j].at })
	out := make([]string, len(restores))
	for i, l := range restores {
		out[i] = l.text
	}
	return out
}

// restoredTargets is the objects and scene-wide units a restore named, once
// each and in a stable order.
func restoredTargets(units []string) []string {
	var out []string
	for _, u := range units {
		target := u
		if id, _, ok := objectOfUnit(u); ok {
			target = id
		} else if u == unitObjects {
			continue
		}
		if !slices.Contains(out, target) {
			out = append(out, target)
		}
	}
	sort.Strings(out)
	return out
}

const (
	// maxReadObjects is how many objects read_scene details; past it, unless
	// object_ids narrows the read, objects come in brief.
	maxReadObjects   = 300
	maxReadObjectIDs = 200
	maxReadCode      = 3
	// maxConflictEntries bounds a conflict result; the rest is counted.
	maxConflictEntries = 50
)

// ReadInput is read_scene's arguments with the conversation's runs, whose
// writes are the conversation's own, and the bytes the result may take.
type ReadInput struct {
	DocumentID       string
	ObjectIDs        []string
	IncludeCode      []string
	ConversationRuns []string
	MaxBytes         int
}

// SceneRead is what read_scene returns. Revision is the baseline the next
// write may be based on.
type SceneRead struct {
	DocumentID            string            `json:"document_id"`
	Title                 string            `json:"title"`
	Revision              int64             `json:"revision"`
	Environment           Environment       `json:"environment"`
	Render                Render            `json:"render"`
	ObjectCount           int               `json:"object_count"`
	Brief                 bool              `json:"brief,omitempty"`
	Objects               []ObjectView      `json:"objects"`
	OmittedObjects        int               `json:"omitted_objects,omitempty"`
	Missing               []string          `json:"missing_object_ids,omitempty"`
	Code                  map[string]string `json:"code,omitempty"`
	CodeOmitted           []string          `json:"code_omitted,omitempty"`
	ChangesSinceLastWrite *ChangeSummary    `json:"changes_since_last_write"`
	Note                  string            `json:"note,omitempty"`
}

// ObjectView is one object as the assistant reads it. A brief listing carries
// only the id, name, kind, parent and position.
type ObjectView struct {
	ID            string                  `json:"id"`
	Name          string                  `json:"name"`
	Kind          string                  `json:"kind"`
	ParentID      *string                 `json:"parent_id"`
	Position      []float64               `json:"position,omitempty"`
	Transform     *Transform              `json:"transform,omitempty"`
	Visible       *bool                   `json:"visible,omitempty"`
	Locked        *bool                   `json:"locked,omitempty"`
	Builtin       string                  `json:"builtin,omitempty"`
	Params        map[string]any          `json:"params,omitempty"`
	Model         *ModelView              `json:"model,omitempty"`
	MaterialSlots map[string]MaterialSlot `json:"material_slots,omitempty"`
	Light         *Light                  `json:"light,omitempty"`
	Camera        *Camera                 `json:"camera,omitempty"`
}

// ModelView is a placed model: its revision and what was baked, never its
// code (read_scene's include_code asks for that).
type ModelView struct {
	ContentRevisionID string               `json:"content_revision_id"`
	Usable            bool                 `json:"usable"`
	NameHint          string               `json:"name_hint,omitempty"`
	Params            map[string]any       `json:"params,omitempty"`
	ParamsSchema      map[string]paramSpec `json:"params_schema,omitempty"`
	BoundsM           *creativeglb.Bounds  `json:"bounds_m,omitempty"`
	Triangles         int                  `json:"triangles,omitempty"`
	Slots             []creativeglb.Slot   `json:"slots,omitempty"`
}

// placedModel is a model revision as a write transaction reads it.
type placedModel struct {
	contentID string
	payload   ModelPayload
	usable    bool
}

func placedModelsInTx(ctx context.Context, tx store.TxAccountScope, ids []string) (map[string]placedModel, error) {
	out := make(map[string]placedModel, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	batch, err := creativecontent.ReadDisplayBatchInTx(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		res := batch[id]
		m := placedModel{contentID: res.Revision.ContentID}
		if p, ok := completeModel(res.Revision); res.Err == nil && ok {
			m.payload, m.usable = p, true
		}
		out[id] = m
	}
	return out, nil
}

func objectView(o Object, models map[string]placedModel) ObjectView {
	v := ObjectView{ID: o.ID, Name: o.Name, Kind: o.Kind, ParentID: o.ParentID, MaterialSlots: o.MaterialSlots, Light: o.Light, Camera: o.Camera}
	t, visible, locked := o.Transform, o.Visible, o.Locked
	v.Transform, v.Visible, v.Locked = &t, &visible, &locked
	if o.Primitive != nil {
		v.Builtin, v.Params = o.Primitive.Builtin, o.Primitive.Params
	}
	if o.Model != nil {
		mv := &ModelView{ContentRevisionID: o.Model.ContentRevisionID}
		if m, ok := models[o.Model.ContentRevisionID]; ok && m.usable {
			bounds := m.payload.Manifest.Bounds
			mv.Usable, mv.NameHint, mv.BoundsM = true, m.payload.NameHint, &bounds
			mv.Params, mv.ParamsSchema = m.payload.Generator.Params, m.payload.Generator.ParamsSchema
			mv.Triangles, mv.Slots = m.payload.Manifest.Triangles, m.payload.Manifest.Slots
		}
		v.Model = mv
	}
	return v
}

// AgentReadInTx is read_scene: the scene in full, or in brief past 300
// objects unless object_ids narrows it; the code of up to three placed models;
// and what others changed since the conversation last wrote it. A result past
// MaxBytes drops code that does not fit, then details, then trailing objects,
// and says so.
func AgentReadInTx(ctx context.Context, tx store.TxAccountScope, in ReadInput) (SceneRead, error) {
	if len(in.ObjectIDs) > maxReadObjectIDs {
		return SceneRead{}, invalid("object_ids 最多 %d 个", maxReadObjectIDs)
	}
	if len(in.IncludeCode) > maxReadCode {
		return SceneRead{}, invalid("include_code 最多 %d 个模型修订", maxReadCode)
	}
	cur, err := creativedocument.ReadCurrentInTx(ctx, tx, in.DocumentID, TypeKey)
	if err != nil {
		return SceneRead{}, err
	}
	if cur.Document.TrashedAt != nil {
		return SceneRead{}, creativedocument.ErrTrashed
	}
	scene, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return SceneRead{}, err
	}
	out := SceneRead{DocumentID: in.DocumentID, Title: cur.Title, Revision: cur.Revision,
		Environment: scene.Environment, Render: scene.Render, ObjectCount: len(scene.Objects), Objects: []ObjectView{}}
	listed := scene.Objects
	if len(in.ObjectIDs) > 0 {
		idx := indexObjects(scene.Objects)
		listed = nil
		for _, id := range in.ObjectIDs {
			if i, ok := idx[id]; ok {
				listed = append(listed, scene.Objects[i])
			} else if !slices.Contains(out.Missing, id) {
				out.Missing = append(out.Missing, id)
			}
		}
	} else if len(scene.Objects) > maxReadObjects {
		out.Brief = true
		for _, o := range scene.Objects {
			out.Objects = append(out.Objects, ObjectView{ID: o.ID, Name: o.Name, Kind: o.Kind, ParentID: o.ParentID, Position: o.Transform.Position})
		}
		listed = nil
	}
	ids := modelRefs(listed)
	for _, id := range in.IncludeCode {
		if !slices.Contains(cur.Refs, id) {
			return SceneRead{}, invalid("模型修订 %s 不在这个场景里", id)
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	models, err := placedModelsInTx(ctx, tx, ids)
	if err != nil {
		return SceneRead{}, err
	}
	for _, o := range listed {
		out.Objects = append(out.Objects, objectView(o, models))
	}
	code := map[string]string{}
	for _, id := range in.IncludeCode {
		m := models[id]
		if !m.usable {
			return SceneRead{}, modelUnavailable(id)
		}
		code[id] = m.payload.Generator.Code
	}
	if out.ChangesSinceLastWrite, err = ChangesSinceInTx(ctx, tx, in.DocumentID, in.ConversationRuns); err != nil {
		return SceneRead{}, err
	}
	if in.MaxBytes > 0 {
		err = out.fit(in.MaxBytes, code, in.IncludeCode, scene.Objects, len(in.ObjectIDs) > 0)
	} else if len(code) > 0 {
		out.Code = code
	}
	return out, err
}

// fit keeps a read within budget bytes: requested code first, in order, as
// long as it fits; then the objects in detail, or in brief when they do not
// fit and the read was not narrowed; then as many objects as fit.
func (r *SceneRead) fit(budget int, code map[string]string, order []string, all []Object, narrowed bool) error {
	size := func(v any) (int, error) {
		raw, err := json.Marshal(v)
		return len(raw), err
	}
	objects := r.Objects
	r.Objects = []ObjectView{}
	used, err := size(r)
	if err != nil {
		return err
	}
	used += 320 // room for the brief flag, the omission count and the notes
	for _, id := range order {
		n, err := size(map[string]string{id: code[id]})
		if err != nil {
			return err
		}
		if used+n > budget {
			r.CodeOmitted = append(r.CodeOmitted, id)
			continue
		}
		if r.Code == nil {
			r.Code = map[string]string{}
		}
		r.Code[id] = code[id]
		used += n
	}
	if len(r.CodeOmitted) > 0 {
		r.Note = "部分模型代码太长没有附上，一次只读一个模型的代码。"
	}
	take := func(list []ObjectView) ([]ObjectView, error) {
		total := used
		for i, o := range list {
			n, err := size(o)
			if err != nil {
				return nil, err
			}
			if total+n+1 > budget {
				return list[:i], nil
			}
			total += n + 1
		}
		return list, nil
	}
	kept, err := take(objects)
	if err != nil {
		return err
	}
	if len(kept) < len(objects) && !narrowed && !r.Brief {
		brief := make([]ObjectView, 0, len(all))
		for _, o := range all {
			brief = append(brief, ObjectView{ID: o.ID, Name: o.Name, Kind: o.Kind, ParentID: o.ParentID, Position: o.Transform.Position})
		}
		r.Brief, objects = true, brief
		if kept, err = take(objects); err != nil {
			return err
		}
	}
	r.Objects = kept
	if r.OmittedObjects = len(objects) - len(kept); r.OmittedObjects > 0 {
		r.Note = strings.TrimSpace(r.Note + " 结果太长，后面的物件没有列出；用 object_ids 分批读取。")
	}
	return nil
}

// WriteConflict is an assistant write refused because someone else changed
// what it touches after its baseline. Revision is the new baseline; the
// summary says what happened in between.
type WriteConflict struct {
	Status       string          `json:"status"`
	Message      string          `json:"message"`
	Conflicts    []ConflictEntry `json:"conflicts"`
	More         int             `json:"more,omitempty"`
	ChangesSince ChangeSummary   `json:"changes_since"`
	Revision     int64           `json:"revision"`
	RetryJobID   string          `json:"retry_job_id,omitempty"`
}

// ConflictEntry is one unit that changed: an object's placement or content,
// or a scene-wide unit, with who changed it last and what it is now (null
// when the object is gone).
type ConflictEntry struct {
	ObjectID  string `json:"object_id,omitempty"`
	Unit      string `json:"unit,omitempty"`
	Name      string `json:"name"`
	Part      string `json:"part"`
	ChangedBy string `json:"changed_by"`
	Current   any    `json:"current"`
}

// othersChanges is who other than the run last changed each unit after a
// baseline; a whole restore counts for every unit.
type othersChanges struct {
	units map[string]creativedocument.HistoryRow
	whole *creativedocument.HistoryRow
}

func (o othersChanges) by(u string) (string, bool) {
	row, ok := o.units[u]
	switch {
	case o.whole != nil && (!ok || o.whole.Revision > row.Revision):
		return o.whole.ActorKind, true
	case ok:
		return row.ActorKind, true
	}
	return "", false
}

func othersSince(ctx context.Context, pages creativedocument.PageReader, documentID string, base int64, runID string) (othersChanges, error) {
	rows, err := creativedocument.History(ctx, pages, documentID, base, maxHistoryRows+1)
	if err != nil {
		return othersChanges{}, err
	}
	if len(rows) > maxHistoryRows {
		return othersChanges{}, invalid("第 %d 版之后的修订太多，请先用 read_scene 读取当前场景，按新的 revision 写入", base)
	}
	out := othersChanges{units: map[string]creativedocument.HistoryRow{}}
	for i, row := range rows {
		if row.RunID == runID && runID != "" {
			continue
		}
		if row.WholeRestore() {
			out.whole = &rows[i]
			continue
		}
		for _, u := range append(slices.Clone(row.Changed), row.Removed...) {
			out.units[u] = row
		}
	}
	return out, nil
}

// checkBaseline is the conflict a write of touched units based on base meets:
// units someone else than the run changed after base. It is nil when there is
// none. Writes of the run itself never conflict, so the baseline need not
// move past them.
func checkBaseline(ctx context.Context, tx store.TxAccountScope, cur creativedocument.Current, base int64, runID string, touched []string) (*WriteConflict, error) {
	if base < 1 || base > cur.Revision {
		return nil, invalid("based_on_revision 须在 1 到当前修订 %d 之间", cur.Revision)
	}
	others, err := othersSince(ctx, tx, cur.Document.ID, base, runID)
	if err != nil {
		return nil, err
	}
	var hits []string
	for _, u := range touched {
		if _, ok := others.by(u); ok && !slices.Contains(hits, u) {
			hits = append(hits, u)
		}
	}
	if len(hits) == 0 {
		return nil, nil
	}
	c, err := describeConflict(ctx, tx, cur, base, runID, hits, others)
	return &c, err
}

var partLabels = map[bool]string{false: "摆放", true: "内容"}

// describeConflict words a conflict on units for the model: each unit with
// who changed it and its current state, the summary since base and the new
// baseline.
func describeConflict(ctx context.Context, tx store.TxAccountScope, cur creativedocument.Current, base int64, runID string, units []string, others othersChanges) (WriteConflict, error) {
	now, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return WriteConflict{}, err
	}
	old, err := creativedocument.ReadAtInTx(ctx, tx, cur.Document.ID, TypeKey, base)
	if err != nil {
		return WriteConflict{}, err
	}
	before, err := decodeScene(old.Content.Payload)
	if err != nil {
		return WriteConflict{}, err
	}
	nowIdx, beforeIdx := indexObjects(now.Objects), indexObjects(before.Objects)
	models, err := placedModelsInTx(ctx, tx, modelRefs(now.Objects))
	if err != nil {
		return WriteConflict{}, err
	}
	out := WriteConflict{Status: "conflict", Conflicts: []ConflictEntry{}, Revision: cur.Revision}
	sort.Strings(units)
	for _, u := range units {
		e := ConflictEntry{ChangedBy: "本次运行"}
		if actor, ok := others.by(u); ok {
			e.ChangedBy = actorLabel(actor)
		}
		if id, content, ok := objectOfUnit(u); ok {
			e.ObjectID, e.Part = id, partLabels[content]
			if i, ok := nowIdx[id]; ok {
				e.Name, e.Current = now.Objects[i].Name, objectView(now.Objects[i], models)
			} else if i, ok := beforeIdx[id]; ok {
				e.Name = before.Objects[i].Name
			}
		} else {
			e.Unit, e.Name, e.Part = u, unitLabels[u], unitLabels[u]
			switch u {
			case unitEnvironment:
				e.Current = now.Environment
			case unitRender:
				e.Current = now.Render
			case creativedocument.MetaUnit:
				e.Current = cur.Title
			}
		}
		if len(out.Conflicts) == maxConflictEntries {
			out.More++
			continue
		}
		out.Conflicts = append(out.Conflicts, e)
	}
	if out.ChangesSince, err = ChangesAfterInTx(ctx, tx, cur.Document.ID, base, runID); err != nil {
		return WriteConflict{}, err
	}
	out.Message = fmt.Sprintf("第 %d 版之后有人改了这些内容，这次没有写入。按它们现在的状态重新决定；再写入时 based_on_revision 用 %d。", base, cur.Revision)
	return out, nil
}

// EditInput is edit_scene's arguments with the run writing and the
// conversation's runs (revert_run's "previous").
type EditInput struct {
	DocumentID       string
	RunID            string
	BasedOn          int64
	Operations       []json.RawMessage
	ConversationRuns []string
}

// EditResult is a written edit. WrittenRevision only reports what was
// written: it is never a baseline, since others may have written before it.
type EditResult struct {
	WrittenRevision int64    `json:"written_revision"`
	ChangedObjects  []string `json:"changed_objects"`
	ChangedCount    int      `json:"changed_count"`
}

// maxListedEntries bounds the lists an edit or revert result carries; counts
// say how many there were.
const maxListedEntries = 200

// RevertResult is a taken-back run: what was put back and what was left
// because someone else changed it since.
type RevertResult struct {
	WrittenRevision *int64          `json:"written_revision"`
	RunID           string          `json:"run_id"`
	Restored        []RevertedEntry `json:"restored"`
	RestoredCount   int             `json:"restored_count"`
	Skipped         []RevertedEntry `json:"skipped"`
	SkippedCount    int             `json:"skipped_count"`
}

type RevertedEntry struct {
	ObjectID string `json:"object_id,omitempty"`
	Unit     string `json:"unit,omitempty"`
	Name     string `json:"name"`
	Change   string `json:"change,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// opsNotForTheAssistant are scene command operations edit_scene leaves out:
// models come from write_model, and taking changes back is revert_run.
var opsNotForTheAssistant = map[string]string{
	"commit_model":  "模型用 write_model 写入",
	"restore":       "撤回改动用 revert_run",
	"restore_units": "撤回改动用 revert_run",
	"rename":        "不能改场景标题",
}

// opsOnTheObjectList all change the objects unit, which a revision changes
// once: the scene command would refuse a second one only after the batch has
// been resolved, with a reason that does not say what to do.
var opsOnTheObjectList = []string{"add_objects", "remove_objects", "reparent_objects"}

// AgentEditInTx is edit_scene: scene operations checked against the
// baseline and written as the run, or a single revert_run. It returns an
// EditResult, a RevertResult or a *WriteConflict.
func AgentEditInTx(ctx context.Context, tx store.TxAccountScope, in EditInput) (any, error) {
	if in.RunID == "" || len(in.Operations) == 0 {
		return nil, creativeops.ErrValidation
	}
	kinds := make([]string, len(in.Operations))
	onTheList := 0
	for i, raw := range in.Operations {
		var head struct {
			Op          string `json:"op"`
			Environment *struct {
				Sky struct {
					Kind string `json:"kind"`
				} `json:"sky"`
			} `json:"environment"`
		}
		if json.Unmarshal(raw, &head) != nil || head.Op == "" {
			return nil, invalid("第 %d 个操作缺少 op", i+1)
		}
		if why, ok := opsNotForTheAssistant[head.Op]; ok {
			return nil, invalid("第 %d 个操作 %s 不可用：%s", i+1, head.Op, why)
		}
		if head.Op == "set_environment" && head.Environment != nil && head.Environment.Sky.Kind == "hdri" {
			return nil, invalid("第 %d 个操作：暂无内置 HDRI，请用程序化天空（procedural）或纯色（color）", i+1)
		}
		if slices.Contains(opsOnTheObjectList, head.Op) {
			onTheList++
		}
		kinds[i] = head.Op
	}
	if slices.Contains(kinds, "revert_run") {
		if len(kinds) != 1 {
			return nil, invalid("revert_run 必须单独提交")
		}
		return agentRevertInTx(ctx, tx, in)
	}
	if onTheList > 1 {
		return nil, invalid("add_objects、remove_objects 和 reparent_objects 都改物件列表，一次修改里只能有其中一个：同类的条目合进一个操作（新分组和挂进它的物件写在同一个 reparent_objects 里，新物件在 add_objects 里直接写 parent_id），不同类的分次提交")
	}
	cur, err := creativedocument.LockCurrentInTx(ctx, tx, in.DocumentID, TypeKey)
	if err != nil {
		return nil, err
	}
	if cur.Document.TrashedAt != nil {
		return nil, creativedocument.ErrTrashed
	}
	scene, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return nil, err
	}
	if in.Operations, err = resolveSlots(ctx, tx, in.Operations, scene); err != nil {
		return nil, err
	}
	if in.Operations, err = resolveReparent(in.Operations, scene); err != nil {
		return nil, err
	}
	if in.Operations, err = resolveLookAt(in.Operations, scene); err != nil {
		return nil, err
	}
	actor := creativedocument.Actor{Kind: "agent", RunID: &in.RunID}
	touched, err := sceneType{}.Touched(ctx, tx, cur, actor, in.Operations)
	if err != nil {
		return nil, err
	}
	conflict, err := checkBaseline(ctx, tx, cur, in.BasedOn, in.RunID, touched)
	if err != nil || conflict != nil {
		return conflict, err
	}
	expected := make(map[string]*creativeops.Revision, len(touched))
	for _, u := range touched {
		expected[u] = currentRev(cur.UnitRevs, u)
	}
	payload, err := json.Marshal(map[string]any{"document_id": in.DocumentID, "expected": expected, "operations": in.Operations})
	if err != nil {
		return nil, err
	}
	written, err := creativedocument.CommandInTx(ctx, tx, actor, payload)
	if err != nil {
		return nil, err
	}
	changed := changedObjects(append(slices.Clone(written.ChangedUnits), written.RemovedUnits...))
	return EditResult{WrittenRevision: int64(written.Revision), ChangedObjects: changed[:min(len(changed), maxListedEntries)], ChangedCount: len(changed)}, nil
}

func changedObjects(units []string) []string {
	out := []string{}
	for _, u := range units {
		if id, _, ok := objectOfUnit(u); ok && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// revertRunOp is edit_scene's revert_run: a run of this conversation
// ("previous" for the last one before this that wrote the scene), and
// optionally only some of the objects it changed.
type revertRunOp struct {
	Op        string   `json:"op"`
	RunID     string   `json:"run_id"`
	ObjectIDs []string `json:"object_ids,omitempty"`
}

var blockedReasons = map[string]string{
	BlockedByOthers:       "之后被%s改过",
	BlockedByWholeRestore: "之后%s把整个场景恢复过",
	BlockedHierarchy:      "层级调整不能单独恢复",
}

// agentRevertInTx puts back what a run changed, the way the photographer's
// 撤销本轮 does: entries someone else changed since are skipped and said so,
// never overwritten.
func agentRevertInTx(ctx context.Context, tx store.TxAccountScope, in EditInput) (RevertResult, error) {
	var op revertRunOp
	if err := decodeInput(in.Operations[0], &op); err != nil {
		return RevertResult{}, err
	}
	if len(op.ObjectIDs) > maxBatchItems {
		return RevertResult{}, invalid("object_ids 最多 %d 个", maxBatchItems)
	}
	cur, err := creativedocument.LockCurrentInTx(ctx, tx, in.DocumentID, TypeKey)
	if err != nil {
		return RevertResult{}, err
	}
	if cur.Document.TrashedAt != nil {
		return RevertResult{}, creativedocument.ErrTrashed
	}
	runID := op.RunID
	switch {
	case runID == "previous":
		var earlier []string
		for _, r := range in.ConversationRuns {
			if r != in.RunID {
				earlier = append(earlier, r)
			}
		}
		if runID, err = creativedocument.LastRunWriter(ctx, tx, in.DocumentID, earlier); err != nil {
			return RevertResult{}, err
		}
		if runID == "" {
			return RevertResult{}, invalid("本会话之前的运行没有改过这个场景")
		}
	case runID != in.RunID && !slices.Contains(in.ConversationRuns, runID):
		return RevertResult{}, invalid("run_id 须是本会话的运行，或写 previous")
	}
	selected := func(t ChangeTarget) bool {
		return len(op.ObjectIDs) == 0 || t.Kind == "object" && slices.Contains(op.ObjectIDs, t.ObjectID)
	}
	set, err := runChangesFor(ctx, tx, func(revision int64) (creativedocument.Current, error) {
		return creativedocument.ReadAtInTx(ctx, tx, in.DocumentID, TypeKey, revision)
	}, in.DocumentID, runID, selected)
	if err != nil {
		return RevertResult{}, err
	}
	out := RevertResult{RunID: runID, Restored: []RevertedEntry{}, Skipped: []RevertedEntry{}}
	seen := map[string]bool{}
	for _, e := range set.Entries {
		if !selected(e.Target) {
			continue
		}
		seen[e.Target.ObjectID] = true
		entry := RevertedEntry{ObjectID: e.Target.ObjectID, Unit: e.Target.Unit, Name: e.Name, Change: e.Change}
		if e.Blocked != nil {
			reason := blockedReasons[e.Blocked.Reason]
			if strings.Contains(reason, "%s") {
				reason = fmt.Sprintf(reason, actorLabel(e.Blocked.ActorKind))
			}
			entry.Reason = reason
			out.Skipped = append(out.Skipped, entry)
			continue
		}
		out.Restored = append(out.Restored, entry)
	}
	for _, id := range op.ObjectIDs {
		if !seen[id] {
			seen[id] = true
			out.Skipped = append(out.Skipped, RevertedEntry{ObjectID: id, Name: id, Reason: "这次运行没有改过它"})
		}
	}
	actor := creativedocument.Actor{Kind: "agent", RunID: &in.RunID}
	revs := cur.UnitRevs
	for _, batch := range set.UndoAll {
		// Batches take disjoint units but may share the object list, which an
		// earlier batch moves on; the document is locked, so now is what counts.
		expected := make(map[string]*creativeops.Revision, len(batch.Expected))
		for u := range batch.Expected {
			expected[u] = currentRev(revs, u)
		}
		payload, err := json.Marshal(map[string]any{"document_id": in.DocumentID, "expected": expected,
			"operations": []any{map[string]any{"op": "restore_units", "from_revision": batch.FromRevision, "units": batch.Units}}})
		if err != nil {
			return RevertResult{}, err
		}
		written, err := creativedocument.CommandInTx(ctx, tx, actor, payload)
		if err != nil {
			return RevertResult{}, err
		}
		r := int64(written.Revision)
		out.WrittenRevision = &r
		head, err := creativedocument.ReadHeadInTx(ctx, tx, in.DocumentID, TypeKey)
		if err != nil {
			return RevertResult{}, err
		}
		revs = head.UnitRevs
	}
	out.RestoredCount, out.SkippedCount = len(out.Restored), len(out.Skipped)
	out.Restored, out.Skipped = out.Restored[:min(len(out.Restored), maxListedEntries)], out.Skipped[:min(len(out.Skipped), maxListedEntries)]
	return out, nil
}

// JobConflictInTx words a bake that failed on a conflict for the run that
// asked for it, from the baseline its call was based on: what changed, the
// new baseline, and the job to retry with.
func JobConflictInTx(ctx context.Context, tx store.TxAccountScope, job Job, failure JobFailure, base int64) (WriteConflict, error) {
	if job.RunID == nil {
		return WriteConflict{}, creativeops.ErrValidation
	}
	cur, err := creativedocument.ReadCurrentInTx(ctx, tx, job.DocumentID, TypeKey)
	if err != nil {
		return WriteConflict{}, err
	}
	base = min(max(base, 1), cur.Revision)
	others, err := othersSince(ctx, tx, job.DocumentID, base, *job.RunID)
	if err != nil {
		return WriteConflict{}, err
	}
	units := make([]string, 0, len(failure.Conflicts))
	for _, c := range failure.Conflicts {
		units = append(units, c.Unit)
	}
	out, err := describeConflict(ctx, tx, cur, base, *job.RunID, units, others)
	if err != nil {
		return WriteConflict{}, err
	}
	out.RetryJobID = job.ID
	out.Message = fmt.Sprintf("烘焙期间这些内容被改过，模型没有写入。按它们现在的状态决定后，用 retry_job_id 重试即可写入，不用重新烘焙；based_on_revision 用 %d。", cur.Revision)
	return out, nil
}

// BakedModel is what write_model reports of a written model.
type BakedModel struct {
	SizeM     [3]float64           `json:"size_m"`
	Triangles int                  `json:"triangles"`
	Slots     []creativeglb.Slot   `json:"slots"`
	Parts     []string             `json:"parts"`
	Anchors   []creativeglb.Anchor `json:"anchors"`
}

// BakedModelInTx reads a written model's manifest: its size in metres
// (rounded to millimetres), triangles, material slots, parts and anchors.
func BakedModelInTx(ctx context.Context, tx store.TxAccountScope, revisionID string) (BakedModel, error) {
	models, err := placedModelsInTx(ctx, tx, []string{revisionID})
	if err != nil {
		return BakedModel{}, err
	}
	m := models[revisionID]
	if !m.usable {
		return BakedModel{}, modelUnavailable(revisionID)
	}
	manifest := m.payload.Manifest
	out := BakedModel{Triangles: manifest.Triangles, Slots: manifest.Slots, Parts: manifest.Parts, Anchors: manifest.Anchors}
	for i := range out.SizeM {
		out.SizeM[i] = math.Round((manifest.Bounds.Max[i]-manifest.Bounds.Min[i])*1000) / 1000
	}
	return out, nil
}

// BuiltinCatalog lists the builtin primitives for the assistant: each with
// its parameter names, sorted.
func BuiltinCatalog() string {
	keys := make([]string, 0, len(builtins))
	for k := range builtins {
		if k != "ground@1" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		spec := builtins[k]
		names := make([]string, 0, len(spec.Params))
		for n := range spec.Params {
			names = append(names, n)
		}
		sort.Strings(names)
		parts = append(parts, fmt.Sprintf("%s %s(%s)", k, spec.Label, strings.Join(names, ",")))
	}
	return strings.Join(parts, "；")
}
