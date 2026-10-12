// Browser regression for the canvas asset drawer, rebuilt on the standalone
// asset library components (cards, filters, tree, menus, viewer). All API traffic is mocked; run
// against the Vite dev server: node scripts/creative-library.e2e.mjs.
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const browser = await chromium.launch({ headless: true })
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } })
page.setDefaultTimeout(8000)
const errors = []
page.on('pageerror', (error) => errors.push(error.message))

const caps = (kind) => ({
  actions: ['move', 'resize', 'rename', 'duplicate', 'remove', 'edit', 'reference', ...(kind === 'text' ? [] : ['replace', 'maximize', 'download']), 'save_to_library'],
  prompt_mode: 'draft',
  disabled_reason: null,
})
const media = (w, h) => [{ role: 'original', blob_id: 'b', mime: 'image/png', byte_size: 1, width: w, height: h, duration_ms: null }, { role: 'display', blob_id: 'b', mime: 'image/png', byte_size: 1, width: w, height: h, duration_ms: null }]
const content = (id, kind, extra = {}) => ({ id, content_id: `c-${id}`, kind, sequence: '1', payload: {}, truncated: false, ...extra })
const makeNode = (id, kind, x, y, filled) => ({
  id,
  parent_id: null,
  metadata: { type_key: `core.${kind}`, type_version: 1, title: id === 'empty' ? '空图片' : id === 'empty2' ? '' : id, intent: '', x, y, width: 280, height: 180, z_order: 0 },
  prompt: null,
  capabilities: caps(kind),
  status: { content_state: filled ? 'ready' : 'empty', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
  placement_revision: '1',
  data_revision: '1',
  data: { schema_version: 1, config: {}, content_id: filled ? `c-${filled.id}` : null, content_revision_id: filled?.id ?? null, selected_version_id: null, document_id: null },
  ...(filled ? { content: filled } : {}),
})
const canvas = {
  id: 'qa', project_id: 'qa-project', project_name: '资产库回归', project_revision: '1', revision: '1', topology_revision: '1', archived: false,
  nodes: [
    makeNode('filled', 'image', 700, 120, content('rev-a1', 'image', { media: media(1000, 1500) })),
    makeNode('empty', 'image', 1100, 120),
    makeNode('empty2', 'image', 1100, 420),
    { ...makeNode('brief', 'text', 700, 560), content: content('rev-brief', 'text', { payload: { body: '一句提示' } }), data: { ...makeNode('brief', 'text', 0, 0).data, content_id: 'c-rev-brief', content_revision_id: 'rev-brief' } },
  ],
  edges: [], node_inputs: [], changes: [], object_states: [],
}
const groupDeletions = new Map()
const receipts = new Map()
let loseGroupDelete = false
const groups = [
  { id: 'g1', parent_id: null, name: '客户样片', position: 0, revision: '1' },
  { id: 'g1a', parent_id: 'g1', name: '海边婚纱', position: 0, revision: '1' },
  { id: 'g2', parent_id: null, name: '风格参考', position: 1, revision: '1' },
]
const tags = [{ id: 't1', name: '逆光', color: '#e2a453', category_id: null, revision: '1' }]
const asset = (n, title, kind = 'image', w = 800, h = 1000, extra = {}) => ({
  id: `a${n}`, title, description: '', kind, revision: '1', content_id: `c-rev-a${n}`, content_revision_id: `rev-a${n}`,
  content: content(`rev-a${n}`, kind, kind === 'text' ? { payload: { body: `${title}的正文` } } : { media: media(w, h) }),
  unavailable: false, is_favorite: n === 1, created_at: '2026-09-01T00:00:00Z', deleted_at: null, purge_after: null,
  group_ids: n <= 7 ? ['g1'] : n <= 9 ? ['g2'] : [], tag_ids: n === 2 ? ['t1'] : [], ...extra,
})
const all = [
  asset(1, '日落海边', 'image', 1000, 1500), asset(2, '逆光发丝'), asset(3, '白裙', 'image', 1200, 800), asset(4, '花艺'),
  asset(5, '城市夜景', 'image', 900, 600), asset(6, '礁石'), asset(7, '海风'), asset(8, '胶片暖调'), asset(9, '冷调', 'image', 600, 900),
  asset(10, '客户偏好', 'text'),
  asset(11, '现场环境音', 'audio', 0, 0, { rating: 4.5, content: content('rev-a11', 'audio', { media: [{ role: 'original', blob_id: 'b', mime: 'audio/mpeg', byte_size: 1, width: null, height: null, duration_ms: 184000 }] }) }),
  asset(12, '布光参考', 'link', 0, 0, { content: content('rev-a12', 'link', { payload: { url: 'https://www.example.com/lighting' } }) }),
]
const page_ = (items, total = items.length) => ({ items, next_cursor: '', total_count: total, library_revision: '1' })
const assetQueries = []
const commands = []
const posts = []
let revision = 1
let hierarchy = 1
// 模拟另一窗口抢先占用的分组名：本机目录里没有，只有服务端会拒绝。
const takenOnServer = new Set(['棚拍布光'])

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
  const url = new URL(route.request().url())
  const path = url.pathname
  const json = (value, status = 200) => route.fulfill({ status, json: value })
  const method = route.request().method()
  if (path.endsWith('/auth/refresh')) return json({ access_token: 'qa-mock-token', token_type: 'Bearer', expires_in: 600 })
  if (path.endsWith('/me')) return json({ id: 'qa-library', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
  if (path.endsWith('/media-access-tickets')) {
    const art = '<svg xmlns="http://www.w3.org/2000/svg" width="400" height="500"><rect width="400" height="500" fill="#8a8f86"/><circle cx="280" cy="140" r="60" fill="#d79768"/></svg>'
    return json({ url: `data:image/svg+xml,${encodeURIComponent(art)}`, expires_at: new Date(Date.now() + 3600000).toISOString() })
  }
  if (path.endsWith('/generation/catalog')) return json({ models: [], disabled_reason: '未配置生成模型' })
  if (path.endsWith('/versions')) return json({ items: [], selected_version_id: null, data_revision: '1' })
  if (path.endsWith('/media-capabilities'))
    return json({ formats: [{ kind: 'image', mime: 'image/png', extensions: ['.png'] }], image_max_bytes: 1000000, av_max_bytes: 10000000, batch_limit: 10 })
  if (path.includes('/operations/')) return receipts.has(path.split('/').at(-1)) ? json({ response: receipts.get(path.split('/').at(-1)) }) : json({}, 404)
  if (/\/assets\/[^/]+$/.test(path) && method === 'GET') return json(all.find((a) => a.id === path.split('/').at(-1)))
  if (path.endsWith('/canvases/qa')) return json(canvas)
  if (path.endsWith('/commands')) {
    const actions = route.request().postDataJSON()?.payload?.actions ?? []
    commands.push(actions)
    revision++
    // 像服务端一样落库并推进修订，否则快照永远追不上回执。
    for (const a of actions) {
      const src = all.find((x) => x.id === a.asset?.asset_id)
      if (a.type === 'add_node') {
        const n = makeNode(a.node_id, a.type_key.slice(5), a.x, a.y, src?.content)
        n.metadata.title = a.title ?? ''
        canvas.nodes.push(n)
      } else if (a.type === 'fill_from_asset') {
        const n = canvas.nodes.find((x) => x.id === a.node_id)
        Object.assign(n, makeNode(n.id, 'image', n.metadata.x, n.metadata.y, src.content), { metadata: { ...n.metadata, title: n.metadata.title || src.title } })
      } else if (a.type === 'connect_reference')
        canvas.edges.push({ id: `e-${canvas.edges.length}`, source_node_id: a.source_node_id, target_node_id: a.target_node_id, source_port: 'output', target_port: 'reference', role: 'reference', ordinal: 0, revision: '1' })
    }
    canvas.revision = String(revision)
    return json({
      change_id: `change-${revision}`, result_revision: String(revision), before_topology_revision: '1', result_topology_revision: '1',
      object_results: [], created_ids: actions.filter((a) => a.type === 'add_node').map((a) => a.node_id), removed_ids: [], id_mapping: {}, omitted_reference_ids: [],
    })
  }
  if (method === 'POST') {
    const body = route.request().postDataJSON()
    posts.push({ path, payload: body?.payload })
    if (path.endsWith('/assets/batch-trash')) {
      for (const item of body.payload.assets) { const a = all.find((a) => a.id === item.asset_id); a.deleted_at = new Date().toISOString(); a.revision = String(Number(a.revision) + 1) }
      return json({ library_revision: '3' })
    }
    if (/\/assets\/[^/]+\/restore$/.test(path)) {
      const a = all.find((a) => a.id === path.split('/').at(-2)); a.deleted_at = null; a.revision = String(Number(a.revision) + 1)
      return json({ id: a.id, revision: a.revision, library_revision: '4' })
    }
    if (path.endsWith('/asset-groups')) {
      if (takenOnServer.has(body.payload.name))
        return json({ error: { code: 'creative_group_name_taken', message: '同一层已有同名分组' } }, 409)
      hierarchy++
      groups.push({ id: `g-new-${hierarchy}`, parent_id: body.payload.parent_id, name: body.payload.name, position: 9, revision: '1' })
      return json({ id: `g-new-${hierarchy}`, revision: '1', hierarchy_revision: String(hierarchy), library_revision: '1' })
    }
    if (/\/asset-groups\/[^/]+\/delete$/.test(path)) {
      const id = path.split('/').at(-2), index = groups.findIndex((g) => g.id === id)
      groupDeletions.set('d-' + id, { group: structuredClone(groups[index]), children: groups.filter((g) => g.parent_id === id).map((g) => g.id), members: all.filter((a) => a.group_ids.includes(id)).map((a) => a.id) })
      groups.splice(index, 1); groups.forEach((g) => { if (g.parent_id === id) g.parent_id = null }); all.forEach((a) => { a.group_ids = a.group_ids.filter((g) => g !== id) }); hierarchy++
      const receipt = { id, revision: '2', hierarchy_revision: String(hierarchy), library_revision: '2', deletion_id: 'd-' + id }
      receipts.set(route.request().postDataJSON().operation_id, receipt)
      if (loseGroupDelete) { loseGroupDelete = false; return route.abort('failed') }
      return json(receipt)
    }
    if (/\/asset-groups\/deletions\/[^/]+\/restore$/.test(path)) {
      const d = groupDeletions.get(path.split('/').at(-2)); groups.push(d.group); groups.forEach((g) => { if (d.children.includes(g.id)) g.parent_id = d.group.id }); all.forEach((a) => { if (d.members.includes(a.id)) a.group_ids.push(d.group.id) }); hierarchy++
      return json({ id: d.group.id, revision: '3', hierarchy_revision: String(hierarchy), library_revision: '3' })
    }
    if (path.endsWith('/assets/batch-organize')) {
      for (const item of body.payload.assets) {
        const a = all.find((a) => a.id === item.asset_id)
        a.group_ids = [...new Set([...a.group_ids.filter((g) => !(body.payload.remove_group_ids ?? []).includes(g)), ...(body.payload.add_group_ids ?? [])])]
        a.revision = String(Number(a.revision) + 1)
      }
      return json({ library_revision: '5' })
    }
    if (path.endsWith('/assets/from-canvas-node'))
      return json({ asset_id: 'a1', revision: '2', content_revision_id: 'rev-a1', library_revision: '2', tag_mapping: {}, reused: true })
    return json({})
  }
  if (path.endsWith('/asset-groups')) {
    const counts = Object.fromEntries(groups.map((g) => [g.id, all.filter((a) => !a.deleted_at && a.group_ids.some((id) => id === g.id || groups.find((x) => x.id === id)?.parent_id === g.id)).length]))
    return json({ items: groups, hierarchy_revision: String(hierarchy), asset_counts: counts })
  }
  if (path.endsWith('/library-usage')) return json({ asset_count: all.length, stored_bytes: 1024 ** 3, limit_bytes: 20 * 1024 ** 3 })
  if (path.endsWith('/tags')) return json({ items: tags.map((t) => ({ ...t, asset_count: all.filter((a) => a.tag_ids.includes(t.id)).length })), hierarchy_revision: String(hierarchy) })
  if (path.endsWith('/tag-categories')) return json({ items: [], hierarchy_revision: String(hierarchy) })
  if (path.endsWith('/library-settings')) return json({ retention_days: 30, revision: '1', hierarchy_revision: String(hierarchy), library_revision: '1' })
  if (path.endsWith('/assets')) {
    const q = url.searchParams
    assetQueries.push(q.toString())
    let items = all.filter((a) => !a.deleted_at)
    const view = q.get('view') ?? 'all'
    if (view === 'favorites') items = items.filter((a) => a.is_favorite)
    if (view === 'unclassified') items = items.filter((a) => !a.group_ids.length)
    if (view === 'trash') items = all.filter((a) => a.deleted_at)
    const g = q.get('group_id')
    if (g) {
      const scope = q.get('include_descendants') === 'true' ? groups.filter((x) => x.id === g || x.parent_id === g).map((x) => x.id) : [g]
      items = items.filter((a) => a.group_ids.some((id) => scope.includes(id)))
    }
    if (q.get('kind')) items = items.filter((a) => a.kind === q.get('kind'))
    if (q.get('q')) items = items.filter((a) => a.title.includes(q.get('q')))
    // 标签数量只按「满足任一」模型：在其余条件下数每个标签。
    const kinds = Object.fromEntries(['image', 'video', 'audio', 'text', 'link'].map((k) => [k, items.filter((a) => a.kind === k).length]))
    const facets = q.get('include_facets') === 'true' ? { kinds, tags: tags.map((t) => ({ tag_id: t.id, count: items.filter((a) => a.tag_ids.includes(t.id)).length })) } : undefined
    if (q.get('tag_ids')) items = items.filter((a) => a.tag_ids.some((t) => q.get('tag_ids').split(',').includes(t)))
    const limit = Number(q.get('limit') ?? 30)
    return json({ ...page_(items.slice(0, limit), items.length), facets })
  }
  return json(page_([]))
})

const drawer = page.locator('#creative-library')
const tile = (title) => drawer.locator('.al-tile', { has: page.getByText(title, { exact: true }) }).first()
const tiles = () => drawer.locator('.al-tile')
const scope = () => drawer.getByRole('combobox', { name: '视图' })
const treeRow = (name) => drawer.locator('.al-tree .al-row', { has: page.locator('.al-name', { hasText: new RegExp(`^${name}$`) }) })
const until = async (fn, label) => { for (let i = 0; i < 80; i++) { if (await fn()) return; await page.waitForTimeout(50) } throw new Error(`timeout: ${label}`) }
const count = (n) => until(async () => (await tiles().count()) === n, `${n} tiles`)
// 画布编辑先在本机投影，250ms 后才发出；断言请求内容前等它真正离开。
async function nextCommand(run) {
  const n = commands.length
  await run()
  for (let i = 0; i < 40 && commands.length === n; i++) await page.waitForTimeout(100)
  assert(commands.length > n, 'canvas command was sent')
  return commands.at(-1)
}
const metrics = {}
try {
  await page.goto(`${base}/creative/canvases/qa`)
  await page.locator('.react-flow__node').nth(3).waitFor()
  await page.getByRole('button', { name: '资产库', exact: true }).click()
  await drawer.waitFor()

  // 1. 与独立页同一套浏览区：一张结果网格、视图下拉、类型/颜色/画幅/评分/标签筛选；旧的分组平铺与悬停大图都没有了。
  await count(12)
  await drawer.evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)))
  metrics.drawer = await drawer.evaluate((el) => {
    const s = getComputedStyle(el)
    return { bg: s.backgroundColor, width: Math.round(el.getBoundingClientRect().width), left: Math.round(el.getBoundingClientRect().left) }
  })
  assert.deepEqual(metrics.drawer, { bg: 'rgb(27, 27, 29)', width: 344, left: 64 })
  assert.equal(await drawer.locator('.cl-sec, .cl-tile, .cl-views').count(), 0, 'old drawer layout is gone')
  assert.equal(await drawer.locator('.al-tree').count(), 0, 'narrow drawer has no tree')
  await scope().waitFor()
  for (const name of ['颜色', '画幅', '评分', '标签']) assert(await drawer.locator('.al-filters').getByText(name, { exact: true }).count(), `filter ${name}`)
  assert.equal(await drawer.getByRole('group', { name: '布局' }).count(), 0, 'no layout toggle in the drawer')
  assert.equal(await drawer.getByRole('slider', { name: '缩略图大小' }).count(), 0)
  assert.equal(await drawer.locator('.cl-masonry-col').count(), 2)
  const noSideScroll = await drawer.evaluate((el) => [...el.querySelectorAll('.al-board, .al-scroll, .al-filters')].every((e) => e.scrollWidth <= e.clientWidth))
  assert(noSideScroll, 'drawer does not scroll sideways')
  // 卡片与独立页一致（V6-060）：音频装饰条与时长、链接只显示域名、只读星级。
  const voice = tile('现场环境音')
  assert.equal(await voice.locator('.cl-wave i').count(), 36)
  assert.equal((await voice.locator('.cl-card-row small').innerText()).trim(), '3:04')
  await voice.getByRole('img', { name: '4.5 星' }).waitFor()
  assert.equal((await tile('布光参考').locator('.cl-card-link small').innerText()).trim(), 'example.com')
  await tile('白裙').hover(); await page.waitForTimeout(500)
  assert.equal(await page.locator('.cl-peek').count(), 0, 'no hover peek')
  await page.screenshot({ path: '/tmp/creative-library-drawer.png' })

  // 2. 单击是选择（不再直接打开浮窗），双击或回车查看，Esc 先清选择。
  await tile('逆光发丝').click()
  assert.equal(await drawer.locator('.al-tile.is-on').count(), 1)
  assert.equal(await page.locator('.viewer').count(), 0, 'single click does not open the viewer')
  assert.equal(await drawer.locator('.cl-foot > span').first().innerText(), '已选 1 项')
  await page.keyboard.press('Escape')
  assert.equal(await drawer.locator('.al-tile.is-on').count(), 0)
  assert(await drawer.isVisible(), 'Esc with a selection keeps the drawer open')
  await tile('逆光发丝').dblclick()
  const look = page.getByRole('dialog', { name: '快速查看：逆光发丝' })
  await look.waitFor()
  await page.keyboard.press('ArrowRight')
  await page.getByRole('dialog', { name: /^快速查看：/ }).waitFor()
  assert.notEqual(await page.getByRole('dialog', { name: /^快速查看：/ }).getAttribute('aria-label'), '快速查看：逆光发丝')
  await page.keyboard.press('ArrowLeft')
  await look.waitFor()
  await page.keyboard.press('Escape')
  await look.waitFor({ state: 'detached' })
  await page.waitForFunction(() => document.activeElement?.getAttribute('aria-label') === '逆光发丝')

  // 3. 视图下拉进分组（含子组）；类型与标签筛选。
  await scope().selectOption({ label: '客户样片' })
  await count(7)
  assert(assetQueries.at(-1).includes('group_id=g1') && assetQueries.at(-1).includes('include_descendants=true'))
  assert.equal(await drawer.locator('.al-subgroup').innerText().then((t) => t.split('\n')[0]), '海边婚纱')
  assert.equal(await drawer.getAttribute('data-scope-group'), 'g1', 'canvas nodes dropped on the drawer go into the browsed group')
  await scope().selectOption({ label: '全部' })
  await count(12)
  await drawer.getByRole('group', { name: '类型' }).getByRole('button', { name: /^文字/ }).click()
  await count(1)
  assert(assetQueries.at(-1).includes('kind=text'))
  await drawer.getByRole('group', { name: '类型' }).getByRole('button', { name: /^全部/ }).click()
  await count(12)
  await drawer.locator('.al-filters').getByRole('button', { name: /^标签/ }).click()
  const tagPop = page.getByRole('dialog', { name: '标签筛选' })
  await tagPop.getByRole('button', { name: /逆光/ }).click()
  await count(1)
  assert(assetQueries.at(-1).includes('tag_ids=t1'))
  await page.keyboard.press('Escape')
  await drawer.getByRole('button', { name: '清除全部', exact: true }).click()
  await count(12)

  // 4. 加宽后左侧出现资料库树（与独立页同一组件）：点分组浏览，节点拖到分组上存入该组。
  await drawer.getByRole('button', { name: '加宽资产库', exact: true }).click()
  await treeRow('风格参考').waitFor()
  assert.equal(Math.round((await drawer.boundingBox()).width), 760)
  assert.equal(await scope().isVisible(), false, 'wide drawer uses the tree instead of the dropdown')
  await treeRow('风格参考').click()
  await count(2)
  assert(assetQueries.at(-1).includes('group_id=g2'))
  await page.screenshot({ path: '/tmp/creative-library-drawer-wide.png' })
  await treeRow('全部').click().catch(() => drawer.locator('.al-tree nav .al-row').first().click())
  await count(12)
  const node = page.locator('.react-flow__node[data-id="filled"]')
  // 宽抽屉盖住了节点：把画布往右平移，让它露出来。
  await page.mouse.move(1300, 800)
  for (let i = 0; i < 12 && (await node.boundingBox()).x < 880; i++) { await page.mouse.wheel(-120, 0); await page.waitForTimeout(60) }
  assert((await node.boundingBox()).x >= 860, `node visible beside the wide drawer: ${(await node.boundingBox()).x}`)
  const before = await node.boundingBox()
  const head = await treeRow('风格参考').boundingBox()
  const grip = await page.evaluate(({ x, y, width, height }) => {
    for (let dy = 8; dy < height; dy += 12)
      for (let dx = 8; dx < width; dx += 24)
        if (document.elementFromPoint(x + dx, y + dy)?.closest('.react-flow__node')?.getAttribute('data-id') === 'filled') return { x: x + dx, y: y + dy }
    return null
  }, before)
  assert(grip, 'filled node is reachable')
  await page.mouse.move(grip.x, grip.y)
  await page.mouse.down()
  await page.mouse.move(grip.x - 60, grip.y + 4, { steps: 4 })
  await page.mouse.move(head.x + 60, head.y + head.height / 2, { steps: 12 })
  assert.equal(await treeRow('风格参考').and(page.locator('.is-drop-hot')).count(), 1)
  await page.mouse.up()
  await page.locator('.cc-save-status').filter({ hasText: '已归入「风格参考」' }).waitFor()
  const save = posts.find((p) => p.path.endsWith('/assets/from-canvas-node'))
  assert.deepEqual(save.payload.target.group_ids, ['g2'])
  const after = await node.boundingBox()
  assert.deepEqual([Math.round(after.x), Math.round(after.y)], [Math.round(before.x), Math.round(before.y)])
  assert.equal(await page.locator('.is-drop-hot').count(), 0)

  // 5. 树里删除分组：确认后用服务端快照撤销（V6-018），与独立页同一流程。
  await treeRow('客户样片').click({ button: 'right' })
  await page.getByRole('menuitem', { name: '删除分组', exact: true }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: '删除分组', exact: true }).click()
  await page.locator('.al-toast').getByRole('button', { name: '撤销', exact: true }).waitFor()
  assert(!groups.some((g) => g.id === 'g1'))
  await page.locator('.al-toast').getByRole('button', { name: '撤销', exact: true }).click()
  await page.locator('.al-toast').getByText('已恢复分组与归类').waitFor()
  assert(groups.some((g) => g.id === 'g1'))
  assert.equal(groups.find((g) => g.id === 'g1a').parent_id, 'g1')
  await drawer.getByRole('button', { name: '收窄资产库', exact: true }).click()
  await drawer.getByRole('button', { name: '收起资产库', exact: true }).click()

  // 6. 空节点：按节点类型筛选，点一份就填进去。
  const empty = page.locator('.react-flow__node[data-id="empty"]')
  await empty.getByRole('button', { name: '从资产库填充' }).click()
  await drawer.getByRole('heading', { name: '选择图片' }).waitFor()
  assert.equal(await drawer.locator('.cl-foot').innerText(), '点一份图片填进「空图片」\n取消')
  await count(9)
  assert(assetQueries.at(-1).includes('kind=image'))
  const fill = await nextCommand(() => tile('胶片暖调').click())
  await drawer.waitFor({ state: 'hidden' })
  assert.deepEqual(fill, [{ type: 'fill_from_asset', node_id: 'empty', asset: { asset_id: 'a8', expected_asset_revision: '1', content_revision_id: 'rev-a8' } }])
  await empty.locator('.cc-media').waitFor()

  // 7. 素材直接拖到空节点上也是填充，不另建节点。
  await page.locator('.react-flow__pane').click({ position: { x: 1500, y: 60 } })
  await page.getByRole('button', { name: '资产库', exact: true }).click()
  await count(12)
  const dropFill = await nextCommand(() => tile('冷调').dragTo(page.locator('.react-flow__node[data-id="empty2"] .cc-media-empty')))
  await page.waitForFunction(() => document.querySelector('.react-flow__node[data-id="empty2"] .cc-media'))
  assert.equal(dropFill[0].type, 'fill_from_asset')
  assert.equal(dropFill[0].node_id, 'empty2')
  assert.equal(await page.locator('.react-flow__node[data-id="empty2"] .cc-node-header h3').innerText(), '冷调')
  await drawer.getByRole('button', { name: '收起资产库', exact: true }).click()

  // 8. 为节点选参考：点选即多选，确认后在目标左侧竖排并连线。
  await page.locator('.react-flow__node[data-id="brief"]').click({ button: 'right' })
  await page.getByRole('menuitem', { name: '从资产库添加参考' }).click()
  await drawer.getByRole('heading', { name: '选择参考素材' }).waitFor()
  await count(12)
  await tile('白裙').click()
  await tile('花艺').click()
  const pick = await nextCommand(() => drawer.getByRole('button', { name: '添加 2 项' }).click())
  await drawer.waitFor({ state: 'hidden' })
  assert.deepEqual(pick.map((a) => a.type), ['add_node', 'add_node', 'connect_reference', 'connect_reference'])
  assert(pick.slice(2).every((a) => a.target_node_id === 'brief'))
  await page.locator('.cc-save-status').filter({ hasText: '已连接 2 个参考' }).waitFor()

  // 9. 多选：单击后 Shift 范围选，底栏「放入画布」横排放下。
  await page.locator('.react-flow__pane').click({ position: { x: 1500, y: 60 } })
  await page.getByRole('button', { name: '资产库', exact: true }).click()
  await scope().selectOption({ label: '客户样片' })
  await count(7)
  // 范围按列表顺序（瀑布流的 DOM 顺序是按列的）：a1 日落海边 … a3 白裙。
  await tile('日落海边').click()
  await tile('白裙').click({ modifiers: ['Shift'] })
  assert.equal(await drawer.locator('.al-tile.is-on').count(), 3)
  assert.equal(await drawer.locator('.cl-foot > span').first().innerText(), '已选 3 项')
  assert((await drawer.locator('.cl-foot').boundingBox()).height < 48, 'selection footer stays on one line')
  const nodesBefore = await page.locator('.react-flow__node').count()
  const placed = (await nextCommand(() => drawer.getByRole('button', { name: '放入画布', exact: true }).click())).filter((a) => a.type === 'add_node')
  await page.waitForFunction((n) => document.querySelectorAll('.react-flow__node').length === n + 3, nodesBefore)
  assert.equal(placed.length, 3)
  assert.equal(new Set(placed.map((a) => a.y)).size, 1, 'batch lands in one row')
  assert(placed.every((a) => a.asset?.asset_id), 'placed nodes keep their asset source')

  // 10. 右键菜单与独立页相同，多一项「放入画布」。
  await tile('礁石').click({ button: 'right' })
  const menu = page.getByRole('menu', { name: '素材操作' })
  const labels = await menu.getByRole('menuitem').evaluateAll((els) => els.map((e) => e.getAttribute('aria-label') ?? e.textContent.trim()))
  assert.equal(labels[0], '放入画布')
  for (const label of ['评分…', '归入分组…', '打标签…', '移出这个分组', '移到回收站']) assert(labels.includes(label), `menu has ${label}: ${labels}`)
  const one = (await nextCommand(() => menu.getByRole('menuitem', { name: '放入画布' }).click())).filter((a) => a.type === 'add_node')
  assert.equal(one.length, 1)

  // 11. 浮窗内回收/恢复刷新抽屉卡片后，按素材身份归还焦点。
  await tile('日落海边').focus()
  await page.keyboard.press('Enter')
  const assetViewer = page.locator('.viewer')
  await assetViewer.getByRole('textbox', { name: '标题', exact: true }).waitFor()
  await assetViewer.getByRole('button', { name: '移到回收站', exact: true }).click()
  await assetViewer.getByRole('alertdialog').getByRole('button', { name: '确认', exact: true }).click()
  await assetViewer.getByRole('button', { name: '恢复', exact: true }).click()
  await assetViewer.getByRole('button', { name: '移到回收站', exact: true }).waitFor()
  await page.waitForFunction(() => document.querySelector('.v-close') && !document.querySelector('.v-close').disabled)
  await page.keyboard.press('Escape')
  await assetViewer.waitFor({ state: 'detached' })
  await page.waitForFunction(() => document.activeElement?.getAttribute('data-asset') === 'a1')

  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ ok: true, metrics, commands: commands.length, assetQueries: assetQueries.length }))
} finally {
  await page.screenshot({ path: '/tmp/creative-library-e2e.png' })
  await browser.close()
}
