// 创意空间首页原型：一句话开始、继续创作、模板、最近素材，以及完整项目列表。
// 数据只在内存里，刷新即回到示例。
import { ic, esc, hydrateIcons, openMenu, openPopover, closeFloating, toast } from './ui.mjs'
import { mountSide, setCurrent, setRecent } from './side.mjs'

const P = [
  [1, 1.5], [2, 0.667], [3, 1.5], [4, 0.667], [5, 1.5], [6, 1.47],
  [7, 1.333], [8, 1.5], [9, 0.667], [10, 1.5], [11, 0.75], [12, 1.333],
].map(([n, ratio]) => ({ src: `../v4/assets/photo-${String(n).padStart(2, '0')}.jpg`, ratio }))
const photo = (n) => P[n - 1]

// 封面坐标：x/y/w 为封面宽高的百分比；图片节点高度按原图比例算，文字节点给 h。
const projects = [
  {
    id: 'p1', name: '九月，向海而行', ago: 12, nodes: 18, state: 'agent', stateText: '助手运行中',
    cover: { nodes: [{ x: 5, y: 16, w: 17, h: 34, text: true }, { x: 28, y: 8, w: 14, p: 10 }, { x: 28, y: 56, w: 22, p: 11 }, { x: 56, y: 10, w: 16, p: 7 }, { x: 77, y: 10, w: 16, p: 12 }, { x: 56, y: 54, w: 16, empty: true }], edges: [[0, 1], [0, 2], [1, 3], [1, 4], [2, 5]] },
  },
  {
    id: 'p2', name: '雪山婚纱 · 二稿', ago: 95, nodes: 11, state: 'gen', stateText: '生成中 · 2',
    cover: { nodes: [{ x: 6, y: 12, w: 26, p: 2 }, { x: 6, y: 54, w: 18, h: 30, text: true }, { x: 40, y: 8, w: 15, p: 5 }, { x: 60, y: 8, w: 15, p: 1 }, { x: 80, y: 8, w: 15, empty: true, h: 36 }], edges: [[0, 2], [1, 2], [2, 3], [3, 4]] },
  },
  {
    id: 'p3', name: '品牌 Lookbook · 春季', ago: 60 * 26, nodes: 24, state: 'wait', stateText: '等你确认',
    cover: { nodes: [{ x: 4, y: 8, w: 18, p: 9 }, { x: 4, y: 40, w: 18, p: 4 }, { x: 4, y: 72, w: 18, h: 20, text: true }, { x: 30, y: 6, w: 20, p: 6 }, { x: 55, y: 6, w: 20, p: 8 }, { x: 80, y: 6, w: 16, p: 3 }], edges: [[0, 3], [1, 3], [3, 4], [4, 5]] },
  },
  {
    id: 'p4', name: '毕业照 · 光影练习', ago: 60 * 50, nodes: 9,
    cover: { nodes: [{ x: 10, y: 10, w: 22, p: 8 }, { x: 40, y: 18, w: 22, h: 40, text: true }, { x: 70, y: 10, w: 22, p: 3 }], edges: [[0, 1], [1, 2]] },
  },
  { id: 'p5', name: '未命名项目', ago: 60 * 75, nodes: 0, cover: { nodes: [], edges: [] } },
  {
    id: 'p6', name: '咖啡馆情侣写真', ago: 60 * 24 * 6, nodes: 14,
    cover: { nodes: [{ x: 8, y: 14, w: 30, p: 11 }, { x: 46, y: 10, w: 18, p: 6 }, { x: 70, y: 10, w: 22, h: 34, text: true }], edges: [[0, 1], [1, 2]] },
  },
  {
    id: 'p7', name: '城市夜拍 · 街头线条', ago: 60 * 24 * 21, nodes: 7, archived: true,
    cover: { nodes: [{ x: 12, y: 10, w: 20, p: 8 }, { x: 44, y: 10, w: 20, p: 1 }], edges: [[0, 1]] },
  },
  {
    id: 'p8', name: '白裙与拱门', ago: 60 * 24 * 40, nodes: 5, archived: true,
    cover: { nodes: [{ x: 30, y: 8, w: 22, p: 5 }, { x: 58, y: 18, w: 20, h: 36, text: true }], edges: [[0, 1]] },
  },
]

