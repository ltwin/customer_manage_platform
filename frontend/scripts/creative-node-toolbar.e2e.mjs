// Browser regression for V6-007 (selection toolbar, context menus, derive).
// API is mocked; /commands applies add_node/connect_reference and returns a
// receipt so derived nodes are confirmed and their composer unlocks.
// Run against the Vite dev server: node scripts/creative-node-toolbar.e2e.mjs.
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
const filled = (id, type, x, y) => makeNode(id, type, x, y, {
  content: { id: `r-${id}`, content_id: `c-${id}`, kind: type.slice(5), revision_no: '1', title: id, payload: {}, truncated: false,
    media: [{ role: 'original', blob_id: `b-${id}`, mime: type === 'core.video' ? 'video/mp4' : 'image/svg+xml', byte_size: 1, width: 300, height: 300, duration_ms: type === 'core.video' ? 4000 : null }] },
  status: { content_state: 'ready', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
  data: { schema_version: 1, config: {}, content_id: `c-${id}`, content_revision_id: `r-${id}`, selected_version_id: null, document_id: null },
  capabilities: { actions: [...caps(type), 'save_to_library'], prompt_mode: 'draft', disabled_reason: null },
})
const text = (id, x, y, body) => makeNode(id, 'core.text', x, y, {
  content: { id: `r-${id}`, content_id: `c-${id}`, kind: 'text', revision_no: '1', title: id, payload: { body }, truncated: false, media: [] },
  status: { content_state: 'ready', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
  data: { schema_version: 1, config: {}, content_id: `c-${id}`, content_revision_id: `r-${id}`, selected_version_id: null, document_id: null },
  capabilities: { actions: [...caps('core.text'), 'save_to_library'], prompt_mode: 'draft', disabled_reason: null },
})
const fixture = () => ({
  id: 'qa', project_id: 'qa-project', project_name: '工具条回归', project_revision: '1',
  revision: '1', topology_revision: '1', archived: false,
  nodes: [
    text('brief', 80, 100, '海边逆光人像'),
    filled('photo', 'core.image', 80, 420),
    filled('clip', 'core.video', 460, 420),
    makeNode('empty', 'core.image', 840, 420),
    makeNode('g', 'core.group', 900, 60, { metadata: { type_key: 'core.group', type_version: 1, title: '参考组', intent: '', x: 900, y: 60, width: 380, height: 300, z_order: 0 } }),
    makeNode('gc', 'core.text', 40, 60, { parent_id: 'g', metadata: { type_key: 'core.text', type_version: 1, title: '组内', intent: '', x: 40, y: 60, width: 300, height: 220, z_order: 0 } }),
  ],
  edges: [], node_inputs: [], changes: [], object_states: [],
})

async function fresh({ offline = false } = {}) {
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
      for (const a of actions) {
        if (a.type === 'connect_reference') canvas.edges.push({ id: `e-${canvas.edges.length}`, source_node_id: a.source_node_id, target_node_id: a.target_node_id, source_port: 'output', target_port: 'reference', role: 'reference', ordinal: 0, revision: '1' })
        if (a.type === 'add_node') canvas.nodes.push(makeNode(a.node_id, a.type_key, a.x, a.y, { parent_id: a.parent_id ?? null, metadata: { type_key: a.type_key, type_version: 1, title: a.title ?? '', intent: '', x: a.x, y: a.y, width: size[a.type_key][0], height: size[a.type_key][1], z_order: 0 } }))
      }
      canvas.revision = canvas.topology_revision = String(Number(canvas.revision) + 1)
      return json({ change_id: `ch-${canvas.revision}`, result_revision: canvas.revision, result_topology_revision: canvas.topology_revision, object_results: [], created_ids: actions.filter((a) => a.type === 'add_node').map((a) => a.node_id) })
    }
    if (req.method() !== 'GET') server.other.push(path)
    if (path.endsWith('/assets/from-canvas-node')) return json({ id: `asset-${server.other.length}`, revision: '1', reused: false })
    if (path.endsWith('/library-settings')) return json({ revision: '1', view_mode: 'grid', sort: 'updated_desc', thumbnail_size: 'medium' })
    if (path.endsWith('/media-capabilities')) return json({ formats: [{ kind: 'image', mime: 'image/png', extensions: ['.png'] }], image_max_bytes: 10485760, av_max_bytes: 104857600, batch_limit: 10 })
    return json({ items: [], next_cursor: '', total_count: 0, library_revision: '1', hierarchy_revision: '1' })
  })
  await page.goto(`${base}/creative/canvases/qa`)
  await page.locator('.react-flow__node[data-id="gc"]').waitFor()
  await page.waitForTimeout(300)
  // 离线时改动只留在本机、可直接撤销；在线时 mock 会确认，派生节点的输入框才解锁。
  if (offline)
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => false })
      window.dispatchEvent(new Event('offline'))
    })
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
const record = async (ac, fn, opts) => {
  if (only && !only.includes(ac)) return
  step = ''
  try {
    await fresh(opts)
    await fn()
    results[ac] = 'pass'
  } catch (error) {
    results[ac] = `FAIL${step ? ` @${step}` : ''}: ${String(error.message).split('\n')[0]}`
    if (process.env.TRACE) console.error(ac, error.stack)
  }
}
const promptBox = prompt
const activeLabel = active
const selectedIDs = selected

