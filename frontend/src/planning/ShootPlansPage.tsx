import { useLegacyReadOnly } from '../creative/useLegacyReadOnly'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
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
  type ShootPlanListSort,
  type ShootPlanStatus,
} from './api'
import StatusBadge from './StatusBadge'
import { crmSummaryLine, listStatusLines, windowRangeLabel } from './listCard'
import { planningErrorMessage, shootPlanStatusLabel } from './presentation'
import './planning.css'

const planPageSize = 50

const sortOptions: ReadonlyArray<{ value: ShootPlanListSort; label: string }> = [
  { value: 'updated_at_desc', label: '最近更新' },
  { value: 'updated_at_asc', label: '最久未更新' },
  { value: 'created_at_desc', label: '最新创建' },
]

// 累加式翻页：已加载的页留在列表里，page 记住下一页从哪续。
type PlanListPage = { items: ShootPlanListItem[]; total: number; page: number }

// 查询条件是一个整体：搜索、排序、状态任一变化都要重开列表，
// 所以翻页的「是否还是同一份查询」只比这一个对象。
type PlanListQuery = { q: string; sort: ShootPlanListSort; filter: 'all' | ShootPlanStatus }

function listRequest(query: PlanListQuery, page: number) {
  return {
    status: query.filter === 'all' ? undefined : query.filter,
    archived: query.filter === 'archived',
    q: query.q || undefined,
    sort: query.sort,
    page,
    pageSize: planPageSize,
  }
}

function emptyListMessage(query: PlanListQuery): string {
  if (query.q) return `没有匹配「${query.q}」的拍摄策划。换个关键词，或清空搜索看全部。`
  if (query.filter !== 'all') return '当前筛选下没有拍摄策划。'
  return '还没有拍摄策划。把和客户聊过的记录粘进「从聊天整理」，一次生成镜头与准备项；也可以先空白建案，之后再补。'
}

export default function ShootPlansPage() {
 const legacyReadOnly = useLegacyReadOnly() !== 'write'
  const navigate = useNavigate()
  const { notify } = useShell()
  const [filter, setFilter] = useState<'all' | ShootPlanStatus>('all')
  const [keyword, setKeyword] = useState('')
  const [sort, setSort] = useState<ShootPlanListSort>('updated_at_desc')
  const [reloadTick, setReloadTick] = useState(0)
  const [state, setState] = useState<PageReadState<PlanListPage>>({ kind: 'loading', message: '正在加载拍摄策划' })
  const [creating, setCreating] = useState<'ingestion' | 'workspace' | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const query = useMemo<PlanListQuery>(() => ({ q: keyword.trim(), sort, filter }), [filter, keyword, sort])
  const queryRef = useRef(query)
  const loadMoreRequestSeq = useRef(0)

  useEffect(() => { queryRef.current = query }, [query])

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
    // 换查询条件等于重开一份列表：在途的「加载更多」作废，避免旧条件的下一页追进新结果。
    loadMoreRequestSeq.current += 1
    setLoadingMore(false)
    setState((current) => beginPageRead(current, '正在加载拍摄策划', true))
    listShootPlans(listRequest(query, 1)).then((result) => {
      if (!active) return
      setState(completePageRead({ items: result.items, total: result.total, page: 1 }, result.items.length === 0, emptyListMessage(query)))
    }).catch((error: unknown) => {
      if (!active) return
      setState((current) => failPageRead(current, planningErrorMessage(error, '拍摄策划加载失败'), reload))
    })
    return () => { active = false }
  }, [query, reloadTick, reload])

  async function loadMorePlans() {
    const loaded = readyPageData(state)
    if (!loaded) return
    const nextPage = loaded.page + 1
    const requestQuery = query
    const requestSeq = loadMoreRequestSeq.current + 1
    loadMoreRequestSeq.current = requestSeq
    const requestStillCurrent = () => loadMoreRequestSeq.current === requestSeq && queryRef.current === requestQuery
    setLoadingMore(true)
    try {
      const result = await listShootPlans(listRequest(requestQuery, nextPage))
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
          <h1>{legacyReadOnly ? '旧策划记录' : '拍摄策划'}</h1>
        </div>
        <div className="topbar-actions">
 {legacyReadOnly ? <span>旧记录只读，请在创意空间开始新的拍摄。</span> : <>
          <button className="btn btn-primary" type="button" disabled={creating !== null} onClick={() => void createBlank('ingestion')}>{creating === 'ingestion' ? '正在创建…' : '＋ 从聊天整理'}</button>
          <button className="btn" type="button" disabled={creating !== null} onClick={() => void createBlank('workspace')}>{creating === 'workspace' ? '正在创建…' : '空白建案'}</button>
        </>}
 </div>
      </header>
      <main className="content planning-content">
        <section className="planning-ledger-head">
          <div>
            <p className="planning-eyebrow">策划台账</p>
            <h2>全部拍摄策划</h2>
            <p className="muted-text">策划是独立的创作工具，不关联客户或订单也能正常使用。</p>
          </div>
          <div className="planning-filters">
            <label className="planning-filter">
              <span>搜索</span>
              <input className="input" type="search" value={keyword} maxLength={120} placeholder="标题或拍摄主体" onChange={(event) => setKeyword(event.target.value)} />
            </label>
            <label className="planning-filter">
              <span>状态筛选</span>
              <select className="input" value={filter} onChange={(event) => setFilter(event.target.value as 'all' | ShootPlanStatus)}>
                <option value="all">全部状态</option>
                {(['draft', 'ready', 'in_progress', 'completed', 'archived'] as const).map((status) => (
                  <option key={status} value={status}>{shootPlanStatusLabel(status)}</option>
                ))}
              </select>
            </label>
            <label className="planning-filter">
              <span>排序</span>
              <select className="input" value={sort} onChange={(event) => setSort(event.target.value as ShootPlanListSort)}>
                {sortOptions.map((option) => (
                  <option key={option.value} value={option.value}>{option.label}</option>
                ))}
              </select>
            </label>
          </div>
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
