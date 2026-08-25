import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  Check,
  ClipboardCheck,
  TrendingDown,
  TrendingUp,
  Wallet,
  X,
} from 'lucide-react'
import {
  ApiError,
  dismissReminder,
  fetchDashboardV2,
  getSettings,
  listOrders,
  listScheduleSlots,
  markReminderDone,
  updateOrder,
} from '../api/client'
import type {
  DashboardV2,
  DashboardV2Reminder,
  OrderListItem,
  ScheduleSlotListItem,
  Settings,
} from '../api/client'
import { useShell } from '../components/shellContext'
import StateNotice from '../components/StateNotice'
import EmptyState from '../components/EmptyState'
import { useFocusTrap } from '../components/useFocusTrap'
import {
  beginPageRead,
  completePageRead,
  failPageRead,
  pageReadPresentation,
  readyPageData,
  type PageReadState,
} from '../components/pageReadState'
import { groupOpeningsByDate, openingsText } from './calendar/openings'
import type { Opening } from './calendar/model'
import { localDayRange } from '../components/schedule/timezone'
import CopyTextSheet from './dashboard/CopyTextSheet'
import {
  channelLabels,
  computeUpcomingOpenings,
  formatShortYuan,
  formatYuan,
  healthCopyText,
  localDateOfInstant,
  minutesOfHHMM,
  minutesToHHMM,
  upcomingLocalDates,
  v2DeliveryRows,
  v2FocusModel,
  v2HealthCounts,
  v2HealthRows,
  v2HealthTierHint,
  v2HealthTierMeta,
  type V2HealthTierKey,
  v2MatrixModel,
  v2TimelineModel,
  v2WaterfallModel,
} from './dashboard/dashboardV2Model'
import './dashboard/dashboardV2.css'

const upcomingOpeningsDays = 14

const reminderTypeLabel: Record<DashboardV2Reminder['type'], string> = {
  birthday: '生日',
  follow_up: '回访',
  churn: '流失',
  custom: '自定义',
  plan_assignment_checklist: '认领项核对',
}

function formatToday(timezone: string | null): string {
  const options: Intl.DateTimeFormatOptions = {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    weekday: 'long',
  }
  if (timezone) options.timeZone = timezone
  try {
    return new Intl.DateTimeFormat('zh-CN', options).format(new Date())
  } catch {
    return new Intl.DateTimeFormat('zh-CN', {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
      weekday: 'long',
    }).format(new Date())
  }
}

function formatDateOnly(date: Date, timezone: string | null): string {
  return localDateOfInstant(date.toISOString(), timezone)
}

interface OpeningsAux {
  settings: Settings | null
  openings: Opening[]
  failed: boolean
}

const initialOpeningsAux: OpeningsAux = { settings: null, openings: [], failed: false }

