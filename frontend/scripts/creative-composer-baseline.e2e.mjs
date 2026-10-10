// Browser regression for V6-056 (node generation composer baseline). All API
// traffic is mocked with one image/text model; each AC runs in a fresh context.
// Run against the Vite dev server: node scripts/creative-composer-baseline.e2e.mjs.
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const browser = await chromium.launch({ headless: true })
const errors = []
let context, page, canvas, generationPosts
const caps = (media) => ['move', 'resize', 'rename', 'duplicate', 'remove', 'edit', 'reference', ...(media ? ['replace', 'maximize', 'download'] : [])]
const makeNode = (id, typeKey, x, y, extra = {}) => ({
  id,
  parent_id: null,
  metadata: { type_key: typeKey, type_version: 1, title: id, intent: '', x, y, width: 280, height: 280, z_order: 0 },
  prompt: null,
  capabilities: { actions: typeKey === 'core.group' ? ['move', 'rename', 'duplicate', 'remove', 'ungroup'] : caps(typeKey !== 'core.text'), prompt_mode: typeKey === 'core.group' ? 'scope' : 'draft', disabled_reason: null },
  status: { content_state: 'empty', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
  placement_revision: '1',
  data_revision: '1',
  data: { schema_version: 1, config: {}, content_id: null, content_revision_id: null, selected_version_id: null, document_id: null },
  ...extra,
})
const fixture = () => ({
  id: 'qa', project_id: 'qa-project', project_name: '输入框回归', project_revision: '1', revision: '1', topology_revision: '1', archived: false,
  nodes: [
    makeNode('img', 'core.image', 120, 120),
    makeNode('img2', 'core.image', 620, 120),
    makeNode('txt', 'core.text', 1120, 120),
    makeNode('g', 'core.group', 100, 560, { metadata: { type_key: 'core.group', type_version: 1, title: '组', intent: '', x: 100, y: 560, width: 360, height: 300, z_order: 0 } }),
    makeNode('gc', 'core.image', 40, 64, { parent_id: 'g', metadata: { type_key: 'core.image', type_version: 1, title: 'gc', intent: '', x: 40, y: 64, width: 200, height: 200, z_order: 0 } }),
  ],
  edges: [{ id: 'e1', source_node_id: 'txt', target_node_id: 'img2', source_port: 'output', target_port: 'reference', role: 'reference', ordinal: 0, revision: '1' }],
  node_inputs: [], changes: [], object_states: [],
})
async function fresh({ keepStorage = false } = {}) {
  if (!keepStorage) {
    await context?.close()
    context = await browser.newContext({ viewport: { width: 1600, height: 1000 } })
    canvas = fixture()
  }
  generationPosts = 0
  page = await context.newPage()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
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
    if (path.endsWith('/me')) return json({ id: 'qa-composer', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
    if (path.endsWith('/generation-catalog'))
      return json({ models: [{ key: 'qa-model', label: '契约模型', kind: 'image', configuration_revision: '1', parameters: [] }], disabled_reason: '' })
    if (path.endsWith('/versions')) return json({ items: [], selected_version_id: null, data_revision: '1' })
    if (path.endsWith('/commands')) {
      const v = route.request().postDataJSON().payload
      const n = canvas.nodes.find((x) => x.id === v.node_id)
      if (v.type === 'save_prompt' && n) {
        n.prompt = { draft_id: 'd-' + n.id, draft_revision: String(Number(n.prompt?.draft_revision ?? 0) + 1), action_key: v.action_key, text: v.text, model_key: v.model_key, parameters: v.parameters, references: v.references.map((r, i) => ({ ...r, id: 'ref-' + i, node_id: n.id, revision: '1' })) }
        canvas.revision = String(Number(canvas.revision) + 1)
        return json({ draft_id: n.prompt.draft_id, draft_revision: n.prompt.draft_revision, change: {} })
      }
      if (v.type === 'cancel_execution') return json({})
      return json({ error: { code: 'unavailable', message: 'not modelled' } }, 503)
    }
    if (path.endsWith('/executions') && route.request().method() === 'POST') {
      generationPosts++
      const n = canvas.nodes.find((x) => x.id === route.request().postDataJSON().payload.node_id) ?? canvas.nodes[0]
      // 保持运行中：用于检查生成键变为「生成中」。
      n.status = { ...n.status, generation_state: 'running', active_execution_id: 'e-run', latest_execution_id: 'e-run' }
      canvas.revision = String(Number(canvas.revision) + 1)
      return json({ execution_id: 'e-run', node_id: n.id, canvas_id: 'qa', action_key: 'gateway.generation', executor_version: '1', state: 'running', apply_state: null, execution_epoch: 1, deadline: new Date(Date.now() + 3600000).toISOString(), change_id: null, error: '', created_at: new Date().toISOString() })
    }
    if (path.endsWith('/executions/e-run'))
      return json({ execution_id: 'e-run', node_id: 'img', canvas_id: 'qa', action_key: 'gateway.generation', executor_version: '1', state: 'running', apply_state: null, execution_epoch: 1, deadline: new Date(Date.now() + 3600000).toISOString(), change_id: null, error: '', created_at: new Date().toISOString() })
    if (path.endsWith('/canvases/qa')) return json(canvas)
    if (path.endsWith('/library-settings')) return json({ revision: '1', view_mode: 'grid', sort: 'updated_desc', thumbnail_size: 'medium' })
    if (path.endsWith('/media-capabilities')) return json({ formats: [], image_max_bytes: 1, av_max_bytes: 1, batch_limit: 10 })
    return json({ items: [], next_cursor: '', total_count: 0, library_revision: '1', hierarchy_revision: '1' })
  })
  await page.goto(`${base}/creative/canvases/qa`)
  await page.locator('.react-flow__node[data-id="gc"]').waitFor()
}
const node = (id) => page.locator(`.react-flow__node[data-id="${id}"]`)
const header = (id) => node(id).locator('.cc-node-header h3')
const panel = () => page.getByRole('complementary', { name: '节点生成' })
const shown = async () => (await panel().count()) > 0 && (await panel().isVisible())
const editor = () => panel().getByLabel('提示词', { exact: true })
const blank = { x: 1500, y: 940 }
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
const nodeCard = (id) => node(id).locator('.cc-node').boundingBox()

try {
  // AC-01 出现条件、位置与随选中切换内容。
  const softProblems = []
  await record('AC-01', async () => {
    for (const id of ['img', 'txt']) {
      at(`single ${id}`)
      await header(id).click()
      await panel().waitFor()
    }
    at('offset')
    await header('img').click()
    await panel().waitFor()
    const card = await nodeCard('img')
    const box = await panel().boundingBox()
    const gap = box.y - (card.y + card.height)
    // 间距单独收集，不阻断后面的出现条件检查。
    if (Math.abs(gap - 14) >= 1.5) softProblems.push(`panel sits 14px below the card (got ${gap.toFixed(1)})`)
    at('multi')
    await header('img2').click({ modifiers: ['Shift'] })
    assert.equal(await shown(), false, 'hidden for multi-selection')
    at('group')
    await page.mouse.click(blank.x, blank.y)
    await header('g').click()
    // PRD §11.3 #8：组按 CAN-09 显示组范围任务描述。
    await panel().getByText('组任务将在 Agent 工具接入后开放', { exact: false }).waitFor()
    at('edge')
    await page.mouse.click(blank.x, blank.y)
    const mid = await page.locator('.react-flow__edge[data-id="e1"] .react-flow__edge-path').evaluate((p) => { const t = p.getPointAtLength(p.getTotalLength() / 2); const s = new DOMPoint(t.x, t.y).matrixTransform(p.getScreenCTM()); return { x: s.x, y: s.y } })
    await page.mouse.click(mid.x, mid.y)
    assert.equal(await shown(), false, 'hidden for a selected edge')
    at('text editing')
    await node('txt').locator('.cc-node-content').dblclick()
    await page.getByRole('textbox', { name: '正文', exact: true }).waitFor()
    assert.equal(await shown(), false, 'hidden while editing text')
    await page.keyboard.press('Escape')
    at('switch content')
    await header('img').click()
    await editor().fill('图片一的草稿')
    await header('img2').click()
    await panel().getByLabel('参考：txt', { exact: true }).waitFor()
    assert.equal(await editor().innerText(), '', 'switching shows the new node prompt')
    if (softProblems.length) throw new Error(softProblems.join(' | '))
  })
  // AC-02 宽 460，跟随平移缩放；左侧避开工具轨、右侧留 8px，超出底部时上移贴底（PRD §11.3 #9）。
  await record('AC-02', async () => {
    await header('img2').click()
    await panel().waitFor()
    const a = await panel().boundingBox()
    assert.equal(Math.round(a.width), 460, 'width 460')
    const centred = async (label) => {
      const b = await panel().boundingBox()
      const card = await nodeCard('img2')
      assert.ok(Math.abs(b.x + b.width / 2 - (card.x + card.width / 2)) < 2, `${label}: centred under the node`)
      assert.ok(Math.abs(b.y - (card.y + card.height) - 14) < 1.5, `${label}: 14px below the node`)
    }
    at('follows pan')
    await page.mouse.move(800, 900)
    await page.mouse.wheel(80, 60)
    await page.waitForTimeout(300)
    await centred('pan')
    at('follows zoom')
    await page.getByRole('button', { name: '缩小', exact: true }).click()
    await page.waitForTimeout(400)
    await centred('zoom')
    at('left edge')
    const rail = await page.getByRole('navigation', { name: '创作工具' }).boundingBox()
    await page.mouse.wheel(3000, 0)
    await page.waitForTimeout(300)
    const c = await panel().boundingBox()
    assert.ok(c.x >= rail.x + rail.width, `does not cover the tool rail (panel ${c.x}, rail right ${rail.x + rail.width})`)
    at('right edge')
    await page.mouse.click(blank.x, blank.y)
    await header('txt').click()
    await panel().waitFor()
    await page.mouse.move(800, 600)
    for (let i = 0; i < 3; i++) await page.mouse.wheel(-200, 0)
    await page.waitForTimeout(300)
    const d = await panel().boundingBox()
    assert.ok(Math.abs(1600 - (d.x + d.width) - 8) < 1, `clamped 8px from the right (got ${1600 - d.x - d.width})`)
    at('bottom edge')
    for (let i = 0; i < 3; i++) await page.mouse.wheel(0, -300)
    await page.waitForTimeout(300)
    const e = await panel().boundingBox()
    const card = await nodeCard('txt')
    assert.ok(e.y + e.height <= 1000 && e.y < card.y + card.height + 14, `moves up to stay inside the bottom (bottom ${e.y + e.height})`)
  })

  // AC-03 结构：参考行 → 提示词 → 底栏；底栏为模型、比例/数量、点数、圆形生成键。
  await record('AC-03', async () => {
    await header('img2').click()
    await panel().waitFor()
    const order = await panel().evaluate((el) => {
      const pick = (sel) => el.querySelector(sel)?.getBoundingClientRect().top ?? -1
      return { refs: pick('.cc-generation-refs'), prompt: pick('[aria-label="提示词"]'), footer: pick('footer') }
    })
    assert.ok(order.refs >= 0 && order.refs < order.prompt && order.prompt < order.footer, `refs → prompt → footer (${JSON.stringify(order)})`)
    // 比例/数量 chip 与点数由 V6-021/022 交付；本条只验底栏以模型开头、生成键结尾。
    const foot = await panel().locator('footer').evaluate((f) => [...f.querySelectorAll('select,button')].filter((x) => x.offsetParent).map((x) => x.getAttribute('aria-label')))
    assert.equal(foot[0], '生成模型', `footer starts with the model (got ${foot.join(', ')})`)
    assert.equal(foot.at(-1), '生成新版本', `footer ends with the send button (got ${foot.join(', ')})`)
    const send = await panel().getByRole('button', { name: '生成新版本', exact: true }).evaluate((b) => getComputedStyle(b).borderRadius)
    assert.equal(send, '50%', 'round send button')
  })
  // AC-04 输入框内点击不取消选中，按键不触发画布快捷键。
  await record('AC-04', async () => {
    await header('img').click()
    await panel().waitFor()
    const nodesBefore = await page.locator('.react-flow__node').count()
    await editor().click()
    assert.equal(await node('img').evaluate((el) => el.classList.contains('selected')), true, 'click inside keeps the selection')
    for (const key of ['t', 'i', 'Delete', 'Backspace', 'Meta+a', 'Meta+d', 'Shift+Digit1']) await page.keyboard.press(key)
    await page.waitForTimeout(300)
    assert.equal(await page.locator('.react-flow__node').count(), nodesBefore, 'canvas shortcuts do not fire from the composer')
    assert.equal(await node('img').evaluate((el) => el.classList.contains('selected')), true, 'still selected after typing')
    await panel().getByRole('button', { name: '生成模型', exact: true }).or(panel().getByLabel('生成模型', { exact: true })).first().click()
    assert.equal(await node('img').evaluate((el) => el.classList.contains('selected')), true, 'clicking footer controls keeps the selection')
  })

  // AC-05 草稿按节点保存：切换、刷新、重开后仍在；保存草稿不改变当前作品。
  await record('AC-05', async () => {
    await header('img').click()
    await editor().fill('图片一：逆光人像')
    await header('img2').click()
    await editor().fill('图片二：窗边柔光')
    at('switch back')
    await header('img').click()
    await page.waitForFunction(() => document.querySelector('.cc-generation [aria-label="提示词"]')?.innerText === '图片一：逆光人像')
    at('save')
    await panel().getByRole('button', { name: '保存草稿', exact: true }).click()
    await page.waitForFunction(() => document.querySelector('.cc-generation footer')?.textContent.includes('草稿已保存'))
    const img = canvas.nodes.find((n) => n.id === 'img')
    assert.equal(img.data.content_revision_id, null, 'saving a draft does not change the current work')
    assert.equal(img.status.content_state, 'empty')
    at('reload')
    await page.waitForTimeout(400)
    await page.reload()
    await node('img').waitFor()
    await header('img2').click()
    await page.waitForFunction(() => document.querySelector('.cc-generation [aria-label="提示词"]')?.innerText === '图片二：窗边柔光')
    await header('img').click()
    await page.waitForFunction(() => document.querySelector('.cc-generation [aria-label="提示词"]')?.innerText === '图片一：逆光人像')
    // PRD §11.3 #10：已保存的草稿在新窗口重开后仍在；未保存的本机输入按窗口隔离。
    at('reopen saved draft')
    await page.close()
    await fresh({ keepStorage: true })
    await header('img').click()
    await page.waitForFunction(() => document.querySelector('.cc-generation [aria-label="提示词"]')?.innerText === '图片一：逆光人像')
  })
  async function pickModel() {
    await panel().getByRole('combobox', { name: '生成模型', exact: true }).click()
    await page.getByRole('listbox', { name: '生成模型' }).getByRole('option', { name: '契约模型' }).click()
  }
  // AC-06 生成键：空提示词且无上游时禁用；Enter 生成、Shift+Enter 换行、组字不误触发；生成中变为不可点的「生成中」（生成不提供停止，owner 2026-10-09）。
  await record('AC-06', async () => {
    await header('img').click()
    await pickModel()
    const send = () => panel().getByRole('button', { name: '生成新版本', exact: true })
    assert.equal(await send().isDisabled(), true, 'disabled with empty prompt and no upstream')
    at('upstream enables')
    await header('img2').click()
    await pickModel()
    assert.equal(await send().isDisabled(), false, 'an upstream reference is enough')
    at('keys')
    await header('img').click()
    await editor().fill('第一行')
    await editor().press('Shift+Enter')
    await page.waitForTimeout(200)
    assert.equal(generationPosts, 0, 'Shift+Enter only adds a newline')
    assert.match(await editor().innerText(), /第一行\n/, 'newline inserted')
    await editor().evaluate((el) => el.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', isComposing: true, bubbles: true, cancelable: true })))
    await page.waitForTimeout(200)
    assert.equal(generationPosts, 0, 'IME Enter does not generate')
    await editor().press('Enter')
    await page.waitForFunction(() => document.querySelector('.cc-generation [aria-label="生成中"]:disabled'), null, { timeout: 4000 })
    assert.equal(generationPosts, 1, 'Enter generates once')
  })

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify(results, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!only && Object.keys(results).length !== 6)) process.exitCode = 1
