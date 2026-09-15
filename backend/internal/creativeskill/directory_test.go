package creativeskill_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// named publishes one skill under its own slug so a listing has something to
// tell apart. Each publication is its own skill line, not a second version.
func named(t *testing.T, f *fixture, scope, slug, displayName, origin string) creativeskill.Version {
	t.Helper()
	req := newRequest()
	req.OperationID = uuid.NewString()
	req.Slug = slug
	req.DisplayName = displayName
	req.Origin = origin
	switch scope {
	case "a":
		return publish(t, f, f.a, req)
	default:
		return publish(t, f, f.b, req)
	}
}

// One account's picker shows its own skills and the platform catalog as a
// single list. Which side of that line a skill came from is a label on the
// entry, not a separate request the client has to make.
func TestTheDirectoryMergesOwnSkillsWithThePlatformCatalog(t *testing.T) {
	f := setup(t)
	platform := named(t, f, "a", "reference-direction", "参考整理与创作方向", "platform")
	mine := named(t, f, "b", "my-own-notes", "我的笔记整理", "account")
	// The publisher's private skill is not part of the catalog it publishes.
	hidden := named(t, f, "a", "private-line", "内部草稿", "account")

	page, err := f.svc.ListAccessibleSkills(t.Context(), f.b, "", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]creativeskill.CatalogEntry{}
	for _, entry := range page.Items {
		seen[entry.SkillID] = entry
	}
	if len(seen) != 2 || seen[platform.SkillID].SkillID == "" || seen[mine.SkillID].SkillID == "" {
		t.Fatalf("the merged listing is %+v", page.Items)
	}
	if _, leaked := seen[hidden.SkillID]; leaked {
		t.Fatal("the publisher's private skill appeared in the platform catalog")
	}
	if got := seen[platform.SkillID]; got.Origin != "platform" || got.OwnerAccountID != platformAccount {
		t.Fatalf("a platform entry must name its publisher: %+v", got)
	}
	if got := seen[mine.SkillID]; got.Origin != "account" || got.OwnerAccountID != "skill-b" {
		t.Fatalf("an own entry must name the account: %+v", got)
	}
	// The entry carries what a picker needs to name the version later, and the
	// declaration the deployment needs to judge whether it can run it.
	if got := seen[platform.SkillID]; got.VersionID != platform.ID || got.VersionNumber != 1 ||
		got.Digest != platform.Digest || got.MaxInputs != 20 || len(got.ToolAllowlist) != 2 {
		t.Fatalf("entry %+v", got)
	}
}

// Search is how the slash selector finds one skill among many. A slug is typed
// from the front; a display name is recognised from anywhere inside it.
func TestTheDirectorySearchesBySlugPrefixAndDisplayName(t *testing.T) {
	f := setup(t)
	direction := named(t, f, "a", "reference-direction", "参考整理与创作方向", "platform")
	retouch := named(t, f, "a", "retouch-notes", "修图要点", "platform")

	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{"slug 前缀", "reference", []string{direction.SkillID}},
		{"slug 中段不匹配前缀", "direction", nil},
		{"展示名子串", "修图", []string{retouch.SkillID}},
		{"两者都不匹配", "没有这个", nil},
		{"空查询返回全部", "", []string{direction.SkillID, retouch.SkillID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := f.svc.ListAccessibleSkills(t.Context(), f.b, tc.query, "", 20)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != len(tc.want) {
				t.Fatalf("%q matched %d of %d: %+v", tc.query, len(page.Items), len(tc.want), page.Items)
			}
			for _, id := range tc.want {
				found := false
				for _, entry := range page.Items {
					found = found || entry.SkillID == id
				}
				if !found {
					t.Fatalf("%q did not match %s", tc.query, id)
				}
			}
		})
	}
}

// A photographer typing a pattern character is typing text, not a pattern.
// All three matter: % spans anything, _ spans one character silently, and a
// backslash is the escape character itself.
func TestASearchTermIsNeverReadAsAPattern(t *testing.T) {
	f := setup(t)
	named(t, f, "a", "reference-direction", "参考整理与创作方向", "platform")

	for _, query := range []string{"%", "_", "\\", "reference%", "referenc_"} {
		t.Run(query, func(t *testing.T) {
			page, err := f.svc.ListAccessibleSkills(t.Context(), f.b, query, "", 20)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 0 {
				t.Fatalf("%q matched %d skills as if it were a pattern", query, len(page.Items))
			}
		})
	}
}

// I6's case: paging has to hold when both halves of the merge are producing
// rows. With only one side non-empty the merge is never exercised at a page
// boundary, which is exactly where it could repeat or skip a row.
func TestPagingHoldsWhenBothCatalogsProduceRows(t *testing.T) {
	f := setup(t)
	want := map[string]bool{}
	// Interleaved by publication order, so the merged order alternates between
	// the account's own skills and the platform catalog.
	want[named(t, f, "b", "mine-one", "我的一", "account").SkillID] = false
	want[named(t, f, "a", "platform-one", "平台一", "platform").SkillID] = false
	want[named(t, f, "b", "mine-two", "我的二", "account").SkillID] = false
	want[named(t, f, "a", "platform-two", "平台二", "platform").SkillID] = false

	cursor := ""
	for range len(want) {
		page, err := f.svc.ListAccessibleSkills(t.Context(), f.b, "", cursor, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("a page of one returned %d", len(page.Items))
		}
		id := page.Items[0].SkillID
		if seen, known := want[id]; !known || seen {
			t.Fatalf("%s came back twice or was never published", id)
		}
		want[id] = true
		cursor = page.NextCursor
	}
	for id, seen := range want {
		if !seen {
			t.Fatalf("%s was skipped", id)
		}
	}
}