export default function DashboardPage() {
  const navigate = useNavigate()
  const { notify, timezone } = useShell()
  const [readState, setReadState] = useState<PageReadState<DashboardV2>>({
    kind: 'loading',
    message: '正在加载仪表盘',
  })
  const [actionId, setActionId] = useState<string | null>(null)
  const [tick, setTick] = useState(0)
  const [openingsAux, setOpeningsAux] = useState<OpeningsAux>(initialOpeningsAux)
  const [receivableSheetOpen, setReceivableSheetOpen] = useState(false)
  const [receivableOrders, setReceivableOrders] = useState<OrderListItem[] | null>(null)
  const [copySheet, setCopySheet] = useState<{ sub: string; text: string } | null>(null)
  // 健康度分层默认聚焦高危层（挽回优先看价值），沿原型交互
  const [healthTier, setHealthTier] = useState<V2HealthTierKey>('at_risk')
  const auxLoadedRef = useRef(false)

  const load = useCallback(() => {
    setReadState((current) => beginPageRead(current, '正在加载仪表盘', true))
    fetchDashboardV2()
      .then((result) => setReadState(completePageRead(result, false, '')))
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 401) {
          setReadState({ kind: 'unauthorized' })
          navigate('/login', { replace: true })
          return
        }
        setReadState((current) => failPageRead(
          current,
          err instanceof Error ? err.message : '仪表盘加载失败',
          () => setTick((value) => value + 1),
        ))
      })
  }, [navigate])

  useEffect(() => {
    load()
  }, [load, tick])

  const reloadAux = useCallback(() => {
    auxLoadedRef.current = false
    setOpeningsAux(initialOpeningsAux)
  }, [])

  // 辅助读：settings（availability + 交付 SLA）与未来 14 天档期，用于空档速览与交付
  // 进度条；失败不阻塞主面板。与 Calendar 页同源算法（验收标准 3）。
  useEffect(() => {
    if (auxLoadedRef.current) return
    auxLoadedRef.current = true
    let active = true
    void (async () => {
      try {
        const settings = await getSettings()
        if (!active) return
        const tz = settings.timezone
        const fromDate = localDateOfInstant(new Date().toISOString(), tz)
        const dates = upcomingLocalDates(fromDate, upcomingOpeningsDays)
        const range = localDayRange(dates[dates.length - 1] ?? fromDate, tz)
        const slots = await listScheduleSlots(
          localDayRange(fromDate, tz).start,
          range.end,
        )
        if (!active) return
        const openings = computeUpcomingOpenings(slots, fromDate, upcomingOpeningsDays, tz, settings.availability)
        setOpeningsAux({ settings, openings, failed: false })
      } catch {
        if (active) setOpeningsAux({ settings: null, openings: [], failed: true })
      }
    })()
    return () => {
      active = false
    }
  }, [reloadAux, tick])

  const refetch = useCallback(() => {
    setTick((n) => n + 1)
    reloadAux()
  }, [reloadAux])

  const handleError = useCallback(
    (err: unknown, fallback: string) => {
      if (err instanceof ApiError && err.status === 401) {
        navigate('/login', { replace: true })
        return
      }
      notify(err instanceof Error ? err.message : fallback)
    },
    [navigate, notify],
  )

  async function actReminder(id: string, kind: 'done' | 'dismiss') {
    setActionId(id)
    try {
      if (kind === 'done') await markReminderDone(id)
      else await dismissReminder(id)
      notify(kind === 'done' ? '已标记完成' : '已忽略该提醒')
      refetch()
    } catch (err) {
      handleError(err, '操作失败')
    } finally {
      setActionId(null)
    }
  }

  async function settleOrder(id: string) {
    setActionId(id)
    try {
      await updateOrder(id, { balance_paid: true })
      notify('已标记尾款收讫')
      // 弹层保持打开时原地刷新明细，维持 v1「标记收讫 → 列表更新」行为
      setReceivableOrders(null)
      if (receivableSheetOpen) {
        loadReceivableOrders()
      }
      refetch()
    } catch (err) {
      handleError(err, '标记失败')
    } finally {
      setActionId(null)
    }
  }

  function loadReceivableOrders() {
    listOrders({ unpaidBalance: true, pageSize: 100 })
      .then((result) => setReceivableOrders(result.items))
      .catch((err: unknown) => {
        handleError(err, '待收明细加载失败')
        setReceivableSheetOpen(false)
      })
  }

  function openReceivableSheet() {
    setReceivableSheetOpen(true)
    setReceivableOrders(null)
    loadReceivableOrders()
  }

  const presentation = pageReadPresentation(readState)
  const data = readyPageData(readState)

  if (!presentation.showReadyData) {
    return (
      <main className="content content-wide">
        {presentation.notice && <StateNotice {...presentation.notice} />}
      </main>
    )
  }

  if (!data) return null

  const now = new Date()
  const todayDate = formatDateOnly(now, timezone)
  const focus = v2FocusModel(data.next_shoot, timezone, now)
  const timeline = v2TimelineModel(
    data.today_slots,
    data.today_openings.working_window,
    data.today_openings.openings,
    timezone,
    now,
  )
  const waterfall = v2WaterfallModel(data.revenue_waterfall)
  const matrix = v2MatrixModel(data.channel_matrix)
  const deliveryRows = v2DeliveryRows(
    data.delivery_queue.items,
    openingsAux.settings?.delivery_sla_days ?? 14,
    timezone,
    todayDate,
  )
  const due = data.due_reminders
  const slots = data.today_slots
  const todayShootCount = slots.filter(
    (slot) => slot.type === 'shoot' && slot.order_status !== 'cancelled',
  ).length
  const overdueCount = data.delivery_queue.items.filter((item) => item.overdue).length
  const utilization = data.schedule_utilization
  const health = data.customer_health
  const healthCounts = v2HealthCounts(health)
  const healthRows = v2HealthRows(health, healthTier, todayDate)
  const healthMeta = v2HealthTierMeta(healthTier)
  // 两套流失口径分离（§4.3）：高危层只标注、不判定——哪些客户已有 churn 唤回提醒
  const churnCustomerIds = new Set(
    due.filter((item) => item.type === 'churn' && item.customer_id).map((item) => item.customer_id),
  )
  const churnCrossCount = healthTier === 'at_risk'
    ? healthRows.filter((row) => churnCustomerIds.has(row.customerId)).length
    : 0

  return (
    <>
      <header className="topbar">
        <div>
          <h1>{todayShootCount > 0 ? `今天有 ${todayShootCount} 场拍摄` : '今天暂无拍摄安排'}</h1>
          <div className="sub">
            {formatToday(timezone)} · {due.length} 条待办需处理
          </div>
        </div>
        <div className="topbar-actions">
          <Link className="btn hide-mobile" to="/calendar">
            查看档期
          </Link>
          <Link className="btn btn-primary" to="/customers/new">
            ＋ 30 秒建档
          </Link>
        </div>
      </header>

      <main className="content content-wide">
        {presentation.notice && <StateNotice {...presentation.notice} />}

        <section className="dv2-focus-strip">
          <FocusCard focus={focus} todaySlots={slots} />
          <section className="card">
            <h2 className="card-title">
              今日时间轴
              <span className="count">
                · {todayShootCount} 场拍摄，{data.today_openings.openings.length} 段空档
              </span>
              <Link className="more" to="/calendar">
                打开日历 →
              </Link>
            </h2>
            <TimelineCard timeline={timeline} />
            <div className="dv2-tl-legend">
              <span><i className="dot" style={{ background: 'var(--slot-shoot)' }} />拍摄</span>
              <span><i className="dot" style={{ background: 'var(--slot-hold)' }} />预留</span>
              <span><i className="dot" style={{ background: 'var(--slot-busy)' }} />个人占用</span>
              <span><i className="dot" style={{ background: 'var(--accent2)' }} />当前时间</span>
            </div>
          </section>
        </section>

        <div className="two-col section-gap">
          <section className="card">
            <h2 className="card-title">
              今日待办 · 近 3 天 <span className="count">· {due.length}</span>
              <Link className="more" to="/reminders">
                全部提醒 →
              </Link>
            </h2>
            <div className="row-list">
              {due.length === 0 ? (
                <EmptyState icon={ClipboardCheck} title="待办已清空" hint="今天可以专心拍摄了" />
              ) : (
                due.map((reminder) => (
                  <ReminderRow
                    key={reminder.id}
                    reminder={reminder}
                    today={todayDate}
                    busy={actionId === reminder.id}
                    onDone={() => void actReminder(reminder.id, 'done')}
                    onDismiss={() => void actReminder(reminder.id, 'dismiss')}
                  />
                ))
              )}
            </div>
          </section>

          <section className="card">
            <h2 className="card-title">
              后期交付
              <span className="count">
                · {data.delivery_queue.count} 单未交付
                {openingsAux.settings
                  ? ` · 承诺拍摄后 ${openingsAux.settings.delivery_sla_days} 天交片`
                  : ''}
                {overdueCount > 0 ? ` · ${overdueCount} 单已逾期` : ''}
              </span>
              <Link className="more" to="/orders?status=shot">
                拍摄后订单 →
              </Link>
            </h2>
            <div className="row-list">
              {deliveryRows.length === 0 ? (
                <EmptyState icon={Check} title="没有在手后期" hint="拍完的订单会按应交付日排在这里" />
              ) : (
                deliveryRows.map((row) => (
                  <DeliveryRow key={row.orderId} row={row} />
                ))
              )}
            </div>
          </section>
        </div>

        <div className="two-col section-gap">
          <section className="card">
            <h2 className="card-title">收入构成<span className="count">· 已确认 / 待收 / 在途</span></h2>
            <div className="dv2-waterfall-head">
              <div className="dv2-waterfall-total">
                <span className="k">近 30 天已确认收入</span>
                <span className="v">{formatYuan(waterfall.confirmedCents)}</span>
              </div>
              {waterfall.trendLabel && (
                <span
                  className={
                    waterfall.trendUp === true
                      ? 'dv2-trend dv2-trend-up'
                      : waterfall.trendUp === false
                        ? 'dv2-trend dv2-trend-down'
                        : 'dv2-trend'
                  }
                >
                  {waterfall.trendUp === false ? <TrendingDown aria-hidden="true" strokeWidth={2.2} /> : waterfall.trendUp === true ? <TrendingUp aria-hidden="true" strokeWidth={2.2} /> : null}
                  {waterfall.trendLabel}
                </span>
              )}
            </div>
            <div className="dv2-wf-bar">
              {waterfall.segments
                .filter((segment) => segment.valueCents > 0)
                .map((segment) => (
                  <div
                    key={segment.key}
                    className={`dv2-wf-seg dv2-wf-${segment.key}`}
                    style={{ width: `${segment.percent}%` }}
                    title={`${segment.label} ${formatYuan(segment.valueCents)} · ${Math.round(segment.percent)}%`}
                  >
                    {segment.percent > 13 ? formatShortYuan(segment.valueCents) : ''}
                  </div>
                ))}
            </div>
            <div className="dv2-wf-legend">
              {waterfall.segments.map((segment) => (
                <button
                  key={segment.key}
                  type="button"
                  className="dv2-wf-leg-row"
                  onClick={segment.key === 'receivable' ? openReceivableSheet : undefined}
                  title={segment.key === 'receivable' ? '查看待收明细' : segment.note}
                >
                  <span className={`swatch dv2-wf-${segment.key}`} />
                  <span>
                    <span className="k">{segment.label}</span>
                    <span className="d"> · {segment.note}</span>
                  </span>
                  <span className="v">{formatYuan(segment.valueCents)}</span>
                </button>
              ))}
            </div>
            <div className="dv2-kpi-row">
              {waterfall.kpis.map((kpi) => (
                <div key={kpi.label} className="dv2-kpi">
                  <div className="k">{kpi.label}</div>
                  <div className="v">{kpi.value}</div>
                  <div className="d">{kpi.note}</div>
                </div>
              ))}
            </div>
          </section>

          <section className="card">
            <h2 className="card-title">档期利用率<span className="count">· {utilization.month}</span></h2>
            <div className="dv2-util-head">
              <span className="dv2-util-rate">
                {utilization.utilization == null ? '—' : `${utilization.utilization}%`}
              </span>
              <span className="dv2-util-sub">
                {utilization.shoot_count} 场拍摄 · {utilization.hold_days} 天仅预留 · {utilization.open_days} 天有空档 · {utilization.conflict_days} 天冲突
              </span>
            </div>
            <OpeningsPanel
              openings={openingsAux.openings}
              failed={openingsAux.failed}
              onCopy={(text) => setCopySheet({
                sub: '发给客户 / 朋友圈',
                text,
              })}
            />
          </section>
        </div>

        <div className="two-col section-gap">
          <section className="card">
            <h2 className="card-title">
              健康度分层
              <span className="count">· 共 {health.total} 位在册客户 · 按个人拍摄节奏分层</span>
              <Link className="more" to="/customers">
                全部客户 →
              </Link>
            </h2>
            <div className="dv2-cohort-bar">
              {healthCounts
                .filter((entry) => entry.count > 0)
                .map((entry) => {
                  const meta = v2HealthTierMeta(entry.key)
                  return (
                    <button
                      key={entry.key}
                      type="button"
                      className={`dv2-cohort-seg dv2-cohort-${entry.key}${entry.key === healthTier ? ' is-active' : ''}`}
                      style={{ width: `${entry.percent}%` }}
                      title={`${meta.label} · ${entry.count} 位 · ${v2HealthTierHint(entry.key, health.thresholds)}`}
                      onClick={() => setHealthTier(entry.key)}
                    >
                      <span className="n">{entry.count}</span>
                      <span className="l">{meta.label}</span>
                    </button>
                  )
                })}
            </div>
            {churnCrossCount > 0 && (
              <div className="dv2-cohort-crossref">
                其中 {churnCrossCount} 位已生成唤回提醒，见上方「今日待办」，处理完记得回来闭环。
              </div>
            )}
            <div className="dv2-cohort-title">
              {healthMeta.label}客户<span className="count">· {health.tiers[healthTier].count} 位 · {v2HealthTierHint(healthTier, health.thresholds)}</span>
            </div>
            {healthRows.length === 0 ? (
              <EmptyState
                icon={ClipboardCheck}
                title="这一层暂时没有客户"
                hint="点上方色块切换其他分层"
              />
            ) : (
              <div className="dv2-risk-list">
                {healthRows.map((row) => (
                  <div key={row.customerId} className="dv2-risk-row">
                    <div className="grow">
                      <div className="dv2-risk-name">
                        {row.displayName}
                        <span className="dv2-risk-badge">{row.channelLabel}</span>
                        {row.fallbackBaseline && (
                          <span
                            className="dv2-risk-badge"
                            title={`仅 1 次拍摄，无个人节奏样本，按 ${health.thresholds.fallback_cadence_days} 天通用基线判定`}
                          >
                            通用基线
                          </span>
                        )}
                      </div>
                      <div className="dv2-risk-meta">
                        {row.sinceDays != null
                          ? <>距上次拍摄 <b className="num">{row.sinceDays}</b> 天 · 个人节奏 <b className="num">{row.cadenceDays}</b> 天</>
                          : <>建档 <b className="num">{row.createdDaysAgo}</b> 天，尚无拍摄记录</>}
                      </div>
                      {row.unsettledText && (
                        <div className="dv2-risk-extra">另有未结清已收 {row.unsettledText}</div>
                      )}
                    </div>
                    <div className="dv2-risk-ltv">
                      <span className="v">{row.settledText}</span>
                      <span className="k">累计贡献</span>
                    </div>
                    <div className="dv2-risk-gauge">
                      <div className="dv2-risk-gauge-track">
                        <div
                          className="dv2-risk-gauge-fill"
                          style={{
                            width: `${row.gaugePercent ?? 0}%`,
                            background: `var(--${healthMeta.tone === 'muted' ? 'slot-busy' : healthMeta.tone})`,
                          }}
                        />
                      </div>
                      <div className="dv2-risk-gauge-label">{row.ratioText ?? '—'}</div>
                    </div>
                    <button
                      type="button"
                      className="btn btn-sm"
                      onClick={() =>
                        setCopySheet({
                          sub: `${healthMeta.label}客户触达文案`,
                          text: healthCopyText(healthMeta.ctaKind, row),
                        })
                      }
                    >
                      {healthMeta.ctaLabel}
                    </button>
                  </div>
                ))}
              </div>
            )}
            <details className="dv2-cohort-explain">
              <summary>判定口径与基线说明</summary>
              <div style={{ marginTop: 6 }}>
                判定口径：<code>距上次拍摄天数 ÷ 该客户历史平均拍摄间隔</code>。
                比值 ≤{health.thresholds.sleeping_ratio} 活跃、≤{health.thresholds.at_risk_ratio} 沉睡、≤
                {health.thresholds.lost_ratio} 高危、&gt;{health.thresholds.lost_ratio} 已流失。
                只拍过一次的客户没有间隔样本，回退到 {health.thresholds.fallback_cadence_days} 天通用基线并单独标注。
                高危 / 已流失层按累计贡献排序，优先挽回高价值客户。
                「已流失」是资产盘点口径，与提醒引擎的固定天数流失提醒相互独立（可在设置中调整两组参数）。
                累计贡献 = 已结清订单报价之和（与渠道矩阵同源）；另有未结清已收按实际收款补充展示。
              </div>
            </details>
          </section>

          <section className="card">
            <h2 className="card-title">渠道 × 类型 收入分布<span className="count">· 累计已结清</span></h2>
            {matrix.rows.length === 0 ? (
              <EmptyState icon={Wallet} title="暂无已结清收入" hint="订单结清后按下单时归因展示" />
            ) : (
              <>
                <div>
                  {matrix.rows.map((row) => (
                    <div key={row.channelLabel} className="dv2-mx-row">
                      <div className="dv2-mx-label">
                        {row.channelLabel}
                        <small>{row.customerCount} 位 · {row.orderCount} 单</small>
                      </div>
                      <div className="dv2-mx-track">
                        {row.segments
                          .filter((segment) => segment.cents > 0)
                          .map((segment) => (
                            <span
                              key={segment.type}
                              className={`dv2-mx-seg dv2-mx-${segment.type}`}
                              style={{ width: `${segment.percent}%` }}
                              title={`${segment.label} ${formatYuan(segment.cents)}`}
                            >
                              {segment.percent > 22 ? formatShortYuan(segment.cents) : ''}
                            </span>
                          ))}
                      </div>
                      <div className="dv2-mx-total">
                        {formatShortYuan(row.totalCents)}
                        <small>{row.totalPercent}%</small>
                      </div>
                    </div>
                  ))}
                </div>
                <div className="dv2-insight">{matrix.insight}</div>
              </>
            )}
          </section>
        </div>
      </main>

      {receivableSheetOpen && (
        <ReceivableSheet
          orders={receivableOrders}
          count={data.revenue_waterfall.receivable.count}
          totalCents={data.revenue_waterfall.receivable.total}
          timezone={timezone}
          today={todayDate}
          busyId={actionId}
          onSettle={(id) => { void settleOrder(id) }}
          onClose={() => {
            setReceivableSheetOpen(false)
            setReceivableOrders(null)
          }}
        />
      )}

      {copySheet && (
        <CopyTextSheet
          title="可约时间文案"
          sub={copySheet.sub}
          text={copySheet.text}
          onClose={() => setCopySheet(null)}
        />
      )}
    </>
  )
}

