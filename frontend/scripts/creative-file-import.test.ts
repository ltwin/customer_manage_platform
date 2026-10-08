import test from 'node:test'
import assert from 'node:assert/strict'
import { canvasHistory } from '../src/creative-canvas/editor/graph.ts'
import { fileTitle, importPlan, importSlot } from '../src/creative-canvas/editor/fileImport.ts'
import type { Canvas } from '../src/creative-canvas/editor/api.ts'
import type { MediaCapabilities } from '../src/creative-canvas/editor/media.ts'

// 快照按新到旧排列。
const change = (id: string, group: string | null = null, inverse: string | null = null) =>
  ({ id, change_group_id: group, inverse_of: inverse, result_revision: '1', read_set: [{ kind: 'node', id: `n-${id}`, placement_revision: '1', data_revision: '1' }] }) as Canvas['changes'][number]

test('an import batch and its late bindings undo as one step', () => {
  const h = canvasHistory([change('bind2', 'g'), change('bind1', 'g'), change('add', 'g'), change('move')])
  assert.equal(h.undoGroup, 'g')
  assert.deepEqual(h.undoIDs, ['add', 'bind1', 'bind2'])
  assert.equal(h.undo, 'bind2')
})

test('the group inverse pops the whole batch and becomes the redo target', () => {
  const h = canvasHistory([change('inv', null, 'bind2'), change('bind2', 'g'), change('bind1', 'g'), change('add', 'g'), change('move')])
  assert.equal(h.undo, 'move')
  assert.equal(h.undoGroup, undefined)
  assert.equal(h.redo, 'inv')
})

test('a group with a single change undoes by change id', () => {
  const h = canvasHistory([change('add', 'g'), change('move')])
  assert.equal(h.undoGroup, undefined)
  assert.equal(h.undoStepGroup, 'g')
  assert.equal(h.undo, 'add')
})

test('a group split by another edit falls back to per-change undo', () => {
  const h = canvasHistory([change('bind', 'g'), change('move'), change('add', 'g')])
  assert.equal(h.undoGroup, undefined)
  assert.equal(h.undo, 'bind')
})

const caps = {
  formats: [
    { kind: 'image', mime: 'image/png', extensions: ['.png'] },
    { kind: 'video', mime: 'video/mp4', extensions: ['.mp4'] },
  ],
  image_max_bytes: 100,
  av_max_bytes: 1000,
  batch_limit: 2,
} as MediaCapabilities
const file = (name: string, type: string, size = 10) => new File([new Uint8Array(size)], name, { type })

test('import plan keeps order, explains each rejection and the batch limit', () => {
  const plan = importPlan([file('a.png', 'image/png'), file('notes.txt', 'text/plain'), file('b.mp4', 'video/mp4'), file('c.png', 'image/png')], caps)
  assert.deepEqual(plan.accepted.map((a) => [a.file.name, a.kind]), [['a.png', 'image'], ['b.mp4', 'video']])
  assert.deepEqual(plan.rejected.map((r) => [r.file.name, r.message]), [['notes.txt', '暂不支持此文件格式'], ['c.png', '一次最多导入 2 个文件']])
})

test('nodes line up 330 apart, the first centred on the drop point', () => {
  assert.deepEqual(importSlot(0, { x: 500, y: 400 }, 'core.image'), { x: 350, y: 250 })
  assert.deepEqual(importSlot(2, { x: 500, y: 400 }, 'core.audio'), { x: 1010, y: 340 })
})

test('titles drop only the last extension', () => {
  assert.equal(fileTitle('窗边.晨光.png'), '窗边.晨光')
  assert.equal(fileTitle('%E6%97%A5%E8%90%BD.jpg'), '日落')
  assert.equal(fileTitle('.png'), '.png')
  assert.equal([...fileTitle(`${'长'.repeat(250)}.png`)].length, 200)
})
