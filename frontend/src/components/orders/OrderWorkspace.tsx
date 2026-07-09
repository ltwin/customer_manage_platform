import { Link, useNavigate } from 'react-router-dom'
import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ApiError,
  createOrder,
  deleteOrder,
  listCustomers,
  listOrders,
  listPackages,
  updateOrder,
} from '../../api/client'
import type {
  CreateOrderBody,
  CustomerListResponse,
  OrderListItem,
  OrderStatus,
  PackageListStatus,
  PackageListResponse,
  UpdateOrderBody,
} from '../../api/client'
import { useShell } from '../shellContext'
import {
  isPackagePriceYuanInputAllowed,
  packagePriceYuanToCents,
  validatePackagePriceYuan,
} from '../../pages/packagePrice'

type OrderStatusValue = NonNullable<OrderStatus> & string
type StatusFilter = OrderStatusValue | ''
type CustomerOption = CustomerListResponse['items'][number]
type PackageOption = PackageListResponse['items'][number]

interface FixedCustomer {
  id?: string
  display_name: string
}

interface OrderDraft {
  customerId: string
  packageId: string
  title: string
  priceYuan: string
  backfill: boolean
  status: OrderStatusValue
  shotDate: string
  deliveredDate: string
  depositPaid: boolean
  balancePaid: boolean
  note: string
}

interface ProgressTarget {
  order: OrderListItem
  status: OrderStatusValue
  date: string
}

const orderPageSize = 30
const optionPageSize = 100

const statusOrder: OrderStatusValue[] = [
  'consulting',
  'scheduled',
  'shot',
  'selected',
  'retouching',
  'delivered',
  'closed',
  'cancelled',
]
const backfillStatusOrder = statusOrder.filter((item) => item !== 'consulting')

const statusLabels: Record<OrderStatusValue, string> = {
  consulting: '咨询',
  scheduled: '定档',
  shot: '已拍摄',
  selected: '已选片',
  retouching: '精修中',
  delivered: '已交付',
  closed: '完结',
  cancelled: '取消',
}

const statusFilters: Array<[StatusFilter, string]> = [
  ['', '全部'],
  ['consulting', '咨询'],
  ['scheduled', '定档'],
  ['shot', '拍摄后'],
  ['selected', '已选片'],
  ['retouching', '精修中'],
  ['delivered', '已交付'],
  ['closed', '完结'],
  ['cancelled', '取消'],
]

