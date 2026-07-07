import { useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { DICT } from '../crm/prototypeData'
import { byId, customerAge, customerOrders, formatPrice, orderStatusBadgeClass, shortDate, visibleBirthday } from '../crm/model'
import { usePrototypeStore } from '../crm/prototypeStoreContext'
import { useShell } from '../components/shellContext'

const flow = ['consulting', 'scheduled', 'shot', 'selected', 'retouching', 'delivered', 'closed'] as const

export default function CustomerDetailPage() {
  const { id = '' } = useParams()
  const store = usePrototypeStore()
  const { notify } = useShell()
  const [tab, setTab] = useState<'orders' | 'notes' | 'reminders'>('orders')
  const [note, setNote] = useState('')
  const customer = byId(store.customers, id)

  const orders = useMemo(() => customer ? customerOrders(store.orders, customer.id).sort((a, b) => b.created_at.localeCompare(a.created_at)) : [], [customer, store.orders])
  const reminders = useMemo(() => customer ? store.reminders.filter((reminder) => reminder.customer_id === customer.id) : [], [customer, store.reminders])

  if (!customer) {
    return (
      <>
        <header className="topbar">
          <div>
            <div className="crumb"><Link to="/customers">客户</Link> / 档案</div>
            <h1>客户不存在</h1>
          </div>
        </header>
        <main className="content"><div className="empty">没有找到这份客户档案</div></main>
      </>
    )
  }

  const totalAmount = orders.filter((order) => order.status !== 'cancelled').reduce((sum, order) => sum + (order.price ?? 0), 0)
  const lastShot = orders.map((order) => order.shot_at).filter((date): date is string => Boolean(date)).sort().at(-1)
  const directReferrals = store.customers.filter((item) => item.referrer_customer_id === customer.id)
  const secondReferrals = store.customers.filter((item) => directReferrals.some((ref) => ref.id === item.referrer_customer_id))

  function addNote() {
    const value = note.trim()
    if (!value) {
      notify('先写点内容再添加')
      return
    }
    store.addNote(customer.id, value)
    setNote('')
    notify('备注已添加')
  }

  return (
    <>
      <header className="topbar">
        <div>
          <div className="crumb"><Link to="/customers">客户</Link> / 档案</div>
          <h1>{customer.display_name}</h1>
        </div>
        <div className="topbar-actions">
          <button className="btn hide-mobile" type="button" onClick={() => notify('编辑资料将在档案补全功能中接入')}>编辑资料</button>
          <Link className="btn btn-primary" to="/calendar">＋ 新约单</Link>
        </div>
      </header>

      <main className="content">
        <div className="detail-grid">
          <div className="detail-side">
            <section className="card">
              <div className="profile-head">
                <div className={`avatar ${customer.avatar}`}>{customer.display_name[0]}</div>
                <div>
                  <h2>{customer.display_name}</h2>
                  <div className="sub"><span className={customer.status === 'archived' ? 'badge badge-muted' : 'badge badge-success'}>{customer.status === 'archived' ? '已归档' : '活跃'}</span>　建档 {shortDate(customer.created_at)}</div>
                </div>
              </div>
              <div className="value-strip">
                <div className="vs"><div className="n">{formatPrice(totalAmount)}</div><div className="l">累计消费</div></div>
                <div className="vs"><div className="n">{orders.length}</div><div className="l">约单</div></div>
                <div className="vs"><div className="n">{customerAge(customer.created_at)}</div><div className="l">客龄</div></div>
              </div>
              <div className="kv">
                <span className="k">真实姓名</span><span className="v">{customer.real_name || '未填写'}</span>
                <span className="k">手机号</span><span className="v num">{customer.phone || '未填写'}</span>
                <span className="k">生日</span><span className="v num">{visibleBirthday(customer.birthday)} <span className="muted-text">· 提前 3 天提醒</span></span>
                <span className="k">来源渠道</span><span className="v">{DICT.channel[customer.channel]}</span>
                <span className="k">最近拍摄</span><span className="v num">{shortDate(lastShot)}</span>
              </div>
            </section>

            <section className="card">
              <div className="card-title">私域账号 <span className="count">· {customer.identities.length}</span>
                <button className="btn btn-sm btn-ghost more" type="button" onClick={() => notify('添加账号将在档案补全功能中接入')}>＋ 添加</button>
              </div>
              {customer.identities.map((identity) => (
                <div className="identity-item" key={`${identity.platform}-${identity.handle}`}>
                  <span className="plat">{DICT.platform[identity.platform]}</span>
                  <span className="handle">{identity.handle}</span>
                  <span className="rmk">{identity.remark}</span>
                </div>
              ))}
            </section>

            <section className="card">
              <div className="card-title">拍摄回顾 <span className="count">· {Math.max(1, orders.length)}</span></div>
              <div className="shot-grid">
                {orders.slice(0, 3).map((order) => (
                  <button className="shot-thumb" type="button" key={order.id} onClick={() => notify('选片相册将在后续版本接入')}>
                    <span className="tag"><b>{order.title}</b>{shortDate(order.shot_at)} · {DICT.orderStatus[order.status]}</span>
                  </button>
                ))}
              </div>
            </section>

            <section className="card">
              <div className="card-title">人脉链</div>
              <div className="ref-chain">
                <RefNode customer={customer} self />
                {[...directReferrals, ...secondReferrals].map((item) => (
                  <div key={item.id}>
                    <div className="ref-link" />
                    <RefNode customer={item} />
                  </div>
                ))}
                <div className="ref-sum">转介绍 {directReferrals.length + secondReferrals.length} 位客户 · 后续可接入订单贡献统计</div>
              </div>
            </section>
          </div>

          <div>
            <div className="tabs">
              <button className={`tab${tab === 'orders' ? ' active' : ''}`} type="button" onClick={() => setTab('orders')}>约单记录 · {orders.length}</button>
              <button className={`tab${tab === 'notes' ? ' active' : ''}`} type="button" onClick={() => setTab('notes')}>备注流 · {customer.notes.length}</button>
              <button className={`tab${tab === 'reminders' ? ' active' : ''}`} type="button" onClick={() => setTab('reminders')}>提醒 · {reminders.length}</button>
            </div>

            {tab === 'orders' && (
              <section className="card tab-panel active">
                {orders.map((order) => {
                  const pkg = byId(store.packages, order.package_id)
                  const payState = order.balance_paid ? '已结清' : order.deposit_paid ? '已收定金' : '未付款'
                  return (
                    <div className="order-item" key={order.id}>
                      <div className="line1">
                        <span className="t">{order.title}</span>
                        <span className={`badge ${orderStatusBadgeClass(order.status)}`}>{DICT.orderStatus[order.status]}</span>
                        <span className="price">{formatPrice(order.price)}</span>
                      </div>
                      <div className="meta">{pkg?.name ?? '未选套系'} · {payState}{order.shot_at ? ` · 拍摄 ${shortDate(order.shot_at)}` : ''}{order.delivered_at ? ` · 交付 ${shortDate(order.delivered_at)}` : ''}</div>
                      <StatusFlow status={order.status} />
                    </div>
                  )
                })}
              </section>
            )}

            {tab === 'notes' && (
              <section className="card tab-panel active">
                <div className="field compact-field">
                  <textarea className="input" value={note} onChange={(event) => setNote(event.target.value)} placeholder="记一笔：偏好、忌讳、下次想拍什么…" />
                </div>
                <div className="right-actions"><button className="btn btn-sm btn-primary" type="button" onClick={addNote}>添加备注</button></div>
                {customer.notes.length === 0 ? <div className="empty">还没有备注</div> : customer.notes.map((item) => (
                  <div className="note-item" key={`${item.time}-${item.content}`}>
                    <div className="time">{item.time}</div>
                    <div>{item.content}</div>
                  </div>
                ))}
              </section>
            )}

            {tab === 'reminders' && (
              <section className="card tab-panel active">
                {reminders.length === 0 ? <div className="empty">暂无提醒</div> : reminders.map((reminder) => (
                  <div className={`row-item${reminder.status === 'done' ? ' done' : ''}`} key={reminder.id}>
                    <span className={reminder.status === 'done' ? 'badge badge-muted' : 'badge badge-accent'}>{DICT.reminderType[reminder.type]}</span>
                    <div className="grow">
                      <div className="title">{reminder.content}</div>
                      <div className="meta num">{shortDate(reminder.due_date)}{reminder.status === 'done' ? ' · 已完成' : ''}</div>
                    </div>
                    {reminder.status === 'pending' && <button className="btn btn-sm" type="button" onClick={() => { store.markReminderDone(reminder.id); notify('已标记完成') }}>完成</button>}
                  </div>
                ))}
              </section>
            )}
          </div>
        </div>
      </main>
    </>
  )
}

function RefNode({ customer, self }: { customer: { id: string; display_name: string; avatar: string; created_at: string }; self?: boolean }) {
  return (
    <div className={`ref-node${self ? ' self' : ''}`}>
      <div className={`avatar ${customer.avatar}`}>{customer.display_name[0]}</div>
      <span className="who"><Link to={`/customers/${customer.id}`}>{customer.display_name}</Link>{self && <span className="muted-text"> 本人</span>}</span>
      {!self && <span className="when">{shortDate(customer.created_at).slice(0, 7)} 介绍</span>}
    </div>
  )
}

function StatusFlow({ status }: { status: typeof flow[number] | 'cancelled' }) {
  if (status === 'cancelled') return <span className="badge badge-danger">已取消</span>
  const current = flow.indexOf(status)
  return (
    <div className="status-flow">
      {flow.map((step, index) => (
        <span key={step} className="status-step-wrap">
          <span className={`step${index <= current ? ' on' : ''}`}>{DICT.orderStatus[step]}</span>
          {index < flow.length - 1 && <span className="sep">›</span>}
        </span>
      ))}
    </div>
  )
}
