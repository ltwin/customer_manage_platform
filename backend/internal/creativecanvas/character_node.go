package creativecanvas

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type CharacterNodeView struct {
	Name             string `json:"name"`
	CoverRevisionID  string `json:"cover_revision_id"`
	ViewCount        int    `json:"view_count"`
	AssetID          string `json:"asset_id,omitempty"`
	LatestRevisionID string `json:"latest_revision_id,omitempty"`
}

func fillCharacterNodeViews(ctx context.Context, tx canvasReader, nodes []Node) error {
	byContent := map[string][]*Node{}
	var contentIDs []string
	for i := range nodes {
		n := &nodes[i]
		if n.TypeKey != characterNodeType || n.Content == nil {
			continue
		}
		card := n.Content.Payload.Character
		if card == nil || len(card.Views) == 0 {
			return ErrGraphIntegrity
		}
		n.Character = &CharacterNodeView{
			Name: card.Name, CoverRevisionID: card.Views[0].ContentRevisionID, ViewCount: len(card.Views),
		}
		contentID := n.Content.ContentID
		if _, seen := byContent[contentID]; !seen {
			contentIDs = append(contentIDs, contentID)
		}
		byContent[contentID] = append(byContent[contentID], n)
	}
	if len(contentIDs) == 0 {
		return nil
	}
	return snapshotRows(ctx, tx, "creative_assets", "id,content_id,content_revision_id",
		"kind='character' AND deleted_at IS NULL AND content_id=ANY($2::text[])", func(row store.Row) error {
			var id, contentID, revisionID string
			if err := row.Scan(&id, &contentID, &revisionID); err != nil {
				return err
			}
			for _, n := range byContent[contentID] {
				n.Character.AssetID = id
				if revisionID != n.Content.ID {
					n.Character.LatestRevisionID = revisionID
				}
			}
			return nil
		}, contentIDs)
}

func (e *editGraph) updateCharacter(a Action) error {
	n, err := e.node(a.NodeID, "data")
	if err != nil {
		return err
	}
	if n.TypeKey != characterNodeType || n.ContentID == nil || n.ContentRevisionID == nil || a.ContentRevisionID == "" {
		return creativeops.ErrValidation
	}
	var current string
	err = e.tx.QueryRow(e.ctx, "creative_assets", "content_revision_id",
		"kind='character' AND deleted_at IS NULL AND content_id=$2", *n.ContentID).Scan(&current)
	if err != nil {
		return notFound(err)
	}
	if current != a.ContentRevisionID {
		return ErrVersionConflict
	}
	if current == *n.ContentRevisionID {
		return nil
	}
	r, err := creativecontent.RequireUsable(e.ctx, e.tx, current, "display")
	if err != nil {
		return err
	}
	if r.Kind != creativecontent.CharacterKind || r.ContentID != *n.ContentID {
		return ErrGraphIntegrity
	}
	n.ContentRevisionID = &r.ID
	if err := e.manualVersion(&n, r, "asset"); err != nil {
		return err
	}
	e.g.Nodes[n.ID] = n
	return nil
}

func acceptsCharacterReference(character bool, target string) bool {
	if target == characterNodeType {
		return false
	}
	return !character || target == "core.image"
}

func characterReferenceError() error {
	return creativeops.Explain(creativeops.ErrValidation, "角色卡只能作为生成节点的参考")
}