const templates = [
  { name: '婚纱外景 · 拍摄策划', desc: '从地点、时间到逐镜头分镜', pics: [5, 10, 7], tags: ['参考组', '分镜 8 格', '器材清单'], skill: '拍摄策划' },
  { name: '人像布光 · 三灯方案', desc: '布光示意、参数和对照样片', pics: [3, 1, 6], tags: ['布光图', '参数', '参考'], skill: '布光方案' },
  { name: '品牌 Lookbook 情绪板', desc: '色板、搭配与场景参考', pics: [9, 4, 11], tags: ['情绪板', '色板', '搭配'], skill: '情绪板' },
  { name: '海边写真 · 分镜草图', desc: '机位、时间线和生成草图', pics: [12, 10, 2], tags: ['分镜', '机位', '时间线'], skill: '分镜草图' },
]

const skills = [
  { name: '普通对话', sub: '自由讨论，助手按需要读写画布' },
  { name: '拍摄策划', sub: '地点、时间、流程与分镜' },
  { name: '布光方案', sub: '灯位示意、参数与参考' },
  { name: '分镜草图', sub: '逐镜头描述并生成草图' },
  { name: '情绪板', sub: '整理参考、色板与关键词' },
  { name: '精修与扩图', sub: '基于参考图重绘、扩图、换背景' },
]
const models = [
  { name: 'DeepSeek V3.2', sub: '文字' },
  { name: 'Qwen3 VL', sub: '识图 · 可读参考图' },
]

const starters = [
  { label: '婚纱外景策划', pic: 5, skill: '拍摄策划', text: '下个月在海边拍一组婚纱外景，傍晚逆光为主，想要安静克制的感觉。帮我出拍摄策划和分镜。' },
  { label: '棚拍布光方案', pic: 3, skill: '布光方案', text: '棚拍半身人像，想要暗调伦勃朗光，背景深灰。帮我出布光方案和相机参数。' },
  { label: '品牌情绪板', pic: 9, skill: '情绪板', text: '春季品牌 Lookbook，低饱和暖调、自然光，帮我整理一版情绪板。' },
  { label: '写真分镜草图', pic: 12, skill: '分镜草图', text: '海边写真，12 个镜头，从午后到日落。按时间线出分镜并生成草图。' },
  { label: '客片扩图换背景', pic: 6, skill: '精修与扩图', text: '把参考图扩成 4:5 竖幅，背景换成海边自然光，人物保持不变。' },
]

let refs = []
let skill = skills[0].name
let model = models[0].name
let archivedView = false
let sort = '最近编辑'
let query = ''
let seq = 100

const $ = (id) => document.getElementById(id)
const ago = (m) =>
  m < 1 ? "刚刚" : m < 60 ? `${m} 分钟前` : m < 60 * 24 ? `${Math.round(m / 60)} 小时前` : m < 60 * 24 * 2 ? '昨天' : `${Math.round(m / 60 / 24)} 天前`

