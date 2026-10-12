// Browser regression for the standalone asset library page (V6-067.1 with
// V6-016 / V6-060). The API is an in-memory mock that filters, sorts, pages
// and counts like the server; each check runs in a fresh context.
// Run against the Vite dev server: node scripts/creative-assets.e2e.mjs.
import assert from 'node:assert/strict'
import { mkdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const output = process.env.CREATIVE_QA_OUTPUT_DIR ?? '/tmp/creative-assets-qa'
mkdirSync(output, { recursive: true })
const webm = readFileSync(fileURLToPath(new URL('../../backend/internal/creativemedia/testdata/sample.webm', import.meta.url)))
const browser = await chromium.launch({ headless: true })
const errors = []
const results = {}
let context, page, server

const DAY = 86400000
const now = Date.now()
const GiB = 1024 ** 3
const groups = [
  { id: 'g-wed', parent_id: null, name: '婚礼', position: 0, revision: '1' },
  { id: 'g-out', parent_id: 'g-wed', name: '外景', position: 0, revision: '1' },
  { id: 'g-in', parent_id: 'g-wed', name: '棚拍', position: 1, revision: '1' },
  { id: 'g-brand', parent_id: null, name: '品牌', position: 1, revision: '1' },
]
const categories = [
  { id: 'c-style', name: '风格', position: 0, revision: '1' },
  { id: 'c-light', name: '光线', position: 1, revision: '1' },
]
const tags = [
  { id: 't-warm', name: '暖调', color: '#d9a07a', category_id: 'c-style', revision: '1' },
  { id: 't-film', name: '胶片', color: '#8fa37a', category_id: 'c-style', revision: '1' },
  { id: 't-back', name: '逆光', color: '#d9c38a', category_id: 'c-light', revision: '1' },
  { id: 't-misc', name: '待整理', color: '#a1a1a7', category_id: null, revision: '1' },
]
const mime = { image: 'image/jpeg', video: 'video/webm', audio: 'audio/mpeg' }
function asset(o) {
  const kind = o.kind ?? 'image'
  const original = { role: 'original', blob_id: `b-${o.id}`, mime: mime[kind], byte_size: 2_400_000, width: kind === 'audio' ? null : (o.w ?? 1200), height: kind === 'audio' ? null : (o.h ?? 800), duration_ms: o.duration ?? null }
  const media = mime[kind] ? [original, ...(kind === 'image' ? [{ ...original, role: 'display' }] : [])] : undefined
  const payload = kind === 'text' ? { body: o.body } : kind === 'link' ? { url: o.url } : { caption: '' }
  return {
    id: o.id,
    title: o.title,
    description: o.description ?? '',
    rating: o.rating ?? null,
    kind,
    revision: '1',
    content_id: `c-${o.id}`,
    content_revision_id: `r-${o.id}`,
    content: { id: `r-${o.id}`, content_id: `c-${o.id}`, kind, sequence: '1', payload, truncated: false, ...(media ? { media } : {}) },
    unavailable: false,
    is_favorite: !!o.fav,
    created_at: new Date(now - (o.age ?? 30) * DAY).toISOString(),
    deleted_at: o.trashed ? new Date(now - DAY).toISOString() : null,
    purge_after: o.trashed ? new Date(now + 29 * DAY).toISOString() : null,
    group_ids: o.groups ?? [],
    tag_ids: o.tags ?? [],
  }
}
// 7 份有名字的素材覆盖五种类型，另有 63 张填充图，合计 70 项（超过一页 60 项）。
const fixture = () => [
  asset({ id: 'f-sea', title: '海边逆光', w: 1200, h: 1800, rating: 5, groups: ['g-out'], tags: ['t-back', 't-warm'], age: 1, fav: true, description: '日落前二十分钟' }),
  asset({ id: 'f-veil', title: '白纱与拱门', w: 1600, h: 1067, rating: 4.5, groups: ['g-out', 'g-brand'], tags: ['t-warm', 't-film'], age: 2 }),
  asset({ id: 'f-studio', title: '棚拍布光', w: 1000, h: 1000, rating: 3.5, groups: ['g-in'], tags: ['t-film'], age: 3 }),
  asset({ id: 'f-clip', title: '海风走位', kind: 'video', w: 1920, h: 1080, duration: 12_000, rating: 4, groups: ['g-out'], tags: ['t-back'], age: 4 }),
  asset({ id: 'f-voice', title: '现场环境音', kind: 'audio', duration: 184_000, groups: ['g-in'], tags: ['t-misc'], age: 5 }),
  asset({ id: 'f-note', title: '拍摄脚本 v2', kind: 'text', body: '07:30 集合\n08:10 海边逆光\n09:00 白纱与拱门', rating: 4, tags: ['t-misc'], age: 6 }),
  asset({ id: 'f-ref', title: '布光参考', kind: 'link', url: 'https://www.example.com/lighting/101', groups: ['g-brand'], age: 10 }),
  asset({ id: 'f-old', title: '旧照片', trashed: true, age: 40 }),
  ...Array.from({ length: 63 }, (_, i) =>
    asset({ id: `f-${String(i).padStart(2, '0')}`, title: `素材 ${String(i).padStart(2, '0')}`, w: 1200, h: i % 3 ? 800 : 1500, rating: i % 4 ? null : 3, age: 20 + i }),
  ),
]

const key = (s) => s.normalize('NFKC').toLowerCase().trim()
function mockServer(assets) {
  const s = { assets, reads: [], other: [], revision: '1', hold: null }
  const live = () => s.assets.filter((a) => !a.deleted_at)
  const descendants = (id) => [id, ...groups.filter((g) => g.parent_id === id).map((g) => g.id)]
  function scope(q, { kind = true, tagFilter = true } = {}) {
    const view = q.get('view') ?? 'all'
    let items = view === 'trash' ? s.assets.filter((a) => a.deleted_at) : live()
    if (view === 'favorites') items = items.filter((a) => a.is_favorite)
    if (view === 'unclassified') items = items.filter((a) => !a.group_ids.length)
    if (view === 'recent') items = items.filter((a) => now - Date.parse(a.created_at) <= 7 * DAY)
    const g = q.get('group_id')
    if (g) {
      const ids = q.get('include_descendants') === 'true' ? descendants(g) : [g]
      items = items.filter((a) => a.group_ids.some((x) => ids.includes(x)))
    }
    if (kind && q.get('kind')) items = items.filter((a) => a.kind === q.get('kind'))
    const text = key(q.get('q') ?? '')
    if (text)
      items = items.filter((a) =>
        key([a.title, a.description, a.content.payload.body ?? '', a.content.payload.url ?? '', ...a.tag_ids.map((t) => tags.find((x) => x.id === t).name)].join(' ')).includes(text),
      )
    if (q.get('unrated') === 'true') items = items.filter((a) => a.rating === null)
    if (q.get('min_rating')) items = items.filter((a) => a.rating !== null && a.rating >= Number(q.get('min_rating')))
    const selected = (q.get('tag_ids') ?? '').split(',').filter(Boolean)
    if (tagFilter && selected.length)
      items = items.filter((a) => (q.get('tag_mode') === 'any' ? selected.some((t) => a.tag_ids.includes(t)) : selected.every((t) => a.tag_ids.includes(t))))
    return items
  }
  const sorters = {
    recent: (a, b) => b.created_at.localeCompare(a.created_at),
    oldest: (a, b) => a.created_at.localeCompare(b.created_at),
    name: (a, b) => (key(a.title) < key(b.title) ? -1 : key(a.title) > key(b.title) ? 1 : 0),
    rating: (a, b) => (b.rating ?? -1) - (a.rating ?? -1) || b.created_at.localeCompare(a.created_at),
  }
  s.handle = async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    // 页面已经放弃的请求（条件变了被中止）再回应会报错，忽略即可。
    const json = (value, status = 200) => route.fulfill({ status, json: value }).catch(() => {})
    if (path.endsWith('/auth/refresh')) return json({ access_token: 'qa', token_type: 'Bearer', expires_in: 600 })
    if (path.endsWith('/me')) return json({ id: 'qa-assets', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
    if (path.endsWith('/account/profile')) return json({ display_name: 'Samson', profile_revision: '1', avatar_revision: '0' })
    if (path.endsWith('/creative/projects')) return json({ items: [], next_cursor: '', activity_counts: { running: 0, waiting: 0 } })
    if (path.endsWith('/creative/asset-groups')) {
      const counts = Object.fromEntries(groups.map((g) => [g.id, live().filter((a) => a.group_ids.some((x) => descendants(g.id).includes(x))).length]))
      return json({ items: groups, hierarchy_revision: '1', asset_counts: counts })
    }
    if (path.endsWith('/creative/tags')) return json({ items: tags, hierarchy_revision: '1' })
    if (path.endsWith('/creative/tag-categories')) return json({ items: categories, hierarchy_revision: '1' })
    if (path.endsWith('/creative/library-usage')) return json({ asset_count: live().length, stored_bytes: Math.round(3.24 * GiB), limit_bytes: 20 * GiB })
    if (path.endsWith('/creative/media-access-tickets')) {
      const id = request.postDataJSON().content_revision_id.slice(2)
      const a = s.assets.find((x) => x.id === id)
      if (a?.kind === 'video') return json({ url: `${base}/__qa_media/${id}.webm`, expires_at: new Date(Date.now() + 3600000).toISOString() })
      const hue = [...id].reduce((h, c) => h + c.charCodeAt(0), 0) % 360
      const m = a.content.media[0]
      const art = `<svg xmlns="http://www.w3.org/2000/svg" width="${m.width}" height="${m.height}"><rect width="100%" height="100%" fill="hsl(${hue} 22% 46%)"/><circle cx="70%" cy="30%" r="14%" fill="hsl(${(hue + 40) % 360} 45% 66%)"/></svg>`
      return json({ url: `data:image/svg+xml,${encodeURIComponent(art)}`, expires_at: new Date(Date.now() + 3600000).toISOString() })
    }
    if (path.endsWith('/creative/assets') && request.method() === 'GET') {
      const q = url.searchParams
      s.reads.push(q.toString())
      if (s.hold && s.hold.match(q)) {
        if (s.hold.wait) await s.hold.wait(q)
        else await new Promise((r) => setTimeout(r, s.hold.ms))
      }
      const items = scope(q).sort(sorters[q.get('sort') ?? 'recent'])
      const limit = Number(q.get('limit') ?? 30)
      const offset = Number(q.get('cursor') || 0)
      let facets
      if (q.get('include_facets') === 'true') {
        if (q.get('cursor')) return json({ error: { code: 'validation_failed', message: 'include_facets 只能用于首屏' } }, 400)
        const byKind = scope(q, { kind: false })
        const selected = (q.get('tag_ids') ?? '').split(',').filter(Boolean)
        const loose = scope(q, { tagFilter: false })
        facets = {
          kinds: Object.fromEntries(['text', 'link', 'image', 'video', 'audio'].map((k) => [k, byKind.filter((a) => a.kind === k).length])),
          tags: tags.map((t) => ({
            tag_id: t.id,
            count: loose.filter((a) => (q.get('tag_mode') === 'any' ? a.tag_ids.includes(t.id) : [...selected, t.id].every((x) => a.tag_ids.includes(x)))).length,
          })),
        }
      }
      return json({ items: items.slice(offset, offset + limit), next_cursor: offset + limit < items.length ? String(offset + limit) : '', total_count: items.length, library_revision: s.revision, ...(facets ? { facets } : {}) })
    }
    s.other.push(`${request.method()} ${path}`)
    return json({ error: { code: 'unavailable', message: 'not modelled' } }, 503)
  }
  return s
}

// 瀑布流按列排布，DOM 顺序不是阅读顺序；要比对结果顺序的检查用方格打开。
async function fresh({ width = 1440, height = 900, assets = fixture(), layout = 'masonry' } = {}) {
  await context?.close()
  context = await browser.newContext({ viewport: { width, height } })
  if (layout === 'grid')
    await context.addInitScript(() => {
      if (!localStorage.getItem('creative-assets:prefs'))
        localStorage.setItem('creative-assets:prefs', JSON.stringify({ layout: 'grid', size: 220, inspector: true }))
    })
  server = mockServer(assets)
  page = await context.newPage()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
  await page.route('**/api/v1/**', server.handle)
  await page.route('**/__qa_media/**', (route) => route.fulfill({ status: 200, contentType: 'video/webm', body: webm }))
}
const open = async (query = '') => {
  await page.goto(`${base}/creative/library${query}`)
  await page.locator('.al-tree').waitFor({ state: 'attached' })
  await settled()
}
const until = async (fn, label, ms = 5000) => {
  const start = Date.now()
  while (!(await fn())) {
    if (Date.now() - start > ms) throw new Error(`timeout: ${label}`)
    await new Promise((r) => setTimeout(r, 30))
  }
}
// 做一个会换条件的动作：等新的列表请求真正发出，再等结果画完。
async function after(run) {
  const n = server.reads.length
  await run()
  await until(() => server.reads.length > n, 'list request')
  await settled()
}
const tiles = () => page.locator('.al-tile')
const tile = (title) => page.locator('.al-tile', { has: page.getByText(title, { exact: true }) }).first()
const titles = async () => page.locator('.al-tile').evaluateAll((els) => els.map((e) => e.getAttribute('aria-label')))
const params = () => new URL(page.url()).searchParams
const lastRead = () => new URLSearchParams(server.reads.at(-1))
const settled = () => page.waitForFunction(() => !document.querySelector('.al-tiles[aria-busy="true"]'))
const tree = () => page.getByRole('complementary', { name: '资料库' })
// 树里的一行（视图、分组或标签），按名称精确找，不会撞上「子分组：…」按钮。
const row = (name) => tree().locator('.al-row', { has: page.locator('.al-name', { hasText: new RegExp(`^${name}$`) }) })
const n = async (name) => (await row(name).locator('.al-n').innerText()).trim()
const board = () => page.getByRole('region', { name: '资产' })
const noHorizontalScroll = async (label) => {
  const r = await page.evaluate(() => ({ doc: document.documentElement.scrollWidth, win: innerWidth, board: [...document.querySelectorAll('.al-board, .al-scroll')].map((e) => e.scrollWidth - e.clientWidth) }))
  assert(r.doc <= r.win, `${label}: page scrolls sideways ${r.doc} > ${r.win}`)
  assert(r.board.every((d) => d <= 0), `${label}: board scrolls sideways ${r.board}`)
}
async function check(name, run) {
  try {
    await run()
    results[name] = 'pass'
  } catch (error) {
    results[name] = `fail: ${error.message}`
    await page?.screenshot({ path: `${output}/${name}-failure.png`, fullPage: true }).catch(() => {})
  }
}

// AC-01：独立页面，不加载画布；三档宽度的布局，都没有横向滚动。
await check('AC-01', async () => {
  await fresh()
  await open()
  assert.equal(await page.locator('.react-flow').count(), 0)
  assert(!server.other.some((r) => r.includes('/canvases')), server.other.join('|'))
  assert.equal(await page.getByRole('complementary', { name: '创意空间导航' }).count(), 1)
  assert.equal(await page.getByRole('link', { name: '资产库' }).getAttribute('aria-current'), 'page')
  for (const name of ['资料库', '检查器']) assert(await page.getByRole('complementary', { name }).isVisible(), name)
  assert(await board().isVisible())
  await noHorizontalScroll('1440')
  // 工具条放得下：标题和排序不被挤得截断，缩放滑杆可用。
  for (const el of [page.locator('.al-crumb h2'), page.getByRole('combobox', { name: '排序' }).locator('.cc-picker-value')])
    assert(await el.evaluate((e) => e.scrollWidth <= e.clientWidth), 'toolbar text is truncated')
  assert(await page.getByRole('slider', { name: '缩略图大小' }).isVisible())
  await page.screenshot({ path: `${output}/ac01-desktop.png` })
  // 检查器可以收起，记在本机，刷新后保持。
  await page.getByRole('button', { name: '检查器' }).click()
  assert.equal(await page.getByRole('complementary', { name: '检查器' }).count(), 0)
  await after(() => page.reload())
  assert.equal(await page.getByRole('complementary', { name: '检查器' }).count(), 0)
  assert.equal(await page.getByRole('button', { name: '检查器' }).getAttribute('aria-pressed'), 'false')
  await page.getByRole('button', { name: '检查器' }).click()

  await page.setViewportSize({ width: 1000, height: 800 })
  // 等侧栏折叠的过渡动画结束再截图。
  await page.waitForTimeout(400)
  assert(await tree().isVisible())
  assert.equal(await page.getByRole('complementary', { name: '检查器' }).isVisible(), false)
  assert.equal(await page.getByRole('button', { name: '检查器' }).isVisible(), false)
  await noHorizontalScroll('1000')
  await page.screenshot({ path: `${output}/ac01-medium.png` })

  await page.setViewportSize({ width: 390, height: 844 })
  await page.waitForTimeout(400)
  assert.equal(await tree().isVisible(), false)
  // 手机上没有缩放滑杆，瀑布流至少两列。
  assert.equal(await page.locator('.al-tiles .cl-masonry-col').count(), 2)
  const side = await page.locator('.cs-side').boundingBox()
  assert(side.height < 80 && side.width >= 380, `sidebar becomes a top bar ${JSON.stringify(side)}`)
  const views = page.getByRole('combobox', { name: '视图' })
  assert(await views.isVisible())
  await noHorizontalScroll('390')
  await page.screenshot({ path: `${output}/ac01-phone.png` })
  await after(() => views.selectOption({ label: '　外景' }))
  await page.getByRole('heading', { name: /婚礼.*外景/ }).waitFor()
  assert.equal(params().get('group'), 'g-out')
  assert.equal(await tiles().count(), 3)
  await noHorizontalScroll('390 group')
})

// AC-02：资料库树：固定视图、两层分组（计数含子组）、按分类的标签、用量。
await check('AC-02', async () => {
  await fresh({ layout: 'grid' })
  await open()
  const views = await tree().getByRole('navigation', { name: '视图' }).getByRole('button').allInnerTexts()
  assert.deepEqual(views.map((v) => v.trim()), ['全部', '未归类', '最爱', '最近添加', '回收站'])
  assert.equal(await n('婚礼'), '5')
  assert.equal(await n('外景'), '3')
  assert.equal(await n('棚拍'), '2')
  assert.equal(await n('品牌'), '2')
  // 收起父组，子组随之隐藏；进入子组时父组自动展开。
  await tree().getByRole('button', { name: '子分组：婚礼' }).click()
  assert.equal(await row('外景').count(), 0)
  await tree().getByRole('button', { name: '子分组：婚礼' }).click()
  assert.equal(await row('外景').count(), 1)
  // 最近添加：7 天内，按添加时间倒序。
  await after(() => row('最近添加').click())
  assert.equal(lastRead().get('view'), 'recent')
  assert.deepEqual(await titles(), ['海边逆光', '白纱与拱门', '棚拍布光', '海风走位', '现场环境音', '拍摄脚本 v2'])
  await after(() => row('全部').click())
  // 标签按分类列出，数量随当前条件；点一下加入筛选。
  const cats = await tree().locator('.al-tag-cat').allInnerTexts()
  assert.deepEqual(cats, ['风格', '光线', '未分类'])
  assert.equal(await n('暖调'), '2')
  await after(() => row('暖调').click())
  assert.equal(params().get('tags'), 't-warm')
  assert.equal(await row('暖调').getAttribute('aria-pressed'), 'true')
  assert.deepEqual(await titles(), ['海边逆光', '白纱与拱门'])
  // all 模式下的数量是「再勾上它还剩几项」。
  assert.equal(await n('胶片'), '1')
  const usage = await tree().locator('.al-usage').innerText()
  assert.match(usage, /70 项 · 已用 3\.2 GB \/ 20 GB/)
  const meter = await tree().getByRole('meter', { name: '空间用量' }).locator('i').evaluate((e) => e.style.width)
  assert.match(meter, /^16\.\d+%$/)
  await page.screenshot({ path: `${output}/ac02-tree.png` })
})

// AC-03 + V6-016：类型分段带数量、搜索、标签模式、pill、排序、布局偏好、换视图清空、面包屑与子分组。
await check('AC-03', async () => {
  await fresh({ layout: 'grid' })
  await open()
  const kinds = page.getByRole('group', { name: '类型' })
  assert.deepEqual((await kinds.getByRole('button').allInnerTexts()).map((t) => t.replace(/\s+/g, '')), ['全部70', '图片66', '视频1', '音频1', '文字1', '链接1'])
  // 列表、总数和计数来自同一个首屏请求，不另发计数请求。
  const firstReads = server.reads.filter((r) => !r.includes('cursor'))
  assert(firstReads.every((r) => r.includes('include_facets=true')), firstReads.join('\n'))
  // 进入棚拍：没有的类型不显示；面包屑显示父组。
  await after(() => row('棚拍').click())
  await page.getByRole('heading', { name: /婚礼.*棚拍/ }).waitFor()
  assert.deepEqual((await kinds.getByRole('button').allInnerTexts()).map((t) => t.replace(/\s+/g, '')), ['全部2', '图片1', '音频1'])
  await after(() => board().getByRole('heading').getByRole('button', { name: '婚礼' }).click())
  await page.getByRole('heading', { name: '婚礼', exact: true }).waitFor()
  // 进入父组：子分组 chip，包含子组的素材。
  const chips = await page.getByLabel('子分组').getByRole('button').allInnerTexts()
  assert.deepEqual(chips.map((t) => t.replace(/\s+/g, '')), ['外景3', '棚拍2'])
  assert.equal(lastRead().get('include_descendants'), 'true')
  assert.equal(await tiles().count(), 5)
  await after(() => page.getByLabel('子分组').getByRole('button', { name: /外景/ }).click())
  await page.getByRole('heading', { name: /婚礼.*外景/ }).waitFor()
  // 进分组记进历史，可以后退。
  await after(() => page.goBack())
  await page.getByRole('heading', { name: '婚礼', exact: true }).waitFor()
  await after(() => row('全部').click())

  // 关键词覆盖标题、备注、正文、网址和标签。
  const search = page.getByRole('searchbox', { name: '搜索素材' })
  assert.match(await search.getAttribute('placeholder'), /标题.*备注.*正文.*网址.*标签/)
  for (const [q, expected] of [
    ['日落', ['海边逆光']],
    ['08:10', ['拍摄脚本 v2']],
    ['example.com', ['布光参考']],
    ['胶片', ['白纱与拱门', '棚拍布光']],
  ]) {
    await after(() => search.fill(q))
    assert.equal(params().get('q'), q)
    assert.deepEqual(await titles(), expected, q)
  }
  // 按 / 聚焦搜索框。
  await page.locator('.al-tile').first().focus()
  await page.keyboard.press('/')
  assert(await search.evaluate((e) => e === document.activeElement))
  await after(() => board().getByRole('button', { name: '移除条件：“胶片”' }).click())
  assert.equal(await search.inputValue(), '')
  assert.equal(params().has('q'), false)

  // 标签：选一个没有「同时满足 / 满足任一」，选两个后出现，切换后结果变化。
  await board().getByRole('button', { name: '标签', exact: true }).click()
  const pop = page.getByRole('dialog', { name: '标签筛选' })
  await after(() => pop.getByRole('button', { name: /暖调/ }).click())
  assert.equal(await pop.getByRole('button', { name: /同时满足|满足任一/ }).count(), 0)
  await after(() => pop.getByRole('button', { name: /胶片/ }).click())
  assert.deepEqual(await titles(), ['白纱与拱门'])
  await after(() => pop.getByRole('button', { name: '同时满足全部所选' }).click())
  assert.equal(params().get('mode'), 'any')
  assert.deepEqual(await titles(), ['海边逆光', '白纱与拱门', '棚拍布光'])
  // any 模式的数量忽略已选标签。
  assert.equal((await pop.getByRole('button', { name: /逆光/ }).locator('.cl-tag-n').innerText()).trim(), '2')
  await page.keyboard.press('Escape')
  const pills = page.getByLabel('生效的条件')
  assert.deepEqual((await pills.locator('.al-pill').allInnerTexts()).map((t) => t.trim()), ['暖调', '胶片', '满足任一', '清除全部'])
  await after(() => pills.getByRole('button', { name: '移除条件：暖调' }).click())
  assert.equal(params().get('tags'), 't-film')
  assert.equal(await pills.getByRole('button', { name: /满足任一|同时满足/ }).count(), 0)

  // 评分筛选与按评分排序。
  await page.getByRole('combobox', { name: '评分' }).click()
  await after(() => page.getByRole('option', { name: '4 星以上' }).click())
  assert.equal(lastRead().get('min_rating'), '4')
  assert.deepEqual(await titles(), ['白纱与拱门'])
  await after(() => pills.getByRole('button', { name: '清除全部' }).click())
  await page.getByRole('combobox', { name: '评分' }).click()
  await after(() => page.getByRole('option', { name: '未评分' }).click())
  assert.equal(lastRead().get('unrated'), 'true')
  assert.equal(lastRead().has('min_rating'), false)
  await after(() => pills.getByRole('button', { name: '清除全部' }).click())
  await page.getByRole('combobox', { name: '排序' }).click()
  await after(() => page.getByRole('option', { name: '评分' }).click())
  assert.equal(lastRead().get('sort'), 'rating')
  assert.deepEqual((await titles()).slice(0, 4), ['海边逆光', '白纱与拱门', '海风走位', '拍摄脚本 v2'])

  // 换视图时清空筛选，排序保留。
  await after(() => kinds.getByRole('button', { name: /图片/ }).click())
  await after(() => search.fill('素材'))
  await after(() => row('最爱').click())
  assert.deepEqual(Object.fromEntries(params()), { view: 'favorites', sort: 'rating' })
  assert.equal(await search.inputValue(), '')
  assert.deepEqual(await titles(), ['海边逆光'])

  // 瀑布流 / 方格和缩略图大小记在本机。
  await after(() => row('全部').click())
  await board().getByRole('button', { name: '瀑布流' }).click()
  await page.locator('.al-tiles .cl-masonry-col').first().waitFor()
  await page.getByRole('slider', { name: '缩略图大小' }).evaluate((el) => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, '300')
    el.dispatchEvent(new Event('input', { bubbles: true }))
  })
  // 浏览区约 820px 宽，300px 的卡片排两列。
  await page.waitForFunction(() => document.querySelectorAll('.al-tiles .cl-masonry-col').length === 2)
  await page.screenshot({ path: `${output}/ac03-masonry.png` })
  await after(() => page.reload())
  assert.equal(await board().getByRole('button', { name: '瀑布流' }).getAttribute('aria-pressed'), 'true')
  assert.equal(await page.getByRole('slider', { name: '缩略图大小' }).inputValue(), '300')
  assert.equal(await page.locator('.al-tiles .cl-masonry-col').count(), 2)
  await board().getByRole('button', { name: '方格' }).click()
  const [w, h] = await tile('海边逆光').locator('.al-thumb').evaluate((e) => [e.clientWidth, e.clientHeight])
  assert(w >= 300 && Math.abs(w - h) <= 1, `grid cells are square ${w}×${h}`)
  await page.screenshot({ path: `${output}/ac03-grid.png` })
})

