package store

import (
	"context"
	"errors"
)

var ErrCreativeTextReferences = errors.New("creative text references are inconsistent")

// CheckCreativeTextReferences verifies live asset/node roots without treating
// historical, unreferenced revisions as corrupt or granting them read access.
func (sc AccountScope) CheckCreativeTextReferences(ctx context.Context) error {
	return sc.WithTxScope(ctx, func(tx TxAccountScope) error {
		var exists bool
		if err := tx.scope.execRunner().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1)", tx.AccountID()).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrCreativeAccessDenied
		}

		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		for _, owner := range []struct{ table, kind string }{{"creative_assets", "kind"}, {"creative_nodes", "type_key"}} {
			kindMatch := "c.kind=" + owner.table + ".kind"
			if owner.kind == "type_key" {
				kindMatch = "'core.'||c.kind=" + owner.table + ".type_key"
			}
			cond := "content_revision_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM creative_content_revisions r JOIN creative_contents c ON c.account_id=r.account_id AND c.id=r.content_id WHERE r.account_id=" + owner.table + ".account_id AND r.id=" + owner.table + ".content_revision_id AND r.content_id=" + owner.table + ".content_id AND " + kindMatch + ")"
			count, err := tx.Count(ctx, owner.table, cond)
			if err != nil {
				return err
			}
			if count != 0 {
				return ErrCreativeTextReferences
			}
		}
		return nil
	})
}