/* ================= L1 · 焦点卡 ================= */

function FocusCard({
  focus,
  todaySlots,
}: {
  focus: ReturnType<typeof v2FocusModel>
  todaySlots: ScheduleSlotListItem[]
}) {
  if (focus.state === 'none') {
    const rest = todaySlots.filter((slot) => slot.type !== 'shoot')
    return (
      <article className="dv2-focus-next">
        <div className="dv2-focus-empty">
          <div className="t">{todaySlots.length > 0 ? '今天没有拍摄安排' : '近期没有排拍摄'}</div>
          <div className="h">
            {rest.length > 0
              ? `今天还有 ${rest.length} 项非拍摄安排（预留 / 占用）`
              : '去日历里挑一天开新单，或把意向客户约起来'}
          </div>
          <Link className="btn btn-sm" to="/calendar">打开日历</Link>
        </div>
      </article>
    )
  }
  const customerHref = focus.slot && focus.slot.type === 'shoot' && focus.slot.customer_id
    ? `/customers/${focus.slot.customer_id}`
    : null
  const orderHref = focus.slot && focus.slot.type === 'shoot' && focus.slot.customer_id
    ? `/customers/${focus.slot.customer_id}?tab=orders&order=${focus.slot.order_id}`
    : null
  return (
    <article className="dv2-focus-next">
      <div className="dv2-focus-eyebrow">
        {focus.state === 'today' ? '下一场拍摄' : '下一场拍摄（未来）'}
        {focus.countdownLabel && <span className="dv2-focus-countdown">{focus.countdownLabel}</span>}
      </div>
      <div className="dv2-focus-when">
        {focus.state === 'today' ? (
          <>
            <span className="dv2-focus-time">{focus.startLabel}</span>
            <span className="dv2-focus-dur">
              — {focus.endLabel} · {Math.floor(focus.durationMinutes / 60)}h
              {focus.durationMinutes % 60 ? `${focus.durationMinutes % 60}m` : ''}
            </span>
          </>
        ) : (
          <span className="dv2-focus-time">{focus.whenLabel}</span>
        )}
      </div>
      <div className="dv2-focus-who">
        <div className="grow">
          <div className="name">{focus.customerName ?? focus.title ?? '档期安排'}</div>
          {focus.note && <div className="sub">{focus.note}</div>}
        </div>
      </div>
      <div className="dv2-focus-actions">
        {customerHref && <Link className="btn btn-sm" to={customerHref}>客户档案</Link>}
        {orderHref && <Link className="btn btn-sm btn-ghost" to={orderHref}>订单详情</Link>}
      </div>
    </article>
  )
}

