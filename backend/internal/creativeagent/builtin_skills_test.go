package creativeagent

import (
	"io/fs"
	"os"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
)

// Every built-in package is synced into every deployment on start. One that
// names a tool this runtime does not register is hidden from the picker as
// unavailable, so a manifest that drifts from the registry has to fail here
// rather than ship as a skill nobody can see.
func TestBuiltInSkillPackagesOnlyNameRegisteredTools(t *testing.T) {
	root := os.DirFS("../../../deploy/creative-skills")
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, tool := range runtimeToolEntries() {
		registered[toolRef(tool.Key, tool.Version)] = true
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sub, err := fs.Sub(root, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		pkg, err := creativeskill.ReadPackage(sub)
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if ok, why := runnableHere(registered, pkg.Manifest.ToolAllowlist); !ok {
			t.Errorf("%s: %s", entry.Name(), why)
		}
	}
}
