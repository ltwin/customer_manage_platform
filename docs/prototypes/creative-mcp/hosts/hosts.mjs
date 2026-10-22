// 宿主对照：同一个剧本在 Claude、ChatGPT、Claude Code、Codex CLI 四个模拟宿主里同步推进。
// 四个宿主都常驻在页面里，切换只是换显示，卡片和任务的状态不会丢。
import { HOSTS, ORIGIN, page } from '../shared/data.mjs'
import { TITLES, READ_ONLY } from '../shared/results.mjs'
import { MockServer } from './mock-server.mjs'
import { mountWidget, hostContext } from './host-bridge.mjs'
import { STEPS } from './scenario.mjs'

const $ = (sel, root = document) => root.querySelector(sel)
const esc = (s) => String(s ?? '').replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c])
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const SERVER = { gui: '创意空间', claude_code: 'creative', codex: 'creative' }

const params = new URLSearchParams(location.search)
const ui = {
  host: HOSTS.some((h) => h.id === params.get('host')) ? params.get('host') : 'claude',
  theme: params.get('theme') === 'dark' ? 'dark' : 'light',
  step: 1, // 已展开到第几步
  annotate: params.get('annotate') === '1',
}

// ---------- Markdown（只支持剧本用到的：链接、粗体、行内代码、列表、段落） ----------
function inline(s, tui) {
  return esc(s)
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, label, url) =>
      tui ? `<a href="${url}" class="t-link">${label}</a><span class="t-url"> ${url}</span>` : `<a href="${url}">${label}</a>`,
    )
    .replace(/\*\*([^*]+)\*\*/g, '<b>$1</b>')
    .replace(/`([^`]+)`/g, '<code>$1</code>')
}
function md(text, tui = false) {
  return text
    .split(/\n{2,}/)
    .map((block) => {
      const lines = block.split('\n')
      if (lines.every((l) => /^(\d+\.|-) /.test(l))) {
        const tag = /^\d/.test(lines[0]) ? 'ol' : 'ul'
        return `<${tag}>${lines.map((l) => `<li>${inline(l.replace(/^(\d+\.|-) /, ''), tui)}</li>`).join('')}</${tag}>`
      }
      return `<p>${lines.map((l) => inline(l, tui)).join('<br>')}</p>`
    })
    .join('')
}

// ---------- 宿主实例 ----------
const hosts = HOSTS.map((h) => ({ ...h, pane: null, thread: null }))
const byId = (id) => hosts.find((h) => h.id === id)

function reset(h) {
  h.widgets?.forEach((w) => w.destroy())
  h.server = new MockServer(h.name)
  h.widgets = []
  h.ctx = { taskIds: [], sceneTaskIds: [] }
  h.log = []
  h.done = 0 // 已执行完的步数
  h.queue = Promise.resolve()
  h.gen = (h.gen ?? 0) + 1 // 重置后作废还在跑的旧队列
  h.widgetAllowed = false
  h.pane?.remove()
  h.pane = buildPane(h)
  $('#stage').append(h.pane)
  h.thread = $('.thread', h.pane)
  // 对话停在底部时，新内容和变高的卡片都跟着滚到底；创作者往上翻了就不打扰
  h.pinned = true
  h.thread.addEventListener('scroll', () => {
    const t = h.thread
    h.pinned = t.scrollHeight - t.scrollTop - t.clientHeight < 80
  })
  h.pane.hidden = h.id !== ui.host
}

function buildPane(h) {
  const el = document.createElement('section')
  el.className = `pane ${h.kind} host-${h.id} theme-${ui.theme}`
  el.setAttribute('aria-label', `${h.name} 模拟宿主`)
  if (h.kind === 'gui') {
    const chatgpt = h.id === 'chatgpt'
    el.innerHTML = `
      <aside class="g-side" data-src="host"><span class="g-logo">${chatgpt ? 'ChatGPT' : 'Claude'}</span><span class="g-new">＋ 新对话</span></aside>
      <div class="g-main">
        <div class="thread"></div>
        <div class="fs-layer" hidden><header class="fs-head" data-src="host"><span>创意空间</span><button class="fs-close" aria-label="关闭全屏">✕</button></header><div class="fs-body"></div></div>
        <div class="composer" data-src="host"><span>${chatgpt ? '询问任何问题' : '回复 Claude…'}</span><span class="c-tools">${chatgpt ? '＋ · 开发者模式 · 创意空间' : '＋ · 连接器：创意空间'}</span></div>
      </div>`
  } else {
    const cc = h.id === 'claude_code'
    el.innerHTML = `
      <div class="t-bar" data-src="host"><i></i><i></i><i></i><span>~/shoots — ${cc ? 'claude' : 'codex'}</span></div>
      <div class="thread term"></div>
      <div class="t-input" data-src="host">${cc ? '<span class="t-box">&gt; <span class="t-cursor"></span></span><span class="t-hint">? for shortcuts</span>' : '<span class="t-box">› <span class="t-cursor"></span></span><span class="t-hint">⏎ send · gpt-5.5 · creative 已连接</span>'}</div>`
  }
  el.addEventListener('click', (e) => onPaneClick(h, e))
  return el
}

// ---------- 链接：生产地址换成本地原型页面 ----------
function toLocal(h, url) {
  const u = new URL(url, ORIGIN)
  if (u.origin === ORIGIN && u.pathname.startsWith('/creative/canvases/')) {
    if (u.searchParams.get('tasks')) h.server.openPage()
    u.searchParams.set('host', h.id)
    return page(`canvas.html${u.search}`)
  }
  return url
}
function onPaneClick(h, e) {
  const a = e.target.closest('a[href]')
  if (a) {
    e.preventDefault()
    window.open(toLocal(h, a.getAttribute('href')), '_blank')
    return
  }
  const more = e.target.closest('[data-more]')
  if (more) more.closest('.t-result, .g-tool').classList.toggle('open')
  if (e.target.closest('.fs-close')) exitFull(h)
}

// ---------- 事件渲染 ----------
const add = (h, html) => {
  h.thread.insertAdjacentHTML('beforeend', html)
  const node = h.thread.lastElementChild
  h.thread.scrollTop = h.thread.scrollHeight
  return node
}
const argStr = (args, json) =>
  json ? JSON.stringify(args) : Object.entries(args).map(([k, v]) => `${k}: ${JSON.stringify(v)}`).join(', ')
const textOf = (r) => (r.content ?? []).filter((c) => c.type === 'text').map((c) => c.text).join('\n')
const imagesOf = (r) => (r.content ?? []).filter((c) => c.type === 'image')

function userMsg(h, text) {
  if (h.kind === 'gui') add(h, `<div class="g-user" data-src="creator"><p>${esc(text)}</p></div>`)
  else add(h, `<div class="t-user" data-src="creator">${h.id === 'codex' ? '›' : '&gt;'} ${esc(text)}</div>`)
}
function assistantMsg(h, text) {
  if (h.kind === 'gui') add(h, `<div class="g-bot" data-src="model">${md(text)}</div>`)
  else add(h, `<div class="t-bot" data-src="model"><span class="t-mark">${h.id === 'codex' ? '•' : '⏺'}</span><div>${md(text, true)}</div></div>`)
}
function hintMsg(h, text) {
  add(h, `<div class="hint" data-src="proto">原型提示：${esc(text)}</div>`)
}

function toolRow(h, name, args, r) {
  const text = textOf(r)
  const lines = text.split('\n')
  const imgs = imagesOf(r)
  if (h.kind === 'gui') {
    const label = h.id === 'chatgpt' ? '已调用' : ''
    return add(
      h,
      `<div class="g-tool" data-src="host"><button data-more><span class="chev">›</span>${label}<span class="g-app">创意空间</span><span data-src="ours">${TITLES[name]}</span></button>
        <div class="g-detail"><div class="g-k">请求</div><pre data-src="model">${esc(JSON.stringify(args, null, 2))}</pre><div class="g-k">结果（模型读到的 content）</div><pre data-src="ours">${esc(text)}</pre>${imgs.length ? `<div class="g-k">另附 ${imgs.length} 张图片给模型</div>` : ''}</div></div>`,
    )
  }
  const shown = h.id === 'claude_code' ? 3 : 4
  const head =
    h.id === 'claude_code'
      ? `<span class="t-mark ok">⏺</span><b>${SERVER.claude_code} - ${name}</b> <span class="t-dim">(MCP)</span><span data-src="model">(${esc(argStr(args))})</span>`
      : `<span class="t-mark ok">•</span><b>Called</b> <span data-src="model">${SERVER.codex}.${name}(${esc(argStr(args, true))})</span>`
  const corner = h.id === 'claude_code' ? '⎿' : '└'
  const rest = lines.length - shown
  const imgLine = imgs.length
    ? h.id === 'codex'
      ? `<div class="t-imgs">${imgs.map((i) => `<img src="${i.url}" alt="" style="aspect-ratio:${i.w}/${i.h}">`).join('')}<span class="t-dim">终端支持时显示图片（待核实）</span></div>`
      : `<div class="t-dim">[图片 ×${imgs.length}：终端里看不到，模型能看到（待核实 U9）]</div>`
    : ''
  return add(
    h,
    `<div class="t-call"><div class="t-head" data-src="host">${head}</div>
      <div class="t-result" data-src="ours"><span class="t-corner">${corner}</span><div>
        <pre class="t-first">${esc(lines.slice(0, shown).join('\n'))}</pre>
        ${rest > 0 ? `<pre class="t-rest">${esc(lines.slice(shown).join('\n'))}</pre><button class="t-more" data-more data-src="host">… +${rest} lines (${h.id === 'claude_code' ? 'ctrl+o to expand' : '点开查看'})</button>` : ''}
        ${imgLine}</div></div></div>`,
  )
}

function permission(h, name, args) {
  const title = TITLES[name]
  let html
  if (h.id === 'claude')
    html = `<div class="g-perm" data-src="host"><p>Claude 想使用<b>创意空间</b>的「<span data-src="ours">${title}</span>」</p><div><span class="pb">拒绝</span><span class="pb">允许一次</span><span class="pb on">始终允许</span></div></div>`
  else if (h.id === 'chatgpt')
    html = `<div class="g-perm" data-src="host"><p>创意空间 · <span data-src="ours">${title}</span>：这个操作会修改你的数据，要继续吗？</p><div><span class="pb">取消</span><span class="pb on">确认</span></div></div>`
  else if (h.id === 'claude_code')
    html = `<div class="t-perm" data-src="host"><div class="t-perm-box"><b>Tool use</b><div>  creative - ${name}(${esc(argStr(args))}) (MCP)</div><div class="t-dim" data-src="ours">  ${title}</div><div>Do you want to proceed?</div><div class="sel">❯ 1. Yes</div><div>  2. Yes, and don't ask again for creative - ${name} commands in ~/shoots</div><div>  3. No, and tell Claude what to do differently (esc)</div></div></div>`
  else
    html = `<div class="t-perm" data-src="host"><div class="t-perm-box"><b>Allow Codex to run this MCP tool?</b><div>  creative.${name} — <span data-src="ours">${title}</span></div><div class="sel">▌ Yes, allow once</div><div>  Yes, always allow creative.${name}</div><div>  No, tell Codex what to do differently</div></div></div>`
  add(h, html)
}

