package llmgateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func TestGenerationPolicyFileSourcePublication(t *testing.T) {
	d, target := policyFixture(t)
	path := filepath.Join(t.TempDir(), "model.v1.json")
	write := func() {
		t.Helper()
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	source, err := gw.NewGenerationPolicyFileSource(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	var catalog gw.GenerationPolicyCatalog
	rev, err := catalog.PublishFromSource(context.Background(), source, target, 0, now.Add(time.Hour), now)
	if err != nil || rev != 1 {
		t.Fatalf("file publication: %d %v", rev, err)
	}
	original, _, err := catalog.Snapshot(now)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.PublishFromSource(context.Background(), source, target, rev, now.Add(time.Hour), now); err == nil {
		t.Fatal("invalid file published")
	}
	current, revision, err := catalog.Snapshot(now)
	if err != nil || current != original || revision != 1 {
		t.Fatal("failed reload changed active policy")
	}
	d.Version = "policy-2"
	write()
	rev, err = catalog.PublishFromSource(context.Background(), source, target, rev, now.Add(time.Hour), now)
	if err != nil || rev != 2 {
		t.Fatalf("reload: %d %v", rev, err)
	}
	if original.Version() != "policy-1" {
		t.Fatal("reload mutated old policy")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.PublishFromSource(context.Background(), source, target, rev, now.Add(time.Hour), now); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
	current, revision, err = catalog.Snapshot(now)
	if err != nil || current.Version() != "policy-2" || revision != 2 {
		t.Fatal("missing file cleared active policy")
	}
}

func TestGenerationPolicyFileSourceBoundsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
	}{{"empty", 0}, {"oversized", (512 << 10) + 1}} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "policy.json")
			if err := os.WriteFile(path, make([]byte, tc.size), 0600); err != nil {
				t.Fatal(err)
			}
			source, err := gw.NewGenerationPolicyFileSource(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := source.Read(context.Background()); !errors.Is(err, gw.ErrValidation) {
				t.Fatalf("file bounds: %v", err)
			}
		})
	}
	if _, err := gw.NewGenerationPolicyFileSource(""); !errors.Is(err, gw.ErrValidation) {
		t.Fatalf("empty path: %v", err)
	}
	source, err := gw.NewGenerationPolicyFileSource(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Read(context.Background()); !errors.Is(err, gw.ErrValidation) {
		t.Fatalf("directory accepted: %v", err)
	}
	source, err = gw.NewGenerationPolicyFileSource(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Read(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %v", err)
	}
}

type policySourceFunc func(context.Context) ([]byte, error)

func (f policySourceFunc) Read(ctx context.Context) ([]byte, error) { return f(ctx) }
func TestGenerationPolicySourceCannotPublishAfterCancellation(t *testing.T) {
	d, target := policyFixture(t)
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	source := policySourceFunc(func(context.Context) ([]byte, error) { cancel(); return raw, nil })
	var catalog gw.GenerationPolicyCatalog
	now := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	if _, err := catalog.PublishFromSource(ctx, source, target, 0, now.Add(time.Hour), now); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled publication: %v", err)
	}
	if _, _, err := catalog.Snapshot(now); !errors.Is(err, gw.ErrCapability) {
		t.Fatalf("cancelled source activated: %v", err)
	}
}
