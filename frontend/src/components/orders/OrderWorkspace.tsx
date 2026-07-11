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
import { useFocusTrap } from '../useFocusTrap'
import {
	accountDateAtNoonToInstant,
	accountToday,
	instantToLocalDateTime,
	isValidAccountDate,
} from '../schedule/timezone'
import {
  clearPendingSchedule,
  clearScheduleDraft,
  pendingScheduleExpired,
  readPendingSchedule,
  scheduleAttemptKey,
} from '../schedule/journal'
import type { PendingScheduleFlow, ScheduleDraft } from '../schedule/journal'
import { backfillTimestampFields } from '../schedule/backfill'
import {
  BackfillPersistenceError,
  claimPendingFlow,
  completeBackfillOrder,
  orderInUseScheduleAction,
  pendingAfterDeterministicFailure,
  readScheduleBackfillContext,
  scheduleCreateFailureKind,
} from '../schedule/flow'
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
const scheduleDraftStatusOrder: OrderStatusValue[] = [
  'scheduled', 'shot', 'selected', 'retouching', 'delivered', 'closed',
]

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
  focusOrderId,
  scheduleDraftId,
  scheduleMode,
}: {
  customer?: FixedCustomer
  onChanged?: () => void
  focusOrderId?: string
  scheduleDraftId?: string
  scheduleMode?: string
}) {
  const navigate = useNavigate()
	const { notify, timezone } = useShell()
  const fixedCustomerId = customer?.id ?? ''
  const [items, setItems] = useState<OrderListItem[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<StatusFilter>('')
  const [unpaidOnly, setUnpaidOnly] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [errorAction, setErrorAction] = useState<{ href: string; label: string } | null>(null)
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
  const [focusMissing, setFocusMissing] = useState(false)
  const [scheduleContext, setScheduleContext] = useState<ScheduleDraft | null>(null)
  const [schedulePending, setSchedulePending] = useState<PendingScheduleFlow | null>(null)
  const [failedSchedulePending, setFailedSchedulePending] = useState<PendingScheduleFlow | null>(null)
  const [knownOrderInMemory, setKnownOrderInMemory] = useState<string | null>(null)
  const [scheduleIdempotencyConflict, setScheduleIdempotencyConflict] = useState(false)

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
    setErrorAction(null)
    setItems([])
    setTotal(0)
    setPage(1)
    const load = focusOrderId
      ? loadThroughOrder(focusOrderId, fixedCustomerId)
      : listOrders(requestParams).then((result) => ({ ...result, found: true, page: 1 }))
    load
      .then((result) => {
        if (!active) return
        setItems(result.items)
        setTotal(result.total)
        setPage(result.page)
        setFocusMissing(!result.found)
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
  }, [fixedCustomerId, focusOrderId, goLogin, reloadTick, requestParams])

  useEffect(() => {
    if (!focusOrderId) return
    setStatus('')
    setUnpaidOnly(false)
  }, [focusOrderId])

  useEffect(() => {
    if (!focusOrderId || focusMissing || loading) return
    const target = document.querySelector<HTMLElement>(`[data-order-id="${CSS.escape(focusOrderId)}"]`)
    target?.scrollIntoView({ block: 'center' })
    target?.focus({ preventScroll: true })
  }, [focusMissing, focusOrderId, items, loading])

  useEffect(() => {
    if (!scheduleDraftId || scheduleMode !== 'backfill') return
    try {
      const { draft: context, pending: matchingPending } = readScheduleBackfillContext(scheduleDraftId)
      if (!context || (fixedCustomerId && context.customer_id !== fixedCustomerId)) {
        setError('排期补录草稿已过期或不属于当前客户')
        return
      }
      if (!timezone) {
        setError('账号时区不可用，暂不能恢复历史补录')
        return
      }
      setScheduleContext(context)
      setSchedulePending(matchingPending)
      setFailedSchedulePending(null)
      setScheduleIdempotencyConflict(false)
      const localStart = instantToLocalDateTime(context.start_at, timezone)
      const next = defaultDraft(fixedCustomerId || context.customer_id)
      next.backfill = true
      next.status = 'shot'
      next.shotDate = localStart.date
      next.note = context.note ?? ''
      if (matchingPending) applyCreateBodyToDraft(next, matchingPending.normalized_body as CreateOrderBody, timezone)
      setDraft(next)
      setSubmitted(false)
      setFormError(matchingPending && pendingScheduleExpired(matchingPending) ? '恢复记录已超过 24 小时，请人工核对后再放弃' : null)
      setDialogOpen(true)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '排期补录草稿读取失败')
    }
  }, [fixedCustomerId, scheduleDraftId, scheduleMode, timezone])

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
    setErrorAction(null)
    setDialogOpen(true)
  }

  async function loadMore() {
    const nextPage = page + 1
    setLoadingMore(true)
    setError(null)
    setErrorAction(null)
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
    if (draft.backfill && !timezone) {
      setFormError('账号时区不可用，暂不能补录历史日期')
      return
    }
    if (scheduleContext && !scheduleDraftStatusOrder.includes(draft.status)) {
      setFormError('排期补录只允许 scheduled 到 closed 的六种状态')
      return
    }
    setSaving(true)
    try {
      const body = toCreateBody(draft, timezone)
      if (scheduleContext) {
        await saveScheduleBackfill(body)
        return
      }
      await createOrder(body)
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

  async function saveScheduleBackfill(body: CreateOrderBody) {
    if (!scheduleContext || !timezone) return
    let pending = schedulePending
    if (!pending) {
      pending = failedSchedulePending
        ? {
            ...pendingAfterDeterministicFailure(failedSchedulePending, body),
            created_at: new Date().toISOString(),
          }
        : {
            flow_id: scheduleContext.draft_id,
            phase: 'backfill_order',
            source_draft_id: scheduleContext.draft_id,
            normalized_body: body,
            attempt_key: scheduleAttemptKey(scheduleContext.draft_id, 'order', 1),
            known_customer_id: scheduleContext.customer_id,
            created_at: new Date().toISOString(),
          }
      const existing = claimPendingFlow(pending)
      if (existing) {
        if (existing.phase === 'backfill_order' && existing.source_draft_id === scheduleContext.draft_id) {
          setSchedulePending(existing)
        }
        setFormError('当前标签页已有待恢复排期，请先确认原流程')
        return
      }
      setSchedulePending(pending)
      setFailedSchedulePending(null)
    }
    if (pendingScheduleExpired(pending)) {
      setFormError('恢复记录已超过 24 小时，禁止自动重放')
      return
    }
    try {
      const completed = await completeBackfillOrder(
        pending,
        scheduleContext,
        createOrder,
        knownOrderInMemory ?? undefined,
      )
      setSchedulePending(null)
      setFailedSchedulePending(null)
      setKnownOrderInMemory(null)
      setScheduleIdempotencyConflict(false)
      setScheduleContext(completed.draft)
      setDialogOpen(false)
      reloadOrders()
      const separator = scheduleContext.return_to.includes('?') ? '&' : '?'
      navigate(`${scheduleContext.return_to}${separator}schedule_draft=${scheduleContext.draft_id}`)
    } catch (reason) {
      if (reason instanceof BackfillPersistenceError) {
        setKnownOrderInMemory(reason.knownOrderID)
        setSchedulePending(readPendingSchedule() ?? pending)
        setFormError('订单已创建，本地收口失败；重试只会写本地记录')
        return
      }
      const failureKind = scheduleCreateFailureKind(
        reason instanceof ApiError ? reason : null,
        false,
      )
      if (failureKind === 'idempotency_conflict') {
        setSchedulePending(readPendingSchedule() ?? pending)
        setScheduleIdempotencyConflict(true)
        setFormError('原请求 key 已绑定其他成功请求，禁止自动重放、修改请求或换 key；请先人工核对客户订单')
        return
      }
      if (failureKind === 'deterministic' && reason instanceof ApiError) {
        clearPendingSchedule()
        setSchedulePending(null)
        setFailedSchedulePending(pending)
        setScheduleIdempotencyConflict(false)
        setFormError(reason.message)
        return
      }
      setFormError('补录结果未知，必须保留原 body/key 再次确认')
    }
  }

  function abandonScheduleDraft() {
    if (!scheduleContext || schedulePending) return
    try {
      clearScheduleDraft(scheduleContext.draft_id)
      setScheduleContext(null)
      setFailedSchedulePending(null)
      setDraft((current) => ({ ...current, status: 'delivered' }))
      setFormError(null)
    } catch (reason) {
      setFormError(reason instanceof Error ? reason.message : '放弃排期失败')
    }
  }

  function abandonExpiredScheduleRecovery() {
    if (!scheduleContext || !schedulePending || !pendingScheduleExpired(schedulePending)) return
    try {
      clearPendingSchedule()
      clearScheduleDraft(scheduleContext.draft_id)
      const customerID = schedulePending.known_customer_id ?? scheduleContext.customer_id
      setSchedulePending(null)
      setScheduleContext(null)
      setFailedSchedulePending(null)
      setKnownOrderInMemory(null)
      setScheduleIdempotencyConflict(false)
      setFormError(null)
      setDialogOpen(false)
      navigate(`/customers/${customerID}?tab=orders`, { replace: true })
    } catch (reason) {
      setFormError(reason instanceof Error ? reason.message : '放弃恢复记录失败')
    }
  }

  async function applyUpdate(order: OrderListItem, body: UpdateOrderBody, message: string) {
    setActionId(order.id)
    setError(null)
    setErrorAction(null)
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
    setErrorAction(null)
    if (next === 'closed' && !order.balance_paid) {
      setError('完结前需要先标记尾款')
      return
    }
		if (next === 'shot' || next === 'delivered') {
			if (!timezone) {
				setError('账号时区不可用，暂不能写入日期')
				return
			}
			setError(null)
			setProgressTarget({ order, status: next, date: accountToday(timezone) })
      return
    }
    void applyUpdate(order, { status: next }, `订单已推进到${statusLabels[next]}`)
  }

	function confirmProgress() {
		if (!progressTarget) return
		if (!timezone || !isValidAccountDate(progressTarget.date, timezone)) {
      setError('请选择有效日期')
      return
    }
    const body: UpdateOrderBody = { status: progressTarget.status }
		if (progressTarget.status === 'shot') body.shot_at = accountDateAtNoonToInstant(progressTarget.date, timezone)
		if (progressTarget.status === 'delivered') body.delivered_at = accountDateAtNoonToInstant(progressTarget.date, timezone)
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
    setErrorAction(null)
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
      setErrorAction(err instanceof ApiError ? orderInUseScheduleAction(err, timezone) : null)
      setError(err instanceof Error ? err.message : '删除失败')
    } finally {
      setActionId(null)
    }
  }

  const schedulePendingIsExpired = Boolean(schedulePending && pendingScheduleExpired(schedulePending))
  const scheduleRecoveryCustomerID = schedulePending?.known_customer_id ?? scheduleContext?.customer_id
  const scheduleRecoveryOrderID = schedulePending?.known_order_id ?? knownOrderInMemory
  const scheduleRecoveryAction = (schedulePendingIsExpired || scheduleIdempotencyConflict) && scheduleRecoveryCustomerID
    ? {
        href: scheduleRecoveryOrderID
          ? `/customers/${scheduleRecoveryCustomerID}?tab=orders&order=${scheduleRecoveryOrderID}`
          : `/customers/${scheduleRecoveryCustomerID}?tab=orders`,
        label: scheduleRecoveryOrderID ? '查看已知订单' : '前往客户订单核对',
      }
    : null

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

	      {error && (
          <div className="form-error">
            {error}
            {errorAction && <> <Link to={errorAction.href}>{errorAction.label}</Link></>}
          </div>
        )}
	      {focusMissing && <div className="form-error">目标订单已不存在，当前客户上下文仍保留。</div>}

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
                focused={focusOrderId === order.id}
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
			timezone={timezone}
			scheduleDraft={Boolean(scheduleContext)}
			schedulePending={Boolean(schedulePending)}
			schedulePendingExpired={schedulePendingIsExpired}
			scheduleIdempotencyConflict={scheduleIdempotencyConflict}
			scheduleRecoveryAction={scheduleRecoveryAction}
	        onClose={() => {
				if (schedulePending) {
					setFormError('补录请求已发出，必须先确认结果，不能关闭恢复记录')
					return
				}
				setDialogOpen(false)
			}}
	        onDraft={setDraft}
	        onSave={() => { void saveOrder() }}
			onAbandonSchedule={abandonScheduleDraft}
			onAbandonExpiredSchedule={abandonExpiredScheduleRecovery}
      />

      {progressTarget && (
        <div className="overlay open" onClick={(event) => { if (event.target === event.currentTarget) setProgressTarget(null) }}>
          <section className="dialog" role="dialog" aria-modal="true" aria-labelledby="progressOrderTitle">
            <h2 id="progressOrderTitle">确认{statusLabels[progressTarget.status]}日期</h2>
			<p className="dialog-sub">{orderTitle(progressTarget.order)}</p>
			<div className="hint">账号时区：{timezone}</div>
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
				<button className="btn btn-primary" type="button" disabled={!timezone || !isValidAccountDate(progressTarget.date, timezone)} onClick={confirmProgress}>确认推进</button>
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
  focused,
  fixedCustomer,
  busy,
  onProgress,
  onCancel,
  onDelete,
  onUpdate,
}: {
  order: OrderListItem
  focused: boolean
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
	    <article className={`order-card status-${order.status}`} data-order-id={order.id} tabIndex={focused ? -1 : undefined}>
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
	timezone,
	scheduleDraft,
	schedulePending,
  schedulePendingExpired,
  scheduleIdempotencyConflict,
  scheduleRecoveryAction,
  onClose,
  onDraft,
  onSave,
	onAbandonSchedule,
	onAbandonExpiredSchedule,
}: {
  open: boolean
  draft: OrderDraft
  packages: PackageOption[]
  customers: CustomerOption[]
  fixedCustomer?: FixedCustomer
  submitted: boolean
  saving: boolean
	formError: string | null
	timezone: string | null
	scheduleDraft: boolean
	schedulePending: boolean
  schedulePendingExpired: boolean
  scheduleIdempotencyConflict: boolean
  scheduleRecoveryAction: { href: string; label: string } | null
  onClose(): void
  onDraft(next: OrderDraft): void
  onSave(): void
	onAbandonSchedule(): void
	onAbandonExpiredSchedule(): void
}) {
  const needsShotAt = draft.backfill && reached(draft.status, 'shot') && draft.status !== 'cancelled'
  const needsDeliveredAt = draft.backfill && reached(draft.status, 'delivered') && draft.status !== 'cancelled'
  const dialogRef = useFocusTrap<HTMLElement>(open, onClose, !saving)
  return (
    <div className={`overlay${open ? ' open' : ''}`} onClick={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <section ref={dialogRef} className="dialog order-dialog" role="dialog" aria-modal="true" aria-labelledby="orderDialogTitle" tabIndex={-1} autoFocus>
	        <h2 id="orderDialogTitle">{scheduleDraft ? '补录历史订单后返回排期' : '新建订单'}</h2>
	        <div className="dialog-sub">{scheduleDraft ? '仅可选择能继续历史排期的订单状态' : '订单会写入当前账号，客户与套系均由服务端校验'}</div>
        {formError && <div className="form-error">{formError}</div>}
        {schedulePending && (schedulePendingExpired || scheduleIdempotencyConflict) && (
          <div className="schedule-recovery">
            <div className="conflict-tip">
              <strong>{schedulePendingExpired ? '恢复记录已超过 24 小时' : '幂等记录与当前请求不一致'}</strong>
              <div>{schedulePendingExpired
                ? '自动重放已禁用。请先核对客户订单，再显式放弃恢复记录。'
                : '原 key 已绑定其他成功请求，当前记录禁止自动重放、修改 body 或换 key。请先核对客户订单。'}</div>
            </div>
            {scheduleRecoveryAction && (
              <div className="schedule-resource-links">
                <Link className="btn btn-sm" to={scheduleRecoveryAction.href} target="_blank" rel="noreferrer">{scheduleRecoveryAction.label}</Link>
              </div>
            )}
          </div>
        )}

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
	          <input type="checkbox" disabled={scheduleDraft} checked={draft.backfill} onChange={(event) => onDraft({ ...draft, backfill: event.target.checked, status: event.target.checked ? 'delivered' : 'consulting', packageId: '' })} />
	          补录历史订单
        </label>

		{draft.backfill && (
			<>
				<div className="hint">账号时区：{timezone ?? '不可用'}</div>
            <div className="field-row">
              <div className="field">
                <label htmlFor="orderStatus">状态</label>
                <select id="orderStatus" className="input" value={draft.status} onChange={(event) => onDraft({ ...draft, status: event.target.value as OrderStatusValue })}>
	                  {(scheduleDraft ? scheduleDraftStatusOrder : backfillStatusOrder).map((item) => <option key={item} value={item}>{statusLabels[item]}</option>)}
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
			{scheduleDraft && !schedulePending && <button className="btn btn-danger-ghost" type="button" onClick={onAbandonSchedule}>放弃本次排期</button>}
			{scheduleDraft && schedulePendingExpired && <button className="btn btn-danger-ghost" type="button" disabled={saving} onClick={onAbandonExpiredSchedule}>已人工核对，放弃恢复记录</button>}
		          <button className="btn" type="button" onClick={onClose}>{scheduleDraft ? '返回' : '取消'}</button>
          <button className="btn btn-primary" type="button" disabled={saving || schedulePendingExpired || scheduleIdempotencyConflict} onClick={onSave}>
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

async function loadThroughOrder(orderID: string, customerID: string): Promise<{
  items: OrderListItem[]
  total: number
  page: number
  found: boolean
}> {
  const items: OrderListItem[] = []
  for (let page = 1; ; page += 1) {
    const result = await listOrders({
      customerId: customerID || undefined,
      page,
      pageSize: orderPageSize,
    })
    items.push(...result.items)
    if (items.some((item) => item.id === orderID)) {
      return { items, total: result.total, page, found: true }
    }
    if (items.length >= result.total || result.items.length === 0) {
      return { items, total: result.total, page, found: false }
    }
  }
}

function applyCreateBodyToDraft(draft: OrderDraft, body: CreateOrderBody, timezone: string) {
  draft.customerId = body.customer_id
  draft.packageId = body.package_id ?? ''
  draft.title = body.title ?? ''
  draft.priceYuan = body.price == null ? '' : String(body.price / 100)
  draft.backfill = body.creation_mode === 'backfill'
  draft.status = (body.status ?? 'consulting') as OrderStatusValue
  draft.depositPaid = body.deposit_paid ?? false
  draft.balancePaid = body.balance_paid ?? false
  draft.note = body.note ?? ''
  draft.shotDate = body.shot_at ? instantToLocalDateTime(body.shot_at, timezone).date : ''
  draft.deliveredDate = body.delivered_at ? instantToLocalDateTime(body.delivered_at, timezone).date : ''
}

function toCreateBody(draft: OrderDraft, timezone: string | null): CreateOrderBody {
  const body: CreateOrderBody = {
    creation_mode: draft.backfill ? 'backfill' : 'new',
    customer_id: draft.customerId,
  }
  if (draft.packageId) body.package_id = draft.packageId
  if (draft.title.trim()) body.title = draft.title.trim()
  if (draft.priceYuan.trim()) body.price = packagePriceYuanToCents(draft.priceYuan)
  if (draft.depositPaid) body.deposit_paid = true
  if (draft.balancePaid) body.balance_paid = true
  if (draft.note.trim()) body.note = draft.note.trim()
		if (draft.backfill) {
			if (!timezone) throw new Error('account timezone required for backfill')
			body.status = draft.status
			Object.assign(body, backfillTimestampFields(
				draft.status,
				draft.shotDate ? accountDateAtNoonToInstant(draft.shotDate, timezone) : undefined,
				draft.deliveredDate ? accountDateAtNoonToInstant(draft.deliveredDate, timezone) : undefined,
			))
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
