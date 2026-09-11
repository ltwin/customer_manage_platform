// Browser regression for focus and local feedback. All API traffic is mocked;
// run against the Vite dev server: node scripts/creative-canvas-interaction.e2e.mjs.
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const browser = await chromium.launch({ headless: true })
const page = await browser.newPage({
  viewport: { width: 1600, height: 1000 },
})
page.setDefaultTimeout(8000)
const errors = []
page.on('pageerror', (error) => errors.push(error.message))
const makeNode = (id, x) => ({
  id,
  parent_id: null,
  metadata: {
    type_key: 'core.text',
    type_version: 1,
    title: id,
    intent: '',
    x,
    y: 150,
    width: 280,
    height: 180,
    z_order: 0,
  },
  prompt: null,
  capabilities: {
    actions: [
      'move',
      'resize',
      'rename',
      'duplicate',
      'remove',
      'edit',
      'reference',
    ],
    prompt_mode: 'draft',
    disabled_reason: null,
  },
  status: {
    content_state: 'empty',
    generation_state: 'idle',
    active_execution_id: null,
    latest_execution_id: null,
    apply_state: null,
    error: null,
    status_revision: '1',
  },
  placement_revision: '1',
  data_revision: '1',
  data: {
    schema_version: 1,
    config: {},
    content_id: null,
    content_revision_id: null,
    selected_version_id: null,
    document_id: null,
  },
})
const canvas = {
  id: 'qa',
  project_id: 'qa-project',
  project_name: '交互回归',
  project_revision: '1',
  revision: '1',
  topology_revision: '1',
  archived: false,
  nodes: [makeNode('first', 100), makeNode('second', 650)],
  edges: [{
    id: 'qa-reference', source_node_id: 'first', target_node_id: 'second',
    source_port: 'output', target_port: 'reference', role: 'reference',
    ordinal: 0, revision: '1',
  }],
  node_inputs: [],
  changes: [],
  object_states: [],
}
let snapshotReads = 0
await page.addInitScript(() => {
  const original = window.fetch.bind(window)
  window.__canvasStreams = []
  window.fetch = async (input, init) => {
    if (!String(input).endsWith('/canvases/qa/events')) return original(input, init)
    const connection = { closed: false, auth: new Headers(init?.headers).get('Authorization') }
    window.__canvasStreams.push(connection)
    return new Response(new ReadableStream({
      start(controller) {
        connection.invalidate = () => controller.enqueue(new TextEncoder().encode('event: invalidate\ndata: {}\n\n'))
        connection.invalidate()
        init.signal.addEventListener('abort', () => {
          connection.closed = true
          controller.error(new DOMException('Aborted', 'AbortError'))
        }, { once: true })
      },
    }), { headers: { 'Content-Type': 'text/event-stream' } })
  }
})
let qaAccount = 'qa-account'
let hoverVideoURL = ''
let posts = 0
let started
const requestStarted = new Promise((resolve) => { started = resolve })
await page.route('**/api/v1/**', async (route) => {
  const path = new URL(route.request().url()).pathname
  const json = (value) => route.fulfill({ json: value })
  if (path.endsWith('/auth/refresh'))
    return json({
      access_token: 'qa-mock-token',
      token_type: 'Bearer',
      expires_in: 600,
    })
  if (path.endsWith('/media-access-tickets')) {
    const body = route.request().postDataJSON()
    if (body?.content_revision_id === 'qa-video' && hoverVideoURL)
      return json({ url: hoverVideoURL, expires_at: new Date(Date.now() + 3600000).toISOString() })
    const art = '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="640" viewBox="0 0 640 640"><rect width="640" height="640" fill="#deded3"/><circle cx="445" cy="165" r="70" fill="#d79768"/><path d="M0 430L170 190L365 455L480 300L640 455V640H0Z" fill="#748675"/><path d="M0 510L225 360L425 580L640 420V640H0Z" fill="#364d43"/></svg>'
    return json({ url: `data:image/svg+xml,${encodeURIComponent(art)}`, expires_at: new Date(Date.now() + 3600000).toISOString() })
  }
  if (path.endsWith('/me'))
    return json({
      id: qaAccount,
      email: 'qa@example.test',
      created_at: '2026-01-01T00:00:00Z',
      timezone: 'Asia/Shanghai',
    })
  if (path.endsWith('/commands')) {
    posts++
    started()
    // Leave the command unanswered: visible feedback cannot come from a receipt.
    await new Promise((resolve) => setTimeout(resolve, 1500))
    return route.fulfill({
      status: 503,
      json: {
        error: { code: 'unavailable', message: 'QA delayed transport' },
      },
    })
  }
  if (path.endsWith('/canvases/qa')) { snapshotReads++; return json(canvas) }
  if (path.endsWith('/library-settings'))
    return json({
      revision: '1',
      view_mode: 'grid',
      sort: 'updated_desc',
      thumbnail_size: 'medium',
    })
  if (path.endsWith('/media-capabilities'))
    return json({ formats: [
      { kind: 'image', mime: 'image/png', extensions: ['.png'] },
      { kind: 'video', mime: 'video/mp4', extensions: ['.mp4'] },
      { kind: 'audio', mime: 'audio/mpeg', extensions: ['.mp3'] },
    ], image_max_bytes: 1000000, av_max_bytes: 10000000, batch_limit: 10 })
  return json({
    items: [],
    next_cursor: '',
    total_count: 0,
    library_revision: '1',
  })
})
async function waitCount(selector, count) {
  await page.waitForFunction(
    ({ selector, count }) =>
      document.querySelectorAll(selector).length === count,
    { selector, count },
    { timeout: 1200 },
  )
}
const nodes = '.react-flow__node'
const group = '.cc-group-node'
const metrics = {}
try {
  await page.goto(
    `${process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'}/creative/canvases/qa`,
  )
  await waitCount(nodes, 2)
  // Offline prevents the debounce timer winning the local undo tests.
  await page.waitForTimeout(300)
  const idleReads = snapshotReads
  await page.waitForTimeout(5500)
  assert.equal(snapshotReads, idleReads, 'idle canvas must not poll snapshots')
  assert.equal(await page.evaluate(() => window.__canvasStreams.filter((stream) => !stream.closed).length), 1)
  assert.match(await page.evaluate(() => window.__canvasStreams.at(-1).auth), /^Bearer /)
  canvas.nodes[0].metadata.title = 'SSE 远端更新'
  canvas.nodes[0].placement_revision = '2'
  canvas.revision = '2'
  await page.evaluate(() => window.__canvasStreams.at(-1).invalidate())
  await page.getByText('SSE 远端更新', { exact: true }).waitFor()
  assert.equal(snapshotReads, idleReads + 1, 'one SSE invalidation fetches the new snapshot')
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => false })
    window.dispatchEvent(new Event('offline'))
  })
  await page.getByRole('button', { name: '新建节点', exact: true }).click()
  const createMenu = page.getByRole('menu', { name: '新建节点' })
  assert.equal(await createMenu.getByRole('menuitem').count(), 4)
  assert.equal(await createMenu.getByRole('menuitem', { name: '新增链接节点' }).count(), 0)
  await page.getByRole('button', { name: '关闭新建菜单' }).click()
  await page.locator('[data-id="first"] .cc-node-header').click()
  await page.locator('[data-id="first"]').getByRole('button', { name: '添加下游节点' }).click()
  const connectedMenu = page.getByRole('dialog', { name: '创建并连接节点' })
  assert.equal(await connectedMenu.getByRole('button', { name: '链接', exact: true }).count(), 0)
  await connectedMenu.getByRole('button', { name: '文字', exact: true }).waitFor()
  await page.getByRole('button', { name: '关闭连接菜单' }).click()
  assert.equal(await page.getByRole('button', { name: '编辑节点', exact: true }).count(), 0)
  await page.locator('[data-id="first"] .cc-node-content').dblclick()
  const textEditor = page.getByRole('textbox', { name: '正文', exact: true })
  assert.equal(await textEditor.getAttribute('placeholder'), '输入文字或粘贴链接')
  await textEditor.fill('参考资料：https://example.com/reference')
  await textEditor.press('Control+Enter')
  await page.locator('[data-id="first"]').getByText('参考资料：https://example.com/reference', { exact: true }).waitFor()
  assert.equal(posts, 0)
  await page.keyboard.press('Control+z')
  await waitCount('[data-id="first"] .cc-node-empty', 1)
  // Title and body edit in place, including IME, cancellation and native undo.
  const firstTitle = page.locator('[data-id="first"] .cc-node-header h3')
  const titleBefore = await firstTitle.textContent()
  await firstTitle.dblclick()
  const titleInput = page.getByRole('textbox', { name: '节点名称', exact: true })
  await titleInput.waitFor()
  assert.equal(await page.getByRole('dialog').count(), 0, 'rename has no dialog')
  await titleInput.fill('就地改名')
  assert.ok(await titleInput.evaluate((input) => input.scrollWidth <= input.clientWidth), 'short Chinese title fits input width')
  await page.screenshot({ path: '/tmp/canvas-inline-title.png' })
  await titleInput.dispatchEvent('keydown', { key: 'Enter', isComposing: true })
  assert.equal(await titleInput.count(), 1, 'IME Enter does not submit title')
  await titleInput.press('Enter')
  await firstTitle.filter({ hasText: '就地改名' }).waitFor()
  await page.keyboard.press('Control+z')
  await firstTitle.filter({ hasText: titleBefore }).waitFor()
  await firstTitle.dblclick()
  await titleInput.fill('不保存标题')
  await titleInput.press('Escape')
  assert.equal(await firstTitle.textContent(), titleBefore)
  await firstTitle.dblclick()
  await titleInput.fill('失焦改名')
  await page.mouse.click(1400, 850)
  await firstTitle.filter({ hasText: '失焦改名' }).waitFor()
  await page.keyboard.press('Control+z')
  await firstTitle.filter({ hasText: titleBefore }).waitFor()
  const bodyCard = page.locator('[data-id="first"] .cc-node-content')
  const bodyBounds = await bodyCard.boundingBox()
  await bodyCard.dblclick()
  await textEditor.waitFor()
  assert.equal(await page.getByRole('dialog').count(), 0, 'body editor is in canvas')
  const editorBounds = await textEditor.boundingBox()
  assert.ok(Math.abs(bodyBounds.width - editorBounds.width) < 2 && Math.abs(bodyBounds.height - editorBounds.height) < 2, 'inline editor preserves node geometry')
  await textEditor.fill('第一行')
  await textEditor.press('End')
  await textEditor.press('Enter')
  await textEditor.pressSequentially('second')
  assert.equal(await textEditor.inputValue(), '第一行\nsecond')
  await textEditor.press('Control+z')
  assert.equal(await page.locator(nodes).count(), 2, 'native text undo cannot undo a canvas action')
  await textEditor.fill('第一行\n第二行')
  await textEditor.dispatchEvent('keydown', { key: 'Enter', ctrlKey: true, isComposing: true })
  assert.equal(await textEditor.count(), 1, 'IME Enter does not submit content')
  await page.screenshot({ path: '/tmp/canvas-inline-text.png' })
  await page.mouse.click(1400, 850)
  await bodyCard.getByText('第一行\n第二行', { exact: true }).waitFor()
  await page.keyboard.press('Control+z')
  await waitCount('[data-id="first"] .cc-node-empty', 1)
  await bodyCard.dblclick()
  assert.equal(await textEditor.inputValue(), '', 'undo does not restore stale saved draft into editor')
  await textEditor.fill('不保存正文')
  await textEditor.press('Escape')
  await waitCount('[data-id="first"] .cc-node-empty', 1)
  const blankContextPoint = { x: 1520, y: 870 }
  await page.mouse.click(blankContextPoint.x, blankContextPoint.y, { button: 'right' })
  const canvasMenu = page.getByRole('menu', { name: '画布右键菜单' })
  await canvasMenu.waitFor()
  const menuBounds = await canvasMenu.boundingBox()
  assert.ok(menuBounds.x >= 0 && menuBounds.x + menuBounds.width <= 1600 && menuBounds.y + menuBounds.height <= 1000, 'context menu stays inside viewport')
  assert.equal(await canvasMenu.getByRole('menuitem', { name: '新增链接节点' }).count(), 0)
  await page.keyboard.press('ArrowDown')
  assert.equal(await page.evaluate(() => document.activeElement.textContent), '新增图片节点')
  await page.keyboard.press('Escape')
  await canvasMenu.waitFor({ state: 'hidden' })
  await page.mouse.click(blankContextPoint.x, blankContextPoint.y, { button: 'right' })
  await canvasMenu.getByRole('menuitem', { name: '新增文字节点', exact: true }).click()
  await waitCount(nodes, 3)
  const createdBox = await page.locator('.react-flow__node').last().boundingBox()
  assert.ok(Math.abs(createdBox.x - blankContextPoint.x) < 2 && Math.abs(createdBox.y - blankContextPoint.y) < 2, 'new node appears at right-click location')
  await page.keyboard.press('Control+z')
  await waitCount(nodes, 2)
  const firstCard = page.locator('[data-id="first"] .cc-node-empty')
  const secondCard = page.locator('[data-id="second"] .cc-node-empty')
  await firstCard.click()
  await secondCard.click({ button: 'right' })
  const nodeMenu = page.getByRole('menu', { name: '节点右键菜单' })
  await nodeMenu.waitFor()
  assert.equal(await page.locator('[data-id="first"] .cc-node.is-selected').count(), 0, 'right click selects the target node')
  await nodeMenu.getByRole('menuitem', { name: '重命名', exact: true }).click()
  await page.getByRole('textbox', { name: '节点名称' }).fill('右键改名')
  await page.getByRole('textbox', { name: '节点名称' }).press('Enter')
  await page.locator('[data-id="second"] h3').filter({ hasText: '右键改名' }).waitFor()
  await page.keyboard.press('Control+z')
  await page.locator('[data-id="second"] h3').filter({ hasText: 'second' }).waitFor()
  await firstCard.click()
  await secondCard.click({ modifiers: ['Shift'] })
  await waitCount('.react-flow__node.selected', 2)
  await firstCard.click({ button: 'right' })
  await nodeMenu.getByRole('menuitem', { name: '复制选中节点', exact: true }).waitFor()
  assert.equal(await page.locator('.cc-node.is-selected').count(), 2, 'right-click within selection preserves it')
  await nodeMenu.getByRole('menuitem', { name: '复制选中节点', exact: true }).click()
  await waitCount(nodes, 4)
  await page.keyboard.press('Control+z')
  await waitCount(nodes, 2)
  await firstCard.click({ button: 'right' })
  await page.mouse.click(1300, 750)
  assert.equal(await nodeMenu.count(), 0, 'outside click dismisses menu')
  const scissors = page.getByRole('button', { name: '删除连线', exact: true })
  const edge = page.locator('.react-flow__edge[data-id="qa-reference"] .react-flow__edge-path')
  const midpoint = () => edge.evaluate((path) => {
    const point = path.getPointAtLength(path.getTotalLength() / 2)
    const screen = new DOMPoint(point.x, point.y).matrixTransform(path.getScreenCTM())
    return { x: screen.x, y: screen.y }
  })
  const clickEdge = async () => {
    const point = await midpoint()
    await page.mouse.click(point.x, point.y)
  }
  await page.locator('[data-id="first"] .cc-node-header').click()
  assert.equal(await scissors.count(), 0, 'node highlight must not expose scissors')
  const edgePoint = await midpoint()
  await page.mouse.click(edgePoint.x, edgePoint.y, { button: 'right' })
  const edgeMenu = page.getByRole('menu', { name: '连线右键菜单' })
  await edgeMenu.getByRole('menuitem', { name: '删除连线', exact: true }).click()
  await waitCount('.react-flow__edge[data-id="qa-reference"]', 0)
  await page.keyboard.press('Control+z')
  await waitCount('.react-flow__edge[data-id="qa-reference"]', 1)
  await clickEdge()
  await scissors.waitFor()
  assert.equal(await page.locator('.cc-edge-tools').count(), 0, 'old corner panel is removed')
  const assertCutPosition = async () => {
    const point = await midpoint()
    const box = await scissors.boundingBox()
    assert.ok(box && Math.abs(box.x + box.width / 2 - point.x) < 2 && Math.abs(box.y + box.height / 2 - point.y) < 2, 'scissors follows the edge midpoint')
    assert.ok(Math.abs(box.width - 32) < 1, 'scissors remains 32 screen pixels at any zoom')
  }
  await assertCutPosition()
  await page.getByRole('button', { name: '缩小', exact: true }).click()
  await page.waitForTimeout(350)
  await assertCutPosition()
  await page.screenshot({ path: '/tmp/canvas-edge-cut.png' })
  await page.locator('[data-id="first"] .cc-node-header').click()
  assert.equal(await scissors.count(), 0, 'selecting a node hides scissors')
  await clickEdge()
  await scissors.click()
  await waitCount('.react-flow__edge[data-id="qa-reference"]', 0)
  assert.equal(posts, 0, 'disconnect appears immediately before network')
  await page.keyboard.press('Control+z')
  await waitCount('.react-flow__edge[data-id="qa-reference"]', 1)
  assert.equal(await scissors.count(), 0, 'undo restores the edge without stale selection')
  await page.locator('[data-id="first"] .cc-node-header').click()
  const nodeTitle = await page.locator('[data-id="first"] .cc-node-header').boundingBox()
  const nodeCard = await page.locator('[data-id="first"] .cc-node').boundingBox()
  const titleBlank = { x: nodeCard.x + nodeCard.width - 12, y: nodeTitle.y + nodeTitle.height / 2 }
  assert.ok(nodeTitle.x + nodeTitle.width < titleBlank.x, 'short title hit area ends with its text')
  await page.mouse.click(titleBlank.x, titleBlank.y)
  assert.equal(await page.getByRole('toolbar', { name: '节点操作' }).count(), 0, 'blank beside standalone title hits canvas')
  await page.locator('[data-id="first"] .cc-node-header h3').click()
  let start = Date.now()
  await page.getByRole('button', { name: '打组', exact: true }).click()
  await waitCount(group, 1)
  metrics.group_ms = Date.now() - start
  const groupBox = await page.locator(group).boundingBox()
  const groupTitleBox = await page.locator(`${group} .cc-node-header`).boundingBox()
  const groupToolbarBox = await page.getByRole('toolbar', { name: '节点操作' }).boundingBox()
  assert.ok(groupTitleBox.y + groupTitleBox.height < groupBox.y, 'group title is outside its border')
  assert.ok(groupToolbarBox.y + groupToolbarBox.height < groupTitleBox.y, 'group toolbar leaves room for title')
  assert.equal(await page.locator(`${group} .cc-node-header .lucide-folder`).count(), 1)
  await page.locator(`${group} .cc-node-header h3`).dblclick()
  await titleInput.fill('临时组名')
  await titleInput.press('Escape')
  assert.equal(await page.getByRole('dialog').count(), 0, 'group name edits in place')
  assert.equal(await page.getByRole('button', { name: '打组', exact: true }).count(), 0)
  assert.equal(await page.getByRole('button', { name: '解组', exact: true }).locator('.lucide-folder-minus').count(), 1)
  await page.locator('[data-id="first"] .cc-node-empty').click()
  assert.equal(await page.getByRole('button', { name: '解组', exact: true }).count(), 0, 'child node wins hit testing inside a group')
  assert.equal(await page.getByRole('button', { name: '打组', exact: true }).count(), 1, 'normal node still offers grouping')
  const childTitle = await page.locator('[data-id="first"] .cc-node-header').boundingBox()
  const childCard = await page.locator('[data-id="first"] .cc-node').boundingBox()
  await page.mouse.click(childCard.x + childCard.width - 12, childTitle.y + childTitle.height / 2)
  await page.getByRole('button', { name: '解组', exact: true }).waitFor()
  assert.equal(await page.locator('[data-id="first"] .cc-node.is-selected').count(), 0, 'blank beside child title selects parent group')
  await page.locator('[data-id="first"] .cc-node-header h3').click()
  assert.equal(await page.getByRole('button', { name: '解组', exact: true }).count(), 0, 'visible title still selects child')
  const childBeforeDrag = await page.locator('[data-id="first"] .cc-node').boundingBox()
  await page.mouse.move(childBeforeDrag.x + childBeforeDrag.width / 2, childBeforeDrag.y + childBeforeDrag.height / 2)
  await page.mouse.down()
  await page.mouse.move(childBeforeDrag.x + childBeforeDrag.width / 2 + 10, childBeforeDrag.y + childBeforeDrag.height / 2 + 8, { steps: 6 })
  await page.mouse.up()
  const childAfterDrag = await page.locator('[data-id="first"] .cc-node').boundingBox()
  assert.ok(childAfterDrag.x > childBeforeDrag.x + 5, 'child remains draggable inside a group')
  const groupAfterDrag = await page.locator(group).boundingBox()
  assert.ok(Math.abs(groupAfterDrag.x - groupBox.x) < 1, 'dragging a child does not drag its group')
  await page.keyboard.press('Control+z')
  await page.waitForFunction(({ x }) => Math.abs(document.querySelector('[data-id="first"] .cc-node').getBoundingClientRect().x - x) < 1, { x: childBeforeDrag.x })

  // Start from a group that is not selected: dragging its blank area selects
  // and moves it, while preserving the child's relative position.
  const blank = { x: groupBox.x + groupBox.width - 8, y: groupBox.y + groupBox.height - 8 }
  await page.mouse.move(blank.x, blank.y)
  await page.mouse.down()
  await page.mouse.move(blank.x + 48, blank.y + 32, { steps: 8 })
  await page.mouse.up()
  const movedGroup = await page.locator(group).boundingBox()
  const movedChild = await page.locator('[data-id="first"] .cc-node').boundingBox()
  assert.ok(movedGroup.x - groupBox.x > 30 && movedGroup.y - groupBox.y > 20, 'blank-area drag moves the group')
  assert.ok(Math.abs((movedChild.x - movedGroup.x) - (childBeforeDrag.x - groupBox.x)) < 1 && Math.abs((movedChild.y - movedGroup.y) - (childBeforeDrag.y - groupBox.y)) < 1, 'group drag preserves child placement')
  await page.keyboard.press('Control+z')
  await page.waitForFunction(({ x, y }) => {
    const box = document.querySelector('.cc-group-node').getBoundingClientRect()
    return Math.abs(box.x - x) < 1 && Math.abs(box.y - y) < 1
  }, { x: groupBox.x, y: groupBox.y })
  const restoredChild = await page.locator('[data-id="first"] .cc-node').boundingBox()
  assert.ok(Math.abs(restoredChild.x - childBeforeDrag.x) < 1 && Math.abs(restoredChild.y - childBeforeDrag.y) < 1, 'undo restores group and child together')
  await page.mouse.click(groupBox.x + groupBox.width - 8, groupBox.y + groupBox.height - 8)
  await page.getByRole('button', { name: '解组', exact: true }).waitFor()
  assert.equal(await page.getByRole('button', { name: '打组', exact: true }).count(), 0, 'group blank area selects the group')
  await page.keyboard.press('Control+g')
  assert.equal(await page.locator(group).count(), 1, 'single group cannot be nested through shortcut')
  await page.screenshot({ path: '/tmp/canvas-group-folder.png' })
  await page.mouse.click(groupBox.x + groupBox.width - 8, groupBox.y + groupBox.height - 8, { button: 'right' })
  await nodeMenu.getByRole('menuitem', { name: '解组', exact: true }).waitFor()
  assert.equal(await nodeMenu.getByRole('menuitem', { name: '打组', exact: true }).count(), 0, 'single group context menu only offers ungroup')
  await page.screenshot({ path: '/tmp/canvas-context-menu.png' })
  await nodeMenu.getByRole('menuitem', { name: '解组', exact: true }).click()
  await waitCount(group, 0)
  await page.keyboard.press('Control+z')
  await waitCount(group, 1)
  assert.equal(posts, 0)
  // Toolbar focus was the original failure: do not focus the canvas manually.
  await page
    .getByRole('button', { name: '重做', exact: true })
    .evaluate((el) => el.blur())
  await page.getByRole('button', { name: '撤销', exact: true }).focus()
  await page.keyboard.press('Control+z')
  await waitCount(group, 0)
  await page.keyboard.press('Control+Shift+z')
  await waitCount(group, 1)
  await page.keyboard.press('Meta+z')
  await waitCount(group, 0)
  await page.keyboard.press('Meta+Shift+z')
  await waitCount(group, 1)
  await page.keyboard.press('Control+z')
  await waitCount(group, 0)
  // Copy and undo from document focus, with no canvas key handler in the path.
  await page.locator('[data-id="first"] .cc-node-header').click()
  start = Date.now()
  await page.getByRole('button', { name: '复制节点', exact: true }).click()
  await waitCount(nodes, 3)
  metrics.copy_ms = Date.now() - start
  await page.evaluate(() => document.activeElement?.blur())
  await page.keyboard.press('Meta+z')
  await waitCount(nodes, 2)
  await page.keyboard.press('Control+y')
  await waitCount(nodes, 3)
  // Input undo stays native, so it cannot remove a canvas node.
  await page.evaluate(() => {
    const input = document.createElement('input')
    input.id = 'qa-input'
    document.body.append(input)
    input.focus()
  })
  await page.keyboard.type('draft')
  await page.keyboard.press('Control+z')
  assert.equal(await page.locator(nodes).count(), 3)
  await page.evaluate(() => document.querySelector('#qa-input').remove())
  // Unavailable redo must not cancel the pending copy.
  await page.keyboard.press('Meta+Shift+z')
  assert.equal(await page.locator(nodes).count(), 3)
  assert.equal(posts, 0)
  await page.waitForTimeout(350)
  assert.equal(posts, 0, 'offline edits stay local beyond the debounce window')
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => true })
    window.dispatchEvent(new Event('online'))
  })
  await requestStarted
  // A second copy remains immediate even with the first request in flight.
  await page.locator('[data-id^="pending:"] .cc-node-header').click()
  start = Date.now()
  await page.getByRole('button', { name: '复制节点', exact: true }).click()
  await waitCount(nodes, 4)
  metrics.copy_while_saving_ms = Date.now() - start
  assert.equal(posts, 1, 'the dependent copy must wait behind the held request')
  assert.deepEqual(errors, [])
  // Fresh account avoids the intentionally held save from the previous fixture.
  qaAccount = 'qa-readonly-account'
  canvas.archived = true
  canvas.revision = '20'
  await page.reload()
  await waitCount(nodes, 2)
  await page.locator('[data-id="first"] .cc-node').click({ button: 'right' })
  const readonlyMenu = page.getByRole('menu', { name: '节点右键菜单' })
  assert.equal(await readonlyMenu.getByRole('menuitem', { name: '复制节点', exact: true }).isDisabled(), true)
  assert.equal(await readonlyMenu.getByRole('menuitem', { name: '移除节点', exact: true }).isDisabled(), true)
  await page.keyboard.press('Escape')
  await page.mouse.click(blankContextPoint.x, blankContextPoint.y, { button: 'right' })
  assert.equal(await page.getByRole('menu', { name: '画布右键菜单' }).getByRole('menuitem', { name: '新增文字节点', exact: true }).isDisabled(), true)
  await page.keyboard.press('Escape')
  await page.locator('[data-id="first"] .cc-node-header h3').dblclick()
  assert.equal(await page.getByRole('textbox', { name: '节点名称', exact: true }).count(), 0, 'readonly titles cannot edit')
  await page.locator('[data-id="first"] .cc-node-content').dblclick()
  assert.equal(await textEditor.count(), 0, 'readonly content cannot edit')
  // Truncated preview must load the full revision; remote changes keep the
  // active draft and require the existing conflict confirmation on blur.
  qaAccount = 'qa-inline-fulltext-account'
  canvas.archived = false
  const fullText = '完整正文\n' + '长内容'.repeat(1000)
  const longNode = makeNode('long-text', 400)
  longNode.content = { id: 'long-rev', kind: 'text', payload: { body: '截断预览' }, truncated: true }
  longNode.data.content_revision_id = 'long-rev'
  canvas.nodes = [longNode, makeNode('other-text', 900)]
  canvas.edges = []
  await page.route('**/content-revisions/long-rev', (route) => route.fulfill({ json: { id: 'long-rev', kind: 'text', payload: { body: fullText }, truncated: false } }))
  await page.reload()
  await waitCount(nodes, 2)
  await page.locator('[data-id="long-text"] .cc-node-content').dblclick()
  await textEditor.waitFor()
  assert.equal(await textEditor.inputValue(), fullText, 'editing loads the full content revision')
  await textEditor.fill('本机保留的正文')
  canvas.nodes[0].data_revision = '2'
  canvas.revision = '21'
  await page.evaluate(() => window.__canvasStreams.at(-1).invalidate())
  await page.waitForTimeout(250)
  assert.equal(await textEditor.inputValue(), '本机保留的正文', 'remote snapshot cannot replace active input')
  await page.locator('[data-id="other-text"] .cc-node-header').click()
  const conflict = page.getByRole('alertdialog', { name: '节点已有新的内容' })
  await conflict.waitFor()
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => false })
    window.dispatchEvent(new Event('offline'))
  })
  await conflict.getByRole('button', { name: '将草稿应用到当前版本' }).click()
  await page.locator('[data-id="long-text"] .cc-node-content').getByText('本机保留的正文', { exact: true }).waitFor()
  assert.equal(await page.locator('[data-id="other-text"] .cc-node-empty').count(), 1, 'blur save targets the edited node after selection changes')
  // A fresh media fixture checks the actual picker and guards, bypassing
  // accept with setInputFiles to exercise validation before any request.
  qaAccount = 'qa-media-upload-account'
  canvas.archived = false
  canvas.edges = []
  canvas.nodes = ['image', 'video', 'audio'].map((kind, i) => {
    const n = makeNode(`${kind}-upload`, 100 + i * 360)
    n.metadata.type_key = `core.${kind}`
    n.capabilities.actions.push('replace')
    return n
  })
  await page.reload()
  await waitCount(nodes, 3)
  await page.getByRole('button', { name: '收起资产库', exact: true }).click()
  await page.waitForTimeout(350)
  await page.getByRole('button', { name: '适应全部节点', exact: true }).click()
  await page.waitForTimeout(350)
  let choosers = 0
  const countChooser = () => { choosers++ }
  page.on('filechooser', countChooser)
  let uploadRequests = 0
  await page.route('**/api/v1/creative/uploads', (route) => {
    uploadRequests++
    return route.fulfill({ status: 422, json: { error: { code: 'qa', message: 'QA upload stop' } } })
  })
  for (const [kind, ext, mime, otherName, otherMime] of [
    ['image', '.png', 'image/png', 'audio.mp3', 'audio/mpeg'],
    ['video', '.mp4', 'video/mp4', 'image.png', 'image/png'],
    ['audio', '.mp3', 'audio/mpeg', 'video.mp4', 'video/mp4'],
  ]) {
    const card = page.locator(`[data-id="${kind}-upload"] .cc-media-empty`)
    const before = choosers
    await card.click()
    await card.dblclick()
    await page.waitForTimeout(150)
    assert.equal(choosers, before, 'empty media click/double-click never opens picker')
    assert.equal(await page.locator(`[data-id="${kind}-upload"].selected`).count(), 1)
    const toolbarUpload = page.getByRole('button', { name: '上传媒体', exact: true })
    await Promise.all([page.waitForEvent('filechooser'), toolbarUpload.click()])
    const input = page.getByLabel('选择节点媒体文件')
    assert.equal(await input.getAttribute('accept'), `${mime},${ext}`)
    const requestsBefore = uploadRequests
    await input.setInputFiles({ name: otherName, mimeType: otherMime, buffer: Buffer.from('wrong kind') })
    await page.getByRole('alert').filter({ hasText: '请选择' }).waitFor()
    assert.equal(uploadRequests, requestsBefore, 'wrong media kind rejected before network')
    assert.equal(await toolbarUpload.isEnabled(), true, 'validation error does not lock canvas')
    await Promise.all([page.waitForEvent('filechooser'), toolbarUpload.click()])
    const uploadResponse = page.waitForResponse((response) => response.url().endsWith('/creative/uploads'))
    await input.setInputFiles({ name: `valid${ext}`, mimeType: mime, buffer: Buffer.from('mock valid media') })
    await uploadResponse
    assert.equal(uploadRequests, requestsBefore + 1, 'matching kind proceeds to upload')
    assert.equal(await page.getByRole('alert').filter({ hasText: '请选择' }).count(), 0)
  }
  page.off('filechooser', countChooser)
  // Canvas video nodes preview on hover and never swallow the pointer: the
  // surface plays muted while hovered, pauses when the pointer leaves, and
  // dragging starting on the video moves the node.
  hoverVideoURL = await page.evaluate(() => new Promise((resolve) => {
    const canvas = document.createElement('canvas')
    canvas.width = 64
    canvas.height = 48
    const ctx = canvas.getContext('2d')
    let frame = 0
    const timer = setInterval(() => {
      ctx.fillStyle = frame % 2 ? '#20344a' : '#4a3420'
      ctx.fillRect(0, 0, 64, 48)
      frame++
    }, 100)
    const recorder = new MediaRecorder(canvas.captureStream(10), { mimeType: 'video/webm' })
    const chunks = []
    recorder.ondataavailable = (event) => chunks.push(event.data)
    recorder.onstop = () => {
      clearInterval(timer)
      const reader = new FileReader()
      reader.onload = () => resolve(reader.result)
      reader.readAsDataURL(new Blob(chunks, { type: 'video/webm' }))
    }
    recorder.start()
    setTimeout(() => recorder.stop(), 1500)
  }))
  qaAccount = 'qa-video-hover-account'
  canvas.archived = false
  canvas.edges = []
  const hoverNode = makeNode('video-hover', 200)
  hoverNode.metadata.type_key = 'core.video'
  hoverNode.content = {
    id: 'qa-video',
    kind: 'video',
    payload: {},
    media: [{ role: 'original', mime: 'video/webm' }],
  }
  canvas.nodes = [hoverNode]
  await page.reload()
  await waitCount(nodes, 1)
  await page.getByRole('button', { name: '收起资产库', exact: true }).click()
  await page.waitForTimeout(350)
  await page.getByRole('button', { name: '适应全部节点', exact: true }).click()
  await page.waitForTimeout(350)
  const hoverVideo = page.locator('[data-id="video-hover"] video')
  await hoverVideo.waitFor()
  assert.notEqual(await hoverVideo.getAttribute('controls'), null, 'canvas video keeps the native play button and progress bar')
  assert.equal(await hoverVideo.evaluate((v) => v.muted), false, 'hover preview keeps sound; muting stays a user choice')
  await hoverVideo.hover()
  await page.waitForFunction(() => {
    const v = document.querySelector('[data-id="video-hover"] video')
    return v && !v.paused && v.currentTime > 0
  })
  // A manual pause holds while the pointer stays inside the node; only a
  // fresh enter resumes playback.
  await hoverVideo.evaluate((v) => v.pause())
  const videoBox = await hoverVideo.boundingBox()
  await page.mouse.move(videoBox.x + videoBox.width * 0.4, videoBox.y + videoBox.height * 0.25)
  await page.waitForTimeout(150)
  assert.equal(await hoverVideo.evaluate((v) => v.paused), true, 'moving inside the node never overrides a manual pause')
  await page.mouse.move(16, 16)
  assert.equal(await hoverVideo.evaluate((v) => v.paused), true, 'leaving the node pauses the preview')
  await page.mouse.move(videoBox.x + videoBox.width / 2, videoBox.y + videoBox.height / 2)
  await page.waitForFunction(() => {
    const v = document.querySelector('[data-id="video-hover"] video')
    return v && !v.paused
  })
  // The video surface drags the node; the control-bar strip scrubs instead.
  const videoCard = page.locator('[data-id="video-hover"] .cc-node')
  const videoBeforeDrag = await videoCard.boundingBox()
  await page.mouse.down()
  await page.mouse.move(videoBeforeDrag.x + videoBeforeDrag.width / 2 + 120, videoBeforeDrag.y + videoBeforeDrag.height / 2 + 70, { steps: 6 })
  await page.mouse.up()
  const videoAfterDrag = await videoCard.boundingBox()
  assert.ok(videoAfterDrag.x - videoBeforeDrag.x > 60 && videoAfterDrag.y - videoBeforeDrag.y > 30, 'dragging on the video surface moves the node')
  const stripBox = await hoverVideo.boundingBox()
  await page.mouse.move(stripBox.x + stripBox.width / 2, stripBox.y + stripBox.height - 10)
  await page.mouse.down()
  await page.mouse.move(stripBox.x + stripBox.width / 2 + 70, stripBox.y + stripBox.height - 10, { steps: 4 })
  await page.mouse.up()
  const videoAfterScrub = await videoCard.boundingBox()
  assert.ok(Math.abs(videoAfterScrub.x - videoAfterDrag.x) < 1 && Math.abs(videoAfterScrub.y - videoAfterDrag.y) < 1, 'control-bar presses scrub instead of dragging the node')
  assert.deepEqual(errors, [])
  if (process.env.CREATIVE_NODE_STYLE_QA === '1') {
    const mediaNode = (id, x, kind, content) => {
      const n = makeNode(id, x)
      n.metadata = { ...n.metadata, type_key: `core.${kind}`, title: id === 'image-ready' ? '山间光影' : id === 'image-empty' ? '图片' : '视频', y: 150, width: 280, height: 280 }
      n.content = content
      n.capabilities.actions.push('replace', 'maximize', 'download')
      return n
    }
    canvas.archived = false
    canvas.nodes = [
      mediaNode('image-ready', 100, 'image', { id: 'qa-image', kind: 'image', payload: {}, media: [{ role: 'display', mime: 'image/svg+xml' }] }),
      mediaNode('image-empty', 460, 'image'),
      mediaNode('video-empty', 820, 'video'),
    ]
    canvas.edges = []
    canvas.revision = '10'
    qaAccount = 'qa-style-account'
    await page.reload()
    await waitCount(nodes, 3)
    await page.getByRole('button', { name: '收起资产库', exact: true }).click()
    await page.waitForTimeout(350)
    await page.getByRole('button', { name: '适应全部节点', exact: true }).click()
    await page.locator('[data-id="image-ready"] img').waitFor()
    await page.locator('[data-id="image-ready"] .cc-node-header').click()
    await page.waitForTimeout(350)
    for (const id of ['image-ready', 'image-empty', 'video-empty']) {
      const card = page.locator(`[data-id="${id}"] .cc-node`)
      const box = await card.boundingBox()
      const title = await card.locator('.cc-node-header').boundingBox()
      const content = await card.locator('.cc-node-content').boundingBox()
      assert.ok(title.y + title.height < box.y, 'title is outside and above the card')
      assert.ok(Math.abs(content.height - box.height) < 3, 'content fills the node')
      assert.equal(await card.locator('.cc-node-footer, .cc-node-header button').count(), 0)
      assert.equal(await card.evaluate((el) => getComputedStyle(el).boxShadow), id === 'image-ready' ? 'rgb(182, 187, 184) 0px 0px 0px 1px' : 'none')
    }
    const toolbarBox = await page.getByRole('toolbar', { name: '节点操作' }).boundingBox()
    const titleBox = await page.locator('[data-id="image-ready"] .cc-node-header').boundingBox()
    assert.ok(toolbarBox.y + toolbarBox.height < titleBox.y, 'toolbar leaves room for external title')
    assert.equal(await page.getByRole('img', { name: '空图片节点', exact: true }).textContent(), '')
    await page.screenshot({ path: '/tmp/canvas-simple-nodes.png' })
    await page.setViewportSize({ width: 1000, height: 800 })
    await page.getByRole('button', { name: '适应全部节点', exact: true }).click()
    await page.waitForTimeout(350)
    await page.screenshot({ path: '/tmp/canvas-simple-nodes-narrow.png' })
  }
  console.log(
    JSON.stringify({
      result: 'passed',
      ...metrics,
      checks: [
        'inline title/body: geometry, blur save, Enter, Escape, IME, native undo and canvas undo',
        'empty media selects without upload; toolbar filters and validates each media kind',
        'video node plays on hover, pauses on leave, resumes on re-hover after a manual pause; controls stay and the surface drags',
        'idle canvas makes no polling requests',
        'authenticated SSE invalidation updates remote nodes',
        'edge scissors follows midpoint at different zooms, deletes immediately, and supports undo',
        'only text/media creation entries, URL saved as text and undoable',
        'title hit area matches text; adjacent blank selects canvas or parent group',
        'group blank-area selection and dragging, independent child dragging, and undo',
        'context menus: position, keyboard, create, rename, multi-select copy, ungroup and undo',
        'group/copy before network',
        'Ctrl/Cmd undo and redo from buttons/body',
        'Ctrl+Y redo',
        'native input undo',
        'unavailable redo preserves edit',
      ],
    }),
  )
} catch (error) {
  console.error(await page.locator('body').innerText())
  await page.screenshot({ path: '/tmp/canvas-media-failure.png' })
  throw error
} finally {
  await browser.close()
}
