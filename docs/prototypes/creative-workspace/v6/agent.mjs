// 创作助手：一轮回复是一串有序的块——思考、计划、工具调用、提问、正文、本轮改动。
// 工具调用就是对画布的操作：悬停高亮相关节点、点击定位；助手正在处理的节点在画布上带「助手」标记。
import { state, ui, byId, upstream, checkpoint, connect, makeNode, models, photos, mainSrc, isGroup, removeNodes, group } from './store.mjs'
import { on, emit, render, commit, select, fitView, nodeEl } from './canvas.mjs'
import { ic, esc, openMenu, toast, confirmModal } from './ui.mjs'

const $ = (id) => document.getElementById(id)
const panel = $('agent')
const stage = $('stage')
const TYPE_ICON = { text: 'file-text', image: 'image', video: 'video' }
const TOOL_ICON = { read: 'scan', create: 'plus', connect: 'link', generate: 'image', edit: 'pencil' }
const STOP = Symbol('stop')

const thread = [] // { role: 'user', text, ctx } | { role: 'ai', parts, status, t0, ms, followups }
let seq = 0
let cur = null // 当前运行：见 makeRun
let queued = null // 运行中发出的下一条消息
let ctx = [] // 输入框上方的上下文节点；默认跟随画布选中
let ctxManual = false
let mode = 'confirm' // confirm：消耗点数、改动画布前先出计划等确认；auto：直接执行
let consented = false
let tick = null

const secs = (ms) => `${Math.max(1, Math.round(ms / 1000))} 秒`
const thumb = (n) => {
  const src = n && n.type !== 'text' ? mainSrc(n) : null
  return src ? `<img src="${src}" alt="">` : `<span class="ti">${ic(TYPE_ICON[n?.type] ?? 'circle')}</span>`
}
const nodeChip = (id) => {
  const n = byId(id)
  if (!n) return '<span class="nchip gone">已删除</span>'
  return `<button class="nchip" data-act="locate" data-ids="${id}">${thumb(n)}<span>${esc(n.title)}</span></button>`
}

// 极简 markdown：段落、有序/无序列表、**粗体**、`代码`。流式时对已到达的部分反复渲染
function md(src) {
  const inline = (s) => esc(s).replace(/\*\*(.+?)\*\*/g, '<b>$1</b>').replace(/`(.+?)`/g, '<code>$1</code>')
  const out = []
  let list = null
  for (const line of src.split('\n')) {
    const m = line.match(/^(\d+\.|-)\s+(.*)/)
    if (m) {
      const tag = m[1] === '-' ? 'ul' : 'ol'
      if (list !== tag) { if (list) out.push(`</${list}>`); out.push(`<${tag}>`); list = tag }
      out.push(`<li>${inline(m[2])}</li>`)
      continue
    }
    if (list) { out.push(`</${list}>`); list = null }
    if (line.trim()) out.push(`<p>${inline(line)}</p>`)
  }
  if (list) out.push(`</${list}>`)
  return out.join('')
}

// ---------- 画布标记 ----------
const busy = new Set()
function setBusy(ids, v) {
  for (const id of ids) v ? busy.add(id) : busy.delete(id)
  paintMarks()
}
let lit = new Set()
function paintMarks() {
  stage.classList.toggle('agent-focus', [...lit].some(byId))
  for (const n of state.nodes) {
    const el = nodeEl(n.id)
    el?.classList.toggle('agent-busy', busy.has(n.id))
    el?.classList.toggle('agent-hl', lit.has(n.id))
  }
}
// 悬停对话里的工具行、节点 chip 时，画布上对应节点高亮，其余变暗
function highlight(ids) {
  const next = new Set(ids)
  if (next.size === lit.size && [...next].every((id) => lit.has(id))) return
  lit = next
  paintMarks()
}
function locate(ids) {
  const alive = ids.filter(byId)
  if (!alive.length) return toast('节点已被删除')
  select(alive)
  fitView(new Set(alive))
}

// ---------- 渲染：每个块一个带 key 的元素，html 没变就不动 DOM，悬停与输入不被打断 ----------
function reconcile(parent, items) {
  const old = new Map([...parent.children].map((c) => [c.dataset.k, c]))
  let prev = null
  for (const { key, html } of items) {
    let el = old.get(key)
    old.delete(key)
    if (!el) { el = document.createElement('div'); el.dataset.k = key }
    if (el._html !== html) { el.innerHTML = html; el._html = html; alignAnimations(el) }
    const want = prev ? prev.nextSibling : parent.firstChild
    if (el !== want) parent.insertBefore(el, want)
    prev = el
  }
  for (const el of old.values()) el.remove()
}

// 块内容变化会重建 DOM；按当前时间给负延迟，旋转圈和流光文字不会跳回起点
function alignAnimations(el) {
  const t = performance.now()
  el.querySelectorAll('.spin').forEach((x) => (x.style.animationDelay = `-${(t % 800) / 1000}s`))
  el.querySelectorAll('.shimmer').forEach((x) => (x.style.animationDelay = `-${(t % 1800) / 1000}s`))
}

const chev = (open) => `<span class="p-chev${open ? ' open' : ''}">${ic('chevron-right')}</span>`
const spinner = () => '<span class="spin"></span>'
const WORK = new Set(['think', 'tool', 'plan'])

function thinkHTML(p) {
  const last = p.text.split('\n').filter(Boolean).pop() ?? ''
  return `<div class="p-think">
    <button class="p-row" data-act="toggle" data-pid="${p.id}" aria-expanded="${Boolean(p.open)}">
      <span class="p-ico">${ic('lightbulb')}</span>
      <span class="p-title${p.live ? ' shimmer' : ''}">${p.live ? '思考中' : `已思考 ${secs(p.ms)}`}</span>${chev(p.open)}
    </button>
    ${p.open ? `<div class="think-body">${md(p.text)}</div>` : p.live && last ? `<div class="think-peek">${esc(last)}</div>` : ''}</div>`
}

function planHTML(p) {
  const done = p.steps.filter((s) => s.s === 'done').length
  return `<div class="p-plan"><div class="plan-head">${ic('file-check')}<span>计划</span><span class="plan-n">${done}/${p.steps.length}</span></div>
    <ol>${p.steps.map((s) => `<li class="${s.s}"><span class="box">${s.s === 'done' ? ic('check') : s.s === 'doing' ? spinner() : ''}</span><span>${esc(s.t)}</span></li>`).join('')}</ol></div>`
}

