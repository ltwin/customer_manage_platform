// Browser regression for V6-008 (batch file import, one-step undo, cancel on
// undo, fill rules). API is mocked: /commands keeps a change list with groups
// and applies a group undo; uploads stay open until the test releases them.
// Run against the Vite dev server: node scripts/creative-file-import.e2e.mjs.
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const browser = await chromium.launch({ headless: true })
const errors = []
let context, page, server

const size = { 'core.image': [300, 300], 'core.video': [300, 300], 'core.audio': [300, 120] }
const caps = ['move', 'resize', 'rename', 'duplicate', 'remove', 'edit', 'reference', 'maximize', 'download', 'replace']
const status = (ready) => ({ content_state: ready ? 'ready' : 'empty', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' })
const media = (w, h) => [{ role: 'original', blob_id: 'b', mime: 'image/svg+xml', byte_size: 1, width: w, height: h, duration_ms: null }]
const makeNode = (id, type, x, y, title = '') => ({
  id, parent_id: null,
  metadata: { type_key: type, type_version: 1, title, intent: '', x, y, width: size[type][0], height: size[type][1], z_order: 0 },
  prompt: null,
  capabilities: { actions: caps, prompt_mode: 'draft', disabled_reason: null },
  status: status(false),
  placement_revision: '1', data_revision: '1',
  data: { schema_version: 1, config: {}, content_id: null, content_revision_id: null, selected_version_id: null, document_id: null },
})
const fill = (n, rev, kind, w = 300, h = 300) => {
  n.content = { id: rev, content_id: `c-${rev}`, kind, revision_no: '1', title: '', payload: {}, truncated: false, media: media(w, h) }
  n.data = { ...n.data, content_id: `c-${rev}`, content_revision_id: rev }
  n.status = status(true)
  n.metadata = { ...n.metadata, height: n.metadata.width * h / w }
  n.data_revision = String(Number(n.data_revision) + 1)
}
const art = `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="200"><rect width="300" height="200" fill="#666"/></svg>`
const fixture = () => {
  const photo = makeNode('photo', 'core.image', 80, 120, '晨光')
  fill(photo, 'r-photo', 'image')
  return {
    id: 'qa', project_id: 'qa-project', project_name: '导入回归', project_revision: '1',
    revision: '1', topology_revision: '1', archived: false,
    nodes: [photo, makeNode('empty', 'core.image', 460, 120), makeNode('clip', 'core.video', 840, 120)],
    edges: [], node_inputs: [], changes: [], object_states: [],
  }
}
const nodeRead = (n) => ({ kind: 'node', id: n.id, placement_revision: n.placement_revision, data_revision: n.data_revision })
const uploadView = (u) => ({
  id: u.id, state: u.state, io_phase: 'none', revision: String(u.revision), kind: u.kind, mime: u.mime, size: u.size, file_name: u.file_name,
  part_size: Math.max(u.size, 1), part_count: 1, uploaded_parts: [], target_kind: 'staged', expires_at: new Date(Date.now() + 3600000).toISOString(),
  error_code: u.state === 'cancelled' ? 'cancelled' : '', publication: u.publication ?? null, binding: u.binding ?? null,
  content_revision_id: u.state === 'ready' ? `rev-${u.id}` : null, created_at: new Date().toISOString(),
})

// 发布：暂存上传只就绪，不碰画布；由前端用 fill_from_upload 放进节点。
function release(id) {
  const u = server.uploads[id]
  if (u.state !== 'verifying') return
  u.revision++
  u.state = 'ready'
  u.publication = { kind: 'candidate', id: `stg-${id}`, revision: '1' }
  u.binding = { status: 'staged', target_kind: null, target_id: null, candidate_id: `stg-${id}`, expires_at: null }
  u.staged = 'pending'
}
function record(c) {
  const canvas = server.canvas
  canvas.revision = canvas.topology_revision = String(Number(canvas.revision) + 1)
  const change = { id: `ch-${canvas.revision}`, inverse_of: c.inverse ?? null, change_group_id: c.group ?? null, result_revision: canvas.revision, read_set: c.read ?? [] }
  canvas.changes.unshift(change)
  server.effects[change.id] = c
  return change
}
// 撤销一组或一条：删掉它们新建的节点，清空它们填入的内容。
function undo(payload) {
  const canvas = server.canvas
  const targets = payload.change_group_id
    ? canvas.changes.filter((c) => c.change_group_id === payload.change_group_id && !c.inverse_of)
    : canvas.changes.filter((c) => c.id === payload.change_id)
  const removed = []
  for (const c of targets) {
    const e = server.effects[c.id]
    for (const id of e.created ?? []) {
      canvas.nodes = canvas.nodes.filter((n) => n.id !== id)
      removed.push(id)
    }
    const n = canvas.nodes.find((v) => v.id === e.filled)
    if (n) {
      delete n.content
      n.data = { ...n.data, content_id: null, content_revision_id: null }
      n.status = status(false)
      n.data_revision = String(Number(n.data_revision) + 1)
    }
  }
  const last = targets.sort((a, b) => Number(a.result_revision) - Number(b.result_revision)).at(-1)
  const change = record({ inverse: last.id })
  return { change, removed }
}
async function fresh({ offline = false, hold = false } = {}) {
  await context?.close()
  context = await browser.newContext({ viewport: { width: 1600, height: 1000 } })
  page = await context.newPage()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
  // hold：上传完成后停在 verifying，由测试决定何时发布。
  server = { canvas: fixture(), posts: [], other: [], uploads: {}, effects: {}, hold }
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
  await page.route('**/__qa_part/**', (route) => route.fulfill({ status: 200, body: '' }))
  await page.route('**/api/v1/**', async (route) => {
    const req = route.request()
    const path = new URL(req.url()).pathname
    const json = (value, status = 200) => route.fulfill({ status, json: value })
    const body = req.method() === 'POST' ? req.postDataJSON() : null
    const canvas = server.canvas
    if (path.endsWith('/auth/refresh')) return json({ access_token: 'qa', token_type: 'Bearer', expires_in: 600 })
    if (path.endsWith('/me')) return json({ id: 'qa-import', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
    if (path.endsWith('/generation/catalog')) return json({ models: [], disabled_reason: '' })
    if (path.endsWith('/versions')) return json({ items: [], selected_version_id: null, data_revision: '1' })
    if (path.includes('/content-urls') || path.includes('/download-url') || path.includes('/media-url') || path.endsWith('/media-access-tickets'))
      return json({ url: `data:image/svg+xml,${encodeURIComponent(art)}`, expires_at: new Date(Date.now() + 3600000).toISOString() })
    if (path.endsWith('/canvases/qa')) return json(canvas)
    if (path.endsWith('/commands')) {
      // gate：测试可以先扣住命令，制造「填充与撤销同时在路上」的时序。
      while (server.gate) await new Promise((r) => setTimeout(r, 30))
      const payload = body?.payload ?? {}
      server.posts.push(payload)
      if (payload.type === 'undo') {
        const { change, removed } = undo(payload)
        return json({ change_id: change.id, result_revision: canvas.revision, before_topology_revision: canvas.revision, result_topology_revision: canvas.topology_revision, object_results: removed.map((id) => ({ kind: 'node', id, is_live: false })), created_ids: [], removed_ids: removed, id_mapping: {}, omitted_reference_ids: [] })
      }
      const created = []
      let filled = null
      for (const a of payload.actions ?? []) {
        if (a.type === 'add_node') {
          canvas.nodes.push(makeNode(a.node_id, a.type_key, a.x, a.y, a.title ?? ''))
          created.push(a.node_id)
        }
        if (a.type === 'fill_from_upload') {
          const u = server.uploads[a.upload_id]
          const n = canvas.nodes.find((v) => v.id === a.node_id)
          // 与后端一致：暂存必须就绪且没用过，节点必须存在且为空。
          if (!u || u.staged !== 'pending' || !n || n.content) return json({ error: { code: 'creative_revision_conflict', message: '版本冲突' } }, 409)
          u.staged = 'placed'
          fill(n, `rev-${u.id}`, u.kind, 300, 200)
          if (!n.metadata.title) n.metadata.title = u.file_name.replace(/\.[^.]+$/, '')
          filled = n.id
        }
      }
      const touched = [...created, ...(filled ? [filled] : [])]
      const change = record({ group: payload.change_group_id ?? null, created, filled, read: touched.map((id) => nodeRead(canvas.nodes.find((n) => n.id === id))) })
      return json({ change_id: change.id, result_revision: canvas.revision, before_topology_revision: canvas.revision, result_topology_revision: canvas.topology_revision, object_results: touched.map((id) => ({ ...nodeRead(canvas.nodes.find((n) => n.id === id)), is_live: true })), created_ids: created, removed_ids: [], id_mapping: {}, omitted_reference_ids: [] })
    }
    if (path.endsWith('/creative/uploads') && req.method() === 'POST') {
      if (server.failNext) {
        server.failNext = false
        return json({ error: { code: 'unavailable', message: '上传服务暂时不可用' } }, 503)
      }
      const p = body.payload
      const id = `up-${Object.keys(server.uploads).length + 1}`
      server.uploads[id] = { id, state: 'uploading', revision: 1, kind: p.kind, mime: p.mime, size: p.size, file_name: p.file_name, target: p.target }
      server.posts.push({ upload: id, target: p.target })
      return json(uploadView(server.uploads[id]))
    }
    const up = /\/creative\/uploads\/([^/]+)(\/[a-z-]+)?$/.exec(path)
    if (up) {
      const u = server.uploads[up[1]]
      if (up[2] === '/part-authorizations') return json({ upload_id: u.id, parts: body.part_numbers.map((n) => ({ part_number: n, url: `${base}/__qa_part/${n}`, method: 'PUT', headers: {}, expires_at: new Date(Date.now() + 3600000).toISOString() })) })
      if (up[2] === '/complete') {
        u.state = 'verifying'
        u.revision++
        if (!server.hold) release(u.id)
        return json(uploadView(u))
      }
      if (up[2] === '/cancel') {
        server.other.push(`cancel:${u.id}`)
        // cancelFails：会话已进入发布，取消没赶上。
        if (server.cancelFails || !['created', 'uploading', 'verifying'].includes(u.state)) return json({ error: { code: 'creative_state', message: '状态已变化' } }, 409)
        u.state = 'cancelled'
        u.revision++
        return json(uploadView(u))
      }
      return json(uploadView(u))
    }
    const discard = /\/upload-candidates\/([^/]+)\/discard$/.exec(path)
    if (discard) {
      server.other.push(`discard:${discard[1]}`)
      const u = Object.values(server.uploads).find((v) => `stg-${v.id}` === discard[1])
      if (u?.staged !== 'pending') return json({ error: { code: 'creative_state', message: '状态已变化' } }, 409)
      u.staged = 'discarded'
      return json({})
    }
    if (req.method() !== 'GET') server.other.push(path)
    if (path.endsWith('/library-settings')) return json({ revision: '1', view_mode: 'grid', sort: 'updated_desc', thumbnail_size: 'medium' })
    if (path.endsWith('/media-capabilities'))
      return json({ formats: [{ kind: 'image', mime: 'image/png', extensions: ['.png'] }, { kind: 'video', mime: 'video/mp4', extensions: ['.mp4'] }], image_max_bytes: 10485760, av_max_bytes: 104857600, batch_limit: 3 })
    return json({ items: [], next_cursor: '', total_count: 0, library_revision: '1', hierarchy_revision: '1' })
  })
  await page.goto(`${base}/creative/canvases/qa`)
  await page.locator('.react-flow__node[data-id="clip"]').waitFor()
  await page.waitForTimeout(300)
  if (offline)
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => false })
      window.dispatchEvent(new Event('offline'))
    })
}
const node = (id) => page.locator(`.react-flow__node[data-id="${id}"]`)
const ids = () => page.locator('.react-flow__node').evaluateAll((els) => els.map((e) => e.getAttribute('data-id')))
const selected = () => page.locator('.react-flow__node.selected').evaluateAll((els) => els.map((e) => e.getAttribute('data-id')))
const original = ['photo', 'empty', 'clip']
const added = async () => (await ids()).filter((id) => !original.includes(id))
const blank = { x: 900, y: 700 }
const zoom = () => page.locator('.react-flow__viewport').evaluate((el) => new DOMMatrix(getComputedStyle(el).transform).a)
const until = async (fn, label, timeout = 5000) => {
  const end = Date.now() + timeout
  while (Date.now() < end) {
    if (await fn()) return
    await page.waitForTimeout(50)
  }
  throw new Error(`timeout: ${label}`)
}
// 用真实 DataTransfer 在屏幕坐标处派发 dragover/drop。
async function drop(files, at = blank) {
  await page.evaluate(async ({ x, y, files }) => {
    const dt = new DataTransfer()
    for (const f of files) {
      // 图片用真实 PNG，本机预览才能读出尺寸；其余用占位字节。
      let body = new Uint8Array(f.size ?? 8)
      if (f.type === 'image/png') {
        const c = new OffscreenCanvas(f.w ?? 300, f.h ?? 200)
        c.getContext('2d').fillRect(0, 0, 10, 10)
        body = await c.convertToBlob({ type: 'image/png' })
      }
      dt.items.add(new File([body], f.name, { type: f.type }))
    }
    const target = document.elementFromPoint(x, y)
    for (const type of ['dragover', 'drop'])
      target.dispatchEvent(new DragEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: y, dataTransfer: dt }))
  }, { ...at, files })
}
const tray = () => page.getByRole('region', { name: '导入文件' })
const trayRows = () => tray().locator('li').evaluateAll((els) => els.map((li) => [li.querySelector('.cc-upload-name')?.textContent, li.querySelector('small')?.textContent]))
const uploadsPosted = () => server.posts.filter((p) => p.upload)
// 点画布上方空白把焦点还给画布（选中节点时下方是生成面板）。
const undoKey = async () => {
  await page.mouse.click(1300, 90)
  await page.keyboard.press('Meta+z')
}

