// 画布：视口、节点/连线渲染与指针交互。浮层（工具条、输入框）由 composer.mjs 订阅钩子后定位。
import { DEFAULT_RATIO, photos, state, ui, byId, cover, modelOf, checkpoint, save, saveView, loadView, connect, makeNode, removeNodes, undo, redo, uid, sizeFor, isGroup, descendants, roots, fitGroups, group, ungroup, mainIndex, isPinned, setMain, followLatest } from './store.mjs'
import { ic, esc, openMenu, closeFloating, onFloatingClose, toast } from './ui.mjs'
import { attachMagneticPorts } from './ports.mjs'

const stage = document.getElementById('stage')
const world = document.getElementById('world')
const layer = document.getElementById('nodes')
const svg = document.getElementById('edges')
const marquee = document.getElementById('marquee')
const els = new Map()
const hooks = {}
export const on = (k, f) => (hooks[k] ??= []).push(f)
export const emit = (k, arg) => hooks[k]?.forEach((f) => f(arg))
const clamp = (v, a, b) => Math.min(b, Math.max(a, v))
const TYPE_ICON = { text: 'file-text', image: 'image', video: 'video' }

export const toScreen = (x, y) => ({ x: x * ui.view.k + ui.view.x, y: y * ui.view.k + ui.view.y })
export const toWorld = (x, y) => ({ x: (x - ui.view.x) / ui.view.k, y: (y - ui.view.y) / ui.view.k })

export function applyView() {
  const { x, y, k } = ui.view
  world.style.transform = `translate(${x}px, ${y}px) scale(${k})`
  let g = 24 * k
  while (g < 14) g *= 2
  stage.style.setProperty('--grid', `${g}px`)
  stage.style.backgroundPosition = `${x}px ${y}px`
  saveView(ui.view)
  // 缩放按钮、⌘+/-、小地图、适应画布都会走这里：锚在旧位置的菜单一律收起
  closeFloating()
  emit('view')
}
// 视口按画布记住，重新打开回到离开时的位置；没有记录或记录损坏时由调用方适应全部
export function restoreView() {
  const v = loadView()
  if (!v) return false
  ui.view = v
  applyView()
  return true
}

export function zoomAt(cx, cy, f) {
  const k = clamp(ui.view.k * f, 0.1, 4)
  const p = toWorld(cx, cy)
  ui.view = { k, x: cx - p.x * k, y: cy - p.y * k }
  applyView()
}
export const zoomBy = (f) => zoomAt(innerWidth / 2, innerHeight / 2, f)

function insets() {
  const assets = !document.getElementById('assets').hidden
  const agent = !document.getElementById('agent').hidden
  if (innerWidth <= 760) return { l: 16, r: 16, t: 72, b: 80 }
  return { l: assets ? 432 : 80, r: agent ? 416 : 40, t: 90, b: 90 }
}
export function fitView(ids) {
  const list = ids ? state.nodes.filter((n) => ids.has(n.id)) : state.nodes
  if (!list.length) return
  const x0 = Math.min(...list.map((n) => n.x)), y0 = Math.min(...list.map((n) => n.y - 28))
  const x1 = Math.max(...list.map((n) => n.x + n.w)), y1 = Math.max(...list.map((n) => n.y + n.h))
  const { l, r, t, b } = insets()
  const W = innerWidth - l - r, H = innerHeight - t - b
  const k = clamp(Math.min(W / (x1 - x0), H / (y1 - y0)), 0.2, 1.1)
  ui.view = { k, x: l + (W - (x1 - x0) * k) / 2 - x0 * k, y: t + (H - (y1 - y0) * k) / 2 - y0 * k }
  applyView()
}
// ---------- 渲染 ----------
function body(n) {
  const run = ui.running.get(n.id)
  if (n.type === 'text') {
    if (!n.text && ui.editing !== n.id)
      return `<div class="card-empty"><span class="glyph">${ic('file-text')}</span>双击输入文字<br>或作为下游节点的提示词来源</div>`
    return `<div class="card-text" ${ui.editing === n.id ? 'contenteditable="true"' : ''}>${n.text}</div>`
  }
  if (run) {
    const src = cover(n)
    return `${src ? `<img src="${src}" alt="" style="opacity:.35;filter:blur(6px)">` : ''}
      <div class="run-state"><span class="run-dot"></span><span>${run.label}</span><span class="bar"><span style="width:${run.p}%"></span></span></div>`
  }
  const src = cover(n)
  if (!src)
    return `<div class="card-empty"><span class="glyph">${ic(TYPE_ICON[n.type])}</span>
      <span>选中后输入提示词生成<br>或拖入${n.type === 'video' ? '视频' : '图片'}</span>
      <span class="hint-actions"><button data-act="upload">上传</button><button data-act="assets">资产库</button></span></div>`
  const multi = ui.sel.size === 1 && ui.sel.has(n.id) && n.versions.length > 1
  const main = mainIndex(n)
  const viewing = n.vi ?? main
  const previewing = viewing !== main
  return `<img src="${src}" alt="${esc(n.title)}" draggable="false">
    ${n.type === 'video' ? `<span class="badge">${ic('play')}0:05</span><span class="play-line"></span>` : ''}
    ${previewing ? `<span class="preview-flag">预览版本 ${viewing + 1} · 下游仍用版本 ${main + 1}</span>` : ''}
    ${multi ? versionTray(n, viewing, main) : ''}`
}

