package creativemedia_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func matches(t *testing.T, scope store.AccountScope, digests ...string) map[string]creativelibrary.Asset {
	t.Helper()
	found, err := creativelibrary.FindByDigest(t.Context(), scope, digests)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]creativelibrary.Asset{}
	for _, m := range found.Items {
		out[m.Digest] = m.Asset
	}
	return out
}

// An import is matched by its bytes, within the account, recycle bin included;
// the copy outside the bin wins over a trashed one, then the newest.
func TestDigestMatchesFindTheSameFileInTheAccountLibrary(t *testing.T) {
	f := setup(t)
	body := sample(t, "sample.png")
	other := solidPNG(t, color.NRGBA{10, 20, 30, 255})
	asset := func(scope store.AccountScope, title string, body []byte) creativelibrary.Asset {
		u := f.uploadFile(t, scope, title+".png", "image/png", "image", body, creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: title}})
		a, err := creativelibrary.GetAsset(t.Context(), scope, u.Publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	first := asset(f.a, "海边", body)
	sameTitle := asset(f.a, "海边", other)
	foreign := asset(f.b, "海边", body)

	got := matches(t, f.a, digestOf(body), digestOf(other), strings.Repeat("0", 64))
	if len(got) != 2 || got[digestOf(body)].ID != first.ID || got[digestOf(other)].ID != sameTitle.ID {
		t.Fatalf("matched by something other than content: %+v", got)
	}
	if got[digestOf(body)].Content == nil {
		t.Fatal("match carries no preview to show")
	}
	if got := matches(t, f.b, digestOf(body)); got[digestOf(body)].ID != foreign.ID {
		t.Fatalf("crossed accounts: %+v", got)
	}

	second := asset(f.a, "海边 2", body)
	if got := matches(t, f.a, digestOf(body)); got[digestOf(body)].ID != second.ID {
		t.Fatalf("newest copy not preferred: %+v", got)
	}
	_ = must[creativelibrary.LibraryResult](t)(creativelibrary.Trash(t.Context(), f.a, command(t, creativelibrary.AssetVersion{ID: second.ID, Revision: second.Revision})))
	if got := matches(t, f.a, digestOf(body)); got[digestOf(body)].ID != first.ID {
		t.Fatalf("trashed copy preferred over a live one: %+v", got)
	}
	_ = must[creativelibrary.LibraryResult](t)(creativelibrary.Trash(t.Context(), f.a, command(t, creativelibrary.AssetVersion{ID: first.ID, Revision: first.Revision})))
	if got := matches(t, f.a, digestOf(body)); got[digestOf(body)].ID != second.ID || got[digestOf(body)].DeletedAt == nil {
		t.Fatalf("recycle bin not matched: %+v", got)
	}

	tooMany := make([]string, creativelibrary.DigestLookupLimit+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("%064x", i)
	}
	for _, bad := range [][]string{nil, {"SHA256"}, {strings.ToUpper(digestOf(body))}, tooMany} {
		if _, err := creativelibrary.FindByDigest(t.Context(), f.a, bad); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("%d digests accepted: %v", len(bad), err)
		}
	}
}

// Replacing takes the import into the existing asset: out of the bin, under the
// imported title and in the import's group, keeping everything else.
func TestImportReplaceRestoresRenamesAndAddsTheGroup(t *testing.T) {
	f := setup(t)
	group := func(name string, hierarchy creativeops.Revision) creativelibrary.LibraryResult {
		return must[creativelibrary.LibraryResult](t)(creativelibrary.CreateGroup(t.Context(), f.a, command(t, creativelibrary.GroupInput{Name: name, HierarchyRevision: hierarchy})))
	}
	kept := group("人像", 1)
	target := group("海边", kept.HierarchyRevision)
	u := f.uploadFile(t, f.a, "海边.png", "image/png", "image", sample(t, "sample.png"), creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: "海边", GroupIDs: []string{kept.ID}, Favorite: true}})
	if _, err := f.db.Exec(`UPDATE creative_assets SET rating_halves=8,description='留档' WHERE id=$1`, u.Publication.ID); err != nil {
		t.Fatal(err)
	}
	a, err := creativelibrary.GetAsset(t.Context(), f.a, u.Publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = must[creativelibrary.LibraryResult](t)(creativelibrary.Trash(t.Context(), f.a, command(t, creativelibrary.AssetVersion{ID: a.ID, Revision: a.Revision})))

	replace := func(revision creativeops.Revision, title string, groups ...string) (creativeops.Receipt, error) {
		return creativelibrary.ReplaceByImport(t.Context(), f.a, command(t, creativelibrary.ImportReplaceInput{ID: a.ID, ExpectedRevision: revision, Title: title, GroupIDs: groups}))
	}
	if _, err = replace(a.Revision, "beach_final", target.ID); !errors.Is(err, creativelibrary.ErrVersionConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	r := must[creativelibrary.LibraryResult](t)(replace(a.Revision+1, "beach_final", target.ID))
	got, err := creativelibrary.GetAsset(t.Context(), f.a, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got.GroupIDs)
	want := []string{kept.ID, target.ID}
	slices.Sort(want)
	if got.DeletedAt != nil || got.Title != "beach_final" || !slices.Equal(got.GroupIDs, want) || got.Revision != r.Revision || r.Revision != a.Revision+2 {
		t.Fatalf("replace result %+v / %+v", got, r)
	}
	if got.Rating == nil || *got.Rating != 4 || !got.Favorite || got.Description != "留档" {
		t.Fatalf("replace lost what the asset had: %+v", got)
	}
	page, err := creativelibrary.SearchAssets(t.Context(), f.a, creativelibrary.Search{Limit: 10, Q: "beach_final"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != a.ID {
		t.Fatalf("new title not searchable: %+v %v", page, err)
	}

	same := must[creativelibrary.LibraryResult](t)(replace(got.Revision, "beach_final", target.ID))
	if same.Revision != got.Revision || same.LibraryRevision != r.LibraryRevision {
		t.Fatalf("a replace that changes nothing bumped revisions: %+v", same)
	}
	if _, err = replace(got.Revision, "beach_final", "no-such-group"); !errors.Is(err, creativelibrary.ErrNotFound) {
		t.Fatalf("unknown group accepted: %v", err)
	}
	if _, err = creativelibrary.ReplaceByImport(t.Context(), f.b, command(t, creativelibrary.ImportReplaceInput{ID: a.ID, ExpectedRevision: got.Revision, Title: "x"})); !errors.Is(err, creativelibrary.ErrNotFound) {
		t.Fatalf("another account replaced the asset: %v", err)
	}
}
