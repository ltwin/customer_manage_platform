import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  ApiError,
  dismissReminder,
  fetchDashboard,
  markReminderDone,
  updateOrder,
} from '../api/client'
import type {
  Dashboard,
  DashboardReminder,
  DashboardSlot,
  DashboardUnpaidOrder,
} from '../api/client'
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

const reminderTypeLabel: Record<DashboardReminder['type'], string> = {
  birthday: '生日',
  follow_up: '回访',
  churn: '流失',
  custom: '自定义',
}

const orderStatusLabel: Record<string, string> = {
  consulting: '咨询',
  scheduled: '待拍',
  shot: '已拍',
  selected: '已选',
  retouching: '精修',
  delivered: '已交付',
  closed: '已完成',
  cancelled: '已取消',
}

function formatPrice(cents: number | null | undefined): string {
  if (cents == null) return '—'
  return `¥${(cents / 100).toLocaleString('zh-CN', { maximumFractionDigits: 0 })}`
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

function formatSlotTime(iso: string, timezone: string | null): string {
  const options: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit', hour12: false }
  if (timezone) options.timeZone = timezone
  try {
    return new Intl.DateTimeFormat('zh-CN', options).format(new Date(iso))
  } catch {
    return new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false }).format(
      new Date(iso),
    )
  }
}

function slotTitle(slot: DashboardSlot): string {
  if (slot.type === 'shoot') {
    return `${slot.customer_display_name} · ${slot.order_title ?? slot.package_name ?? '未命名订单'}`
  }
  return slot.note ?? (slot.type === 'hold' ? '预留' : '个人占用')
}

