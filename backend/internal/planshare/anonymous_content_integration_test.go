package planshare_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
)

type allowShareMediaHolder struct{}

func (allowShareMediaHolder) AuthorizeMediaHolderInScope(
	context.Context, store.TxAccountScope, planningmedia.HolderRequest,
) (planningmedia.HolderProof, error) {
	return planningmedia.HolderProof{}, nil
}

func sharePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 40), uint8(y * 40), 120, 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func seedMoodboard(
	t *testing.T,
	scope store.AccountScope,
	objects immutablefs.ObjectStore,
	planID string,
	planRevision int64,
	clock func() time.Time,
) (mediaApp *planningmedia.Application, binding planningmedia.BindingResult, displayChecksum string) {
	t.Helper()
	ctx := context.Background()
	mediaApp = planningmedia.NewApplication(
		planningmedia.Repository{},
		idempotency.NewExecutor(),
		objects,
		planningmedia.WithHolderAuthorizer(allowShareMediaHolder{}),
		planningmedia.WithClock(clock),
	)
	body := sharePNG(t)
	spool, err := planningmedia.NewUploadSpool(ctx, bytes.NewReader(body), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spool.Close() })
	uploaded, err := mediaApp.Upload(ctx, scope, "share-moodboard-upload-"+planID+"-"+clock().UTC().Format("150405.000000000"), planningmedia.UploadInput{
		PlanID:               planID,
		ExpectedPlanRevision: planRevision,
		DisplayName:          "moodboard",
		Content:              spool,
		Rights: planningmedia.RightsDeclarationInput{
			SourceClass: planningmedia.SourceOfficial,
			RightsBasis: planningmedia.RightsCitationOrDisplay,
		},
		IntendedPurpose: planningmedia.PurposeMoodboardDisplay,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err = mediaApp.CreateBinding(ctx, scope, "share-moodboard-bind-"+planID+"-"+uploaded.Asset.ID, planningmedia.CreateBindingInput{
		PlanID:                planID,
		AssetID:               uploaded.Asset.ID,
		Generation:            uploaded.Generation.Generation,
		HolderKind:            planningmedia.HolderPlan,
		HolderID:              planID,
		Purpose:               planningmedia.PurposeMoodboardDisplay,
		ExpectedPlanRevision:  planRevision,
		ExpectedAssetRevision: uploaded.Asset.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	return mediaApp, binding, uploaded.AccessRef.DisplayChecksum
}

func TestAnonymousSharedMediaContentMatrix(t *testing.T) {
	ctx := context.Background()
	db := openShareStore(t)
	scope := createShareAccount(t, db, "share-content-acct")
	clock := newAtomicClock(time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC))
	app := planshare.NewApplication(idempotency.NewExecutor(), planshare.WithClock(clock.Now))
	repo := shootplanning.NewPostgresRepository()
	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "素材分享", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := immutablefs.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mediaApp, binding, checksum := seedMoodboard(t, scope, objects, plan.ID, plan.Revision, clock.Now)

	secret := mustSecret(t)
	issued := issueOK(t, app, scope, "content-issue", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: plan.Revision,
		ViewLevel:            planshare.ViewLevelProposal,
		SecretCommitment:     planshare.CommitShareSecret(secret),
		ExpiresAt:            clock.Now().Add(2 * time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	wire, err := planshare.ComposeShareTokenWire(issued.Selector, secret)
	if err != nil {
		t.Fatal(err)
	}
	resolver := planshare.NewResolver(planshare.StoreTokenLookup{Store: db}, planshare.DefaultTrustedCapabilityFactory{})
	runner := planshare.StoreShareTransactionRunner{Store: db, Media: mediaApp}
	projDeps := planshare.AnonymousProjectionDeps{Resolver: resolver, Runner: runner}
	contentDeps := planshare.AnonymousContentDeps{Resolver: resolver, Runner: runner, Media: mediaApp}

	proj, err := app.GetAnonymousProjection(ctx, projDeps, wire)
	if err != nil || proj.Proposal == nil || len(proj.Proposal.Moodboard) != 1 {
		t.Fatalf("projection=%+v err=%v", proj, err)
	}
	ref := proj.Proposal.Moodboard[0].Ref
	if proj.Proposal.Moodboard[0].Checksum != checksum {
		t.Fatalf("checksum mismatch %s vs %s", proj.Proposal.Moodboard[0].Checksum, checksum)
	}

	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logBuf, nil))
	router := httpapi.NewRouter(httpapi.RouterDeps{
		Logger: logger,
		DB:     db,
		AnonymousShare: httpapi.AnonymousShareDeps{
			App:           app,
			Resolver:      resolver,
			Runner:        runner,
			PlanningMedia: mediaApp,
			ReadBudget:    fakeReadBudget{allowed: true},
			IPDigest:      fakeIPDigest{},
			Now:           clock.Now,
		},
	})

	contentURL := "/api/v1/shared/plans/" + url.PathEscape(wire) + "/assets/" + url.PathEscape(ref) + "/content?v=" + url.QueryEscape(checksum)
	req := httptest.NewRequest(http.MethodGet, contentURL, nil)
	req.RemoteAddr = "203.0.113.20:443"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "private, no-store" ||
		rec.Header().Get("Referrer-Policy") != "no-referrer" ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
		rec.Header().Get("Content-Disposition") != "inline" ||
		!strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'none'") ||
		rec.Header().Get("ETag") != `"`+checksum+`"` {
		t.Fatalf("headers=%v", rec.Header())
	}
	sum := sha256.Sum256(rec.Body.Bytes())
	got := "sha256-" + hexEncode(sum[:])
	if got != checksum {
		t.Fatalf("body checksum=%s want=%s", got, checksum)
	}
	body := rec.Body.String()
	logged := logBuf.String()
	for _, leak := range []string{wire, binding.Binding.ID, binding.Asset.ID, "planning/", ref} {
		if leak == ref {
			if strings.Contains(logged, ref) {
				t.Fatalf("ref canary in logs: %s", logged)
			}
			continue
		}
		if strings.Contains(body, leak) || strings.Contains(logged, leak) {
			t.Fatalf("leak %q in body/logs", leak)
		}
	}
	if strings.Contains(logged, wire) {
		t.Fatalf("token canary in logs: %s", logged)
	}

	// rotate → new generation; concurrent first GETs mint exactly one ref
	secret2 := mustSecret(t)
	rotated := rotateOK(t, app, scope, "content-rotate", plan.ID, issued.ShareID, planshare.RotateInput{
		ExpectedShareRevision: issued.Revision,
		NewSecretCommitment:   planshare.CommitShareSecret(secret2),
		ExpiresAt:             clock.Now().Add(2 * time.Hour),
		ExpirySource:          planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:         planshare.PolicyVersionV1,
	})
	wire2, err := planshare.ComposeShareTokenWire(rotated.Selector, secret2)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	refs := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := app.GetAnonymousProjection(ctx, projDeps, wire2)
			if err != nil {
				errs <- err
				return
			}
			if p.Proposal == nil || len(p.Proposal.Moodboard) != 1 {
				errs <- errMoodboard()
				return
			}
			refs <- p.Proposal.Moodboard[0].Ref
			errs <- nil
		}()
	}
	wg.Wait()
	close(errs)
	close(refs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]struct{}{}
	for r := range refs {
		seen[r] = struct{}{}
	}
	if len(seen) != 1 {
		t.Fatalf("concurrent first GET refs=%v", seen)
	}
	var ref2 string
	for r := range seen {
		ref2 = r
	}
	if ref2 == "" || ref2 == ref {
		t.Fatalf("new generation ref invalid: old=%s new=%s", ref, ref2)
	}

	for _, badURL := range []string{
		"/api/v1/shared/plans/" + url.PathEscape(wire) + "/assets/" + url.PathEscape(ref) + "/content?v=" + url.QueryEscape(checksum),
		"/api/v1/shared/plans/" + url.PathEscape(wire2) + "/assets/" + url.PathEscape(ref) + "/content?v=" + url.QueryEscape(checksum),
		"/api/v1/shared/plans/" + url.PathEscape(wire2) + "/assets/" + url.PathEscape(ref2) + "/content?v=sha256-deadbeef",
		"/api/v1/shared/plans/" + url.PathEscape(wire2) + "/assets/sar_not_a_real_ref/content?v=" + url.QueryEscape(checksum),
	} {
		badReq := httptest.NewRequest(http.MethodGet, badURL, nil)
		badReq.RemoteAddr = "203.0.113.20:443"
		badRec := httptest.NewRecorder()
		router.ServeHTTP(badRec, badReq)
		if badRec.Code != http.StatusNotFound {
			t.Fatalf("url=%s status=%d body=%s", badURL, badRec.Code, badRec.Body.String())
		}
		if strings.Contains(badRec.Body.String(), binding.Asset.ID) ||
			strings.Contains(badRec.Body.String(), "asset_reference_stale") ||
			strings.Contains(badRec.Body.String(), "asset_gc_in_progress") {
			t.Fatalf("anonymous 404 leaked internals: %s", badRec.Body.String())
		}
	}

	okURL := "/api/v1/shared/plans/" + url.PathEscape(wire2) + "/assets/" + url.PathEscape(ref2) + "/content?v=" + url.QueryEscape(checksum)
	okReq := httptest.NewRequest(http.MethodGet, okURL, nil)
	okReq.RemoteAddr = "203.0.113.20:443"
	okRec := httptest.NewRecorder()
	router.ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Fatalf("rotated content status=%d body=%s", okRec.Code, okRec.Body.String())
	}

	// binding release then content → 404
	_, err = mediaApp.ReleaseBinding(ctx, scope, "content-release", plan.ID, binding.Asset.ID, binding.Binding.ID,
		plan.Revision, binding.Asset.Revision, binding.Binding.Revision)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := app.IssueAnonymousContentPermit(ctx, contentDeps, wire2, ref2, checksum)
	if err != planshare.ErrShareNotFound {
		t.Fatalf("after release err=%v permit=%v", err, permit)
	}
	relReq := httptest.NewRequest(http.MethodGet, okURL, nil)
	relReq.RemoteAddr = "203.0.113.20:443"
	relRec := httptest.NewRecorder()
	router.ServeHTTP(relRec, relReq)
	if relRec.Code != http.StatusNotFound {
		t.Fatalf("released content status=%d", relRec.Code)
	}

	// revoked token → 404
	revokeOK(t, app, scope, "content-revoke", plan.ID, rotated.ShareID, planshare.RevokeInput{
		ExpectedShareRevision: rotated.Revision,
		PolicyVersion:         planshare.PolicyVersionV1,
	})
	revReq := httptest.NewRequest(http.MethodGet, okURL, nil)
	revReq.RemoteAddr = "203.0.113.20:443"
	revRec := httptest.NewRecorder()
	router.ServeHTTP(revRec, revReq)
	if revRec.Code != http.StatusNotFound {
		t.Fatalf("revoked content status=%d", revRec.Code)
	}
}

