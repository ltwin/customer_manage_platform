// Browser regression for V6-059 (canvas save status). All API traffic is mocked;
// the command endpoint is held, confirmed or refused per AC to drive each state.
// Run against the Vite dev server: node scripts/creative-save-status.e2e.mjs.
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const browser = await chromium.launch({ headless: true })
const errors = []
// 每条 AC 用新的 browser context：本机草稿/outbox 不跨 AC 残留。
let context, page, server

const caps = ['move', 'resize', 'rename', 'duplicate', 'remove', 'edit', 'reference']
const makeNode = (id, x) => ({
  id,
  parent_id: null,
  metadata: { type_key: 'core.text', type_version: 1, title: id, intent: '', x, y: 160, width: 280, height: 180, z_order: 0 },
  prompt: null,
  capabilities: { actions: caps, prompt_mode: 'draft', disabled_reason: null },
  status: { content_state: 'empty', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
  placement_revision: '1',
  data_revision: '1',
  data: { schema_version: 1, config: {}, content_id: null, content_revision_id: null, selected_version_id: null, document_id: null },
})
const fixture = () => ({
  id: 'qa',
  project_id: 'qa-project',
  project_name: '保存状态',
  project_revision: '1',
  revision: '1',
  topology_revision: '1',
  archived: false,
  nodes: [makeNode('a', 100), makeNode('b', 560)],
  edges: [],
  node_inputs: [],
  changes: [],
  object_states: [],
})

async function fresh() {
  await context?.close()
  context = await browser.newContext({ viewport: { width: 1600, height: 1000 } })
  page = await context.newPage()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
  const canvas = fixture()
  // mode：hold = 等 release() 后回执成功；conflict = 409 另一窗口先保存。
  server = { mode: 'hold', posts: [], release: null, canvas }
  await page.addInitScript(() => {
    const original = window.fetch.bind(window)
    window.fetch = async (input, init) => {
      if (!String(input).endsWith('/canvases/qa/events')) return original(input, init)
      return new Response(new ReadableStream({
        start(controller) {
          controller.enqueue(new TextEncoder().encode('event: invalidate\ndata: {}\n\n'))
          init.signal.addEventListener('abort', () => controller.error(new DOMException('Aborted', 'AbortError')), { once: true })
        },
      }), { headers: { 'Content-Type': 'text/event-stream' } })
    }
  })
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    const json = (value, status = 200) => route.fulfill({ status, json: value })
    if (path.endsWith('/auth/refresh')) return json({ access_token: 'qa', token_type: 'Bearer', expires_in: 600 })
    if (path.endsWith('/me')) return json({ id: 'qa-save-status', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
    if (path.endsWith('/generation/catalog')) return json({ models: [], disabled_reason: '未配置生成模型' })
    if (path.endsWith('/versions')) return json({ items: [], selected_version_id: null, data_revision: '1' })
    if (path.endsWith('/canvases/qa')) return json(canvas)
    if (path.includes('/operations/')) return json({ error: { code: 'not_found', message: 'no receipt' } }, 404)
    if (path.endsWith('/commands')) {
      const actions = route.request().postDataJSON()?.payload?.actions ?? []
      server.posts.push(actions)
      if (server.mode === 'conflict')
        return json({ error: { code: 'creative_revision_conflict', message: '画布已在其他窗口更新' } }, 409)
      await new Promise((resolve) => { server.release = resolve })
      server.release = null
      const next = String(Number(canvas.revision) + 1)
      for (const a of actions) {
        const n = canvas.nodes.find((n) => n.id === a.node_id)
        if (a.type === 'move_node' && n) Object.assign(n.metadata, { x: a.x, y: a.y })
      }
      canvas.revision = next
      return json({ change_id: `change-${next}`, result_revision: next, result_topology_revision: canvas.topology_revision, object_results: [], created_ids: [] })
    }
    if (path.endsWith('/library-settings')) return json({ revision: '1', view_mode: 'grid', sort: 'updated_desc', thumbnail_size: 'medium' })
    if (path.endsWith('/media-capabilities')) return json({ formats: [], image_max_bytes: 1, av_max_bytes: 1, batch_limit: 10 })
    return json({ items: [], next_cursor: '', total_count: 0, library_revision: '1', hierarchy_revision: '1' })
  })
  await page.goto(`${base}/creative/canvases/qa`)
  await page.locator('.react-flow__node[data-id="b"]').waitFor()
}

const status = page_ => page_.locator('.cc-header .cc-save-state')
const statusText = async () => ((await status(page).count()) ? (await status(page).textContent()) : '')
const waitStatus = (text) => page.waitForFunction(
  (t) => document.querySelector('.cc-header .cc-save-state')?.textContent === t, text, { timeout: 4000 })
const header = (id) => page.locator(`.react-flow__node[data-id="${id}"] .cc-node-header h3`)
const nudge = async (id) => { await header(id).click(); await page.keyboard.press('ArrowRight') }
const waitPosts = async (n) => {
  for (let i = 0; i < 80 && server.posts.length < n; i++) await page.waitForTimeout(50)
  assert.ok(server.posts.length >= n, `expected ${n} command posts, got ${server.posts.length}`)
}
const goOffline = (off) => page.evaluate((v) => {
  Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => !v })
  window.dispatchEvent(new Event(v ? 'offline' : 'online'))
}, off)
const undoState = () => page.evaluate(() => ({
  undo: document.querySelector('button[aria-label="撤销"]')?.disabled,
  redo: document.querySelector('button[aria-label="重做"]')?.disabled,
}))

