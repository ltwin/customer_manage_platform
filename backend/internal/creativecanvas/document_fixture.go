package creativecanvas

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Internal document creation is exposed only by the opt-in integration harness.
// No production route or node creation catalog registers this fixture.
type CreateDocumentInput struct {
	Title   string                `json:"title"`
	Content creativecontent.Draft `json:"content"`
}
type Document struct {
	ID                string                    `json:"id"`
	Title             string                    `json:"title"`
	TypeKey           string                    `json:"type_key"`
	ContentRevisionID string                    `json:"content_revision_id"`
	Revision          creativeops.Revision      `json:"revision"`
	Content           *creativecontent.Revision `json:"content,omitempty"`
}

func CreateInternalDocument(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "canvas.internal_create_document", c, func(v CreateDocumentInput) error {
		if !validName(v.Title) || v.Content.Kind != "text" {
			return creativeops.ErrValidation
		}
		return creativecontent.Validate(v.Content)
	}, func(ctx context.Context, tx store.TxAccountScope, v CreateDocumentInput) (creativeops.Outcome, error) {
		d := Document{ID: "cwdoc_" + uuid.NewString(), Title: v.Title, TypeKey: "internal.document", Revision: 1}
		r, err := creativecontent.WriteAndRetain(ctx, tx, v.Content, "", nil, func(r creativecontent.Revision) error {
			return tx.Insert(ctx, "creative_documents", []string{"id", "type_key", "title", "content_revision_id"}, d.ID, d.TypeKey, d.Title, r.ID)
		})
		if err != nil {
			return creativeops.Outcome{}, err
		}
		d.ContentRevisionID = r.ID
		return outcome(201, "document", d.ID, d.Revision, d)
	})
}
func readDocument(ctx context.Context, tx store.TxAccountScope, id string) (Document, error) {
	var d Document
	var revision int64
	err := tx.QueryRow(ctx, "creative_documents", "id,type_key,title,content_revision_id,revision", "id=$2", id).Scan(&d.ID, &d.TypeKey, &d.Title, &d.ContentRevisionID, &revision)
	if err != nil {
		return d, notFound(err)
	}
	d.Revision = creativeops.Revision(revision)
	r, err := creativecontent.RequireUsable(ctx, tx, d.ContentRevisionID, "display")
	if err != nil {
		return d, err
	}
	d.Content = &r
	return d, nil
}
func ReadDocument(ctx context.Context, scope store.AccountScope, id string) (Document, error) {
	var d Document
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		var err error
		d, err = readDocument(ctx, tx, id)
		return err
	})
	return d, err
}

type ReferenceDocumentInput struct {
	CanvasID                 string               `json:"canvas_id"`
	NodeID                   string               `json:"node_id"`
	DocumentID               string               `json:"document_id"`
	ExpectedTopologyRevision creativeops.Revision `json:"expected_topology_revision"`
	X                        float64              `json:"x"`
	Y                        float64              `json:"y"`
}

func ReferenceInternalDocument(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "canvas.internal_reference_document", c, func(v ReferenceDocumentInput) error {
		if !validNodeID(v.NodeID) || !validPoint(v.X, v.Y) {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v ReferenceDocumentInput) (creativeops.Outcome, error) {
		canvas, err := lockCanvas(ctx, tx, v.CanvasID, true)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if canvas.TopologyRevision != v.ExpectedTopologyRevision {
			return creativeops.Outcome{}, ErrVersionConflict
		}
		doc, err := readDocument(ctx, tx, v.DocumentID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		before, ids, err := loadGraph(ctx, tx, canvas.ID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		used, err := tx.Exists(ctx, "creative_graph_identities", "id=$2", v.NodeID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if used {
			return creativeops.Outcome{}, ErrVersionConflict
		}
		after := cloneGraph(before)
		after.Nodes[v.NodeID] = Node{ID: v.NodeID, TypeKey: "internal.document", TypeVersion: 1, Title: doc.Title, DocumentID: &doc.ID, X: v.X, Y: v.Y, Width: 380, Height: 260, Config: json.RawMessage(`{}`), PlacementRevision: 1, DataRevision: 1, StatusRevision: 1}
		changes, err := planChanges(before, after, ids)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		result, err := persistGraph(ctx, tx, canvas, before, after, ids, changes, c.OperationID, newChangeID(), "", "", nil, nil)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		return outcome(200, "canvas_change", result.ChangeID, result.ResultRevision, result)
	})
}
