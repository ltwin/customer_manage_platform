import assert from 'node:assert/strict'
import { mkdir, writeFile, readFile } from 'node:fs/promises'
import path from 'node:path'
import { chromium } from 'playwright'
const url = process.env.CREATIVE_EDITOR_URL,
  api = process.env.CREATIVE_EDITOR_API,
  samples = process.env.CREATIVE_MEDIA_SAMPLES,
  output = '/tmp/creative-media-qa'
const only = process.env.CREATIVE_MEDIA_E2E_ONLY
assert.ok(only === undefined || only === 'transparent', 'CREATIVE_MEDIA_E2E_ONLY supports only "transparent"')
await mkdir(output, { recursive: true })
const browser = await chromium.launch({
    headless: true,
    args: process.platform === 'darwin'
      ? ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu']
      : ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'],
  }),
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
      '.cc-library button[aria-label="新增资产"],.pp-head button.btn-primary',
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

async function imageCorner(image) {
  const screenshot = await image.screenshot({ scale: 'css' })
  return page.evaluate(async (src) => {
    const bitmap = await createImageBitmap(await (await fetch(src)).blob())
    const canvas = new OffscreenCanvas(bitmap.width, bitmap.height)
    const ctx = canvas.getContext('2d')
    ctx.drawImage(bitmap, 0, 0)
    bitmap.close()
    const { data } = ctx.getImageData(Math.floor(canvas.width * 0.1), Math.floor(canvas.height * 0.1), 1, 1)
    return Array.from(data)
  }, `data:image/png;base64,${screenshot.toString('base64')}`)
}