const results = {}
let step = ''
const at = (label) => { step = label }
const only = process.env.ONLY?.split(',')
const record = async (ac, fn) => {
  if (only && !only.includes(ac)) return
  step = ''
  try {
    await fresh()
    await fn()
    results[ac] = 'pass'
  } catch (error) {
    results[ac] = `FAIL${step ? ` @${step}` : ''}: ${String(error.message).split('\n')[0]}`
  }
}

try {
  // AC-01 有未确认修改显示「保存中」，回执到达后才显示「已保存」。
  await record('AC-01', async () => {
    at('fresh canvas')
    assert.equal(await statusText(), '', 'no claim before any edit')
    at('saving')
    await nudge('a')
    await waitStatus('保存中')
    await page.locator('.cc-header').screenshot({ path: '/tmp/v6-059-saving.png' })
    await waitPosts(1)
    await page.waitForTimeout(600)
    assert.equal(await statusText(), '保存中', 'held request stays 保存中')
    at('saved')
    server.release()
    await waitStatus('已保存')
    await page.locator('.cc-header').screenshot({ path: '/tmp/v6-059-saved.png' })
    at('second edit')
    await nudge('b')
    await waitStatus('保存中')
    await waitPosts(2)
    server.release()
    await waitStatus('已保存')
    assert.equal(await status(page).getAttribute('role'), 'status')
  })

  // AC-02 断网显示离线并保留本机修改，恢复后自动提交并回到「已保存」。
  await record('AC-02', async () => {
    await goOffline(true)
    at('offline edit')
    await nudge('a')
    await waitStatus('离线 · 修改保留在本机')
    await page.waitForTimeout(600)
    assert.equal(server.posts.length, 0, 'nothing sent while offline')
    const moved = await page.locator('.react-flow__node[data-id="a"]').evaluate((el) => el.style.transform)
    at('back online')
    await goOffline(false)
    await waitPosts(1)
    assert.equal(server.posts[0][0]?.type, 'move_node', 'the offline edit is the one submitted')
    await waitStatus('保存中')
    server.release()
    await waitStatus('已保存')
    assert.equal(await page.locator('.react-flow__node[data-id="a"]').evaluate((el) => el.style.transform), moved, 'edit kept')
  })

  // AC-03 另一窗口先保存：状态说明冲突而不是已保存，正文草稿保留。
  await record('AC-03', async () => {
    at('first save ok')
    await nudge('a')
    await waitPosts(1)
    server.release()
    await waitStatus('已保存')
    at('conflict')
    server.mode = 'conflict'
    await page.locator('.react-flow__node[data-id="b"] .cc-node-content').dblclick()
    const editor = page.getByRole('textbox', { name: '正文', exact: true })
    await editor.waitFor()
    await editor.fill('本窗口的正文')
    await editor.press('Escape')
    await waitStatus('保存冲突 · 待处理')
    await page.waitForTimeout(400)
    assert.equal(await statusText(), '保存冲突 · 待处理', 'never falls back to 已保存')
    at('draft kept')
    // 服务端保留另一窗口的内容；本窗口的正文留在本机草稿里，由摄影师决定。
    await page.getByRole('button', { name: '关闭被拒绝的请求，保留草稿', exact: true }).waitFor()
    assert.equal(server.posts.length, 2, 'the refused save is not retried silently')
  })

  // AC-04 状态只读、不抢焦点、不进撤销栈。
  await record('AC-04', async () => {
    const before = await undoState()
    await nudge('a')
    await waitStatus('保存中')
    const focused = await page.evaluate(() => document.activeElement?.className ?? '')
    assert.ok(!String(focused).includes('cc-save-state'), 'status never takes focus')
    assert.equal(await status(page).evaluate((el) => getComputedStyle(el).pointerEvents), 'none')
    await waitPosts(1)
    server.release()
    await waitStatus('已保存')
    at('undo is the edit only')
    const after = await undoState()
    assert.equal(before.undo, true)
    assert.equal(after.undo, false, 'the edit is undoable')
    await page.keyboard.press('Control+z')
    await waitPosts(2)
    server.release()
    await waitStatus('已保存')
    assert.equal((await undoState()).undo, true, 'one edit, one undo step; status adds none')
  })

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify({ results }, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!process.env.ONLY && Object.keys(results).length !== 4)) process.exitCode = 1
