package creativedocument

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	maxOperations      = 200
	maxRestoreUnits    = maxUnits // undoing a large delete must fit in one request
	receiptListBudget  = 48 << 10
	opRename           = "rename"
	opRestore          = "restore"
	opRestoreUnits     = "restore_units"
	commandOperationID = "document.command"
)

type commandInput struct {
	DocumentID       string                `json:"document_id"`
	Expected         json.RawMessage       `json:"expected,omitempty"`
	ExpectedRevision *creativeops.Revision `json:"expected_revision,omitempty"`
	Operations       []json.RawMessage     `json:"operations"`
}

type opHead struct {
	Op string `json:"op"`
}

type renameOp struct {
	Op    string `json:"op"`
	Title string `json:"title"`
}

type restoreOp struct {
	Op       string               `json:"op"`
	Revision creativeops.Revision `json:"revision"`
}

type restoreUnitsOp struct {
	Op           string               `json:"op"`
	FromRevision creativeops.Revision `json:"from_revision"`
	Units        []string             `json:"units"`
}

// CommandResult is the bounded receipt of a document command. A unit in
// ChangedUnits now has revision Revision; the caller re-reads when
// ResetRequired is set.
type CommandResult struct {
	Revision       creativeops.Revision `json:"revision"`
	ChangedUnits   []string             `json:"changed_units"`
	AnnotatedUnits []string             `json:"annotated_units"`
	RemovedUnits   []string             `json:"removed_units"`
	ResetRequired  bool                 `json:"reset_required"`
	// Extra holds the type's receipt fields (Changes.Response), serialized at
	// the top level next to the shared fields.
	Extra map[string]json.RawMessage `json:"-"`
}

// sharedReceiptFields are the receipt fields a type's Response cannot reuse.
var sharedReceiptFields = map[string]bool{"revision": true, "changed_units": true, "annotated_units": true, "removed_units": true, "reset_required": true}

type plainResult CommandResult

// MarshalJSON writes the shared fields and, when present, the type's fields at
// the same level. Without Extra the receipt is exactly the shared shape.
func (r CommandResult) MarshalJSON() ([]byte, error) {
	base, err := json.Marshal(plainResult(r))
	if err != nil || len(r.Extra) == 0 {
		return base, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(base, &fields); err != nil {
		return nil, err
	}
	for k, v := range r.Extra {
		if sharedReceiptFields[k] {
			return nil, fmt.Errorf("%w: receipt field %q", ErrTypeContract, k)
		}
		fields[k] = v
	}
	return json.Marshal(fields)
}

// UnmarshalJSON reads a receipt back, keeping the type's fields in Extra.
func (r *CommandResult) UnmarshalJSON(raw []byte) error {
	var plain plainResult
	if err := json.Unmarshal(raw, &plain); err != nil {
		return err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for k := range sharedReceiptFields {
		delete(fields, k)
	}
	*r = CommandResult(plain)
	if len(fields) > 0 {
		r.Extra = fields
	}
	return nil
}

// Command applies one photographer batch; the HTTP handler puts the path's
// document id into the payload so it is part of the idempotency hash.
func Command(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return CommandAs(ctx, scope, Photographer, c)
}

// CommandAs applies a batch for a server-chosen actor. Agent tools pass their
// run guard on c.Guard, as canvas tools do.
func CommandAs(ctx context.Context, scope store.AccountScope, actor Actor, c creativeops.Command) (creativeops.Receipt, error) {
	if actor.Kind == "agent" && actor.RunID == nil || actor.Kind != "agent" && actor.Kind != "photographer" {
		return creativeops.Receipt{}, creativeops.ErrValidation
	}
	var parsed commandInput
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: commandOperationID, Capability: "manual_write", Validate: func(raw json.RawMessage) error {
		var err error
		parsed, err = decodeCommand(raw)
		return err
	}, Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
		result, err := commandInTx(ctx, tx, actor, parsed)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		id := parsed.DocumentID
		return creativeops.Outcome{HTTPStatus: 200, Response: body, ResultKind: "document", ResultID: &id, ResultRevision: &result.Revision}, nil
	}}, c)
}