const toolbar = () => page.getByRole('toolbar', { name: '节点操作' })
// 工具条上依次可见的按钮名称（文字按钮取文本，图标按钮取 aria-label）。
const toolbarItems = () => toolbar().evaluate((el) => [...el.children].filter((c) => c.tagName === 'BUTTON' || c.tagName === 'SPAN' && !c.classList.contains('cc-tool-sep')).map((c) => c.getAttribute('aria-label') ?? c.textContent.trim()))
const pick = async (id) => { await page.mouse.click(blank.x, blank.y); await node(id).locator('.cc-node-header h3').click() }
const pickMany = async (...list) => { await pick(list[0]); for (const id of list.slice(1)) await node(id).locator('.cc-node-header h3').click({ modifiers: ['Shift'] }) }
const nodeMenu = () => page.getByRole('menu', { name: '节点右键菜单' })
const undo = () => page.getByRole('button', { name: '撤销', exact: true })
// 派生：位置、连线、预填、焦点、不自动生成、一步撤销（AC-01/02/05）。
async function checkDerive(sources, button, type, prefill) {
  const before = await ids()
  const boxes = await Promise.all(sources.map((id) => node(id).locator('.cc-node').boundingBox()))
  await toolbar().getByRole('button', { name: button, exact: true }).click()
  const id = await waitNew(before)
  await page.waitForFunction(() => document.activeElement?.getAttribute('aria-label') === '提示词', null, { timeout: 5000 })
  assert.deepEqual(await selected(), [id], 'only the derived node is selected')
  const action = server.posts.flat().find((a) => a.type === 'add_node' && a.node_id === id)
  assert.equal(action?.type_key, type)
  const links = server.posts.flat().filter((a) => a.type === 'connect_reference' && a.target_node_id === id).map((a) => a.source_node_id)
  assert.deepEqual(links.sort(), [...sources].sort(), 'every source connects to the new node')
  const posted = server.posts.find((p) => p.some((a) => a.type === 'add_node' && a.node_id === id))
  assert.ok(posted.some((a) => a.type === 'connect_reference'), 'node and edges travel in one command')
  const card = await node(id).locator('.cc-node').boundingBox()
  const z = await page.locator('.react-flow__viewport').evaluate((el) => Number(/scale\(([-\d.]+)\)/.exec(el.style.transform)[1]))
  assert.ok(Math.abs(card.width - card.height) < 1, 'derived node is 1:1')
  assert.ok(Math.abs(card.x - (Math.max(...boxes.map((b) => b.x + b.width)) + 120 * z)) < 2, 'placed 120 right of all sources')
  const cy = boxes.reduce((s, b) => s + b.y + b.height / 2, 0) / boxes.length
  assert.ok(Math.abs(card.y + card.height / 2 - cy) < 2, 'vertically at the average source centre')
  const chips = await promptBox().locator('[data-ref]').evaluateAll((els) => els.map((e) => e.dataset.ref))
  assert.deepEqual(chips, sources, 'prefilled @ references in source order')
  const textOnly = await promptBox().evaluate((el) => [...el.childNodes].filter((n) => n.nodeType === 3).map((n) => n.textContent).join('').trim())
  assert.equal(textOnly, prefill, 'prefilled text after the references')
  assert.ok(!server.other.some((p) => p.endsWith('/executions')), 'derive never starts generation')
}
// 离线派生后一次撤销去掉新节点和全部连线（AC-02）。
async function checkDeriveUndo(sources, button) {
  const before = await ids()
  const edges = await page.locator('.react-flow__edge').count()
  await toolbar().getByRole('button', { name: button, exact: true }).click()
  const id = await waitNew(before)
  await page.waitForFunction((n) => document.querySelectorAll('.react-flow__edge').length === n, edges + sources.length)
  await page.mouse.click(blank.x, blank.y)
  await undo().click()
  await page.waitForFunction((v) => !document.querySelector(`.react-flow__node[data-id="${v}"]`), id)
  assert.equal((await ids()).length, before.length, 'one undo removes the node')
  assert.equal(await page.locator('.react-flow__edge').count(), edges, 'and every new edge')
  assert.equal(await undo().isDisabled(), true, 'nothing else left to undo')
}

