// Browser regression for the v6 asset drawer. All API traffic is mocked; run
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
  if (path.endsWith('/generation-catalog')) return json({ models: [], disabled_reason: '未配置生成模型' })
  if (path.endsWith('/versions')) return json({ items: [], selected_version_id: null, data_revision: '1' })
  if (path.endsWith('/media-capabilities'))
    return json({ formats: [{ kind: 'image', mime: 'image/png', extensions: ['.png'] }], image_max_bytes: 1000000, av_max_bytes: 10000000, batch_limit: 10 })
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
    if (path.endsWith('/asset-groups')) {
      if (takenOnServer.has(body.payload.name))
        return json({ error: { code: 'creative_group_name_taken', message: '同一层已有同名分组' } }, 409)
      hierarchy++
      groups.push({ id: `g-new-${hierarchy}`, parent_id: body.payload.parent_id, name: body.payload.name, position: 9, revision: '1' })
      return json({ id: `g-new-${hierarchy}`, revision: '1', hierarchy_revision: String(hierarchy), library_revision: '1' })
    }
    if (path.endsWith('/assets/from-canvas-node'))
      return json({ asset_id: 'a1', revision: '2', content_revision_id: 'rev-a1', library_revision: '2', tag_mapping: {}, reused: true })
    return json({})
  }
  if (path.endsWith('/asset-groups')) return json({ items: groups, hierarchy_revision: String(hierarchy) })
  if (path.endsWith('/tags')) return json({ items: tags, hierarchy_revision: String(hierarchy) })
  if (path.endsWith('/tag-categories')) return json({ items: [], hierarchy_revision: String(hierarchy) })
  if (path.endsWith('/library-settings')) return json({ retention_days: 30, revision: '1', hierarchy_revision: String(hierarchy), library_revision: '1' })
  if (path.endsWith('/assets')) {
    const q = url.searchParams
    assetQueries.push(q.toString())
    let items = all
    const view = q.get('view') ?? 'all'
    if (view === 'favorites') items = items.filter((a) => a.is_favorite)
    if (view === 'unclassified') items = items.filter((a) => !a.group_ids.length)
    if (view === 'trash') items = []
    const g = q.get('group_id')
    if (g) {
      const scope = q.get('include_descendants') === 'true' ? groups.filter((x) => x.id === g || x.parent_id === g).map((x) => x.id) : [g]
      items = items.filter((a) => a.group_ids.some((id) => scope.includes(id)))
    }
    if (q.get('kind')) items = items.filter((a) => a.kind === q.get('kind'))
    if (q.get('tag_ids')) items = items.filter((a) => a.tag_ids.some((t) => q.get('tag_ids').split(',').includes(t)))
    if (q.get('q')) items = items.filter((a) => a.title.includes(q.get('q')))
    const limit = Number(q.get('limit') ?? 30)
    return json(page_(items.slice(0, limit), items.length))
  }
  return json(page_([]))
})

