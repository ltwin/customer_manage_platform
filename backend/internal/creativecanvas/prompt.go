package creativecanvas

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type NodePrompt struct {
	ID         string               `json:"draft_id"`
	Revision   creativeops.Revision `json:"draft_revision"`
	ActionKey  string               `json:"action_key"`
	Text       string               `json:"text"`
	ModelKey   string               `json:"model_key"`
	Parameters json.RawMessage      `json:"parameters"`
	References []NodeInput          `json:"references"`
}
type SavePromptInput struct {
	CanvasID                 string                `json:"canvas_id"`
	NodeID                   string                `json:"node_id"`
	ExpectedRevision         *creativeops.Revision `json:"expected_draft_revision"`
	ExpectedTopologyRevision creativeops.Revision  `json:"expected_topology_revision"`
	ReadSet                  []ObjectRead          `json:"read_set"`
	ActionKey                string                `json:"action_key"`
	Text                     string                `json:"text"`
	ModelKey                 string                `json:"model_key"`
	Parameters               json.RawMessage       `json:"parameters"`
	References               []InputSource         `json:"references"`
}

func SaveNodePrompt(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "canvas.save_prompt", c, func(v SavePromptInput) error {
		if v.CanvasID == "" || v.NodeID == "" || utf8.RuneCountInString(v.Text) > 32000 || len(v.References) > 50 || len(v.ActionKey) > 100 || len(v.ModelKey) > 200 || len(v.Parameters) > 8192 {
			return creativeops.ErrValidation
		}
		var parameters map[string]any
		if err := json.Unmarshal(v.Parameters, &parameters); err != nil || parameters == nil {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v SavePromptInput) (creativeops.Outcome, error) {
		return savePromptInTx(ctx, tx, v, c.OperationID)
	}, func(raw json.RawMessage) error {
		var f map[string]json.RawMessage
		if err := json.Unmarshal(raw, &f); err != nil {
			return err
		}
		if _, ok := f["expected_draft_revision"]; !ok {
			return creativeops.ErrValidation
		}
		return nil
	})
}
func readPrompt(ctx context.Context, tx store.TxAccountScope, canvas, node string) (*NodePrompt, error) {
	var p NodePrompt
	var revision int64
	err := tx.QueryRow(ctx, "creative_node_prompt_drafts", "id,revision,action_key,text,model_key,parameters", "canvas_id=$2 AND node_id=$3", canvas, node).Scan(&p.ID, &revision, &p.ActionKey, &p.Text, &p.ModelKey, &p.Parameters)
	if err == store.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Revision = creativeops.Revision(revision)
	p.References = []NodeInput{}
	rows, err := tx.QueryPage(ctx, "creative_node_prompt_refs", "id,node_id,slot,ordinal,role,source_node_id,content_revision_id,revision", "draft_id=$2 AND canvas_id=$3", []store.OrderBy{{Column: "ordinal"}}, 51, 0, p.ID, canvas)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var i NodeInput
		var r int64
		if err = rows.Scan(&i.ID, &i.NodeID, &i.Slot, &i.Ordinal, &i.Role, &i.SourceNodeID, &i.ContentRevisionID, &r); err != nil {
			return nil, err
		}
		i.Revision = creativeops.Revision(r)
		i.DraftID = &p.ID
		p.References = append(p.References, i)
	}
	return &p, rows.Err()
}
func promptTable(id string) bool { return strings.HasPrefix(id, "cwpref_") }

