import type { CreateOrderBody } from '../../api/client.ts'
import type { ScheduleDraft } from './journal.ts'
import { instantToLocalDateTime } from './timezone.ts'

type BackfillStatus = NonNullable<CreateOrderBody['status']>

export function backfillTimestampFields(
  status: BackfillStatus,
  shotAt?: string,
  deliveredAt?: string,
): Pick<CreateOrderBody, 'shot_at' | 'delivered_at'> {
  const includesShot = ['shot', 'selected', 'retouching', 'delivered', 'closed'].includes(status)
  const includesDelivery = status === 'delivered' || status === 'closed'
  return {
    ...(includesShot && shotAt ? { shot_at: shotAt } : {}),
    ...(includesDelivery && deliveredAt ? { delivered_at: deliveredAt } : {}),
  }
}

export interface ScheduleDraftResumeInput {
  startDate: string
  startTime: string
  endDate: string
  endTime: string
  note: string
  customerID: string
  orderID: string
}

export function scheduleDraftResumeInput(
  draft: ScheduleDraft,
  timezone: string,
): ScheduleDraftResumeInput {
  const start = instantToLocalDateTime(draft.start_at, timezone)
  const end = instantToLocalDateTime(draft.end_at, timezone)
  return {
    startDate: start.date,
    startTime: start.time,
    endDate: end.date,
    endTime: end.time,
    note: draft.note ?? '',
    customerID: draft.customer_id,
    orderID: draft.known_order_id ?? '',
  }
}
