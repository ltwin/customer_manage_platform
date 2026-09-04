import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FocusEvent,
  type PointerEvent,
} from 'react'

import type { CalendarProjection } from './model'
import { SLOT_HOVER_CARD_DELAY_MS, type HoverCardRect } from './slotHoverGeometry'

export interface SlotHoverTarget {
  projection: CalendarProjection
  timeLabel: string
  anchor: HoverCardRect
}

export interface SlotHoverHandlers {
  onPointerEnter(event: PointerEvent<HTMLElement>): void
  onPointerLeave(): void
  onFocus(event: FocusEvent<HTMLElement>): void
  onBlur(): void
}

export interface SlotHoverController {
  target: SlotHoverTarget | null
  hide(): void
  handlers(projection: CalendarProjection, timeLabel: string): SlotHoverHandlers
}

/** 日历事项被时长压扁后信息看不全，悬停（或键盘聚焦）补一张完整摘要卡。 */
export function useSlotHoverCard(): SlotHoverController {
  const [target, setTarget] = useState<SlotHoverTarget | null>(null)
  const timerRef = useRef<number | null>(null)

  const clearTimer = useCallback(() => {
    if (timerRef.current === null) return
    window.clearTimeout(timerRef.current)
    timerRef.current = null
  }, [])

  const hide = useCallback(() => {
    clearTimer()
    setTarget(null)
  }, [clearTimer])

  const show = useCallback((
    element: HTMLElement,
    projection: CalendarProjection,
    timeLabel: string,
    delay: number,
  ) => {
    clearTimer()
    const open = () => {
      timerRef.current = null
      const rect = element.getBoundingClientRect()
      setTarget({
        projection,
        timeLabel,
        anchor: { top: rect.top, left: rect.left, width: rect.width, height: rect.height },
      })
    }
    if (delay <= 0) open()
    else timerRef.current = window.setTimeout(open, delay)
  }, [clearTimer])

  useEffect(() => clearTimer, [clearTimer])

  useEffect(() => {
    if (!target) return
    // 卡片按触发那一刻的位置固定，滚动或改窗口后就脱锚了——收起比跟随更老实。
    window.addEventListener('scroll', hide, true)
    window.addEventListener('resize', hide)
    return () => {
      window.removeEventListener('scroll', hide, true)
      window.removeEventListener('resize', hide)
    }
  }, [hide, target])

  const handlers = useCallback((projection: CalendarProjection, timeLabel: string): SlotHoverHandlers => ({
    onPointerEnter(event) {
      if (event.pointerType !== 'mouse') return
      show(event.currentTarget, projection, timeLabel, SLOT_HOVER_CARD_DELAY_MS)
    },
    onPointerLeave: hide,
    onFocus(event) {
      // 鼠标点击也会让按钮拿到焦点，只对键盘落焦弹卡片。
      if (!event.currentTarget.matches(':focus-visible')) return
      show(event.currentTarget, projection, timeLabel, 0)
    },
    onBlur: hide,
  }), [hide, show])

  return { target, hide, handlers }
}
