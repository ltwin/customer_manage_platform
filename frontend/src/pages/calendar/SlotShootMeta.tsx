import type { ScheduleSlotListItem } from '../../api/client'
import {
  calendarOrderStatusLabel,
  calendarShootTypeLabel,
  formatCalendarMoney,
} from './format'

export type ShootSlot = Extract<ScheduleSlotListItem, { type: 'shoot' }>

/** 拍摄档期的订单摘要行；当日详情与悬停摘要卡共用同一份口径。 */
export default function SlotShootMeta({ slot }: { slot: ShootSlot }) {
  return (
    <div className="calendar-v2-shoot-meta">
      <span>{slot.package_name ?? '未选套系'} · {calendarShootTypeLabel(slot.package_shoot_type)}</span>
      <span>{calendarOrderStatusLabel(slot.order_status)} · {formatCalendarMoney(slot.order_price)}</span>
      <span>定金 {slot.order_deposit_paid ? '已收' : '未收'} · 尾款 {slot.order_balance_paid ? '已收' : '未收'}</span>
      {slot.customer_status === 'archived' && <span className="warning-text">客户已归档</span>}
      {slot.order_status === 'cancelled' && <span className="danger-text">订单已取消，档期保留但不占可约时间</span>}
    </div>
  )
}