function toolHTML(p) {
  const ico = p.status === 'run' ? spinner() : p.status === 'error' ? ic('x') : ic(TOOL_ICON[p.kind] ?? 'circle')
  const meta = p.status === 'run' ? (p.progress != null ? `${p.progress}%` : '') : p.status === 'cancel' ? '已停止' : p.status === 'error' ? '失败' : p.ms ? secs(p.ms) : ''
  const hasDetail = Boolean(p.detail?.length)
  return `<div class="p-tool ${p.status}" data-hl="${(p.ids ?? []).join(',')}">
    <button class="p-row" ${hasDetail ? `data-act="toggle" data-pid="${p.id}" aria-expanded="${Boolean(p.open)}"` : 'tabindex="-1"'}>
      <span class="p-ico">${ico}</span>
      <span class="p-title"><span class="verb">${esc(p.verb)}</span><span class="obj">${esc(p.obj ?? '')}</span></span>
      <span class="p-meta">${meta}</span>${hasDetail ? chev(p.open) : ''}
    </button>
    ${p.progress != null && p.status === 'run' ? `<div class="p-bar"><span style="width:${p.progress}%"></span></div>` : ''}
    ${p.error ? `<div class="p-err">${esc(p.error)}</div>` : ''}
    ${p.result ? `<div class="p-result">${p.result.map((src) => `<button data-act="locate" data-ids="${p.ids.join(',')}"><img src="${src}" alt=""></button>`).join('')}</div>` : ''}
    ${p.open && hasDetail ? `<dl class="p-detail">${p.detail.map(([k, v]) => `<dt>${esc(k)}</dt><dd>${v}</dd>`).join('')}</dl>` : ''}</div>`
}

function askHTML(p) {
  if (p.state !== 'wait') {
    const lines = p.qs.map((q) => `<li><span>${esc(q.short)}</span>${esc(q.answer?.label ?? (q.skipped ? '由助手决定' : '—'))}</li>`).join('')
    const head = p.state === 'cancel' ? '已停止，未回答' : p.qs.every((q) => q.skipped) ? '已跳过，按默认处理' : '已回答'
    return `<div class="p-ask done"><div class="ask-done-head">${ic(p.state === 'cancel' ? 'x' : 'check')}${head}</div><ul>${lines}</ul></div>`
  }
  const q = p.qs[p.at]
  const opts = q.opts.map((o, i) => {
    const on = q.multi ? q.picked?.has(i) : false
    const media = o.img ? `<img src="${o.img}" alt="">` : o.ratio ? `<span class="ratio-box" style="aspect-ratio:${o.ratio.replace(':', '/')}"></span>` : ''
    return `<button class="ask-opt${on ? ' on' : ''}${o.img ? ' has-img' : ''}" data-act="pick" data-pid="${p.id}" data-i="${i}" ${q.multi ? `role="checkbox" aria-checked="${on}"` : ''}>
      ${media}<span class="o-main"><b>${esc(o.label)}</b>${o.desc ? `<span>${esc(o.desc)}</span>` : ''}</span><kbd>${i + 1}</kbd></button>`
  }).join('')
  return `<div class="p-ask">
    <div class="ask-top"><span class="ask-q">${esc(q.q)}</span>${p.qs.length > 1 ? `<span class="ask-step">${p.at + 1}/${p.qs.length}</span>` : ''}</div>
    ${q.hint ? `<div class="ask-hint">${esc(q.hint)}</div>` : ''}
    <div class="ask-opts${q.opts.some((o) => o.img) ? ' grid' : ''}">${opts}</div>
    <div class="ask-foot">
      <input class="ask-other" data-pid="${p.id}" placeholder="其他，直接输入后回车" aria-label="其他回答">
      <button class="btn ghost" data-act="skip" data-pid="${p.id}">跳过</button>
      ${q.multi ? `<button class="btn btn-primary" data-act="ask-ok" data-pid="${p.id}" ${q.picked?.size ? '' : 'disabled'}>确定</button>` : ''}
    </div></div>`
}

function confirmHTML(p) {
  const rows = p.items.map((it) => `<li data-hl="${it.id}">${nodeChip(it.id)}<span class="cf-meta">${esc(it.model)} · ${it.ratio}</span><span class="cf-cost">${it.cost} 点</span></li>`).join('')
  if (p.state !== 'wait')
    return `<div class="p-confirm settled"><div class="cf-head">${ic(p.state === 'ok' ? 'check' : 'x')}${p.state === 'ok' ? `已确认生成 ${p.items.length} 张 · ${p.cost} 点` : '已取消生成'}</div></div>`
  return `<div class="p-confirm">
    <div class="cf-head">${ic('sparkles')}<span>需要确认：生成 ${p.items.length} 张分镜草图</span></div>
    <ul class="cf-list">${rows}</ul>
    <label class="cf-remember"><input type="checkbox" data-act="remember" data-pid="${p.id}" ${p.remember ? 'checked' : ''}>本对话内不再询问，直接生成</label>
    <div class="cf-foot"><span class="cf-bal">余额 ${state.credits.toLocaleString()} 点</span><span class="grow"></span>
      <button class="btn ghost" data-act="cf-no" data-pid="${p.id}">取消</button>
      <button class="btn btn-primary" data-act="cf-ok" data-pid="${p.id}">生成 · ${p.cost} 点</button></div></div>`
}

// 提示词里的 @[id] 显示成节点 chip，和节点输入框里看到的一致
const withChips = (text) => text.split(/(@\[[\w-]+\])/).map((seg) => {
  const m = seg.match(/^@\[([\w-]+)\]$/)
  return m ? nodeChip(m[1]) : esc(seg)
}).join('')

function diffHTML(p) {
  const n = byId(p.nodeId)
  const head = `<div class="df-head">${ic('pencil')}<span>修改提示词</span>${nodeChip(p.nodeId)}</div>`
  if (p.state !== 'wait') return `<div class="p-diff settled">${head}<div class="df-state">${p.state === 'ok' ? '已应用到节点' : '未应用'}</div></div>`
  return `<div class="p-diff" data-hl="${p.nodeId}">${head}
    <div class="df-body">${p.before ? `<div class="df-del">${withChips(p.before)}</div>` : '<div class="df-empty">原提示词为空</div>'}<div class="df-add">${withChips(p.after)}</div></div>
    <div class="cf-foot"><span class="cf-bal">${n ? esc(n.title) : ''} 不会立即生成，应用后可在节点输入框里继续改</span><span class="grow"></span>
      <button class="btn ghost" data-act="df-no" data-pid="${p.id}">放弃</button>
      <button class="btn btn-primary" data-act="df-ok" data-pid="${p.id}">应用</button></div></div>`
}

