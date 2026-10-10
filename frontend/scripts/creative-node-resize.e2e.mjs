// Browser regression for V6-004 (manual node resize). All API traffic is mocked;
// the page goes offline after load so each resize stays a local, undoable edit.
// Run against the Vite dev server: node scripts/creative-node-resize.e2e.mjs.
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const browser = await chromium.launch({ headless: true })
const errors = []
// 每条 AC 用新的 browser context：本机草稿/outbox 不跨 AC 残留。
let context, page, commands

const caps = ['move', 'resize', 'rename', 'duplicate', 'remove', 'edit', 'reference', 'replace']
const makeNode = (id, typeKey, x, y, extra = {}) => ({
  id,
  parent_id: null,
  metadata: { type_key: typeKey, type_version: 1, title: id, intent: '', x, y, width: 300, height: 300, z_order: 0 },
  prompt: null,
  capabilities: { actions: typeKey === 'core.group' ? ['move', 'rename', 'duplicate', 'remove', 'ungroup'] : caps, prompt_mode: 'draft', disabled_reason: null },
  status: { content_state: 'empty', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
  placement_revision: '1',
  data_revision: '1',
  data: { schema_version: 1, config: {}, content_id: null, content_revision_id: null, selected_version_id: null, document_id: null },
  ...extra,
})
const art = `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="300"><rect width="300" height="300" fill="#555"/></svg>`
const imageContent = (id) => ({
  id, content_id: `c-${id}`, kind: 'image', revision_no: '1', title: id,
  payload: {}, truncated: false,
  media: [{ role: 'original', blob_id: `b-${id}`, mime: 'image/svg+xml', byte_size: 1, width: 300, height: 300, duration_ms: null }],
})
const ready = (id, typeKey, x, y, summary, state = 'idle') => makeNode(id, typeKey, x, y, {
  content: imageContent(`r-${id}`),
  status: { content_state: 'ready', generation_state: state, active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
  data: { schema_version: 1, config: {}, content_id: `c-${id}`, content_revision_id: `r-${id}`, selected_version_id: null, document_id: null },
  ...(summary ? { version_summary: summary } : {}),
})
const sized = (n, w, h) => ({ ...n, metadata: { ...n.metadata, width: w, height: h } })
const fixture = () => {
  // 16:9 图片；ghost 的原图宽高未知（尺寸读不到）。
  const wide = ready('wide', 'core.image', 100, 120, null)
  wide.content.media[0] = { ...wide.content.media[0], width: 1600, height: 900 }
  const ghost = ready('ghost', 'core.image', 100, 560, null)
  ghost.content.media[0] = { ...ghost.content.media[0], width: null, height: null }
  return {
    id: 'qa', project_id: 'qa-project', project_name: '缩放回归', project_revision: '1',
    revision: '1', topology_revision: '1', archived: false,
    nodes: [
      sized(wide, 400, 225),
      sized(ghost, 300, 260),
      makeNode('text', 'core.text', 620, 120, { metadata: { type_key: 'core.text', type_version: 1, title: 'text', intent: '', x: 620, y: 120, width: 300, height: 220, z_order: 0 } }),
      makeNode('audio', 'core.audio', 620, 480, { metadata: { type_key: 'core.audio', type_version: 1, title: 'audio', intent: '', x: 620, y: 480, width: 300, height: 120, z_order: 0 } }),
      makeNode('g', 'core.group', 1040, 120, { capabilities: { actions: ['move', 'rename', 'duplicate', 'remove', 'ungroup'], prompt_mode: 'scope', disabled_reason: null }, metadata: { type_key: 'core.group', type_version: 1, title: '参考组', intent: '', x: 1040, y: 120, width: 380, height: 320, z_order: 0 } }),
      makeNode('gc', 'core.text', 40, 60, { parent_id: 'g', metadata: { type_key: 'core.text', type_version: 1, title: '组内', intent: '', x: 40, y: 60, width: 300, height: 220, z_order: 0 } }),
    ],
    edges: [], node_inputs: [], changes: [], object_states: [],
  }
}

async function fresh() {
  await context?.close()
  context = await browser.newContext({ viewport: { width: 1600, height: 1000 } })
  page = await context.newPage()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
  commands = []
  const canvas = fixture()
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
    if (path.endsWith('/me')) return json({ id: 'qa-node-label', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
    if (path.endsWith('/generation/catalog'))
      return json({ models: [{ key: 'seedream', label: 'Seedream 4', kind: 'image', configuration_revision: '1', parameters: [] }], disabled_reason: '' })
    if (path.endsWith('/versions')) return json({ items: [], selected_version_id: null, data_revision: '1' })
    if (path.includes('/content-urls') || path.includes('/download-url') || path.includes('/media-url'))
      return json({ url: `data:image/svg+xml,${encodeURIComponent(art)}`, expires_at: new Date(Date.now() + 3600000).toISOString() })
    if (path.endsWith('/canvases/qa')) return json(canvas)
    if (path.endsWith('/commands')) {
      commands.push(route.request().postDataJSON()?.payload?.actions ?? [])
      return json({ error: { code: 'unavailable', message: 'offline fixture' } }, 503)
    }
    if (path.endsWith('/library-settings')) return json({ revision: '1', view_mode: 'grid', sort: 'updated_desc', thumbnail_size: 'medium' })
    if (path.endsWith('/media-capabilities'))
      return json({ formats: [{ kind: 'image', mime: 'image/png', extensions: ['.png'] }, { kind: 'video', mime: 'video/mp4', extensions: ['.mp4'] }], image_max_bytes: 10485760, av_max_bytes: 104857600, batch_limit: 10 })
    return json({ items: [], next_cursor: '', total_count: 0, library_revision: '1', hierarchy_revision: '1' })
  })
  await page.goto(`${base}/creative/canvases/qa`)
  await page.locator('.react-flow__node[data-id="gc"]').waitFor()
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => false })
    window.dispatchEvent(new Event('offline'))
  })
}

