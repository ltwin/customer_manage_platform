import { Link, useNavigate } from 'react-router-dom'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  BanknoteArrowUp,
  Check,
  Coins,
  MoreHorizontal,
  Plus,
  Trash2,
  TruckIcon,
  Wallet,
  XCircle,
} from 'lucide-react'
import {
  ApiError,
  createOrder,
  deleteOrder,
  listOrders,
  listPackages,
  updateOrder,
} from '../../api/client'
import type {
  CreateOrderBody,
  OrderListItem,
  OrderStatus,
  PackageListStatus,
  PackageListResponse,
  UpdateOrderBody,
} from '../../api/client'
import { useShell } from '../shellContext'
import PlanningSummaryLink from '../PlanningSummaryLink'
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
import CustomerAvatar from '../customers/CustomerAvatar'
import CustomerPicker from '../customers/CustomerPicker'
import StateNotice from '../StateNotice'
import {
  buildPaymentPatch,
  hydratePaymentDraft,
  validatePaymentDraft,
  yuanInputToCents,
  type PaymentDraft,
} from './paymentDraft'
import {
  beginPageRead,
  completePageRead,
  failPageRead,
  pageReadPresentation,
  readyPageData,
  type PageReadState,
} from '../pageReadState'

type OrderStatusValue = NonNullable<OrderStatus> & string
type StatusFilter = OrderStatusValue | ''
type PackageOption = PackageListResponse['items'][number]

interface FixedCustomer {
  id?: string
  display_name: string
  status: string
  avatar_revision: string
  avatar_url?: string
}

interface OrderDraft {
  customerId: string
  packageId: string
  title: string
  priceYuan: string
  amountPaidYuan: string
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
const emptyOrderItems: OrderListItem[] = []

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