function changesHTML(p) {
  const alive = [...p.created, ...p.prompts.map((x) => x.id)].filter(byId)
  const media = alive.map(byId).filter((n) => n.type !== 'text' && mainSrc(n))
  const bits = [p.created.length ? `新增 ${p.created.length} 个节点` : '', p.prompts.length ? `改写 ${p.prompts.length} 段提示词` : '', p.edges ? `${p.edges} 条连线` : '', p.cost ? `消耗 ${p.cost} 点` : ''].filter(Boolean).join(' · ')
  return `<div class="p-changes${p.undone ? ' undone' : ''}" data-hl="${alive.join(',')}">
    <div class="ch-head">${ic('layers')}<span>本轮改动</span><span class="ch-sum">${p.undone ? '已撤销' : bits}</span></div>
    ${media.length && !p.undone ? `<div class="ch-grid">${media.map((n) => `<button data-act="locate" data-ids="${n.id}" aria-label="${esc(n.title)}"><img src="${mainSrc(n)}" alt=""></button>`).join('')}</div>` : ''}
    ${p.undone ? '' : `<div class="ch-foot"><button class="btn" data-act="locate" data-ids="${alive.join(',')}">${ic('scan')}在画布上查看</button><button class="btn ghost" data-act="undo-turn" data-pid="${p.id}">${ic('undo-2')}撤销本轮</button></div>`}</div>`
}

function textHTML(p) {
  return `<div class="p-text">${md(p.text)}${p.live ? '<span class="caret"></span>' : ''}</div>`
}

const PART = { think: thinkHTML, plan: planHTML, tool: toolHTML, ask: askHTML, confirm: confirmHTML, diff: diffHTML, changes: changesHTML, text: textHTML }

function items() {
  const out = []
  for (const t of thread) {
    if (t.role === 'user') {
      out.push({ key: t.id, html: `<div class="msg-user">${t.ctx.length ? `<div class="u-ctx">${t.ctx.map(nodeChip).join('')}</div>` : ''}<div class="u-text">${esc(t.text)}</div></div>` })
      continue
    }
    const work = t.parts.filter((p) => WORK.has(p.type))
    // 结束后把思考与工具过程收成一行，只留下正文、确认记录和改动
    const fold = t.status !== 'run' && work.length > 1
    if (fold) out.push({ key: `${t.id}:fold`, html: `<button class="p-fold" data-act="fold" data-tid="${t.id}" aria-expanded="${Boolean(t.open)}">${chev(t.open)}已处理 ${secs(t.ms)}<span>· ${work.filter((p) => p.type === 'tool').length} 次操作</span></button>` })
    for (const p of t.parts) {
      if (fold && !t.open && WORK.has(p.type)) continue
      out.push({ key: `${t.id}:${p.id}`, html: `<div class="part${fold && WORK.has(p.type) ? ' in-fold' : ''}">${PART[p.type](p)}</div>` })
    }
    if (t.status !== 'run') {
      const note = t.status === 'stopped' ? '<span class="stopped">已停止</span>' : ''
      out.push({ key: `${t.id}:foot`, html: `<div class="turn-foot">${note}<button class="icon-btn" data-act="copy" aria-label="复制">${ic('copy')}</button><button class="icon-btn" data-act="retry" data-tid="${t.id}" aria-label="重新回答">${ic('refresh-cw')}</button></div>
        ${t.followups?.length && t === thread.at(-1) ? `<div class="followups">${t.followups.map((f) => `<button data-s="${esc(f)}">${ic('arrow-right')}${esc(f)}</button>`).join('')}</div>` : ''}` })
    }
  }
  return out
}

// ---------- 面板骨架：只建一次，之后按块增量更新 ----------
const SUGGEST = [
  ['clapperboard', '根据拍摄概念拆 4 个分镜，并生成草图'],
  ['pencil', '帮镜头 02 写一段提示词'],
  ['file-check', '整理一份现场拍摄清单'],
]
function mount() {
  panel.innerHTML = `<div class="panel-head">
      <button class="thread-btn" data-act="threads"><span class="t-name">新对话</span>${ic('chevron-down')}</button>
      <span class="grow"></span>
      <button class="icon-btn" data-act="new" aria-label="新对话">${ic('plus')}</button>
      <button class="icon-btn" data-act="close" aria-label="收起助手">${ic('panel-right-close')}</button></div>
    <div class="log" role="log" aria-live="polite" aria-label="对话记录">
      <div class="empty-agent"><h3>想拍点什么？</h3><p>我会读画布上的节点，拆分镜、写提示词、直接在画布上建节点和生成草图。每一步都会写在这里，可以随时停下。</p>
        <div class="suggest">${SUGGEST.map(([i, s]) => `<button data-s="${s}">${ic(i)}${s}</button>`).join('')}</div></div>
      <div class="turns"></div></div>
    <button class="to-latest" data-act="latest" hidden>${ic('arrow-up')}回到最新</button>
    <div class="agent-dock">
      <div class="dock-status" hidden></div>
      <div class="agent-box">
        <div class="ctx-row"></div>
        <textarea rows="2" aria-label="向助手提问" placeholder="描述任务，@ 添加画布节点作为上下文"></textarea>
        <div class="composer-foot">
          <button class="chip" data-act="model">${ic('sparkles')}<span>DeepSeek V4</span></button>
          <button class="chip" data-act="mode"></button>
          <span class="grow"></span>
          <button class="send" data-act="send" aria-label="发送" disabled>${ic('arrow-up')}</button>
        </div></div></div>`
  const log = panel.querySelector('.log')
  log.addEventListener('scroll', () => {
    pinned = log.scrollHeight - log.scrollTop - log.clientHeight < 40
    panel.querySelector('.to-latest').hidden = pinned
  })
}

let pinned = true
function paint() {
  if (panel.hidden) return
  const log = panel.querySelector('.log')
  panel.querySelector('.empty-agent').hidden = thread.length > 0
  panel.querySelector('.t-name').textContent = thread.length ? '海边写真策划' : '新对话'
  reconcile(panel.querySelector('.turns'), items())
  if (pinned) log.scrollTop = log.scrollHeight
  paintDock()
  paintMarks()
}

