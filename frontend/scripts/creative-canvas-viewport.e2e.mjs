// Browser regression for V6-002 (viewport zoom, pan, fit and restore). All API
// traffic is mocked; the page goes offline after load so nothing leaves.
// Run against the Vite dev server: node scripts/creative-canvas-viewport.e2e.mjs.
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const browser = await chromium.launch({ headless: true })
const errors = []
// 每条 AC 用新的 browser context：本机视口/草稿不跨 AC 残留。
let context, page

const caps = ['move', 'resize', 'rename', 'duplicate', 'remove', 'edit', 'reference', 'maximize', 'download', 'replace']
const art = `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="300"><rect width="300" height="300" fill="#666"/></svg>`
const makeNode = (id, typeKey, x, y, w = 300, h = 220, extra = {}) => ({
  id, parent_id: null,
  metadata: { type_key: typeKey, type_version: 1, title: id, intent: '', x, y, width: w, height: h, z_order: 0 },
  prompt: null,
  capabilities: { actions: caps, prompt_mode: 'draft', disabled_reason: null },
  status: { content_state: 'empty', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
  placement_revision: '1', data_revision: '1',
  data: { schema_version: 1, config: {}, content_id: null, content_revision_id: null, selected_version_id: null, document_id: null },
  ...extra,
})
const photo = makeNode('photo', 'core.image', 500, 100, 300, 300, {
  content: { id: 'r-photo', content_id: 'c-photo', kind: 'image', revision_no: '1', title: '晨光', payload: {}, truncated: false,
    media: [{ role: 'original', blob_id: 'b', mime: 'image/svg+xml', byte_size: 1, width: 300, height: 300, duration_ms: null }] },
  status: { content_state: 'ready', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
  data: { schema_version: 1, config: {}, content_id: 'c-photo', content_revision_id: 'r-photo', selected_version_id: null, document_id: null },
})
// 节点总包围盒：x 0–2400，y 72–700（含 28px 标题区）。
const fixture = () => ({
  id: 'qa', project_id: 'qa-project', project_name: '视口回归', project_revision: '1',
  revision: '1', topology_revision: '1', archived: false,
  nodes: [makeNode('left', 'core.text', 0, 100), photo, makeNode('right', 'core.text', 2100, 480)],
  edges: [], node_inputs: [], changes: [], object_states: [],
})

async function fresh({ stored } = {}) {
  await context?.close()
  context = await browser.newContext({ viewport: { width: 1600, height: 1000 } })
  page = await context.newPage()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
  const canvas = fixture()
  await page.addInitScript((value) => {
    if (value !== undefined) localStorage.setItem('creative-viewport:qa-viewport:qa', value)
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
  }, stored)
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    const json = (value, status = 200) => route.fulfill({ status, json: value })
    if (path.endsWith('/auth/refresh')) return json({ access_token: 'qa', token_type: 'Bearer', expires_in: 600 })
    if (path.endsWith('/me')) return json({ id: 'qa-viewport', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
    if (path.endsWith('/generation-catalog')) return json({ models: [], disabled_reason: '未配置生成模型' })
    if (path.endsWith('/versions')) return json({ items: [], selected_version_id: null, data_revision: '1' })
    if (path.includes('/content-urls') || path.includes('/download-url') || path.includes('/media-url'))
      return json({ url: `data:image/svg+xml,${encodeURIComponent(art)}`, expires_at: new Date(Date.now() + 3600000).toISOString() })
    if (path.endsWith('/canvases/qa')) return json(canvas)
    if (path.endsWith('/commands')) return json({ error: { code: 'unavailable', message: 'offline fixture' } }, 503)
    if (path.endsWith('/library-settings')) return json({ revision: '1', view_mode: 'grid', sort: 'updated_desc', thumbnail_size: 'medium' })
    if (path.endsWith('/media-capabilities')) return json({ formats: [], image_max_bytes: 1, av_max_bytes: 1, batch_limit: 10 })
    return json({ items: [], next_cursor: '', total_count: 0, library_revision: '1', hierarchy_revision: '1' })
  })
  await page.goto(`${base}/creative/canvases/qa`)
  await page.locator('.react-flow__node[data-id="right"]').waitFor()
  await page.waitForTimeout(400)
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => false })
    window.dispatchEvent(new Event('offline'))
  })
}

