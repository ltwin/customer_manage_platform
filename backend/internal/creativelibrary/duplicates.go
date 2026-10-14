package creativelibrary

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary/textindex"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// DigestLookupLimit bounds one import check; the client asks in batches.
const DigestLookupLimit = 50

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// DigestMatch is a library asset whose original file has the bytes an import
// is about to upload.
type DigestMatch struct {
	Digest string `json:"digest"`
	Asset  Asset  `json:"asset"`
}
type DigestMatches struct {
	Items []DigestMatch `json:"items"`
}

// FindByDigest names, for each SHA-256 digest (lowercase hex), the asset that
// already holds the same file. The recycle bin counts too, since replacing
// restores. Of several matches the one outside the bin wins, then the newest.
func FindByDigest(ctx context.Context, sc store.AccountScope, digests []string) (DigestMatches, error) {
	digests, err := uniqueIDs(digests, DigestLookupLimit)
	if err != nil || len(digests) == 0 {
		return DigestMatches{}, creativeops.ErrValidation
	}
	keys := make([]string, len(digests))
	for i, d := range digests {
		if !digestPattern.MatchString(d) {
			return DigestMatches{}, creativeops.ErrValidation
		}
		keys[i] = "sha256-" + d
	}
	result := DigestMatches{Items: []DigestMatch{}}
	err = sc.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		blobs, err := pairs(ctx, tx, "creative_blobs", "id,sha256", "state='ready' AND sha256=ANY($2::text[])", keys)
		if err != nil || len(blobs) == 0 {
			return err
		}
		revisions, err := pairs(ctx, tx, "creative_content_objects", "content_revision_id,blob_id", "role='original' AND blob_id=ANY($2::text[])", keysOf(blobs))
		if err != nil || len(revisions) == 0 {
			return err
		}
		rows, err := tx.Query(ctx, "creative_assets", assetColumns, "content_revision_id=ANY($2::text[])", keysOf(revisions))
		if err != nil {
			return err
		}
		best := map[string]Asset{}
		for rows.Next() {
			a, e := scanAsset(rows)
			if e != nil {
				rows.Close()
				return e
			}
			digest := strings.TrimPrefix(blobs[revisions[a.ContentRevisionID]], "sha256-")
			if held, ok := best[digest]; !ok || preferred(a, held) {
				best[digest] = a
			}
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		items := []Asset{}
		for _, d := range digests {
			if a, ok := best[d]; ok {
				items = append(items, a)
				result.Items = append(result.Items, DigestMatch{Digest: d})
			}
		}
		if err = projectAssets(ctx, tx, items); err != nil {
			return err
		}
		for i := range items {
			result.Items[i].Asset = items[i]
		}
		return nil
	})
	if err != nil {
		return DigestMatches{}, err
	}
	return result, nil
}

func preferred(a, b Asset) bool {
	if (a.DeletedAt == nil) != (b.DeletedAt == nil) {
		return a.DeletedAt == nil
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID > b.ID
}

// pairs reads a two-column mapping, first column to second.
func pairs(ctx context.Context, tx store.ReadTxAccountScope, table, columns, cond string, args ...any) (map[string]string, error) {
	rows, err := tx.Query(ctx, table, columns, cond, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err = rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ImportReplaceInput takes an import whose file the library already holds
// into that asset instead of adding a second one.
type ImportReplaceInput struct {
	ID               string               `json:"asset_id"`
	ExpectedRevision creativeops.Revision `json:"expected_revision"`
	Title            string               `json:"title"`
	GroupIDs         []string             `json:"group_ids"`
}

// ReplaceByImport restores the asset if it is in the recycle bin, renames it
// after the imported file and adds it to the import's groups. Rating, tags,
// description, favourite and its other groups stay as they are.
func ReplaceByImport(ctx context.Context, sc store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return libraryRun(ctx, sc, c, "asset_import_replace", func(v ImportReplaceInput) error {
		if v.ID == "" || v.ExpectedRevision < 1 || strings.TrimSpace(v.Title) == "" || utf8.RuneCountInString(v.Title) > 200 {
			return creativeops.ErrValidation
		}
		_, err := uniqueIDs(v.GroupIDs, 100)
		return err
	}, func(ctx context.Context, tx store.TxAccountScope, v ImportReplaceInput) (LibraryResult, error) {
		a, err := lockAsset(ctx, tx, v.ID, v.ExpectedRevision)
		if err != nil {
			return LibraryResult{}, err
		}
		restored := a.DeletedAt != nil
		if restored {
			if _, err = tx.Update(ctx, "creative_assets", "deleted_at=NULL,purge_after=NULL", "id=$2", a.ID); err != nil {
				return LibraryResult{}, err
			}
		}
		before, err := relationCount(ctx, tx, a.ID)
		if err != nil {
			return LibraryResult{}, err
		}
		if _, err = organizeExisting(ctx, tx, a.ID, v.GroupIDs, nil, nil); err != nil {
			return LibraryResult{}, err
		}
		after, err := relationCount(ctx, tx, a.ID)
		if err != nil {
			return LibraryResult{}, err
		}
		renamed := v.Title != a.Title
		changed := restored || renamed || after != before
		if !changed {
			return libraryResult(ctx, tx, a.ID, a.Revision, false, false)
		}
		a.Title = v.Title
		a.Revision++
		if _, err = tx.Update(ctx, "creative_assets", "title=$2,normalized_title=$3,revision=revision+1,updated_at=clock_timestamp()", "id=$4", a.Title, textindex.Normalize(a.Title), a.ID); err != nil {
			return LibraryResult{}, err
		}
		if renamed {
			// The title is searchable, so the index is rebuilt from content the asset can still use.
			content, e := creativecontent.RequireUsable(ctx, tx, a.ContentRevisionID, "display")
			if e != nil {
				return LibraryResult{}, e
			}
			err = indexAsset(ctx, tx, a, content.Payload)
		} else {
			_, err = tx.Update(ctx, "creative_asset_search", "asset_revision=$2", "asset_id=$3", a.Revision, a.ID)
		}
		if err != nil {
			return LibraryResult{}, err
		}
		return libraryResult(ctx, tx, a.ID, a.Revision, true, false)
	})
}
