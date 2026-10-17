// C1 scene editor in a real browser, against an in-memory scene server.
// Run with the frontend Vite server on CREATIVE_SCENE_URL (default :5179):
//   node scripts/creative-scene.e2e.mjs
// The server applies operations with the client's own copy of the semantics
// (state/apply.ts); the Go tests own the real ones. Every check gets a fresh
// browser context.
import assert from 'node:assert/strict'
import { rm, writeFile } from 'node:fs/promises'
import { chromium } from 'playwright'
import { applyOps } from '../src/creative-scene/state/apply.ts'
import { changedUnits } from '../src/creative-scene/state/intents.ts'

const DOC = 'cwdoc_scene'
const fixture = new URL('../.scene-editor-qa.html', import.meta.url)
const canvasFixture = new URL('../.scene-canvas-qa.html', import.meta.url)
await writeFile(
  fixture,
  `<html><body style="margin:0"><div id="root" class="cc-workspace" style="width:100vw;height:100vh"></div><script type="module">
import React from 'react';
import {createRoot} from 'react-dom/client';
import '/src/index.css';
import '/src/creative-canvas/editor/workspace.css';
import SceneEditor from '/src/creative-scene/SceneEditor.tsx';
import {notifyCanvasInvalidate} from '/src/creative-canvas/editor/canvasEvents.ts';
import {setAuthenticated} from '/src/auth/session.ts';
setAuthenticated({access_token:'local-test',expires_in:3600,token_type:'Bearer'},{id:'qa-account',username:'qa'});
try { localStorage.setItem('creative-scene:first-run-seen', '1') } catch {}
window.__invalidate = notifyCanvasInvalidate;
window.__closed = 0;
createRoot(document.getElementById('root')).render(React.createElement(SceneEditor,{documentID:'${DOC}',onClose:()=>{window.__closed++}}));
</script></body></html>`,
)
// The canvas page module graph, without the editor: three must not load.
await writeFile(
  canvasFixture,
  `<html><body><script type="module">
import '/src/creative-canvas/editor/WorkspacePage.tsx';
window.__loaded = true;
</script></body></html>`,
)
const base = process.env.CREATIVE_SCENE_URL || 'http://localhost:5179'
const browser = await chromium.launch({ headless: true, args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'] })
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
const close = (a, b, eps = 1e-6) => Math.abs(a - b) < eps

const identity = () => ({ position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] })
const placement = (id) => `object:${id}`
const content = (id) => `object:${id}:content`

/** One scene document with revisions, conflict checks and trash. */
class SceneServer {
  constructor() {
    this.title = '棚拍布景'
    this.scene = {
      environment: {
        sky: { kind: 'procedural', sun_elevation_deg: 35, sun_azimuth_deg: 135, turbidity: 3 },
        ambient: { color: '#ffffff', intensity: 0.4 },
        fog: null,
      },
      render: { mode: 'clay' },
      objects: [
        {
          id: 'sobj_ground',
          kind: 'primitive',
          name: '地面',
          parent_id: null,
          order: 0,
          transform: identity(),
          visible: true,
          locked: true,
          primitive: { builtin: 'ground@1', params: { size: 40 } },
        },
      ],
    }
    this.revision = 1
    this.unitRevs = { meta: '1', environment: '1', render: '1', objects: '1', [placement('sobj_ground')]: '1', [content('sobj_ground')]: '1' }
    this.trashedAt = null
    this.receipts = new Map()
    this.commands = []
    this.history = [this.record(['*'], [], null)]
    this.reads = 0
    this.hold = null
  }
  record(changed, removed, restoredFrom) {
    return {
      revision: String(this.revision),
      title: this.title,
      scene: structuredClone(this.scene),
      unit_revs: { ...this.unitRevs },
      changed_units: changed,
      removed_units: removed,
      restored_from: restoredFrom,
      created_at: new Date(Date.UTC(2026, 9, 16, 9, 0, this.revision)).toISOString(),
    }
  }
  doc() {
    return {
      id: DOC,
      title: this.title,
      type_key: 'scene',
      content_revision_id: `ccrv_${this.revision}`,
      revision: String(this.revision),
      unit_revs: { ...this.unitRevs },
      trashed_at: this.trashedAt,
      created_at: '2026-10-16T09:00:00Z',
      updated_at: '2026-10-16T09:00:00Z',
      scene: structuredClone(this.scene),
      models: {},
    }
  }
  object(id) {
    return this.scene.objects.find((o) => o.id === id)
  }
  objectUnits() {
    return new Set(this.scene.objects.flatMap((o) => [placement(o.id), content(o.id)]))
  }
  /** Applies one command body; returns [status, body]. */
  command(body) {
    const seen = this.receipts.get(body.operation_id)
    if (seen) return [200, seen]
    if (this.trashedAt) return [409, { error: { code: 'creative_state_conflict', message: '文档在回收站' } }]
    const { payload } = body
    const ops = payload.operations
    if (ops[0]?.op === 'restore') return this.restoreWhole(body, payload.expected_revision, ops[0].revision)
    const conflicts = []
    for (const [unit, rev] of Object.entries(payload.expected)) {
      const current = this.unitRevs[unit] ?? null
      if (current !== rev) conflicts.push({ unit, expected_rev: rev, current_rev: current })
    }
    if (conflicts.length && process.env.SCENE_E2E_DEBUG) console.log('CONFLICT', JSON.stringify(payload), JSON.stringify(conflicts))
    if (conflicts.length)
      return [409, { error: { code: 'creative_document_conflict', message: '文档已被修改', details: { conflicts } } }]
    const before = { title: this.title, scene: this.scene }
    const beforeUnits = this.objectUnits()
    let changed
    let restoredFrom = null
    if (ops[0]?.op === 'restore_units') {
      restoredFrom = ops[0].from_revision
      const after = this.restoreUnits(ops[0])
      this.title = after.title
      this.scene = after.scene
      changed = [...ops[0].units]
    } else {
      const after = applyOps(before, ops)
      this.title = after.title
      this.scene = after.scene
      changed = changedUnits({ ops }, before)
    }
    const afterUnits = this.objectUnits()
    const removed = [...beforeUnits].filter((u) => !afterUnits.has(u))
    const existenceChanged = removed.length || [...afterUnits].some((u) => !beforeUnits.has(u))
    if (existenceChanged && !changed.includes('objects')) changed.push('objects')
    changed = changed.filter((u) => !removed.includes(u) && (!u.startsWith('object:') || afterUnits.has(u)))
    this.revision++
    for (const u of changed) this.unitRevs[u] = String(this.revision)
    for (const u of removed) delete this.unitRevs[u]
    this.history.push(this.record(changed, removed, restoredFrom))
    const receipt = { revision: String(this.revision), changed_units: changed, annotated_units: [], removed_units: removed, reset_required: false }
    this.receipts.set(body.operation_id, receipt)
    this.commands.push({ ...body, receipt })
    return [200, receipt]
  }
  restoreUnits(op) {
    const source = this.history.find((h) => h.revision === op.from_revision)
    assert.ok(source, `restore from unknown revision ${op.from_revision}`)
    let objects = structuredClone(this.scene.objects)
    let { environment, render } = this.scene
    let title = this.title
    for (const unit of op.units) {
      if (unit === 'meta') title = source.title
      else if (unit === 'environment') environment = source.scene.environment
      else if (unit === 'render') render = source.scene.render
      else if (unit.startsWith('object:')) {
        const id = unit.split(':')[1]
        const was = source.scene.objects.find((o) => o.id === id)
        const now = objects.find((o) => o.id === id)
        if (!was) objects = objects.filter((o) => o.id !== id)
        else if (!now) objects.push(structuredClone(was))
        else if (unit.endsWith(':content')) Object.assign(now, { primitive: was.primitive, light: was.light, camera: was.camera })
        else Object.assign(now, { name: was.name, transform: was.transform, visible: was.visible, locked: was.locked })
      }
    }
    return { title, scene: { ...this.scene, environment, render, objects } }
  }
  restoreWhole(body, expected, revision) {
    if (expected !== String(this.revision))
      return [409, { error: { code: 'creative_document_conflict', message: '文档已被修改', details: { conflicts: [{ unit: '*', expected_rev: expected, current_rev: String(this.revision) }] } } }]
    const source = this.history.find((h) => h.revision === revision)
    this.revision++
    this.title = source.title
    this.scene = structuredClone(source.scene)
    this.unitRevs = Object.fromEntries(Object.keys(source.unit_revs).map((u) => [u, String(this.revision)]))
    this.history.push(this.record(['*'], [], revision))
    const receipt = { revision: String(this.revision), changed_units: [], annotated_units: [], removed_units: [], reset_required: true }
    this.receipts.set(body.operation_id, receipt)
    this.commands.push({ ...body, receipt })
    return [200, receipt]
  }
  /** Someone else's edit, straight on the server. */
  external(ops) {
    const before = { title: this.title, scene: this.scene }
    const after = applyOps(before, ops)
    const changed = changedUnits({ ops }, before)
    this.title = after.title
    this.scene = after.scene
    this.revision++
    for (const u of changed) this.unitRevs[u] = String(this.revision)
    this.history.push(this.record(changed, [], null))
  }
  revisionItem(h) {
    return {
      revision: h.revision,
      actor_kind: 'photographer',
      run_id: null,
      restored_from: h.restored_from,
      changed_units: h.changed_units,
      annotated_units: [],
      removed_units: h.removed_units,
      title: h.title,
      created_at: h.created_at,
    }
  }
}

async function route(page, server) {
  await page.route('**/api/v1/creative/**', async (r) => {
    const request = r.request()
    const url = new URL(request.url())
    const path = url.pathname.replace('/api/v1/creative', '')
    const json = (status, body) => r.fulfill({ status, json: body })
    if (path === `/documents/${DOC}` && request.method() === 'GET') {
      server.reads++
      return json(200, server.doc())
    }
    if (path === `/documents/${DOC}/commands` && request.method() === 'POST') {
      if (server.hold) await server.hold
      const [status, body] = server.command(request.postDataJSON())
      return json(status, body)
    }
    if (path === `/documents/${DOC}/revisions`) {
      const items = [...server.history].reverse().map((h) => server.revisionItem(h))
      return json(200, { items, next_cursor: null })
    }
    const one = path.match(new RegExp(`^/documents/${DOC}/revisions/(\\d+)$`))
    if (one) {
      const h = server.history.find((x) => x.revision === one[1])
      if (!h) return json(404, { error: { code: 'not_found', message: '修订不存在' } })
      return json(200, { ...server.revisionItem(h), unit_revs: h.unit_revs, view: { scene: h.scene, models: {} } })
    }
    return json(404, { error: { code: 'not_found', message: `unmocked ${path}` } })
  })
}

async function open(server, options = {}) {
  const context = options.context ?? (await browser.newContext({ viewport: { width: 1280, height: 820 } }))
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('console', (m) => m.type() === 'error' && !/Failed to load resource/.test(m.text()) && errors.push(m.text()))
  await route(page, server)
  await page.goto(`${base}/.scene-editor-qa.html`)
  await page.locator('.cc-scene-editor canvas').waitFor({ timeout: 20_000 })
  await page.getByRole('treeitem').first().waitFor()
  // Let R3F mount the scene and the camera settle.
  await sleep(400)
  return { context, page, errors }
}

async function viewportCenter(page) {
  const box = await page.locator('.cc-scene-editor canvas').boundingBox()
  return { x: box.x + box.width / 2, y: box.y + box.height / 2, box }
}

async function waitFor(check, message, timeout = 5000) {
  const start = Date.now()
  for (;;) {
    if (await check()) return
    if (Date.now() - start > timeout) throw new Error(`timed out: ${message}`)
    await sleep(25)
  }
}

const saveText = (page) => page.locator('.cc-scene-save').textContent()
const saved = (page) => waitFor(async () => (await saveText(page)) === '已保存', 'saved')

async function addBox(page, server) {
  const before = server.commands.length
  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '方块' }).click()
  await waitFor(() => server.commands.length === before + 1, 'add command')
  await saved(page)
  const add = server.commands.at(-1).payload.operations[0]
  assert.equal(add.op, 'add_objects')
  return add.objects[0].id
}

