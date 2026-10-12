// Browser regression for the creative projects page (V6-064). The API is a
// small in-memory mock with revisions, idempotent replay and fault injection;
// each AC runs in a fresh context.
// Run against the Vite dev server: node scripts/creative-projects.e2e.mjs.
import assert from 'node:assert/strict'
import { mkdirSync } from 'node:fs'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const output = process.env.CREATIVE_QA_OUTPUT_DIR ?? '/tmp/creative-projects-qa'
mkdirSync(output, { recursive: true })
const browser = await chromium.launch({ headless: true })
const errors = []
let context, page, server

const project = (id, name, minutesAgo, extra = {}) => ({
  id,
  name,
  default_canvas_id: `c-${id}`,
  revision: '1',
  archived: false,
  updated_at: '2026-10-01T00:00:00Z',
  edited_at: new Date(Date.now() - minutesAgo * 60000).toISOString(),
  running: false,
  waiting: false,
  cover: { nodes: [], edges: [], more: false },
  ...extra,
})
// 38 个当前项目（超过一页 30 个）和 2 个已归档项目；名字里的 Spring 大小写混写。
const fixture = () => [
  project('p-sept', '九月，向海而行', 1),
  project('p-run', 'Lookbook 运行中', 2, { running: true }),
  project('p-both', 'Lookbook 双状态', 3, { running: true, waiting: true }),
  ...Array.from({ length: 35 }, (_, i) =>
    project(`p-s${String(i).padStart(2, '0')}`, `春日 ${['Spring', 'SPRING', 'spring'][i % 3]} ${String(i).padStart(2, '0')}`, 10 + i),
  ),
  project('p-old-a', '旧档 A', 600, { archived: true }),
  project('p-old-b', '旧档 B', 700, { archived: true }),
]
const key = (s) => s.normalize('NFKC').toLowerCase().trim()

