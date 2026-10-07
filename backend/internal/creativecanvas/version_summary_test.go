package creativecanvas

import (
	"encoding/json"
	"testing"
)

func TestSummarizeVersions(t *testing.T) {
	t.Parallel()
	gen := func(model string) json.RawMessage {
		raw, _ := json.Marshal(map[string]any{"prompt": map[string]any{"model_key": model}})
		return raw
	}
	id := func(s string) *string { return &s }
	if summarizeVersions(nil, nil) != nil {
		t.Fatal("no versions, no summary")
	}
	// version_no 有空洞（中间删过版本）时按位置计数。
	rows := []versionRow{{"v5", 5, gen("seedream")}, {"v1", 1, gen("banana")}, {"v3", 3, json.RawMessage(`{}`)}}
	for _, tc := range []struct {
		name     string
		selected *string
		ordinal  int
		model    string
	}{
		{"latest when unselected", nil, 3, "seedream"},
		{"pinned first", id("v1"), 1, "banana"},
		{"non-generated main has no model", id("v3"), 2, ""},
		{"stale selection falls back to latest", id("gone"), 3, "seedream"},
	} {
		s := summarizeVersions(append([]versionRow(nil), rows...), tc.selected)
		if s.Count != 3 || s.MainOrdinal != tc.ordinal {
			t.Fatal(tc.name, s)
		}
		if got := ""; s.MainModelKey != nil {
			got = *s.MainModelKey
			if got != tc.model {
				t.Fatal(tc.name, got)
			}
		} else if tc.model != "" {
			t.Fatal(tc.name, "missing model")
		}
	}
	if s := summarizeVersions([]versionRow{{"only", 1, json.RawMessage(`not json`)}}, nil); s.Count != 1 || s.MainModelKey != nil {
		t.Fatal("bad provenance is treated as no model", s)
	}
}

func TestDefaultNodeSizeAndCopyTitle(t *testing.T) {
	t.Parallel()
	for key, want := range map[string][2]float64{"core.text": {300, 220}, "core.image": {300, 300}, "core.video": {300, 300}, "core.link": {300, 140}, "core.audio": {300, 120}, "core.group": {300, 220}} {
		if w, h := defaultNodeSize(key); w != want[0] || h != want[1] {
			t.Fatal(key, w, h)
		}
	}
	if copyTitle("") != "副本" || copyTitle("晨光") != "晨光 副本" {
		t.Fatal("copy title")
	}
}

func TestValidResize(t *testing.T) {
	t.Parallel()
	content := "rev"
	image := Node{TypeKey: "core.image", ContentRevisionID: &content}
	if !validResize(image, 160, 4) {
		t.Fatal("filled media height follows content ratio; only width is checked")
	}
	if validResize(Node{TypeKey: "core.image"}, 160, 99) {
		t.Fatal("empty media keeps the height floor")
	}
	if validResize(Node{TypeKey: "core.group"}, 400, 400) {
		t.Fatal("groups cannot be resized by hand")
	}
	if validResize(Node{TypeKey: "core.audio"}, 300, 99) || !validResize(Node{TypeKey: "core.audio"}, 300, 100) {
		t.Fatal("audio height floor")
	}
}