// SCENE_E2E_SHOTS=<dir> saves screenshots at the main steps for a look.
const shot = (page, name) => process.env.SCENE_E2E_SHOTS && page.screenshot({ path: `${process.env.SCENE_E2E_SHOTS}/${name}.png` })

const results = []
const only = process.env.SCENE_E2E_ONLY
async function check(name, fn) {
  if (only && !name.includes(only)) return
  const started = Date.now()
  try {
    await fn()
    results.push(`ok   ${name} (${Date.now() - started} ms)`)
  } catch (error) {
    results.push(`FAIL ${name}\n     ${error.stack?.split('\n').slice(0, 4).join('\n     ')}`)
  }
}

await check('keyboard flow: Shift+A, G Z 2 Enter, R cancelled, A G confirmed', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  assert.equal(server.reads, 1)
  const id = await addBox(page, server)
  const added = server.object(id)
  assert.equal(added.primitive.builtin, 'box@1')
  assert.equal(added.parent_id, null)
  assert.equal(added.order, 1, 'after the ground')
  assert.ok(close(added.transform.position[1], 0), 'placed on the ground')
  await page.getByRole('treeitem', { name: /方块/ }).waitFor()
  assert.equal(await page.getByRole('treeitem', { name: /方块/ }).getAttribute('aria-selected'), 'true', 'a new object is selected')

  await shot(page, '1-added')
  const start = [...added.transform.position]
  await page.keyboard.press('KeyG')
  await waitFor(async () => /移动/.test(await page.locator('.cc-scene-hint').textContent()), 'grab running')
  await page.keyboard.press('KeyZ')
  await page.keyboard.press('Digit2')
  assert.match(await page.locator('.cc-scene-hint').textContent(), /^移动 · Z 轴 · .*2/, 'the status bar shows mode, axis and the typed value')
  await page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === 2, 'move command')
  await saved(page)
  const move = server.commands[1].payload
  assert.deepEqual(Object.keys(move.expected), [placement(id)], 'a move names only the placement unit')
  const moved = server.object(id).transform.position
  assert.ok(close(moved[0], start[0]) && close(moved[1], start[1]) && close(moved[2], start[2] + 2), `G Z 2: ${moved} from ${start}`)

  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('KeyR')
  await page.mouse.move(x + 90, y + 30, { steps: 4 })
  await shot(page, '2-rotating')
  await page.mouse.click(x + 90, y + 30, { button: 'right' })
  await sleep(300)
  assert.equal(server.commands.length, 2, 'a cancelled rotate sends nothing')
  assert.match(await page.locator('.cc-scene-hint').textContent(), /G 移动/, 'back to idle')
  assert.deepEqual(server.object(id).transform.rotation, [0, 0, 0, 1])

  await page.keyboard.press('KeyA')
  await page.mouse.move(x, y)
  await page.keyboard.press('KeyG')
  await page.mouse.move(x + 120, y, { steps: 6 })
  await page.mouse.click(x + 120, y)
  await waitFor(() => server.commands.length === 3, 'select-all move command')
  await saved(page)
  const all = server.commands[2].payload.operations[0]
  assert.equal(all.op, 'update_objects')
  assert.deepEqual(all.updates.map((u) => u.object_id), [id], 'the locked ground stays put')
  const final = server.object(id).transform.position
  assert.ok(!close(final[0], moved[0], 1e-3), 'moved along the view')
  assert.ok(close(final[1], moved[1], 1e-6), 'a sideways drag moves parallel to the screen, without vertical drift')
  assert.deepEqual(server.object('sobj_ground').transform, identity())
  await shot(page, '3-final')
  assert.equal(server.commands.length, 3)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('new objects go where the menu opened, not where its item is', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '方块' }).waitFor()
  assert.equal(await page.getByRole('menuitem').first().textContent(), '方块', 'the first item is a box, not a second ground')
  await page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === 1, 'keyboard add')
  const viaKeyboard = server.commands[0].payload.operations[0].objects[0].transform.position
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '方块' }).click()
  await waitFor(() => server.commands.length === 2, 'mouse add')
  const viaMouse = server.commands[1].payload.operations[0].objects[0].transform.position
  assert.deepEqual(viaMouse, viaKeyboard, 'the pointer travelling to the item does not move the spot')

  await page.mouse.click(x + 160, y + 90, { button: 'right' })
  await page.getByRole('menuitem', { name: '球体' }).click()
  await waitFor(() => server.commands.length === 3, 'context-menu add')
  const sphere = server.commands[2].payload.operations[0].objects[0].transform.position
  assert.ok(Math.hypot(sphere[0] - viaMouse[0], sphere[2] - viaMouse[2]) > 0.5, 'a right click elsewhere adds elsewhere')

  await page.getByRole('button', { name: '添加' }).click()
  await page.getByRole('menuitem', { name: '圆柱' }).click()
  await waitFor(() => server.commands.length === 4, 'menu-bar add')
  const cylinder = server.commands[3].payload.operations[0].objects[0].transform.position
  assert.ok(cylinder.every(Number.isFinite) && cylinder[1] === 0, 'the menu bar adds at the view centre on the ground')
  await saved(page)
  await shot(page, '0-placed')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('reopening shows exactly what the server has', async () => {
  const server = new SceneServer()
  const first = await open(server)
  const id = await addBox(first.page, server)
  await first.page.keyboard.press('KeyG')
  await first.page.keyboard.press('KeyX')
  await first.page.keyboard.press('Minus')
  await first.page.keyboard.press('Digit3')
  await first.page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === 2, 'move')
  await saved(first.page)
  await first.context.close()

  const again = await open(server)
  await again.page.getByRole('treeitem', { name: /方块/ }).click()
  const fields = again.page.locator('.cc-scene-props input[type=number]')
  const [px, py, pz] = server.object(id).transform.position
  const value = async (i) => Number(await fields.nth(i).inputValue())
  assert.ok(close(await value(0), Math.round(px * 1000) / 1000), 'x')
  assert.ok(close(await value(1), Math.round(py * 1000) / 1000), 'y')
  assert.ok(close(await value(2), Math.round(pz * 1000) / 1000), 'z')
  assert.equal(server.reads, 2)
  assert.deepEqual([...first.errors, ...again.errors], [])
  await again.context.close()
})

