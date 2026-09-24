package llmgateway

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const generationJobKind = "llm_generation_advance"

type generationJob struct {
	RequestID string `json:"request_id"`
}

func (r *GenerationRuntime) enqueue(ctx context.Context, tx store.TxAccountScope, id string) error {
	payload, err := json.Marshal(generationJob{RequestID: id})
	if err != nil {
		return err
	}
	_, err = jobs.EnqueueInTx(ctx, tx.Jobs(r.runtime), jobs.Request{Kind: generationJobKind, OperationID: uuid.NewString(), CreatedAt: r.gateway.now().UTC(), Payload: payload, UniqueByArgs: true})
	return err
}
func (r *GenerationRuntime) Handlers() []store.JobHandler {
	return []store.JobHandler{{Kind: generationJobKind, AllowInactiveAccount: true, Timeout: transportLifetime + finishTimeout + time.Minute, Work: func(ctx context.Context, scope store.AccountScope, job jobs.Request) error {
		var in generationJob
		if err := json.Unmarshal(job.Payload, &in); err != nil || in.RequestID == "" {
			return jobs.ErrInvalidTask
		}
		progress, err := r.AdvanceGeneration(ctx, scope, in.RequestID)
		if err != nil {
			return err
		}
		if progress.NextAdvanceAt != nil {
			return jobs.Defer(min(time.Hour, max(time.Second, progress.NextAdvanceAt.Sub(r.gateway.now()))))
		}
		return nil
	}}}
}

// SweepGenerationDue 从持久due索引补投递，队列丢失/耗尽不会使任务永久搁浅。
// 一页账号、一页请求；包括停用账号的既有任务，恢复核算不要求新外发授权。
func (r *GenerationRuntime) SweepGenerationDue(ctx context.Context, db *store.Store, after string, batch int) (string, error) {
	if batch < 1 || batch > 1000 || r.runtime == nil {
		return after, ErrValidation
	}
	accounts, err := db.MaintenanceAccountIDs(ctx, after, batch)
	if err != nil {
		return after, err
	}
	var errs []error
	for _, id := range accounts {
		scope := db.ScopeFor(auth.AccountContext{AccountID: id})
		err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			rows, err := tx.QueryPage(ctx, "llm_generation_requests", "request_id", "control_revision>0 AND next_advance_at<=$2 AND (verification_required=FALSE OR request_id IN (SELECT id FROM llm_requests WHERE account_id=$1 AND state='prepared' AND deadline_at<=$2))", []store.OrderBy{{Column: "next_advance_at"}, {Column: "request_id"}}, batch, 0, r.gateway.now().UTC())
			if err != nil {
				return err
			}
			var ids []string
			for rows.Next() {
				var request string
				if err := rows.Scan(&request); err != nil {
					rows.Close()
					return err
				}
				ids = append(ids, request)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			for _, request := range ids {
				if err := r.enqueue(ctx, tx, request); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	next := ""
	if len(accounts) == batch {
		next = accounts[len(accounts)-1]
	}
	return next, errors.Join(errs...)
}

// RunRecovery 由组合根持有并等待退出；每轮有界扫描，返回时没有遗留goroutine。
func (r *GenerationRuntime) RunRecovery(ctx context.Context, db *store.Store) error {
	ticker := time.NewTicker(generationPoll)
	defer ticker.Stop()
	cursor := ""
	for {
		sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		next, err := r.SweepGenerationDue(sweepCtx, db, cursor, 100)
		cancel()
		cursor = next
		if err != nil && ctx.Err() == nil {
			r.gateway.logger.WarnContext(ctx, "generation recovery sweep failed")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