// Claude 第一次显示卡片前会问一次
async function allowWidget(h) {
  if (h.id !== 'claude' || h.widgetAllowed) return
  add(h, `<div class="g-perm" data-src="host"><p><b>创意空间</b>想在对话里显示交互界面</p><div><span class="pb">不允许</span><span class="pb">允许</span><span class="pb on">始终允许</span></div></div>`)
  await sleep(700)
  h.widgetAllowed = true
}

function trackTasks(h, r) {
  for (const t of r.structuredContent?.tasks ?? []) {
    const list = t.kind === 'generate' ? h.ctx.taskIds : h.ctx.sceneTaskIds
    if (!list.includes(t.id)) list.push(t.id)
  }
}

function logEntry(h, kind, text) {
  const last = h.log.at(-1)
  if (kind === 'call' && last?.kind === 'call' && last.text === text) last.n++
  else h.log.push({ kind, text, n: 1 })
  if (h.id === ui.host) renderNotes()
}

function widget(h, resource, args, r) {
  const box = add(h, `<div class="widget ${h.id === 'chatgpt' ? 'chatgpt' : ''}" data-src="ours">${h.id === 'chatgpt' ? '<div class="w-app" data-src="host"><span class="w-icon">创</span>创意空间</div>' : ''}</div>`)
  let mode = 'inline'
  const w = mountWidget(box, {
    resource,
    args,
    result: r,
    server: h.server,
    hostName: h.name,
    context: () => hostContext(h.id, ui.theme, mode),
    onToolCall: (name, a, res) => {
      trackTasks(h, res)
      logEntry(h, 'call', `卡片调用 ${name}`)
    },
    onModelContext: (text) => logEntry(h, 'ctx', text),
    // 卡片变高时，如果对话本来就停在底部，跟着滚到底
    onResize: () => {
      if (h.pinned) h.thread.scrollTop = h.thread.scrollHeight
    },
    onOpenLink: (url) => window.open(toLocal(h, url), '_blank'),
    onDisplayMode: (m) => {
      if (m !== 'fullscreen') return 'inline'
      enterFull(h, box, w)
      mode = 'fullscreen'
      return 'fullscreen'
    },
  })
  w.box = box
  w.setMode = (m) => (mode = m)
  h.widgets.push(w)
}