/* ================= L1 · 时间轴 ================= */

function TimelineCard({ timeline }: { timeline: ReturnType<typeof v2TimelineModel> }) {
  const { dayStart, dayEnd, items, openings, nowMin } = timeline
  const span = Math.max(dayEnd - dayStart, 60)
  const height = Math.round((span / 60) * 22)
  const y = (minutes: number) => ((minutes - dayStart) / span) * height

  const rules: number[] = []
  for (let m = Math.ceil(dayStart / 120) * 120; m <= dayEnd; m += 120) rules.push(m)

  return (
    <div className="dv2-timeline" style={{ height: `${height}px` }} role="img" aria-label="今日时间轴">
      {rules.map((m) => (
        <div key={m} className="dv2-tl-rule" style={{ top: `${y(m)}px` }} />
      ))}
      {rules.map((m) => (
        <div key={m} className="dv2-tl-hour" style={{ top: `${y(m)}px` }}>{minutesToHHMM(m)}</div>
      ))}
      {openings.map((opening, index) => {
        const start = minutesOfHHMM(opening.start)
        const end = minutesOfHHMM(opening.end)
        const top = y(start)
        return (
          <div
            key={`${opening.start}-${index}`}
            className="dv2-tl-gap"
            style={{ top: `${top}px`, height: `${Math.max(22, y(end) - top - 3)}px` }}
          >
            空档 {opening.start}–{opening.end}
          </div>
        )
      })}
      {items.map((item) => {
        const top = y(item.startMin)
        return (
          <div
            key={item.id}
            className={`dv2-tl-item dv2-tl-${item.kind}`}
            style={{ top: `${top}px`, height: `${Math.max(26, y(item.endMin) - top - 3)}px` }}
            title={`${item.title}${item.statusLabel ? ` · ${item.statusLabel}` : ''}`}
          >
            <span className="t">{item.title}</span>
            <span className="m">{minutesToHHMM(item.startMin)}–{minutesToHHMM(item.endMin)}</span>
          </div>
        )
      })}
      {nowMin != null && (
        <div className="dv2-tl-now" style={{ top: `${y(nowMin)}px` }}>
          <span className="dv2-tl-now-label">现在 {minutesToHHMM(nowMin)}</span>
        </div>
      )}
    </div>
  )
}

