// 参考宿主：用 ext-apps 官方的 AppBridge 加载我们的卡片，验证卡片的握手和消息与标准实现兼容。
// 不模仿任何真实宿主的外观；真实宿主的表现要在 Claude 桌面端里看。
// 构建：node refhost/build.mjs，然后打开 http://127.0.0.1:8774/p/creative-mcp/live/refhost/index.html
import { Client, StreamableHTTPClientTransport } from '@modelcontextprotocol/client'
import { AppBridge, PostMessageTransport } from '@modelcontextprotocol/ext-apps/app-bridge'

const log = (...a) => {
  const li = document.createElement('li')
  li.textContent = a.map((x) => (typeof x === 'string' ? x : JSON.stringify(x))).join(' ')
  document.getElementById('log').append(li)
}
const q = new URLSearchParams(location.search)
const theme = q.get('theme') === 'dark' ? 'dark' : 'light'
// ?notools=1：不声明 serverTools，模拟卡片不能调用工具的宿主
const hostCaps = { openLinks: {}, updateModelContext: { text: {} }, ...(q.get('notools') ? {} : { serverTools: {} }) }

const client = new Client(
  { name: 'refhost', version: '0' },
  { capabilities: { extensions: { 'io.modelcontextprotocol/ui': { mimeTypes: ['text/html;profile=mcp-app'] } } } },
)
await client.connect(new StreamableHTTPClientTransport(new URL('/mcp', location.origin)))
log('已连接，工具数', (await client.listTools()).tools.length)

async function show(name, args) {
  const { tools } = await client.listTools()
  const uri = tools.find((t) => t.name === name)?._meta?.ui?.resourceUri
  const result = await client.callTool({ name, arguments: args })
  log(`调用 ${name}`, result.content[0].text.split('\n')[0])
  if (!uri) return
  const res = await client.readResource({ uri })
  const iframe = document.createElement('iframe')
  iframe.sandbox = 'allow-scripts'
  iframe.style.colorScheme = theme
  iframe.srcdoc = res.contents[0].text
  document.getElementById('cards').append(iframe)
  const bridge = new AppBridge(client, { name: 'refhost', version: '0' }, hostCaps, {
    hostContext: { theme, displayMode: 'inline', availableDisplayModes: ['inline', 'fullscreen'], platform: 'web', locale: 'zh-CN' },
  })
  bridge.oninitialized = () => {
    log(`卡片 ${uri} 已初始化`)
    bridge.sendToolInput({ arguments: args })
    bridge.sendToolResult(result)
  }
  bridge.onsizechange = ({ height }) => height && (iframe.style.height = `${height}px`)
  bridge.onupdatemodelcontext = async (p) => {
    log('上下文 →', p.content?.map((c) => c.text).join(' '))
    return {}
  }
  bridge.onopenlink = async ({ url }) => {
    log('打开链接', url)
    return {}
  }
  bridge.onrequestdisplaymode = async ({ mode }) => {
    log('请求显示模式', mode)
    return { mode: 'inline' }
  }
  await bridge.connect(new PostMessageTransport(iframe.contentWindow, iframe.contentWindow))
}

await show('generate_node', { canvas_id: 'cv_9f2a', node_id: 'shot-2', count: 2, dry_run: true })
await show('generate_node', { canvas_id: 'cv_9f2a', node_id: 'shot-2', count: 2 })
await show('render_view', { node_id: 'scene-1' })
await show('export_shots', { canvas_id: 'cv_9f2a', node_id: 'scene-1' })