await check('undo and redo go through restore_units', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  const start = [...server.object(id).transform.position]
  await page.keyboard.press('KeyG')
  await page.keyboard.press('KeyX')
  await page.keyboard.press('Digit4')
  await page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === 2, 'move')
  await saved(page)
  const mod = process.platform === 'darwin' ? 'Meta' : 'Control'
  await page.keyboard.press(`${mod}+KeyZ`)
  await waitFor(() => server.commands.length === 3, 'undo')
  const undo = server.commands[2].payload.operations[0]
  assert.equal(undo.op, 'restore_units')
  assert.ok(close(server.object(id).transform.position[0], start[0]), 'back where it was')
  await saved(page)
  await page.keyboard.press(`${mod}+KeyZ`)
  await waitFor(() => server.commands.length === 4, 'undo the add')
  assert.equal(server.object(id), undefined, 'undoing the add removes the box')
  await saved(page)
  await page.keyboard.press(`${mod}+Shift+KeyZ`)
  await waitFor(() => server.commands.length === 5, 'redo the add')
  assert.ok(server.object(id), 'redo brings the same object back')
  await saved(page)
  await page.keyboard.press(`${mod}+Shift+KeyZ`)
  await waitFor(() => server.commands.length === 6, 'redo the move')
  assert.ok(close(server.object(id).transform.position[0], start[0] + 4))
  await saved(page)
  assert.equal(await page.getByRole('treeitem', { name: /方块/ }).count(), 1)
  assert.deepEqual(errors, [])
  await context.close()
})

