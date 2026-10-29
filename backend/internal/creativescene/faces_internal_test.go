package creativescene

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// data: 模型内容「面级材质分配」, the server's share: shape and limits only
// (scene-face-materials 3.1).

const okTopology = "t1-9f86d081884c7d65"

func facesFrom(t *testing.T, raw string) *Faces {
	t.Helper()
	var f Faces
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return &f
}

func TestFacesRoundTrip(t *testing.T) {
	raw := `{"meshes":[{"path":[["part:capital",0],["",2]],"primitive":1,"topology":"` + okTopology + `","assign":{"brick":[[0,12],[40,44]],"stone":[[12,16]]}}],"hints":{"brick":"brick"}}`
	f := facesFrom(t, raw)
	if err := checkFaces(f, []string{"stone"}, false); err != nil {
		t.Fatalf("a valid assignment: %v", err)
	}
	if f.Meshes[0].Path[0] != (PathStep{Name: "part:capital", N: 0}) {
		t.Errorf("path = %+v", f.Meshes[0].Path)
	}
	back, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != raw {
		t.Errorf("round trip\n got %s\nwant %s", back, raw)
	}
	if err := checkFaces(nil, nil, false); err != nil {
		t.Errorf("no assignment is valid: %v", err)
	}
}

func TestFacesShapeErrors(t *testing.T) {
	mesh := func(path, assign string) string {
		return `{"path":` + path + `,"primitive":0,"topology":"` + okTopology + `","assign":` + assign + `}`
	}
	one := func(m string) string { return `{"meshes":[` + m + `]}` }
	many := func(n int, assign string) string {
		var ms []string
		for i := range n {
			ms = append(ms, mesh(fmt.Sprintf(`[["",%d]]`, i), assign))
		}
		return `{"meshes":[` + strings.Join(ms, ",") + `]}`
	}
	var manyRanges []string
	for i := range 8193 {
		manyRanges = append(manyRanges, fmt.Sprintf("[%d,%d]", 2*i, 2*i+1))
	}
	var manySlots []string
	for i := range 32 {
		manySlots = append(manySlots, fmt.Sprintf(`"s%d":[[%d,%d]]`, i, i, i+1))
	}
	cases := map[string]string{
		"no meshes":           `{"meshes":[]}`,
		"too many meshes":     many(2001, `{"a":[[0,1]]}`),
		"empty path":          one(mesh(`[]`, `{"a":[[0,1]]}`)),
		"path too deep":       one(mesh("["+strings.TrimSuffix(strings.Repeat(`["",0],`, 65), ",")+"]", `{"a":[[0,1]]}`)),
		"negative step":       one(mesh(`[["",-1]]`, `{"a":[[0,1]]}`)),
		"long node name":      one(mesh(`[["`+strings.Repeat("x", 65)+`",0]]`, `{"a":[[0,1]]}`)),
		"primitive too large": `{"meshes":[{"path":[["",0]],"primitive":256,"topology":"` + okTopology + `","assign":{"a":[[0,1]]}}]}`,
		"same mesh twice":     `{"meshes":[` + mesh(`[["",0]]`, `{"a":[[0,1]]}`) + `,` + mesh(`[["",0]]`, `{"b":[[2,3]]}`) + `]}`,
		"bad topology":        `{"meshes":[{"path":[["",0]],"primitive":0,"topology":"t2-9f86d081884c7d65","assign":{"a":[[0,1]]}}]}`,
		"empty assign":        one(mesh(`[["",0]]`, `{}`)),
		"bad slot id":         one(mesh(`[["",0]]`, `{"a b":[[0,1]]}`)),
		"slot without ranges": one(mesh(`[["",0]]`, `{"a":[]}`)),
		"range of three":      one(mesh(`[["",0]]`, `{"a":[[0,1,2]]}`)),
		"empty range":         one(mesh(`[["",0]]`, `{"a":[[3,3]]}`)),
		"range past limit":    one(mesh(`[["",0]]`, `{"a":[[0,300001]]}`)),
		"unsorted ranges":     one(mesh(`[["",0]]`, `{"a":[[4,5],[0,1]]}`)),
		"overlap across":      one(mesh(`[["",0]]`, `{"a":[[0,4]],"b":[[3,6]]}`)),
		"too many ranges":     one(mesh(`[["",0]]`, `{"a":[`+strings.Join(manyRanges, ",")+`]}`)),
		"hint for unused":     `{"meshes":[` + mesh(`[["",0]]`, `{"a":[[0,1]]}`) + `],"hints":{"b":"x"}}`,
		"long hint":           `{"meshes":[` + mesh(`[["",0]]`, `{"a":[[0,1]]}`) + `],"hints":{"a":"` + strings.Repeat("x", 41) + `"}}`,
		"too many slots":      one(mesh(`[["",0]]`, `{`+strings.Join(manySlots, ",")+`}`)),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if err := checkFaces(facesFrom(t, raw), []string{"stone"}, false); err == nil {
				t.Error("accepted")
			}
		})
	}
	// 31 own slots plus the GLB's one is the most there can be.
	if err := checkFaces(facesFrom(t, one(mesh(`[["",0]]`, `{`+strings.Join(manySlots[:31], ",")+`}`))), []string{"stone"}, false); err != nil {
		t.Errorf("32 slots in all: %v", err)
	}
	// Over 128 KiB, under every count limit: 40 meshes of 64 long-named steps.
	long := strings.Repeat("x", 64)
	var meshes []string
	for i := range 40 {
		steps := []string{fmt.Sprintf(`["",%d]`, i)}
		for range 63 {
			steps = append(steps, `["`+long+`",0]`)
		}
		meshes = append(meshes, `{"path":[`+strings.Join(steps, ",")+`],"primitive":0,"topology":"`+okTopology+`","assign":{"a":[[0,1]]}}`)
	}
	big := facesFrom(t, `{"meshes":[`+strings.Join(meshes, ",")+`]}`)
	if err := checkFaces(big, nil, false); err == nil || !strings.Contains(err.Error(), "128 KiB") {
		t.Errorf("an assignment over 128 KiB: %v", err)
	}
}