// 全屏：卡片的 iframe 不能挪动（会重新加载），只把外框盖到对话区上面；宿主输入框仍在最下面
function enterFull(h, box, w) {
  exitFull(h)
  h.full = w
  box.classList.add('full')
  $('.fs-layer', h.pane).hidden = false
  w.setContext({ displayMode: 'fullscreen', safeAreaInsets: { top: 0, right: 0, bottom: 112, left: 0 } })
}
function exitFull(h) {
  const w = h.full
  if (!w) return
  h.full = null
  w.box.classList.remove('full')
  $('.fs-layer', h.pane).hidden = true
  w.setMode('inline')
  w.setContext({ displayMode: 'inline', safeAreaInsets: { top: 0, right: 0, bottom: 0, left: 0 } })
}

async function runEvent(h, ev, gen) {
  if (ev.only && ev.only !== h.kind) return
  if (gen !== h.gen) return
  await sleep(220)
  if (ev.type === 'user') return userMsg(h, ev.text)
  if (ev.type === 'assistant') return assistantMsg(h, ev.text)
  if (ev.type === 'hint') return hintMsg(h, ev.text)
  if (ev.type === 'open-page') return h.server.openPage()
  if (ev.type === 'permission') {
    if (READ_ONLY.has(ev.name)) return
    permission(h, ev.name, ev.args)
    return sleep(500)
  }
  if (ev.type === 'tool') {
    for (;;) {
      const args = typeof ev.args === 'function' ? ev.args(h.ctx) : ev.args
      const r = await h.server.call(ev.name, args)
      if (gen !== h.gen) return
      trackTasks(h, r)
      toolRow(h, ev.name, args, r)
      if (ev.ui && h.kind === 'gui') {
        await allowWidget(h)
        widget(h, ev.ui, args, r)
      }
      const open = (r.structuredContent?.tasks ?? []).some((t) => t.status !== 'succeeded' && t.status !== 'failed')
      if (!ev.repeat || !open) return
      await sleep(1200)
    }
  }
}

