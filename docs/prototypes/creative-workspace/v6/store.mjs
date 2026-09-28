// 原型状态：节点/连线/视口 + 快照式撤销，本地保存到 localStorage。
// 加入分组后示例数据结构变化，换 key 让示例直接包含一个组
const KEY = 'creative-canvas-prototype-v6.2'

export const photos = [
  ['01', 850, 1275, '光落在她停留的地方'], ['02', 850, 567, '把风景框进画面'],
  ['03', 850, 1275, '一束光'], ['04', 850, 567, '海岸边的紫色'],
  ['05', 850, 1275, '白裙与拱形'], ['06', 850, 1249, '柔软的白'],
  ['07', 850, 1133, '花影'], ['08', 850, 1275, '城市光影'],
  ['09', 850, 567, '弧线与玫瑰色'], ['10', 850, 1275, '等一阵海风'],
  ['11', 850, 638, '蓝色'], ['12', 850, 1133, '去海边找留白'],
].map(([n, w, h, title]) => ({ src: `../v4/assets/photo-${n}.jpg`, w, h, title }))

export const models = {
  image: [
    { key: 'seedream', name: 'Seedream 5.0', desc: '人像与质感', logo: 'SD', price: 6 },
    { key: 'banana', name: 'Banana Pro', desc: '参考一致性', logo: 'BN', price: 8 },
    { key: 'gpt-image', name: 'GPT Image 2', desc: '构图与文字', logo: 'GI', price: 10 },
  ],
  video: [
    { key: 'kling', name: 'Kling 3.0', desc: '5 秒 · 运镜', logo: 'KL', price: 40 },
    { key: 'veo', name: 'Veo 3', desc: '8 秒 · 带声音', logo: 'VE', price: 90 },
  ],
}
export const ratios = ['1:1', '3:4', '4:3', '9:16', '16:9']
// 新建节点一律 1:1，需要别的画幅在输入框的比例 chip 里改
export const DEFAULT_RATIO = '1:1'
export const WIDTH = { text: 300, image: 300, video: 300 }

export function sizeFor(type, ratio) {
  const w = WIDTH[type]
  if (type === 'text') return { w, h: 220 }
  const [a, b] = ratio.split(':').map(Number)
  return { w, h: Math.round((w * b) / a) }
}

let seq = Date.now() % 100000
export const uid = (p) => `${p}-${(seq++).toString(36)}`

function seed() {
  const p = photos
  const img = (id, x, y, ph, title, extra = {}) => ({
    id, type: 'image', x, y, w: 300, h: Math.round((300 * ph.h) / ph.w), title,
    versions: [ph.src], main: null, prompt: '', model: 'seedream', ratio: DEFAULT_RATIO, count: 1, ...extra,
  })
  return {
    name: '九月，向海而行',
    nodes: [
      { id: 'g-refs', type: 'group', title: '参考素材', parent: null, x: 0, y: 0, w: 0, h: 0 },
      { id: 'brief', type: 'text', x: 0, y: 40, w: 300, h: 240, title: '拍摄概念',
        text: '<h4>九月，向海而行</h4><p>傍晚六点的海边，逆光，白色长裙被风带起。</p><p>情绪：安静、留白、低饱和暖调，像一卷过期的胶片。</p><p>机位以中远景为主，保留大面积天空。</p>' },
      img('ref-1', 400, -120, p[9], '参考 · 海风', { parent: 'g-refs' }),
      img('ref-2', 400, 390, p[11], '参考 · 留白', { parent: 'g-refs' }),
      // 示例：生成过 3 版，摄影师手动把第 1 版定为主版本
      img('shot-1', 820, 60, p[4], '镜头 01 · 逆光长裙', {
        versions: [p[4].src, p[0].src, p[2].src], main: 0,
        prompt: '@[ref-1] 傍晚海边逆光，白色长裙被风吹起，胶片颗粒，低饱和暖调，中远景留出天空',
      }),
      { id: 'shot-2', type: 'image', x: 1240, y: 60, w: 300, h: 300, title: '镜头 02 · 背影',
        versions: [], main: null, prompt: '', model: 'banana', ratio: DEFAULT_RATIO, count: 2 },
    ],
    edges: [
      { id: 'e1', from: 'brief', to: 'ref-1' },
      { id: 'e2', from: 'brief', to: 'ref-2' },
      { id: 'e3', from: 'ref-1', to: 'shot-1' },
      { id: 'e4', from: 'shot-1', to: 'shot-2' },
    ],
    credits: 1280,
  }
}

function load() {
  try {
    const raw = localStorage.getItem(KEY)
    if (raw) return JSON.parse(raw)
  } catch { /* 损坏时回到示例 */ }
  return seed()
}

