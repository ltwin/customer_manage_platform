// 查看浮窗：左边看/播放，右边是信息面板。资产库、生成记录、画布节点共用。
// 调用方给出每一项的媒体描述（stage）和右侧面板（panel），浮窗只负责展示、翻页和播放。
import { ic, esc, isFloatingOpen } from './ui.mjs'

const STAR = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2.6l2.9 6 6.6.8-4.9 4.6 1.3 6.5L12 17.3l-5.9 3.2 1.3-6.5-4.9-4.6 6.6-.8z"/></svg>'

// 星级：最小半颗星。点星的左半边是 .5，右半边是整颗；再点当前分数清零。
export function starsHTML(value = 0, { size = 'md', label = '评分', readOnly = false } = {}) {
  const stars = Array.from({ length: 5 }, (_, i) => {
    const fill = Math.max(0, Math.min(1, value - i)) * 100
    return `<span class="star" data-i="${i}">${STAR}<span class="star-fill" style="width:${fill}%">${STAR}</span></span>`
  }).join('')
  const text = value ? `${value} 星` : '未评分'
  return readOnly
    ? `<span class="stars stars-${size} ro" aria-label="${text}">${stars}</span>`
    : `<span class="stars stars-${size}" role="slider" tabindex="0" aria-label="${esc(label)}" aria-valuemin="0" aria-valuemax="5" aria-valuenow="${value}" aria-valuetext="${text}" data-value="${value}">${stars}<span class="stars-v">${value ? value.toFixed(1) : ''}</span></span>`
}
// 在 root 上代理所有星级的点击、悬停预览和方向键
export function bindStars(root, onSet) {
  const pick = (el, e) => {
    const star = e.target.closest('.star')
    if (!star) return null
    const r = star.getBoundingClientRect()
    return Number(star.dataset.i) + (e.clientX - r.left < r.width / 2 ? 0.5 : 1)
  }
  const paint = (el, v) => el.querySelectorAll('.star').forEach((s, i) => {
    s.querySelector('.star-fill').style.width = `${Math.max(0, Math.min(1, v - i)) * 100}%`
  })
  root.addEventListener('pointermove', (e) => {
    const el = e.target.closest('.stars:not(.ro)')
    if (!el) return
    const v = pick(el, e)
    if (v != null) { paint(el, v); el.classList.add('preview') }
  })
  root.addEventListener('pointerout', (e) => {
    const el = e.target.closest('.stars:not(.ro)')
    if (el && !el.contains(e.relatedTarget)) { paint(el, Number(el.dataset.value)); el.classList.remove('preview') }
  })
  root.addEventListener('click', (e) => {
    const el = e.target.closest('.stars:not(.ro)')
    if (!el) return
    e.stopPropagation()
    const v = pick(el, e)
    if (v == null) return
    onSet(el, v === Number(el.dataset.value) ? 0 : v)
  })
  root.addEventListener('keydown', (e) => {
    const el = e.target.closest('.stars:not(.ro)')
    if (!el) return
    const v = Number(el.dataset.value)
    if (e.key === 'ArrowRight' || e.key === 'ArrowUp') { e.preventDefault(); e.stopPropagation(); onSet(el, Math.min(5, v + 0.5)) }
    if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') { e.preventDefault(); e.stopPropagation(); onSet(el, Math.max(0, v - 0.5)) }
  })
}

