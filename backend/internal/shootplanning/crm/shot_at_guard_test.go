package crm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCRMPackageDoesNotReadOrderShotAt(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Clean(entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "shot_at") || strings.Contains(string(body), "ShotAt") {
			t.Fatalf("%s reads Order.shot_at", entry.Name())
		}
	}
}
