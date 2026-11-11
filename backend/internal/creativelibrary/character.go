package creativelibrary

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary/textindex"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type CharacterTarget struct {
	GroupIDs    []string `json:"group_ids,omitempty"`
	TagIDs      []string `json:"tag_ids,omitempty"`
	NewTags     []NewTag `json:"new_tags,omitempty"`
	Description string   `json:"description,omitempty"`
	Favorite    bool     `json:"is_favorite,omitempty"`
}

type CreateCharacterInput struct {
	Character creativecontent.CharacterPayload `json:"character"`
	Target    CharacterTarget                  `json:"target,omitempty"`
}

type SaveCharacterRevisionInput struct {
	ID               string                           `json:"asset_id"`
	ExpectedRevision creativeops.Revision             `json:"expected_revision"`
	Character        creativecontent.CharacterPayload `json:"character"`
}

type CharacterBrief struct {
	Name      string                       `json:"name"`
	Variant   string                       `json:"variant,omitempty"`
	IP        *creativecontent.CharacterIP `json:"ip,omitempty"`
	Work      string                       `json:"work,omitempty"`
	ViewCount int                          `json:"view_count"`
}

func characterBrief(card *creativecontent.CharacterPayload) *CharacterBrief {
	if card == nil {
		return nil
	}
	return &CharacterBrief{Name: card.Name, Variant: card.Variant, IP: card.IP, Work: card.Work, ViewCount: len(card.Views)}
}

func characterTitle(card creativecontent.CharacterPayload) string {
	if card.Variant == "" {
		return card.Name
	}
	return card.Name + " · " + card.Variant
}

func readCharacterCreate(raw json.RawMessage) (creativecontent.CharacterPayload, ImportTarget, error) {
	var in CreateCharacterInput
	if err := creativeops.Decode(raw, &in); err != nil {
		return creativecontent.CharacterPayload{}, ImportTarget{}, err
	}
	card, err := normalizeLibraryCharacter(in.Character)
	if err != nil {
		return creativecontent.CharacterPayload{}, ImportTarget{}, err
	}
	target := ImportTarget{
		Title: characterTitle(card), Description: in.Target.Description, Favorite: in.Target.Favorite,
		GroupIDs: in.Target.GroupIDs, TagIDs: in.Target.TagIDs, NewTags: in.Target.NewTags,
	}
	return card, target, ValidateImportTarget(target)
}

func CreateCharacter(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{
		Key: "library.create_character", Capability: "manual_write",
		Validate: func(raw json.RawMessage) error {
			_, _, err := readCharacterCreate(raw)
			return err
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, raw json.RawMessage) (creativeops.Outcome, error) {
			card, target, err := readCharacterCreate(raw)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			libraryRevision, err := lockLibrary(ctx, tx)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			var result AssetResult
			_, err = creativecontent.WriteCharacterAndRetain(ctx, tx, card, "", func(r creativecontent.Revision) error {
				result, err = createAssetFromRevision(ctx, tx, r, target, libraryRevision)
				return err
			})
			if err != nil {
				return creativeops.Outcome{}, err
			}
			return characterOutcome(201, result)
		},
	}, command)
}

func readCharacterSave(raw json.RawMessage) (SaveCharacterRevisionInput, error) {
	var in SaveCharacterRevisionInput
	if err := creativeops.Decode(raw, &in); err != nil {
		return in, err
	}
	if in.ID == "" || in.ExpectedRevision < 1 {
		return in, creativeops.ErrValidation
	}
	var err error
	in.Character, err = normalizeLibraryCharacter(in.Character)
	return in, err
}

func SaveCharacterRevision(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{
		Key: "library.save_character_revision", Capability: "manual_write",
		Validate: func(raw json.RawMessage) error {
			_, err := readCharacterSave(raw)
			return err
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, raw json.RawMessage) (creativeops.Outcome, error) {
			in, err := readCharacterSave(raw)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			result, err := saveCharacterRevisionInTx(ctx, tx, in)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			return characterOutcome(200, result)
		},
	}, command)
}