export const state = load()
// 旧数据里提示词直接写 @节点名，迁移成引用 token：只认当前节点已连接的上游
for (const n of state.nodes) {
  if (!n.prompt || n.prompt.includes('@[')) continue
  for (const e of state.edges.filter((x) => x.to === n.id)) {
    const u = state.nodes.find((m) => m.id === e.from)
    if (u) n.prompt = n.prompt.split(`@${u.title}`).join(`@[${u.id}]`)
  }
}
export const ui = { sel: new Set(), edgeSel: null, view: { x: 0, y: 0, k: 1 }, tool: 'select', running: new Map() }

const past = []
const future = []
const snap = () => JSON.stringify({ nodes: state.nodes, edges: state.edges })
// 修改前调用：记录可撤销快照
export function checkpoint() {
  past.push(snap())
  if (past.length > 80) past.shift()
  future.length = 0
}
function restore(s) {
  const v = JSON.parse(s)
  state.nodes = v.nodes
  state.edges = v.edges
  for (const id of [...ui.sel]) if (!byId(id)) ui.sel.delete(id)
}
export function undo() { if (!past.length) return false; future.push(snap()); restore(past.pop()); return true }
export function redo() { if (!future.length) return false; past.push(snap()); restore(future.pop()); return true }
export const canUndo = () => past.length > 0
export const canRedo = () => future.length > 0

let saveTimer
export function save() {
  clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    // 「正在看的版本」是临时浏览状态，不落盘；刷新后回到主版本
  const clean = { ...state, nodes: state.nodes.map((n) => ({ ...n, running: undefined, vi: undefined })) }
    localStorage.setItem(KEY, JSON.stringify(clean))
  }, 200)
}
// 视口是个人浏览偏好，和画布内容分开存；平移时高频调用，合并成一次写入
const VIEW_KEY = `${KEY}:view`
let viewTimer
export function saveView(v) {
  clearTimeout(viewTimer)
  viewTimer = setTimeout(() => {
    try { localStorage.setItem(VIEW_KEY, JSON.stringify(v)) } catch { /* 视口偏好可丢 */ }
  }, 250)
}
export function loadView() {
  try {
    const v = JSON.parse(localStorage.getItem(VIEW_KEY))
    if (v && [v.x, v.y, v.k].every(Number.isFinite) && v.k >= 0.1 && v.k <= 4) return v
  } catch { /* 损坏时适应全部 */ }
  return null
}
export function reset() { localStorage.removeItem(KEY); localStorage.removeItem(VIEW_KEY); location.reload() }

export const byId = (id) => state.nodes.find((n) => n.id === id)
export const upstream = (id) => state.edges.filter((e) => e.to === id).map((e) => byId(e.from)).filter(Boolean)
// 版本：vi 只是「正在看哪一张」；main 是主版本，下游参考只认它。
// main 为 null 表示自动，始终跟随最新生成；手动设过之后固定，新生成不再抢走。
export const mainIndex = (n) => (n.versions?.length ? n.main ?? n.versions.length - 1 : -1)
export const mainSrc = (n) => n.versions?.[mainIndex(n)] ?? null