async function transparentSceneExports() {
  // Export receipts must refresh the current canvas even without its event stream.
  await context.route('**/creative/canvases/*/events', (route) => route.fulfill({ status: 503 }))
  await page.goto(`${url}/creative/projects`)
  await page.locator('.pp-head').getByRole('button', { name: '新建项目', exact: true }).click()
  await page.waitForURL('**/creative/canvases/*')
  await page.evaluate(() => localStorage.setItem('creative-scene:first-run-seen', '1'))
  await page.getByRole('button', { name: '新建节点', exact: true }).click()
  await page.getByRole('menuitem', { name: '3D 场景', exact: true }).click()
  const canvasNode = (kind) => snapshot?.nodes.find((n) => n.metadata.type_key === kind)
  await until(() => canvasNode('core.scene'), 'scene node saved')
  const sceneID = canvasNode('core.scene').id
  await page.locator('.react-flow__pane').click({ button: 'right', position: { x: 250, y: 450 } })
  await page.getByRole('menuitem', { name: '图片', exact: true }).click()
  await until(() => canvasNode('core.image'), 'sibling node saved')
  await node(sceneID).locator('.cc-node-header h3').click()
  await node(canvasNode('core.image').id).locator('.cc-node-header h3').click({ modifiers: ['Shift'] })
  await page.getByRole('button', { name: '打组', exact: true }).click()
  await until(() => canvasNode('core.scene')?.parent_id, 'scene group saved')
  const parentID = canvasNode('core.scene').parent_id
  await node(sceneID).locator('.cc-node-header h3').click()
  await page.getByRole('toolbar', { name: '节点操作' }).getByRole('button', { name: '打开场景', exact: true }).click()
  const editor = page.getByRole('region', { name: '3D 场景编辑' })
  await editor.locator('canvas').waitFor({ timeout: 30000 })
  for (const name of ['方块', '摄像机']) {
    await editor.getByRole('button', { name: '添加', exact: true }).click()
    await editor.getByRole('menuitem', { name, exact: true }).click()
    await until(async () => (await editor.locator('.cc-scene-save').textContent()) === '已保存', `${name} saved`)
  }
  await editor.getByRole('tablist', { name: '属性' }).getByRole('tab', { name: '渲染', exact: true }).click()
  const transparentBackground = editor.getByRole('checkbox', { name: '透明背景' })
  const resolutions = editor.getByRole('group', { name: '输出分辨率' })
  const exports = []
  for (const [resolution, size, mime, transparent] of [
    ['2K', 2048, 'image/png', true],
    ['4K', 4096, 'image/png', true],
    ['2K', 2048, 'image/png', false],
  ]) {
    await transparentBackground.setChecked(transparent)
    await resolutions.getByRole('button', { name: resolution, exact: true }).click()
    await until(async () => (await editor.locator('.cc-scene-save').textContent()) === '已保存', `${resolution} settings saved`)
    await editor.getByRole('button', { name: '导出', exact: true }).click()
    const dialog = editor.getByRole('dialog', { name: '导出分镜' })
    await dialog.getByRole('checkbox', { name: '线稿', exact: true }).uncheck()
    const lit = dialog.getByRole('checkbox', { name: '材质光照', exact: true })
    if (await lit.isChecked()) await lit.uncheck()
    const response = page.waitForResponse((r) => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/scene-exports'), { timeout: 120000 })
    // Suspend just before delivery. Rendering ran normally and the event
    // stream stays unavailable when the page comes back.
    const suspended = !transparent ? 'hidden' : resolution === '4K' ? 'offline' : null
    if (suspended) {
      await context.route('**/creative/documents/*/scene-exports', async (route) => {
        await page.evaluate((state) => {
          if (state === 'hidden') {
            Object.defineProperty(document, 'hidden', { configurable: true, value: true })
            document.dispatchEvent(new Event('visibilitychange'))
          } else {
            Object.defineProperty(navigator, 'onLine', { configurable: true, value: false })
            window.dispatchEvent(new Event('offline'))
          }
        }, suspended)
        await route.fallback()
      }, { times: 1 })
    }
    await dialog.getByRole('button', { name: '导出', exact: true }).click()
    const result = await response
    assert.ok(result.ok(), `${resolution} scene export: ${await result.text()}`)
    assert.equal(result.request().postDataJSON().payload.effective_transparent_background, transparent)
    const { nodes } = await result.json()
    assert.equal(nodes.length, 1, `${resolution} places one canvas image`)
    exports.push({ id: nodes[0].node_id, size, mime, transparent })
    await dialog.getByRole('button', { name: '留在编辑器' }).click()
    await dialog.waitFor({ state: 'hidden' })
    if (suspended) {
      assert.ok(!snapshot?.nodes.some((n) => n.id === nodes[0].node_id), 'snapshot still predates the receipt')
      await page.evaluate((state) => {
        if (state === 'hidden') {
          Reflect.deleteProperty(document, 'hidden')
          document.dispatchEvent(new Event('visibilitychange'))
        } else {
          Reflect.deleteProperty(navigator, 'onLine')
          window.dispatchEvent(new Event('online'))
        }
      }, suspended)
      await until(() => snapshot?.nodes.some((n) => n.id === nodes[0].node_id && n.content), `canvas refreshes after ${suspended}`, 10000)
    }
  }
  await editor.getByRole('button', { name: '还原节点', exact: true }).click()
  await editor.waitFor({ state: 'hidden' })
  await until(() => exports.every(({ id }) => snapshot?.nodes.some((n) => n.id === id && n.content)), 'scene exports appear without reloading the page', 10000)
  await until(async () => {
    const box = await node(exports.at(-1).id).boundingBox()
    const { width, height } = page.viewportSize()
    return box && Math.abs(box.x + box.width / 2 - width / 2) < 80
      && Math.abs(box.y + box.height / 2 - height / 2) < 80
  }, 'returning to the canvas reveals the latest export')
  await page.getByRole('button', { name: '适应全部节点' }).click()
  for (const { id, size, mime, transparent } of exports) {
    const exportedNode = snapshot.nodes.find((n) => n.id === id)
    assert.equal(exportedNode.parent_id, parentID)
    const media = exportedNode.content.media
    const original = media.find((r) => r.role === 'original')
    const display = media.find((r) => r.role === 'display')
    assert.equal(original.mime, mime)
    assert.equal(Math.max(original.width, original.height), size)
    assert.equal(display?.mime, transparent ? 'image/png' : 'image/jpeg')
    assert.equal(Math.max(display.width, display.height), 1600)

    const cardImage = node(id).locator('.cc-media img')
    await cardImage.evaluate((img) => img.decode())
    assert.equal(await cardImage.evaluate((img) => Math.max(img.naturalWidth, img.naturalHeight)), 1600)
    if (!transparent) continue
    assert.deepEqual(await imageCorner(cardImage), [21, 21, 22, 255], `${size} display image shows the canvas card's #151516 through its sky`)
    await node(id).dblclick()
    const preview = page.getByRole('dialog', { name: /媒体预览/ })
    const originalImage = preview.locator('img')
    await originalImage.evaluate((img) => img.decode())
    assert.equal(await originalImage.evaluate((img) => Math.max(img.naturalWidth, img.naturalHeight)), size)
    const corner = await imageCorner(originalImage)
    assert.ok(corner.slice(0, 3).every((value) => value > 0 && value < 40), `${size} original shows the dark preview backdrop: ${corner}`)
    await preview.getByRole('button', { name: '关闭预览', exact: true }).click()
    await preview.waitFor({ state: 'hidden' })
    // The selected card's generation panel can cover the next row of exports.
    await page.locator('.react-flow__pane').click({ position: { x: 100, y: 180 } })
  }
}

async function existingMediaScenarios() {
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
  const tiles = page.locator('#creative-library .al-tile')
  await until(async () => (await tiles.count()) === 3, 'assets listed')
  // Thumbnails render through tickets, lazily.
  await until(
    async () => (await page.locator('#creative-library .al-tile .cl-media.is-ready img').count()) === 1,
    'image thumbnail ready',
  )
  await until(
    async () => (await page.locator('#creative-library .al-tile .cl-media.is-ready video').count()) === 1,
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
  await page.locator('#creative-library .al-tile[data-kind="image"]').click()
  await page.getByRole('dialog', { name: /^快速查看：/ }).waitFor()
  await until(async () => (await page.locator('.cl-look img').count()) === 1, 'preview image')
  await page.screenshot({ path: `${output}/library-preview.png` })
  await page.keyboard.press('Escape')
  await page.getByRole('dialog', { name: /^快速查看：/ }).waitFor({ state: 'hidden' })

  // 2. Project: drag assets in (multi-select), drop a file, upload to node.
  await page.goto(`${url}/creative/projects`)
  await saved()
  await page.getByRole('button', { name: '新建项目', exact: true }).click()
  await page.waitForURL('**/creative/canvases/*')
  // v6 资产库默认收起，从工具轨打开。
  await page.getByRole('button', { name: '资产库', exact: true }).click()
  await saved()
  const canvasURL = page.url()
  await until(async () => (await tiles.count()) === 3, 'shelf assets')
  // Uploads finish concurrently, so pick cards by kind rather than position.
  const card = (kind) => page.locator(`#creative-library .al-tile[data-kind="${kind}"]`)
  await card('image').click({ modifiers: ['ControlOrMeta'] })
  await until(
    async () => (await page.locator('#creative-library .al-tile.is-on').count()) === 1,
    'first asset selected',
  )
  await card('video').click({ modifiers: ['ControlOrMeta'] })
  await until(
    async () => (await page.locator('#creative-library .al-tile.is-on').count()) === 2,
    'two assets selected: ' +
      JSON.stringify(await page.locator('#creative-library .al-tile').evaluateAll((b) => b.map((x) => x.getAttribute('aria-pressed')))),
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
  await page.setViewportSize({ width: 1600, height: 1000 })
  return [
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
  ]
}

try {
  await page.goto(`${url}/login`)
  await page.getByLabel('邮箱', { exact: true }).fill(process.env.CREATIVE_EDITOR_EMAIL)
  await page.getByLabel('登录密码', { exact: true }).fill(process.env.CREATIVE_EDITOR_PASSWORD)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await page.waitForURL('**/dashboard')
  const scenarios = only ? [] : await existingMediaScenarios()
  await transparentSceneExports()
  scenarios.push('manual transparent PNG/WebP and opaque PNG appear and are revealed without canvas events or a reload')
  assert.deepEqual(errors, [])
  await writeFile(
    `${output}/result.json`,
    JSON.stringify(
      {
        scenarios,
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
