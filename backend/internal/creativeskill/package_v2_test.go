package creativeskill_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
)

func TestStandardSkillPackageWithoutManifest(t *testing.T) {
	pkg, err := creativeskill.ReadPackage(fstest.MapFS{
		"SKILL.md":            {Data: []byte("---\nname: plain-method\ndescription: A reusable method\nlicense: MIT\nallowed-tools: Bash\nmetadata:\n  author: example\n---\n# Method\nRead references/guide.md.")},
		"references/guide.md": {Data: []byte("Reference text")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Slug != "plain-method" || pkg.Manifest.ManifestSchemaVersion != 2 || len(pkg.Manifest.Dependencies()) != 0 || strings.Contains(pkg.Instructions, "allowed-tools") {
		t.Fatalf("unexpected package: %+v", pkg)
	}
	if pkg.Manifest.Metadata["license"] != "MIT" || len(pkg.Resources) != 1 {
		t.Fatalf("lost metadata/resources: %+v", pkg)
	}
}

func TestStandardSkillPackageRefusesUnsupportedOrAmbiguousContent(t *testing.T) {
	for _, tc := range []struct {
		name, header, path string
		data               []byte
	}{
		{"missing description", "name: method", "", nil},
		{"duplicate identity", "name: method\nname: other\ndescription: example", "", nil},
		{"script", "name: method\ndescription: example", "scripts/run.py", []byte("print('hi')")},
		{"nul binary", "name: method\ndescription: example", "assets/data.bin", []byte{0, 1, 2}},
		{"binary", "name: method\ndescription: example", "assets/picture.png", []byte{0xff, 0, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := fstest.MapFS{"SKILL.md": {Data: []byte("---\n" + tc.header + "\n---\nInstructions")}}
			if tc.path != "" {
				files[tc.path] = &fstest.MapFile{Data: tc.data}
			}
			if _, err := creativeskill.ReadPackage(files); err == nil {
				t.Fatal("accepted unsupported package")
			}
		})
	}
}

func TestStandardPackagePublishesLongDescriptionAndPreservesV2(t *testing.T) {
	f := setup(t)
	pkg, err := creativeskill.ReadPackage(fstest.MapFS{"SKILL.md": {Data: []byte("---\nname: 3d-method\ndescription: " + strings.Repeat("长", 1024) + "\n---\n方法正文")}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.svc.Publish(t.Context(), f.a, pkg, creativeskill.PublishIntent{OperationID: "00112233-4455-4677-8899-aabbccddeeff", Origin: "account", Activate: true})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.svc.ResolveVersion(t.Context(), f.a, first.SkillID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Manifest.ManifestSchemaVersion != 2 || len(snapshot.Manifest.Dependencies()) != 0 || snapshot.Description != pkg.Description {
		t.Fatalf("snapshot drift: %+v", snapshot)
	}
	again, err := f.svc.Publish(t.Context(), f.a, pkg, creativeskill.PublishIntent{OperationID: "00112233-4455-4677-8899-aabbccddeeff", Origin: "account", Activate: true})
	if err != nil || again.ID != first.ID {
		t.Fatalf("replay: %+v %v", again, err)
	}
}
