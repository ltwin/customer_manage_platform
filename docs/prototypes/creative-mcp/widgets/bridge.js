// 卡片一侧的 MCP Apps 通信（规范 2026-01-26）：经 postMessage 收发 JSON-RPC。
// 不依赖 SDK，正式实现可以换成 @modelcontextprotocol/ext-apps 的 App 类，线协议相同。
// 用普通 <script> 加载：卡片在沙箱 iframe（不同源）里，模块脚本会被跨源拦截。
window.McpApp = (() => {
  let seq = 0
  const pending = new Map()
  const listeners = {}
  const root = document.documentElement
  let context = {}

  const post = (msg) => parent.postMessage({ jsonrpc: '2.0', ...msg }, '*')
  const request = (method, params) =>
    new Promise((resolve, reject) => {
      const id = ++seq
      pending.set(id, { resolve, reject })
      post({ id, method, params })
    })
  const notify = (method, params) => post({ method, params })
  const emit = (method, params) => (listeners[method] ?? []).forEach((fn) => fn(params))

  // 宿主上下文：主题、标准 CSS 变量、显示模式、安全区。字段可能缺，缺的沿用 widget.css 的默认值。
  function apply(ctx = {}) {
    context = { ...context, ...ctx }
    if (ctx.theme) root.dataset.theme = ctx.theme
    if (ctx.displayMode) root.dataset.mode = ctx.displayMode
    for (const [k, v] of Object.entries(ctx.styles?.variables ?? {})) if (v) root.style.setProperty(`--${k}`, v)
    if (ctx.safeAreaInsets) root.style.setProperty('--safe-bottom', `${ctx.safeAreaInsets.bottom ?? 0}px`)
  }

  addEventListener('message', (e) => {
    if (e.source !== parent) return
    const m = e.data
    if (!m || m.jsonrpc !== '2.0') return
    if (m.id != null && !m.method) {
      const p = pending.get(m.id)
      if (!p) return
      pending.delete(m.id)
      if (m.error) p.reject(m.error)
      else p.resolve(m.result)
      return
    }
    if (m.method === 'ui/resource-teardown') return post({ id: m.id, result: {} })
    if (m.method === 'ui/notifications/host-context-changed') apply(m.params)
    emit(m.method, m.params)
  })

  return {
    async connect({ name, modes = ['inline'] }) {
      const res = await request('ui/initialize', {
        protocolVersion: '2026-01-26',
        appInfo: { name, version: '0.1.0' },
        appCapabilities: { availableDisplayModes: modes },
      })
      apply(res?.hostContext)
      notify('ui/notifications/initialized', {})
      // 行内卡片高度随内容，内容变了就告诉宿主
      let last = 0
      new ResizeObserver(() => {
        const h = Math.ceil(document.documentElement.scrollHeight)
        if (h !== last) notify('ui/notifications/size-changed', { width: innerWidth, height: (last = h) })
      }).observe(document.body)
      return res
    },
    on(method, fn) {
      ;(listeners[method] ??= []).push(fn)
    },
    get context() {
      return context
    },
    canUse: (mode) => (context.availableDisplayModes ?? []).includes(mode),
    callTool: (name, args) => request('tools/call', { name, arguments: args }),
    // 创作者在卡片里做的事要告诉外部助手，否则它下一轮可能重复提交。宿主可能等到下一条消息才交给模型。
    updateModelContext: (text) => request('ui/update-model-context', { content: [{ type: 'text', text }] }),
    openLink: (url) => request('ui/open-link', { url }),
    requestDisplayMode: (mode) => request('ui/request-display-mode', { mode }),
  }
})()
