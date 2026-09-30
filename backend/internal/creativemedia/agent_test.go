package creativemedia_test

import (
	"crypto/sha256"
	"fmt"
	"io"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
)

func TestAgentImageReadsAuthorizedImmutableBytes(t *testing.T) {
	f := setup(t)
	view := f.uploadFile(t, f.a, "参考.png", "image/png", "image", sample(t, "sample.png"), creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "参考"}})
	asset, err := creativelibrary.GetAsset(t.Context(), f.a, view.Publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	image, err := f.svc.AgentImage(t.Context(), f.a, asset.ContentRevisionID, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := image.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) != image.ByteSize || fmt.Sprintf("sha256-%x", sha256.Sum256(data)) != image.Digest {
		t.Fatal("image handle does not describe the pinned bytes")
	}
	if _, err = f.svc.AgentImage(t.Context(), f.b, asset.ContentRevisionID, 2<<20); err == nil {
		t.Fatal("cross-account image leaked")
	}
	if _, err = f.svc.AgentImage(t.Context(), f.a, asset.ContentRevisionID, 1); err == nil {
		t.Fatal("oversize image accepted")
	}
}