const node = (id) => page.locator(`.react-flow__node[data-id="${id}"]`)
const meta = async (id) => {
  const m = node(id).locator('.cc-node-header .cc-node-meta')
  return (await m.count()) ? (await m.textContent()) : ''
}
const ids = () => page.locator('.react-flow__node').evaluateAll((els) => els.map((e) => e.getAttribute('data-id')))
const geometry = (id) => node(id).evaluate((el) => {
  const m = /translate\(([-\d.]+)px,\s*([-\d.]+)px\)/.exec(el.style.transform)
  return { x: Number(m[1]), y: Number(m[2]), width: parseFloat(el.style.width), height: parseFloat(el.style.height) }
})
const title = (id) => node(id).locator('.cc-node-header h3').textContent()
const newIDs = async (before) => (await ids()).filter((id) => !before.includes(id))
const waitNew = async (before, n = 1) => {
  for (let i = 0; i < 60; i++) {
    if ((await newIDs(before)).length >= n) return newIDs(before)
    await page.waitForTimeout(50)
  }
  throw new Error(`expected ${n} new nodes`)
}

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
const zoom = () => page.locator('.react-flow__viewport').evaluate((el) => Number(/scale\(([-\d.]+)\)/.exec(el.style.transform)[1]))
const select = (id) => node(id).locator('.cc-node-header h3').click()
const handle = (id, corner = 'bottom.right') => node(id).locator(`.react-flow__resize-control.handle.${corner}`)
const blank = { x: 1500, y: 960 }
// 拖右下角手柄：dx/dy 是世界坐标；onMiddle 在松手前执行，用来读尺寸提示。
async function drag(id, dx, dy, onMiddle) {
  const z = await zoom()
  const box = await handle(id).boundingBox()
  const x = box.x + box.width / 2, y = box.y + box.height / 2
  await page.mouse.move(x, y)
  await page.mouse.down()
  await page.mouse.move(x + dx * z, y + dy * z, { steps: 8 })
  if (onMiddle) await onMiddle()
  await page.mouse.up()
  await page.waitForTimeout(150)
}
const undoDisabled = () => page.getByRole('button', { name: '撤销', exact: true }).isDisabled()

