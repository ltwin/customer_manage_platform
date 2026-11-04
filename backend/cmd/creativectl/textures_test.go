package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
)

type fakeTextures struct {
	result  creativescene.BuiltinImport
	missing []creativescene.BuiltinTextureFile
	err     error
}

func (f *fakeTextures) Import(context.Context, creativescene.FetchBuiltinTexture) creativescene.BuiltinImport {
	return f.result
}

func (f *fakeTextures) Missing(context.Context) ([]creativescene.BuiltinTextureFile, error) {
	return f.missing, f.err
}

func runTexturesCommand(t *testing.T, store *fakeTextures, args ...string) (int, map[string]any) {
	t.Helper()
	var out bytes.Buffer
	code := run(t.Context(), args, commandDeps{
		stdout: &out, stderr: &out,
		loadConfig:   func() (config.Config, error) { return config.Config{}, nil },
		openTextures: func(config.Config) (textureStore, error) { return store, nil },
	})
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("output %q: %v", out.String(), err)
	}
	return code, report
}

func TestTexturesImportReportsWhatItDid(t *testing.T) {
	code, r := runTexturesCommand(t, &fakeTextures{result: creativescene.BuiltinImport{Imported: 70, Skipped: 2, Failed: []creativescene.BuiltinImportFailure{}}}, "textures", "import")
	if code != exitOK || r["status"] != "imported" || r["imported"] != 70.0 || r["skipped"] != 2.0 {
		t.Fatalf("%d %v", code, r)
	}
	code, r = runTexturesCommand(t, &fakeTextures{result: creativescene.BuiltinImport{Imported: 1, Failed: []creativescene.BuiltinImportFailure{{File: "a.jpg", Material: "wood_floor@1", Reason: "HTTP 404"}}}}, "textures", "import")
	if code != exitInternal || r["status"] != "incomplete" || len(r["failed"].([]any)) != 1 {
		t.Fatalf("%d %v", code, r)
	}
}

func TestTexturesCheckSaysWhatIsMissing(t *testing.T) {
	code, r := runTexturesCommand(t, &fakeTextures{}, "textures", "check")
	if code != exitOK || r["status"] != "ready" || len(r["missing"].([]any)) != 0 {
		t.Fatalf("%d %v", code, r)
	}
	code, r = runTexturesCommand(t, &fakeTextures{missing: []creativescene.BuiltinTextureFile{{Name: "a.jpg", Material: "wood_floor@1"}}}, "textures", "check")
	if code != exitNotReady || r["status"] != "missing" || r["missing"].([]any)[0].(map[string]any)["material"] != "wood_floor@1" {
		t.Fatalf("%d %v", code, r)
	}
	code, r = runTexturesCommand(t, &fakeTextures{err: errors.New("denied")}, "textures", "check")
	if code != exitInternal || r["status"] != "failed" {
		t.Fatalf("%d %v", code, r)
	}
	if code, _ := runTexturesCommand(t, &fakeTextures{}, "textures", "check", "--all"); code != exitInvalidInput {
		t.Fatalf("an argument: %d", code)
	}
}
