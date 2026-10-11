import assert from 'node:assert/strict'
import { mkdir, writeFile, readFile } from 'node:fs/promises'
import path from 'node:path'
import { chromium } from 'playwright'
const url = process.env.CREATIVE_EDITOR_URL,
  api = process.env.CREATIVE_EDITOR_API,
  samples = process.env.CREATIVE_MEDIA_SAMPLES,
  output = '/tmp/creative-media-qa'
await mkdir(output, { recursive: true })
const browser = await chromium.launch({ headless: true }),
  context = await browser.newContext({
    viewport: { width: 1600, height: 1000 },
    acceptDownloads: true,
  }),
  errors = []
let snapshot = null
const mediaRequests = [],
  postedCommands = []
let uploadsCreated = 0
await context.route('**/api/v1/**', async (route) => {
  // route.fetch 会等整个响应体结束，SSE 长连接必须原样转发给真实后端流式读取。
  if (route.request().headers().accept === 'text/event-stream') {
    const u = new URL(route.request().url())
    return route.continue({ url: api + u.pathname + u.search })
  }
  const request = route.request(),
    u = new URL(request.url())
  if (request.method() === 'POST' && u.pathname.endsWith('/commands'))
    postedCommands.push(request.postDataJSON()?.payload)
  if (request.method() === 'POST' && u.pathname === '/api/v1/creative/uploads')
    uploadsCreated++
  if (u.pathname.startsWith('/api/v1/creative/media/'))
    mediaRequests.push({
      path: u.pathname,
      url: api + u.pathname + u.search,
      range: request.headers()['range'] ?? null,
    })
  const response = await route.fetch({ url: api + u.pathname + u.search })
  if (
    request.method() === 'GET' &&
    /^\/api\/v1\/creative\/canvases\/[^/]+$/.test(u.pathname) &&
    response.ok()
  )
    snapshot = await response.json()
  await route.fulfill({ response })
})
const page = await context.newPage()
page.setDefaultTimeout(15000)
page.on('pageerror', (e) => errors.push(e.message))
const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
async function until(check, label, timeout = 30000) {
  const end = Date.now() + timeout
  while (!(await check())) {
    assert(Date.now() < end, label)
    await pause(50)
  }
}
async function saved() {
  await page.waitForFunction(() => {
    const button = document.querySelector(
      '.cc-library button[aria-label="新增资产"],button.ch-create',
    )
    return (
      button &&
      !button.disabled &&
      !document.querySelector('.cc-stage[data-sync="pending"]')
    )
  })
  assert.equal(
    await page
      .getByText(/正在保存|正在确认保存结果|已与服务器同步|^已保存$/)
      .count(),
    0,
  )
}
async function trayIdle() {
  await until(
    async () =>
      (await page.locator('.cc-upload-tray li').count()) > 0 &&
      (await page
        .locator('.cc-upload-tray li')
        .evaluateAll((items) =>
          items.every(
            (li) =>
              li.classList.contains('is-done') ||
              li.classList.contains('is-failed'),
          ),
        )),
    'upload tray idle',
    60000,
  )
}
const node = (id) => page.locator(`.react-flow__node[data-id="${id}"]`)
const sample = (name) => path.join(samples, name)
try {
  await page.goto(`${url}/login`)
  await page
    .getByLabel('邮箱', { exact: true })
    .fill(process.env.CREATIVE_EDITOR_EMAIL)
  await page
    .getByLabel('登录密码', { exact: true })
    .fill(process.env.CREATIVE_EDITOR_PASSWORD)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await page.waitForURL('**/dashboard')

  // 1. Library import: three media kinds plus a fake image become assets.
  await page.goto(`${url}/creative/library`)
  await saved()
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('button', { name: '导入文件', exact: true }).click()
  await (await chooser).setFiles([
    sample('sample.png'),
    sample('sample.webm'),
    sample('sample.mp3'),
  ])
  await trayIdle()
  await page.screenshot({ path: `${output}/library-import-tray.png` })
  const rows = await page.locator('.cc-upload-tray li').allInnerTexts()
  assert.equal(rows.filter((r) => r.includes('已完成')).length, 3, rows.join('|'))
  const tiles = page.locator('#creative-library .cl-tile')
  await until(async () => (await tiles.count()) === 3, 'assets listed')
  // Thumbnails render through tickets, lazily.
  await until(
    async () => (await page.locator('#creative-library .cl-tile .cl-media.is-ready img').count()) === 1,
    'image thumbnail ready',
  )
  await until(
    async () => (await page.locator('#creative-library .cl-tile .cl-media.is-ready video').count()) === 1,
    'video thumbnail ready',
  )
  await page.screenshot({ path: `${output}/library-media.png` })
  // Fake media (declared png, mp3 bytes) fails server verification per item.
  const fakePath = path.join(output, 'fake.png')
  await writeFile(fakePath, await readFile(sample('sample.mp3')))
  const chooser2 = page.waitForEvent('filechooser')
  await page.getByRole('button', { name: '导入文件', exact: true }).click()
  await (await chooser2).setFiles([fakePath])
  await trayIdle()
  await until(
    async () =>
      (await page.locator('.cc-upload-tray li').allInnerTexts()).some((r) =>
        r.includes('文件内容不是支持的媒体格式'),
      ),
    'fake media reported',
  )
  await page.getByRole('button', { name: '重试上传：fake.png' }).waitFor()
  await until(async () => (await tiles.count()) === 3, 'no asset for fake media')
  // Retry while the queue is idle starts a brand-new session and fails again
  // with the same reason (the bytes did not change).
  const sessionsBefore = uploadsCreated
  await page.getByRole('button', { name: '重试上传：fake.png' }).click()
  await until(() => uploadsCreated > sessionsBefore, 'retry created a new session')
  await trayIdle()
  await page.getByRole('button', { name: '重试上传：fake.png' }).waitFor()
  await until(async () => (await tiles.count()) === 3, 'retry still no asset')
  await page.getByRole('button', { name: '收起导入列表', exact: true }).click()
  // Kind filter and library preview.
  const kindFilter = async (label) => {
    await page.getByRole('button', { name: '资产类型', exact: true }).click()
    await page.getByRole('menu', { name: '资产类型' }).getByRole('menuitem', { name: label, exact: true }).click()
  }
  await kindFilter('视频')
  await until(async () => (await tiles.count()) === 1, 'video filter')
  await kindFilter('全部')
  await until(async () => (await tiles.count()) === 3, 'filter cleared')
  // v6 点卡片即快速查看，Esc 关闭。
  await page.locator('#creative-library .cl-tile[data-kind="image"]').click()
  await page.getByRole('dialog', { name: /^快速查看：/ }).waitFor()
  await until(async () => (await page.locator('.cl-look img').count()) === 1, 'preview image')
  await page.screenshot({ path: `${output}/library-preview.png` })
  await page.keyboard.press('Escape')
  await page.getByRole('dialog', { name: /^快速查看：/ }).waitFor({ state: 'hidden' })

  // 2. Project: drag assets in (multi-select), drop a file, upload to node.
  await page.goto(`${url}/creative/projects`)
  await saved()
  await page.getByRole('button', { name: '开启新的创作' }).click()
  await page.waitForURL('**/creative/canvases/*')
  // v6 资产库默认收起，从工具轨打开。
  await page.getByRole('button', { name: '资产库', exact: true }).click()
  await saved()
  const canvasURL = page.url()
  await until(async () => (await tiles.count()) === 3, 'shelf assets')
  // Uploads finish concurrently, so pick cards by kind rather than position.
  const card = (kind) => page.locator(`#creative-library .cl-tile[data-kind="${kind}"]`)
  await card('image').click({ modifiers: ['ControlOrMeta'] })
  await until(
    async () => (await page.locator('#creative-library .cl-tile.is-on').count()) === 1,
    'first asset selected',
  )
  await card('video').click({ modifiers: ['ControlOrMeta'] })
  await until(
    async () => (await page.locator('#creative-library .cl-tile.is-on').count()) === 2,
    'two assets selected: ' +
      JSON.stringify(await page.locator('#creative-library .cl-tile').evaluateAll((b) => b.map((x) => x.getAttribute('aria-pressed')))),
  )
  await card('image').dragTo(page.locator('.react-flow__pane'), {
    targetPosition: { x: 500, y: 300 },
  })
  await until(
    () => snapshot?.nodes.length === 2,
    'two nodes from multi drag: ' + JSON.stringify(postedCommands.at(-1)),
  )
  await saved()
  // Drop a real file onto the canvas: an empty media node is created then filled.
  const [dropX, dropY] = [900, 500]
  await page.evaluate(async ({ x, y, bytes, name }) => {
    const dt = new DataTransfer()
    dt.items.add(new File([new Uint8Array(bytes)], name, { type: 'image/jpeg' }))
    const target = document.elementFromPoint(x, y)
    for (const type of ['dragover', 'drop'])
      target.dispatchEvent(
        new DragEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: y, dataTransfer: dt }),
      )
  }, { x: dropX, y: dropY, bytes: Array.from(await readFile(sample('sample.jpg'))), name: 'drop.jpg' })
  await trayIdle()
  await until(
    () => snapshot?.nodes.length === 3 && snapshot.nodes.every((n) => n.content),
    'dropped file bound to node',
  )
  await saved()
  await until(
    async () => (await page.locator('.react-flow__node .cc-media.is-ready').count()) === 3,
    'node media rendered',
  )
  // 64x48 samples: nodes never resized by hand take the picture's aspect
  // ratio at the default width (300 * 48 / 64 = 225), audio
  // keeps the default box (300x120, PRD CV-C0).
  for (const n of snapshot.nodes) {
    const kind = n.metadata.type_key.slice(5)
    assert.equal(n.metadata.width, 300, `${kind} width`)
    assert.equal(n.metadata.height, kind === 'audio' ? 120 : 225, `${kind} height`)
  }
  await page.screenshot({ path: `${output}/canvas-media.png` })
  const jpgNode = snapshot.nodes.find((n) => n.content.media[0].mime === 'image/jpeg')
  const beforeReplace = jpgNode.data.content_revision_id
  // Replace via the toolbar; the asset in the library stays untouched.
  await node(jpgNode.id).click()
  const chooser3 = page.waitForEvent('filechooser')
  // V6-007：替换媒体在「更多」菜单里（工具条按 CV-70 不再单列）。
  await page.getByRole('toolbar', { name: '节点操作' }).getByRole('button', { name: '更多', exact: true }).click()
  await page.getByRole('menu', { name: '节点右键菜单' }).getByRole('menuitem', { name: '替换媒体', exact: true }).click()
  await (await chooser3).setFiles([sample('sample.webp')])
  await trayIdle()
  await until(
    () => snapshot?.nodes.find((n) => n.id === jpgNode.id)?.data.content_revision_id !== beforeReplace,
    'node replaced',
  )
  assert.equal(
    snapshot.nodes.find((n) => n.id === jpgNode.id).content.media[0].mime,
    'image/webp',
  )
  await saved()
  // Undo restores the previous media revision.
  await page.getByRole('button', { name: '撤销', exact: true }).click()
  await until(
    () => snapshot?.nodes.find((n) => n.id === jpgNode.id)?.data.content_revision_id === beforeReplace,
    'undo replacement',
  )
  await saved()
  // v6 双击有内容的媒体是全屏查看；说明从右键菜单修改，保存为新修订。
  await node(jpgNode.id).click({ button: 'right' })
  await page.getByRole('menu', { name: '节点右键菜单' }).getByRole('menuitem', { name: '编辑说明', exact: true }).click()
  await page.getByLabel('媒体说明').fill('窗边的光')
  await page.getByRole('button', { name: '保存到节点', exact: true }).click()
  await until(
    () => snapshot?.nodes.find((n) => n.id === jpgNode.id)?.content?.payload?.caption === '窗边的光',
    'caption saved',
  )
  assert.equal(
    snapshot.nodes.find((n) => n.id === jpgNode.id).content.media[0].mime,
    'image/jpeg',
  )
  await saved()
  // Maximize preview and download from the toolbar.
  await node(jpgNode.id).click()
  await page.getByRole('toolbar', { name: '节点操作' }).getByRole('button', { name: '全屏查看', exact: true }).click()
  await page.getByRole('dialog', { name: /媒体预览/ }).waitFor()
  await until(async () => (await page.locator('.cc-media-preview img').count()) === 1, 'node preview')
  await page.screenshot({ path: `${output}/canvas-preview.png` })
  await page.getByRole('button', { name: '关闭预览', exact: true }).click()
  await node(jpgNode.id).click()
  const download = page.waitForEvent('download')
  await page.getByRole('toolbar', { name: '节点操作' }).getByRole('button', { name: '下载', exact: true }).click()
  const file = await download
  assert.equal(file.suggestedFilename(), 'drop.jpg')
  const body = await readFile(await file.path())
  assert.deepEqual(body, await readFile(sample('sample.jpg')))
  // The ticketed URL answers HEAD and single ranges with 206/416 and denies a
  // forged role without leaking bytes.
  const downloadURL = mediaRequests.findLast((r) => r.path.endsWith('/original'))
  assert(downloadURL, 'download went through the ticketed media route')
  const head = await page.request.head(downloadURL.url)
  assert.equal(head.status(), 200)
  assert.equal(head.headers()['accept-ranges'], 'bytes')
  const partial = await page.request.get(downloadURL.url, { headers: { Range: 'bytes=2-5' } })
  assert.equal(partial.status(), 206)
  assert.equal(partial.headers()['content-range'], `bytes 2-5/${body.length}`)
  assert.deepEqual(Buffer.from(await partial.body()), body.subarray(2, 6))
  const beyond = await page.request.get(downloadURL.url, { headers: { Range: `bytes=${body.length + 10}-` } })
  assert.equal(beyond.status(), 416)
  const forged = await page.request.get(downloadURL.url.replace('/original?', '/display?'))
  assert.equal(forged.status(), 403)
  // Video plays: real playback with a ranged request through the ticketed URL.
  const videoNode = snapshot.nodes.find((n) => n.content.media[0].mime === 'video/webm')
  const played = await node(videoNode.id).locator('video').evaluate(async (v) => {
    v.muted = true
    await v.play()
    await new Promise((r) => setTimeout(r, 700))
    return { time: v.currentTime, duration: v.duration, ready: v.readyState }
  })
  assert(played.time > 0 && played.ready >= 2, JSON.stringify(played))
  assert(mediaRequests.some((r) => r.range), 'browser used Range for media')
  // Save a node back to the library as a new asset.
  await node(jpgNode.id).click()
  await page.getByRole('toolbar', { name: '节点操作' }).getByRole('button', { name: '存入资产库', exact: true }).click()
  await page.getByLabel('资产名称').fill('从节点存回')
  await page.getByRole('button', { name: '保存资产', exact: true }).click()
  await until(async () => (await tiles.count()) === 4, 'saved asset listed')
  await saved()
  // Reopen: everything is still there and renders.
  await page.goto(canvasURL)
  await saved()
  await until(() => snapshot?.nodes.length === 3, 'reopened nodes')
  await until(
    async () => (await page.locator('.react-flow__node .cc-media.is-ready').count()) === 3,
    'reopened media rendered',
  )
  await page.setViewportSize({ width: 390, height: 780 })
  await pause(300)
  await page.screenshot({ path: `${output}/canvas-media-mobile.png` })
  assert.deepEqual(errors, [])
  await writeFile(
    `${output}/result.json`,
    JSON.stringify(
      {
        scenarios: [
          'library import png/webm/mp3',
          'fake media rejected per item',
          'kind filter and quick look',
          'multi-select drag to canvas',
          'file drop creates and fills node',
          'replace media and undo',
          'maximize preview and download original',
          'video playback with range',
          'save node to library',
          'reopen consistency',
        ],
        mediaRequests: mediaRequests.length,
        errors,
      },
      null,
      2,
    ),
  )
  console.log('creative media browser PASS')
} catch (error) {
  console.error('page errors', errors)
  await page.screenshot({ path: `${output}/failure.png` }).catch(() => {})
  await writeFile(
    `${output}/failure.txt`,
    String(error) + '\n' + JSON.stringify(errors) + '\n' + (await page.locator('body').innerText().catch(() => '')),
  )
  throw error
} finally {
  await context.unrouteAll({ behavior: 'wait' })
  await browser.close()
}
