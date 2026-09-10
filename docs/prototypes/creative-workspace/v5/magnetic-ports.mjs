// All attraction distances use screen pixels, independent of canvas zoom.
export function createMagneticPorts(surface, scale, blocked) {
  const states = new Map();
  const reduced = matchMedia("(prefers-reduced-motion: reduce)");
  let frame = 0,
    last = 0;
  function wake() {
    if (!frame) {
      last = 0;
      frame = requestAnimationFrame(tick);
    }
  }
  function tick(time) {
    frame = 0;
    const step = last ? Math.min(2, (time - last) / 16.67) : 1;
    last = time;
    let moving = false;
    for (const [port, s] of states) {
      if (!port.isConnected) {
        states.delete(port);
        continue;
      }
      if (port.classList.contains("connecting")) continue;
      if (reduced.matches) {
        s.x = s.y = s.vx = s.vy = 0;
      } else {
        s.vx = (s.vx + (s.tx - s.x) * 0.15 * step) * Math.pow(0.68, step);
        s.vy = (s.vy + (s.ty - s.y) * 0.15 * step) * Math.pow(0.68, step);
        s.x += s.vx * step;
        s.y += s.vy * step;
        if (
          Math.abs(s.tx - s.x) +
            Math.abs(s.ty - s.y) +
            Math.abs(s.vx) +
            Math.abs(s.vy) <
          0.04
        ) {
          s.x = s.tx;
          s.y = s.ty;
          s.vx = s.vy = 0;
        } else moving = true;
      }
      port.style.setProperty("--magnet-x", s.x / scale() + "px");
      port.style.setProperty("--magnet-y", s.y / scale() + "px");
    }
    if (moving) frame = requestAnimationFrame(tick);
  }
  function release() {
    for (const [port, s] of states) {
      s.tx = s.ty = 0;
      port.classList.remove("near-pointer");
    }
    wake();
  }
  surface.addEventListener("pointermove", (event) => {
    if (event.pointerType === "touch" || blocked() || event.buttons) return;
    // Do not activate controls behind floating tools or another node.
    const hit = event.target.closest("[data-node]");
    if (event.target.closest(".canvas-bottom,.selection-tools"))
      return release();
    for (const port of surface.querySelectorAll(".node-port")) {
      let s = states.get(port);
      if (!s) {
        s = { x: 0, y: 0, vx: 0, vy: 0, tx: 0, ty: 0 };
        states.set(port, s);
      }
      const rect = port.getBoundingClientRect();
      const dx = event.clientX - (rect.left + rect.width / 2 - s.x);
      const dy = event.clientY - (rect.top + rect.height / 2 - s.y);
      const distance = Math.hypot(dx, dy);
      const near =
        distance < 48 && (!hit || hit === port.closest("[data-node]"));
      port.classList.toggle("near-pointer", near);
      const strength = near ? Math.min(0.32, 12 / Math.max(1, distance)) : 0;
      s.tx = dx * strength;
      s.ty = dy * strength;
    }
    wake();
  });
  surface.addEventListener("pointerleave", release);
  window.addEventListener("blur", release);
  return {
    release,
    reset() {
      cancelAnimationFrame(frame);
      frame = 0;
      states.clear();
    },
  };
}
