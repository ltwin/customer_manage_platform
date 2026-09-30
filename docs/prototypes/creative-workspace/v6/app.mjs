// 入口：外框控件、资产抽屉、全局快捷键；创作助手在 agent.mjs。
import { state, ui, byId, undo, redo, canUndo, canRedo, reset, save, checkpoint, descendants, roots, fitGroups, isGroup } from './store.mjs'
import { on, emit, render, commit, select, applyView, restoreView, fitView, zoomBy, addMenu, pickFiles, duplicate, remove, toWorld, groupSelection, ungroupSelection } from './canvas.mjs'
import { initComposer } from './composer.mjs'
import { initAgent } from './agent.mjs'
import * as library from './library.mjs'
import { ic, esc, hydrateIcons, openMenu, openPopover, toast, confirmModal } from './ui.mjs'

const $ = (id) => document.getElementById(id)
const stage = $('stage')
const setIcon = (el, name) => (el.innerHTML = ic(name))

// ---------- 顶栏 ----------
setIcon($('back'), 'arrow-left')
$('back').dataset.kbd = ''
$('project').querySelector('.chev').innerHTML = ic('chevron-down')
$('project-name').textContent = state.name
$('project').addEventListener('click', (e) => {
  openMenu([
    { label: '重命名项目', icon: 'pencil', run: renameProject },
    { label: '复制项目', icon: 'copy', run: () => toast('原型不复制项目') },
    { label: '导出画布', icon: 'download', run: () => toast('原型不导出') },
    'sep',
    { label: '重置示例数据', icon: 'refresh-cw', run: async () => (await confirmModal({ title: '重置示例数据？', body: '当前画布的修改会丢失，恢复为初始示例。', ok: '重置' })) && reset() },
    { label: '归档项目', icon: 'package', danger: true, run: () => toast('原型不归档') },
  ], { rect: e.currentTarget.getBoundingClientRect() }, e.currentTarget)
})
function renameProject() {
  const span = $('project-name')
  const input = Object.assign(document.createElement('input'), { value: state.name, className: 'project-input' })
  input.style.cssText = 'width:220px;height:28px;border:0;border-radius:6px;background:var(--s3);padding:0 8px;outline:none'
  span.replaceWith(input)
  input.focus()
  input.select()
  const done = () => {
    state.name = input.value.trim() || state.name
    document.title = `${state.name} · 创意空间`
    input.replaceWith(span)
    span.textContent = state.name
    save()
  }
  input.addEventListener('keydown', (e) => { e.stopPropagation(); if (e.key === 'Enter' || e.key === 'Escape') input.blur() })
  input.addEventListener('click', (e) => e.stopPropagation())
  input.addEventListener('blur', done, { once: true })
}
const credits = document.querySelector('.credits')
const paintCredits = () => (credits.lastChild.textContent = state.credits.toLocaleString())
on('credits', paintCredits)
paintCredits()

// ---------- 工具轨 ----------
const rail = document.querySelector('.rail')
setIcon($('tool-add'), 'plus')
$('tool-add').dataset.kbd = '双击画布'
rail.querySelector('[data-tool="select"]').innerHTML = ic('mouse-pointer-2')
rail.querySelector('[data-tool="hand"]').innerHTML = ic('hand')
setIcon($('assets-toggle'), 'library')
setIcon($('help'), 'command')
function setTool(t) {
  ui.tool = t
  rail.querySelectorAll('[data-tool]').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.tool === t)))
  stage.classList.toggle('tool-hand', t === 'hand')
}
setTool('select')
rail.addEventListener('click', (e) => {
  const t = e.target.closest('[data-tool]')?.dataset.tool
  if (t) setTool(t)
})
$('tool-add').addEventListener('click', (e) => {
  const r = e.currentTarget.getBoundingClientRect()
  addMenu({ x: r.right + 10, y: r.top })
})
$('help').addEventListener('click', (e) => {
  const rows = [
    ['添加节点', '双击画布'], ['平移', 'Space 拖动'], ['缩放', '⌘ 滚轮'], ['适应画布', '⇧ 1'],
    ['多选', '⇧ 点击 / 框选'], ['打组 / 解组', '⌘ G / ⇧⌘ G'], ['编辑文字', '双击 / Enter'],
    ['复制', '⌘ D'], ['删除', '⌫'], ['撤销 / 重做', '⌘ Z / ⇧⌘ Z'],
    ['全屏查看', 'Space'], ['生成', 'Enter'], ['引用素材', '@'], ['预设指令', '/'],
  ]
  const r = e.currentTarget.getBoundingClientRect()
  openPopover(`<div class="help">${rows.map(([a, b]) => `<div class="help-row"><span>${a}</span><span>${b.split(' ').map((k) => (/[a-zA-Z0-9⌘⇧⌫@/]/.test(k) ? `<kbd>${k}</kbd>` : k)).join(' ')}</span></div>`).join('')}</div>`,
    { x: r.right + 10, y: r.bottom - 360 }, e.currentTarget)
})
// ---------- 资产抽屉（见 library.mjs） ----------
$('assets-toggle').addEventListener('click', () => library.toggle())
on('assets', () => library.open())
on('assets-pick', (id) => library.open({ pickFor: id }))
on('upload', (id) => pickFiles(undefined, id))
on('fit', (ids) => fitView(ids))

