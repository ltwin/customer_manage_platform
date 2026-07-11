import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import {
  ApiError,
  createOrder,
  createScheduleSlot,
  deleteOrder,
  deleteScheduleSlot,
  listOrders,
  listScheduleSlots,
  updateOrder,
  updateScheduleSlot,
} from '../../api/client'
import type {
  CreateOrderBody,
  CreateScheduleSlotBody,
  ScheduleSlotListItem,
  UpdateScheduleSlotBody,
} from '../../api/client'
import { packagePriceYuanToCents, validatePackagePriceYuan } from '../../pages/packagePrice'
import { useFocusTrap } from '../useFocusTrap'
import ShootOrderFlow from './ShootOrderFlow'
import type { FixedScheduleCustomer, ShootOrderDraft } from './ShootOrderFlow'
import { overlappingSlots } from './calendarModel'
import {
  clearPendingSchedule,
  clearScheduleDraft,
  newScheduleFlowID,
  pendingScheduleExpired,
  readPendingSchedule,
  readScheduleDraft,
  scheduleAttemptKey,
  ScheduleStorageError,
  writePendingSchedule,
  writeScheduleDraft,
} from './journal'
import type { PendingScheduleFlow, ScheduleDraft } from './journal'
import { scheduleDraftResumeInput } from './backfill'
import {
  claimPendingFlow,
  locateStatusSyncOrder,
  pendingAfterDeterministicFailure,
  pendingForAutomaticReplay,
  scheduleCreateFailureKind,
  scheduleKnownSlotAction,
  scheduleRequestResultKnown,
  scheduleRecoveryActions,
  statusSyncSatisfied,
} from './flow'
import type { ScheduleRecoveryMode } from './flow'
import {
  instantToLocalDateTime,
  localDayRange,
  resolveLocalDateTime,
  ScheduleTimeError,
} from './timezone'
import type { LocalTimeOccurrence } from './timezone'

type SlotType = CreateScheduleSlotBody['type']

interface SlotDraft {
  type: SlotType
  startDate: string
  startTime: string
  endDate: string
  endTime: string
  allDay: boolean
  startOccurrence?: LocalTimeOccurrence
  endOccurrence?: LocalTimeOccurrence
  note: string
  shoot: ShootOrderDraft
}

