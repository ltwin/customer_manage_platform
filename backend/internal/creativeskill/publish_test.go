package creativeskill_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// seedPackageDir is the platform package this deployment publishes. It lives
// outside the Go module's domain packages because nothing compiles it any more:
// the running server reads skills from the database, the import command reads
// this directory, and what gets published is whatever an operator can see here.
const seedPackageDir = "../../../deploy/creative-skills/reference-direction.v1"

// The deployment publishes this package once. Running the import again with the
// same operation id has to be a replay, because that is what makes the seed
// step safe to put in a deploy script that reruns on every release.
func TestTheSeedPackageImportsIdempotently(t *testing.T) {
	f := setup(t)
	pkg, err := creativeskill.ReadPackage(os.DirFS(seedPackageDir))
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Slug != "reference-direction" {
		t.Fatalf("the seed package publishes under slug %q", pkg.Slug)
	}
	// The seed's operation id is a literal the deployment pins, not a value
	// generated per run: a fresh id every release would publish a new version
	// of identical content on every deploy.
	intent := creativeskill.PublishIntent{
		OperationID: seedOperationID, Origin: "platform", Activate: true,
	}
	first, err := f.svc.Publish(t.Context(), f.a, pkg, intent)
	if err != nil {
		t.Fatal(err)
	}
	if first.VersionNumber != 1 || first.Digest == "" {
		t.Fatalf("the seed import must freeze version 1 with a digest: %+v", first)
	}
	again, err := f.svc.Publish(t.Context(), f.a, pkg, intent)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || again.Digest != first.Digest {
		t.Fatalf("rerunning the seed import produced %q, not %q", again.ID, first.ID)
	}
	var versions int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_skill_versions WHERE account_id='skill-a'`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 1 {
		t.Fatalf("two seed runs produced %d versions", versions)
	}
	// The published resource is the file on disk, not a copy that drifted.
	const declared = "references/comparison-checklist.md"
	want, err := os.ReadFile(filepath.Join(seedPackageDir, declared))
	if err != nil {
		t.Fatal(err)
	}
	body, err := f.svc.ReadResource(t.Context(), f.a, first.SkillID, first.ID, declared)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, want) {
		t.Fatal("the frozen resource does not match the package file")
	}
}

// An import that ran out of window owns objects nobody will ever reference.
// Reclaiming them is an explicit command in this phase: no timer runs, so an
// operator has to be able to see what one sweep actually did.
func TestReclaimDropsTheObjectsOfAnExpiredImportAndLeavesFrozenOnesAlone(t *testing.T) {
	f := setup(t)
	kept := publish(t, f, f.a, newRequest())

	abandoned := newRequest()
	abandoned.OperationID = uuid.NewString()
	abandoned.ExpectedSkillRevision = kept.SkillRevision
	record, err := f.svc.BeginImport(t.Context(), f.a, abandoned)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.StageResource(t.Context(), f.a, record.ID, "references/comparison-checklist.md",
		bytes.NewReader(checklist)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE creative_skill_imports SET expires_at=clock_timestamp()-interval '1 hour'
		WHERE id=$1`, record.ID); err != nil {
		t.Fatal(err)
	}

	reclaimed, err := f.svc.ReclaimExpiredImports(t.Context(), f.a, 50)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed.Imports != 1 || reclaimed.Objects != 1 {
		t.Fatalf("one expired import with one staged file swept as %+v", reclaimed)
	}
	var state string
	var staged string
	if err := f.db.QueryRow(`SELECT state, staged_objects::text FROM creative_skill_imports WHERE id=$1`,
		record.ID).Scan(&state, &staged); err != nil {
		t.Fatal(err)
	}
	if state != "expired" || staged != "[]" {
		t.Fatalf("the swept import is %q holding %s", state, staged)
	}
	// The frozen version published before the sweep keeps its bytes: retention
	// for a version is that nothing deletes it, and reclamation is not an
	// exception to that.
	if _, err := f.svc.ReadResource(t.Context(), f.a, kept.SkillID, kept.ID, "references/comparison-checklist.md"); err != nil {
		t.Fatalf("the sweep reached a frozen version: %v", err)
	}
	// A second sweep has nothing left to do rather than failing on objects it
	// already deleted.
	if again, err := f.svc.ReclaimExpiredImports(t.Context(), f.a, 50); err != nil || again.Imports != 0 {
		t.Fatalf("resweeping reported %+v (%v)", again, err)
	}
}