function waiting() {
  return cur?.turn.parts.find((p) => (p.type === 'ask' || p.type === 'confirm' || p.type === 'diff') && p.state === 'wait')
}

function paintDock() {
  const status = panel.querySelector('.dock-status')
  const run = cur && !cur.stopped
  const w = waiting()
  const live = run && [...cur.turn.parts].reverse().find((p) => p.status === 'run' || p.live)
  const label = !run ? '' : w ? { ask: '等你回答', confirm: '等你确认', diff: '等你确认改动' }[w.type]
    : live?.type === 'tool' ? `${live.verb}${live.obj ? ' ' + live.obj : ''}` : live?.type === 'think' ? '思考中' : '回复中'
  status.hidden = !run
  if (run) {
    const html = `<span class="${w ? 'dot-wait' : 'dot-run'}"></span><span class="ds-label${w ? '' : ' shimmer'}">${esc(label)}</span>
      <span class="ds-time">${secs(Date.now() - cur.turn.t0)}</span><span class="grow"></span>
      ${queued ? `<span class="ds-queue" title="${esc(queued.text)}">已排队 1 条</span>` : ''}
      <button class="ds-stop" data-act="stop">${ic('square')}停止<kbd>Esc</kbd></button>`
    if (status._html !== html) { status.innerHTML = html; status._html = html; alignAnimations(status) }
  }
  // 上下文 chip
  const row = panel.querySelector('.ctx-row')
  const html = ctx.filter(byId).map((id) => `<span class="ctx-chip" data-hl="${id}">${thumb(byId(id))}<span>${esc(byId(id).title)}</span><button data-act="ctx-x" data-id="${id}" aria-label="移除上下文">${ic('x')}</button></span>`).join('')
    + `<button class="ctx-add" data-act="ctx-add" aria-label="添加画布节点作为上下文"><span class="at">@</span>${ctx.length ? '' : '<span>添加上下文</span>'}</button>`
    + (ctxManual ? '' : ctx.length ? '<span class="ctx-auto">跟随选中</span>' : '')
  if (row._html !== html) { row.innerHTML = html; row._html = html }
  // 输入框：运行中仍可输入，发送进队列；等待回答时，直接输入就是回答
  const ta = panel.querySelector('textarea')
  ta.placeholder = w?.type === 'ask' ? '直接输入回答，或在上方选择' : run ? '补充要求，会在本轮结束后发送' : '描述任务，@ 添加画布节点作为上下文'
  const send = panel.querySelector('[data-act="send"]')
  send.disabled = !ta.value.trim()
  send.setAttribute('aria-label', run ? '加入队列' : '发送')
  const m = panel.querySelector('[data-act="mode"]')
  const mh = `${ic(mode === 'auto' ? 'sparkles' : 'lock')}<span>${mode === 'auto' ? '自动执行' : '执行前确认'}</span>`
  if (m._html !== mh) { m.innerHTML = mh; m._html = mh }
}

// ---------- 运行：脚本拿到一组可取消的步骤；提问、确认会挂起运行直到被回答 ----------
let trust = false // 本对话内「不再询问」
function begin(turn) {
  cur = { turn, stopped: false, timers: new Set(), rejects: new Set(), jobs: new Set(), created: [], edges: 0, cost: 0, prompts: [], touched: false, reported: false }
  clearInterval(tick)
  tick = setInterval(() => { paintDock(); paintToggle() }, 1000)
  return cur
}

function api(run) {
  const t = run.turn
  const sleep = (ms) => new Promise((res, rej) => {
    if (run.stopped) return rej(STOP)
    const id = setTimeout(() => { run.timers.delete(id); run.rejects.delete(rej); res() }, ms)
    run.timers.add(id)
    run.rejects.add(rej)
  })
  const add = (p) => { p.id = `p${++seq}`; t.parts.push(p); paint(); return p }
  const stream = async (p, text) => {
    p.live = true
    for (let i = 0; i < text.length;) {
      i = Math.min(text.length, i + 2 + Math.floor(Math.random() * 3))
      p.text = text.slice(0, i)
      paint()
      await sleep(26)
    }
    p.live = false
    paint()
  }
  const block = (p) => new Promise((res, rej) => {
    p._res = (v) => { run.rejects.delete(rej); res(v) }
    run.rejects.add(rej)
    paint()
    if (panel.hidden) toast(p.type === 'ask' ? '助手在等你回答' : '助手在等你确认', { label: '查看', run: () => toggle(true) })
    focusBlocking()
  })
  // 第一次改画布前记一次快照：⌘Z 一次撤掉整轮
  const touch = () => { if (!run.touched) { checkpoint(); run.touched = true } }
  return {
    sleep, touch, run,
    async think(text) {
      const t0 = Date.now()
      const p = add({ type: 'think', text: '' })
      await stream(p, text)
      p.ms = Date.now() - t0
      paint()
    },
    say: (text) => stream(add({ type: 'text', text: '' }), text),
    plan(steps) {
      const p = add({ type: 'plan', steps: steps.map((s) => ({ t: s, s: 'todo' })) })
      return (i, s) => { p.steps[i].s = s; paint() }
    },
    async tool(spec, body) {
      const t0 = Date.now()
      const p = add({ type: 'tool', status: 'run', ...spec })
      if (p.ids) setBusy(p.ids, true)
      try {
        const out = body ? await body(p) : await sleep(spec.ms ?? 700)
        p.status = 'done'
        return out
      } catch (e) {
        p.status = e === STOP ? 'cancel' : 'error'
        if (e !== STOP) p.error = e.message
        throw e
      } finally {
        p.ms = Date.now() - t0
        setBusy(p.ids ?? [], false)
        paint()
      }
    },
    ask: (qs) => block(add({ type: 'ask', qs, at: 0, state: 'wait' })),
    confirm(items) {
      if (mode === 'auto' || trust) return Promise.resolve(true)
      return block(add({ type: 'confirm', items, cost: items.reduce((s, x) => s + x.cost, 0), state: 'wait' }))
    },
    diff: (nodeId, before, after) => block(add({ type: 'diff', nodeId, before, after, state: 'wait' })),
    changes() { run.reported = true; if (run.created.length || run.prompts.length) add(changesPart(run)) },
    // 在画布上跑生成：节点各自出现进度，工具行显示总进度，结果缩略图挂在工具行下面
    generate(nodes, p) {
      return new Promise((res, rej) => {
        run.rejects.add(rej)
        const used = new Set(state.nodes.flatMap((n) => n.versions ?? []))
        const pool = photos.filter((ph) => !used.has(ph.src))
        const t0 = Date.now()
        const jobs = nodes.map((n, i) => ({ n, at: t0 + i * 450, job: { p: 0, label: '排队中' }, src: (pool[i] ?? photos[i]).src }))
        for (const j of jobs) { ui.running.set(j.n.id, j.job); run.jobs.add(j.n.id) }
        render()
        const timer = setInterval(() => {
          let sum = 0
          for (const j of jobs) {
            if (!j.done && !ui.running.has(j.n.id)) j.done = j.cancel = true // 在节点输入框里被单独停掉
            if (!j.done) {
              const e = Date.now() - j.at
              j.job.p = Math.max(0, Math.min(100, Math.round((e / 3200) * 100)))
              j.job.label = e < 500 ? '排队中' : `生成中 ${j.job.p}%`
              const el = nodeEl(j.n.id)?.querySelector('.run-state')
              if (el) { el.querySelector('span:nth-child(2)').textContent = j.job.label; el.querySelector('.bar span').style.width = `${j.job.p}%` }
              if (j.job.p >= 100) {
                j.done = true
                ui.running.delete(j.n.id)
                run.jobs.delete(j.n.id)
                j.n.versions.push(j.src)
                j.n.lastRequest = { by: 'agent' }
                p.result = [...(p.result ?? []), j.src]
                const cost = models.image.find((m) => m.key === j.n.model)?.price ?? 0
                state.credits -= cost
                run.cost += cost
                emit('credits')
                commit()
              }
            }
            sum += j.done ? 100 : j.job.p
          }
          p.progress = Math.round(sum / jobs.length)
          paint()
          if (jobs.every((j) => j.done)) {
            clearInterval(timer)
            run.timers.delete(timer)
            run.rejects.delete(rej)
            res(jobs.filter((j) => !j.cancel).map((j) => j.src))
          }
        }, 120)
        run.timers.add(timer)
      })
    },
  }
}