await check("someone else's edit: a re-read shows it, a stale edit asks first", async () => {
  const server = new SceneServer()
  const a = await open(server)
  const id = await addBox(a.page, server)
  const b = await open(server, { context: await browser.newContext({ viewport: { width: 1280, height: 820 } }) })
  await b.page.getByRole('treeitem', { name: /方块/ }).click()

  // A moves the box; B learns of it from the canvas page's notification.
  await a.page.keyboard.press('KeyG')
  await a.page.keyboard.press('KeyX')
  await a.page.keyboard.press('Digit1')
  await a.page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === 2, 'A moves')
  await b.page.evaluate(() => window.__invalidate())
  const bx = b.page.locator('.cc-scene-props input[type=number]').first()
  await waitFor(async () => close(Number(await bx.inputValue()), Math.round(server.object(id).transform.position[0] * 1000) / 1000), 'B shows the move')

  // A moves again; B, not yet told, moves the same box and is asked.
  server.external([{ op: 'update_objects', updates: [{ object_id: id, transform: { ...server.object(id).transform, position: [5, 0, 5] } }] }])
  await b.page.keyboard.press('KeyG')
  await b.page.keyboard.press('KeyY')
  await b.page.keyboard.press('Digit1')
  await b.page.keyboard.press('Enter')
  await b.page.getByRole('alert').filter({ hasText: '重新应用' }).waitFor()
  await shot(b.page, '5-conflict')
  assert.equal(await saveText(b.page), '有冲突待处理')
  assert.deepEqual(server.object(id).transform.position, [5, 0, 5], 'the conflicting edit did not land')
  await b.page.getByRole('button', { name: '还原节点' }).click()
  await b.page.getByText('有修改因冲突没有保存').waitFor()
  await b.page.getByRole('button', { name: '返回处理' }).click()
  assert.equal(await b.page.evaluate(() => window.__closed), 0, 'closing waits for a decision')
  await b.page.getByRole('button', { name: '重新应用我的修改' }).click()
  await waitFor(() => close(server.object(id).transform.position[1], 1, 1e-6), 'reapplied on top of the latest')
  await saved(b.page)
  assert.deepEqual([...a.errors, ...b.errors], [])
  await a.context.close()
  await b.context.close()
})

