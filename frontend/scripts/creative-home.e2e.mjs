// Browser regression for the creative home (V6-061 sidebar and routes, V6-062
// one-sentence start, V6-063 continue cards and covers, V6-029 first-send
// notice). All API
// traffic is mocked; each AC runs in a fresh context.
// Run against the Vite dev server: node scripts/creative-home.e2e.mjs.
import assert from 'node:assert/strict'
import { mkdirSync, readFileSync } from 'node:fs'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const output = process.env.CREATIVE_QA_OUTPUT_DIR ?? '/tmp/creative-home-qa'
mkdirSync(output, { recursive: true })
// 首次外发说明的版本跟着源码走，升级版本时不用改这里。
const noticeVersion = /egressNoticeVersion = '([^']+)'/.exec(readFileSync(new URL('../src/creative-canvas/editor/agentState.ts', import.meta.url), 'utf8'))[1]
const browser = await chromium.launch({ headless: true })
const errors = []
const pixel = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mN8z8DwHwAFhAJ/wlseKgAAAABJRU5ErkJggg=='
let context, page, projectReads, ticketReads
const project = (n, name, extra = {}) => ({
  id: `p${n}`,
  name,
  default_canvas_id: `c${n}`,
  revision: '1',
  archived: false,
  updated_at: '2026-10-01T00:00:00Z',
  edited_at: `2026-10-0${9 - n}T00:00:00Z`,
  running: false,
  waiting: false,
  cover: { nodes: [], edges: [], more: false },
  ...extra,
})
const node = (id, kind, x, y, width, height, revision) => ({ id, kind, x, y, width, height, ...(revision ? { content_revision_id: revision } : {}) })
let projects, counts, server
// V6-062 的服务端：建项目、建对话、授权、建运行都按 Idempotency-Key 回放回执，
// 只有第一次受理才真正建出东西；fault 控制每一步的故障。
const catalog = (skills = true) => ({
  catalog_version: '1',
  skill_catalog_revision: '1',
  limits_version: '1',
  policy_version: '1',
  limits: { attachments: 10, image_preview_count: 8 },
  tools: [],
  models: [
    { model_key: 'off', display_name: '停用模型', available: false, unavailable_reason: '维护中', capability: { image_input: true } },
    { model_key: 'vision', display_name: '识图模型', available: true, capability: { image_input: true } },
    { model_key: 'plain', display_name: '纯文本模型', available: true, capability: { image_input: false } },
  ],
  skills: skills
    ? [{ skill_id: 'sk', skill_version_id: 'skv', origin: 'platform', slug: 'reference-direction', display_name: '参考整理与创作方向', description: '检索参考、给出创作方向', available: true }]
    : [],
})
const freshServer = () => ({
  fault: {},
  posts: { projects: [], conversations: [], consents: [], runs: [], uploads: [] },
  receipts: new Map(),
  created: { projects: 0, conversations: 0, consents: 0, runs: 0 },
  assets: [],
  skills: true,
  uploadGate: null,
  uploads: new Map(),
})
const fixture = () => {
  counts = { running: 2, waiting: 2 }
  projects = [
    // 两张图和一段文字，一条连线：缩影按 600×400 的边界框等比缩放。
    project(1, '九月，向海而行', {
      running: true,
      cover: {
        nodes: [node('a', 'image', 0, 0, 200, 200, 'rev-a'), node('b', 'image', 400, 0, 200, 200, 'rev-b'), node('t', 'text', 200, 300, 200, 100)],
        edges: [{ source: 'a', target: 'b' }],
        more: false,
      },
    }),
    project(2, '雪山婚纱 · 二稿', { waiting: true, cover: { nodes: [node('t', 'text', 0, 0, 300, 220), node('e', 'empty', 400, 0, 300, 300)], edges: [], more: false } }),
    // 分组里 8 张图，节点超过抽样上限：最多取 6 张图，右下角写「部分节点」。
    project(3, '品牌 Lookbook · 春季，名字很长很长很长很长很长很长', {
      running: true,
      waiting: true,
      cover: {
        nodes: [...Array.from({ length: 8 }, (_, i) => node(`m${i}`, 'image', 40 + (i % 4) * 220, 64 + Math.floor(i / 4) * 220, 200, 200, `rev-m${i}`)), node('g', 'group', 0, 0, 960, 520)],
        edges: [],
        more: true,
      },
    }),
    project(4, '毕业照 · 光影练习'),
    project(5, '未命名项目'),
  ]
}
// 默认已看过首次外发说明，其余用例不被弹窗挡住；只有 029 的用例从没看过开始。
async function fresh({ width = 1280, height = 800, init, clock = false, notice = noticeVersion } = {}) {
  await context?.close()
  context = await browser.newContext({ viewport: { width, height } })
  projectReads = []
  ticketReads = []
  fixture()
  server = freshServer()
  page = await context.newPage()
  if (clock) await page.clock.install()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
  if (notice) await page.addInitScript((version) => localStorage.setItem('creative-egress-notice:qa-home', version), notice)
  if (init) await page.addInitScript(init)
  // 开发模式的 StrictMode 会挂载两次、取消第一次请求，只数真正完成的读取。
  page.on('requestfinished', (request) => {
    const url = new URL(request.url())
    if (url.pathname.endsWith('/creative/projects')) projectReads.push(url.search)
  })
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    const json = (value, status = 200) => route.fulfill({ status, json: value })
    if (path.endsWith('/auth/refresh')) return json({ access_token: 'qa', token_type: 'Bearer', expires_in: 600 })
    if (path.endsWith('/me')) return json({ id: 'qa-home', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
    if (path.endsWith('/account/profile')) return json({ display_name: 'Samson', profile_revision: '1', avatar_revision: '0' })
    if (path.endsWith('/creative/media-access-tickets')) {
      ticketReads.push(route.request().postDataJSON().content_revision_id)
      return json({ url: pixel, expires_at: new Date(Date.now() + 3600000).toISOString(), mime: 'image/png', byte_size: 68 })
    }
    if (path.endsWith('/creative/projects') && route.request().method() === 'GET') {
      const limit = Number(url.searchParams.get('limit') ?? 50)
      const items = url.searchParams.get('archived') === 'true' ? [] : projects.slice(0, limit)
      return json({ items, next_cursor: '', activity_counts: counts })
    }
    if ((await startRoutes(route, url, json)) !== false) return
    return json({ error: { code: 'unavailable', message: 'not modelled' } }, 503)
  })
  await page.route('**/qa-storage/**', (route) => route.fulfill({ status: 200, body: '' }))
}
async function startRoutes(route, url, json) {
  const path = url.pathname.replace('/api/v1/creative', '')
  const method = route.request().method()
  const key = route.request().headers()['idempotency-key']
  const body = method === 'POST' ? route.request().postDataJSON() : null
  if (method === 'GET' && path === '/agent/catalog') return json(catalog(server.skills))
  if (method === 'GET' && path === '/media-capabilities')
    return json({ schema_version: 1, formats: [{ kind: 'image', mime: 'image/png', extensions: ['.png'] }], image_max_bytes: 10485760, av_max_bytes: 10485760, part_size: 5242880, batch_limit: 20 })
  if (method === 'GET' && path === '/assets') {
    server.assetQuery = url.search
    return json({ items: server.assets, next_cursor: '', total_count: server.assets.length, library_revision: '1' })
  }
  if (method === 'GET' && path.startsWith('/operations/')) {
    const receipt = server.receipts.get(path.slice('/operations/'.length))
    return receipt ? json({ response: receipt }) : json({ error: { code: 'not_found', message: 'none' } }, 404)
  }
  // 同一操作身份回放原回执，不再建一次。
  const replay = (list, make) => {
    list.push({ key, body })
    if (server.receipts.has(key)) return json(server.receipts.get(key), 201)
    const value = make()
    server.receipts.set(key, value)
    return value
  }
  if (method === 'POST' && path === '/projects') {
    if (server.fault.project500) {
      server.posts.projects.push({ key, body })
      server.fault.project500 = false
      return json({ error: { code: 'internal', message: '服务暂时不可用' } }, 500)
    }
    const made = replay(server.posts.projects, () => {
      const n = ++server.created.projects
      return { id: `pn${n}`, name: body.payload.name, default_canvas_id: `cn${n}`, revision: '1', archived: false, updated_at: '2026-10-11T00:00:00Z', edited_at: '2026-10-11T00:00:00Z', running: false, waiting: false, cover: { nodes: [], edges: [], more: false } }
    })
    return typeof made.then === 'function' ? made : json(made, 201)
  }
  if (method === 'POST' && /^\/canvases\/[^/]+\/conversations$/.test(path)) {
    const made = replay(server.posts.conversations, () => ({ id: `conv${++server.created.conversations}`, project_id: 'pn1', canvas_id: path.split('/')[2], title: '新对话', title_source: 'default', archived: false, revision: '1', updated_at: '2026-10-11T00:00:00Z' }))
    return typeof made.then === 'function' ? made : json(made, 201)
  }
  if (method === 'POST' && path.endsWith('/egress-consents')) {
    if (server.fault.consent503) {
      server.posts.consents.push({ key, body })
      server.fault.consent503 = false
      return json({ error: { code: 'unavailable', message: '服务暂时不可用' } }, 503)
    }
    const made = replay(server.posts.consents, () => ({ id: `consent${++server.created.consents}`, conversation_id: path.split('/')[2], approved_revisions: body.payload.selected_revision_ids.length }))
    return typeof made.then === 'function' ? made : json(made, 201)
  }
  if (method === 'POST' && path.endsWith('/runs')) {
    if (server.fault.run503) {
      server.posts.runs.push({ key, body })
      server.fault.run503 = false
      return json({ error: { code: 'unavailable', message: '服务暂时不可用' } }, 503)
    }
    if (server.fault.run422) {
      server.posts.runs.push({ key, body })
      server.fault.run422 = false
      return json({ error: { code: 'creative_model_unavailable', message: 'Model unavailable' } }, 422)
    }
    const made = replay(server.posts.runs, () => ({ id: `run${++server.created.runs}`, conversation_id: path.split('/')[2], canvas_id: 'cn1', trigger_message_id: 'm1', egress_consent_id: body.payload.egress_consent_id, model_key: body.payload.model_key, state: 'queued', settlement_state: 'pending', limits_version: '1', revision: '1', last_event_seq: '0', pruned_through_seq: '0', deadline_at: '2026-10-11T01:00:00Z', created_at: '2026-10-11T00:00:00Z' }))
    // 服务端已经受理，但响应在路上断了。
    if (server.fault.runLost) {
      server.fault.runLost = false
      return route.abort('failed')
    }
    return typeof made.then === 'function' ? made : json(made, 202)
  }
  // 暂存上传：建会话 → 上传分片 → 完成 → 轮询到 ready。
  if (method === 'POST' && path === '/uploads') {
    server.posts.uploads.push(body.payload)
    const id = `up${server.posts.uploads.length}`
    const fail = body.payload.file_name.startsWith('bad') && !server.uploads.has(`retried:${body.payload.file_name}`)
    if (fail) server.uploads.set(`retried:${body.payload.file_name}`, true)
    server.uploads.set(id, { state: 'uploading', fail, name: body.payload.file_name })
    return json(uploadView(id), 201)
  }
  const upload = /^\/uploads\/([^/]+)(\/[a-z-]+)?$/.exec(path)
  if (upload) {
    const [, id, action] = upload
    if (action === '/part-authorizations')
      return json({ upload_id: id, parts: [{ part_number: 1, url: `${base}/qa-storage/${id}`, method: 'PUT', headers: {} }] })
    if (action === '/complete') {
      if (server.uploadGate) await server.uploadGate.promise
      const item = server.uploads.get(id)
      item.state = item.fail ? 'failed' : 'ready'
      return json(uploadView(id))
    }
    if (action === '/cancel') {
      server.uploads.get(id).state = 'cancelled'
      return json(uploadView(id))
    }
    if (!action) return json(uploadView(id))
  }
  return false
}
function uploadView(id) {
  const item = server.uploads.get(id)
  return {
    id, state: item.state, io_phase: 'none', revision: '1', kind: 'image', mime: 'image/png', size: 68, file_name: item.name,
    part_size: 5242880, part_count: 1, uploaded_parts: [], target_kind: 'staged', expires_at: '2026-10-12T00:00:00Z',
    error_code: item.state === 'failed' ? 'verify_failed' : '', publication: null, binding: null,
    content_revision_id: item.state === 'ready' ? `ccrv_${id}` : null, created_at: '2026-10-11T00:00:00Z',
  }
}
const gate = () => {
  let release
  const promise = new Promise((resolve) => { release = resolve })
  return { promise, release }
}
const png = (name, seed = name) => ({
  name,
  mimeType: 'image/png',
  buffer: Buffer.concat([Buffer.from(pixel.split(',')[1], 'base64'), Buffer.from(seed)]),
})
const ask = () => page.getByRole('form', { name: '一句话开始' })
const prompt = () => page.getByLabel('创作想法')
const sendButton = () => page.getByRole('button', { name: '交给助手' })
const pick = async (label, option) => {
  await ask().getByRole('combobox', { name: label, exact: true }).click()
  await page.getByRole('listbox', { name: label, exact: true }).getByRole('option', { name: option }).click()
}
const refs = () => ask().getByRole('list', { name: '参考图' }).getByRole('listitem')
const keys = (list) => [...new Set(list.map((p) => p.key))]
// 进入新画布时助手面板已经打开（画布页读到 openAgent 标记）。
const openedWithAgent = async (canvas = 'cn1') => {
  await page.waitForURL(new RegExp(`/creative/canvases/${canvas}$`))
  await page.getByRole('complementary', { name: '创作助手' }).waitFor()
  return page.evaluate(() => history.state?.usr?.openAgent === true)
}
const side = () => page.getByRole('complementary', { name: '创意空间导航' })
const navLink = (name) => side().getByRole('link', { name, exact: true })
const recent = () => side().getByRole('navigation', { name: '最近打开的项目' }).getByRole('link')
const width = async () => Math.round((await side().boundingBox()).width)
const noHorizontalScroll = async (label) => {
  const sizes = await page.evaluate(() => {
    const main = document.querySelector('.cs-main')
    return {
      page: [document.documentElement.scrollWidth, document.documentElement.clientWidth],
      main: [main.scrollWidth, main.clientWidth],
    }
  })
  assert.ok(sizes.page[0] <= sizes.page[1], `${label}: page scrolls sideways ${sizes.page}`)
  assert.ok(sizes.main[0] <= sizes.main[1], `${label}: main scrolls sideways ${sizes.main}`)
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
  // V6-061 AC-01 展开 216、折叠 56，图标位置不变；偏好记在本机，换页面保持；本机存储不可用时默认展开。
  await record('061-AC-01', async () => {
    await fresh()
    await page.goto(`${base}/creative`)
    await recent().first().waitFor()
    at('expanded')
    assert.equal(await width(), 216)
    await page.screenshot({ path: `${output}/side-expanded-1280.png` })
    const icons = async () =>
      page.evaluate(() => [...document.querySelectorAll('.cs-side-nav a svg, .cs-side-recent .cs-side-thumb, .cs-side-link svg')].map((el) => Math.round(el.getBoundingClientRect().x)))
    const before = await icons()
    at('collapse')
    await side().getByRole('button', { name: '收起侧栏' }).click()
    await page.waitForTimeout(300)
    assert.equal(await width(), 56)
    assert.deepEqual(await icons(), before, 'icons keep their x position')
    assert.equal(await page.evaluate(() => localStorage.getItem('creative-side-collapsed')), '1')
    await page.screenshot({ path: `${output}/side-collapsed-1280.png` })
    at('other page keeps it')
    await navLink('项目').click()
    await page.waitForURL('**/creative/projects')
    await page.getByRole('heading', { name: '项目', exact: true }).waitFor()
    assert.equal(await width(), 56)
    at('expand again')
    await side().getByRole('button', { name: '展开侧栏' }).click()
    await page.waitForTimeout(300)
    assert.equal(await width(), 216)
    assert.equal(await page.evaluate(() => localStorage.getItem('creative-side-collapsed')), '0')
    at('storage throws')
    await fresh({
      init: () => {
        Storage.prototype.getItem = () => { throw new DOMException('denied', 'SecurityError') }
        Storage.prototype.setItem = () => { throw new DOMException('denied', 'SecurityError') }
      },
    })
    await page.goto(`${base}/creative`)
    await recent().first().waitFor()
    assert.equal(await width(), 216, 'defaults to expanded')
    await side().getByRole('button', { name: '收起侧栏' }).click()
    await page.waitForTimeout(300)
    assert.equal(await width(), 56, 'still toggles without storage')
  })

  // V6-061 AC-02 折叠时悬停或聚焦条目，右侧显示名称提示，不被侧栏裁切。
  await record('061-AC-02', async () => {
    await fresh({ init: () => localStorage.setItem('creative-side-collapsed', '1') })
    await page.goto(`${base}/creative`)
    await recent().first().waitFor()
    const tip = page.getByRole('tooltip')
    const outside = async (label, text) => {
      await tip.waitFor()
      assert.equal(await tip.innerText(), text, `${label}: tip text`)
      const t = await tip.boundingBox()
      const s = await side().boundingBox()
      assert.ok(t.x >= s.x + s.width, `${label}: tip starts right of the sidebar (${t.x} vs ${s.x + s.width})`)
      assert.ok(t.width > 20, `${label}: tip is not clipped`)
    }
    at('hover nav')
    await navLink('项目').hover()
    await outside('hover', '项目')
    at('hover recent')
    await recent().nth(2).hover()
    await outside('recent', projects[2].name)
    at('leave')
    await page.mouse.move(800, 400)
    await tip.waitFor({ state: 'detached' })
    at('focus')
    await navLink('资产库').focus()
    await outside('focus', '资产库')
    await page.screenshot({ path: `${output}/side-tip.png` })
    at('expanded has no tip')
    await page.mouse.move(800, 400)
    // 点标题让焦点离开侧栏；页面中部现在是输入框，不能随手点空白处。
    await page.getByRole('heading', { name: '这次想拍点什么？' }).click()
    await side().getByRole('button', { name: '展开侧栏' }).click()
    await navLink('项目').hover()
    await page.waitForTimeout(200)
    assert.equal(await tip.count(), 0)
  })

  // V6-061 AC-03 761–1100 自动折叠、隐藏切换按钮、不改偏好；≤760 顶栏；各宽度无横向滚动。
  await record('061-AC-03', async () => {
    for (const w of [1280, 1000, 700, 390]) {
      at(`width ${w}`)
      await fresh({ width: w, init: () => localStorage.setItem('creative-side-collapsed', '0') })
      for (const route of ['/creative', '/creative/projects']) {
        await page.goto(`${base}${route}`)
        await page.getByRole('link', { name: '首页', exact: true }).waitFor()
        await page.waitForTimeout(300)
        await noHorizontalScroll(`${w} ${route}`)
      }
      const toggle = side().getByRole('button', { name: /侧栏/ })
      if (w === 1280) {
        assert.equal(await width(), 216)
        assert.ok(await toggle.isVisible())
      } else if (w === 1000) {
        assert.equal(await width(), 56, 'forced collapse')
        assert.equal(await toggle.isVisible(), false, 'toggle hidden')
        await page.screenshot({ path: `${output}/side-1000.png` })
      } else {
        const box = await side().boundingBox()
        assert.ok(box.height < 64 && Math.round(box.width) === w, `top bar ${JSON.stringify(box)}`)
        assert.equal(await toggle.isVisible(), false, 'toggle hidden')
        for (const name of ['创意空间', '首页', '项目', '资产库']) assert.ok(await side().getByRole('link', { name, exact: true }).isVisible(), `${name} visible`)
        assert.equal(await side().getByRole('link', { name: '返回 CRM' }).isVisible(), false, 'foot hidden')
        assert.equal(await recent().first().isVisible(), false, 'recent hidden')
        await page.screenshot({ path: `${output}/side-${w}.png` })
      }
      assert.equal(await page.evaluate(() => localStorage.getItem('creative-side-collapsed')), '0', 'preference untouched')
    }
  })

  // V6-061 AC-04 当前页高亮带 aria-current；最近打开 5 个，按最近编辑，状态点，点击进入画布。
  await record('061-AC-04', async () => {
    await fresh()
    await page.goto(`${base}/creative`)
    await recent().first().waitFor()
    at('current page')
    assert.equal(await navLink('首页').getAttribute('aria-current'), 'page')
    assert.equal(await navLink('项目').getAttribute('aria-current'), null)
    at('one shared read')
    assert.deepEqual(projectReads, ['?limit=5'], 'home and sidebar share one read')
    at('order and links')
    assert.equal(await recent().count(), 5)
    for (const [i, p] of projects.entries()) {
      assert.equal(await recent().nth(i).getAttribute('data-tip'), p.name)
      assert.equal(await recent().nth(i).getAttribute('href'), `/creative/canvases/${p.default_canvas_id}`)
    }
    at('dots')
    const dots = await page.evaluate(() =>
      [...document.querySelectorAll('.cs-side-recent a')].map((a) => {
        const dot = a.querySelector('.cs-side-dot')
        return dot ? [dot.getAttribute('aria-label'), getComputedStyle(dot).backgroundColor] : null
      }),
    )
    const amber = 'rgb(226, 164, 83)'
    const white = 'rgb(244, 244, 245)'
    assert.deepEqual(dots, [['运行中', amber], ['等你确认', white], ['等你确认', white], null, null])
    at('thumbnail')
    await page.locator('.cs-side-recent a').first().locator('img').waitFor()
    assert.equal(await page.locator('.cs-side-recent img').count(), 2, 'only projects with an image get one')
    assert.equal(await page.locator('.cs-side-recent a').nth(3).locator('img').count(), 0)
    at('projects page')
    await navLink('项目').click()
    await page.waitForURL('**/creative/projects')
    await page.getByRole('heading', { name: '项目', exact: true }).waitFor()
    await recent().first().waitFor()
    assert.equal(await navLink('项目').getAttribute('aria-current'), 'page')
    assert.equal(await navLink('首页').getAttribute('aria-current'), null)
    at('open project')
    await recent().nth(3).click()
    await page.waitForURL('**/creative/canvases/c4')
    at('archived redirect')
    await page.goto(`${base}/creative?view=archived`)
    await page.waitForURL('**/creative/projects?view=archived')
    assert.equal(await page.getByRole('button', { name: '已归档' }).getAttribute('aria-pressed'), 'true')
  })

  // V6-061 AC-05 导航增加条目时最近打开整体下移，放不下只在自己的区域里滚动；底部是返回 CRM 和账号区。
  await record('061-AC-05', async () => {
    await fresh({ height: 720 })
    await page.goto(`${base}/creative`)
    await recent().first().waitFor()
    at('foot')
    const foot = side().locator('.cs-side-foot')
    assert.ok(await foot.getByRole('link', { name: '返回 CRM' }).isVisible())
    assert.match(await foot.locator('.cs-side-account').innerText(), /S\s*Samson/)
    const recentTop = async () => (await side().locator('.cs-side-recent').boundingBox()).y
    const before = await recentTop()
    at('twelve nav items')
    await page.evaluate(() => {
      const nav = document.querySelector('.cs-side-nav')
      const sample = nav.lastElementChild
      for (let i = 0; i < 9; i++) nav.append(sample.cloneNode(true))
    })
    const navBox = await side().locator('.cs-side-nav').boundingBox()
    assert.ok((await recentTop()) > before, 'recent moves down')
    assert.ok((await recentTop()) >= navBox.y + navBox.height, 'recent stays below the nav')
    const sizes = await page.evaluate(() => {
      const list = document.querySelector('.cs-side-recent nav')
      const aside = document.querySelector('.cs-side')
      return { list: [list.scrollHeight, list.clientHeight], aside: [aside.scrollHeight, aside.clientHeight] }
    })
    assert.ok(sizes.list[0] > sizes.list[1], `recent scrolls inside itself ${sizes.list}`)
    assert.ok(sizes.aside[0] <= sizes.aside[1], `sidebar does not overflow ${sizes.aside}`)
    const footBox = await foot.boundingBox()
    assert.ok(footBox.y + footBox.height <= 720, 'foot stays on screen')
    await page.screenshot({ path: `${output}/side-crowded.png` })
  })

  const card = (name) => page.getByRole('link', { name: `打开项目：${name}` })
  const cover = (name) => card(name).locator('.pc-cover')
  const box = async (locator) => locator.boundingBox()

  // V6-062 AC-01 Enter 发送（Shift+Enter 换行），建好项目并带着打开助手的标记进入画布；
  // 同一次提交无论第 3 步失败重试、响应丢失、刷新找回还是被拒后改了再发，都不重复建项目。
  await record('062-AC-01', async () => {
    await fresh()
    server.fault.run503 = true
    await page.goto(`${base}/creative`)
    await prompt().waitFor()
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    at('shift+enter is a newline')
    await prompt().fill('海边婚纱外景，傍晚逆光')
    await prompt().press('Shift+Enter')
    await prompt().pressSequentially('想要安静克制')
    assert.equal(await prompt().inputValue(), '海边婚纱外景，傍晚逆光\n想要安静克制')
    assert.equal(server.posts.projects.length, 0)
    at('step 3 fails once')
    await prompt().press('Enter')
    const notice = page.getByText('消息还没确认送达')
    await notice.waitFor()
    assert.equal(await prompt().getAttribute('readonly'), '', 'input locked while the outcome is unknown')
    assert.equal(await page.evaluate(() => JSON.parse(sessionStorage.getItem('creative-home-start:qa-home')).name), '海边婚纱外景，傍晚逆光')
    await page.screenshot({ path: `${output}/start-unknown.png` })
    await page.getByRole('button', { name: '重试发送' }).click()
    assert.equal(await openedWithAgent(), true, 'canvas opens with the assistant')
    assert.equal(server.posts.projects.length, 1, 'one project')
    assert.equal(server.posts.conversations.length, 1)
    assert.equal(server.posts.consents.length, 1)
    assert.equal(server.posts.runs.length, 2)
    assert.equal(keys(server.posts.runs).length, 1, 'retry keeps the run identity')
    assert.equal(server.created.runs, 1)
    assert.equal(await page.evaluate(() => sessionStorage.getItem('creative-home-start:qa-home')), null, 'record cleared after acceptance')
    assert.equal(await page.evaluate(() => sessionStorage.getItem('creative-agent-pending:qa-home:cn1')), null, 'chain cleared after acceptance')

    at('accepted but the response was lost')
    await fresh()
    server.fault.runLost = true
    await page.goto(`${base}/creative`)
    await prompt().fill('响应丢失的一句话')
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    await prompt().press('Enter')
    await page.getByText('消息还没确认送达').waitFor()
    assert.equal(server.created.runs, 1, 'server accepted the run')
    assert.equal(await prompt().getAttribute('readonly'), '')
    at('refresh finds it again')
    await page.reload()
    await page.getByText('消息还没确认送达').waitFor()
    assert.equal(await prompt().inputValue(), '响应丢失的一句话')
    assert.equal(await prompt().getAttribute('readonly'), '', 'still locked after refresh')
    assert.equal(await sendButton().isDisabled(), true)
    await page.getByRole('button', { name: '重试发送' }).click()
    assert.equal(await openedWithAgent(), true)
    assert.equal(server.posts.projects.length, 1)
    assert.equal(server.posts.runs.length, 2)
    assert.equal(keys(server.posts.runs).length, 1)
    assert.equal(server.created.runs, 1, 'the replay did not start a second run')

    at('refused, edited and sent again')
    await fresh()
    server.fault.run422 = true
    await page.goto(`${base}/creative`)
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    await prompt().fill('第一次的说法')
    await prompt().press('Enter')
    await page.getByRole('alert').filter({ hasText: '当前模型暂无可用的服务渠道，请换一个模型' }).waitFor()
    await page.getByText('项目「第一次的说法」已建好，消息还没发出。').waitFor()
    assert.equal(await prompt().getAttribute('readonly'), null, 'unlocked after a definite refusal')
    assert.equal(await prompt().inputValue(), '第一次的说法')
    await page.reload()
    await page.getByText('项目「第一次的说法」已建好，消息还没发出。').waitFor()
    await prompt().fill('改过的说法')
    await prompt().press('Enter')
    assert.equal(await openedWithAgent(), true)
    assert.equal(server.posts.projects.length, 1, 'no second project')
    assert.equal(server.posts.conversations.length, 1, 'the conversation is reused')
    assert.equal(server.posts.consents.length, 2)
    assert.equal(keys(server.posts.consents).length, 2, 'a new message gets new identities')
    assert.deepEqual(server.posts.runs.at(-1).body.payload.instruction.instruction_segments, [{ type: 'text', text: '改过的说法' }])
  })

  // V6-062 AC-02 参考图、Skill、模型随首条消息进入助手；默认模型是第一个可用的；授权只覆盖这条消息的参考图。
  await record('062-AC-02', async () => {
    await fresh()
    server.assets = [
      { id: 'a1', title: '海边逆光', kind: 'image', content_revision_id: 'ccrv_lib1', unavailable: false },
    ]
    await page.goto(`${base}/creative`)
    at('default model')
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    at('library and upload references')
    await page.getByRole('button', { name: '加为参考：海边逆光' }).click()
    await ask().getByRole('combobox', { name: '添加参考图' }).click()
    const chooser = page.waitForEvent('filechooser')
    await page.getByRole('option', { name: '上传图片' }).click()
    await (await chooser).setFiles(png('a.png'))
    await refs().nth(1).and(page.locator('[data-state="ready"]')).waitFor()
    assert.equal(server.posts.uploads[0].target.kind, 'staged', 'uploads are staged, not stored in the library')
    await pick('Skill', '参考整理与创作方向')
    await prompt().fill('按这两张图出方向')
    await prompt().press('Enter')
    await openedWithAgent()
    const consent = server.posts.consents[0].body.payload
    assert.deepEqual(consent, {
      purpose: 'creative_assistance',
      mode: 'selected_revisions',
      data_classes: ['text', 'canvas_metadata', 'image_preview'],
      selected_revision_ids: ['ccrv_lib1', 'ccrv_up1'],
    })
    const run = server.posts.runs[0].body.payload
    assert.equal(run.model_key, 'vision')
    assert.equal(run.egress_consent_id, 'consent1')
    assert.deepEqual(run.instruction.instruction_segments, [
      { type: 'skill_ref', skill_id: 'sk', skill_version_id: 'skv' },
      { type: 'text', text: '按这两张图出方向' },
      { type: 'content_ref', content_revision_id: 'ccrv_lib1' },
      { type: 'content_ref', content_revision_id: 'ccrv_up1' },
    ])
  })

  // V6-062 AC-03 上传完成前不能发送；失败逐项说明可重试；可移除；重复拦截；最多 6 张；模型不识图时提示。
  await record('062-AC-03', async () => {
    await fresh()
    await page.goto(`${base}/creative`)
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    await prompt().fill('参考图规则')
    const upload = async (...files) => {
      await ask().getByRole('combobox', { name: '添加参考图' }).click()
      const chooser = page.waitForEvent('filechooser')
      await page.getByRole('option', { name: '上传图片' }).click()
      await (await chooser).setFiles(files)
    }
    at('uploading blocks sending')
    server.uploadGate = gate()
    await upload(png('one.png'))
    await refs().first().and(page.locator('[data-state="uploading"]')).waitFor()
    await page.getByText('参考图上传完成后才能发送').waitFor()
    assert.equal(await sendButton().isDisabled(), true)
    server.uploadGate.release()
    server.uploadGate = null
    await refs().first().and(page.locator('[data-state="ready"]')).waitFor()
    assert.equal(await sendButton().isDisabled(), false)
    at('duplicate is refused')
    await upload(png('one-again.png', 'one.png'))
    await page.getByText('这张图已经添加过').waitFor()
    assert.equal(await refs().count(), 1)
    at('failure explained per item, retry works')
    await upload(png('bad.png'))
    await page.getByText('bad.png：媒体校验失败').waitFor()
    await page.getByText('有参考图上传失败，重试或移除后再发送').waitFor()
    assert.equal(await sendButton().isDisabled(), true)
    await page.getByRole('button', { name: '重试上传：bad.png' }).click()
    await refs().nth(1).and(page.locator('[data-state="ready"]')).waitFor()
    at('drag and drop')
    const drop = await page.evaluateHandle(async (src) => {
      const data = new DataTransfer()
      const bytes = await (await fetch(src)).blob()
      data.items.add(new File([bytes, 'dropped'], 'dropped.png', { type: 'image/png' }))
      return data
    }, pixel)
    await ask().dispatchEvent('dragover', { dataTransfer: drop })
    await ask().dispatchEvent('drop', { dataTransfer: drop })
    await refs().nth(2).and(page.locator('[data-state="ready"]')).waitFor()
    at('limit of six')
    await upload(png('p4.png'), png('p5.png'), png('p6.png'), png('p7.png'))
    await page.getByText('最多 6 张').waitFor()
    assert.equal(await refs().count(), 3, 'the whole batch is refused')
    await upload(png('p4.png'), png('p5.png'), png('p6.png'))
    await refs().nth(5).and(page.locator('[data-state="ready"]')).waitFor()
    await upload(png('p7.png'))
    await page.getByText('最多 6 张').waitFor()
    assert.equal(await refs().count(), 6)
    await page.screenshot({ path: `${output}/start-references.png` })
    at('remove')
    await page.getByRole('button', { name: '移除参考图：p6.png' }).click()
    assert.equal(await refs().count(), 5)
    at('model without image input')
    await pick('模型', '纯文本模型')
    await page.getByText('所选模型不支持图片，换一个识图模型或移除图片').waitFor()
    assert.equal(await sendButton().isDisabled(), true)
    assert.equal(server.posts.projects.length, 0, 'nothing was sent')
    assert.ok(server.posts.uploads.every((u) => u.target.kind === 'staged'))
  })

  // V6-062 AC-04 点场景 chip 填入示例文字并切换 Skill，不发送；目录里没有这个 Skill 时只填文字。
  await record('062-AC-04', async () => {
    await fresh()
    await page.goto(`${base}/creative`)
    await ask().getByRole('combobox', { name: 'Skill', exact: true }).filter({ hasText: '普通对话' }).waitFor()
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    await page.getByRole('button', { name: '参考整理与创作方向' }).click()
    assert.match(await prompt().inputValue(), /^春季品牌 Lookbook/)
    await ask().getByRole('combobox', { name: 'Skill', exact: true }).filter({ hasText: '参考整理与创作方向' }).waitFor()
    await page.waitForTimeout(200)
    assert.equal(server.posts.projects.length, 0, 'a chip never sends')
    at('skill missing from the catalog')
    await fresh()
    server.skills = false
    await page.goto(`${base}/creative`)
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    await page.getByRole('button', { name: '参考整理与创作方向' }).click()
    assert.match(await prompt().inputValue(), /^春季品牌 Lookbook/)
    await ask().getByRole('combobox', { name: 'Skill', exact: true }).filter({ hasText: '普通对话' }).waitFor()
  })

  // V6-062 AC-05 「空白画布」建空项目进入画布；建项目或某一步失败时停在首页说明原因，文字和参考图保留。
  await record('062-AC-05', async () => {
    await fresh()
    await page.goto(`${base}/creative`)
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    at('blank canvas')
    await page.getByRole('button', { name: '空白画布' }).click()
    await page.waitForURL(/\/creative\/canvases\/cn1$/)
    await page.getByRole('button', { name: '打开创作助手' }).waitFor()
    assert.equal(await page.getByRole('complementary', { name: '创作助手' }).count(), 0, 'a blank canvas opens without the assistant')
    assert.deepEqual(server.posts.projects.map((p) => p.body.payload), [{ name: '未命名项目' }])
    assert.equal(server.posts.conversations.length, 0)

    at('creating the project fails')
    await fresh()
    server.fault.project500 = true
    server.assets = [{ id: 'a1', title: '海边逆光', kind: 'image', content_revision_id: 'ccrv_lib1', unavailable: false }]
    await page.goto(`${base}/creative`)
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    await page.getByRole('button', { name: '加为参考：海边逆光' }).click()
    await prompt().fill('建项目失败也要留着')
    await prompt().press('Enter')
    await page.getByRole('button', { name: '查询并恢复原保存' }).waitFor()
    assert.ok(page.url().endsWith('/creative'), 'stays on the home page')
    assert.equal(await prompt().inputValue(), '建项目失败也要留着')
    assert.equal(await refs().count(), 1)
    await page.screenshot({ path: `${output}/start-project-failed.png` })
    await page.getByRole('button', { name: '查询并恢复原保存' }).click()
    assert.equal(await openedWithAgent(), true)
    assert.equal(server.created.projects, 1, 'recovery created exactly one project')
    assert.equal(keys(server.posts.projects).length, 1, 'recovery replays the same operation')

    at('step 2 returns 503')
    await fresh()
    server.fault.consent503 = true
    server.assets = [{ id: 'a1', title: '海边逆光', kind: 'image', content_revision_id: 'ccrv_lib1', unavailable: false }]
    await page.goto(`${base}/creative`)
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    await page.getByRole('button', { name: '加为参考：海边逆光' }).click()
    await prompt().fill('授权失败也要留着')
    await prompt().press('Enter')
    await page.getByText('项目「授权失败也要留着」已建好，消息还没确认送达。').waitFor()
    assert.ok(page.url().endsWith('/creative'))
    assert.equal(await prompt().inputValue(), '授权失败也要留着')
    assert.equal(await refs().count(), 1)
    await page.getByRole('button', { name: '重试发送' }).click()
    assert.equal(await openedWithAgent(), true)
    assert.equal(keys(server.posts.consents).length, 1)
    assert.equal(server.created.consents, 1)
    assert.equal(server.posts.projects.length, 1)
  })

  // V6-062 AC-06 「最近添加」显示资产库最近的图片，「+ 参考」放进输入框；资产库为空时不出现这一行。
  await record('029-AC-01', async () => {
    await fresh({ notice: null })
    await page.goto(`${base}/creative`)
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    const dialog = page.getByRole('alertdialog', { name: '发送前说明' })
    const seen = () => page.evaluate(() => localStorage.getItem('creative-egress-notice:qa-home'))
    at('first send explains before anything is created')
    await prompt().fill('海边婚纱外景')
    await prompt().press('Enter')
    await dialog.waitFor()
    await dialog.getByText('平台签约的模型服务商').waitFor()
    // 名单链接到公开的隐私说明页，新标签页打开，草稿和弹窗都还在。
    const privacy = dialog.getByRole('link', { name: '隐私说明' })
    assert.equal(await privacy.getAttribute('href'), '/privacy')
    assert.equal(await privacy.getAttribute('target'), '_blank')
    // 焦点先落在「取消」，不是正文里的链接：再按一次 Enter 只会关掉说明。
    await page.waitForFunction(() => document.activeElement?.textContent === '取消')
    await dialog.getByText('资产库里没有被引用的素材不会发送', { exact: false }).waitFor()
    assert.equal(server.posts.projects.length, 0, 'no project before the notice is acknowledged')
    await page.evaluate(() => Promise.all(document.querySelector(".overlay.open").getAnimations({ subtree: true }).map((a) => a.finished)))
    await page.screenshot({ path: `${output}/egress-notice.png` })
    at('cancel keeps the draft and sends nothing')
    await dialog.getByRole('button', { name: '取消' }).click()
    await dialog.waitFor({ state: 'detached' })
    assert.equal(await prompt().inputValue(), '海边婚纱外景')
    assert.equal(await prompt().evaluate((el) => el === document.activeElement), true, 'focus returns to the input')
    assert.equal(await seen(), null)
    at('escape is a cancel too, and the notice comes back')
    await sendButton().click()
    await dialog.waitFor()
    await page.keyboard.press('Escape')
    await dialog.waitFor({ state: 'detached' })
    assert.equal(server.posts.projects.length, 0)
    assert.equal(await prompt().inputValue(), '海边婚纱外景')
    at('manual work still goes on: a blank canvas needs no notice')
    await page.getByRole('button', { name: '空白画布' }).click()
    await page.waitForURL(/\/creative\/canvases\//)
    assert.equal(await dialog.count(), 0)
    assert.equal(server.posts.projects.length, 1)
    await page.goto(`${base}/creative`)
    at('acknowledge then send')
    await prompt().fill('海边婚纱外景')
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    await sendButton().click()
    await dialog.getByRole('button', { name: '知道了，发送' }).click()
    assert.equal(await openedWithAgent('cn2'), true)
    assert.equal(await seen(), noticeVersion)
    assert.equal(server.created.runs, 1)
    at('the same account is not asked again')
    await page.goto(`${base}/creative`)
    await prompt().fill('第二句话')
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    await sendButton().click()
    await page.waitForURL(/\/creative\/canvases\//)
    assert.equal(await dialog.count(), 0)
    assert.equal(server.created.runs, 2)
  })

  await record('029-privacy', async () => {
    // 隐私说明是公开页面：不读会话也能打开，不点名具体服务商，手机宽度不横向滚动。
    await fresh({ width: 390, height: 800 })
    const reads = []
    page.on('request', (request) => { if (request.url().includes('/api/v1/')) reads.push(request.url()) })
    await page.goto(`${base}/privacy`)
    await page.getByRole('heading', { name: '你的内容会去哪里' }).waitFor()
    await page.getByRole('region', { name: '谁会处理这些内容' }).getByText('平台签约的模型服务商', { exact: false }).waitFor()
    const text = await page.locator('main').innerText()
    for (const vendor of ['DeepSeek', 'Krill', 'AiCodeWith', 'OpenAI'])
      assert.ok(!text.toLowerCase().includes(vendor.toLowerCase()), `the notice names ${vendor}`)
    await page.getByText('至少保留 3 天', { exact: false }).waitFor()
    assert.deepEqual(reads, [], 'the notice needs no session')
    const sizes = await page.evaluate(() => [document.documentElement.scrollWidth, document.documentElement.clientWidth])
    assert.ok(sizes[0] <= sizes[1], `page scrolls sideways ${sizes}`)
    await page.screenshot({ path: `${output}/privacy-390.png`, fullPage: true })
  })

  await record('029-AC-03', async () => {
    // 看过的是旧版说明：再问一次。
    await fresh({ notice: 'egress-2000-01-1' })
    await page.goto(`${base}/creative`)
    await prompt().fill('说明升级之后')
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    await prompt().press('Enter')
    const dialog = page.getByRole('alertdialog', { name: '发送前说明' })
    await dialog.waitFor()
    assert.equal(server.posts.projects.length, 0)
    await dialog.getByRole('button', { name: '知道了，发送' }).click()
    assert.equal(await openedWithAgent(), true)
    assert.equal(await page.evaluate(() => localStorage.getItem('creative-egress-notice:qa-home')), noticeVersion)
  })

  await record('062-AC-06', async () => {
    await fresh()
    server.assets = Array.from({ length: 3 }, (_, i) => ({ id: `a${i}`, title: `素材 ${i}`, kind: 'image', content_revision_id: `ccrv_lib${i}`, unavailable: false }))
    await page.goto(`${base}/creative`)
    const row = page.getByRole('heading', { name: '资产库 · 最近添加' })
    await row.waitFor()
    assert.equal(new URLSearchParams(server.assetQuery).get('sort'), 'recent')
    assert.equal(new URLSearchParams(server.assetQuery).get('kind'), 'image')
    assert.equal(new URLSearchParams(server.assetQuery).get('limit'), '12')
    await page.locator('.hp-asset img').nth(2).waitFor()
    const heights = await page.locator('.hp-asset').evaluateAll((items) => items.map((el) => Math.round(el.getBoundingClientRect().height)))
    assert.deepEqual(heights, [132, 132, 132], 'one row of equal height')
    await page.locator('.hp-asset').first().hover()
    await page.getByRole('button', { name: '加为参考：素材 0' }).click()
    await refs().first().waitFor()
    assert.equal(server.posts.uploads.length, 0, 'library references are not uploaded')
    await page.getByRole('button', { name: '加为参考：素材 0' }).click()
    await page.getByText('这张图已经添加过').waitFor()
    await page.screenshot({ path: `${output}/home-start-1280.png` })
    at('empty library hides the row')
    await fresh()
    await page.goto(`${base}/creative`)
    await prompt().waitFor()
    await page.waitForTimeout(400)
    assert.equal(await page.getByRole('heading', { name: '资产库 · 最近添加' }).count(), 0)
  })

  // V6-063 AC-01 首页 4 个最近编辑的项目；封面按画布比例缩影，图片用真实缩略图，空项目写「空白画布」。
  await record('063-AC-01', async () => {
    await fresh()
    await page.goto(`${base}/creative`)
    await card(projects[0].name).waitFor()
    at('four cards in order')
    const names = await page.locator('.hp-card strong').allInnerTexts()
    assert.deepEqual(names, projects.slice(0, 4).map((p) => p.name))
    at('proportions')
    await cover(projects[0].name).locator('img').nth(1).waitFor()
    const frame = await box(cover(projects[0].name))
    const [a, b, t] = await Promise.all(['a', 'b', 'text'].map((k) => box(cover(projects[0].name).locator(k === 'text' ? '.pc-text' : '.pc-image').nth(k === 'b' ? 1 : 0))))
    const scale = a.width / 200
    assert.ok(Math.abs(b.x - a.x - 400 * scale) < 1, 'b sits 400 canvas px right of a')
    assert.ok(Math.abs(t.y - a.y - 300 * scale) < 1, 'text sits 300 canvas px below a')
    assert.ok(Math.abs(t.height - 100 * scale) < 1, 'text keeps its height ratio')
    const left = a.x - frame.x
    const top = a.y - frame.y
    const right = frame.x + frame.width - (b.x + b.width)
    const bottom = frame.y + frame.height - (t.y + t.height)
    assert.ok(Math.abs(top - frame.height * 0.08) < 1 && Math.abs(bottom - top) < 1, `8% margin on the tight axis (${top}, ${bottom})`)
    assert.ok(Math.abs(left - right) < 1 && left >= frame.width * 0.08 - 1, 'centred on the loose axis')
    assert.equal(await cover(projects[0].name).locator('svg path').count(), 1, 'one edge')
    assert.equal(await cover(projects[0].name).locator('img').count(), 2, 'real thumbnails')
    at('text, empty, group, more')
    assert.equal(await cover(projects[1].name).locator('.pc-text i').count(), 4)
    assert.equal(await cover(projects[1].name).locator('.pc-empty').count(), 1)
    const p3 = cover(projects[2].name)
    assert.equal(await p3.locator('.pc-node').first().getAttribute('data-kind'), 'group', 'group drawn first, behind its children')
    await p3.locator('img').nth(5).waitFor()
    assert.equal(await p3.locator('img').count(), 6, 'at most six images per cover')
    assert.equal(ticketReads.filter((r) => r.startsWith('rev-m')).length, 6, 'at most six tickets per cover')
    assert.equal(await p3.getByText('部分节点').count(), 1)
    assert.equal(await cover(projects[0].name).getByText('部分节点').count(), 0)
    at('blank canvas')
    assert.equal(await cover(projects[3].name).innerText(), '空白画布')
    assert.equal(await cover(projects[3].name).locator('.pc-node').count(), 0)
    await page.screenshot({ path: `${output}/home-1280.png` })
    for (const w of [1000, 390]) {
      await page.setViewportSize({ width: w, height: 800 })
      await page.waitForTimeout(300)
      await noHorizontalScroll(`home ${w}`)
      await page.screenshot({ path: `${output}/home-${w}.png`, fullPage: true })
    }
  })

  // V6-063 AC-02 运行中琥珀点、等你确认白点，只有左上角标签，卡片不加琥珀描边；问候语省略数量为 0 的部分。
  await record('063-AC-02', async () => {
    await fresh()
    await page.goto(`${base}/creative`)
    await card(projects[0].name).waitFor()
    at('badges')
    const badges = await page.evaluate(() =>
      [...document.querySelectorAll('.hp-card')].map((c) => {
        const badge = c.querySelector('.pc-badge')
        return badge ? [badge.textContent, getComputedStyle(badge, '::before').backgroundColor] : null
      }),
    )
    const amber = 'rgb(226, 164, 83)'
    const white = 'rgb(244, 244, 245)'
    assert.deepEqual(badges, [['运行中', amber], ['等你确认', white], ['等你确认', white], null])
    at('no amber outline')
    const outlines = await page.evaluate(() =>
      [...document.querySelectorAll('.hp-card, .hp-card .pc-cover')].map((el) => {
        const style = getComputedStyle(el)
        return `${style.boxShadow} ${style.borderColor} ${style.outlineColor}`
      }),
    )
    assert.ok(outlines.every((o) => !o.includes('226, 164, 83')), 'cards carry no amber outline')
    at('greeting both')
    const hello = page.locator('.hp-hello')
    assert.match(await hello.innerText(), /^(夜深了|早上好|中午好|下午好|晚上好)，Samson · 2 个项目在运行，2 个等你确认$/)
    at('greeting waiting only')
    counts = { running: 0, waiting: 1 }
    await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')))
    await page.waitForFunction(() => document.querySelector('.hp-hello').textContent.endsWith('· 1 个等你确认'))
    assert.doesNotMatch(await hello.innerText(), /在运行/)
    at('greeting none')
    counts = { running: 0, waiting: 0 }
    await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')))
    await page.waitForFunction(() => !document.querySelector('.hp-hello').textContent.includes('·'))
    assert.match(await hello.innerText(), /^(夜深了|早上好|中午好|下午好|晚上好)，Samson$/)
  })

  // V6-063 AC-04 切回标签页、返回首页或轮询后状态是最新的；没有运行中的项目时停止轮询。
  await record('063-AC-04', async () => {
    await fresh({ clock: true })
    await page.goto(`${base}/creative`)
    await card(projects[0].name).waitFor()
    const badge = cover(projects[0].name).locator('.pc-badge')
    const dot = recent().first().locator('.cs-side-dot')
    assert.equal(await badge.innerText(), '运行中')
    at('poll while running')
    projects[0].running = false
    counts = { running: 1, waiting: 2 }
    await page.clock.runFor(20_000)
    await badge.waitFor({ state: 'detached' })
    await dot.waitFor({ state: 'detached' })
    at('visibility')
    projects[0].running = true
    await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')))
    await badge.waitFor()
    at('back from the canvas')
    await card(projects[0].name).click()
    await page.waitForURL('**/creative/canvases/c1')
    // 等画布页真正替换掉首页，返回时首页才会重新挂载。
    await page.locator('.cs-app').waitFor({ state: 'detached' })
    projects[0].running = false
    await page.goBack()
    await card(projects[0].name).waitFor()
    // 页面装了假时钟，用定位器等待而不是 waitForFunction（它靠 rAF 轮询）。
    await badge.waitFor({ state: 'detached' })
    at('stop polling when idle')
    for (const p of projects) {
      p.running = false
      p.waiting = p.id === 'p2'
    }
    counts = { running: 0, waiting: 1 }
    await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')))
    await page.locator('.hp-hello', { hasText: /· 1 个等你确认$/ }).waitFor()
    const before = projectReads.length
    await page.clock.runFor(120_000)
    await page.waitForTimeout(300)
    assert.equal(projectReads.length, before, 'waiting alone does not poll')
  })


  await record('062-fix-project-handoff', async () => {
    for (const recovering of [false, true]) {
      await fresh({ init: () => {
        const put = IDBObjectStore.prototype.put
        IDBObjectStore.prototype.put = function (value, ...args) {
          if (this.name === 'journals' && value.job?.path === '/projects') window.reviewProjectPending = true
          if (this.name === 'journals' && window.reviewProjectPending && value.job === null && !sessionStorage.getItem('handoff-interrupted')) {
            this.transaction.addEventListener('complete', (event) => {
              // 模拟 journal 清空落盘后立刻中断，后续 Promise 不再有机会执行。
              event.stopImmediatePropagation()
              sessionStorage.setItem('handoff-interrupted', '1')
              location.reload()
            })
          }
          return put.call(this, value, ...args)
        }
      } })
      server.fault.project500 = recovering
      await page.goto(`${base}/creative`)
      await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
      await prompt().fill('交接中断后仍用同一个项目')
      await sendButton().click()
      if (recovering) await page.getByRole('button', { name: '查询并恢复原保存' }).click()
      await page.getByText('项目「交接中断后仍用同一个项目」已建好，消息还没发出。').waitFor()
      await page.waitForFunction(() => sessionStorage.getItem('handoff-interrupted') === '1')
      assert.equal(await prompt().inputValue(), '交接中断后仍用同一个项目')
      await sendButton().click()
      assert.equal(await openedWithAgent(), true)
      assert.equal(server.created.projects, 1)
      assert.equal(keys(server.posts.projects).length, 1)
    }
  })

  await record('062-fix-hashing-blocks-send', async () => {
    await fresh({ init: () => {
      const digest = crypto.subtle.digest.bind(crypto.subtle)
      crypto.subtle.digest = async (...args) => {
        window.reviewHashStarted = true
        await new Promise(resolve => { window.reviewReleaseHash = resolve })
        return digest(...args)
      }
    } })
    await page.goto(`${base}/creative`)
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    await prompt().fill('必须带上刚选的参考图')
    // 先等媒体能力响应，保证进入摘要计算。
    await page.waitForFunction(() => performance.getEntriesByType('resource').some(entry => entry.name.endsWith('/media-capabilities')))
    await page.locator('input[type=file]').setInputFiles(png('hashing.png'))
    await page.waitForFunction(() => window.reviewHashStarted)
    assert.equal(await sendButton().isDisabled(), true)
    await prompt().press('Enter')
    assert.equal(server.posts.projects.length, 0)
    await page.evaluate(() => window.reviewReleaseHash())
    await refs().first().and(page.locator('[data-state="ready"]')).waitFor()
    await sendButton().click()
    assert.equal(await openedWithAgent(), true)
    assert.deepEqual(server.posts.runs[0].body.payload.instruction.instruction_segments, [
      { type: 'text', text: '必须带上刚选的参考图' },
      { type: 'content_ref', content_revision_id: 'ccrv_up1' },
    ])
  })

  await record('062-fix-refresh-deduplication', async () => {
    await fresh()
    server.fault.run422 = true
    await page.goto(`${base}/creative`)
    await ask().getByRole('combobox', { name: '模型', exact: true }).filter({ hasText: '识图模型' }).waitFor()
    await prompt().fill('刷新后仍然判重')
    await page.waitForFunction(() => performance.getEntriesByType('resource').some(entry => entry.name.endsWith('/media-capabilities')))
    await page.locator('input[type=file]').setInputFiles(png('same.png'))
    await refs().first().and(page.locator('[data-state="ready"]')).waitFor()
    await sendButton().click()
    await page.getByRole('alert').filter({ hasText: '当前模型暂无可用的服务渠道' }).waitFor()
    await page.reload()
    await refs().first().and(page.locator('[data-state="ready"]')).waitFor()
    await page.waitForFunction(() => performance.getEntriesByType('resource').some(entry => entry.name.endsWith('/media-capabilities')))
    await page.locator('input[type=file]').setInputFiles(png('renamed.png', 'same.png'))
    await page.getByText('这张图已经添加过').waitFor()
    assert.equal(await refs().count(), 1)
    assert.equal(server.posts.uploads.length, 1)
  })

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify(results, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!only && Object.keys(results).length !== 20)) process.exitCode = 1
