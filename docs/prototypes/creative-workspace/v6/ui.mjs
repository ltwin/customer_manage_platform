// 通用浮层：图标、菜单、弹层、提示、toast。一次只存在一个菜单/弹层。
import { icons } from '../v5/icons.mjs'

export const ic = (name) => `<span class="i">${icons[name] ?? ''}</span>`
export const esc = (s) =>
  String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c])

export function hydrateIcons(root = document) {
  root.querySelectorAll('[data-i]').forEach((el) => {
    el.innerHTML = icons[el.dataset.i] ?? ''
    el.removeAttribute('data-i')
  })
}

let floating = null
let floatingOwner = null
let floatingClose = null
export function closeFloating() {
  if (!floating) return
  floating.remove()
  floatingOwner?.setAttribute('aria-expanded', 'false')
  floating = floatingOwner = null
  const done = floatingClose
  floatingClose = null
  done?.()
}
// 当前浮层关闭时（选中菜单项、点外面、Esc、视口移动）执行一次
export const onFloatingClose = (fn) => { floatingClose = fn }
export const isFloatingOpen = () => Boolean(floating)

// 以锚点矩形或坐标放置，自动避让视口边缘
function place(el, at) {
  document.body.append(el)
  const r = el.getBoundingClientRect()
  let x = at.x, y = at.y
  if (at.rect) {
    x = at.align === 'end' ? at.rect.right - r.width : at.rect.left
    y = at.rect.bottom + 6
    if (y + r.height > innerHeight - 8) y = at.rect.top - r.height - 6
  }
  x = Math.max(8, Math.min(x, innerWidth - r.width - 8))
  y = Math.max(8, Math.min(y, innerHeight - r.height - 8))
  el.style.left = `${x}px`
  el.style.top = `${y}px`
}

// items: {label, icon, hint, danger, disabled, checked, sub, run} | 'sep' | {section}
export function openMenu(items, at, owner) {
  closeFloating()
  const el = document.createElement('div')
  el.className = 'menu'
  el.setAttribute('role', 'menu')
  for (const it of items) {
    if (it === 'sep') { el.insertAdjacentHTML('beforeend', '<div class="menu-sep"></div>'); continue }
    if (it.section) { el.insertAdjacentHTML('beforeend', `<div class="menu-label">${esc(it.section)}</div>`); continue }
    const b = document.createElement('button')
    b.className = `menu-item${it.danger ? ' danger' : ''}${it.sub ? ' two' : ''}`
    b.setAttribute('role', 'menuitem')
    b.disabled = Boolean(it.disabled)
    b.innerHTML = `${it.icon ? ic(it.icon) : ''}<span>${esc(it.label)}${it.sub ? `<span class="sub">${esc(it.sub)}</span>` : ''}</span>${
      it.checked ? `<span class="check">${ic('check')}</span>` : it.hint ? `<span class="hint">${esc(it.hint)}</span>` : ''}`
    b.addEventListener('click', (e) => { e.stopPropagation(); closeFloating(); it.run?.() })
    el.append(b)
  }
  keyNav(el)
  place(el, at)
  floating = el
  floatingOwner = owner ?? null
  owner?.setAttribute('aria-expanded', 'true')
  el.querySelector('button:not(:disabled)')?.focus({ preventScroll: true })
  return el
}

export function openPopover(html, at, owner, className = '') {
  closeFloating()
  const el = document.createElement('div')
  el.className = `popover ${className}`
  el.innerHTML = html
  place(el, at)
  floating = el
  floatingOwner = owner ?? null
  owner?.setAttribute('aria-expanded', 'true')
  return el
}

function keyNav(menu) {
  menu.addEventListener('keydown', (e) => {
    const items = [...menu.querySelectorAll('button:not(:disabled)')]
    const i = items.indexOf(document.activeElement)
    if (e.key === 'ArrowDown') { e.preventDefault(); items[(i + 1) % items.length]?.focus() }
    if (e.key === 'ArrowUp') { e.preventDefault(); items[(i - 1 + items.length) % items.length]?.focus() }
  })
}

document.addEventListener('pointerdown', (e) => {
  if (floating && !floating.contains(e.target) && !floatingOwner?.contains(e.target)) closeFloating()
}, true)
document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && floating) { e.stopPropagation(); closeFloating() } }, true)

let toastTimer
// action：{ label, run }，例如「撤销」；带动作时多停留一会儿
export function toast(text, action) {
  document.querySelector('.toast')?.remove()
  const el = document.createElement('div')
  el.className = 'toast'
  el.setAttribute('role', 'status')
  el.textContent = text
  if (action) {
    const b = document.createElement('button')
    b.className = 'toast-act'
    b.textContent = action.label
    b.addEventListener('click', () => { el.remove(); action.run() })
    el.append(b)
  }
  document.body.append(el)
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => el.remove(), action ? 5000 : 2200)
}

export function confirmModal({ title, body, ok = '确定', cancel = '取消' }) {
  return new Promise((resolve) => {
    const back = document.createElement('div')
    back.className = 'modal-back'
    back.innerHTML = `<div class="modal" role="dialog" aria-modal="true" aria-label="${esc(title)}">
      <h3>${esc(title)}</h3><p>${esc(body)}</p>
      <div class="row"><button class="btn" data-v="0">${esc(cancel)}</button><button class="btn btn-primary" data-v="1">${esc(ok)}</button></div></div>`
    const done = (v) => { back.remove(); resolve(v) }
    back.addEventListener('click', (e) => {
      if (e.target === back) done(false)
      const v = e.target.closest('[data-v]')?.dataset.v
      if (v) done(v === '1')
    })
    back.addEventListener('keydown', (e) => { if (e.key === 'Escape') { e.stopPropagation(); done(false) } })
    document.body.append(back)
    back.querySelector('.btn-primary').focus()
  })
}

// 轻量提示：读 aria-label 与 data-kbd，悬停 400ms 后出现
let tipEl, tipTimer
document.addEventListener('pointerover', (e) => {
  const t = e.target.closest('[aria-label]:is(button,.avatar)')
  clearTimeout(tipTimer)
  tipEl?.remove()
  // 参考缩略图有自己的大图预览，不再叠一层文字提示
  if (!t || t.closest('.menu,.popover,[data-peek]')) return
  tipTimer = setTimeout(() => {
    const r = t.getBoundingClientRect()
    tipEl = document.createElement('div')
    tipEl.className = 'tip'
    tipEl.innerHTML = esc(t.getAttribute('aria-label')) + (t.dataset.kbd ? `<kbd>${esc(t.dataset.kbd)}</kbd>` : '')
    document.body.append(tipEl)
    const w = tipEl.offsetWidth, h = tipEl.offsetHeight
    const side = t.closest('.rail') && innerWidth > 760
    const x = side ? r.right + 8 : Math.max(8, Math.min(r.left + r.width / 2 - w / 2, innerWidth - w - 8))
    const y = side ? r.top + r.height / 2 - h / 2 : r.bottom + 8 + h > innerHeight ? r.top - h - 8 : r.bottom + 8
    tipEl.style.left = `${x}px`
    tipEl.style.top = `${y}px`
  }, 400)
})
document.addEventListener('pointerdown', () => { clearTimeout(tipTimer); tipEl?.remove() }, true)