// I5's case: a page can be empty and still have a next position, and the client
// has to keep going rather than read it as the end of the directory.
//
// The withdrawn skill is published last so it sorts first, which is what makes
// the first page empty. If the cursor were taken from the surviving entries
// instead of from the skill rows, this page would report no next position and
// the live skill behind it would be unreachable forever.
func TestAnEmptyPageCanStillHaveANextPosition(t *testing.T) {
	f := setup(t)
	live := named(t, f, "a", "live-line", "还在用的一条", "platform")
	withdrawn := named(t, f, "a", "withdrawn-line", "已停用的一条", "platform")
	if err := f.svc.DisableVersion(t.Context(), f.a, withdrawn.ID, "首版描述有误"); err != nil {
		t.Fatal(err)
	}

	first, err := f.svc.ListAccessibleSkills(t.Context(), f.b, "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 0 {
		t.Fatalf("the withdrawn skill was offered: %+v", first.Items)
	}
	if first.NextCursor == "" {
		t.Fatal("an empty page reported no next position, hiding everything behind it")
	}
	second, err := f.svc.ListAccessibleSkills(t.Context(), f.b, "", first.NextCursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].SkillID != live.SkillID {
		t.Fatalf("the live skill behind the empty page was not reached: %+v", second.Items)
	}
}

// Paging walks the whole directory exactly once. The cursor is bound to the
// reader and to the search term, because a position taken under one query means
// nothing under another.
func TestTheDirectoryPagesWithoutRepeatingOrSkipping(t *testing.T) {
	f := setup(t)
	want := map[string]bool{}
	for _, slug := range []string{"skill-one", "skill-two", "skill-three"} {
		want[named(t, f, "a", slug, "目录项 "+slug, "platform").SkillID] = false
	}
	cursor := ""
	for range len(want) {
		page, err := f.svc.ListAccessibleSkills(t.Context(), f.b, "", cursor, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("a page of one returned %d", len(page.Items))
		}
		id := page.Items[0].SkillID
		if seen, known := want[id]; !known || seen {
			t.Fatalf("page returned %s again or unexpectedly", id)
		}
		want[id] = true
		cursor = page.NextCursor
	}
	if cursor != "" {
		final, err := f.svc.ListAccessibleSkills(t.Context(), f.b, "", cursor, 1)
		if err != nil || len(final.Items) != 0 {
			t.Fatalf("paging past the end returned %+v (%v)", final.Items, err)
		}
	}
	// A cursor from one search is not a position in another.
	page, err := f.svc.ListAccessibleSkills(t.Context(), f.b, "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ListAccessibleSkills(t.Context(), f.b, "skill-one", page.NextCursor, 1); err == nil {
		t.Fatal("a cursor was reused under a different query")
	}
	// Nor is it a position in another account's listing.
	if _, err := f.svc.ListAccessibleSkills(t.Context(), f.a, "", page.NextCursor, 1); err == nil {
		t.Fatal("a cursor crossed accounts")
	}
}

// A withdrawn version is not choosable, so its skill is not offered. The
// version itself stays readable: a message that already froze it still renders.
func TestAWithdrawnVersionLeavesTheDirectory(t *testing.T) {
	f := setup(t)
	version := named(t, f, "a", "reference-direction", "参考整理与创作方向", "platform")
	other := named(t, f, "a", "retouch-notes", "修图要点", "platform")

	if err := f.svc.DisableVersion(t.Context(), f.a, version.ID, "首版描述有误"); err != nil {
		t.Fatal(err)
	}
	page, err := f.svc.ListAccessibleSkills(t.Context(), f.b, "", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].SkillID != other.SkillID {
		t.Fatalf("a withdrawn version is still offered: %+v", page.Items)
	}
	resolved, err := f.svc.ResolveVersion(t.Context(), f.b, version.SkillID, version.ID)
	if err != nil {
		t.Fatalf("a withdrawn version must still resolve for history: %v", err)
	}
	if resolved.Usable() || resolved.DisabledReason != "首版描述有误" {
		t.Fatalf("resolved as %+v", resolved.Version)
	}
}

// The page bounds are the directory's own, not the caller's.
func TestTheDirectoryRefusesAnUnboundedRequest(t *testing.T) {
	f := setup(t)
	for _, limit := range []int{0, -1, 51} {
		if _, err := f.svc.ListAccessibleSkills(t.Context(), f.a, "", "", limit); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("limit %d was accepted: %v", limit, err)
		}
	}
	long := make([]rune, 65)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := f.svc.ListAccessibleSkills(t.Context(), f.a, string(long), "", 20); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("an oversized search term was accepted: %v", err)
	}
}

// The recommended pointer has no foreign key, so the listing does not take it
// on faith. A pointer that landed on another skill's version would otherwise
// publish that version's digest and declaration — including its tool allowlist
// — into every account's catalog.
func TestARecommendedPointerIntoAnotherSkillIsNotFollowed(t *testing.T) {
	f := setup(t)
	shown := named(t, f, "a", "shown-line", "会被看到的一条", "platform")
	private := named(t, f, "a", "private-line", "内部草稿", "account")

	if _, err := f.db.Exec(`UPDATE creative_skills SET current_version_id=$2 WHERE id=$1`,
		shown.SkillID, private.ID); err != nil {
		t.Fatal(err)
	}
	page, err := f.svc.ListAccessibleSkills(t.Context(), f.b, "", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range page.Items {
		if entry.VersionID == private.ID || entry.Digest == private.Digest {
			t.Fatalf("a private version reached the catalog through a stray pointer: %+v", entry)
		}
	}
}
