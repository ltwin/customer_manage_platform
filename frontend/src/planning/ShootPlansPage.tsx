import { useCallback, useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import StateNotice from '../components/StateNotice'
import {
  beginPageRead,
  completePageRead,
  failPageRead,
  pageReadPresentation,
  readyPageData,
  type PageReadState,
} from '../components/pageReadState'
import {
  createShootPlan,
  listShootPlans,
  newPlanningMutationKey,
  type ShootPlanList,
  type ShootPlanStatus,
} from './api'
import StatusBadge from './StatusBadge'
import { crmSummaryLine, listStatusLines, windowRangeLabel } from './listCard'
import { planningErrorMessage, shootPlanStatusLabel } from './presentation'
import './planning.css'

export default function ShootPlansPage() {
  const navigate = useNavigate()
  const [filter, setFilter] = useState<'all' | ShootPlanStatus>('all')
  const [reloadTick, setReloadTick] = useState(0)
  const [state, setState] = useState<PageReadState<ShootPlanList>>({ kind: 'loading', message: '正在加载拍摄策划' })
  const [createOpen, setCreateOpen] = useState(false)

  const reload = useCallback(() => setReloadTick((value) => value + 1), [])
  useEffect(() => {
    let active = true
    setState((current) => beginPageRead(current, '正在加载拍摄策划', true))
    listShootPlans({
      status: filter === 'all' ? undefined : filter,
      archived: filter === 'archived' ? true : false,
      page: 1,
      pageSize: 50,
    }).then((result) => {
      if (!active) return
      setState(completePageRead(result, result.items.length === 0, filter === 'all' ? '还没有拍摄策划，可以先新建一份空白策划。' : '当前筛选下没有拍摄策划。'))
    }).catch((error: unknown) => {
      if (!active) return
      setState((current) => failPageRead(current, planningErrorMessage(error, '拍摄策划加载失败'), reload))
    })
    return () => { active = false }
  }, [filter, reloadTick, reload])

  const presentation = pageReadPresentation(state)
  const list = readyPageData(state)

  return (
    <>
      <header className="topbar">
        <div>
          <div className="crumb">创作 / 拍摄策划</div>
          <h1>拍摄策划</h1>
        </div>
        <div className="topbar-actions">
          <button className="btn btn-primary" type="button" onClick={() => setCreateOpen(true)}>＋ 新建策划</button>
        </div>
      </header>
      <main className="content planning-content">
        <section className="planning-ledger-head">
          <div>
            <p className="planning-eyebrow">策划台账</p>
            <h2>全部拍摄策划</h2>
            <p className="muted-text">策划是独立的创作工具，不关联客户或订单也能正常使用。</p>
          </div>
          <label className="planning-filter">
            <span>状态筛选</span>
            <select className="input" value={filter} onChange={(event) => setFilter(event.target.value as 'all' | ShootPlanStatus)}>
              <option value="all">全部状态</option>
              {(['draft', 'ready', 'in_progress', 'completed', 'archived'] as const).map((status) => (
                <option key={status} value={status}>{shootPlanStatusLabel(status)}</option>
              ))}
            </select>
          </label>
        </section>

        {presentation.notice && <StateNotice {...presentation.notice} />}
        {list && (
          <div className="planning-plan-list" aria-live="polite">
            {list.items.map((plan) => (
              <button className="planning-plan-card" type="button" key={plan.id} onClick={() => navigate(`/shoot-plans/${encodeURIComponent(plan.id)}`)}>
                <div className="planning-plan-main">
                  <div className="planning-title-line">
                    <h3>{plan.title}</h3>
                    <StatusBadge status={plan.status} />
                  </div>
                  <p>主体：{plan.subject} · {crmSummaryLine(plan.crm_summary)} · {plan.public_scale.planned_shot_count} 个镜头 · 第 {plan.revision} 版</p>
                  <div className="planning-meta">
                    {plan.execution_window_summary ? (
                      <span>{windowRangeLabel(plan.execution_window_summary.starts_at, plan.execution_window_summary.ends_at, plan.execution_window_summary.timezone)}</span>
                    ) : (
                      <span>未设执行时间</span>
                    )}
                    {listStatusLines(plan).map((line) => (
                      <span key={line}>{line}</span>
                    ))}
                    <span>更新于 {formatDateTime(plan.updated_at)}</span>
                  </div>
                </div>
                <span className="planning-open-hint">打开工作台 →</span>
              </button>
            ))}
          </div>
        )}
      </main>
      {createOpen && (
        <CreatePlanDialog
          onClose={() => setCreateOpen(false)}
          onCreated={(id) => navigate(`/shoot-plans/${encodeURIComponent(id)}`)}
        />
      )}
    </>
  )
}

function CreatePlanDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (id: string) => void }) {
  const [title, setTitle] = useState('')
  const [subject, setSubject] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [key] = useState(() => newPlanningMutationKey('create'))

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (!title.trim() || !subject.trim()) {
      setError('标题和拍摄主体都需要填写。')
      return
    }
    setSaving(true)
    setError(null)
    try {
      const plan = await createShootPlan({ title: title.trim(), subject: subject.trim() }, key)
      onCreated(plan.id)
    } catch (cause) {
      setError(planningErrorMessage(cause, '新建策划失败；再次提交会安全重放同一请求。'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="overlay open" onMouseDown={(event) => { if (event.target === event.currentTarget && !saving) onClose() }}>
      <form className="dialog planning-dialog" role="dialog" aria-modal="true" aria-labelledby="createPlanTitle" onSubmit={submit}>
        <h2 id="createPlanTitle">新建拍摄策划</h2>
        <p className="dialog-sub">客户、订单和档期都不是必填；先把创作想法独立保存下来。</p>
        <label className="field">
          <span>标题</span>
          <input className="input" value={title} maxLength={160} autoFocus onChange={(event) => setTitle(event.target.value)} placeholder="例如：废墟机娘 · 银灰甲胄" />
        </label>
        <label className="field">
          <span>拍摄主体</span>
          <input className="input" value={subject} maxLength={240} onChange={(event) => setSubject(event.target.value)} placeholder="角色、人物或创作对象" />
          <span className="hint">这是自由文本，不要求对应客户档案。</span>
        </label>
        {error && <p className="planning-inline-error" role="alert">{error}</p>}
        <div className="dialog-actions">
          <button className="btn" type="button" disabled={saving} onClick={onClose}>取消</button>
          <button className="btn btn-primary" type="submit" disabled={saving}>{saving ? '正在创建…' : '创建并打开'}</button>
        </div>
      </form>
    </div>
  )
}

function formatDateTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? value : new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(date)
}