// ---------- 缩放条 ----------
const zoombar = $('zoombar')
zoombar.innerHTML = `
  <button class="icon-btn" data-z="undo" aria-label="撤销" data-kbd="⌘Z">${ic('undo-2')}</button>
  <button class="icon-btn" data-z="redo" aria-label="重做" data-kbd="⇧⌘Z">${ic('redo-2')}</button>
  <span class="sep"></span>
  <button class="icon-btn" data-z="out" aria-label="缩小">${ic('minus')}</button>
  <button class="zoom-val" data-z="100" aria-label="缩放到 100%">100%</button>
  <button class="icon-btn" data-z="in" aria-label="放大">${ic('plus')}</button>
  <span class="sep"></span>
  <button class="icon-btn" data-z="fit" aria-label="适应画布" data-kbd="⇧1">${ic('maximize')}</button>
  <button class="icon-btn" data-z="map" aria-label="小地图" aria-pressed="false">${ic('map')}</button>`
const paintZoom = () => {
  zoombar.querySelector('.zoom-val').textContent = `${Math.round(ui.view.k * 100)}%`
  zoombar.querySelector('[data-z="undo"]').disabled = !canUndo()
  zoombar.querySelector('[data-z="redo"]').disabled = !canRedo()
  zoombar.querySelectorAll('button:disabled').forEach((b) => (b.style.opacity = 0.35))
  zoombar.querySelectorAll('button:not(:disabled)').forEach((b) => (b.style.opacity = ''))
  paintMinimap()
}
zoombar.addEventListener('click', (e) => {
  const z = e.target.closest('[data-z]')?.dataset.z
  if (z === 'undo' && undo()) commit()
  if (z === 'redo' && redo()) commit()
  if (z === 'in') zoomBy(1.2)
  if (z === 'out') zoomBy(1 / 1.2)
  if (z === '100') { const c = toWorld(innerWidth / 2, innerHeight / 2); ui.view = { k: 1, x: innerWidth / 2 - c.x, y: innerHeight / 2 - c.y }; applyView() }
  if (z === 'fit') fitView()
  if (z === 'map') {
    const mm = $('minimap')
    mm.hidden = !mm.hidden
    e.target.closest('[data-z]').setAttribute('aria-pressed', String(!mm.hidden))
    paintMinimap()
  }
})
function paintMinimap() {
  const mm = $('minimap')
  if (mm.hidden || !state.nodes.length) return
  const ctx = mm.getContext('2d')
  const W = mm.width, H = mm.height
  const tl = toWorld(0, 0), br = toWorld(innerWidth, innerHeight)
  const xs = state.nodes.flatMap((n) => [n.x, n.x + n.w]).concat(tl.x, br.x)
  const ys = state.nodes.flatMap((n) => [n.y, n.y + n.h]).concat(tl.y, br.y)
  const x0 = Math.min(...xs), y0 = Math.min(...ys)
  const s = Math.min((W - 16) / (Math.max(...xs) - x0), (H - 16) / (Math.max(...ys) - y0))
  const m = (x, y) => [8 + (x - x0) * s, 8 + (y - y0) * s]
  ctx.clearRect(0, 0, W, H)
  for (const n of state.nodes) {
    // 组只画描边，不遮住组内节点
    if (isGroup(n)) {
      ctx.strokeStyle = ui.sel.has(n.id) ? '#f4f4f5' : 'rgba(255,255,255,.18)'
      ctx.strokeRect(...m(n.x, n.y), n.w * s, n.h * s)
      continue
    }
    ctx.fillStyle = ui.sel.has(n.id) ? '#f4f4f5' : '#3a3a3e'
    ctx.fillRect(...m(n.x, n.y), n.w * s, n.h * s)
  }
  ctx.strokeStyle = 'rgba(255,255,255,.55)'
  ctx.strokeRect(...m(tl.x, tl.y), (br.x - tl.x) * s, (br.y - tl.y) * s)
  mm._map = { x0, y0, s }
}
$('minimap').addEventListener('click', (e) => {
  const { x0, y0, s } = $('minimap')._map ?? {}
  if (!s) return
  const r = e.currentTarget.getBoundingClientRect()
  const wx = x0 + (e.clientX - r.left - 8) / s, wy = y0 + (e.clientY - r.top - 8) / s
  ui.view.x = innerWidth / 2 - wx * ui.view.k
  ui.view.y = innerHeight / 2 - wy * ui.view.k
  applyView()
})
on('view', paintZoom)
on('change', paintZoom)