const viewport = () => page.locator('.react-flow__viewport').evaluate((el) => {
  const m = /translate\(([-\d.]+)px,\s*([-\d.]+)px\) scale\(([-\d.]+)\)/.exec(el.style.transform)
  return { x: Number(m[1]), y: Number(m[2]), zoom: Number(m[3]) }
})
const flowBox = () => page.locator('.react-flow').boundingBox()
const settle = () => page.waitForTimeout(350)
const node = (id) => page.locator(`.react-flow__node[data-id="${id}"]`)
const blank = { x: 1500, y: 960 }
const undoDisabled = () => page.getByRole('button', { name: '撤销', exact: true }).isDisabled()
// 屏幕上所有节点（含标签）的并集。
const nodesOnScreen = () => page.locator('.react-flow__node').evaluateAll((els) => {
  const rs = els.map((e) => e.getBoundingClientRect())
  return { left: Math.min(...rs.map((r) => r.left)), right: Math.max(...rs.map((r) => r.right)), top: Math.min(...rs.map((r) => r.top)), bottom: Math.max(...rs.map((r) => r.bottom)) }
})
const focusCanvas = async () => { await page.mouse.click(blank.x, blank.y) }

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
  }
}
try {
  // AC-01 ⌘+/⌘- 以屏幕中心 1.2 倍；⌘0 回到 100%；范围 10%–400%；⌘ 滚轮以指针为中心。
  await record('AC-01', async () => {
    await focusCanvas()
    await page.keyboard.press('Meta+0')
    await settle()
    const box = await flowBox()
    const center = { x: box.width / 2, y: box.height / 2 }
    const before = await viewport()
    assert.equal(before.zoom, 1, '⌘0 resets to 100%')
    const world = (v) => ({ x: (center.x - v.x) / v.zoom, y: (center.y - v.y) / v.zoom })
    await page.keyboard.press('Meta+=')
    await settle()
    const after = await viewport()
    assert.ok(Math.abs(after.zoom - 1.2) < 0.001, `⌘+ zooms by 1.2 (got ${after.zoom})`)
    const w0 = world(before), w1 = world(after)
    assert.ok(Math.abs(w0.x - w1.x) < 1 && Math.abs(w0.y - w1.y) < 1, 'screen centre stays put')
    await page.keyboard.press('Meta+-')
    await settle()
    assert.ok(Math.abs((await viewport()).zoom - 1) < 0.001, '⌘- zooms by 1/1.2')
    at('upper limit')
    for (let i = 0; i < 12; i++) await page.keyboard.press('Meta+=')
    await settle()
    assert.ok(Math.abs((await viewport()).zoom - 4) < 0.001, `stops at 400% (got ${(await viewport()).zoom})`)
    at('lower limit')
    for (let i = 0; i < 25; i++) await page.keyboard.press('Meta+-')
    await settle()
    assert.ok(Math.abs((await viewport()).zoom - 0.1) < 0.001, `stops at 10% (got ${(await viewport()).zoom})`)
    at('pointer zoom')
    await page.keyboard.press('Meta+0')
    await settle()
    const v0 = await viewport()
    const pointer = { x: 300, y: 300 }
    const wp = { x: (pointer.x - box.x - v0.x) / v0.zoom, y: (pointer.y - box.y - v0.y) / v0.zoom }
    await page.mouse.move(pointer.x, pointer.y)
    await page.keyboard.down('Meta')
    await page.mouse.wheel(0, -200)
    await page.keyboard.up('Meta')
    await settle()
    const v1 = await viewport()
    assert.ok(v1.zoom > v0.zoom, 'meta + wheel zooms in')
    const wp1 = { x: (pointer.x - box.x - v1.x) / v1.zoom, y: (pointer.y - box.y - v1.y) / v1.zoom }
    assert.ok(Math.abs(wp.x - wp1.x) < 2 && Math.abs(wp.y - wp1.y) < 2, 'pointer world point stays put')
  })

  // AC-02 适应画布包住全部节点和标题区，避开打开的面板，倍率 20%–110%。
  await record('AC-02', async () => {
    await focusCanvas()
    await page.keyboard.press('Shift+Digit1')
    await settle()
    const v = await viewport()
    assert.ok(v.zoom >= 0.2 && v.zoom <= 1.1, `fit zoom in range (got ${v.zoom})`)
    let r = await nodesOnScreen()
    assert.ok(r.left >= 80 - 1 && r.right <= 1600 - 40 + 1, `nodes inside margins (${r.left}–${r.right})`)
    const label = await node('left').locator('.cc-node-header').boundingBox()
    assert.ok(label.y >= 90 - 1, `label area stays visible (top ${label.y})`)
    at('library open')
    await page.getByRole('button', { name: '资产库', exact: true }).click()
    const drawer = await page.locator('#creative-library').boundingBox()
    await focusCanvas()
    await page.keyboard.press('Shift+Digit1')
    await settle()
    r = await nodesOnScreen()
    assert.ok(r.left >= drawer.x + drawer.width, `fit leaves the drawer (${r.left} vs ${drawer.x + drawer.width})`)
    at('fit button same rule')
    await page.getByRole('button', { name: '适应全部节点', exact: true }).click()
    await settle()
    r = await nodesOnScreen()
    assert.ok(r.left >= drawer.x + drawer.width, 'zoom-bar fit also leaves the drawer')
    assert.ok((await viewport()).zoom <= 1.1 + 1e-6, 'fit never exceeds 110%')
  })

  // AC-03 视口按账号/画布恢复；非法值回退适应；视口变化不进撤销栈。
  await record('AC-03', async () => {
    const v = await viewport()
    assert.ok(v.zoom >= 0.2 && v.zoom <= 1.1, `no stored view → fit (got ${v.zoom})`)
    await focusCanvas()
    await page.keyboard.press('Meta+=')
    await page.mouse.wheel(120, 80)
    await settle()
    assert.equal(await undoDisabled(), true, 'viewport changes leave no undo step')
    const saved = await viewport()
    await page.reload()
    await page.locator('.react-flow__node[data-id="right"]').waitFor()
    await settle()
    const restored = await viewport()
    assert.ok(Math.abs(restored.zoom - saved.zoom) < 0.001 && Math.abs(restored.x - saved.x) < 1 && Math.abs(restored.y - saved.y) < 1, 'restored after reload')
  })
  await record('AC-03-invalid', async () => {
    const v = await viewport()
    assert.ok(v.zoom >= 0.2 && v.zoom <= 1.1, `out-of-range stored zoom falls back to fit (got ${v.zoom})`)
    const r = await nodesOnScreen()
    assert.ok(r.left >= 79 && r.right <= 1561, 'fit shows every node')
  }, { stored: JSON.stringify({ x: 10, y: 10, zoom: 9 }) })
  await record('AC-03-garbage', async () => {
    const v = await viewport()
    assert.ok(v.zoom >= 0.2 && v.zoom <= 1.1, `unparsable stored view falls back to fit (got ${v.zoom})`)
  }, { stored: '{not json' })

  // AC-04 四种平移；Space 有选中时交给全屏；滚轮关闭菜单。
  await record('AC-04', async () => {
    const drag = async (button, from, dx, dy) => {
      await page.mouse.move(from.x, from.y)
      await page.mouse.down({ button })
      await page.mouse.move(from.x + dx, from.y + dy, { steps: 6 })
      await page.mouse.up({ button })
      await settle()
    }
    let v0 = await viewport()
    at('middle button')
    await drag('middle', { x: 1400, y: 850 }, -120, -60)
    let v1 = await viewport()
    assert.ok(Math.abs(v1.x - v0.x + 120) < 2 && Math.abs(v1.y - v0.y + 60) < 2, 'middle drag pans')
    at('hand tool')
    await page.getByRole('button', { name: '移动画布', exact: true }).click()
    v0 = v1
    await drag('left', { x: 1400, y: 850 }, 100, 40)
    v1 = await viewport()
    assert.ok(Math.abs(v1.x - v0.x - 100) < 2, 'hand tool left drag pans')
    await page.getByRole('button', { name: '选择工具', exact: true }).click()
    at('space with nothing selected')
    await focusCanvas()
    v0 = await viewport()
    await page.keyboard.down('Space')
    await drag('left', { x: 1400, y: 850 }, -80, 0)
    await page.keyboard.up('Space')
    v1 = await viewport()
    assert.ok(Math.abs(v1.x - v0.x + 80) < 2, 'space + drag pans when nothing is selected')
    assert.equal(await page.getByRole('dialog').count(), 0, 'no preview opens')
    at('wheel pans and closes menus')
    await page.mouse.dblclick(blank.x - 100, blank.y - 100)
    const menu = page.getByRole('menu', { name: '添加节点' })
    await menu.waitFor()
    v0 = await viewport()
    await page.mouse.move(800, 500)
    await page.mouse.wheel(0, 120)
    await menu.waitFor({ state: 'detached' })
    v1 = await viewport()
    assert.ok(v1.y < v0.y - 20 && Math.abs(v1.x - v0.x) < 2, `wheel pans vertically (dy ${v1.y - v0.y})`)
    at('space with a media node selected opens preview')
    await node('photo').locator('.cc-node-header h3').click()
    v0 = await viewport()
    await page.keyboard.press('Space')
    await page.getByRole('dialog', { name: /媒体预览/ }).waitFor()
    assert.deepEqual(await viewport(), v0, 'space does not pan with a selection')
    await page.keyboard.press('Escape')
    await page.getByRole('dialog', { name: /媒体预览/ }).waitFor({ state: 'detached' })
    at('space with a text node selected does nothing')
    await node('left').locator('.cc-node-header h3').click()
    await page.keyboard.press('Space')
    await page.waitForTimeout(200)
    assert.equal(await page.getByRole('dialog').count(), 0, 'text selection: no preview')
    assert.equal(await undoDisabled(), true, 'panning leaves no undo step')
  })

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify({ results }, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!process.env.ONLY && Object.keys(results).length !== 6)) process.exitCode = 1
