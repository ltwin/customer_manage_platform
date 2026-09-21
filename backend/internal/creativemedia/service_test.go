package creativemedia_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

type fixture struct {
	db      *sql.DB
	a, b    store.AccountScope
	svc     *creativemedia.Service
	runtime jobs.Runtime
	parts   *httptest.Server
	cfg     creativemedia.Config
	local   versionedfs.Adapter
	key     []byte
}

func setup(t *testing.T) *fixture {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('media-a','test','active'),('media-b','test','active')`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err = st.MigrateCreativeJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg := creativemedia.DefaultConfig()
	cfg.PartSize = 4096
	cfg.ImageMaxBytes = 4 << 20
	cfg.MaxParts = 2000
	cfg.QuotaLimitBytes = 2 << 20
	key := bytes.Repeat([]byte{7}, 32)
	f := &fixture{db: db, cfg: cfg}
	var svc *creativemedia.Service
	f.parts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := svc.WriteLocalPart(r.Context(), r.URL.Query().Get("token"), r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(f.parts.Close)
	root := filepath.Join(t.TempDir(), "media")
	// The signer needs the service; build in two steps around the local adapter.
	stub, err := creativemedia.NewService(cfg, stubAdapter{}, creativemedia.NewVerifier(cfg), key, "/api/v1/creative/media")
	if err != nil {
		t.Fatal(err)
	}
	local, err := versionedfs.NewLocal(root, stub.PartSigner(f.parts.URL+"/part"))
	if err != nil {
		t.Fatal(err)
	}
	svc, err = creativemedia.NewService(cfg, local, creativemedia.NewVerifier(cfg), key, "/api/v1/creative/media")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := st.NewJobRuntime(svc.Handlers(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	svc.SetRuntime(runtime)
	f.svc, f.runtime, f.local, f.key = svc, runtime, local, key
	f.a = st.ScopeFor(auth.AccountContext{AccountID: "media-a"})
	f.b = st.ScopeFor(auth.AccountContext{AccountID: "media-b"})
	return f
}

type stubAdapter struct{ versionedfs.Adapter }

func (stubAdapter) Driver() string { return "local" }
func (stubAdapter) Bucket() string { return "" }

func command(t *testing.T, in any) creativeops.Command {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw}
}
func decode[T any](t *testing.T, r creativeops.Receipt, err error) T {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err = json.Unmarshal(r.Outcome.Response, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func must[T any](t *testing.T) func(creativeops.Receipt, error) T {
	return func(r creativeops.Receipt, err error) T { return decode[T](t, r, err) }
}
func sample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func rights() creativecontent.RightsDeclarationInput {
	return creativecontent.RightsDeclarationInput{SourceClass: planningmedia.SourcePhotographerOwned, RightsBasis: planningmedia.RightsOwnershipAttested}
}

// drain runs every queued creative job exactly like the worker process would.
func (f *fixture) drain(t *testing.T, scope store.AccountScope) {
	t.Helper()
	for range 20 {
		rows, err := f.db.Query(`SELECT id,args FROM creative_jobs.river_job WHERE state='available' ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		type queued struct {
			id   int64
			args []byte
		}
		var pending []queued
		for rows.Next() {
			var q queued
			if err := rows.Scan(&q.id, &q.args); err != nil {
				t.Fatal(err)
			}
			pending = append(pending, q)
		}
		_ = rows.Close()
		if len(pending) == 0 {
			return
		}
		for _, q := range pending {
			var envelope struct {
				Request jobs.Request `json:"request"`
			}
			if err := json.Unmarshal(q.args, &envelope); err != nil {
				t.Fatal(err)
			}
			var handler func(context.Context, store.AccountScope, jobs.Request) error
			for _, h := range f.svc.Handlers() {
				if h.Kind == envelope.Request.Kind {
					handler = h.Work
				}
			}
			if handler == nil {
				t.Fatalf("no handler for %s", envelope.Request.Kind)
			}
			if err := handler(t.Context(), scope, envelope.Request); err != nil {
				t.Fatalf("%s: %v", envelope.Request.Kind, err)
			}
			if _, err := f.db.Exec(`UPDATE creative_jobs.river_job SET state='completed',finalized_at=now() WHERE id=$1`, q.id); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Fatal("queue did not drain")
}

// uploadFile drives the client protocol: create → poll → sign parts → PUT → complete.
func (f *fixture) uploadFile(t *testing.T, scope store.AccountScope, name, mime, kind string, body []byte, target creativemedia.Target) creativemedia.UploadView {
	t.Helper()
	accepted := must[creativemedia.UploadView](t)(f.svc.CreateUpload(t.Context(), scope, command(t, creativemedia.CreateUploadInput{FileName: name, Kind: kind, Mime: mime, Size: int64(len(body)), Rights: rights(), Target: target})))
	if accepted.State != "created" {
		t.Fatalf("accepted %+v", accepted)
	}
	f.drain(t, scope)
	view, err := f.svc.GetUpload(t.Context(), scope, accepted.ID)
	if err != nil || view.State != "uploading" {
		t.Fatalf("after init %+v %v", view, err)
	}
	numbers := []int{}
	for i := 1; i <= view.PartCount; i++ {
		numbers = append(numbers, i)
	}
	authorized, err := f.svc.AuthorizeParts(t.Context(), scope, view.ID, numbers)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range authorized.Parts {
		start := int64(p.Number-1) * view.PartSize
		end := min(start+view.PartSize, int64(len(body)))
		req, _ := http.NewRequest(p.Method, p.URL, bytes.NewReader(body[start:end]))
		for k, v := range p.Headers {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("part %d: %d", p.Number, res.StatusCode)
		}
	}
	completed := must[creativemedia.UploadView](t)(f.svc.CompleteUpload(t.Context(), scope, command(t, creativemedia.CompleteUploadInput{UploadID: view.ID, ExpectedRevision: view.Revision})))
	if completed.IOPhase != "completing" {
		t.Fatalf("complete %+v", completed)
	}
	f.drain(t, scope)
	view, err = f.svc.GetUpload(t.Context(), scope, accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func project(t *testing.T, scope store.AccountScope) creativecanvas.ProjectResult {
	t.Helper()
	r, err := creativecanvas.CreateProject(t.Context(), scope, command(t, creativecanvas.CreateProjectInput{Name: "媒体项目"}))
	if err != nil {
		t.Fatal(err)
	}
	var p creativecanvas.ProjectResult
	if err := json.Unmarshal(r.Outcome.Response, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImageUploadPublishesAssetAndCanvasNodeRoundTrip(t *testing.T) {
	f := setup(t)
	png := sample(t, "sample.png")
	view := f.uploadFile(t, f.a, "窗边.png", "image/png", "image", png, creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "窗边自然光", NewTags: []creativelibrary.NewTag{{ClientKey: "k", Name: "自然光", Color: "#ABCDEF"}}}})
	if view.State != "ready" || view.Publication == nil || view.Publication.Kind != "asset" || view.Binding == nil || view.Binding.Status != "applied" {
		t.Fatalf("publication %+v", view)
	}
	var blobID, handoff, reserved string
	if err := f.db.QueryRow(`SELECT COALESCE(blob_id,''),COALESCE(handed_off_at::text,''),reserved_bytes::text FROM creative_uploads WHERE id=$1`, view.ID).Scan(&blobID, &handoff, &reserved); err != nil || blobID != "" || handoff == "" || reserved != "0" {
		t.Fatalf("handoff blob=%q handoff=%q reserved=%s %v", blobID, handoff, reserved, err)
	}
	var stored, reservedTotal int64
	if err := f.db.QueryRow(`SELECT stored_bytes,reserved_bytes FROM creative_media_quotas WHERE account_id='media-a'`).Scan(&stored, &reservedTotal); err != nil || stored != int64(len(png)) || reservedTotal != 0 {
		t.Fatalf("quota stored=%d reserved=%d %v", stored, reservedTotal, err)
	}
	asset, err := creativelibrary.GetAsset(t.Context(), f.a, view.Publication.ID)
	if err != nil || asset.Kind != "image" || asset.Content == nil || len(asset.Content.Media) != 1 || asset.Content.Media[0].Mime != "image/png" || *asset.Content.Media[0].Width != 64 || len(asset.TagIDs) != 1 {
		t.Fatalf("asset %+v %v", asset, err)
	}
	if _, err := creativelibrary.GetAsset(t.Context(), f.b, view.Publication.ID); !errors.Is(err, creativelibrary.ErrNotFound) {
		t.Fatal("cross-account asset visible", err)
	}
	page, err := creativelibrary.SearchAssets(t.Context(), f.a, creativelibrary.Search{Kind: "image", Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("kind filter %+v %v", page, err)
	}
	// Drag the asset into a canvas: the node references the same revision.
	p := project(t, f.a)
	nodeID := "cwnode_" + uuid.NewString()
	if _, err := creativecanvas.AddNode(t.Context(), f.a, command(t, creativecanvas.AddNodeInput{CanvasID: p.CanvasID, NodeID: nodeID, TypeKey: "core.image", ExpectedTopologyRevision: 1, X: 10, Y: 20, Asset: &creativelibrary.AssetReference{ID: asset.ID, Revision: asset.Revision, ContentRevisionID: asset.ContentRevisionID}})); err != nil {
		t.Fatal(err)
	}
	canvas, err := creativecanvas.GetCanvas(t.Context(), f.a, p.CanvasID)
	if err != nil || len(canvas.Nodes) != 1 || canvas.Nodes[0].Content == nil || len(canvas.Nodes[0].Content.Media) != 1 || *canvas.Nodes[0].ContentRevisionID != asset.ContentRevisionID {
		t.Fatalf("canvas %+v %v", canvas, err)
	}
	// Replace the node's media by uploading directly to the node.
	jpg := sample(t, "sample.jpg")
	replaced := f.uploadFile(t, f.a, "替换.jpg", "image/jpeg", "image", jpg, creativemedia.Target{Kind: "node", Node: &creativemedia.NodeTarget{CanvasID: p.CanvasID, NodeID: nodeID, ExpectedDataRevision: canvas.Nodes[0].DataRevision}})
	if replaced.State != "ready" || replaced.Publication == nil || replaced.Publication.Kind != "node" {
		t.Fatalf("node publication %+v", replaced)
	}
	after, err := creativecanvas.GetCanvas(t.Context(), f.a, p.CanvasID)
	if err != nil || *after.Nodes[0].ContentRevisionID == asset.ContentRevisionID || after.Nodes[0].Content.Media[0].Mime != "image/jpeg" || after.Nodes[0].DataRevision != canvas.Nodes[0].DataRevision+1 {
		t.Fatalf("replaced canvas %+v %v", after, err)
	}
	original, err := creativelibrary.GetAsset(t.Context(), f.a, asset.ID)
	if err != nil || original.ContentRevisionID != asset.ContentRevisionID {
		t.Fatal("asset changed by node replacement", err)
	}
	// Undo of the binding change restores the previous revision.
	latest := after.Changes[0] // Snapshots list the newest change first.
	if _, err := creativecanvas.Undo(t.Context(), f.a, command(t, creativecanvas.UndoInput{CanvasID: p.CanvasID, ChangeID: latest.ID, ReadSet: latest.ReadSet})); err != nil {
		t.Fatal("undo binding", err)
	}
	restored, err := creativecanvas.GetCanvas(t.Context(), f.a, p.CanvasID)
	if err != nil || *restored.Nodes[0].ContentRevisionID != asset.ContentRevisionID {
		t.Fatalf("undo restored %+v %v", restored.Nodes[0], err)
	}
	// Save the node back to the library as a new, independent asset.
	saved := must[creativelibrary.AssetResult](t)(creativecanvas.SaveNodeToLibrary(t.Context(), f.a, command(t, creativecanvas.SaveNodeInput{CanvasID: p.CanvasID, NodeID: nodeID, ExpectedDataRevision: restored.Nodes[0].DataRevision, Target: creativelibrary.ImportTarget{Title: "存回库"}})))
	if saved.ID == asset.ID || saved.ContentRevisionID != asset.ContentRevisionID {
		t.Fatalf("saved %+v", saved)
	}
	// Tickets authorize reads; a revoked usage grant denies both ticket and stream.
	ticket, err := f.svc.IssueTicket(t.Context(), f.a, creativemedia.TicketInput{ContentRevisionID: asset.ContentRevisionID, Role: "original", Purpose: "download", FileName: "窗边.png"})
	if err != nil || ticket.ByteSize != int64(len(png)) {
		t.Fatal("ticket", ticket, err)
	}
	token := ticket.URL[strings.Index(ticket.URL, "ticket=")+7:]
	scopeFor := func(id string) store.AccountScope { return f.a }
	rangeOf := func(r *versionedfs.ByteRange) func(int64) (*versionedfs.ByteRange, error) {
		return func(int64) (*versionedfs.ByteRange, error) { return r, nil }
	}
	if _, err := f.svc.Open(t.Context(), scopeFor, token, asset.ContentRevisionID, "display", nil); !errors.Is(err, creativemedia.ErrTicket) {
		t.Fatal("ticket must bind role", err)
	}
	stream, err := f.svc.Open(t.Context(), scopeFor, token, asset.ContentRevisionID, "original", rangeOf(&versionedfs.ByteRange{Start: 4, End: 7}))
	if err != nil || stream.Size != int64(len(png)) || !stream.Download {
		t.Fatal("open", err)
	}
	partial, _ := io.ReadAll(stream.Body)
	_ = stream.Body.Close()
	if !bytes.Equal(partial, png[4:8]) {
		t.Fatal("range bytes", partial)
	}
	var pins int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_blob_read_pins WHERE account_id='media-a' AND expires_at>now()`).Scan(&pins); err != nil || pins != 1 {
		t.Fatal("read pin", pins, err)
	}
	if _, err := f.svc.Open(t.Context(), scopeFor, token, asset.ContentRevisionID, "original", rangeOf(&versionedfs.ByteRange{Start: 0, End: int64(len(png))})); !errors.Is(err, creativemedia.ErrRange) {
		t.Fatal("range beyond size", err)
	}
	if _, err := f.db.Exec(`UPDATE creative_usage_grants SET revoked_at=now() WHERE account_id='media-a' AND declaration_id=(SELECT rights_declaration_id FROM creative_content_revisions WHERE id=$1)`, asset.ContentRevisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Open(t.Context(), scopeFor, token, asset.ContentRevisionID, "original", nil); !errors.Is(err, creativecontent.ErrUsageDenied) {
		t.Fatal("revoked grant still streams", err)
	}
	if _, err := f.svc.IssueTicket(t.Context(), f.a, creativemedia.TicketInput{ContentRevisionID: asset.ContentRevisionID, Role: "original", Purpose: "display"}); !errors.Is(err, creativecontent.ErrUsageDenied) {
		t.Fatal("revoked grant still issues tickets", err)
	}
}

func TestEveryEnabledFormatVerifiesAndFakeMimeFails(t *testing.T) {
	f := setup(t)
	for _, c := range []struct{ name, mime, kind string }{{"sample.jpg", "image/jpeg", "image"}, {"sample.png", "image/png", "image"}, {"sample.webp", "image/webp", "image"}, {"sample.mp4", "video/mp4", "video"}, {"sample.webm", "video/webm", "video"}, {"sample.mp3", "audio/mpeg", "audio"}, {"sample.wav", "audio/wav", "audio"}} {
		view := f.uploadFile(t, f.a, c.name, c.mime, c.kind, sample(t, c.name), creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: c.name}})
		if view.State != "ready" || view.Publication == nil || view.Publication.Kind != "asset" {
			t.Fatalf("%s: %+v", c.name, view)
		}
		asset, err := creativelibrary.GetAsset(t.Context(), f.a, view.Publication.ID)
		if err != nil || asset.Kind != c.kind || asset.Content.Media[0].Mime != c.mime {
			t.Fatalf("%s asset %+v %v", c.name, asset, err)
		}
		if c.kind != "image" && (asset.Content.Media[0].DurationMs == nil) {
			t.Fatalf("%s missing duration", c.name)
		}
		if c.kind == "video" && (asset.Content.Media[0].Width == nil || *asset.Content.Media[0].Width != 64) {
			t.Fatalf("%s missing dimensions", c.name)
		}
	}
	// Declared image, actually audio bytes: verification fails without leaking the file.
	fake := f.uploadFile(t, f.a, "fake.png", "image/png", "image", sample(t, "sample.mp3"), creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "假图"}})
	if fake.State != "failed" || fake.ErrorCode != "media_unsupported" {
		t.Fatalf("fake %+v", fake)
	}
	var reserved int64
	if err := f.db.QueryRow(`SELECT reserved_bytes FROM creative_media_quotas WHERE account_id='media-a'`).Scan(&reserved); err != nil || reserved != 0 {
		t.Fatal("failed upload retained reservation", reserved, err)
	}
	page, _ := creativelibrary.SearchAssets(t.Context(), f.a, creativelibrary.Search{Limit: 20})
	if page.TotalCount != 7 {
		t.Fatalf("failed upload created asset: %d", page.TotalCount)
	}
	// Unsupported declared MIME and oversize declarations are rejected on create.
	// Omitted rights default to the photographer's own work.
	if _, err := f.svc.CreateUpload(t.Context(), f.a, command(t, creativemedia.CreateUploadInput{FileName: "own.png", Kind: "image", Mime: "image/png", Size: 10, Target: creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "own"}}})); err != nil {
		t.Fatalf("upload without rights: %v", err)
	}
	if _, err := f.svc.CreateUpload(t.Context(), f.a, command(t, creativemedia.CreateUploadInput{FileName: "x.gif", Kind: "image", Mime: "image/gif", Size: 10, Rights: rights(), Target: creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "x"}}})); !errors.Is(err, creativemedia.ErrUnsupported) {
		t.Fatal("gif accepted", err)
	}
	if _, err := f.svc.CreateUpload(t.Context(), f.a, command(t, creativemedia.CreateUploadInput{FileName: "x.png", Kind: "image", Mime: "image/png", Size: 5 << 20, Rights: rights(), Target: creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "x"}}})); !errors.Is(err, creativemedia.ErrSizeLimit) {
		t.Fatal("oversize accepted", err)
	}
	if _, err := f.svc.CreateUpload(t.Context(), f.a, command(t, creativemedia.CreateUploadInput{FileName: "x.png", Kind: "image", Mime: "image/png", Size: 3 << 20, Rights: rights(), Target: creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "x"}}})); !errors.Is(err, creativemedia.ErrQuota) {
		t.Fatal("quota not enforced", err)
	}
}

func TestChangedNodeTargetBecomesCandidateAdoptOrDiscardOnce(t *testing.T) {
	f := setup(t)
	p := project(t, f.a)
	nodeID := "cwnode_" + uuid.NewString()
	if _, err := creativecanvas.AddNode(t.Context(), f.a, command(t, creativecanvas.AddNodeInput{CanvasID: p.CanvasID, NodeID: nodeID, TypeKey: "core.image", ExpectedTopologyRevision: 1, X: 0, Y: 0})); err != nil {
		t.Fatal(err)
	}
	canvas, _ := creativecanvas.GetCanvas(t.Context(), f.a, p.CanvasID)
	png := sample(t, "sample.png")
	target := creativemedia.Target{Kind: "node", Node: &creativemedia.NodeTarget{CanvasID: p.CanvasID, NodeID: nodeID, ExpectedDataRevision: canvas.Nodes[0].DataRevision}}
	accepted := must[creativemedia.UploadView](t)(f.svc.CreateUpload(t.Context(), f.a, command(t, creativemedia.CreateUploadInput{FileName: "a.png", Kind: "image", Mime: "image/png", Size: int64(len(png)), Rights: rights(), Target: target})))
	f.drain(t, f.a)
	view, _ := f.svc.GetUpload(t.Context(), f.a, accepted.ID)
	authorized, err := f.svc.AuthorizeParts(t.Context(), f.a, view.ID, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("PUT", authorized.Parts[0].URL, bytes.NewReader(png))
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatal("part", err)
	}
	_ = res.Body.Close()
	// The node changes while the upload is in flight (title edit advances data revision).
	if _, err := creativecanvas.ApplyCommands(t.Context(), f.a, command(t, creativecanvas.BatchInput{CanvasID: p.CanvasID, ReadSet: creativecanvas.ReadSetFor(canvas), Actions: []creativecanvas.Action{{Type: "update_metadata", NodeID: nodeID, Title: "改名"}}})); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CompleteUpload(t.Context(), f.a, command(t, creativemedia.CompleteUploadInput{UploadID: view.ID, ExpectedRevision: view.Revision})); err != nil {
		t.Fatal(err)
	}
	f.drain(t, f.a)
	view, _ = f.svc.GetUpload(t.Context(), f.a, accepted.ID)
	if view.State != "ready" || view.Publication == nil || view.Publication.Kind != "candidate" || view.Binding == nil || view.Binding.Status != "needs_review" {
		t.Fatalf("expected candidate %+v", view)
	}
	candidates, err := f.svc.ListCandidates(t.Context(), f.a)
	if err != nil || len(candidates.Items) != 1 || candidates.Items[0].ID != view.Publication.ID {
		t.Fatalf("candidates %+v %v", candidates, err)
	}
	if other, _ := f.svc.ListCandidates(t.Context(), f.b); len(other.Items) != 0 {
		t.Fatal("cross-account candidate")
	}
	c := candidates.Items[0]
	// Adopt onto the node at its current revision; a stale discard then loses.
	current, _ := creativecanvas.GetCanvas(t.Context(), f.a, p.CanvasID)
	adopted := must[creativemedia.Candidate](t)(f.svc.AdoptCandidate(t.Context(), f.a, command(t, creativemedia.CandidateInput{CandidateID: c.ID, CandidateRevision: c.Revision, Target: &creativemedia.Target{Kind: "node", Node: &creativemedia.NodeTarget{CanvasID: p.CanvasID, NodeID: nodeID, ExpectedDataRevision: current.Nodes[0].DataRevision}}})))
	if adopted.State != "applied" || adopted.Adopted == nil || adopted.Adopted.Kind != "node" {
		t.Fatalf("adopted %+v", adopted)
	}
	if _, err := f.svc.DiscardCandidate(t.Context(), f.a, command(t, creativemedia.CandidateInput{CandidateID: c.ID, CandidateRevision: c.Revision})); !errors.Is(err, creativecanvas.ErrVersionConflict) {
		t.Fatal("stale discard accepted", err)
	}
	if _, err := f.svc.AdoptCandidate(t.Context(), f.a, command(t, creativemedia.CandidateInput{CandidateID: c.ID, CandidateRevision: adopted.Revision})); !errors.Is(err, creativemedia.ErrState) {
		t.Fatal("second adoption accepted", err)
	}
	after, _ := creativecanvas.GetCanvas(t.Context(), f.a, p.CanvasID)
	if after.Nodes[0].ContentRevisionID == nil || after.Nodes[0].Content == nil || after.Nodes[0].Content.Media[0].Mime != "image/png" {
		t.Fatalf("node not bound %+v", after.Nodes[0])
	}
	view, _ = f.svc.GetUpload(t.Context(), f.a, accepted.ID)
	if view.Binding == nil || view.Binding.Status != "applied" || view.Binding.TargetID == nil || *view.Binding.TargetID != nodeID {
		t.Fatalf("binding view %+v", view.Binding)
	}
}

func TestStaleEpochCannotWriteAndReplayIsIdempotent(t *testing.T) {
	f := setup(t)
	png := sample(t, "sample.png")
	create := command(t, creativemedia.CreateUploadInput{FileName: "a.png", Kind: "image", Mime: "image/png", Size: int64(len(png)), Rights: rights(), Target: creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "幂等"}}})
	first := must[creativemedia.UploadView](t)(f.svc.CreateUpload(t.Context(), f.a, create))
	replay := must[creativemedia.UploadView](t)(f.svc.CreateUpload(t.Context(), f.a, create))
	if first.ID != replay.ID {
		t.Fatal("replay created a second session")
	}
	var queued int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_jobs.river_job`).Scan(&queued); err != nil || queued != 1 {
		t.Fatal("replay enqueued again", queued, err)
	}
	// Simulate a worker that lost its lease: bump the epoch, then run init.
	if _, err := f.db.Exec(`UPDATE creative_uploads SET execution_epoch=execution_epoch+5 WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	f.drain(t, f.a) // The fresh claim uses the current epoch; it still succeeds.
	view, _ := f.svc.GetUpload(t.Context(), f.a, first.ID)
	if view.State != "uploading" {
		t.Fatalf("init %+v", view)
	}
	// Cancel invalidates the epoch; a late complete job must not verify.
	cancelled := must[creativemedia.UploadView](t)(f.svc.CancelUpload(t.Context(), f.a, command(t, creativemedia.CancelUploadInput{UploadID: view.ID, ExpectedRevision: view.Revision})))
	if cancelled.State != "cancelled" {
		t.Fatalf("cancel %+v", cancelled)
	}
	if _, err := f.svc.AuthorizeParts(t.Context(), f.a, view.ID, []int{1}); !errors.Is(err, creativemedia.ErrState) {
		t.Fatal("cancelled session still signs parts", err)
	}
	if _, err := f.svc.CompleteUpload(t.Context(), f.a, command(t, creativemedia.CompleteUploadInput{UploadID: view.ID, ExpectedRevision: cancelled.Revision})); !errors.Is(err, creativemedia.ErrState) {
		t.Fatal("cancelled session completes", err)
	}
	f.drain(t, f.a)
	var reserved int64
	if err := f.db.QueryRow(`SELECT reserved_bytes FROM creative_media_quotas WHERE account_id='media-a'`).Scan(&reserved); err != nil || reserved != 0 {
		t.Fatal("cancel kept reservation", reserved, err)
	}
	// Expired sessions are swept without touching ready ones.
	if _, err := f.db.Exec(`UPDATE creative_uploads SET expires_at=now()-interval '1 minute' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := f.svc.SweepExpired(t.Context(), f.a, 10); err != nil || n != 0 {
		t.Fatal("sweep touched terminal session", n, err)
	}
	if _, err := f.svc.GetUpload(t.Context(), f.b, first.ID); !errors.Is(err, creativemedia.ErrNotFound) {
		t.Fatal("cross-account upload visible", err)
	}
}

func TestInterruptedPromotionFailsExplicitlyAndReleasesQuota(t *testing.T) {
	f := setup(t)
	png := sample(t, "sample.png")
	accepted := must[creativemedia.UploadView](t)(f.svc.CreateUpload(t.Context(), f.a, command(t, creativemedia.CreateUploadInput{FileName: "a.png", Kind: "image", Mime: "image/png", Size: int64(len(png)), Rights: rights(), Target: creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "中断"}}})))
	f.drain(t, f.a)
	// Simulate a worker that died after preallocating the blob during promotion.
	for _, q := range []string{
		`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,sha256,byte_size,mime,state) VALUES('ccbl_dead','media-a','local','creative-v2/media-a/blobs/ccbl_dead/original','sha256-'||repeat('0',64),1,'image/png','verifying')`,
		`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,sha256,byte_size,mime,state,codec_metadata) VALUES('ccbl_dead_d','media-a','local','creative-v2/media-a/blobs/ccbl_dead_d/display','sha256-'||repeat('1',64),1,'image/jpeg','verifying','{"derived_from":"ccbl_dead"}')`,
		`UPDATE creative_uploads SET state='verifying',io_phase='promoting',blob_id='ccbl_dead',staging_version='v',lease_until=now()-interval '1 minute' WHERE id='` + accepted.ID + `'`,
	} {
		if _, err := f.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(map[string]string{"upload_id": accepted.ID})
	job := jobs.Request{Kind: "media.upload_verify", OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw}
	var handler func(context.Context, store.AccountScope, jobs.Request) error
	for _, h := range f.svc.Handlers() {
		if h.Kind == job.Kind {
			handler = h.Work
		}
	}
	if err := handler(t.Context(), f.a, job); err != nil {
		t.Fatal(err)
	}
	// A large file may verify for many minutes: every stage lease must cover
	// its job attempt timeout, or a concurrent claim could kill a live worker.
	for _, h := range f.svc.Handlers() {
		timeout := h.Timeout
		if timeout == 0 {
			timeout = 2 * time.Minute
		}
		if creativemedia.LeaseBudget(h.Kind) < timeout {
			t.Fatalf("%s lease %v shorter than attempt timeout %v", h.Kind, creativemedia.LeaseBudget(h.Kind), timeout)
		}
	}
	view, err := f.svc.GetUpload(t.Context(), f.a, accepted.ID)
	if err != nil || view.State != "failed" || view.ErrorCode != "promote_interrupted" {
		t.Fatalf("rescue %+v %v", view, err)
	}
	var reserved int64
	var states []string
	if err := f.db.QueryRow(`SELECT reserved_bytes FROM creative_media_quotas WHERE account_id='media-a'`).Scan(&reserved); err != nil || reserved != 0 {
		t.Fatal("reservation kept", reserved, err)
	}
	rows, err := f.db.Query(`SELECT state FROM creative_blobs WHERE account_id='media-a' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		states = append(states, s)
	}
	_ = rows.Close()
	if len(states) != 2 || states[0] != "deleting" || states[1] != "deleting" {
		t.Fatal("original and derived blobs must be marked deleting", states)
	}
	// A live lease means another worker owns it: the rescue must not fire.
	second := must[creativemedia.UploadView](t)(f.svc.CreateUpload(t.Context(), f.a, command(t, creativemedia.CreateUploadInput{FileName: "b.png", Kind: "image", Mime: "image/png", Size: int64(len(png)), Rights: rights(), Target: creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "活跃"}}})))
	f.drain(t, f.a)
	if _, err := f.db.Exec(`UPDATE creative_uploads SET state='verifying',io_phase='promoting',lease_until=now()+interval '1 minute' WHERE id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]string{"upload_id": second.ID})
	if err := handler(t.Context(), f.a, jobs.Request{Kind: job.Kind, OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw}); err != nil {
		t.Fatal(err)
	}
	if view, _ := f.svc.GetUpload(t.Context(), f.a, second.ID); view.State != "verifying" || view.IOPhase != "promoting" {
		t.Fatal("live promotion was disturbed", view)
	}
}