/* ================= L1 · 待办 ================= */

function ReminderRow({
  reminder,
  today,
  busy,
  onDone,
  onDismiss,
}: {
  reminder: DashboardV2Reminder
  today: string
  busy: boolean
  onDone: () => void
  onDismiss: () => void
}) {
  const overdue = reminder.due_date < today
  const detailLink = reminder.plan_id
    ? `/shoot-plans/${reminder.plan_id}?tab=readiness`
    : reminder.customer_id
      ? `/customers/${reminder.customer_id}`
      : null
  return (
    <div className="row-item">
      <span className="badge badge-muted">{reminderTypeLabel[reminder.type]}</span>
      <div className="grow">
        <div className="title">{reminder.content}</div>
        <div className="meta">
          {detailLink ? (
            <Link to={detailLink}>{reminder.plan_id ? '打开策划准备项' : '查看客户'}</Link>
          ) : (
            '未关联客户'
          )}
          {reminder.customer_summary && ` · ${channelLabels[reminder.customer_summary.channel]}来源`}
          {' · '}
          <span className="num">{reminder.due_date.slice(5).replace('-', '/')}</span>
          {overdue && <span className="danger-text"> · 已逾期</span>}
        </div>
      </div>
      <button className="btn btn-sm" type="button" disabled={busy} onClick={onDone}>
        <Check aria-hidden="true" strokeWidth={2} />
        完成
      </button>
      <button className="btn btn-sm btn-ghost" type="button" disabled={busy} onClick={onDismiss}>
        <X aria-hidden="true" strokeWidth={2} />
        忽略
      </button>
    </div>
  )
}