// ——封面：画布缩影——
function cover(p) {
  const { nodes, edges } = p.cover
  if (!nodes.length) return `<div class="cover"><span class="cover-blank">空白画布</span>${badge(p)}</div>`
  const boxes = nodes.map((n) => ({ ...n, h: n.h ?? (n.p ? n.w * 1.6 * photo(n.p).ratio : 30) }))
  const lines = edges
    .map(([a, b]) => {
      const s = boxes[a], t = boxes[b]
      const x1 = s.x + s.w, y1 = (s.y + s.h / 2) * 0.625, x2 = t.x, y2 = (t.y + t.h / 2) * 0.625
      const mid = (x1 + x2) / 2
      return `<path d="M${x1} ${y1} C${mid} ${y1} ${mid} ${y2} ${x2} ${y2}"/>`
    })
    .join('')
  const cells = boxes
    .map((n) => {
      const style = `left:${n.x}%;top:${n.y}%;width:${n.w}%;height:${n.h}%`
      if (n.text) return `<div class="cn cn-text" style="${style}"><i></i><i></i><i></i><i></i></div>`
      if (n.empty) return `<div class="cn empty" style="${style}"></div>`
      return `<div class="cn" style="${style}"><img src="${photo(n.p).src}" alt="" loading="lazy"></div>`
    })
    .join('')
  const running = p.state === 'agent' || p.state === 'gen'
  return `<div class="cover${running ? ' running' : ''}"><svg viewBox="0 0 100 62.5" preserveAspectRatio="none" aria-hidden="true">${lines}</svg>${cells}${badge(p)}</div>`
}
const badge = (p) => (p.state ? `<span class="badge${p.state === 'wait' ? ' wait' : ''}">${esc(p.stateText)}</span>` : '')

function card(p, fresh) {
  const meta = p.nodes ? `${ago(p.ago)} · ${p.nodes} 个节点` : `${ago(p.ago)} · 还没有内容`
  return `<div class="card${fresh ? ' fresh' : ''}" data-id="${p.id}">
    <a class="card-open" href="./index.html" aria-label="打开项目：${esc(p.name)}">${cover(p)}</a>
    <div class="card-meta"><span class="t"><strong>${esc(p.name)}</strong><small>${meta}</small></span>
      <button class="icon-btn menu-btn" aria-label="项目操作" aria-expanded="false">${ic('ellipsis')}</button></div>
  </div>`
}

// ——渲染——
function renderHome(freshID) {
  const live = projects.filter((p) => !p.archived).sort((a, b) => a.ago - b.ago)
  $('recent').innerHTML = live.slice(0, 4).map((p) => card(p, p.id === freshID)).join('')
  const running = live.filter((p) => p.state === 'agent' || p.state === 'gen').length
  const waiting = live.filter((p) => p.state === 'wait').length
  const h = new Date().getHours()
  const greet = h < 6 ? '夜深了' : h < 11 ? '早上好' : h < 13 ? '中午好' : h < 18 ? '下午好' : '晚上好'
  const status = [running && `${running} 个项目在运行`, waiting && `${waiting} 个等你确认`].filter(Boolean).join('，')
  $('hello').textContent = `${greet}，Samson${status ? ` · ${status}` : ''}`
  setRecent(live.map((p) => {
    const first = p.cover.nodes.find((n) => n.p)
    const state = p.state === 'agent' || p.state === 'gen' ? 'run' : p.state === 'wait' ? 'wait' : undefined
    return { name: p.name, thumb: first ? photo(first.p).src : '', state }
  }))
}

function renderRefs() {
  $('refs').innerHTML = refs
    .map((r, i) => `<div class="ref" style="background-image:url(${r.src})"><small>图${i + 1}</small><button type="button" aria-label="移除参考" data-rm="${i}">${ic('x')}</button></div>`)
    .join('')
  document.querySelectorAll('.asset').forEach((el) => el.classList.toggle('used', refs.some((r) => r.src === el.dataset.src)))
  syncSend()
}
function syncSend() {
  $('send').disabled = !$('prompt').value.trim()
}

