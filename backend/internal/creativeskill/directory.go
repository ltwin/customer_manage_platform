package creativeskill

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// maxCatalogPage bounds one directory request. The picker asks on every
// keystroke, and a page is a database round trip per scope.
const maxCatalogPage = 50

// maxCatalogQuery bounds the search term. A longer one cannot match anything —
// slugs stop at 64 characters — so accepting it would only widen what reaches
// the pattern match.
const maxCatalogQuery = 64

// CatalogEntry is one choosable skill: the version its author currently
// recommends, plus the identity a caller needs to name it later.
//
// The name and description come from the skill, the version number, digest and
// declaration from the version. That split is deliberate: renaming a skill
// should change what the picker shows immediately, while what a run executes is
// fixed by the version and must not move when a name does.
//
// There are no json tags here. This is a domain value; the wire shape belongs
// to whoever answers the request, and the two should be free to differ.
type CatalogEntry struct {
	SkillID   string
	VersionID string
	// OwnerAccountID is the publishing account. A platform skill is owned by the
	// trusted publisher, not by the account reading it, and a message that
	// records this reference has to keep that fact.
	OwnerAccountID string
	Origin         string
	Slug           string
	DisplayName    string
	Description    string
	VersionNumber  int
	Digest         string
	MaxInputs      int
	// ToolAllowlist is the version's declared upper bound. The directory reports
	// it rather than judging it: whether this deployment has registered those
	// tools is not something the skill store knows.
	ToolAllowlist []string
}

type CatalogPage struct {
	Items      []CatalogEntry
	NextCursor string
}

// ListAccessibleSkills answers what one account may choose right now: its own
// skills and the deployment's trusted platform catalog, merged into one page.
//
// The cross-account half happens here and nowhere else. A caller hands in its
// own scope and receives value objects; the platform publisher's AccountScope
// never leaves this package, so no consumer can turn "read the platform
// catalog" into "read another account's rows" (§5). Every query underneath is
// still account-scoped — there is no unscoped "find any version" read.
//
// Only choosable entries are listed: a skill whose author switched it off, or
// whose recommended version was withdrawn, is absent rather than present with a
// reason. A reason is worth showing when the account could act on it, and in
// this phase no account can publish or re-enable anything through the API.
func (s *Service) ListAccessibleSkills(ctx context.Context, scope store.AccountScope, query, cursor string, limit int) (CatalogPage, error) {
	if limit < 1 || limit > maxCatalogPage {
		return CatalogPage{}, creativeops.ErrValidation
	}
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) > maxCatalogQuery || !utf8.ValidString(query) {
		return CatalogPage{}, creativeops.ErrValidation
	}
	// The cursor is bound to the reader and to the search term. A position taken
	// under one query means nothing under another, and rebinding it silently
	// would skip rows rather than fail.
	cursorQuery := "creative_skills:" + query
	key, err := creativeops.DecodePageCursor(cursor, scope.AccountID(), cursorQuery)
	if err != nil {
		return CatalogPage{}, err
	}
	rows, err := s.listSkillRows(ctx, scope, false, query, cursor != "", key, limit+1)
	if err != nil {
		return CatalogPage{}, err
	}
	// The viewer may itself be the platform publisher, in which case its own
	// listing already contains the platform skills.
	if platform := s.platform.AccountID(); platform != "" && platform != scope.AccountID() {
		trusted, err := s.listSkillRows(ctx, s.platform, true, query, cursor != "", key, limit+1)
		if err != nil {
			return CatalogPage{}, err
		}
		rows = mergeByRecency(rows, trusted, limit+1)
	}
	page := CatalogPage{Items: []CatalogEntry{}}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		// The cursor is taken from the skill rows, before withdrawn versions are
		// dropped below. Deriving it from the surviving entries instead would
		// make a page of entirely withdrawn skills report no next position and
		// hide everything behind them.
		page.NextCursor, err = creativeops.EncodePageCursor(scope.AccountID(), cursorQuery, last.updatedAt, last.id)
		if err != nil {
			return CatalogPage{}, err
		}
	}
	items, err := s.attachVersions(ctx, scope, rows)
	if err != nil {
		return CatalogPage{}, err
	}
	page.Items = items
	return page, nil
}

// skillRow is one row of the discovery table, before its recommended version is
// read. It carries the account it was read under so the second query goes back
// to the same scope.
type skillRow struct {
	id, slug, origin, displayName, description, versionID, ownerAccountID string
	updatedAt                                                             time.Time
}

// listSkillCond is one constant with fixed placeholders rather than a string
// assembled per call. Optional conditions are expressed as "argument absent or
// argument matches", so the argument positions never shift and the whole
// predicate stays readable as a single piece of SQL.
//
// A skill with no recommended version has nothing to choose yet, and a
// switched-off one cannot be started; neither belongs in a picker. Slug matches
// by prefix and display name by substring: a slash command is typed from the
// front, a name is recognised anywhere inside it.
const listSkillCond = "current_version_id IS NOT NULL AND availability='active'" +
	" AND ($2::timestamptz IS NULL OR (updated_at,id)<($2::timestamptz,$3::text))" +
	" AND ($4::text='' OR slug LIKE $4::text||'%' OR display_name ILIKE '%'||$4::text||'%')"