const fmt = (s) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, '0')}`
const parse = (t = '0:00') => t.split(':').reduce((a, b) => a * 60 + Number(b), 0)

let current = null

// opts: { count, index, stage(i) → {kind, src, title, text, url, duration, ratio}, panel(i) → html, onPanel(event, i, viewer), onClose }
export function openViewer(opts) {
  current?.close()
  const back = document.createElement('div')
  back.className = 'viewer-back'
  back.innerHTML = `<div class="viewer" role="dialog" aria-modal="true" aria-label="查看">
    <div class="v-stage" id="vStage"></div>
    <aside class="v-panel" id="vPanel"></aside>
    <button class="v-close icon-btn" aria-label="关闭">${ic('x')}</button>
  </div>`
  document.body.append(back)
  const stageEl = back.querySelector('#vStage')
  const panelEl = back.querySelector('#vPanel')
  let index = opts.index
  let timer = null
  const prevFocus = document.activeElement

  const stop = () => { clearInterval(timer); timer = null }
  function drawStage() {
    stop()
    const it = opts.stage(index)
    const nav = `<button class="v-nav prev" aria-label="上一项" ${index ? '' : 'disabled'}>${ic('chevron-left')}</button><button class="v-nav next" aria-label="下一项" ${index < opts.count - 1 ? '' : 'disabled'}>${ic('chevron-right')}</button><span class="v-count">${index + 1} / ${opts.count}</span>`
    let body = ''
    if (it.kind === 'image') body = `<div class="v-media v-image"><img src="${it.src}" alt="${esc(it.title)}" draggable="false"></div><button class="v-zoom" hidden aria-label="还原缩放"></button>`
    else if (it.kind === 'video' || it.kind === 'audio') {
      const total = parse(it.duration)
      const bars = Array.from({ length: 90 }, (_, i) => 10 + Math.abs(Math.sin(i * 0.9 + 1.3) * Math.cos(i * 0.23)) * 70)
      body = `<div class="v-media v-${it.kind}">
        ${it.kind === 'video' ? `<img src="${it.src}" alt="" draggable="false">` : `<div class="v-wave">${bars.map((h) => `<i style="height:${h}%"></i>`).join('')}</div>`}
        <button class="v-bigplay" aria-label="播放">${ic('play')}</button>
        <div class="v-controls"><button class="v-play icon-btn" aria-label="播放">${ic('play')}</button>
          <input class="v-seek" type="range" min="0" max="${total}" step="0.1" value="0" aria-label="进度">
          <span class="v-time">0:00 / ${it.duration}</span>
          <button class="icon-btn" aria-label="全屏">${ic('maximize')}</button></div></div>`
    } else if (it.kind === 'text') body = `<article class="v-media v-text"><h2>${esc(it.title)}</h2><p>${esc(it.text)}</p></article>`
    else if (it.kind === 'link') body = `<div class="v-media v-link">${it.src ? `<img src="${it.src}" alt="">` : `<div class="v-link-blank">${ic('globe')}</div>`}<div><strong>${esc(it.title)}</strong><span>${esc(it.url)}</span><button class="btn btn-primary" data-open-url>${ic('external-link')}打开网址</button></div></div>`
    // 画幅铺不满时，两侧用同一张图的模糊放大填充，不留黑边
    const backdrop = it.src && (it.kind === 'image' || it.kind === 'video') ? `<div class="v-backdrop" style="background-image:url(${it.src})" aria-hidden="true"></div>` : ''
    stageEl.innerHTML = backdrop + body + nav
    stageEl.dataset.kind = it.kind
  }
  function drawPanel() {
    panelEl.innerHTML = opts.panel(index)
  }
  function go(i) {
    if (i < 0 || i >= opts.count) return
    index = i
    zoom = { s: 1, x: 0, y: 0 }
    drawStage()
    drawPanel()
  }
  function play() {
    const seek = stageEl.querySelector('.v-seek')
    if (!seek) return
    const media = stageEl.querySelector('.v-media')
    if (timer) { stop(); media.classList.remove('playing'); return }
    media.classList.add('playing')
    if (Number(seek.value) >= Number(seek.max)) seek.value = 0
    timer = setInterval(() => {
      seek.value = Number(seek.value) + 0.1
      sync()
      if (Number(seek.value) >= Number(seek.max)) { stop(); media.classList.remove('playing') }
    }, 100)
  }
  function sync() {
    const seek = stageEl.querySelector('.v-seek')
    const it = opts.stage(index)
    stageEl.querySelector('.v-time').textContent = `${fmt(Number(seek.value))} / ${it.duration}`
    seek.style.setProperty('--p', `${(seek.value / seek.max) * 100}%`)
    stageEl.querySelectorAll('.v-wave i').forEach((b, i, all) => b.classList.toggle('on', i / all.length < seek.value / seek.max))
  }

  stageEl.addEventListener('click', (e) => {
    if (e.target.closest('.prev')) return go(index - 1)
    if (e.target.closest('.next')) return go(index + 1)
    if (e.target.closest('.v-play, .v-bigplay')) return play()
    if (e.target.closest('.v-zoom')) return setZoom(1)
  })
  stageEl.addEventListener('input', (e) => { if (e.target.closest('.v-seek')) sync() })
  // 缩放：图片已经按画幅放到最大，不再提供点击放大；
  // 只用滚轮和触控板双指缩放，以指针为中心，放大后拖动或两指滑动平移。
  let zoom = { s: 1, x: 0, y: 0 }
  const MAX = 8
  const imgEl = () => stageEl.querySelector('.v-image img')
  function paintZoom() {
    const img = imgEl()
    if (!img) return
    img.style.transform = `translate(${zoom.x}px, ${zoom.y}px) scale(${zoom.s})`
    stageEl.classList.toggle('zoomed', zoom.s > 1)
    const pill = stageEl.querySelector('.v-zoom')
    pill.hidden = zoom.s === 1
    pill.textContent = `${Math.round(zoom.s * 100)}% · 还原`
  }
  function clampPan() {
    const img = imgEl()
    const w = img.offsetWidth, h = img.offsetHeight
    const sw = stageEl.clientWidth, sh = stageEl.clientHeight
    // 放大后的图片边缘最多推到舞台边缘，不会被拖出视野
    const mx = Math.max(0, (w * zoom.s - sw) / 2), my = Math.max(0, (h * zoom.s - sh) / 2)
    zoom.x = Math.max(-mx, Math.min(mx, zoom.x))
    zoom.y = Math.max(-my, Math.min(my, zoom.y))
  }
  function setZoom(next, at) {
    const img = imgEl()
    if (!img) return
    next = Math.max(1, Math.min(MAX, next))
    if (next === 1) zoom = { s: 1, x: 0, y: 0 }
    else {
      const r = stageEl.getBoundingClientRect()
      // 指针相对舞台中心的位置保持不动
      const qx = (at?.x ?? r.left + r.width / 2) - (r.left + r.width / 2)
      const qy = (at?.y ?? r.top + r.height / 2) - (r.top + r.height / 2)
      zoom.x = qx - (next / zoom.s) * (qx - zoom.x)
      zoom.y = qy - (next / zoom.s) * (qy - zoom.y)
      zoom.s = next
      clampPan()
    }
    paintZoom()
  }
  stageEl.addEventListener('wheel', (e) => {
    if (!imgEl() || e.target.closest('.v-controls')) return
    e.preventDefault()
    // 触控板捏合在浏览器里是带 ctrlKey 的 wheel；鼠标滚轮是整数步长、没有横向量
    const mouse = !e.ctrlKey && e.deltaX === 0 && Number.isInteger(e.deltaY) && Math.abs(e.deltaY) >= 40
    if (e.ctrlKey || mouse) setZoom(zoom.s * Math.exp(-e.deltaY * (e.ctrlKey ? 0.01 : 0.0015)), { x: e.clientX, y: e.clientY })
    else if (zoom.s > 1) { zoom.x -= e.deltaX; zoom.y -= e.deltaY; clampPan(); paintZoom() }
  }, { passive: false })
  // Safari 的捏合手势
  let gestureBase = 1
  stageEl.addEventListener('gesturestart', (e) => { e.preventDefault(); gestureBase = zoom.s })
  stageEl.addEventListener('gesturechange', (e) => { e.preventDefault(); setZoom(gestureBase * e.scale, { x: e.clientX, y: e.clientY }) })
  stageEl.addEventListener('pointerdown', (e) => {
    if (zoom.s === 1 || !e.target.closest('.v-image')) return
    e.preventDefault()
    const sx = e.clientX - zoom.x, sy = e.clientY - zoom.y
    stageEl.classList.add('panning')
    const mv = (ev) => { zoom.x = ev.clientX - sx; zoom.y = ev.clientY - sy; clampPan(); paintZoom() }
    const up = () => { stageEl.classList.remove('panning'); removeEventListener('pointermove', mv); removeEventListener('pointerup', up) }
    addEventListener('pointermove', mv)
    addEventListener('pointerup', up)
  })
  panelEl.addEventListener('click', (e) => opts.onPanel?.(e, index, api))
  panelEl.addEventListener('change', (e) => opts.onPanel?.(e, index, api))
  back.addEventListener('click', (e) => { if (e.target === back || e.target.closest('.v-close')) close() })
  // 浮窗打开时按键只归它管，不再落到底下的页面
  function onKey(e) {
    if (isFloatingOpen()) return
    // 输入框和星级自己处理按键（星级用方向键调分）
    if (e.target.closest?.('input:not([type=range]), textarea, [contenteditable], .stars')) {
      if (e.key === 'Escape') { e.stopPropagation(); e.target.blur() }
      return
    }
    e.stopPropagation()
    if (e.key === 'Escape') { e.preventDefault(); close() }
    else if (e.key === 'ArrowRight') go(index + 1)
    else if (e.key === 'ArrowLeft') go(index - 1)
    else if (e.key === ' ' && opts.stage(index).kind.match(/video|audio/)) { e.preventDefault(); play() }
    else if (e.key === ' ') { e.preventDefault(); close() }
    else opts.onKey?.(e, index, api)
  }
  document.addEventListener('keydown', onKey, true)
  function close() {
    stop()
    document.removeEventListener('keydown', onKey, true)
    back.remove()
    current = null
    opts.onClose?.(index)
    prevFocus?.focus?.({ preventScroll: true })
  }
  const api = { close, refresh: drawPanel, go, get index() { return index } }
  current = api
  go(index)
  back.querySelector('.v-close').focus()
  return api
}
