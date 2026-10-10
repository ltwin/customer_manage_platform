package creativecanvas

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
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

// MarshalJSON writes CreatedAt in UTC. A version's JSON is hashed into its
// graph head, so it must not depend on the time zone of the process that
// happened to load or create it.
func (v NodeVersion) MarshalJSON() ([]byte, error) {
	type plain NodeVersion
	p := plain(v)
	p.CreatedAt = p.CreatedAt.UTC()
	return json.Marshal(p)
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
func loadVersions(ctx context.Context, tx canvasReader, canvas string) (map[string]NodeVersion, error) {
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
	if len(out) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(out))
	for id := range out {
		ids = append(ids, id)
	}
	rows, err = tx.QueryPage(ctx, "creative_node_version_input_refs", "version_id,content_revision_id,role", "version_id=ANY($2::text[])", []store.OrderBy{{Column: "version_id"}, {Column: "ordinal"}}, len(out)*101, 0, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var input VersionInput
		if err := rows.Scan(&id, &input.RevisionID, &input.Role); err != nil {
			return nil, err
		}
		v := out[id]
		if len(v.Inputs) >= 100 {
			return nil, ErrLimit
		}
		v.Inputs = append(v.Inputs, input)
		out[id] = v
	}
	if err := rows.Err(); err != nil {
		return nil, err
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
	if a.FollowLatest {
		for _, other := range e.g.Versions {
			if other.NodeID == n.ID && other.Number > v.Number {
				return ErrVersionConflict
			}
		}
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
	n.Config = mainVersionConfig(n.Config, !a.FollowLatest)
	fitMediaSize(&n, r)
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

// 固定模式与选定版本一起进入图的 data 撤销链；旧节点默认跟随最新。
func mainVersionPinned(n Node) bool {
	if n.SelectedVersionID == nil {
		return false
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(n.Config, &config) != nil {
		return false
	}
	var mode string
	_ = json.Unmarshal(config["main_version_mode"], &mode)
	return mode == "pinned"
}
func mainVersionConfig(raw json.RawMessage, pinned bool) json.RawMessage {
	var config map[string]json.RawMessage
	_ = json.Unmarshal(raw, &config)
	if config == nil {
		config = map[string]json.RawMessage{}
	}
	mode := "latest"
	if pinned {
		mode = "pinned"
	}
	config["main_version_mode"], _ = json.Marshal(mode)
	return creativegraph.Canonical(config)
}
