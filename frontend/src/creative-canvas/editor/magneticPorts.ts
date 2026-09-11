// v5 spring parameters. Offsets use screen pixels; graph handles stay fixed.
export function attachMagneticPorts(
  surface: HTMLElement,
  scale: () => number,
  blocked: () => boolean,
) {
  const reduced = matchMedia('(prefers-reduced-motion: reduce)')
  const states = new Map<
    HTMLButtonElement,
    {
      x: number
      y: number
      vx: number
      vy: number
      tx: number
      ty: number
    }
  >()
  let frame = 0,
    last = 0,
    pointer: PointerEvent | null = null
  const wake = () => {
    if (!frame) {
      last = 0
      frame = requestAnimationFrame(tick)
    }
  }
  function tick(time: number) {
    frame = 0
    const step = last ? Math.min(2, (time - last) / 16.67) : 1
    last = time
    // At most one geometry read pass per frame, before any style writes.
    if (pointer) {
      const event = pointer
      pointer = null
      const hit =
        event.target instanceof Element
          ? event.target.closest('.react-flow__node')
          : null
      for (const port of Array.from(
        surface.querySelectorAll<HTMLButtonElement>('.cc-port-target button'),
      )) {
        let state = states.get(port)
        if (!state) {
          state = { x: 0, y: 0, vx: 0, vy: 0, tx: 0, ty: 0 }
          states.set(port, state)
        }
        const rect = port.getBoundingClientRect()
        const dx = event.clientX - (rect.left + rect.width / 2 - state.x)
        const dy = event.clientY - (rect.top + rect.height / 2 - state.y)
        const distance = Math.hypot(dx, dy)
        const near =
          !port.disabled &&
          distance < 48 &&
          (!hit || hit === port.closest('.react-flow__node'))
        const strength = near ? Math.min(0.32, 12 / Math.max(1, distance)) : 0
        state.tx = dx * strength
        state.ty = dy * strength
      }
    }
    let moving = false
    const zoom = scale()
    for (const [port, state] of states) {
      if (!port.isConnected) {
        states.delete(port)
        continue
      }
      const near = state.tx !== 0 || state.ty !== 0
      port.classList.toggle('near-pointer', near && !blocked())
      if (reduced.matches || blocked()) {
        state.x = state.y = state.vx = state.vy = 0
      } else {
        state.vx =
          (state.vx + (state.tx - state.x) * 0.15 * step) * Math.pow(0.68, step)
        state.vy =
          (state.vy + (state.ty - state.y) * 0.15 * step) * Math.pow(0.68, step)
        state.x += state.vx * step
        state.y += state.vy * step
        if (
          Math.abs(state.tx - state.x) +
            Math.abs(state.ty - state.y) +
            Math.abs(state.vx) +
            Math.abs(state.vy) <
          0.04
        ) {
          state.x = state.tx
          state.y = state.ty
          state.vx = state.vy = 0
        } else moving = true
      }
      port.style.setProperty('--magnet-x', `${state.x / zoom}px`)
      port.style.setProperty('--magnet-y', `${state.y / zoom}px`)
    }
    if (moving) frame = requestAnimationFrame(tick)
  }
  function release() {
    pointer = null
    for (const state of states.values()) state.tx = state.ty = 0
    wake()
  }
  function move(event: PointerEvent) {
    if (event.buttons) return
    if (
      event.pointerType === 'touch' ||
      blocked() ||
      !(event.target instanceof Element) ||
      !event.target.closest('.react-flow')
    ) {
      release()
      return
    }
    pointer = event
    wake()
  }
  surface.addEventListener('pointermove', move)
  surface.addEventListener('pointerleave', release)
  surface.addEventListener('wheel', release, { passive: true })
  window.addEventListener('blur', release)
  reduced.addEventListener('change', release)
  return () => {
    cancelAnimationFrame(frame)
    surface.removeEventListener('pointermove', move)
    surface.removeEventListener('pointerleave', release)
    surface.removeEventListener('wheel', release)
    window.removeEventListener('blur', release)
    reduced.removeEventListener('change', release)
    for (const port of states.keys()) {
      port.classList.remove('near-pointer')
      port.style.removeProperty('--magnet-x')
      port.style.removeProperty('--magnet-y')
    }
    states.clear()
  }
}
