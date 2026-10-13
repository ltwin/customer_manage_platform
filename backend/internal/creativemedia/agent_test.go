package creativemedia_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
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
		t.Fatal("an image that does not fit even as a preview was accepted")
	}
}

// An image larger than the model may receive is sent as a smaller JPEG copy
// instead of failing the run; the handle describes the copy's own bytes.
func TestAgentImageSendsAPreviewOfALargeImage(t *testing.T) {
	f := setup(t)
	// Noise compresses like a photograph, so the PNG is larger than its JPEG.
	src := image.NewRGBA(image.Rect(0, 0, 900, 450))
	seed := uint32(7)
	for i := range src.Pix {
		seed = seed*1664525 + 1013904223
		src.Pix[i] = uint8(seed >> 24)
		if i%4 == 3 {
			src.Pix[i] = 255
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	view := f.uploadFile(t, f.a, "参考.png", "image/png", "image", raw, creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "参考"}})
	asset, err := creativelibrary.GetAsset(t.Context(), f.a, view.Publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	limit := int64(len(raw)) - 1
	handle, err := f.svc.AgentImage(t.Context(), f.a, asset.ContentRevisionID, limit)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := handle.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, format, err := image.Decode(bytes.NewReader(data)); err != nil || format != "jpeg" || handle.MIME != "image/jpeg" {
		t.Fatalf("preview %s %v", format, err)
	}
	if int64(len(data)) != handle.ByteSize || handle.ByteSize > limit || fmt.Sprintf("sha256-%x", sha256.Sum256(data)) != handle.Digest {
		t.Fatal("preview handle does not describe its own bytes")
	}
}

// Sent chat images survive staged expiry and GC without a library entry.
func TestMessageReferenceKeepsStagedImageAfterExpiry(t *testing.T) {
	f := setup(t)
	upload := f.uploadFile(t, f.a, "会话截图.png", "image/png", "image", sample(t, "sample.png"), creativemedia.Target{Kind: "staged"})
	if upload.ContentRevisionID == nil {
		t.Fatal("staged upload has no revision")
	}
	id := *upload.ContentRevisionID
	if _, err := f.db.Exec(`INSERT INTO creative_message_content_refs(account_id,message_id,content_revision_id,role) VALUES('media-a','sent-message',$1,'input')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE creative_upload_candidates SET expires_at=clock_timestamp()-interval '30 days' WHERE content_revision_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	report, err := f.svc.CollectGarbage(t.Context(), f.a, creativemedia.GCDelete)
	if err != nil {
		t.Fatal(err)
	}
	if report.UnheldRevisions != 0 || report.ReclaimableBlobs != 0 {
		t.Fatalf("referenced image collected: %+v", report)
	}
	image, err := f.svc.AgentImage(t.Context(), f.a, id, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := image.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || len(data) == 0 {
		t.Fatalf("kept image bytes: %v", err)
	}
	if _, err := f.db.Exec(`DELETE FROM creative_message_content_refs WHERE account_id='media-a' AND content_revision_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	report, err = f.svc.CollectGarbage(t.Context(), f.a, creativemedia.GCDelete)
	if err != nil {
		t.Fatal(err)
	}
	if report.UnheldRevisions != 1 {
		t.Fatalf("unreferenced expired image retained: %+v", report)
	}
}