function renderStarters() {
  $('starters').innerHTML = starters
    .map((s, i) => `<button type="button" class="starter" data-s="${i}"><span class="pic" style="background-image:url(${photo(s.pic).src})"></span>${esc(s.label)}</button>`)
    .join('')
}
function renderTemplates() {
  $('templates').innerHTML = templates
    .map((t, i) => `<button type="button" class="tpl" data-t="${i}">
      <span class="tpl-pic">${t.pics.map((n) => `<img src="${photo(n).src}" alt="" loading="lazy">`).join('')}<span class="tpl-go">用这个模板${ic('arrow-right')}</span></span>
      <strong>${esc(t.name)}</strong><small>${esc(t.desc)}</small>
      <span class="tags">${t.tags.map((x) => `<span>${esc(x)}</span>`).join('')}</span></button>`)
    .join('')
}
function renderAssets() {
  $('assets').innerHTML = [4, 10, 3, 9, 7, 1, 11, 6, 12, 2, 8, 5]
    .map((n) => `<div class="asset" data-src="${photo(n).src}"><img src="${photo(n).src}" alt="" loading="lazy"><button type="button" data-add="${n}">${ic('plus')}参考</button></div>`)
    .join('')
}

function renderAll() {
  const order = {
    最近编辑: (a, b) => a.ago - b.ago,
    最早编辑: (a, b) => b.ago - a.ago,
    名称: (a, b) => a.name.localeCompare(b.name, 'zh-CN'),
  }[sort]
  const list = projects
    .filter((p) => !!p.archived === archivedView && p.name.includes(query.trim()))
    .sort(order)
  $('all').innerHTML = list.map((p) => card(p)).join('')
  const empty = $('listEmpty')
  empty.hidden = list.length > 0
  empty.textContent = query
    ? `没有名称包含「${query.trim()}」的项目`
    : archivedView ? '还没有归档的项目。归档后项目会留在这里，随时可以恢复。' : '还没有项目。点右上角「新建项目」，或回首页用一句话开始。'
}

function route() {
  const view = location.hash === '#projects' ? 'projects' : 'home'
  $('view-home').hidden = view !== 'home'
  $('view-projects').hidden = view !== 'projects'
  setCurrent(view)
  document.title = view === 'home' ? '创意空间' : '项目 · 创意空间'
  $('main').scrollTop = 0
  if (view === 'projects') renderAll()
}

// ——新建——
function addProject(p) {
  projects.push(p)
  renderHome(p.id)
  if (location.hash === '#projects') renderAll()
  toast(`已新建「${p.name}」`, { label: '打开', run: () => { location.href = './index.html' } })
}
// 一句话、空白画布、模板都是「开始做」，直接进画布；复制项目才留在原地。
function openCanvas() {
  location.href = './index.html'
}
function fromPrompt() {
  const text = $('prompt').value.trim()
  if (!text) return
  const nodes = [{ x: 6, y: 18, w: 20, h: 34, text: true }]
  refs.slice(0, 3).forEach((r, i) => nodes.push({ x: 34 + i * 20, y: 10, w: 16, p: P.indexOf(r) + 1 }))
  // 名字先取想法的开头，助手第一次回答后会替它起正式的名字。
  const name = text.replace(/\s+/g, ' ').slice(0, 14) + (text.length > 14 ? '…' : '')
  addProject({
    id: `n${seq++}`, name, ago: 0, nodes: nodes.length, state: 'agent', stateText: `${skill === '普通对话' ? '助手' : skill} · 运行中`,
    cover: { nodes, edges: nodes.slice(1).map((_, i) => [0, i + 1]) },
  })
  $('prompt').value = ''
  refs = []
  renderRefs()
  document.querySelectorAll('.starter.on').forEach((b) => b.classList.remove('on'))
  autosize()
  openCanvas()
}
function blank() {
  addProject({ id: `n${seq++}`, name: '未命名项目', ago: 0, nodes: 0, cover: { nodes: [], edges: [] } })
  openCanvas()
}
function fromTemplate(t) {
  addProject({
    id: `n${seq++}`, name: t.name, ago: 0, nodes: 12,
    cover: { nodes: [{ x: 5, y: 14, w: 18, h: 36, text: true }, ...t.pics.map((n, i) => ({ x: 30 + i * 23, y: 10, w: 19, p: n }))], edges: [[0, 1], [1, 2], [2, 3]] },
  })
  openCanvas()
}