export default function ScheduleSlotDialog({
  open,
  timezone,
  initialDate,
  slot,
  scheduleDraftId,
  fixedType,
  fixedCustomer,
  source = 'calendar',
  onClose,
  onChanged,
  onCompleted,
}: {
  open: boolean
  timezone: string | null
  initialDate: string
  slot?: ScheduleSlotListItem | null
  scheduleDraftId?: string
  fixedType?: SlotType
  fixedCustomer?: FixedScheduleCustomer
  source?: 'calendar' | 'customer'
  onClose(): void
  onChanged(): Promise<void> | void
  onCompleted?(date: string): void
}) {
  const navigate = useNavigate()
  const [draft, setDraft] = useState<SlotDraft>(() => emptyDraft(initialDate, fixedType, fixedCustomer))
  const [preview, setPreview] = useState<ScheduleSlotListItem[] | null>(null)
  const [pending, setPending] = useState<PendingScheduleFlow | null>(null)
  const [flowID, setFlowID] = useState(() => newScheduleFlowID())
  const [orderAttempt, setOrderAttempt] = useState(1)
  const [slotAttempt, setSlotAttempt] = useState(1)
  const [candidateReloadToken, setCandidateReloadToken] = useState(0)
  const [customerRefreshRequired, setCustomerRefreshRequired] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [recoveryMessage, setRecoveryMessage] = useState<string | null>(null)
  const [recoveryMode, setRecoveryMode] = useState<ScheduleRecoveryMode>(null)
  const [resumedDraft, setResumedDraft] = useState<ScheduleDraft | null>(null)
  const [failedFlow, setFailedFlow] = useState<PendingScheduleFlow | null>(null)
  const dialogRef = useFocusTrap<HTMLElement>(open, onClose, !saving)

  useEffect(() => {
    if (!open) return
    setPreview(null)
    setError(null)
    setRecoveryMessage(null)
    setRecoveryMode(null)
    setCandidateReloadToken(0)
    setCustomerRefreshRequired(false)
    try {
      const stored = readPendingSchedule()
      setPending(stored)
      if (stored) {
        setRecoveryMode(scheduleRequestResultKnown(stored) ? 'deterministic' : 'unknown')
      } else if (scheduleDraftId) {
        const sourceDraft = readScheduleDraft(scheduleDraftId)
        if (!sourceDraft?.known_order_id) {
          setResumedDraft(null)
          setError('补录订单尚未确认，不能返回创建档期')
          return
        }
        if (!timezone) {
          setResumedDraft(sourceDraft)
          setError('账号时区不可用，暂不能恢复排期时间')
          return
        }
        if (fixedCustomer && sourceDraft.customer_id !== fixedCustomer.id) {
          setResumedDraft(null)
          setError('排期草稿不属于当前客户')
          return
        }
        const input = scheduleDraftResumeInput(sourceDraft, timezone)
        setResumedDraft(sourceDraft)
        setFlowID(sourceDraft.draft_id)
        setDraft({
          type: 'shoot',
          startDate: input.startDate,
          startTime: input.startTime,
          endDate: input.endDate,
          endTime: input.endTime,
          allDay: false,
          note: input.note,
          shoot: {
            source: 'existing',
            customerId: input.customerID,
            orderId: input.orderID,
            packageId: '',
            title: '',
            priceYuan: '',
          },
        })
      } else {
        setResumedDraft(null)
        setFailedFlow(null)
        setFlowID(newScheduleFlowID())
        setOrderAttempt(1)
        setSlotAttempt(1)
        setDraft(slot ? draftFromSlot(slot, timezone) : emptyDraft(initialDate, fixedType, fixedCustomer))
      }
    } catch (reason) {
      setError(errorMessage(reason, '恢复记录读取失败'))
    }
  }, [open, slot, scheduleDraftId, initialDate, fixedType, fixedCustomer, timezone])

  const resolved = useMemo(() => {
    if (!timezone) return null
    try {
      return resolveDraftRange(draft, timezone)
    } catch {
      return null
    }
  }, [draft, timezone])
  const pendingSourceDraft = useMemo(() => {
    if (!pending?.source_draft_id) return null
    try {
      return readScheduleDraft(pending.source_draft_id, sessionStorage, Date.now(), true)
    } catch {
      return null
    }
  }, [pending])

  function changeDraft(next: SlotDraft) {
    setDraft(next)
    setPreview(null)
    setError(null)
  }

  async function checkConflicts() {
    if (!timezone) {
      setError('账号时区不可用，暂不能写入档期')
      return
    }
    let range: { startAt: string; endAt: string }
    try {
      range = resolveDraftRange(draft, timezone)
      validateDraft(draft, range)
    } catch (reason) {
      setError(errorMessage(reason, '档期输入无效'))
      return
    }
    setSaving(true)
    setError(null)
    try {
      const items = await listScheduleSlots(range.startAt, range.endAt)
      setPreview(overlappingSlots(items, range.startAt, range.endAt, slot?.id))
    } catch (reason) {
      setPreview(null)
      setError(errorMessage(reason, '冲突预览加载失败，请重试'))
    } finally {
      setSaving(false)
    }
  }

  async function save() {
    if (!timezone || !resolved) {
      setError('账号时区不可用或日期时间无效')
      return
    }
    try {
      validateDraft(draft, resolved)
    } catch (reason) {
      setError(errorMessage(reason, '档期输入无效'))
      return
    }
    if (preview === null) {
      await checkConflicts()
      return
    }
    if (slot?.id) {
      await saveExisting(slot.id, resolved)
      return
    }
    await beginCreate(resolved)
  }

  async function saveExisting(id: string, range: { startAt: string; endAt: string }) {
    setSaving(true)
    setError(null)
    const body: UpdateScheduleSlotBody = {
      start_at: range.startAt,
      end_at: range.endAt,
      type: draft.type,
      note: draft.note.trim() || null,
      order_id: draft.type === 'shoot' ? draft.shoot.orderId : null,
    }
    try {
      await updateScheduleSlot(id, body)
      setCustomerRefreshRequired(false)
      await onChanged()
      onCompleted?.(draft.startDate)
      onClose()
    } catch (reason) {
      if (reason instanceof ApiError && reason.code === 'customer_changed') {
        setPreview(null)
        setCustomerRefreshRequired(true)
        try {
          await refreshEditedSlotCustomer(id, range)
          setError('订单客户已发生变化，已刷新客户与订单候选；请重新检查冲突后确认')
        } catch {
          setError('订单客户已发生变化，但最新候选刷新失败；请重试后再确认')
        }
      } else {
        setError(errorMessage(reason, '档期更新失败'))
      }
    } finally {
      setSaving(false)
    }
  }

  async function refreshEditedSlotCustomer(id: string, range: { startAt: string; endAt: string }) {
    // customer_changed rolls the PATCH back, so locate the persisted slot in its original range.
    const lookupRange = slot?.id === id
      ? { startAt: slot.start_at, endAt: slot.end_at }
      : range
    const refreshed = (await listScheduleSlots(lookupRange.startAt, lookupRange.endAt))
      .find((candidate) => candidate.id === id)
    if (!refreshed || refreshed.type !== 'shoot') {
      throw new Error('未找到最新拍摄档期')
    }
    setDraft((current) => ({
      ...current,
      shoot: {
        ...current.shoot,
        customerId: refreshed.customer_id,
        orderId: refreshed.order_id,
        orderStatus: refreshed.order_status,
      },
    }))
    setCandidateReloadToken((current) => current + 1)
    setCustomerRefreshRequired(false)
  }

  async function retryEditedSlotCustomerRefresh() {
    if (!slot?.id || !resolved) return
    setSaving(true)
    try {
      await refreshEditedSlotCustomer(slot.id, resolved)
      setError('客户与订单候选已刷新，请重新检查冲突后确认')
    } catch {
      setError('最新客户与订单候选仍加载失败，请稍后重试')
    } finally {
      setSaving(false)
    }
  }

  async function beginCreate(range: { startAt: string; endAt: string }) {
    setSaving(true)
    setError(null)
    try {
      const existing = readPendingSchedule()
      if (existing) {
        setPending(existing)
        setRecoveryMode(scheduleRequestResultKnown(existing) ? 'deterministic' : 'unknown')
        setRecoveryMessage('当前标签页已有待恢复排期，请先确认原流程')
        return
      }
      const slotBody = createSlotBody(draft, range)
      let next: PendingScheduleFlow
      if (draft.type === 'shoot' && draft.shoot.source === 'new') {
        const orderBody = createNewOrderBody(draft)
        const sourceDraft = scheduleDraftForFlow(flowID, draft, range, source)
        writeScheduleDraft(sourceDraft)
        const fresh: PendingScheduleFlow = {
          flow_id: flowID,
          phase: 'order',
          source_draft_id: sourceDraft.draft_id,
          normalized_body: orderBody,
          attempt_key: scheduleAttemptKey(flowID, 'order', orderAttempt),
          known_customer_id: draft.shoot.customerId,
          prior_order_status: 'consulting',
          created_at: new Date().toISOString(),
        }
        next = failedFlow?.phase === 'order'
          ? {
              ...pendingAfterDeterministicFailure(failedFlow, orderBody),
              source_draft_id: sourceDraft.draft_id,
              created_at: fresh.created_at,
            }
          : fresh
      } else {
        let sourceDraft = resumedDraft
        if (draft.type === 'shoot' && !sourceDraft) {
          sourceDraft = {
            ...scheduleDraftForFlow(flowID, draft, range, source),
            known_order_id: draft.shoot.orderId,
          }
          writeScheduleDraft(sourceDraft)
        }
        const fresh: PendingScheduleFlow = {
          flow_id: flowID,
          phase: 'slot',
          source_draft_id: sourceDraft?.draft_id,
          normalized_body: slotBody,
          attempt_key: scheduleAttemptKey(flowID, 'slot', slotAttempt),
          known_customer_id: draft.shoot.customerId || undefined,
          known_order_id: draft.type === 'shoot' ? draft.shoot.orderId : undefined,
          prior_order_status: draft.type === 'shoot' ? draft.shoot.orderStatus : undefined,
          created_at: new Date().toISOString(),
        }
        next = failedFlow?.phase === 'slot'
          ? { ...pendingAfterDeterministicFailure(failedFlow, slotBody), created_at: fresh.created_at }
          : fresh
      }
      const blocking = claimPendingFlow(next)
      if (blocking) {
        setPending(blocking)
        setRecoveryMode(scheduleRequestResultKnown(blocking) ? 'deterministic' : 'unknown')
        setRecoveryMessage('当前标签页已有待恢复排期，请先确认原流程')
        return
      }
      setPending(next)
      setFailedFlow(null)
      await executePending(next)
    } catch (reason) {
      handleFlowFailure(reason)
    } finally {
      setSaving(false)
    }
  }

  async function executePending(flow: PendingScheduleFlow): Promise<void> {
    try {
      pendingForAutomaticReplay(flow)
    } catch {
      setPending(flow)
      setRecoveryMessage('恢复记录已超过 24 小时，请先人工核对已知资源')
      return
    }
    if (flow.phase === 'backfill_order') {
      if (flow.known_customer_id && flow.source_draft_id) {
        navigate(`/customers/${flow.known_customer_id}?tab=orders&mode=backfill&schedule_draft=${flow.source_draft_id}`)
      }
      return
    }
    if (flow.phase === 'order') {
      const order = await createOrder(flow.normalized_body as CreateOrderBody, flow.attempt_key)
      const confirmed = { ...flow, known_order_id: order.id }
      setPending(confirmed)
      try {
        writePendingSchedule(confirmed)
        if (!flow.source_draft_id) throw new ScheduleStorageError('排期时间草稿缺失')
        const sourceDraft = readScheduleDraft(flow.source_draft_id, sessionStorage, Date.now(), true)
        if (!sourceDraft) throw new ScheduleStorageError('排期时间草稿缺失')
        const slotBody: CreateScheduleSlotBody = {
          start_at: sourceDraft.start_at,
          end_at: sourceDraft.end_at,
          type: 'shoot',
          order_id: order.id,
          ...(sourceDraft.note ? { note: sourceDraft.note } : {}),
        }
        const slotFlow: PendingScheduleFlow = {
          ...confirmed,
          phase: 'slot',
          normalized_body: slotBody,
          attempt_key: scheduleAttemptKey(flow.flow_id, 'slot', 1),
          known_customer_id: order.customer_id,
          prior_order_status: order.status,
        }
        writePendingSchedule(slotFlow)
        setPending(slotFlow)
        try {
          await executePending(slotFlow)
        } catch (reason) {
          handleFlowFailure(reason, slotFlow)
        }
      } catch (reason) {
        handleFlowFailure(reason, confirmed)
      }
      return
    }
    if (flow.phase === 'slot') {
      const body = flow.normalized_body as CreateScheduleSlotBody
      const result = await createScheduleSlot(body, flow.attempt_key)
      const confirmed = { ...flow, known_slot_id: result.slot.id }
      setPending(confirmed)
      try {
        writePendingSchedule(confirmed)
        await onChanged()
        const item = body.type === 'shoot'
          ? (await refreshKnownSlot(confirmed)).item
          : (await listScheduleSlots(body.start_at, body.end_at)).find((candidate) => candidate.id === result.slot.id)
        if (!item) throw new Error('档期已创建但刷新结果暂未找到，请重试确认')
        if (item.type === 'shoot' && item.order_status === 'consulting') {
          const statusFlow: PendingScheduleFlow = {
            ...confirmed,
            phase: 'status_sync',
            normalized_body: { status: 'scheduled' },
            known_customer_id: item.customer_id,
            known_order_id: item.order_id,
            prior_order_status: item.order_status,
          }
          writePendingSchedule(statusFlow)
          setPending(statusFlow)
          try {
            await executePending(statusFlow)
          } catch (reason) {
            handleFlowFailure(reason, statusFlow)
          }
          return
        }
        await completeFlow(confirmed, localDateForInstant(body.start_at, timezone))
      } catch (reason) {
        handleFlowFailure(reason, confirmed)
      }
      return
    }
    if (!flow.known_order_id) throw new Error('待同步订单 id 缺失')
    try {
      await updateOrder(flow.known_order_id, flow.normalized_body as { status: 'scheduled' })
    } catch (reason) {
      if (reason instanceof ApiError && reason.status === 409) {
        await resolveStatusSyncConflict(flow)
        return
      }
      throw reason
    }
    const syncedFlow = { ...flow, prior_order_status: 'scheduled' }
    setPending(syncedFlow)
    try {
      writePendingSchedule(syncedFlow)
      const startAt = await pendingStartAt(syncedFlow)
      await completeFlow(syncedFlow, localDateForInstant(startAt, timezone))
    } catch (reason) {
      handleFlowFailure(reason, syncedFlow)
    }
  }

  async function refreshKnownSlot(flow: PendingScheduleFlow): Promise<{
    flow: PendingScheduleFlow
    item: Extract<ScheduleSlotListItem, { type: 'shoot' }>
  }> {
    if (!flow.known_slot_id) throw new Error('待确认档期 id 缺失')
    let startAt = ''
    let endAt = ''
    if (flow.source_draft_id) {
      const sourceDraft = readScheduleDraft(flow.source_draft_id, sessionStorage, Date.now(), true)
      startAt = sourceDraft?.start_at ?? ''
      endAt = sourceDraft?.end_at ?? ''
    }
    if ((!startAt || !endAt) && flow.phase === 'slot') {
      const body = flow.normalized_body as CreateScheduleSlotBody
      startAt = body.start_at
      endAt = body.end_at
    }
    if (!startAt || !endAt) throw new Error('档期时间恢复信息缺失，请通过档期链接人工核对')
    const item = (await listScheduleSlots(startAt, endAt)).find((candidate) => candidate.id === flow.known_slot_id)
    if (!item || item.type !== 'shoot') throw new Error('档期刷新结果未找到对应拍摄档期')
    const refreshed: PendingScheduleFlow = {
      ...flow,
      known_customer_id: item.customer_id,
      known_order_id: item.order_id,
    }
    writePendingSchedule(refreshed)
    setPending(refreshed)
    return { flow: refreshed, item }
  }

  async function resolveStatusSyncConflict(flow: PendingScheduleFlow) {
    let refreshed = await refreshKnownSlot(flow)
    const orderID = refreshed.flow.known_order_id
    if (!orderID) throw new Error('待同步订单 id 缺失')
    const order = await locateStatusSyncOrder(
      orderID,
      refreshed.item.customer_id,
      async () => {
        refreshed = await refreshKnownSlot(refreshed.flow)
        return refreshed.item.customer_id
      },
      (customerID, page) => listOrders({ customerId: customerID, page, pageSize: 100 }),
    )
    if (!order) {
      setRecoveryMode('deterministic')
      setRecoveryMessage('未能在订单列表中确认当前状态，可查看已知资源后重试或保留现状')
      setError('订单状态确认失败')
      return
    }
    const confirmed = {
      ...refreshed.flow,
      known_customer_id: order.customer_id,
      known_order_id: order.id,
      prior_order_status: order.status,
    }
    writePendingSchedule(confirmed)
    setPending(confirmed)
    if (statusSyncSatisfied(order.status)) {
      const startAt = await pendingStartAt(confirmed)
      try {
        await completeFlow(confirmed, localDateForInstant(startAt, timezone))
      } catch (reason) {
        handleFlowFailure(reason, confirmed)
      }
      return
    }
    setRecoveryMode('deterministic')
    if (order.status === 'consulting') {
      setRecoveryMessage('订单仍为咨询状态，可重试同步、查看订单、删除档期或保留现状并结束')
      setError('订单状态尚未同步')
      return
    }
    setRecoveryMessage('订单已取消，档期仍保留；请查看订单后选择删除档期或保留现状')
    setError('订单已取消，未自动修改档期')
  }

  function handleFlowFailure(reason: unknown, currentFlow?: PendingScheduleFlow) {
    const active = currentFlow ?? readPendingSafely() ?? pending
    const kind = scheduleCreateFailureKind(
      reason instanceof ApiError ? reason : null,
      Boolean(active && scheduleRequestResultKnown(active)),
    )
    if (kind === 'idempotency_conflict') {
      setPending(active)
      setRecoveryMode('idempotency_conflict')
      setRecoveryMessage('原请求 key 已绑定其他成功请求，禁止自动重放、修改 body 或换 key；请先人工核对客户订单与档期')
      setError('幂等记录与当前请求不一致')
      return
    }
    if (kind === 'unknown') {
      setPending(active)
      setRecoveryMode('unknown')
      setRecoveryMessage('请求结果未知，必须使用原请求继续确认，不能换 key 或撤销')
      setError(errorMessage(reason, '请求结果未知'))
      return
    }
    if (kind === 'customer_changed') {
      setPending(active)
      setRecoveryMode('customer_changed')
      setRecoveryMessage('订单客户连续变化，原候选与冲突确认已失效')
      setError('请重新加载候选和冲突后再次确认')
      return
    }
    if (active?.phase === 'order' && !active.known_order_id) {
      clearPendingSafely()
      setFlowID(active.flow_id)
      setFailedFlow(active)
      setPending(null)
      setRecoveryMode(null)
      setError(errorMessage(reason, '排期请求失败'))
      return
    }
    setPending(active)
    setRecoveryMode('deterministic')
    setRecoveryMessage(active && scheduleRequestResultKnown(active)
      ? '资源结果已确认，但后续刷新或本地收口失败；可重试、查看资源或明确结束'
      : '请求已明确失败，可重试、保留现状或执行补偿')
    setError(errorMessage(reason, '排期流程收口失败'))
  }

  async function completeFlow(flow: PendingScheduleFlow, date: string) {
    await onChanged()
    if (flow.source_draft_id) clearScheduleDraft(flow.source_draft_id)
    clearPendingSchedule()
    setPending(null)
    setRecoveryMode(null)
    setRecoveryMessage(null)
    onCompleted?.(date)
    onClose()
  }

  async function keepAndEnd() {
    if (!pending) return
    try {
      clearPendingSchedule()
      if (pending.source_draft_id) clearScheduleDraft(pending.source_draft_id)
      setPending(null)
      setRecoveryMode(null)
      await onChanged()
      onClose()
    } catch (reason) {
      setError(errorMessage(reason, '结束恢复失败'))
    }
  }

  async function compensateOrder() {
    if (!pending?.known_order_id) return
    setSaving(true)
    try {
      await updateOrder(pending.known_order_id, { status: 'cancelled' })
      try {
        await deleteOrder(pending.known_order_id)
      } catch (reason) {
        if (!(reason instanceof ApiError) || reason.status !== 404) throw reason
      }
      await keepAndEnd()
    } catch (reason) {
      setError(errorMessage(reason, '撤销订单失败，可选择保留订单并结束'))
    } finally {
      setSaving(false)
    }
  }

  async function removeKnownSlot() {
    if (!pending?.known_slot_id) return
    setSaving(true)
    try {
      await deleteScheduleSlot(pending.known_slot_id)
      await keepAndEnd()
    } catch (reason) {
      setError(errorMessage(reason, '删除档期失败'))
    } finally {
      setSaving(false)
    }
  }

  async function resetCustomerChanged() {
    if (!pending || !timezone) return
    const body = pending.phase === 'slot' ? pending.normalized_body as CreateScheduleSlotBody : null
    if (!body) return
    setSaving(true)
    try {
      const start = instantToLocalDateTime(body.start_at, timezone)
      const end = instantToLocalDateTime(body.end_at, timezone)
      let customerID = pending.known_customer_id ?? ''
      let orderID = body.order_id ?? pending.known_order_id ?? ''
      let orderStatus = pending.prior_order_status
      if (body.type === 'shoot' && orderID) {
        const order = await locateStatusSyncOrder(
          orderID,
          customerID,
          async () => customerID,
          (candidateCustomerID, page) => listOrders({ customerId: candidateCustomerID, page, pageSize: 100 }),
        )
        if (!order) throw new Error('已知订单不在候选列表中，请查看订单后重试')
        customerID = order.customer_id
        orderID = order.id
        orderStatus = order.status
      }
      const editable = pendingAfterDeterministicFailure(pending, body)
      const nextFailed = {
        ...editable,
        known_customer_id: customerID || undefined,
        known_order_id: orderID || undefined,
        prior_order_status: orderStatus,
      }
      if (nextFailed.source_draft_id) {
        const sourceDraft = readScheduleDraft(nextFailed.source_draft_id, sessionStorage, Date.now(), true)
        if (sourceDraft) writeScheduleDraft({ ...sourceDraft, customer_id: customerID || sourceDraft.customer_id, known_order_id: orderID || sourceDraft.known_order_id })
      }
      setDraft((current) => ({
        ...current,
        type: body.type,
        startDate: start.date,
        startTime: start.time,
        endDate: end.date,
        endTime: end.time,
        note: body.note ?? '',
        shoot: {
          ...current.shoot,
          source: 'existing',
          customerId: customerID,
          orderId: orderID,
          orderStatus,
        },
      }))
      clearPendingSchedule()
      setFlowID(pending.flow_id)
      setFailedFlow(nextFailed)
      setPending(null)
      setPreview(null)
      setRecoveryMode(null)
      setRecoveryMessage(null)
      setError(null)
    } catch (reason) {
      setError(errorMessage(reason, '恢复候选失败'))
    } finally {
      setSaving(false)
    }
  }

  function handoffHistorical(customerID: string) {
    if (!resolved) return
    const draftID = newScheduleFlowID()
    const handoff: ScheduleDraft = {
      draft_id: draftID,
      customer_id: customerID,
      start_at: resolved.startAt,
      end_at: resolved.endAt,
      note: draft.note.trim() || undefined,
      source,
      return_to: source === 'customer'
        ? `/customers/${customerID}`
        : `/calendar?date=${draft.startDate}`,
      created_at: new Date().toISOString(),
    }
    try {
      writeScheduleDraft(handoff)
      navigate(`/customers/${customerID}?tab=orders&mode=backfill&schedule_draft=${draftID}`)
    } catch (reason) {
      setError(errorMessage(reason, '无法保存补录交接草稿'))
    }
  }

  if (!open) return null

  const expired = pending ? pendingScheduleExpired(pending) : false
  return (
    <div className="overlay open" onClick={(event) => { if (event.target === event.currentTarget && !saving) onClose() }}>
      <section ref={dialogRef} className="dialog schedule-dialog" role="dialog" aria-modal="true" aria-labelledby="scheduleDialogTitle" tabIndex={-1} autoFocus>
        <h2 id="scheduleDialogTitle">{slot ? '编辑档期' : '新建档期'}</h2>
        <div className="dialog-sub">时间按 {timezone ?? '账号时区不可用'}</div>

        {pending ? (
          <RecoveryPanel
            flow={pending}
            sourceDraft={pendingSourceDraft}
            timezone={timezone}
            expired={expired}
            mode={recoveryMode}
            message={recoveryMessage}
            saving={saving}
            onResume={() => {
              setSaving(true)
              executePending(pending).catch((reason) => handleFlowFailure(reason, pending)).finally(() => setSaving(false))
            }}
            onReset={() => { void resetCustomerChanged() }}
            onKeep={() => { void keepAndEnd() }}
            onCompensate={() => { void compensateOrder() }}
            onDeleteSlot={() => { void removeKnownSlot() }}
            onAbandon={() => { void keepAndEnd() }}
          />
        ) : (
          <>
            {!fixedType && !slot && (
              <div className="segmented" aria-label="档期类型">
                {(['shoot', 'hold', 'busy'] as const).map((type) => (
                  <button key={type} type="button" className={draft.type === type ? 'active' : ''} onClick={() => changeDraft({ ...draft, type })}>
                    {slotTypeLabel(type)}
                  </button>
                ))}
              </div>
            )}

            <div className="field-row schedule-date-row">
              <div className="field">
                <label htmlFor="slotStartDate">开始日期</label>
                <input id="slotStartDate" className="input" type="date" value={draft.startDate} onChange={(event) => changeDraft({ ...draft, startDate: event.target.value })} />
              </div>
              <div className="field">
                <label htmlFor="slotStartTime">开始时间</label>
                <input id="slotStartTime" className="input" type="time" disabled={draft.allDay} value={draft.startTime} onChange={(event) => changeDraft({ ...draft, startTime: event.target.value, startOccurrence: undefined })} />
                <OccurrenceSelect date={draft.startDate} time={draft.startTime} timezone={timezone} value={draft.startOccurrence} onChange={(value) => changeDraft({ ...draft, startOccurrence: value })} />
              </div>
              <div className="field">
                <label htmlFor="slotEndDate">结束日期</label>
                <input id="slotEndDate" className="input" type="date" disabled={draft.allDay} value={draft.endDate} onChange={(event) => changeDraft({ ...draft, endDate: event.target.value })} />
              </div>
              <div className="field">
                <label htmlFor="slotEndTime">结束时间</label>
                <input id="slotEndTime" className="input" type="time" disabled={draft.allDay} value={draft.endTime} onChange={(event) => changeDraft({ ...draft, endTime: event.target.value, endOccurrence: undefined })} />
                <OccurrenceSelect date={draft.endDate} time={draft.endTime} timezone={timezone} value={draft.endOccurrence} onChange={(value) => changeDraft({ ...draft, endOccurrence: value })} />
              </div>
            </div>
            <label className="check-line">
              <input type="checkbox" checked={draft.allDay} onChange={(event) => changeDraft({ ...draft, allDay: event.target.checked, endDate: draft.startDate })} />
              全天
            </label>

            {draft.type === 'shoot' && (
              <ShootOrderFlow
                value={draft.shoot}
                targetEndAt={resolved?.endAt ?? null}
                reloadToken={candidateReloadToken}
                fixedCustomer={fixedCustomer}
                onChange={(shoot) => changeDraft({ ...draft, shoot })}
                onHistoricalHandoff={handoffHistorical}
              />
            )}

            <div className="field">
              <label htmlFor="slotNote">备注</label>
              <input id="slotNote" className="input" maxLength={500} value={draft.note} onChange={(event) => changeDraft({ ...draft, note: event.target.value })} />
            </div>

            {preview !== null && (
              <div className={preview.length ? 'conflict-tip' : 'schedule-preview-clear'}>
                {preview.length ? (
                  <>
                    <strong>发现 {preview.length} 条重叠档期</strong>
                    {preview.map((item) => <div key={item.id}>{slotTypeLabel(item.type)} · {formatInstantRange(item, timezone)}</div>)}
                  </>
                ) : '目标时间没有重叠档期'}
              </div>
            )}
            {error && <div className="form-error" role="alert">{error}</div>}
            {customerRefreshRequired && (
              <div className="schedule-empty-action">
                <span>必须先刷新最新客户与订单候选，不能继续提交旧候选。</span>
                <button className="btn btn-sm" type="button" disabled={saving} onClick={() => { void retryEditedSlotCustomerRefresh() }}>
                  重新加载客户与候选
                </button>
              </div>
            )}
            <div className="dialog-actions">
              <button className="btn" type="button" disabled={saving} onClick={onClose}>取消</button>
              <button className="btn btn-primary" type="button" disabled={saving || !timezone || customerRefreshRequired} onClick={() => { void save() }}>
                {saving ? '处理中' : preview === null ? '检查冲突' : preview.length ? '仍然保存' : slot ? '保存修改' : '保存档期'}
              </button>
            </div>
          </>
        )}
      </section>
    </div>
  )
}

