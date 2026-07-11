import { useEffect, useMemo, useState } from 'react'

import {
  ApiError,
  listCustomers,
  listOrders,
  listPackages,
} from '../../api/client'
import type {
  CustomerListResponse,
  OrderListItem,
  PackageListResponse,
} from '../../api/client'
import { missingSelectedOrderOption } from './flow'

type CustomerOption = CustomerListResponse['items'][number]
type PackageOption = PackageListResponse['items'][number]

export interface FixedScheduleCustomer {
  id: string
  display_name: string
  status: string
}

export interface ShootOrderDraft {
  source: 'existing' | 'new'
  customerId: string
  orderId: string
  orderStatus?: string
  packageId: string
  title: string
  priceYuan: string
}

export default function ShootOrderFlow({
  value,
  targetEndAt,
  reloadToken,
  fixedCustomer,
  onChange,
  onHistoricalHandoff,
}: {
  value: ShootOrderDraft
  targetEndAt: string | null
  reloadToken: number
  fixedCustomer?: FixedScheduleCustomer
  onChange(value: ShootOrderDraft): void
  onHistoricalHandoff(customerID: string): void
}) {
  const historical = Boolean(targetEndAt && Date.parse(targetEndAt) <= Date.now())
  const [includeArchived, setIncludeArchived] = useState(false)
  const [customers, setCustomers] = useState<CustomerOption[]>([])
  const [orders, setOrders] = useState<OrderListItem[]>([])
  const [packages, setPackages] = useState<PackageOption[]>([])
  const [loadingOrders, setLoadingOrders] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (fixedCustomer) {
      setCustomers([fixedCustomer as CustomerOption])
      return
    }
    let active = true
    loadAll((page) => listCustomers({
      status: historical && includeArchived ? 'all' : 'active',
      page,
      pageSize: 100,
    }))
      .then((items) => { if (active) setCustomers(items.filter((item) => item.status !== 'merged')) })
      .catch((reason: unknown) => { if (active) setError(errorMessage(reason, '客户候选加载失败')) })
    return () => { active = false }
  }, [fixedCustomer, historical, includeArchived])

  useEffect(() => {
    if (!value.customerId || !targetEndAt) {
      setOrders([])
      return
    }
    let active = true
    setOrders([])
    setLoadingOrders(true)
    setError(null)
    loadAll((page) => listOrders({
      customerId: value.customerId,
      schedulableAt: targetEndAt,
      page,
      pageSize: 100,
    }))
      .then((items) => {
        if (!active) return
        setOrders(items)
      })
      .catch((reason: unknown) => { if (active) setError(errorMessage(reason, '可排期订单加载失败')) })
      .finally(() => { if (active) setLoadingOrders(false) })
    return () => { active = false }
  }, [reloadToken, targetEndAt, value.customerId])

  useEffect(() => {
    if (value.source !== 'new' || historical) return
    let active = true
    loadAll((page) => listPackages({ status: 'active', page, pageSize: 100 }))
      .then((items) => { if (active) setPackages(items) })
      .catch((reason: unknown) => { if (active) setError(errorMessage(reason, '套系加载失败')) })
    return () => { active = false }
  }, [historical, value.source])

  const selectedOrder = useMemo(
    () => orders.find((item) => item.id === value.orderId),
    [orders, value.orderId],
  )

  function selectOrder(orderID: string) {
    const order = orders.find((item) => item.id === orderID)
    onChange({ ...value, orderId: orderID, orderStatus: order?.status })
  }

  function selectPackage(packageID: string) {
    const selected = packages.find((item) => item.id === packageID)
    onChange({
      ...value,
      packageId: packageID,
      title: selected?.name ?? value.title,
      priceYuan: selected ? formatYuan(selected.base_price) : value.priceYuan,
    })
  }

  return (
    <div className="shoot-order-flow">
      <div className="segmented" aria-label="订单来源">
        <button
          type="button"
          className={value.source === 'existing' ? 'active' : ''}
          onClick={() => onChange({ ...value, source: 'existing' })}
        >
          选择已有订单
        </button>
        {!historical && (
          <button
            type="button"
            className={value.source === 'new' ? 'active' : ''}
            onClick={() => onChange({ ...value, source: 'new', orderId: '', orderStatus: undefined })}
          >
            新建咨询订单
          </button>
        )}
      </div>

      <div className="field">
        <label htmlFor="scheduleCustomer">客户</label>
        {fixedCustomer ? (
          <div className="readonly-field">{fixedCustomer.display_name}</div>
        ) : (
          <select
            id="scheduleCustomer"
            className="input"
            value={value.customerId}
            onChange={(event) => onChange({
              ...value,
              customerId: event.target.value,
              orderId: '',
              orderStatus: undefined,
            })}
          >
            <option value="">选择客户</option>
            {customers.map((customer) => (
              <option key={customer.id} value={customer.id}>
                {customer.display_name}{customer.status === 'archived' ? '（已归档）' : ''}
              </option>
            ))}
          </select>
        )}
      </div>

      {historical && !fixedCustomer && (
        <label className="check-line">
          <input
            type="checkbox"
            checked={includeArchived}
            onChange={(event) => setIncludeArchived(event.target.checked)}
          />
          包括已归档客户
        </label>
      )}

      {value.source === 'existing' ? (
        <div className="field">
          <label htmlFor="scheduleOrder">可排期订单</label>
          <select
            id="scheduleOrder"
            className="input"
            value={value.orderId}
            disabled={!value.customerId || loadingOrders}
            onChange={(event) => selectOrder(event.target.value)}
          >
            <option value="">{loadingOrders ? '加载中' : '选择订单'}</option>
            {missingSelectedOrderOption(value.orderId, orders) && (
              <option value={value.orderId}>
                {value.title || '当前订单'} · {orderStatusLabel(value.orderStatus ?? '')}
              </option>
            )}
            {orders.map((order) => (
              <option key={order.id} value={order.id}>
                {order.title ?? order.package_name ?? '未命名订单'} · {orderStatusLabel(order.status)}
              </option>
            ))}
          </select>
          {selectedOrder && (
            <div className="hint">
              {selectedOrder.customer_display_name} · {orderStatusLabel(selectedOrder.status)}
            </div>
          )}
          {!loadingOrders && value.customerId && orders.length === 0 && historical && (
            <div className="schedule-empty-action">
              <span>该客户没有符合目标时间的历史订单</span>
              <button className="btn btn-sm" type="button" onClick={() => onHistoricalHandoff(value.customerId)}>
                先补录历史订单
              </button>
            </div>
          )}
        </div>
      ) : (
        <>
          <div className="field">
            <label htmlFor="schedulePackage">套系</label>
            <select id="schedulePackage" className="input" value={value.packageId} onChange={(event) => selectPackage(event.target.value)}>
              <option value="">不关联套系</option>
              {packages.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
            </select>
          </div>
          <div className="field-row">
            <div className="field">
              <label htmlFor="scheduleOrderTitle">订单标题</label>
              <input id="scheduleOrderTitle" className="input" value={value.title} onChange={(event) => onChange({ ...value, title: event.target.value })} />
            </div>
            <div className="field">
              <label htmlFor="scheduleOrderPrice">价格（元）</label>
              <input id="scheduleOrderPrice" className="input" inputMode="decimal" value={value.priceYuan} onChange={(event) => onChange({ ...value, priceYuan: event.target.value })} />
            </div>
          </div>
        </>
      )}

      {error && <div className="form-error">{error}</div>}
    </div>
  )
}

async function loadAll<T>(load: (page: number) => Promise<{ items: T[]; total: number }>): Promise<T[]> {
  const items: T[] = []
  for (let page = 1; ; page += 1) {
    const result = await load(page)
    items.push(...result.items)
    if (items.length >= result.total || result.items.length === 0) return items
  }
}

function formatYuan(cents: number): string {
  return String(cents / 100)
}

function orderStatusLabel(status: string): string {
  return {
    consulting: '咨询', scheduled: '定档', shot: '已拍摄', selected: '已选片',
    retouching: '精修中', delivered: '已交付', closed: '完结', cancelled: '取消',
  }[status] ?? status
}

function errorMessage(reason: unknown, fallback: string): string {
  if (reason instanceof ApiError || reason instanceof Error) return reason.message
  return fallback
}
