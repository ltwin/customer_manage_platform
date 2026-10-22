// 宿主一侧的 MCP Apps 桥：把卡片装进沙箱 iframe，应答 ui/initialize，推送工具结果，
// 把卡片发起的 tools/call 转给（假的）MCP 服务。真实宿主做的事更多（预取、CSP、许可），这里只模拟走查需要的部分。
const APP_TOOLS = new Set(['generate_node', 'wait_tasks', 'save_to_library']) // _meta.ui.visibility 含 "app" 的工具

// Claude 公布的宿主变量（design-guidelines 的表格）；ChatGPT 的值是近似，待活原型核对。
const CLAUDE = {
  light: {
    'color-background-primary': '#ffffff', 'color-background-secondary': '#f5f4ed', 'color-background-tertiary': '#faf9f5',
    'color-background-inverse': '#141413', 'color-text-primary': '#141413', 'color-text-secondary': '#3d3d3a',
    'color-text-tertiary': '#73726c', 'color-text-inverse': '#ffffff', 'color-border-primary': 'rgba(31,30,29,.4)',
    'color-border-tertiary': 'rgba(31,30,29,.15)', 'font-sans': '"Anthropic Sans", system-ui, -apple-system, "PingFang SC", sans-serif',
  },
  dark: {
    'color-background-primary': '#30302e', 'color-background-secondary': '#262624', 'color-background-tertiary': '#141413',
    'color-background-inverse': '#faf9f5', 'color-text-primary': '#faf9f5', 'color-text-secondary': '#c2c0b6',
    'color-text-tertiary': '#9c9a92', 'color-text-inverse': '#141413', 'color-border-primary': 'rgba(222,220,209,.4)',
    'color-border-tertiary': 'rgba(222,220,209,.15)', 'font-sans': '"Anthropic Sans", system-ui, -apple-system, "PingFang SC", sans-serif',
  },
}
const CHATGPT = {
  light: {
    'color-background-primary': '#ffffff', 'color-background-secondary': '#f3f3f3', 'color-background-tertiary': '#f9f9f9',
    'color-background-inverse': '#0d0d0d', 'color-text-primary': '#0d0d0d', 'color-text-secondary': '#5d5d5d',
    'color-text-tertiary': '#8f8f8f', 'color-text-inverse': '#ffffff', 'color-border-primary': 'rgba(0,0,0,.15)',
    'color-border-tertiary': 'rgba(0,0,0,.08)', 'font-sans': 'system-ui, -apple-system, "PingFang SC", sans-serif',
    'border-radius-md': '12px', 'border-radius-sm': '8px',
  },
  dark: {
    'color-background-primary': '#212121', 'color-background-secondary': '#303030', 'color-background-tertiary': '#2a2a2a',
    'color-background-inverse': '#ffffff', 'color-text-primary': '#ececec', 'color-text-secondary': '#b4b4b4',
    'color-text-tertiary': '#8f8f8f', 'color-text-inverse': '#0d0d0d', 'color-border-primary': 'rgba(255,255,255,.18)',
    'color-border-tertiary': 'rgba(255,255,255,.08)', 'font-sans': 'system-ui, -apple-system, "PingFang SC", sans-serif',
    'border-radius-md': '12px', 'border-radius-sm': '8px',
  },
}
// 卡片不申请摄像头、麦克风等 permissions，也不需要 connectDomains（动作都走 tools/call）
export const hostContext = (host, theme, mode = 'inline') => ({
  theme,
  displayMode: mode,
  availableDisplayModes: host === 'chatgpt' ? ['inline', 'fullscreen', 'pip'] : ['inline', 'fullscreen'],
  platform: 'desktop',
  locale: 'zh-CN',
  styles: { variables: (host === 'chatgpt' ? CHATGPT : CLAUDE)[theme] },
  safeAreaInsets: { top: 0, right: 0, bottom: mode === 'fullscreen' ? 112 : 0, left: 0 },
})

/**
 * @param {HTMLElement} box 放卡片的容器
 * @param {object} o
 *   resource: 'tasks' | 'shots'；result: 工具结果；args: 工具参数；server: MockServer
 *   context(): 当前宿主上下文；onModelContext(text)；onOpenLink(url)；onDisplayMode(mode) => 实际模式；onToolCall(name, args, result)
 */
export function mountWidget(box, o) {
  const iframe = document.createElement('iframe')
  iframe.sandbox = 'allow-scripts' // 不同源：卡片碰不到宿主页面、cookie 和存储
  iframe.title = o.resource === 'tasks' ? '生成与任务卡' : '镜头卡'
  iframe.src = new URL(`../widgets/${o.resource}.html`, import.meta.url).href
  iframe.className = 'widget-frame'
  iframe.style.colorScheme = o.context().theme // 和卡片的 color-scheme 一致，iframe 背景才是透明的
  box.append(iframe)

  let ready = false
  const send = (msg) => iframe.contentWindow?.postMessage({ jsonrpc: '2.0', ...msg }, '*')

  async function onMessage(e) {
    if (e.source !== iframe.contentWindow) return
    const m = e.data
    if (!m || m.jsonrpc !== '2.0' || !m.method) return
    const reply = (result) => send({ id: m.id, result })
    const fail = (message) => send({ id: m.id, error: { code: -32000, message } })
    switch (m.method) {
      case 'ui/initialize':
        return reply({
          protocolVersion: '2026-01-26',
          hostInfo: { name: o.hostName, version: 'prototype' },
          hostCapabilities: { openLinks: {}, serverTools: {}, updateModelContext: { text: {} } },
          hostContext: o.context(),
        })
      case 'ui/notifications/initialized':
        ready = true
        send({ method: 'ui/notifications/tool-input', params: { arguments: o.args } })
        send({ method: 'ui/notifications/tool-result', params: o.result })
        return
      case 'ui/notifications/size-changed':
        if (box.classList.contains('full')) return
        iframe.style.height = `${m.params.height}px`
        o.onResize?.()
        return
      case 'tools/call': {
        if (!APP_TOOLS.has(m.params.name)) return fail(`工具 ${m.params.name} 不对卡片开放`)
        const r = await o.server.call(m.params.name, m.params.arguments)
        o.onToolCall?.(m.params.name, m.params.arguments, r)
        return reply(r)
      }
      case 'ui/update-model-context':
        o.onModelContext?.((m.params.content ?? []).map((c) => c.text).join('\n'))
        return reply({})
      case 'ui/open-link':
        o.onOpenLink?.(m.params.url)
        return reply({})
      case 'ui/request-display-mode':
        return reply({ mode: o.onDisplayMode?.(m.params.mode) ?? 'inline' })
      default:
        return m.id != null ? fail(`原型未实现 ${m.method}`) : undefined
    }
  }
  addEventListener('message', onMessage)

  return {
    setContext(partial) {
      if (partial.theme) iframe.style.colorScheme = partial.theme
      if (ready) send({ method: 'ui/notifications/host-context-changed', params: partial })
    },
    destroy() {
      removeEventListener('message', onMessage)
      iframe.remove()
    },
  }
}
