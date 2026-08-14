package planshare

import (
	"context"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

// StoreTokenLookup adapts store.Store to TokenCommitmentLookup.
type StoreTokenLookup struct {
	Store *store.Store
}

func (l StoreTokenLookup) LookupCommitmentBySelector(
	ctx context.Context,
	selector string,
) (TokenCommitmentRecord, bool, error) {
	if l.Store == nil {
		return TokenCommitmentRecord{}, false, errors.New("store is not open")
	}
	row, found, err := l.Store.LookupShareCommitmentBySelector(ctx, selector)
	if err != nil || !found {
		return TokenCommitmentRecord{}, found, err
	}
	if len(row.Commitment) != shareSecretByteLen {
		return TokenCommitmentRecord{}, false, errors.New("invalid share commitment length")
	}
	var commitment ShareSecretCommitment
	copy(commitment[:], row.Commitment)
	return TokenCommitmentRecord{
		Selector:    row.Selector,
		Commitment:  commitment,
		Fingerprint: row.Fingerprint,
	}, true, nil
}

// StoreShareTransactionRunner adapts store.RunShareTransaction to
// txcap.TransactionRunner[ShareTxScope].
type StoreShareTransactionRunner struct {
	Store *store.Store
	Media *planningmedia.Application
}

func (r StoreShareTransactionRunner) Run(
	ctx context.Context,
	capability txcap.ShareTransactionCapability,
	fn func(txcap.LedgerTxView, ShareTxScope) error,
) error {
	if r.Store == nil {
		return errors.New("store is not open")
	}
	if capability == nil || capability.Context() == nil {
		return ErrShareNotFound
	}
	fingerprint := capability.Context().SelectorFingerprint()
	if fingerprint == "" {
		return ErrShareNotFound
	}
	err := r.Store.RunShareTransaction(ctx, fingerprint, func(tx store.TxAccountScope) error {
		return fn(tx.IdempotencyLedgerView(), BindShareTxScope(tx, fingerprint, r.Media))
	})
	if errors.Is(err, store.ErrNoRows) {
		return ErrShareNotFound
	}
	return err
}