// 版本条：缩略图点击只切换预览；主版本角标常驻；「设为主版本」写回 main
function versionTray(n, viewing, main) {
  const thumbs = n.versions.map((v, i) => `<button data-vi="${i}" class="${i === viewing ? 'on' : ''}${i === main ? ' main' : ''}"
      aria-label="版本 ${i + 1}${i === main ? '（主版本）' : ''}" aria-pressed="${i === viewing}"><img src="${v}" alt="">${i === main ? `<span class="main-dot">${ic('check')}</span>` : ''}</button>`).join('')
  let action
  if (viewing !== main) action = `<button class="set-main" data-main="${viewing}">设为主版本</button>`
  else if (isPinned(n) && main !== n.versions.length - 1) action = `<button class="set-main ghost" data-follow>改为跟随最新</button>`
  else action = `<span class="main-state">${isPinned(n) ? '主版本' : '主版本 · 自动'}</span>`
  return `<div class="versions">${thumbs}<span class="v-sep"></span>${action}</div>`
}

function meta(n) {
  if (ui.running.has(n.id)) return ''
  if (n.type === 'text') return ''
  if (n.versions.length > 1) return `主版本 ${mainIndex(n) + 1}/${n.versions.length}`
  return n.versions.length ? esc(modelOf(n)?.name ?? '') : ''
}

const groupDepth = (n) => { let d = 0; for (let p = n.parent; p; p = byId(p)?.parent) d++; return d }

function groupHTML(g) {
  const count = state.nodes.filter((n) => n.parent === g.id).length
  return `<div class="group-frame"></div>
    <div class="group-head">${ic('layers')}<span class="title">${esc(g.title)}</span><span class="meta">${count} 项</span></div>`
}

function renderNode(n) {
  let el = els.get(n.id)
  if (!el) {
    el = document.createElement('div')
    el.className = isGroup(n) ? 'node group' : `node ${n.type}`
    el.dataset.id = n.id
    layer.append(el)
    els.set(n.id, el)
  }
  el.style.transform = `translate(${n.x}px, ${n.y}px)`
  el.style.width = `${n.w}px`
  el.style.height = `${n.h}px`
  // 组永远在子节点下层；内层组压在外层组上面
  el.style.zIndex = isGroup(n) ? String(groupDepth(n)) : '50'
  el.classList.toggle('selected', ui.sel.has(n.id))
  if (ui.editing === n.id && el.querySelector('[contenteditable]')) return
  if (el.querySelector(':scope > .node-label input, :scope > .group-head input')) return
  const html = isGroup(n)
    ? groupHTML(n)
    : `<div class="node-label">${ic(TYPE_ICON[n.type])}<span class="title">${esc(n.title)}</span><span class="meta">${meta(n)}</span></div>
    <div class="card${ui.running.has(n.id) ? ' running' : ''}">${body(n)}</div>
    ${n.type !== 'text' ? `<button class="port in" aria-label="添加上游参考">${ic('plus')}</button>` : ''}
    <button class="port out" aria-label="添加下游节点">${ic('plus')}</button>`
  // 内容没变就不重建 DOM，拖动时图片不闪
  if (el._html !== html) { el.innerHTML = html; el._html = html }
  // 只有选中节点的端口进入 Tab 顺序，避免满画布的隐形按钮
  for (const port of el.querySelectorAll(':scope > .port')) port.tabIndex = ui.sel.has(n.id) ? 0 : -1
}

