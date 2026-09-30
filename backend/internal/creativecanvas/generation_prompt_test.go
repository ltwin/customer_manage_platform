package creativecanvas

import (
	"errors"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

func TestCompilePromptReferencesPreservesAttachmentOrder(t *testing.T) {
	refs := []numberedPromptRef{
		{source: "text", kind: "text", text: "写实的夕阳", label: "文本1", ordinal: 1},
		{source: "image", kind: "image", label: "图1", ordinal: 2},
		{source: "video", kind: "video", label: "视频1", ordinal: 3},
		{source: "image", kind: "image", label: "图2", ordinal: 4},
	}
	got, err := compilePromptText("让 @[image] 按 @[video] 的构图，@[text]；再次使用 @[image]", refs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "让 [图1] 按 [视频1] 的构图，写实的夕阳；再次使用 [图1]") {
		t.Fatal(got)
	}
	for _, rule := range []string{"[图1] 对应附件第 2 项", "[视频1] 对应附件第 3 项", "[图2] 对应附件第 4 项"} {
		if !strings.Contains(got, rule) {
			t.Fatalf("missing %s: %s", rule, got)
		}
	}
	if _, err = compilePromptText("@[disconnected]", refs); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("dangling mention: %v", err)
	}
	text, err := compilePromptText("@[text]", refs[:1])
	if err != nil || text != "写实的夕阳" {
		t.Fatalf("text expansion: %s %v", text, err)
	}
}

func TestOrderedGenerationReferencesDraftThenInputsThenEdges(t *testing.T) {
	source, rev, draft := "source", "revision", "draft"
	g := graphState{Nodes: map[string]Node{source: {ID: source, ContentRevisionID: &rev}}, Inputs: map[string]NodeInput{
		"input":   {ID: "input", NodeID: "target", SourceNodeID: &source, Slot: "a", Ordinal: 0},
		"draft-z": {ID: "draft-z", NodeID: "target", DraftID: &draft, ContentRevisionID: &rev, Slot: "z", Ordinal: 0},
		"draft-a": {ID: "draft-a", NodeID: "target", DraftID: &draft, ContentRevisionID: &rev, Slot: "a", Ordinal: 1},
	}, Edges: map[string]Edge{"edge": {ID: "edge", SourceNodeID: source, TargetNodeID: "target", TargetPort: "a"}}}
	refs, readset, err := orderedGenerationRefs(g, "target")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 4 {
		t.Fatal(refs)
	}
	for i, want := range []string{"draft-a", "draft-z", "input", "edge"} {
		if refs[i].id != want {
			t.Fatal(refs)
		}
	}
	if !readset[source]["data"] || !readset["target"]["data"] {
		t.Fatal(readset)
	}
	n := g.Nodes[source]
	n.ContentRevisionID = nil
	g.Nodes[source] = n
	if _, _, err := orderedGenerationRefs(g, "target"); !errors.Is(err, ErrGraphIntegrity) {
		t.Fatalf("empty reference: %v", err)
	}
}
