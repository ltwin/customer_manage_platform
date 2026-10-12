export type Selection = { ids: string[]; anchor: string | null }
export type Modifiers = { additive?: boolean; shift?: boolean }
export function clickSelection(
  current: Selection,
  id: string,
  order: string[],
  keys: Modifiers,
): Selection {
  const anchor = current.anchor ? order.indexOf(current.anchor) : -1
  if (keys.shift && anchor >= 0 && order.includes(id)) {
    const end = order.indexOf(id)
    return {
      ids: order.slice(Math.min(anchor, end), Math.max(anchor, end) + 1),
      anchor: current.anchor,
    }
  }
  return {
    ids: keys.additive
      ? current.ids.includes(id)
        ? current.ids.filter((x) => x !== id)
        : [...current.ids, id]
      : [id],
    anchor: id,
  }
}
export type Box = { left: number; right: number; top: number; bottom: number }
export function boxSelection(
  base: string[],
  cards: (Box & { id: string })[],
  box: Box,
): string[] {
  return [
    ...new Set([
      ...base,
      ...cards
        .filter(
          (r) =>
            r.left < box.right &&
            r.right > box.left &&
            r.top < box.bottom &&
            r.bottom > box.top,
        )
        .map((r) => r.id),
    ]),
  ]
}
export const halfRating = (star: number, fraction: number) =>
  star - (fraction < 0.5 ? 0.5 : 0)
export const stepRating = (
  value: number | null,
  direction: number,
): number | null =>
  Math.max(0, Math.min(5, (value ?? 0) + direction * 0.5)) || null