function changesPart(run) {
  return { id: `p${++seq}`, type: 'changes', created: [...run.created], edges: run.edges, cost: run.cost, prompts: [...run.prompts] }
}

function stop() {
  const run = cur
  if (!run || run.stopped) return
  run.stopped = true
  for (const id of run.timers) clearInterval(id)
  for (const rej of [...run.rejects]) rej(STOP)
  for (const p of run.turn.parts) {
    p.live = false
    if (p.status === 'run') p.status = 'cancel'
    if (p.state === 'wait') p.state = p.type === 'ask' ? 'cancel' : 'no'
    p.steps?.forEach((s) => { if (s.s === 'doing') s.s = 'todo' })
  }
  for (const id of run.jobs) ui.running.delete(id)
  busy.clear()
  render()
}

function finish(run, status) {
  const t = run.turn
  t.status = status
  t.ms = Date.now() - t.t0
  // 中途停下也要交代已经改了什么
  if (!run.reported && (run.created.length || run.prompts.length)) t.parts.push(changesPart(run))
  if (run.touched) commit()
  cur = null
  clearInterval(tick)
  paint()
  paintToggle()
  if (panel.hidden && status === 'done') toast('助手已完成', { label: '查看', run: () => toggle(true) })
  if (queued) { const q = queued; queued = null; send(q.text, q.ctx) }
}

function paintToggle() {
  const b = $('agent-toggle')
  b.dataset.state = !cur ? '' : waiting() ? 'wait' : 'run'
}

// 需要回答时把焦点放到选项上，数字键可直接选
function focusBlocking() {
  if (panel.hidden) return
  requestAnimationFrame(() => panel.querySelector('.p-ask:not(.done) .ask-opt, .p-confirm:not(.settled) .btn-primary, .p-diff:not(.settled) .btn-primary')?.focus({ preventScroll: true }))
}

// ---------- 示例脚本：原型没有真实模型，按关键词走固定剧本 ----------
const bottom = () => Math.max(...state.nodes.map((n) => n.y + n.h)) + 140
const plainText = (html) => { const d = document.createElement('div'); d.innerHTML = html ?? ''; return d.innerText.trim() }

