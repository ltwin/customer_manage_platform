package creativescene

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	sweepInterval  = 15 * time.Second
	jobRetention   = 30 * 24 * time.Hour
	sweepBatchSize = 100
)

// SweepCounts is what one sweep did, for the per-tick summary log.
type SweepCounts struct {
	Requeued   int
	HostErrors int
	Expired    int
	Deleted    int
	Aborted    int
}

func (c *SweepCounts) add(o SweepCounts) {
	c.Requeued += o.Requeued
	c.HostErrors += o.HostErrors
	c.Expired += o.Expired
	c.Deleted += o.Deleted
	c.Aborted += o.Aborted
}

// SweepJobs moves one account's jobs past their lease or deadline and deletes
// old finished ones. Each job gets its own transaction, so a run that cannot be
// settled does not hold up the others.
func SweepJobs(ctx context.Context, scope store.AccountScope, batch int) (SweepCounts, error) {
	var counts SweepCounts
	lapsed, err := jobIDs(ctx, scope, "state='claimed' AND lease_until<clock_timestamp() AND deadline>clock_timestamp()", batch)
	if err != nil {
		return counts, err
	}
	overdue, err := jobIDs(ctx, scope, "state IN ('queued','claimed') AND deadline<=clock_timestamp()", batch)
	if err != nil {
		return counts, err
	}
	var errs []error
	for _, id := range lapsed {
		if err := reclaim(ctx, scope, id, &counts); err != nil {
			errs = append(errs, fmt.Errorf("job %s: %w", id, err))
		}
	}
	for _, id := range overdue {
		if err := expire(ctx, scope, id, &counts); err != nil {
			errs = append(errs, fmt.Errorf("job %s: %w", id, err))
		}
	}
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		n, err := tx.Delete(ctx, "creative_scene_jobs", fmt.Sprintf(
			"id IN (SELECT id FROM creative_scene_jobs WHERE account_id=$1 AND state IN ('succeeded','failed','expired','cancelled') AND updated_at<clock_timestamp()-interval '%d hours' LIMIT %d)",
			int(jobRetention.Hours()), batch))
		counts.Deleted = int(n)
		return err
	})
	if err != nil {
		errs = append(errs, err)
	}
	return counts, errors.Join(errs...)
}