// ——项目操作——
function projectMenu(btn) {
  const p = projects.find((x) => x.id === btn.closest('.card').dataset.id)
  const items = p.archived
    ? [{ label: '恢复', icon: 'undo-2', run: () => setArchived(p, false) }]
    : [
        { label: '打开', icon: 'external-link', run: () => { location.href = './index.html' } },
        { label: '重命名', icon: 'pencil', run: () => rename(p) },
        { label: '复制项目', icon: 'copy', run: () => duplicate(p) },
        'sep',
        { label: '归档', icon: 'package', disabled: p.state === 'agent' || p.state === 'gen', sub: p.state === 'agent' || p.state === 'gen' ? '任务结束后才能归档' : undefined, run: () => setArchived(p, true) },
      ]
  openMenu(items, { rect: btn.getBoundingClientRect(), align: 'end' }, btn)
}
function rerender() {
  renderHome()
  if (location.hash === '#projects') renderAll()
}
function setArchived(p, value) {
  p.archived = value
  rerender()
  toast(value ? `已归档「${p.name}」` : `已恢复「${p.name}」`, { label: '撤销', run: () => { p.archived = !value; rerender() } })
}
function duplicate(p) {
  addProject({ ...structuredClone(p), id: `n${seq++}`, name: `${p.name} 副本`, ago: 0, state: undefined, stateText: undefined })
}
function rename(p) {
  const list = location.hash === '#projects' ? $('all') : $('recent')
  const strong = list.querySelector(`.card[data-id="${p.id}"] .card-meta strong`)
  if (!strong) return
  const input = document.createElement('input')
  input.className = 'rename'
  input.value = p.name
  input.maxLength = 60
  strong.replaceWith(input)
  input.focus()
  input.select()
  let cancelled = false
  input.addEventListener('keydown', (e) => {
    if (e.isComposing) return
    if (e.key === 'Escape') cancelled = true
    if (e.key === 'Enter' || e.key === 'Escape') input.blur()
  })
  input.addEventListener('blur', () => {
    const v = input.value.trim()
    if (!cancelled && v) p.name = v
    rerender()
  })
}

// ——参考——
function addRef(src) {
  const item = P.find((x) => x.src === src)
  if (!item || refs.includes(item)) return
  if (refs.length >= 6) return toast('一次最多 6 张参考')
  refs.push(item)
  renderRefs()
}
function pickFromLibrary(btn) {
  const html = `<div class="pick"><p>从资产库选择</p><div class="pick-grid">${P.map((x) => `<button type="button" data-src="${x.src}" style="background-image:url(${x.src})" aria-label="选择这张"></button>`).join('')}</div></div>`
  const pop = openPopover(html, { rect: btn.getBoundingClientRect() }, btn, 'pick-pop')
  pop?.addEventListener?.('click', (e) => {
    const b = e.target.closest('[data-src]')
    if (b) { addRef(b.dataset.src); closeFloating() }
  })
}
function autosize() {
  const t = $('prompt')
  t.style.height = 'auto'
  t.style.height = `${Math.min(t.scrollHeight, 200)}px`
}

// ——事件——
mountSide($('side'), { current: 'home' })
hydrateIcons()
renderStarters()
renderTemplates()
renderAssets()
renderHome()
renderRefs()
route()
addEventListener('hashchange', route)