function mockServer(projects) {
  const receipts = new Map()
  const s = { projects, reads: [], writes: [], fault: null }
  s.handle = async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    const json = (value, status = 200) => route.fulfill({ status, json: value })
    if (path.endsWith('/auth/refresh')) return json({ access_token: 'qa', token_type: 'Bearer', expires_in: 600 })
    if (path.endsWith('/me')) return json({ id: 'qa-projects', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
    if (path.endsWith('/account/profile')) return json({ display_name: 'Samson', profile_revision: '1', avatar_revision: '0' })
    if (path.endsWith('/creative/projects') && request.method() === 'GET') {
      s.reads.push(Object.fromEntries(url.searchParams))
      const archived = url.searchParams.get('archived') === 'true'
      const q = key(url.searchParams.get('q') ?? '')
      const sort = url.searchParams.get('sort') ?? 'edited_desc'
      const limit = Number(url.searchParams.get('limit') ?? 30)
      const offset = Number(url.searchParams.get('cursor') || 0)
      const items = s.projects
        .filter((p) => p.archived === archived && key(p.name).includes(q))
        .sort((a, b) =>
          sort === 'name'
            ? key(a.name) < key(b.name) ? -1 : key(a.name) > key(b.name) ? 1 : a.id < b.id ? -1 : 1
            : sort === 'edited_asc'
              ? a.edited_at.localeCompare(b.edited_at)
              : b.edited_at.localeCompare(a.edited_at),
        )
      const live = s.projects.filter((p) => !p.archived)
      return json({
        items: items.slice(offset, offset + limit),
        next_cursor: offset + limit < items.length ? String(offset + limit) : '',
        activity_counts: { running: live.filter((p) => p.running).length, waiting: live.filter((p) => p.waiting).length },
      })
    }
    const write = path.match(/\/creative\/projects\/([^/]+)\/(archive|restore|rename)$/)
    if (write && request.method() === 'POST') {
      const operation = request.headers()['idempotency-key']
      const body = request.postDataJSON()
      s.writes.push({ id: decodeURIComponent(write[1]), action: write[2], operation, payload: body.payload })
      const replay = receipts.get(operation)
      if (replay) return json(replay.body, replay.status)
      const fault = s.fault?.action === write[2] ? s.fault : null
      if (fault) s.fault = null
      if (fault?.mode === 'conflict') {
        const p = s.projects.find((x) => x.id === decodeURIComponent(write[1]))
        p.name = fault.name
        p.revision = String(Number(p.revision) + 1)
      }
      if (fault?.mode === 'busy') return json({ error: { code: 'creative_project_busy', message: '项目里还有任务在运行，结束后再归档' } }, 409)
      const p = s.projects.find((x) => x.id === decodeURIComponent(write[1]))
      let result
      if (!p) result = { status: 404, body: { error: { code: 'not_found', message: '项目不存在' } } }
      else if (p.revision !== body.payload.expected_revision)
        result = { status: 409, body: { error: { code: 'creative_revision_conflict', message: '内容已被其他窗口修改，本机这次修改未保存' } } }
      else if (write[2] === 'archive' && p.running)
        result = { status: 409, body: { error: { code: 'creative_project_busy', message: '项目里还有任务在运行，结束后再归档' } } }
      else {
        p.revision = String(Number(p.revision) + 1)
        if (write[2] === 'rename') {
          p.name = body.payload.name
          p.edited_at = new Date().toISOString()
        } else p.archived = write[2] === 'archive'
        result = { status: 200, body: { project_id: p.id, revision: p.revision, ...(write[2] === 'rename' ? {} : { archived: p.archived }) } }
      }
      receipts.set(operation, result)
      // 服务端已经处理，但响应在路上丢了。
      if (fault?.mode === 'lost') return route.abort('failed')
      return json(result.body, result.status)
    }
    return json({ error: { code: 'unavailable', message: 'not modelled' } }, 503)
  }
  return s
}

async function fresh({ width = 1280, height = 900, projects = fixture() } = {}) {
  await context?.close()
  context = await browser.newContext({ viewport: { width, height } })
  server = mockServer(projects)
  page = await context.newPage()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
  await page.route('**/api/v1/**', server.handle)
}
const open = async (query = '') => {
  await page.goto(`${base}/creative/projects${query}`)
  await page.getByRole('heading', { name: '项目', exact: true }).waitFor()
}
const cards = () => page.locator('.pp-card')
const names = () => page.locator('.pp-card .pp-title strong').allInnerTexts()
const card = (name) => page.locator('.pp-card', { has: page.getByRole('link', { name: `打开项目：${name}`, exact: true }) })
const menuOf = async (name) => {
  await card(name).hover()
  await page.getByRole('button', { name: `项目操作：${name}` }).click()
  return page.getByRole('menu', { name: `项目操作：${name}` })
}
const toast = () => page.locator('.pp-toast')
const search = () => page.getByRole('searchbox', { name: '搜索项目' })
const loaded = async (count) => page.waitForFunction((n) => document.querySelectorAll('.pp-card').length === n, count)
const noHorizontalScroll = async (label) => {
  const sizes = await page.evaluate(() => {
    const main = document.querySelector('.cs-main')
    return [document.documentElement.scrollWidth, document.documentElement.clientWidth, main.scrollWidth, main.clientWidth]
  })
  assert.ok(sizes[0] <= sizes[1] && sizes[2] <= sizes[3], `${label}: scrolls sideways ${sizes}`)
}
const results = {}
let step = ''
const at = (label) => { step = label }
const only = process.env.ONLY?.split(',')
const record = async (ac, fn) => {
  if (only && !only.includes(ac)) return
  step = ''
  try {
    await fn()
    results[ac] = 'pass'
  } catch (error) {
    results[ac] = `fail at ${step}: ${error.message}`
  }
}

try {
  // AC-01 标签「当前 / 已归档」；服务端搜索不区分大小写、跨页正确；三种排序，切换排序保留搜索词。
  await record('AC-01', async () => {
    await fresh()
    await open()
    await loaded(30)
    at('tabs')
    const tabs = page.getByRole('group', { name: '项目状态' }).getByRole('button')
    assert.deepEqual(await tabs.allInnerTexts(), ['当前', '已归档'])
    assert.equal(await page.getByText('进行中').count(), 0)
    assert.equal(await tabs.first().getAttribute('aria-pressed'), 'true')
    at('first page and more')
    assert.deepEqual(server.reads.at(-1), { archived: 'false', sort: 'edited_desc', limit: '30' })
    await page.getByRole('button', { name: '加载更多项目' }).click()
    await loaded(38)
    at('debounced server search')
    const before = server.reads.length
    await search().pressSequentially('sPRing', { delay: 40 })
    await page.waitForURL(/q=sPRing/)
    await loaded(30)
    const searches = server.reads.slice(before)
    assert.equal(searches.length, 1, `one request after typing: ${JSON.stringify(searches)}`)
    assert.equal(searches[0].q, 'sPRing')
    assert.ok((await names()).every((n) => /spring/i.test(n)), 'every result matches')
    at('search across pages')
    await page.getByRole('button', { name: '加载更多项目' }).click()
    await loaded(35)
    assert.equal(server.reads.at(-1).q, 'sPRing', 'cursor read keeps the query')
    assert.equal(new Set(await names()).size, 35)
    assert.equal(await page.getByRole('button', { name: '加载更多项目' }).count(), 0)
    at('sort keeps the query')
    await page.getByRole('combobox', { name: '排序' }).selectOption('name')
    await page.waitForURL(/sort=name/)
    assert.match(page.url(), /q=sPRing/)
    await loaded(30)
    assert.deepEqual({ q: server.reads.at(-1).q, sort: server.reads.at(-1).sort }, { q: 'sPRing', sort: 'name' })
    const byName = await names()
    assert.deepEqual(byName, [...byName].sort((a, b) => (key(a) < key(b) ? -1 : 1)))
    assert.equal(await search().inputValue(), 'sPRing')
    at('oldest first')
    await page.getByRole('combobox', { name: '排序' }).selectOption('edited_asc')
    await page.waitForURL(/sort=edited_asc/)
    await page.locator('.pp-card').first().filter({ hasText: '春日 SPRING 34' }).waitFor()
    at('tab keeps query and sort')
    await tabs.nth(1).click()
    await page.waitForURL(/view=archived/)
    assert.match(page.url(), /q=sPRing/)
    assert.match(page.url(), /sort=edited_asc/)
    await page.screenshot({ path: `${output}/projects-search.png` })
  })

  // AC-02 就地重命名：Enter 或失焦提交，Esc 取消，空名保留原名；冲突时重读并提示。
  await record('AC-02', async () => {
    await fresh()
    await open()
    const original = '九月，向海而行'
    at('enter submits trimmed')
    await (await menuOf(original)).getByRole('menuitem', { name: '重命名' }).click()
    const input = page.getByRole('textbox', { name: '项目名称' })
    assert.equal(await input.evaluate((el) => el === document.activeElement && el.selectionEnd - el.selectionStart), original.length, 'focused with the name selected')
    await input.fill('  九月 · 定稿  ')
    await input.press('Enter')
    await card('九月 · 定稿').waitFor()
    assert.deepEqual(server.writes.map((w) => [w.action, w.payload]), [['rename', { expected_revision: '1', name: '九月 · 定稿' }]])
    at('escape cancels')
    await (await menuOf('九月 · 定稿')).getByRole('menuitem', { name: '重命名' }).click()
    await input.fill('不要这个')
    await input.press('Escape')
    await card('九月 · 定稿').waitFor()
    assert.equal(server.writes.length, 1)
    at('blank keeps the name')
    await (await menuOf('九月 · 定稿')).getByRole('menuitem', { name: '重命名' }).click()
    await input.fill('   ')
    await input.press('Enter')
    await card('九月 · 定稿').waitFor()
    assert.equal(server.writes.length, 1)
    at('blur submits')
    await (await menuOf('Lookbook 运行中')).getByRole('menuitem', { name: '重命名' }).click()
    await input.fill('Lookbook 夏季')
    await page.getByRole('heading', { name: '项目', exact: true }).click()
    await card('Lookbook 夏季').waitFor()
    assert.equal(server.writes.length, 2)
    assert.equal(server.writes[1].payload.name, 'Lookbook 夏季')
    at('conflict reloads')
    server.fault = { action: 'rename', mode: 'conflict', name: '别处改的名字' }
    await (await menuOf('Lookbook 双状态')).getByRole('menuitem', { name: '重命名' }).click()
    await input.fill('我的名字')
    await input.press('Enter')
    await toast().filter({ hasText: '项目已在别处改过' }).waitFor()
    await card('别处改的名字').waitFor()
    assert.equal(await card('我的名字').count(), 0)
  })

  // AC-03 归档、恢复后 toast 可撤销；运行中的项目归档项禁用并说明原因，服务端同样拒绝。
  await record('AC-03', async () => {
    await fresh()
    await open()
    at('archive and undo')
    await (await menuOf('九月，向海而行')).getByRole('menuitem', { name: '归档' }).click()
    await card('九月，向海而行').waitFor({ state: 'detached' })
    assert.equal(await toast().locator('span').innerText(), '已归档「九月，向海而行」')
    await toast().getByRole('button', { name: '撤销' }).click()
    await card('九月，向海而行').waitFor()
    assert.deepEqual(server.writes.map((w) => [w.action, w.payload.expected_revision]), [['archive', '1'], ['restore', '2']], 'undo uses the new revision')
    assert.notEqual(server.writes[0].operation, server.writes[1].operation)
    await toast().filter({ hasText: '已撤销归档' }).waitFor()
    at('running cannot be archived')
    for (const name of ['Lookbook 运行中', 'Lookbook 双状态']) {
      const menu = await menuOf(name)
      const item = menu.getByRole('menuitem', { name: /归档/ })
      assert.ok(await item.isDisabled(), `${name}: archive disabled`)
      assert.match(await item.innerText(), /有任务在运行，结束后可归档/)
      await page.keyboard.press('Escape')
    }
    assert.equal(await card('Lookbook 双状态').locator('.pc-badge').innerText(), '等你确认', 'badge shows waiting, archive still follows running')
    at('server refuses a busy project')
    server.fault = { action: 'archive', mode: 'busy' }
    await (await menuOf('春日 Spring 00')).getByRole('menuitem', { name: '归档' }).click()
    await toast().filter({ hasText: '有任务在运行，结束后可归档' }).waitFor()
    assert.equal(await card('春日 Spring 00').count(), 1)
    at('lost response retries with the same operation')
    server.fault = { action: 'archive', mode: 'lost' }
    await (await menuOf('春日 SPRING 01')).getByRole('menuitem', { name: '归档' }).click()
    await toast().filter({ hasText: '归档结果未确认' }).waitFor()
    await toast().getByRole('button', { name: '重试' }).click()
    await card('春日 SPRING 01').waitFor({ state: 'detached' })
    const tries = server.writes.filter((w) => w.id === 'p-s01')
    assert.equal(tries.length, 2)
    assert.equal(tries[0].operation, tries[1].operation, 'retry reuses the operation')
    assert.equal(server.projects.find((p) => p.id === 'p-s01').revision, '2', 'committed once')
    at('sidebar follows')
    await page.getByRole('navigation', { name: '最近打开的项目' }).getByRole('link', { name: '春日 SPRING 01' }).waitFor({ state: 'detached' })
    at('restore and undo')
    await page.getByRole('group', { name: '项目状态' }).getByRole('button', { name: '已归档' }).click()
    await card('旧档 A').waitFor()
    const archivedMenu = await menuOf('旧档 A')
    assert.deepEqual(await archivedMenu.getByRole('menuitem').allInnerTexts(), ['打开', '恢复'])
    await archivedMenu.getByRole('menuitem', { name: '恢复' }).click()
    await card('旧档 A').waitFor({ state: 'detached' })
    assert.equal(await toast().locator('span').innerText(), '已恢复「旧档 A」')
    await toast().getByRole('button', { name: '撤销' }).click()
    await card('旧档 A').waitFor()
    assert.equal(server.projects.find((p) => p.id === 'p-old-a').archived, true)
    await page.screenshot({ path: `${output}/projects-archived.png` })
  })

  // AC-04 新建只在右上角一处，网格里没有占位卡；三种空态文案不同。
  await record('AC-04', async () => {
    await fresh()
    await open()
    await loaded(30)
    at('one create button')
    assert.equal(await page.getByRole('button', { name: '新建项目' }).count(), 1)
    assert.equal(await page.getByText('开启新的创作').count(), 0)
    const create = await page.getByRole('button', { name: '新建项目' }).boundingBox()
    const head = await page.locator('.pp-head').boundingBox()
    assert.ok(create.x + create.width > head.x + head.width - 2, 'at the right end of the header')
    await page.screenshot({ path: `${output}/projects-1280.png` })
    for (const w of [1000, 390]) {
      await page.setViewportSize({ width: w, height: 900 })
      await page.waitForTimeout(300)
      await noHorizontalScroll(`width ${w}`)
      await page.screenshot({ path: `${output}/projects-${w}.png` })
    }
    await page.setViewportSize({ width: 1280, height: 900 })
    at('no search results')
    await search().fill('不存在的名字')
    await page.getByText('没有名称包含「不存在的名字」的项目').waitFor()
    await page.getByRole('button', { name: '清除搜索' }).click()
    await loaded(30)
    assert.equal(await search().inputValue(), '')
    assert.doesNotMatch(page.url(), /q=/)
    at('no projects')
    await fresh({ projects: [] })
    await open()
    await page.getByText('还没有项目', { exact: true }).waitFor()
    at('no archived projects')
    await page.getByRole('group', { name: '项目状态' }).getByRole('button', { name: '已归档' }).click()
    await page.getByText('还没有归档的项目。归档后项目会留在这里，随时可以恢复。').waitFor()
    await page.screenshot({ path: `${output}/projects-empty-archived.png` })
  })

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify(results, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!only && Object.keys(results).length !== 4)) process.exitCode = 1
