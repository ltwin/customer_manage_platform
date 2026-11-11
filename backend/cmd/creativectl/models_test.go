package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
)

type fakeModels struct {
	result  creativescene.BuiltinModelImport
	missing []creativescene.BuiltinModelFile
	err     error
	// read is what Import was given to read files with.
	read creativescene.ReadBuiltinModel
}

func (f *fakeModels) Import(_ context.Context, read creativescene.ReadBuiltinModel) creativescene.BuiltinModelImport {
	f.read = read
	return f.result
}

func (f *fakeModels) Missing(context.Context) ([]creativescene.BuiltinModelFile, error) {
	return f.missing, f.err
}

func runModelsCommand(t *testing.T, store *fakeModels, args ...string) (int, map[string]any) {
	t.Helper()
	var out bytes.Buffer
	code := run(t.Context(), args, commandDeps{
		stdout: &out, stderr: &out,
		loadConfig: func() (config.Config, error) { return config.Config{}, nil },
		openModels: func(config.Config) (modelStore, error) { return store, nil },
	})
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("output %q: %v", out.String(), err)
	}
	return code, report
}

func TestModelsImportReadsTheDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.glb"), []byte("glb bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &fakeModels{result: creativescene.BuiltinModelImport{Imported: 1, Skipped: 1, Failed: []creativescene.BuiltinModelImportFailure{}}}
	code, r := runModelsCommand(t, store, "models", "import", "--dir", dir)
	if code != exitOK || r["status"] != "imported" || r["imported"] != 1.0 || r["skipped"] != 1.0 {
		t.Fatalf("%d %v", code, r)
	}
	// Files come from the directory, read one byte past the listed size.
	if body, err := store.read(t.Context(), creativescene.BuiltinModelFile{Name: "a.glb", Bytes: 3}); err != nil || string(body) != "glb " {
		t.Fatalf("read %q %v", body, err)
	}
	if _, err := store.read(t.Context(), creativescene.BuiltinModelFile{Name: "b.glb", Bytes: 3}); err == nil {
		t.Fatal("a file not in the directory read")
	}

	code, r = runModelsCommand(t, &fakeModels{result: creativescene.BuiltinModelImport{Failed: []creativescene.BuiltinModelImportFailure{{File: "a.glb", Model: "body_female@1", Reason: "sha256 与文件名不符"}}}}, "models", "import", "--dir", dir)
	if code != exitInternal || r["status"] != "incomplete" || r["failed"].([]any)[0].(map[string]any)["model"] != "body_female@1" {
		t.Fatalf("%d %v", code, r)
	}
	for _, args := range [][]string{{"models", "import"}, {"models", "import", "--dir", dir, "extra"}, {"models", "import", "--url", "x"}} {
		if code, _ := runModelsCommand(t, &fakeModels{}, args...); code != exitInvalidInput {
			t.Fatalf("%v: %d", args, code)
		}
	}
}

func TestModelsCheckSaysWhatIsMissing(t *testing.T) {
	code, r := runModelsCommand(t, &fakeModels{}, "models", "check")
	if code != exitOK || r["status"] != "ready" || len(r["missing"].([]any)) != 0 {
		t.Fatalf("%d %v", code, r)
	}
	code, r = runModelsCommand(t, &fakeModels{missing: []creativescene.BuiltinModelFile{{Name: "a.glb", Model: "body_female@1"}}}, "models", "check")
	if code != exitNotReady || r["status"] != "missing" || r["missing"].([]any)[0].(map[string]any)["model"] != "body_female@1" {
		t.Fatalf("%d %v", code, r)
	}
	code, r = runModelsCommand(t, &fakeModels{err: errors.New("denied")}, "models", "check")
	if code != exitInternal || r["status"] != "failed" {
		t.Fatalf("%d %v", code, r)
	}
	if code, _ := runModelsCommand(t, &fakeModels{}, "models", "check", "--dir", "x"); code != exitInvalidInput {
		t.Fatalf("an argument: %d", code)
	}
}
