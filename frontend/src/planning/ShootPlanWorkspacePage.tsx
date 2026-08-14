import { useCallback, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import StateNotice from '../components/StateNotice'
import {
  beginPageRead,
  completePageRead,
  failPageRead,
  pageReadPresentation,
  readyPageData,
  type PageReadState,
} from '../components/pageReadState'
import { ApiError } from '../api/client'
import {
  applyShootPlanCommand,
  getShootPlan,
  newPlanningMutationKey,
  transitionShootPlan,
  type PlanTransition,
  type ShootPlanDetail,
  type ShootPlanMutationRequest,
} from './api'
import StatusBadge from './StatusBadge'
import { planningErrorMessage } from './presentation'
import BriefPanel from './panels/BriefPanel'
import ShotsPanel from './panels/ShotsPanel'
import ReadinessPanel from './panels/ReadinessPanel'
import ExecutionHistoryPanel from './panels/ExecutionHistoryPanel'
import PlanningMediaPanel from './panels/PlanningMediaPanel'
import ShareCollaborationPanel from './share/ShareCollaborationPanel'
import './planning.css'
import './share/share.css'

export type CommandRunner = (command: ShootPlanMutationRequest, scope: string) => Promise<void>
export type TransitionRunner = (transition: PlanTransition, scope: string) => Promise<void>

type WorkspaceTab = 'brief' | 'shots' | 'readiness' | 'assets' | 'share' | 'history'

function parseWorkspaceTab(value: string | null): WorkspaceTab | null {
  if (value === 'brief' || value === 'shots' || value === 'readiness' || value === 'assets' || value === 'share' || value === 'history') {
    return value
  }
  return null
}

function parseShareFocus(value: string | null):
  | { kind: 'feedback' }
  | { kind: 'shot'; shotID: string }
  | { kind: 'offer'; offerID: string }
  | { kind: 'readiness'; readinessItemID: string }
  | null {
  if (!value) return null
  if (value === 'feedback') return { kind: 'feedback' }
  if (value.startsWith('shot:')) return { kind: 'shot', shotID: value.slice(5) }
  if (value.startsWith('offer:')) return { kind: 'offer', offerID: value.slice(6) }
  if (value.startsWith('readiness:')) return { kind: 'readiness', readinessItemID: value.slice(10) }
  return null
}

export default function ShootPlanWorkspacePage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const initialTab = parseWorkspaceTab(searchParams.get('tab')) ?? 'brief'
  const [tab, setTab] = useState<WorkspaceTab>(initialTab)
  const shareFocus = parseShareFocus(searchParams.get('focus'))

  useEffect(() => {
    const next = parseWorkspaceTab(searchParams.get('tab'))
    if (next) setTab(next)
  }, [searchParams])
  const [state, setState] = useState<PageReadState<ShootPlanDetail>>({ kind: 'loading', message: '正在加载策划工作台' })
  const [reloadTick, setReloadTick] = useState(0)
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<string | null>(null)
  const pendingKeys = useRef(new Map<string, string>())

  const reload = useCallback(() => setReloadTick((value) => value + 1), [])
  const load = useCallback(async (preserve = true) => {
    setState((current) => beginPageRead(current, '正在加载策划工作台', preserve))
    try {
      const detail = await getShootPlan(id, true)
      setState(completePageRead(detail, false, ''))
      return detail
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) {
        setState({ kind: 'error', retryable: false, message: '这份策划不存在或不属于当前账号。' })
      } else {
        setState((current) => failPageRead(current, planningErrorMessage(error, '策划工作台加载失败'), reload))
      }
      throw error
    }
  }, [id, reload])

  useEffect(() => {
    let active = true
    setState((current) => beginPageRead(current, '正在加载策划工作台', true))
    getShootPlan(id, true).then((detail) => {
      if (active) setState(completePageRead(detail, false, ''))
    }).catch((error: unknown) => {
      if (!active) return
      if (error instanceof ApiError && error.status === 404) {
        setState({ kind: 'error', retryable: false, message: '这份策划不存在或不属于当前账号。' })
      } else {
        setState((current) => failPageRead(current, planningErrorMessage(error, '策划工作台加载失败'), reload))
      }
    })
    return () => { active = false }
  }, [id, reloadTick, reload])

  const runCommand: CommandRunner = useCallback(async (command, scope) => {
    const signature = `command:${JSON.stringify(command)}`
    const key = pendingKeys.current.get(signature) ?? newPlanningMutationKey(scope)
    pendingKeys.current.set(signature, key)
    setBusy(true)
    setFeedback(null)
    try {
      await applyShootPlanCommand(id, command, key)
      pendingKeys.current.delete(signature)
      await load(true)
      setFeedback('已保存最新版本。')
    } catch (error) {
      if (error instanceof ApiError && error.code === 'plan_revision_conflict') {
        await load(true).catch(() => undefined)
        setFeedback('策划已在其他页面更新。页面已刷新，请核对保留的输入后重新保存。')
      }
      throw error
    } finally {
      setBusy(false)
    }
  }, [id, load])

  const runTransition: TransitionRunner = useCallback(async (transition, scope) => {
    const signature = `transition:${JSON.stringify(transition)}`
    const key = pendingKeys.current.get(signature) ?? newPlanningMutationKey(scope)
    pendingKeys.current.set(signature, key)
    setBusy(true)
    setFeedback(null)
    try {
      await transitionShootPlan(id, transition, key)
      pendingKeys.current.delete(signature)
      await load(true)
      setFeedback('策划状态已更新。')
    } catch (error) {
      if (error instanceof ApiError && (error.code === 'plan_revision_conflict' || error.code === 'archive_acknowledgement_required')) {
        await load(true).catch(() => undefined)
        setFeedback(error.code === 'archive_acknowledgement_required'
          ? '归档影响已变化。页面已刷新，请重新阅读并确认。'
          : '策划版本已变化。页面已刷新，请重新确认状态。')
      }
      throw error
    } finally {
      setBusy(false)
    }
  }, [id, load])

  const presentation = pageReadPresentation(state)
  const plan = readyPageData(state)

  return (
    <>
      <header className="topbar">
        <div>
          <div className="crumb"><Link to="/shoot-plans">拍摄策划</Link> / 工作台</div>
          <h1>{plan?.title ?? '策划工作台'}</h1>
        </div>
        <div className="topbar-actions">
          <button className="btn" type="button" onClick={() => navigate('/shoot-plans')}>← 全部策划</button>
          {plan && plan.status !== 'archived' && <button className="btn btn-primary" type="button" onClick={() => navigate(`/shoot-plans/${encodeURIComponent(plan.id)}/ingestions/new`)}>从聊天整理</button>}
          {plan && <StatusActions plan={plan} busy={busy} runTransition={runTransition} onRun={() => navigate(`/shoot-plans/${encodeURIComponent(plan.id)}/run`)} />}
        </div>
      </header>
      <main className="content planning-content planning-workspace">
        {presentation.notice && <StateNotice {...presentation.notice} />}
        {feedback && <div className="planning-feedback" role="status">{feedback}</div>}
        {plan && (
          <>
            <div className="planning-summary-strip">
              <StatusBadge status={plan.status} />
              <span>第 {plan.revision} 版</span>
              <span>主体：{plan.subject}</span>
              <span>{plan.public_scale.planned_shot_count} 个镜头</span>
              {plan.status === 'archived' && <span className="danger-text">只读</span>}
            </div>
            <div className="tabs planning-tabs" role="tablist" aria-label="策划分区">
              <TabButton active={tab === 'brief'} onClick={() => setTab('brief')}>创作 brief</TabButton>
              <TabButton active={tab === 'shots'} onClick={() => setTab('shots')}>镜头表 {plan.shots.length}</TabButton>
              <TabButton active={tab === 'readiness'} onClick={() => setTab('readiness')}>准备项 {plan.readiness_items.length}</TabButton>
              <TabButton active={tab === 'assets'} onClick={() => setTab('assets')}>参考素材</TabButton>
              <TabButton active={tab === 'share'} onClick={() => setTab('share')}>分享协作</TabButton>
              <TabButton active={tab === 'history'} onClick={() => setTab('history')}>执行历史</TabButton>
            </div>
            <div className="tab-panel active">
              {tab === 'brief' && <BriefPanel plan={plan} busy={busy} runCommand={runCommand} />}
              {tab === 'shots' && <ShotsPanel plan={plan} busy={busy} runCommand={runCommand} />}
              {tab === 'readiness' && <ReadinessPanel plan={plan} busy={busy} runCommand={runCommand} />}
              {tab === 'assets' && <PlanningMediaPanel planID={plan.id} planRevision={plan.revision} readOnly={plan.status === 'archived'} />}
              {tab === 'share' && (
                <ShareCollaborationPanel
                  planID={plan.id}
                  planRevision={plan.revision}
                  readOnly={plan.status === 'archived'}
                  focus={shareFocus}
                />
              )}
              {tab === 'history' && <ExecutionHistoryPanel plan={plan} busy={busy} onReload={() => load(true).then(() => undefined)} />}
            </div>
          </>
        )}
      </main>
    </>
  )
}

function TabButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return <button className={`tab${active ? ' active' : ''}`} role="tab" aria-selected={active} type="button" onClick={onClick}>{children}</button>
}

function StatusActions({ plan, busy, runTransition, onRun }: { plan: ShootPlanDetail; busy: boolean; runTransition: TransitionRunner; onRun: () => void }) {
  const [error, setError] = useState<string | null>(null)
  async function transition(kind: 'mark_ready' | 'start' | 'complete' | 'reopen') {
    setError(null)
    let body: PlanTransition
    switch (kind) {
      case 'mark_ready':
        body = { expected_revision: plan.revision, transition: 'mark_ready', payload: {} }
        break
      case 'start':
        body = { expected_revision: plan.revision, transition: 'start', payload: {} }
        break
      case 'complete':
        body = { expected_revision: plan.revision, transition: 'complete', payload: { expected_execution_fact_revision: plan.execution_fact_revision } }
        break
      case 'reopen':
        body = { expected_revision: plan.revision, transition: 'reopen', payload: {} }
        break
    }
    try {
      await runTransition(body, kind)
    } catch (cause) {
      setError(planningErrorMessage(cause, '状态更新失败'))
    }
  }

  async function archive() {
    const acknowledgement = plan.required_archive_acknowledgement
    const effects = acknowledgement.effects.map((effect) => `• ${archiveEffectLabel(effect)}`).join('\n')
    if (!window.confirm(`归档后策划将永久只读，执行历史会保留。\n\n${effects}\n\n确认归档吗？`)) return
    setError(null)
    try {
      await runTransition({ expected_revision: plan.revision, transition: 'archive', payload: acknowledgement } as PlanTransition, 'archive')
    } catch (cause) {
      setError(planningErrorMessage(cause, '归档失败'))
    }
  }

  return (
    <div className="planning-status-actions">
      {(plan.status === 'ready' || plan.status === 'in_progress') && <button className="btn btn-primary" disabled={busy} type="button" onClick={onRun}>进入 Run Mode</button>}
      {plan.status === 'draft' && <button className="btn btn-primary" disabled={busy} type="button" onClick={() => void transition('mark_ready')}>标记已就绪</button>}
      {plan.status === 'ready' && <button className="btn btn-primary" disabled={busy} type="button" onClick={() => void transition('start')}>手动开始拍摄</button>}
      {plan.status === 'in_progress' && <button className="btn btn-primary" disabled={busy} type="button" onClick={() => void transition('complete')}>标记完成</button>}
      {plan.status === 'completed' && <button className="btn" disabled={busy} type="button" onClick={() => { if (window.confirm('重新打开后可继续修改并产生新的完成快照，旧快照会保留。')) void transition('reopen') }}>重新打开</button>}
      {plan.status !== 'archived' && <button className="btn btn-danger-ghost" disabled={busy} type="button" onClick={() => void archive()}>归档</button>}
      {error && <span className="planning-action-error" role="alert">{error}</span>}
    </div>
  )
}

function archiveEffectLabel(effect: string): string {
  const labels: Record<string, string> = {
    plan_becomes_read_only: '策划变为只读',
    execution_history_retained: '执行历史继续保留',
    active_share_links_become_unavailable: '现有分享链接失效',
    share_feedback_retained: '分享反馈继续保留',
    share_assignments_retained: '准备认领历史继续保留',
    active_assignment_reminders_withdrawn: '现有认领项检查提醒撤回',
  }
  return labels[effect] ?? effect
}
