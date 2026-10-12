// Browser regression for the creative home (V6-061 sidebar and routes, V6-063
// continue cards and covers). All API
// traffic is mocked; each AC runs in a fresh context.
// Run against the Vite dev server: node scripts/creative-home.e2e.mjs.
import assert from 'node:assert/strict'
import { mkdirSync } from 'node:fs'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const output = process.env.CREATIVE_QA_OUTPUT_DIR ?? '/tmp/creative-home-qa'
mkdirSync(output, { recursive: true })
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
let projects, counts
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
async function fresh({ width = 1280, height = 800, init, clock = false } = {}) {
  await context?.close()
  context = await browser.newContext({ viewport: { width, height } })
  projectReads = []
  ticketReads = []
  fixture()
  page = await context.newPage()
  if (clock) await page.clock.install()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
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
    if (path.endsWith('/creative/projects')) {
      const limit = Number(url.searchParams.get('limit') ?? 50)
      const items = url.searchParams.get('archived') === 'true' ? [] : projects.slice(0, limit)
      return json({ items, next_cursor: '', activity_counts: counts })
    }
    return json({ error: { code: 'unavailable', message: 'not modelled' } }, 503)
  })
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
    await page.locator('body').click({ position: { x: 800, y: 400 } })
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

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify(results, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!only && Object.keys(results).length !== 9)) process.exitCode = 1
