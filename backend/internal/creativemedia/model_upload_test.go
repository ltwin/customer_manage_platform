package creativemedia_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/creativestaging"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func glbFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "creativeglb", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (f *fixture) stagedRevision(t *testing.T, uploadID string) creativecontent.Revision {
	t.Helper()
	var r creativecontent.Revision
	err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		s, err := creativestaging.Lock(t.Context(), tx, uploadID)
		if err != nil {
			return err
		}
		r, err = creativecontent.RequireUsable(t.Context(), tx, s.ContentRevisionID, "display")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// A GLB upload is verified against the static subset by the worker and lands
// as the staged form of a model, carrying the manifest the server computed.
func TestModelUploadIsVerifiedIntoStagedForm(t *testing.T) {
	f := setup(t)
	staged := creativemedia.Target{Kind: "staged"}
	glb := glbFixture(t, "basic.glb")
	up := f.uploadFile(t, f.a, "柱子.glb", "model/gltf-binary", "model", glb, staged)
	if up.State != "ready" || up.Binding == nil || up.Binding.Status != "staged" {
		t.Fatalf("model upload %+v", up)
	}
	r := f.stagedRevision(t, up.ID)
	if r.Kind != "model" || len(r.Media) != 1 || r.Media[0].Role != "original" || r.Media[0].Mime != "model/gltf-binary" {
		t.Fatalf("staged revision %+v", r)
	}
	var payload struct {
		Format    string               `json:"format"`
		Manifest  creativeglb.Manifest `json:"manifest"`
		Generator json.RawMessage      `json:"generator"`
	}
	if err := json.Unmarshal(r.Payload.Model, &payload); err != nil {
		t.Fatal(err)
	}
	want, err := creativeglb.Inspect(glb, creativeglb.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if payload.Format != "glb" || payload.Generator != nil || !payload.Manifest.Matches(want) || payload.Manifest.Triangles != 12 {
		t.Fatalf("staged payload %s", r.Payload.Model)
	}

	// Outside the subset the upload fails as unsupported media.
	bad := f.uploadFile(t, f.a, "篡改.glb", "model/gltf-binary", "model", glbFixture(t, "bad-bounds-mismatch.glb"), staged)
	if bad.State != "failed" || bad.ErrorCode != "media_unsupported" {
		t.Fatalf("tampered GLB %+v", bad)
	}
	// An image disguised as a model is sniffed and refused.
	fake := f.uploadFile(t, f.a, "假.glb", "model/gltf-binary", "model", sample(t, "sample.png"), staged)
	if fake.State != "failed" || fake.ErrorCode != "media_unsupported" {
		t.Fatalf("png as model %+v", fake)
	}

	// Models only go to staged targets and are never offered to file pickers.
	p := project(t, f.a)
	for name, target := range map[string]creativemedia.Target{
		"asset": {Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "模型"}},
		"node":  {Kind: "node", Node: &creativemedia.NodeTarget{CanvasID: p.CanvasID, NodeID: "cwnode_x", ExpectedDataRevision: 1}},
	} {
		_, err := f.svc.CreateUpload(t.Context(), f.a, command(t, creativemedia.CreateUploadInput{FileName: "m.glb", Kind: "model", Mime: "model/gltf-binary", Size: int64(len(glb)), Rights: rights(), Target: target}))
		if !errors.Is(err, creativeops.ErrValidation) {
			t.Errorf("model to %s: %v", name, err)
		}
	}
	if _, err := f.svc.CreateUpload(t.Context(), f.a, command(t, creativemedia.CreateUploadInput{FileName: "m.glb", Kind: "model", Mime: "image/png", Size: 10, Rights: rights(), Target: staged})); !errors.Is(err, creativemedia.ErrUnsupported) {
		t.Fatalf("model kind with png mime: %v", err)
	}
	for _, format := range f.svc.Capabilities().Formats {
		if format.Kind == "model" {
			t.Fatal("capabilities offer models to file pickers")
		}
	}

	// A staged model can only be consumed by a scene command, never adopted.
	var candidateID string
	var revision int64
	if err := f.db.QueryRow(`SELECT id,revision FROM creative_upload_candidates WHERE upload_id=$1`, up.ID).Scan(&candidateID, &revision); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AdoptCandidate(t.Context(), f.a, command(t, creativemedia.CandidateInput{CandidateID: candidateID, CandidateRevision: creativeops.Revision(revision), Target: &creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "模型"}}})); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("adopt model candidate: %v", err)
	}
}

