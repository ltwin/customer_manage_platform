package creativecanvas

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type NodeMetadata struct {
	TypeKey     string  `json:"type_key"`
	TypeVersion int     `json:"type_version"`
	Title       string  `json:"title"`
	Intent      string  `json:"intent"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	ZOrder      int     `json:"z_order"`
}
type NodeData struct {
	SchemaVersion     int             `json:"schema_version"`
	Config            json.RawMessage `json:"config"`
	ContentID         *string         `json:"content_id"`
	ContentRevisionID *string         `json:"content_revision_id"`
	SelectedVersionID *string         `json:"selected_version_id"`
	DocumentID        *string         `json:"document_id"`
}
type NodeStatus struct {
	ContentState      string               `json:"content_state"`
	GenerationState   string               `json:"generation_state"`
	ActiveExecutionID *string              `json:"active_execution_id"`
	LatestExecutionID *string              `json:"latest_execution_id"`
	ApplyState        *string              `json:"apply_state"`
	Error             *string              `json:"error"`
	Revision          creativeops.Revision `json:"status_revision"`
}
type NodeCapabilities struct {
	Actions        []string `json:"actions"`
	PromptMode     string   `json:"prompt_mode"`
	DisabledReason *string  `json:"disabled_reason"`
}

func (n Node) MarshalJSON() ([]byte, error) {
	status := n.Status
	status.ContentState = "empty"
	if n.ContentRevisionID != nil || n.DocumentID != nil {
		status.ContentState = "ready"
	}
	if n.Unavailable {
		status.ContentState = "unavailable"
	}
	if status.GenerationState == "" {
		status.GenerationState = "idle"
	}
	status.Revision = n.StatusRevision
	status.ActiveExecutionID = n.ActiveExecutionID
	status.LatestExecutionID = n.LatestExecutionID
	capabilities := NodeCapabilities{Actions: []string{}, PromptMode: "unavailable"}
	if d, ok := definition(n.TypeKey); ok && d.SchemaVersion == n.TypeVersion {
		capabilities.Actions = d.Actions
		capabilities.PromptMode = d.PromptMode
	} else {
		reason := "此节点类型当前只读"
		capabilities.DisabledReason = &reason
	}
	if n.ReadOnly {
		capabilities.Actions = []string{}
		reason := "项目已归档"
		capabilities.DisabledReason = &reason
	}

	return json.Marshal(struct {
		ID                string                    `json:"id"`
		ParentID          *string                   `json:"parent_id"`
		Metadata          NodeMetadata              `json:"metadata"`
		Data              NodeData                  `json:"data"`
		Status            NodeStatus                `json:"status"`
		Prompt            *NodePrompt               `json:"prompt"`
		Capabilities      NodeCapabilities          `json:"capabilities"`
		PlacementRevision creativeops.Revision      `json:"placement_revision"`
		DataRevision      creativeops.Revision      `json:"data_revision"`
		Content           *creativecontent.Revision `json:"content,omitempty"`
	}{ID: n.ID, ParentID: n.ParentID, Metadata: NodeMetadata{TypeKey: n.TypeKey, TypeVersion: n.TypeVersion, Title: n.Title, Intent: n.Intent, X: n.X, Y: n.Y, Width: n.Width, Height: n.Height, ZOrder: n.ZOrder}, Data: NodeData{SchemaVersion: 1, Config: n.Config, ContentID: n.ContentID, ContentRevisionID: n.ContentRevisionID, SelectedVersionID: n.SelectedVersionID, DocumentID: n.DocumentID}, Status: status, Prompt: n.Prompt, Capabilities: capabilities, PlacementRevision: n.PlacementRevision, DataRevision: n.DataRevision, Content: n.Content})
}
func fillNodeView(ctx context.Context, tx store.TxAccountScope, canvas string, n *Node) error {
	if n.DocumentID != nil && n.TypeKey == "internal.document" {
		doc, err := readDocument(ctx, tx, *n.DocumentID)
		if errors.Is(err, creativecontent.ErrUsageDenied) || errors.Is(err, ErrNotFound) {
			n.Unavailable = true
		} else if err != nil {
			return err
		} else {
			preview := creativecontent.Preview(*doc.Content)
			n.Content = &preview
		}
	}
	p, err := readPrompt(ctx, tx, canvas, n.ID)
	if err != nil {
		return err
	}
	n.Prompt = p
	if n.LatestExecutionID == nil {
		return nil
	}
	var state, apply, message string
	if err = tx.QueryRow(ctx, "creative_node_executions", "state,apply_state,error", "id=$2 AND canvas_id=$3 AND node_id=$4", *n.LatestExecutionID, canvas, n.ID).Scan(&state, &apply, &message); err != nil {
		return err
	}
	n.Status = NodeStatus{GenerationState: state, ApplyState: &apply}
	if message != "" {
		n.Status.Error = &message
	}
	return nil
}

// The persistence projection is deliberately independent from the public DTO.

func nodeStateJSON(n Node) json.RawMessage {
	return creativegraph.Canonical(map[string]any{"id": n.ID, "type_key": n.TypeKey, "type_version": n.TypeVersion, "title": n.Title, "intent": n.Intent, "x": n.X, "y": n.Y, "width": n.Width, "height": n.Height, "z_order": n.ZOrder, "parent_id": n.ParentID, "config": n.Config, "content_id": n.ContentID, "content_revision_id": n.ContentRevisionID, "selected_version_id": n.SelectedVersionID, "document_id": n.DocumentID})
}