// A live import is not a leftover. Sweeping one would delete bytes a finalize
// is about to freeze, and the freeze would still report success.
func TestReclaimLeavesAnImportInsideItsWindowAlone(t *testing.T) {
	f := setup(t)
	record, err := f.svc.BeginImport(t.Context(), f.a, newRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.StageResource(t.Context(), f.a, record.ID, "references/comparison-checklist.md",
		bytes.NewReader(checklist)); err != nil {
		t.Fatal(err)
	}
	if reclaimed, err := f.svc.ReclaimExpiredImports(t.Context(), f.a, 50); err != nil || reclaimed.Imports != 0 {
		t.Fatalf("a live import was swept: %+v (%v)", reclaimed, err)
	}
	version, err := f.svc.FinalizeImport(t.Context(), f.a, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ReadResource(t.Context(), f.a, version.SkillID, version.ID, "references/comparison-checklist.md"); err != nil {
		t.Fatal(err)
	}
}

// Every file in the directory is a declared resource. A package cannot carry
// something a run could reach but the version never described.
func TestReadPackageDeclaresEveryFileItFinds(t *testing.T) {
	pkg, err := creativeskill.ReadPackage(fstest.MapFS{
		"manifest.json":   {Data: []byte(seedManifest)},
		"SKILL.md":        {Data: []byte("正文")},
		"references/a.md": {Data: []byte("甲")},
		"references/b.md": {Data: []byte("乙")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Resources) != 2 {
		t.Fatalf("declared %d of 2 files", len(pkg.Resources))
	}
	for _, res := range pkg.Resources {
		if _, ok := pkg.Body(res.Path); !ok {
			t.Fatalf("%s is declared with no bytes behind it", res.Path)
		}
		if res.ByteSize < 1 || len(res.SHA256) != 64 {
			t.Fatalf("%s is declared as %+v", res.Path, res)
		}
	}
}

// A package that grants tool access is read strictly: a misspelled field would
// otherwise import a narrower skill than the file plainly describes, and no one
// reading the directory afterwards would see why.
func TestReadPackageRefusesAManifestFieldItDoesNotKnow(t *testing.T) {
	_, err := creativeskill.ReadPackage(fstest.MapFS{
		"manifest.json": {Data: []byte(`{"manifest_schema_version":1,"key":"a","version":1,` +
			`"display_name":"名","description":"述","input_kinds":["text"],"max_inputs":1,` +
			`"tool_allowlist":["read_nodes@1"],"required_model_capabilities":[],` +
			`"output_contract":"契约","completion_check_key":"k","tool_allowlst":["write_nodes@1"]}`)},
		"SKILL.md": {Data: []byte("正文")},
	})
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("an unknown manifest field must stop the import: %v", err)
	}
}

// seedOperationID is the identity the deploy script reuses; see
// docs/dev/creative-skill-import.md.
const seedOperationID = "4d8f4d0e-7a2b-4c6d-9f31-5eed00000001"

const seedManifest = `{"manifest_schema_version":1,"key":"a","version":1,` +
	`"display_name":"名","description":"述","input_kinds":["text"],"max_inputs":1,` +
	`"tool_allowlist":["read_nodes@1"],"required_model_capabilities":[],` +
	`"output_contract":"契约","completion_check_key":"k"}`

// A .DS_Store an operator never sees must not become a declared resource of a
// version nothing deletes, and must not silently change the content digest and
// burn the pinned seed operation id.
func TestReadPackageRefusesWhatAnOperatorCannotSee(t *testing.T) {
	for _, name := range []string{".DS_Store", "references/.hidden.md", ".git/config"} {
		t.Run(name, func(t *testing.T) {
			files := fstest.MapFS{
				"manifest.json": {Data: []byte(seedManifest)},
				"SKILL.md":      {Data: []byte("正文")},
				name:            {Data: []byte("看不见的字节")},
			}
			if _, err := creativeskill.ReadPackage(files); !errors.Is(err, creativeops.ErrValidation) {
				t.Fatalf("a hidden entry must stop the import: %v", err)
			}
		})
	}
	// An empty file is refused where the path can still be named.
	_, err := creativeskill.ReadPackage(fstest.MapFS{
		"manifest.json":      {Data: []byte(seedManifest)},
		"SKILL.md":           {Data: []byte("正文")},
		"references/void.md": {Data: []byte("")},
	})
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("an empty resource must be refused: %v", err)
	}
}

// One import that can never be deleted must not shadow every import behind it.
// The listing is oldest-first, so aborting on the first failure would make every
// future sweep stop on the same row forever.
func TestOneUnreclaimableImportDoesNotBlockTheRest(t *testing.T) {
	f := setup(t)
	// The stuck one is deliberately the oldest, because the sweep walks
	// oldest-first: if it did not sort ahead, this test would pass without ever
	// exercising the head-of-line case it is named for.
	stale := expire(t, f, staleDriverImport(t, f), "2 hours")
	good := expire(t, f, stagedImport(t, f), "1 hour")

	reclaimed, err := f.svc.ReclaimExpiredImports(t.Context(), f.a, 50)
	if err == nil {
		t.Fatal("a sweep that could not finish must say so")
	}
	if reclaimed.Failed != 1 || reclaimed.Imports != 1 || reclaimed.Objects != 1 {
		t.Fatalf("the healthy import behind the stuck one was not reclaimed: %+v", reclaimed)
	}
	// The one it could finish is clear; the one it could not keeps its list so
	// the next run still knows which objects it owns.
	var cleared, held string
	if err := f.db.QueryRow(`SELECT staged_objects::text FROM creative_skill_imports WHERE id=$1`, good).Scan(&cleared); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT staged_objects::text FROM creative_skill_imports WHERE id=$1`, stale).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if cleared != "[]" || held == "[]" {
		t.Fatalf("cleared=%s held=%s", cleared, held)
	}
}

// A stuck import must not hold the head of the queue forever. The sweep takes a
// bounded page, so it is not enough to skip a failure inside one run: the next
// run has to select something else, or a page's worth of unreclaimable imports
// would hide every import behind them permanently.
func TestAStuckImportYieldsItsPlaceToTheNextSweep(t *testing.T) {
	f := setup(t)
	stale := expire(t, f, staleDriverImport(t, f), "2 hours")
	good := expire(t, f, stagedImport(t, f), "1 hour")

	// A page of one is the whole point: the stuck import fills it.
	first, err := f.svc.ReclaimExpiredImports(t.Context(), f.a, 1)
	if err == nil || first.Failed != 1 || first.Imports != 0 {
		t.Fatalf("the first sweep reported %+v (%v)", first, err)
	}
	second, err := f.svc.ReclaimExpiredImports(t.Context(), f.a, 1)
	if err != nil {
		t.Fatalf("the stuck import still owns the only page: %+v (%v)", second, err)
	}
	if second.Imports != 1 || second.Objects != 1 {
		t.Fatalf("the second sweep reported %+v", second)
	}
	var cleared, held string
	if err := f.db.QueryRow(`SELECT staged_objects::text FROM creative_skill_imports WHERE id=$1`, good).Scan(&cleared); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT staged_objects::text FROM creative_skill_imports WHERE id=$1`, stale).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if cleared != "[]" || held == "[]" {
		t.Fatalf("cleared=%s held=%s", cleared, held)
	}
}

// Deleting from the wrong bucket of the right driver is the dangerous case:
// an absent object is not an error, so the delete "succeeds" and the sweep
// would go on to clear the only record of where the bytes really are.
func TestReclaimRefusesAnObjectStagedIntoAnotherBucket(t *testing.T) {
	f := setup(t)
	id := expire(t, f, stagedImport(t, f), "1 hour")
	if _, err := f.db.Exec(`UPDATE creative_skill_imports
		SET staged_objects=jsonb_set(staged_objects,'{0,bucket}','"a-bucket-this-deployment-no-longer-uses"')
		WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := f.svc.ReclaimExpiredImports(t.Context(), f.a, 50)
	if !errors.Is(err, creativeskill.ErrResourceUnavailable) {
		t.Fatalf("a foreign bucket was swept as if it were ours: %+v (%v)", reclaimed, err)
	}
	if reclaimed.Failed != 1 || reclaimed.Objects != 0 {
		t.Fatalf("sweep reported %+v", reclaimed)
	}
	var held string
	if err := f.db.QueryRow(`SELECT staged_objects::text FROM creative_skill_imports WHERE id=$1`, id).Scan(&held); err != nil {
		t.Fatal(err)
	}
	if held == "[]" {
		t.Fatal("the import lost the record of objects this sweep never deleted")
	}
}

// This phase publishes UTF-8 reference text. The extension is not the gate,
// so the reader has to look at the bytes.
func TestReadPackageRefusesResourceBytesThatAreNotText(t *testing.T) {
	_, err := creativeskill.ReadPackage(fstest.MapFS{
		"manifest.json":       {Data: []byte(seedManifest)},
		"SKILL.md":            {Data: []byte("正文")},
		"references/cover.md": {Data: []byte{0xff, 0xfe, 0xfd}},
	})
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("a binary resource must be refused: %v", err)
	}
}

// The limit is checked against what the directory reports, before the bytes are
// read: a mistyped --package-dir should cost a stat, not a read of whatever it
// happened to point at.
func TestReadPackageRefusesAnOversizedFileWithoutReadingIt(t *testing.T) {
	const oversized = "references/huge.md"
	files := fstest.MapFS{
		"manifest.json": {Data: []byte(seedManifest)},
		"SKILL.md":      {Data: []byte("正文")},
		oversized:       {Data: bytes.Repeat([]byte("a"), 64<<10+1)},
	}
	_, err := creativeskill.ReadPackage(unreadable{FS: files, blocked: oversized})
	if !errors.Is(err, creativeskill.ErrLimit) {
		t.Fatalf("an oversized file must be refused by size alone: %v", err)
	}
}

// unreadable answers questions about a file but refuses to produce its bytes,
// which is how this test tells "checked the size" apart from "read it anyway
// and then measured".
type unreadable struct {
	fs.FS
	blocked string
}

func (u unreadable) Open(name string) (fs.File, error) {
	if name == u.blocked {
		return nil, errors.New("the reader opened a file it had already been told was too big")
	}
	return u.FS.Open(name)
}

func (u unreadable) Stat(name string) (fs.FileInfo, error) { return fs.Stat(u.FS, name) }

// stagedImport begins an import and stages its one declared file.
func stagedImport(t *testing.T, f *fixture) string {
	t.Helper()
	req := newRequest()
	req.OperationID = uuid.NewString()
	record, err := f.svc.BeginImport(t.Context(), f.a, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.StageResource(t.Context(), f.a, record.ID, "references/comparison-checklist.md",
		bytes.NewReader(checklist)); err != nil {
		t.Fatal(err)
	}
	return record.ID
}

// staleDriverImport rewrites a staged object to claim another driver, which is
// what a deployment that switched from oss to local leaves behind. The local
// adapter cannot delete an OSS version id, so this import can never be finished.
func staleDriverImport(t *testing.T, f *fixture) string {
	t.Helper()
	id := stagedImport(t, f)
	if _, err := f.db.Exec(`UPDATE creative_skill_imports
		SET staged_objects=jsonb_set(staged_objects,'{0,storage_driver}','"oss"') WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

// expire backdates one import. updated_at moves with expires_at because the
// sweep orders by it: a test that claims one import sorts ahead of another has
// to set the column that decides it.
func expire(t *testing.T, f *fixture, importID, ago string) string {
	t.Helper()
	if _, err := f.db.Exec(`UPDATE creative_skill_imports
		SET expires_at=clock_timestamp()-$2::interval, updated_at=clock_timestamp()-$2::interval
		WHERE id=$1`, importID, ago); err != nil {
		t.Fatal(err)
	}
	return importID
}
