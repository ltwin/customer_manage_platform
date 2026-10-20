import test from 'node:test'
import assert from 'node:assert/strict'
import type { SceneDocument, SceneObject } from '../src/creative-scene/api.ts'
import type { SceneModel } from '../src/creative-scene/models/cache.ts'
import type { Intent } from '../src/creative-scene/state/intents.ts'
import { saveTarget, type SaveInput } from '../src/creative-scene/saveModel.ts'

// C6: saving a scene's code model into the asset library, and placing one.

const objectID = (n: number) => `sobj_${String(n).padStart(21, '0')}`
const object = (n: number, extra: Partial<SceneObject>): SceneObject => ({
  id: objectID(n), kind: 'model_instance', name: '柱子', parent_id: null, order: n,
  transform: { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }, visible: true, locked: false, material_slots: {},
  ...extra,
}) as SceneObject
const summary = (extra: Partial<SceneModel> = {}): SceneModel => ({ content_id: 'ccnt_1', usable: true, manifest: null, generator: null, name_hint: '哥特束柱', ...extra })
const pillar = object(1, { model: { content_revision_id: 'ccrv_1' } })
const base = { unit_revs: { objects: '3', [`object:${objectID(1)}:content`]: '6' } } as unknown as SceneDocument
const input = (patch: Partial<SaveInput> = {}): SaveInput => ({
  selected: [objectID(1)], objects: [pillar, object(2, { kind: 'primitive', model: undefined })], base,
  models: { ccrv_1: summary() }, previews: {}, unsaved: [], ...patch,
})
const intent = (expected: Record<string, unknown>) => ({ id: 'i', expected }) as unknown as Intent

test('a model can be saved when the editor shows a settled, usable model', () => {
  assert.deepEqual(saveTarget(input()), { objectID: objectID(1), contentRevisionID: 'ccrv_1', unitRev: '6', name: '哥特束柱' })
  assert.equal((saveTarget(input({ models: { ccrv_1: summary({ name_hint: null }) } })) as { name: string }).name, '柱子', 'no name hint: the object name')
})

test('each reason it cannot be saved, in the design table order', () => {
  const rows: [string, Partial<SaveInput>, string][] = [
    ['nothing selected', { selected: [] }, '选一个模型'],
    ['two selected', { selected: [objectID(1), objectID(2)] }, '选一个模型'],
    ['a primitive', { selected: [objectID(2)] }, '只有代码模型能存入资产库'],
    ['still baking', { objects: [object(1, { model: { content_revision_id: 'local:intent1:m' } })] }, '模型还在生成'],
    ['retune running', { previews: { [objectID(1)]: 'local:intent2:m' } }, '参数修改还在生成，稍后再存'],
    ['unusable model', { models: { ccrv_1: summary({ usable: false }) } }, '模型不可用'],
    ['unknown model', { models: {} }, '模型不可用'],
    ['an unsaved edit of its model', { unsaved: [intent({ [`object:${objectID(1)}:content`]: '6' })] }, '正在保存，稍后再试'],
    ['gone from the scene', { objects: [] }, '这个物件已不在场景里'],
  ]
  for (const [name, patch, error] of rows) assert.deepEqual(saveTarget(input(patch)), { error }, name)
  assert.ok(!('error' in saveTarget(input({ unsaved: [intent({ [`object:${objectID(1)}`]: '2' })] }))), 'a move of the same object does not block')
})

test('after a conflict the dialog waits until the editor shows the new version', async () => {
  const { whenShown } = await import('../src/creative-scene/saveModel.ts')
  const listeners = new Set<() => void>()
  let state: { base: SceneDocument | null } = { base: null }
  const store = { getState: () => state, subscribe: (l: () => void) => (listeners.add(l), () => listeners.delete(l)) }
  const read = (rev: string, objects = [pillar]) => {
    state = { base: { unit_revs: { [`object:${objectID(1)}:content`]: rev }, scene: { objects } } as unknown as SceneDocument }
    for (const l of [...listeners]) l()
  }
  let shown: boolean | undefined
  void whenShown(store, objectID(1), '7', new AbortController().signal).then((v) => (shown = v))
  read('6')
  await Promise.resolve()
  assert.equal(shown, undefined, 'the old version is still on screen')
  read('7')
  await Promise.resolve()
  assert.equal(shown, true)
  assert.equal(listeners.size, 0, 'it stops listening')

  let gone: boolean | undefined
  void whenShown(store, objectID(1), '9', new AbortController().signal).then((v) => (gone = v))
  read('7', [])
  await Promise.resolve()
  assert.equal(gone, true, 'a deleted object ends the wait; the check then says why')
  assert.deepEqual(saveTarget(input({ objects: [], selected: [objectID(1)] })), { error: '这个物件已不在场景里' })

  read('7')
  const stop = new AbortController()
  let cancelled: boolean | undefined
  void whenShown(store, objectID(1), '99', stop.signal).then((v) => (cancelled = v))
  stop.abort()
  await Promise.resolve()
  assert.equal(cancelled, false)
  assert.equal(listeners.size, 0)
})