const drawer = page.locator('#creative-library')
const tile = (title) => drawer.locator('.cl-tile', { has: page.getByText(title, { exact: true }) }).first()
const lastCommand = () => commands.at(-1) ?? []
// 画布编辑先在本机投影，250ms 后才发出；断言请求内容前等它真正离开。
async function nextCommand(run) {
  const count = commands.length
  await run()
  for (let i = 0; i < 40 && commands.length === count; i++) await page.waitForTimeout(100)
  assert(commands.length > count, 'canvas command was sent')
  return commands.at(-1)
}
const metrics = {}
try {
  await page.goto(`${base}/creative/canvases/qa`)
  await page.locator('.react-flow__node').nth(3).waitFor()
  await page.getByRole('button', { name: '资产库', exact: true }).click()
  await drawer.waitFor()

  // 1. 平铺浏览：每个根分组一节，未归类放最后；方格预览，超出的最后一格是「+N」。
  await drawer.getByRole('region', { name: '客户样片' }).locator('.cl-tile').first().waitFor()
  assert.deepEqual(await drawer.locator('.cl-sec-toggle > span').allInnerTexts(), ['客户样片', '风格参考', '未归类'])
  assert.equal(await drawer.locator('.cl-groups-bar > span').innerText(), '2 个分组')
  const client = drawer.getByRole('region', { name: '客户样片' })
  assert.equal(await client.locator('.cl-grid .cl-tile').count(), 5)
  assert.equal(await client.getByRole('button', { name: '查看「客户样片」全部 7 项' }).innerText(), '+2')
  assert.equal(await client.locator('.cl-sub-chips button').innerText(), '海边婚纱')
  assert(assetQueries.some((q) => q.includes('group_id=g1') && q.includes('include_descendants=true') && q.includes('limit=6')), 'group preview reads descendants')
  await drawer.evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)))
  metrics.drawer = await drawer.evaluate((el) => {
    const s = getComputedStyle(el)
    return { bg: s.backgroundColor, width: Math.round(el.getBoundingClientRect().width), left: Math.round(el.getBoundingClientRect().left) }
  })
  assert.deepEqual(metrics.drawer, { bg: 'rgb(27, 27, 29)', width: 344, left: 64 })
  // 已在画布上的素材带定位角标。
  await tile('日落海边').getByRole('button', { name: '已在画布上，定位：日落海边' }).click()
  await page.locator('.react-flow__node[data-id="filled"].selected').waitFor()

  // 2. 进组：面包屑、子组标签、保持比例的两列瀑布流。
  await client.locator('.cl-sec-head').hover()
  await client.getByRole('button', { name: '查看全部', exact: true }).click()
  await drawer.locator('.cl-crumb b', { hasText: '客户样片' }).waitFor()
  await page.waitForFunction(() => document.querySelectorAll('#creative-library .cl-masonry .cl-tile').length === 7)
  assert.equal(await drawer.locator('.cl-masonry-col').count(), 2)
  assert.equal(await drawer.locator('.cl-crumb em').innerText(), '7 项')
  assert.match(page.url(), /lgroup=g1/)
  const ratio = await tile('日落海边').evaluate((el) => el.getBoundingClientRect().height / el.getBoundingClientRect().width)
  assert(Math.abs(ratio - 1.5) < 0.03, `masonry keeps the aspect ratio: ${ratio}`)
  await drawer.locator('.cl-sub-chips').getByRole('button', { name: '海边婚纱' }).click()
  await drawer.locator('.cl-crumb b', { hasText: '海边婚纱' }).waitFor()
  assert.equal((await drawer.locator('.cl-path').innerText()).replace(/\s+/g, ''), '客户样片/海边婚纱')
  // 最多两层：子分组的菜单里没有「新建子分组」。
  await drawer.getByRole('button', { name: '分组操作：海边婚纱' }).click()
  const childMenu = page.getByRole('menu', { name: '分组操作：海边婚纱' })
  assert.deepEqual(await childMenu.getByRole('menuitem').evaluateAll((els) => els.map((e) => e.getAttribute('aria-label'))), ['重命名分组', '移动分组', '删除分组'])
  await page.keyboard.press('Escape')
  await drawer.getByRole('button', { name: '返回', exact: true }).click()
  await drawer.locator('.cl-crumb b', { hasText: '客户样片' }).waitFor()

  // 3. 快速查看：点卡片打开，左右键翻看，空格关闭并把焦点还给卡片。
  await tile('逆光发丝').click()
  const look = page.getByRole('dialog', { name: '快速查看：逆光发丝' })
  await look.waitFor()
  assert.match(await look.locator('figcaption').innerText(), /800×1000.*客户样片.*逆光/s)
  await page.keyboard.press('ArrowRight')
  await page.getByRole('dialog', { name: /^快速查看：/ }).waitFor()
  assert.notEqual(await page.getByRole('dialog', { name: /^快速查看：/ }).getAttribute('aria-label'), '快速查看：逆光发丝')
  await page.keyboard.press('ArrowLeft')
  await look.waitFor()
  await page.keyboard.press(' ')
  await look.waitFor({ state: 'detached' })
  assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('aria-label')), '逆光发丝')
  // 方向键在卡片间移动焦点。
  await page.keyboard.press('ArrowDown')
  assert.notEqual(await page.evaluate(() => document.activeElement?.getAttribute('aria-label')), '逆光发丝')
  assert.equal(await page.evaluate(() => document.activeElement?.classList.contains('cl-tile')), true)

  // 4. 悬停放大出现在抽屉右侧。
  await tile('白裙').hover()
  await page.locator('.cl-peek').waitFor()
  const peek = await page.locator('.cl-peek').boundingBox()
  assert(peek.x >= 64 + 344, 'peek sits right of the drawer')
  await drawer.locator('.cl-crumb').hover()
  await page.locator('.cl-peek').waitFor({ state: 'detached' })

  // 6. 视图、类型与标签筛选。
  await drawer.getByRole('tab', { name: '最爱' }).click()
  await page.waitForFunction(() => document.querySelectorAll('#creative-library .cl-masonry .cl-tile').length === 1)
  assert(assetQueries.at(-1).includes('view=favorites'))
  await drawer.getByRole('tab', { name: '全部' }).click()
  await drawer.locator('.cl-groups-bar').waitFor()
  await drawer.getByRole('button', { name: '资产类型' }).click()
  await page.getByRole('menu', { name: '资产类型' }).getByRole('menuitem', { name: '文字' }).click()
  await drawer.locator('.cl-crumb b', { hasText: '筛选结果' }).waitFor()
  await page.waitForFunction(() => document.querySelectorAll('#creative-library .cl-masonry .cl-tile').length === 1)
  assert(assetQueries.at(-1).includes('kind=text'))
  await drawer.getByRole('button', { name: '资产类型' }).click()
  await page.getByRole('menu', { name: '资产类型' }).getByRole('menuitem', { name: '全部' }).click()
  await drawer.locator('.cl-groups-bar').waitFor()
  await drawer.getByRole('button', { name: '标签', exact: true }).click()
  const tagPop = page.getByRole('dialog', { name: '标签筛选' })
  await tagPop.getByRole('button', { name: '逆光' }).click()
  await page.waitForFunction(() => document.querySelectorAll('#creative-library .cl-masonry .cl-tile').length === 1)
  assert(assetQueries.at(-1).includes('tag_ids=t1'))
  await page.keyboard.press('Escape')
  await drawer.getByRole('button', { name: '移除标签筛选：逆光' }).click()
  await drawer.locator('.cl-groups-bar').waitFor()

  // 7. 就地新建分组：同层重名本机即拦下，不发请求，并保留已输入的名称继续修改（V6-017）。
  const groupPosts = () => posts.filter((p) => p.path.endsWith('/asset-groups'))
  await drawer.getByRole('button', { name: '新建分组', exact: true }).click()
  const nameField = drawer.locator('input.cl-name-input')
  await nameField.fill('客户样片')
  await page.keyboard.press('Enter')
  await drawer.getByRole('status').filter({ hasText: '同一层已有「客户样片」' }).waitFor()
  assert.equal(groupPosts().length, 0)
  assert.equal(await nameField.inputValue(), '客户样片', 'duplicate name keeps the typed input')
  assert(await nameField.evaluate((el) => el === document.activeElement), 'input keeps focus after Enter rejection')
  // 失焦校验失败也不清空输入、不抢回焦点。
  await drawer.locator('.cl-head h2').click()
  assert.equal(await nameField.inputValue(), '客户样片', 'blur rejection keeps the typed input')
  // 服务端拒绝（另一窗口已占用）：输入框和名称保留，关闭拒绝提示后改名即可提交。
  await nameField.fill('棚拍布光')
  await page.keyboard.press('Enter')
  await page.getByRole('button', { name: '放弃被拒绝的请求' }).waitFor()
  assert.equal(groupPosts().length, 1)
  assert.equal(await nameField.inputValue(), '棚拍布光', 'server rejection keeps the typed input')
  await page.getByRole('button', { name: '放弃被拒绝的请求' }).click()
  await page.getByRole('button', { name: '放弃被拒绝的请求' }).waitFor({ state: 'detached' })
  // 连按两次 Enter 只提交一次，成功后关闭输入框且不留空分组。
  await nameField.fill('光线与时刻')
  // 同一 tick 内连按两次，第二次落在请求仍在途时。关闭拒绝提示后队列可能仍在刷新，
  // 此时 Enter 被忽略但输入保留；像摄影师一样重按，直到请求真正发出。
  for (let i = 0; i < 20 && groupPosts().length < 2; i++) {
    await nameField.evaluate((el) => {
      for (let k = 0; k < 2; k++) el.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    })
    await page.waitForTimeout(250)
  }
  await drawer.getByRole('region', { name: '光线与时刻' }).waitFor()
  assert.equal(groupPosts().length, 2, 'double Enter sends one create request')
  assert.deepEqual(groupPosts().at(-1).payload, { name: '光线与时刻', parent_id: null, hierarchy_revision: '1' })
  assert.equal(await drawer.locator('input.cl-name-input').count(), 0, 'input closes after success')
  // Esc 取消不发请求；输入法组字中的 Enter 不提交；失焦提交一次。
  await drawer.getByRole('button', { name: '新建分组', exact: true }).click()
  await nameField.fill('取消的分组')
  await nameField.press('Escape')
  assert.equal(await drawer.locator('input.cl-name-input').count(), 0, 'Esc closes the input')
  assert.equal(groupPosts().length, 2, 'Esc sends nothing')
  await drawer.getByRole('button', { name: '新建分组', exact: true }).click()
  await nameField.fill('婚纱外景')
  await nameField.evaluate((el) => el.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', isComposing: true, bubbles: true, cancelable: true })))
  await page.waitForTimeout(250)
  assert.equal(groupPosts().length, 2, 'Enter during IME composition does not submit')
  assert.equal(await nameField.inputValue(), '婚纱外景')
  await drawer.locator('.cl-head h2').click()
  await drawer.getByRole('region', { name: '婚纱外景' }).waitFor()
  assert.equal(groupPosts().length, 3, 'blur submits once')

  // 8. 节点拖到分组标题上：存入该组，节点回到原处。
  const node = page.locator('.react-flow__node[data-id="filled"]')
  const before = await node.boundingBox()
  const head = await drawer.getByRole('region', { name: '风格参考' }).locator('.cl-sec-head').boundingBox()
  // 刚放入的节点可能叠在上面：找一处确实按在 filled 上的点。
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
  await page.mouse.move(head.x + 120, head.y + head.height / 2, { steps: 12 })
  assert.equal(await drawer.getByRole('region', { name: '风格参考' }).locator('.cl-sec-head.is-drop-hot').count(), 1)
  await page.mouse.up()
  await page.locator('.cc-save-status').filter({ hasText: '已归入「风格参考」' }).waitFor()
  const save = posts.find((p) => p.path.endsWith('/assets/from-canvas-node'))
  assert.deepEqual(save.payload.target.group_ids, ['g2'])
  assert.equal(save.payload.node_id, 'filled')
  const after = await node.boundingBox()
  assert.deepEqual([Math.round(after.x), Math.round(after.y)], [Math.round(before.x), Math.round(before.y)])
  assert.equal(await drawer.locator('.is-drop-hot').count(), 0)
  await drawer.getByRole('button', { name: '收起资产库', exact: true }).click()

  // 9. 空节点：「资产库」入口按节点类型筛选，点一份就填进去。
  const empty = page.locator('.react-flow__node[data-id="empty"]')
  await empty.getByRole('button', { name: '从资产库填充' }).click()
  await drawer.getByRole('heading', { name: '选择图片' }).waitFor()
  assert.equal(await drawer.locator('.cl-foot').innerText(), '点一份图片填进「空图片」\n取消')
  await drawer.getByRole('region', { name: '风格参考' }).locator('.cl-tile').first().waitFor()
  assert(assetQueries.at(-1).includes('kind=image'))
  assert.equal(await drawer.getByRole('region', { name: '未归类' }).count(), 0, 'fill task hides other kinds')
  const fill = await nextCommand(() => tile('胶片暖调').click())
  await drawer.waitFor({ state: 'hidden' })
  assert.deepEqual(fill, [{ type: 'fill_from_asset', node_id: 'empty', asset: { asset_id: 'a8', expected_asset_revision: '1', content_revision_id: 'rev-a8' } }])
  await empty.locator('.cc-media').waitFor()

  // 10. 素材直接拖到空节点上也是填充，不另建节点。先取消选中，别让生成面板挡住落点。
  await page.locator('.react-flow__pane').click({ position: { x: 1500, y: 60 } })
  await page.getByRole('button', { name: '资产库', exact: true }).click()
  await drawer.getByRole('region', { name: '风格参考' }).locator('.cl-tile').first().waitFor()
  const dropFill = await nextCommand(() => tile('冷调').dragTo(page.locator('.react-flow__node[data-id="empty2"] .cc-media-empty')))
  await page.waitForFunction(() => document.querySelector('.react-flow__node[data-id="empty2"] .cc-media'))
  assert.equal(dropFill[0].type, 'fill_from_asset')
  assert.equal(dropFill[0].node_id, 'empty2')
  // 没有名称的节点沿用素材名。
  assert.equal(await page.locator('.react-flow__node[data-id="empty2"] .cc-node-header h3').innerText(), '冷调')
  await drawer.getByRole('button', { name: '收起资产库', exact: true }).click()

  // 11. 为节点选参考：多选后在目标左侧竖排并连线。
  await page.locator('.react-flow__node[data-id="brief"]').click({ button: 'right' })
  await page.getByRole('menuitem', { name: '从资产库添加参考' }).click()
  await drawer.getByRole('heading', { name: '选择参考素材' }).waitFor()
  await drawer.getByRole('region', { name: '客户样片' }).locator('.cl-tile').first().waitFor()
  await tile('白裙').click()
  await tile('花艺').click()
  const pick = await nextCommand(() => drawer.getByRole('button', { name: '添加 2 项' }).click())
  await drawer.waitFor({ state: 'hidden' })
  assert.deepEqual(pick.map((a) => a.type), ['add_node', 'add_node', 'connect_reference', 'connect_reference'])
  assert(pick.slice(2).every((a) => a.target_node_id === 'brief'))
  assert(pick[0].x < 700 - 280 && pick[0].x === pick[1].x && pick[1].y > pick[0].y, 'references stack left of the target')
  await page.locator('.cc-save-status').filter({ hasText: '已连接 2 个参考' }).waitFor()
  await page.locator('.react-flow__node[data-id="brief"] .cc-node-header').click()
  const prompt=page.getByRole('complementary',{name:'节点生成'}).getByRole('textbox',{name:'提示词',exact:true})
  await page.waitForFunction(()=>document.querySelectorAll('.cc-prompt-editor [data-ref]').length===2)
  assert.deepEqual(await prompt.locator('[data-ref]').evaluateAll(chips=>chips.map(chip=>chip.dataset.ref)),pick.slice(0,2).map(a=>a.node_id),'library picks automatically become prompt references')

  // 12. 多选：勾选一张后 Shift 范围选，底栏批量放入画布，横排一次撤销。
  await page.getByRole('button', { name: '资产库', exact: true }).click()
  await drawer.getByRole('region', { name: '客户样片' }).locator('.cl-sec-head').hover()
  await drawer.getByRole('region', { name: '客户样片' }).getByRole('button', { name: '查看全部', exact: true }).click()
  await page.waitForFunction(() => document.querySelectorAll('#creative-library .cl-masonry .cl-tile').length === 7)
  await tile('逆光发丝').hover()
  await drawer.getByRole('button', { name: '选择资产：逆光发丝' }).click()
  const ids = await drawer.locator('.cl-masonry .cl-tile').evaluateAll((els) => els.map((e) => e.getAttribute('aria-label')))
  const target = ids[ids.indexOf('逆光发丝') + 2]
  await drawer.getByRole('button', { name: `选择资产：${target}` }).click({ modifiers: ['Shift'] })
  assert.equal(await drawer.locator('.cl-tile.is-on').count(), 3)
  assert.equal(await drawer.locator('.cl-foot > span').first().innerText(), '已选 3 项')
  // 底栏一行放下全部操作。
  assert((await drawer.locator('.cl-foot').boundingBox()).height < 48, 'selection footer stays on one line')
  const placed = (await nextCommand(() => drawer.getByRole('button', { name: '放入画布', exact: true }).click())).filter((a) => a.type === 'add_node')
  await page.waitForFunction(() => document.querySelectorAll('.react-flow__node').length === 9)
  assert.equal(placed.length, 3)
  assert.equal(new Set(placed.map((a) => a.y)).size, 1, 'batch lands in one row')
  assert.deepEqual(placed.map((a, i) => (i ? a.x - placed[i - 1].x : 0)), [0, 320, 320])
  assert(placed.every((a) => a.asset?.asset_id), 'placed nodes keep their asset source')

  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ ok: true, metrics, commands: commands.length, assetQueries: assetQueries.length }))
} finally {
  await page.screenshot({ path: '/tmp/creative-library-e2e.png' })
  await browser.close()
}
