package creativecanvas

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func Undo(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return reverse(ctx, scope, c, false)
}
func Redo(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return reverse(ctx, scope, c, true)
}
func reverse(ctx context.Context, scope store.AccountScope, command creativeops.Command, redo bool) (creativeops.Receipt, error) {
	key := "canvas.undo"
	if redo {
		key = "canvas.redo"
	}
	return run(ctx, scope, key, command, func(v UndoInput) error {
		if v.CanvasID == "" || (v.ChangeID == "") == (v.ChangeGroupID == "") || redo && v.ChangeGroupID != "" {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v UndoInput) (creativeops.Outcome, error) {
		c, err := lockCanvas(ctx, tx, v.CanvasID, true)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		before, ids, err := loadGraph(ctx, tx, c.ID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		condition := "canvas_id=$2 AND reversible AND id=$3"
		target := v.ChangeID
		if v.ChangeGroupID != "" {
			condition = "canvas_id=$2 AND reversible AND change_group_id=$3"
			target = v.ChangeGroupID
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		rows, err := tx.QueryPage(ctx, "creative_changes", "id,inverse_of,before_after,expires_at", condition, []store.OrderBy{{Column: "result_revision"}}, 101, 0, c.ID, target)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		merged := map[string]fieldChange{}
		count := 0
		inverseOf := ""
		for rows.Next() {
			var id string
			var inverse *string
			var raw json.RawMessage
			var expires time.Time
			if err = rows.Scan(&id, &inverse, &raw, &expires); err != nil {
				rows.Close()
				return creativeops.Outcome{}, err
			}
			count++
			if !now.Before(expires) {
				rows.Close()
				return creativeops.Outcome{}, creativeops.ErrExpired
			}
			if redo && inverse == nil {
				rows.Close()
				return creativeops.Outcome{}, creativeops.ErrValidation
			}
			var detail changeDetail
			if err = json.Unmarshal(raw, &detail); err != nil || detail.SchemaVersion != 1 {
				rows.Close()
				return creativeops.Outcome{}, ErrGraphIntegrity
			}
			inverseOf = id
			for _, f := range detail.Fields {
				k := f.ID + ":" + f.Field
				if old, ok := merged[k]; ok {
					if old.After.Token != f.Before.Token || old.After.Hash != f.Before.Hash {
						rows.Close()
						return creativeops.Outcome{}, ErrVersionConflict
					}
					old.After = f.After
					merged[k] = old
				} else {
					merged[k] = f
				}
			}
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return creativeops.Outcome{}, err
		}
		if count == 0 {
			return creativeops.Outcome{}, ErrNotFound
		}
		if count > 100 {
			return creativeops.Outcome{}, ErrLimit
		}
		fields := graphFields(before)
		required := map[string]map[string]bool{}
		changes := []fieldChange{}
		keys := make([]string, 0, len(merged))
		for k := range merged {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			f := merged[key]
			ident, ok := ids[f.ID]
			if !ok {
				return creativeops.Outcome{}, ErrGraphIntegrity
			}
			head, ok := ident.Heads[f.Field]
			if !ok {
				return creativeops.Outcome{}, ErrGraphIntegrity
			}
			actual := fieldValue(fields[f.ID], f.Field)
			if head.Hash != creativegraph.Hash(actual) || !equalValue(head.Value, actual) {
				return creativeops.Outcome{}, ErrGraphIntegrity
			}
			if head.Token != f.After.Token || head.Hash != f.After.Hash {
				return creativeops.Outcome{}, ErrVersionConflict
			}
			if required[f.ID] == nil {
				required[f.ID] = map[string]bool{}
			}
			if f.Field == "placement" {
				required[f.ID]["placement"] = true
			} else {
				required[f.ID]["data"] = true
			}
			if f.Field == "existence" {
				required[f.ID]["placement"] = true
			}
			changes = append(changes, fieldChange{ID: f.ID, Kind: f.Kind, Field: f.Field, Before: head, After: f.Before})
		}
		if err = validateReadSet(v.ReadSet, required, ids); err != nil {
			return creativeops.Outcome{}, err
		}
		after, err := restoreFields(before, changes)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		for _, f := range changes {
			if f.Kind != "node" {
				continue
			}
			n, ok := after.Nodes[f.ID]
			if !ok {
				n, ok = before.Nodes[f.ID]
			}
			if ok {
				d, known := definition(n.TypeKey)
				if !known || d.SchemaVersion != n.TypeVersion {
					return creativeops.Outcome{}, creativeops.ErrValidation
				}
			}
		}
		if err = validateGraph(after); err != nil {
			return creativeops.Outcome{}, err
		}
		// Check only restored content; revoked unrelated nodes cannot block layout undo.
		touched := map[string]bool{}
		for _, f := range changes {
			if f.Field == "data" || f.Field == "existence" {
				touched[f.ID] = true
			}
		}
		subset := graphState{Nodes: map[string]Node{}}
		for id := range touched {
			if n, ok := after.Nodes[id]; ok {
				subset.Nodes[id] = n
			}
		}
		if err = validateContent(ctx, tx, subset); err != nil {
			return creativeops.Outcome{}, err
		}
		result, err := persistGraph(ctx, tx, c, before, after, ids, changes, command.OperationID, newChangeID(), "", inverseOf, nil, nil)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		return outcome(200, "canvas_change", result.ChangeID, result.ResultRevision, result)
	})
}
func restoreFields(before graphState, changes []fieldChange) (graphState, error) {
	after := cloneGraph(before)
	fields := graphFields(before)
	kinds := map[string]string{}
	for _, f := range changes {
		if fields[f.ID] == nil {
			fields[f.ID] = map[string]json.RawMessage{}
		}
		fields[f.ID][f.Field] = f.After.Value
		kinds[f.ID] = f.Kind
	}
	for id, kind := range kinds {
		f := fields[id]
		if !equalValue(f["existence"], creativegraph.Canonical(true)) {
			delete(after.Nodes, id)
			delete(after.Edges, id)
			delete(after.Inputs, id)
			delete(after.Versions, id)
			continue
		}
		switch kind {
		case "node":
			n := after.Nodes[id]
			if n.PlacementRevision < 1 {
				n.PlacementRevision = 1
			}
			if n.DataRevision < 1 {
				n.DataRevision = 1
			}
			if n.StatusRevision < 1 {
				n.StatusRevision = 1
			}
			m := map[string]json.RawMessage{"id": creativegraph.Canonical(id)}
			for _, field := range []string{"placement", "data"} {
				var values map[string]json.RawMessage
				if err := json.Unmarshal(f[field], &values); err != nil {
					return after, ErrGraphIntegrity
				}
				for k, v := range values {
					m[k] = v
				}
			}
			if err := json.Unmarshal(creativegraph.Canonical(m), &n); err != nil {
				return after, err
			}
			after.Nodes[id] = n
		case "edge":
			var e Edge
			if err := json.Unmarshal(f["data"], &e); err != nil {
				return after, err
			}
			after.Edges[id] = e
		case "version":
			var v NodeVersion
			if err := json.Unmarshal(f["data"], &v); err != nil {
				return after, err
			}
			after.Versions[id] = v
		case "input":
			var i NodeInput
			if err := json.Unmarshal(f["data"], &i); err != nil {
				return after, err
			}
			after.Inputs[id] = i
		default:
			return after, ErrGraphIntegrity
		}
	}
	// Relations are a checked invariant, never an independent writable bag.
	actual := graphFields(after)
	for _, f := range changes {
		if !equalValue(fieldValue(actual[f.ID], f.Field), f.After.Value) {
			return after, ErrGraphIntegrity
		}
	}
	return after, nil
}
