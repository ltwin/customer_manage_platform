import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useShell } from '../components/shellContext'
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
  const { notify } = useShell()
  const [filter, setFilter] = useState<'all' | ShootPlanStatus>('all')
  const [reloadTick, setReloadTick] = useState(0)
  const [state, setState] = useState<PageReadState<ShootPlanList>>({ kind: 'loading', message: '正在加载拍摄策划' })
  const [creating, setCreating] = useState(false)

  // 一键空白建案：标题/主体给默认值（后端要求非空），客户、订单与档期都可后补。
  async function createBlank() {
    setCreating(true)
    try {
      const plan = await createShootPlan({ title: '未命名策划', subject: '待补充' }, newPlanningMutationKey('create'))
      notify('已新建空白策划（客户与订单均可留空）')
      navigate(`/shoot-plans/${encodeURIComponent(plan.id)}`)
    } catch (cause) {
      notify(planningErrorMessage(cause, '新建策划失败；请稍后重试。'))
    } finally {
      setCreating(false)
    }
  }

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
          <button className="btn btn-primary" type="button" disabled={creating} onClick={() => void createBlank()}>{creating ? '正在创建…' : '＋ 新建策划'}</button>
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
    </>
  )
}


function formatDateTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? value : new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(date)
}