const anchor = (n, side) => ({ x: side === 'out' ? n.x + n.w : n.x, y: n.y + n.h / 2 })
const curve = (a, b) => {
  const d = Math.max(60, Math.abs(b.x - a.x) * 0.5)
  return `M${a.x},${a.y} C${a.x + d},${a.y} ${b.x - d},${b.y} ${b.x},${b.y}`
}
// 流光每次重绘都会重建，按当前时间给负延迟，拖动中也连续流动不跳回起点
const PULSE_MS = 2800
// 拖线松手弹出添加菜单时，虚线留在原处，直到菜单关闭：选了节点就换成实线，取消就消失
let pendingLink = null
function renderEdges(draft = pendingLink) {
  let html = ''
  const phase = `animation-delay:-${(performance.now() % PULSE_MS) / 1000}s`
  // 选中组时，组内节点关联的连线一起高亮
  const lit = descendants([...ui.sel])
  for (const e of state.edges) {
    const a = byId(e.from), b = byId(e.to)
    if (!a || !b) continue
    const hot = lit.has(a.id) || lit.has(b.id)
    const cls = ui.running.has(b.id) ? 'flow' : ui.edgeSel === e.id ? 'sel' : hot ? 'hot' : ''
    const d = curve(anchor(a, 'out'), anchor(b, 'in'))
    html += `<path class="edge-hit" data-edge="${e.id}" d="${d}"/><path class="edge ${cls}" d="${d}"/>`
    // 选中节点、组或连线时，相关连线沿数据流向走一道白光；运行中的琥珀虚线优先
    if (cls === 'sel' || cls === 'hot') html += `<path class="edge-pulse" style="${phase}" d="${d}"/>`
  }
  // 待定连线只记节点 id 和松手点，端口位置每次重算
  if (draft?.from || draft?.to) {
    const n = byId(draft.from ?? draft.to)
    draft = n ? (draft.from ? { a: anchor(n, 'out'), b: draft.b } : { a: draft.a, b: anchor(n, 'in') }) : null
  }
  if (draft) {
    html += `<path class="edge draft${draft.snap ? ' snapped' : ''}" d="${curve(draft.a, draft.b)}"/>`
    if (draft.snap) html += `<circle class="edge-snap" cx="${draft.snap.x}" cy="${draft.snap.y}" r="${4 / ui.view.k}"/>`
  }
  svg.innerHTML = html
}

// 节点尺寸：空节点按比例 chip（默认 1:1）；有内容后宽度不变，高度跟随当前显示内容的真实长宽比。
// 生成结果、切换预览版本、上传都会触发；尺寸读不到时先保持原样，读到后再重绘。
const natural = new Map(photos.map((p) => [p.src, { w: p.w, h: p.h }]))
function syncMediaSize() {
  let changed = false
  for (const n of state.nodes) {
    if (n.type !== 'image' && n.type !== 'video') continue
    const src = cover(n)
    if (!src) continue
    const size = natural.get(src)
    if (size === null) continue // 正在读取
    if (!size) {
      natural.set(src, null)
      const probe = new Image()
      probe.onload = () => { natural.set(src, { w: probe.naturalWidth, h: probe.naturalHeight }); render(); save() }
      probe.src = src
      continue
    }
    const h = Math.round((n.w * size.h) / size.w)
    if (Math.abs(h - n.h) > 1) { n.h = h; changed = true }
  }
  if (changed) fitGroups()
}

export function render() {
  syncMediaSize()
  const live = new Set(state.nodes.map((n) => n.id))
  for (const [id, el] of els) if (!live.has(id)) { el.remove(); els.delete(id) }
  state.nodes.forEach(renderNode)
  renderEdges()
  emit('change')
}
export function commit() { render(); save() }
// 离开选中的节点结束预览，卡片回到主版本
function endPreviews(keep = new Set()) {
  for (const id of ui.sel) if (!keep.has(id)) { const n = byId(id); if (n) delete n.vi }
}
export function select(ids, additive = false) {
  endPreviews(new Set(additive ? [...ui.sel, ...ids] : ids))
  if (!additive) ui.sel.clear()
  for (const id of ids) ui.sel.add(id)
  ui.edgeSel = null
  render()
  emit('select')
}
export const nodeEl = (id) => els.get(id)

