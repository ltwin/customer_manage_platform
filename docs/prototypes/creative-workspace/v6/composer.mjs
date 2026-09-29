// 选中态浮层：节点上方工具条 + 媒体节点下方生成输入框 + 连线断开条 + 全屏预览。
import { DEFAULT_RATIO, state, ui, byId, upstream, cover, models, ratios, modelOf, sizeFor, checkpoint, save, connect, makeNode, photos, isGroup, mainSrc, mainIndex, isPinned, setMain } from './store.mjs'
import { on, emit, render, commit, select, toScreen, duplicate, remove, nodeMenu, startEdit, nodeEl, groupSelection, ungroupSelection } from './canvas.mjs'
import { ic, esc, openMenu, openPopover, closeFloating, toast } from './ui.mjs'
import { refLabels, compilePrompt, MENTION } from './store.mjs'
import { toHTML, fromDOM, mentionQuery, insertChip, caretToEnd } from './prompt-editor.mjs'

const overlay = document.getElementById('overlay')
const lightbox = document.getElementById('lightbox')
let bar = null, box = null, edgeBar = null, boxFor = null, dragging = false

const single = () => (ui.sel.size === 1 ? byId([...ui.sel][0]) : null)
const isMedia = (n) => n?.type === 'image' || n?.type === 'video'

// ---------- 定位 ----------
function position() {
  const n = single()
  if (bar) {
    const list = [...ui.sel].map(byId).filter(Boolean)
    const x0 = Math.min(...list.map((m) => m.x)), x1 = Math.max(...list.map((m) => m.x + m.w))
    // 普通节点的标签在卡片外 26px，组的标题栏在框内；工具条贴着可见内容上沿，少压住邻居
    const y0 = Math.min(...list.map((m) => (isGroup(m) ? m.y : m.y - 26)))
    const a = toScreen((x0 + x1) / 2, y0)
    const w = bar.offsetWidth, h = bar.offsetHeight
    bar.style.left = `${Math.max(8, Math.min(a.x - w / 2, innerWidth - w - 8))}px`
    bar.style.top = `${Math.max(60, a.y - h - 8)}px`
  }
  if (box && n) {
    const a = toScreen(n.x + n.w / 2, n.y + n.h)
    const w = box.offsetWidth, h = box.offsetHeight
    let y = a.y + 14
    if (y + h > innerHeight - 8) y = innerHeight - h - 8
    box.style.left = `${Math.max(8, Math.min(a.x - w / 2, innerWidth - w - 8))}px`
    box.style.top = `${y}px`
  }
  if (edgeBar) {
    const e = state.edges.find((x) => x.id === ui.edgeSel)
    const a = e && byId(e.from), b = e && byId(e.to)
    if (!a || !b) return edgeBar.remove(), (edgeBar = null)
    const m = toScreen((a.x + a.w + b.x) / 2, (a.y + a.h / 2 + b.y + b.h / 2) / 2)
    edgeBar.style.left = `${m.x - edgeBar.offsetWidth / 2}px`
    edgeBar.style.top = `${m.y - 44}px`
  }
}

