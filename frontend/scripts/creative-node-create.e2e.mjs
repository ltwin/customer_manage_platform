// Browser regression for V6-006 (create entries, shortcuts, focus after create,
// double-click meaning). API is mocked; /commands applies add_node and returns a
// receipt so a new media node is confirmed and its composer unlocks.
// Run against the Vite dev server: node scripts/creative-node-create.e2e.mjs.
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const browser = await chromium.launch({ headless: true })
const errors = []
let context, page, server

const caps = (type) => type === 'core.group'
  ? ['move', 'rename', 'duplicate', 'remove', 'ungroup']
  : ['move', 'resize', 'rename', 'duplicate', 'remove', 'edit', 'reference', ...(type === 'core.text' ? [] : ['maximize', 'download', 'replace'])]
const size = { 'core.text': [300, 220], 'core.image': [300, 300], 'core.video': [300, 300], 'core.audio': [300, 120], 'core.group': [300, 220] }
const makeNode = (id, type, x, y, extra = {}) => ({
  id, parent_id: null,
  metadata: { type_key: type, type_version: 1, title: id, intent: '', x, y, width: size[type][0], height: size[type][1], z_order: 0 },
  prompt: null,
  capabilities: { actions: caps(type), prompt_mode: type === 'core.group' ? 'scope' : 'draft', disabled_reason: null },
  status: { content_state: 'empty', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
  placement_revision: '1', data_revision: '1',
  data: { schema_version: 1, config: {}, content_id: null, content_revision_id: null, selected_version_id: null, document_id: null },
  ...extra,
})
const art = `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="300"><rect width="300" height="300" fill="#666"/></svg>`
const fixture = () => ({
  id: 'qa', project_id: 'qa-project', project_name: '新建回归', project_revision: '1',
  revision: '1', topology_revision: '1', archived: false,
  nodes: [
    makeNode('src', 'core.text', 80, 120),
    makeNode('photo', 'core.image', 460, 120, {
      content: { id: 'r-photo', content_id: 'c-photo', kind: 'image', revision_no: '1', title: '晨光', payload: {}, truncated: false,
        media: [{ role: 'original', blob_id: 'b', mime: 'image/svg+xml', byte_size: 1, width: 300, height: 300, duration_ms: null }] },
      status: { content_state: 'ready', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
      data: { schema_version: 1, config: {}, content_id: 'c-photo', content_revision_id: 'r-photo', selected_version_id: null, document_id: null },
    }),
    makeNode('empty', 'core.image', 840, 120),
    makeNode('g', 'core.group', 80, 520, { metadata: { type_key: 'core.group', type_version: 1, title: '参考组', intent: '', x: 80, y: 520, width: 900, height: 420, z_order: 0 } }),
  ],
  edges: [], node_inputs: [], changes: [], object_states: [],
})

async function fresh() {
  await context?.close()
  context = await browser.newContext({ viewport: { width: 1600, height: 1000 } })
  page = await context.newPage()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
  const canvas = fixture()
  server = { canvas, posts: [], other: [] }
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
    const req = route.request()
    const path = new URL(req.url()).pathname
    const json = (value, status = 200) => route.fulfill({ status, json: value })
    if (path.endsWith('/auth/refresh')) return json({ access_token: 'qa', token_type: 'Bearer', expires_in: 600 })
    if (path.endsWith('/me')) return json({ id: 'qa-create', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
    if (path.endsWith('/generation-catalog')) return json({ models: [{ key: 'qa-model', label: '契约模型', kind: 'image', configuration_revision: '1', parameters: [] }], disabled_reason: '' })
    if (path.endsWith('/versions')) return json({ items: [], selected_version_id: null, data_revision: '1' })
    if (path.includes('/content-urls') || path.includes('/download-url') || path.includes('/media-url'))
      return json({ url: `data:image/svg+xml,${encodeURIComponent(art)}`, expires_at: new Date(Date.now() + 3600000).toISOString() })
    if (path.endsWith('/canvases/qa')) return json(canvas)
    if (path.endsWith('/commands')) {
      const actions = req.postDataJSON()?.payload?.actions ?? []
      server.posts.push(actions)
      for (const a of actions)
        if (a.type === 'add_node') canvas.nodes.push(makeNode(a.node_id, a.type_key, a.x, a.y, { parent_id: a.parent_id ?? null, metadata: { type_key: a.type_key, type_version: 1, title: a.title ?? '', intent: '', x: a.x, y: a.y, width: size[a.type_key][0], height: size[a.type_key][1], z_order: 0 } }))
      canvas.revision = canvas.topology_revision = String(Number(canvas.revision) + 1)
      return json({ change_id: `ch-${canvas.revision}`, result_revision: canvas.revision, result_topology_revision: canvas.topology_revision, object_results: [], created_ids: actions.filter((a) => a.type === 'add_node').map((a) => a.node_id) })
    }
    if (req.method() !== 'GET') server.other.push(path)
    if (path.endsWith('/library-settings')) return json({ revision: '1', view_mode: 'grid', sort: 'updated_desc', thumbnail_size: 'medium' })
    if (path.endsWith('/media-capabilities')) return json({ formats: [{ kind: 'image', mime: 'image/png', extensions: ['.png'] }], image_max_bytes: 10485760, av_max_bytes: 104857600, batch_limit: 10 })
    return json({ items: [], next_cursor: '', total_count: 0, library_revision: '1', hierarchy_revision: '1' })
  })
  await page.goto(`${base}/creative/canvases/qa`)
  await page.locator('.react-flow__node[data-id="g"]').waitFor()
  await page.waitForTimeout(300)
}

const node = (id) => page.locator(`.react-flow__node[data-id="${id}"]`)
const ids = () => page.locator('.react-flow__node').evaluateAll((els) => els.map((e) => e.getAttribute('data-id')))
const selected = () => page.locator('.react-flow__node.selected').evaluateAll((els) => els.map((e) => e.getAttribute('data-id')))
const waitNew = async (before) => {
  for (let i = 0; i < 80; i++) {
    const now = (await ids()).filter((id) => !before.includes(id))
    if (now.length) return now[0]
    await page.waitForTimeout(50)
  }
  throw new Error('no new node')
}
const active = () => page.evaluate(() => {
  const el = document.activeElement
  return el ? (el.getAttribute('aria-label') ?? el.tagName) : ''
})
const textEditor = () => page.getByRole('textbox', { name: '正文', exact: true })
const prompt = () => page.getByRole('textbox', { name: '提示词', exact: true })
const blank = { x: 1450, y: 900 }
const menuItems = (menu) => menu.getByRole('menuitem').evaluateAll((els) => els.map((e) => e.getAttribute('aria-label') ?? e.textContent.trim()))

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
const promptBox = prompt
const activeLabel = active
const selectedIDs = selected

try {
  // AC-01 四个入口打开同一个菜单；端口菜单标题与文本上游说明。
  await record('AC-01', async () => {
    const menu = page.getByRole('menu', { name: '添加节点' })
    const expected = ['文字', '图片', '视频', '音频', '上传文件', '从资产库添加']
    at('rail')
    await page.getByRole('button', { name: '新建节点', exact: true }).click()
    assert.deepEqual(await menuItems(menu), expected)
    await page.keyboard.press('Escape')
    at('double click blank')
    await page.mouse.dblclick(blank.x, blank.y)
    assert.deepEqual(await menuItems(menu), expected)
    await page.keyboard.press('Escape')
    at('right click blank')
    await page.mouse.click(blank.x, blank.y, { button: 'right' })
    const ctx = page.getByRole('menu', { name: '画布右键菜单' })
    const ctxItems = await menuItems(ctx)
    assert.deepEqual(ctxItems.slice(0, 6), expected, 'context menu starts with the same add items')
    await page.keyboard.press('Escape')
    at('output port title')
    await node('src').locator('.cc-node-header h3').click()
    await node('src').locator('.cc-port-output button').click()
    const portMenu = page.getByRole('menu', { name: '创建并连接节点' })
    assert.equal(await portMenu.locator('.cc-context-section').textContent(), '生成下游节点')
    assert.equal(await portMenu.getByRole('menuitem', { name: '图片', exact: true }).locator('small').textContent(), '以上游文本为提示词')
    await page.keyboard.press('Escape')
    at('input port title')
    await node('empty').locator('.cc-node-header h3').click()
    await node('empty').locator('.cc-port-input button').click()
    assert.equal(await portMenu.locator('.cc-context-section').textContent(), '添加上游参考')
    assert.equal(await portMenu.getByRole('menuitem', { name: '图片', exact: true }).locator('small').count(), 0, 'no text hint from an input port')
  })

  // AC-02 新建后只选中新节点；文本立即编辑、媒体把焦点交给生成输入框；不自动入组。
  await record('AC-02', async () => {
    at('text')
    await page.mouse.dblclick(blank.x, blank.y)
    await page.getByRole('menu', { name: '添加节点' }).getByRole('menuitem', { name: '文字', exact: true }).click()
    await textEditor().waitFor()
    assert.equal(await activeLabel(), '正文', 'new text node is being edited')
    assert.equal((await selectedIDs()).length, 1, 'only the new node is selected')
    await textEditor().press('Escape')
    at('image focuses the composer after confirmation')
    await page.mouse.dblclick(blank.x, blank.y - 300)
    await page.getByRole('menu', { name: '添加节点' }).getByRole('menuitem', { name: '图片', exact: true }).click()
    await page.waitForFunction(() => document.activeElement?.getAttribute('aria-label') === '提示词', null, { timeout: 5000 })
    const [created] = await selectedIDs()
    assert.ok(created && !['src', 'photo', 'empty', 'g'].includes(created), 'new image is the only selection')
    at('inside a group box does not join the group')
    await page.mouse.click(blank.x, blank.y)
    const gbox = await node('g').boundingBox()
    await page.mouse.dblclick(gbox.x + gbox.width / 2, gbox.y + gbox.height / 2)
    await page.getByRole('menu', { name: '添加节点' }).getByRole('menuitem', { name: '视频', exact: true }).click()
    await page.waitForFunction(() => document.activeElement?.getAttribute('aria-label') === '提示词', null, { timeout: 5000 })
    const added = server.posts.flat().filter((a) => a.type === 'add_node').at(-1)
    assert.equal(added?.type_key, 'core.video')
    assert.ok(!added.parent_id, 'new node is not placed into the group')
  })

  // AC-03 T/I/O/A 在屏幕中心新建；输入聚焦或按修饰键时不触发。
  await record('AC-03', async () => {
    const flow = await page.locator('.react-flow').boundingBox()
    const center = { x: flow.x + flow.width / 2, y: flow.y + flow.height / 2 }
    for (const [key, type] of [['t', 'core.text'], ['i', 'core.image'], ['o', 'core.video'], ['a', 'core.audio']]) {
      at(`key ${key}`)
      await page.mouse.click(blank.x, blank.y)
      const before = await ids()
      await page.keyboard.press(key)
      await page.waitForFunction((n) => document.querySelectorAll('.react-flow__node').length === n, before.length + 1)
      const [id] = (await ids()).filter((v) => !before.includes(v))
      const box = await node(id).locator('.cc-node').boundingBox()
      assert.ok(Math.abs(box.x + box.width / 2 - center.x) < 3 && Math.abs(box.y + box.height / 2 - center.y) < 3, `${key} creates at screen centre`)
      let action
      for (let i = 0; i < 60 && !(action = server.posts.flat().find((a) => a.type === 'add_node' && a.node_id === id)); i++) await page.waitForTimeout(50)
      assert.equal(action?.type_key, type, `${key} creates ${type}`)
      if (type === 'core.text') await textEditor().press('Escape')
    }
    at('modifier does not create')
    await page.mouse.click(blank.x, blank.y)
    const before = (await ids()).length
    await page.keyboard.press('Meta+i')
    await page.keyboard.press('Alt+t')
    await page.waitForTimeout(300)
    assert.equal((await ids()).length, before, 'modifier + key creates nothing')
    at('typing does not create')
    await node('photo').locator('.cc-node-header h3').click()
    await promptBox().click()
    await page.keyboard.type('tioa')
    await page.waitForTimeout(300)
    assert.equal((await ids()).length, before, 'typing in the composer creates nothing')
  })

  // AC-04 双击按位置区分。
  await record('AC-04', async () => {
    at('title renames')
    await node('src').locator('.cc-node-header h3').dblclick()
    await page.getByRole('textbox', { name: '节点名称', exact: true }).waitFor()
    await page.keyboard.press('Escape')
    at('text edits')
    await node('src').locator('.cc-node-content').dblclick()
    await textEditor().waitFor()
    await textEditor().press('Escape')
    at('filled media goes fullscreen')
    await node('photo').locator('.cc-node-media').dblclick()
    const preview = page.getByRole('dialog', { name: /媒体预览/ })
    await preview.waitFor()
    await page.keyboard.press('Escape')
    await preview.waitFor({ state: 'detached' })
    at('empty media focuses the composer only')
    const chooser = []
    page.on('filechooser', (c) => chooser.push(c))
    const posts = server.posts.length
    await node('empty').locator('.cc-media-empty').dblclick({ position: { x: 20, y: 20 } })
    await page.waitForFunction(() => document.activeElement?.getAttribute('aria-label') === '提示词')
    await page.waitForTimeout(300)
    assert.equal(chooser.length, 0, 'no upload dialog')
    assert.equal(server.posts.length, posts, 'no command sent')
    assert.ok(!server.other.some((p) => p.endsWith('/executions')), 'no generation started')
    at('blank opens the add menu')
    await page.mouse.dblclick(blank.x, blank.y)
    await page.getByRole('menu', { name: '添加节点' }).waitFor()
  })

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify({ results }, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!process.env.ONLY && Object.keys(results).length !== 4)) process.exitCode = 1