// ---------- 新建节点菜单（工具轨 / 双击 / 端口 / 右键 共用） ----------
export function addMenu(screen, opts = {}) {
  const w = toWorld(screen.x, screen.y)
  const make = (type, extra) => () => {
    checkpoint()
    const src = opts.from ? byId(opts.from) : opts.to ? byId(opts.to) : null
    const size = sizeFor(type, DEFAULT_RATIO)
    let x = w.x - size.w / 2, y = w.y - size.h / 2
    // 拖线松手创建：新节点的端口正好落在虚线终点，虚线原地变成实线
    if (opts.dropped && opts.from) x = w.x
    if (opts.dropped && opts.to) x = w.x - size.w
    if (opts.from && src && !opts.dropped) { x = src.x + src.w + 120; y = src.y + src.h / 2 - size.h / 2 }
    if (opts.to && src && !opts.dropped) { x = src.x - size.w - 120; y = src.y + src.h / 2 - size.h / 2 }
    const n = makeNode(type, x, y, extra)
    if (opts.from) connect(opts.from, n.id)
    if (opts.to) connect(n.id, opts.to)
    select([n.id])
    save()
    if (type === 'text') startEdit(n.id)
    else emit('focus', n.id)
  }
  const fromText = opts.from && byId(opts.from)?.type === 'text'
  openMenu([
    { section: opts.from ? '生成下游节点' : opts.to ? '添加上游参考' : '添加节点' },
    { label: '文本', icon: 'file-text', hint: 'T', run: make('text') },
    { label: '图片', icon: 'image', hint: 'I', sub: fromText ? '以上游文本为提示词' : '', run: make('image') },
    { label: '视频', icon: 'video', hint: 'O', run: make('video') },
    'sep',
    { label: '上传文件', icon: 'upload', run: () => pickFiles(w) },
    { label: '从资产库添加', icon: 'library', run: () => emit('assets') },
  ], screen)
}

// into：填进这个空节点；否则在 at 处新建节点
export function pickFiles(at, into) {
  const input = Object.assign(document.createElement('input'), { type: 'file', accept: 'image/*', multiple: !into })
  input.onchange = () => (into ? fillNode(into, input.files[0]) : addFiles([...input.files], at ?? toWorld(innerWidth / 2, innerHeight / 2)))
  input.click()
}
const DEFAULT_TITLES = new Set(['图片', '视频'])
// 空媒体节点才能被填充；已有内容的节点走生成与版本
export const fillable = (n) => Boolean(n) && (n.type === 'image' || n.type === 'video') && !cover(n)
export function fillNode(id, file) {
  const n = byId(id)
  if (!fillable(n) || !file) return
  if (!file.type.startsWith('image/')) return toast('原型只接收图片文件')
  fillWith(n, { src: URL.createObjectURL(file), title: file.name.replace(/\.[^.]+$/, '') })
}
// 高度随后由 syncMediaSize 按图片真实比例更新
export function fillWith(n, { src, title, assetId }) {
  checkpoint()
  n.versions = [src]
  n.main = null
  if (title && DEFAULT_TITLES.has(n.title)) n.title = title
  if (assetId) n.assetId = assetId
  select([n.id])
  commit()
}
export function addFiles(files, at) {
  const imgs = files.filter((f) => f.type.startsWith('image/'))
  if (!imgs.length) return toast('原型只接收图片文件')
  checkpoint()
  const ids = []
  imgs.forEach((f, i) => {
    const url = URL.createObjectURL(f)
    const n = makeNode('image', at.x + i * 330 - 150, at.y - 200, { title: f.name.replace(/\.[^.]+$/, ''), versions: [url] })
    ids.push(n.id)
  })
  select(ids)
  save()
}
// at 是节点中心；批量放入时由调用方统一 checkpoint，一次撤销整批
export function addAsset(asset, at, { batch = false } = {}) {
  if (!batch) checkpoint()
  const from = asset.id ? { assetId: asset.id } : {}
  const n = asset.text
    ? makeNode('text', at.x - 150, at.y - 110, { title: asset.title, text: asset.text, ...from })
    : makeNode('image', at.x - 150, at.y - 150, { title: asset.title, versions: [asset.src], h: Math.round((300 * asset.h) / asset.w), ...from })
  n.y = at.y - n.h / 2
  if (!batch) { select([n.id]); save(); emit('asset-used', asset.id) }
  return n
}