// ---------- 工具条 ----------
function barHTML() {
  const n = single()
  if (!n) {
    const hasGroup = [...ui.sel].some((id) => isGroup(byId(id)))
    const refs = [...ui.sel].map(byId).filter((m) => m && !isGroup(m))
    return `<span class="tool" style="pointer-events:none">已选 ${ui.sel.size} 项</span><span class="sep"></span>
      <button class="tool" data-b="group" data-kbd="⌘G" aria-label="打组">${ic('layers')}打组</button>
      ${hasGroup ? `<button class="tool" data-b="ungroup">解组</button>` : ''}
      ${refs.length > 1 ? `<button class="tool" data-b="combine">${ic('sparkles')}合并为参考生成</button>` : ''}
      <span class="sep"></span>
      <button class="icon-btn" data-b="dup" aria-label="复制" data-kbd="⌘D">${ic('copy')}</button>
      <button class="icon-btn" data-b="del" aria-label="删除" data-kbd="⌫">${ic('trash-2')}</button>`
  }
  if (isGroup(n))
    return `<button class="tool" data-b="rename-group">${ic('pencil')}重命名</button>
      <button class="tool" data-b="ungroup" data-kbd="⇧⌘G" aria-label="解组">${ic('layers')}解组</button>
      <span class="sep"></span>
      <button class="icon-btn" data-b="dup" aria-label="复制整组" data-kbd="⌘D">${ic('copy')}</button>
      <button class="icon-btn" data-b="more" aria-label="更多">${ic('ellipsis')}</button>`
  if (n.type === 'text')
    return `<button class="tool" data-b="edit">${ic('pencil')}编辑</button>
      <button class="tool" data-b="to-image">${ic('image')}生成图片</button>
      <span class="sep"></span>
      <button class="icon-btn" data-b="more" aria-label="更多">${ic('ellipsis')}</button>`
  if (!cover(n))
    return `<button class="tool" data-b="upload">${ic('upload')}上传</button>
      <span class="sep"></span>
      <button class="icon-btn" data-b="more" aria-label="更多">${ic('ellipsis')}</button>`
  return `${n.type === 'image' ? `
      <button class="tool" data-b="tool" data-t="重绘">${ic('pencil')}重绘</button>
      <button class="tool" data-b="tool" data-t="扩图">${ic('scan')}扩图</button>
      <button class="tool" data-b="tool" data-t="高清">${ic('sparkles')}高清</button>
      <button class="tool" data-b="to-video">${ic('video')}生成视频</button>
      <span class="sep"></span>` : ''}
    <button class="icon-btn" data-b="full" aria-label="全屏查看" data-kbd="Space">${ic('maximize')}</button>
    <button class="icon-btn" data-b="save" aria-label="存入资产库">${ic('bookmark')}</button>
    <button class="icon-btn" data-b="dl" aria-label="下载">${ic('download')}</button>
    <button class="icon-btn" data-b="more" aria-label="更多">${ic('ellipsis')}</button>`
}

// 从选中节点派生一个下游节点，并直接进入它的输入框
function derive(sources, type, prompt) {
  checkpoint()
  const last = sources[sources.length - 1]
  const ratio = DEFAULT_RATIO
  const { w, h } = sizeFor(type, ratio)
  const cy = sources.reduce((s, m) => s + m.y + m.h / 2, 0) / sources.length
  const x = Math.max(...sources.map((m) => m.x + m.w)) + 120
  const n = makeNode(type, x, cy - h / 2, { ratio, w, h, prompt })
  sources.forEach((s) => connect(s.id, n.id))
  select([n.id])
  save()
  emit('focus', n.id)
}

function onBar(e) {
  const b = e.target.closest('[data-b]')
  if (!b) return
  const n = single()
  const act = b.dataset.b
  if (act === 'more') return nodeMenu(n?.id ?? [...ui.sel][0], { rect: b.getBoundingClientRect() })
  if (act === 'dup') return duplicate([...ui.sel])
  if (act === 'group') return groupSelection()
  if (act === 'ungroup') return ungroupSelection()
  if (act === 'rename-group') return emit('rename', n.id)
  if (act === 'del') return remove(new Set(ui.sel))
  if (act === 'edit') return startEdit(n.id)
  if (act === 'full') return emit('lightbox', n.id)
  if (act === 'save') return emit('save-asset', { ids: [...ui.sel], at: { rect: b.getBoundingClientRect() } })
  if (act === 'dl') return toast('原型不下载文件')
  if (act === 'upload') return emit('upload', n.id)
  if (act === 'to-image') return derive([n], 'image', `@[${n.id}] `)
  if (act === 'to-video') return derive([n], 'video', `@[${n.id}] 镜头缓慢推进，`)
  if (act === 'tool') return derive([n], 'image', `/${b.dataset.t} @[${n.id}] `)
  if (act === 'combine') {
    const list = [...ui.sel].map(byId).filter((m) => m && !isGroup(m))
    return derive(list, 'image', list.map((m) => `@[${m.id}]`).join(' ') + ' ')
  }
}
// ---------- 生成输入框 ----------
const PRESETS = [
  ['重绘', 'pencil', '保留构图，按描述修改局部'],
  ['扩图', 'scan', '向四周延展画面'],
  ['换背景', 'image', '保留主体，替换环境'],
  ['多机位', 'camera', '同一场景的 4 个角度'],
  ['调光', 'sun', '改变光线方向与色温'],
  ['高清', 'sparkles', '放大到 4K 并补细节'],
]
const ratioIcon = (r) => {
  const [a, b] = r.split(':').map(Number)
  const s = 12 / Math.max(a, b)
  return `<span class="ratio-ico" style="width:${Math.round(a * s)}px;height:${Math.round(b * s)}px"></span>`
}
const costOf = (n) => (modelOf(n)?.price ?? 0) * (n.count ?? 1)

