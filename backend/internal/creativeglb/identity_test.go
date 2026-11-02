package creativeglb

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Mesh identities (data: 模型内容「面级材质分配」): a primitive is named by
// the path of its node from the scene root, each step the node's name and its
// place among same-named siblings, plus the primitive's index. The server never
// reads them from a GLB; this test pins the rule so that
// frontend/scripts/creative-scene-model.test.ts, which derives them from
// GLTFLoader, can be checked against the same file.

type identityRow struct {
	Path      [][2]any `json:"path"`
	Primitive int      `json:"primitive"`
	Triangles int      `json:"triangles"`
}

func primitiveIdentities(t *testing.T, glb []byte) []identityRow {
	t.Helper()
	jsonChunk, _, err := splitContainer(glb)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	var doc struct {
		Scene  int `json:"scene"`
		Scenes []struct {
			Nodes []int `json:"nodes"`
		} `json:"scenes"`
		Nodes []struct {
			Name     string `json:"name"`
			Children []int  `json:"children"`
			Mesh     *int   `json:"mesh"`
		} `json:"nodes"`
		Meshes []struct {
			Primitives []struct {
				Indices    *int           `json:"indices"`
				Attributes map[string]int `json:"attributes"`
			} `json:"primitives"`
		} `json:"meshes"`
		Accessors []struct {
			Count int `json:"count"`
		} `json:"accessors"`
	}
	if err := json.Unmarshal(jsonChunk, &doc); err != nil {
		t.Fatalf("json: %v", err)
	}
	var rows []identityRow
	var walk func(siblings []int, parent [][2]any)
	walk = func(siblings []int, parent [][2]any) {
		seen := map[string]int{}
		for _, index := range siblings {
			node := doc.Nodes[index]
			path := append(append([][2]any(nil), parent...), [2]any{node.Name, seen[node.Name]})
			seen[node.Name]++
			if node.Mesh != nil {
				for j, p := range doc.Meshes[*node.Mesh].Primitives {
					count := doc.Accessors[p.Attributes["POSITION"]].Count
					if p.Indices != nil {
						count = doc.Accessors[*p.Indices].Count
					}
					rows = append(rows, identityRow{Path: path, Primitive: j, Triangles: count / 3})
				}
			}
			walk(node.Children, path)
		}
	}
	walk(doc.Scenes[doc.Scene].Nodes, nil)
	return rows
}

func TestIdentitySamples(t *testing.T) {
	glb, err := os.ReadFile(filepath.Join("testdata", "identity-samples.glb"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	m, err := Inspect(glb, DefaultLimits())
	if err != nil {
		t.Fatalf("the sandbox's export must pass the server's checks: %v", err)
	}
	rows := primitiveIdentities(t, glb)
	total := 0
	for _, r := range rows {
		total += r.Triangles
	}
	if total != m.Triangles {
		t.Fatalf("identities cover %d triangles, the manifest counts %d", total, m.Triangles)
	}
	got, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	golden := filepath.Join("testdata", "identity-samples.json")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("testdata/identity-samples.json is stale; run go test ./internal/creativeglb -run TestIdentitySamples -update\n%s", got)
	}
}