/* ================= L1.5 · 交付队列 ================= */

function DeliveryRow({ row }: { row: ReturnType<typeof v2DeliveryRows>[number] }) {
  const orderHref = row.customerId
    ? `/customers/${row.customerId}?tab=orders&order=${row.orderId}`
    : null
  return (
    <div className="dv2-dl-row">
      <div className="grow">
        <div className="dv2-dl-title">
          {orderHref ? <Link to={orderHref}>{row.title}</Link> : row.title}
          <span className={`badge ${row.overdue ? 'badge-danger' : 'badge-muted'}`}>{row.stageLabel}</span>
        </div>
        <div className="dv2-dl-meta">
          {row.customerName}
          {row.daysSinceShot != null && <> · 拍摄已过 <b className="num">{row.daysSinceShot}</b> 天</>}
          {' · '}
          {row.overdue ? (
            <span className="dv2-dl-overdue">{row.dueLabel}</span>
          ) : (
            <>
              {row.dueLabel}
              {row.dueDate && `（应 ${row.dueDate.slice(5).replace('-', '/')} 交付）`}
            </>
          )}
        </div>
      </div>
      {row.progressPct != null && (
        <div className="dv2-gauge">
          <div className="dv2-gauge-track">
            <div
              className="dv2-gauge-fill"
              style={{
                width: `${row.progressPct}%`,
                background: row.overdue
                  ? 'var(--danger)'
                  : row.progressPct > 70
                    ? 'var(--warning)'
                    : 'var(--accent)',
              }}
            />
          </div>
          <div className="dv2-gauge-label">周期 {row.progressPct}%</div>
        </div>
      )}
    </div>
  )
}

