import { useEffect, useRef } from 'react'
import type { GenerationPhase as Phase } from './labelMeta.ts'

// 生成中的卡片遮罩：毛玻璃压住旧内容，上面一层半调网点。网点里有一个几何轮廓
// 在星形、圆、方圆之间缓慢变形旋转，只有沿轮廓跑的一段彗尾用琥珀（v6 里琥珀只表示运行中）。
// 排队时只剩网点与慢扫光；确认结果时轮廓停止变形、彗尾加速。

const phaseLabel: Record<Phase, string> = {
  queued: '排队中',
  running: '生成中',
  reconciling: '正在确认结果',
}

const pitch = 7 // 网点间距（CSS px）
const levels = 6 // 透明度分档，同档网点合成一条路径一次填充
const white = '244, 244, 245'
const amber = '226, 164, 83'

interface Surface {
  canvas: HTMLCanvasElement
  phase: Phase
  width: number
  height: number
  visible: boolean
  seed: number
}

// 所有生成中的卡片共用一个 rAF，并限到约 30fps；离开视口的卡片不画。
const surfaces = new Set<Surface>()
let frame = 0
let last = 0
const reducedMotion =
  typeof window !== 'undefined' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches

function tick(now: number) {
  frame = surfaces.size ? requestAnimationFrame(tick) : 0
  if (now - last < 33) return
  last = now
  for (const s of surfaces) if (s.visible) paint(s, now / 1000 + s.seed)
}

function attach(s: Surface) {
  surfaces.add(s)
  if (reducedMotion) paint(s, s.seed)
  else if (!frame) frame = requestAnimationFrame(tick)
}

function detach(s: Surface) {
  surfaces.delete(s)
  if (!surfaces.size && frame) {
    cancelAnimationFrame(frame)
    frame = 0
  }
}

function paint(s: Surface, t: number) {
  const { canvas, width: w, height: h, phase } = s
  const ctx = canvas.getContext('2d')
  if (!ctx || !w || !h) return
  const dpr = Math.min(window.devicePixelRatio || 1, 2)
  if (canvas.width !== Math.round(w * dpr)) canvas.width = Math.round(w * dpr)
  if (canvas.height !== Math.round(h * dpr)) canvas.height = Math.round(h * dpr)
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
  ctx.clearRect(0, 0, w, h)

  const running = phase === 'running'
  const shape = phase !== 'queued'
  const cx = w / 2,
    cy = h / 2,
    r = Math.min(w, h) * 0.3,
    // 指数 0.5 → 4：星形 → 圆 → 方圆；确认结果时定格在圆。
    n = running ? 2 ** Math.sin(t * 0.45) : 2,
    turn = t * (running ? 0.35 : 0.1),
    cos = Math.cos(turn),
    sin = Math.sin(turn),
    head = t * (phase === 'reconciling' ? 3.2 : 1.6),
    sweep = (((t * 0.22) % 1.5) - 0.25) * (w + h),
    reach = Math.max(w, h) * 0.6
  const cols = Math.floor(w / pitch),
    rows = Math.floor(h / pitch),
    ox = (w - (cols - 1) * pitch) / 2,
    oy = (h - (rows - 1) * pitch) / 2
  const paths = {
    white: Array.from({ length: levels }, () => new Path2D()),
    amber: Array.from({ length: levels }, () => new Path2D()),
  }
  for (let j = 0; j < rows; j++) {
    for (let i = 0; i < cols; i++) {
      const x = ox + i * pitch,
        y = oy + j * pitch,
        dx = x - cx,
        dy = y - cy,
        d = Math.hypot(dx, dy)
      let v = 0.07
      let lit = false
      if (shape) {
        const u = (dx * cos + dy * sin) / r,
          k = (-dx * sin + dy * cos) / r,
          q = (Math.abs(u) ** n + Math.abs(k) ** n) ** (1 / n),
          e = ((q - 1) * r) / 7,
          edge = Math.exp(-e * e)
        v += edge * 0.85 + (q < 1 ? 0.16 * (1 - q) : 0)
        if (edge > 0.45) {
          // 彗尾：轮廓上离旋转头部越近越亮，只有这一段是琥珀。
          const gap =
            (((head - Math.atan2(k, u)) % (Math.PI * 2)) + Math.PI * 2) %
            (Math.PI * 2)
          if (gap < 1.1) {
            lit = true
            v = Math.max(v, 1 - gap / 1.6)
          }
        }
      }
      if (running)
        v += (0.11 + 0.11 * Math.sin(d * 0.12 - t * 2.4)) * Math.exp(-d / reach)
      const band = (x + y - sweep) / 30
      v += Math.exp(-band * band) * (running ? 0.3 : 0.22)
      v = Math.min(v, 1)
      const level = Math.min(levels - 1, Math.floor(v * levels)),
        path = (lit ? paths.amber : paths.white)[level],
        radius = 0.35 + v * 1.75
      path.moveTo(x + radius, y)
      path.arc(x, y, radius, 0, Math.PI * 2)
    }
  }
  for (const [color, list] of [
    [white, paths.white],
    [amber, paths.amber],
  ] as const) {
    list.forEach((path, level) => {
      ctx.fillStyle = `rgba(${color}, ${0.18 + (0.8 * (level + 1)) / levels})`
      ctx.fill(path)
    })
  }
}

export function GeneratingVeil({ phase }: { phase: Phase }) {
  const canvas = useRef<HTMLCanvasElement>(null)
  const surface = useRef<Surface | null>(null)
  const initial = useRef(phase)
  useEffect(() => {
    const el = canvas.current
    if (!el) return
    const s: Surface = {
      canvas: el,
      phase: initial.current,
      width: el.clientWidth,
      height: el.clientHeight,
      visible: true,
      seed: Math.random() * 20,
    }
    surface.current = s
    const resize = new ResizeObserver(() => {
      s.width = el.clientWidth
      s.height = el.clientHeight
      if (reducedMotion) paint(s, s.seed)
    })
    const view = new IntersectionObserver(([entry]) => {
      s.visible = entry?.isIntersecting ?? true
    })
    resize.observe(el)
    view.observe(el)
    attach(s)
    return () => {
      resize.disconnect()
      view.disconnect()
      detach(s)
      surface.current = null
    }
  }, [])
  // 阶段变化只改参数，不重建画布。
  useEffect(() => {
    const s = surface.current
    if (!s) return
    s.phase = phase
    if (reducedMotion) paint(s, s.seed)
  }, [phase])
  return (
    <div
      className={`cc-generating is-${phase}`}
      role="status"
      aria-label={phaseLabel[phase]}
    >
      <canvas ref={canvas} aria-hidden="true" />
      <span className="cc-generating-state">
        <i aria-hidden="true" />
        {phaseLabel[phase]}
      </span>
    </div>
  )
}
