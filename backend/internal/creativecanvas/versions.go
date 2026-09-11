package creativecanvas

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type VersionInput struct {
	RevisionID string `json:"content_revision_id"`
	Role       string `json:"role"`
}
type NodeVersion struct {
	ID                string               `json:"id"`
	NodeID            string               `json:"node_id"`
	Number            creativeops.Revision `json:"version_no"`
	Origin            string               `json:"origin_kind"`
	ContentRevisionID string               `json:"content_revision_id"`
	ExecutionID       *string              `json:"execution_id_snapshot"`
	Ordinal           *int                 `json:"output_ordinal"`
	Provenance        json.RawMessage      `json:"generation_provenance"`
	CreatedAt         time.Time            `json:"created_at"`
	Revision          creativeops.Revision `json:"revision"`
	Inputs            []VersionInput       `json:"inputs"`
}
type VersionPage struct {
	Items             []NodeVersion        `json:"items"`
	SelectedVersionID *string              `json:"selected_version_id"`
	DataRevision      creativeops.Revision `json:"data_revision"`
}

func ListNodeVersions(ctx context.Context, scope store.AccountScope, canvas, node string) (VersionPage, error) {
	var page VersionPage
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		if _, err := lockCanvas(ctx, tx, canvas, false); err != nil {
			return err
		}
		n, err := scanNode(tx.QueryRow(ctx, "creative_nodes", nodeColumns, "id=$2 AND canvas_id=$3", node, canvas))
		if err != nil {
			return err
		}
		versions, err := loadVersions(ctx, tx, canvas)
		if err != nil {
			return err
		}
		page = VersionPage{Items: []NodeVersion{}, SelectedVersionID: n.SelectedVersionID, DataRevision: n.DataRevision}
		for _, v := range versions {
			if v.NodeID == node {
				page.Items = append(page.Items, v)
			}
		}
		sort.Slice(page.Items, func(i, j int) bool { return page.Items[i].Number > page.Items[j].Number })
		return nil
	})
	return page, err
}
func loadVersions(ctx context.Context, tx store.TxAccountScope, canvas string) (map[string]NodeVersion, error) {
	out := map[string]NodeVersion{}
	rows, err := tx.QueryPage(ctx, "creative_node_versions", "id,node_id,version_no,origin_kind,content_revision_id,execution_id_snapshot,output_ordinal,generation_provenance,created_at,revision", "canvas_id=$2 AND deleted_at IS NULL", []store.OrderBy{{Column: "id"}}, 10001, 0, canvas)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v NodeVersion
		var number, revision int64
		if err = rows.Scan(&v.ID, &v.NodeID, &number, &v.Origin, &v.ContentRevisionID, &v.ExecutionID, &v.Ordinal, &v.Provenance, &v.CreatedAt, &revision); err != nil {
			rows.Close()
			return nil, err
		}
		v.Number = creativeops.Revision(number)
		v.Revision = creativeops.Revision(revision)
		v.Inputs = []VersionInput{}
		out[v.ID] = v
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > 10000 {
		return nil, ErrLimit
	}
	for id, v := range out {
		rows, err := tx.QueryPage(ctx, "creative_node_version_input_refs", "content_revision_id,role", "version_id=$2", []store.OrderBy{{Column: "ordinal"}}, 101, 0, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var i VersionInput
			if err = rows.Scan(&i.RevisionID, &i.Role); err != nil {
				rows.Close()
				return nil, err
			}
			v.Inputs = append(v.Inputs, i)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, nil
}
func nextVersion(ctx context.Context, tx store.TxAccountScope, node string) (creativeops.Revision, error) {
	exists, err := tx.Exists(ctx, "creative_node_version_counters", "node_id=$2", node)
	if err != nil {
		return 0, err
	}
	if !exists {
		if err = tx.Insert(ctx, "creative_node_version_counters", []string{"node_id"}, node); err != nil {
			return 0, err
		}
	}
	if _, err = tx.Update(ctx, "creative_node_version_counters", "last_version_no=last_version_no+1", "node_id=$2", node); err != nil {
		return 0, err
	}
	var number int64
	err = tx.QueryRow(ctx, "creative_node_version_counters", "last_version_no", "node_id=$2", node).Scan(&number)
	return creativeops.Revision(number), err
}
func makeVersion(ctx context.Context, tx store.TxAccountScope, node, revision, origin string, provenance json.RawMessage, inputs []VersionInput) (NodeVersion, error) {
	number, err := nextVersion(ctx, tx, node)
	if err != nil {
		return NodeVersion{}, err
	}
	now, err := tx.CreativeNow(ctx)
	return NodeVersion{ID: "cwver_" + uuid.NewString(), NodeID: node, Number: number, Origin: origin, ContentRevisionID: revision, Provenance: provenance, CreatedAt: now, Revision: 1, Inputs: inputs}, err
}
func (e *editGraph) selectVersion(a Action) error {
	n, err := e.node(a.NodeID, "data")
	if err != nil {
		return err
	}
	v, ok := e.g.Versions[a.VersionID]
	if !ok || v.NodeID != n.ID {
		return ErrNotFound
	}
	e.need(v.ID, "data")
	r, err := creativecontent.RequireUsable(e.ctx, e.tx, v.ContentRevisionID, "display")
	if err != nil {
		return err
	}
	if n.TypeKey != "core."+r.Kind {
		return creativeops.ErrValidation
	}
	n.ContentID = &r.ContentID
	n.ContentRevisionID = &r.ID
	n.SelectedVersionID = &v.ID
	e.g.Nodes[n.ID] = n
	return nil
}
func (e *editGraph) deleteVersion(a Action) error {
	n, err := e.node(a.NodeID, "data")
	if err != nil {
		return err
	}
	v, ok := e.g.Versions[a.VersionID]
	if !ok || v.NodeID != n.ID {
		return ErrNotFound
	}
	if n.SelectedVersionID != nil && *n.SelectedVersionID == v.ID {
		return creativeops.ErrValidation
	}
	e.need(v.ID, "data")
	delete(e.g.Versions, v.ID)
	return nil
}
func persistVersion(ctx context.Context, tx store.TxAccountScope, canvas, id string, i identity, g graphState) error {
	if !i.Live {
		_, err := tx.Update(ctx, "creative_node_versions", "deleted_at=clock_timestamp(),revision=$2", "id=$3 AND canvas_id=$4", int64(i.D), id, canvas)
		return err
	}
	v := g.Versions[id]
	exists, err := tx.Exists(ctx, "creative_node_versions", "id=$2 AND canvas_id=$3", id, canvas)
	if err != nil {
		return err
	}
	if exists {
		_, err = tx.Update(ctx, "creative_node_versions", "deleted_at=NULL,revision=$2", "id=$3 AND canvas_id=$4", int64(i.D), id, canvas)
		return err
	}
	if err = tx.Insert(ctx, "creative_node_versions", []string{"id", "canvas_id", "node_id", "version_no", "origin_kind", "content_revision_id", "execution_id_snapshot", "output_ordinal", "generation_provenance", "created_at", "revision"}, v.ID, canvas, v.NodeID, int64(v.Number), v.Origin, v.ContentRevisionID, v.ExecutionID, v.Ordinal, v.Provenance, v.CreatedAt, int64(i.D)); err != nil {
		return err
	}
	for ordinal, ref := range v.Inputs {
		if err = tx.Insert(ctx, "creative_node_version_input_refs", []string{"version_id", "ordinal", "content_revision_id", "role"}, id, ordinal, ref.RevisionID, ref.Role); err != nil {
			return err
		}
	}
	return nil
}

type ReuseVersionInput struct {
	CanvasID                 string                `json:"canvas_id"`
	NodeID                   string                `json:"node_id"`
	VersionID                string                `json:"version_id"`
	ExpectedDraftRevision    *creativeops.Revision `json:"expected_draft_revision"`
	ExpectedTopologyRevision creativeops.Revision  `json:"expected_topology_revision"`
	ReadSet                  []ObjectRead          `json:"read_set"`
}

func ReuseVersionPrompt(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "canvas.reuse_version_prompt", c, func(v ReuseVersionInput) error {
		if v.CanvasID == "" || v.NodeID == "" || v.VersionID == "" {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v ReuseVersionInput) (creativeops.Outcome, error) {
		if _, err := lockCanvas(ctx, tx, v.CanvasID, true); err != nil {
			return creativeops.Outcome{}, err
		}
		versions, err := loadVersions(ctx, tx, v.CanvasID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		version, ok := versions[v.VersionID]
		if !ok || version.NodeID != v.NodeID {
			return creativeops.Outcome{}, ErrNotFound
		}
		var provenance struct {
			Prompt NodePrompt `json:"prompt"`
		}
		if err = json.Unmarshal(version.Provenance, &provenance); err != nil || provenance.Prompt.ID == "" {
			return creativeops.Outcome{}, creativeops.ErrValidation
		}
		refs := []InputSource{}
		for ordinal, input := range version.Inputs {
			if _, err = creativecontent.RequireUsable(ctx, tx, input.RevisionID, "display"); err != nil {
				return creativeops.Outcome{}, err
			}
			id := input.RevisionID
			refs = append(refs, InputSource{Slot: "reference", Ordinal: ordinal, Role: input.Role, ContentRevisionID: &id})
		}
		return savePromptInTx(ctx, tx, SavePromptInput{CanvasID: v.CanvasID, NodeID: v.NodeID, ExpectedRevision: v.ExpectedDraftRevision, ExpectedTopologyRevision: v.ExpectedTopologyRevision, ReadSet: v.ReadSet, ActionKey: provenance.Prompt.ActionKey, Text: provenance.Prompt.Text, ModelKey: provenance.Prompt.ModelKey, Parameters: provenance.Prompt.Parameters, References: refs}, c.OperationID)
	})
}