func decodeCommand(raw json.RawMessage) (commandInput, error) {
	var in commandInput
	if err := creativeops.Decode(raw, &in); err != nil {
		return in, err
	}
	if in.DocumentID == "" {
		return in, creativeops.ErrValidation
	}
	if len(in.Operations) == 0 || len(in.Operations) > maxOperations {
		return in, creativeops.Explain(creativeops.ErrValidation, "一次提交 1 到 %d 个操作", maxOperations)
	}
	return in, nil
}

func decodeExpected(raw json.RawMessage) (map[string]*creativeops.Revision, error) {
	out := map[string]*creativeops.Revision{}
	if len(raw) == 0 {
		return nil, creativeops.Explain(creativeops.ErrValidation, "缺少 expected")
	}
	if err := creativeops.Decode(raw, &out); err != nil {
		return nil, err
	}
	if len(out) > maxUnits {
		return nil, ErrSizeLimit
	}
	for u := range out {
		if !unitName.MatchString(u) {
			return nil, creativeops.Explain(creativeops.ErrValidation, "单元名 %q 不合法", u)
		}
	}
	return out, nil
}

// write is everything a command decided before it is persisted.
type write struct {
	snapshot     Snapshot
	title        string
	changes      Changes
	restoredFrom *int64
	restoredRefs map[string]bool // refs of the revision a restore reads from
	wholeRestore bool
}

func commandInTx(ctx context.Context, tx store.TxAccountScope, actor Actor, in commandInput) (CommandResult, error) {
	head, err := lockHead(ctx, tx, in.DocumentID)
	if err != nil {
		return CommandResult{}, err
	}
	if head.TrashedAt != nil {
		return CommandResult{}, ErrTrashed
	}
	t, ok := Lookup(head.TypeKey)
	if !ok {
		return CommandResult{}, ErrNotFound
	}
	kinds := make([]string, len(in.Operations))
	for i, raw := range in.Operations {
		var h opHead
		if err := json.Unmarshal(raw, &h); err != nil || h.Op == "" {
			return CommandResult{}, creativeops.Explain(creativeops.ErrValidation, "第 %d 个操作缺少 op", i+1)
		}
		kinds[i] = h.Op
	}
	for _, k := range kinds {
		if (k == opRestore || k == opRestoreUnits) && len(kinds) != 1 {
			return CommandResult{}, creativeops.Explain(creativeops.ErrValidation, "%s 必须单独提交", k)
		}
	}
	if kinds[0] == opRestore {
		if in.ExpectedRevision == nil || len(in.Expected) != 0 {
			return CommandResult{}, creativeops.Explain(creativeops.ErrValidation, "整份恢复只带 expected_revision")
		}
	} else if in.ExpectedRevision != nil {
		return CommandResult{}, creativeops.Explain(creativeops.ErrValidation, "只有整份恢复可以带 expected_revision")
	}
	cur, err := loadInTx(ctx, tx, head, 0)
	if err != nil {
		return CommandResult{}, err
	}
	next := head.Revision + 1
	pre := map[string]bool{}
	retain := func(r creativecontent.Revision) error {
		if pre[r.ID] {
			return nil
		}
		pre[r.ID] = true
		return tx.Insert(ctx, "creative_document_content_refs", []string{"document_id", "revision", "content_revision_id"}, head.ID, next, r.ID)
	}

	var w write
	switch kinds[0] {
	case opRestore:
		w, err = planRestore(ctx, tx, t, cur, int64(*in.ExpectedRevision), in.Operations[0], next)
	case opRestoreUnits:
		w, err = planRestoreUnits(ctx, tx, t, cur, in.Expected, in.Operations[0], retain)
	default:
		w, err = planBatch(ctx, tx, t, cur, actor, in.Expected, in.Operations, kinds, retain)
	}
	if err != nil {
		return CommandResult{}, err
	}
	return persist(ctx, tx, t, cur, actor, w, next, pre)
}

