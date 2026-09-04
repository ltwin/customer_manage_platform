import { useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

import SlotShootMeta from './SlotShootMeta'
import { calendarSlotTitle, calendarSlotTypeLabel } from './format'
import { slotHoverCardPosition, type HoverCardPosition } from './slotHoverGeometry'
import type { SlotHoverTarget } from './useSlotHoverCard'

/** 纯视觉补充（读屏走「当日详情」），所以整块 aria-hidden 且不吃指针事件。 */
export default function SlotHoverCard({ target }: { target: SlotHoverTarget | null }) {
  const cardRef = useRef<HTMLDivElement>(null)
  const [placement, setPlacement] = useState<(HoverCardPosition & { target: SlotHoverTarget }) | null>(null)

  useLayoutEffect(() => {
    const card = cardRef.current
    if (!target || !card) {
      setPlacement(null)
      return
    }
    // 先按 0,0 渲染量一次真实高度，再定位；卡片宽度固定，量出来的高度不会因此偏。
    const rect = card.getBoundingClientRect()
    setPlacement({
      target,
      ...slotHoverCardPosition(
        target.anchor,
        { width: rect.width, height: rect.height },
        { width: window.innerWidth, height: window.innerHeight },
      ),
    })
  }, [target])

  if (!target) return null
  const placed = placement?.target === target ? placement : null
  const slot = target.projection.slot
  return createPortal(
    <div
      ref={cardRef}
      className={`calendar-v2-hover-card${target.projection.cancelled ? ' cancelled' : ''}`}
      aria-hidden="true"
      style={{ left: placed?.left ?? 0, top: placed?.top ?? 0, visibility: placed ? 'visible' : 'hidden' }}
    >
      <div className="calendar-v2-slot-head">
        <span className={`calendar-v2-filter-dot slot-${slot.type}`} />
        <span className="badge badge-muted">{calendarSlotTypeLabel(slot.type)}</span>
        <time>{target.timeLabel}</time>
      </div>
      <h3>{calendarSlotTitle(slot)}</h3>
      {slot.type === 'shoot' && <SlotShootMeta slot={slot} />}
      {/* 预留 / 个人占用的标题本身就是备注，别在卡片里重复一遍 */}
      {slot.note && slot.note !== calendarSlotTitle(slot) && <p>{slot.note}</p>}
      {target.projection.conflicting && <div className="calendar-v2-conflict">时间重叠 · 保存仍允许，请人工确认</div>}
      {target.projection.cancelled && <div className="calendar-v2-hover-card-tag">已取消，不占可约时间</div>}
    </div>,
    document.body,
  )
}
