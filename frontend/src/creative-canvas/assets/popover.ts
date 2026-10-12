export type Point = { x: number; y: number }
export const anchorPoint = (el: HTMLElement): Point => {
  const r = el.getBoundingClientRect()
  return { x: r.left, y: r.bottom + 6 }
}