func checkExpected(head Document, expected map[string]*creativeops.Revision, touched []string) error {
	seen := map[string]bool{}
	var conflicts []Conflict
	for _, u := range touched {
		if seen[u] {
			return creativeops.Explain(creativeops.ErrValidation, "单元 %s 在一次提交里被改了两次", u)
		}
		seen[u] = true
		want, present := expected[u]
		if !present {
			return creativeops.Explain(creativeops.ErrValidation, "expected 缺少单元 %s", u)
		}
		got, exists := head.UnitRevs[u]
		switch {
		case want == nil && !exists, want != nil && exists && int64(*want) == got:
		default:
			c := Conflict{Unit: u, ExpectedRev: want}
			if exists {
				c.CurrentRev = rev(got)
			}
			conflicts = append(conflicts, c)
		}
	}
	if len(conflicts) > 0 {
		sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Unit < conflicts[j].Unit })
		return &ConflictError{Conflicts: conflicts}
	}
	return nil
}

func planBatch(ctx context.Context, tx store.TxAccountScope, t Type, cur Current, actor Actor, rawExpected json.RawMessage, ops []json.RawMessage, kinds []string, retain Retain) (write, error) {
	expected, err := decodeExpected(rawExpected)
	if err != nil {
		return write{}, err
	}
	w := write{title: cur.Title, snapshot: Snapshot{Payload: cur.Content.Payload, Refs: cur.Refs}}
	var touched []string
	var typeOps []json.RawMessage
	renamed := false
	for i, raw := range ops {
		if kinds[i] != opRename {
			typeOps = append(typeOps, raw)
			continue
		}
		var r renameOp
		if err := creativeops.Decode(raw, &r); err != nil {
			return write{}, err
		}
		title, err := normalizeTitle(r.Title)
		if err != nil {
			return write{}, err
		}
		w.title, renamed = title, true
		touched = append(touched, MetaUnit)
	}
	if len(typeOps) > 0 {
		units, err := t.Touched(ctx, tx, cur, actor, typeOps)
		if err != nil {
			return write{}, err
		}
		for _, u := range units {
			if u == MetaUnit || !unitName.MatchString(u) {
				return write{}, fmt.Errorf("%w: touched unit %q", ErrTypeContract, u)
			}
		}
		touched = append(touched, units...)
	}
	if err := checkExpected(cur.Document, expected, touched); err != nil {
		return write{}, err
	}
	if len(typeOps) > 0 {
		w.snapshot, w.changes, err = t.Apply(ctx, tx, cur, actor, retain, typeOps)
		if err != nil {
			return write{}, err
		}
	}
	if renamed {
		w.changes.Changed = append(w.changes.Changed, MetaUnit)
	}
	return w, checkChanges(cur.Document, touched, w.changes)
}

func planRestore(ctx context.Context, tx store.TxAccountScope, t Type, cur Current, expectedRevision int64, raw json.RawMessage, next int64) (write, error) {
	var op restoreOp
	if err := creativeops.Decode(raw, &op); err != nil {
		return write{}, err
	}
	if expectedRevision != cur.Revision {
		return write{}, &ConflictError{Conflicts: []Conflict{{Unit: "*", ExpectedRev: rev(expectedRevision), CurrentRev: rev(cur.Revision)}}}
	}
	k := int64(op.Revision)
	if k < 1 || k >= cur.Revision {
		return write{}, creativeops.Explain(creativeops.ErrValidation, "只能恢复到第 1 到第 %d 版", cur.Revision-1)
	}
	old, err := loadRestoreSource(ctx, tx, cur.Document, k)
	if err != nil {
		return write{}, err
	}
	snapshot := Snapshot{Payload: old.Content.Payload, Refs: old.Refs}
	if hook, ok := t.(RestoreHook); ok {
		if snapshot, err = hook.AfterRestore(ctx, tx, old, snapshot, next); err != nil {
			return write{}, err
		}
	}
	return write{snapshot: snapshot, title: old.Title, restoredFrom: &k, restoredRefs: toSet(old.Refs), wholeRestore: true}, nil
}