// 参考一律取上游的主版本；上游在本节点生成后换过主版本时标出来
// 引用行列出全部已连接的参考（= 会发给模型的集合）；点一下把它 @ 进句子里
function refsHTML(n) {
  const labels = refLabels(n.id)
  return upstream(n.id).map((u) => {
    const src = mainSrc(u)
    const used = n.refsUsed?.[u.id]
    const changed = used && src && used !== src
    const ver = u.versions?.length > 1 ? `<span class="ref-ver">v${mainIndex(u) + 1}</span>` : ''
    const no = u.type === 'text' ? '文本' : labels.get(u.id)
    return `<span class="ref${changed ? ' changed' : ''}" data-peek="${u.id}"><button class="ref-body" data-mention="${u.id}" aria-label="在提示词中引用 ${esc(u.title)}">${src ? `<img src="${src}" alt="">` : `<span class="ti">${ic(u.type === 'text' ? 'file-text' : 'image')}</span>`}
      <span class="ref-no">${esc(no)}</span>${esc(u.title)}${ver}${changed ? '<span class="ref-new">已更新</span>' : ''}</button><button class="ref-x" data-unref="${u.id}" aria-label="断开参考 ${esc(u.title)}">${ic('x')}</button></span>`
  }).join('')
    + `<button class="ref-add" data-c="ref" aria-label="引用参考" data-kbd="@"><span class="at">@</span></button>`
}

function boxHTML(n) {
  const m = modelOf(n)
  const running = ui.running.has(n.id)
  return `<div class="refs">${refsHTML(n)}</div>
    <div class="prompt" contenteditable="true" role="textbox" aria-multiline="true" aria-label="提示词" spellcheck="false"
      data-placeholder="描述你想${n.type === 'video' ? '让画面怎样动起来' : '生成的画面'}，@ 引用已连接的参考，/ 使用预设">${toHTML(n.prompt, n.id)}</div>
    <div class="composer-foot">
      <button class="chip" data-c="model" aria-expanded="false">${ic('sparkles')}${esc(m.name)}<span class="i chev">${''}</span></button>
      <button class="chip" data-c="ratio" aria-expanded="false">${ratioIcon(n.ratio)}${n.ratio}${n.type === 'image' ? ` · ${n.count} 张` : ''}</button>
      <span class="grow"></span>
      <span class="cost">${running ? '生成中…' : `${costOf(n)} 点`}</span>
      <button class="send" data-c="go" aria-label="${running ? '停止' : '生成'}" data-kbd="Enter" ${!running && !(n.prompt ?? '').trim() && !upstream(n.id).length ? 'disabled' : ''}>${ic(running ? 'square' : 'arrow-up')}</button>
    </div>`
}

function refreshBox() {
  const n = single()
  if (!box || !n) return
  const old = box.querySelector('.prompt')
  const focused = old && document.activeElement === old
  const sel = focused && getSelection().rangeCount ? getSelection().getRangeAt(0).cloneRange() : null
  box.classList.toggle('busy', ui.running.has(n.id))
  box.innerHTML = boxHTML(n)
  box.querySelectorAll('.chev').forEach((c) => (c.innerHTML = ic('chevron-down')))
  // 正在输入时保留原编辑器节点（光标、输入法状态都在上面），只同步 chip 的缩略图与编号
  if (focused) {
    box.querySelector('.prompt').replaceWith(old)
    syncChips(old, n)
    old.focus()
    if (sel) { getSelection().removeAllRanges(); getSelection().addRange(sel) }
  }
  position()
}
// 上游换了主版本、改了名或断开连线时，已写进句子的 chip 跟着更新
function syncChips(el, n) {
  const labels = refLabels(n.id)
  for (const chip of el.querySelectorAll('.mention')) {
    const u = labels.has(chip.dataset.ref) && byId(chip.dataset.ref)
    if (!u) { chip.remove(); continue }
    chip.dataset.label = labels.get(u.id)
    chip.querySelector('.m-label').textContent = u.title
    const img = chip.querySelector('img')
    if (img && mainSrc(u) && img.getAttribute('src') !== mainSrc(u)) img.src = mainSrc(u)
  }
}

