// 外部助手活原型（D13）：假数据 MCP 服务，接进本机的 Claude Code、Codex CLI、Claude 桌面端，
// 核实卡片和结果文字在真实宿主里的样子（design U1、U7、U9–U11）。一次性代码，不进生产。
//
// node server.mjs          HTTP，给 Claude Code、Codex CLI：http://127.0.0.1:8774/mcp
// node server.mjs --stdio  stdio，给 Claude 桌面端；另在 8775 端口提供页面和图片
//
// 工具、结果文字和卡片全部复用静态原型（../shared/results.mjs、../widgets/），只是换成真实的 MCP 传输。
// 不鉴权、不连数据库；每次请求、每个卡片事件都记到 logs/ 下的 JSONL，之后据此整理结论。
import http from 'node:http'
import { randomUUID } from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { McpServer } from '@modelcontextprotocol/server'
import { StdioServerTransport } from '@modelcontextprotocol/server/stdio'
import { NodeStreamableHTTPServerTransport } from '@modelcontextprotocol/node'
import { registerAppTool, registerAppResource, RESOURCE_MIME_TYPE, getUiCapability } from '@modelcontextprotocol/ext-apps/server'
import { z } from 'zod'

const STDIO = process.argv.includes('--stdio')
const PORT = Number(process.env.PORT ?? (STDIO ? 8775 : 8774))
const ORIGIN = `http://127.0.0.1:${PORT}`
const HERE = path.dirname(fileURLToPath(import.meta.url))
const PROTOTYPES = path.resolve(HERE, '../..') // docs/prototypes
const PHOTOS = path.join(PROTOTYPES, 'creative-workspace/v4/assets')
const GEN_MS = Number(process.env.GEN_SECONDS ?? 25) * 1000 // 生成约 25 秒，比真实略长，能看到「进行中」
const RENDER_MS = 6000
const NO_STRUCTURED = process.env.NO_STRUCTURED === '1' // 对照实验：不返回 structuredContent（U9）
// 卡片里的图片怎么给（U11）：
// - url（默认）：本机 http 地址，CSP resourceDomains 声明本机；
// - data：直接内联成 data: URI，不走网络，用来确认问题出在网络还是卡片本身；
// - https：换成公网 https 图床（picsum.photos，图不一样），看宿主是不是只拦本机或 http。
const PHOTO_MODE = process.env.PHOTO_MODE ?? 'url'

// results.mjs 读取这两个变量，链接和图片换成本机地址；必须在导入之前设置
process.env.CREATIVE_ORIGIN = ORIGIN
process.env.CREATIVE_PHOTO_BASE = `${ORIGIN}/p/creative-workspace/v4/assets/`
const R = await import('../shared/results.mjs')

// ---------- 日志 ----------
fs.mkdirSync(path.join(HERE, 'logs'), { recursive: true })
const LOG = path.join(HERE, 'logs', `live-${new Date().toISOString().slice(0, 10)}.jsonl`)
function log(entry) {
  const line = JSON.stringify({ ts: new Date().toISOString(), mode: STDIO ? 'stdio' : 'http', ...entry })
  fs.appendFileSync(LOG, `${line}\n`)
  process.stderr.write(`${line.slice(0, 300)}\n`) // stdio 模式下 stdout 是协议通道，只写 stderr
}

// ---------- 假任务（进程内，所有会话共用） ----------
const tasks = new Map()
let pageOpenedAt = 0
let seq = 0
function advance(t) {
  const now = Date.now()
  if (t.kind === 'generate') {
    t.progress = Math.min(1, (now - t.startedAt) / GEN_MS)
    t.status = t.progress >= 1 ? 'succeeded' : 'running'
  } else if (!pageOpenedAt) {
    t.status = 'waiting_for_page'
  } else {
    t.progress = Math.min(1, (now - Math.max(pageOpenedAt, t.startedAt)) / RENDER_MS)
    t.status = t.progress >= 1 ? 'succeeded' : 'running'
  }
  return t
}
function newTask(kind) {
  const id = `${kind === 'generate' ? 'cwexec' : 'csjob'}_${(++seq).toString(36)}${randomUUID().slice(0, 4)}`
  const t = { id, kind, status: 'queued', progress: 0, startedAt: Date.now() }
  tasks.set(id, t)
  return advance(t)
}
const terminal = (t) => t.status === 'succeeded' || t.status === 'failed'