// commit_model reuses the staged blobs in a complete model with a thumbnail.
// The thumbnail is readable through a ticket; consumed staged revisions are
// reclaimed while the complete model holds the blobs, and the blobs follow once
// the document history that referenced the model is gone.
func TestModelThumbnailTicketAndGC(t *testing.T) {
	f := setup(t)
	staged := creativemedia.Target{Kind: "staged"}
	png := sample(t, "sample.png")
	glbUp := f.uploadFile(t, f.a, "m.glb", "model/gltf-binary", "model", glbFixture(t, "basic.glb"), staged)
	pngUp := f.uploadFile(t, f.a, "m.png", "image/png", "image", png, staged)
	stagedGLB, stagedPNG := f.stagedRevision(t, glbUp.ID), f.stagedRevision(t, pngUp.ID)
	if _, err := f.svc.IssueTicket(t.Context(), f.a, creativemedia.TicketInput{ContentRevisionID: stagedGLB.ID, Role: "thumbnail", Purpose: "display"}); !errors.Is(err, creativecontent.ErrNotFound) {
		t.Fatalf("staged model has no thumbnail: %v", err)
	}
	// A staged image shows before anything adopts it: the assistant's
	// screenshots stay staged. Small ones have no display rendition.
	if ticket, err := f.svc.IssueTicket(t.Context(), f.a, creativemedia.TicketInput{ContentRevisionID: stagedPNG.ID, Role: "original", Purpose: "display"}); err != nil || ticket.Mime != "image/png" {
		t.Fatalf("staged image ticket %+v %v", ticket, err)
	}

	var model creativecontent.Revision
	err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		glb, err := creativestaging.Lock(t.Context(), tx, glbUp.ID)
		if err != nil {
			return err
		}
		thumb, err := creativestaging.Lock(t.Context(), tx, pngUp.ID)
		if err != nil {
			return err
		}
		draft := creativecontent.Draft{Kind: "model", DeclarationID: stagedGLB.DeclarationID, Payload: creativecontent.Payload{Model: json.RawMessage(`{"format":"glb","generator":{},"manifest":{}}`)}}
		objects := []creativecontent.ObjectBinding{{Role: "original", BlobID: stagedGLB.Media[0].BlobID}, {Role: "thumbnail", BlobID: stagedPNG.Media[0].BlobID}}
		model, err = creativecontent.WriteModelAndRetain(t.Context(), tx, draft, "", objects, func(r creativecontent.Revision) error {
			return tx.Insert(t.Context(), "creative_document_content_refs", []string{"document_id", "revision", "content_revision_id"}, "cwdoc_scene", int64(2), r.ID)
		})
		if err != nil {
			return err
		}
		if err := creativestaging.Consume(t.Context(), tx, glb, "cwdoc_scene", 2); err != nil {
			return err
		}
		return creativestaging.Consume(t.Context(), tx, thumb, "cwdoc_scene", 2)
	})
	if err != nil {
		t.Fatal(err)
	}
	var kind, target string
	if err := f.db.QueryRow(`SELECT state,adopted_target_kind FROM creative_upload_candidates WHERE upload_id=$1`, glbUp.ID).Scan(&kind, &target); err != nil || kind != "applied" || target != "document" {
		t.Fatalf("consumed candidate %s %s %v", kind, target, err)
	}
	if view, _ := f.svc.GetUpload(t.Context(), f.a, glbUp.ID); view.Binding == nil || view.Binding.Status != "applied" {
		t.Fatalf("consumed upload binding %+v", view.Binding)
	}

	ticket, err := f.svc.IssueTicket(t.Context(), f.a, creativemedia.TicketInput{ContentRevisionID: model.ID, Role: "thumbnail", Purpose: "display"})
	if err != nil || ticket.Mime != "image/png" {
		t.Fatalf("thumbnail ticket %+v %v", ticket, err)
	}
	u, err := url.Parse(ticket.URL)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := f.svc.Open(t.Context(), func(string) store.AccountScope { return f.a }, u.Query().Get("ticket"), model.ID, "thumbnail", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(stream.Body)
	_ = stream.Body.Close()
	if err != nil || !bytes.Equal(body, png) {
		t.Fatal("thumbnail bytes", err)
	}
	if _, err := f.svc.IssueTicket(t.Context(), f.b, creativemedia.TicketInput{ContentRevisionID: model.ID, Role: "thumbnail", Purpose: "display"}); err == nil {
		t.Fatal("thumbnail ticket across accounts")
	}

	// The consumed staged revisions are reclaimed; the complete model keeps
	// both blobs alive.
	if r := f.collect(t, creativemedia.GCDelete); r.UnheldRevisions != 2 {
		t.Fatalf("staged revisions not reclaimed: %+v", r)
	}
	revisionState := func(id string) string {
		var state string
		if err := f.db.QueryRow(`SELECT state FROM creative_content_revisions WHERE id=$1`, id).Scan(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	blobState := func(id string) string {
		var state string
		if err := f.db.QueryRow(`SELECT state FROM creative_blobs WHERE id=$1`, id).Scan(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	if revisionState(stagedGLB.ID) != "deleted" || revisionState(stagedPNG.ID) != "deleted" || revisionState(model.ID) != "ready" {
		t.Fatal("GC removed the wrong revisions")
	}
	for _, blob := range []string{stagedGLB.Media[0].BlobID, stagedPNG.Media[0].BlobID} {
		if blobState(blob) != "ready" {
			t.Fatalf("blob %s of a referenced model was reclaimed", blob)
		}
	}

	// Once no document revision references the model, it goes as media does.
	if _, err := f.db.Exec(`DELETE FROM creative_document_content_refs WHERE account_id='media-a'; UPDATE creative_blob_read_pins SET expires_at=clock_timestamp()-interval '1 hour' WHERE account_id='media-a'`); err != nil {
		t.Fatal(err)
	}
	f.collect(t, creativemedia.GCDelete)
	if revisionState(model.ID) != "deleted" {
		t.Fatal("unreferenced model survived GC")
	}
	for _, blob := range []string{stagedGLB.Media[0].BlobID, stagedPNG.Media[0].BlobID} {
		if s := blobState(blob); s == "ready" {
			t.Fatalf("blob %s of an unreferenced model still ready", blob)
		}
	}
}