try {
  // AC-01 只有单选普通节点显示缩放手柄；多选和组都不显示。
  await record('AC-01', async () => {
    await select('text')
    assert.equal(await node('text').locator('.react-flow__resize-control').count() > 0, true, 'single node shows handles')
    at('multi selection')
    await node('wide').locator('.cc-node-header h3').click({ modifiers: ['Shift'] })
    assert.equal(await page.locator('.react-flow__resize-control').count(), 0, 'multi-selection hides handles')
    at('group')
    await page.mouse.click(blank.x, blank.y)
    await select('g')
    assert.equal(await node('g').locator('.react-flow__resize-control').count(), 0, 'group has no handles')
  })

  // AC-02 图片等比、文本/音频自由；拖动中实时显示尺寸，松手后消失。
  await record('AC-02', async () => {
    await select('wide')
    let tip = ''
    await drag('wide', 80, 0, async () => { tip = await page.locator('.cc-resize-tip').textContent() })
    const w = await geometry('wide')
    assert.ok(Math.abs(w.width / w.height - 16 / 9) < 0.02, `image keeps 16:9 (got ${w.width}x${w.height})`)
    assert.match(tip, /^\d+ × \d+$/, `live size tip (got "${tip}")`)
    assert.equal(await page.locator('.cc-resize-tip').count(), 0, 'tip disappears on release')
    at('text free')
    await page.mouse.click(blank.x, blank.y)
    await select('text')
    await drag('text', 60, -40)
    const t = await geometry('text')
    assert.ok(Math.abs(t.width - 360) < 3 && Math.abs(t.height - 180) < 3, `text resizes freely (got ${t.width}x${t.height})`)
    at('audio free')
    await page.mouse.click(blank.x, blank.y)
    await select('audio')
    await drag('audio', -40, 40)
    const a = await geometry('audio')
    assert.ok(Math.abs(a.width - 260) < 3 && Math.abs(a.height - 160) < 3, `audio resizes freely (got ${a.width}x${a.height})`)
  })

  // AC-03 一次调整一次撤销；发出一个 move + resize 请求。
  await record('AC-03', async () => {
    assert.equal(await undoDisabled(), true)
    const before = await geometry('text')
    await select('text')
    await drag('text', 80, 60)
    assert.equal(await undoDisabled(), false, 'resize is undoable')
    await page.keyboard.press('Control+z')
    await page.waitForFunction(([w, h]) => {
      const el = document.querySelector('.react-flow__node[data-id="text"]')
      return Math.abs(parseFloat(el.style.width) - w) < 1 && Math.abs(parseFloat(el.style.height) - h) < 1
    }, [before.width, before.height])
    assert.equal(await undoDisabled(), true, 'one resize, one undo step')
    at('request')
    await page.keyboard.press('Control+Shift+z')
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => true })
      window.dispatchEvent(new Event('online'))
    })
    for (let i = 0; i < 60 && !commands.length; i++) await page.waitForTimeout(50)
    const types = commands[0]?.map((a) => a.type) ?? []
    assert.deepEqual(types, ['move_node', 'resize_node'], `one request carries the resize (got ${types})`)
  })

  // AC-04 拖到边界时卡住：宽 160–1200、文本高 ≥100。
  await record('AC-04', async () => {
    await select('text')
    await drag('text', -400, -400)
    const small = await geometry('text')
    assert.equal(Math.round(small.width), 160, `min width (got ${small.width})`)
    assert.equal(Math.round(small.height), 100, `min height (got ${small.height})`)
    at('max width')
    await page.getByRole('button', { name: '缩小', exact: true }).click()
    await page.getByRole('button', { name: '缩小', exact: true }).click()
    await page.waitForTimeout(350)
    await drag('text', 2000, 0)
    assert.equal(Math.round((await geometry('text')).width), 1200, 'max width')
  })

  // AC-05 媒体高度跟内容比例；尺寸读不到时保持原高度。
  await record('AC-05', async () => {
    const w = await geometry('wide')
    assert.ok(Math.abs(w.height - (400 * 9) / 16) < 1, `filled image height follows ratio at current width (got ${w.height})`)
    const g = await geometry('ghost')
    assert.equal(g.height, 260, `unknown dimensions keep the current height (got ${g.height})`)
  })

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify({ results }, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!process.env.ONLY && Object.keys(results).length !== 5)) process.exitCode = 1