// ---------- 文本编辑 / 重命名 ----------
export function startEdit(id) {
  ui.editing = id
  render()
  const el = els.get(id)?.querySelector('.card-text')
  if (!el) return
  el.focus()
  const r = document.createRange()
  r.selectNodeContents(el)
  r.collapse(false)
  getSelection().removeAllRanges()
  getSelection().addRange(r)
  el.addEventListener('blur', () => {
    const n = byId(id)
    ui.editing = null
    if (n && n.text !== el.innerHTML) { checkpoint(); n.text = el.innerText.trim() ? el.innerHTML : '' }
    commit()
  }, { once: true })
}
on('rename', (id) => rename(id))
function rename(id) {
  const n = byId(id)
  const label = els.get(id)?.querySelector(':scope > .node-label .title, :scope > .group-head .title')
  if (!n || !label) return
  const input = Object.assign(document.createElement('input'), { value: n.title })
  label.replaceWith(input)
  input.focus()
  input.select()
  const done = (ok) => {
    if (ok && input.value.trim() && input.value !== n.title) { checkpoint(); n.title = input.value.trim() }
    // 输入框不在渲染缓存里，强制重建标签
    const el = els.get(id)
    if (el) { input.remove(); el._html = null }
    commit()
  }
  input.addEventListener('keydown', (e) => {
    e.stopPropagation()
    if (e.key === 'Enter') input.blur()
    if (e.key === 'Escape') { input.onblur = null; done(false) }
  })
  input.onblur = () => done(true)
}
// ---------- 节点动作（工具条、右键、快捷键共用） ----------
// 复制选中的根对象及其整棵子树，内部连线一起复制
export function duplicate(ids) {
  checkpoint()
  const top = roots(ids)
  const all = descendants(top)
  const map = new Map([...all].map((id) => [id, uid(isGroup(byId(id)) ? 'g' : 'n')]))
  const copies = state.nodes.filter((n) => all.has(n.id)).map((n) => ({
    ...structuredClone(n),
    id: map.get(n.id),
    parent: map.get(n.parent) ?? n.parent ?? null,
    x: n.x + 40, y: n.y + 40,
    title: top.includes(n.id) ? `${n.title} 副本` : n.title,
  }))
  // 组插到最前，保持在节点下层
  state.nodes.unshift(...copies.filter(isGroup))
  state.nodes.push(...copies.filter((n) => !isGroup(n)))
  for (const e of [...state.edges]) if (map.has(e.from) && map.has(e.to)) state.edges.push({ id: uid('e'), from: map.get(e.from), to: map.get(e.to) })
  fitGroups()
  select(top.map((id) => map.get(id)))
  save()
}
export function groupSelection() {
  checkpoint()
  const g = group([...ui.sel])
  if (!g) return toast('至少选中两个节点才能打组')
  select([g.id])
  save()
}
export function ungroupSelection() {
  const ids = new Set([...ui.sel].filter((id) => isGroup(byId(id))))
  if (!ids.size) return
  checkpoint()
  select(ungroup(ids))
  save()
}
export function remove(ids) {
  if (!ids.size) return
  checkpoint()
  removeNodes(ids)
  ui.sel.clear()
  commit()
  emit('select')
}
export function nodeMenu(id, at) {
  const n = byId(id)
  const multi = ui.sel.size > 1
  const hasGroup = [...ui.sel].some((i) => isGroup(byId(i)))
  if (!multi && isGroup(n))
    return openMenu([
      { label: '重命名分组', icon: 'pencil', run: () => rename(id) },
      { label: '解组', icon: 'layers', hint: '⇧⌘G', run: ungroupSelection },
      { label: '复制整组', icon: 'copy', hint: '⌘D', run: () => duplicate([id]) },
      { label: '适应到分组', icon: 'scan', run: () => fitView(descendants([id])) },
      'sep',
      { label: '删除分组及内容', icon: 'trash-2', hint: '⌫', danger: true, run: () => remove(new Set([id])) },
    ], at)
  openMenu([
    ...(multi ? [
      { label: '打组', icon: 'layers', hint: '⌘G', run: groupSelection },
      ...(hasGroup ? [{ label: '解组', icon: 'layers', hint: '⇧⌘G', run: ungroupSelection }] : []),
      'sep',
    ] : [
      { label: '重命名', icon: 'pencil', run: () => rename(id) },
      ...(n.type === 'text' ? [{ label: '编辑文字', icon: 'file-text', hint: 'Enter', run: () => startEdit(id) }] : []),
      ...(cover(n) ? [{ label: '全屏查看', icon: 'maximize', hint: 'Space', run: () => emit('lightbox', id) }, { label: '下载', icon: 'download', run: () => toast('原型不下载文件') }] : []),
    ]),
    { label: '复制', icon: 'copy', hint: '⌘D', run: () => duplicate([...ui.sel]) },
    { label: '存入资产库…', icon: 'bookmark', run: () => emit('save-asset', { ids: [...ui.sel], at }) },
    'sep',
    { label: '删除', icon: 'trash-2', hint: '⌫', danger: true, run: () => remove(new Set(ui.sel)) },
  ], at)
}