func saveCharacterRevisionInTx(ctx context.Context, tx store.TxAccountScope, in SaveCharacterRevisionInput) (AssetResult, error) {
	libraryRevision, err := lockLibrary(ctx, tx)
	if err != nil {
		return AssetResult{}, err
	}
	a, err := scanAsset(tx.QueryRowForUpdate(ctx, "creative_assets", assetColumns, "id=$2", in.ID))
	if err != nil {
		return AssetResult{}, err
	}
	switch {
	case a.Kind != creativecontent.CharacterKind:
		return AssetResult{}, creativeops.ErrValidation
	case a.DeletedAt != nil:
		return AssetResult{}, ErrTrashed
	case a.Revision != in.ExpectedRevision:
		return AssetResult{}, ErrVersionConflict
	}
	current, err := creativecontent.RequireUsable(ctx, tx, a.ContentRevisionID, "display")
	if err != nil {
		return AssetResult{}, err
	}
	if current.Kind != a.Kind || current.ContentID != a.ContentID || current.Payload.Character == nil {
		return AssetResult{}, creativecontent.ErrMissingRoot
	}
	card := in.Character
	result := AssetResult{
		ID: a.ID, Title: a.Title, Description: a.Description, TagMapping: map[string]string{},
		Revision: a.Revision, ContentRevisionID: a.ContentRevisionID, LibraryRevision: libraryRevision,
	}
	if sameCharacter(*current.Payload.Character, card) {
		result.Reused = true
		return result, nil
	}
	a.Title = characterTitle(card)
	a.Revision++
	newRevision, err := creativecontent.WriteCharacterAndRetain(ctx, tx, card, a.ContentID, func(r creativecontent.Revision) error {
		_, err := tx.Update(ctx, "creative_assets", "content_revision_id=$2,title=$3,normalized_title=$4,revision=revision+1,updated_at=clock_timestamp()",
			"id=$5", r.ID, a.Title, textindex.Normalize(a.Title), a.ID)
		if err != nil {
			return err
		}
		return indexAsset(ctx, tx, a, r.Payload)
	})
	if err != nil {
		return AssetResult{}, err
	}
	if _, err := tx.Update(ctx, "creative_library_settings", "library_revision=library_revision+1", ""); err != nil {
		return AssetResult{}, err
	}
	result.Title = a.Title
	result.ContentRevisionID = newRevision.ID
	result.Revision = a.Revision
	result.LibraryRevision++
	return result, nil
}

func sameCharacter(a, b creativecontent.CharacterPayload) bool {
	return a.Name == b.Name && a.Appearance == b.Appearance && a.Variant == b.Variant &&
		a.Origin == b.Origin && a.Work == b.Work && a.Attribution == b.Attribution &&
		sameCharacterIP(a.IP, b.IP) && slices.Equal(a.Views, b.Views) && slices.Equal(a.Sources, b.Sources) &&
		slices.Equal(a.Aliases, b.Aliases) && slices.Equal(a.SourceURLs, b.SourceURLs)
}

func sameCharacterIP(a, b *creativecontent.CharacterIP) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func characterOutcome(status int, result AssetResult) (creativeops.Outcome, error) {
	// The shared result historically omits false; both character APIs always
	// return the reuse flag.
	body, err := json.Marshal(struct {
		AssetResult
		Reused bool `json:"reused"`
	}{AssetResult: result, Reused: result.Reused})
	if err != nil {
		return creativeops.Outcome{}, err
	}
	return creativeops.Outcome{
		HTTPStatus: status, Response: body, ResultKind: "asset", ResultID: &result.ID, ResultRevision: &result.Revision,
	}, nil
}

// indexCharacter adds the card's filter columns; its text, catalog aliases
// excepted, comes from textindex.Document like every other asset.
func indexCharacter(ctx context.Context, tx store.TxAccountScope, asset Asset, text string, card *creativecontent.CharacterPayload) error {
	var ipKey, ipName, work *string
	if card.IP != nil {
		ipName = normalizedCharacterProjection(card.IP.Name)
		work = normalizedCharacterProjection(card.Work)
		if card.IP.Key != "" {
			ipKey = &card.IP.Key
		}
	}
	columns := []string{"asset_id", "normalized_text", "asset_revision", "normalization_version", "character_ip_key", "character_ip_name", "character_work"}
	return tx.Upsert(ctx, "creative_asset_search", columns, []string{"account_id", "asset_id"}, columns[1:],
		asset.ID, text, asset.Revision, textindex.Version, ipKey, ipName, work)
}

func normalizedCharacterProjection(value string) *string {
	key := creativecontent.CharacterIdentityKey(value)
	if key == "" {
		return nil
	}
	return &key
}