export default function OrderWorkspace({
  customer,
  onChanged,
}: {
  customer?: FixedCustomer
  onChanged?: () => void
}) {
  const navigate = useNavigate()
  const { notify } = useShell()
  const fixedCustomerId = customer?.id ?? ''
  const [items, setItems] = useState<OrderListItem[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<StatusFilter>('')
  const [unpaidOnly, setUnpaidOnly] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reloadTick, setReloadTick] = useState(0)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [draft, setDraft] = useState<OrderDraft>(() => defaultDraft(fixedCustomerId))
  const [submitted, setSubmitted] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [packages, setPackages] = useState<PackageOption[]>([])
  const [customers, setCustomers] = useState<CustomerOption[]>([])
  const [actionId, setActionId] = useState<string | null>(null)
  const [progressTarget, setProgressTarget] = useState<ProgressTarget | null>(null)
  const [cancelTarget, setCancelTarget] = useState<OrderListItem | null>(null)
  const [cancelNote, setCancelNote] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<OrderListItem | null>(null)

  const goLogin = useCallback(() => {
    navigate('/login', { replace: true })
  }, [navigate])

  const requestParams = useMemo(() => ({
    customerId: fixedCustomerId || undefined,
    status: status || undefined,
    unpaidBalance: unpaidOnly,
    page: 1,
    pageSize: orderPageSize,
  }), [fixedCustomerId, status, unpaidOnly])

  const reloadOrders = useCallback(() => {
    setReloadTick((tick) => tick + 1)
    onChanged?.()
  }, [onChanged])

  useEffect(() => {
    let active = true
    setLoading(true)
    setError(null)
    setItems([])
    setTotal(0)
    setPage(1)
    listOrders(requestParams)
      .then((result) => {
        if (!active) return
        setItems(result.items)
        setTotal(result.total)
      })
      .catch((err: unknown) => {
        if (!active) return
        if (err instanceof ApiError && err.status === 401) {
          goLogin()
          return
        }
        setError(err instanceof Error ? err.message : '订单加载失败')
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [goLogin, reloadTick, requestParams])

  useEffect(() => {
    if (fixedCustomerId) return
    let active = true
    fetchCustomerOptions()
      .then((result) => {
        if (active) setCustomers(result)
      })
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 401) {
          goLogin()
          return
        }
        if (active) setError(err instanceof Error ? err.message : '客户列表加载失败')
      })
    return () => {
      active = false
    }
  }, [fixedCustomerId, goLogin, reloadTick])

  useEffect(() => {
    if (!dialogOpen) return
    let active = true
    fetchPackageOptions(draft.backfill ? 'all' : 'active')
      .then((result) => {
        if (active) setPackages(result)
      })
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 401) {
          goLogin()
          return
        }
        if (active) setFormError(err instanceof Error ? err.message : '套系列表加载失败')
      })
    return () => {
      active = false
    }
  }, [dialogOpen, draft.backfill, goLogin])

  function openCreate() {
    setDraft(defaultDraft(fixedCustomerId))
    setSubmitted(false)
    setFormError(null)
    setDialogOpen(true)
  }

  async function loadMore() {
    const nextPage = page + 1
    setLoadingMore(true)
    setError(null)
    try {
      const result = await listOrders({ ...requestParams, page: nextPage })
      setItems((current) => [...current, ...result.items])
      setTotal(result.total)
      setPage(nextPage)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        goLogin()
        return
      }
      setError(err instanceof Error ? err.message : '加载更多失败')
    } finally {
      setLoadingMore(false)
    }
  }

  async function saveOrder() {
    setSubmitted(true)
    setFormError(null)
    const validation = validateDraft(draft, Boolean(fixedCustomerId))
    if (validation) {
      setFormError(validation)
      return
    }
    setSaving(true)
    try {
      await createOrder(toCreateBody(draft))
      notify('订单已创建')
      setDialogOpen(false)
      reloadOrders()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        goLogin()
        return
      }
      setFormError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function applyUpdate(order: OrderListItem, body: UpdateOrderBody, message: string) {
    setActionId(order.id)
    setError(null)
    try {
      await updateOrder(order.id, body)
      notify(message)
      reloadOrders()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        goLogin()
        return
      }
      setError(err instanceof Error ? err.message : '操作失败')
    } finally {
      setActionId(null)
    }
  }

  function requestProgress(order: OrderListItem, next: OrderStatusValue) {
    if (next === 'closed' && !order.balance_paid) {
      setError('完结前需要先标记尾款')
      return
    }
    if (next === 'shot' || next === 'delivered') {
      setError(null)
      setProgressTarget({ order, status: next, date: todayDate() })
      return
    }
    void applyUpdate(order, { status: next }, `订单已推进到${statusLabels[next]}`)
  }

  function confirmProgress() {
    if (!progressTarget) return
    if (!isValidDateInput(progressTarget.date)) {
      setError('请选择有效日期')
      return
    }
    const body: UpdateOrderBody = { status: progressTarget.status }
    if (progressTarget.status === 'shot') body.shot_at = dateToAPI(progressTarget.date)
    if (progressTarget.status === 'delivered') body.delivered_at = dateToAPI(progressTarget.date)
    void applyUpdate(progressTarget.order, body, `订单已推进到${statusLabels[progressTarget.status]}`)
    setProgressTarget(null)
  }

  function confirmCancel() {
    if (!cancelTarget) return
    const note = cancelNote.trim()
    void applyUpdate(cancelTarget, note ? { status: 'cancelled', note } : { status: 'cancelled' }, '订单已取消')
    setCancelTarget(null)
    setCancelNote('')
  }

  async function confirmDelete() {
    if (!deleteTarget) return
    setActionId(deleteTarget.id)
    setError(null)
    try {
      await deleteOrder(deleteTarget.id)
      notify('订单已删除')
      setDeleteTarget(null)
      reloadOrders()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        goLogin()
        return
      }
      setError(err instanceof Error ? err.message : '删除失败')
    } finally {
      setActionId(null)
    }
  }

  return (
    <div className="order-workspace">
      <div className="order-toolbar">
        <div className="chips">
          {statusFilters.map(([key, label]) => (
            <button
              key={key || 'all'}
              className={`chip${status === key ? ' active' : ''}`}
              type="button"
              onClick={() => setStatus(key)}
            >
              {label}
            </button>
          ))}
          <button
            className={`chip${unpaidOnly ? ' active' : ''}`}
            type="button"
            onClick={() => setUnpaidOnly((current) => !current)}
          >
            未收尾款
          </button>
        </div>
        <button className="btn btn-primary" type="button" onClick={openCreate}>＋ 新建订单</button>
      </div>

      {error && <div className="form-error">{error}</div>}

      {loading ? (
        <div className="empty">加载中</div>
      ) : items.length === 0 ? (
        <div className="empty">暂无订单</div>
      ) : (
        <>
          <div className="order-list">
            {items.map((order) => (
              <OrderRow
                key={order.id}
                order={order}
                fixedCustomer={Boolean(fixedCustomerId)}
                busy={actionId === order.id}
                onProgress={requestProgress}
                onCancel={(target) => {
                  setCancelTarget(target)
                  setCancelNote(target.note ?? '')
                }}
                onDelete={setDeleteTarget}
                onUpdate={(target, body, message) => { void applyUpdate(target, body, message) }}
              />
            ))}
          </div>
          {items.length < total && (
            <div className="load-more">
              <button className="btn" type="button" disabled={loadingMore} onClick={() => { void loadMore() }}>
                {loadingMore ? '加载中' : `加载更多（${items.length}/${total}）`}
              </button>
            </div>
          )}
        </>
      )}

      <OrderDialog
        open={dialogOpen}
        draft={draft}
        packages={packages}
        customers={customers}
        fixedCustomer={customer}
        submitted={submitted}
        saving={saving}
        formError={formError}
        onClose={() => setDialogOpen(false)}
        onDraft={setDraft}
        onSave={() => { void saveOrder() }}
      />

      {progressTarget && (
        <div className="overlay open" onClick={(event) => { if (event.target === event.currentTarget) setProgressTarget(null) }}>
          <section className="dialog" role="dialog" aria-modal="true" aria-labelledby="progressOrderTitle">
            <h2 id="progressOrderTitle">确认{statusLabels[progressTarget.status]}日期</h2>
            <p className="dialog-sub">{orderTitle(progressTarget.order)}</p>
            <div className="field">
              <label htmlFor="progressDate">{progressTarget.status === 'shot' ? '拍摄日期' : '交付日期'}</label>
              <input
                id="progressDate"
                className="input"
                type="date"
                value={progressTarget.date}
                onChange={(event) => setProgressTarget({ ...progressTarget, date: event.target.value })}
                autoFocus
              />
            </div>
            <div className="dialog-actions">
              <button className="btn" type="button" onClick={() => setProgressTarget(null)}>取消</button>
              <button className="btn btn-primary" type="button" disabled={!isValidDateInput(progressTarget.date)} onClick={confirmProgress}>确认推进</button>
            </div>
          </section>
        </div>
      )}

      {cancelTarget && (
        <div className="overlay open" onClick={(event) => { if (event.target === event.currentTarget) setCancelTarget(null) }}>
          <section className="dialog" role="dialog" aria-modal="true" aria-labelledby="cancelOrderTitle">
            <h2 id="cancelOrderTitle">取消订单</h2>
            <p className="dialog-sub">{orderTitle(cancelTarget)}</p>
            <div className="field">
              <label htmlFor="cancelNote">取消原因</label>
              <textarea
                id="cancelNote"
                className="input"
                value={cancelNote}
                onChange={(event) => setCancelNote(event.target.value)}
                autoFocus
              />
            </div>
            <div className="dialog-actions">
              <button className="btn" type="button" onClick={() => setCancelTarget(null)}>返回</button>
              <button className="btn btn-danger" type="button" onClick={confirmCancel}>确认取消</button>
            </div>
          </section>
        </div>
      )}

      {deleteTarget && (
        <div className="overlay open" onClick={(event) => { if (event.target === event.currentTarget) setDeleteTarget(null) }}>
          <section className="dialog" role="dialog" aria-modal="true" aria-labelledby="deleteOrderTitle">
            <h2 id="deleteOrderTitle">删除订单</h2>
            <p className="dialog-sub">
              {deleteTarget.status === 'closed'
                ? '删除完结订单会减少订单数与累计金额统计。'
                : '删除后该订单不会再出现在列表中。'}
            </p>
            <div className="dialog-actions">
              <button className="btn" type="button" onClick={() => setDeleteTarget(null)}>取消</button>
              <button
                className="btn btn-danger"
                type="button"
                disabled={actionId === deleteTarget.id}
                onClick={() => { void confirmDelete() }}
              >
                确认删除
              </button>
            </div>
          </section>
        </div>
      )}
    </div>
  )
}