/* ================= L2 · 空档速览 ================= */

function OpeningsPanel({
  openings,
  failed,
  onCopy,
}: {
  openings: Opening[]
  failed: boolean
  onCopy(text: string): void
}) {
  if (failed) {
    return <div className="dv2-open-summary">未来空档暂不可用，可稍后在日历页查看。</div>
  }
  const days = groupOpeningsByDate(openings, upcomingOpeningsDays)
  if (days.length === 0) {
    return <div className="dv2-open-summary">未来 {upcomingOpeningsDays} 天暂无可约空档，新客建议约更晚档期。</div>
  }
  const chips = days.slice(0, 4).map((day) => ({
    key: day.date,
    label: `${day.date.slice(5).replace('-', '/')} · ${day.openings.map((opening) => `${opening.start}–${opening.end}`).join('、')}`,
  }))
  return (
    <div className="dv2-util-openings">
      <div className="dv2-open-summary">
        未来 {upcomingOpeningsDays} 天 <b>{days.length}</b> 天有可约空档 · 最近 <b>{days[0].date.slice(5).replace('-', '/')}</b>
      </div>
      <div className="dv2-open-chips">
        {chips.map((chip) => (
          <span key={chip.key} className="dv2-open-chip">{chip.label}</span>
        ))}
        <button className="btn btn-sm" type="button" onClick={() => onCopy(openingsText(openings, 5))}>
          复制可约时间
        </button>
      </div>
    </div>
  )
}

