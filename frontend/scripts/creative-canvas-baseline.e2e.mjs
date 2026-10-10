// Browser regression for V6-055 (canvas baseline interactions). All API traffic
// is mocked and the page goes offline after load, so every edit stays a local
// pending change and undo cancels it without a server history.
// Run against the Vite dev server: node scripts/creative-canvas-baseline.e2e.mjs.
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const browser = await chromium.launch({ headless: true })
const errors = []
// 每条 AC 用新的 browser context：本机草稿/outbox 不跨 AC 残留，失败也不连锁。
let context, page

const caps = ['move', 'resize', 'rename', 'duplicate', 'remove', 'edit', 'reference']
const makeNode = (id, typeKey, x, y, extra = {}) => ({
  id,
  parent_id: null,
  metadata: { type_key: typeKey, type_version: 1, title: id, intent: '', x, y, width: 280, height: 180, z_order: 0 },
  prompt: null,
  capabilities: { actions: typeKey === 'core.group' ? ['move', 'rename', 'duplicate', 'remove', 'ungroup'] : caps, prompt_mode: 'draft', disabled_reason: null },
  status: { content_state: 'empty', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
  placement_revision: '1',
  data_revision: '1',
  data: { schema_version: 1, config: {}, content_id: null, content_revision_id: null, selected_version_id: null, document_id: null },
  ...extra,
})
const edge = (id, source, target) => ({ id, source_node_id: source, target_node_id: target, source_port: 'output', target_port: 'reference', role: 'reference', ordinal: 0, revision: '1' })
const fixture = () => ({
  id: 'qa',
  project_id: 'qa-project',
  project_name: '基线回归',
  project_revision: '1',
  revision: '1',
  topology_revision: '1',
  archived: false,
  nodes: [
    makeNode('a', 'core.text', 100, 120),
    makeNode('b', 'core.text', 520, 120),
    makeNode('c', 'core.text', 940, 120),
    makeNode('g', 'core.group', 80, 520, { metadata: { type_key: 'core.group', type_version: 1, title: '参考组', intent: '', x: 80, y: 520, width: 360, height: 260, z_order: 0 } }),
    makeNode('gc', 'core.text', 40, 64, { parent_id: 'g' }),
  ],
  edges: [edge('ab', 'a', 'b')],
  node_inputs: [],
  changes: [],
  object_states: [],
})
const posted = []
async function fresh() {
  await context?.close()
  context = await browser.newContext({ viewport: { width: 1600, height: 1000 } })
  page = await context.newPage()
  page.setDefaultTimeout(6000)
  page.on('pageerror', (error) => errors.push(error.message))
  const canvas = fixture()
  await page.addInitScript(() => {
  const original = window.fetch.bind(window)
  window.fetch = async (input, init) => {
    if (!String(input).endsWith('/canvases/qa/events')) return original(input, init)
    return new Response(new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode('event: invalidate\ndata: {}\n\n'))
        init.signal.addEventListener('abort', () => controller.error(new DOMException('Aborted', 'AbortError')), { once: true })
      },
    }), { headers: { 'Content-Type': 'text/event-stream' } })
  }
})
  await page.route('**/api/v1/**', async (route) => {
  const path = new URL(route.request().url()).pathname
  const json = (value, status = 200) => route.fulfill({ status, json: value })
  if (path.endsWith('/auth/refresh')) return json({ access_token: 'qa', token_type: 'Bearer', expires_in: 600 })
  if (path.endsWith('/me')) return json({ id: 'qa-baseline', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
  if (path.endsWith('/generation/catalog')) return json({ models: [], disabled_reason: '未配置生成模型' })
  if (path.endsWith('/versions')) return json({ items: [], selected_version_id: null, data_revision: '1' })
  if (path.endsWith('/canvases/qa')) return json(canvas)
  if (path.endsWith('/commands')) {
    posted.push(route.request().postDataJSON()?.payload?.actions ?? [])
    return json({ error: { code: 'unavailable', message: 'offline fixture' } }, 503)
  }
  if (path.endsWith('/library-settings')) return json({ revision: '1', view_mode: 'grid', sort: 'updated_desc', thumbnail_size: 'medium' })
  if (path.endsWith('/media-capabilities')) return json({ formats: [], image_max_bytes: 1, av_max_bytes: 1, batch_limit: 10 })
  return json({ items: [], next_cursor: '', total_count: 0, library_revision: '1', hierarchy_revision: '1' })
  })
  await page.goto(`${base}/creative/canvases/qa`)
  await present(nodeSel('gc'))
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => false })
    window.dispatchEvent(new Event('offline'))
  })
  await page.getByText('当前离线', { exact: false }).waitFor()
}