function OrderRow({
  order,
  fixedCustomer,
  busy,
  onProgress,
  onCancel,
  onDelete,
  onUpdate,
}: {
  order: OrderListItem
  fixedCustomer: boolean
  busy: boolean
  onProgress(order: OrderListItem, next: OrderStatusValue): void
  onCancel(order: OrderListItem): void
  onDelete(order: OrderListItem): void
  onUpdate(order: OrderListItem, body: UpdateOrderBody, message: string): void
}) {
  const next = nextStatus(order)
  const canSkipDelivered = order.status === 'shot' || order.status === 'selected'
  const terminal = isTerminal(order.status)
  return (
    <article className={`order-card status-${order.status}`}>
      <div className="order-main">
        <div className="order-title-line">
          <span className={`badge ${statusBadge(order.status)}`}>{statusLabels[order.status]}</span>
          <h3>{orderTitle(order)}</h3>
          <span className="order-price">{formatPrice(order.price)}</span>
        </div>
        <div className="order-meta">
          {!fixedCustomer && (
            <Link to={`/customers/${order.customer_id}`}>{order.customer_display_name}</Link>
          )}
          {fixedCustomer && <span>{order.customer_display_name}</span>}
          <span>{order.package_name ?? '未选套系'}</span>
          <span>建单 {shortDate(order.created_at)}</span>
          {order.shot_at && <span>拍摄 {shortDate(order.shot_at)}</span>}
          {order.delivered_at && <span>交付 {shortDate(order.delivered_at)}</span>}
        </div>
        <div className="payment-flags">
          <span className={order.deposit_paid ? 'badge badge-success' : 'badge badge-warning'}>
            {order.deposit_paid ? '定金已收' : '定金未收'}
          </span>
          <span className={order.balance_paid ? 'badge badge-success' : 'badge badge-muted'}>
            {order.balance_paid ? '尾款已收' : '尾款未收'}
          </span>
        </div>
      </div>
      <div className="order-actions">
        {!terminal && !order.deposit_paid && (
          <button className="btn btn-sm" type="button" disabled={busy} onClick={() => onUpdate(order, { deposit_paid: true }, '已标记定金')}>
            标记定金
          </button>
        )}
        {!terminal && !order.balance_paid && (
          <button className="btn btn-sm" type="button" disabled={busy} onClick={() => onUpdate(order, { balance_paid: true }, '已标记尾款')}>
            标记尾款
          </button>
        )}
        {next && (
          <button className="btn btn-sm btn-primary" type="button" disabled={busy || (next === 'closed' && !order.balance_paid)} onClick={() => onProgress(order, next)}>
            推进到{statusLabels[next]}
          </button>
        )}
        {canSkipDelivered && (
          <button className="btn btn-sm" type="button" disabled={busy} onClick={() => onProgress(order, 'delivered')}>
            直接交付
          </button>
        )}
        {!terminal && (
          <button className="btn btn-sm btn-ghost" type="button" disabled={busy} onClick={() => onCancel(order)}>
            取消订单
          </button>
        )}
        {terminal && (
          <button className="btn btn-sm btn-danger-ghost" type="button" disabled={busy} onClick={() => onDelete(order)}>
            删除
          </button>
        )}
      </div>
    </article>
  )
}

