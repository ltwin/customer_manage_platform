// Browser regression for the canvas node outline (canvas-node-outline). All API
// traffic is mocked; the page goes offline after load so nothing leaves.
// Run against a Vite dev server: CREATIVE_EDITOR_URL=http://localhost:5181 node scripts/creative-canvas-outline.e2e.mjs
// One case at a time: ONLY=reveal node scripts/creative-canvas-outline.e2e.mjs
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const browser = await chromium.launch({ headless: true })
const errors = []
let context, page

const caps = ['move', 'resize', 'rename', 'duplicate', 'remove', 'edit', 'reference', 'maximize', 'download', 'replace']
const art = `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="300"><rect width="300" height="300" fill="#666"/></svg>`
const idle = { content_state: 'empty', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' }
const makeNode = (id, typeKey, x, y, created, extra = {}) => ({
  id, parent_id: null, created_at: created,
  metadata: { type_key: typeKey, type_version: 1, title: id, intent: '', x, y, width: 300, height: 220, z_order: 0 },
  prompt: null,
  capabilities: { actions: caps, prompt_mode: typeKey === 'core.group' ? 'unavailable' : 'draft', disabled_reason: null },
  status: idle,
  placement_revision: '1', data_revision: '1',
  data: { schema_version: 1, config: {}, content_id: null, content_revision_id: null, selected_version_id: null, document_id: null },
  ...extra,
})
const fixture = ({ archived = false } = {}) => ({
  id: 'qa', project_id: 'qa-project', project_name: '节点树回归', project_revision: '1',
  revision: '1', topology_revision: '1', archived,
  nodes: [
    makeNode('left', 'core.text', 0, 100, '2026-11-01T10:00:00Z', { metadata: { type_key: 'core.text', type_version: 1, title: '晨光文案', intent: '', x: 0, y: 100, width: 300, height: 220, z_order: 0 } }),
    makeNode('photo', 'core.image', 500, 100, '2026-11-01T10:01:00Z', {
      metadata: { type_key: 'core.image', type_version: 1, title: '晨光人像', intent: '', x: 500, y: 100, width: 300, height: 300, z_order: 0 },
      content: { id: 'r-photo', content_id: 'c-photo', kind: 'image', revision_no: '1', title: '晨光', payload: {}, truncated: false,
        media: [{ role: 'original', blob_id: 'b', mime: 'image/svg+xml', byte_size: 1, width: 300, height: 300, duration_ms: null }] },
      status: { ...idle, content_state: 'ready' },
      data: { schema_version: 1, config: {}, content_id: 'c-photo', content_revision_id: 'r-photo', selected_version_id: null, document_id: null },
    }),
    makeNode('g', 'core.group', 1000, 60, '2026-11-01T10:02:00Z', { metadata: { type_key: 'core.group', type_version: 1, title: '布景', intent: '', x: 1000, y: 60, width: 700, height: 400, z_order: 0 } }),
    makeNode('inner', 'core.image', 24, 52, '2026-11-01T10:03:00Z', { parent_id: 'g' }),
    makeNode('busy', 'core.image', 1000, 700, '2026-10-30T10:00:00Z', {
      status: { ...idle, generation_state: 'running', active_execution_id: 'e-2', latest_execution_id: 'e-2' },
    }),
    makeNode('far', 'core.text', 6000, 3000, '2026-11-01T10:04:00Z', {
      status: { ...idle, generation_state: 'failed', latest_execution_id: 'e-1', error: 'generation_failed' },
    }),
  ],
  edges: [], node_inputs: [], changes: [], object_states: [],
})

async function fresh({ archived = false, width = 1600, height = 1000, offline = true } = {}) {
  await context?.close()
  context = await browser.newContext({ viewport: { width, height } })
  page = await context.newPage()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
  const canvas = fixture({ archived })
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
    if (path.endsWith('/me')) return json({ id: 'qa-outline', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
    if (path.endsWith('/generation/catalog')) return json({ models: [], disabled_reason: '未配置生成模型' })
    if (path.endsWith('/versions')) return json({ items: [], selected_version_id: null, data_revision: '1' })
    if (path.endsWith('/media-access-tickets') || path.includes('/content-urls') || path.includes('/download-url') || path.includes('/media-url'))
      return json({ url: `data:image/svg+xml,${encodeURIComponent(art)}`, expires_at: new Date(Date.now() + 3600000).toISOString() })
    if (path.endsWith('/canvases/qa')) return json(canvas)
    if (path.endsWith('/commands')) return json({ error: { code: 'unavailable', message: 'offline fixture' } }, 503)
    if (path.endsWith('/library-settings')) return json({ revision: '1', view_mode: 'grid', sort: 'updated_desc', thumbnail_size: 'medium' })
    if (path.endsWith('/media-capabilities')) return json({ formats: [], image_max_bytes: 1, av_max_bytes: 1, batch_limit: 10 })
    return json({ items: [], next_cursor: '', total_count: 0, library_revision: '1', hierarchy_revision: '1' })
  })
  await page.goto(`${base}/creative/canvases/qa`)
  await page.locator('.react-flow__node[data-id="photo"]').waitFor()
  await page.waitForTimeout(400)
  // 窄屏下离线提示条会盖住工具轨（既有布局），手机用例不进离线态。
  if (offline) await page.evaluate(() => {
    Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => false })
    window.dispatchEvent(new Event('offline'))
  })
}

