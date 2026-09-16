package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

// SkillControlView binds one skill version's control facts to this physical
// transaction, without the account filter every other business query carries.
//
// That exception is the whole point. A platform skill is published by one
// account and started by another, so "may this version still start work" is a
// question a reader cannot ask inside its own scope — and the disable that must
// beat a dispatch is committed by the publisher, in a transaction this one can
// only be serialised against through a key that belongs to neither account.
//
// The view is sealed and deliberately narrow: control status only. The frozen
// content a run follows already travels with the run, and everything else about
// a version stays behind creativeskill's own ports, which is where the rule
// about who may see what is enforced.
func (sc TxAccountScope) SkillControlView() txcap.SkillControlView {
	return txcap.BindSkillControlView(txcap.SkillControlOps{
		Lock: func(ctx context.Context, versionID string) error {
			return sc.lockSkillVersion(ctx, versionID)
		},
		Read: func(ctx context.Context, versionID string) (txcap.SkillControl, bool, error) {
			return sc.readSkillControl(ctx, versionID)
		},
	})
}

// lockSkillVersion is the version control lock the skill design names: the
// publisher's disable and a reader's dispatch intent take the same one, so a
// disable that commits first is seen by every dispatch after it, and a dispatch
// already holding it commits before any disable can.
//
// The key carries no account, because the two parties are in different accounts
// by construction. It is an advisory transaction lock rather than a row lock
// for the same reason: a row lock would have to be taken through an
// account-scoped query that cannot reach the publisher's row.
func (sc TxAccountScope) lockSkillVersion(ctx context.Context, versionID string) error {
	if versionID == "" {
		return errors.New("skill version control lock needs a version")
	}
	_, err := sc.scope.execRunner().Exec(ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended('creative-skill-version:' || $1::text, 0))", versionID)
	if err != nil {
		return fmt.Errorf("lock creative skill version: %w", err)
	}
	return nil
}

func (sc TxAccountScope) readSkillControl(ctx context.Context, versionID string) (txcap.SkillControl, bool, error) {
	var control txcap.SkillControl
	err := sc.scope.execRunner().QueryRow(ctx,
		`SELECT v.skill_id, v.account_id, s.origin, v.execution_status, s.availability
		 FROM creative_skill_versions v
		 JOIN creative_skills s ON s.id = v.skill_id AND s.account_id = v.account_id
		 WHERE v.id = $1`, versionID).
		Scan(&control.SkillID, &control.OwnerAccountID, &control.Origin,
			&control.ExecutionStatus, &control.SkillAvailability)
	if errors.Is(err, pgx.ErrNoRows) {
		return txcap.SkillControl{}, false, nil
	}
	if err != nil {
		return txcap.SkillControl{}, false, err
	}
	return control, true, nil
}
