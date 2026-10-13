package creativeimageedit

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
)

func TestSourceStageIsOwnedByStartupSweep(t *testing.T) {
	root := t.TempDir()
	body := []byte("verified source bytes")
	src := creativemedia.EditSource{Size: int64(len(body)), SHA256: fmt.Sprintf("sha256-%x", sha256.Sum256(body))}
	path, err := stageSourceBody(bytes.NewReader(body), src, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("staged source: %q %v", got, err)
	}
	// A killed process leaves the file behind. Startup must remove it while
	// preserving unrelated entries in the configured root.
	keep := filepath.Join(root, "keep")
	if err := os.WriteFile(keep, body, 0o600); err != nil {
		t.Fatal(err)
	}
	codec := &Codec{cfg: CodecConfig{TempRoot: root}}
	if err := codec.SweepTemp(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("startup left source behind: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("startup removed an unrelated file: %v", err)
	}
}

func TestSourceStageFailureRemovesPartialFile(t *testing.T) {
	for _, size := range []int64{2, 3, 4} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			root := t.TempDir()
			// Every case has either the wrong size or the wrong digest.
			if _, err := stageSourceBody(bytes.NewBufferString("abc"), creativemedia.EditSource{Size: size, SHA256: "wrong"}, root); err == nil {
				t.Fatal("invalid source accepted")
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed staging left files: %v %v", entries, err)
			}
		})
	}
}
