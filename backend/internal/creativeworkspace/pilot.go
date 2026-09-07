package creativeworkspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Pilot 实现 ITEM-2：干净账号准入（preflight 只读 inventory + 逐类清场清单）、原子切换、stop 与写态判定。
// 谓词冻结于 .codestable/work/feat-legacy-planner-barrier.md §4；新增或放宽类别属契约变化。
type Pilot struct {
	repo PostgresRepository
	now  func() time.Time
}

func NewPilot(repo PostgresRepository) *Pilot { return &Pilot{repo: repo, now: time.Now} }

func (p *Pilot) WithClock(now func() time.Time) *Pilot { p.now = now; return p }

// State 返回账号当前 pilot 状态；无记录即 legacy_write。
func (p *Pilot) State(ctx context.Context, sc store.AccountScope) (PilotAccount, error) {
	return p.repo.GetPilot(ctx, sc)
}

// CanWriteNew：只有 pilot_new_write 可以写新模型。
func (p *Pilot) CanWriteNew(ctx context.Context, sc store.AccountScope) error {
	st, err := p.repo.GetPilot(ctx, sc)
	if err != nil {
		return err
	}
	if st.State != PilotNewWrite {
		return ErrPilotRequired
	}
	return nil
}

// LegacyWriteBlocked：pilot_new_write 与 stopped 账号对旧策划写面一律 legacy_read_only。
// stopped 默认仍保持旧历史只读；恢复未来旧计划新建须另行显式授权。
func (p *Pilot) LegacyWriteBlocked(ctx context.Context, sc store.AccountScope) (bool, error) {
	st, err := p.repo.GetPilot(ctx, sc)
	if err != nil {
		return false, err
	}
	return st.State != PilotLegacyWrite, nil
}

// preflightCheck 是一类只读谓词。
type preflightCheck struct {
	key, title, howTo string
	blocking          bool
	count             func(context.Context, scope) (int64, error)
}

func countOf(table, cond string, args ...any) func(context.Context, scope) (int64, error) {
	return func(ctx context.Context, sc scope) (int64, error) { return sc.Count(ctx, table, cond, args...) }
}

// 草稿两类需要 JOIN shoot_plans；scope 只接受单表，故分两步：先取计划状态，再按 plan_id 归类。
func (p *Pilot) draftCounts(ctx context.Context, sc scope) (blocking, diagnostic int64, err error) {
	rows, err := sc.Query(ctx, "planning_business_drafts", "plan_id", "terminal_status = 'fresh'")
	if err != nil {
		return 0, 0, fmt.Errorf("preflight drafts: %w", err)
	}
	planIDs := make([]string, 0, 16)
	perPlan := make(map[string]int64, 16)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, 0, err
		}
		if perPlan[id] == 0 {
			planIDs = append(planIDs, id)
		}
		perPlan[id]++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if len(planIDs) == 0 {
		return 0, 0, nil
	}
	prow, err := sc.Query(ctx, "shoot_plans", "id, status", "id = ANY($2)", planIDs)
	if err != nil {
		return 0, 0, fmt.Errorf("preflight draft plans: %w", err)
	}
	defer prow.Close()
	for prow.Next() {
		var id, status string
		if err := prow.Scan(&id, &status); err != nil {
			return 0, 0, err
		}
		switch status {
		case "draft", "ready", "in_progress":
			blocking += perPlan[id]
		default:
			diagnostic += perPlan[id]
		}
	}
	return blocking, diagnostic, prow.Err()
}