const present = (sel, n = 1) => page.waitForFunction(([q, c]) => document.querySelectorAll(q).length === c, [sel, n], { timeout: 4000 })
const nodeSel = (id) => `.react-flow__node[data-id="${id}"]`
const edgeSel = (id) => `.react-flow__edge[data-id="${id}"]`
const node = (id) => page.locator(`.react-flow__node[data-id="${id}"]`)
const header = (id) => node(id).locator('.cc-node-header h3')
const selectedIDs = () => page.locator('.react-flow__node.selected').evaluateAll((els) => els.map((e) => e.getAttribute('data-id')).sort())
const waitFor = (fn, arg) => page.waitForFunction(fn, arg, { timeout: 3000 })
const pos = (id) => node(id).evaluate((el) => {
  const m = /translate\(([-\d.]+)px,\s*([-\d.]+)px\)/.exec(el.style.transform)
  return { x: Number(m[1]), y: Number(m[2]) }
})
const viewport = () => page.locator('.react-flow__viewport').evaluate((el) => {
  const m = /translate\(([-\d.]+)px,\s*([-\d.]+)px\) scale\(([-\d.]+)\)/.exec(el.style.transform)
  return { x: Number(m[1]), y: Number(m[2]), zoom: Number(m[3]) }
})
const undo = () => page.getByRole('button', { name: '撤销', exact: true })
const redo = () => page.getByRole('button', { name: '重做', exact: true })
const results = {}
let step = ''
const at = (label) => { step = label }
// ONLY=AC-03,AC-05 只跑指定 AC，便于排查。
const only = process.env.ONLY?.split(',')
const record = async (ac, fn) => {
  if (only && !only.includes(ac)) return
  step = ''
  try {
    await fresh()
    await fn()
    results[ac] = 'pass'
  } catch (error) {
    results[ac] = `FAIL${step ? ` @${step}` : ''}: ${String(error.message).split('\n')[0]}`
  }
}
const blank = { x: 1500, y: 900 }

