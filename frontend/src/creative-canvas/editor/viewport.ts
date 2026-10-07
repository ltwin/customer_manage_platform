import type { CanvasNode } from './api.ts'

export type Viewport = { x: number; y: number; zoom: number }
export type Rect = { x: number; y: number; width: number; height: number }
export type Insets = { left: number; right: number; top: number; bottom: number }

// 画布缩放范围（PRD CV-02）与「适应画布」的倍率范围（CV-03）。
export const zoomLimits = { min: 0.1, max: 4 }
export const fitZoomLimits = { min: 0.2, max: 1.1 }
// 节点标签在卡片外上方，适应画布时把它算进包围盒。
const labelHeight = 28

const clamp = (v: number, min: number, max: number) =>
  Math.min(max, Math.max(min, v))

export function validViewport(v: unknown): v is Viewport {
  if (!v || typeof v !== 'object') return false
  const { x, y, zoom } = v as Record<string, unknown>
  return (
    [x, y, zoom].every((n) => typeof n === 'number' && Number.isFinite(n)) &&
    (zoom as number) >= zoomLimits.min &&
    (zoom as number) <= zoomLimits.max
  )
}

// 根节点的世界坐标包围盒，子节点坐标相对父组、已被组框包住。
export function nodeBounds(nodes: CanvasNode[]): Rect | null {
  const roots = nodes.filter((n) => !n.parent_id)
  if (!roots.length) return null
  const x0 = Math.min(...roots.map((n) => n.metadata.x))
  const y0 = Math.min(...roots.map((n) => n.metadata.y - labelHeight))
  const x1 = Math.max(...roots.map((n) => n.metadata.x + n.metadata.width))
  const y1 = Math.max(...roots.map((n) => n.metadata.y + n.metadata.height))
  return { x: x0, y: y0, width: x1 - x0, height: y1 - y0 }
}

// 把包围盒放进去掉 insets 后的可见区：倍率限制在 20%–110%，居中。
export function fitViewport(
  bounds: Rect,
  stage: { width: number; height: number },
  insets: Insets,
): Viewport {
  const w = Math.max(1, stage.width - insets.left - insets.right)
  const h = Math.max(1, stage.height - insets.top - insets.bottom)
  const zoom = clamp(
    Math.min(w / Math.max(1, bounds.width), h / Math.max(1, bounds.height)),
    fitZoomLimits.min,
    fitZoomLimits.max,
  )
  return {
    zoom,
    x: insets.left + (w - bounds.width * zoom) / 2 - bounds.x * zoom,
    y: insets.top + (h - bounds.height * zoom) / 2 - bounds.y * zoom,
  }
}

// 面板占位：资产库在左、助手在右，读实际 DOM 位置；窄屏用固定边距。
export function panelInsets(
  stage: DOMRect,
  library: DOMRect | null,
  agent: DOMRect | null,
): Insets {
  if (stage.width <= 760) return { left: 16, right: 16, top: 72, bottom: 80 }
  return {
    left: library ? Math.max(80, library.right - stage.left + 16) : 80,
    right: agent ? Math.max(40, stage.right - agent.left + 16) : 40,
    top: 90,
    bottom: 90,
  }
}
