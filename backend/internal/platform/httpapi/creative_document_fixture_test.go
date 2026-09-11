package httpapi_test

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func documentFixture(ctx context.Context, scope store.AccountScope, canvasID, documentID string) (map[string]string, error) {
	command := func(v any) creativeops.Command {
		raw, _ := json.Marshal(v)
		return creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw}
	}
	if canvasID == "" {
		r, err := creativecanvas.CreateProject(ctx, scope, command(creativecanvas.CreateProjectInput{Name: "文档节点验证"}))
		if err != nil {
			return nil, err
		}
		var p creativecanvas.ProjectResult
		if err = json.Unmarshal(r.Outcome.Response, &p); err != nil {
			return nil, err
		}
		canvasID = p.CanvasID
	}
	if documentID == "" {
		body := "以光线组织空间，以留白承托人物。\n\n窗边的自然光保留层次，画面使用克制的灰绿与暖白。此内容保存在独立文档中，移除画布上的摘要后仍可重新引用。"
		r, err := creativecanvas.CreateInternalDocument(ctx, scope, command(creativecanvas.CreateDocumentInput{Title: "光线与空间", Content: creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}, Rights: creativecontent.RightsDeclarationInput{SourceClass: planningmedia.SourcePhotographerOwned, RightsBasis: planningmedia.RightsOwnershipAttested}}}))
		if err != nil {
			return nil, err
		}
		var d creativecanvas.Document
		if err = json.Unmarshal(r.Outcome.Response, &d); err != nil {
			return nil, err
		}
		documentID = d.ID
	}
	snapshot, err := creativecanvas.GetCanvas(ctx, scope, canvasID)
	if err != nil {
		return nil, err
	}
	nodeID := "cwnode_" + uuid.NewString()
	_, err = creativecanvas.ReferenceInternalDocument(ctx, scope, command(creativecanvas.ReferenceDocumentInput{CanvasID: canvasID, DocumentID: documentID, NodeID: nodeID, ExpectedTopologyRevision: snapshot.TopologyRevision, X: 240, Y: 180}))
	if err != nil {
		return nil, err
	}
	return map[string]string{"canvas_id": canvasID, "document_id": documentID, "node_id": nodeID}, nil
}
