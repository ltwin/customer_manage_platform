package creativedocument

import (
	"context"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// ReadConversation returns the assistant conversation a document keeps on a
// canvas. A pointer to a conversation that no longer exists reads as none.
func ReadConversation(ctx context.Context, scope store.AccountScope, documentID, canvasID string) (*string, error) {
	if canvasID == "" {
		return nil, creativeops.Explain(creativeops.ErrValidation, "缺少 canvas_id")
	}
	var out *string
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		if _, err := readHead(ctx, tx, documentID); err != nil {
			return err
		}
		exists, err := tx.Exists(ctx, "creative_canvases", "id=$2", canvasID)
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		var id string
		err = tx.QueryRow(ctx, "creative_document_conversations", "conversation_id", "document_id=$2 AND canvas_id=$3", documentID, canvasID).Scan(&id)
		if errors.Is(err, store.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		live, err := tx.Exists(ctx, "creative_agent_conversations", "id=$2", id)
		if err != nil || !live {
			return err
		}
		out = &id
		return nil
	})
	return out, err
}

// SetConversation records the conversation a document is being worked on in,
// inside the caller's transaction (sending a message, a type's agent tool).
func SetConversation(ctx context.Context, tx store.TxAccountScope, documentID, canvasID, conversationID string) error {
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return err
	}
	return tx.Upsert(ctx, "creative_document_conversations", []string{"document_id", "canvas_id", "conversation_id", "updated_at"},
		[]string{"account_id", "document_id", "canvas_id"}, []string{"conversation_id", "updated_at"},
		documentID, canvasID, conversationID, now)
}
