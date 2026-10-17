package creativedocument_test

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// board is a test document type with everything the port has to support: a
// collection whose members appear and disappear, a free unit, references,
// content created inside a write, an annotation and a restore hook.
//
// Units: "items" (membership), "item:<id>" (one value each), "note", "refs".
// Extra["leak"] lets a test plant a reference that a later restore of "refs"
// brings back without it being in that revision's reference rows.
type board struct{}

type boardState struct {
	Items     map[string]int    `json:"items"`
	Note      string            `json:"note"`
	Refs      []string          `json:"refs"`
	Confirmed map[string]int64  `json:"confirmed"`
	Extra     map[string]string `json:"extra,omitempty"`
}

var errBoardFail = errors.New("board: failing on purpose")

func (board) Key() string         { return "test.board" }
func (board) NodeTypeKey() string { return "test.board" }
func (board) ContentKind() string { return "text" }
func (board) Creatable() bool     { return true }

func decodeBoard(p creativecontent.Payload) (boardState, error) {
	s := boardState{Items: map[string]int{}, Confirmed: map[string]int64{}}
	if p.Body == nil {
		return s, nil
	}
	if err := json.Unmarshal([]byte(*p.Body), &s); err != nil {
		return s, err
	}
	if s.Items == nil {
		s.Items = map[string]int{}
	}
	if s.Confirmed == nil {
		s.Confirmed = map[string]int64{}
	}
	return s, nil
}

func encodeBoard(s boardState) (Snapshot, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return Snapshot{}, err
	}
	body := string(raw)
	return Snapshot{Payload: creativecontent.Payload{Body: &body}, Refs: append([]string{}, s.Refs...)}, nil
}

type Snapshot = creativedocument.Snapshot

func (board) Empty(context.Context, store.TxAccountScope, string, json.RawMessage) (Snapshot, error) {
	return encodeBoard(boardState{Items: map[string]int{}, Note: "空白", Confirmed: map[string]int64{}})
}

func (board) Units(p creativecontent.Payload) ([]string, error) {
	s, err := decodeBoard(p)
	if err != nil {
		return nil, err
	}
	out := []string{"items", "note", "refs"}
	for id := range s.Items {
		out = append(out, "item:"+id)
	}
	return out, nil
}

type boardOp struct {
	Op    string   `json:"op"`
	ID    string   `json:"id,omitempty"`
	IDs   []string `json:"ids,omitempty"`
	Value int      `json:"value,omitempty"`
	Text  string   `json:"text,omitempty"`
	Ref   string   `json:"ref,omitempty"`
}

func decodeOp(raw json.RawMessage) (boardOp, error) {
	var op boardOp
	return op, creativeops.Decode(raw, &op)
}

func (board) Touched(_ context.Context, _ store.TxAccountScope, _ creativedocument.Current, _ creativedocument.Actor, ops []json.RawMessage) ([]string, error) {
	var out []string
	for _, raw := range ops {
		op, err := decodeOp(raw)
		if err != nil {
			return nil, err
		}
		switch op.Op {
		case "add_items":
			out = append(out, "items")
			for _, id := range op.IDs {
				out = append(out, "item:"+id)
			}
		case "set_item", "confirm":
			out = append(out, "item:"+op.ID)
		case "remove_items":
			out = append(out, "items")
			for _, id := range op.IDs {
				out = append(out, "item:"+id)
			}
		case "set_note", "plant_leak", "sneak", "respond", "respond_clash":
			out = append(out, "note")
		case "add_ref", "create_ref", "create_orphan", "fail_after_create":
			out = append(out, "refs")
		default:
			return nil, creativeops.Explain(creativeops.ErrValidation, "unknown op %q", op.Op)
		}
	}
	return out, nil
}

func writeText(ctx context.Context, tx store.TxAccountScope, text string, retain creativedocument.Retain) (creativecontent.Revision, error) {
	return creativecontent.WriteAndRetain(ctx, tx, creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &text},
		Rights: creativecontent.RightsDeclarationInput{SourceClass: planningmedia.SourcePhotographerOwned, RightsBasis: planningmedia.RightsOwnershipAttested}}, "", nil, func(r creativecontent.Revision) error { return retain(r) })
}