// 按客户端自报的名字识别宿主，用于署名和画布页的指示；认不出时显示原名
let lastHost = { id: 'codex', name: 'Codex' }
function hostOf(clientName = '') {
  const n = clientName.toLowerCase()
  if (n.includes('codex')) return { id: 'codex', name: 'Codex' }
  if (n.includes('claude-code') || n.includes('claude code')) return { id: 'claude_code', name: 'Claude Code' }
  if (n.includes('claude')) return { id: 'claude', name: 'Claude' }
  return { id: 'codex', name: clientName || '外部助手' }
}

// 长轮询：任一任务状态变化或到时返回
async function waitFor(ids, timeoutS) {
  const deadline = Date.now() + Math.min(45, Math.max(1, timeoutS ?? 40)) * 1000
  const list = () => ids.map((id) => tasks.get(id)).filter(Boolean).map(advance)
  const before = list().map((t) => `${t.status}`).join()
  while (Date.now() < deadline) {
    const now = list()
    if (now.every(terminal) || now.map((t) => t.status).join() !== before) return now
    await new Promise((r) => setTimeout(r, 500))
  }
  return list()
}

// ---------- 结果 → MCP CallToolResult ----------
// 按契约：content 文字 = 摘要 + 空行 + 结构化结果 JSON；图片转成 base64 ImageContent；
// 卡片数据（_meta）只给声明了 MCP Apps 扩展的客户端。
function toResult(r, { ui }) {
  const text = r.content.find((c) => c.type === 'text')?.text ?? ''
  const images = r.content.filter((c) => c.type === 'image').slice(0, 4)
  const json = r.structuredContent ? `\n\n${JSON.stringify(r.structuredContent)}` : ''
  const out = {
    content: [
      { type: 'text', text: text + json },
      ...images.map((i) => ({ type: 'image', mimeType: 'image/jpeg', data: imageData(i.url) })),
    ],
  }
  if (r.structuredContent && !NO_STRUCTURED) out.structuredContent = r.structuredContent
  if (ui && r._meta) out._meta = photoUrls(r._meta)
  return out
}
// 按 PHOTO_MODE 改写 _meta 里的图片地址；结构化数据和文字里的链接不动
const PHOTO_BASE = process.env.CREATIVE_PHOTO_BASE
const PICSUM = { 2: 1018, 4: 1043, 5: 1027, 8: 1011, 10: 1039, 11: 1015 } // 本地图片编号 → picsum 图片编号
function photoUrls(v) {
  if (PHOTO_MODE === 'url') return v
  if (typeof v === 'string' && v.startsWith(PHOTO_BASE)) {
    const name = v.slice(PHOTO_BASE.length)
    if (PHOTO_MODE === 'data') return `data:image/jpeg;base64,${fs.readFileSync(path.join(PHOTOS, name)).toString('base64')}`
    return `https://picsum.photos/id/${PICSUM[Number(name.match(/\d+/)[0])] ?? 1015}/850/1275`
  }
  if (Array.isArray(v)) return v.map(photoUrls)
  if (v && typeof v === 'object') return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, photoUrls(x)]))
  return v
}
function imageData(url) {
  const name = url.split('/').pop()
  return fs.readFileSync(path.join(PHOTOS, name)).toString('base64')
}