func planRestoreUnits(ctx context.Context, tx store.TxAccountScope, t Type, cur Current, rawExpected json.RawMessage, raw json.RawMessage, retain Retain) (write, error) {
	var op restoreUnitsOp
	if err := creativeops.Decode(raw, &op); err != nil {
		return write{}, err
	}
	expected, err := decodeExpected(rawExpected)
	if err != nil {
		return write{}, err
	}
	k := int64(op.FromRevision)
	if k < 1 || k >= cur.Revision {
		return write{}, creativeops.Explain(creativeops.ErrValidation, "只能恢复到第 1 到第 %d 版", cur.Revision-1)
	}
	if len(op.Units) == 0 || len(op.Units) > maxRestoreUnits {
		return write{}, creativeops.Explain(creativeops.ErrValidation, "一次恢复 1 到 %d 个单元", maxRestoreUnits)
	}
	seen := map[string]bool{}
	var typeUnits []string
	meta := false
	for _, u := range op.Units {
		if !unitName.MatchString(u) || seen[u] {
			return write{}, creativeops.Explain(creativeops.ErrValidation, "单元 %q 不合法或重复", u)
		}
		seen[u] = true
		if u == MetaUnit {
			meta = true
		} else {
			typeUnits = append(typeUnits, u)
		}
	}
	old, err := loadRestoreSource(ctx, tx, cur.Document, k)
	if err != nil {
		return write{}, err
	}
	var touched []string
	if meta {
		touched = append(touched, MetaUnit)
	}
	if len(typeUnits) > 0 {
		units, err := t.RestoreTouched(ctx, tx, old, cur, typeUnits)
		if err != nil {
			return write{}, err
		}
		touched = append(touched, units...)
	}
	if err := checkExpected(cur.Document, expected, touched); err != nil {
		return write{}, err
	}
	w := write{title: cur.Title, snapshot: Snapshot{Payload: cur.Content.Payload, Refs: cur.Refs}, restoredFrom: &k, restoredRefs: toSet(old.Refs)}
	if len(typeUnits) > 0 {
		if w.snapshot, w.changes, err = t.RestoreUnits(ctx, tx, old, cur, retain, typeUnits); err != nil {
			return write{}, err
		}
	}
	if meta {
		w.title = old.Title
		w.changes.Changed = append(w.changes.Changed, MetaUnit)
	}
	return w, checkChanges(cur.Document, touched, w.changes)
}

// loadRestoreSource reads revision k for a restore. Documents created before
// history existed only have their current revision on record, so a missing
// older one is a bad request rather than a missing document.
func loadRestoreSource(ctx context.Context, tx store.TxAccountScope, head Document, k int64) (Current, error) {
	old, err := loadInTx(ctx, tx, head, k)
	if errors.Is(err, ErrRevisionNotFound) {
		return Current{}, creativeops.Explain(creativeops.ErrValidation, "第 %d 版没有保存历史，不能恢复", k)
	}
	return old, err
}

// checkChanges keeps a type honest: everything it advanced, annotated or
// removed passed the version check, except units it newly created, which the
// collection unit already guarded.
func checkChanges(head Document, touched []string, c Changes) error {
	allowed := toSet(touched)
	removed := toSet(c.Removed)
	for _, u := range c.Changed {
		_, existed := head.UnitRevs[u]
		if !unitName.MatchString(u) || removed[u] || existed && !allowed[u] {
			return fmt.Errorf("%w: changed unit %q", ErrTypeContract, u)
		}
	}
	for _, u := range append(append([]string{}, c.Annotated...), c.Removed...) {
		if !allowed[u] {
			return fmt.Errorf("%w: unit %q was not checked", ErrTypeContract, u)
		}
	}
	return nil
}