func savePromptInTx(ctx context.Context, tx store.TxAccountScope, v SavePromptInput, operation string) (creativeops.Outcome, error) {
	canvas, err := lockCanvas(ctx, tx, v.CanvasID, true)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	before, ids, err := loadGraph(ctx, tx, canvas.ID)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	n, ok := before.Nodes[v.NodeID]
	if !ok {
		return creativeops.Outcome{}, ErrNotFound
	}
	d, known := definition(n.TypeKey)
	if !known || d.SchemaVersion != n.TypeVersion || d.PromptMode == "unavailable" {
		return creativeops.Outcome{}, creativeops.ErrValidation
	}
	existing, err := readPrompt(ctx, tx, canvas.ID, n.ID)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	if (existing == nil) != (v.ExpectedRevision == nil) || existing != nil && existing.Revision != *v.ExpectedRevision {
		return creativeops.Outcome{}, ErrVersionConflict
	}
	p := NodePrompt{ID: "cwpd_" + uuid.NewString(), Revision: 1, ActionKey: v.ActionKey, Text: v.Text, ModelKey: v.ModelKey, Parameters: v.Parameters, References: []NodeInput{}}
	if existing != nil {
		p.ID = existing.ID
		p.Revision = existing.Revision + 1
	}
	e := editGraph{ctx: ctx, tx: tx, c: canvas, before: before, g: cloneGraph(before), ids: ids, required: map[string]map[string]bool{}, changeID: newChangeID()}
	for id, i := range e.g.Inputs {
		if i.DraftID != nil && *i.DraftID == p.ID {
			delete(e.g.Inputs, id)
			e.need(id, "data")
		}
	}
	slots := map[string]bool{}
	for _, source := range v.References {
		i := inputFromSource(source)
		if i.Slot != "reference" || i.Role != "reference" || i.Ordinal < 0 || (i.SourceNodeID == nil) == (i.ContentRevisionID == nil) {
			return creativeops.Outcome{}, creativeops.ErrValidation
		}
		key := string(creativegraph.Canonical([]any{i.Slot, i.Ordinal}))
		if slots[key] {
			return creativeops.Outcome{}, creativeops.ErrValidation
		}
		slots[key] = true
		if i.SourceNodeID != nil {
			source, err := e.node(*i.SourceNodeID, "data")
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if !hasPorts(source.TypeKey) || !hasPorts(n.TypeKey) {
				return creativeops.Outcome{}, creativeops.ErrValidation
			}
		} else {
			if _, err := creativecontent.RequireUsable(ctx, tx, *i.ContentRevisionID, "display"); err != nil {
				return creativeops.Outcome{}, err
			}
		}
		i.ID = "cwpref_" + uuid.NewString()
		if existing != nil {
			for _, old := range existing.References {
				if old.Slot == i.Slot && old.Ordinal == i.Ordinal && old.Role == i.Role && equalValue(creativegraph.Canonical(old.SourceNodeID), creativegraph.Canonical(i.SourceNodeID)) && equalValue(creativegraph.Canonical(old.ContentRevisionID), creativegraph.Canonical(i.ContentRevisionID)) {
					i.ID = old.ID
					break
				}
			}
		}
		i.DraftID = &p.ID
		i.NodeID = n.ID
		i.Revision = 1
		e.g.Inputs[i.ID] = i
		p.References = append(p.References, i)
	}
	if err = validateGraph(e.g); err != nil {
		return creativeops.Outcome{}, err
	}
	changes, err := planChanges(before, e.g, ids)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	// A text-only draft save has its own CAS, independent of data/placement.
	if len(changes) > 0 {
		if canvas.TopologyRevision != v.ExpectedTopologyRevision {
			return creativeops.Outcome{}, ErrVersionConflict
		}
		for _, f := range changes {
			if _, ok := before.Nodes[f.ID]; ok {
				e.need(f.ID, "data")
			}
		}
		if err = validateReadSet(v.ReadSet, e.required, ids); err != nil {
			return creativeops.Outcome{}, err
		}
	}
	if existing == nil {
		err = tx.Insert(ctx, "creative_node_prompt_drafts", []string{"id", "canvas_id", "node_id", "action_key", "text", "model_key", "parameters"}, p.ID, canvas.ID, n.ID, p.ActionKey, p.Text, p.ModelKey, p.Parameters)
	} else {
		_, err = tx.Update(ctx, "creative_node_prompt_drafts", "action_key=$2,text=$3,model_key=$4,parameters=$5,revision=$6", "id=$7 AND node_id=$8", p.ActionKey, p.Text, p.ModelKey, p.Parameters, int64(p.Revision), p.ID, n.ID)
	}
	if err != nil {
		return creativeops.Outcome{}, err
	}
	result, err := persistGraph(ctx, tx, canvas, before, e.g, ids, changes, operation, e.changeID, "", "", nil, nil, graphPersistence{PromptSave: true})
	if err != nil {
		return creativeops.Outcome{}, err
	}
	if _, err = tx.Update(ctx, "creative_changes", "reversible=false", "id=$2", result.ChangeID); err != nil {
		return creativeops.Outcome{}, err
	}
	return outcome(200, "node_prompt", p.ID, p.Revision, PromptResult{DraftID: p.ID, Revision: p.Revision, ChangeResult: result})
}

type PromptResult struct {
	DraftID      string               `json:"draft_id"`
	Revision     creativeops.Revision `json:"draft_revision"`
	ChangeResult ChangeResult         `json:"change"`
}