func TestFacesOnBuiltins(t *testing.T) {
	ok := `{"meshes":[{"path":[["",0]],"primitive":0,"topology":"` + okTopology + `","assign":{"brick":[[0,2]]}}]}`
	if err := checkFaces(facesFrom(t, ok), []string{"main"}, true); err != nil {
		t.Fatalf("a builtin's assignment: %v", err)
	}
	var ranges []string
	for i := range 257 {
		ranges = append(ranges, fmt.Sprintf("[%d,%d]", 2*i, 2*i+1))
	}
	for name, raw := range map[string]string{
		"other path":      strings.Replace(ok, `[["",0]]`, `[["box",0]]`, 1),
		"other primitive": strings.Replace(ok, `"primitive":0`, `"primitive":1`, 1),
		"two meshes":      `{"meshes":[` + ok[11:len(ok)-2] + `,` + strings.Replace(ok[11:len(ok)-2], `"primitive":0`, `"primitive":1`, 1) + `]}`,
		"too many ranges": strings.Replace(ok, `[[0,2]]`, "["+strings.Join(ranges, ",")+"]", 1),
	} {
		if err := checkFaces(facesFrom(t, raw), []string{"main"}, true); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEffectiveSlotIDs(t *testing.T) {
	f := facesFrom(t, `{"meshes":[{"path":[["a",0]],"primitive":0,"topology":"`+okTopology+`","assign":{"stone":[[0,1]],"face_2":[[1,2]]}},{"path":[["b",0]],"primitive":0,"topology":"`+okTopology+`","assign":{"face_1":[[0,1]]}}]}`)
	got := effectiveSlotIDs([]string{"stone", "trim"}, f)
	if strings.Join(got, ",") != "stone,trim,face_1,face_2" {
		t.Errorf("effective slots = %v", got)
	}
	if got := effectiveSlotIDs([]string{"main"}, nil); strings.Join(got, ",") != "main" {
		t.Errorf("without faces = %v", got)
	}
}
