import type { components } from '../api/schema'
import {
  packagePriceYuanToCents,
  validatePackagePriceYuan,
} from '../pages/packagePrice.ts'

type UnavailableReason =
  | components['schemas']['OrderBusinessDraftUnavailableReason']
  | components['schemas']['ScheduleBusinessDraftUnavailableReason']
type GenerationFeedbackItem = { state: 'generated' } | { state: 'unavailable'; reason: UnavailableReason }
type GenerationFeedbackResult = {
  order_adjustment?: GenerationFeedbackItem
  schedule_duration?: GenerationFeedbackItem
}

const unavailableReasonLabels: Record<UnavailableReason, string> = {
  order_required: '需要先关联订单',
  order_cancelled: '关联订单已取消',
  business_calculation_overflow: '金额计算超出范围',
  duration_unknown: '预估时长仍未知',
  duration_not_positive: '预估时长必须大于零',
  duration_out_of_range: '预估时长超出范围',
  schedule_stage_ineligible: '当前订单阶段不适用档期建议',
  schedule_slot_not_future: '拍摄档期必须在未来',
}

export function validateOptionalAbsoluteTargetPriceYuan(value: string): string | null {
  if (value.trim() === '') return null
  return validatePackagePriceYuan(value)?.replaceAll('基础价', '绝对目标价') ?? null
}

export function optionalAbsoluteTargetPriceYuanToCents(value: string): number | null {
  if (value.trim() === '') return null
  return packagePriceYuanToCents(value)
}

export function businessDraftGenerationMessage(result: GenerationFeedbackResult): string {
  const messages: string[] = []
  if (result.order_adjustment) {
    messages.push(result.order_adjustment.state === 'generated'
      ? '订单价格草稿已生成'
      : `订单价格草稿不可用：${unavailableReasonLabel(result.order_adjustment.reason)}`)
  }
  if (result.schedule_duration) {
    messages.push(result.schedule_duration.state === 'generated'
      ? '档期时长草稿已生成'
      : `档期时长草稿不可用：${unavailableReasonLabel(result.schedule_duration.reason)}`)
  }
  return `${messages.join('；')}。订单和档期尚未修改。`
}

export function businessDraftUnavailableMessage(details: unknown): string | null {
  if (!isRecord(details)) return null
  const messages = [
    unavailableItemMessage(details.order_adjustment, '订单价格草稿'),
    unavailableItemMessage(details.schedule_duration, '档期时长草稿'),
  ].filter((item): item is string => item !== null)
  return messages.length > 0 ? `无法生成经营草稿：${messages.join('；')}` : null
}

export function unavailableReasonLabel(reason: UnavailableReason): string {
  return unavailableReasonLabels[reason]
}

function unavailableItemMessage(value: unknown, label: string): string | null {
  if (!isRecord(value) || value.state !== 'unavailable' || !isUnavailableReason(value.reason)) return null
  return `${label}：${unavailableReasonLabel(value.reason)}`
}

function isUnavailableReason(value: unknown): value is UnavailableReason {
  return typeof value === 'string' && Object.hasOwn(unavailableReasonLabels, value)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