function runTo(h, n) {
  const gen = h.gen
  for (let i = h.done; i < n; i++) {
    h.queue = h.queue.then(async () => {
      for (const ev of STEPS[i].events) await runEvent(h, ev, gen)
    })
  }
  h.done = Math.max(h.done, n)
}

function goto(n) {
  ui.step = Math.min(Math.max(n, 1), STEPS.length)
  for (const h of hosts) {
    if (ui.step < h.done) reset(h)
    runTo(h, ui.step)
  }
  renderNav()
  renderNotes()
}

// ---------- 外框：宿主切换、主题、标注、步骤、右侧说明 ----------
function renderTabs() {
  $('#tabs').innerHTML = hosts
    .map((h) => `<button role="tab" aria-selected="${h.id === ui.host}" data-host="${h.id}"><span class="tab-k">${h.kind === 'gui' ? '图形界面' : '终端'}</span>${h.name}</button>`)
    .join('')
  $('#theme').hidden = byId(ui.host).kind !== 'gui'
  $('#theme').querySelectorAll('button').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.theme === ui.theme)))
}
function renderNav() {
  $('#steps').innerHTML = STEPS.map(
    (s, i) => `<button data-step="${i + 1}" class="${i + 1 <= ui.step ? 'on' : ''}" aria-current="${i + 1 === ui.step}"><span>${i + 1}</span>${s.label}</button>`,
  ).join('')
  $('#prev').disabled = ui.step <= 1
  $('#next').disabled = ui.step >= STEPS.length
}
function renderNotes() {
  const s = STEPS[ui.step - 1]
  const h = byId(ui.host)
  const log = h.log.length
    ? h.log.map((l) => `<li class="${l.kind}">${l.kind === 'ctx' ? '<b>卡片 → 外部助手</b>' : ''}${esc(l.text)}${l.n > 1 ? ` ×${l.n}` : ''}</li>`).join('')
    : `<li class="empty">${h.kind === 'gui' ? '卡片还没有动作' : '终端宿主没有卡片，全靠工具结果文字'}</li>`
  $('#notes').innerHTML = `
    <p class="n-step">第 ${ui.step} / ${STEPS.length} 步 · ${s.label}</p>
    <h2>这一步看什么</h2><p>${esc(s.look)}</p>
    <h2>我们能设计的</h2><ul>${s.ours.map((o) => `<li>${esc(o)}</li>`).join('')}</ul>
    <h2>卡片动作日志 · ${esc(h.name)}</h2><ul class="log">${log}</ul>`
}

