import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { chromium } from 'playwright'
const url = process.env.CREATIVE_EDITOR_URL,
  api = process.env.CREATIVE_EDITOR_API,
  output = '/tmp/creative-editor-qa'
await mkdir(output, { recursive: true })
const browser = await chromium.launch({ headless: true })
const context = await browser.newContext({
  viewport: { width: 1600, height: 1000 },
})
const errors = []
await context.exposeBinding('reportSaveNotice', (_, message) =>
  errors.push(message),
)
await context.addInitScript(() => {
  const observer = new MutationObserver(() => {
    const text = document.body?.innerText ?? ''
    const match = text.match(
      /正在保存|正在确认保存结果|已与服务器同步|^已保存$|保存结果已确认/m,
    )
    if (match)
      void window.reportSaveNotice('unexpected save notice: ' + match[0])
  })
  observer.observe(document, {
    childList: true,
    subtree: true,
    characterData: true,
  })
})
// Hold only the completion notification of a real, already committed IndexedDB write.
await context.addInitScript(() => {
  const pending = new WeakSet()
  const put = IDBObjectStore.prototype.put
  IDBObjectStore.prototype.put = function (value, key) {
    if (
      this.name === 'journals' &&
      window.holdProjectCompletion &&
      value.job === null &&
      !value.drafts?.['project-form']
    ) {
      window.holdProjectCompletion = false
      pending.add(this.transaction)
    }
    return put.call(this, value, key)
  }
  const complete = Object.getOwnPropertyDescriptor(
    IDBTransaction.prototype,
    'oncomplete',
  )
  Object.defineProperty(IDBTransaction.prototype, 'oncomplete', {
    ...complete,
    set(handler) {
      complete.set.call(this, function (event) {
        if (pending.has(this)) {
          window.projectCompletionHeld = true
          window.releaseProjectCompletion = () => {
            window.projectCompletionHeld = false
            handler.call(this, event)
          }
        } else handler?.call(this, event)
      })
    },
  })
})
let loseNext = false
let loseNode = false
let loseProject = false
let failedCanvasPath = null
let holdMove = false
let releaseMove
const moves = []
let canvasReads = 0
await context.route('**/api/v1/**', async (route) => {
  const u = new URL(route.request().url()),
    path = u.pathname + u.search
  if (u.pathname === failedCanvasPath && route.request().method() === 'GET') {
    await route.abort('failed')
    return
  }
  if (
    u.pathname.includes('/creative/canvases/') &&
    route.request().method() === 'GET'
  )
    canvasReads++
  const payload =
    route.request().method() === 'POST'
      ? route.request().postDataJSON()?.payload
      : null
  if (payload?.type === 'move_node') {
    moves.push(payload)
    if (holdMove) {
      holdMove = false
      await new Promise((resolve) => {
        releaseMove = resolve
      })
    }
  }
  const response = await route.fetch({ url: api + path })
  if (
    route.request().method() === 'POST' &&
    ((loseNext && path === '/api/v1/creative/assets') ||
      (loseNode && path.endsWith('/commands')) ||
      (loseProject && path === '/api/v1/creative/projects'))
  ) {
    loseNext = false
    loseNode = false
    loseProject = false
    await route.abort('failed')
    return
  }
  await route.fulfill({ response })
})
async function waitUntil(check, message) {
  const deadline = Date.now() + 10000
  while (!check()) {
    assert(Date.now() < deadline, message)
    await new Promise((resolve) => setTimeout(resolve, 25))
  }
}
async function saved(p) {
  await p.waitForFunction(
    () => {
      const button = document.querySelector(
        'button.ch-create, .cc-library button[aria-label="新增资产"]',
      )
      return button && !button.disabled
    },
    null,
    { timeout: 15000 },
  )
  assert.equal(
    await p
      .getByText(
        /正在保存|正在确认保存结果|已与服务器同步|^已保存$|保存结果已确认/,
      )
      .count(),
    0,
  )
}
async function selectNode(p) {
  await p.locator('.react-flow__node').first().click()
  await p.locator('.cc-inspector textarea').waitFor()
}
try {
  const page = await context.newPage()
  page.on('pageerror', (e) => errors.push(e.message))
  await page.goto(`${url}/login`)
  await page
    .getByLabel('邮箱', { exact: true })
    .fill(process.env.CREATIVE_EDITOR_EMAIL)
  await page
    .getByLabel('登录密码', { exact: true })
    .fill(process.env.CREATIVE_EDITOR_PASSWORD)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await page.waitForURL('**/dashboard')
  await page.goto(`${url}/creative`)
  await saved(page)
  await page.getByRole('heading', { name: '我的项目', exact: true }).waitFor()
  assert.equal(await page.locator('.react-flow').count(), 0)
  await page.screenshot({ path: `${output}/home-empty.png`, fullPage: true })
  await page
    .getByRole('link', { name: '个人资产库', exact: true })
    .first()
    .click()
  await saved(page)
  await page.getByRole('button', { name: '新增资产', exact: true }).click()
  await page.getByLabel('资产名称').fill('共同参考')
  await page.getByLabel('正文', { exact: true }).fill('两张画布共用的原始文字')
  await page.getByLabel('内容来源').selectOption('owned')
  loseNext = true
  await page.getByRole('button', { name: '保存资产', exact: true }).click()
  await page.getByRole('button', { name: '查询并恢复原保存' }).waitFor()
  const originalTab = await page.evaluate(() =>
    sessionStorage.getItem('creative-editor-tab'),
  )
  await page.reload()
  await page.getByRole('button', { name: '查询并恢复原保存' }).click()
  assert.equal(
    await page.evaluate(() => sessionStorage.getItem('creative-editor-tab')),
    originalTab,
  )
  await saved(page)
  assert.equal(await page.locator('.cc-asset').count(), 1)
  async function createProject(name) {
    await page.getByRole('link', { name: '返回创意空间', exact: true }).click()
    await saved(page)
    assert.equal(await page.locator('.ch-create input').count(), 0)
    if (name === '第一张画布') loseProject = true
    // Every creation entry uses the same one-click flow.
    const createButton =
      name === '第一张画布'
        ? page.getByRole('button', { name: '创建项目', exact: true })
        : page.getByRole('button', { name: '开启新的创作' })
    await createButton.evaluate((button) => {
      button.click()
      button.click()
    })
    if (name === '第一张画布') {
      await page.getByRole('button', { name: '查询并恢复原保存' }).waitFor()
      await page.reload()
      await page.getByRole('button', { name: '查询并恢复原保存' }).click()
    }
    await page.waitForURL('**/creative/canvases/*')
    await saved(page)
    await page
      .getByRole('heading', { name: '未命名项目', exact: true })
      .waitFor()
    await page.getByRole('button', { name: '改名', exact: true }).click()
    await page.getByLabel('新名称', { exact: true }).fill(name)
    await page.getByRole('button', { name: '保存名称', exact: true }).click()
    await page.getByRole('heading', { name, exact: true }).waitFor()
    await saved(page)
    await page.reload()
    await saved(page)
    await page.getByRole('heading', { name, exact: true }).waitFor()
    if (name === '第一张画布') {
      loseNode = true
      await page.getByRole('button', { name: '放入画布', exact: true }).click()
      await page.getByRole('button', { name: '查询并恢复原保存' }).click()
    } else {
      await page
        .locator('.cc-asset')
        .first()
        .dragTo(page.locator('.react-flow__pane'))
    }
    await page.locator('.react-flow__node').waitFor()
    assert.equal(await page.locator('.react-flow__node').count(), 1)
    await saved(page)
    return page.url()
  }
  const first = await createProject('第一张画布'),
    second = await createProject('第二张画布')
  await page.goto(first)
  await saved(page)
  await selectNode(page)
  await page.getByLabel('正文', { exact: true }).fill('第一张画布的本机草稿')
  await page.getByText('有本机草稿 · 尚未保存到项目', { exact: true }).waitFor()
  await page.reload()
  await saved(page)
  await selectNode(page)
  assert.equal(
    await page.getByLabel('正文', { exact: true }).inputValue(),
    '第一张画布的本机草稿',
  )
  await page.getByRole('button', { name: '保存到节点', exact: true }).click()
  await saved(page)
  await page.goto(second)
  await saved(page)
  await selectNode(page)
  assert.equal(
    await page.getByLabel('正文', { exact: true }).inputValue(),
    '两张画布共用的原始文字',
  )
  assert(
    (await page.locator('.cc-asset').innerText()).includes(
      '两张画布共用的原始文字',
    ),
  )
  await page.goto(first)
  await saved(page)
  await selectNode(page)
  await page.getByLabel('正文', { exact: true }).fill('保留我的冲突草稿')
  const other = await context.newPage()
  other.on('pageerror', (e) => errors.push(e.message))
  await other.goto(first)
  await saved(other)
  await selectNode(other)
  await other.getByLabel('正文', { exact: true }).fill('另一窗口已经保存')
  await other.getByRole('button', { name: '保存到节点', exact: true }).click()
  await saved(other)
  await other.close()
  await page.waitForTimeout(5500)
  assert.equal(
    await page.getByLabel('正文', { exact: true }).inputValue(),
    '保留我的冲突草稿',
  )
  await page.getByRole('button', { name: '保存到节点', exact: true }).click()
  await page.getByRole('alertdialog').waitFor()
  await page
    .getByRole('alertdialog')
    .evaluate((el) =>
      Promise.all(el.getAnimations().map((animation) => animation.finished)),
    )
  await page.screenshot({ path: `${output}/conflict.png` })
  await page
    .getByRole('button', { name: '将草稿应用到当前版本', exact: true })
    .click()
  await saved(page)
  await page.getByRole('button', { name: '向→移动', exact: true }).click()
  await saved(page)
  await page.getByRole('button', { name: '归档项目', exact: true }).click()
  await saved(page)
  assert(
    await page
      .getByRole('button', { name: '＋ 文字', exact: true })
      .isDisabled(),
  )
  await page.getByRole('button', { name: '恢复项目', exact: true }).click()
  await saved(page)
  assert(
    !(await page
      .getByRole('button', { name: '＋ 文字', exact: true })
      .isDisabled()),
  )

  // Link input and manual draft discard use the same real save/recovery path.
  await page.getByRole('button', { name: '新增资产', exact: true }).click()
  await page.getByLabel('资产名称').fill('只保存链接')
  await page.getByLabel('内容类型').selectOption('link')
  await page
    .getByLabel('链接地址', { exact: true })
    .fill('https://example.invalid/reference')
  await page.getByLabel('内容来源').selectOption('reference')
  await page.getByRole('button', { name: '保存资产', exact: true }).click()
  await saved(page)
  assert.equal(await page.locator('.cc-asset').count(), 2)
  await selectNode(page)
  await context.setOffline(true)
  // route.fetch uses a separate request context; abort canvas reads to model the offline poll.
  failedCanvasPath =
    '/api/v1/creative/canvases/' + new URL(first).pathname.split('/').pop()
  await page.waitForFunction(
    () => {
      const button = [...document.querySelectorAll('button')].find(
        (b) => b.textContent === '保存到节点',
      )
      return button?.disabled
    },
    null,
    { timeout: 10000 },
  )
  await page.getByLabel('正文', { exact: true }).fill('离线输入仍保留')
  await page.getByText('有本机草稿 · 尚未保存到项目', { exact: true }).waitFor()
  failedCanvasPath = null
  await context.setOffline(false)
  await page.reload()
  await saved(page)
  await selectNode(page)
  assert.equal(
    await page.getByLabel('正文', { exact: true }).inputValue(),
    '离线输入仍保留',
  )
  await page.getByRole('button', { name: '放弃本机修改', exact: true }).click()
  await page.getByRole('alertdialog').waitFor()
  await page.getByRole('button', { name: '取消', exact: true }).click()
  assert.equal(
    await page.getByLabel('正文', { exact: true }).inputValue(),
    '离线输入仍保留',
  )
  await page.getByRole('button', { name: '放弃本机修改', exact: true }).click()
  await page.getByRole('button', { name: '放弃草稿', exact: true }).click()
  await saved(page)
  // Failed in-app navigation must never leave the previous canvas writable.
  failedCanvasPath =
    '/api/v1/creative/canvases/' + new URL(second).pathname.split('/').pop()
  await page.getByRole('link', { name: '返回创意空间', exact: true }).click()
  await page
    .getByRole('link', { name: '打开项目：第二张画布', exact: true })
    .click()
  await page.waitForURL(second)
  await page.waitForFunction(
    () => document.querySelectorAll('.react-flow__node').length === 0,
  )
  assert(
    await page
      .getByRole('button', { name: '新增文字节点', exact: true })
      .isDisabled(),
  )
  failedCanvasPath = null
  await page.getByRole('link', { name: '返回创意空间', exact: true }).click()
  await page
    .getByRole('link', { name: '打开项目：第一张画布', exact: true })
    .click()
  await saved(page)
  await selectNode(page)
  await page.screenshot({ path: `${output}/desktop.png`, fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: `${output}/mobile.png`, fullPage: true })
  assert(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
    'narrow overflow',
  )
  await page.getByRole('button', { name: '收起编辑面板', exact: true }).click()
  await page.locator('.cc-flow').evaluate(async (el) => {
    await Promise.all(el.getAnimations().map((animation) => animation.finished))
    await new Promise((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(resolve)),
    )
  })
  const mobileNodeWidth = await page
    .locator('.react-flow__node')
    .first()
    .evaluate((el) => el.getBoundingClientRect().width)
  assert(mobileNodeWidth >= 160, `stable mobile node width: ${mobileNodeWidth}`)
  await page.screenshot({ path: `${output}/mobile-canvas.png` })
  await page.getByRole('button', { name: '资产库', exact: true }).click()
  assert(
    await page.getByRole('complementary', { name: '个人资产库' }).isVisible(),
  )
  await page.screenshot({ path: `${output}/mobile-library.png` })
  await page.getByRole('button', { name: '收起资产库', exact: true }).click()
  assert.equal(
    await page.getByRole('complementary', { name: '个人资产库' }).isVisible(),
    false,
  )
  assert.equal(await page.locator('.sidebar').count(), 0)
  assert.equal(
    await page.getByRole('link', { name: '旧空间', exact: true }).count(),
    0,
  )
  // New assets remain reachable without a project at a phone viewport.
  await page.goto(`${url}/creative/library`)
  await saved(page)
  await page.getByRole('button', { name: '资产库', exact: true }).click()
  for (const kind of ['text', 'link']) {
    await page.getByRole('button', { name: '新增资产', exact: true }).click()
    await page
      .getByLabel('资产名称')
      .fill(kind === 'text' ? '手机文字灵感' : '手机链接参考')
    await page.getByLabel('内容类型').selectOption(kind)
    await page
      .getByLabel(kind === 'text' ? '正文' : '链接地址', { exact: true })
      .fill(
        kind === 'text' ? '随时记下的想法' : 'https://example.invalid/mobile',
      )
    await page.getByLabel('内容来源').selectOption('owned')
    await page.getByRole('button', { name: '保存资产', exact: true }).click()
    await saved(page)
    assert(
      await page
        .getByRole('heading', {
          name: kind === 'text' ? '手机文字灵感' : '手机链接参考',
          exact: true,
        })
        .isVisible(),
    )
  }
  await page.getByRole('link', { name: '返回创意空间', exact: true }).click()
  await page
    .getByRole('link', { name: '打开项目：第一张画布', exact: true })
    .waitFor()
  await page.screenshot({ path: `${output}/home-mobile.png`, fullPage: true })
  assert(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  )
  await page.setViewportSize({ width: 1600, height: 1000 })
  await page.screenshot({ path: `${output}/home-desktop.png`, fullPage: true })
  await page.getByRole('button', { name: '已归档', exact: true }).click()
  await page
    .getByRole('heading', { name: '还没有归档项目', exact: true })
    .waitFor()
  await page.getByRole('button', { name: '进行中', exact: true }).click()
  await page
    .getByRole('link', { name: '打开项目：第一张画布', exact: true })
    .waitFor()
  assert.equal(
    await page
      .getByRole('link', { name: '打开项目：未命名项目', exact: true })
      .count(),
    0,
  )
  // A completed save must never navigate after its page has been left.
  for (const recover of [false, true]) {
    const previousCount = await page
      .getByRole('link', { name: '打开项目：未命名项目', exact: true })
      .count()
    if (recover) loseProject = true
    else
      await page.evaluate(() => {
        window.holdProjectCompletion = true
      })
    await page.getByRole('button', { name: '创建项目', exact: true }).click()
    if (recover) {
      await page.getByRole('button', { name: '查询并恢复原保存' }).waitFor()
      await page.evaluate(() => {
        window.holdProjectCompletion = true
      })
      await page.getByRole('button', { name: '查询并恢复原保存' }).click()
    }
    await page.waitForFunction(() => window.projectCompletionHeld === true)
    await page
      .getByRole('link', { name: '个人资产库', exact: true })
      .first()
      .click()
    await page.waitForURL('**/creative/library')
    await page.evaluate(() => window.releaseProjectCompletion())
    await saved(page)
    assert.equal(
      new URL(page.url()).pathname,
      '/creative/library',
      'late completion stole navigation',
    )
    await page.getByRole('link', { name: '返回创意空间', exact: true }).click()
    await page
      .getByRole('link', { name: '打开项目：未命名项目', exact: true })
      .nth(previousCount)
      .waitFor()
    assert.equal(
      await page
        .getByRole('link', { name: '打开项目：未命名项目', exact: true })
        .count(),
      previousCount + 1,
    )
  }
  // Deliberately stall the server while continuing real pointer drags.
  await page.goto(first)
  await saved(page)
  const dragged = page.locator('.react-flow__node').first()
  async function dragBy(target, dx, dy) {
    const box = await target.boundingBox()
    await page.mouse.move(box.x + box.width / 2, box.y + 35)
    await page.mouse.down()
    await page.mouse.move(box.x + box.width / 2 + dx, box.y + 35 + dy, {
      steps: 12,
    })
    await page.mouse.up()
  }
  await page.screenshot({ path: `${output}/drag-before.png` })
  const beforeDrag = await dragged.boundingBox()
  const moveStart = moves.length
  holdMove = true
  await dragBy(dragged, 65, 20)
  await page.waitForFunction(() =>
    document
      .querySelector('.react-flow__node')
      .style.transform.includes('translate'),
  )
  while (!releaseMove) await new Promise((resolve) => setTimeout(resolve, 10))
  const firstDrop = await dragged.boundingBox()
  assert(
    firstDrop.x - beforeDrag.x > 50,
    'drop snapped back while server was blocked',
  )
  await dragBy(dragged, 55, 20)
  await dragBy(dragged, 45, 15)
  const latestDrop = await dragged.boundingBox()
  assert(latestDrop.x - firstDrop.x > 80, 'pending save blocked later drags')
  const oldReads = canvasReads
  // At least one real poll reads the unchanged server position during the held POST.
  await waitUntil(() => canvasReads > oldReads, 'canvas poll did not run')
  await page.waitForTimeout(150)
  const afterPoll = await dragged.boundingBox()
  assert(
    Math.abs(afterPoll.x - latestDrop.x) < 1,
    'old snapshot overwrote local position',
  )
  assert.equal(moves.length - moveStart, 1)
  await page.screenshot({ path: `${output}/drag-pending.png` })
  releaseMove()
  releaseMove = undefined
  await saved(page)
  assert.equal(
    moves.length - moveStart,
    2,
    'intermediate drags were not coalesced',
  )
  const finalTransform = await dragged.evaluate((node) => node.style.transform)
  await page.reload()
  await saved(page)
  assert.equal(
    await dragged.evaluate((node) => node.style.transform),
    finalTransform,
    'latest drop was not saved',
  )

  // Losing a receipt must not freeze the next drag or lose its latest position on reload.
  loseNode = true
  await dragBy(dragged, 45, 10)
  await page.getByRole('button', { name: '查询并恢复原保存' }).waitFor()
  const uncertainDrop = await dragged.boundingBox()
  await dragBy(dragged, 40, 10)
  assert((await dragged.boundingBox()).x - uncertainDrop.x > 25)
  const uncertainTransform = await dragged.evaluate(
    (node) => node.style.transform,
  )
  await page.reload()
  await page.getByRole('button', { name: '查询并恢复原保存' }).waitFor()
  assert.equal(
    await dragged.evaluate((node) => node.style.transform),
    uncertainTransform,
  )
  await page.getByRole('button', { name: '查询并恢复原保存' }).click()
  await saved(page)
  await page.reload()
  await saved(page)
  assert.equal(
    await dragged.evaluate((node) => node.style.transform),
    uncertainTransform,
  )
  // Explicit offline mode queues locally; reconnect sends the latest position.
  const beforeOffline = moves.length
  await context.setOffline(true)
  await page.getByText('当前离线 · 输入仍保留在本机', { exact: true }).waitFor()
  await dragBy(dragged, -50, 10)
  const offlineTransform = await dragged.evaluate(
    (node) => node.style.transform,
  )
  assert.notEqual(offlineTransform, uncertainTransform)
  assert.equal(moves.length, beforeOffline)
  await context.setOffline(false)
  await saved(page)
  await page.reload()
  await saved(page)
  assert.equal(
    await dragged.evaluate((node) => node.style.transform),
    offlineTransform,
  )

  assert.deepEqual(errors, [])
  await writeFile(
    `${output}/result.json`,
    JSON.stringify(
      {
        pass: true,
        errors,
        scenarios: [
          'no-project-library',
          'lost-receipt-reload',
          'lost-node-receipt-no-duplicate',
          'asset-drag-to-canvas',
          'two-canvas-fork',
          'draft-reload',
          'cross-window-conflict',
          'keyboard-position',
          'archive-restore',
          'narrow-layout',
          'link-no-fetch',
          'offline-poll-error-draft-reload',
          'discard-confirmation',
          'failed-canvas-switch',
          'mobile-asset-create-without-project',
          'mobile-panel-and-stable-viewport',
          'project-home-and-canvas-navigation',
          'project-one-click-create-rename-and-unknown-receipt',
          'project-home-filters-and-responsive-layout',
          'project-home-completion-after-navigation',
          'responsive-drag-during-delayed-save-and-stale-poll',
          'drag-lost-receipt-reload-and-offline-reconnect',
        ],
      },
      null,
      2,
    ),
  )
  console.log('creative editor browser PASS')
} catch (e) {
  console.error('page errors', errors, e)
  for (const [i, p] of context.pages().entries())
    await p.screenshot({ path: `${output}/failure-${i}.png`, fullPage: true })
  throw e
} finally {
  releaseMove?.()
  await context.unrouteAll({ behavior: 'wait' })
  await browser.close()
}