// ---------- 卡片资源：把 widgets/ 内联成单个 HTML ----------
function cardHtml(kind) {
  const w = (f) => fs.readFileSync(path.join(PROTOTYPES, 'creative-mcp/widgets', f), 'utf8')
  return `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<style>${w('widget.css')}</style></head><body><main id="app"></main>
<script>window.CARD_DIAG = { beacon: '${ORIGIN}/diag' }</script><script>${w('bridge.js')}</script><script>${w('cards.js')}</script>
<script>Cards.mount('creative-${kind}')</script></body></html>`
}
const SERVER_URL = `${ORIGIN}/mcp`
const UI_META = {
  csp: { resourceDomains: [ORIGIN, ...(PHOTO_MODE === 'https' ? ['https://picsum.photos', 'https://fastly.picsum.photos'] : [])], connectDomains: [], frameDomains: [], baseUriDomains: [] },
  prefersBorder: true,
}

// ---------- 服务端 instructions：草稿 + 一个探针，看宿主有没有把它交给模型（U7） ----------
const INSTRUCTIONS = `${fs
  .readFileSync(path.join(PROTOTYPES, 'creative-mcp/instructions.md'), 'utf8')
  .replace(/^> .*\n/gm, '')
  .trim()}

## 活原型探针
调用 get_capabilities 时，带上参数 probe: "instructions-v1"。`

