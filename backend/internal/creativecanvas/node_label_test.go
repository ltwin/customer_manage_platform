package creativecanvas_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
)

// V6-003：新建默认尺寸与前端一致；复制只给根节点标题加「 副本」。
func TestNodeDefaultSizeAndCopyTitle(t *testing.T) {
	_, scope, _ := setup(t)
	p := project(t, scope)
	id := func() string { return "cwnode_" + uuid.NewString() }
	text, image, video, link, audio, group, child := id(), id(), id(), id(), id(), id(), id()
	long := strings.Repeat("长", 200)
	graph(t, scope, p.CanvasID,
		creativecanvas.Action{Type: "add_node", NodeID: text, TypeKey: "core.text", Title: long},
		creativecanvas.Action{Type: "add_node", NodeID: image, TypeKey: "core.image", Title: "晨光"},
		creativecanvas.Action{Type: "add_node", NodeID: video, TypeKey: "core.video"},
		creativecanvas.Action{Type: "add_node", NodeID: link, TypeKey: "core.link"},
		creativecanvas.Action{Type: "add_node", NodeID: audio, TypeKey: "core.audio"},
		creativecanvas.Action{Type: "add_node", NodeID: group, TypeKey: "core.group", Title: "参考组"},
	)
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "add_node", NodeID: child, TypeKey: "core.text", Title: "组内", ParentID: &group})
	c, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		w, h float64
	}{{text, 300, 220}, {image, 300, 300}, {video, 300, 300}, {link, 300, 140}, {audio, 300, 120}} {
		n := findNode(t, c, tc.id)
		if n.Width != tc.w || n.Height != tc.h {
			t.Fatalf("%s default size %vx%v, want %vx%v", n.TypeKey, n.Width, n.Height, tc.w, tc.h)
		}
	}
	src := findNode(t, c, image)
	r := graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "duplicate_selection", NodeIDs: []string{image, text, video, group}, DX: 40, DY: 40})
	c, _ = creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	copied := findNode(t, c, r.IDMapping[image])
	if copied.Title != "晨光 副本" || copied.X != src.X+40 || copied.Y != src.Y+40 {
		t.Fatal("root copy title/offset", copied.Title, copied.X, copied.Y)
	}
	if got := findNode(t, c, r.IDMapping[video]).Title; got != "副本" {
		t.Fatal("untitled root copy", got)
	}
	if got := findNode(t, c, r.IDMapping[group]).Title; got != "参考组 副本" {
		t.Fatal("group copy", got)
	}
	if got := findNode(t, c, r.IDMapping[child]).Title; got != "组内" {
		t.Fatal("child copy title must not change", got)
	}
	if got := findNode(t, c, r.IDMapping[text]).Title; utf8.RuneCountInString(got) != 200 || !strings.HasSuffix(got, " 副本") {
		t.Fatal("long title copy must stay within 200 runes with full suffix", utf8.RuneCountInString(got))
	}
}

// V6-003：快照带版本汇总；主版本自动时取最新，固定后取选定版本。
func TestNodeVersionSummary(t *testing.T) {
	_, scope, runtime, svc, p, ids := executionFixture(t)
	run := func() {
		e, cmd := requestExecution(t, scope, runtime, svc, p, ids[2])
		raw, _ := json.Marshal(creativecanvas.ExecutionTarget{CanvasID: p.CanvasID, ExecutionID: e.ID})
		if err := svc.Work(t.Context(), scope, jobs.Request{Kind: "canvas.node_execution", OperationID: cmd.OperationID, CreatedAt: time.Now(), Payload: raw}); err != nil {
			t.Fatal(err)
		}
	}
	summary := func(id string) *creativecanvas.VersionSummary {
		c, err := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
		if err != nil {
			t.Fatal(err)
		}
		return findNode(t, c, id).Versions
	}
	if s := summary(ids[2]); s != nil {
		t.Fatal("node without versions has a summary", s)
	}
	if s := summary(ids[0]); s != nil {
		t.Fatal("authored content is not a version", s)
	}
	run()
	if s := summary(ids[2]); s == nil || s.Count != 1 || s.MainOrdinal != 1 || s.MainModelKey != nil {
		t.Fatal("one version", s)
	}
	run()
	if s := summary(ids[2]); s == nil || s.Count != 2 || s.MainOrdinal != 2 {
		t.Fatal("latest is main by default", s)
	}
	versions, err := creativecanvas.ListNodeVersions(t.Context(), scope, p.CanvasID, ids[2])
	if err != nil || len(versions.Items) != 2 {
		t.Fatal(versions, err)
	}
	first := versions.Items[len(versions.Items)-1]
	graph(t, scope, p.CanvasID, creativecanvas.Action{Type: "select_version", NodeID: ids[2], VersionID: first.ID})
	if s := summary(ids[2]); s == nil || s.Count != 2 || s.MainOrdinal != 1 {
		t.Fatal("pinned main version", s)
	}
	c, _ := creativecanvas.GetCanvas(t.Context(), scope, p.CanvasID)
	raw, err := json.Marshal(findNode(t, c, ids[2]))
	if err != nil || !strings.Contains(string(raw), `"version_summary":{"count":2,"main_ordinal":1,"main_model_key":null}`) {
		t.Fatal("wire shape", string(raw), err)
	}
}