func (p *Pilot) checks() []preflightCheck {
	return []preflightCheck{
		{key: "active_plans", title: "未结束的旧策划", blocking: true,
			howTo: "先处理下方的经营草稿、分享与认领，再对每个策划执行「完成」或「归档」。归档会自动关闭现场会话；完成或归档后系统会短暂整理提醒，属正常现象。",
			count: countOf("shoot_plans", "status IN ('draft','ready','in_progress')")},
		{key: "open_run_sessions", title: "未关闭的现场会话", blocking: true,
			howTo: "完成或归档所属策划即自动关闭。",
			count: countOf("shoot_plan_run_sessions", "closed_at IS NULL")},
		{key: "active_share_tokens", title: "仍有效的分享链接", blocking: true,
			howTo: "在策划的「分享」里撤销该链接，或等待它过期。",
			count: countOf("share_generations", "state = 'active' AND expires_at > $2", nil)},
		{key: "pending_feedback", title: "未处理的客户反馈", blocking: true,
			howTo: "在策划的「反馈」里逐条标记「采纳」或「忽略」。",
			count: countOf("share_feedbacks", "disposition = 'pending'")},
		{key: "active_assignments", title: "进行中的客户认领", blocking: true,
			howTo: "在策划的「分工」里撤销认领；撤销后客户的自撤凭证随之失效。",
			count: countOf("share_assignments", "status = 'active'")},
		{key: "open_offers", title: "仍开放的现场协助邀请", blocking: true,
			howTo: "关闭该邀请；如有进行中的认领需先撤销。",
			count: countOf("share_assignment_offers", "state = 'open'")},
		{key: "pending_plan_reminders", title: "未处理的策划提醒", blocking: true,
			howTo: "在提醒页把它们标记完成或忽略。",
			count: countOf("reminders", "type = 'plan_assignment_checklist' AND status = 'pending'")},
		{key: "reminder_generation_in_progress", title: "系统仍在整理提醒", blocking: true,
			howTo: "无需操作，这通常是上面清场动作的正常产物，稍后重试即可；持续数分钟不消失时联系我们。",
			count: func(ctx context.Context, sc scope) (int64, error) {
				var total int64
				var applied, target int64
				err := sc.QueryRow(ctx, "planning_reminder_account_generations", "applied_generation, target_generation", "").Scan(&applied, &target)
				if err != nil && !errors.Is(err, store.ErrNoRows) {
					return 0, err
				}
				if err == nil && applied < target {
					total++
				}
				for _, q := range []struct{ table, cond string }{
					{"planning_reminder_generation_work", "state <> 'applied'"},
					{"plan_assignment_reminder_quarantines", "resolved_at IS NULL"},
					{"plan_assignment_reminder_reconcile_epochs", "status = 'open'"},
				} {
					n, err := sc.Count(ctx, q.table, q.cond)
					if err != nil {
						return 0, fmt.Errorf("preflight %s: %w", q.table, err)
					}
					total += n
				}
				return total, nil
			}},
	}
}

// Preflight 只读逐类计数；任一阻断类非零即不合格，同时返回全部清单（含诊断类）。
func (p *Pilot) Preflight(ctx context.Context, sc store.AccountScope) (PreflightResult, error) {
	var result PreflightResult
	err := sc.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		r, err := p.preflightIn(ctx, readScopeAdapter{ReadTxAccountScope: tx, accountID: sc.AccountID()})
		result = r
		return err
	})
	return result, err
}

func (p *Pilot) preflightIn(ctx context.Context, sc scope) (PreflightResult, error) {
	now := p.now().UTC()
	result := PreflightResult{Eligible: true, Items: make([]PreflightItem, 0, 12)}
	for _, c := range p.checks() {
		var n int64
		var err error
		if c.key == "active_share_tokens" {
			n, err = sc.Count(ctx, "share_generations", "state = 'active' AND expires_at > $2", now)
		} else {
			n, err = c.count(ctx, sc)
		}
		if err != nil {
			return result, fmt.Errorf("preflight %s: %w", c.key, err)
		}
		item := PreflightItem{Key: c.key, Title: c.title, Count: int(n), Blocking: c.blocking, HowTo: c.howTo}
		if c.blocking && n > 0 {
			result.Eligible = false
		}
		result.Items = append(result.Items, item)
	}
	blockingDrafts, diagDrafts, err := p.draftCounts(ctx, sc)
	if err != nil {
		return result, err
	}
	result.Items = append(result.Items,
		PreflightItem{Key: "fresh_drafts", Title: "未决定的经营草稿", Count: int(blockingDrafts), Blocking: true,
			HowTo: "在旧策划的「经营草稿」里逐条「忽略」；必须在完成或归档策划之前做，完成或归档后就无法再忽略。"},
		PreflightItem{Key: "fresh_drafts_on_terminal_plans", Title: "已完成 / 已归档策划上残留的经营草稿", Count: int(diagDrafts), Blocking: false,
			HowTo: "不影响准入。已完成的策划若想清掉，可「重新打开 → 忽略草稿 → 再次完成」；已归档的无需处理。"},
	)
	if blockingDrafts > 0 {
		result.Eligible = false
	}
	editing, err := sc.Count(ctx, "shoot_plan_ingestion_sessions", "state = 'editing'")
	if err != nil {
		return result, fmt.Errorf("preflight ingestion: %w", err)
	}
	result.Items = append(result.Items, PreflightItem{Key: "editing_ingestions", Title: "编辑中的聊天整理", Count: int(editing), Blocking: false,
		HowTo: "不影响准入；随所属策划归档而失效。"})
	return result, nil
}