// V6-016 AC-03：快速切换时旧请求不覆盖新结果；空结果可以清除条件或扩大范围。
await check('V6-016-AC-03', async () => {
  await fresh({ layout: 'grid' })
  await open()
  server.hold = { match: (q) => q.get('kind') === 'video', ms: 1200 }
  const kinds = page.getByRole('group', { name: '类型' })
  await kinds.getByRole('button', { name: /视频/ }).click()
  await after(() => kinds.getByRole('button', { name: /文字/ }).click())
  // 等到被拖住的视频请求也回来了，结果仍是后选的文字。
  await page.waitForTimeout(1500)
  assert.deepEqual(await titles(), ['拍摄脚本 v2'])
  assert.equal(await kinds.getByRole('button', { name: /文字/ }).getAttribute('aria-pressed'), 'true')
  server.hold = null
  await after(() => row('棚拍').click())
  await after(() => page.getByRole('searchbox', { name: '搜索素材' }).fill('海边'))
  await page.getByText('没有符合条件的素材').waitFor()
  await page.screenshot({ path: `${output}/v6016-empty.png` })
  await after(() => page.getByRole('button', { name: '在全部里查找' }).click())
  assert.equal(params().get('q'), '海边')
  assert.equal(params().has('group'), false)
  assert.deepEqual(await titles(), ['海边逆光', '拍摄脚本 v2'])
  await after(() => page.getByRole('searchbox', { name: '搜索素材' }).fill('没有这个词'))
  await page.getByText('没有符合条件的素材').waitFor()
  assert.equal(await page.getByRole('button', { name: '在全部里查找' }).count(), 0)
  await after(() => page.getByRole('button', { name: '清除条件' }).click())
  assert.equal(await tiles().count(), 60)
})

