// 磁吸端口：指针靠近端口时提前显出 +，并轻微吸向指针；拖拽、平移、菜单打开时让位。
// 偏移用屏幕像素计算、按缩放换算回画布坐标，端口的真实锚点不动，连线不受影响。
import { ui } from './store.mjs'
import { isFloatingOpen } from './ui.mjs'

const RADIUS = 48 // 屏幕像素：进入这个范围就显出端口
const reduced = matchMedia('(prefers-reduced-motion: reduce)')

export function attachMagneticPorts(stage, blocked) {
  const states = new Map()
  let frame = 0, last = 0, pointer = null

  const wake = () => {
    if (!frame) { last = 0; frame = requestAnimationFrame(tick) }
  }
  function tick(time) {
    frame = 0
    const step = last ? Math.min(2, (time - last) / 16.67) : 1
    last = time
    const off = blocked() || isFloatingOpen()
    // 每帧最多一次几何读取，读完再统一写样式
    if (pointer) {
      const { x, y, target } = pointer
      pointer = null
      // 指针压在别的节点上时，不去点亮被它挡住的邻居端口
      const over = target instanceof Element ? target.closest('.node:not(.group)') : null
      for (const port of stage.querySelectorAll('.port')) {
        let s = states.get(port)
        if (!s) states.set(port, (s = { x: 0, y: 0, vx: 0, vy: 0, tx: 0, ty: 0 }))
        const r = port.getBoundingClientRect()
        const dx = x - (r.left + r.width / 2 - s.x)
        const dy = y - (r.top + r.height / 2 - s.y)
        const d = Math.hypot(dx, dy)
        const near = d < RADIUS && (!over || over === port.closest('.node'))
        const k = near ? Math.min(0.32, 12 / Math.max(1, d)) : 0
        s.near = near
        s.tx = dx * k
        s.ty = dy * k
      }
    }
    let moving = false
    for (const [port, s] of states) {
      if (!port.isConnected) { states.delete(port); continue }
      port.classList.toggle('near', s.near && !off)
      if (reduced.matches || off) {
        s.x = s.y = s.vx = s.vy = 0
      } else {
        // 弹簧 + 阻尼，轻柔跟随不晃
        s.vx = (s.vx + (s.tx - s.x) * 0.15 * step) * Math.pow(0.68, step)
        s.vy = (s.vy + (s.ty - s.y) * 0.15 * step) * Math.pow(0.68, step)
        s.x += s.vx * step
        s.y += s.vy * step
        if (Math.abs(s.tx - s.x) + Math.abs(s.ty - s.y) + Math.abs(s.vx) + Math.abs(s.vy) < 0.04) {
          s.x = s.tx; s.y = s.ty; s.vx = s.vy = 0
        } else moving = true
      }
      port.style.setProperty('--mx', `${s.x / ui.view.k}px`)
      port.style.setProperty('--my', `${s.y / ui.view.k}px`)
    }
    if (moving) frame = requestAnimationFrame(tick)
  }
  function release() {
    pointer = null
    for (const s of states.values()) { s.tx = s.ty = 0; s.near = false }
    wake()
  }
  stage.addEventListener('pointermove', (e) => {
    if (e.buttons || e.pointerType === 'touch') return release()
    pointer = { x: e.clientX, y: e.clientY, target: e.target }
    wake()
  })
  stage.addEventListener('pointerleave', release)
  stage.addEventListener('pointerdown', release)
  stage.addEventListener('wheel', release, { passive: true })
  addEventListener('blur', release)
  reduced.addEventListener('change', release)
}
