import { useCallback, useEffect, useRef, useState } from 'react'
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
  type ShootPlanListItem,
  type ShootPlanStatus,
} from './api'
import StatusBadge from './StatusBadge'
import { crmSummaryLine, listStatusLines, windowRangeLabel } from './listCard'
import { planningErrorMessage, shootPlanStatusLabel } from './presentation'
import './planning.css'

const planPageSize = 50

// 累加式翻页：已加载的页留在列表里，page 记住下一页从哪续。
type PlanListPage = { items: ShootPlanListItem[]; total: number; page: number }

export default function ShootPlansPage() {
  const navigate = useNavigate()
  const { notify } = useShell()
  const [filter, setFilter] = useState<'all' | ShootPlanStatus>('all')
  const [reloadTick, setReloadTick] = useState(0)
  const [state, setState] = useState<PageReadState<PlanListPage>>({ kind: 'loading', message: '正在加载拍摄策划' })
  const [creating, setCreating] = useState<'ingestion' | 'workspace' | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const filterRef = useRef(filter)
  const loadMoreRequestSeq = useRef(0)

  useEffect(() => { filterRef.current = filter }, [filter])

  // 一键空白建案：标题/主体给默认值（后端要求非空），客户、订单与档期都可后补。
  // 两个入口建的都是同一份空白策划，区别只在建完落到哪一步：主路径直接进「从聊天整理」，
  // 次级入口保留摄影师零成本先建案、之后再补信息的走法。
  async function createBlank(next: 'ingestion' | 'workspace') {
    setCreating(next)
    try {
      const plan = await createShootPlan({ title: '未命名策划', subject: '待补充' }, newPlanningMutationKey('create'))
      notify(next === 'ingestion' ? '已新建策划，接着把聊天记录整理成方案。' : '已新建空白策划（客户与订单均可留空）')
      const path = `/shoot-plans/${encodeURIComponent(plan.id)}`
      navigate(next === 'ingestion' ? `${path}/ingestions/new` : path)
    } catch (cause) {
      notify(planningErrorMessage(cause, '新建策划失败；请稍后重试。'))
    } finally {
      setCreating(null)
    }
  }

  const reload = useCallback(() => setReloadTick((value) => value + 1), [])
  useEffect(() => {
    let active = true
    // 换筛选等于重开一份列表：在途的「加载更多」作废，避免旧筛选的下一页追进新结果。
    loadMoreRequestSeq.current += 1
    setLoadingMore(false)
    setState((current) => beginPageRead(current, '正在加载拍摄策划', true))
    listShootPlans({
      status: filter === 'all' ? undefined : filter,
      archived: filter === 'archived' ? true : false,
      page: 1,
      pageSize: planPageSize,
    }).then((result) => {
      if (!active) return
      setState(completePageRead({ items: result.items, total: result.total, page: 1 }, result.items.length === 0, filter === 'all' ? '还没有拍摄策划。把和客户聊过的记录粘进「从聊天整理」，一次生成镜头与准备项；也可以先空白建案，之后再补。' : '当前筛选下没有拍摄策划。'))
    }).catch((error: unknown) => {
      if (!active) return
      setState((current) => failPageRead(current, planningErrorMessage(error, '拍摄策划加载失败'), reload))
    })
    return () => { active = false }
  }, [filter, reloadTick, reload])

  async function loadMorePlans() {
    const loaded = readyPageData(state)
    if (!loaded) return
    const nextPage = loaded.page + 1
    const requestFilter = filter
    const requestSeq = loadMoreRequestSeq.current + 1
    loadMoreRequestSeq.current = requestSeq
    const requestStillCurrent = () => loadMoreRequestSeq.current === requestSeq && filterRef.current === requestFilter
    setLoadingMore(true)
    try {
      const result = await listShootPlans({
        status: requestFilter === 'all' ? undefined : requestFilter,
        archived: requestFilter === 'archived' ? true : false,
        page: nextPage,
        pageSize: planPageSize,
      })
      if (!requestStillCurrent()) return
      setState((current) => {
        const data = readyPageData(current)
        if (!data) return current
        return completePageRead({ items: [...data.items, ...result.items], total: result.total, page: nextPage }, false, '')
      })
    } catch (error) {
      if (!requestStillCurrent()) return
      // 下一页失败不清空已加载的策划：列表保持可用，错误条带重试。
      setState((current) => failPageRead(current, planningErrorMessage(error, '加载更多失败，仍显示已加载的策划'), () => { void loadMorePlans() }))
    } finally {
      if (requestStillCurrent()) setLoadingMore(false)
    }
  }

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
          <button className="btn btn-primary" type="button" disabled={creating !== null} onClick={() => void createBlank('ingestion')}>{creating === 'ingestion' ? '正在创建…' : '＋ 从聊天整理'}</button>
          <button className="btn" type="button" disabled={creating !== null} onClick={() => void createBlank('workspace')}>{creating === 'workspace' ? '正在创建…' : '空白建案'}</button>
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
        {list && <div className="result-meta">显示 {list.items.length} / {list.total} 份策划</div>}
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
        {list && list.items.length < list.total && (
          <div className="load-more">
            <button className="btn" type="button" disabled={loadingMore} onClick={() => { void loadMorePlans() }}>
              {loadingMore ? '加载中' : `加载更多（${list.items.length}/${list.total}）`}
            </button>
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
