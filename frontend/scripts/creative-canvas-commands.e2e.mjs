import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { chromium } from 'playwright'

// v6 窄屏隐藏缩放条：按钮不可见时走 ⇧1。
// V6-007：单选时复制/删除在「更多」菜单里（PRD CV-70/74/80）。
async function fromMore(label) {
  await page
    .getByRole('toolbar', { name: '节点操作' })
    .getByRole('button', { name: '更多', exact: true })
    .click()
  await page
    .getByRole('menu', { name: '节点右键菜单' })
    .getByRole('menuitem', { name: label, exact: true })
    .click()
}
async function fitAll(page) {
  const button = page.getByRole('button', { name: '适应全部节点', exact: true })
  if (await button.isVisible()) return button.click()
  await page.locator('.react-flow').focus()
  await page.keyboard.press('Shift+Digit1')
}

const url = process.env.CREATIVE_EDITOR_URL,
  api = process.env.CREATIVE_EDITOR_API,
  output = '/tmp/creative-commands-qa'
await mkdir(output, { recursive: true })
const browser = await chromium.launch({ headless: true }),
  context = await browser.newContext({
    viewport: { width: 1600, height: 1000 },
  }),
  errors = []
let snapshot = null,
  posts = 0
let holdConnection = false,
  releaseConnection
