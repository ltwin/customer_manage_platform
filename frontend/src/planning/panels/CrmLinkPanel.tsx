import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { listCustomers, listOrders, fetchCustomer, type CustomerListResponse, type OrderListItem } from '../../api/client'
import { planningErrorMessage } from '../presentation'
import type { CommandRunner } from '../ShootPlanWorkspacePage'
import type { ShootPlanDetail } from '../api'

type CustomerListItem = CustomerListResponse['items'][number]

export default function CrmLinkPanel({ plan, busy, runCommand }: { plan: ShootPlanDetail; busy: boolean; runCommand: CommandRunner }) {
  const crm = plan.crm
  const [query, setQuery] = useState('')
  const [customers, setCustomers] = useState<CustomerListItem[]>([])
  const [orders, setOrders] = useState<OrderListItem[]>([])
  const [customerName, setCustomerName] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [confirmUnlink, setConfirmUnlink] = useState<'order' | 'customer' | null>(null)

  useEffect(() => {
    const customerID = crm?.customer_id
    if (!customerID) {
      setCustomerName(null)
      return
    }
    let active = true
    void fetchCustomer(customerID).then((detail) => {
      if (active) setCustomerName(detail.display_name)
    }).catch(() => {
      if (active) setCustomerName(customerID)
    })
    return () => { active = false }
  }, [crm?.customer_id])

  useEffect(() => {
    const customerID = crm?.customer_id
    if (!customerID) {
      setOrders([])
      return
    }
    let active = true
    void loadAllCustomerOrders(customerID).then((items) => {
      if (active) setOrders(items)
    }).catch(() => {
      if (active) setOrders([])
    })
    return () => { active = false }
  }, [crm?.customer_id])

  async function searchCustomers() {
    setError(null)
    try {
      const result = await listCustomers({ q: query.trim() || undefined, status: 'active', pageSize: 8 })
      setCustomers(result.items)
    } catch (cause) {
      setError(planningErrorMessage(cause, '客户搜索失败'))
    }
  }

  async function run(command: Parameters<CommandRunner>[0], scope: string) {
    setError(null)
    try {
      await runCommand(command, scope)
      setConfirmUnlink(null)
    } catch (cause) {
      setError(planningErrorMessage(cause, 'CRM 关联失败'))
    }
  }

  const projection = crm?.schedule_projection
  const futureSlot = useMemo(() => {
    if (!projection?.ends_at) return null
    if (Date.parse(projection.ends_at) <= Date.now()) return null
    if (projection.status !== 'active_applied' && projection.status !== 'active_manual_override' && projection.status !== 'active_unapplied') {
      return null
    }
    return projection
  }, [projection])

  const linkedOrder = orders.find((item) => item.id === crm?.order_id) ?? null
  const snapshot = crm?.linked_order_snapshot

  return (
    <section className="card planning-panel" data-testid="crm-link-card">
      <div className="planning-panel-head">
        <div>
          <h2>CRM 关联</h2>
          <p>弱耦合：关联动作不会推动订单或档期状态。</p>
        </div>
      </div>

      <dl className="planning-crm-kv">
        <div>
          <dt>客户</dt>
          <dd>
            {crm?.customer_id ? (
              <>
                {customerName ?? crm.customer_id}
                {' · '}
                <Link to={`/customers/${crm.customer_id}`}>查看客户档案</Link>
              </>
            ) : (
              <span className="muted-text">未关联</span>
            )}
          </dd>
        </div>
        <div>
          <dt>订单</dt>
          <dd>
            {crm?.order_id || crm?.state === 'order_deleted' ? (
              <>
                {snapshot?.title ?? linkedOrder?.title ?? snapshot?.order_id ?? crm?.order_id}
                {snapshot?.status_at_link ? ` · ${snapshot.status_at_link}` : ''}
                {snapshot?.package_name ? ` · 套系「${snapshot.package_name}」` : ''}
                {crm?.state === 'order_cancelled' && <div className="planning-crm-warning">订单已取消，关联仍保留快照。</div>}
                {crm?.state === 'order_deleted' && <div className="planning-crm-warning">订单已删除，当前引用已清空。</div>}
              </>
            ) : (
              <span className="muted-text">未关联</span>
            )}
          </dd>
        </div>
        <div>
          <dt>未来拍摄档期</dt>
          <dd>
            {futureSlot?.slot_id ? (
              <>
                {futureSlot.slot_id}
                {futureSlot.starts_at && futureSlot.ends_at ? ` · ${formatRange(futureSlot.starts_at, futureSlot.ends_at, futureSlot.timezone)}` : ''}
                {projection?.status === 'active_manual_override' && <div className="hint">当前执行时间窗为手动填写，档期投影未覆盖。</div>}
                {projection?.apply_suppressed && <div className="hint">已抑制自动跟随；需要时再采纳档期投影。</div>}
              </>
            ) : (
              <span className="muted-text">没有未来拍摄档期</span>
            )}
          </dd>
        </div>
      </dl>

      {error && <p className="planning-inline-error" role="alert">{error}</p>}

      {!crm?.customer_id && (
        <form className="planning-form-actions" onSubmit={(event) => { event.preventDefault(); void searchCustomers() }}>
          <input
            aria-label="搜索客户"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="搜索客户姓名或备注"
          />
          <button className="btn btn-secondary btn-sm" type="submit" disabled={busy}>搜索客户</button>
        </form>
      )}
      {!crm?.customer_id && customers.length > 0 && (
        <ul className="planning-crm-choices">
          {customers.map((customer) => (
            <li key={customer.id}>
              <button
                className="btn btn-ghost btn-sm"
                type="button"
                disabled={busy || !customer.id}
                onClick={() => void run({
                  expected_revision: plan.revision,
                  operation: 'link_customer',
                  customer_id: customer.id ?? '',
                }, 'link-customer')}
              >
                关联 {customer.display_name}
              </button>
            </li>
          ))}
        </ul>
      )}

      {crm?.customer_id && !crm.order_id && orders.length > 0 && (
        <ul className="planning-crm-choices">
          {orders.map((order) => (
            <li key={order.id}>
              <button
                className="btn btn-ghost btn-sm"
                type="button"
                disabled={busy || !order.id}
                onClick={() => void run({
                  expected_revision: plan.revision,
                  operation: 'link_order',
                  order_id: order.id ?? '',
                }, 'link-order')}
              >
                关联订单 {order.title ?? order.id}
              </button>
            </li>
          ))}
        </ul>
      )}

      {projection?.status === 'active_unapplied' && projection.apply_suppressed && crm?.projection_revision && (
        <button
          className="btn btn-secondary btn-sm"
          type="button"
          disabled={busy}
          onClick={() => void run({
            expected_revision: plan.revision,
            operation: 'adopt_schedule_projection',
            projection_revision: crm.projection_revision ?? 1,
          }, 'adopt-projection')}
        >
          采纳档期投影
        </button>
      )}

      <div className="planning-form-actions">
        {crm?.order_id && confirmUnlink !== 'order' && (
          <button className="btn btn-secondary btn-sm" type="button" disabled={busy} onClick={() => setConfirmUnlink('order')}>
            解除订单关联
          </button>
        )}
        {crm?.customer_id && !crm.order_id && confirmUnlink !== 'customer' && (
          <button className="btn btn-secondary btn-sm" type="button" disabled={busy} onClick={() => setConfirmUnlink('customer')}>
            解除客户关联
          </button>
        )}
        {futureSlot?.slot_id && (
          <Link className="btn btn-ghost btn-sm" to="/calendar">查看档期</Link>
        )}
      </div>

      {confirmUnlink === 'order' && (
        <div className="planning-crm-confirm" role="alertdialog" aria-labelledby="unlink-order-title">
          <p id="unlink-order-title"><strong>解除与订单的关联？</strong></p>
          <p>解除后此策划立即失去当前客户/订单关联：完整档分享链接会立即失效（不降级），认领提醒会被撤销。订单和档期本身不受任何影响。</p>
          <div className="planning-form-actions">
            <button className="btn btn-primary btn-sm" type="button" disabled={busy} onClick={() => void run({
              expected_revision: plan.revision,
              operation: 'unlink_order',
            }, 'unlink-order')}>解除关联</button>
            <button className="btn btn-ghost btn-sm" type="button" onClick={() => setConfirmUnlink(null)}>取消</button>
          </div>
        </div>
      )}
      {confirmUnlink === 'customer' && (
        <div className="planning-crm-confirm" role="alertdialog" aria-labelledby="unlink-customer-title">
          <p id="unlink-customer-title"><strong>解除与客户的关联？</strong></p>
          <p>解除后此策划变为独立策划：完整档分享链接会立即失效（不降级），认领提醒会被撤销。订单和档期本身不受任何影响。</p>
          <div className="planning-form-actions">
            <button className="btn btn-primary btn-sm" type="button" disabled={busy} onClick={() => void run({
              expected_revision: plan.revision,
              operation: 'unlink_customer',
            }, 'unlink-customer')}>解除关联</button>
            <button className="btn btn-ghost btn-sm" type="button" onClick={() => setConfirmUnlink(null)}>取消</button>
          </div>
        </div>
      )}
    </section>
  )
}

async function loadAllCustomerOrders(customerID: string): Promise<OrderListItem[]> {
  const pageSize = 100
  const items: OrderListItem[] = []
  let page = 1
  for (;;) {
    const result = await listOrders({ customerId: customerID, page, pageSize })
    items.push(...result.items)
    if (result.items.length < pageSize || items.length >= result.total) {
      return items
    }
    page += 1
  }
}

function formatRange(start: string, end: string, timezone?: string) {
  try {
    const zone = timezone || 'Asia/Shanghai'
    const fmt = new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', timeZone: zone, hour12: false })
    return `${fmt.format(new Date(start))}–${fmt.format(new Date(end))}`
  } catch {
    return `${start}–${end}`
  }
}
