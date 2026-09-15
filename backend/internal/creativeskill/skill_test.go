package creativeskill_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

type fixture struct {
	db  *sql.DB
	svc *creativeskill.Service
	// objects is the same port svc holds, so a test can wrap it and control
	// when an upload returns.
	objects creativeskill.ObjectPort
	a, b    store.AccountScope
}

func setup(t *testing.T) *fixture {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status)
		VALUES ('skill-a','test','active'),('skill-b','test','active')`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	// Skill resources never arrive as browser multipart uploads, so the local
	// adapter's part signer is unreachable from this package's port.
	local, err := versionedfs.NewLocal(filepath.Join(t.TempDir(), "skills"),
		func(string, string, int, time.Time) versionedfs.PartAuthorization {
			panic("skill resources are published directly, never in parts")
		})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := creativeskill.NewService(local)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{
		db: db, svc: svc, objects: local,
		a: st.ScopeFor(auth.AccountContext{AccountID: "skill-a"}),
		b: st.ScopeFor(auth.AccountContext{AccountID: "skill-b"}),
	}
}

func declare(path, mime string, body []byte) creativeskill.ResourceDeclaration {
	sum := sha256.Sum256(body)
	return creativeskill.ResourceDeclaration{
		Path: path, Mime: mime, ByteSize: int64(len(body)), SHA256: hex.EncodeToString(sum[:]),
	}
}

func manifest() creativeskill.Manifest {
	return creativeskill.Manifest{
		ManifestSchemaVersion:     1,
		InputKinds:                []string{"text", "canvas_selection"},
		MaxInputs:                 20,
		ToolAllowlist:             []string{"search_assets@1", "read_nodes@1"},
		RequiredModelCapabilities: []string{"tool_calling"},
		OutputContract:            "一组参考节点加一段创作方向文字。",
		CompletionCheckKey:        "reference_direction_v1",
	}
}

var checklist = []byte("# 比较清单\n\n1. 光线\n2. 色调\n")

func newRequest() creativeskill.ImportRequest {
	return creativeskill.ImportRequest{
		OperationID:  uuid.NewString(),
		Origin:       "platform",
		Slug:         "reference-direction",
		DisplayName:  "参考整理与创作方向",
		Description:  "在个人库中检索参考、比较候选，产出一组参考节点和一段创作方向文字。",
		Instructions: "先检索，再比较，最后写方向。",
		Manifest:     manifest(),
		Resources:    []creativeskill.ResourceDeclaration{declare("references/comparison-checklist.md", "text/markdown", checklist)},
		Activate:     true,
	}
}

// publish drives the whole protocol so each test can start from a real frozen
// version instead of hand-inserted rows.
func publish(t *testing.T, f *fixture, scope store.AccountScope, req creativeskill.ImportRequest) creativeskill.Version {
	t.Helper()
	record, err := f.svc.BeginImport(t.Context(), scope, req)
	if err != nil {
		t.Fatal(err)
	}
	for _, pending := range record.Pending {
		if _, err := f.svc.StageResource(t.Context(), scope, record.ID, pending, bytes.NewReader(checklist)); err != nil {
			t.Fatal(err)
		}
	}
	version, err := f.svc.FinalizeImport(t.Context(), scope, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	return version
}

// One operation is one publication. Retrying it must return the version it
// already produced, and reusing its identity for different content must fail
// rather than quietly becoming a second edit under the first attempt's name.
func TestOneImportOperationProducesExactlyOneVersion(t *testing.T) {
	f := setup(t)
	req := newRequest()
	first := publish(t, f, f.a, req)

	replayed, err := f.svc.BeginImport(t.Context(), f.a, req)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ResultVersionID != first.ID {
		t.Fatalf("a replayed import must report its original version: %q vs %q", replayed.ResultVersionID, first.ID)
	}
	again, err := f.svc.FinalizeImport(t.Context(), f.a, replayed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Fatalf("finalizing twice must not freeze a second version: %q vs %q", again.ID, first.ID)
	}
	var versions int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_skill_versions WHERE account_id='skill-a'`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 1 {
		t.Fatalf("one import operation produced %d versions", versions)
	}

	edited := req
	edited.Instructions = "换一段完全不同的正文。"
	if _, err := f.svc.BeginImport(t.Context(), f.a, edited); !errors.Is(err, creativeskill.ErrImportConflict) {
		t.Fatalf("one operation id must not name two different requests: %v", err)
	}
}

