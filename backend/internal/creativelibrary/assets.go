package creativelibrary

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary/textindex"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var ErrTrashed = errors.New("asset is in recycle bin")

type AssetVersion struct {
	ID       string               `json:"asset_id"`
	Revision creativeops.Revision `json:"expected_revision"`
}
type MetadataInput struct {
	ID               string               `json:"asset_id"`
	ExpectedRevision creativeops.Revision `json:"expected_revision"`
	Title            *string              `json:"title,omitempty"`
	Description      *string              `json:"description,omitempty"`
	Favorite         *bool                `json:"is_favorite,omitempty"`
}
type OrganizeInput struct {
	Assets       []AssetVersion `json:"assets"`
	AddGroups    []string       `json:"add_group_ids"`
	RemoveGroups []string       `json:"remove_group_ids"`
	AddTags      []string       `json:"add_tag_ids"`
	RemoveTags   []string       `json:"remove_tag_ids"`
}
type BatchAssetInput struct {
	Assets []AssetVersion `json:"assets"`
}
type SettingsInput struct {
	ExpectedRevision creativeops.Revision `json:"expected_revision"`
	RetentionDays    *int                 `json:"retention_days"`
}

func uniqueIDs(ids []string, max int) ([]string, error) {
	set := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			return nil, creativeops.ErrValidation
		}
		set[id] = true
	}
	if len(set) > max {
		return nil, creativeops.ErrValidation
	}
	result := make([]string, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Strings(result)
	return result, nil
}
func indexAsset(ctx context.Context, tx store.TxAccountScope, a Asset, payload creativecontent.Payload) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	source := ""
	if payload.URL != nil {
		source = *payload.URL
	}
	text, err := textindex.Document(a.Title, a.Description, source, raw)
	if err != nil {
		return err
	}
	return tx.Upsert(ctx, "creative_asset_search", []string{"asset_id", "normalized_text", "asset_revision", "normalization_version"}, []string{"account_id", "asset_id"}, []string{"normalized_text", "asset_revision", "normalization_version"}, a.ID, text, a.Revision, textindex.Version)
}
func organizeNew(ctx context.Context, tx store.TxAccountScope, id string, groups, tags []string, drafts []NewTag) (map[string]string, error) {
	groups, err := uniqueIDs(groups, 100)
	if err != nil {
		return nil, err
	}
	tags, err = uniqueIDs(tags, 50)
	if err != nil || len(drafts) > 50 {
		return nil, creativeops.ErrValidation
	}
	for _, g := range groups {
		if err = requireEntity(ctx, tx, "creative_asset_groups", g); err != nil {
			return nil, err
		}
	}
	for _, t := range tags {
		if err = requireEntity(ctx, tx, "creative_tags", t); err != nil {
			return nil, err
		}
	}
	mapping := map[string]string{}
	structural := false
	for _, d := range drafts {
		if d.ClientKey == "" || mapping[d.ClientKey] != "" {
			return nil, creativeops.ErrValidation
		}
		tag, created, e := createTag(ctx, tx, d)
		if e != nil {
			return nil, e
		}
		mapping[d.ClientKey] = tag.ID
		tags = append(tags, tag.ID)
		structural = structural || created
	}
	tags, err = uniqueIDs(tags, 50)
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		if err = tx.Insert(ctx, "creative_asset_group_members", []string{"asset_id", "group_id"}, id, g); err != nil {
			return nil, err
		}
	}
	for _, t := range tags {
		if err = tx.Insert(ctx, "creative_asset_tags", []string{"asset_id", "tag_id"}, id, t); err != nil {
			return nil, err
		}
	}
	if structural {
		_, err = tx.Update(ctx, "creative_library_settings", "hierarchy_revision=hierarchy_revision+1", "")
	}
	return mapping, err
}
func lockAsset(ctx context.Context, tx store.TxAccountScope, id string, revision creativeops.Revision) (Asset, error) {
	a, err := scanAsset(tx.QueryRowForUpdate(ctx, "creative_assets", assetColumns, "id=$2", id))
	if err != nil {
		return Asset{}, err
	}
	if a.Revision != revision {
		return Asset{}, ErrVersionConflict
	}
	return a, nil
}
func Metadata(ctx context.Context, sc store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	var fields map[string]json.RawMessage
	if err := creativeops.Decode(c.Payload, &fields); err != nil {
		return creativeops.Receipt{}, err
	}
	for _, key := range []string{"title", "description", "is_favorite"} {
		if raw, ok := fields[key]; ok && string(raw) == "null" {
			return creativeops.Receipt{}, creativeops.ErrValidation
		}
	}

	return libraryRun(ctx, sc, c, "asset_metadata", func(v MetadataInput) error {
		if v.ID == "" || v.ExpectedRevision < 1 || (v.Title == nil && v.Description == nil && v.Favorite == nil) || (v.Title != nil && (strings.TrimSpace(*v.Title) == "" || utf8.RuneCountInString(*v.Title) > 200)) || (v.Description != nil && utf8.RuneCountInString(*v.Description) > 2000) {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v MetadataInput) (LibraryResult, error) {
		a, err := lockAsset(ctx, tx, v.ID, v.ExpectedRevision)
		if err != nil {
			return LibraryResult{}, err
		}
		if a.DeletedAt != nil {
			return LibraryResult{}, ErrTrashed
		}
		title, desc, fav := a.Title, a.Description, a.Favorite
		if v.Title != nil {
			title = *v.Title
		}
		if v.Description != nil {
			desc = *v.Description
		}
		if v.Favorite != nil {
			fav = *v.Favorite
		}
		changed := title != a.Title || desc != a.Description || fav != a.Favorite
		if changed {
			// Metadata still requires currently usable content before rebuilding its search projection.
			content, e := creativecontent.RequireUsable(ctx, tx, a.ContentRevisionID, "display")
			if e != nil {
				return LibraryResult{}, e
			}
			a.Title = title
			a.Description = desc
			a.Favorite = fav
			a.Revision++
			if _, err = tx.Update(ctx, "creative_assets", "title=$2,normalized_title=$3,description=$4,is_favorite=$5,revision=revision+1,updated_at=clock_timestamp()", "id=$6", title, textindex.Normalize(title), desc, fav, a.ID); err != nil {
				return LibraryResult{}, err
			}
			if err = indexAsset(ctx, tx, a, content.Payload); err != nil {
				return LibraryResult{}, err
			}
		}
		return libraryResult(ctx, tx, a.ID, a.Revision, changed, false)
	})
}
func validAssetVersions(v []AssetVersion) error {
	if len(v) < 1 || len(v) > 100 {
		return creativeops.ErrValidation
	}
	seen := map[string]bool{}
	for _, a := range v {
		if a.ID == "" || a.Revision < 1 || seen[a.ID] {
			return creativeops.ErrValidation
		}
		seen[a.ID] = true
	}
	return nil
}
func Organize(ctx context.Context, sc store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return libraryRun(ctx, sc, c, "batch_organize", func(v OrganizeInput) error {
		if err := validAssetVersions(v.Assets); err != nil {
			return err
		}
		for i, ids := range [][]string{v.AddGroups, v.RemoveGroups, v.AddTags, v.RemoveTags} {
			max := 100
			if i >= 2 {
				max = 50
			}
			if _, err := uniqueIDs(ids, max); err != nil {
				return err
			}
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v OrganizeInput) (LibraryResult, error) {
		for _, ref := range []struct {
			table string
			ids   []string
		}{{"creative_asset_groups", append(append([]string{}, v.AddGroups...), v.RemoveGroups...)}, {"creative_tags", append(append([]string{}, v.AddTags...), v.RemoveTags...)}} {
			for _, id := range ref.ids {
				if err := requireEntity(ctx, tx, ref.table, id); err != nil {
					return LibraryResult{}, err
				}
			}
		}
		sort.Slice(v.Assets, func(i, j int) bool { return v.Assets[i].ID < v.Assets[j].ID })
		changed := false
		var latest creativeops.Revision = 1
		for _, ref := range v.Assets {
			a, err := lockAsset(ctx, tx, ref.ID, ref.Revision)
			if err != nil {
				return LibraryResult{}, err
			}
			if a.DeletedAt != nil {
				return LibraryResult{}, ErrTrashed
			}
			assetChanged := false
			for _, rel := range []struct {
				table, column string
				add, remove   []string
			}{{"creative_asset_group_members", "group_id", v.AddGroups, v.RemoveGroups}, {"creative_asset_tags", "tag_id", v.AddTags, v.RemoveTags}} {
				adds, _ := uniqueIDs(rel.add, 100)
				removes, _ := uniqueIDs(rel.remove, 100)
				for _, id := range adds {
					for _, removed := range removes {
						if id == removed {
							return LibraryResult{}, creativeops.ErrValidation
						}
					}
					exists, err := tx.Exists(ctx, rel.table, "asset_id=$2 AND "+rel.column+"=$3", a.ID, id)
					if err != nil {
						return LibraryResult{}, err
					}
					if !exists {
						if err = tx.Insert(ctx, rel.table, []string{"asset_id", rel.column}, a.ID, id); err != nil {
							return LibraryResult{}, err
						}
						assetChanged = true
					}
				}
				for _, id := range removes {
					n, err := tx.Delete(ctx, rel.table, "asset_id=$2 AND "+rel.column+"=$3", a.ID, id)
					if err != nil {
						return LibraryResult{}, err
					}
					assetChanged = assetChanged || n > 0
				}
			}

			for _, limit := range []struct {
				table string
				max   int64
			}{{"creative_asset_group_members", 100}, {"creative_asset_tags", 50}} {
				count, e := tx.Count(ctx, limit.table, "asset_id=$2", a.ID)
				if e != nil {
					return LibraryResult{}, e
				}
				if count > limit.max {
					return LibraryResult{}, creativeops.ErrValidation
				}
			}
			if assetChanged {
				if _, err := tx.Update(ctx, "creative_assets", "revision=revision+1,updated_at=clock_timestamp()", "id=$2", a.ID); err != nil {
					return LibraryResult{}, err
				}
				a.Revision++
				if _, err := tx.Update(ctx, "creative_asset_search", "asset_revision=$2", "asset_id=$3", a.Revision, a.ID); err != nil {
					return LibraryResult{}, err
				}
				changed = true
			}
			if ref.ID == v.Assets[0].ID {
				latest = a.Revision
			}
		}
		return libraryResult(ctx, tx, v.Assets[0].ID, latest, changed, false)
	})
}
func assetState(ctx context.Context, sc store.AccountScope, c creativeops.Command, action string, batch bool) (creativeops.Receipt, error) {
	// Both routes share the same locked, atomic batch implementation.
	if !batch {
		var v AssetVersion
		if err := creativeops.Decode(c.Payload, &v); err != nil {
			return creativeops.Receipt{}, err
		}
		raw, err := json.Marshal(BatchAssetInput{Assets: []AssetVersion{v}})
		if err != nil {
			return creativeops.Receipt{}, err
		}
		c.Payload = raw
	}
	return libraryRun(ctx, sc, c, "asset_"+action, func(v BatchAssetInput) error { return validAssetVersions(v.Assets) }, func(ctx context.Context, tx store.TxAccountScope, v BatchAssetInput) (LibraryResult, error) {
		sort.Slice(v.Assets, func(i, j int) bool { return v.Assets[i].ID < v.Assets[j].ID })
		settings, err := readSettings(ctx, tx)
		if err != nil {
			return LibraryResult{}, err
		}
		var revision creativeops.Revision = 1
		for _, ref := range v.Assets {
			a, err := lockAsset(ctx, tx, ref.ID, ref.Revision)
			if err != nil {
				return LibraryResult{}, err
			}
			switch action {
			case "trash":
				if a.DeletedAt != nil {
					return LibraryResult{}, ErrTrashed
				}
				now, e := tx.CreativeNow(ctx)
				if e != nil {
					return LibraryResult{}, e
				}
				var purge *time.Time
				if settings.RetentionDays != nil {
					at := now.AddDate(0, 0, *settings.RetentionDays)
					purge = &at
				}
				_, err = tx.Update(ctx, "creative_assets", "deleted_at=$2,purge_after=$3,revision=revision+1,updated_at=clock_timestamp()", "id=$4", now, purge, a.ID)
			case "restore":
				if a.DeletedAt == nil {
					return LibraryResult{}, creativeops.ErrValidation
				}
				_, err = tx.Update(ctx, "creative_assets", "deleted_at=NULL,purge_after=NULL,revision=revision+1,updated_at=clock_timestamp()", "id=$2", a.ID)
			case "purge":
				if a.DeletedAt == nil {
					return LibraryResult{}, creativeops.ErrValidation
				}
				for _, table := range []string{"creative_asset_group_members", "creative_asset_tags", "creative_asset_search"} {
					if _, err = tx.Delete(ctx, table, "asset_id=$2", a.ID); err != nil {
						return LibraryResult{}, err
					}
				}
				_, err = tx.Delete(ctx, "creative_assets", "id=$2", a.ID)
			}
			if err != nil {
				return LibraryResult{}, err
			}
			nextRevision := a.Revision + 1
			if ref.ID == v.Assets[0].ID {
				revision = nextRevision
			}
			if action != "purge" {
				if _, err := tx.Update(ctx, "creative_asset_search", "asset_revision=$2", "asset_id=$3", nextRevision, a.ID); err != nil {
					return LibraryResult{}, err
				}
			}
		}
		return libraryResult(ctx, tx, v.Assets[0].ID, revision, true, false)
	})
}
func Trash(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return assetState(ctx, s, c, "trash", false)
}
func Restore(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return assetState(ctx, s, c, "restore", false)
}
func Purge(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return assetState(ctx, s, c, "purge", false)
}
func BatchTrash(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return assetState(ctx, s, c, "trash", true)
}
func BatchRestore(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return assetState(ctx, s, c, "restore", true)
}
func BatchPurge(ctx context.Context, s store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return assetState(ctx, s, c, "purge", true)
}
func SaveSettings(ctx context.Context, sc store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	var fields map[string]json.RawMessage
	if err := creativeops.Decode(c.Payload, &fields); err != nil {
		return creativeops.Receipt{}, err
	}
	if _, ok := fields["retention_days"]; !ok {
		return creativeops.Receipt{}, creativeops.ErrValidation
	}

	return libraryRun(ctx, sc, c, "settings", func(v SettingsInput) error {
		if v.ExpectedRevision < 1 || (v.RetentionDays != nil && *v.RetentionDays != 7 && *v.RetentionDays != 30 && *v.RetentionDays != 90) {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v SettingsInput) (LibraryResult, error) {
		s, err := readSettings(ctx, tx)
		if err != nil {
			return LibraryResult{}, err
		}
		if s.Revision != v.ExpectedRevision {
			return LibraryResult{}, ErrVersionConflict
		}
		changed := (s.RetentionDays == nil) != (v.RetentionDays == nil) || (s.RetentionDays != nil && v.RetentionDays != nil && *s.RetentionDays != *v.RetentionDays)
		if changed {
			if _, err = tx.Update(ctx, "creative_library_settings", "retention_days=$2,revision=revision+1", "", v.RetentionDays); err != nil {
				return LibraryResult{}, err
			}
			s.Revision++
		}
		return libraryResult(ctx, tx, "settings", s.Revision, changed, false)
	})
}