async function storyboard(a) {
  const brief = byId('brief') ?? state.nodes.find((n) => n.type === 'text')
  const refs = state.nodes.filter((n) => n.type === 'image' && n.title.startsWith('参考'))
  await a.think('先看画布上的拍摄概念和参考图，确认情绪关键词。\n概念写的是傍晚六点、逆光、白色长裙、低饱和暖调，机位以中远景为主。\n四个镜头按景别从远到近排，这样剪辑时有节奏。画幅和质感还没定，先问一下。')
  await a.tool({ kind: 'read', verb: '读取画布', obj: `${brief?.title ?? '拍摄概念'} 等 ${1 + refs.length} 个节点`, ids: [brief?.id, ...refs.map((r) => r.id)].filter(Boolean), ms: 900,
    detail: [['文本', esc(plainText(brief?.text).slice(0, 60)) + '…'], ['参考图', refs.map((r) => esc(r.title)).join('、') || '无']] })
  const [look, ratio, extra] = await a.ask([
    { q: '这组草图走哪种质感？', short: '质感', hint: '会写进每个分镜的提示词', opts: [
      { label: '胶片暖调', desc: '颗粒、低饱和、高光溢出', img: photos[9].src },
      { label: '清透日系', desc: '高调、偏青、干净', img: photos[5].src },
      { label: '电影冷调', desc: '青橙对比、暗部压低', img: photos[10].src },
    ] },
    { q: '草图用什么画幅？', short: '画幅', opts: [
      { label: '3:4', desc: '竖构图，适合小红书', ratio: '3:4' },
      { label: '16:9', desc: '横构图，留天空', ratio: '16:9' },
      { label: '9:16', desc: '全屏竖版，适合视频', ratio: '9:16' },
    ] },
    { q: '还有要特别注意的吗？', short: '要点', multi: true, hint: '可多选', opts: [
      { label: '保留大面积天空' }, { label: '人物不露正脸' }, { label: '加入胶片颗粒' },
    ] },
  ])
  const style = look?.label ?? '胶片暖调'
  const r = ratio?.ratio ?? (ratio?.label?.match(/\d+:\d+/)?.[0]) ?? '3:4'
  const notes = extra?.label ? `，${extra.label}` : ''
  await a.think(`按「${style}」、${r} 来写。${extra?.label ? `额外要求：${extra.label}。` : ''}\n每个分镜都连上「参考 · 海风」，远景再加「参考 · 留白」。生成会消耗点数，先建节点，再确认生成。`)
  const step = a.plan(['建 4 个分镜节点并写好提示词', '连上参考素材', '生成 4 张草图', '整理成一个组'])

  step(0, 'doing')
  const shots = [
    ['远景 · 海平线', '人物占画面 1/6，大面积天空留白'],
    ['中景 · 逆光长裙', '太阳压在肩线，裙摆被风带起'],
    ['近景 · 回头', '发丝轮廓光，焦点在眼睛'],
    ['特写 · 手与裙摆', '浅景深，暖调高光溢出'],
  ]
  const ref = refs[0], wide = refs[1]
  const nodes = []
  await a.tool({ kind: 'create', verb: '新建节点', obj: '4 个分镜', ids: [] }, async (p) => {
    a.touch()
    const y = bottom()
    for (const [i, [t, d]] of shots.entries()) {
      const n = makeNode('image', i * 340, y, {
        title: `分镜 ${String(i + 1).padStart(2, '0')} · ${t}`, model: 'seedream', ratio: r,
        prompt: `${ref ? `@[${ref.id}] ` : ''}傍晚海边，${d}，${style}${notes}`,
      })
      Object.assign(n, { w: 300, h: Math.round((300 * r.split(':')[1]) / r.split(':')[0]) })
      nodes.push(n)
      a.run.created.push(n.id)
      p.ids = nodes.map((x) => x.id)
      p.obj = `${nodes.length}/4 个分镜`
      setBusy([n.id], true)
      render()
      // 第一个节点出现时把视口拉到能同时看到原画布和新区域，之后的节点就在眼前逐个长出来
      if (i === 0) fitView(new Set(state.nodes.map((x) => x.id)))
      await a.sleep(420)
    }
    p.obj = '4 个分镜'
    p.detail = nodes.map((n) => [n.title.split(' · ')[1], esc(n.prompt.replace(/@\[[\w-]+\]\s*/, '[图1] '))])
    fitView(new Set(p.ids))
  })
  step(0, 'done')

  step(1, 'doing')
  await a.tool({ kind: 'connect', verb: '连接参考', obj: `${ref?.title ?? '参考'} → 4 个分镜`, ids: [ref?.id, wide?.id, ...nodes.map((n) => n.id)].filter(Boolean) }, async () => {
    for (const n of nodes) {
      if (ref && connect(ref.id, n.id)) a.run.edges++
      render()
      await a.sleep(160)
    }
    if (wide && connect(wide.id, nodes[0].id)) a.run.edges++
    render()
  })
  step(1, 'done')

  step(2, 'doing')
  const price = models.image.find((m) => m.key === 'seedream').price
  const ok = await a.confirm(nodes.map((n) => ({ id: n.id, model: 'Seedream 5.0', ratio: r, cost: price })))
  if (ok) {
    await a.tool({ kind: 'generate', verb: '生成草图', obj: `4 张 · Seedream 5.0`, ids: nodes.map((n) => n.id), progress: 0 }, (p) => a.generate(nodes, p))
    step(2, 'done')
  } else step(2, 'skip')

  step(3, 'doing')
  await a.tool({ kind: 'create', verb: '整理成组', obj: '分镜草图', ids: nodes.map((n) => n.id), ms: 500 }, async () => {
    await a.sleep(400)
    const g = group(nodes.map((n) => n.id))
    if (g) { g.title = '分镜草图'; a.run.created.push(g.id) }
    render()
  })
  step(3, 'done')
  a.changes()
  await a.say(ok
    ? `4 个分镜已经放在画布下方的「分镜草图」组里，都连着「${ref?.title ?? '参考'}」。\n草图是 **${style}**、${r}，每个节点的提示词都能单独改，改完选中节点按 Enter 重新生成。`
    : '好的，没有生成。4 个分镜节点和提示词已经留在画布上，逐个选中调整后按 Enter 就能生成。')
  a.run.turn.followups = ok ? ['把分镜 03 改成侧脸', '整理一份现场拍摄清单'] : ['帮我生成这 4 张', '整理一份现场拍摄清单']
}

async function writePrompt(a) {
  const n = byId('shot-2') ?? state.nodes.find((x) => x.type === 'image')
  if (!n) return a.say('画布上还没有图片节点。先建一个，我再帮你写提示词。')
  const ups = upstream(n.id)
  await a.think(`「${n.title}」目前没有提示词，上游连着 ${ups.map((u) => `「${u.title}」`).join('、') || '空'}。\n背影镜头要和镜头 01 的光线一致：同一时间、同一片海，逆光改为侧逆光，让轮廓更清楚。`)
  await a.tool({ kind: 'read', verb: '读取节点', obj: `${n.title} 与上游 ${ups.length} 个`, ids: [n.id, ...ups.map((u) => u.id)], ms: 700 })
  const ref = ups.find((u) => u.type === 'image')
  const after = `${ref ? `@[${ref.id}] ` : ''}同一片海边的傍晚，人物背对镜头走向海面，侧逆光勾出发丝和裙摆轮廓，胶片颗粒，低饱和暖调，中远景，地平线压在画面下三分之一`
  const ok = await a.diff(n.id, n.prompt ?? '', after)
  if (ok) {
    a.touch()
    a.run.prompts.push({ id: n.id, before: n.prompt ?? '' })
    n.prompt = after
    commit()
    a.changes()
    await a.say(`已写入「${n.title}」。它参考的是「${ref?.title ?? '上游'}」的主版本，选中节点按 Enter 就能生成。`)
    a.run.turn.followups = ['直接帮我生成镜头 02', '再写一版更安静的']
  } else {
    await a.say('没有改动节点。需要的话告诉我想往哪个方向调。')
  }
}

async function generateOne(a) {
  const n = byId('shot-2')
  if (!n) return a.say('找不到「镜头 02」，它可能已经被删除了。')
  const m = models.image.find((x) => x.key === n.model) ?? models.image[0]
  await a.think(`「${n.title}」用 ${m.name}，${n.ratio}。生成一张。`)
  const ok = await a.confirm([{ id: n.id, model: m.name, ratio: n.ratio, cost: m.price }])
  if (!ok) return a.say('好的，没有生成。')
  a.touch()
  await a.tool({ kind: 'generate', verb: '生成', obj: `${n.title} · ${m.name}`, ids: [n.id], progress: 0 }, (p) => a.generate([n], p))
  await a.say(`「${n.title}」有了新版本。版本条里可以对比，满意的那张设为主版本，下游就会用它。`)
}

