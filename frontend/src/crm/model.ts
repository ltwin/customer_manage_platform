import { DICT, TODAY } from './prototypeData'
import type { Customer, Order, Package, Reminder, Slot } from './prototypeData'

export function formatPrice(cents: number | null | undefined): string {
  if (cents == null) return '—'
  return `¥${(cents / 100).toLocaleString('zh-CN', { maximumFractionDigits: 0 })}`
}

export function byId<T extends { id: string }>(items: T[], id: string | null | undefined): T | null {
  if (!id) return null
  return items.find((item) => item.id === id) ?? null
}

export function customerOrders(orders: Order[], customerId: string): Order[] {
  return orders.filter((order) => order.customer_id === customerId)
}

export function lastShotDate(orders: Order[], customerId: string): string | null {
  const dates = customerOrders(orders, customerId)
    .map((order) => order.shot_at)
    .filter((date): date is string => Boolean(date))
    .sort()
  return dates.at(-1) ?? null
}

export function visibleBirthday(birthday: string): string {
  return birthday ? birthday.replace('-', '/') : '—'
}

export function shortDate(date: string | null | undefined): string {
  return date ? date.replaceAll('-', '/') : '—'
}

export function customerAge(createdAt: string): string {
  const start = new Date(`${createdAt}T00:00:00+08:00`)
  const end = new Date(`${TODAY}T00:00:00+08:00`)
  const months = Math.max(0, (end.getFullYear() - start.getFullYear()) * 12 + end.getMonth() - start.getMonth())
  if (months < 1) return '本月'
  if (months < 12) return `${months} 个月`
  return `${Math.floor(months / 12)} 年 ${months % 12} 个月`
}

export function packagePricingText(pkg: Package): string {
  if (pkg.pricing_mode === 'per_photo') return `${formatPrice(pkg.base_price)} /张`
  if (pkg.pricing_mode === 'per_duration') {
    const unit = pkg.duration_minutes >= 60 ? `${pkg.duration_minutes / 60} 小时` : `${pkg.duration_minutes} 分钟`
    return `${formatPrice(pkg.base_price)} /${unit}`
  }
  return formatPrice(pkg.base_price)
}

export function slotOverlaps(a: Slot, b: Slot): boolean {
  return a.date === b.date && a.start < b.end && b.start < a.end
}

export function slotOverlapIds(slots: Slot[], slot: Slot): string[] {
  return slots.filter((candidate) => candidate.id !== slot.id && slotOverlaps(candidate, slot)).map((candidate) => candidate.id)
}

export function slotLabel(slot: Slot, orders: Order[], customers: Customer[]): string {
  const order = byId(orders, slot.order_id)
  if (!order) return slot.note
  const customer = byId(customers, order.customer_id)
  return `${customer?.display_name ?? '未知客户'} · ${order.title}`
}

export function orderStatusBadgeClass(status: Order['status']): string {
  if (status === 'retouching') return 'badge-warning'
  if (status === 'delivered') return 'badge-success'
  if (status === 'cancelled') return 'badge-danger'
  if (status === 'scheduled' || status === 'shot' || status === 'selected') return 'badge-accent'
  return 'badge-muted'
}

export function reminderBadgeClass(reminder: Reminder): string {
  if (reminder.status === 'done') return 'badge-muted'
  if (reminder.type === 'birthday') return 'badge-accent'
  if (reminder.type === 'follow_up') return 'badge-success'
  if (reminder.type === 'churn') return 'badge-warning'
  return 'badge-muted'
}

export function channelLabel(channel: Customer['channel']): string {
  return DICT.channel[channel]
}