// ---------- 指针 ----------
let drag = null
let lastEdgeDown = {}
// 连线落点：直接压在节点上以该节点为准；落在空白处时，18 屏幕像素内最近的可连节点也算。
// 压在浮层（工具条、输入框）上时不透过去连下面的节点。
function linkTarget(d, x, y) {
  const hit = document.elementFromPoint(x, y)
  if (!hit || !stage.contains(hit)) return { node: null }
  const ok = (n) => n && !isGroup(n) && n.id !== d.id && (d.side === 'in' || n.type !== 'text')
  const el = hit.closest('.node:not(.group)')
  if (el) {
    const n = byId(el.dataset.id)
    return ok(n) ? { node: n } : { node: null, invalid: n && n.id !== d.id }
  }
  const p = toWorld(x, y)
  let best = null, dist = 18 / ui.view.k
  for (const n of state.nodes) {
    if (!ok(n)) continue
    const gap = Math.hypot(Math.max(n.x - p.x, 0, p.x - n.x - n.w), Math.max(n.y - p.y, 0, p.y - n.y - n.h))
    if (gap < dist) { best = n; dist = gap }
  }
  return { node: best }
}

stage.addEventListener('pointerdown', (e) => {
  if (e.button === 2) return
  closeFloating()
  const t = e.target
  const nodeElHit = t.closest('.node')
  const id = nodeElHit?.dataset.id
  const panLike = e.button === 1 || ui.tool === 'hand' || ui.space
  if (panLike) {
    drag = { kind: 'pan', sx: e.clientX, sy: e.clientY, vx: ui.view.x, vy: ui.view.y }
    stage.classList.add('panning')
  } else if (t.closest('.port')) {
    const n = byId(id)
    const side = t.closest('.port').classList.contains('out') ? 'out' : 'in'
    drag = { kind: 'link', id, side, sx: e.clientX, sy: e.clientY, a: anchor(n, side), moved: false }
    stage.classList.add('linking')
  } else if (t.closest('[data-vi]')) {
    // 只切换预览，不进撤销栈、不影响下游
    byId(id).vi = Number(t.closest('[data-vi]').dataset.vi)
    render()
    emit('select')
    return
  } else if (t.closest('[data-main]')) {
    checkpoint()
    setMain(byId(id), Number(t.closest('[data-main]').dataset.main))
    commit()
    emit('select')
    toast('已设为主版本，下游节点将使用这一版')
    return
  } else if (t.closest('[data-follow]')) {
    checkpoint()
    followLatest(byId(id))
    commit()
    emit('select')
    toast('主版本改为跟随最新生成')
    return
  } else if (t.closest('[data-act]')) {
    const act = t.closest('[data-act]').dataset.act
    select([id])
    // 空节点里的两个入口都填充这个节点本身
    if (act === 'upload') pickFiles(undefined, id)
    else emit('assets-fill', id)
    return
  } else if (t.closest('[contenteditable="true"]') || t.closest('.node-label input')) {
    return
  } else if (id) {
    if (e.shiftKey || e.metaKey || e.ctrlKey) {
      ui.sel.has(id) ? ui.sel.delete(id) : ui.sel.add(id)
      select([], true)
    } else if (!ui.sel.has(id)) select([id])
    // 拖动选中的根对象，组带上整棵子树
    const moving = [...descendants(roots([...ui.sel]))].map(byId).filter(Boolean)
    drag = { kind: 'move', sx: e.clientX, sy: e.clientY, starts: moving.map((n) => ({ n, x: n.x, y: n.y })), moved: false }
  } else if (t.closest('.edge-hit')) {
    const edge = t.closest('.edge-hit').dataset.edge
    // 选中连线会重建 SVG，浏览器的 dblclick 不会触发，这里自己判定第二次按下
    if (lastEdgeDown.id === edge && e.timeStamp - lastEdgeDown.t < 400) {
      lastEdgeDown = {}
      checkpoint()
      state.edges = state.edges.filter((x) => x.id !== edge)
      ui.edgeSel = null
      commit()
      emit('select')
      return toast('已断开连线，⌘Z 可撤销')
    }
    lastEdgeDown = { id: edge, t: e.timeStamp }
    endPreviews()
    ui.sel.clear()
    ui.edgeSel = edge
    render()
    emit('select')
    return
  } else {
    drag = { kind: 'box', sx: e.clientX, sy: e.clientY, additive: e.shiftKey, base: new Set(e.shiftKey ? ui.sel : []) }
    if (!e.shiftKey && (ui.sel.size || ui.edgeSel)) { endPreviews(); ui.sel.clear(); ui.edgeSel = null; render(); emit('select') }
  }
  stage.setPointerCapture(e.pointerId)
})