function OrderDialog({
  open,
  draft,
  packages,
  customers,
  fixedCustomer,
  submitted,
  saving,
  formError,
  onClose,
  onDraft,
  onSave,
}: {
  open: boolean
  draft: OrderDraft
  packages: PackageOption[]
  customers: CustomerOption[]
  fixedCustomer?: FixedCustomer
  submitted: boolean
  saving: boolean
  formError: string | null
  onClose(): void
  onDraft(next: OrderDraft): void
  onSave(): void
}) {
  const needsShotAt = draft.backfill && reached(draft.status, 'shot') && draft.status !== 'cancelled'
  const needsDeliveredAt = draft.backfill && reached(draft.status, 'delivered') && draft.status !== 'cancelled'
  return (
    <div className={`overlay${open ? ' open' : ''}`} onClick={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <section className="dialog order-dialog" role="dialog" aria-modal="true" aria-labelledby="orderDialogTitle">
        <h2 id="orderDialogTitle">新建订单</h2>
        <div className="dialog-sub">订单会写入当前账号，客户与套系均由服务端校验</div>
        {formError && <div className="form-error">{formError}</div>}

        {fixedCustomer ? (
          <div className="field">
            <label>客户</label>
            <div className="readonly-field">{fixedCustomer.display_name}</div>
          </div>
        ) : (
          <div className={`field${submitted && !draft.customerId ? ' show-err' : ''}`}>
            <label htmlFor="orderCustomer">客户 *</label>
            <select
              id="orderCustomer"
              className={`input${submitted && !draft.customerId ? ' invalid' : ''}`}
              value={draft.customerId}
              onChange={(event) => onDraft({ ...draft, customerId: event.target.value })}
              autoFocus
            >
              <option value="">选择客户</option>
              {customers.map((item) => (
                <option key={item.id} value={item.id}>{item.display_name}</option>
              ))}
            </select>
            <div className="err">请选择客户</div>
          </div>
        )}

        <div className="field-row">
          <div className="field">
            <label htmlFor="orderPackage">套系</label>
            <select id="orderPackage" className="input" value={draft.packageId} onChange={(event) => onDraft({ ...draft, packageId: event.target.value })}>
              <option value="">不关联套系</option>
              {packages.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}{item.status === 'archived' ? '（已下架）' : ''}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="orderPrice">价格（元）</label>
            <input
              id="orderPrice"
              className="input"
              value={draft.priceYuan}
              inputMode="decimal"
              onChange={(event) => {
                if (isPackagePriceYuanInputAllowed(event.target.value)) onDraft({ ...draft, priceYuan: event.target.value })
              }}
              placeholder="680"
            />
          </div>
        </div>

        <div className="field">
          <label htmlFor="orderTitle">标题</label>
          <input id="orderTitle" className="input" value={draft.title} onChange={(event) => onDraft({ ...draft, title: event.target.value })} placeholder="如：春日外景写真" />
        </div>

        <label className="check-line">
          <input type="checkbox" checked={draft.backfill} onChange={(event) => onDraft({ ...draft, backfill: event.target.checked, status: event.target.checked ? 'delivered' : 'consulting', packageId: '' })} />
          补录历史订单
        </label>

        {draft.backfill && (
          <>
            <div className="field-row">
              <div className="field">
                <label htmlFor="orderStatus">状态</label>
                <select id="orderStatus" className="input" value={draft.status} onChange={(event) => onDraft({ ...draft, status: event.target.value as OrderStatusValue })}>
                  {backfillStatusOrder.map((item) => <option key={item} value={item}>{statusLabels[item]}</option>)}
                </select>
              </div>
              <div className="field">
                <label htmlFor="orderNote">备注</label>
                <input id="orderNote" className="input" value={draft.note} onChange={(event) => onDraft({ ...draft, note: event.target.value })} />
              </div>
            </div>
            <div className="field-row">
              <div className={`field${submitted && needsShotAt && !draft.shotDate ? ' show-err' : ''}`}>
                <label htmlFor="orderShotDate">拍摄日期{needsShotAt ? ' *' : ''}</label>
                <input id="orderShotDate" className={`input${submitted && needsShotAt && !draft.shotDate ? ' invalid' : ''}`} type="date" value={draft.shotDate} onChange={(event) => onDraft({ ...draft, shotDate: event.target.value })} />
                <div className="err">该状态需要拍摄日期</div>
              </div>
              <div className={`field${submitted && needsDeliveredAt && !draft.deliveredDate ? ' show-err' : ''}`}>
                <label htmlFor="orderDeliveredDate">交付日期{needsDeliveredAt ? ' *' : ''}</label>
                <input id="orderDeliveredDate" className={`input${submitted && needsDeliveredAt && !draft.deliveredDate ? ' invalid' : ''}`} type="date" value={draft.deliveredDate} onChange={(event) => onDraft({ ...draft, deliveredDate: event.target.value })} />
                <div className="err">该状态需要交付日期</div>
              </div>
            </div>
          </>
        )}

        <div className="check-grid">
          <label className="check-line">
            <input type="checkbox" checked={draft.depositPaid} onChange={(event) => onDraft({ ...draft, depositPaid: event.target.checked })} />
            定金已收
          </label>
          <label className="check-line">
            <input type="checkbox" checked={draft.balancePaid} onChange={(event) => onDraft({ ...draft, balancePaid: event.target.checked })} />
            尾款已收
          </label>
        </div>

        <div className="dialog-actions">
          <button className="btn" type="button" onClick={onClose}>取消</button>
          <button className="btn btn-primary" type="button" disabled={saving} onClick={onSave}>
            {saving ? '保存中' : '保存'}
          </button>
        </div>
      </section>
    </div>
  )
}

function defaultDraft(customerId: string): OrderDraft {
  return {
    customerId,
    packageId: '',
    title: '',
    priceYuan: '',
    backfill: false,
    status: 'consulting',
    shotDate: '',
    deliveredDate: '',
    depositPaid: false,
    balancePaid: false,
    note: '',
  }
}

function validateDraft(draft: OrderDraft, fixedCustomer: boolean): string | null {
  if (!fixedCustomer && !draft.customerId) return '请选择客户'
  if (draft.priceYuan.trim()) {
    const priceError = validatePackagePriceYuan(draft.priceYuan)
    if (priceError) return priceError.replace('基础价', '价格')
  }
  if (draft.backfill && draft.status !== 'cancelled') {
    if (reached(draft.status, 'shot') && !draft.shotDate) return '该状态需要拍摄日期'
    if (reached(draft.status, 'delivered') && !draft.deliveredDate) return '该状态需要交付日期'
  }
  if (draft.backfill && draft.status === 'consulting') return '请选择补录状态'
  if (draft.backfill && draft.status === 'closed' && !draft.balancePaid) return '完结订单必须标记尾款已收'
  return null
}

async function fetchCustomerOptions(): Promise<CustomerOption[]> {
  return fetchAllPages((page) => listCustomers({ page, pageSize: optionPageSize }))
}

async function fetchPackageOptions(status: PackageListStatus): Promise<PackageOption[]> {
  return fetchAllPages((page) => listPackages({ status, page, pageSize: optionPageSize }))
}

async function fetchAllPages<T>(loadPage: (page: number) => Promise<{ items: T[]; total: number }>): Promise<T[]> {
  const all: T[] = []
  for (let page = 1; ; page += 1) {
    const result = await loadPage(page)
    all.push(...result.items)
    if (all.length >= result.total || result.items.length === 0) return all
  }
}

function toCreateBody(draft: OrderDraft): CreateOrderBody {
  const body: CreateOrderBody = { customer_id: draft.customerId }
  if (draft.packageId) body.package_id = draft.packageId
  if (draft.title.trim()) body.title = draft.title.trim()
  if (draft.priceYuan.trim()) body.price = packagePriceYuanToCents(draft.priceYuan)
  if (draft.depositPaid) body.deposit_paid = true
  if (draft.balancePaid) body.balance_paid = true
  if (draft.note.trim()) body.note = draft.note.trim()
  if (draft.backfill) {
    body.status = draft.status
    if (draft.shotDate) body.shot_at = dateToAPI(draft.shotDate)
    if (draft.deliveredDate) body.delivered_at = dateToAPI(draft.deliveredDate)
  }
  return body
}

function nextStatus(order: OrderListItem): OrderStatusValue | null {
  if (order.status === 'closed' || order.status === 'cancelled') return null
  if (order.status === 'delivered' && !order.balance_paid) return null
  const index = statusOrder.indexOf(order.status)
  if (index < 0 || index >= statusOrder.indexOf('closed')) return null
  return statusOrder[index + 1]
}

function reached(status: OrderStatusValue, target: OrderStatusValue): boolean {
  return statusOrder.indexOf(status) >= statusOrder.indexOf(target)
}

function isTerminal(status: OrderStatusValue): boolean {
  return status === 'closed' || status === 'cancelled'
}

function statusBadge(status: OrderStatusValue): string {
  if (status === 'closed' || status === 'delivered') return 'badge-success'
  if (status === 'cancelled') return 'badge-danger'
  if (status === 'retouching') return 'badge-warning'
  if (status === 'scheduled' || status === 'shot' || status === 'selected') return 'badge-accent'
  return 'badge-muted'
}

function orderTitle(order: OrderListItem): string {
  return order.title ?? order.package_name ?? '未命名订单'
}

function formatPrice(cents: number | undefined): string {
  if (cents == null) return '未报价'
  return `¥${(cents / 100).toLocaleString('zh-CN', { maximumFractionDigits: 0 })}`
}

function shortDate(value: string | undefined): string {
  return value ? value.slice(0, 10) : '暂无'
}

function todayDate(): string {
  const now = new Date()
  const local = new Date(now.getTime() - now.getTimezoneOffset() * 60000)
  return local.toISOString().slice(0, 10)
}

function dateToAPI(date: string): string {
  return new Date(`${date}T12:00:00+08:00`).toISOString()
}

function isValidDateInput(date: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return false
  return !Number.isNaN(new Date(`${date}T12:00:00+08:00`).getTime())
}