function RecoveryPanel({
  flow, sourceDraft, timezone, expired, mode, message, saving, onResume, onReset, onKeep, onCompensate, onDeleteSlot, onAbandon,
}: {
  flow: PendingScheduleFlow
  sourceDraft: ScheduleDraft | null
  timezone: string | null
  expired: boolean
  mode: ScheduleRecoveryMode
  message: string | null
  saving: boolean
  onResume(): void
  onReset(): void
  onKeep(): void
  onCompensate(): void
  onDeleteSlot(): void
  onAbandon(): void
}) {
  const actions = scheduleRecoveryActions(flow, expired, mode, sourceDraft)
  const slotAction = scheduleKnownSlotAction(flow, timezone, sourceDraft)
  return (
    <div className="schedule-recovery">
      <div className="conflict-tip">
        <strong>{expired ? '恢复已过期' : '发现未完成的排期流程'}</strong>
        <div>{message ?? phaseLabel(flow.phase)}</div>
      </div>
      <div className="schedule-resource-links">
        {flow.known_customer_id && (
          <Link
            className="btn btn-sm"
            to={`/customers/${flow.known_customer_id}?tab=orders${flow.known_order_id ? `&order=${flow.known_order_id}` : ''}`}
            target="_blank"
            rel="noreferrer"
          >
            {flow.known_order_id ? '查看订单' : '查看客户订单'}
          </Link>
        )}
        {slotAction && <Link className="btn btn-sm" to={slotAction.href}>{slotAction.label}</Link>}
      </div>
      <div className="dialog-actions recovery-actions">
        {actions.resume && <button className="btn btn-primary" type="button" disabled={saving} onClick={onResume}>用原请求继续确认</button>}
        {actions.reset && <button className="btn" type="button" disabled={saving} onClick={onReset}>重新加载并编辑</button>}
        {actions.deleteSlot && <button className="btn btn-danger-ghost" type="button" disabled={saving} onClick={onDeleteSlot}>删除档期</button>}
        {actions.compensateOrder && <button className="btn btn-danger-ghost" type="button" disabled={saving} onClick={onCompensate}>撤销刚建订单</button>}
        {actions.keep && <button className="btn" type="button" disabled={saving} onClick={onKeep}>保留当前状态并结束</button>}
        {actions.abandon && <button className="btn btn-danger-ghost" type="button" disabled={saving} onClick={onAbandon}>已人工核对，放弃恢复记录</button>}
      </div>
    </div>
  )
}