async function checklist(a) {
  const all = state.nodes.filter((n) => !isGroup(n))
  await a.think('把概念、参考和已有镜头合在一起，按到场前、拍摄中、收尾排。傍晚逆光窗口大约 40 分钟，要把远景放在最前面。')
  await a.tool({ kind: 'read', verb: '读取画布', obj: `全部 ${all.length} 个节点`, ids: all.map((n) => n.id), ms: 800 })
  const body = '- **17:20 到场**：确认日落方位，白裙备一件替换\n- **17:40 远景**：先拍海平线，天空亮度最好\n- **18:00 中近景**：逆光长裙、回头，反光板补眼神光\n- **18:20 特写**：手与裙摆，光线变暖后拍\n- **收尾**：拍 30 秒空镜和海浪声'
  await a.say(body)
  await a.tool({ kind: 'create', verb: '新建文本节点', obj: '现场拍摄清单', ids: [] }, async (p) => {
    a.touch()
    const n = makeNode('text', 0, bottom(), { title: '现场拍摄清单', h: 240,
      text: `<h4>现场拍摄清单</h4>${body.split('\n').map((l) => `<p>${esc(l.replace(/^- /, '').replace(/\*\*/g, ''))}</p>`).join('')}` })
    a.run.created.push(n.id)
    p.ids = [n.id]
    render()
    fitView(new Set([n.id]))
    await a.sleep(400)
  })
  a.changes()
  await a.say('清单也放到了画布上，可以直接改。')
}

async function chat(a, text) {
  await a.think(`对方问的是「${text.slice(0, 30)}」。原型只演示了几条固定流程，这里给个方向就好。`)
  await a.say('这个原型只接了几条示例流程：拆分镜并生成草图、给镜头写提示词、整理拍摄清单。换一个试试？')
  a.run.turn.followups = SUGGEST.map((x) => x[1])
}

const pendingShots = () => state.nodes.filter((n) => n.type === 'image' && n.title.startsWith('分镜') && !n.versions.length)

async function generateShots(a) {
  const nodes = pendingShots()
  if (!nodes.length) return storyboard(a)
  await a.think(`画布上有 ${nodes.length} 个还没生成的分镜，沿用它们各自的提示词和参考。`)
  const ok = await a.confirm(nodes.map((n) => ({ id: n.id, model: 'Seedream 5.0', ratio: n.ratio, cost: models.image[0].price })))
  if (!ok) return a.say('好的，没有生成。')
  a.touch()
  await a.tool({ kind: 'generate', verb: '生成草图', obj: `${nodes.length} 张 · Seedream 5.0`, ids: nodes.map((n) => n.id), progress: 0 }, (p) => a.generate(nodes, p))
  a.changes()
  await a.say('生成好了，每张都在原节点里作为主版本。')
}

async function editShot(a) {
  const n = state.nodes.find((x) => x.type === 'image' && /分镜 03/.test(x.title))
  if (!n) return a.say('画布上没有「分镜 03」。先让我拆一组分镜？')
  await a.think('回头改成侧脸：人物转向画面右侧，保留发丝轮廓光，焦点仍在眼睛。只改提示词，不动参考。')
  await a.tool({ kind: 'read', verb: '读取节点', obj: n.title, ids: [n.id], ms: 500 })
  const after = n.prompt.includes('发丝轮廓光') ? n.prompt.replace('发丝轮廓光', '侧脸朝向画面右侧，发丝轮廓光') : `${n.prompt}，侧脸朝向画面右侧`
  const ok = await a.diff(n.id, n.prompt, after)
  if (!ok) return a.say('没有改动。')
  a.touch()
  a.run.prompts.push({ id: n.id, before: n.prompt })
  n.prompt = after
  commit()
  a.changes()
  await a.say(`「${n.title}」的提示词已更新，现有草图没有动。选中节点按 Enter 生成新版本，旧版本会留在版本条里。`)
  a.run.turn.followups = ['帮我生成分镜 03']
}

function scenario(text) {
  if (/改成|侧脸/.test(text)) return editShot
  if (/生成这|生成分镜 03/.test(text)) return /03/.test(text) ? generateNamed('分镜 03') : generateShots
  if (/生成.*02/.test(text)) return generateOne
  if (/分镜|拆/.test(text)) return storyboard
  if (/提示词/.test(text)) return writePrompt
  if (/清单/.test(text)) return checklist
  return chat
}
function generateNamed(key) {
  return async (a) => {
    const n = state.nodes.find((x) => x.type === 'image' && x.title.includes(key))
    if (!n) return a.say(`画布上找不到「${key}」。`)
    const m = models.image.find((x) => x.key === n.model) ?? models.image[0]
    const ok = await a.confirm([{ id: n.id, model: m.name, ratio: n.ratio, cost: m.price }])
    if (!ok) return a.say('好的，没有生成。')
    a.touch()
    await a.tool({ kind: 'generate', verb: '生成', obj: `${n.title} · ${m.name}`, ids: [n.id], progress: 0 }, (p) => a.generate([n], p))
    await a.say(`「${n.title}」有了新版本，已作为主版本。`)
  }
}

// ---------- 发送、回答与交互 ----------
const input = () => panel.querySelector('textarea')
const findPart = (pid) => thread.flatMap((t) => t.parts ?? []).find((p) => p.id === pid)

async function send(text, c = ctx.filter(byId)) {
  text = text.trim()
  if (!text) return
  const w = waiting()
  if (w?.type === 'ask') { input().value = ''; return answer(w, { label: text }) }
  // 运行中再发一条：排到本轮之后，不打断正在做的事
  if (cur) { queued = { text, ctx: c }; input().value = ''; return paintDock() }
  if (!consented) {
    consented = await confirmModal({ title: '发送给外部模型？', body: '对话和所选节点的文字、图片会发送给 DeepSeek 处理。本项目只需确认一次，可在设置中撤回。', ok: '同意并发送' })
    if (!consented) return
  }
  input().value = ''
  thread.push({ id: `t${++seq}`, role: 'user', text, ctx: c })
  const turn = { id: `t${++seq}`, role: 'ai', parts: [], status: 'run', t0: Date.now() }
  thread.push(turn)
  ctxManual = false
  ctx = [...ui.sel].filter((id) => !isGroup(byId(id)))
  pinned = true
  const run = begin(turn)
  paint()
  paintToggle()
  const a = api(run)
  try {
    await a.sleep(300)
    await scenario(text)(a, text)
    finish(run, 'done')
  } catch (e) {
    if (e !== STOP) { console.error(e); turn.parts.push({ id: `p${++seq}`, type: 'text', text: `出错了：${e.message}` }) }
    finish(run, e === STOP ? 'stopped' : 'error')
  }
}