// Enroll 通过 preflight 后原子切换：同一事务内再跑一遍全部谓词 + 写 pilot capability。
func (p *Pilot) Enroll(ctx context.Context, sc store.AccountScope, windowID string) (PilotAccount, PreflightResult, error) {
	windowID = strings.TrimSpace(windowID)
	var acct PilotAccount
	var pre PreflightResult
	err := sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		cur, err := p.repo.GetPilot(ctx, tx)
		if err != nil {
			return err
		}
		if cur.State == PilotNewWrite {
			acct = cur
			pre = PreflightResult{Eligible: true, Items: []PreflightItem{}}
			return nil
		}
		if cur.State == PilotStopped {
			return ErrPilotRequired
		}
		r, err := p.preflightIn(ctx, tx)
		if err != nil {
			return err
		}
		pre = r
		if !r.Eligible {
			return ErrPreflightFailed
		}
		now := p.now().UTC()
		var win *string
		if windowID != "" {
			win = &windowID
		}
		acct = PilotAccount{State: PilotNewWrite, EnrolledAt: &now, WindowID: win, Revision: cur.Revision + 1}
		return p.repo.UpsertPilot(ctx, tx, acct, now)
	})
	return acct, pre, err
}

// Stop：停止新增空间；已产生的新空间由新模型只读保留（DEC-9 / 验收 15）。
func (p *Pilot) Stop(ctx context.Context, sc store.AccountScope) (PilotAccount, error) {
	var acct PilotAccount
	err := sc.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		cur, err := p.repo.GetPilot(ctx, tx)
		if err != nil {
			return err
		}
		if cur.State != PilotNewWrite {
			return ErrPilotRequired
		}
		now := p.now().UTC()
		acct = PilotAccount{State: PilotStopped, EnrolledAt: cur.EnrolledAt, StoppedAt: &now, WindowID: cur.WindowID, Revision: cur.Revision + 1}
		return p.repo.UpsertPilot(ctx, tx, acct, now)
	})
	return acct, err
}

// ErrPreflightFailed 表示准入被拒；调用方把 PreflightResult 一并返回给账号。
var ErrPreflightFailed = errors.New("pilot_preflight_failed")

// readScopeAdapter 让只读快照事务满足 scope 接口（写方法永不被调用；调用即 panic 以暴露 bug）。
type readScopeAdapter struct {
	store.ReadTxAccountScope
	accountID string
}

func (r readScopeAdapter) AccountID() string { return r.accountID }

func (r readScopeAdapter) QueryRowForUpdate(context.Context, string, string, string, ...any) store.Row {
	panic("preflight is read-only")
}
func (r readScopeAdapter) Insert(context.Context, string, []string, ...any) error {
	panic("preflight is read-only")
}
func (r readScopeAdapter) Update(context.Context, string, string, string, ...any) (int64, error) {
	panic("preflight is read-only")
}
func (r readScopeAdapter) Delete(context.Context, string, string, ...any) (int64, error) {
	panic("preflight is read-only")
}
func (r readScopeAdapter) Upsert(context.Context, string, []string, []string, []string, ...any) error {
	panic("preflight is read-only")
}
