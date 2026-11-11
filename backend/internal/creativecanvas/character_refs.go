package creativecanvas

import (
	"context"
	"errors"
	"fmt"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const maxGenerationCharacters = 8

type CharacterGenerationError struct {
	Characters []*GenerationCharacter
	Limit      int
	Total      int
	ImageIndex int
	RevisionID string
}

func (e *CharacterGenerationError) Error() string {
	if e.ImageIndex != 0 && len(e.Characters) > 0 {
		return fmt.Sprintf("角色卡「%s」的第 %d 张参考图不可用或超出模型限制", e.Characters[0].Name, e.ImageIndex)
	}
	return fmt.Sprintf("参考图展开后有 %d 张，模型最多支持 %d 张", e.Total, e.Limit)
}

func (e *CharacterGenerationError) Unwrap() error { return creativeops.ErrValidation }

type GenerationCharacter struct {
	NodeID            string `json:"node_id"`
	ContentID         string `json:"content_id"`
	ContentRevisionID string `json:"content_revision_id"`
	Name              string `json:"name"`
	Images            [2]int `json:"images"`
	appearance        string
	ViewCount         int `json:"-"`
}

func expandCharacterRefsInTx(ctx context.Context, tx store.TxAccountScope, refs []generationPromptRef) ([]generationPromptRef, []*GenerationCharacter, error) {
	if len(refs) == 0 {
		return refs, nil, nil
	}
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.revision)
	}
	var characterIDs []string
	err := snapshotRows(ctx, tx, "creative_content_revisions", "id", "id=ANY($2::text[]) AND payload ? 'character'", func(row store.Row) error {
		var id string
		if err := row.Scan(&id); err != nil {
			return err
		}
		characterIDs = append(characterIDs, id)
		return nil
	}, ids)
	if err != nil || len(characterIDs) == 0 {
		return refs, nil, err
	}
	cards, err := creativecontent.ReadDisplayBatchInTx(ctx, tx, characterIDs)
	if err != nil {
		return nil, nil, err
	}
	expanded := make([]generationPromptRef, 0, len(refs))
	var characters []*GenerationCharacter
	for _, ref := range refs {
		result, character := cards[ref.revision]
		if !character {
			expanded = append(expanded, ref)
			continue
		}
		if result.Err != nil {
			return nil, nil, creativeops.Explain(result.Err, "角色卡版本 %s 不可用", ref.revision)
		}
		card := result.Revision.Payload.Character
		if result.Revision.Kind != creativecontent.CharacterKind || card == nil || len(card.Views) == 0 {
			return nil, nil, ErrGraphIntegrity
		}
		characterRef := &GenerationCharacter{
			NodeID: ref.source, ContentID: result.Revision.ContentID, ContentRevisionID: ref.revision,
			Name: card.Name, appearance: card.Appearance, ViewCount: len(card.Views),
		}
		characters = append(characters, characterRef)
		for i, view := range card.Views {
			image := ref
			image.revision = view.ContentRevisionID
			image.character = characterRef
			image.viewIndex = i + 1
			expanded = append(expanded, image)
		}
	}
	if len(characters) > maxGenerationCharacters {
		return nil, nil, creativeops.Explain(creativeops.ErrValidation, "一次生成最多引用 %d 张角色卡，当前有 %d 张", maxGenerationCharacters, len(characters))
	}
	return expanded, characters, nil
}

func (c GenerationCharacter) imageMention() string {
	label := fmt.Sprintf("[图%d]", c.Images[0])
	if c.Images[1] > c.Images[0] {
		label += fmt.Sprintf("–[图%d]", c.Images[1])
	}
	return "「" + c.Name + "」（" + label + "）"
}

func (r generationPromptRef) explainReferenceError(err error) error {
	if r.character == nil {
		return err
	}
	if !errors.Is(err, creativecontent.ErrNotFound) && !errors.Is(err, creativecontent.ErrUsageDenied) &&
		!errors.Is(err, creativecontent.ErrMissingRoot) && !errors.Is(err, creativeops.ErrValidation) {
		return err
	}
	return &CharacterGenerationError{Characters: []*GenerationCharacter{r.character}, ImageIndex: r.viewIndex, RevisionID: r.revision}
}