// ---------- 工具 ----------
const S = z.string()
function createServer(sessionTag) {
  const server = new McpServer(
    { name: 'creative-proto', title: '创意空间（活原型）', version: '0.1.0' },
    { capabilities: { tools: { listChanged: true }, resources: {} }, instructions: INSTRUCTIONS },
  )
  let client = {}
  let ui = false

  // 声明了 MCP Apps 扩展才挂卡片，和契约的「协商」一致
  server.server.oninitialized = () => {
    const caps = server.server.getClientCapabilities()
    client = server.server.getClientVersion() ?? {}
    ui = Boolean(getUiCapability(caps)?.mimeTypes?.includes(RESOURCE_MIME_TYPE))
    lastHost = hostOf(client.name)
    log({ session: sessionTag, event: 'initialized', client, ui, capabilities: caps })
    register()
  }

  const call = (name, fn) => async (args) => {
    const t0 = Date.now()
    try {
      const r = await fn(args ?? {})
      const out = toResult(r, { ui })
      log({ session: sessionTag, client: client.name, event: 'call', tool: name, args, ms: Date.now() - t0, textHead: out.content[0].text.split('\n')[0], images: out.content.length - 1, structured: Boolean(out.structuredContent), meta: Boolean(out._meta) })
      return out
    } catch (err) {
      log({ session: sessionTag, client: client.name, event: 'error', tool: name, args, err: String(err?.stack ?? err) })
      return { isError: true, content: [{ type: 'text', text: JSON.stringify({ code: 'internal', message: String(err?.message ?? err) }) }] }
    }
  }

  function tool(name, { visibility = ['model'], ...config }, fn, card) {
    const base = { title: R.TITLES[name] ?? config.title, annotations: { readOnlyHint: R.READ_ONLY.has(name) }, ...config }
    if (ui && card) {
      registerAppTool(server, name, { ...base, _meta: { ui: { resourceUri: card.uri, visibility: card.visibility ?? ['model', 'app'] } } }, call(name, fn))
    } else {
      server.registerTool(name, { ...base, ...(ui ? { _meta: { ui: { visibility } } } : {}) }, call(name, fn))
    }
  }
  const TASKS = { uri: 'ui://creative/tasks/v1' }
  const SHOTS = { uri: 'ui://creative/shots/v1', visibility: ['model'] }

  function register() {
    tool(
      'get_capabilities',
      { description: '确认连接的账号、剩余点数、可用模型，以及有没有打开的创意空间页面。每次会话第一次使用前调用一次。', inputSchema: z.object({ probe: S.optional().describe('活原型探针，按 instructions 填写') }) },
      (a) => {
        log({ session: sessionTag, client: client.name, event: 'probe', instructionsRead: a.probe === 'instructions-v1', probe: a.probe ?? null })
        return R.getCapabilities({ pageOpen: Boolean(pageOpenedAt), host: hostOf(client.name).name })
      },
    )
    tool('list_canvases', { description: '按最近编辑列出画布。', inputSchema: z.object({ query: S.optional(), limit: z.number().int().min(1).max(50).optional() }) }, () => R.listCanvases())
    tool('read_canvas', { description: '读取一张画布的节点和结构。', inputSchema: z.object({ canvas_id: S }) }, () => R.readCanvas())
    tool(
      'set_node_prompt',
      { description: '改写图片或视频节点的提示词。', inputSchema: z.object({ canvas_id: S, node_id: S, prompt: S }), annotations: { readOnlyHint: false } },
      () => R.setNodePrompt(),
    )
    tool(
      'generate_node',
      {
        description: '为节点生成图片，会扣点。dry_run: true 时只预检：不生成、不扣点，返回模型、参考和预计扣费。是否预检由你判断。',
        inputSchema: z.object({ canvas_id: S, node_id: S, count: z.number().int().min(1).max(4).optional(), dry_run: z.boolean().optional() }),
      },
      (a) => (a.dry_run ? R.generateDryRun() : R.generateSubmit(newTask('generate'))),
      TASKS,
    )
    tool(
      'wait_tasks',
      { description: '等待任务句柄，任一完成或到时即返回（单次最多 45 秒）。', inputSchema: z.object({ task_ids: z.array(S).min(1).max(8), timeout_s: z.number().int().min(1).max(45).optional() }), annotations: { readOnlyHint: true } },
      async (a) => {
        const list = await waitFor(a.task_ids, a.timeout_s)
        if (!list.length) throw new Error('任务不存在')
        return R.waitTasks(list, { waited: !list.every(terminal) })
      },
      TASKS,
    )
    tool('save_to_library', { description: '把生成结果存入资产库。', inputSchema: z.object({ task_id: S }), visibility: ['model', 'app'] }, () => R.saveToLibrary(R.GEN.count))
    tool('edit_scene', { description: '修改 3D 场景。', inputSchema: z.object({ canvas_id: S, node_id: S, ops: S.optional() }) }, () => R.editScene({ host: hostOf(client.name).name }))
    tool(
      'render_view',
      { description: '回看场景，由打开的创意空间页面渲染；没有页面时任务排队，结果里有要打开的链接。', inputSchema: z.object({ node_id: S, camera: S.optional() }) },
      () => R.renderView(newTask('render')),
      SHOTS,
    )
    tool('export_shots', { description: '把场景里的镜头导出到画布。', inputSchema: z.object({ canvas_id: S, node_id: S }) }, () => R.exportShots(), SHOTS)
    // 诊断：测宿主的工具超时（U7）
    tool(
      'probe_timeout',
      { title: '超时探针', description: '活原型诊断工具：等待指定秒数后返回，用来测宿主的工具超时。只在创作者要求时调用。', inputSchema: z.object({ seconds: z.number().int().min(1).max(600) }), annotations: { readOnlyHint: true } },
      async (a) => {
        await new Promise((r) => setTimeout(r, a.seconds * 1000))
        return { content: [{ type: 'text', text: `已等待 ${a.seconds} 秒。` }] }
      },
    )
    if (ui) {
      // 卡片诊断（只对卡片可见）
      tool(
        'report_card_event',
        { title: '卡片诊断', description: '活原型诊断：卡片回报宿主行为。', inputSchema: z.object({ event: S, data: S.optional() }), visibility: ['app'] },
        (a) => {
          log({ session: sessionTag, client: client.name, event: 'card', via: 'tool', card: a.event, data: a.data })
          return { content: [{ type: 'text', text: 'ok' }] }
        },
      )
      for (const kind of ['tasks', 'shots']) {
        const uri = `ui://creative/${kind}/v1`
        registerAppResource(server, kind === 'tasks' ? '生成与任务卡' : '镜头卡', uri, { mimeType: RESOURCE_MIME_TYPE, _meta: { ui: UI_META } }, async () => {
          log({ session: sessionTag, client: client.name, event: 'resource_read', uri })
          return { contents: [{ uri, mimeType: RESOURCE_MIME_TYPE, text: cardHtml(kind), _meta: { ui: UI_META } }] }
        })
      }
    }
  }
  return server
}