// 翻页：滚到底部附近按游标取下一页，不再带计数；翻页时资料库变化则提示刷新。
await check('paging', async () => {
  await fresh()
  await open()
  assert.equal(await tiles().count(), 60)
  await page.locator('.al-end').scrollIntoViewIfNeeded()
  await page.waitForFunction(() => document.querySelectorAll('.al-tile').length === 70)
  const next = new URLSearchParams(server.reads.find((r) => r.includes('cursor=')))
  assert.equal(next.get('cursor'), '60')
  assert.equal(next.has('include_facets'), false)
  assert.equal(new Set(await titles()).size, 70)
  await after(() => page.reload())
  server.revision = '2'
  await page.locator('.al-end').scrollIntoViewIfNeeded()
  await page.getByText('资产库刚有变化，后面的内容需要重新读取。').waitFor()
  assert.equal(await tiles().count(), 60)
  await after(() => page.getByRole('button', { name: '刷新' }).click())
  await page.getByText('资产库刚有变化').waitFor({ state: 'detached' })
  // 切回标签页时重读。
  await after(() => page.evaluate(() => document.dispatchEvent(new Event('visibilitychange'))))
})

// 切回标签页刷新首屏时，旧列表仍可见，但不能拿旧游标继续翻页，否则会跳过中间一页。
await check('refresh-paging', async () => {
  const assets = Array.from({ length: 180 }, (_, i) => asset({ id: `refresh-${i}`, title: `素材 ${i}`, age: i }))
  await fresh({ assets, layout: 'grid' })
  await open()
  await page.locator('.al-end').scrollIntoViewIfNeeded()
  await page.waitForFunction(() => document.querySelectorAll('.al-tile').length === 120)
  await page.locator('.al-scroll').evaluate((e) => e.scrollTo({ top: 0 }))
  await page.waitForTimeout(100)
  let release
  const held = new Promise((resolve) => { release = resolve })
  server.hold = { match: (q) => !q.has('cursor'), wait: () => held }
  const start = server.reads.length
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')))
  await until(() => server.reads.length > start, 'refresh request')
  try {
    assert.equal(await tiles().count(), 120, 'keep the current page visible during refresh')
    await page.locator('.al-end').scrollIntoViewIfNeeded()
    await page.waitForTimeout(250)
    assert.equal(server.reads.slice(start).some((r) => new URLSearchParams(r).has('cursor')), false, 'must not page with an old cursor while the first page is refreshing')
  } finally {
    // 首屏回来前回到顶部，避免底部观察器立即加载下一页，掩盖首屏断言。
    await page.locator('.al-scroll').evaluate((e) => e.scrollTo({ top: 0 }))
    await page.waitForTimeout(100)
    release()
    server.hold = null
  }
  await page.waitForFunction(() => document.querySelectorAll('.al-tile').length === 60)
  for (const count of [120, 180]) {
    await page.locator('.al-end').scrollIntoViewIfNeeded()
    await page.waitForFunction((n) => document.querySelectorAll('.al-tile').length === n, count)
  }
  assert.deepEqual(await titles(), assets.map((a) => a.title), 'refresh and subsequent pages must not omit or reorder any assets')
})

