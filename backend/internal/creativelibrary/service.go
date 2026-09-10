// Package creativelibrary owns personal asset entries independently of projects.
package creativelibrary

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary/textindex"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	ErrNotFound        = errors.New("creative asset not found")
	ErrVersionConflict = errors.New("creative asset version conflict")
)

type CreateAssetInput struct {
	GroupIDs    []string              `json:"group_ids,omitempty"`
	TagIDs      []string              `json:"tag_ids,omitempty"`
	NewTags     []NewTag              `json:"new_tags,omitempty"`
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Content     creativecontent.Draft `json:"content"`
}
type AssetResult struct {
	TagMapping        map[string]string    `json:"tag_mapping"`
	ID                string               `json:"asset_id"`
	Revision          creativeops.Revision `json:"revision"`
	ContentRevisionID string               `json:"content_revision_id"`
	LibraryRevision   creativeops.Revision `json:"library_revision"`
}
type AssetReference struct {
	ID                string               `json:"asset_id"`
	Revision          creativeops.Revision `json:"expected_asset_revision"`
	ContentRevisionID string               `json:"content_revision_id"`
}
type Asset struct {
	Favorite          bool                      `json:"is_favorite"`
	CreatedAt         time.Time                 `json:"created_at"`
	DeletedAt         *time.Time                `json:"deleted_at"`
	PurgeAfter        *time.Time                `json:"purge_after"`
	GroupIDs          []string                  `json:"group_ids"`
	TagIDs            []string                  `json:"tag_ids"`
	ID                string                    `json:"id"`
	Title             string                    `json:"title"`
	Description       string                    `json:"description"`
	Kind              string                    `json:"kind"`
	Revision          creativeops.Revision      `json:"revision"`
	ContentID         string                    `json:"content_id"`
	ContentRevisionID string                    `json:"content_revision_id"`
	Content           *creativecontent.Revision `json:"content,omitempty"`
	Unavailable       bool                      `json:"unavailable"`
}

func CreateAsset(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{
		Key: "library.create_asset", Capability: "manual_write",
		Validate: func(raw json.RawMessage) error {
			var in CreateAssetInput
			if err := creativeops.Decode(raw, &in); err != nil {
				return err
			}
			if strings.TrimSpace(in.Title) == "" || utf8.RuneCountInString(in.Title) > 200 || utf8.RuneCountInString(in.Description) > 2000 {
				return creativeops.ErrValidation
			}
			return creativecontent.Validate(in.Content)
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, raw json.RawMessage) (creativeops.Outcome, error) {
			var in CreateAssetInput
			if err := creativeops.Decode(raw, &in); err != nil {
				return creativeops.Outcome{}, err
			}
			revision, err := lockLibrary(ctx, tx)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			id := "ccas_" + uuid.NewString()
			content, err := creativecontent.WriteAndRetain(ctx, tx, in.Content, "", nil, func(r creativecontent.Revision) error {
				return tx.Insert(ctx, "creative_assets", []string{"id", "kind", "title", "normalized_title", "description", "content_id", "content_revision_id", "source_url"}, id, r.Kind, in.Title, textindex.Normalize(in.Title), in.Description, r.ContentID, r.ID, in.Content.Payload.URL)
			})
			if err != nil {
				return creativeops.Outcome{}, err
			}
			mapping, err := organizeNew(ctx, tx, id, in.GroupIDs, in.TagIDs, in.NewTags)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if err = indexAsset(ctx, tx, Asset{ID: id, Title: in.Title, Description: in.Description, Revision: 1}, in.Content.Payload); err != nil {
				return creativeops.Outcome{}, err
			}
			if _, err := tx.Update(ctx, "creative_library_settings", "library_revision=library_revision+1", ""); err != nil {
				return creativeops.Outcome{}, err
			}
			body, err := json.Marshal(AssetResult{TagMapping: mapping, ID: id, Revision: 1, ContentRevisionID: content.ID, LibraryRevision: revision + 1})
			if err != nil {
				return creativeops.Outcome{}, err
			}
			v := creativeops.Revision(1)
			return creativeops.Outcome{HTTPStatus: 201, Response: body, ResultKind: "asset", ResultID: &id, ResultRevision: &v}, nil
		}}, command)
}

func lockLibrary(ctx context.Context, tx store.TxAccountScope) (creativeops.Revision, error) {
	// The account writer barrier precedes this port; only the first writer creates the root.
	exists, err := tx.Exists(ctx, "creative_library_settings", "")
	if err != nil {
		return 0, err
	}
	if !exists {
		if err := tx.Insert(ctx, "creative_library_settings", []string{"revision"}, int64(1)); err != nil {
			return 0, err
		}
	}
	var revision int64
	if err := tx.QueryRowForUpdate(ctx, "creative_library_settings", "library_revision", "").Scan(&revision); err != nil {
		return 0, err
	}
	return creativeops.Revision(revision), nil
}

// ResolveAssetsInTx locks the library and selected asset before canvas locks.
// It fixes identity/revision only; the canvas checks content usage after taking
// its project/canvas locks, preserving the library -> canvas -> content order.
func ResolveAssetsInTx(ctx context.Context, tx store.TxAccountScope, ref AssetReference) (Asset, error) {
	if ref.ID == "" || ref.Revision < 1 || ref.ContentRevisionID == "" {
		return Asset{}, creativeops.ErrValidation
	}
	if _, err := lockLibrary(ctx, tx); err != nil {
		return Asset{}, err
	}
	a, err := scanAsset(tx.QueryRowForUpdate(ctx, "creative_assets", assetColumns, "id=$2 AND deleted_at IS NULL", ref.ID))
	if err != nil {
		return Asset{}, err
	}
	if a.Revision != ref.Revision || a.ContentRevisionID != ref.ContentRevisionID {
		return Asset{}, ErrVersionConflict
	}
	return a, nil
}

const assetColumns = "id,title,description,kind,revision,content_id,content_revision_id,is_favorite,created_at,deleted_at,purge_after"

func scanAsset(row store.Row) (Asset, error) {
	a := Asset{GroupIDs: []string{}, TagIDs: []string{}}
	var revision int64
	err := row.Scan(&a.ID, &a.Title, &a.Description, &a.Kind, &revision, &a.ContentID, &a.ContentRevisionID, &a.Favorite, &a.CreatedAt, &a.DeletedAt, &a.PurgeAfter)
	if errors.Is(err, store.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	a.Revision = creativeops.Revision(revision)
	return a, err
}
func GetAsset(ctx context.Context, scope store.AccountScope, id string) (Asset, error) {
	var a Asset
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		var err error
		a, err = scanAsset(tx.QueryRow(ctx, "creative_assets", assetColumns, "id=$2", id))
		if err != nil {
			return err
		}
		if err = attachRelations(ctx, tx, &a); err != nil {
			return err
		}
		r, err := creativecontent.ReadInSnapshot(ctx, tx, a.ContentRevisionID, "display")
		if errors.Is(err, creativecontent.ErrUsageDenied) {
			a.Unavailable = true
			return nil
		}
		if err != nil {
			return err
		}
		if r.ContentID != a.ContentID || r.Kind != a.Kind {
			return creativecontent.ErrMissingRoot
		}
		a.Content = &r
		return nil
	})
	if err != nil {
		return Asset{}, err
	}
	return a, nil
}
