export type Zoom = { s: number; x: number; y: number }
export type Bounds = {
  width: number
  height: number
  imageWidth: number
  imageHeight: number
}
export function clampPan(zoom: Zoom, box: Bounds): Zoom {
  const mx = Math.max(0, (box.imageWidth * zoom.s - box.width) / 2)
  const my = Math.max(0, (box.imageHeight * zoom.s - box.height) / 2)
  return {
    s: zoom.s,
    x: Math.max(-mx, Math.min(mx, zoom.x)),
    y: Math.max(-my, Math.min(my, zoom.y)),
  }
}
export function zoomAt(
  zoom: Zoom,
  scale: number,
  point: { x: number; y: number },
  box: Bounds,
): Zoom {
  const s = Math.max(1, Math.min(8, scale))
  if (s === 1) return { s: 1, x: 0, y: 0 }
  return clampPan(
    {
      s,
      x: point.x - (s / zoom.s) * (point.x - zoom.x),
      y: point.y - (s / zoom.s) * (point.y - zoom.y),
    },
    box,
  )
}
/** 与 v6 原型一致：整数大步长的纯纵向滚轮缩放，其余触控板滑动平移。 */
export function wheelZoom(e: {
  ctrlKey: boolean
  deltaX: number
  deltaY: number
}): number | null {
  const mouse =
    !e.ctrlKey &&
    e.deltaX === 0 &&
    Number.isInteger(e.deltaY) &&
    Math.abs(e.deltaY) >= 40
  return e.ctrlKey || mouse
    ? Math.exp(-e.deltaY * (e.ctrlKey ? 0.01 : 0.0015))
    : null
}
