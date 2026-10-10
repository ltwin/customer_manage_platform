package creativegeneration

import (
	"context"
	"log/slog"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// SweepDue补投递已丢失或耗尽的队列任务，不从内存重建生成输入。
func (s *Service) SweepDue(ctx context.Context, db *store.Store, after string, batch int) (string, error) {
	if batch < 1 || batch > 1000 || s.queue == nil {
		return after, gw.ErrValidation
	}
	accounts, err := db.MaintenanceAccountIDs(ctx, after, batch)
	if err != nil {
		return after, err
	}
	for _, id := range accounts {
		scope := db.ScopeFor(auth.AccountContext{AccountID: id})
		if err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			rows, err := tx.QueryPage(ctx, "creative_generation_bindings", "execution_id,canvas_id", "next_step_at<=clock_timestamp() AND verification_required=FALSE", []store.OrderBy{{Column: "next_step_at"}, {Column: "execution_id"}}, batch, 0)
			if err != nil {
				return err
			}
			var targets []creativecanvas.ExecutionTarget
			for rows.Next() {
				var v creativecanvas.ExecutionTarget
				if err = rows.Scan(&v.ExecutionID, &v.CanvasID); err != nil {
					rows.Close()
					return err
				}
				targets = append(targets, v)
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				return err
			}
			for _, v := range targets {
				if err = s.enqueue(ctx, tx, v); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return after, err
		}
	}
	if len(accounts) == batch {
		return accounts[len(accounts)-1], nil
	}
	return "", nil
}

// Resume只恢复导入/核实调度，不重发模型请求。最终派发仍受G3一次性许可保护。
func (s *Service) Resume(ctx context.Context, scope store.AccountScope, target creativecanvas.ExecutionTarget) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "manual_write"); err != nil {
			return err
		}
		count, err := tx.Update(ctx, "creative_generation_bindings", "verification_required=FALSE,failures=0,next_step_at=clock_timestamp(),recovery_until=clock_timestamp()+interval '24 hours'", "execution_id=$2 AND canvas_id=$3 AND (lease_until IS NULL OR lease_until<=clock_timestamp())", target.ExecutionID, target.CanvasID)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrBusy
		}
		return s.enqueue(ctx, tx, target)
	})
}

// RunRecovery由组合根持有生命周期，同时扫描Gateway与交接due。单轮失败只记录，
// 下一轮从头重扫，不让一次数据库抖动停掉整个worker。
func (s *Service) RunRecovery(ctx context.Context, db *store.Store, logger *slog.Logger) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	gatewayCursor, canvasCursor := "", ""
	for {
		sweepCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		var err error
		if gatewayCursor, err = s.generation.SweepGenerationDue(sweepCtx, db, gatewayCursor, 100); err != nil && ctx.Err() == nil {
			gatewayCursor = ""
			logger.Warn("creative generation recovery", slog.String("stage", "gateway"), slog.String("error", err.Error()))
		}
		if canvasCursor, err = s.SweepDue(sweepCtx, db, canvasCursor, 100); err != nil && ctx.Err() == nil {
			canvasCursor = ""
			logger.Warn("creative generation recovery", slog.String("stage", "handoff"), slog.String("error", err.Error()))
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
