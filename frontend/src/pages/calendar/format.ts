import type { ScheduleSlotListItem } from '../../api/client'

export function calendarSlotTitle(slot: ScheduleSlotListItem): string {
  if (slot.type !== 'shoot') return slot.note || calendarSlotTypeLabel(slot.type)
  return `${slot.customer_display_name} · ${slot.order_title ?? slot.package_name ?? '未命名订单'}`
}

export function calendarSlotTypeLabel(type: string): string {
  return { shoot: '拍摄', hold: '预留', busy: '个人占用' }[type] ?? type
}

export function calendarOrderStatusLabel(status: string): string {
  return {
    consulting: '咨询',
    scheduled: '定档',
    shot: '已拍摄',
    selected: '已选片',
    retouching: '精修中',
    delivered: '已交付',
    closed: '完结',
    cancelled: '取消',
  }[status] ?? status
}

export function calendarShootTypeLabel(type?: string): string {
  if (!type) return '未分类'
  return { portrait: '人像', cosplay: 'Cosplay', other: '其他' }[type] ?? type
}

export function formatCalendarMoney(cents?: number): string {
  if (cents === undefined) return '未报价'
  return new Intl.NumberFormat('zh-CN', {
    style: 'currency',
    currency: 'CNY',
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  }).format(cents / 100)
}

export function formatCalendarDay(date: string): string {
  if (!date) return '当天档期'
  return `${Number(date.slice(5, 7))} 月 ${Number(date.slice(8, 10))} 日`
}

export function formatCalendarMonth(month: string): string {
  const [year, value] = month.split('-')
  return `${year} 年 ${Number(value)} 月`
}
