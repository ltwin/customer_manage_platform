import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { DICT, TODAY } from '../crm/prototypeData'
import type { Slot, SlotType } from '../crm/prototypeData'
import type { SlotInput } from '../crm/prototypeStoreContext'
import { byId, formatPrice, packagePricingText, slotLabel, slotOverlapIds } from '../crm/model'
import { usePrototypeStore } from '../crm/prototypeStoreContext'
import { useShell } from '../components/shellContext'

const dows = ['一', '二', '三', '四', '五', '六', '日']

const emptyDraft: SlotInput = {
  type: 'shoot',
  date: TODAY,
  start: '14:00',
  end: '16:00',
  note: '',
  orderId: null,
}

export default function CalendarPage() {
  const store = usePrototypeStore()
  const { notify } = useShell()
  const [selectedDate, setSelectedDate] = useState<string | null>(TODAY)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [draft, setDraft] = useState<SlotInput>(emptyDraft)
  const [slotError, setSlotError] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)

  const cells = useMemo(() => {
    const value: Array<{ date: string; n: number; other: boolean }> = [
      { date: '2026-06-29', n: 29, other: true },
      { date: '2026-06-30', n: 30, other: true },
    ]
    for (let i = 1; i <= 31; i += 1) {
      value.push({ date: `2026-07-${String(i).padStart(2, '0')}`, n: i, other: false })
    }
    value.push({ date: '2026-08-01', n: 1, other: true }, { date: '2026-08-02', n: 2, other: true })
    return value
  }, [])

  const selectedSlots = selectedDate ? store.slots.filter((slot) => slot.date === selectedDate).sort((a, b) => a.start.localeCompare(b.start)) : []
  const selectableOrders = store.orders.filter((order) => order.status !== 'cancelled')
  const hasConflict = store.slots.some((slot) => slot.id !== draft.id && slot.date === draft.date && slot.start < draft.end && draft.start < slot.end)

  function openDay(date: string) {
    setSelectedDate(date)
    setDrawerOpen(true)
  }

  function openNewSlot(date = selectedDate ?? TODAY) {
    setDraft({ ...emptyDraft, date, orderId: selectableOrders[0]?.id ?? null })
    setSlotError(null)
    setDialogOpen(true)
  }

  function editSlot(slot: Slot) {
    setDraft({
      id: slot.id,
      type: slot.type,
      date: slot.date,
      start: slot.start,
      end: slot.end,
      note: slot.note,
      orderId: slot.order_id,
    })
    setSlotError(null)
    setDialogOpen(true)
  }

  function saveSlot() {
    if (!draft.date || !draft.start || !draft.end) {
      setSlotError('请填写完整日期和时间')
      return
    }
    if (draft.start >= draft.end) {
      setSlotError('结束时间必须晚于开始时间')
      return
    }
    if (draft.type === 'shoot' && !draft.orderId) {
      setSlotError('拍摄档期需要关联一笔订单')
      return
    }
    store.upsertSlot(draft)
    setDialogOpen(false)
    setSlotError(null)
    setSelectedDate(draft.date)
    notify(draft.id ? '档期已更新' : '档期已保存')
  }

  function closeDialog() {
    setDialogOpen(false)
    setSlotError(null)
  }

  function deleteSlot(slotId: string) {
    if (confirmDelete !== slotId) {
      setConfirmDelete(slotId)
      window.setTimeout(() => setConfirmDelete(null), 3000)
      return
    }
    store.deleteSlot(slotId)
    setConfirmDelete(null)
    notify('档期已删除')
  }

  return (
    <>
      <header className="topbar">
        <div>
          <h1>档期</h1>
          <div className="sub">点击日期查看当天安排 · 重叠档期会标出提醒</div>
        </div>
        <div className="topbar-actions">
          <button className="btn btn-primary" type="button" onClick={() => openNewSlot()}>＋ 新建档期</button>
        </div>
      </header>

      <main className="content">
        <div className="cal-head">
          <button className="btn btn-sm" type="button" onClick={() => notify('上一月将在真实日历能力中接入')} aria-label="上一月">‹</button>
          <h2>2026 年 7 月</h2>
          <button className="btn btn-sm" type="button" onClick={() => notify('下一月将在真实日历能力中接入')} aria-label="下一月">›</button>
          <div className="cal-legend">
            <span><span className="dot slot-shoot" />拍摄</span>
            <span><span className="dot slot-hold" />预留</span>
            <span><span className="dot slot-busy" />占用/休假</span>
          </div>
        </div>
        <div className="cal-grid">
          {dows.map((dow) => <div className="cal-dow" key={dow}>周{dow}</div>)}
          {cells.map((cell) => {
            const daySlots = store.slots.filter((slot) => slot.date === cell.date)
            return (
              <button
                type="button"
                className={`cal-cell${cell.other ? ' other' : ''}${cell.date === TODAY ? ' today' : ''}${cell.date === selectedDate ? ' selected' : ''}`}
                key={cell.date}
                onClick={() => openDay(cell.date)}
              >
                <span className="cal-date">{cell.n}</span>
                <span className="cal-events">
                  {daySlots.map((slot) => (
                    <span className={`cal-event ${slot.type}${slotOverlapIds(store.slots, slot).length ? ' overlap' : ''}`} key={slot.id}>
                      {slot.start} {slotLabel(slot, store.orders, store.customers)}
                    </span>
                  ))}
                </span>
                <span className="cal-count">{daySlots.map((slot) => <i className={`slot-${slot.type}`} key={slot.id} />)}</span>
              </button>
            )
          })}
        </div>
      </main>

      <div className={`drawer-overlay${drawerOpen ? ' open' : ''}`} onClick={() => setDrawerOpen(false)} />
      <aside className={`drawer${drawerOpen ? ' open' : ''}`}>
        <div className="drawer-head">
          <h2>{selectedDate ? `${Number(selectedDate.slice(5, 7))} 月 ${Number(selectedDate.slice(8, 10))} 日` : '当天安排'}</h2>
          <button className="icon-btn" type="button" onClick={() => setDrawerOpen(false)} aria-label="关闭">×</button>
        </div>
        <div className="drawer-body">
          {selectedSlots.length === 0 ? <div className="empty">这天还没有安排，档期空闲</div> : selectedSlots.map((slot) => {
            const order = byId(store.orders, slot.order_id)
            const customer = order ? byId(store.customers, order.customer_id) : null
            const pkg = order ? byId(store.packages, order.package_id) : null
            const overlapIds = slotOverlapIds(store.slots, slot)
            return (
              <div className="slot-entry" key={slot.id}>
                <div className="head">
                  <span className={`dot slot-${slot.type}`} />
                  <span className="badge badge-muted">{DICT.slotType[slot.type]}</span>
                  <span className="time">{slot.start}–{slot.end}</span>
                </div>
                <div className="body">{order && customer ? `${customer.display_name} · ${order.title}` : slot.note}</div>
                <div className="meta">{order && pkg ? `${pkg.name} · ${formatPrice(order.price)} · ${slot.note}` : ''}</div>
                <div className="slot-actions">
                  {customer && <Link className="btn btn-sm" to={`/customers/${customer.id}`}>查看客户档案</Link>}
                  <button className="btn btn-sm" type="button" onClick={() => editSlot(slot)}>编辑</button>
                  <button className="btn btn-sm btn-danger-ghost" type="button" onClick={() => deleteSlot(slot.id)}>{confirmDelete === slot.id ? '确认删除？' : '删除'}</button>
                </div>
                {overlapIds.length > 0 && <div className="conflict-tip">与当天另一条档期时间重叠，请确认是否改期</div>}
              </div>
            )
          })}
        </div>
        <div className="drawer-foot">
          <button className="btn" type="button" onClick={() => setDrawerOpen(false)}>关闭</button>
          <button className="btn btn-primary" type="button" onClick={() => openNewSlot()}>＋ 在这天加档期</button>
        </div>
      </aside>

      <div className={`overlay${dialogOpen ? ' open' : ''}`} onClick={(event) => { if (event.target === event.currentTarget) closeDialog() }}>
        <div className="dialog" role="dialog" aria-modal="true" aria-labelledby="slotDialogTitle">
          <h2 id="slotDialogTitle">{draft.id ? '编辑档期' : '新建档期'}</h2>
          <div className="dialog-sub">拍摄档期可关联现有订单；预留/占用只需时间和备注</div>

          <div className="field">
            <label htmlFor="slotType">类型</label>
            <select
              id="slotType"
              className="input"
              value={draft.type}
              onChange={(event) => {
                const type = event.target.value as SlotType
                setDraft({ ...draft, type, orderId: type === 'shoot' ? draft.orderId ?? selectableOrders[0]?.id ?? null : null })
                setSlotError(null)
              }}
            >
              <option value="shoot">拍摄（关联客户与套系）</option>
              <option value="hold">预留（意向占位）</option>
              <option value="busy">占用（个人事务/休假）</option>
            </select>
          </div>

          <div className="field-row">
            <div className="field">
              <label htmlFor="slotDate">日期</label>
              <input id="slotDate" className="input" type="date" value={draft.date} onChange={(event) => setDraft({ ...draft, date: event.target.value })} />
            </div>
            <div className="field">
              <label>时间</label>
              <div className="inline-inputs">
                <input className="input" type="time" value={draft.start} onChange={(event) => setDraft({ ...draft, start: event.target.value })} />
                <span>–</span>
                <input className="input" type="time" value={draft.end} onChange={(event) => setDraft({ ...draft, end: event.target.value })} />
              </div>
            </div>
          </div>

          {draft.type === 'shoot' && (
            <div className="field">
              <label htmlFor="slotOrder">关联订单</label>
              <select id="slotOrder" className="input" value={draft.orderId ?? ''} onChange={(event) => { setDraft({ ...draft, orderId: event.target.value || null }); setSlotError(null) }}>
                <option value="">请选择订单</option>
                {selectableOrders.map((order) => {
                  const customer = byId(store.customers, order.customer_id)
                  const pkg = byId(store.packages, order.package_id)
                  return <option key={order.id} value={order.id}>{customer?.display_name ?? '未知客户'} · {order.title} · {pkg ? packagePricingText(pkg) : '未选套系'}</option>
                })}
              </select>
              <div className="hint">客户与套系来自所选订单，正式创建订单流程会在 order-tracking 接入。</div>
            </div>
          )}

          <div className="field">
            <label htmlFor="slotNote">备注</label>
            <input id="slotNote" className="input" value={draft.note} onChange={(event) => setDraft({ ...draft, note: event.target.value })} placeholder="场地、造型、注意事项…" />
          </div>

          {hasConflict && <div className="conflict-tip">与当天已有档期时间重叠，保存后会在日历上标出</div>}
          {slotError && <p role="alert" className="form-error">{slotError}</p>}

          <div className="dialog-actions">
            <button className="btn" type="button" onClick={closeDialog}>取消</button>
            <button className="btn btn-primary" type="button" onClick={saveSlot}>{draft.id ? '保存修改' : '保存档期'}</button>
          </div>
        </div>
      </div>
    </>
  )
}