// ---------- 页面、图片、链接落地 ----------
const TYPES = { '.html': 'text/html; charset=utf-8', '.css': 'text/css', '.mjs': 'text/javascript', '.js': 'text/javascript', '.jpg': 'image/jpeg', '.svg': 'image/svg+xml', '.md': 'text/markdown; charset=utf-8', '.json': 'application/json' }
function serveStatic(req, res, url) {
  const rel = decodeURIComponent(url.pathname.slice('/p/'.length))
  const file = path.resolve(PROTOTYPES, rel)
  if (!file.startsWith(PROTOTYPES + path.sep) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) {
    res.writeHead(404).end('not found')
    return
  }
  res.writeHead(200, { 'content-type': TYPES[path.extname(file)] ?? 'application/octet-stream', 'access-control-allow-origin': '*' })
  fs.createReadStream(file).pipe(res)
}
// 结果文字里的画布链接：带 tasks=1 表示有任务等页面，打开就算页面认领了任务
function landCanvas(req, res, url) {
  if (url.searchParams.get('tasks')) pageOpenedAt ||= Date.now()
  log({ event: 'page_open', path: url.pathname + url.search })
  const q = new URLSearchParams(url.search)
  if (!q.get('state')) q.set('state', q.get('tasks') ? 'tasks' : 'handoff')
  if (!q.get('host')) q.set('host', lastHost.id)
  res.writeHead(302, { location: `/p/creative-mcp/web/canvas.html?${q}` }).end()
}

const sessions = new Map()
async function handleMcp(req, res) {
  let body
  if (req.method === 'POST') {
    const chunks = []
    for await (const c of req) chunks.push(c)
    try {
      body = JSON.parse(Buffer.concat(chunks).toString('utf8') || 'null')
    } catch {
      res.writeHead(400).end('bad json')
      return
    }
  }
  const sid = req.headers['mcp-session-id']
  let entry = sid ? sessions.get(sid) : undefined
  if (!entry) {
    if (sid) {
      res.writeHead(404, { 'content-type': 'application/json' }).end(JSON.stringify({ jsonrpc: '2.0', error: { code: -32001, message: 'session not found' }, id: null }))
      return
    }
    const tag = randomUUID().slice(0, 8)
    const transport = new NodeStreamableHTTPServerTransport({
      sessionIdGenerator: () => randomUUID(),
      onsessioninitialized: (id) => sessions.set(id, entry),
    })
    transport.onclose = () => transport.sessionId && sessions.delete(transport.sessionId)
    const server = createServer(tag)
    entry = { transport, server }
    await server.connect(transport)
  }
  await entry.transport.handleRequest(req, res, body)
}

const web = http.createServer(async (req, res) => {
  const url = new URL(req.url, ORIGIN)
  try {
    if (url.pathname === '/mcp' && !STDIO) return await handleMcp(req, res)
    if (url.pathname.startsWith('/p/')) return serveStatic(req, res, url)
    if (url.pathname.startsWith('/creative/canvases/')) return landCanvas(req, res, url)
    // 卡片诊断的图片信标：不依赖宿主转发工具调用，卡片能加载图片就能回报
    if (url.pathname === '/diag') {
      log({ event: 'card', via: 'beacon', card: url.searchParams.get('e'), data: url.searchParams.get('d') })
      return res.writeHead(204).end()
    }
    if (url.pathname === '/reset') {
      tasks.clear()
      pageOpenedAt = 0
      log({ event: 'reset' })
      return res.writeHead(200).end('已重置任务和页面状态')
    }
    res.writeHead(404).end('not found')
  } catch (err) {
    log({ event: 'http_error', path: url.pathname, err: String(err?.stack ?? err) })
    if (!res.headersSent) res.writeHead(500).end('internal error')
  }
})
web.listen(PORT, '127.0.0.1', () => log({ event: 'listening', origin: ORIGIN, mcp: STDIO ? 'stdio' : SERVER_URL, photoMode: PHOTO_MODE, noStructured: NO_STRUCTURED }))

if (STDIO) await createServer('stdio').connect(new StdioServerTransport())