await check('a trashed scene turns read-only and keeps the edit for after untrash', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  server.trashedAt = '2026-10-16T10:00:00Z'
  await page.keyboard.press('KeyG')
  await page.keyboard.press('KeyX')
  await page.keyboard.press('Digit2')
  await page.keyboard.press('Enter')
  await page.getByText('场景在回收站，请先在画布上恢复后再编辑').waitFor()
  assert.equal(await saveText(page), '只读')
  await page.keyboard.press('Shift+KeyA')
  assert.equal(await page.getByRole('menu').count(), 0, 'no add menu while trashed')
  await page.getByRole('alert').filter({ hasText: '回收站' }).waitFor()
  server.trashedAt = null
  await page.evaluate(() => window.__invalidate())
  await waitFor(async () => (await page.getByText('场景在回收站，请先在画布上恢复后再编辑').count()) === 0, 'untrashed')
  await page.getByRole('button', { name: '重新应用我的修改' }).click()
  await waitFor(() => server.commands.length === 2, 'kept edit saved')
  assert.ok(close(server.object(id).transform.position[0], 2, 1e-6) || server.object(id).transform.position[0] > 1.5)
  await saved(page)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('history lists revisions and restores one as a new revision', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  await addBox(page, server)
  await page.getByRole('button', { name: '历史' }).click()
  const panel = page.getByRole('complementary', { name: '历史版本' })
  await panel.waitFor()
  await waitFor(async () => (await panel.locator('li').count()) >= 1, 'history items')
  assert.match(await panel.locator('li').last().textContent(), /新建场景/, 'the creation lists on its own')
  await panel.locator('li button').last().click()
  await page.getByText(/正在预览第 1 版/).first().waitFor()
  await shot(page, '4-history-preview')
  assert.equal(await page.getByRole('treeitem', { name: /方块/ }).count(), 0, 'revision 1 has only the ground')
  await page.getByRole('button', { name: '恢复到此版本' }).click()
  await page.getByRole('button', { name: '恢复', exact: true }).click()
  await waitFor(() => server.commands.length === 2, 'whole restore')
  assert.equal(server.history.at(-1).restored_from, '1')
  await waitFor(async () => (await page.getByRole('treeitem', { name: /方块/ }).count()) === 0, 'box gone after restore')
  assert.equal(server.scene.objects.length, 1)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('selection edits: box select, H / Alt+H, Shift+D, X', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const { x, y, box } = await viewportCenter(page)
  const first = await addBox(page, server)
  const second = await addBox(page, server)

  // Box select from empty ground around both; the locked ground stays out.
  await page.keyboard.press('Alt+KeyA')
  await page.mouse.move(box.x + 40, box.y + 40)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width - 40, box.y + box.height - 40, { steps: 8 })
  await page.mouse.up()
  const selectedRows = async () => page.locator('[role=treeitem][aria-selected=true]').allTextContents()
  assert.deepEqual(await selectedRows(), ['方块', '方块'], 'box select takes both boxes, not the locked ground')

  await page.keyboard.press('KeyH')
  await waitFor(() => server.commands.length === 3, 'hide')
  assert.deepEqual([server.object(first).visible, server.object(second).visible], [false, false])
  await page.keyboard.press('Alt+KeyH')
  await waitFor(() => server.commands.length === 4, 'reveal')
  assert.deepEqual([server.object(first).visible, server.object(second).visible], [true, true])
  await saved(page)

  // Shift+D copies and moves in one go: one add, nothing in between.
  await page.getByRole('treeitem').nth(1).click()
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyD')
  await page.mouse.move(x + 60, y + 20, { steps: 5 })
  await sleep(150)
  assert.equal(server.commands.length, 4, 'the copy waits until the move ends')
  await page.mouse.click(x + 60, y + 20)
  await waitFor(() => server.commands.length === 5, 'duplicate')
  const dup = server.commands[4].payload.operations
  assert.equal(dup.length, 1)
  assert.equal(dup[0].op, 'add_objects')
  const copy = dup[0].objects[0]
  assert.notEqual(copy.id, first)
  assert.notDeepEqual(copy.transform.position, server.object(first).transform.position, 'the copy lands where it was moved')
  await saved(page)

  // X asks first; the menu's single item deletes.
  await page.keyboard.press('KeyX')
  await page.getByRole('menuitem', { name: '删除 1 个物件' }).click()
  await waitFor(() => server.commands.length === 6, 'delete')
  assert.equal(server.object(copy.id), undefined)
  await saved(page)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('the move tool drags the selection with its handle: one update', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  const before = [...server.object(id).transform.position]
  await page.getByRole('button', { name: '移动', exact: true }).click()
  const { x, y } = await viewportCenter(page)
  // The handle's centre sits on the box's origin, the middle of the view.
  await page.mouse.move(x - 3, y + 2)
  await page.mouse.move(x, y)
  await page.mouse.down()
  await page.mouse.move(x + 90, y, { steps: 10 })
  await sleep(100)
  assert.equal(server.commands.length, 1, 'nothing goes out mid-drag')
  await page.mouse.up()
  await waitFor(() => server.commands.length === 2, 'gizmo move')
  const op = server.commands[1].payload.operations[0]
  assert.equal(op.op, 'update_objects')
  assert.deepEqual(op.updates.map((u) => u.object_id), [id])
  assert.ok(Math.hypot(...server.object(id).transform.position.map((v, i) => v - before[i])) > 0.2, 'it moved')
  await saved(page)
  await shot(page, '6-gizmo')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('properties commit 300 ms after typing stops; focusing alone writes nothing', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  const width = page.getByLabel('宽（m）')
  await width.click()
  await width.fill('3')
  const typed = Date.now()
  await waitFor(() => server.commands.length === 2, 'debounced commit')
  assert.ok(Date.now() - typed >= 250, 'not before the pause')
  assert.equal(server.object(id).primitive.params.width, 3)
  assert.deepEqual(server.commands[1].payload.expected, { [content(id)]: '2' }, 'a parameter is a content edit')
  await page.keyboard.press('Enter')
  await sleep(400)
  assert.equal(server.commands.length, 2, 'Enter after the commit sends nothing more')
  // Rotation shows rounded degrees; focusing and leaving must not write them back.
  const rx = page.locator('.cc-scene-props fieldset').nth(1).locator('input').first()
  await rx.click()
  await page.getByLabel('名称').click()
  await sleep(400)
  assert.equal(server.commands.length, 2)
  await saved(page)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('key boundaries: fields, Esc, Cmd/Ctrl+S, ?, nothing leaks to the page', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  await addBox(page, server)
  await page.evaluate(() => {
    window.__leaked = 0
    window.addEventListener('keydown', () => window.__leaked++)
  })
  const mod = process.platform === 'darwin' ? 'Meta' : 'Control'
  assert.match(await page.locator('.cc-scene-hint').textContent(), /G 移动 · R 旋转/)
  await page.getByRole('button', { name: '物件' }).click()
  assert.equal(await page.getByRole('menuitem', { name: /复制/ }).locator('kbd').textContent(), process.platform === 'darwin' ? '⇧D' : 'Shift+D')
  await page.keyboard.press('Escape')

  const name = page.getByLabel('名称')
  await name.click()
  await name.fill('grsx 方块')
  await page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === 2, 'rename')
  assert.deepEqual(Object.keys(server.commands[1].payload.operations[0].updates[0]), ['object_id', 'name'], 'letters typed in a field are text, not shortcuts')

  for (let i = 0; i < 3; i++) await page.keyboard.press('Escape')
  assert.equal(await page.evaluate(() => window.__closed), 0, 'Esc never closes the view')
  assert.equal(await page.locator('[role=treeitem][aria-selected=true]').count(), 0, 'Esc clears the selection')

  await page.keyboard.press(`${mod}+KeyS`)
  await waitFor(async () => (await page.locator('.cc-scene-hint').textContent()) === '场景会自动保存，不需要手动保存', 'save notice')
  await page.keyboard.press('Shift+Slash')
  await page.getByRole('dialog', { name: /快捷键/ }).waitFor()
  await page.keyboard.press('Escape')
  // From a field, a click in the viewport gives the keys back to the editor.
  await page.getByRole('treeitem', { name: /方块/ }).click()
  await page.getByLabel('名称').click()
  const { box } = await viewportCenter(page)
  await page.mouse.click(box.x + 30, box.y + 30)
  await page.keyboard.press('KeyA')
  assert.equal(await page.locator('[role=treeitem][aria-selected=true]').count(), 1, 'A selects the box after a viewport click')
  await page.keyboard.press('KeyG')
  await page.keyboard.press('KeyA')
  assert.equal(await page.evaluate(() => window.__leaked), 0, 'no key reaches listeners behind the editor')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('leaving with unsaved edits is intercepted; saved, it is not', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  let release
  server.hold = new Promise((r) => (release = r))
  await page.keyboard.press('KeyG')
  await page.keyboard.press('KeyX')
  await page.keyboard.press('Digit1')
  await page.keyboard.press('Enter')
  await waitFor(async () => (await saveText(page)) === '保存中', 'saving')
  const dialogs = []
  page.on('dialog', (d) => {
    dialogs.push(d.type())
    void d.dismiss()
  })
  await page.close({ runBeforeUnload: true })
  await waitFor(() => dialogs.length === 1, 'beforeunload prompt')
  assert.deepEqual(dialogs, ['beforeunload'])
  assert.equal(page.isClosed(), false, 'dismissing keeps the page')
  release()
  server.hold = null
  await waitFor(() => server.commands.length === 2, 'saved after all')
  await saved(page)
  assert.ok(server.object(id).transform.position[0] > 0.5)
  await page.close({ runBeforeUnload: true })
  await waitFor(() => page.isClosed(), 'closes without asking once saved')
  assert.equal(dialogs.length, 1)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('without WebGL2 the view explains instead of going blank', async () => {
  const server = new SceneServer()
  const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
  await context.addInitScript(() => {
    const real = HTMLCanvasElement.prototype.getContext
    HTMLCanvasElement.prototype.getContext = function (type, ...rest) {
      return type === 'webgl2' ? null : real.call(this, type, ...rest)
    }
  })
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  await route(page, server)
  await page.goto(`${base}/.scene-editor-qa.html`)
  await page.getByText('当前浏览器无法显示 3D 场景，请使用最新版 Chrome、Edge 或 Safari').waitFor()
  assert.equal(await page.locator('.cc-scene-editor canvas').count(), 0)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('trackpad swipes and pinches move the view at once, without a click', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  await addBox(page, server)
  await page.getByRole('button', { name: '视图' }).click()
  await page.getByRole('menuitem', { name: '触控板' }).click()
  const { x, y, box } = await viewportCenter(page)
  const view = () => page.screenshot({ clip: box })
  await page.mouse.move(x, y)
  // The first wheel settles the device guess, a re-render that would hide the bug.
  await page.mouse.wheel(1, 0)
  await sleep(300)
  let before = await view()
  // Two-finger swipe orbits; the frame must change with no click after it.
  for (let i = 0; i < 5; i++) await page.mouse.wheel(40, 0)
  await sleep(300)
  let after = await view()
  assert.ok(!before.equals(after), 'a two-finger swipe redraws the view by itself')
  before = after
  // A pinch arrives as a wheel with ctrlKey.
  await page.keyboard.down('Control')
  for (let i = 0; i < 5; i++) await page.mouse.wheel(0, -30)
  await page.keyboard.up('Control')
  await sleep(300)
  after = await view()
  assert.ok(!before.equals(after), 'a pinch redraws the view by itself')
  assert.equal(server.commands.length, 1, 'moving the view saves nothing')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('the editor makes room for the open assistant panel', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const editor = page.locator('.cc-scene-editor')
  const before = await editor.boundingBox()
  await page.evaluate(() => {
    const panel = document.createElement('aside')
    panel.className = 'cc-agent-panel'
    document.getElementById('root').appendChild(panel)
  })
  await sleep(100)
  const after = await editor.boundingBox()
  assert.ok(after.x + after.width <= 1280 - 380 - 24 + 1, `editor ends at ${after.x + after.width}`)
  assert.ok(after.width < before.width)
  await page.setViewportSize({ width: 700, height: 820 })
  await sleep(100)
  const narrow = await editor.boundingBox()
  assert.ok(narrow.x + narrow.width >= 700 - 12 - 1, 'a bottom-sheet panel takes no width from the editor')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('opening the canvas page loads no three.js', async () => {
  const context = await browser.newContext()
  const page = await context.newPage()
  const requested = []
  page.on('request', (r) => requested.push(r.url()))
  await page.goto(`${base}/.scene-canvas-qa.html`)
  await page.waitForFunction(() => window.__loaded === true, null, { timeout: 20_000 })
  await sleep(300)
  const three = requested.filter((u) => /\/(three|@react-three)[/_.]/.test(u) || /deps\/(three|@react-three)/.test(u))
  assert.deepEqual(three, [], 'the editor and three load only when a scene opens')
  assert.ok(requested.some((u) => u.includes('WorkspacePage')), 'the canvas page itself loaded')
  await context.close()
})

await browser.close()
await rm(fixture, { force: true })
await rm(canvasFixture, { force: true })
console.log(results.join('\n'))
if (results.some((r) => r.startsWith('FAIL'))) process.exit(1)