function modelPopover(n, owner) {
  const list = models[n.type]
  const pop = openPopover(list.map((m) => `<button class="model-item${m.key === n.model ? ' active' : ''}" data-m="${m.key}">
      <span class="logo">${m.logo}</span><span><span class="name">${esc(m.name)}</span><br><span class="desc">${esc(m.desc)}</span></span>
      <span class="price">${m.price} 点</span>${m.key === n.model ? `<span class="check">${ic('check')}</span>` : ''}</button>`).join(''),
    { rect: owner.getBoundingClientRect() }, owner)
  pop.addEventListener('click', (e) => {
    const k = e.target.closest('[data-m]')?.dataset.m
    if (!k) return
    n.model = k
    save()
    closeFloating()
    refreshBox()
  })
}

function ratioPopover(n, owner) {
  const html = `<div class="opt-group"><div class="opt-title">比例</div><div class="seg">${ratios.map((r) => `<button data-r="${r}" class="${r === n.ratio ? 'on' : ''}">${ratioIcon(r)}${r}</button>`).join('')}</div></div>
    ${n.type === 'image' ? `<div class="opt-group"><div class="opt-title">数量</div><div class="seg">${[1, 2, 4].map((c) => `<button data-n="${c}" class="${c === n.count ? 'on' : ''}">${c} 张</button>`).join('')}</div></div>` : ''}`
  const pop = openPopover(html, { rect: owner.getBoundingClientRect() }, owner)
  pop.style.width = '300px'
  pop.addEventListener('click', (e) => {
    const r = e.target.closest('[data-r]')?.dataset.r
    const c = e.target.closest('[data-n]')?.dataset.n
    if (r && r !== n.ratio) {
      checkpoint()
      n.ratio = r
      if (!cover(n)) Object.assign(n, sizeFor(n.type, r))
    }
    if (c) n.count = Number(c)
    if (!r && !c) return
    commit()
    pop.querySelectorAll('.seg button').forEach((b) => b.classList.toggle('on', b.dataset.r === n.ratio || Number(b.dataset.n) === n.count))
    refreshBox()
  })
}