function OccurrenceSelect({
  date, time, timezone, value, onChange,
}: {
  date: string
  time: string
  timezone: string | null
  value?: LocalTimeOccurrence
  onChange(value: LocalTimeOccurrence): void
}) {
  if (!timezone || !isAmbiguous(date, time, timezone)) return null
  const first = resolveLocalDateTime(date, time, timezone, 'first')
  const second = resolveLocalDateTime(date, time, timezone, 'second')
  return (
    <select className="input occurrence-select" value={value ?? ''} onChange={(event) => onChange(event.target.value as LocalTimeOccurrence)}>
      <option value="">选择重复时间</option>
      <option value="first">第一次（{first.offset}）</option>
      <option value="second">第二次（{second.offset}）</option>
    </select>
  )
}

function emptyDraft(date: string, fixedType?: SlotType, fixedCustomer?: FixedScheduleCustomer): SlotDraft {
  return {
    type: fixedType ?? 'shoot',
    startDate: date,
    startTime: '10:00',
    endDate: date,
    endTime: '12:00',
    allDay: false,
    note: '',
    shoot: {
      source: 'existing',
      customerId: fixedCustomer?.id ?? '',
      orderId: '',
      packageId: '',
      title: '',
      priceYuan: '',
    },
  }
}

function draftFromSlot(slot: ScheduleSlotListItem, timezone: string | null): SlotDraft {
  if (!timezone) return emptyDraft(slot.start_at.slice(0, 10))
  const start = instantToLocalDateTime(slot.start_at, timezone)
  const end = instantToLocalDateTime(slot.end_at, timezone)
  return {
    type: slot.type,
    startDate: start.date,
    startTime: start.time,
    endDate: end.date,
    endTime: end.time,
    allDay: false,
    note: slot.note ?? '',
    shoot: {
      source: 'existing',
      customerId: slot.type === 'shoot' ? slot.customer_id : '',
      orderId: slot.type === 'shoot' ? slot.order_id : '',
      orderStatus: slot.type === 'shoot' ? slot.order_status : undefined,
      packageId: '',
      title: slot.type === 'shoot' ? (slot.order_title ?? slot.package_name ?? '当前订单') : '',
      priceYuan: '',
    },
  }
}

