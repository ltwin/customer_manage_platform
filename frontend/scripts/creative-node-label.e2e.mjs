// Browser regression for V6-003 (node label meta, empty states, default sizes,
// copy). All API traffic is mocked; the page goes offline after load so new and
// copied nodes stay local previews whose geometry and titles we can read.
// Run against the Vite dev server: node scripts/creative-node-label.e2e.mjs.
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
const fixture = () => {
  const group = makeNode('g', 'core.group', 60, 640, { metadata: { type_key: 'core.group', type_version: 1, title: '参考组', intent: '', x: 60, y: 640, width: 420, height: 300, z_order: 0 } })
  return {
    id: 'qa', project_id: 'qa-project', project_name: '标签回归', project_revision: '1',
    revision: '1', topology_revision: '1', archived: false,
    nodes: [
      ready('multi', 'core.image', 100, 120, { count: 3, main_ordinal: 2, main_model_key: 'seedream' }),
      ready('single', 'core.image', 480, 120, { count: 1, main_ordinal: 1, main_model_key: 'seedream' }),
      ready('unknown', 'core.video', 860, 120, { count: 1, main_ordinal: 1, main_model_key: 'mystery-model' }),
      ready('uploaded', 'core.image', 1240, 120, null),
      ready('running', 'core.image', 100, 1100, { count: 3, main_ordinal: 3, main_model_key: 'seedream' }, 'running'),
      makeNode('text', 'core.text', 560, 640, { metadata: { type_key: 'core.text', type_version: 1, title: '文字', intent: '', x: 560, y: 640, width: 300, height: 220, z_order: 0 }, version_summary: { count: 2, main_ordinal: 1, main_model_key: null } }),
      makeNode('empty', 'core.image', 960, 640, { metadata: { type_key: 'core.image', type_version: 1, title: '图片', intent: '', x: 960, y: 640, width: 300, height: 300, z_order: 0 } }),
      group,
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
try {
  // AC-01 多版本「主版本 i/N」、单版本模型名（未知时显示 key）；运行中、文本、无版本不显示。
  await record('AC-01', async () => {
    assert.equal(await meta('multi'), '主版本 2/3')
    at('single uses catalog label')
    await page.waitForFunction(() => document.querySelector('.react-flow__node[data-id="single"] .cc-node-meta')?.textContent === 'Seedream 4')
    assert.equal(await meta('unknown'), 'mystery-model', 'unknown model falls back to key')
    assert.equal(await meta('uploaded'), '', 'no versions, no meta')
    assert.equal(await meta('running'), '', 'running hides meta')
    assert.equal(await meta('text'), '', 'text never shows meta')
    assert.equal(await meta('g'), '', 'group has its own head')
    await page.screenshot({ path: '/tmp/v6-003-labels.png' })
    at('meta follows the title, outside the card')
    const head = await node('multi').locator('.cc-node-header').boundingBox()
    const m = await node('multi').locator('.cc-node-meta').boundingBox()
    const card = await node('multi').boundingBox()
    assert.ok(head.y + head.height <= card.y + 1, 'label is above the card')
    const cssHeight = await node('multi').locator('.cc-node-header').evaluate((el) => getComputedStyle(el).height)
    assert.equal(cssHeight, '26px', 'label is 26px tall in canvas units')
    const t = await node('multi').locator('.cc-node-header h3').boundingBox()
    assert.ok(m.x >= t.x + t.width, 'meta follows the title')
    assert.ok(m.x + m.width <= card.x + card.width + 1, 'meta stays within the card width')
  })

  // AC-02 新建默认宽 300；文本 220 高、图片/视频 1:1、音频 120；空文本有编辑提示。
  await record('AC-02', async () => {
    const add = async (label) => {
      const before = await ids()
      await page.mouse.dblclick(1450, 560)
      await page.getByRole('menu', { name: '添加节点' }).getByRole('menuitem', { name: label, exact: true }).click()
      const [id] = await waitNew(before)
      await page.mouse.click(1500, 980)
      return geometry(id).then((g) => ({ id, ...g }))
    }
    for (const [label, w, h] of [['文字', 300, 220], ['图片', 300, 300], ['视频', 300, 300], ['音频', 300, 120]]) {
      at(`add ${label}`)
      const g = await add(label)
      assert.equal(g.width, w, `${label} width`)
      assert.equal(g.height, h, `${label} height`)
      if (label === '文字') assert.equal(await node(g.id).locator('.cc-empty-hint').textContent(), '双击输入文字')
      await page.keyboard.press('Control+z')
    }
    at('empty text hint on existing node')
    assert.equal(await node('text').locator('.cc-empty-hint').textContent(), '双击输入文字')
  })

  // AC-03 复制 +40/+40，只有根节点标题加「 副本」；本机预览与发往服务端的请求一致。
  await record('AC-03', async () => {
    const before = await ids()
    const src = await geometry('multi')
    await node('multi').locator('.cc-node-header h3').click()
    await page.keyboard.press('Meta+d')
    const [copy] = await waitNew(before)
    const g = await geometry(copy)
    assert.deepEqual({ x: g.x, y: g.y }, { x: src.x + 40, y: src.y + 40 }, 'copy offset +40/+40')
    assert.equal(await title(copy), 'multi 副本')
    at('group copy keeps child titles')
    await page.mouse.click(1500, 980)
    const before2 = await ids()
    await node('g').locator('.cc-node-header h3').click()
    await page.keyboard.press('Meta+d')
    const added = await waitNew(before2, 2)
    const titles = await Promise.all(added.map(title))
    assert.deepEqual(titles.sort(), ['参考组 副本', '组内'].sort(), `only the root gets the suffix (got ${titles.join(',')})`)
    at('request offset')
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => true })
      window.dispatchEvent(new Event('online'))
    })
    for (let i = 0; i < 60 && !commands.flat().some((a) => a.type === 'duplicate_selection'); i++) await page.waitForTimeout(50)
    const dup = commands.flat().find((a) => a.type === 'duplicate_selection')
    assert.equal(dup?.dx, 40)
    assert.equal(dup?.dy, 40)
  })

  // AC-04 空图片/视频有引导文案和「上传」「资产库」；两个入口都只指向当前节点。
  await record('AC-04', async () => {
    const empty = node('empty')
    assert.equal((await empty.locator('.cc-empty-hint').innerText()).replace(/\s+/g, ''), '选中后输入提示词生成或拖入图片')
    at('library fills this node')
    await empty.getByRole('button', { name: '从资产库填充' }).click()
    await page.getByRole('heading', { name: '选择图片' }).waitFor()
    await page.keyboard.press('Escape')
    at('upload targets this node')
    const before = await ids()
    const chooser = page.waitForEvent('filechooser')
    await empty.getByRole('button', { name: '上传到此节点' }).click()
    await chooser
    assert.deepEqual(await ids(), before, 'upload entry does not create a node')
  })

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify({ results }, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!process.env.ONLY && Object.keys(results).length !== 4)) process.exitCode = 1