stage.addEventListener('pointermove', (e) => {
  if (!drag) return
  const dx = e.clientX - drag.sx, dy = e.clientY - drag.sy
  if (drag.kind === 'pan') {
    ui.view.x = drag.vx + dx
    ui.view.y = drag.vy + dy
    applyView()
  } else if (drag.kind === 'move') {
    if (!drag.moved && Math.hypot(dx, dy) < 3) return
    if (!drag.moved) { checkpoint(); drag.moved = true; emit('dragstart') }
    for (const s of drag.starts) { s.n.x = s.x + dx / ui.view.k; s.n.y = s.y + dy / ui.view.k }
    // 组内单个节点移动时，所在组的边框跟着伸缩
    fitGroups()
    state.nodes.forEach(renderNode)
    renderEdges()
    emit('view')
    emit('node-drag', { x: e.clientX, y: e.clientY })
  } else if (drag.kind === 'link') {
    drag.moved ||= Math.hypot(dx, dy) > 4
    const p = toWorld(e.clientX, e.clientY)
    document.querySelectorAll('.node.drop-target').forEach((el) => el.classList.remove('drop-target'))
    const tn = linkTarget(drag, e.clientX, e.clientY).node
    if (tn) nodeEl(tn.id)?.classList.add('drop-target')
    const end = tn ? anchor(tn, drag.side === 'out' ? 'in' : 'out') : p
    const snap = tn ? end : null
    renderEdges(drag.side === 'out' ? { a: drag.a, b: end, snap } : { a: end, b: drag.a, snap })
  } else if (drag.kind === 'box') {
    const x = Math.min(e.clientX, drag.sx), y = Math.min(e.clientY, drag.sy)
    const w = Math.abs(dx), h = Math.abs(dy)
    if (w + h < 4) return
    Object.assign(marquee.style, { left: `${x}px`, top: `${y}px`, width: `${w}px`, height: `${h}px` })
    marquee.hidden = false
    const a = toWorld(x, y), b = toWorld(x + w, y + h)
    ui.sel = new Set(drag.base)
    // 组要完整框住才选中；普通节点碰到即选中
    for (const n of state.nodes) {
      const hit = isGroup(n)
        ? n.x >= a.x && n.x + n.w <= b.x && n.y >= a.y && n.y + n.h <= b.y
        : n.x < b.x && n.x + n.w > a.x && n.y < b.y && n.y + n.h > a.y
      if (hit) ui.sel.add(n.id)
    }
    // 组已选中时，组内节点不再重复选中
    for (const id of [...ui.sel]) if (!roots([...ui.sel]).includes(id)) ui.sel.delete(id)
    for (const n of state.nodes) nodeEl(n.id)?.classList.toggle('selected', ui.sel.has(n.id))
  }
})

stage.addEventListener('pointerup', (e) => {
  if (!drag) return
  const d = drag
  drag = null
  stage.classList.remove('panning', 'linking')
  marquee.hidden = true
  if (d.kind === 'move' && d.moved) {
    // 落在资产库上：收进资产库，节点回到原位
    const out = { x: e.clientX, y: e.clientY, ids: d.starts.map((s) => s.n.id), taken: false }
    emit('node-drop', out)
    if (out.taken) for (const s of d.starts) { s.n.x = s.x; s.n.y = s.y }
    if (out.taken) fitGroups()
    commit(); emit('dragend')
  }
  if (d.kind === 'box') { render(); emit('select') }
  if (d.kind === 'link') {
    document.querySelectorAll('.node.drop-target').forEach((el) => el.classList.remove('drop-target'))
    const { node: target, invalid } = linkTarget(d, e.clientX, e.clientY)
    if (d.moved && target) {
      checkpoint()
      const ok = d.side === 'out' ? connect(d.id, target.id) : connect(target.id, d.id)
      ok ? commit() : (renderEdges(), toast('这两个节点已经连接'))
    } else if (d.moved && invalid) {
      renderEdges()
      toast('文本节点不能接收输入')
    } else if (d.moved && !stage.contains(document.elementFromPoint(e.clientX, e.clientY))) {
      // 松手在工具条、输入框等浮层上：视为取消
      renderEdges()
    } else if (d.moved && !document.elementFromPoint(e.clientX, e.clientY)?.closest('.node:not(.group)')) {
      const end = toWorld(e.clientX, e.clientY)
      addMenu({ x: e.clientX, y: e.clientY }, d.side === 'out' ? { from: d.id, dropped: true } : { to: d.id, dropped: true })
      pendingLink = d.side === 'out' ? { from: d.id, b: end } : { to: d.id, a: end }
      onFloatingClose(() => { pendingLink = null; renderEdges() })
      renderEdges()
    } else {
      renderEdges()
      addMenu({ x: e.clientX + 8, y: e.clientY - 20 }, d.side === 'out' ? { from: d.id } : { to: d.id })
    }
  }
})