function resolveDraftRange(draft: SlotDraft, timezone: string): { startAt: string; endAt: string } {
  if (draft.allDay) {
    const range = localDayRange(draft.startDate, timezone)
    return { startAt: range.start, endAt: range.end }
  }
  const start = resolveLocalDateTime(draft.startDate, draft.startTime, timezone, draft.startOccurrence)
  const end = resolveLocalDateTime(draft.endDate, draft.endTime, timezone, draft.endOccurrence)
  if (Date.parse(end.instant) <= Date.parse(start.instant)) throw new ScheduleTimeError('invalid_date_time', '结束时间必须晚于开始时间')
  return { startAt: start.instant, endAt: end.instant }
}

function validateDraft(draft: SlotDraft, range: { startAt: string; endAt: string }) {
  if (!draft.startDate || !range.startAt || !range.endAt) throw new Error('请填写完整日期时间')
  if (draft.note.length > 500) throw new Error('备注不能超过 500 字')
  if (draft.type !== 'shoot') return
  if (!draft.shoot.customerId) throw new Error('请选择客户')
  if (draft.shoot.source === 'existing' && !draft.shoot.orderId) throw new Error('请选择可排期订单')
  if (draft.shoot.source === 'new') {
    if (Date.parse(range.endAt) <= Date.now()) throw new Error('历史档期不能在弹窗内新建咨询订单')
    if (draft.shoot.priceYuan.trim()) {
      const priceError = validatePackagePriceYuan(draft.shoot.priceYuan)
      if (priceError) throw new Error(priceError.replace('基础价', '价格'))
    }
  }
}

