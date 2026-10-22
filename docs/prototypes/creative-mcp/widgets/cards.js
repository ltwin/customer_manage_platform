// U-2 生成与任务卡、U-3 镜头卡。两个 ui:// 资源共用这份渲染代码：
// - ui://creative/tasks 挂在 generate_node、wait_tasks；
// - ui://creative/shots 挂在 render_view、export_shots。
// 回看和导出也可能先排队，所以两张卡都能显示任务状态。
// 规则：
// - 行内卡片最多两个动作，不嵌套滚动、不用菜单；
// - 卡片里的动作一律经宿主 tools/call 走 MCP，不直连网页端接口；
// - 创作者在卡片里做了什么，都用 ui/update-model-context 告诉外部助手。
window.Cards = (() => {
  const app = window.McpApp
  const el = document.getElementById('app')
  const svg = (d) => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${d}</svg>`
  const IC = {
    expand: svg('<path d="M15 3h6v6"/><path d="M9 21H3v-6"/><path d="M21 3l-7 7"/><path d="M3 21l7-7"/>'),
    open: svg('<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>'),
    check: svg('<path d="M20 6 9 17l-5-5"/>'),
  }
  const esc = (s) => String(s ?? '').replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c])
  const pct = (p) => `${Math.round((p ?? 0) * 100)}%`
  const terminal = (t) => t.status === 'succeeded' || t.status === 'failed'

  let s = { view: null } // 当前显示的数据：structuredContent + _meta + 卡片本地状态
  let input = {} // 工具调用的参数，卡片里点「生成」时原样复用
  // 交给外部助手的上下文：规范规定每次更新覆盖上一次，所以维护一份累计摘要，每次发完整内容
  const told = []
  const tell = (line) => {
    told.push(line)
    return app.updateModelContext(told.join('\n'))
  }
  let timer = 0
  // 卡片能不能自己调用工具：宿主没声明 serverTools，或调用失败、超时，就当不能。
  // 不能时卡片只显示提交那一刻的快照，不假装在刷新，结果交给外部助手后面的工具结果。
  let offline = false
  let current = 0 // 全屏查看时的当前图

  function fromResult(r) {
    const sc = r?.structuredContent ?? {}
    return { ...sc, meta: r?._meta ?? {}, local: s.local ?? {} }
  }

  // ---- 视图 ----
  const skeleton = () => `<div class="card" aria-busy="true"><div class="skel" style="height:18px;width:40%"></div><div class="skel" style="aspect-ratio:3/2"></div></div>`

  function dryRun(d) {
    const thumbs = (d.meta.thumbs ?? []).map((u, i) => `<img src="${esc(u)}" alt="${esc(d.refs?.[i]?.title)}">`).join('')
    const head = `<div class="head"><div class="t"><p class="title">生成预检</p><p class="sub">${esc(d.node?.title)} · 还没有生成，没有扣点</p></div></div>`
    const facts = `<dl class="facts">
        <dt>模型</dt><dd>${esc(d.model)}</dd>
        <dt>数量</dt><dd>${d.count} 张 · ${esc(d.aspect)}</dd>
        <dt>参考</dt><dd class="refs">${thumbs}<span class="muted">${esc((d.refs ?? []).map((r) => r.title).join('、'))}</span></dd>
        <dt>预计</dt><dd><b>${d.cost} 点</b> <span class="muted">· 剩余 ${Number(d.balance).toLocaleString()} 点</span></dd>
      </dl>`
    if (d.local.declined)
      return `<div class="card">${head}<p class="note muted">你选择了不生成。要调整就直接在对话里说。</p></div>`
    if (offline)
      return `<div class="card">${head}${facts}<p class="note muted">${d.local.failed ? '卡片没能提交，没有扣点。' : ''}要生成，请在对话里回复「生成」。</p></div>`
    const busy = d.local.submitting
    return `<div class="card">${head}${facts}
      <div class="actions">
        <button class="btn primary" data-act="generate" ${busy ? 'disabled' : ''}>${busy ? '提交中…' : `生成 · ${d.cost} 点`}</button>
        <button class="btn" data-act="decline" ${busy ? 'disabled' : ''}>不生成</button>
      </div></div>`
  }

  function statusChip(t) {
    if (t.status === 'succeeded') return `<span class="status ok">${IC.check.replace('<svg', '<svg width="12" height="12"')}已完成${t.cost ? ` · 扣 ${t.cost} 点` : ''}</span>`
    if (t.status === 'failed') return `<span class="status err"><i></i>失败</span>`
    if (t.status === 'waiting_for_page') return `<span class="status run"><i></i>等待页面</span>`
    if (offline) return `<span class="status run"><i></i>已提交</span>`
    return `<span class="status run"><i></i>${t.kind === 'generate' ? '生成中' : '渲染中'} ${pct(t.progress)}</span>`
  }

  const expandBtn = () => (app.canUse('fullscreen') && !isFull() ? `<button class="icon" data-act="expand" aria-label="放大查看">${IC.expand}</button>` : '')
  const isFull = () => document.documentElement.dataset.mode === 'fullscreen'

  function images(list, ratio) {
    if (isFull() && list.length) {
      const cur = list[Math.min(current, list.length - 1)]
      return `<div class="viewer"><img src="${esc(cur.url)}" alt="${esc(cur.label)}"><span class="muted">${esc(cur.label)}</span>
        ${list.length > 1 ? `<div class="thumbs">${list.map((x, i) => `<button data-act="pick" data-i="${i}" aria-current="${i === current}" aria-label="${esc(x.label)}"><img src="${esc(x.url)}" alt=""></button>`).join('')}</div>` : ''}</div>`
    }
    // 行内不让结果图把对话撑得太长：竖图两列最宽 420px，单张横图最宽 560px；全屏时不限
    const cols = list.length === 1 && ratio >= 1 ? 1 : 2
    return `<div class="grid" style="--cols:${cols};max-width:${cols === 1 ? 560 : 420}px">${list.map((x) => `<figure class="shot"><img src="${esc(x.url)}" alt="${esc(x.label)}" style="aspect-ratio:${x.w}/${x.h}"><figcaption>${esc(x.label)}</figcaption></figure>`).join('')}</div>`
  }

  function task(t, meta) {
    const m = meta.tasks?.[t.id] ?? {}
    const [rw, rh] = String(t.aspect ?? '3:2').split(':').map(Number)
    const ratio = rw / rh
    const n = t.kind === 'generate' ? t.count ?? 1 : 1
    const sub = t.kind === 'generate' ? `${n} 张 · ${esc(t.aspect)}` : '由打开的创意空间页面渲染'
    const head = `<div class="head"><div class="t"><p class="title">${esc(t.title)}</p><p class="sub row">${statusChip(t)}<span>${sub}</span></p></div>${t.status === 'succeeded' ? expandBtn() : ''}</div>`
    if (t.status === 'succeeded') {
      const saved = s.local.saved
      const second = t.kind === 'generate' ? `<button class="btn" data-act="save" ${saved ? 'disabled' : ''}>${saved ? '已存入资产库' : '存入资产库'}</button>` : ''
      return `${head}${images(m.results ?? [], ratio)}<div class="actions"><button class="btn primary" data-act="open" data-url="${esc(m.canvas_url)}">${IC.open}在画布中查看</button>${second}</div>`
    }
    if (t.status === 'failed')
      return `${head}<p class="note">${esc(t.error ?? '生成失败：模型服务超时。没有扣点。')}</p><div class="actions"><button class="btn" data-act="open" data-url="${esc(m.canvas_url)}">在画布中查看</button></div>`
    const skels = `<div class="grid" style="--cols:${n === 1 && ratio >= 1 ? 1 : 2}">${Array.from({ length: n }, () => `<div class="skel" style="aspect-ratio:${rw}/${rh}"></div>`).join('')}</div>`
    if (t.status === 'waiting_for_page')
      return `${head}<p class="note">回看要由一个打开的创意空间页面来渲染，现在没有。打开后会自动继续，完成前请保持页面开着。${offline ? '这张卡片不会自己刷新，结果由外部助手在后面的回复里给出。' : ''}</p>
        <div class="actions"><button class="btn primary" data-act="open" data-url="${esc(m.open_url)}">${IC.open}打开创意空间</button></div>`
    if (offline)
      return `${head}<p class="note muted">这张卡片不会自己刷新。完成后，外部助手会在后面的回复里给出结果。</p>
        <div class="actions"><button class="btn" data-act="open" data-url="${esc(m.canvas_url)}">在画布中查看进度</button></div>`
    return `${head}${skels}<div class="bar"><i style="width:${pct(t.progress)}"></i></div>
      <div class="actions"><button class="btn" data-act="open" data-url="${esc(m.canvas_url)}">在画布中查看进度</button></div>`
  }

  function tasks(d) {
    return `<div class="card">${(d.tasks ?? []).map((t) => task(t, d.meta)).join('<hr style="border:0;border-top:var(--border-width-regular) solid var(--color-border-tertiary);margin:4px 0">')}</div>`
  }

  function shots(d) {
    const list = (d.meta.shots ?? []).map((x, i) => ({ ...x, ...(d.shots?.[i] ?? {}) }))
    if (isFull()) return `<div class="card"><div class="head"><div class="t"><p class="title">导出分镜</p><p class="sub">${list.length} 个镜头</p></div></div>${images(list, 1.5)}</div>`
    return `<div class="card">
      <div class="head"><div class="t"><p class="title">导出分镜 · ${list.length} 个镜头</p><p class="sub">已作为图片节点放进画布</p></div>${expandBtn()}</div>
      <div class="rail">${list
        .map(
          (x) => `<div class="item"><img src="${esc(x.url)}" alt="${esc(x.label)}"><span class="name">${esc(x.label)}</span>
          <span class="meta">${(x.meta ?? []).slice(0, 2).map(esc).join('<br>')}</span>
          <button class="link" data-act="open" data-url="${esc(x.canvas_url)}">在画布中查看</button></div>`,
        )
        .join('')}</div></div>`
  }

  function render() {
    if (!s.view) return (el.innerHTML = skeleton())
    el.innerHTML = s.view === 'dry_run' ? dryRun(s) : s.view === 'shots' ? shots(s) : tasks(s)
  }

  // ---- 轮询：卡片自己经宿主调用 wait_tasks（visibility 含 "app"）----
  function schedule() {
    clearTimeout(timer)
    if (offline || s.view !== 'tasks' || (s.tasks ?? []).every(terminal)) return
    timer = setTimeout(poll, 1500)
  }
  async function poll() {
    const before = s.tasks.filter(terminal).length
    let r
    try {
      r = await app.callTool('wait_tasks', { task_ids: s.tasks.map((t) => t.id), timeout_s: 20 })
    } catch (err) {
      return goOffline('wait_tasks', err)
    }
    s = fromResult(r)
    render()
    const done = s.tasks.filter((t) => t.status === 'succeeded')
    if (s.tasks.filter(terminal).length > before && done.length)
      tell(
        `卡片显示：${done.map((t) => `任务 ${t.id}（${t.title}）已完成`).join('；')}。结果图片创作者已在卡片里看到，不必再调用 wait_tasks。`,
      )
    schedule()
  }

  function goOffline(tool, err) {
    offline = true
    beacon('tool_error', { tool, err: String(err?.message ?? err) })
    render()
  }

  // ---- 动作 ----
  el.addEventListener('click', async (e) => {
    const b = e.target.closest('[data-act]')
    if (!b) return
    const act = b.dataset.act
    if (act === 'open') return app.openLink(b.dataset.url)
    if (act === 'expand') return app.requestDisplayMode('fullscreen')
    if (act === 'pick') {
      current = Number(b.dataset.i)
      return render()
    }
    if (act === 'decline') {
      s.local = { ...s.local, declined: true }
      render()
      return tell('创作者在预检卡里选择了不生成，没有扣点。')
    }
    if (act === 'generate') {
      s.local = { ...s.local, submitting: true }
      render()
      let r
      try {
        r = await app.callTool('generate_node', { ...input, node_id: s.node.id, count: s.count, dry_run: false })
      } catch (err) {
        s.local = { failed: true }
        return goOffline('generate_node', err)
      }
      s = fromResult(r)
      s.local = {}
      render()
      const t = s.tasks[0]
      tell(`创作者在预检卡里确认了生成：已提交任务 ${t.id}（${t.count} 张，预计 ${t.cost} 点）。不要重复提交；进度和结果显示在卡片里。`)
      return schedule()
    }
    if (act === 'save') {
      const n = s.meta.tasks?.[s.tasks[0].id]?.results?.length ?? 0
      await app.callTool('save_to_library', { task_id: s.tasks[0].id })
      s.local = { ...s.local, saved: true }
      render()
      return tell(`创作者在卡片里把 ${n} 张结果存入了资产库「未归类」。`)
    }
  })

  // 活原型诊断（window.CARD_DIAG）：把宿主给的上下文、图片加载、上下文更新结果回报给服务端日志。
  // 两条路：图片请求打到服务端的 /diag（不依赖宿主转发工具调用），和 app-only 工具 report_card_event
  // （服务端收到它就说明卡片能调用工具）。两者都只在活原型服务里存在。
  function beacon(event, data) {
    if (!window.CARD_DIAG?.beacon) return
    new Image().src = `${window.CARD_DIAG.beacon}?e=${encodeURIComponent(event)}&d=${encodeURIComponent(JSON.stringify(data).slice(0, 1500))}`
  }
  function report(event, data) {
    beacon(event, data)
    if (app.canCallTools()) app.callTool('report_card_event', { event, data: JSON.stringify(data).slice(0, 4000) }).catch(() => {})
  }
  function diag(res) {
    const ctx = res?.hostContext ?? {}
    report('init', {
      host: res?.hostInfo,
      caps: res?.hostCapabilities,
      theme: ctx.theme,
      displayMode: ctx.displayMode,
      modes: ctx.availableDisplayModes,
      platform: ctx.platform,
      dims: ctx.containerDimensions,
      safe: ctx.safeAreaInsets,
      vars: Object.keys(ctx.styles?.variables ?? {}).length,
      fonts: Boolean(ctx.styles?.css?.fonts),
      width: innerWidth,
    })
    const seen = new Set()
    el.addEventListener('load', (e) => e.target.tagName === 'IMG' && !seen.has(e.target.src) && (seen.add(e.target.src), report('img_ok', { src: e.target.src.slice(0, 120) })), true)
    el.addEventListener('error', (e) => e.target.tagName === 'IMG' && report('img_error', { src: e.target.src.slice(0, 120) }), true)
    const ctxUpdate = app.updateModelContext
    app.updateModelContext = (text) =>
      ctxUpdate(text).then(
        (r) => (report('ctx_ok', { len: text.length }), r),
        (err) => (report('ctx_error', { err: String(err?.message ?? err) }), Promise.reject(err)),
      )
    const open = app.openLink
    app.openLink = (url) =>
      open(url).then(
        (r) => (report('open_ok', { url }), r),
        (err) => (report('open_error', { url, err: String(err?.message ?? err) }), Promise.reject(err)),
      )
    const mode = app.requestDisplayMode
    app.requestDisplayMode = (m) => mode(m).then((r) => (report('mode', { asked: m, got: r?.mode }), r))
  }

  return {
    async mount(name) {
      render()
      app.on('ui/notifications/tool-result', (r) => {
        s = fromResult(r)
        render()
        schedule()
      })
      app.on('ui/notifications/tool-input', (p) => (input = p?.arguments ?? {}))
      app.on('ui/notifications/host-context-changed', render)
      const res = await app.connect({ name, modes: ['inline', 'fullscreen'] })
      offline = !app.canCallTools()
      render()
      if (window.CARD_DIAG) diag(res)
    },
  }
})()