// ---------- 快捷键 ----------
document.addEventListener('keydown', (e) => {
  const typing = /input|textarea/i.test(e.target.tagName) || e.target.isContentEditable
  if (typing) return
  // 聚焦在按钮上时 Space / Enter 属于按钮本身
  if ((e.key === ' ' || e.key === 'Enter') && e.target.closest?.('button')) return
  const mod = e.metaKey || e.ctrlKey
  const k = e.key.toLowerCase()
  if (e.key === ' ' && !ui.sel.size) { ui.space = true; stage.classList.add('panning'); e.preventDefault() }
  if (mod && k === 'z') { e.preventDefault(); if (e.shiftKey ? redo() : undo()) commit() }
  else if (mod && k === 'd') { e.preventDefault(); if (ui.sel.size) duplicate([...ui.sel]) }
  else if (mod && k === 'g') { e.preventDefault(); e.shiftKey ? ungroupSelection() : groupSelection() }
  else if (mod && k === 'a') { e.preventDefault(); select(state.nodes.map((n) => n.id)) }
  else if (mod && (k === '=' || k === '+')) { e.preventDefault(); zoomBy(1.2) }
  else if (mod && k === '-') { e.preventDefault(); zoomBy(1 / 1.2) }
  else if (e.shiftKey && e.code === 'Digit1') fitView()
  else if (e.key === 'Backspace' || e.key === 'Delete') {
    if (ui.edgeSel) { checkpoint(); state.edges = state.edges.filter((x) => x.id !== ui.edgeSel); ui.edgeSel = null; commit(); emit('select') }
    else remove(new Set(ui.sel))
  } else if (e.key === 'Escape') { if (ui.sel.size || ui.edgeSel) select([]) }
  else if (!mod && k === 'v') setTool('select')
  else if (!mod && k === 'h') setTool('hand')
  else if (!mod && ['t', 'i', 'o'].includes(k)) {
    const c = { x: innerWidth / 2, y: innerHeight / 2 }
    addMenu(c)
    const idx = { t: 0, i: 1, o: 2 }[k]
    document.querySelectorAll('.menu .menu-item')[idx]?.click()
  } else if (['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(e.key) && ui.sel.size) {
    e.preventDefault()
    checkpoint()
    const d = e.shiftKey ? 10 : 1
    for (const id of descendants(roots([...ui.sel]))) {
      const n = byId(id)
      if (e.key === 'ArrowUp') n.y -= d
      if (e.key === 'ArrowDown') n.y += d
      if (e.key === 'ArrowLeft') n.x -= d
      if (e.key === 'ArrowRight') n.x += d
    }
    fitGroups()
    commit()
  }
})
document.addEventListener('keyup', (e) => { if (e.key === ' ') { ui.space = false; stage.classList.remove('panning') } })

// ---------- 启动 ----------
hydrateIcons()
document.title = `${state.name} · 创意空间`
fitGroups()
render()
initComposer()
initAgent()
if (!restoreView()) fitView()