// ---------- @ 选择器 ----------
// 只列当前节点已连接的上游参考：连线决定「能引用什么」，@ 只决定「在句子哪里点名」。
// 焦点始终留在输入框：继续输入即过滤，方向键选择，Enter / Tab 确认，Esc 关闭。
let picker = null
function closePicker() { picker?.el.remove(); picker = null }
function pickerItems(n, query) {
  const labels = refLabels(n.id)
  const used = new Set([...(n.prompt ?? '').matchAll(MENTION)].map((m) => m[1]))
  const q = query.trim().toLowerCase()
  return upstream(n.id)
    .filter((u) => !q || u.title.toLowerCase().includes(q) || labels.get(u.id).toLowerCase().includes(q))
    .map((u) => ({ u, label: labels.get(u.id), used: used.has(u.id) }))
}
function openPicker(n, el, range) {
  if (!picker) {
    const pel = document.createElement('div')
    pel.className = 'mention-picker'
    pel.setAttribute('role', 'listbox')
    pel.setAttribute('aria-label', '引用参考')
    // 按下时不抢输入框焦点，光标和输入法状态都保留
    pel.addEventListener('mousedown', (e) => e.preventDefault())
    pel.addEventListener('pointerdown', (e) => e.stopPropagation())
    pel.addEventListener('click', (e) => {
      const i = e.target.closest('[data-pi]')?.dataset.pi
      if (i != null) return choose(Number(i))
      if (e.target.closest('[data-pick-asset]')) { const id = picker.n.id; closePicker(); emit('assets-pick', id) }
    })
    pel.addEventListener('pointermove', (e) => {
      const i = e.target.closest('[data-pi]')?.dataset.pi
      if (i != null && Number(i) !== picker.index) { picker.index = Number(i); paintPicker() }
    })
    document.body.append(pel)
    picker = { el: pel, index: 0 }
  }
  Object.assign(picker, { n, editor: el, range, query: range ? range.toString().slice(1) : '' })
  picker.items = pickerItems(n, picker.query)
  picker.index = Math.min(picker.index, Math.max(0, picker.items.length - 1))
  paintPicker()
}
function paintPicker() {
  const { el, items, index, query } = picker
  const empty = !upstream(picker.n.id).length
  el.innerHTML = `<div class="menu-label">${empty ? '还没有连接参考' : '已连接的参考'}</div>
    ${items.map(({ u, label, used }, i) => {
      const src = u.type !== 'text' ? mainSrc(u) : null
      return `<div class="mp-item${i === index ? ' active' : ''}" role="option" aria-selected="${i === index}" data-pi="${i}">
        ${src ? `<img src="${src}" alt="">` : `<span class="ti">${ic(u.type === 'text' ? 'file-text' : 'image')}</span>`}
        <span class="mp-title">${esc(u.title)}</span>
        <span class="mp-meta">${used ? '已引用 · ' : ''}${esc(u.type === 'text' ? '文本' : label)}</span></div>`
    }).join('')}
    ${!empty && !items.length ? `<div class="mp-empty">没有匹配「${esc(query)}」的参考</div>` : ''}
    ${empty ? '<div class="mp-empty">从节点左侧的 + 拖线连接画布中的节点，连上后就能在这里引用</div>' : ''}
    <div class="menu-sep"></div>
    <button class="menu-item" data-pick-asset>${ic('library')}<span>从资产库添加参考</span></button>`
  // 贴着光标出现；光标矩形取不到时退回输入框左下
  const r = picker.range?.getBoundingClientRect()
  const base = r && r.height ? r : picker.editor.getBoundingClientRect()
  const w = el.offsetWidth, h = el.offsetHeight
  let x = (r && r.height ? r.left : base.left + 8), y = (r && r.height ? r.bottom : base.bottom) + 6
  if (y + h > innerHeight - 8) y = (r && r.height ? r.top : base.top) - h - 6
  el.style.left = `${Math.max(8, Math.min(x, innerWidth - w - 8))}px`
  el.style.top = `${Math.max(8, y)}px`
}
function choose(i) {
  const item = picker?.items[i]
  if (!item) return
  const { n, editor, range } = picker
  closePicker()
  insertChip(editor, item.u, item.label, range)
  persistPrompt(n, editor)
}
function persistPrompt(n, el) {
  n.prompt = fromDOM(el)
  // 删空后浏览器会留一个 <br>，清掉才能显示占位文字
  if (!n.prompt.trim() && !el.querySelector('.mention')) el.innerHTML = ''
  save()
  const send = box?.querySelector('.send')
  if (send && !ui.running.has(n.id)) send.disabled = !n.prompt.trim() && !upstream(n.id).length
}

// ---------- 引用悬停预览 ----------
// 句子里的 chip 和引用行都能悬停看大图，同时说明发给模型时对应的编号
let peek = null, peekTimer = 0
function showPeek(target) {
  clearTimeout(peekTimer)
  peekTimer = setTimeout(() => {
    const id = target.dataset.ref ?? target.dataset.peek
    const n = single(), u = byId(id)
    if (!u || !n || !target.isConnected) return
    const label = refLabels(n.id).get(id)
    const src = u.type !== 'text' ? mainSrc(u) : null
    peek?.remove()
    peek = document.createElement('div')
    peek.className = 'ref-peek'
    peek.innerHTML = `${src ? `<img src="${src}" alt="">` : `<div class="peek-text">${esc(u.text ? new DOMParser().parseFromString(u.text, 'text/html').body.innerText : '（空文本）')}</div>`}
      <div class="peek-cap"><b>${esc(u.title)}</b><span>${u.type === 'text' ? '文本参考 · 引用处展开为原文' : `发送时记为 [${esc(label)}]`}</span></div>`
    document.body.append(peek)
    const r = target.getBoundingClientRect()
    const w = peek.offsetWidth, h = peek.offsetHeight
    const above = r.top - h - 8 > 8
    peek.style.left = `${Math.max(8, Math.min(r.left + r.width / 2 - w / 2, innerWidth - w - 8))}px`
    peek.style.top = `${above ? r.top - h - 8 : r.bottom + 8}px`
  }, 180)
}
function hidePeek() { clearTimeout(peekTimer); peek?.remove(); peek = null }