$('prompt').addEventListener('input', () => { syncSend(); autosize() })
$('prompt').addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); fromPrompt() }
})
$('ask').addEventListener('submit', (e) => { e.preventDefault(); fromPrompt() })
$('blank').addEventListener('click', blank)
$('newProject').addEventListener('click', blank)
$('refs').addEventListener('click', (e) => {
  const rm = e.target.closest('[data-rm]')
  if (rm) { refs.splice(Number(rm.dataset.rm), 1); renderRefs() }
})
$('addRef').addEventListener('click', (e) => {
  const btn = e.currentTarget
  openMenu([
    { label: '上传本地图片', icon: 'upload', run: () => $('file').click() },
    { label: '从资产库选择', icon: 'library', run: () => requestAnimationFrame(() => pickFromLibrary(btn)) },
  ], { rect: btn.getBoundingClientRect() }, btn)
})
$('file').addEventListener('change', (e) => {
  for (const f of e.target.files) {
    const src = URL.createObjectURL(f)
    P.push({ src, ratio: 1 })
    addRef(src)
  }
  e.target.value = ''
})
// 拖入本地图片
const ask = $('ask')
ask.addEventListener('dragover', (e) => { if (e.dataTransfer.types.includes('Files')) { e.preventDefault(); ask.classList.add('dragging') } })
ask.addEventListener('dragleave', (e) => { if (!ask.contains(e.relatedTarget)) ask.classList.remove('dragging') })
ask.addEventListener('drop', (e) => {
  e.preventDefault()
  ask.classList.remove('dragging')
  for (const f of e.dataTransfer.files) {
    if (!f.type.startsWith('image/')) continue
    const src = URL.createObjectURL(f)
    P.push({ src, ratio: 1 })
    addRef(src)
  }
})

function chipMenu(btn, list, current, set) {
  openMenu(list.map((x) => ({ label: x.name, sub: x.sub, checked: x.name === current, run: () => { set(x.name); btn.querySelector('.v').textContent = x.name } })), { rect: btn.getBoundingClientRect() }, btn)
}
$('skill').addEventListener('click', (e) => chipMenu(e.currentTarget, skills, skill, (v) => { skill = v }))
$('model').addEventListener('click', (e) => chipMenu(e.currentTarget, models, model, (v) => { model = v }))

$('starters').addEventListener('click', (e) => {
  const b = e.target.closest('.starter')
  if (!b) return
  const s = starters[Number(b.dataset.s)]
  document.querySelectorAll('.starter.on').forEach((x) => x.classList.remove('on'))
  b.classList.add('on')
  skill = s.skill
  $('skill').querySelector('.v').textContent = s.skill
  $('prompt').value = s.text
  syncSend()
  autosize()
  $('prompt').focus()
  $('prompt').setSelectionRange(s.text.length, s.text.length)
})
$('templates').addEventListener('click', (e) => {
  const t = e.target.closest('.tpl')
  if (t) fromTemplate(templates[Number(t.dataset.t)])
})
$('assets').addEventListener('click', (e) => {
  const b = e.target.closest('[data-add]')
  if (!b) return
  addRef(photo(Number(b.dataset.add)).src)
  $('main').scrollTo({ top: 0, behavior: 'smooth' })
  $('prompt').focus({ preventScroll: true })
})
document.addEventListener('click', (e) => {
  const m = e.target.closest('.menu-btn')
  if (m) { e.preventDefault(); projectMenu(m) }
})

document.querySelectorAll('.seg button').forEach((b) =>
  b.addEventListener('click', () => {
    archivedView = b.dataset.archived === '1'
    document.querySelectorAll('.seg button').forEach((x) => x.setAttribute('aria-pressed', String(x === b)))
    renderAll()
  }),
)
$('search').addEventListener('input', (e) => { query = e.target.value; renderAll() })
$('sort').addEventListener('click', (e) =>
  chipMenu(e.currentTarget, [{ name: '最近编辑' }, { name: '最早编辑' }, { name: '名称' }], sort, (v) => { sort = v; renderAll() }),
)
document.addEventListener('keydown', (e) => {
  if (e.key === '/' && document.activeElement === document.body && location.hash !== '#projects') { e.preventDefault(); $('prompt').focus() }
})
