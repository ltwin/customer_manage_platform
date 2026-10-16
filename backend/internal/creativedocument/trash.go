package creativedocument

import (
	"context"
	"encoding/json"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type targetInput struct {
	DocumentID string `json:"document_id"`
}

type trashResult struct {
	ID        string     `json:"id"`
	TrashedAt *time.Time `json:"trashed_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type purgeResult struct {
	ID     string `json:"id"`
	Purged bool   `json:"purged"`
}

func runTarget(ctx context.Context, scope store.AccountScope, key string, c creativeops.Command, apply func(context.Context, store.TxAccountScope, string) (any, error)) (creativeops.Receipt, error) {
	var in targetInput
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: key, Capability: "manual_write", Validate: func(raw json.RawMessage) error {
		if err := creativeops.Decode(raw, &in); err != nil {
			return err
		}
		if in.DocumentID == "" {
			return creativeops.ErrValidation
		}
		return nil
	}, Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
		value, err := apply(ctx, tx, in.DocumentID)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		body, err := json.Marshal(value)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		id := in.DocumentID
		return creativeops.Outcome{HTTPStatus: 200, Response: body, ResultKind: "document", ResultID: &id}, nil
	}}, c)
}

// Trash moves a document into the trash; it stays readable but not writable.
// Trashing a trashed document keeps its original time.
func Trash(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return runTarget(ctx, scope, "document.trash", c, func(ctx context.Context, tx store.TxAccountScope, id string) (any, error) {
		return setTrashed(ctx, tx, id, true)
	})
}

// Untrash brings a document back; an untrashed document is left as is.
func Untrash(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return runTarget(ctx, scope, "document.untrash", c, func(ctx context.Context, tx store.TxAccountScope, id string) (any, error) {
		return setTrashed(ctx, tx, id, false)
	})
}

func setTrashed(ctx context.Context, tx store.TxAccountScope, id string, trashed bool) (trashResult, error) {
	head, err := lockHead(ctx, tx, id)
	if err != nil {
		return trashResult{}, err
	}
	if (head.TrashedAt != nil) != trashed {
		set := "trashed_at=NULL,updated_at=clock_timestamp()"
		if trashed {
			set = "trashed_at=clock_timestamp(),updated_at=clock_timestamp()"
		}
		if _, err := tx.Update(ctx, "creative_documents", set, "id=$2", id); err != nil {
			return trashResult{}, err
		}
		if head, err = readHead(ctx, tx, id); err != nil {
			return trashResult{}, err
		}
	}
	return trashResult{ID: head.ID, TrashedAt: head.TrashedAt, UpdatedAt: head.UpdatedAt}, nil
}

// Purge deletes a trashed document and its whole history. Nodes keep their
// document_id and read as unavailable; snapshot content stays behind without
// any entry point, and referenced media go to media GC once nothing holds them.
func Purge(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return runTarget(ctx, scope, "document.purge", c, func(ctx context.Context, tx store.TxAccountScope, id string) (any, error) {
		head, err := lockHead(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if head.TrashedAt == nil {
			return nil, ErrNotTrashed
		}
		for _, table := range []string{"creative_document_conversations", "creative_document_content_refs", "creative_document_revisions"} {
			if _, err := tx.Delete(ctx, table, "document_id=$2", id); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Delete(ctx, "creative_documents", "id=$2", id); err != nil {
			return nil, err
		}
		return purgeResult{ID: id, Purged: true}, nil
	})
}