func TestAnonymousSharedMediaOpenVersusReleaseAndArchive(t *testing.T) {
	ctx := context.Background()
	db := openShareStore(t)
	scope := createShareAccount(t, db, "share-content-race")
	clock := newAtomicClock(time.Date(2026, 8, 14, 16, 0, 0, 0, time.UTC))
	app := planshare.NewApplication(idempotency.NewExecutor(), planshare.WithClock(clock.Now))
	repo := shootplanning.NewPostgresRepository()
	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "竞态", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := immutablefs.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blocking := &blockingShareOpenStore{ObjectStore: objects, started: make(chan struct{}), release: make(chan struct{})}
	mediaApp, binding, checksum := seedMoodboard(t, scope, blocking, plan.ID, plan.Revision, clock.Now)

	secret := mustSecret(t)
	issued := issueOK(t, app, scope, "race-issue", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: plan.Revision,
		ViewLevel:            planshare.ViewLevelProposal,
		SecretCommitment:     planshare.CommitShareSecret(secret),
		ExpiresAt:            clock.Now().Add(2 * time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	wire, err := planshare.ComposeShareTokenWire(issued.Selector, secret)
	if err != nil {
		t.Fatal(err)
	}
	resolver := planshare.NewResolver(planshare.StoreTokenLookup{Store: db}, planshare.DefaultTrustedCapabilityFactory{})
	runner := planshare.StoreShareTransactionRunner{Store: db, Media: mediaApp}
	projDeps := planshare.AnonymousProjectionDeps{Resolver: resolver, Runner: runner}
	contentDeps := planshare.AnonymousContentDeps{Resolver: resolver, Runner: runner, Media: mediaApp}

	proj, err := app.GetAnonymousProjection(ctx, projDeps, wire)
	if err != nil || proj.Proposal == nil || len(proj.Proposal.Moodboard) != 1 {
		t.Fatalf("projection=%+v err=%v", proj, err)
	}
	ref := proj.Proposal.Moodboard[0].Ref

	readDone := make(chan error, 1)
	go func() {
		permit, err := app.IssueAnonymousContentPermit(ctx, contentDeps, wire, ref, checksum)
		if err != nil {
			readDone <- err
			return
		}
		stream, err := mediaApp.OpenDisplayWithPermit(ctx, permit)
		if err != nil {
			readDone <- err
			return
		}
		_, err = io.ReadAll(stream.Reader)
		closeErr := stream.Close()
		if err == nil {
			err = closeErr
		}
		readDone <- err
	}()
	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		t.Fatal("content open did not reach object store")
	}
	releaseDone := make(chan error, 1)
	go func() {
		_, err := mediaApp.ReleaseBinding(ctx, scope, "race-release", plan.ID, binding.Asset.ID, binding.Binding.ID,
			plan.Revision, binding.Asset.Revision, binding.Binding.Revision)
		releaseDone <- err
	}()
	if err := <-releaseDone; err != nil {
		t.Fatalf("release during open: %v", err)
	}
	close(blocking.release)
	if err := <-readDone; err != nil {
		t.Fatalf("open-after-permit must complete: %v", err)
	}

	// subsequent content after release → 404
	if _, err := app.IssueAnonymousContentPermit(ctx, contentDeps, wire, ref, checksum); err != planshare.ErrShareNotFound {
		t.Fatalf("post-release err=%v", err)
	}

	// archive on a fresh plan → content 404
	plan2, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "归档", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	objects2, err := immutablefs.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mediaApp2, _, checksum2 := seedMoodboard(t, scope, objects2, plan2.ID, plan2.Revision, clock.Now)
	runner2 := planshare.StoreShareTransactionRunner{Store: db, Media: mediaApp2}
	secret3 := mustSecret(t)
	issued3 := issueOK(t, app, scope, "archive-issue", plan2.ID, planshare.IssueInput{
		ExpectedPlanRevision: plan2.Revision,
		ViewLevel:            planshare.ViewLevelProposal,
		SecretCommitment:     planshare.CommitShareSecret(secret3),
		ExpiresAt:            clock.Now().Add(2 * time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	wire3, err := planshare.ComposeShareTokenWire(issued3.Selector, secret3)
	if err != nil {
		t.Fatal(err)
	}
	projDeps2 := planshare.AnonymousProjectionDeps{Resolver: resolver, Runner: runner2}
	contentDeps2 := planshare.AnonymousContentDeps{Resolver: resolver, Runner: runner2, Media: mediaApp2}
	proj3, err := app.GetAnonymousProjection(ctx, projDeps2, wire3)
	if err != nil || proj3.Proposal == nil || len(proj3.Proposal.Moodboard) != 1 {
		t.Fatalf("proj3=%+v err=%v", proj3, err)
	}
	ref3 := proj3.Proposal.Moodboard[0].Ref
	if _, err := scope.Update(ctx, "shoot_plans", "archived_at = $2", "id = $3", clock.Now(), plan2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.IssueAnonymousContentPermit(ctx, contentDeps2, wire3, ref3, checksum2); err != planshare.ErrShareNotFound {
		t.Fatalf("archived content err=%v", err)
	}
}

func TestAnonymousSharedMediaExpiredToken404(t *testing.T) {
	ctx := context.Background()
	db := openShareStore(t)
	scope := createShareAccount(t, db, "share-content-exp")
	clock := newAtomicClock(time.Date(2026, 8, 14, 17, 0, 0, 0, time.UTC))
	app := planshare.NewApplication(idempotency.NewExecutor(), planshare.WithClock(clock.Now))
	repo := shootplanning.NewPostgresRepository()
	plan, err := repo.Create(ctx, scope, shootplanning.CreatePlanInput{Title: "过期", Subject: "主体"})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := immutablefs.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mediaApp, _, checksum := seedMoodboard(t, scope, objects, plan.ID, plan.Revision, clock.Now)
	secret := mustSecret(t)
	issued := issueOK(t, app, scope, "exp-content-issue", plan.ID, planshare.IssueInput{
		ExpectedPlanRevision: plan.Revision,
		ViewLevel:            planshare.ViewLevelProposal,
		SecretCommitment:     planshare.CommitShareSecret(secret),
		ExpiresAt:            clock.Now().Add(time.Hour),
		ExpirySource:         planshare.ExpirySourceV1{Kind: planshare.ExpirySourceExplicit},
		PolicyVersion:        planshare.PolicyVersionV1,
	})
	wire, err := planshare.ComposeShareTokenWire(issued.Selector, secret)
	if err != nil {
		t.Fatal(err)
	}
	resolver := planshare.NewResolver(planshare.StoreTokenLookup{Store: db}, planshare.DefaultTrustedCapabilityFactory{})
	runner := planshare.StoreShareTransactionRunner{Store: db, Media: mediaApp}
	proj, err := app.GetAnonymousProjection(ctx, planshare.AnonymousProjectionDeps{Resolver: resolver, Runner: runner}, wire)
	if err != nil || proj.Proposal == nil || len(proj.Proposal.Moodboard) != 1 {
		t.Fatalf("projection=%+v err=%v", proj, err)
	}
	ref := proj.Proposal.Moodboard[0].Ref
	clock.Set(clock.Now().Add(2 * time.Hour))
	_, err = app.IssueAnonymousContentPermit(ctx, planshare.AnonymousContentDeps{
		Resolver: resolver, Runner: runner, Media: mediaApp,
	}, wire, ref, checksum)
	if err != planshare.ErrShareNotFound {
		t.Fatalf("expired content err=%v", err)
	}
}

func errMoodboard() error {
	return errString("moodboard missing")
}

type errString string

func (e errString) Error() string { return string(e) }

func hexEncode(raw []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(raw)*2)
	for i, b := range raw {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0x0f]
	}
	return string(out)
}

type blockingShareOpenStore struct {
	immutablefs.ObjectStore
	started chan struct{}
	release chan struct{}
}

func (b *blockingShareOpenStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	select {
	case <-b.started:
	default:
		close(b.started)
	}
	select {
	case <-b.release:
		return b.ObjectStore.Open(ctx, key)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