// ---------- 引用 ----------
// 提示词里用 @[节点 id] 表示「在这里指代某个参考」。连线决定参考集合，@ 只是在句子里点名其中一个；
// 连上却不 @ 的参考照样发给模型，作为整体参考。
export const MENTION = /@\[([\w-]+)\]/g
// 编号按连线先后，图片、视频各自计数，和发给模型的附件顺序一致；文本参考用标题
export function refLabels(id) {
  const count = { image: 0, video: 0 }
  return new Map(upstream(id).map((u) => [u.id, u.type === 'text' ? u.title : `${u.type === 'video' ? '视频' : '图'}${++count[u.type]}`]))
}
const plain = (html) => {
  const el = document.createElement('div')
  el.innerHTML = html ?? ''
  return el.innerText.replace(/\s+/g, ' ').trim()
}
// 发给模型的内容：token 换成 [图N] 这类占位词，附件按编号顺序排好，再附一段规则说明占位词的含义。
// 已断开的引用直接去掉；文本参考被 @ 时原文展开到句子里，没被 @ 时作为补充描述。
export function compilePrompt(n) {
  const ups = upstream(n.id)
  const labels = refLabels(n.id)
  const mentioned = new Set()
  const prompt = (n.prompt ?? '').replace(MENTION, (_, id) => {
    const u = ups.find((m) => m.id === id)
    if (!u) return ''
    mentioned.add(id)
    return u.type === 'text' ? plain(u.text) : `[${labels.get(id)}]`
  }).replace(/[ \t]+/g, ' ').trim()
  const attachments = ups.filter((u) => u.type !== 'text' && mainSrc(u))
    .map((u) => ({ label: labels.get(u.id), title: u.title, src: mainSrc(u), mentioned: mentioned.has(u.id) }))
  const context = ups.filter((u) => u.type === 'text' && !mentioned.has(u.id) && plain(u.text)).map((u) => plain(u.text))
  const rules = attachments.length
    ? `参考附件按顺序依次对应 ${attachments.map((a) => `[${a.label}]`).join('、')}。提示词中出现这些占位词时，指的就是对应附件，按句子描述的方式使用它；未被提及的附件作为整体风格与内容参考。`
    : ''
  return { prompt, attachments, context, rules }
}
export const isPinned = (n) => n.main != null
export const cover = (n) => n.versions?.[n.vi ?? mainIndex(n)] ?? mainSrc(n)
// 手动指定主版本：即使选的正好是最新一张也固定下来，之后的新生成不会覆盖
export function setMain(n, i) {
  n.main = i
  n.vi = i
}
export function followLatest(n) {
  n.main = null
  n.vi = mainIndex(n)
}
export const modelOf = (n) => models[n.type]?.find((m) => m.key === n.model) ?? models[n.type]?.[0]
// ---------- 分组：坐标一律是世界坐标，parent 只表达归属；组框由子节点自动撑开 ----------
const PAD = 24, HEAD = 40
export const isGroup = (n) => n?.type === 'group'
export function descendants(ids) {
  const out = new Set(ids)
  let grew = true
  while (grew) {
    grew = false
    for (const n of state.nodes) if (n.parent && out.has(n.parent) && !out.has(n.id)) { out.add(n.id); grew = true }
  }
  return out
}
// 选中集合里去掉「祖先也被选中」的节点，避免拖动时重复位移
export function roots(ids) {
  const chosen = new Set(ids)
  return [...chosen].filter((id) => {
    for (let p = byId(id)?.parent; p; p = byId(p)?.parent) if (chosen.has(p)) return false
    return true
  })
}
const depth = (n) => { let d = 0; for (let p = n.parent; p; p = byId(p)?.parent) d++; return d }
export function fitGroups() {
  // 子节点全部移除后，空组没有意义，连带清掉（可能逐层向上）
  let pruned = true
  while (pruned) {
    const empty = new Set(state.nodes.filter((g) => isGroup(g) && !state.nodes.some((n) => n.parent === g.id)).map((g) => g.id))
    pruned = empty.size > 0
    state.nodes = state.nodes.filter((n) => !empty.has(n.id))
  }
  const groups = state.nodes.filter(isGroup).sort((a, b) => depth(b) - depth(a))
  for (const g of groups) {
    const kids = state.nodes.filter((n) => n.parent === g.id)
    if (!kids.length) continue
    const x0 = Math.min(...kids.map((n) => n.x)), y0 = Math.min(...kids.map((n) => n.y - (isGroup(n) ? 0 : 26)))
    const x1 = Math.max(...kids.map((n) => n.x + n.w)), y1 = Math.max(...kids.map((n) => n.y + n.h))
    Object.assign(g, { x: x0 - PAD, y: y0 - HEAD, w: x1 - x0 + PAD * 2, h: y1 - y0 + HEAD + PAD })
  }
}
export function group(ids) {
  const top = roots(ids).map(byId).filter(Boolean)
  if (top.length < 2) return null
  const parents = new Set(top.map((n) => n.parent ?? null))
  const g = { id: uid('g'), type: 'group', title: '未命名分组', parent: parents.size === 1 ? [...parents][0] : null, x: 0, y: 0, w: 0, h: 0 }
  top.forEach((n) => (n.parent = g.id))
  // 组放在最前，保证渲染在子节点下层
  state.nodes.unshift(g)
  fitGroups()
  return g
}
export function ungroup(ids) {
  const released = []
  for (const g of state.nodes.filter((n) => ids.has(n.id) && isGroup(n))) {
    for (const n of state.nodes) if (n.parent === g.id) { n.parent = g.parent ?? null; released.push(n.id) }
  }
  state.nodes = state.nodes.filter((n) => !(ids.has(n.id) && isGroup(n)))
  fitGroups()
  return released
}
export function removeNodes(ids) {
  const all = descendants(ids)
  state.nodes = state.nodes.filter((n) => !all.has(n.id))
  state.edges = state.edges.filter((e) => !all.has(e.from) && !all.has(e.to))
  fitGroups()
}
export function connect(from, to) {
  if (from === to || state.edges.some((e) => e.from === from && e.to === to)) return false
  if (isGroup(byId(from)) || isGroup(byId(to))) return false
  if (byId(to)?.type === 'text') return false
  state.edges.push({ id: uid('e'), from, to })
  return true
}
export function makeNode(type, x, y, extra = {}) {
  const ratio = DEFAULT_RATIO
  const { w, h } = sizeFor(type, ratio)
  const titles = { text: '文本', image: '图片', video: '视频' }
  const n = { id: uid('n'), type, x, y, w, h, title: titles[type], ...extra }
  if (type === 'text') n.text = n.text ?? ''
  else Object.assign(n, { versions: [], main: null, prompt: '', model: models[type][0].key, ratio, count: 1 }, extra)
  state.nodes.push(n)
  return n
}