/* ================= 待收明细弹层 ================= */

function ReceivableSheet({
  orders,
  count,
  totalCents,
  timezone,
  today,
  busyId,
  onSettle,
  onClose,
}: {
  orders: OrderListItem[] | null
  count: number
  totalCents: number
  timezone: string | null
  today: string
  busyId: string | null
  onSettle(id: string): void
  onClose(): void
}) {
  const dialogRef = useFocusTrap<HTMLElement>(true, onClose, true)
  return (
    <div className="overlay open" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <section ref={dialogRef} className="dialog" role="dialog" aria-modal="true" aria-labelledby="dv2ReceivableTitle" tabIndex={-1} autoFocus>
        <h2 id="dv2ReceivableTitle">待收尾款明细</h2>
        <p className="dialog-sub">
          {count} 笔已交付未结清 · 合计 {formatYuan(totalCents)}（按 outstanding_amount，非报价推算）
        </p>
        {orders == null ? (
          <StateNotice kind="loading" message="正在加载待收明细" />
        ) : (
          <div className="dv2-sheet-list">
            {orders.map((order) => {
              const delivered = order.delivered_at ? localDateOfInstant(order.delivered_at, timezone) : null
              const ageDays = delivered
                ? Math.round((Date.parse(`${today}T12:00:00Z`) - Date.parse(`${delivered}T12:00:00Z`)) / 86400000)
                : null
              return (
                <div key={order.id} className="dv2-sheet-item">
                  <div className="grow">
                    <div className="t">{order.title ?? order.package_name ?? '未命名订单'}</div>
                    <div className="m">
                      {order.customer_display_name}
                      {delivered && ` · 交付 ${delivered.slice(5).replace('-', '/')}`}
                      {ageDays != null && ageDays > 0 && ` · 已过 ${ageDays} 天`}
                    </div>
                  </div>
                  <span className="v">
                    {order.outstanding_amount == null ? '—' : formatYuan(order.outstanding_amount)}
                  </span>
                  <button
                    className="btn btn-sm"
                    type="button"
                    disabled={busyId === order.id}
                    onClick={() => onSettle(order.id)}
                  >
                    标记收讫
                  </button>
                </div>
              )
            })}
            {orders.length === 0 && <div className="m">没有待收明细。</div>}
            {orders.length >= 100 && <div className="m">仅显示前 100 笔，完整列表见订单页「未收尾款」筛选。</div>}
          </div>
        )}
        <div className="dialog-actions">
          <button className="btn" type="button" onClick={onClose}>关闭</button>
        </div>
      </section>
    </div>
  )
}
