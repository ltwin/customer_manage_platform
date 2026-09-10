import test from 'node:test'
import assert from 'node:assert/strict'
import { fixture, createCanvasStore, canConnect } from '../src/creative-canvas/lab/state.ts'

test('group movement keeps relative child positions and fixed references', () => {
  const store = createCanvasStore()
  const before = store.getState()
  store.getState().onNodesChange([{ type: 'position', id: 'group', position: { x: 210, y: 200 } }])
  const after = store.getState()
  assert.deepEqual(after.nodes.find(n => n.id === 'texture')?.position, before.nodes.find(n => n.id === 'texture')?.position)
  assert.deepEqual(after.nodes.find(n => n.id === 'light')?.position, before.nodes.find(n => n.id === 'light')?.position)
  assert.deepEqual(after.edges, before.edges)
})
test('references reject cycles, duplicate edges, group endpoints and wrong handles', () => {
  const { nodes, edges } = fixture()
  const connection = { source: 'direction', target: 'light', sourceHandle: 'out', targetHandle: 'in' }
  assert.equal(canConnect(nodes, edges, connection), false)
  assert.equal(canConnect(nodes, edges, { ...connection, source: 'light', target: 'direction' }), false)
  assert.equal(canConnect(nodes, edges, { ...connection, source: 'group' }), false)
  assert.equal(canConnect(nodes, edges, { ...connection, source: 'light', target: 'texture', sourceHandle: 'in' }), false)
  assert.equal(canConnect(nodes, edges, { ...connection, source: 'light', target: 'texture' }), true)
})
test('performance fixture is deterministic and has no dangling endpoints', () => {
  const data = fixture(true)
  assert.equal(data.nodes.length, 200); assert.equal(data.edges.length, 300)
  const ids = new Set(data.nodes.map(n => n.id))
  assert(data.edges.every(e => ids.has(e.source) && ids.has(e.target)))
  assert.deepEqual(data, fixture(true))
})
