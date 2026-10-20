package creativemedia_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
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

// A model is seen through its PNG thumbnail; its GLB (here a blob with no
// bytes behind it) is never opened.
func TestAgentImageOfAModelIsItsThumbnail(t *testing.T) {
	f := setup(t)
	view := f.uploadFile(t, f.a, "缩略图.png", "image/png", "image", sample(t, "sample.png"), creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "缩略图"}})
	picture, err := creativelibrary.GetAsset(t.Context(), f.a, view.Publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	thumb := picture.Content.Media[0]
	if _, err := f.db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,state,verified_at)
 VALUES('ccbl_glb','media-a','local','creative-v2/media-a/blobs/missing','v1','sha256-'||repeat('1',64),100,'model/gltf-binary','ready',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	var model creativecontent.Revision
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		d := creativecontent.Draft{Kind: "model", Payload: creativecontent.Payload{Model: []byte(`{"format":"glb","manifest":{},"generator":{"code":"x"}}`)}}
		var err error
		model, err = creativecontent.WriteModelAndRetain(t.Context(), tx, d, "", []creativecontent.ObjectBinding{{Role: "original", BlobID: "ccbl_glb"}, {Role: "thumbnail", BlobID: thumb.BlobID}},
			func(r creativecontent.Revision) error {
				_, err := creativelibrary.SaveRevisionInTx(t.Context(), tx, r, creativelibrary.ImportTarget{Title: "柱"})
				return err
			})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	image, err := f.svc.AgentImage(t.Context(), f.a, model.ID, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if image.MIME != "image/png" || image.ByteSize != thumb.ByteSize {
		t.Fatalf("model image %+v", image)
	}
	reader, err := image.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || fmt.Sprintf("sha256-%x", sha256.Sum256(data)) != image.Digest {
		t.Fatalf("thumbnail bytes: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("thumbnail is not a PNG: %v", err)
	}

	// Other kinds still have no image to give.
	body := "文字"
	text := must[creativelibrary.AssetResult](t)(creativelibrary.CreateAsset(t.Context(), f.a, command(t, creativelibrary.CreateAssetInput{Title: "文字", Content: creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}, Rights: rights()}})))
	if _, err := f.svc.AgentImage(t.Context(), f.a, text.ContentRevisionID, 2<<20); !errors.Is(err, llmgateway.ErrCapability) {
		t.Fatalf("text image: %v", err)
	}
}
