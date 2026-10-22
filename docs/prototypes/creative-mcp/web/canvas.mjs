// W-4 「外部助手已连接」指示、W-5 链接落地（聚焦节点、等页面执行的任务）、W-6 场景历史署名。
// 外部助手给的链接形如 canvas.html?focus=shot-2&tasks=1&host=codex（生产地址由宿主对照页换成这里）。
import { HOSTS, NODES } from '../shared/data.mjs'
import { mountStates, toast } from '../shared/proto.mjs'

const $ = (s) => document.querySelector(s)
const esc = (s) => String(s ?? '').replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c])
const q = new URLSearchParams(location.search)
const host = HOSTS.find((h) => h.id === q.get('host')) ?? HOSTS.find((h) => h.id === 'codex')
const H = host.name.replace(' CLI', '') // 署名用宿主名：Codex、Claude Code、Claude、ChatGPT
const RENDER_MS = 4000

// 链接带了参数却没指定原型状态时，按链接决定
if (!q.get('state') && (q.get('tasks') || q.get('focus'))) {
  q.set('state', q.get('tasks') ? 'tasks' : 'handoff')
  history.replaceState(null, '', `?${q}`)
}

// ---------- 画布（静态示意） ----------
const EDGES = [['ref-1', 'shot-2'], ['ref-2', 'shot-2'], ['shot-2', 'scene-1']]
const box = (n) => ({ x: n.x, y: n.y, w: n.w, h: Math.round(n.w / n.ratio) })
const node = (id) => NODES.find((n) => n.id === id)

function drawWorld() {
  const refs = NODES.filter((n) => n.group === 'refs').map(box)
  const g = {
    x: Math.min(...refs.map((r) => r.x)) - 16,
    y: Math.min(...refs.map((r) => r.y)) - 56, // 留出组标题和第一个节点标题的位置
    r: Math.max(...refs.map((r) => r.x + r.w)) + 16,
    b: Math.max(...refs.map((r) => r.y + r.h)) + 16,
  }
  const edges = EDGES.map(([a, b]) => {
    const A = box(node(a))
    const B = box(node(b))
    const x1 = A.x + A.w, y1 = A.y + A.h / 2, x2 = B.x, y2 = B.y + B.h / 2
    const dx = Math.max(40, (x2 - x1) / 2)
    return `<path d="M${x1},${y1} C${x1 + dx},${y1} ${x2 - dx},${y2} ${x2},${y2}"/>`
  }).join('')
  $('#world').innerHTML = `
    <div class="group" style="left:${g.x}px;top:${g.y}px;width:${g.r - g.x}px;height:${g.b - g.y}px"><span class="nt">参考素材</span></div>
    <svg class="edge" width="1" height="1">${edges}</svg>
    ${NODES.map((n) => {
      const b = box(n)
      return `<div class="node${n.scene ? ' scene' : ''}" data-id="${n.id}" style="left:${b.x}px;top:${b.y}px;width:${b.w}px">
        <span class="nt">${esc(n.title)}</span><div class="card" style="height:${b.h}px;position:relative"><img src="${n.img}" alt="${esc(n.title)}"></div></div>`
    }).join('')}`
}

// 视口：整张画布放进可见区域（右侧抽屉打开时让出位置）。示例画布不大，聚焦只做高亮，不平移。
function view() {
  const all = NODES.map(box)
  const minX = Math.min(...all.map((b) => b.x)) - 20, minY = Math.min(...all.map((b) => b.y)) - 40
  const maxX = Math.max(...all.map((b) => b.x + b.w)) + 20, maxY = Math.max(...all.map((b) => b.y + b.h)) + 60
  const right = $('#drawer').hidden ? 0 : 364
  const W = innerWidth - right, Hh = innerHeight
  const top = 72, bottom = 64
  const k = Math.min(1, (W - 48) / (maxX - minX), (Hh - top - bottom) / (maxY - minY))
  const x = (W - (maxX - minX) * k) / 2 - minX * k
  const y = top + (Hh - top - bottom - (maxY - minY) * k) / 2 - minY * k
  $('#world').style.transform = `translate(${x}px, ${y}px) scale(${k})`
}

const el = (id) => $(`.node[data-id="${id}"]`)
function clearMarks() {
  document.querySelectorAll('.node').forEach((n) => n.classList.remove('focus', 'xa-edit', 'rendering'))
  document.querySelectorAll('.xa-tag, .focus-tag').forEach((t) => t.remove())
}
function focusNode(id, label) {
  const n = el(id)
  if (!n) return
  n.classList.remove('focus')
  void n.offsetWidth // 重新触发闪烁动画
  n.classList.add('focus')
  n.querySelector('.focus-tag')?.remove()
  if (label) n.insertAdjacentHTML('beforeend', `<span class="focus-tag">${esc(label)}</span>`)
  view()
}
function markEditing(id, on) {
  const n = el(id)
  if (!n) return
  n.classList.toggle('xa-edit', on)
  n.querySelector('.xa-tag')?.remove()
  if (on) n.insertAdjacentHTML('beforeend', `<span class="xa-tag">${esc(H)}</span>`)
}

