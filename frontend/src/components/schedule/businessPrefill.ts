import { Temporal } from '@js-temporal/polyfill'

import {
  instantToLocalDateTime,
  resolveLocalDateTime,
  type LocalTimeOccurrence,
} from './timezone.ts'

export const planningSchedulePrefillKind = 'planning-schedule-prefill-v1' as const

export interface PlanningSchedulePrefill {
  kind: typeof planningSchedulePrefillKind
  order_id: string
  basis_minutes: number
}

export type PlanningSchedulePrefillEnvelope =
  | { present: false; value: null; error: null }
  | { present: true; value: PlanningSchedulePrefill; error: null }
  | { present: true; value: null; error: string }

interface BusinessDurationDraft {
  startDate: string
  startTime: string
  endDate: string
  endTime: string
  allDay: boolean
  startOccurrence?: LocalTimeOccurrence
  endOccurrence?: LocalTimeOccurrence
  businessDurationMinutes?: number
}

export function parsePlanningSchedulePrefill(state: unknown): PlanningSchedulePrefillEnvelope {
  if (!isRecord(state) || state.kind !== planningSchedulePrefillKind) {
    return { present: false, value: null, error: null }
  }
  const orderID = typeof state.order_id === 'string' ? state.order_id.trim() : ''
  if (!orderID || orderID.length > 200) {
    return { present: true, value: null, error: '经营草稿中的订单信息无效' }
  }
  const basisMinutes = typeof state.basis_minutes === 'number' ? state.basis_minutes : Number.NaN
  if (!Number.isSafeInteger(basisMinutes) || basisMinutes < 1 || basisMinutes > 10080) {
    return { present: true, value: null, error: '经营草稿中的建议时长无效' }
  }
  return {
    present: true,
    value: {
      kind: planningSchedulePrefillKind,
      order_id: orderID,
      basis_minutes: basisMinutes,
    },
    error: null,
  }
}

export function deriveBusinessPrefillEnd(
  start: { date: string; time: string; occurrence?: LocalTimeOccurrence },
  timezone: string,
  basisMinutes: number,
): { date: string; time: string } | null {
  if (!start.date || !start.time || !Number.isSafeInteger(basisMinutes) || basisMinutes < 1) return null
  try {
    const resolved = resolveLocalDateTime(start.date, start.time, timezone, start.occurrence)
    const end = Temporal.Instant.from(resolved.instant).add({ minutes: basisMinutes }).toString()
    return instantToLocalDateTime(end, timezone)
  } catch {
    return null
  }
}

export function applyBusinessDurationStart<T extends BusinessDurationDraft>(
  draft: T,
  timezone: string | null,
): T {
  if (!timezone || draft.allDay || draft.businessDurationMinutes === undefined) return draft
  const end = deriveBusinessPrefillEnd({
    date: draft.startDate,
    time: draft.startTime,
    occurrence: draft.startOccurrence,
  }, timezone, draft.businessDurationMinutes)
  return end ? {
    ...draft,
    endDate: end.date,
    endTime: end.time,
    endOccurrence: undefined,
  } : draft
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