function slashMenu(n, el) {
  const menu = openMenu(PRESETS.map(([name, icon, sub]) => ({
    label: name, icon, sub, run: () => {
      // 只在开头触发：把 / 换成预设名，其余内容（含引用 chip）原样保留
      n.prompt = `/${name} ${fromDOM(el).replace(/^\//, '')}`
      save()
      const ed = box.querySelector('.prompt')
      ed.innerHTML = toHTML(n.prompt, n.id)
      caretToEnd(ed)
    },
  })), { x: box.getBoundingClientRect().left + 8, y: box.getBoundingClientRect().bottom + 6 })
  menu.classList.add('slash')
}

// ---------- 模拟生成 ----------
function generate(n) {
  if (ui.running.has(n.id)) {
    clearInterval(ui.running.get(n.id).timer)
    ui.running.delete(n.id)
    render()
    return refreshBox()
  }
  if (state.credits < costOf(n)) return toast('点数不足')
  const cost = costOf(n)
  // 真实实现里这就是请求体：占位词提示词 + 按编号排序的附件 + 占位词规则
  const request = compilePrompt(n)
  console.info('[v6 原型] 发送给模型', request)
  const job = { p: 0, label: '排队中' }
  ui.running.set(n.id, job)
  const t0 = Date.now()
  const total = n.type === 'video' ? 5200 : 3200
  job.timer = setInterval(() => {
    const t = Date.now() - t0
    job.p = Math.min(100, Math.round((t / total) * 100))
    job.label = t < 500 ? '排队中' : `生成中 ${job.p}%`
    const el = nodeEl(n.id)?.querySelector('.run-state')
    if (el) { el.querySelector('span:nth-child(2)').textContent = job.label; el.querySelector('.bar span').style.width = `${job.p}%` }
    if (t < total) return
    clearInterval(job.timer)
    ui.running.delete(n.id)
    checkpoint()
    const pool = photos.filter((p) => !n.versions.includes(p.src))
    const picks = Array.from({ length: n.type === 'image' ? n.count : 1 }, (_, i) => pool[(Math.floor(Math.random() * pool.length) + i * 3) % pool.length])
    n.versions.push(...picks.map((p) => p.src))
    n.lastRequest = request
    // 记下这次生成用的是上游哪一版，之后上游换主版本时能提示
    n.refsUsed = Object.fromEntries(upstream(n.id).filter(mainSrc).map((u) => [u.id, mainSrc(u)]))
    const batch = picks.length > 1 ? `${picks.length} 个版本` : '1 个版本'
    if (isPinned(n)) {
      // 主版本是手动定的：不抢，只把新结果放到预览里
      n.vi = n.versions.length - picks.length
      toast(`已生成 ${batch}，主版本仍是版本 ${mainIndex(n) + 1}`)
    } else {
      delete n.vi
      toast(`已生成 ${batch}，最新一版已作为主版本`)
    }
    state.credits -= cost
    emit('credits')
    commit()
    refreshBox()
  }, 120)
  render()
  refreshBox()
}