export default function DashboardPage() {
  const navigate = useNavigate()
  const { notify, timezone } = useShell()
  const [readState, setReadState] = useState<PageReadState<Dashboard>>({
    kind: 'loading',
    message: '正在加载仪表盘',
  })
  const [actionId, setActionId] = useState<string | null>(null)
  const [tick, setTick] = useState(0)

  const load = useCallback(() => {
    setReadState((current) => beginPageRead(current, '正在加载仪表盘', true))
    fetchDashboard()
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

  const refetch = useCallback(() => setTick((n) => n + 1), [])

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
      refetch()
    } catch (err) {
      handleError(err, '标记失败')
    } finally {
      setActionId(null)
    }
  }

  const presentation = pageReadPresentation(readState)
  const data = readyPageData(readState)

  if (!presentation.showReadyData) {
    return (
      <main className="content">
        {presentation.notice && <StateNotice {...presentation.notice} />}
      </main>
    )
  }

  if (!data) return null

  const due = data.due_reminders
  const slots = data.today_slots
  const unpaid = data.unpaid_orders
  const churn = data.churn_alerts
  const stats = data.recent_stats

  return (
    <>
      <header className="topbar">
        <div>
          <h1>{slots.length > 0 ? `今天有 ${slots.length} 个安排` : '今天暂无档期安排'}</h1>
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

      <main className="content">
        {presentation.notice && <StateNotice {...presentation.notice} />}
        <div className="stat-grid">
          <StatCard tone="danger" label="待办提醒" value={String(due.length)} delta="近 3 天 · 含逾期提醒" />
          <StatCard tone="accent" label="今日档期" value={String(slots.length)} delta="拍摄 / 预留 / 占用集中查看" />
          <StatCard tone="warning" label="待收尾款" value={String(unpaid.count)} delta={`${unpaid.count} 笔 · 已交付未结清`} />
          <StatCard tone="success" label="近 30 天确认收入" value={formatPrice(stats.revenue_confirmed)} delta={`交付 ${stats.orders_delivered} · 新建 ${stats.orders_created}`} />
        </div>

        <div className="two-col section-gap">
          <section className="card">
            <h2 className="card-title">
              待办提醒 · 近 3 天 <span className="count">· {due.length}</span>
              <Link className="more" to="/reminders">
                全部提醒 →
              </Link>
            </h2>
            <div className="row-list">
              {due.length === 0 ? (
                <div className="empty">待办已清空，今天可以专心拍摄了</div>
              ) : (
                due.map((reminder) => (
                  <ReminderRow
                    key={reminder.id}
                    reminder={reminder}
                    timezone={timezone}
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
              今日档期 <span className="count">· {slots.length}</span>
              <Link className="more" to="/calendar">
                打开日历 →
              </Link>
            </h2>
            <div className="row-list">
              {slots.length === 0 ? (
                <div className="empty">今天没有档期</div>
              ) : (
                slots.map((slot) => <SlotRow key={slot.id} slot={slot} timezone={timezone} />)
              )}
            </div>
          </section>
        </div>

        <div className="two-col">
          <section className="card">
            <h2 className="card-title">
              待收尾款订单 <span className="count">· {unpaid.count}</span>
            </h2>
            <div className="row-list">
              {unpaid.items.length === 0 ? (
                <div className="empty">没有待收尾款，账都收齐了</div>
              ) : (
                unpaid.items.map((order) => (
                  <UnpaidRow
                    key={order.id}
                    order={order}
                    timezone={timezone}
                    busy={actionId === order.id}
                    onSettle={() => void settleOrder(order.id)}
                  />
                ))
              )}
            </div>
          </section>

          <section className="card">
            <h2 className="card-title">
              流失预警 <span className="count">· {churn.length}</span>
            </h2>
            <div className="row-list">
              {churn.length === 0 ? (
                <div className="empty">暂无流失预警</div>
              ) : (
                churn.map((reminder) => (
                  <div className="row-item" key={reminder.id}>
                    <div className="grow">
                      <div className="title">{reminder.content}</div>
                      <div className="meta">
                        {reminder.customer_id ? (
                          <Link to={`/customers/${reminder.customer_id}`}>查看档案</Link>
                        ) : (
                          '未关联客户'
                        )}
                      </div>
                    </div>
                  </div>
                ))
              )}
            </div>
          </section>
        </div>
      </main>
    </>
  )
}

function ReminderRow({
  reminder,
  timezone,
  busy,
  onDone,
  onDismiss,
}: {
  reminder: DashboardReminder
  timezone: string | null
  busy: boolean
  onDone: () => void
  onDismiss: () => void
}) {
  const today = formatDateOnly(new Date(), timezone)
  const overdue = reminder.due_date < today
  return (
    <div className="row-item">
      <span className="badge badge-muted">{reminderTypeLabel[reminder.type]}</span>
      <div className="grow">
        <div className="title">{reminder.content}</div>
        <div className="meta">
          {reminder.customer_id ? (
            <Link to={`/customers/${reminder.customer_id}`}>查看客户</Link>
          ) : (
            '未关联客户'
          )}
          {' · '}
          <span className="num">{reminder.due_date.slice(5).replace('-', '/')}</span>
          {overdue && <span className="danger-text"> · 已逾期</span>}
        </div>
      </div>
      <button className="btn btn-sm" type="button" disabled={busy} onClick={onDone}>
        完成
      </button>
      <button className="btn btn-sm btn-ghost" type="button" disabled={busy} onClick={onDismiss}>
        忽略
      </button>
    </div>
  )
}

function SlotRow({ slot, timezone }: { slot: DashboardSlot; timezone: string | null }) {
  const to =
    slot.type === 'shoot' && slot.customer_id
      ? `/customers/${slot.customer_id}?tab=orders&order=${slot.order_id}`
      : '/calendar'
  return (
    <Link className="row-item row-item-link" to={to}>
      <span className={`dot slot-${slot.type}`} />
      <div className="grow">
        <div className="title">{slotTitle(slot)}</div>
        <div className="meta">
          {slot.type === 'shoot' ? orderStatusLabel[slot.order_status] ?? slot.order_status : '档期'}
        </div>
      </div>
      <span className="num muted-text">
        {formatSlotTime(slot.start_at, timezone)}–{formatSlotTime(slot.end_at, timezone)}
      </span>
    </Link>
  )
}

function UnpaidRow({
  order,
  timezone,
  busy,
  onSettle,
}: {
  order: DashboardUnpaidOrder
  timezone: string | null
  busy: boolean
  onSettle: () => void
}) {
  return (
    <div className="row-item">
      <div className="grow">
        <div className="title">{order.title ?? order.customer_display_name}</div>
        <div className="meta">
          {order.customer_id ? (
            <Link to={`/customers/${order.customer_id}?tab=orders&order=${order.id}`}>
              {order.customer_display_name}
            </Link>
          ) : (
            order.customer_display_name
          )}
          {order.delivered_at && ` · 已交付 ${formatDateOnly(new Date(order.delivered_at), timezone).slice(5)}`}
        </div>
      </div>
      <span className="num muted-text">报价 {formatPrice(order.price)}</span>
      <button className="btn btn-sm" type="button" disabled={busy} onClick={onSettle}>
        标记收讫
      </button>
    </div>
  )
}

function formatDateOnly(date: Date, timezone: string | null): string {
  const options: Intl.DateTimeFormatOptions = { year: 'numeric', month: '2-digit', day: '2-digit' }
  if (timezone) options.timeZone = timezone
  try {
    // en-CA 产出 YYYY-MM-DD，便于与后端 due_date 字符串比较
    return new Intl.DateTimeFormat('en-CA', options).format(date)
  } catch {
    return new Intl.DateTimeFormat('en-CA', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).format(date)
  }
}

function StatCard({
  tone,
  label,
  value,
  delta,
}: {
  tone: 'accent' | 'success' | 'warning' | 'danger'
  label: string
  value: string
  delta: string
}) {
  return (
    <div className="stat">
      <div className="label">
        <span className={`dot tone-${tone}`} />
        {label}
      </div>
      <div className="value">{value}</div>
      <div className="delta">{delta}</div>
    </div>
  )
}