const viewport = () => page.locator('.react-flow__viewport').evaluate((el) => {
  const m = /translate\(([-\d.]+)px,\s*([-\d.]+)px\) scale\(([-\d.]+)\)/.exec(el.style.transform)
  return { x: Number(m[1]), y: Number(m[2]), zoom: Number(m[3]) }
})
const node = (id) => page.locator(`.react-flow__node[data-id="${id}"]`)
const tree = () => page.getByRole('tree', { name: '画布节点' })
const row = (id) => page.locator(`[role="treeitem"][data-node="${id}"]`)
const rowIDs = () => page.locator('[role="treeitem"]').evaluateAll((els) => els.map((e) => e.dataset.node))
const foot = () => page.locator('.cc-outline-foot').innerText()
const openTree = async () => {
  await page.getByRole('button', { name: '节点树', exact: true }).click()
  await tree().waitFor()
}
const settle = () => page.waitForTimeout(400)

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
    results[ac] = `fail at ${step}: ${error.message}`
  }
}

try {
  await record('structure', async () => {
    at('newest first, groups nest, count includes groups')
    await openTree()
    assert.deepEqual(await rowIDs(), ['far', 'g', 'inner', 'photo', 'left', 'busy'])
    assert.equal(await foot(), '共 6 节点')
    at('running and failed nodes are marked')
    await row('busy').locator('.cc-outline-status.is-running').waitFor()
    assert.equal((await row('far').locator('.cc-outline-status').innerText()).trim(), '失败')
    assert.equal(await row('g').locator('.cc-outline-status').count(), 0)
    assert.equal(await page.getByRole('tab', { name: '画布' }).getAttribute('aria-selected'), 'true')
    at('image row shows a thumbnail, text row an icon')
    await row('photo').locator('img').waitFor()
    assert.equal(await row('left').locator('img').count(), 0)
    at('collapse hides children, count unchanged')
    await row('g').getByRole('button', { name: '折叠分组' }).click()
    assert.deepEqual(await rowIDs(), ['far', 'g', 'photo', 'left', 'busy'])
    assert.equal(await foot(), '共 6 节点')
  })

  await record('search', async () => {
    await openTree()
    await page.getByRole('button', { name: '折叠分组' }).click()
    at('search by title keeps hits only')
    await page.getByRole('searchbox', { name: '搜索节点名称' }).fill('晨光')
    assert.deepEqual(await rowIDs(), ['photo', 'left'])
    assert.equal(await foot(), '2 / 共 6 节点')
    at('type filter keeps the group as an open path')
    await page.getByRole('searchbox', { name: '搜索节点名称' }).fill('')
    await page.getByRole('button', { name: '按类型筛选' }).click()
    await page.getByRole('menuitemcheckbox', { name: /图片/ }).click()
    await page.keyboard.press('Escape')
    assert.deepEqual(await rowIDs(), ['g', 'inner', 'photo', 'busy'])
    assert.equal(await foot(), '3 / 共 6 节点')
    at('clearing the search restores the collapsed group')
    await page.getByRole('button', { name: /按类型筛选/ }).click()
    await page.getByRole('menuitem', { name: '显示全部类型' }).click()
    await page.keyboard.press('Escape')
    assert.deepEqual(await rowIDs(), ['far', 'g', 'photo', 'left', 'busy'])
    at('no hits offer to clear')
    await page.getByRole('searchbox', { name: '搜索节点名称' }).fill('不存在')
    await page.getByText('没有匹配的节点').waitFor()
    await page.getByRole('button', { name: '清除筛选' }).click()
    assert.equal((await rowIDs()).length, 5)
  })

  await record('reveal', async () => {
    await openTree()
    at('zoom in on the left node so the far one is off screen')
    await page.mouse.move(900, 500)
    await page.keyboard.down('Control')
    for (let i = 0; i < 6; i++) await page.mouse.wheel(0, -40)
    await page.keyboard.up('Control')
    await row('left').click()
    await settle()
    const offBefore = await node('far').boundingBox()
    assert.ok(!offBefore || offBefore.x > 1600 || offBefore.y > 1000, `far node is off screen first: ${JSON.stringify(offBefore)}`)
    const before = await viewport()
    at('clicking the row selects it and brings it into view at the same zoom')
    await row('far').click()
    await settle()
    const after = await viewport()
    assert.equal(after.zoom, before.zoom, 'zoom kept')
    const box = await node('far').boundingBox()
    const panel = await page.locator('#creative-library').boundingBox()
    assert.ok(box && box.x > panel.x + panel.width && box.x + box.width < 1600 && box.y > 0 && box.y + box.height < 1000, `far node visible beside the panel: ${JSON.stringify(box)}`)
    assert.equal(await node('far').getAttribute('class').then((c) => c.includes('selected')), true)
    assert.equal(await row('far').getAttribute('aria-selected'), 'true')
    at('a node inside a group is revealed too')
    await row('inner').click()
    await settle()
    const inner = await node('inner').boundingBox()
    assert.ok(inner && inner.x > panel.x + panel.width && inner.x + inner.width < 1600, 'grouped node visible')
  })

  await record('multi', async () => {
    await openTree()
    const before = await viewport()
    at('cmd/ctrl click adds rows without moving the view')
    await row('left').click()
    await settle()
    const v0 = await viewport()
    await row('far').click({ modifiers: ['ControlOrMeta'] })
    await settle()
    assert.deepEqual(await viewport(), v0, 'modifier click does not move')
    assert.equal(await page.locator('.react-flow__node.selected').count(), 2)
    assert.equal(await row('left').getAttribute('aria-selected'), 'true')
    assert.equal(await row('far').getAttribute('aria-selected'), 'true')
    at('shift click selects the visible range from the anchor')
    await row('photo').click({ modifiers: ['Shift'] })
    // 锚点是最后一次修饰键点击的 far：far、g、inner、photo。
    assert.equal(await page.locator('[role="treeitem"][aria-selected="true"]').count(), 4)
    assert.equal(await page.locator('.react-flow__node.selected').count(), 4)
    assert.notDeepEqual(before, null)
  })

  await record('canvas-sync', async () => {
    await openTree()
    // 先把分组带到面板右侧，再折叠它在树里的行。
    await row('g').click()
    await settle()
    await row('g').getByRole('button', { name: '折叠分组' }).click()
    assert.equal(await row('inner').count(), 0)
    at('selecting on the canvas highlights rows and opens a collapsed group')
    await node('inner').locator('.cc-node-header h3').click()
    await row('inner').waitFor()
    assert.equal(await row('inner').getAttribute('aria-selected'), 'true')
    assert.equal(await row('g').getAttribute('aria-selected'), 'false')
    at('canvas select-all (top-level nodes) highlights exactly those rows')
    await page.keyboard.press('ControlOrMeta+a')
    await settle()
    const on = await page.locator('[role="treeitem"][aria-selected="true"]').evaluateAll((els) => els.map((e) => e.dataset.node))
    assert.deepEqual(on, ['far', 'g', 'photo', 'left', 'busy'])
  })

  await record('rename', async () => {
    await openTree()
    at('double-click renames the node on the canvas')
    await row('photo').locator('.cc-outline-title').dblclick()
    const input = row('photo').getByRole('textbox', { name: '节点名称' })
    await input.fill('黄昏人像')
    await input.press('Enter')
    await node('photo').locator('.cc-node-header h3', { hasText: '黄昏人像' }).waitFor()
    await row('photo').locator('.cc-outline-title', { hasText: '黄昏人像' }).waitFor()
    at('undo restores the name')
    await page.getByRole('button', { name: '撤销', exact: true }).click()
    await node('photo').locator('.cc-node-header h3', { hasText: '晨光人像' }).waitFor()
    await row('photo').locator('.cc-outline-title', { hasText: '晨光人像' }).waitFor()
  })

  await record('readonly', async () => {
    await openTree()
    at('an archived canvas does not enter rename')
    await row('photo').locator('.cc-outline-title').dblclick()
    await page.waitForTimeout(200)
    assert.equal(await row('photo').getByRole('textbox').count(), 0)
  }, { archived: true })

  await record('tabs', async () => {
    at('node tree and library buttons switch tabs without closing')
    await openTree()
    await page.getByRole('button', { name: '资产库', exact: true }).click()
    assert.equal(await page.getByRole('tab', { name: '资产' }).getAttribute('aria-selected'), 'true')
    assert.equal(await page.locator('#creative-library').isVisible(), true)
    assert.equal(await tree().isVisible(), false)
    await page.getByRole('tab', { name: '画布' }).click()
    assert.equal(await tree().isVisible(), true)
    at('the node tree button closes the panel when its tab is showing')
    await page.getByRole('button', { name: '节点树', exact: true }).click()
    assert.equal(await page.locator('#creative-library').isVisible(), false)
    at('a library task from a node shows the library with its title and no tabs')
    await openTree()
    await row('inner').click() // 先把它带到面板右侧
    await settle()
    await node('inner').getByRole('button', { name: '从资产库填充' }).click()
    await page.locator('#creative-library').getByRole('heading', { name: '选择图片' }).waitFor()
    assert.equal(await page.getByRole('tab', { name: '画布' }).count(), 0, 'no node tree tab during a library task')
    at('escape in the tree closes the panel')
    await page.getByRole('button', { name: '取消', exact: true }).click()
    await openTree()
    await row('left').focus()
    await page.keyboard.press('Escape')
    assert.equal(await page.locator('#creative-library').isVisible(), false)
  })

  await record('mobile', async () => {
    await openTree()
    at('390px wide: no horizontal page scroll')
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'no page overflow')
  }, { width: 390, height: 844, offline: false })

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify({ results }, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!process.env.ONLY && Object.keys(results).length !== 9)) process.exitCode = 1