func persist(ctx context.Context, tx store.TxAccountScope, t Type, cur Current, actor Actor, w write, next int64, pre map[string]bool) (CommandResult, error) {
	if err := checkPayloadSize(w.snapshot.Payload); err != nil {
		return CommandResult{}, err
	}
	refs := dedupe(w.snapshot.Refs)
	inSnapshot := toSet(refs)
	for id := range pre {
		if !inSnapshot[id] {
			return CommandResult{}, fmt.Errorf("%w: retained %s outside the snapshot", ErrTypeContract, id)
		}
	}
	current := toSet(cur.Refs)
	for _, ref := range refs {
		if current[ref] || pre[ref] {
			continue
		}
		if w.restoredFrom != nil {
			if !w.restoredRefs[ref] {
				return CommandResult{}, creativeops.Explain(creativeops.ErrValidation, "恢复带回的内容 %s 不在第 %d 版的引用里", ref, *w.restoredFrom)
			}
			continue
		}
		if _, err := creativecontent.RequireUsable(ctx, tx, ref, "display"); err != nil {
			return CommandResult{}, err
		}
	}

	revs := make(map[string]int64, len(cur.UnitRevs))
	changed, annotated, removed := dedupe(w.changes.Changed), dedupe(w.changes.Annotated), dedupe(w.changes.Removed)
	if w.wholeRestore {
		units, err := t.Units(w.snapshot.Payload)
		if err != nil {
			return CommandResult{}, err
		}
		revs[MetaUnit] = next
		for _, u := range units {
			if !unitName.MatchString(u) || u == MetaUnit {
				return CommandResult{}, fmt.Errorf("%w: unit %q", ErrTypeContract, u)
			}
			revs[u] = next
		}
		changed, annotated, removed = []string{"*"}, nil, nil
	} else {
		for k, v := range cur.UnitRevs {
			revs[k] = v
		}
		for _, u := range removed {
			delete(revs, u)
		}
		for _, u := range changed {
			revs[u] = next
		}
	}
	if len(revs) > maxUnits {
		return CommandResult{}, ErrSizeLimit
	}
	for k, v := range w.changes.Response {
		if sharedReceiptFields[k] || !json.Valid(v) {
			return CommandResult{}, fmt.Errorf("%w: receipt field %q", ErrTypeContract, k)
		}
	}
	unitJSON, err := json.Marshal(revs)
	if err != nil {
		return CommandResult{}, err
	}
	declaration := cur.Content.DeclarationID
	draft := creativecontent.Draft{Kind: t.ContentKind(), Payload: w.snapshot.Payload, DeclarationID: declaration}
	if declaration == "" {
		draft.Rights = creativecontent.RightsDeclarationInput{}.OrDefault()
	}
	r, err := creativecontent.WriteDocumentAndRetain(ctx, tx, draft, func(r creativecontent.Revision) error {
		return insertRevision(ctx, tx, cur.Document.ID, next, r.ID, w.title, actor, w.restoredFrom, changed, annotated, removed, unitJSON)
	})
	if err != nil {
		return CommandResult{}, err
	}
	if err := insertRefs(ctx, tx, cur.Document.ID, next, refs, pre); err != nil {
		return CommandResult{}, err
	}
	if _, err := tx.Update(ctx, "creative_documents", "revision=$2,content_revision_id=$3,title=$4,unit_revs=$5,updated_at=clock_timestamp()", "id=$6",
		next, r.ID, w.title, unitJSON, cur.Document.ID); err != nil {
		return CommandResult{}, err
	}
	res := receipt(next, w.wholeRestore, changed, annotated, removed)
	if len(w.changes.Response) > 0 {
		res.Extra = w.changes.Response
	}
	return res, nil
}

func receipt(revision int64, reset bool, changed, annotated, removed []string) CommandResult {
	res := CommandResult{Revision: creativeops.Revision(revision), ChangedUnits: []string{}, AnnotatedUnits: []string{}, RemovedUnits: []string{}, ResetRequired: true}
	if reset {
		return res
	}
	lists, err := json.Marshal([][]string{changed, annotated, removed})
	if err != nil || len(lists) > receiptListBudget {
		return res
	}
	res.ResetRequired = false
	res.ChangedUnits, res.AnnotatedUnits, res.RemovedUnits = orEmpty(changed), orEmpty(annotated), orEmpty(removed)
	return res
}

func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func toSet(v []string) map[string]bool {
	out := make(map[string]bool, len(v))
	for _, s := range v {
		out[s] = true
	}
	return out
}

// IsPublicConflict reports whether err is a unit conflict, for callers that
// shape it into a tool result instead of an HTTP error.
func IsPublicConflict(err error) (*ConflictError, bool) {
	var c *ConflictError
	ok := errors.As(err, &c)
	return c, ok
}
