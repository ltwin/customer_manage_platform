// 入口：外框控件、资产抽屉、创作助手、全局快捷键。
import { state, ui, byId, undo, redo, canUndo, canRedo, reset, save, checkpoint, descendants, roots, fitGroups, isGroup } from './store.mjs'
import { on, emit, render, commit, select, applyView, restoreView, fitView, zoomBy, addMenu, pickFiles, duplicate, remove, toWorld, groupSelection, ungroupSelection } from './canvas.mjs'
import { initComposer } from './composer.mjs'
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

// ---------- 创作助手 ----------
const agent = $('agent')
const thread = []
let consented = false
function paintAgent() {
  const sel = [...ui.sel].map(byId).filter(Boolean)
  agent.innerHTML = `<div class="panel-head">
      <button class="thread-btn" data-g="threads">${thread.length ? '海边写真策划' : '新对话'}${ic('chevron-down')}</button>
      <span class="grow"></span>
      <button class="icon-btn" data-g="new" aria-label="新对话">${ic('plus')}</button>
      <button class="icon-btn" data-g="close" aria-label="收起助手">${ic('panel-right-close')}</button></div>
    <div class="messages">${thread.length ? thread.map((m) => m.role === 'user'
      ? `<div class="msg-user">${esc(m.text)}</div>`
      : m.thinking ? '<div class="thinking"><span></span><span></span><span></span></div>' : `<div class="msg-ai">${m.html}</div>`).join('')
      : `<div class="empty-agent"><h3>想拍点什么？</h3><p>我可以读画布上的节点，帮你拆分镜、写提示词、整理拍摄清单。</p>
        <div class="suggest">
          <button data-s="根据拍摄概念拆 4 个分镜">${ic('clapperboard')}根据拍摄概念拆 4 个分镜</button>
          <button data-s="给镜头 02 写一段提示词">${ic('pencil')}给镜头 02 写一段提示词</button>
          <button data-s="整理一份现场拍摄清单">${ic('file-check')}整理一份现场拍摄清单</button>
        </div></div>`}</div>
    <div class="agent-box">
      ${sel.length ? `<span class="ctx-chip">${ic('scan')}${sel.length === 1 ? esc(sel[0].title) : `${sel.length} 个节点`}</span>` : ''}
      <textarea rows="2" aria-label="向助手提问" placeholder="描述任务，@ 引用画布节点"></textarea>
      <div class="composer-foot">
        <button class="chip" data-g="model">${ic('sparkles')}DeepSeek V4</button>
        <button class="chip" data-g="skill">${ic('lightbulb')}拍摄策划</button>
        <span class="grow"></span>
        <button class="send" data-g="send" aria-label="发送" disabled>${ic('arrow-up')}</button>
      </div></div>`
  const m = agent.querySelector('.messages')
  m.scrollTop = m.scrollHeight
}
function toggleAgent(open = agent.hidden) {
  agent.hidden = !open
  $('agent-toggle').setAttribute('aria-pressed', String(open))
  if (open) { paintAgent(); agent.querySelector('textarea').focus() }
}
$('agent-toggle').querySelector('.i').innerHTML = ic('sparkles')
$('agent-toggle').addEventListener('click', () => toggleAgent())
on('select', () => { if (!agent.hidden && document.activeElement?.closest('#agent') == null) paintAgent() })

async function ask(text) {
  if (!consented) {
    consented = await confirmModal({ title: '发送给外部模型？', body: '所选节点的文字和图片会发送给 DeepSeek 处理。本项目只需确认一次，可在设置中撤回。', ok: '同意并发送' })
    if (!consented) return
  }
  thread.push({ role: 'user', text }, { role: 'ai', thinking: true })
  paintAgent()
  await new Promise((r) => setTimeout(r, 900))
  thread[thread.length - 1] = { role: 'ai', html: `<p>按「九月，向海而行」的情绪，建议 4 个镜头，从远到近：</p>
    <ol><li><b>远景 · 海平线</b>：人物占画面 1/6，大面积天空留白。</li>
    <li><b>中景 · 逆光长裙</b>：太阳压在肩线，裙摆被风带起。</li>
    <li><b>近景 · 回头</b>：发丝轮廓光，焦点在眼睛。</li>
    <li><b>特写 · 手与裙摆</b>：浅景深，暖调高光溢出。</li></ol>
    <div class="msg-actions"><button class="btn btn-primary" data-g="apply">${ic('plus')}放到画布</button><button class="btn" data-g="copy">复制</button></div>` }
  paintAgent()
}
agent.addEventListener('input', (e) => {
  if (e.target.matches('textarea')) agent.querySelector('[data-g="send"]').disabled = !e.target.value.trim()
})
agent.addEventListener('keydown', (e) => {
  e.stopPropagation()
  if (e.target.matches('textarea') && e.key === 'Enter' && !e.shiftKey && !e.isComposing && e.target.value.trim()) {
    e.preventDefault()
    ask(e.target.value.trim())
  }
})
agent.addEventListener('click', (e) => {
  const s = e.target.closest('[data-s]')?.dataset.s
  if (s) return ask(s)
  const g = e.target.closest('[data-g]')
  if (!g) return
  const act = g.dataset.g
  if (act === 'close') return toggleAgent(false)
  if (act === 'send') return ask(agent.querySelector('textarea').value.trim())
  if (act === 'new') { thread.length = 0; return paintAgent() }
  if (act === 'copy') return toast('已复制')
  if (act === 'threads') return openMenu([{ section: '最近对话' }, { label: '海边写真策划', icon: 'clock', checked: thread.length > 0 }, { label: '婚纱夜景提示词', icon: 'clock' }, { label: '产品静物布光', icon: 'clock' }], { rect: g.getBoundingClientRect() }, g)
  if (act === 'model') return openMenu([{ label: 'DeepSeek V4', checked: true }, { label: 'Claude Sonnet 5' }, { label: 'Qwen 3.5' }], { rect: g.getBoundingClientRect() }, g)
  if (act === 'skill') return openMenu([{ section: 'Skill' }, { label: '拍摄策划', sub: '分镜、清单、时间线', checked: true }, { label: '提示词润色', sub: '把描述改写为生成提示词' }, { label: '不使用 Skill' }], { rect: g.getBoundingClientRect() }, g)
  if (act === 'apply') {
    checkpoint()
    const base = Math.max(...state.nodes.map((n) => n.y + n.h)) + 140
    const titles = ['远景 · 海平线', '中景 · 逆光长裙', '近景 · 回头', '特写 · 手与裙摆']
    const ids = titles.map((t, i) => {
      const n = { id: `a-${Date.now()}-${i}`, type: 'text', x: i * 340, y: base, w: 300, h: 140, title: `镜头 ${String(i + 1).padStart(2, '0')}`, text: `<h4>${t}</h4>` }
      state.nodes.push(n)
      return n.id
    })
    commit()
    select(ids)
    fitView(new Set(ids))
    toast('已添加 4 个节点')
  }
})

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
if (!restoreView()) fitView()