function answer(p, a) {
  const q = p.qs[p.at]
  q.answer = a
  if (!a) q.skipped = true
  if (++p.at < p.qs.length) { paint(); return focusBlocking() }
  p.state = 'ok'
  p.at = p.qs.length - 1
  p._res(p.qs.map((x) => x.answer))
  paint()
}

function settle(p, v) {
  p.state = v ? 'ok' : 'no'
  if (p.type === 'confirm' && v && p.remember) trust = true
  p._res(v)
  paint()
}

function undoTurn(p) {
  if (p.undone) return
  checkpoint()
  removeNodes(new Set(p.created))
  for (const { id, before } of p.prompts) { const n = byId(id); if (n) n.prompt = before }
  p.undone = true
  commit()
  emit('select')
  toast(p.cost ? `已撤销本轮改动，已消耗的 ${p.cost} 点不退回` : '已撤销本轮改动')
}

function toggle(open = panel.hidden) {
  panel.hidden = !open
  $('agent-toggle').setAttribute('aria-pressed', String(open))
  if (!open) return highlight([])
  pinned = true
  paint()
  if (waiting()) focusBlocking()
  else input().focus()
}

function onClick(e) {
  const s = e.target.closest('[data-s]')?.dataset.s
  if (s) return send(s)
  const g = e.target.closest('[data-act]')
  if (!g) return
  const act = g.dataset.act
  const p = g.dataset.pid && findPart(g.dataset.pid)
  const menuAt = { rect: g.getBoundingClientRect() }
  switch (act) {
    case 'close': return toggle(false)
    case 'send': return send(input().value)
    case 'stop': return stop()
    case 'new': stop(); queued = null; trust = false; thread.length = 0; return paint()
    case 'latest': pinned = true; return paint()
    case 'toggle': p.open = !p.open; return paint()
    case 'fold': { const t = thread.find((x) => x.id === g.dataset.tid); t.open = !t.open; return paint() }
    case 'locate': return locate(g.dataset.ids.split(',').filter(Boolean))
    case 'pick': {
      const q = p.qs[p.at], i = Number(g.dataset.i)
      if (!q.multi) return answer(p, q.opts[i])
      q.picked ??= new Set()
      q.picked.has(i) ? q.picked.delete(i) : q.picked.add(i)
      return paint()
    }
    case 'ask-ok': { const q = p.qs[p.at]; return answer(p, { label: [...q.picked].sort().map((i) => q.opts[i].label).join('、') }) }
    case 'skip': return answer(p, null)
    case 'remember': p.remember = g.checked; return
    case 'cf-ok': case 'df-ok': return settle(p, true)
    case 'cf-no': case 'df-no': return settle(p, false)
    case 'undo-turn': return undoTurn(p)
    case 'copy': return toast('已复制')
    case 'retry': {
      if (cur) return toast('等这一轮结束，或先停止')
      const i = thread.findIndex((t) => t.id === g.dataset.tid)
      const u = thread[i - 1]
      return u && send(u.text, u.ctx)
    }
    case 'ctx-x': ctxManual = true; ctx = ctx.filter((id) => id !== g.dataset.id); return paintDock()
    case 'ctx-add': return openMenu([{ section: '添加为上下文' }, ...state.nodes.filter((n) => !isGroup(n) && !ctx.includes(n.id)).map((n) => ({
      label: n.title, icon: TYPE_ICON[n.type], run: () => { ctxManual = true; ctx.push(n.id); paintDock(); input().focus() },
    }))], menuAt, g)
    case 'threads': return openMenu([{ section: '最近对话' }, { label: '海边写真策划', icon: 'clock', checked: thread.length > 0 }, { label: '婚纱夜景提示词', icon: 'clock' }, { label: '产品静物布光', icon: 'clock' }], menuAt, g)
    case 'model': return openMenu(['DeepSeek V4', 'Claude Sonnet 5', 'Qwen 3.5'].map((m) => ({
      label: m, checked: g.textContent.trim() === m, run: () => (g.querySelector('span:last-child').textContent = m),
    })), menuAt, g)
    case 'mode': return openMenu([
      { label: '执行前确认', sub: '生成消耗点数、改写提示词前先问', checked: mode === 'confirm', run: () => { mode = 'confirm'; paintDock() } },
      { label: '自动执行', sub: '直接动手，结束后可整轮撤销', checked: mode === 'auto', run: () => { mode = 'auto'; paintDock() } },
    ], menuAt, g)
  }
}

export function initAgent() {
  mount()
  const tg = $('agent-toggle')
  tg.querySelector('.i').innerHTML = ic('sparkles')
  tg.addEventListener('click', () => toggle())
  panel.addEventListener('click', onClick)
  panel.addEventListener('input', (e) => { if (e.target.matches('textarea')) paintDock() })
  panel.addEventListener('keydown', (e) => {
    e.stopPropagation()
    if (e.key === 'Escape' && cur) { e.preventDefault(); return stop() }
    if (e.isComposing) return
    if (e.target.matches('textarea') && e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); return send(e.target.value) }
    if (e.target.matches('.ask-other') && e.key === 'Enter' && e.target.value.trim()) return answer(findPart(e.target.dataset.pid), { label: e.target.value.trim() })
    // 焦点在提问卡片里时，数字键直接选
    const ask = e.target.closest('.p-ask:not(.done)')
    if (ask && !e.target.matches('input') && /^[1-9]$/.test(e.key)) ask.querySelectorAll('.ask-opt')[Number(e.key) - 1]?.click()
  })
  panel.addEventListener('pointerover', (e) => {
    const t = e.target.closest('[data-ids],[data-hl]')
    highlight((t?.dataset.ids ?? t?.dataset.hl ?? '').split(',').filter(Boolean))
  })
  panel.addEventListener('pointerleave', () => highlight([]))
  on('change', () => paint())
  on('select', () => {
    if (ctxManual || panel.hidden) return
    ctx = [...ui.sel].filter((id) => !isGroup(byId(id)))
    paintDock()
  })
}
