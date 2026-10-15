package creativeskill_test

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// seedPackagesDir is what the image carries to /usr/local/share/creative-skills
// and what every server start syncs into the platform catalog.
const seedPackagesDir = "../../../deploy/creative-skills"

// Every start runs the sync, so a second run over the same directory has to
// leave the catalog exactly as the first one did. It is also the check that
// every package in the repository can be read at all.
func TestSyncPlatformPublishesTheRepositoryPackagesOnce(t *testing.T) {
	f := setup(t)
	first, err := f.svc.SyncPlatform(t.Context(), os.DirFS(seedPackagesDir))
	if err != nil {
		t.Fatal(err)
	}
	if first.Failed != 0 || len(first.Published) == 0 {
		t.Fatalf("the repository packages must all publish: %+v", first)
	}
	again, err := f.svc.SyncPlatform(t.Context(), os.DirFS(seedPackagesDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Published) != 0 || again.Unchanged != len(first.Published) {
		t.Fatalf("an unchanged directory published again: %+v", again)
	}
	if n := countVersions(t, f); n != len(first.Published) {
		t.Fatalf("two syncs froze %d versions for %d packages", n, len(first.Published))
	}
}

// A deployment that imported the seed by hand before sync existed must not get
// a duplicate version of the same content on its first sync.
func TestSyncPlatformLeavesAHandImportedSeedAlone(t *testing.T) {
	f := setup(t)
	pkg, err := creativeskill.ReadPackage(os.DirFS(seedPackageDir))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Publish(t.Context(), f.a, pkg, creativeskill.PublishIntent{
		OperationID: seedOperationID, Origin: "platform", Activate: true,
	}); err != nil {
		t.Fatal(err)
	}
	synced, err := f.svc.SyncPlatform(t.Context(), os.DirFS(seedPackagesDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(synced.Published) != 0 || countVersions(t, f) != 1 {
		t.Fatalf("the sync republished content already current: %+v", synced)
	}
}

// Editing a package is how a built-in skill changes: the next sync appends a
// version and makes it current, while the old version stays for work pinned to it.
func TestSyncPlatformPublishesAChangedPackageAsTheCurrentVersion(t *testing.T) {
	f := setup(t)
	packages := fstest.MapFS{
		"a.v1/manifest.json": {Data: []byte(seedManifest)},
		"a.v1/SKILL.md":      {Data: []byte("第一版")},
	}
	if _, err := f.svc.SyncPlatform(t.Context(), packages); err != nil {
		t.Fatal(err)
	}
	before := currentVersion(t, f, "a")
	packages["a.v1/SKILL.md"] = &fstest.MapFile{Data: []byte("第二版")}
	synced, err := f.svc.SyncPlatform(t.Context(), packages)
	if err != nil {
		t.Fatal(err)
	}
	if len(synced.Published) != 1 || synced.Published[0] != "a" {
		t.Fatalf("the edited package was not published: %+v", synced)
	}
	after := currentVersion(t, f, "a")
	if after == before {
		t.Fatal("the new version did not become current")
	}
	var instructions string
	if err := f.db.QueryRow(`SELECT instructions FROM creative_skill_versions WHERE id=$1`, after).
		Scan(&instructions); err != nil {
		t.Fatal(err)
	}
	if instructions != "第二版" {
		t.Fatalf("the current version carries %q", instructions)
	}
	if n := countVersions(t, f); n != 2 {
		t.Fatalf("the old version must stay: %d versions", n)
	}
}

// A broken package is reported, and the packages next to it still sync.
func TestSyncPlatformCarriesOnPastABrokenPackage(t *testing.T) {
	f := setup(t)
	synced, err := f.svc.SyncPlatform(t.Context(), fstest.MapFS{
		"a.v1/manifest.json": {Data: []byte(seedManifest)},
		"a.v1/SKILL.md":      {Data: []byte("正文")},
		"broken/SKILL.md":    {Data: []byte("没有声明")},
		"README.md":          {Data: []byte("不是包")},
	})
	if err == nil {
		t.Fatal("a broken package must be reported")
	}
	if synced.Failed != 1 || len(synced.Published) != 1 {
		t.Fatalf("one package failed and one should still publish: %+v", synced)
	}
}

// Composed services file built-in skills under the reserved owner, which is no
// account at all; every account still finds them and can resolve them.
func TestBuiltInSkillsUnderThePlatformOwnerReachEveryAccount(t *testing.T) {
	f := setup(t)
	svc, err := creativeskill.NewService(f.objects,
		f.store.ScopeFor(auth.AccountContext{AccountID: creativeskill.PlatformOwner}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncPlatform(t.Context(), os.DirFS(seedPackagesDir)); err != nil {
		t.Fatal(err)
	}
	for _, reader := range []store.AccountScope{f.a, f.b} {
		page, err := svc.ListAccessibleSkills(t.Context(), reader, "", "", 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 0 {
			t.Fatalf("%s sees no built-in skill", reader.AccountID())
		}
		for _, entry := range page.Items {
			if entry.Origin != "platform" || entry.OwnerAccountID != creativeskill.PlatformOwner {
				t.Fatalf("%s sees %+v", reader.AccountID(), entry)
			}
			if _, err := svc.ResolveVersion(t.Context(), reader, entry.SkillID, entry.VersionID); err != nil {
				t.Fatalf("%s cannot resolve %s: %v", reader.AccountID(), entry.Slug, err)
			}
		}
	}
}

// A sync that failed partway and was not retried within the import window has
// left an expired import under the id it derives. The next start must recover
// with a fresh attempt rather than fail on that id forever, and the start after
// that must not publish again.
func TestSyncPlatformRecoversFromAnExpiredAttempt(t *testing.T) {
	f := setup(t)
	port := &failingPort{ObjectPort: f.objects, fail: true}
	svc, err := creativeskill.NewService(port, f.platform)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncPlatform(t.Context(), os.DirFS(seedPackagesDir)); err == nil {
		t.Fatal("a sync whose uploads fail must report it")
	}
	if _, err := f.db.Exec(`UPDATE creative_skill_imports
		SET expires_at=clock_timestamp()-interval '25 hours'`); err != nil {
		t.Fatal(err)
	}
	port.fail = false
	for run := range 2 {
		synced, err := svc.SyncPlatform(t.Context(), os.DirFS(seedPackagesDir))
		if err != nil {
			t.Fatalf("run %d after recovery: %v", run+1, err)
		}
		if synced.Failed != 0 {
			t.Fatalf("run %d after recovery: %+v", run+1, synced)
		}
	}
	if n := countVersions(t, f); n != 1 {
		t.Fatalf("recovery froze %d versions", n)
	}
}

// Two directories naming one slug would take turns being current, appending a
// version on every start. Neither is published, and the report names both.
func TestSyncPlatformRefusesTwoPackagesForOneSlug(t *testing.T) {
	f := setup(t)
	packages := fstest.MapFS{
		"a.v1/manifest.json": {Data: []byte(seedManifest)},
		"a.v1/SKILL.md":      {Data: []byte("第一版")},
		"a.v2/manifest.json": {Data: []byte(seedManifest)},
		"a.v2/SKILL.md":      {Data: []byte("第二版")},
		"b.v1/manifest.json": {Data: []byte(strings.Replace(seedManifest, `"key":"a"`, `"key":"b"`, 1))},
		"b.v1/SKILL.md":      {Data: []byte("正文")},
	}
	for run := range 2 {
		synced, err := f.svc.SyncPlatform(t.Context(), packages)
		if err == nil || !strings.Contains(err.Error(), "a.v1") || !strings.Contains(err.Error(), "a.v2") {
			t.Fatalf("run %d: the conflict must name both directories: %v", run+1, err)
		}
		if synced.Failed != 2 {
			t.Fatalf("run %d: %+v", run+1, synced)
		}
	}
	if n := countVersions(t, f); n != 1 {
		t.Fatalf("only the unambiguous package may publish: %d versions", n)
	}
}

// failingPort refuses uploads while fail is set, standing in for an object
// store that is down.
type failingPort struct {
	creativeskill.ObjectPort
	fail bool
}

func (p *failingPort) PublishVerified(ctx context.Context, key string, body io.Reader, size int64, mime string) (string, error) {
	if p.fail {
		return "", errors.New("object store unavailable")
	}
	return p.ObjectPort.PublishVerified(ctx, key, body, size, mime)
}

// Without a platform scope there is no catalog to sync into.
func TestSyncPlatformNeedsAPlatformScope(t *testing.T) {
	f := setup(t)
	svc, err := creativeskill.NewService(f.objects, store.AccountScope{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncPlatform(t.Context(), os.DirFS(seedPackagesDir)); !errors.Is(err, creativeskill.ErrOriginNotPermitted) {
		t.Fatalf("a service with no platform scope synced anyway: %v", err)
	}
}

func countVersions(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_skill_versions WHERE account_id=$1`,
		platformAccount).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func currentVersion(t *testing.T, f *fixture, slug string) string {
	t.Helper()
	var id string
	if err := f.db.QueryRow(`SELECT current_version_id FROM creative_skills WHERE account_id=$1 AND slug=$2`,
		platformAccount, slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
