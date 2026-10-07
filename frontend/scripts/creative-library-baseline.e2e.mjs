// Browser regression for V6-057 (asset library browsing and saving baseline).
// All API traffic is mocked; each AC runs in a fresh browser context.
// Run against the Vite dev server: node scripts/creative-library-baseline.e2e.mjs.
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const browser = await chromium.launch({ headless: true })
const errors = []
let context, page, state
const media = (w, h) => [{ role: 'original', blob_id: 'b', mime: 'image/png', byte_size: 1, width: w, height: h, duration_ms: null }, { role: 'display', blob_id: 'b', mime: 'image/png', byte_size: 1, width: w, height: h, duration_ms: null }]
const content = (id, kind, extra = {}) => ({ id, content_id: `c-${id}`, kind, sequence: '1', payload: {}, truncated: false, ...extra })
const caps = (kind) => ({ actions: ['move', 'resize', 'rename', 'duplicate', 'remove', 'edit', 'reference', ...(kind === 'text' ? [] : ['replace', 'maximize', 'download']), 'save_to_library'], prompt_mode: 'draft', disabled_reason: null })
const makeNode = (id, kind, x, y, filled) => ({
  id, parent_id: null,
  metadata: { type_key: `core.${kind}`, type_version: 1, title: id, intent: '', x, y, width: 280, height: 200, z_order: 0 },
  prompt: null, capabilities: caps(kind),
  status: { content_state: filled ? 'ready' : 'empty', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
  placement_revision: '1', data_revision: '1',
  data: { schema_version: 1, config: {}, content_id: filled ? `c-${filled.id}` : null, content_revision_id: filled?.id ?? null, selected_version_id: null, document_id: null },
  ...(filled ? { content: filled } : {}),
})
const asset = (n, title, kind, groups, extra = {}) => ({
  id: `a${n}`, title, description: '', kind, revision: '1', content_id: `c-rev-a${n}`, content_revision_id: `rev-a${n}`,
  content: content(`rev-a${n}`, kind, kind === 'text' ? { payload: { body: `${title}正文` } } : { media: media(800, kind === 'video' ? 450 : 1000) }),
  unavailable: false, is_favorite: false, created_at: '2026-09-01T00:00:00Z', deleted_at: null, purge_after: null,
  group_ids: groups, tag_ids: [], ...extra,
})
const fixture = () => ({
  canvas: {
    id: 'qa', project_id: 'qa-project', project_name: '资产库基线', project_revision: '1', revision: '1', topology_revision: '1', archived: false,
    nodes: [
      makeNode('filled', 'image', 760, 140, content('rev-a1', 'image', { media: media(800, 1000) })),
      makeNode('empty', 'image', 1160, 140),
      makeNode('blank', 'text', 760, 520),
    ],
    edges: [], node_inputs: [], changes: [], object_states: [],
  },
  groups: [
    { id: 'g1', parent_id: null, name: '客户样片', position: 0, revision: '1' },
    { id: 'g1a', parent_id: 'g1', name: '海边婚纱', position: 0, revision: '1' },
    { id: 'g2', parent_id: null, name: '风格参考', position: 1, revision: '1' },
    { id: 'g2a', parent_id: 'g2', name: '暖调', position: 0, revision: '1' },
  ],
  // g1 自身 6 项 + 子组 1 项（a7 同时在 g2）：段内 6 格、第 6 格「+N」。
  assets: [1, 2, 3, 4, 5, 6].map((n) => asset(n, `样片${n}`, 'image', ['g1'])).concat([
    asset(7, '海边', 'image', ['g1a', 'g2']), asset(8, '短片', 'video', ['g2a']), asset(9, '客户偏好', 'text', ['g2']),
  ]),
  commands: [], posts: [], uploads: {}, failFirstUpload: true, hierarchy: 1, revision: 1, fromNode: {},
})
const page_ = (items, total = items.length) => ({ items, next_cursor: '', total_count: total, library_revision: '1' })
const upload = (id, st, extra = {}) => ({ id, state: st, io_phase: 'none', revision: '1', kind: 'image', mime: 'image/png', size: 8, file_name: 'a.png', part_size: 8, part_count: 1, uploaded_parts: [], target_kind: 'asset', expires_at: new Date(Date.now() + 3600000).toISOString(), error_code: '', publication: null, binding: null, content_revision_id: st === 'ready' ? 'rev-up' : null, created_at: new Date().toISOString(), ...extra })
async function routes(route) {
  const url = new URL(route.request().url())
  const path = url.pathname, method = route.request().method()
  const json = (value, status = 200) => route.fulfill({ status, json: value })
  const body = method === 'POST' ? route.request().postDataJSON() : null
  if (path.endsWith('/auth/refresh')) return json({ access_token: 'qa', token_type: 'Bearer', expires_in: 600 })
  if (path.endsWith('/me')) return json({ id: 'qa-libbase', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
  if (path.endsWith('/media-access-tickets')) return json({ url: 'data:image/svg+xml,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="8" height="10"><rect width="8" height="10" fill="#888"/></svg>'), expires_at: new Date(Date.now() + 3600000).toISOString() })
  if (path.endsWith('/generation-catalog')) return json({ models: [], disabled_reason: '未配置生成模型' })
  if (path.endsWith('/versions')) return json({ items: [], selected_version_id: null, data_revision: '1' })
  if (path.endsWith('/media-capabilities')) return json({ formats: [{ kind: 'image', mime: 'image/png', extensions: ['.png'] }], image_max_bytes: 1000000, av_max_bytes: 10000000, batch_limit: 10 })
  if (path.endsWith('/canvases/qa')) return json(state.canvas)
  if (path.endsWith('/commands')) {
    const actions = body?.payload?.actions ?? []
    state.commands.push(actions)
    for (const a of actions) {
      if (a.type === 'add_node') state.canvas.nodes.push({ ...makeNode(a.node_id, a.type_key.slice(5), a.x, a.y, a.asset ? content(`rev-${a.asset.asset_id}`, 'image', { media: media(800, 1000) }) : undefined), metadata: { ...makeNode(a.node_id, 'image', a.x, a.y).metadata, type_key: a.type_key, title: a.title ?? '', x: a.x, y: a.y } })
      if (a.type === 'fill_from_asset') Object.assign(state.canvas.nodes.find((n) => n.id === a.node_id), { content: content(a.asset.content_revision_id, 'image', { media: media(800, 1000) }), status: { ...state.canvas.nodes[0].status, content_state: 'ready' } })
      if (a.type === 'remove_nodes') state.canvas.nodes = state.canvas.nodes.filter((n) => !a.node_ids.includes(n.id))
    }
    state.revision++
    state.canvas.revision = String(state.revision)
    return json({ change_id: `change-${state.revision}`, result_revision: String(state.revision), before_topology_revision: '1', result_topology_revision: '1', object_results: [], created_ids: actions.filter((a) => a.type === 'add_node').map((a) => a.node_id), removed_ids: [], id_mapping: {}, omitted_reference_ids: [] })
  }
  if (method === 'POST') state.posts.push({ path, payload: body?.payload })
  if (path.endsWith('/assets/from-canvas-node')) {
    const reused = !!state.fromNode[body.payload.node_id]
    state.fromNode[body.payload.node_id] = true
    return json({ asset_id: 'a1', revision: '2', content_revision_id: 'rev-a1', library_revision: '2', tag_mapping: {}, reused })
  }
  if (path.endsWith('/creative/uploads') && method === 'POST') {
    if (state.failFirstUpload) { state.failFirstUpload = false; return json({ error: { code: 'unavailable', message: '上传服务暂时不可用' } }, 503) }
    const id = `up-${Object.keys(state.uploads).length + 1}`
    state.uploads[id] = upload(id, 'uploading')
    return json(state.uploads[id])
  }
  const up = /\/creative\/uploads\/([^/]+)(\/[a-z-]+)?$/.exec(path)
  if (up) {
    const u = state.uploads[up[1]]
    if (up[2] === '/part-authorizations') return json({ parts: body.part_numbers.map((n) => ({ part_number: n, url: `${base}/__qa_part/${n}`, method: 'PUT', headers: {} })) })
    if (up[2] === '/complete') { state.uploads[u.id] = upload(u.id, 'ready'); return json(state.uploads[u.id]) }
    return json(u)
  }
  if (path.endsWith('/asset-groups') && method === 'POST') {
    state.hierarchy++
    state.groups.push({ id: `g-new-${state.hierarchy}`, parent_id: body.payload.parent_id, name: body.payload.name, position: 9, revision: '1' })
    return json({ id: `g-new-${state.hierarchy}`, revision: '1', hierarchy_revision: String(state.hierarchy), library_revision: '1' })
  }
  if (method === 'POST') return json({})
  if (path.endsWith('/asset-groups')) return json({ items: state.groups, hierarchy_revision: String(state.hierarchy) })
  if (path.endsWith('/tags')) return json({ items: [], hierarchy_revision: String(state.hierarchy) })
  if (path.endsWith('/tag-categories')) return json({ items: [], hierarchy_revision: String(state.hierarchy) })
  if (path.endsWith('/library-settings')) return json({ retention_days: 30, revision: '1', hierarchy_revision: String(state.hierarchy), library_revision: '1' })
  if (path.endsWith('/assets')) {
    const q = url.searchParams
    let items = state.assets
    const view = q.get('view') ?? 'all'
    if (view === 'unclassified') items = items.filter((a) => !a.group_ids.length)
    if (view === 'favorites') items = items.filter((a) => a.is_favorite)
    if (view === 'trash') items = []
    const g = q.get('group_id')
    if (g) {
      const scope = q.get('include_descendants') === 'true' ? state.groups.filter((x) => x.id === g || x.parent_id === g).map((x) => x.id) : [g]
      items = items.filter((a) => a.group_ids.some((id) => scope.includes(id)))
    }
    if (q.get('kind')) items = items.filter((a) => a.kind === q.get('kind'))
    if (q.get('q')) items = items.filter((a) => a.title.includes(q.get('q')))
    return json(page_(items.slice(0, Number(q.get('limit') ?? 30)), items.length))
  }
  return json(page_([]))
}
async function fresh() {
  await context?.close()
  context = await browser.newContext({ viewport: { width: 1600, height: 1000 } })
  state = fixture()
  page = await context.newPage()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
  await page.addInitScript(() => {
    const original = window.fetch.bind(window)
    window.fetch = async (input, init) => {
      if (!String(input).endsWith('/canvases/qa/events')) return original(input, init)
      return new Response(new ReadableStream({
        start(controller) {
          window.__invalidate = () => controller.enqueue(new TextEncoder().encode('event: invalidate\ndata: {}\n\n'))
          window.__invalidate()
          init.signal.addEventListener('abort', () => controller.error(new DOMException('Aborted', 'AbortError')), { once: true })
        },
      }), { headers: { 'Content-Type': 'text/event-stream' } })
    }
  })
  await page.route('**/api/v1/**', routes)
  await page.route('**/__qa_part/**', (route) => route.fulfill({ status: 200, body: '' }))
  await page.goto(`${base}/creative/canvases/qa`)
  await page.locator('.react-flow__node[data-id="blank"]').waitFor()
}
const drawer = () => page.locator('#creative-library')
const tile = (title) => drawer().locator('.cl-tile', { has: page.getByText(title, { exact: true }) }).first()
const node = (id) => page.locator(`.react-flow__node[data-id="${id}"]`)
const openLibrary = async () => {
  await page.getByRole('button', { name: '资产库', exact: true }).click()
  await drawer().getByRole('region', { name: '客户样片' }).locator('.cl-tile').first().waitFor()
}
const closeLibrary = async () => {
  await drawer().getByRole('button', { name: '收起资产库', exact: true }).click()
  await drawer().waitFor({ state: 'hidden' })
}
const nextCommand = async (run) => {
  const count = state.commands.length
  await run()
  for (let i = 0; i < 40 && state.commands.length === count; i++) await page.waitForTimeout(100)
  assert(state.commands.length > count, 'canvas command was sent')
  return state.commands.at(-1)
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
    if (process.env.DEBUG) console.error(ac, error.message)
  }
}

try {
  // AC-01 抽屉宽 344、默认收起；四个入口都能打开；Esc 依次清空关键词、清空多选、关闭抽屉。
  await record('AC-01', async () => {
    assert.equal(await drawer().isVisible(), false, 'collapsed by default')
    at('rail entry')
    await openLibrary()
    assert.equal(Math.round((await drawer().boundingBox()).width), 344, 'width 344')
    at('Esc order')
    const search = drawer().getByRole('searchbox').or(drawer().getByPlaceholder(/搜索/)).first()
    await search.fill('样片')
    await tile('样片1').hover()
    await drawer().getByRole('button', { name: '选择资产：样片1' }).click()
    await search.focus()
    await page.keyboard.press('Escape')
    assert.equal(await search.inputValue(), '', 'first Esc clears the keyword')
    assert.equal(await drawer().locator('.cl-tile.is-on').count(), 1, 'first Esc keeps the selection')
    await page.keyboard.press('Escape')
    assert.equal(await drawer().locator('.cl-tile.is-on').count(), 0, 'second Esc clears the selection')
    assert.equal(await drawer().isVisible(), true, 'second Esc keeps the drawer')
    await page.keyboard.press('Escape')
    await drawer().waitFor({ state: 'hidden' })
    at('add menu entry')
    await page.mouse.dblclick(1450, 900)
    await page.getByRole('menuitem', { name: '从资产库添加', exact: true }).click()
    await drawer().waitFor()
    await closeLibrary()
    at('empty node entry')
    await node('empty').getByRole('button', { name: '从资产库填充' }).click()
    await drawer().getByRole('heading', { name: '选择图片' }).waitFor()
    await closeLibrary()
    at('@ entry')
    await node('filled').locator('.cc-node-header h3').click()
    const composer = page.getByRole('complementary', { name: '节点生成' })
    await composer.getByRole('button', { name: '引用参考', exact: true }).click()
    await page.getByRole('button', { name: '从资产库添加参考', exact: true }).click()
    await drawer().getByRole('heading', { name: '选择参考素材' }).waitFor()
  })
  // AC-02 平铺：根组一段最多 6 格、第 6 格「+N」；未归类为空隐藏；可折叠；子组 chip 在段内。
  await record('AC-02', async () => {
    await openLibrary()
    const client = drawer().getByRole('region', { name: '客户样片' })
    const cells = await client.locator('.cl-grid > *').count()
    assert.equal(cells, 6, `six cells in a section (got ${cells})`)
    assert.equal(await client.getByRole('button', { name: /查看「客户样片」全部 7 项/ }).innerText(), '+2', 'sixth cell is +N')
    assert.equal(await drawer().getByRole('region', { name: '未归类' }).count(), 0, 'empty unclassified section is hidden')
    assert.equal(await client.locator('.cl-sub-chips button').innerText(), '海边婚纱', 'subgroup chip inside the section')
    at('collapse')
    const toggle = client.locator('.cl-sec-toggle')
    await toggle.click()
    assert.equal(await toggle.getAttribute('aria-expanded'), 'false')
    assert.equal(await client.locator('.cl-grid').count(), 0, 'collapsed section hides its grid')
    await toggle.click()
    await client.locator('.cl-grid').waitFor()
    at('+N enters the group')
    await client.getByRole('button', { name: /查看「客户样片」全部 7 项/ }).click()
    await drawer().locator('.cl-crumb b', { hasText: '客户样片' }).waitFor()
  })

  // AC-03 进组：面包屑 + N 项、子组 chip、两列瀑布流；返回层级；计数含子组，一项可属多个分组。
  await record('AC-03', async () => {
    await openLibrary()
    const style = drawer().getByRole('region', { name: '风格参考' })
    await style.locator('.cl-sec-head').hover()
    await style.getByRole('button', { name: '查看全部', exact: true }).click()
    await drawer().locator('.cl-crumb b', { hasText: '风格参考' }).waitFor()
    // 风格参考：海边（同在客户样片）、客户偏好、子组暖调里的短片 → 3 项。
    await drawer().locator('.cl-crumb em', { hasText: '3 项' }).waitFor()
    assert.equal(await drawer().locator('.cl-masonry-col').count(), 2, 'two masonry columns')
    await drawer().locator('.cl-sub-chips').getByRole('button', { name: '暖调' }).waitFor()
    await tile('海边').waitFor()
    at('enter subgroup')
    await drawer().locator('.cl-sub-chips').getByRole('button', { name: '暖调' }).click()
    await drawer().locator('.cl-crumb b', { hasText: '暖调' }).waitFor()
    at('back to parent')
    await drawer().getByRole('button', { name: '返回', exact: true }).click()
    await drawer().locator('.cl-crumb b', { hasText: '风格参考' }).waitFor()
    at('back to all')
    await drawer().getByRole('button', { name: '返回', exact: true }).click()
    await drawer().locator('.cl-groups-bar').waitFor()
  })
  // AC-04 ⌘/Ctrl 单击或选择勾切换，Shift 按显示顺序选范围；底栏「已选 N 项 · 取消 · 放入画布」；不改画布选区。
  await record('AC-04', async () => {
    await node('filled').locator('.cc-node-header h3').click()
    await openLibrary()
    await drawer().getByRole('region', { name: '客户样片' }).getByRole('button', { name: /查看「客户样片」全部 7 项/ }).click()
    await page.waitForFunction(() => document.querySelectorAll('#creative-library .cl-masonry .cl-tile').length === 7)
    const order = await drawer().locator('.cl-masonry .cl-tile').evaluateAll((els) => els.map((e) => e.getAttribute('aria-label')))
    at('Cmd+click toggles')
    await tile(order[0]).click({ modifiers: [process.platform === 'darwin' ? 'Meta' : 'Control'] })
    assert.equal(await drawer().locator('.cl-tile.is-on').count(), 1, 'Cmd+click selects')
    await tile(order[0]).click({ modifiers: [process.platform === 'darwin' ? 'Meta' : 'Control'] })
    assert.equal(await drawer().locator('.cl-tile.is-on').count(), 0, 'Cmd+click toggles off')
    at('check + Shift range')
    await tile(order[1]).hover()
    await drawer().getByRole('button', { name: `选择资产：${order[1]}` }).click()
    await tile(order[3]).click({ modifiers: ['Shift'] })
    const on = await drawer().locator('.cl-tile.is-on').evaluateAll((els) => els.map((e) => e.getAttribute('aria-label')))
    assert.deepEqual(on.sort(), order.slice(1, 4).sort(), 'Shift selects the display-order range')
    at('footer')
    const foot = drawer().locator('.cl-foot')
    // 底栏另有基础需求 LIB-05 的「整理 / 移入回收站」批量动作；这里只验 PRD AL-20 要求的三项。
    assert.match(await foot.innerText(), /^已选 3 项/, 'footer starts with the count')
    await foot.getByRole('button', { name: '取消选择', exact: true }).waitFor()
    await foot.getByRole('button', { name: '放入画布', exact: true }).waitFor()
    assert.equal(await node('filled').evaluate((el) => el.classList.contains('selected')), true, 'canvas selection is unchanged')
    await foot.getByRole('button', { name: '取消选择', exact: true }).click()
    assert.equal(await drawer().locator('.cl-tile.is-on').count(), 0, 'cancel clears the library selection')
  })

  // AC-05 放入画布：视口中心横排间距 40，整批一步撤销并选中；记录来源资产，重复放入不重复上传。
  await record('AC-05', async () => {
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => false })
      window.dispatchEvent(new Event('offline'))
    })
    await openLibrary()
    for (const t of ['样片1', '样片2', '样片3']) {
      await tile(t).hover()
      await drawer().getByRole('button', { name: `选择资产：${t}` }).click()
    }
    await drawer().getByRole('button', { name: '放入画布', exact: true }).click()
    await page.waitForFunction(() => document.querySelectorAll('.react-flow__node').length === 6)
    const placed = await page.locator('.react-flow__node.selected').evaluateAll((els) => els.map((e) => { const m = /translate\(([-\d.]+)px,\s*([-\d.]+)px\)/.exec(e.style.transform); return { id: e.getAttribute('data-id'), x: Number(m[1]), y: Number(m[2]), w: e.getBoundingClientRect().width } }))
    assert.equal(placed.length, 3, 'the whole batch is selected')
    placed.sort((a, b) => a.x - b.x)
    assert.equal(new Set(placed.map((p) => p.y)).size, 1, 'one row')
    const zoom = await page.locator('.react-flow__viewport').evaluate((el) => Number(/scale\(([-\d.]+)\)/.exec(el.style.transform)[1]))
    assert.ok(placed.slice(1).every((p, i) => Math.abs(p.x - placed[i].x - placed[i].w / zoom - 40) < 1.5), 'gap 40 between nodes')
    at('one undo')
    await page.keyboard.press('Control+z')
    await page.waitForFunction(() => document.querySelectorAll('.react-flow__node').length === 3)
  })
  // AC-05（续）：节点记录来源资产，同一资产再放入引用同一份内容，不重新上传。
  await record('AC-05b', async () => {
    // 放入后抽屉保持打开，第二次直接复用。
    const placeOne = async () => {
      if (!(await drawer().isVisible())) await openLibrary()
      await tile('样片4').hover()
      await drawer().getByRole('button', { name: '选择资产：样片4' }).click()
      const actions = await nextCommand(() => drawer().getByRole('button', { name: '放入画布', exact: true }).click())
      return actions.filter((a) => a.type === 'add_node')
    }
    const first = await placeOne()
    const second = await placeOne()
    assert.equal(first.length, 1, 'one asset places one node')
    assert.deepEqual([first[0].asset?.asset_id, second[0].asset?.asset_id], ['a4', 'a4'], 'both nodes record the source asset')
    assert.equal(first[0].asset.content_revision_id, second[0].asset.content_revision_id, 'both reuse the same content revision')
    assert.equal(state.posts.filter((p) => p.path.includes('/creative/uploads')).length, 0, 'no upload is started')
  })

  // AC-06 填充模式：标题按类型、只列同类媒体、点一张即填入；目标已有内容时不做修改。
  await record('AC-06', async () => {
    await node('empty').getByRole('button', { name: '从资产库填充' }).click()
    await drawer().getByRole('heading', { name: '选择图片' }).waitFor()
    await drawer().locator('.cl-tile').first().waitFor()
    const kinds = await drawer().locator('.cl-tile').evaluateAll((els) => els.map((e) => e.getAttribute('aria-label')))
    assert.ok(!kinds.includes('短片') && !kinds.includes('客户偏好'), `only images are listed (got ${kinds.join(',')})`)
    at('fill')
    const fill = await nextCommand(() => tile('样片5').click())
    await drawer().waitFor({ state: 'hidden' })
    assert.deepEqual(fill.map((a) => a.type), ['fill_from_asset'])
    assert.equal(fill[0].node_id, 'empty')
    at('target filled meanwhile')
    // 先取消选中，别让生成输入框挡住后面的点击。
    await page.mouse.click(1500, 60)
    await page.getByRole('complementary', { name: '节点生成' }).waitFor({ state: 'detached' })
    const target = state.canvas.nodes.find((n) => n.id === 'empty')
    delete target.content
    target.status = { ...target.status, content_state: 'empty' }
    state.canvas.revision = String(++state.revision)
    await page.evaluate(() => window.__invalidate())
    await node('empty').getByRole('button', { name: '从资产库填充' }).click()
    await drawer().getByRole('heading', { name: '选择图片' }).waitFor()
    // 抽屉打开期间，节点在别处被填上了内容。
    Object.assign(target, { content: content('rev-other', 'image', { media: media(800, 1000) }), status: { ...target.status, content_state: 'ready' } })
    state.canvas.revision = String(++state.revision)
    await page.evaluate(() => window.__invalidate())
    await node('empty').locator('.cc-media').waitFor()
    const before = state.commands.length
    await tile('样片3').click()
    await page.waitForTimeout(600)
    assert.equal(state.commands.length, before, 'a node that already has content is not overwritten')
  })
  // AC-07 存入资产库：首项「不归类，只存入」、子组缩进紧跟父组；重复存入只追加归类；无内容节点给出提示。
  await record('AC-07', async () => {
    const save = async () => {
      await node('filled').click({ button: 'right' })
      await page.getByRole('menuitem', { name: '存入资产库…', exact: true }).click()
      return page.getByRole('dialog', { name: '存入个人资产库' })
    }
    let dialog = await save()
    const options = await dialog.getByRole('combobox', { name: /归入分组/ }).locator('option').allInnerTexts()
    assert.deepEqual(options, ['不归类，只存入', '客户样片', '　海边婚纱', '风格参考', '　暖调'], `group order (got ${options.join(' | ')})`)
    at('first save')
    await dialog.getByRole('button', { name: '保存资产', exact: true }).click()
    await page.locator('.cc-save-status').filter({ hasText: '已存入资产库' }).waitFor()
    at('repeat save files only')
    dialog = await save()
    await dialog.getByRole('combobox', { name: /归入分组/ }).selectOption('g2')
    await dialog.getByRole('button', { name: '保存资产', exact: true }).click()
    await page.locator('.cc-save-status').filter({ hasText: '已归入「风格参考」' }).waitFor()
    assert.deepEqual(state.posts.filter((p) => p.path.endsWith('/assets/from-canvas-node')).at(-1).payload.target.group_ids, ['g2'])
    at('empty node')
    await page.mouse.click(1500, 940)
    await node('empty').click({ button: 'right' })
    await page.getByRole('menu').first().waitFor()
    const hasEntry = await page.getByRole('menuitem', { name: '存入资产库…', exact: true }).count()
    await page.keyboard.press('Escape')
    if (hasEntry) throw new Error('empty node still offers saving without a hint')
    at('empty node dragged into the library')
    await openLibrary()
    const head = await drawer().getByRole('region', { name: '风格参考' }).locator('.cl-sec-head').boundingBox()
    const card = await node('blank').locator('.cc-node').boundingBox()
    await page.mouse.move(card.x + 40, card.y + 40)
    await page.mouse.down()
    await page.mouse.move(card.x - 40, card.y + 44, { steps: 4 })
    await page.mouse.move(head.x + 120, head.y + head.height / 2, { steps: 12 })
    await page.mouse.up()
    await page.locator('.cc-save-status').filter({ hasText: '还没有内容' }).waitFor()
  })
  // AC-08 分组菜单：标题「…」、面包屑、右键都能打开；含重命名/新建子分组（最多两层）/删除；「未归类」没有菜单。
  await record('AC-08', async () => {
    state.assets.push(asset(20, '散图', 'image', []))
    await openLibrary()
    const labels = async (name) => {
      const menu = page.getByRole('menu', { name: `分组操作：${name}` })
      await menu.waitFor()
      const items = await menu.getByRole('menuitem').evaluateAll((els) => els.map((e) => e.getAttribute('aria-label') || e.textContent.trim()))
      await page.keyboard.press('Escape')
      await menu.waitFor({ state: 'detached' })
      return items
    }
    at('title …')
    const sec = drawer().getByRole('region', { name: '客户样片' })
    await sec.locator('.cl-sec-head').hover()
    await sec.getByRole('button', { name: '分组操作：客户样片' }).click()
    const root = await labels('客户样片')
    for (const want of ['重命名分组', '新建子分组', '删除分组']) assert.ok(root.some((l) => l.includes(want)), `root menu has ${want} (got ${root.join(',')})`)
    at('right click')
    await sec.locator('.cl-sec-head').click({ button: 'right' })
    await labels('客户样片')
    at('breadcrumb')
    await sec.getByRole('button', { name: /查看「客户样片」全部 7 项/ }).click()
    await drawer().locator('.cl-crumb b', { hasText: '客户样片' }).waitFor()
    await drawer().locator('.cl-crumb').getByRole('button', { name: '分组操作：客户样片' }).click()
    await labels('客户样片')
    at('two levels')
    await drawer().locator('.cl-sub-chips').getByRole('button', { name: '海边婚纱' }).click()
    await drawer().locator('.cl-crumb b', { hasText: '海边婚纱' }).waitFor()
    await drawer().locator('.cl-crumb').getByRole('button', { name: '分组操作：海边婚纱' }).click()
    const child = await labels('海边婚纱')
    assert.ok(!child.some((l) => l.includes('新建子分组')), `second level has no 新建子分组 (got ${child.join(',')})`)
    at('unclassified')
    await drawer().getByRole('button', { name: '返回', exact: true }).click()
    await drawer().getByRole('button', { name: '返回', exact: true }).click()
    const unclassified = drawer().getByRole('region', { name: '未归类' })
    await unclassified.locator('.cl-tile').first().waitFor()
    await unclassified.locator('.cl-sec-head').hover()
    assert.equal(await unclassified.getByRole('button', { name: /^分组操作/ }).count(), 0, 'unclassified has no … button')
    await unclassified.locator('.cl-sec-head').click({ button: 'right' })
    await page.waitForTimeout(200)
    assert.equal(await page.getByRole('menu', { name: /^分组操作/ }).count(), 0, 'unclassified has no context menu')
  })

  // AC-09 上传到资产库：只写入资产库、不建画布节点；逐项显示进度，失败项可重试。
  await record('AC-09', async () => {
    await openLibrary()
    const nodesBefore = await page.locator('.react-flow__node').count()
    const png = Buffer.from('89504e470d0a1a0a', 'hex')
    const chooser = page.waitForEvent('filechooser')
    await drawer().getByRole('button', { name: '导入文件', exact: true }).click()
    await (await chooser).setFiles([{ name: '失败重试.png', mimeType: 'image/png', buffer: png }])
    const tray = page.getByRole('region', { name: '导入文件' }).or(page.locator('.cc-upload-tray')).first()
    await tray.getByText('失败重试', { exact: false }).waitFor()
    at('failed item retries')
    const retry = tray.getByRole('button', { name: /重试上传：失败重试/ })
    await retry.waitFor()
    await retry.click()
    await tray.getByText('已完成', { exact: false }).waitFor({ timeout: 8000 })
    at('library only')
    const created = state.posts.filter((p) => p.path.endsWith('/creative/uploads')).at(-1)
    assert.equal(created?.payload?.target?.kind, 'asset', `upload targets the library (got ${JSON.stringify(created?.payload?.target)})`)
    assert.equal(await page.locator('.react-flow__node').count(), nodesBefore, 'no canvas node is created')
    assert.ok(!state.commands.flat().some((a) => a.type === 'add_node'), 'no add_node command')
  })

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify(results, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!only && Object.keys(results).length !== 10)) process.exitCode = 1