// AC-01/03 边界：长分组名、子分组和200字搜索词不能把浏览区或移除按钮撑出屏幕。
await check('long-content', async () => {
  const originalNames = groups.map((g) => g.name)
  const longQuery = '测试'.repeat(100)
  groups[0].name = '父组'.repeat(25)
  groups[1].name = '子组'.repeat(25)
  try {
    for (const width of [390, 1000, 1440]) {
      await fresh({ width })
      await open('?group=g-out')
      await noHorizontalScroll(`long breadcrumbs ${width}`)
      await after(() => page.locator('.al-crumb h2 button').click())
      assert.equal(params().get('group'), 'g-wed')
      await noHorizontalScroll(`long subgroup ${width}`)
      await after(() => page.getByRole('button', { name: groups[1].name, exact: false }).filter({ has: page.locator('small') }).click())
      assert.equal(params().get('group'), 'g-out')
      await after(() => page.getByRole('searchbox', { name: '搜索素材' }).fill(longQuery))
      const remove = page.getByRole('button', { name: `移除条件：“${longQuery}”` })
      const r = await remove.boundingBox()
      assert(r && r.x >= 0 && r.x + r.width <= width, `remove button outside viewport at ${width}`)
      await noHorizontalScroll(`long search ${width}`)
      await page.screenshot({ path: `${output}/long-content-${width}.png` })
      await after(() => remove.click())
      assert.equal(params().has('q'), false)
      assert.equal(await page.getByRole('searchbox', { name: '搜索素材' }).inputValue(), '')
    }
  } finally {
    groups.forEach((g, i) => { g.name = originalNames[i] })
  }
})