function mountBox(n) {
  box = document.createElement('div')
  box.className = 'composer'
  box.setAttribute('role', 'group')
  box.setAttribute('aria-label', `${n.title} 的生成设置`)
  overlay.append(box)
  boxFor = n.id
  refreshBox()
  box.addEventListener('pointerdown', (e) => e.stopPropagation())
  box.addEventListener('click', (e) => {
    const node = single()
    const ed = box.querySelector('.prompt')
    const unref = e.target.closest('[data-unref]')?.dataset.unref
    if (unref) {
      hidePeek()
      checkpoint()
      state.edges = state.edges.filter((x) => !(x.from === unref && x.to === node.id))
      // 断开参考后，句子里对它的引用也一并去掉
      node.prompt = (node.prompt ?? '').replace(new RegExp(`@\\[${unref}\\] ?`, 'g'), '')
      commit()
      box.querySelector('.prompt').innerHTML = toHTML(node.prompt, node.id)
      return refreshBox()
    }
    // 点引用行里的参考：在光标处（没有光标就在句末）把它 @ 进句子
    const mention = e.target.closest('[data-mention]')?.dataset.mention
    if (mention) {
      const u = byId(mention)
      const inside = getSelection().rangeCount && ed.contains(getSelection().getRangeAt(0).startContainer)
      if (!inside) caretToEnd(ed)
      insertChip(ed, u, refLabels(node.id).get(u.id))
      return persistPrompt(node, ed)
    }
    const c = e.target.closest('[data-c]')
    if (!c) return
    if (c.dataset.c === 'ref') {
      if (picker) return closePicker()
      const inside = getSelection().rangeCount && ed.contains(getSelection().getRangeAt(0).startContainer)
      if (!inside) caretToEnd(ed)
      const r = getSelection().getRangeAt(0).cloneRange()
      return openPicker(node, ed, r.collapsed ? null : r)
    }
    if (c.dataset.c === 'model') modelPopover(node, c)
    if (c.dataset.c === 'ratio') ratioPopover(node, c)
    if (c.dataset.c === 'go') generate(node)
  })
  // 按下 @ 按钮时别让输入框失焦，光标位置才能用来插入
  box.addEventListener('mousedown', (e) => { if (e.target.closest('[data-c="ref"],[data-mention]')) e.preventDefault() })
  box.addEventListener('input', (e) => {
    const node = single()
    const ed = e.target.closest?.('.prompt')
    if (!ed) return
    persistPrompt(node, ed)
    const q = mentionQuery(ed)
    if (q) openPicker(node, ed, q.range)
    else closePicker()
    if (e.inputType?.startsWith('insert') && /^\/$/.test(fromDOM(ed))) slashMenu(node, ed)
  })
  // 粘贴只收纯文本，避免带进外部样式
  box.addEventListener('paste', (e) => {
    if (!e.target.closest?.('.prompt')) return
    e.preventDefault()
    document.execCommand('insertText', false, e.clipboardData.getData('text/plain'))
  })
  box.addEventListener('focusout', (e) => { if (e.target.closest?.('.prompt')) setTimeout(() => { if (!box?.contains(document.activeElement)) closePicker() }) })
  box.addEventListener('pointerover', (e) => {
    const t = e.target.closest('.mention, [data-peek]')
    if (t) showPeek(t)
  })
  box.addEventListener('pointerout', (e) => {
    const t = e.target.closest('.mention, [data-peek]')
    if (t && !t.contains(e.relatedTarget)) hidePeek()
  })
  box.addEventListener('keydown', (e) => {
    e.stopPropagation()
    if (picker && !e.isComposing) {
      const len = picker.items.length
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault()
        if (len) { picker.index = (picker.index + (e.key === 'ArrowDown' ? 1 : len - 1)) % len; paintPicker() }
        return
      }
      if ((e.key === 'Enter' || e.key === 'Tab') && len) { e.preventDefault(); return choose(picker.index) }
      if (e.key === 'Escape') { e.preventDefault(); return closePicker() }
    }
    const ed = e.target.closest?.('.prompt')
    if (ed && e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault()
      if (!box.querySelector('.send').disabled) generate(single())
      return
    }
    // Shift+Enter 换行：统一插 <br>，不让浏览器包 div
    if (ed && e.key === 'Enter' && e.shiftKey && !e.isComposing) {
      e.preventDefault()
      document.execCommand('insertLineBreak')
      return
    }
    if (e.key === 'Escape') e.target.blur()
  })
}
// ---------- 连线断开条 ----------
function showEdgeBar() {
  if (!ui.edgeSel) {
    edgeBar?.remove()
    edgeBar = null
    return
  }
  if (!edgeBar) {
    edgeBar = document.createElement('div')
    edgeBar.className = 'node-bar'
    edgeBar.innerHTML = `<button class="tool" data-eb="cut">${ic('unlink')}断开连线</button>`
    edgeBar.addEventListener('click', () => {
      checkpoint()
      state.edges = state.edges.filter((e) => e.id !== ui.edgeSel)
      ui.edgeSel = null
      commit()
      emit('select')
    })
    overlay.append(edgeBar)
  }
  position()
}

