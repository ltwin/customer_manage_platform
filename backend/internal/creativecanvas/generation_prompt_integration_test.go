package creativecanvas_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestGenerationMentionCompileUsesLiveScopedReference(t *testing.T) {
	_, scope, _, _, p, ids := executionFixture(t)
	c, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range c.Nodes {
		if n.ContentRevisionID != nil {
			if err := creativecontent.GrantGenerationReference(t.Context(), scope, *n.ContentRevisionID); err != nil {
				t.Fatal(err)
			}
		}
	}
	var target creativecanvas.Node
	for _, n := range c.Nodes {
		if n.ID == ids[2] {
			target = n
		}
	}
	text := "把 @[" + ids[0] + "] 和 @[" + ids[1] + "] 组合起来"
	_, err = creativecanvas.SaveNodePrompt(t.Context(), scope, command(t, creativecanvas.SavePromptInput{CanvasID: p.CanvasID, NodeID: target.ID, ExpectedRevision: &target.Prompt.Revision, ExpectedTopologyRevision: c.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(c), ActionKey: "internal.text-compose", Text: text, Parameters: json.RawMessage(`{}`)}))
	if err != nil {
		t.Fatal(err)
	}
	target.Prompt.Revision++
	input := creativecanvas.RequestExecutionInput{CanvasID: p.CanvasID, NodeID: target.ID, DraftRevision: target.Prompt.Revision}
	compile := func(in creativecanvas.RequestExecutionInput) (creativecanvas.CompiledPrompt, error) {
		var result creativecanvas.CompiledPrompt
		err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			var e error
			result, e = creativecanvas.CompileGenerationPromptInTx(t.Context(), tx, in)
			return e
		})
		return result, err
	}
	result, err := compile(input)
	if err != nil || result.Raw != text || result.Text != "把 窗边自然光 和 柔和的人像 组合起来" {
		t.Fatalf("compiled: %+v %v", result, err)
	}
	stale := input
	stale.DraftRevision--
	if _, err = compile(stale); !errors.Is(err, creativecanvas.ErrVersionConflict) {
		t.Fatalf("stale draft: %v", err)
	}
	// Removing the edge invalidates the old mention rather than exposing unrelated content.
	c, err = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range c.Edges {
		if e.SourceNodeID == ids[0] {
			graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "disconnect_reference", EdgeID: e.ID})
			break
		}
	}
	if _, err = compile(input); err == nil {
		t.Fatal("disconnected mention compiled")
	}
}
