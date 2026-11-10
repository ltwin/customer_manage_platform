import { test } from 'node:test'
import assert from 'node:assert/strict'
import { outlineRows } from '../src/creative-scene/outlineRows.ts'
import { listClick, emptySelection } from '../src/creative-scene/input/selection.ts'
import {
  dragSelection,
  edgeScroll,
  pickedObjects,
  pickerRows,
  type PickerObject,
} from '../src/creative-canvas/editor/scenePickerModel.ts'

const object = (id: string, name: string, parent: string | null, order: number): PickerObject => ({
  id,
  name,
  kind: parent === null && id.startsWith('g') ? 'group' : 'primitive',
  parent_id: parent,
  order,
})
// 客厅
// ├ g1 灯组: 台灯, 主光
// ├ 沙发
// └ 茶几
const objects = [
  object('table', '茶几', null, 3),
  object('lamp', '台灯', 'g1', 1),
  object('g1', '灯组', null, 1),
  object('sofa', '沙发', null, 2),
  object('key', '主光', 'g1', 2),
]
const order = outlineRows(objects).map((row) => row.object.id)

test('the outline lists children under their parent in sibling order', () => {
  assert.deepEqual(
    outlineRows(objects).map((row) => [row.object.id, row.depth]),
    [['g1', 0], ['lamp', 1], ['key', 1], ['sofa', 0], ['table', 0]],
  )
})

test('click, modifier and shift pick objects the way the outliner does', () => {
  let sel = listClick(emptySelection, 'lamp', order, { mod: false, shift: false })
  sel = listClick(sel, 'sofa', order, { mod: true, shift: false })
  assert.deepEqual(sel.selected, ['lamp', 'sofa'])
  sel = listClick(sel, 'lamp', order, { mod: true, shift: false })
  assert.deepEqual(sel.selected, ['sofa'])
  sel = listClick(sel, 'lamp', order, { mod: false, shift: true })
  assert.deepEqual(new Set(sel.selected), new Set(['lamp', 'key', 'sofa']))
})

test('dragging over rows selects the run between where it started and where it is', () => {
  const base = { selected: ['table'], active: 'table' }
  assert.deepEqual(dragSelection(base, 'lamp', 'sofa', order, false), { selected: ['lamp', 'key', 'sofa'], active: 'sofa' })
  // Dragging back up past the anchor selects the other way.
  assert.deepEqual(dragSelection(base, 'sofa', 'lamp', order, false).selected, ['lamp', 'key', 'sofa'])
  // With a modifier the run is added to what was selected.
  assert.deepEqual(dragSelection(base, 'lamp', 'key', order, true).selected, ['table', 'lamp', 'key'])
  assert.equal(dragSelection(base, 'lamp', 'missing', order, false), base)
})

test('searching keeps the matches with their parents, and a filtered selection stays', () => {
  const rows = pickerRows(objects, '光')
  assert.deepEqual(rows.map((row) => [row.object.id, row.matched]), [['g1', false], ['key', true]])
  assert.equal(pickerRows(objects, '  ').length, objects.length)
  assert.deepEqual(pickerRows(objects, '未命名'), [])
  assert.deepEqual(pickerRows([object('x', '', null, 1)], '未命名').map((row) => row.object.id), ['x'])
  // Selected before the search narrowed the list: still picked, in list order.
  const sel = { selected: ['sofa', 'key'], active: 'key' }
  assert.deepEqual(pickedObjects(sel, objects).map((o) => o.id), ['key', 'sofa'])
})

test('a drag near the edges scrolls the list', () => {
  assert.equal(edgeScroll(105, 100, 400), -8)
  assert.equal(edgeScroll(250, 100, 400), 0)
  assert.equal(edgeScroll(390, 100, 400), 8)
})