func jobIDs(ctx context.Context, scope store.AccountScope, cond string, batch int) ([]string, error) {
	rows, err := scope.QueryPage(ctx, "creative_scene_jobs", "id", cond, []store.OrderBy{{Column: "created_at"}, {Column: "id"}}, batch, 0)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// reclaim puts a job whose host stopped renewing back in the queue. The third
// lapse fails it instead: a job that keeps losing its host (a crash, a closed
// tab, a page that cannot run it) would otherwise keep the run waiting until
// the deadline and keep knocking over the account's open tabs in turn.
func reclaim(ctx context.Context, scope store.AccountScope, id string, counts *SweepCounts) error {
	return settleOnce(ctx, scope, id, counts, func(job Job, leaseLapsed, overdue bool) (*JobResult, bool) {
		if job.State != JobClaimed || !leaseLapsed || overdue {
			return nil, false
		}
		if job.Attempt+1 < maxJobAttempts {
			return nil, true
		}
		return &JobResult{Failure: &JobFailure{Code: "host_error", Message: "页面多次未能完成任务（关闭、崩溃或不支持）"}}, false
	})
}

// expire ends a job nobody finished in time and tells the run.
func expire(ctx context.Context, scope store.AccountScope, id string, counts *SweepCounts) error {
	return settleOnce(ctx, scope, id, counts, func(job Job, _, overdue bool) (*JobResult, bool) {
		if Terminal(job.State) || !overdue {
			return nil, false
		}
		return &JobResult{Failure: &JobFailure{Code: JobExpired, Message: "页面未打开，任务已过期"}}, false
	})
}

// settleOnce runs one sweep decision for one job in the run lock order: the
// run (if any) is admitted before the job is locked. decide sees the locked job
// and returns a terminal result, or requeue=true, or neither (nothing to do).
func settleOnce(ctx context.Context, scope store.AccountScope, id string, counts *SweepCounts,
	decide func(job Job, leaseLapsed, overdue bool) (res *JobResult, requeue bool)) error {
	var gateFailed *runGateError
	var done SweepCounts
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		done = SweepCounts{}
		job, created, err := scanJob(tx.QueryRow(ctx, "creative_scene_jobs", jobColumns, "id=$2", id))
		if err != nil {
			return err
		}
		w := currentWaiter()
		var gate RunGate
		if job.RunID != nil && !Terminal(job.State) {
			if w == nil {
				return errors.New("creative scene: no job waiter registered")
			}
			gate, err = w.Admit(ctx, tx, *job.RunID)
			if errors.Is(err, ErrRunGate) {
				gateFailed = &runGateError{runID: *job.RunID, cause: err}
				return gateFailed
			}
			if err != nil {
				return err
			}
		}
		job, _, err = scanJob(tx.QueryRowForUpdate(ctx, "creative_scene_jobs", jobColumns, "id=$2", id))
		if err != nil {
			return err
		}
		var leaseUntil *time.Time
		if err := tx.QueryRow(ctx, "creative_scene_jobs", "lease_until", "id=$2", id).Scan(&leaseUntil); err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		res, requeue := decide(job, leaseUntil != nil && !now.Before(*leaseUntil), !now.Before(job.Deadline))
		if requeue {
			if _, err := tx.Update(ctx, "creative_scene_jobs",
				"state='queued', attempt=attempt+1, claim_token=NULL, editor_session_id=NULL, lease_until=NULL, updated_at=clock_timestamp()", "id=$2", id); err != nil {
				return err
			}
			logTransition(job, JobQueued, "lease_lapsed", created)
			done.Requeued++
			return nil
		}
		if res == nil {
			return nil
		}
		expired := res.Failure != nil && res.Failure.Code == JobExpired
		if err := finishJob(ctx, tx, job, *res, expired, false, created); err != nil {
			return err
		}
		if expired {
			done.Expired++
		} else {
			done.HostErrors++
		}
		if gate == nil {
			return nil
		}
		job.State = res.state(expired, false)
		return w.Settle(ctx, tx, gate, job, *res)
	})
	if gateFailed != nil {
		if w := currentWaiter(); w != nil {
			if err := w.Abort(ctx, scope, gateFailed.runID, gateFailed.cause); err != nil {
				return err
			}
		}
		counts.Aborted++
		return nil
	}
	if err == nil {
		counts.add(done)
	}
	return err
}

// SweepAllAccounts sweeps one page of accounts and returns where the next page
// starts ("" once the last page is done).
func SweepAllAccounts(ctx context.Context, db *store.Store, after string, batch int) (string, SweepCounts, error) {
	var total SweepCounts
	ids, err := db.MaintenanceAccountIDs(ctx, after, batch)
	if err != nil {
		return after, total, err
	}
	var errs []error
	attempted := after
	for _, id := range ids {
		if ctx.Err() != nil {
			return attempted, total, errors.Join(append(errs, ctx.Err())...)
		}
		attempted = id
		counts, err := SweepJobs(ctx, db.ScopeFor(auth.AccountContext{AccountID: id}), batch)
		total.add(counts)
		if err != nil {
			errs = append(errs, fmt.Errorf("account %s: %w", id, err))
		}
	}
	next := ""
	if len(ids) == batch {
		next = ids[len(ids)-1]
	}
	return next, total, errors.Join(errs...)
}

// RunSweep sweeps every 15 seconds until ctx ends, logging one summary per tick
// that did something.
func RunSweep(ctx context.Context, db *store.Store, log *slog.Logger) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	cursor := ""
	for {
		sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		next, counts, err := SweepAllAccounts(sweepCtx, db, cursor, sweepBatchSize)
		cancel()
		cursor = next
		if err != nil && ctx.Err() == nil {
			log.Warn("creative scene job sweep", slog.String("error", err.Error()))
		}
		if counts != (SweepCounts{}) {
			log.Info("creative scene job sweep", slog.Int("requeued", counts.Requeued), slog.Int("host_errors", counts.HostErrors),
				slog.Int("expired", counts.Expired), slog.Int("deleted", counts.Deleted), slog.Int("aborted", counts.Aborted))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