function createSlotBody(draft: SlotDraft, range: { startAt: string; endAt: string }): CreateScheduleSlotBody {
  return {
    start_at: range.startAt,
    end_at: range.endAt,
    type: draft.type,
    ...(draft.type === 'shoot' && draft.shoot.orderId ? { order_id: draft.shoot.orderId } : {}),
    ...(draft.note.trim() ? { note: draft.note.trim() } : {}),
  }
}

function createNewOrderBody(draft: SlotDraft): CreateOrderBody {
  return {
    creation_mode: 'new',
    customer_id: draft.shoot.customerId,
    status: 'consulting',
    ...(draft.shoot.packageId ? { package_id: draft.shoot.packageId } : {}),
    ...(draft.shoot.title.trim() ? { title: draft.shoot.title.trim() } : {}),
    ...(draft.shoot.priceYuan.trim() ? { price: packagePriceYuanToCents(draft.shoot.priceYuan) } : {}),
  }
}

function scheduleDraftForFlow(
  flowID: string,
  draft: SlotDraft,
  range: { startAt: string; endAt: string },
  source: 'calendar' | 'customer',
): ScheduleDraft {
  return {
    draft_id: flowID,
    customer_id: draft.shoot.customerId,
    start_at: range.startAt,
    end_at: range.endAt,
    note: draft.note.trim() || undefined,
    source,
    return_to: source === 'customer'
      ? `/customers/${draft.shoot.customerId}`
      : `/calendar?date=${draft.startDate}`,
    created_at: new Date().toISOString(),
  }
}

