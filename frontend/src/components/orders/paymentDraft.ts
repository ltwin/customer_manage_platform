// 订单收款事实与应交日的编辑草稿（dashboard-v2 ITEM-5 写侧录入）。
// 金额字段无 null 语义（契约 §4.2）：空输入 = 不修改；outstanding 交给服务端 DEC-10 推定。
import type { OrderListItem, UpdateOrderBody } from '../../api/client.ts'
import { accountDateAtNoonToInstant } from '../schedule/timezone.ts'
import { localDateOfInstant } from '../../pages/dashboard/dashboardV2Model.ts'

export interface PaymentDraft {
  amountPaidYuan: string
  paidDate: string
  dueDate: string
  dueIsOverride: boolean
}

export function yuanInputToCents(input: string): number | null {
  const trimmed = input.trim()
  if (!trimmed) return null
  if (!/^\d+(\.\d{1,2})?$/.test(trimmed)) return null
  const yuan = Number(trimmed)
  return Math.round(yuan * 100)
}

export function hydratePaymentDraft(order: OrderListItem, timezone: string | null): PaymentDraft {
  return {
    amountPaidYuan: order.amount_paid > 0 ? String(order.amount_paid / 100) : '',
    paidDate: order.paid_at ? localDateOfInstant(order.paid_at, timezone) : '',
    dueDate: order.delivery_due_at ?? '',
    dueIsOverride: order.delivery_due_is_override,
  }
}

export function validatePaymentDraft(draft: PaymentDraft, _order: OrderListItem): string | null {
  const cents = yuanInputToCents(draft.amountPaidYuan)
  if (draft.amountPaidYuan.trim() && cents == null) {
    if (draft.amountPaidYuan.trim().startsWith('-')) return '已收金额不能为负数'
    return '金额最多两位小数'
  }
  return null
}

export function buildPaymentPatch(
  draft: PaymentDraft,
  order: OrderListItem,
  timezone: string | null,
): UpdateOrderBody {
  const patch: UpdateOrderBody = {}
  const cents = yuanInputToCents(draft.amountPaidYuan)
  if (cents != null && cents !== order.amount_paid) {
    patch.amount_paid = cents
  }
  const paidDateUnchanged =
    order.paid_at != null && localDateOfInstant(order.paid_at, timezone) === draft.paidDate
  if (draft.paidDate && !paidDateUnchanged) {
    patch.paid_at = timezone
      ? accountDateAtNoonToInstant(draft.paidDate, timezone)
      : `${draft.paidDate}T12:00:00.000Z`
  }
  if (draft.dueDate) {
    if (draft.dueDate !== (order.delivery_due_at ?? '')) patch.delivery_due_at = draft.dueDate
  } else if (order.delivery_due_is_override) {
    patch.delivery_due_at = null
  }
  return patch
}