const results = {}
let step = ''
const at = (label) => { step = label }
const only = process.env.ONLY?.split(',')
const check = async (ac, fn, opts) => {
  if (only && !only.includes(ac)) return
  step = ''
  try {
    await fresh(opts)
    await fn()
    results[ac] = 'pass'
  } catch (error) {
    results[ac] = `FAIL${step ? ` @${step}` : ''}: ${String(error.message).split('\n')[0]}`
  } finally {
    if (server) server.gate = false
  }
}
const png = (name, w = 300, h = 200) => ({ name, type: 'image/png', w, h })
const mp4 = (name) => ({ name, type: 'video/mp4' })
const verifying = (n) => until(() => Object.values(server.uploads).filter((u) => u.state === 'verifying').length >= n, `${n} uploads verifying`)
const fills = () => server.posts.filter((p) => p.type === 'batch' && p.actions.some((a) => a.type === 'fill_from_upload'))
const conflictShown = async () => (await page.locator('.cc-save-state').getAttribute('data-state')) === 'conflict' || (await page.getByText(/冲突/).count()) > 0
const localPreview = (id) => node(id).locator('.cc-local-media')

try {
  // AC-01 横排 330、标题去扩展名、整批选中；拖入即显示本机预览（变暗+进度）；混合格式逐项说明。
  await check('AC-01', async () => {
    at('drop')
    await drop([png('晨光.v2.png'), { name: 'notes.txt', type: 'text/plain' }, mp4('街拍.mp4'), png('c.png'), png('d.png')])
    await until(async () => (await added()).length === 3, 'three nodes')
    const [first] = await added()
    at('local preview at once')
    await localPreview(first).waitFor()
    assert.match(await localPreview(first).getAttribute('aria-label'), /^上传中 \d+%$/, 'dimmed with progress while uploading')
    assert.equal(await localPreview(first).locator('img').count(), 1, 'shows the local image')
    await until(() => server.posts.some((p) => p.type === 'batch'), 'batch sent')
    const batch = server.posts.find((p) => p.type === 'batch')
    assert.ok(batch.change_group_id, 'add batch carries a change group')
    const adds = batch.actions
    assert.deepEqual(adds.map((a) => [a.type_key, a.title]), [['core.image', '晨光.v2'], ['core.video', '街拍'], ['core.image', 'c']])
    assert.deepEqual(adds.map((a) => Math.round(a.x - adds[0].x)), [0, 330, 660], 'spaced 330 apart')
    at('first node centred on the drop point, height from the local image')
    const box = await node(adds[0].node_id).locator('.cc-node').boundingBox()
    assert.ok(Math.abs(box.x + box.width / 2 - blank.x) < 3, 'centred on drop')
    assert.ok(Math.abs(box.height / box.width - 200 / 300) < 0.02, `3:2 before the server answers (${box.width}x${box.height})`)
    assert.deepEqual((await selected()).sort(), adds.map((a) => a.node_id).sort(), 'whole batch selected')
    at('rejections explained')
    const rows = await trayRows()
    assert.deepEqual(rows.filter(([n]) => n === 'notes.txt' || n === 'd.png'), [['notes.txt', '暂不支持此文件格式'], ['d.png', '一次最多导入 3 个文件']])
    assert.equal(await tray().getByRole('button', { name: '重试上传：notes.txt' }).count(), 0, 'nothing to retry for a skipped file')
    at('uploads are staged, fills are user edits in the batch group')
    await verifying(3)
    assert.deepEqual(uploadsPosted().map((p) => p.target), [{ kind: 'staged' }, { kind: 'staged' }, { kind: 'staged' }])
    for (const id of Object.keys(server.uploads)) release(id)
    await until(() => fills().length === 3, 'three fills sent')
    assert.ok(fills().every((p) => p.change_group_id === batch.change_group_id), 'fills join the batch group')
    await until(() => server.canvas.nodes.filter((n) => n.content).length === 4, 'all filled')
    await until(async () => (await localPreview(first).count()) === 0, 'local preview gone once the server image loads')
  }, { hold: true })

  // AC-02 整批一步撤销，任何时机都不冲突：填充与撤销同时在路上也一样。
  await check('AC-02', async () => {
    at('drop two, one ready while the canvas is busy')
    await drop([png('a.png'), png('b.png')])
    await verifying(2)
    await until(() => server.posts.some((p) => p.type === 'batch'), 'add sent')
    await page.waitForTimeout(400)
    server.gate = true
    release('up-1')
    await page.waitForTimeout(400)
    at('undo while the fill is in flight')
    await undoKey()
    server.gate = false
    await until(async () => (await added()).length === 0, 'batch removed')
    await page.waitForTimeout(800)
    assert.ok(!(await conflictShown()), 'no conflict prompt')
    // 填充还没发出时被一起撤回，服务器只需撤销建节点那一条；已发出则按组撤销。
    const undo = server.posts.find((p) => p.type === 'undo')
    assert.ok(undo, 'one undo for the whole batch')
    await until(() => server.uploads['up-2'].state === 'cancelled', 'running upload cancelled')
    assert.equal(server.canvas.nodes.length, 3, 'no empty node left behind')
    assert.equal(uploadsPosted().length, 2, 'no duplicate upload')
    assert.equal(server.posts.filter((p) => p.type === 'undo').length, 1, 'a single undo')
  }, { hold: true })
  await check('AC-02 rapid', async () => {
    at('drop and undo at once')
    await drop([png('a.png'), png('b.png'), png('c.png')])
    await until(async () => (await added()).length === 3, 'local nodes')
    await undoKey()
    await until(async () => (await added()).length === 0, 'undone before anything was sent')
    at('drop again, undo after it is all filled')
    await drop([png('d.png')])
    await until(() => fills().length === 1, 'filled')
    await page.waitForTimeout(400)
    await undoKey()
    await until(async () => (await added()).length === 0, 'second batch undone')
    await page.waitForTimeout(600)
    assert.ok(!(await conflictShown()), 'no conflict prompt')
    assert.equal(server.canvas.nodes.length, 3)
    assert.ok(Object.values(server.uploads).filter((u) => u.file_name !== 'd.png').every((u) => u.state === 'cancelled' || u.staged === 'discarded'), 'undone uploads cancelled or discarded')
  })
  // 撤销后节点立刻消失，不先变成空节点再等服务器回执。
  await check('AC-02 instant', async () => {
    await drop([png('a.png'), png('b.png')])
    await verifying(2)
    for (const id of Object.keys(server.uploads)) release(id)
    await until(() => server.canvas.nodes.filter((n) => n.content).length === 3, 'both filled')
    await page.waitForTimeout(500)
    const batch = await added()
    server.gate = true
    // 观察撤销后的每一帧：节点要么带内容在，要么已经不在，不能出现空节点。
    await page.evaluate((ids) => {
      window.__sawEmpty = false
      const watch = () => {
        for (const id of ids)
          if (document.querySelector(`.react-flow__node[data-id="${id}"] .cc-media-empty`)) window.__sawEmpty = true
        if (!window.__stop) requestAnimationFrame(watch)
      }
      requestAnimationFrame(watch)
    }, batch)
    await undoKey()
    try {
      await until(async () => (await added()).length === 0, 'gone before the server answers', 1500)
      assert.ok(!server.posts.some((p) => p.type === 'undo'), 'undo still held at the server')
    } finally {
      server.gate = false
    }
    await until(() => server.posts.some((p) => p.type === 'undo'), 'undo sent')
    await page.waitForTimeout(600)
    const sawEmpty = await page.evaluate(() => { window.__stop = true; return window.__sawEmpty })
    assert.equal(sawEmpty, false, 'never rendered as an empty node')
    assert.equal((await added()).length, 0, 'stays gone after the receipt')
    assert.ok(!(await conflictShown()), 'no conflict prompt')
  }, { hold: true })
  await check('AC-02 offline', async () => {
    await drop([png('a.png'), png('b.png')])
    await until(async () => (await added()).length === 2, 'local nodes')
    await undoKey()
    await until(async () => (await added()).length === 0, 'cancelled locally')
  }, { offline: true })
  // AC-03 撤销后迟到的成果不写回节点，暂存直接放弃，不提示。
  await check('AC-03', async () => {
    await drop([png('late.png')])
    await verifying(1)
    server.cancelFails = true
    await page.waitForTimeout(600)
    await undoKey()
    await until(async () => (await added()).length === 0, 'node undone')
    await until(() => server.other.includes('cancel:up-1'), 'cancel attempted')
    release('up-1')
    await until(() => server.uploads['up-1'].staged === 'discarded', 'late staged upload discarded')
    await page.waitForTimeout(400)
    assert.equal(fills().length, 0, 'no fill sent after undo')
    assert.equal((await added()).length, 0, 'late result never comes back')
    assert.equal(await page.getByText('目标已变化').count(), 0, 'no candidate prompt')
  }, { hold: true })

  // AC-04 已有内容的节点照常新建；同类空节点填充；视频节点收视频；失败可重试。
  await check('AC-04', async () => {
    const centre = async (id) => {
      const b = await node(id).locator('.cc-node').boundingBox()
      return { x: b.x + b.width / 2, y: b.y + b.height / 2 }
    }
    at('onto filled photo')
    await drop([png('over.png')], await centre('photo'))
    await until(async () => (await added()).length === 1, 'new node instead of overwrite')
    await until(() => fills().length === 1, 'fill sent')
    assert.notEqual(fills()[0].actions[0].node_id, 'photo', 'filled node untouched')
    at('mp4 fills the empty video node')
    const before = server.posts.filter((p) => p.type === 'batch' && p.actions.some((a) => a.type === 'add_node')).length
    await drop([mp4('片段.mp4')], await centre('clip'))
    await until(() => fills().some((p) => p.actions[0].node_id === 'clip'), 'video fills clip')
    assert.equal(server.posts.filter((p) => p.type === 'batch' && p.actions.some((a) => a.type === 'add_node')).length, before, 'no node created for a fill')
    at('mp4 onto empty image creates a node')
    // 落在空图片节点的上沿：新节点以落点为中心，不会盖住下面再填的位置。
    const top = await node('empty').locator('.cc-node').boundingBox()
    await drop([mp4('错类.mp4')], { x: top.x + top.width / 2, y: top.y + 20 })
    await until(async () => (await added()).length === 2, 'mismatched kind creates a node')
    at('failed fill shows on the node and can be retried')
    server.failNext = true
    // 先取消选中，收起盖在节点下半部的生成面板。
    await page.mouse.click(1300, 90)
    const e = await node('empty').locator('.cc-node').boundingBox()
    await drop([png('重试.png')], { x: e.x + e.width / 2, y: e.y + e.height - 30 })
    const retry = tray().getByRole('button', { name: '重试上传：重试.png' })
    await retry.waitFor()
    await until(async () => (await localPreview('empty').getAttribute('aria-label')) === '上传失败', 'failure shown on the node')
    assert.ok(!server.canvas.nodes.find((n) => n.id === 'empty').content, 'node stays empty after failure')
    await retry.click()
    await until(() => !!server.canvas.nodes.find((n) => n.id === 'empty').content, 'retry fills the node')
  })

  // AC-05 填充后标题取文件名、高度按比例；一步撤销回到空节点；上传中撤销即取消。
  await check('AC-05', async () => {
    const at2 = async () => {
      const b = await node('empty').locator('.cc-node').boundingBox()
      return { x: b.x + b.width / 2, y: b.y + b.height / 2 }
    }
    await drop([png('窗边.png')], await at2())
    await until(() => !!server.canvas.nodes.find((n) => n.id === 'empty').content, 'filled')
    await until(async () => (await node('empty').locator('.cc-node-header h3').textContent()) === '窗边', 'title from file name')
    const z = await zoom()
    await until(async () => Math.abs((await node('empty').locator('.cc-node').boundingBox()).height - 200 * z) < 3, 'height follows 3:2 media')
    await page.waitForTimeout(600)
    await undoKey()
    await until(() => !server.canvas.nodes.find((n) => n.id === 'empty').content, 'undo empties the node')
    at('undo while the single fill is uploading cancels it')
    server.hold = true
    await page.waitForTimeout(400)
    await drop([png('再来.png')], await at2())
    await localPreview('empty').waitFor()
    await until(() => Object.values(server.uploads).some((u) => u.file_name === '再来.png' && u.state === 'verifying'), 'second upload verifying')
    await undoKey()
    await until(async () => (await localPreview('empty').count()) === 0, 'local preview removed')
    const second = Object.values(server.uploads).find((u) => u.file_name === '再来.png')
    await until(() => second.state === 'cancelled', 'upload cancelled')
    assert.equal(server.posts.filter((p) => p.type === 'undo').length, 1, 'the earlier fill was not undone twice')
    assert.ok(!(await conflictShown()), 'no conflict prompt')
  })

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify({ results }, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!process.env.ONLY && Object.keys(results).length !== 8)) process.exitCode = 1
