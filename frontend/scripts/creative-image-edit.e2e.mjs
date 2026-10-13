// Browser regression for V6-076 image editing (compress panel, in-canvas crop).
// All API traffic is mocked: renders become ready after one poll, apply swaps
// the node's content and pushes an invalidate through the canvas stream.
// Run against the Vite dev server: node scripts/creative-image-edit.e2e.mjs.
// CREATIVE_QA_CHECK=<名称片段> 只跑部分检查。
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const only = process.env.CREATIVE_QA_CHECK ?? ''
const browser = await chromium.launch({ headless: true })
const errors = []
let context, page, server

const art = (w, h) => `data:image/svg+xml,${encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}"><defs><linearGradient id="g"><stop offset="0" stop-color="#c96"/><stop offset="1" stop-color="#369"/></linearGradient></defs><rect width="${w}" height="${h}" fill="url(#g)"/></svg>`)}`
const status = (extra = {}) => ({ content_state: 'ready', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1', ...extra })
const image = (id, x, { width = 4000, height = 3000, mime = 'image/png', bytes = 2_000_000, rev = `r-${id}`, extra = {} } = {}) => ({
  id, parent_id: null,
  metadata: { type_key: 'core.image', type_version: 1, title: id, intent: '', x, y: 160, width: 400, height: 300, z_order: 0 },
  prompt: null,
  capabilities: { actions: ['move', 'resize', 'rename', 'duplicate', 'remove', 'edit', 'reference', 'maximize', 'download', 'replace', 'save_to_library'], prompt_mode: 'draft', disabled_reason: null },
  status: status(),
  placement_revision: '1', data_revision: '1',
  data: { schema_version: 1, config: {}, content_id: `c-${id}`, content_revision_id: rev, selected_version_id: null, document_id: null },
  content: { id: rev, content_id: `c-${id}`, kind: 'image', sequence: '1', payload: {}, truncated: false,
    media: [{ role: 'original', blob_id: `b-${rev}`, mime, byte_size: bytes, width, height, duration_ms: null },
      { role: 'display', blob_id: `d-${rev}`, mime: 'image/jpeg', byte_size: 1000, width, height, duration_ms: null }] },
  ...extra,
})
const caps = { available: true, actions: ['compress', 'crop'], input_mime_types: ['image/jpeg', 'image/png', 'image/webp'], quality_min: 1, quality_max: 100, max_edge: 12000, max_pixels: 60000000, max_output_bytes: 26214400, ready_ttl_seconds: 1800 }

async function fresh({ viewport = { width: 1600, height: 1000 }, behaviour = {} } = {}) {
  await context?.close()
  context = await browser.newContext({ viewport })
  page = await context.newPage()
  page.setDefaultTimeout(8000)
  page.on('pageerror', (error) => errors.push(error.message))
  const canvas = {
    id: 'qa', project_id: 'qa-project', project_name: '图片编辑回归', project_revision: '1', revision: '1', topology_revision: '1', archived: false,
    nodes: [image('photo', 80), image('busy', 620, { extra: { status: status({ active_execution_id: 'e-1', generation_state: 'running' }) } }), image('gif', 1160, { mime: 'image/gif' })],
    edges: [], node_inputs: [], changes: [], object_states: [],
  }
  server = { canvas, creates: [], applies: [], discards: [], renders: new Map(), behaviour }
  await page.addInitScript(() => {
    const original = window.fetch.bind(window)
    window.__streams = []
    window.fetch = async (input, init) => {
      if (!String(input).endsWith('/canvases/qa/events')) return original(input, init)
      return new Response(new ReadableStream({
        start(controller) {
          const s = { invalidate: () => controller.enqueue(new TextEncoder().encode('event: invalidate\ndata: {}\n\n')) }
          window.__streams.push(s)
          s.invalidate()
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
    if (path.endsWith('/me')) return json({ id: 'qa-edit', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
    if (path.endsWith('/image-edit-capabilities')) return json(caps)
    if (path.endsWith('/media-access-tickets')) {
      const rev = req.postDataJSON().content_revision_id
      const n = canvas.nodes.find((x) => x.content?.id === rev)
      const o = n?.content.media[0] ?? { width: 4000, height: 3000 }
      return json({ url: art(o.width, o.height), expires_at: new Date(Date.now() + 3600000).toISOString() })
    }
    if (path.endsWith('/image-edit-renders') && req.method() === 'POST') {
      const body = req.postDataJSON()
      assert.equal(req.headers()['idempotency-key'], body.operation_id)
      server.creates.push(body)
      const node = canvas.nodes.find((n) => n.id === body.node_id)
      const src = node.content.media[0]
      const crop = body.action === 'crop'
      const render = {
        id: `cier_${server.creates.length}`, operation_id: body.operation_id, canvas_id: 'qa', node_id: body.node_id, source_revision_id: body.source_revision_id,
        action: body.action, parameters: body.parameters, state: 'queued', expires_at: new Date(Date.now() + 120000).toISOString(), created_at: new Date().toISOString(), source_size: src.byte_size,
        polls: 0,
        final: behaviour.final ?? 'ready',
        output: crop
          ? { mime: src.mime, width: body.parameters.width, height: body.parameters.height, size: 900_000, sha256: 'x' }
          : { mime: 'image/jpeg', width: src.width, height: src.height, size: 800_000, sha256: 'x' },
      }
      server.renders.set(render.id, render)
      return json(view(render), 202)
    }
    const renderID = path.match(/image-edit-renders\/([^/]+)/)?.[1]
    if (renderID && path.endsWith('/discard')) {
      const r = server.renders.get(renderID)
      server.discards.push(renderID)
      if (r.state !== 'applied') r.state = 'discarded'
      return json(view(r))
    }
    if (renderID) {
      const r = server.renders.get(renderID)
      if (r.state === 'queued' && ++r.polls >= (behaviour.polls ?? 1)) r.state = r.final
      return json(view(r))
    }
    if (path.endsWith('/image-edits/apply')) {
      const body = req.postDataJSON()
      server.applies.push(body)
      const r = server.renders.get(body.payload.render_id)
      const node = canvas.nodes.find((n) => n.id === r.node_id)
      if (r.state !== 'applied') {
        assert.equal(body.payload.expected_source_revision_id, node.content.id)
        r.state = 'applied'
        const rev = `r-${node.id}-${server.applies.length}`
        const before = node.content.media[0]
        node.content = { ...node.content, id: rev, media: [{ ...before, blob_id: `b-${rev}`, mime: r.output.mime, byte_size: r.output.size, width: r.output.width, height: r.output.height }, { ...node.content.media[1], width: r.output.width, height: r.output.height }] }
        node.data.content_revision_id = rev
        node.data_revision = String(Number(node.data_revision) + 1)
        canvas.revision = String(Number(canvas.revision) + 1)
        canvas.changes.push({ change_id: `ch-${canvas.revision}`, operation_id: body.operation_id, kind: 'apply', step_group_id: null, undone: false, created_at: new Date().toISOString() })
        r.result_revision_id = rev
        r.applied_version_id = `v-${rev}`
      }
      if (behaviour.loseApply && server.applies.length === 1) {
        setTimeout(() => page.evaluate(() => window.__streams.at(-1)?.invalidate()).catch(() => {}), 50)
        return route.abort('connectionreset')
      }
      setTimeout(() => page.evaluate(() => window.__streams.at(-1)?.invalidate()).catch(() => {}), 50)
      return json({ version_id: r.applied_version_id, content_revision_id: r.result_revision_id, change_id: `ch-${canvas.revision}`, result_revision: canvas.revision, before_topology_revision: '1', result_topology_revision: '1', object_results: [], created_ids: [], removed_ids: [], id_mapping: {} })
    }
    if (path.endsWith('/canvases/qa')) return json(canvas)
    if (path.endsWith('/versions')) return json({ items: [], selected_version_id: null, data_revision: '1' })
    if (path.endsWith('/generation/catalog')) return json({ models: [], disabled_reason: '' })
    if (path.endsWith('/library-settings')) return json({ revision: '1', view_mode: 'grid', sort: 'updated_desc', thumbnail_size: 'medium' })
    if (path.endsWith('/media-capabilities')) return json({ formats: [], image_max_bytes: 10485760, av_max_bytes: 104857600, batch_limit: 10 })
    return json({ items: [], next_cursor: '', total_count: 0, library_revision: '1', hierarchy_revision: '1' })
  })
  await page.goto(`${base}/creative/canvases/qa`)
  await page.locator('.react-flow__node[data-id="photo"] .cc-media.is-ready').waitFor()
}
function view(r) {
  const { polls, final, ...rest } = r
  void polls
  void final
  return { ...rest, output: rest.state === 'ready' || rest.state === 'applied' ? rest.output : undefined }
}
const node = (id) => page.locator(`.react-flow__node[data-id="${id}"]`)
const select = async (id) => {
  await node(id).locator('.cc-node-header').click()
  await page.getByRole('toolbar', { name: '节点操作' }).waitFor()
}
const toolbar = () => page.getByRole('toolbar', { name: '节点操作' })
const cropTools = () => page.getByRole('toolbar', { name: '裁切' })
const openEdit = async (id, item) => {
  await select(id)
  await toolbar().getByRole('button', { name: '图片编辑' }).click()
  await page.getByRole('menu', { name: '图片编辑' }).getByRole('menuitem', { name: item }).click()
}

const checks = {
  async '入口：工具条、右键菜单一致，不适用时禁用并说明'() {
    await fresh()
    await select('photo')
    const button = toolbar().getByRole('button', { name: '图片编辑' })
    assert.equal(await button.isDisabled(), false)
    await button.click()
    const menu = page.getByRole('menu', { name: '图片编辑' })
    assert.deepEqual(await menu.getByRole('menuitem').allInnerTexts(), ['裁切', '压缩'])
    await page.keyboard.press('Escape')
    await node('photo').click({ button: 'right', position: { x: 200, y: 150 } })
    const context = page.getByRole('menu', { name: '节点右键菜单' })
    await context.getByText('图片编辑', { exact: true }).waitFor()
    assert.equal(await context.getByRole('menuitem', { name: '裁切' }).isDisabled(), false)
    await page.keyboard.press('Escape')
    for (const [id, reason] of [['busy', '正在生成'], ['gif', 'GIF']]) {
      await select(id)
      const b = toolbar().getByRole('button', { name: '图片编辑' })
      assert.equal(await b.isDisabled(), true, id)
      assert.match(await b.getAttribute('title'), new RegExp(reason))
    }
  },

  async '压缩：调质量不发请求，应用一次并显示真实大小与格式'() {
    await fresh()
    await openEdit('photo', '压缩')
    const dialog = page.getByRole('dialog', { name: '压缩图片' })
    await dialog.getByText('PNG 不含透明像素时转 JPEG').waitFor()
    await dialog.getByText('1.91 MB').waitFor()
    await dialog.getByText('4000 × 3000').waitFor()
    await dialog.getByRole('button', { name: '小文件 60' }).click()
    await dialog.getByLabel('输出质量数值').fill('72')
    await dialog.getByLabel('输出质量数值').press('Enter')
    assert.equal(server.creates.length, 0, '调整质量不发起处理')
    assert.ok(await dialog.isVisible(), 'Enter 不应用、不关闭')
    const apply = dialog.getByRole('button', { name: '应用压缩' })
    await apply.click()
    await dialog.getByRole('button', { name: /正在/ }).waitFor()
    assert.equal(await dialog.getByRole('button', { name: /正在/ }).isDisabled(), true)
    await dialog.getByRole('status').waitFor()
    const result = await dialog.getByRole('status').innerText()
    assert.match(result, /1\.91 MB → 781 KB/)
    assert.match(result, /减少 60%/)
    assert.match(result, /输出 JPEG/)
    await page.screenshot({ path: '/tmp/creative-image-edit-compress.png' })
    assert.equal(server.creates.length, 1)
    assert.deepEqual(server.creates[0].parameters, { quality: 72 })
    assert.equal(server.applies.length, 1)
    assert.equal(server.applies[0].payload.expected_data_revision, '1')
    await dialog.getByRole('button', { name: '完成' }).click()
    await page.waitForFunction(() => !document.querySelector('[role="dialog"]'))
    assert.equal(server.canvas.nodes[0].content.media[0].mime, 'image/jpeg')
  },

  async '压缩未减小：提示且不采用，面板保留'() {
    await fresh({ behaviour: { final: 'not_reduced' } })
    await openEdit('photo', '压缩')
    const dialog = page.getByRole('dialog', { name: '压缩图片' })
    await dialog.getByRole('button', { name: '应用压缩' }).click()
    await dialog.getByRole('alert').getByText('当前设置未减小文件，请尝试降低质量').waitFor()
    assert.equal(server.applies.length, 0)
    assert.equal(await dialog.getByRole('button', { name: '应用压缩' }).isDisabled(), false)
  },

  async '编码中取消：丢弃候选，不采用'() {
    await fresh({ behaviour: { polls: 100 } })
    await openEdit('photo', '压缩')
    const dialog = page.getByRole('dialog', { name: '压缩图片' })
    await dialog.getByRole('button', { name: '应用压缩' }).click()
    await dialog.getByRole('button', { name: '正在压缩' }).waitFor()
    await dialog.getByRole('button', { name: '取消' }).click()
    await page.waitForFunction(() => !document.querySelector('[role="dialog"]'))
    await page.waitForTimeout(300)
    assert.deepEqual(server.discards, ['cier_1'])
    assert.equal(server.applies.length, 0)
  },

  async '采用响应丢失：确认原操作，只采用一次'() {
    await fresh({ behaviour: { loseApply: true } })
    await openEdit('photo', '压缩')
    const dialog = page.getByRole('dialog', { name: '压缩图片' })
    await dialog.getByRole('button', { name: '应用压缩' }).click()
    await dialog.getByRole('status').waitFor({ timeout: 15000 })
    assert.equal(server.creates.length, 1)
    assert.equal(server.canvas.changes.length, 1, '只产生一个版本')
    assert.ok(server.applies.length <= 2)
    if (server.applies.length === 2) assert.deepEqual(server.applies[0], server.applies[1], '重发的请求体与原请求相同')
  },

  async '裁切：比例、重置、取消，缩放后选区仍准确，应用得到真实尺寸'() {
    await fresh()
    const before = await node('photo').boundingBox()
    await openEdit('photo', '裁切')
    await cropTools().waitFor()
    const size = () => cropTools().locator('.cc-crop-size').innerText()
    assert.equal(await size(), '4000 × 3000')
    assert.equal(await cropTools().getByRole('button', { name: '应用裁切' }).isDisabled(), true, '整图不能应用')
    // 编辑中不显示连线端口和缩放把手。
    assert.equal(await node('photo').locator('.cc-reference-handle').count(), 0)
    assert.equal(await node('photo').locator('.react-flow__resize-control').count(), 0)
    await cropTools().getByLabel('裁切比例').selectOption('1:1')
    assert.equal(await size(), '3000 × 3000')
    // 拖右下角往里：固定比例下仍是正方形。
    const handle = node('photo').locator('.cc-crop-handle.is-se')
    const h = await handle.boundingBox()
    await page.mouse.move(h.x + h.width / 2, h.y + h.height / 2)
    await page.mouse.down()
    await page.mouse.move(h.x - 60, h.y - 10, { steps: 4 })
    await page.mouse.up()
    await page.screenshot({ path: '/tmp/creative-image-edit-crop.png' })
    const [w1, h1] = (await size()).split(' × ').map(Number)
    assert.equal(w1, h1)
    assert.ok(w1 < 3000)
    // 拖动选区内部只移动选区，节点不动。
    const box = await node('photo').locator('.cc-crop-box').boundingBox()
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
    await page.mouse.down()
    await page.mouse.move(box.x + box.width / 2 + 40, box.y + box.height / 2, { steps: 3 })
    await page.mouse.up()
    const after = await node('photo').boundingBox()
    assert.equal(Math.round(after.x), Math.round(before.x), '节点没有被拖动')
    // 画布放大后，同样的屏幕位移对应更少的源像素：比例换算随缩放。
    await page.keyboard.press('Meta+=')
    await page.waitForTimeout(250)
    const zoomed = await node('photo').locator('.cc-crop-box').boundingBox()
    const frame = await node('photo').locator('.cc-crop-frame').boundingBox()
    assert.ok(Math.abs(zoomed.width / frame.width - w1 / 4000) < 0.01, '缩放后选区与源像素对应')
    await cropTools().getByRole('button', { name: '重置' }).click()
    assert.equal(await size(), '4000 × 3000')
    await cropTools().getByRole('button', { name: '取消' }).click()
    await toolbar().waitFor()
    assert.equal(server.creates.length, 0, '取消不发请求')
    // Esc 也取消。
    await toolbar().getByRole('button', { name: '图片编辑' }).click()
    await page.getByRole('menu', { name: '图片编辑' }).getByRole('menuitem', { name: '裁切' }).click()
    await cropTools().waitFor()
    await node('photo').locator('.cc-crop-box').focus()
    await page.keyboard.press('Escape')
    await toolbar().waitFor()
    // 应用 16:9 裁切：请求是整数像素框，结果尺寸与工具条显示一致，节点按比例变高。
    await toolbar().getByRole('button', { name: '图片编辑' }).click()
    await page.getByRole('menu', { name: '图片编辑' }).getByRole('menuitem', { name: '裁切' }).click()
    await cropTools().getByLabel('裁切比例').selectOption('16:9')
    const shown = await size()
    await cropTools().getByRole('button', { name: '应用裁切' }).click()
    await toolbar().waitFor({ timeout: 10000 })
    const p = server.creates[0].parameters
    assert.ok(Object.values(p).every(Number.isInteger))
    assert.equal(`${p.width} × ${p.height}`, shown)
    assert.deepEqual(p, { left: 0, top: 375, width: 4000, height: 2250 })
    assert.equal(server.applies.length, 1)
    await page.waitForFunction(() => {
      const el = document.querySelector('.react-flow__node[data-id="photo"]')
      return el && Math.abs(el.getBoundingClientRect().height / el.getBoundingClientRect().width - 9 / 16) < 0.05
    })
  },

  async '窄屏：压缩全屏面板按钮可达，裁切工具条在可视区内'() {
    await fresh({ viewport: { width: 390, height: 844 } })
    await page.evaluate(() => document.querySelector('[aria-label="收起资产库"]')?.click())
    await openEdit('photo', '压缩')
    const dialog = page.getByRole('dialog', { name: '压缩图片' })
    // 等入场动画（缩放）结束再量。
    await page.waitForFunction(() => document.querySelector('.cc-compress-dialog')?.getAnimations().every((a) => a.playState === 'finished'))
    const d = await dialog.boundingBox()
    assert.ok(d.width >= 389 && d.height >= 843, `full screen ${JSON.stringify(d)}`)
    const apply = await dialog.getByRole('button', { name: '应用压缩' }).boundingBox()
    assert.ok(apply.y + apply.height <= 844 && apply.x >= 0)
    await dialog.getByRole('button', { name: '取消' }).click()
    await toolbar().getByRole('button', { name: '图片编辑' }).click()
    await page.getByRole('menu', { name: '图片编辑' }).getByRole('menuitem', { name: '裁切' }).click()
    const t = await cropTools().boundingBox()
    assert.ok(t.x >= 0 && t.x + t.width <= 390, `crop tools ${JSON.stringify(t)}`)
    const a = await cropTools().getByRole('button', { name: '应用裁切' }).boundingBox()
    assert.ok(a.x >= 0 && a.x + a.width <= 390 && a.y >= 0)
    await page.screenshot({ path: '/tmp/creative-image-edit-mobile.png' })
  },
}

let failed = 0
for (const [name, run] of Object.entries(checks)) {
  if (only && !name.includes(only)) continue
  try {
    await run()
    console.log(`✔ ${name}`)
  } catch (error) {
    failed++
    console.log(`✖ ${name}\n  ${error.stack?.split('\n').slice(0, 4).join('\n  ')}`)
    await page?.screenshot({ path: `/tmp/creative-image-edit-failure-${failed}.png` }).catch(() => {})
  }
}
await browser.close()
if (errors.length) {
  console.log('page errors:', errors)
  failed++
}
process.exit(failed ? 1 : 0)