// ---------- W-4 顶栏指示 ----------
const OPS = [
  { text: '改了「镜头 02」的提示词', node: 'shot-2', time: '3 分钟前' },
  { text: '生成 2 张 · 扣 40 点', node: 'shot-2', time: '2 分钟前' },
  { text: '修改场景「海边礁石」', node: 'scene-1', time: '1 分钟前' },
]
function pill(state) {
  const b = $('#xa')
  b.hidden = state === 'none'
  if (state === 'none') return
  b.innerHTML = state === 'busy' ? `<i class="dot run"></i><b>${esc(H)}</b>正在操作` : `<i class="dot on"></i><b>${esc(H)}</b>已连接`
  b.setAttribute('aria-label', state === 'busy' ? `${H} 正在操作这张画布` : `${H} 已连接，最近 5 分钟内调用过`)
}
function renderPop() {
  $('#pop').innerHTML = `
    <h3><span class="mark">${esc(host.mark)}</span><span>${esc(H)}<small>外部助手 · 最近 5 分钟内调用过</small></span></h3>
    <ul>${OPS.map((o) => `<li><button type="button" data-node="${o.node}">${esc(o.text)}</button><time>${o.time}</time></li>`).join('')}</ul>
    <footer><span>改动署名为「${esc(H)}」，可在历史里回退</span><a class="link" href="connect.html">管理连接</a></footer>`
}
$('#xa').addEventListener('click', () => {
  const pop = $('#pop')
  pop.hidden = !pop.hidden
  $('#xa').setAttribute('aria-expanded', String(!pop.hidden))
  if (!pop.hidden) renderPop()
})
$('#pop').addEventListener('click', (e) => {
  const b = e.target.closest('[data-node]')
  if (b) focusNode(b.dataset.node)
})
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    $('#pop').hidden = true
    $('#xa').setAttribute('aria-expanded', 'false')
  }
})

// ---------- W-5 等页面执行的任务 ----------
let raf = 0
function runTasks() {
  const banner = $('#banner')
  banner.hidden = false
  banner.classList.remove('done')
  el('scene-1')?.classList.add('rendering')
  markEditing('scene-1', true)
  pill('busy')
  const t0 = performance.now()
  const tick = (t) => {
    const p = Math.min(1, (t - t0) / RENDER_MS)
    banner.innerHTML = `<div class="row"><i class="dot run"></i><b>正在为 ${esc(H)} 执行场景任务</b><span>回看 · 主机位</span></div>
      <div class="bar"><i style="width:${Math.round(p * 100)}%"></i></div><small>任务由这个页面渲染，完成前请保持页面打开</small>`
    if (p < 1) return (raf = requestAnimationFrame(tick))
    banner.classList.add('done')
    banner.innerHTML = `<div class="row"><i class="dot on"></i><b>已完成</b><span>回看结果已交回 ${esc(H)}</span></div><div class="bar"><i style="width:100%"></i></div>`
    el('scene-1')?.classList.remove('rendering')
    markEditing('scene-1', false)
    pill('idle')
    setTimeout(() => (banner.hidden = true), 5000)
  }
  raf = requestAnimationFrame(tick)
}

// ---------- W-6 场景历史 ----------
const REVS = [
  { who: H, mark: host.mark, ext: true, what: '修改场景：新增礁石 3 块、人物替身，主光改为太阳（高度 4°）', time: '1 分钟前' },
  { who: '你', mark: 'S', what: '移动「镜头 01」机位', time: '6 分钟前' },
  { who: H, mark: host.mark, ext: true, what: '加海面雾 0.15', time: '12 分钟前' },
  { who: '你', mark: 'S', what: '新建场景「海边礁石」', time: '20 分钟前' },
]
function renderDrawer() {
  $('#drawer').innerHTML = `
    <header><h2>场景历史 · 海边礁石</h2><p>外部助手的修订署名为宿主名</p></header>
    <ol class="revs">${REVS.map(
      (r, i) => `<li tabindex="0" data-i="${i}"><span class="mark">${esc(r.mark)}</span>
        <div class="who"><b>${esc(r.who)}</b>${r.ext ? '<em>外部助手</em>' : ''}<time>${r.time}</time></div>
        <div class="what">${esc(r.what)}</div>
        <div class="acts"><button type="button" class="btn sm" data-preview="${i}">预览</button>${i ? `<button type="button" class="btn sm" data-restore="${i}">恢复到这一版</button>` : ''}</div></li>`,
    ).join('')}</ol>
    <footer>⌘Z 只撤销你自己的改动；外部助手的改动在这里预览、恢复。</footer>`
}
$('#drawer').addEventListener('click', (e) => {
  const p = e.target.closest('[data-preview]')
  if (p) {
    document.querySelectorAll('.revs li').forEach((li) => li.setAttribute('aria-current', String(li.dataset.i === p.dataset.preview)))
    toast('正在预览这一版，点「恢复到这一版」才会生效')
  }
  const r = e.target.closest('[data-restore]')
  if (r) toast(`已恢复到「${REVS[r.dataset.restore].what}」之后的版本`, { label: '撤销', run: () => toast('已撤销恢复') })
})
$('#hist').addEventListener('click', () => setDrawer($('#drawer').hidden))
function setDrawer(open) {
  $('#drawer').hidden = !open
  $('#hist').setAttribute('aria-pressed', String(open))
  if (open) renderDrawer()
  view()
}

// ---------- 原型状态 ----------
drawWorld()
addEventListener('resize', () => view())
mountStates({
  options: [
    { id: 'handoff', label: '链接聚焦' },
    { id: 'tasks', label: '等页面执行的任务' },
    { id: 'history', label: '场景历史' },
    { id: 'none', label: '没有外部助手' },
  ],
  onChange: (s) => {
    cancelAnimationFrame(raf)
    clearMarks()
    $('#banner').hidden = true
    $('#pop').hidden = true
    setDrawer(s === 'history')
    if (s === 'none') return pill('none'), view()
    pill('idle')
    if (s === 'handoff') {
      const id = node(q.get('focus')) ? q.get('focus') : 'shot-2'
      focusNode(id, `${H} 给的链接指向这里`)
      markEditing(id, true)
      setTimeout(() => markEditing(id, false), 6000)
    }
    if (s === 'tasks') runTasks()
    if (s === 'history') focusNode('scene-1')
  },
})
