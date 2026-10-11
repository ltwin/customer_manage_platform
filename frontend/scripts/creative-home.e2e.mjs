// Browser regression for the creative home (V6-061 sidebar and routes). All API
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
let context, page, projectReads
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
const image = { id: 'n1', x: 0, y: 0, width: 200, height: 200, kind: 'image', content_revision_id: 'rev-1' }
const projects = [
  project(1, '九月，向海而行', { running: true, cover: { nodes: [image], edges: [], more: false } }),
  project(2, '雪山婚纱 · 二稿', { waiting: true }),
  project(3, '品牌 Lookbook · 春季，名字很长很长很长很长很长很长', { running: true, waiting: true }),
  project(4, '毕业照 · 光影练习'),
  project(5, '未命名项目'),
]
async function fresh({ width = 1280, height = 800, init } = {}) {
  await context?.close()
  context = await browser.newContext({ viewport: { width, height } })
  projectReads = []
  page = await context.newPage()
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
    if (path.endsWith('/creative/media-access-tickets'))
      return json({ url: pixel, expires_at: new Date(Date.now() + 3600000).toISOString(), mime: 'image/png', byte_size: 68 })
    if (path.endsWith('/creative/projects')) {
      const limit = Number(url.searchParams.get('limit') ?? 50)
      const items = url.searchParams.get('archived') === 'true' ? [] : projects.slice(0, limit)
      return json({ items, next_cursor: '', activity_counts: { running: 2, waiting: 2 } })
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
  // AC-01 展开 216、折叠 56，图标位置不变；偏好记在本机，换页面保持；本机存储不可用时默认展开。
  await record('AC-01', async () => {
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
    await page.getByRole('heading', { name: '我的项目' }).waitFor()
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

  // AC-02 折叠时悬停或聚焦条目，右侧显示名称提示，不被侧栏裁切。
  await record('AC-02', async () => {
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

  // AC-03 761–1100 自动折叠、隐藏切换按钮、不改偏好；≤760 顶栏；各宽度无横向滚动。
  await record('AC-03', async () => {
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

  // AC-04 当前页高亮带 aria-current；最近打开 5 个，按最近编辑，状态点，点击进入画布。
  await record('AC-04', async () => {
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
    assert.equal(await page.locator('.cs-side-recent img').count(), 1, 'only the project with an image gets one')
    at('projects page')
    await navLink('项目').click()
    await page.waitForURL('**/creative/projects')
    await page.getByRole('heading', { name: '我的项目' }).waitFor()
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

  // AC-05 导航增加条目时最近打开整体下移，放不下只在自己的区域里滚动；底部是返回 CRM 和账号区。
  await record('AC-05', async () => {
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

  assert.deepEqual(errors, [])
} finally {
  console.log(JSON.stringify(results, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!only && Object.keys(results).length !== 5)) process.exitCode = 1
