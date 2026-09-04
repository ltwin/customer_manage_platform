/** 事项悬停摘要卡的出现时机与定位；纯计算部分放这里，供 scripts/calendar-interaction.test.ts 固定。 */

export const SLOT_HOVER_CARD_DELAY_MS = 240
export const SLOT_HOVER_CARD_GAP = 10
export const SLOT_HOVER_CARD_MARGIN = 8

export interface HoverCardRect {
  top: number
  left: number
  width: number
  height: number
}

export interface HoverCardSize {
  width: number
  height: number
}

export interface HoverCardPosition {
  left: number
  top: number
}

/** 卡片贴事项右侧；右侧放不下换左侧；两侧都放不下就压在事项上，再夹回视口。 */
export function slotHoverCardPosition(
  anchor: HoverCardRect,
  card: HoverCardSize,
  viewport: HoverCardSize,
): HoverCardPosition {
  return {
    left: horizontalPosition(anchor, card, viewport),
    top: clamp(anchor.top, SLOT_HOVER_CARD_MARGIN, viewport.height - card.height - SLOT_HOVER_CARD_MARGIN),
  }
}

function horizontalPosition(anchor: HoverCardRect, card: HoverCardSize, viewport: HoverCardSize): number {
  const right = anchor.left + anchor.width + SLOT_HOVER_CARD_GAP
  if (right + card.width <= viewport.width - SLOT_HOVER_CARD_MARGIN) return right
  const left = anchor.left - SLOT_HOVER_CARD_GAP - card.width
  if (left >= SLOT_HOVER_CARD_MARGIN) return left
  return clamp(anchor.left, SLOT_HOVER_CARD_MARGIN, viewport.width - card.width - SLOT_HOVER_CARD_MARGIN)
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), Math.max(min, max))
}
