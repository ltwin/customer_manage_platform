package creativecanvas_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// V6-004：缩放约束宽 160–1200、非媒体高 ≥100；组不能手动缩放。
func TestResizeNodeLimits(t *testing.T) {
	_, scope, _ := setup(t)
	p := project(t, scope)
	text, group := "cwnode_"+uuid.NewString(), "cwnode_"+uuid.NewString()
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: text, TypeKey: "core.text"}, creativecanvas.Action{Type: "add_node", NodeID: group, TypeKey: "core.group"})
	try := func(id string, w, h float64) error {
		c, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
		if err != nil {
			t.Fatal(err)
		}
		input := creativecanvas.BatchInput{CanvasID: p.CanvasID, ExpectedTopologyRevision: c.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(c), Actions: []creativecanvas.Action{{Type: "resize_node", NodeID: id, Width: w, Height: h}}}
		_, err = creativecanvas.ApplyCommands(t.Context(), scope, command(t, input))
		return err
	}
	for _, tc := range []struct {
		name string
		id   string
		w, h float64
		ok   bool
	}{
		{"min width", text, 160, 100, true},
		{"max width", text, 1200, 400, true},
		{"too narrow", text, 159, 200, false},
		{"too wide", text, 1201, 200, false},
		{"too short", text, 300, 99, false},
		{"group", group, 400, 300, false},
	} {
		err := try(tc.id, tc.w, tc.h)
		if tc.ok != (err == nil) {
			t.Fatalf("%s: err=%v", tc.name, err)
		}
		if !tc.ok && !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("%s: want validation error, got %v", tc.name, err)
		}
	}
	c, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	n := findNode(t, c, text)
	if n.Width != 1200 || n.Height != 400 {
		t.Fatal("last accepted resize is kept", n.Width, n.Height)
	}
	raw, _ := json.Marshal(findNode(t, c, group))
	var view struct {
		Capabilities struct {
			Actions []string `json:"actions"`
		} `json:"capabilities"`
	}
	_ = json.Unmarshal(raw, &view)
	for _, a := range view.Capabilities.Actions {
		if a == "resize" {
			t.Fatal("group still advertises resize")
		}
	}
}
