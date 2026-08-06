package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/samson/customer-manage-platform/backend/internal/platform/planningcapability"
)

const archiveCapabilitySelect = `SELECT singleton_key, capability, revision, promoted_at
FROM planning_archive_capability_state
WHERE singleton_key = 'planning-archive-v1'`

func (sc TxAccountScope) ArchiveCapability() planningcapability.ArchiveCapabilityTxView {
	return planningcapability.BindTrustedTxReader(func(ctx context.Context) (planningcapability.ArchiveCapabilityState, error) {
		return scanArchiveCapability(sc.scope.execRunner().QueryRow(ctx, archiveCapabilitySelect+` FOR SHARE`))
	})
}

func (s *Store) ArchiveCapabilityStartupReader() planningcapability.ArchiveCapabilityStartupReader {
	return planningcapability.BindTrustedStartupReader(func(ctx context.Context) (planningcapability.ArchiveCapabilityState, error) {
		if s == nil || s.pool == nil {
			return planningcapability.ArchiveCapabilityState{}, errors.New("store is not open")
		}
		return scanArchiveCapability(s.pool.QueryRow(ctx, archiveCapabilitySelect))
	})
}

func (s *Store) ArchiveCapabilityPromoter() planningcapability.ArchiveCapabilityPromoter {
	show := func(ctx context.Context) (planningcapability.ArchiveCapabilityState, error) {
		if s == nil || s.pool == nil {
			return planningcapability.ArchiveCapabilityState{}, errors.New("store is not open")
		}
		return scanArchiveCapability(s.pool.QueryRow(ctx, archiveCapabilitySelect))
	}
	cas := func(ctx context.Context, expectedRevision int64, target planningcapability.ArchiveCapability) (planningcapability.ArchiveCapabilityState, error) {
		row := s.pool.QueryRow(ctx, `UPDATE planning_archive_capability_state
SET capability = $1, revision = revision + 1, promoted_at = clock_timestamp()
WHERE singleton_key = 'planning-archive-v1' AND revision = $2
RETURNING singleton_key, capability, revision, promoted_at`, string(target), expectedRevision)
		state, err := scanArchiveCapability(row)
		if errors.Is(err, ErrNoRows) {
			return planningcapability.ArchiveCapabilityState{}, planningcapability.ErrPromotionConflict
		}
		return state, err
	}
	return planningcapability.BindTrustedPromoter(show, cas)
}

type archiveCapabilityRow interface{ Scan(...any) error }

func scanArchiveCapability(row archiveCapabilityRow) (planningcapability.ArchiveCapabilityState, error) {
	var state planningcapability.ArchiveCapabilityState
	if err := row.Scan(&state.SingletonKey, &state.Capability, &state.Revision, &state.PromotedAt); err != nil {
		return planningcapability.ArchiveCapabilityState{}, fmt.Errorf("read planning archive capability: %w", err)
	}
	return state, nil
}
