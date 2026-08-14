package reminder

import (
	"context"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// ApplyDigestIntentRetentionInScope redacts terminal intent payloads after 7d and
// deletes redacted metadata after 90d. Pending/recovery payloads are never cleared.
func ApplyDigestIntentRetentionInScope(ctx context.Context, scope store.AccountScope) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, err := tx.PlanningReminderFence().LockCurrentAccount(ctx); err != nil {
			return err
		}
		if _, err := redactTerminalDigestIntentsInScope(ctx, tx, digestIntentRedactAfter); err != nil {
			return err
		}
		if _, err := deleteRedactedDigestIntentMetadataInScope(ctx, tx, digestIntentDeleteAfter); err != nil {
			return err
		}
		return nil
	})
}

func redactTerminalDigestIntentsInScope(ctx context.Context, tx store.TxAccountScope, after time.Duration) (int64, error) {
	rows, err := tx.Query(ctx, "plan_assignment_reminder_digest_intents",
		"delivery_id, intent_revision",
		`payload_text IS NOT NULL
		 AND payload_redacted_at IS NULL
		 AND created_at <= clock_timestamp() - ($2::text)::interval
		 AND EXISTS (
		   SELECT 1 FROM telegram_deliveries d
		   WHERE d.account_id = plan_assignment_reminder_digest_intents.account_id
		     AND d.id = plan_assignment_reminder_digest_intents.delivery_id
		     AND d.status IN ('sent', 'failed', 'superseded')
		 )`,
		fmt.Sprintf("%d milliseconds", after.Milliseconds()),
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type key struct {
		deliveryID string
		revision   int64
	}
	keys := make([]key, 0)
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.deliveryID, &k.revision); err != nil {
			return 0, err
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var n int64
	for _, k := range keys {
		updated, err := tx.Update(ctx, "plan_assignment_reminder_digest_intents",
			"payload_text = NULL, payload_redacted_at = clock_timestamp()",
			"delivery_id = $2 AND intent_revision = $3 AND payload_text IS NOT NULL AND payload_redacted_at IS NULL",
			k.deliveryID, k.revision,
		)
		if err != nil {
			return n, err
		}
		n += updated
	}
	return n, nil
}

func deleteRedactedDigestIntentMetadataInScope(ctx context.Context, tx store.TxAccountScope, after time.Duration) (int64, error) {
	n, err := tx.Delete(ctx, "plan_assignment_reminder_digest_intents",
		`payload_redacted_at IS NOT NULL
		 AND payload_redacted_at <= clock_timestamp() - ($2::text)::interval
		 AND EXISTS (
		   SELECT 1 FROM telegram_deliveries d
		   WHERE d.account_id = plan_assignment_reminder_digest_intents.account_id
		     AND d.id = plan_assignment_reminder_digest_intents.delivery_id
		     AND d.status IN ('sent', 'failed', 'superseded')
		 )`,
		fmt.Sprintf("%d milliseconds", after.Milliseconds()),
	)
	if err != nil {
		return 0, fmt.Errorf("delete redacted digest intents: %w", err)
	}
	return n, nil
}