stage.addEventListener('dblclick', (e) => {
  // pointerdown 做了 setPointerCapture，dblclick 的 target 会被改成画布本身；按坐标重新命中真实元素
  const hit = document.elementFromPoint(e.clientX, e.clientY)
  if (!hit || !stage.contains(hit)) return
  if (hit.closest('[contenteditable="true"], input')) return
  const id = hit.closest('.node')?.dataset.id
  if (hit.closest('.node-label, .group-head')) return rename(id)
  if (id && isGroup(byId(id))) return addMenu({ x: e.clientX, y: e.clientY })
  if (id) {
    const n = byId(id)
    if (n.type === 'text') startEdit(id)
    else if (cover(n)) emit('lightbox', id)
    else emit('focus', id)
    return
  }
  addMenu({ x: e.clientX, y: e.clientY })
})

stage.addEventListener('contextmenu', (e) => {
  e.preventDefault()
  const id = e.target.closest('.node')?.dataset.id
  if (id) {
    if (!ui.sel.has(id)) select([id])
    return nodeMenu(id, { x: e.clientX, y: e.clientY })
  }
  const w = toWorld(e.clientX, e.clientY)
  openMenu([
    { label: '添加节点', icon: 'plus', hint: '双击', run: () => addMenu({ x: e.clientX, y: e.clientY }) },
    { label: '上传文件', icon: 'upload', run: () => pickFiles(w) },
    'sep',
    { label: '撤销', icon: 'undo-2', hint: '⌘Z', run: () => undo() && commit() },
    { label: '重做', icon: 'redo-2', hint: '⇧⌘Z', run: () => redo() && commit() },
    'sep',
    { label: '全选', icon: 'scan', hint: '⌘A', run: () => select(state.nodes.map((n) => n.id)) },
    { label: '适应画布', icon: 'maximize', hint: '⇧1', run: () => fitView() },
  ], { x: e.clientX, y: e.clientY })
})

stage.addEventListener('wheel', (e) => {
  e.preventDefault()
  // 视口一动，锚在旧位置的菜单就失去意义
  closeFloating()
  if (e.ctrlKey || e.metaKey) zoomAt(e.clientX, e.clientY, Math.exp(-e.deltaY * 0.01))
  else { ui.view.x -= e.deltaX; ui.view.y -= e.deltaY; applyView() }
}, { passive: false })

stage.addEventListener('dragover', (e) => e.preventDefault())
stage.addEventListener('drop', (e) => {
  e.preventDefault()
  const at = toWorld(e.clientX, e.clientY)
  // 落在空的图片/视频节点上就填进去，不另建节点
  const hit = byId(e.target.closest?.('.node')?.dataset.id)
  const into = fillable(hit) ? hit.id : null
  const ids = e.dataTransfer.getData('application/x-assets')
  if (ids) return emit('asset-drop', { ids: JSON.parse(ids), at, into })
  const files = [...e.dataTransfer.files]
  if (into && files.length) return fillNode(into, files[0])
  addFiles(files, at)
})

// 键盘可达：Tab 聚焦端口后按 Enter / Space 打开同一个添加菜单，菜单锚在端口中心
stage.addEventListener('click', (e) => {
  const port = e.detail === 0 && e.target.closest?.('.port')
  if (!port) return
  const id = port.closest('.node').dataset.id
  const r = port.getBoundingClientRect()
  addMenu({ x: r.right + 8, y: r.top - 20 }, port.classList.contains('out') ? { from: id } : { to: id })
})
attachMagneticPorts(stage, () => Boolean(drag) || ui.space || ui.tool === 'hand')