const postedCommands = []
let checkedPendingSelection = false
await context.route('**/api/v1/**', async (route) => {
  // route.fetch 会等整个响应体结束，SSE 长连接必须原样转发给真实后端流式读取。
  if (route.request().headers().accept === 'text/event-stream') {
    const u = new URL(route.request().url())
    return route.continue({ url: api + u.pathname + u.search })
  }
  const request = route.request(),
    u = new URL(request.url())
  if (
    holdConnection &&
    request.method() === 'POST' &&
    request
      .postDataJSON()
      ?.payload?.actions?.some((a) => a.type === 'connect_reference')
  ) {
    holdConnection = false
    await new Promise((resolve) => {
      releaseConnection = resolve
    })
  }
  if (request.method() === 'POST')
    postedCommands.push(request.postDataJSON()?.payload)
  const response = await route.fetch({ url: api + u.pathname + u.search })
  if (request.method() === 'POST') posts++
  // Rejected commands are the usual reason a snapshot never advances.
  if (
    request.method() === 'POST' &&
    u.pathname.startsWith('/api/v1/creative/') &&
    !response.ok()
  )
    console.error(
      'rejected',
      u.pathname,
      response.status(),
      await response.text(),
      request.postData(),
    )
  if (
    request.method() === 'GET' &&
    /^\/api\/v1\/creative\/canvases\/[^/]+$/.test(u.pathname) &&
    response.ok()
  )
    snapshot = await response.json()
  await route.fulfill({ response })
})
const page = await context.newPage()
page.setDefaultTimeout(10000)
page.on('pageerror', (e) => errors.push(e.message))
const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
async function until(check, label) {
  const end = Date.now() + 15000
  while (!(await check())) {
    assert(Date.now() < end, label)
    await pause(30)
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
async function snapshotAfter(work, predicate = () => true) {
  const old = snapshot?.revision
  await work()
  await until(
    () => snapshot?.revision !== old && predicate(snapshot),
    'committed canvas snapshot',
  )
  await saved()
}
const node = (id) => page.locator(`.react-flow__node[data-id="${id}"]`)
const world = (c, id) => {
  let n = c.nodes.find((n) => n.id === id),
    x = n.metadata.x,
    y = n.metadata.y
  while (n.parent_id) {
    n = c.nodes.find((v) => v.id === n.parent_id)
    x += n.metadata.x
    y += n.metadata.y
  }
  return { x, y }
}
async function verifyResizeHandles(id, preserveChildren = []) {
  for (const [sides, dx, dy] of [
    ['left', -18, 0],
    ['right', 18, 0],
    ['top', 0, -18],
    ['bottom', 0, 18],
    ['top.left', -18, -18],
    ['top.right', 18, -18],
    ['bottom.left', -18, 18],
    ['bottom.right', 18, 18],
  ]) {
    await node(id).locator('.cc-node-header').click()
    const old = snapshot.nodes.find((n) => n.id === id).metadata
    const oldWorld = world(snapshot, id)
    const children = preserveChildren.map((child) => [
      child,
      world(snapshot, child),
    ])
    const zoom = Number(
      (await page.locator('.react-flow__viewport').getAttribute('style')).match(
        /scale\(([^)]+)\)/,
      )[1],
    )
    const handle = node(id).locator(
      `.react-flow__resize-control.${sides.includes('.') ? 'handle' : 'line'}.${sides}`,
    )
    const box = await handle.boundingBox()
    assert(box, `resize handle ${sides}`)
    const startX =
      box.x + box.width * (sides === 'top' || sides === 'bottom' ? 0.25 : 0.5)
    const startY =
      box.y + box.height * (sides === 'left' || sides === 'right' ? 0.25 : 0.5)
    await snapshotAfter(async () => {
      await page.mouse.move(startX, startY)
      await page.mouse.down()
      await page.mouse.move(startX + dx, startY + dy, { steps: 6 })
      await page.mouse.up()
    })
    const current = snapshot.nodes.find((n) => n.id === id).metadata
    const currentWorld = world(snapshot, id)
    assert(
      Math.abs(
        currentWorld.x - oldWorld.x - (sides.includes('left') ? dx / zoom : 0),
      ) < 2,
      `${sides} origin x`,
    )
    assert(
      Math.abs(
        currentWorld.y - oldWorld.y - (sides.includes('top') ? dy / zoom : 0),
      ) < 2,
      `${sides} origin y`,
    )
    assert(
      Math.abs(current.width - old.width - Math.abs(dx) / zoom) < 2,
      `${sides} width`,
    )
    assert(
      Math.abs(current.height - old.height - Math.abs(dy) / zoom) < 2,
      `${sides} height`,
    )
    for (const [child, position] of children)
      assert.deepEqual(world(snapshot, child), position)
    await snapshotAfter(() =>
      page.getByRole('button', { name: '撤销', exact: true }).click(),
    )
    assert.deepEqual(snapshot.nodes.find((n) => n.id === id).metadata, old)
    await until(
      async () =>
        Math.abs((await node(id).boundingBox()).width / zoom - old.width) < 2,
      'undo restores rendered dimensions',
    )
    await snapshotAfter(() =>
      page.getByRole('button', { name: '重做', exact: true }).click(),
    )
    assert.deepEqual(snapshot.nodes.find((n) => n.id === id).metadata, current)
    await snapshotAfter(() =>
      page.getByRole('button', { name: '撤销', exact: true }).click(),
    )
  }
}
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
  await page.goto(`${url}/creative/projects`)
  await saved()
  await page.getByRole('button', { name: '新建项目', exact: true }).click()
  await page.waitForURL('**/creative/canvases/*')
  await saved()
  const canvasURL = page.url()
  if (await page.locator('#creative-library').isVisible())
    await page.getByRole('button', { name: '收起资产库', exact: true }).click()
  await snapshotAfter(
    async () => {
      await page.getByRole('button', { name: '新建节点', exact: true }).click()
      await page
        .getByRole('menuitem', { name: '文字', exact: true })
        .click()
    },
    (c) => c.nodes.length === 1,
  )
  const a = snapshot.nodes[0].id
  // 画布交互回归：屏幕空间磁吸、点击锚点与自由平移。
  const interactionFailures = []
  const checkInteraction = (ok, message) => {
    if (!ok) interactionFailures.push(message)
  }
  const port = node(a).getByRole('button', {
    name: '添加下游节点',
    exact: true,
  })
  await node(a).click()
  await page.mouse.move(900, 850)
  await pause(400)
  const resting = await port.boundingBox()
  const center = {
    x: resting.x + resting.width / 2,
    y: resting.y + resting.height / 2,
  }
  await page.mouse.move(center.x + 30, center.y + 12)
  await pause(450)
  const attracted = await port.boundingBox()
  checkInteraction(
    attracted.x > resting.x + 3 && attracted.y > resting.y + 1,
    '加号应在鼠标靠近时弹性跟随',
  )
  const visual = await port.evaluate((el) => ({
    icon: el.querySelector('svg').getBoundingClientRect().width,
    ring: parseFloat(getComputedStyle(el, '::after').borderTopWidth),
    ringWidth: getComputedStyle(el, '::after').width,
    background: getComputedStyle(el, '::after').backgroundColor,
  }))
  assert.equal(visual.background, 'rgb(11, 11, 12)', '加号底色应为 v6 --bg')
  const zoom =
    (await node(a).boundingBox()).width / snapshot.nodes[0].metadata.width
  checkInteraction(
    Math.abs(visual.icon / zoom - 18) < 1 &&
      visual.ring >= 1 &&
      visual.ring <= 1.5 &&
      visual.ringWidth === '24px',
    '加号应为 18px 图标和 1.5px 圆环',
  )
  const clickPoint = {
    x: attracted.x + attracted.width / 2,
    y: attracted.y + attracted.height / 2,
  }
  await page.mouse.click(clickPoint.x, clickPoint.y)
  const menu = page.getByRole('menu', { name: '创建并连接节点' })
  await menu.waitFor()
  const menuBox = await menu.boundingBox()
  checkInteraction(
    Math.abs(menuBox.x - clickPoint.x) < 16 &&
      Math.abs(menuBox.y - clickPoint.y) < 16,
    '连接菜单应出现在点击处',
  )
  await page.screenshot({ path: `${output}/port-menu-desktop.png` })
  await page.keyboard.press('Escape')
  const viewportState = () =>
    page.locator('.react-flow__viewport').evaluate((el) => {
      const matrix = new DOMMatrix(getComputedStyle(el).transform)
      return { x: matrix.e, y: matrix.f, zoom: matrix.a }
    })
  // 选中节点下方有生成面板，滚轮落在空白画布上才是平移。
  await page.mouse.move(1200, 950)
  const beforePan = await viewportState()
  await page.mouse.wheel(140, 0)
  await pause(180)
  const afterPan = await viewportState()
  checkInteraction(
    afterPan.x < beforePan.x - 20 && Math.abs(afterPan.y - beforePan.y) < 1,
    '触摸板应支持横向平移',
  )
  await page.mouse.wheel(-140, 0)
  await pause(180)
  assert.deepEqual(interactionFailures, [], '画布交互回归')
  await page.mouse.wheel(80, 100)
  await pause(180)
  const diagonal = await viewportState()
  assert(
    diagonal.x < beforePan.x - 10 && diagonal.y < beforePan.y - 10,
    '双指斜向滑动同时平移两轴',
  )
  assert.equal(diagonal.zoom, beforePan.zoom, '普通滚动不改变缩放')
  await page.mouse.wheel(-80, -100)
  await pause(180)
  // Reduced motion preserves access while disabling displacement.
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await pause(100)
  const reducedRest = await port.boundingBox()
  await page.mouse.move(
    reducedRest.x + reducedRest.width / 2 + 30,
    reducedRest.y + reducedRest.height / 2 + 12,
  )
  await pause(200)
  const reducedMove = await port.boundingBox()
  assert(
    Math.abs(reducedMove.x - reducedRest.x) < 0.5 &&
      Math.abs(reducedMove.y - reducedRest.y) < 0.5,
  )
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  // Native Ctrl-wheel/pinch path still zooms, and magnets keep screen-pixel reach.
  // 缩放锚点放在节点右侧空白处，节点保持在视口内且不被生成面板遮住。
  await page.mouse.move(resting.x + 120, resting.y)
  await page.keyboard.down('Control')
  await page.mouse.wheel(0, 160)
  await page.keyboard.up('Control')
  await pause(350)
  assert(
    (await viewportState()).zoom < beforePan.zoom,
    'Ctrl-wheel zoom remains available',
  )
  await page.mouse.move(resting.x + 200, resting.y - 200)
  await pause(400)
  const zoomRest = await port.boundingBox()
  await page.mouse.move(
    zoomRest.x + zoomRest.width / 2 + 30,
    zoomRest.y + zoomRest.height / 2 + 12,
  )
  await pause(450)
  const zoomMove = await port.boundingBox()
  assert(
    Math.abs(zoomMove.x - zoomRest.x - (attracted.x - resting.x)) < 1,
    '吸附距离以屏幕像素计，不随画布缩放改变',
  )
  await port.focus()
  await page.keyboard.press('Enter')
  await menu.waitFor()
  const keyboardPort = await port.boundingBox(),
    keyboardMenu = await menu.boundingBox()
  assert(
    Math.abs(keyboardMenu.x - (keyboardPort.x + keyboardPort.width / 2)) < 16,
    '键盘激活使用按钮坐标',
  )
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '适应全部节点', exact: true }).click()
  await pause(200)
  await node(a).click()
  await page.locator('.cc-node.is-selected .cc-node-content').dblclick()
  await page.getByLabel('正文', { exact: true }).fill('以柔和的光线组织画面。')
  await snapshotAfter(() =>
    page.getByLabel('正文', { exact: true }).press('Control+Enter'),
  )
  await node(a).click()
  await page.getByRole('button', { name: '添加下游节点', exact: true }).click()
  await page
    .getByRole('menu', { name: '创建并连接节点' })
    .getByRole('menuitem', { name: '文字', exact: true })
    .click()
  await until(
    () => snapshot.nodes.length === 2 && snapshot.edges.length === 1,
    'port creates node and edge atomically',
  )
  await saved()
  const b = snapshot.nodes.find((n) => n.id !== a).id
  await page.getByRole('button', { name: '适应全部节点', exact: true }).click()
  await until(async () => {
    const box = await node(b).boundingBox()
    return box && box.x > 80
  }, 'node framed')
  // A body drop must connect, without hunting for the tiny target handle.
  async function disconnectAB() {
    await page.locator('.react-flow__edge').first().dispatchEvent('click')
    await snapshotAfter(
      () => page.getByRole('button', { name: '删除连线', exact: true }).click(),
      (c) => c.edges.length === 0,
    )
  }
  async function connectBody(
    source,
    target,
    side = '添加下游节点',
    near = false,
  ) {
    await node(source).locator('.cc-node-header').click()
    const handle = await node(source)
      .getByRole('button', { name: side, exact: true })
      .boundingBox()
    const body = await node(target).boundingBox()
    await page.mouse.move(
      handle.x + handle.width / 2,
      handle.y + handle.height / 2,
    )
    await page.mouse.down()
    await page.mouse.move(
      near ? body.x - 12 : body.x + body.width * 0.65,
      near ? body.y + body.height / 2 : body.y + body.height * 0.7,
      { steps: 12 },
    )
    await pause(80)
    const preview = page.locator('.react-flow__connection-path')
    const endpoint = await preview.evaluate((path) => {
      const p = path.getPointAtLength(path.getTotalLength())
      const screen = new DOMPoint(p.x, p.y).matrixTransform(path.getScreenCTM())
      return { x: screen.x, y: screen.y }
    })
    const expectedX = side === '添加下游节点' ? body.x : body.x + body.width
    const snapped =
      Math.abs(endpoint.x - expectedX) < 2 &&
      Math.abs(endpoint.y - body.y - body.height / 2) < 2
    await page.screenshot({ path: `${output}/connection-snap.png` })
    holdConnection = true
    await page.mouse.up()
    try {
      await until(() => !!releaseConnection, 'connection request held')
      await page.evaluate(
        () =>
          new Promise((resolve) =>
            requestAnimationFrame(() => requestAnimationFrame(resolve)),
          ),
      )
      assert.equal(
        await page.locator('.react-flow__edge').count(),
        1,
        '松手后的连线必须立即保留，不等待后端',
      )
      await page.locator('.react-flow__edge').first().dispatchEvent('click')
      await pause(250)
      assert.equal(await page.locator('.react-flow__edge').count(), 1)
    } finally {
      releaseConnection?.()
      releaseConnection = undefined
      await pause(150)
    }
    await until(() => snapshot.edges.length === 1, 'node body drop connects')
    assert(
      snapshot.edges.length === 1 && snapped,
      `节点主体落点应连接并吸附边缘中心；edges=${snapshot.edges.length}, snapped=${snapped}`,
    )
    await saved()
    if (!checkedPendingSelection) {
      checkedPendingSelection = true
      const count = snapshot.nodes.length,
        requests = postedCommands.length
      // 上面点过连线，选区已转到连线；重新选中节点再删除。
      await node(source).locator('.cc-node-header').click()
      await node(source).focus()
      await page.keyboard.press('Delete')
      await until(
        () => postedCommands.length > requests,
        'delete selection dispatches',
      )
      assert(
        !JSON.stringify(postedCommands.at(-1)).includes('pending:'),
        '临时连线ID不能进入正式删除命令',
      )
      await until(
        () => snapshot.nodes.length === count - 1,
        'selected node removed',
      )
      await snapshotAfter(
        () => page.getByRole('button', { name: '撤销', exact: true }).click(),
        (c) => c.nodes.length === count,
      )
    }
  }
  await disconnectAB()
  await connectBody(a, b)
  await disconnectAB()
  await connectBody(b, a, '添加上游参考')
  assert.equal(snapshot.edges[0].source_node_id, a)
  assert.equal(snapshot.edges[0].target_node_id, b)
  await disconnectAB()
  await connectBody(a, b, '添加下游节点', true)
  await verifyResizeHandles(a)
  // Shift selection, grouping, duplicate/ungroup and undo all use real commands.
  await node(a).click()
  await node(b).click({ modifiers: ['Shift'] })
  await page
    .getByRole('toolbar', { name: '节点操作' })
    .getByText('2 个节点')
    .waitFor()
  const oldA = world(snapshot, a),
    oldB = world(snapshot, b)
  await snapshotAfter(
    () => page.getByRole('button', { name: '打组', exact: true }).click(),
    (c) => c.nodes.some((n) => n.metadata.type_key === 'core.group'),
  )
  const group = snapshot.nodes.find(
    (n) => n.metadata.type_key === 'core.group',
  ).id
  assert.deepEqual(world(snapshot, a), oldA)
  assert.deepEqual(world(snapshot, b), oldB)
  await node(group).locator('.cc-node-header').click()
  const beforeMove = world(snapshot, a),
    box = await node(group).locator('.cc-node-header').boundingBox()
  await snapshotAfter(async () => {
    await page.mouse.move(box.x + 60, box.y + 18)
    await page.mouse.down()
    await page.mouse.move(box.x + 135, box.y + 63, { steps: 12 })
    await page.mouse.up()
  })
  const afterMove = world(snapshot, a)
  assert(afterMove.x > beforeMove.x + 20)
  assert.equal(world(snapshot, b).x - oldB.x, afterMove.x - oldA.x)
  // V6-004：组不能手动缩放，始终由子节点撑开（PRD CV-17）。
  await node(group).locator('.cc-node-header').click()
  assert.equal(
    await node(group).locator('.react-flow__resize-control').count(),
    0,
    'group has no resize handles',
  )
  await disconnectAB()
  await page.getByRole('button', { name: '缩小', exact: true }).click()
  await connectBody(a, b)
  await node(group).locator('.cc-node-header').click()
  await page.screenshot({ path: `${output}/grouped-desktop.png` })
  const beforeCopy = snapshot.nodes.length
  await snapshotAfter(
    () =>
      page
        .getByRole('toolbar', { name: '节点操作' })
        .getByRole('button', { name: '复制整组', exact: true })
        .click(),
    (c) => c.nodes.length === beforeCopy + 3,
  )
  assert.equal(snapshot.edges.length, 2)
  await snapshotAfter(
    () => page.getByRole('button', { name: '撤销', exact: true }).click(),
    (c) => c.nodes.length === beforeCopy,
  )
  await snapshotAfter(
    () => page.getByRole('button', { name: '重做', exact: true }).click(),
    (c) => c.nodes.length === beforeCopy + 3,
  )
  await page.reload()
  await saved()
  assert.equal(snapshot.nodes.length, beforeCopy + 3)
  const copyGroup = snapshot.nodes.find(
    (n) => n.metadata.type_key === 'core.group' && n.id !== group,
  ).id
  await page.getByRole('button', { name: '适应全部节点', exact: true }).click()
  await node(copyGroup).locator('.cc-node-header').click()
  const positions = new Map(
    snapshot.nodes.map((n) => [n.id, world(snapshot, n.id)]),
  )
  await snapshotAfter(
    () => page.getByRole('button', { name: '解组', exact: true }).click(),
    (c) => !c.nodes.some((n) => n.id === copyGroup),
  )
  for (const n of snapshot.nodes)
    assert.deepEqual(world(snapshot, n.id), positions.get(n.id))
  const versionedCopy = snapshot.nodes.find(
    (n) => n.id !== a && n.data.selected_version_id,
  )
  assert(versionedCopy)
  const copyBox = await node(versionedCopy.id).boundingBox()
  await node(versionedCopy.id).click({
    position: { x: copyBox.width - 12, y: copyBox.height - 12 },
  })
  const existingIDs = new Set(snapshot.nodes.map((n) => n.id))
  await snapshotAfter(
    () => fromMore('复制'),
    (c) => c.nodes.length === existingIDs.size + 1,
  )
  const secondCopy = snapshot.nodes.find((n) => !existingIDs.has(n.id))
  assert(secondCopy.data.selected_version_id)
  await snapshotAfter(
    () => fromMore('删除'),
    (c) => !c.nodes.some((n) => n.id === secondCopy.id),
  )
  await snapshotAfter(
    () => page.getByRole('button', { name: '撤销', exact: true }).click(),
    (c) => c.nodes.some((n) => n.id === secondCopy.id),
  )
  assert.equal(
    snapshot.nodes.find((n) => n.id === secondCopy.id).data.selected_version_id,
    secondCopy.data.selected_version_id,
  )
  await snapshotAfter(
    () => fromMore('删除'),
    (c) => !c.nodes.some((n) => n.id === secondCopy.id),
  )
  // v6 没有「连接已有节点」下拉；拖线从 b 的输出落到 a 上，形成环路，由服务端拒绝。
  await node(b).locator('.cc-node-header').click()
  const cycleFrom = await node(b)
    .getByRole('button', { name: '添加下游节点', exact: true })
    .boundingBox()
  const cycleTo = await node(a).boundingBox()
  await page.mouse.move(cycleFrom.x + cycleFrom.width / 2, cycleFrom.y + cycleFrom.height / 2)
  await page.mouse.down()
  await page.mouse.move(cycleTo.x + cycleTo.width * 0.5, cycleTo.y + cycleTo.height * 0.6, { steps: 12 })
  await page.mouse.up()
  await page
    .getByRole('button', { name: '放弃被拒绝的请求', exact: true })
    .waitFor()
  assert.equal(snapshot.edges.length, 2)
  await page
    .getByRole('button', { name: '放弃被拒绝的请求', exact: true })
    .click()
  await saved()
  await page.setViewportSize({ width: 390, height: 844 })
  if (await page.locator('#creative-library').isVisible())
    await page.getByRole('button', { name: '收起资产库', exact: true }).click()
  await fitAll(page)
  await pause(250)
  const mobilePort = node(a).getByRole('button', {
    name: '添加下游节点',
    exact: true,
  })
  await mobilePort.focus()
  await page.keyboard.press('Enter')
  await menu.waitFor()
  const mobileMenu = await menu.boundingBox()
  assert(
    mobileMenu.x >= 0 &&
      mobileMenu.x + mobileMenu.width <= 390 &&
      mobileMenu.y >= 0 &&
      mobileMenu.y + mobileMenu.height <= 844,
    '窄屏分组内节点菜单不越界',
  )
  await page.screenshot({ path: `${output}/port-menu-mobile.png` })
  await page.keyboard.press('Escape')
  const shelfToggle = page.getByRole('button', {
    name: '资产库',
    exact: true,
  })
  if ((await shelfToggle.getAttribute('aria-pressed')) !== 'true')
    await shelfToggle.click()
  await page.getByRole('button', { name: '收起资产库', exact: true }).click()
  await fitAll(page)
  await page.screenshot({ path: `${output}/grouped-mobile.png` })
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  )
  // A fixture is created only in this test server; production has no create-doc endpoint.
  const fixtureResponse = await page.request.post(`${api}/__test/document`)
  assert(fixtureResponse.ok())
  const fixture = await fixtureResponse.json()
  await page.setViewportSize({ width: 1600, height: 1000 })
  await page.goto(`${url}/creative/canvases/${fixture.canvas_id}`)
  await saved()
  await node(fixture.node_id).click()
  const viewport = await page
    .locator('.react-flow__viewport')
    .getAttribute('style')
  await page.getByRole('button', { name: '最大化文档', exact: true }).click()
  await page
    .getByRole('region', { name: '文档最大化' })
    .getByText('以光线组织空间', { exact: false })
    .waitFor()
  await page.screenshot({ path: `${output}/document-maximized.png` })
  await page.getByRole('button', { name: '还原节点', exact: true }).click()
  assert.equal(
    await page.locator('.react-flow__viewport').getAttribute('style'),
    viewport,
  )
  await snapshotAfter(
    () => fromMore('删除'),
    (c) => c.nodes.length === 0,
  )
  const referenced = await page.request.post(
    `${api}/__test/document?canvas_id=${fixture.canvas_id}&document_id=${fixture.document_id}`,
  )
  assert(referenced.ok())
  const again = await referenced.json()
  await page.reload()
  await saved()
  await node(again.node_id).click()
  await page.getByRole('button', { name: '最大化文档', exact: true }).click()
  await page.getByText('以光线组织空间', { exact: false }).waitFor()
  assert.equal(again.document_id, fixture.document_id)
  await page.getByRole('button', { name: '还原节点', exact: true }).click()
  await page.goto(canvasURL)
  await saved()
  await page.screenshot({ path: `${output}/final-desktop.png` })
  assert.deepEqual(errors, [])
  await writeFile(
    `${output}/result.json`,
    JSON.stringify(
      {
        passed: true,
        posts,
        checks: [
          'port create/connect',
          'multi selection',
          'group world positions',
          'group drag',
          'internal edges copy',
          'undo redo reopen',
          'ungroup preserves world',
          'cycle rejection',
          'mobile shelf controls',
          'document maximize preserves viewport',
          'document identity after removal',
        ],
        errors,
      },
      null,
      2,
    ),
  )
  console.log('creative canvas commands browser PASS')
} catch (error) {
  console.error('page errors', errors)
  await page.screenshot({ path: `${output}/failure.png` }).catch(() => {})
  await writeFile(
    `${output}/failure.txt`,
    String(error) +
      '\n' +
      JSON.stringify(errors) +
      '\n' +
      (await page.locator('body').innerText()),
  )
  throw error
} finally {
  releaseConnection?.()
  await context.unrouteAll({ behavior: 'wait' })
  await browser.close()
}