// listPlatformSkillCond additionally excludes the trusted publisher's private
// skills. Being the platform account does not make everything it owns platform;
// only what it published as such is shared.
const listPlatformSkillCond = listSkillCond + " AND origin='platform'"

func (s *Service) listSkillRows(ctx context.Context, scope store.AccountScope, platformOnly bool,
	query string, seeking bool, key creativeops.PageCursor, limit int) ([]skillRow, error) {
	cond := listSkillCond
	if platformOnly {
		cond = listPlatformSkillCond
	}
	var since *time.Time
	if seeking {
		since = &key.Time
	}
	var rows []skillRow
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		result, err := tx.QueryPage(ctx, "creative_skills",
			"id,slug,origin,display_name,description,current_version_id,updated_at", cond,
			[]store.OrderBy{{Column: "updated_at", Desc: true}, {Column: "id", Desc: true}}, limit, 0,
			since, key.ID, escapeLike(query))
		if err != nil {
			return err
		}
		defer result.Close()
		for result.Next() {
			var r skillRow
			if err := result.Scan(&r.id, &r.slug, &r.origin, &r.displayName, &r.description, &r.versionID, &r.updatedAt); err != nil {
				return err
			}
			r.ownerAccountID = scope.AccountID()
			rows = append(rows, r)
		}
		return result.Err()
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// mergeByRecency interleaves two already-sorted listings into one, keeping the
// order the cursor depends on. Sorting the concatenation would work too; this
// says out loud that both inputs are ordered and that the merged order is the
// same one the next cursor will be read against.
func mergeByRecency(own, trusted []skillRow, limit int) []skillRow {
	merged := make([]skillRow, 0, len(own)+len(trusted))
	merged = append(merged, own...)
	merged = append(merged, trusted...)
	sort.SliceStable(merged, func(i, j int) bool {
		if !merged[i].updatedAt.Equal(merged[j].updatedAt) {
			return merged[i].updatedAt.After(merged[j].updatedAt)
		}
		return merged[i].id > merged[j].id
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged
}

// attachVersions reads the recommended version of each listed skill. The rows
// are grouped by the account they were read under, because a platform skill's
// version rows are the publisher's and must be read in the publisher's scope.
func (s *Service) attachVersions(ctx context.Context, scope store.AccountScope, rows []skillRow) ([]CatalogEntry, error) {
	if len(rows) == 0 {
		return []CatalogEntry{}, nil
	}
	byOwner := map[string][]string{}
	for _, r := range rows {
		byOwner[r.ownerAccountID] = append(byOwner[r.ownerAccountID], r.versionID)
	}
	versions := map[string]catalogVersion{}
	for owner, ids := range byOwner {
		sc := scope
		if owner != scope.AccountID() {
			sc = s.platform
		}
		found, err := s.readCatalogVersions(ctx, sc, ids)
		if err != nil {
			return nil, err
		}
		for id, v := range found {
			versions[id] = v
		}
	}
	items := make([]CatalogEntry, 0, len(rows))
	for _, r := range rows {
		version, ok := versions[r.versionID]
		// The recommended pointer is only trusted as far as the row it points
		// at agrees. current_version_id has no foreign key, so a pointer that
		// ever landed on another skill's version would otherwise publish that
		// version's digest and declaration into every account's catalog.
		if ok && version.skillID != r.id {
			ok = false
		}
		if !ok {
			// The recommended version was withdrawn. Dropping it here rather than
			// filtering in SQL keeps the two reads independent: the skills query
			// decides the page, the versions query decides what is startable.
			continue
		}
		items = append(items, CatalogEntry{
			SkillID: r.id, VersionID: r.versionID, OwnerAccountID: r.ownerAccountID,
			Origin: r.origin, Slug: r.slug, DisplayName: r.displayName, Description: r.description,
			VersionNumber: version.number, Digest: version.digest,
			MaxInputs: version.manifest.MaxInputs, ToolAllowlist: version.manifest.ToolAllowlist,
		})
	}
	return items, nil
}

type catalogVersion struct {
	skillID  string
	number   int
	digest   string
	manifest Manifest
}

func (s *Service) readCatalogVersions(ctx context.Context, scope store.AccountScope, ids []string) (map[string]catalogVersion, error) {
	found := map[string]catalogVersion{}
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		rows, err := tx.QueryPage(ctx, "creative_skill_versions", "id,skill_id,version_number,digest,manifest",
			"id = ANY($2::text[]) AND execution_status='active'",
			[]store.OrderBy{{Column: "id"}}, len(ids), 0, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var v catalogVersion
			var manifest []byte
			if err := rows.Scan(&id, &v.skillID, &v.number, &v.digest, &manifest); err != nil {
				return err
			}
			if v.manifest, err = decodeManifest(manifest); err != nil {
				return err
			}
			found[id] = v
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// escapeLike neutralises the pattern characters in a search term. Without it a
// photographer typing "%" would match every skill, and "_" would silently match
// one character of anything.
func escapeLike(q string) string {
	var b strings.Builder
	b.Grow(len(q))
	for _, r := range q {
		if r == '%' || r == '_' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