	/** 主流程状态（互斥单选）；「取消」是旁支，单独放 */
	const pipelineStatusFilters: Array<[StatusFilter, string]> = [
	  ['', '全部'],
	  ['consulting', '咨询'],
	  ['scheduled', '定档'],
	  ['shot', '拍摄后'],
	  ['selected', '已选片'],
	  ['retouching', '精修中'],
	  ['delivered', '已交付'],
	  ['closed', '完结'],
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
  const [readState, setReadState] = useState<PageReadState<{
    items: OrderListItem[]
    total: number
    page: number
  }>>({ kind: 'loading', message: '正在加载订单' })
  const [status, setStatus] = useState<StatusFilter>('')
  const [unpaidOnly, setUnpaidOnly] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [errorAction, setErrorAction] = useState<{ href: string; label: string } | null>(null)
  const [reloadTick, setReloadTick] = useState(0)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [draft, setDraft] = useState<OrderDraft>(() => defaultDraft(fixedCustomerId))
  const [submitted, setSubmitted] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [packages, setPackages] = useState<PackageOption[]>([])
  const [actionId, setActionId] = useState<string | null>(null)
  const [progressTarget, setProgressTarget] = useState<ProgressTarget | null>(null)
  const [paymentTarget, setPaymentTarget] = useState<{ order: OrderListItem; draft: PaymentDraft; error: string | null } | null>(null)
  const [cancelTarget, setCancelTarget] = useState<OrderListItem | null>(null)
  const [cancelNote, setCancelNote] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<OrderListItem | null>(null)
  const [focusMissing, setFocusMissing] = useState(false)
  const [scheduleContext, setScheduleContext] = useState<ScheduleDraft | null>(null)
  const [schedulePending, setSchedulePending] = useState<PendingScheduleFlow | null>(null)
  const [failedSchedulePending, setFailedSchedulePending] = useState<PendingScheduleFlow | null>(null)
  const [knownOrderInMemory, setKnownOrderInMemory] = useState<string | null>(null)
  const [scheduleIdempotencyConflict, setScheduleIdempotencyConflict] = useState(false)
  const loadedRequestKeyRef = useRef('')
  const presentation = pageReadPresentation(readState)
  const readData = readyPageData(readState)
  const items = readData?.items ?? emptyOrderItems
  const total = readData?.total ?? 0
  const page = readData?.page ?? 1
  const loading = readState.kind === 'loading'

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
    const requestKey = JSON.stringify({ focusOrderId, requestParams })
    const preserveReady = loadedRequestKeyRef.current === requestKey
    loadedRequestKeyRef.current = requestKey
    setReadState((current) => beginPageRead(current, '正在加载订单', preserveReady))
    setActionError(null)
    setErrorAction(null)
    const load = focusOrderId
      ? loadThroughOrder(focusOrderId, fixedCustomerId)
      : listOrders(requestParams).then((result) => ({ ...result, found: true, page: 1 }))
    load
      .then((result) => {
        if (!active) return
        setReadState(completePageRead(
          { items: result.items, total: result.total, page: result.page },
          result.items.length === 0,
          '当前筛选下暂无订单',
        ))
        setFocusMissing(!result.found)
      })
      .catch((err: unknown) => {
        if (!active) return
        if (err instanceof ApiError && err.status === 401) {
          setReadState({ kind: 'unauthorized' })
          goLogin()
          return
        }
        setReadState((current) => failPageRead(
          current,
          err instanceof Error ? err.message : '订单加载失败',
          () => setReloadTick((tick) => tick + 1),
        ))
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
        setActionError('排期补录草稿已过期或不属于当前客户')
        return
      }
      if (!timezone) {
        setActionError('账号时区不可用，暂不能恢复历史补录')
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
      setActionError(reason instanceof Error ? reason.message : '排期补录草稿读取失败')
    }
  }, [fixedCustomerId, scheduleDraftId, scheduleMode, timezone])

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
    setActionError(null)
    setErrorAction(null)
    try {
      const result = await listOrders({ ...requestParams, page: nextPage })
      setReadState((current) => {
        const data = readyPageData(current)
        if (!data) return current
        return completePageRead({
          items: [...data.items, ...result.items],
          total: result.total,
          page: nextPage,
        }, false, '')
      })
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        goLogin()
        return
      }
      setReadState((current) => failPageRead(
        current,
        err instanceof Error ? err.message : '加载更多失败，显示上次成功数据',
        () => { void loadMore() },
      ))
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
    setActionError(null)
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
      setActionError(err instanceof Error ? err.message : '操作失败')
    } finally {
      setActionId(null)
    }
  }

	function requestProgress(order: OrderListItem, next: OrderStatusValue) {
    setErrorAction(null)
    if (next === 'closed' && !order.balance_paid) {
      setActionError('完结前需要先标记尾款')
      return
    }
		if (next === 'shot' || next === 'delivered') {
			if (!timezone) {
				setActionError('账号时区不可用，暂不能写入日期')
				return
			}
			setActionError(null)
			setProgressTarget({ order, status: next, date: accountToday(timezone) })
      return
    }
    void applyUpdate(order, { status: next }, `订单已推进到${statusLabels[next]}`)
  }

	function confirmProgress() {
		if (!progressTarget) return
		if (!timezone || !isValidAccountDate(progressTarget.date, timezone)) {
			setActionError('请选择有效日期')
			return
		}
		const body: UpdateOrderBody = { status: progressTarget.status }
		if (progressTarget.status === 'shot') body.shot_at = accountDateAtNoonToInstant(progressTarget.date, timezone)
		if (progressTarget.status === 'delivered') body.delivered_at = accountDateAtNoonToInstant(progressTarget.date, timezone)
    void applyUpdate(progressTarget.order, body, `订单已推进到${statusLabels[progressTarget.status]}`)
    setProgressTarget(null)
  }

	function openPayment(order: OrderListItem) {
		setPaymentTarget({ order, draft: hydratePaymentDraft(order, timezone), error: null })
	}

	function confirmPayment() {
		if (!paymentTarget) return
		const validation = validatePaymentDraft(paymentTarget.draft, paymentTarget.order)
		if (validation) {
			setPaymentTarget({ ...paymentTarget, error: validation })
			return
		}
		const patch = buildPaymentPatch(paymentTarget.draft, paymentTarget.order, timezone)
		if (Object.keys(patch).length === 0) {
			setPaymentTarget({ ...paymentTarget, error: '没有需要保存的修改' })
			return
		}
		const order = paymentTarget.order
		setPaymentTarget(null)
		void applyUpdate(order, patch, '收款与应交日已更新')
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
    setActionError(null)
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
      setActionError(err instanceof Error ? err.message : '删除失败')
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

  const statusFilterLabel = status === ''
    ? null
    : status === 'cancelled'
      ? '取消'
      : statusLabels[status]
  const hasActiveFilters = status !== '' || unpaidOnly

  function clearFilters() {
    setStatus('')
    setUnpaidOnly(false)
  }

	  return (
    <div className="order-workspace">
      <div className="order-toolbar">
        <div className="order-filters">
          <div className="order-filter-row">
            <div className="status-track" role="group" aria-label="按状态筛选">
              {pipelineStatusFilters.map(([key, label]) => (
                <button
                  key={key || 'all'}
                  className={`chip${status === key ? ' active' : ''}`}
                  type="button"
                  aria-pressed={status === key}
                  onClick={() => setStatus(key)}
                >
                  {label}
                </button>
              ))}
            </div>
            <button
              className={`chip chip-side${status === 'cancelled' ? ' active' : ''}`}
              type="button"
              aria-pressed={status === 'cancelled'}
              onClick={() => setStatus('cancelled')}
            >
              取消
            </button>
            <button
              className={`filter-toggle${unpaidOnly ? ' active' : ''}`}
              type="button"
              aria-pressed={unpaidOnly}
              title="可与状态筛选叠加"
              onClick={() => setUnpaidOnly((current) => !current)}
            >
              <Wallet aria-hidden="true" strokeWidth={2.1} />
              <span>未收尾款</span>
            </button>
          </div>
          {hasActiveFilters && (
            <div className="filter-summary" aria-live="polite">
              <span className="filter-summary-label">筛选</span>
              {statusFilterLabel && (
                <button
                  className="filter-tag"
                  type="button"
                  onClick={() => setStatus('')}
                  title="清除状态筛选"
                >
                  {statusFilterLabel}
                  <XCircle aria-hidden="true" strokeWidth={2} />
                </button>
              )}
              {unpaidOnly && (
                <button
                  className="filter-tag filter-tag-warning"
                  type="button"
                  onClick={() => setUnpaidOnly(false)}
                  title="清除尾款筛选"
                >
                  未收尾款
                  <XCircle aria-hidden="true" strokeWidth={2} />
                </button>
              )}
              <button className="filter-clear" type="button" onClick={clearFilters}>
                清除
              </button>
            </div>
          )}
        </div>
        <button className="btn btn-primary" type="button" onClick={openCreate}>
          <Plus aria-hidden="true" strokeWidth={2.2} />
          新建订单
        </button>
      </div>

	      {actionError && (
          <div className="form-error" role="alert">
            {actionError}
            {errorAction && <> <Link to={errorAction.href}>{errorAction.label}</Link></>}
          </div>
        )}
	      {focusMissing && <div className="form-error">目标订单已不存在，当前客户上下文仍保留。</div>}
	      {presentation.notice && <StateNotice {...presentation.notice} />}

      {presentation.showReadyData && (
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
                onPayment={openPayment}
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

      {paymentTarget && (
        <div className="overlay open" onClick={(event) => { if (event.target === event.currentTarget) setPaymentTarget(null) }}>
          <section className="dialog" role="dialog" aria-modal="true" aria-labelledby="paymentOrderTitle">
            <h2 id="paymentOrderTitle">收款与应交日</h2>
            <p className="dialog-sub">
              {orderTitle(paymentTarget.order)}
              {paymentTarget.order.price != null && ` · 报价 ${formatPrice(paymentTarget.order.price)}`}
            </p>
            {paymentTarget.error && <div className="form-error" role="alert">{paymentTarget.error}</div>}
            <div className="field">
              <label htmlFor="paymentAmount">已收金额（元）</label>
              <input
                id="paymentAmount"
                className="input"
                inputMode="decimal"
                placeholder="0"
                value={paymentTarget.draft.amountPaidYuan}
                onChange={(event) => setPaymentTarget({
                  ...paymentTarget,
                  draft: { ...paymentTarget.draft, amountPaidYuan: event.target.value },
                  error: null,
                })}
                autoFocus
              />
              <div className="hint">
                {paymentTarget.order.outstanding_amount == null
                  ? '订单未定价，outstanding 不计入待收合计'
                  : `当前待收 ${formatPrice(paymentTarget.order.outstanding_amount)}，保存后按 DEC-10 推定联动`}
              </div>
            </div>
            <div className="field">
              <label htmlFor="paymentPaidAt">收款日期</label>
              <input
                id="paymentPaidAt"
                className="input"
                type="date"
                value={paymentTarget.draft.paidDate}
                onChange={(event) => setPaymentTarget({
                  ...paymentTarget,
                  draft: { ...paymentTarget.draft, paidDate: event.target.value },
                  error: null,
                })}
              />
              <div className="hint">留空表示不修改；已收现金 30 天按此日期落窗统计</div>
            </div>
            <div className="field">
              <label htmlFor="paymentDue">应交付日（仅已拍摄且未取消的订单可改）</label>
              <input
                id="paymentDue"
                className="input"
                type="date"
                value={paymentTarget.draft.dueDate}
                disabled={dueDateLocked(paymentTarget.order)}
                onChange={(event) => setPaymentTarget({
                  ...paymentTarget,
                  draft: { ...paymentTarget.draft, dueDate: event.target.value },
                  error: null,
                })}
              />
              <div className="hint">
                {paymentTarget.draft.dueIsOverride
                  ? '该应交日是订单级覆盖；清空保存即撤销覆盖，按当前拍摄日与账号 SLA 重新派生'
                  : `缺省按账号 SLA 自动派生；显式填写即成为订单级覆盖${dueDateLocked(paymentTarget.order) ? '（当前订单未到达拍摄，暂不可改）' : ''}`}
              </div>
            </div>
            <div className="dialog-actions">
              <button className="btn" type="button" onClick={() => setPaymentTarget(null)}>取消</button>
              <button className="btn btn-primary" type="button" onClick={confirmPayment}>保存</button>
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
  onPayment,
  onCancel,
  onDelete,
  onUpdate,
}: {
  order: OrderListItem
  focused: boolean
  fixedCustomer: boolean
  busy: boolean
  onProgress(order: OrderListItem, next: OrderStatusValue): void
  onPayment(order: OrderListItem): void
  onCancel(order: OrderListItem): void
  onDelete(order: OrderListItem): void
  onUpdate(order: OrderListItem, body: UpdateOrderBody, message: string): void
}) {
  const next = nextStatus(order)
  const canSkipDelivered = order.status === 'shot' || order.status === 'selected'
  const terminal = isTerminal(order.status)
  const [menuOpen, setMenuOpen] = useState(false)
  const moreRef = useRef<HTMLDivElement>(null)

  // 菜单开启期间接管 Escape 与外部点击，避免多张卡同时展开
  useEffect(() => {
    if (!menuOpen) return
    function onPointerDown(event: MouseEvent) {
      if (!moreRef.current?.contains(event.target as Node)) setMenuOpen(false)
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') setMenuOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [menuOpen])

  function runAction(action: () => void) {
    setMenuOpen(false)
    action()
  }

  // 主动作只留一个：能推进就推进，终态则无主动作
  const primary = next
    ? {
        label: `推进到${statusLabels[next]}`,
        disabled: busy || (next === 'closed' && !order.balance_paid),
        run: () => onProgress(order, next),
      }
    : null

  const overflow: {
    key: string
    label: string
    icon: typeof Check
    danger?: boolean
    disabled?: boolean
    run: () => void
  }[] = []
  if (!terminal && !order.deposit_paid) {
    overflow.push({ key: 'deposit', label: '标记定金', icon: Wallet, run: () => onUpdate(order, { deposit_paid: true }, '已标记定金') })
  }
  if (!terminal && !order.balance_paid) {
    overflow.push({ key: 'balance', label: '标记尾款', icon: BanknoteArrowUp, run: () => onUpdate(order, { balance_paid: true }, '已标记尾款') })
  }
  if (!terminal) {
    overflow.push({ key: 'payment', label: '收款 / 应交日', icon: Coins, run: () => onPayment(order) })
  }
  if (canSkipDelivered) {
    overflow.push({ key: 'deliver', label: '直接交付', icon: TruckIcon, run: () => onProgress(order, 'delivered') })
  }
  if (!terminal) {
    overflow.push({ key: 'cancel', label: '取消订单', icon: XCircle, danger: true, run: () => onCancel(order) })
  }
  if (terminal) {
    overflow.push({ key: 'delete', label: '删除订单', icon: Trash2, danger: true, run: () => onDelete(order) })
  }

  return (
	    <article className={`order-card status-${order.status}`} data-order-id={order.id} tabIndex={focused ? -1 : undefined}>
      <div className="order-main">
        <div className="order-title-line">
          <span className={`badge ${statusBadge(order.status)}`}>{statusLabels[order.status]}</span>
          <h3>{orderTitle(order)}</h3>
        </div>
        <PlanningSummaryLink summary={order.planning_summary} />
        <div className="order-meta">
          {!fixedCustomer && (
            <Link to={`/customers/${order.customer_id}`}>{order.customer_display_name}</Link>
          )}
          {fixedCustomer && <span>{order.customer_display_name}</span>}
          <span className="sep" aria-hidden="true">·</span>
          <span className="pkg">{order.package_name ?? '未选套系'}</span>
        </div>
        <div className="order-dates">
          <span>建单 {shortDate(order.created_at)}</span>
          {order.shot_at && <span>拍摄 {shortDate(order.shot_at)}</span>}
          {order.delivered_at && <span>交付 {shortDate(order.delivered_at)}</span>}
        </div>
        {/* 只在「已收」这类需要确认的情况显示，未收是常态不占版面 */}
        <div className="payment-flags">
          {order.deposit_paid && <span className="badge badge-success">定金已收</span>}
          {order.balance_paid && <span className="badge badge-success">尾款已收</span>}
          {order.amount_paid > 0 && !order.balance_paid && (
            <span className="badge badge-muted">已收 {formatPrice(order.amount_paid)}</span>
          )}
          {!terminal && !order.deposit_paid && !order.balance_paid && order.amount_paid === 0 && (
            <span className="badge badge-warning">未收款</span>
          )}
        </div>
      </div>
      <div className="order-price">{formatPrice(order.price)}</div>
      <div className="order-actions">
        {primary && (
          <button className="btn btn-sm btn-primary" type="button" disabled={primary.disabled} onClick={primary.run}>
            {primary.label}
          </button>
        )}
        {overflow.length > 0 && (
          <div className="order-more" ref={moreRef}>
            <button
              className="btn btn-sm"
              type="button"
              disabled={busy}
              aria-label={`${orderTitle(order)} 的更多操作`}
              aria-haspopup="menu"
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen((open) => !open)}
            >
              <MoreHorizontal aria-hidden="true" strokeWidth={2} />
            </button>
            {menuOpen && (
              <div className="order-more-menu" role="menu">
                {overflow.map(({ key, label, icon: Icon, danger, disabled, run }) => (
                  <button
                    key={key}
                    type="button"
                    role="menuitem"
                    className={danger ? 'danger' : undefined}
                    disabled={busy || disabled}
                    onClick={() => runAction(run)}
                  >
                    <Icon aria-hidden="true" strokeWidth={2} />
                    {label}
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </article>
  )
}

function OrderDialog({
  open,
  draft,
  packages,
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
            <div className="readonly-field customer-summary">
              <CustomerAvatar
                customerId={fixedCustomer.id ?? ''}
                displayName={fixedCustomer.display_name}
                avatarRevision={fixedCustomer.avatar_revision}
                avatarUrl={fixedCustomer.avatar_url}
                size="sm"
                decorative
              />
              <span>{fixedCustomer.display_name}</span>
            </div>
          </div>
        ) : (
          <div className={`field${submitted && !draft.customerId ? ' show-err' : ''}`}>
            <CustomerPicker
              label="客户 *"
              candidateStatuses={['active']}
              value={draft.customerId}
              required
              onChange={(choice) => onDraft({ ...draft, customerId: choice?.id ?? '' })}
            />
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
          <div className="field">
            <label htmlFor="orderAmountPaid">已收金额（元）</label>
            <input
              id="orderAmountPaid"
              className="input"
              value={draft.amountPaidYuan}
              inputMode="decimal"
              onChange={(event) => onDraft({ ...draft, amountPaidYuan: event.target.value })}
              placeholder="0"
            />
            <div className="hint">
              {draft.backfill ? '补录历史收款金额；不写收款时间，需精确日期可在保存后用「收款 / 应交日」补录' : '建单时录入即视为当下收款；待收余额按 DEC-10 推定'}
            </div>
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
    amountPaidYuan: '',
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
  if (draft.amountPaidYuan.trim()) {
    const paidCents = yuanInputToCents(draft.amountPaidYuan)
    if (paidCents == null) return '已收金额须为非负数字，最多两位小数'
  }
  if (draft.backfill && draft.status !== 'cancelled') {
    if (reached(draft.status, 'shot') && !draft.shotDate) return '该状态需要拍摄日期'
    if (reached(draft.status, 'delivered') && !draft.deliveredDate) return '该状态需要交付日期'
  }
  if (draft.backfill && draft.status === 'consulting') return '请选择补录状态'
  if (draft.backfill && draft.status === 'closed' && !draft.balancePaid) return '完结订单必须标记尾款已收'
  return null
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
  draft.amountPaidYuan = body.amount_paid == null ? '' : String(body.amount_paid / 100)
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
  const paidCents = yuanInputToCents(draft.amountPaidYuan)
  if (paidCents != null && paidCents > 0) {
    body.amount_paid = paidCents
    // 「录入即当下收款」只适用于正常建单：补录的是历史订单，paid_at=now 会把历史收款
    // 虚增进近 30 天已收现金窗口（§4.2 创建不自动写，补录不带 paid_at 完全合法）。
    if (!draft.backfill) {
      body.paid_at = new Date().toISOString()
    }
  }
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

/** 契约 §4.2：delivery_due_at 仅已到达拍摄且未取消的订单接受显式写。 */
function dueDateLocked(order: OrderListItem): boolean {
  return order.status === 'cancelled' || !reached(order.status, 'shot')
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