// AC-09 + V6-060：卡片下方只有标题和只读星级；五种类型各有样式；链接不抓取网页。
await check('AC-09', async () => {
  await fresh()
  const external = []
  page.on('request', (r) => {
    if (r.url().includes('example.com')) external.push(r.url())
  })
  await open('?view=recent')
  const sea = tile('海边逆光')
  assert.equal((await sea.locator('.al-cap > span').first().innerText()).trim(), '海边逆光')
  assert.equal(await sea.getByRole('img', { name: '5 星' }).count(), 1)
  const fill = await sea.locator('.cl-star-fill').first().evaluate((e) => getComputedStyle(e).color)
  assert.equal(fill, 'rgb(255, 255, 255)')
  assert.equal(await tile('白纱与拱门').getByRole('img', { name: '4.5 星' }).count(), 1)
  // 视频：首帧封面 + 时长。
  const clip = tile('海风走位')
  await clip.locator('video[preload="metadata"]').waitFor({ state: 'attached' })
  assert.equal((await clip.locator('.cl-video-badge').innerText()).trim(), '0:12')
  // 音频：装饰条 + 时长，不是通用图标；未评分的没有星级。
  const voice = tile('现场环境音')
  assert.equal(await voice.locator('.cl-wave i').count(), 36)
  assert.equal((await voice.locator('.cl-card-row small').innerText()).trim(), '3:04')
  assert.equal(await voice.locator('.cc-audio, .cc-media-placeholder').count(), 0)
  assert.equal(await voice.locator('.al-cap').count(), 0)
  // 文字：正文前几行；卡片下方不再重复标题，只放星级。
  const note = tile('拍摄脚本 v2')
  assert.match(await note.locator('.cl-card-text p').innerText(), /07:30 集合/)
  assert.equal((await note.locator('.al-cap > span').first().innerText()).trim(), '')
  assert.equal(await note.getByRole('img', { name: '4 星' }).count(), 1)
  await page.screenshot({ path: `${output}/ac09-cards.png` })
  await after(() => row('品牌').click())
  const ref = tile('布光参考')
  assert.equal((await ref.locator('.cl-card-link small').innerText()).trim(), 'example.com')
  assert.equal(external.length, 0, external.join('|'))
  // 单击选中，检查器显示详情；点空白处取消。
  await tile('白纱与拱门').click()
  const inspector = page.getByRole('complementary', { name: '检查器' })
  await inspector.getByRole('heading', { name: '白纱与拱门' }).waitFor()
  assert.match(await inspector.innerText(), /1600 × 1067/)
  assert.match(await inspector.innerText(), /外景/)
  await page.screenshot({ path: `${output}/ac09-inspector.png` })
  await page.locator('.al-scroll').click({ position: { x: 5, y: 600 } })
  await inspector.getByText('没有选中素材').waitFor()
})

await context?.close()
await browser.close()
console.log(JSON.stringify(results, null, 1))
if (errors.length) console.error('page errors:', errors)
assert(Object.values(results).every((r) => r === 'pass') && !errors.length)
