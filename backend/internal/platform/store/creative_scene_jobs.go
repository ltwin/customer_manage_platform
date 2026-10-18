package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var savepointName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// Savepoint runs fn inside a savepoint of this transaction. When fn fails, the
// work done since the savepoint is rolled back and fn's error returned; the
// transaction itself stays usable, so the caller can record the failure and
// commit.
func (sc TxAccountScope) Savepoint(ctx context.Context, name string, fn func() error) error {
	if !savepointName.MatchString(name) {
		return fmt.Errorf("savepoint name %q", name)
	}
	run := sc.scope.execRunner()
	if _, err := run.Exec(ctx, "SAVEPOINT "+name); err != nil {
		return err
	}
	if err := fn(); err != nil {
		if _, rollback := run.Exec(ctx, "ROLLBACK TO SAVEPOINT "+name); rollback != nil {
			return errors.Join(err, rollback)
		}
		return err
	}
	_, err := run.Exec(ctx, "RELEASE SAVEPOINT "+name)
	return err
}

// ClaimedSceneJob is a creative_scene_jobs row just claimed by a tab.
type ClaimedSceneJob struct {
	ID         string
	DocumentID string
	Kind       string
	Request    json.RawMessage
	Attempt    int
	LeaseUntil time.Time
	Deadline   time.Time
	RunID      *string
	ToolCallID *string
}

// SceneJobClaim says which job a tab may take and how it holds it.
type SceneJobClaim struct {
	// DocumentID limits the claim to one scene; empty claims across the account.
	DocumentID string
	Kinds      []string
	Token      string
	Session    string
	Lease      time.Duration
}

// ClaimCreativeSceneJob takes the oldest queued, unexpired job the tab can run.
// FOR UPDATE SKIP LOCKED gives each job to one claimer even when several tabs
// ask at once; with nothing queued it takes no lock. ok is false when there is
// nothing to claim.
func (sc TxAccountScope) ClaimCreativeSceneJob(ctx context.Context, c SceneJobClaim) (job ClaimedSceneJob, ok bool, err error) {
	if sc.AccountID() == "" {
		return job, false, ErrEmptyAccountScope
	}
	if len(c.Kinds) == 0 || c.Token == "" || c.Session == "" || c.Lease <= 0 {
		return job, false, errors.New("scene job claim args incomplete")
	}
	const sqlText = `
WITH next AS (
  SELECT id FROM creative_scene_jobs
  WHERE account_id = $1 AND state = 'queued' AND deadline > clock_timestamp()
    AND kind = ANY($2::text[]) AND ($3 = '' OR document_id = $3)
  ORDER BY created_at, id
  LIMIT 1
  FOR UPDATE SKIP LOCKED
)
UPDATE creative_scene_jobs j
SET state = 'claimed', claim_token = $4, editor_session_id = $5,
    lease_until = LEAST(clock_timestamp() + ($6::text)::interval, j.deadline),
    updated_at = clock_timestamp()
FROM next
WHERE j.account_id = $1 AND j.id = next.id
RETURNING j.id, j.document_id, j.kind, j.request, j.attempt, j.lease_until, j.deadline, j.run_id, j.tool_call_id`
	row := sc.scope.execRunner().QueryRow(ctx, sqlText, sc.AccountID(), c.Kinds, c.DocumentID, c.Token, c.Session,
		fmt.Sprintf("%d milliseconds", c.Lease.Milliseconds()))
	err = row.Scan(&job.ID, &job.DocumentID, &job.Kind, &job.Request, &job.Attempt, &job.LeaseUntil, &job.Deadline, &job.RunID, &job.ToolCallID)
	if errors.Is(err, ErrNoRows) {
		return ClaimedSceneJob{}, false, nil
	}
	if err != nil {
		return ClaimedSceneJob{}, false, fmt.Errorf("claim scene job: %w", err)
	}
	return job, true, nil
}