try {
  // AC-01 / AC-02 / AC-05 三种派生。
  await record('AC-01', async () => {
    at('text → image')
    await pick('brief')
    await checkDerive(['brief'], '生成图片', 'core.image', '')
    at('image → video')
    await pick('photo')
    await checkDerive(['photo'], '生成视频', 'core.video', '镜头缓慢推进，')
    at('multi → reference image')
    await pickMany('brief', 'photo', 'clip')
    await checkDerive(['brief', 'photo', 'clip'], '合并为参考生成', 'core.image', '')
  })
  await record('AC-02', async () => {
    at('text → image')
    await pick('brief')
    await checkDeriveUndo(['brief'], '生成图片')
    at('image → video')
    await pick('photo')
    await checkDeriveUndo(['photo'], '生成视频')
    at('multi')
    await pickMany('brief', 'photo', 'clip')
    await checkDeriveUndo(['brief', 'photo', 'clip'], '合并为参考生成')
  }, { offline: true })

  // AC-03 各选区工具条（CV-70）；多选含组可解组/批量存库；组适应视图；更多 = 右键。
  await record('AC-03', async () => {
    at('multi')
    await pickMany('brief', 'photo', 'g')
    assert.deepEqual(await toolbarItems(), ['已选 3 项', '打组', '解组', '合并为参考生成', '复制', '删除'])
    at('group')
    await pick('g')
    assert.deepEqual(await toolbarItems(), ['重命名', '解组', '复制整组', '更多'])
    at('text')
    await pick('brief')
    assert.deepEqual(await toolbarItems(), ['编辑', '生成图片', '更多'])
    at('empty media')
    await pick('empty')
    assert.deepEqual(await toolbarItems(), ['上传', '更多'])
    at('image')
    await pick('photo')
    assert.deepEqual(await toolbarItems(), ['生成视频', '全屏查看', '存入资产库', '下载', '更多'])
    at('video')
    await pick('clip')
    assert.deepEqual(await toolbarItems(), ['全屏查看', '存入资产库', '下载', '更多'])
    at('more equals right click')
    await toolbar().getByRole('button', { name: '更多', exact: true }).click()
    const viaMore = await menuItems(nodeMenu())
    await page.keyboard.press('Escape')
    await node('clip').locator('.cc-node').click({ button: 'right' })
    assert.deepEqual(await menuItems(nodeMenu()), viaMore, '更多 opens the same menu as right click')
    assert.deepEqual(viaMore.filter((l) => !['编辑说明', '替换媒体', '从资产库添加参考'].includes(l)), ['重命名', '全屏查看', '下载', '复制', '存入资产库…', '删除'], 'CV-80 order')
    assert.ok(viaMore.includes('编辑说明'), 'media caption stays editable from the menu')
    await page.keyboard.press('Escape')
    at('text menu')
    await node('brief').locator('.cc-node').click({ button: 'right' })
    assert.deepEqual((await menuItems(nodeMenu())).filter((l) => l !== '从资产库添加参考'), ['重命名', '编辑文字', '复制', '存入资产库…', '删除'])
    await page.keyboard.press('Escape')
    at('group menu')
    await node('g').locator('.cc-node-header h3').click({ button: 'right' })
    assert.deepEqual(await menuItems(nodeMenu()), ['重命名分组', '解组', '复制整组', '适应到分组', '删除分组及内容'])
    at('fit to group')
    await nodeMenu().getByRole('menuitem', { name: '适应到分组', exact: true }).click()
    await page.waitForTimeout(400)
    const gb = await node('g').boundingBox()
    const z = await page.locator('.react-flow__viewport').evaluate((el) => Number(/scale\(([-\d.]+)\)/.exec(el.style.transform)[1]))
    assert.ok(z > 1 && gb.x >= 79 && gb.x + gb.width <= 1561, `group fills the view (zoom ${z})`)
    at('multi menu with group')
    // 适应到分组后其他节点可能在视口外，先恢复全局适应。
    await page.mouse.click(blank.x, blank.y)
    await page.keyboard.press('Shift+Digit1')
    await page.waitForTimeout(400)
    await pickMany('brief', 'g')
    await node('brief').locator('.cc-node').click({ button: 'right' })
    assert.deepEqual(await menuItems(nodeMenu()), ['打组', '解组', '复制', '存入资产库…', '删除'])
    await page.keyboard.press('Escape')
    at('batch save to library')
    await pickMany('photo', 'clip', 'empty')
    await node('photo').locator('.cc-node').click({ button: 'right' })
    await nodeMenu().getByRole('menuitem', { name: '存入资产库…', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '存入个人资产库' })
    await dialog.getByText('已选 3 项', { exact: false }).waitFor()
    await dialog.getByRole('button', { name: '保存资产', exact: true }).click()
    for (let i = 0; i < 60 && server.other.filter((p) => p.endsWith('/assets/from-canvas-node')).length < 2; i++) await page.waitForTimeout(50)
    assert.equal(server.other.filter((p) => p.endsWith('/assets/from-canvas-node')).length, 2, 'only nodes with content are saved')
    at('blank menu')
    await page.mouse.click(blank.x, blank.y, { button: 'right' })
    const blankItems = await menuItems(page.getByRole('menu', { name: '画布右键菜单' }))
    assert.deepEqual(blankItems.slice(-4), ['撤销', '重做', '全选', '适应画布'], 'CV-83 tail')
    await page.getByRole('menu', { name: '画布右键菜单' }).getByRole('menuitem', { name: '全选', exact: true }).click()
    assert.deepEqual((await selected()).sort(), ['brief', 'clip', 'empty', 'g', 'photo'], 'select all roots')
  })

  // AC-04 撤销/重做不可用时置灰；复制、删除按子树规则。
  await record('AC-04', async () => {
    at('disabled undo/redo')
    await page.mouse.click(blank.x, blank.y, { button: 'right' })
    const blankMenu = page.getByRole('menu', { name: '画布右键菜单' })
    assert.equal(await blankMenu.getByRole('menuitem', { name: '撤销', exact: true }).isDisabled(), true)
    assert.equal(await blankMenu.getByRole('menuitem', { name: '重做', exact: true }).isDisabled(), true)
    await page.keyboard.press('Escape')
    assert.equal(await undo().isDisabled(), true)
    at('copy subtree once')
    await pick('g')
    await page.locator('.react-flow__node[data-id="gc"] .cc-node-header h3').click({ modifiers: ['Shift'] })
    const before = await ids()
    await toolbar().getByRole('button', { name: '复制', exact: true }).click()
    for (let i = 0; i < 60 && (await ids()).length < before.length + 2; i++) await page.waitForTimeout(50)
    assert.equal((await ids()).length, before.length + 2, 'group + child copied once (not twice)')
    await undo().click()
    for (let i = 0; i < 60 && (await ids()).length !== before.length; i++) await page.waitForTimeout(50)
    at('remove subtree')
    await pick('g')
    await toolbar().getByRole('button', { name: '更多', exact: true }).click()
    await nodeMenu().getByRole('menuitem', { name: '删除分组及内容', exact: true }).click()
    await page.waitForFunction(() => !document.querySelector('.react-flow__node[data-id="gc"]'))
    assert.equal((await ids()).includes('g'), false, 'group removed with its subtree')
    await undo().click()
    await node('gc').waitFor()
    assert.equal(await page.getByRole('button', { name: '重做', exact: true }).isDisabled(), false, 'redo enabled after undo')
    at('multi ungroup is one undo step')
    await pickMany('brief', 'g')
    await node('brief').locator('.cc-node').click({ button: 'right' })
    await nodeMenu().getByRole('menuitem', { name: '解组', exact: true }).click()
    await page.waitForFunction(() => !document.querySelector('.react-flow__node[data-id="g"]'))
    await undo().click()
    await node('g').waitFor()
  }, { offline: true })

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify({ results }, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!process.env.ONLY && Object.keys(results).length !== 4)) process.exitCode = 1