try {

  // AC-01 缩放条：空栈置灰；点百分比回到 100% 且屏幕中心对应的画布点不动。
  await record('AC-01', async () => {
    assert.equal(await undo().isDisabled(), true, 'undo disabled on empty stack')
    assert.equal(await redo().isDisabled(), true, 'redo disabled on empty stack')
    await page.getByRole('button', { name: '放大', exact: true }).click()
    await page.getByRole('button', { name: '放大', exact: true }).click()
    await page.waitForTimeout(400)
    const flow = await page.locator('.react-flow').boundingBox()
    const center = { x: flow.width / 2, y: flow.height / 2 }
    const before = await viewport()
    assert.notEqual(before.zoom, 1)
    const worldBefore = { x: (center.x - before.x) / before.zoom, y: (center.y - before.y) / before.zoom }
    await page.getByRole('button', { name: '恢复百分之百缩放', exact: true }).click()
    await page.waitForTimeout(400)
    const after = await viewport()
    assert.equal(after.zoom, 1, 'zoom resets to 100%')
    const worldAfter = { x: (center.x - after.x) / after.zoom, y: (center.y - after.y) / after.zoom }
    assert.ok(Math.abs(worldAfter.x - worldBefore.x) < 2 && Math.abs(worldAfter.y - worldBefore.y) < 2, `screen-center world point stays (${JSON.stringify(worldBefore)} → ${JSON.stringify(worldAfter)})`)
    await page.getByRole('button', { name: '适应全部节点', exact: true }).click()
    await page.waitForTimeout(400)
  })

  // AC-02 选择：单击、修饰键切换、点多选内不打散、空白清空、节点与连线互斥。
  await record('AC-02', async () => {
    await header('a').click()
    assert.deepEqual(await selectedIDs(), ['a'])
    // macOS 上 Ctrl+单击是系统右键手势；Ctrl 只在非 mac 平台检查。
    for (const mod of process.platform === 'darwin' ? ['Shift', 'Meta'] : ['Shift', 'Meta', 'Control']) {
      await header('b').click({ modifiers: [mod] })
      assert.deepEqual(await selectedIDs(), ['a', 'b'], `${mod}+click adds`)
      await header('b').click({ modifiers: [mod] })
      assert.deepEqual(await selectedIDs(), ['a'], `${mod}+click toggles off`)
    }
    await header('b').click({ modifiers: ['Shift'] })
    await header('a').click()
    assert.deepEqual(await selectedIDs(), ['a', 'b'], 'plain click inside multi-selection keeps it')
    await page.mouse.click(blank.x, blank.y)
    assert.deepEqual(await selectedIDs(), [], 'blank click clears')
    const mid = await page.locator('.react-flow__edge[data-id="ab"] .react-flow__edge-path').evaluate((path) => {
      const p = path.getPointAtLength(path.getTotalLength() / 2)
      const s = new DOMPoint(p.x, p.y).matrixTransform(path.getScreenCTM())
      return { x: s.x, y: s.y }
    })
    await header('a').click()
    await page.mouse.click(mid.x, mid.y)
    assert.deepEqual(await selectedIDs(), [], 'selecting an edge clears nodes')
    await page.getByRole('button', { name: '删除连线', exact: true }).waitFor()
    await header('a').click()
    assert.equal(await page.getByRole('button', { name: '删除连线', exact: true }).count(), 0, 'selecting a node clears the edge')
    await page.mouse.click(blank.x, blank.y)
  })

  // AC-03 组只能点标题选中；⌘A 全选；Esc 逐层：先关浮层，再清空选中。
  await record('AC-03', async () => {
    const groupBox = await node('g').boundingBox()
    await page.mouse.click(groupBox.x + groupBox.width - 10, groupBox.y + groupBox.height - 10)
    assert.deepEqual(await selectedIDs(), [], 'group body does not select the group')
    await header('g').click()
    assert.deepEqual(await selectedIDs(), ['g'], 'group title selects the group')
    await page.mouse.click(blank.x, blank.y)
    await header('a').click()
    await page.keyboard.press('Meta+a')
    const all = await selectedIDs()
    assert.ok(['a', 'b', 'c', 'g'].every((id) => all.includes(id)), `Cmd+A selects all (got ${all.join(',')})`)
    await page.mouse.click(blank.x, blank.y)
    await header('a').click()
    await node('a').locator('.cc-port-output button').click()
    await page.getByRole('menu', { name: '创建并连接节点' }).waitFor()
    await page.keyboard.press('Escape')
    await page.getByRole('menu', { name: '创建并连接节点' }).waitFor({ state: 'detached' })
    assert.deepEqual(await selectedIDs(), ['a'], 'first Esc only closes the menu')
    await page.locator('.react-flow').focus()
    await page.keyboard.press('Escape')
    assert.deepEqual(await selectedIDs(), [], 'second Esc clears selection')
  })

  // AC-04 方向键 1px / Shift 10px，输入状态不生效，每次可撤销。
  await record('AC-04', async () => {
    await header('c').click()
    const p0 = await pos('c')
    await page.keyboard.press('ArrowRight')
    await waitFor((x) => document.querySelector('.react-flow__node[data-id="c"]').style.transform.includes(`translate(${x}px`), p0.x + 1)
    await page.keyboard.press('Shift+ArrowDown')
    await waitFor((y) => document.querySelector('.react-flow__node[data-id="c"]').style.transform.includes(`, ${y}px)`), p0.y + 10)
    await header('c').dblclick()
    const title = page.getByRole('textbox', { name: '节点名称', exact: true })
    await title.press('ArrowLeft')
    const p1 = await pos('c')
    assert.deepEqual(p1, { x: p0.x + 1, y: p0.y + 10 }, 'arrow keys inside an input do not move')
    await title.press('Escape')
    await page.keyboard.press('Control+z')
    await waitFor((y) => document.querySelector('.react-flow__node[data-id="c"]').style.transform.includes(`, ${y}px)`), p0.y)
    await page.keyboard.press('Control+z')
    await waitFor((x) => document.querySelector('.react-flow__node[data-id="c"]').style.transform.includes(`translate(${x}px`), p0.x)
    assert.deepEqual(await pos('c'), p0, 'each move undoes separately')
  })

  // AC-05 端口：点击打开菜单，新节点在源节点右侧 120、垂直居中并自动连线；拖到空白保留虚线，取消消失，选中后变实线。
  await record('AC-05', async () => {
    await page.mouse.click(blank.x, blank.y)
    await header('c').click()
    const src = await pos('c')
    const srcBox = await node('c').locator('.cc-node').boundingBox()
    const zoom = (await viewport()).zoom
    at('click port')
    await node('c').locator('.cc-port-output button').click()
    const menu = page.getByRole('menu', { name: '创建并连接节点' })
    await menu.getByRole('menuitem', { name: '文字', exact: true }).click()
    at('new node appears')
    await present('.react-flow__node', 6)
    const created = (await selectedIDs())[0]
    assert.ok(created && !['a', 'b', 'c', 'g', 'gc'].includes(created), 'new node is selected')
    const np = await pos(created)
    const newBox = await node(created).locator('.cc-node').boundingBox()
    assert.ok(Math.abs(np.x - (src.x + srcBox.width / zoom + 120)) < 2, `new node 120 to the right (got ${np.x - src.x - srcBox.width / zoom})`)
    assert.ok(Math.abs((newBox.y + newBox.height / 2) - (srcBox.y + srcBox.height / 2)) < 2 * zoom, 'new node vertically centred on source')
    at('auto-connect')
    await present('.react-flow__edge', 2)
    // V6-006：新文本节点直接进入编辑；先 Esc 退出（未改动不留撤销步骤），再撤销创建。
    await page.getByRole('textbox', { name: '正文', exact: true }).press('Escape')
    await page.keyboard.press('Control+z')
    await present(nodeSel(created), 0)
    at('keyboard port')
    // 键盘：聚焦端口按 Enter / Space 打开菜单。
    await header('c').click()
    for (const key of ['Enter', ' ']) {
      await node('c').locator('.cc-port-output button').focus()
      await page.keyboard.press(key === ' ' ? 'Space' : key)
      await menu.waitFor()
      await page.keyboard.press('Escape')
      await menu.waitFor({ state: 'detached' })
    }
    // 拖到空白：虚线保留；取消消失；选中类型后新节点端口落在虚线终点并连实线。
    const port = await node('c').locator('.cc-port-output button').boundingBox()
    // 落点留在窗口内：适应画布按 110% 上限放大后（V6-002），源节点可能靠近右边缘。
    const drop = { x: Math.min(port.x + 160, 1540), y: port.y + 260 }
    const drag = async () => {
      await page.mouse.move(port.x + port.width / 2, port.y + port.height / 2)
      await page.mouse.down()
      await page.mouse.move(drop.x, drop.y, { steps: 10 })
      await page.mouse.up()
      await menu.waitFor()
    }
    at('drag to blank')
    await drag()
    assert.equal(await page.locator('.cc-draft-link path').count(), 1, 'draft link stays while menu is open')
    await page.keyboard.press('Escape')
    await waitFor(() => document.querySelectorAll('.cc-draft-link path').length === 0)
    await drag()
    await menu.getByRole('menuitem', { name: '文字', exact: true }).click()
    await waitFor(() => document.querySelectorAll('.cc-draft-link path').length === 0)
    const dropped = (await selectedIDs())[0]
    await present('.react-flow__edge', 2)
    // 输入端口在卡片左侧中点；新连线终点与它重合，并落在原虚线终点（松手处）。
    const card = await node(dropped).locator('.cc-node').boundingBox()
    const ends = await page.locator('.react-flow__edge').evaluateAll((els) => els.map((e) => {
      const p = e.querySelector('path.react-flow__edge-path')
      const pt = p.getPointAtLength(p.getTotalLength())
      const s = new DOMPoint(pt.x, pt.y).matrixTransform(p.getScreenCTM())
      return { x: s.x, y: s.y }
    }))
    assert.ok(Math.abs(card.x - drop.x) < 3 && Math.abs(card.y + card.height / 2 - drop.y) < 3, 'input port lands on the draft-link end')
    assert.ok(ends.some((e) => Math.abs(e.x - drop.x) < 3 && Math.abs(e.y - drop.y) < 3), 'draft link became a solid edge ending there')
    await page.getByRole('textbox', { name: '正文', exact: true }).press('Escape')
    await page.keyboard.press('Control+z')
    await present(nodeSel(dropped), 0)
  })

  // AC-06 连线规则：文本可作下游；拒绝环、自环、重复、组端点、指向链接，并提示原因。
  await record('AC-06', async () => {
    assert.equal(await node('b').locator('.cc-port-input').count(), 1, 'text node keeps an input port (CV-36)')
    // 组内空白透传给画布，组只能从标题栏命中，所以拖到组的标题上。
    const dragPort = async (from, to) => {
      const p = await node(from).locator('.cc-port-output button').boundingBox()
      const t = await (to === 'g' ? header('g') : node(to).locator('.cc-node')).boundingBox()
      await header(from).click()
      await page.mouse.move(p.x + p.width / 2, p.y + p.height / 2)
      await page.mouse.down()
      await page.mouse.move(t.x + t.width / 2, t.y + t.height / 2, { steps: 12 })
      await page.mouse.up()
      await page.waitForTimeout(300)
    }
    const edges = () => page.locator('.react-flow__edge').count()
    const notice = async (pattern, label) => {
      const text = await page.locator('body').innerText()
      assert.match(text, pattern, `${label}: explains why`)
    }
    const problems = []
    const check = async (label, fn) => {
      try {
        await fresh()
        await fn()
      } catch (e) {
        problems.push(`${label}: ${String(e.message).split('\n')[0]}`)
      }
    }
    await check('cycle b→a', async () => {
      await dragPort('b', 'a')
      assert.equal(await edges(), 1, 'cycle edge is not drawn')
      await notice(/环|循环/, 'cycle')
    })
    await check('duplicate a→b', async () => {
      await dragPort('a', 'b')
      assert.equal(await edges(), 1, 'duplicate edge is not drawn')
      await notice(/已经连接|已连接|重复/, 'duplicate')
    })
    await check('group endpoint a→g', async () => {
      await dragPort('a', 'g')
      assert.equal(await edges(), 1, 'group endpoint edge is not drawn')
      await notice(/组/, 'group endpoint')
    })
    await check('self a→a', async () => {
      await dragPort('a', 'a')
      assert.equal(await edges(), 1, 'self edge is not drawn')
      await notice(/自己|自身/, 'self')
    })
    if (problems.length) throw new Error(problems.join(' | '))
  })

  // AC-07 断线：浮条、双击、Backspace/Delete 都可撤销；双击断开提示「⌘Z 可撤销」。
  await record('AC-07', async () => {
    const edgeMid = async () => page.locator('.react-flow__edge[data-id="ab"] .react-flow__edge-path').evaluate((path) => {
      const p = path.getPointAtLength(path.getTotalLength() / 2)
      const s = new DOMPoint(p.x, p.y).matrixTransform(path.getScreenCTM())
      return { x: s.x, y: s.y }
    })
    const gone = () => present(edgeSel('ab'), 0)
    const back = async () => {
      await page.keyboard.press('Control+z')
      await present(edgeSel('ab'))
    }
    at('cut button')
    let m = await edgeMid()
    await page.mouse.click(m.x, m.y)
    await page.getByRole('button', { name: '删除连线', exact: true }).click()
    await gone()
    await back()
    for (const key of ['Backspace', 'Delete']) {
      at(`edge ${key}`)
      m = await edgeMid()
      await page.mouse.click(m.x, m.y)
      await page.getByRole('button', { name: '删除连线', exact: true }).waitFor()
      await page.keyboard.press(key)
      await gone()
      await back()
    }
    at('edge dblclick')
    m = await edgeMid()
    await page.mouse.dblclick(m.x, m.y)
    await gone()
    const text = await page.locator('body').innerText()
    await back()
    assert.match(text, /⌘Z 可撤销/, 'double-click disconnect hints ⌘Z')
  })

  // AC-08 删除节点子树连同相关连线，不弹确认，可撤销。
  await record('AC-08', async () => {
    await page.mouse.click(blank.x, blank.y)
    at('delete node')
    await header('a').click()
    await page.keyboard.press('Delete')
    await present(nodeSel('a'), 0)
    assert.equal(await page.getByRole('dialog').count(), 0, 'no confirmation')
    assert.equal(await page.locator('.react-flow__edge[data-id="ab"]').count(), 0, 'related edge removed')
    await page.keyboard.press('Control+z')
    await present(nodeSel('a'))
    await present(edgeSel('ab'))
    at('delete group subtree')
    await header('g').click()
    await page.keyboard.press('Backspace')
    await present(nodeSel('gc'), 0)
    await page.keyboard.press('Control+z')
    await present(nodeSel('gc'))
  })

  assert.deepEqual(errors, [])
  // 离线夹具：所有改动只进本机，不能偷偷发到服务端。
  assert.equal(posted.length, 0, 'offline fixture sends no commands')
} finally {
  console.log(JSON.stringify({ results, posted: posted.length }, null, 1))
  await context?.close()
  await browser.close()
}
const failed = Object.entries(results).filter(([, r]) => r !== 'pass')
if (failed.length || (!process.env.ONLY && Object.keys(results).length !== 8)) process.exitCode = 1