func (board) Apply(ctx context.Context, tx store.TxAccountScope, cur creativedocument.Current, _ creativedocument.Actor, retain creativedocument.Retain, ops []json.RawMessage) (Snapshot, creativedocument.Changes, error) {
	s, err := decodeBoard(cur.Content.Payload)
	if err != nil {
		return Snapshot{}, creativedocument.Changes{}, err
	}
	var c creativedocument.Changes
	for _, raw := range ops {
		op, _ := decodeOp(raw)
		switch op.Op {
		case "add_items":
			for _, id := range op.IDs {
				if _, ok := s.Items[id]; ok {
					return Snapshot{}, c, creativeops.Explain(creativeops.ErrValidation, "exists")
				}
				s.Items[id] = 1
				c.Changed = append(c.Changed, "item:"+id)
			}
			c.Changed = append(c.Changed, "items")
		case "set_item":
			if _, ok := s.Items[op.ID]; !ok {
				return Snapshot{}, c, creativedocument.ErrNotFound
			}
			s.Items[op.ID] = op.Value
			c.Changed = append(c.Changed, "item:"+op.ID)
		case "confirm":
			s.Confirmed[op.ID] = cur.UnitRevs["item:"+op.ID]
			c.Annotated = append(c.Annotated, "item:"+op.ID)
		case "remove_items":
			for _, id := range op.IDs {
				delete(s.Items, id)
				delete(s.Confirmed, id)
				c.Removed = append(c.Removed, "item:"+id)
			}
			c.Changed = append(c.Changed, "items")
		case "set_note":
			s.Note = op.Text
			c.Changed = append(c.Changed, "note")
		case "plant_leak":
			s.Extra = map[string]string{"leak": op.Ref}
			c.Changed = append(c.Changed, "note")
		case "respond", "respond_clash":
			// A type-specific receipt field; respond_clash reuses a shared one.
			s.Note = op.Text
			c.Changed = append(c.Changed, "note")
			key := "echo"
			if op.Op == "respond_clash" {
				key = "revision"
			}
			echo, err := json.Marshal(op.Text)
			if err != nil {
				return Snapshot{}, c, err
			}
			c.Response = map[string]json.RawMessage{key: echo}
		case "sneak":
			// Claims the note but reports a unit it never had checked.
			c.Changed = append(c.Changed, "note", "items")
		case "add_ref":
			s.Refs = append(s.Refs, op.Ref)
			c.Changed = append(c.Changed, "refs")
		case "create_ref", "create_orphan", "fail_after_create":
			r, err := writeText(ctx, tx, op.Text, retain)
			if err != nil {
				return Snapshot{}, c, err
			}
			if op.Op == "fail_after_create" {
				return Snapshot{}, c, errBoardFail
			}
			if op.Op == "create_ref" {
				s.Refs = append(s.Refs, r.ID)
			}
			c.Changed = append(c.Changed, "refs")
		}
	}
	snap, err := encodeBoard(s)
	return snap, c, err
}

func (board) RestoreTouched(_ context.Context, _ store.TxAccountScope, old, cur creativedocument.Current, units []string) ([]string, error) {
	o, err := decodeBoard(old.Content.Payload)
	if err != nil {
		return nil, err
	}
	n, err := decodeBoard(cur.Content.Payload)
	if err != nil {
		return nil, err
	}
	out := append([]string{}, units...)
	collection := false
	for _, u := range units {
		if id, ok := strings.CutPrefix(u, "item:"); ok {
			_, inOld := o.Items[id]
			_, inCur := n.Items[id]
			collection = collection || inOld != inCur
		}
	}
	if collection {
		out = append(out, "items")
	}
	return out, nil
}

func (board) RestoreUnits(_ context.Context, _ store.TxAccountScope, old, cur creativedocument.Current, _ creativedocument.Retain, units []string) (Snapshot, creativedocument.Changes, error) {
	o, _ := decodeBoard(old.Content.Payload)
	n, _ := decodeBoard(cur.Content.Payload)
	var c creativedocument.Changes
	for _, u := range units {
		switch {
		case u == "note":
			n.Note = o.Note
			c.Changed = append(c.Changed, u)
		case u == "refs":
			n.Refs = append([]string{}, o.Refs...)
			if leak := o.Extra["leak"]; leak != "" {
				n.Refs = append(n.Refs, leak)
			}
			c.Changed = append(c.Changed, u)
		case strings.HasPrefix(u, "item:"):
			id := strings.TrimPrefix(u, "item:")
			v, inOld := o.Items[id]
			_, inCur := n.Items[id]
			switch {
			case inOld:
				n.Items[id] = v
				c.Changed = append(c.Changed, u)
			case inCur:
				delete(n.Items, id)
				c.Removed = append(c.Removed, u)
			}
			if inOld != inCur {
				c.Changed = append(c.Changed, "items")
			}
		default:
			return Snapshot{}, c, creativeops.Explain(creativeops.ErrValidation, "unit %q", u)
		}
	}
	snap, err := encodeBoard(n)
	return snap, c, err
}

// AfterRestore keeps an item confirmed when its confirmation matched the
// restored revision, which needs that revision's unit revisions.
func (board) AfterRestore(_ context.Context, _ store.TxAccountScope, old creativedocument.Current, restored Snapshot, revision int64) (Snapshot, error) {
	s, err := decodeBoard(restored.Payload)
	if err != nil {
		return Snapshot{}, err
	}
	for id, confirmed := range s.Confirmed {
		if confirmed == old.UnitRevs["item:"+id] {
			s.Confirmed[id] = revision
		} else {
			delete(s.Confirmed, id)
		}
	}
	return encodeBoard(s)
}

func (board) View(_ context.Context, _ store.ReadTxAccountScope, cur creativedocument.Current, units []string) (json.RawMessage, error) {
	s, err := decodeBoard(cur.Content.Payload)
	if err != nil {
		return nil, err
	}
	if units == nil {
		return json.Marshal(map[string]any{"board": s})
	}
	out := map[string]any{}
	for _, u := range units {
		if u == "note" {
			out["note"] = s.Note
		}
	}
	return json.Marshal(out)
}

func (board) Summary(_ context.Context, _ store.ReadTxAccountScope, cur creativedocument.Current) (json.RawMessage, error) {
	s, err := decodeBoard(cur.Content.Payload)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(s.Items))
	for id := range s.Items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return json.Marshal(map[string]any{"items": len(ids), "refs": len(cur.Refs)})
}
