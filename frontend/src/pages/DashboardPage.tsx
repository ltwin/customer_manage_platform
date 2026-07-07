import { Link } from 'react-router-dom'
import { DICT, TODAY } from '../crm/prototypeData'
import { byId, formatPrice, reminderBadgeClass } from '../crm/model'
import { usePrototypeStore } from '../crm/prototypeStoreContext'
import { useShell } from '../components/shellContext'

export default function DashboardPage() {
  const store = usePrototypeStore()
  const { notify } = useShell()
  const dueReminders = store.reminders.filter((item) => item.status === 'pending' && item.type !== 'churn' && item.due_date <= '2026-07-08')
  const todaySlots = store.slots.filter((slot) => slot.date === TODAY)
  const unpaidOrders = store.orders.filter((order) => order.status === 'delivered' && !order.balance_paid)
  const churnAlerts = store.reminders.filter((item) => item.status === 'pending' && item.type === 'churn')
  const revenue = store.orders
    .filter((order) => order.delivered_at && order.delivered_at >= '2026-06-07' && order.balance_paid && order.status !== 'cancelled')
    .reduce((sum, order) => sum + (order.price ?? 0), 0)

  return (
    <>
      <header className="topbar">
        <div>
          <h1>早上好，今天有 {todaySlots.length} 个安排</h1>
          <div className="sub">2026 年 7 月 6 日 周一 · {dueReminders.length} 条待办需处理</div>
        </div>
        <div className="topbar-actions">
          <Link className="btn hide-mobile" to="/calendar">查看档期</Link>
          <Link className="btn btn-primary" to="/customers/new">＋ 30 秒建档</Link>
        </div>
      </header>

      <main className="content">
        <div className="stat-grid">
          <StatCard tone="danger" label="待办提醒" value={dueReminders.length.toString()} delta="近 3 天 · 含逾期提醒" />
          <StatCard tone="accent" label="今日档期" value={todaySlots.length.toString()} delta="拍摄 / 预留 / 占用集中查看" />
          <StatCard tone="warning" label="待收尾款" value={formatPrice(unpaidOrders.reduce((sum, order) => sum + Math.round((order.price ?? 0) / 2), 0))} delta={`${unpaidOrders.length} 笔 · 已交付未结清`} />
          <StatCard tone="success" label="近 30 天确认收入" value={formatPrice(revenue)} delta="按交付与结清口径统计" />
        </div>

        <div className="two-col section-gap">
          <section className="card">
            <h2 className="card-title">待办提醒 · 近 3 天 <span className="count">· {dueReminders.length}</span>
              <Link className="more" to="/customers">全部客户 →</Link>
            </h2>
            <div className="row-list">
              {dueReminders.length === 0 ? <div className="empty">待办已清空，今天可以专心拍摄了</div> : dueReminders.map((reminder) => {
                const customer = byId(store.customers, reminder.customer_id)
                const overdue = reminder.due_date < TODAY
                return (
                  <div className="row-item" key={reminder.id}>
                    <span className={`badge ${reminderBadgeClass(reminder)}`}>{DICT.reminderType[reminder.type]}</span>
                    <div className="grow">
                      <div className="title">{reminder.content}</div>
                      <div className="meta">
                        {customer ? <Link to={`/customers/${customer.id}`}>{customer.display_name}</Link> : '未关联客户'}
                        {' · '}
                        <span className="num">{reminder.due_date.slice(5).replace('-', '/')}</span>
                        {overdue && <span className="danger-text"> · 已逾期</span>}
                      </div>
                    </div>
                    <button className="btn btn-sm" type="button" onClick={() => { store.markReminderDone(reminder.id); notify('已标记完成') }}>完成</button>
                    <button className="btn btn-sm btn-ghost" type="button" onClick={() => { store.dismissReminder(reminder.id); notify('已忽略该提醒') }}>忽略</button>
                  </div>
                )
              })}
            </div>
          </section>

          <section className="card">
            <h2 className="card-title">今日档期 <Link className="more" to="/calendar">打开日历 →</Link></h2>
            <div className="row-list">
              {todaySlots.map((slot) => {
                const order = byId(store.orders, slot.order_id)
                const customer = order ? byId(store.customers, order.customer_id) : null
                const pkg = order ? byId(store.packages, order.package_id) : null
                return (
                  <div className="row-item" key={slot.id}>
                    <span className={`dot slot-${slot.type}`} />
                    <div className="grow">
                      <div className="title">{customer && pkg ? `${customer.display_name} · ${pkg.name}` : slot.note}</div>
                      <div className="meta">{customer ? slot.note : DICT.slotType[slot.type]}</div>
                    </div>
                    <span className="num muted-text">{slot.start}–{slot.end}</span>
                  </div>
                )
              })}
            </div>
          </section>
        </div>

        <div className="two-col">
          <section className="card">
            <h2 className="card-title">待收尾款订单 <span className="count">· {unpaidOrders.length}</span></h2>
            <div className="row-list">
              {unpaidOrders.length === 0 ? <div className="empty">没有待收尾款，账都收齐了</div> : unpaidOrders.map((order) => {
                const customer = byId(store.customers, order.customer_id)
                const paid = order.deposit_paid ? Math.round((order.price ?? 0) / 2) : 0
                return (
                  <div className="row-item" key={order.id}>
                    <div className={`avatar avatar-sm ${customer?.avatar ?? ''}`}>{customer?.display_name[0] ?? '?'}</div>
                    <div className="grow">
                      <div className="title">{order.title}</div>
                      <div className="meta">{customer?.display_name ?? '未知客户'} · 已交付 {order.delivered_at?.slice(5).replace('-', '/')} · 定金已收</div>
                    </div>
                    <span className="num warning-text">{formatPrice((order.price ?? 0) - paid)}</span>
                    <button className="btn btn-sm" type="button" onClick={() => { store.settleOrder(order.id); notify('已标记尾款收讫') }}>标记收讫</button>
                  </div>
                )
              })}
            </div>
          </section>

          <section className="card">
            <h2 className="card-title">流失预警 <span className="count">· {churnAlerts.length}</span></h2>
            <div className="row-list">
              {churnAlerts.map((reminder) => {
                const customer = byId(store.customers, reminder.customer_id)
                return (
                  <div className="row-item" key={reminder.id}>
                    <div className={`avatar avatar-sm ${customer?.avatar ?? ''}`}>{customer?.display_name[0] ?? '?'}</div>
                    <div className="grow">
                      <div className="title">{customer?.display_name ?? '未知客户'} · 超 180 天未拍</div>
                      <div className="meta">{reminder.content}</div>
                    </div>
                    {customer && <Link className="btn btn-sm" to={`/customers/${customer.id}`}>查看档案</Link>}
                  </div>
                )
              })}
            </div>
          </section>
        </div>
      </main>
    </>
  )
}

function StatCard({ tone, label, value, delta }: { tone: 'accent' | 'success' | 'warning' | 'danger'; label: string; value: string; delta: string }) {
  return (
    <div className="stat">
      <div className="label"><span className={`dot tone-${tone}`} />{label}</div>
      <div className="value">{value}</div>
      <div className="delta">{delta}</div>
    </div>
  )
}
