package creativedocument

import (
	"context"
	"encoding/json"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// CommandInTx applies a batch inside a transaction the caller already holds,
// for a write that must commit together with other effects (a scene client
// job's result and the run it resumes). The caller's operation owns the
// receipt, the capability check and any run guard; this is commandInTx with
// the same payload and actor rules as CommandAs.
func CommandInTx(ctx context.Context, tx store.TxAccountScope, actor Actor, payload json.RawMessage) (CommandResult, error) {
	if actor.Kind == "agent" && actor.RunID == nil || actor.Kind != "agent" && actor.Kind != "photographer" {
		return CommandResult{}, creativeops.ErrValidation
	}
	in, err := decodeCommand(payload)
	if err != nil {
		return CommandResult{}, err
	}
	return commandInTx(ctx, tx, actor, in)
}

// ReadHeadInTx reads a document row in the caller's transaction without locking
// it. A document of another type is reported as not found.
func ReadHeadInTx(ctx context.Context, tx store.TxAccountScope, id, typeKey string) (Document, error) {
	head, err := readHead(ctx, tx, id)
	if err != nil {
		return Document{}, err
	}
	if head.TypeKey != typeKey {
		return Document{}, ErrNotFound
	}
	return head, nil
}

// ReadCurrentInTx reads a document's current head and snapshot in the caller's
// transaction without locking the head, for checks that a later command in the
// same transaction will repeat under the lock.
func ReadCurrentInTx(ctx context.Context, tx store.TxAccountScope, id, typeKey string) (Current, error) {
	head, err := ReadHeadInTx(ctx, tx, id, typeKey)
	if err != nil {
		return Current{}, err
	}
	return loadInTx(ctx, tx, head, 0)
}