// A new version is a new choice, not a rewrite. Whatever already fixed version
// one keeps reading version one, byte for byte.
func TestActivatingANewVersionLeavesTheOldOneUntouched(t *testing.T) {
	f := setup(t)
	first := publish(t, f, f.a, newRequest())

	second := newRequest()
	second.Instructions = "第二版：先比较，再检索。"
	second.DisplayName = "参考整理（第二版）"
	second.Resources = nil
	second.ExpectedSkillRevision = first.SkillRevision
	next := publish(t, f, f.a, second)

	if next.VersionNumber != 2 || next.Digest == first.Digest {
		t.Fatalf("the second version must be numbered and hashed on its own: %d %q", next.VersionNumber, next.Digest)
	}
	var current string
	if err := f.db.QueryRow(`SELECT current_version_id FROM creative_skills WHERE id=$1`, first.SkillID).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != next.ID {
		t.Fatalf("activation must move the recommended pointer: %q", current)
	}

	old, err := f.svc.ResolveVersion(t.Context(), f.a, first.SkillID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.Instructions != "先检索，再比较，最后写方向。" || old.Digest != first.Digest {
		t.Fatalf("a fixed version must not follow the pointer: %q", old.Instructions)
	}
	if old.DisplayName != "参考整理与创作方向" {
		t.Fatalf("history must render the name that was shown then: %q", old.DisplayName)
	}
	if len(old.Resources) != 1 || old.Resources[0].Path != "references/comparison-checklist.md" {
		t.Fatalf("the old version must keep its own files: %+v", old.Resources)
	}
	body, err := f.svc.ReadResource(t.Context(), f.a, first.ID, "references/comparison-checklist.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, checklist) {
		t.Fatal("the resource bytes of a fixed version must not change")
	}
}

// Reading someone else's skill is not a different answer, it is no answer.
func TestAnotherAccountCannotReachAPrivateVersion(t *testing.T) {
	f := setup(t)
	mine := publish(t, f, f.a, newRequest())

	if _, err := f.svc.ResolveVersion(t.Context(), f.b, mine.SkillID, mine.ID); !errors.Is(err, creativeskill.ErrNotFound) {
		t.Fatalf("another account resolved a private version: %v", err)
	}
	if _, err := f.svc.ReadResource(t.Context(), f.b, mine.ID, "references/comparison-checklist.md"); !errors.Is(err, creativeskill.ErrNotFound) {
		t.Fatalf("another account read a private resource: %v", err)
	}
	// Publishing under the same slug from another account is a separate skill,
	// not a collision and not a takeover.
	theirs := publish(t, f, f.b, newRequest())
	if theirs.SkillID == mine.SkillID {
		t.Fatal("two accounts must not share one skill identity")
	}
	if _, err := f.svc.ResolveVersion(t.Context(), f.a, theirs.SkillID, theirs.ID); !errors.Is(err, creativeskill.ErrNotFound) {
		t.Fatalf("the first account reached the second account's skill: %v", err)
	}
}

// The declaration made at BeginImport is the contract. Bytes that do not match
// it never become part of a version, because the digest already described them.
func TestStagedBytesMustMatchTheDeclaration(t *testing.T) {
	f := setup(t)
	req := newRequest()
	record, err := f.svc.BeginImport(t.Context(), f.a, req)
	if err != nil {
		t.Fatal(err)
	}
	path := "references/comparison-checklist.md"
	if _, err := f.svc.StageResource(t.Context(), f.a, record.ID, path, bytes.NewReader([]byte("别的内容"))); !errors.Is(err, creativeskill.ErrContentMismatch) {
		t.Fatalf("bytes that contradict the declaration must be refused: %v", err)
	}
	// An undeclared path has no place to go: there is no directory underneath.
	if _, err := f.svc.StageResource(t.Context(), f.a, record.ID, "references/other.md", bytes.NewReader(checklist)); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("an undeclared path must be refused: %v", err)
	}
	if _, err := f.svc.StageResource(t.Context(), f.a, record.ID, "../../etc/passwd", bytes.NewReader(checklist)); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("a traversal must be refused: %v", err)
	}
	// Nothing may freeze while a declared file is still missing.
	if _, err := f.svc.FinalizeImport(t.Context(), f.a, record.ID); !errors.Is(err, creativeskill.ErrImportState) {
		t.Fatalf("an incomplete import must not freeze a version: %v", err)
	}

	if _, err := f.svc.StageResource(t.Context(), f.a, record.ID, path, bytes.NewReader(checklist)); err != nil {
		t.Fatal(err)
	}
	version, err := f.svc.FinalizeImport(t.Context(), f.a, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A stored object that stops matching what the version froze is a fault to
	// report, never content to hand to a model.
	if _, err := f.db.Exec(`UPDATE creative_skill_version_resources SET sha256=repeat('f',64)
		WHERE skill_version_id=$1`, version.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ReadResource(t.Context(), f.a, version.ID, path); !errors.Is(err, creativeskill.ErrContentMismatch) {
		t.Fatalf("a resource that no longer matches its hash must fail loudly: %v", err)
	}
}

// Withdrawing a version stops new work without editing history or its identity.
func TestDisablingAVersionKeepsItsContentAndIdentity(t *testing.T) {
	f := setup(t)
	version := publish(t, f, f.a, newRequest())

	if err := f.svc.DisableVersion(t.Context(), f.a, version.ID, "内容有误"); err != nil {
		t.Fatal(err)
	}
	withdrawn, err := f.svc.ResolveVersion(t.Context(), f.a, version.SkillID, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if withdrawn.Usable() {
		t.Fatal("a withdrawn version must not be usable")
	}
	if withdrawn.Digest != version.Digest || withdrawn.Instructions != "先检索，再比较，最后写方向。" {
		t.Fatal("withdrawing must not change which text ran")
	}
	if withdrawn.DisabledReason != "内容有误" {
		t.Fatalf("the reason must stay readable: %q", withdrawn.DisabledReason)
	}
	// Recommending a withdrawn version would put it back in front of a
	// photographer through a different door.
	if err := f.svc.ActivateVersion(t.Context(), f.a, version.SkillID, version.ID, withdrawn.SkillRevision); !errors.Is(err, creativeskill.ErrImportState) {
		t.Fatalf("a withdrawn version must not be re-recommended: %v", err)
	}
}

// Extending a skill means saying which revision is being extended. A stale or
// missing answer is a conflict, not an overwrite of somebody else's work.
func TestExtendingASkillRequiresTheCurrentRevision(t *testing.T) {
	f := setup(t)
	first := publish(t, f, f.a, newRequest())

	stale := newRequest()
	stale.Resources = nil
	stale.ExpectedSkillRevision = first.SkillRevision
	second := publish(t, f, f.a, stale)
	if second.VersionNumber != 2 {
		t.Fatalf("expected the second version, got %d", second.VersionNumber)
	}

	// The same revision again: the skill has moved on since.
	outdated := newRequest()
	outdated.Resources = nil
	outdated.Instructions = "第三版正文。"
	outdated.ExpectedSkillRevision = first.SkillRevision
	if _, err := f.svc.BeginImport(t.Context(), f.a, outdated); !errors.Is(err, creativeskill.ErrRevisionConflict) {
		t.Fatalf("a stale revision must be refused: %v", err)
	}
	// No revision at all is the "create this skill" intent aimed at a slug that
	// already exists.
	blind := newRequest()
	blind.Resources = nil
	blind.Instructions = "第四版正文。"
	if _, err := f.svc.BeginImport(t.Context(), f.a, blind); !errors.Is(err, creativeskill.ErrRevisionConflict) {
		t.Fatalf("creating an existing slug must be refused: %v", err)
	}
}

// The revision gate that matters under concurrency is the one inside the
// finalize transaction: two imports can both start against the same revision,
// and only one of them may become a version.
func TestTwoImportsOnOneRevisionCannotBothFinalize(t *testing.T) {
	f := setup(t)
	first := publish(t, f, f.a, newRequest())

	begin := func(instructions string) creativeskill.Import {
		t.Helper()
		req := newRequest()
		req.Resources = nil
		req.Instructions = instructions
		req.ExpectedSkillRevision = first.SkillRevision
		record, err := f.svc.BeginImport(t.Context(), f.a, req)
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	// Both see the same revision and both are accepted: nothing has moved yet.
	mine, theirs := begin("我的第二版。"), begin("他的第二版。")

	if _, err := f.svc.FinalizeImport(t.Context(), f.a, mine.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.FinalizeImport(t.Context(), f.a, theirs.ID); !errors.Is(err, creativeskill.ErrRevisionConflict) {
		t.Fatalf("the second finalize must not silently publish over the first: %v", err)
	}
	var versions int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_skill_versions WHERE skill_id=$1`, first.SkillID).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 2 {
		t.Fatalf("expected the original plus one winner, got %d versions", versions)
	}
	// The loser is kept, not discarded: a publisher decides explicitly whether
	// to retry against the revision that actually landed.
	var state string
	if err := f.db.QueryRow(`SELECT state FROM creative_skill_imports WHERE id=$1`, theirs.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "ready" {
		t.Fatalf("a refused finalize must leave its import intact, got %q", state)
	}
}

// An import that outlived its window may have had its staged objects reclaimed
// already, so retrying its operation must say so instead of handing back a
// record that can never finalise.
func TestAnExpiredImportIsReportedRatherThanResumed(t *testing.T) {
	f := setup(t)
	req := newRequest()
	record, err := f.svc.BeginImport(t.Context(), f.a, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE creative_skill_imports SET expires_at=clock_timestamp()-interval '1 hour'
		WHERE id=$1`, record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.BeginImport(t.Context(), f.a, req); !errors.Is(err, creativeskill.ErrImportExpired) {
		t.Fatalf("retrying an expired operation must report expiry: %v", err)
	}
	var state string
	if err := f.db.QueryRow(`SELECT state FROM creative_skill_imports WHERE id=$1`, record.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "expired" {
		t.Fatalf("the attempt must be recorded as expired, got %q", state)
	}
	// Expiry is not "wrong state": the caller needs to know a new operation id
	// is required, which "wrong state" never tells them.
	if _, err := f.svc.StageResource(t.Context(), f.a, record.ID, "references/comparison-checklist.md",
		bytes.NewReader(checklist)); !errors.Is(err, creativeskill.ErrImportExpired) {
		t.Fatalf("staging onto an expired import must report expiry: %v", err)
	}
}

// gatedPort holds the first upload inside the object store so a second caller
// can finish its whole staging transaction first. That is the real window: the
// bytes are durable, but nothing has recorded them yet.
type gatedPort struct {
	creativeskill.ObjectPort
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *gatedPort) PublishVerified(ctx context.Context, key string, body io.Reader, size int64, mime string) (string, error) {
	version, err := g.ObjectPort.PublishVerified(ctx, key, body, size, mime)
	first := false
	g.once.Do(func() { first = true })
	if first {
		close(g.entered)
		<-g.release
	}
	return version, err
}

// Two callers staging one declared file upload identical bytes by construction,
// because both were checked against the same declared hash. On the local driver
// an object's version is its content hash, so both publish the very same file.
// The caller that loses the race must therefore never "clean up its own"
// object: that file is the one the import kept, and deleting it would leave a
// frozen version pointing at bytes that no longer exist — while the freeze
// itself still reported success.
func TestALosingConcurrentStageMustNotDeleteTheWinnersObject(t *testing.T) {
	f := setup(t)
	gate := &gatedPort{
		ObjectPort: f.objects,
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	svc, err := creativeskill.NewService(gate)
	if err != nil {
		t.Fatal(err)
	}
	record, err := svc.BeginImport(t.Context(), f.a, newRequest())
	if err != nil {
		t.Fatal(err)
	}
	const declared = "references/comparison-checklist.md"

	late := make(chan error, 1)
	go func() {
		_, err := svc.StageResource(t.Context(), f.a, record.ID, declared, bytes.NewReader(checklist))
		late <- err
	}()
	<-gate.entered // Its bytes are published; its record is not written yet.

	// The second caller runs to completion and moves the import to ready.
	if _, err := svc.StageResource(t.Context(), f.a, record.ID, declared, bytes.NewReader(checklist)); err != nil {
		t.Fatal(err)
	}
	close(gate.release)
	if err := <-late; err != nil {
		t.Fatalf("the late caller must find its bytes already recorded, not fail: %v", err)
	}

	version, err := svc.FinalizeImport(t.Context(), f.a, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := svc.ReadResource(t.Context(), f.a, version.ID, declared)
	if err != nil {
		t.Fatalf("the frozen version lost its bytes to the late caller's cleanup: %v", err)
	}
	if !bytes.Equal(body, checklist) {
		t.Fatal("the frozen version no longer reads its own content")
	}
}