async function pendingStartAt(flow: PendingScheduleFlow): Promise<string> {
  if (flow.source_draft_id) {
    const source = readScheduleDraft(flow.source_draft_id, sessionStorage, Date.now(), true)
    if (source) return source.start_at
  }
  if (flow.phase === 'slot') return (flow.normalized_body as CreateScheduleSlotBody).start_at
  return new Date().toISOString()
}

function localDateForInstant(instant: string, timezone: string | null): string {
  if (!timezone) return instant.slice(0, 10)
  return instantToLocalDateTime(instant, timezone).date
}

function isAmbiguous(date: string, time: string, timezone: string): boolean {
  try {
    resolveLocalDateTime(date, time, timezone)
    return false
  } catch (reason) {
    return reason instanceof ScheduleTimeError && reason.code === 'ambiguous_local_time'
  }
}

function slotTypeLabel(type: string): string {
  return { shoot: '拍摄', hold: '预留', busy: '个人占用' }[type] ?? type
}

function formatInstantRange(slot: ScheduleSlotListItem, timezone: string | null): string {
  if (!timezone) return `${slot.start_at} - ${slot.end_at}`
  const start = instantToLocalDateTime(slot.start_at, timezone)
  const end = instantToLocalDateTime(slot.end_at, timezone)
  return `${start.date} ${start.time} - ${end.date} ${end.time}`
}

function phaseLabel(phase: PendingScheduleFlow['phase']): string {
  return {
    order: '正在确认订单创建结果',
    slot: '正在确认档期创建结果',
    status_sync: '档期已创建，正在同步订单状态',
    backfill_order: '正在确认历史订单补录结果',
  }[phase]
}

function readPendingSafely(): PendingScheduleFlow | null {
  try {
    return readPendingSchedule()
  } catch {
    return null
  }
}

function clearPendingSafely() {
  try { clearPendingSchedule() } catch { /* keep current in-memory state */ }
}

function errorMessage(reason: unknown, fallback: string): string {
  if (reason instanceof ApiError || reason instanceof Error) return reason.message
  return fallback
}