function applyTheme() {
  for (const h of hosts) {
    h.pane.classList.toggle('theme-dark', ui.theme === 'dark')
    h.pane.classList.toggle('theme-light', ui.theme === 'light')
    for (const w of h.widgets) w.setContext({ theme: ui.theme, styles: hostContext(h.id, ui.theme).styles })
  }
}
function syncUrl() {
  const q = new URLSearchParams({ host: ui.host, theme: ui.theme })
  if (ui.annotate) q.set('annotate', '1')
  history.replaceState(null, '', `?${q}`)
}

$('#tabs').addEventListener('click', (e) => {
  const b = e.target.closest('[data-host]')
  if (!b) return
  ui.host = b.dataset.host
  hosts.forEach((h) => (h.pane.hidden = h.id !== ui.host))
  renderTabs()
  renderNotes()
  syncUrl()
})
$('#theme').addEventListener('click', (e) => {
  const b = e.target.closest('[data-theme]')
  if (!b) return
  ui.theme = b.dataset.theme
  applyTheme()
  renderTabs()
  syncUrl()
})
$('#annotate').addEventListener('change', (e) => {
  ui.annotate = e.target.checked
  document.body.classList.toggle('annotate', ui.annotate)
  syncUrl()
})
$('#steps').addEventListener('click', (e) => {
  const b = e.target.closest('[data-step]')
  if (b) goto(Number(b.dataset.step))
})
$('#prev').addEventListener('click', () => goto(ui.step - 1))
$('#next').addEventListener('click', () => goto(ui.step + 1))
$('#replay').addEventListener('click', () => {
  hosts.forEach(reset)
  goto(1)
})
addEventListener('keydown', (e) => {
  if (e.target.closest('input, textarea')) return
  if (e.key === 'ArrowRight') goto(ui.step + 1)
  if (e.key === 'ArrowLeft') goto(ui.step - 1)
})

fetch(new URL('../instructions.md', import.meta.url))
  .then((r) => r.text())
  .then((t) => ($('#instructions pre').textContent = t))
  .catch(() => ($('#instructions pre').textContent = '读取 instructions.md 失败'))

$('#annotate').checked = ui.annotate
document.body.classList.toggle('annotate', ui.annotate)
hosts.forEach(reset)
renderTabs()
goto(1)