// ---------- 全屏预览 ----------
// 全屏里可以左右翻版本，并直接把看中的一张设为主版本
let lbId = null
function openLightbox(id) {
  const n = byId(id)
  if (!n || !cover(n)) return
  lbId = id
  lightbox.hidden = false
  paintLightbox()
  lightbox.querySelector('[data-x]').focus()
}
function paintLightbox() {
  const n = byId(lbId)
  const i = n.vi ?? mainIndex(n)
  const multi = n.versions.length > 1
  const isMain = i === mainIndex(n)
  lightbox.innerHTML = `<img src="${cover(n)}" alt="${esc(n.title)}">
    <button class="icon-btn" data-x aria-label="关闭">${ic('x')}</button>
    ${multi ? `<button class="icon-btn lb-nav prev" data-step="-1" aria-label="上一版">${ic('chevron-left')}</button>
      <button class="icon-btn lb-nav next" data-step="1" aria-label="下一版">${ic('chevron-right')}</button>` : ''}
    <div class="caption">${esc(n.title)}${multi ? ` · 版本 ${i + 1}/${n.versions.length}` : ''}
      ${multi ? (isMain ? '<span class="lb-main">主版本</span>' : `<button class="btn btn-primary" data-lb-main="${i}">设为主版本</button>`) : ''}</div>`
}
function stepLightbox(d) {
  const n = byId(lbId)
  const len = n.versions.length
  n.vi = ((n.vi ?? mainIndex(n)) + d + len) % len
  paintLightbox()
  render()
}
const closeLightbox = () => { lightbox.hidden = true; lightbox.innerHTML = ''; lbId = null; sync() }
lightbox.addEventListener('click', (e) => {
  if (e.target === lightbox || e.target.closest('[data-x]')) return closeLightbox()
  const step = e.target.closest('[data-step]')?.dataset.step
  if (step) return stepLightbox(Number(step))
  const m = e.target.closest('[data-lb-main]')?.dataset.lbMain
  if (m != null) {
    checkpoint()
    setMain(byId(lbId), Number(m))
    commit()
    paintLightbox()
    toast('已设为主版本，下游节点将使用这一版')
  }
})
lightbox.addEventListener('keydown', (e) => {
  if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') { e.preventDefault(); e.stopPropagation(); if (byId(lbId)?.versions.length > 1) stepLightbox(e.key === 'ArrowLeft' ? -1 : 1) }
})

// ---------- 订阅与同步 ----------
function sync() {
  const n = single()
  const showBar = ui.sel.size > 0 && !dragging && !ui.editing
  if (showBar && !bar) {
    bar = document.createElement('div')
    bar.className = 'node-bar'
    bar.addEventListener('pointerdown', (e) => e.stopPropagation())
    bar.addEventListener('click', onBar)
    bar.addEventListener('dblclick', (e) => e.stopPropagation())
    overlay.append(bar)
  }
  if (!showBar && bar) { bar.remove(); bar = null }
  const wantBox = n && isMedia(n) && !dragging && !ui.editing
  if (wantBox && (!box || boxFor !== n.id)) {
    box?.remove()
    mountBox(n)
  }
  if (!wantBox && box) { closeFloating(); box.remove(); box = null; boxFor = null }
  if (bar) { bar.innerHTML = barHTML(); position() }
  // 上游连线、主版本、标题都可能变了；refreshBox 在输入中会保留编辑器本身
  if (box) refreshBox()
  if (!box) closePicker()
  showEdgeBar()
}

function onKey(e) {
  const typing = /input|textarea/i.test(e.target.tagName) || e.target.isContentEditable
  if (e.key === 'Escape' && !lightbox.hidden) return closeLightbox()
  // 焦点在按钮上时 Enter / Space 交给按钮本身（比如端口 +）
  if (typing || e.target.closest?.('button')) return
  if (e.key === ' ' && single() && cover(single())) { e.preventDefault(); return openLightbox(single().id) }
  if (e.key === 'Enter' && single()?.type === 'text') { e.preventDefault(); return startEdit(single().id) }
}

export function initComposer() {
  on('select', sync)
  on('change', sync)
  on('view', () => { closePicker(); hidePeek(); position() })
  on('dragstart', () => { dragging = true; sync() })
  on('dragend', () => { dragging = false; sync() })
  on('lightbox', openLightbox)
  on('focus', (id) => { const n = byId(id); const ed = box?.querySelector('.prompt'); if (n && isMedia(n) && !ui.running.has(id) && ed) caretToEnd(ed) })
  document.addEventListener('keydown', onKey)
  sync()
}
