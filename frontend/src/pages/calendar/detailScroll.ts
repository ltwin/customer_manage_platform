/** 「当日详情」定位：只滚面板自己的滚动容器，不把整页一起拖走。 */

export const CALENDAR_DETAIL_BODY_SELECTOR = '.calendar-v2-detail-body'
/** 与 --dur-base(220ms) 同一量级，略长一点让长距离滚动看得清落点。 */
export const DETAIL_SCROLL_DURATION_MS = 260

export interface DetailScrollGeometry {
  scrollTop: number
  viewportHeight: number
  scrollHeight: number
  /** 目标条目相对容器内容顶部的偏移 */
  elementTop: number
  elementHeight: number
}

/** 把目标条目滚到容器中间；已经整条可见就原地不动，省掉一次无意义的滑动。 */
export function detailScrollTop(geometry: DetailScrollGeometry): number {
  const { scrollTop, viewportHeight, scrollHeight, elementTop, elementHeight } = geometry
  const visible = elementTop >= scrollTop && elementTop + elementHeight <= scrollTop + viewportHeight
  if (visible) return scrollTop
  const centered = elementTop - (viewportHeight - elementHeight) / 2
  const max = Math.max(0, scrollHeight - viewportHeight)
  return Math.min(Math.max(centered, 0), max)
}

/** 缓出：起步快、收尾稳，和 --ease 的观感一致。 */
export function detailScrollEase(progress: number): number {
  const clamped = Math.min(Math.max(progress, 0), 1)
  return 1 - (1 - clamped) ** 3
}

export function scrollSlotDetailIntoView(element: HTMLElement): void {
  const container = element.closest<HTMLElement>(CALENDAR_DETAIL_BODY_SELECTOR)
  if (!container) {
    element.scrollIntoView({ block: 'center' })
    return
  }
  const top = detailScrollTop({
    scrollTop: container.scrollTop,
    viewportHeight: container.clientHeight,
    scrollHeight: container.scrollHeight,
    elementTop: element.getBoundingClientRect().top - container.getBoundingClientRect().top + container.scrollTop,
    elementHeight: element.offsetHeight,
  })
  if (top === container.scrollTop) return
  if (prefersReducedMotion()) {
    container.scrollTop = top
    return
  }
  animateScrollTop(container, top)
}

// 自己按帧写 scrollTop，不用 behavior: 'smooth'：页面不可见时浏览器会把整段
// smooth 滚动直接丢掉（实测 document.visibilityState === 'hidden' 时一格都不滚），
// 而按帧写在页面回到前台时至少会落到终点。
function animateScrollTop(container: HTMLElement, top: number): void {
  const from = container.scrollTop
  const distance = top - from
  const start = performance.now()
  const step = (now: number) => {
    const progress = Math.min((now - start) / DETAIL_SCROLL_DURATION_MS, 1)
    container.scrollTop = from + distance * detailScrollEase(progress)
    if (progress < 1) window.requestAnimationFrame(step)
  }
  window.requestAnimationFrame(step)
}

function prefersReducedMotion(): boolean {
  return typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}
