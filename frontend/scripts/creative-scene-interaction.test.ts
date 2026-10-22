import test from 'node:test'
import assert from 'node:assert/strict'
import {
  bindings,
  escapeTarget,
  handleKey,
  keysLabel,
  lookup,
  shortcutFor,
  type Context,
  type KeyLike,
} from '../src/creative-scene/input/keymap.ts'

const key = (code: string, extra: Partial<KeyLike> = {}): KeyLike => ({
  code, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...extra,
})
const action = (e: KeyLike, context: Context = 'idle', mac = true) => lookup(e, context, mac)?.action ?? null

test('keys dispatch by physical code, so layouts and Option characters do not matter', () => {
  assert.equal(action(key('KeyA', { key: 'å', altKey: true })), 'deselect-all')
  assert.equal(action(key('KeyG', { key: 'п' })), 'grab')
  assert.equal(action(key('KeyA', { shiftKey: true })), 'add-menu')
  assert.equal(action(key('Digit1')), 'view-front')
  assert.equal(action(key('Numpad1', { metaKey: true })), 'view-back')
})

test('mod is Cmd on macOS and Ctrl elsewhere; the other one never matches', () => {
  assert.equal(action(key('KeyZ', { metaKey: true }), 'idle', true), 'undo')
  assert.equal(action(key('KeyZ', { ctrlKey: true }), 'idle', true), null)
  assert.equal(action(key('KeyZ', { ctrlKey: true }), 'idle', false), 'undo')
  assert.equal(action(key('KeyZ', { ctrlKey: true, shiftKey: true }), 'idle', false), 'redo')
  assert.equal(action(key('KeyZ', { metaKey: true }), 'idle', false), null)
})

test('Backspace deletes on macOS only', () => {
  assert.equal(action(key('Backspace'), 'idle', true), 'delete')
  assert.equal(action(key('Backspace'), 'idle', false), null)
  assert.equal(action(key('Delete'), 'idle', false), 'delete')
})

test('typing in a field or composing with an IME is never a shortcut', () => {
  assert.equal(action(key('KeyG', { target: { tagName: 'INPUT' } })), null)
  assert.equal(action(key('KeyX', { target: { tagName: 'textarea' } })), null)
  assert.equal(action(key('KeyS', { target: { isContentEditable: true } })), null)
  assert.equal(action(key('KeyR', { isComposing: true })), null)
})

test('contexts decide what a key means', () => {
  assert.equal(action(key('KeyX')), 'delete-confirm')
  assert.equal(action(key('KeyX'), 'modal'), 'axis-x')
  assert.equal(action(key('KeyX', { shiftKey: true }), 'modal'), 'plane-x')
  assert.equal(action(key('KeyR'), 'modal'), 'rotate')
  assert.equal(action(key('Space'), 'modal'), 'confirm')
  assert.equal(action(key('Escape'), 'modal'), 'cancel')
  assert.equal(action(key('KeyG'), 'preview'), null, 'a preview is read-only')
  assert.equal(action(key('Digit7'), 'preview'), 'view-top')
  assert.equal(action(key('KeyS', { metaKey: true }), 'modal'), 'save', 'Cmd+S is caught everywhere')
})

test('no two bindings share a key in the same context, and every one is labelled', () => {
  const seen = new Set<string>()
  for (const b of bindings) {
    assert.ok(b.label && b.group, b.action)
    for (const context of b.contexts)
      for (const code of b.codes) {
        const m = b.mods ?? {}
        const k = `${context}:${code}:${!!m.mod}:${!!m.shift}:${!!m.alt}`
        assert.ok(!seen.has(k), `duplicate ${k}`)
        seen.add(k)
      }
  }
})

test('a matched key is claimed; an unmatched one is left alone', () => {
  const calls: string[] = []
  const event = (code: string) => ({
    ...key(code), preventDefault: () => calls.push('prevent'), stopPropagation: () => calls.push('stop'),
  })
  const run: string[] = []
  assert.equal(handleKey(event('KeyS'), 'idle', true, (a) => run.push(a)), true)
  assert.deepEqual(calls, ['prevent', 'stop'])
  assert.deepEqual(run, ['scale'])
  calls.length = 0
  assert.equal(handleKey(event('KeyQ'), 'idle', true, (a) => run.push(a)), false)
  assert.deepEqual(calls, [])
})

test('a held key acts once and stays claimed; only undo and redo repeat', () => {
  const calls: string[] = []
  const held = (code: string, extra: Partial<KeyLike> = {}) => ({
    ...key(code, extra), repeat: true, preventDefault: () => calls.push('prevent'), stopPropagation: () => calls.push('stop'),
  })
  const run: string[] = []
  assert.equal(handleKey(held('KeyG', { altKey: true }), 'idle', true, (a) => run.push(a)), true)
  assert.equal(handleKey(held('KeyN'), 'idle', true, (a) => run.push(a)), true)
  assert.equal(handleKey(held('KeyX'), 'modal', true, (a) => run.push(a)), true)
  assert.deepEqual(run, [])
  assert.deepEqual(calls, ['prevent', 'stop', 'prevent', 'stop', 'prevent', 'stop'], 'a repeat still never reaches the page')
  handleKey(held('KeyZ', { metaKey: true }), 'idle', true, (a) => run.push(a))
  handleKey(held('KeyZ', { metaKey: true, shiftKey: true }), 'idle', true, (a) => run.push(a))
  assert.deepEqual(run, ['undo', 'redo'], 'holding Cmd+Z steps back, as in Blender')
})

test('Esc closes the innermost layer and never the editor', () => {
  assert.equal(escapeTarget({ modal: true, menu: true, preview: true, selection: true }), 'cancel-modal')
  assert.equal(escapeTarget({ modal: false, menu: true, preview: true, selection: true }), 'close-menu')
  assert.equal(escapeTarget({ modal: false, menu: false, preview: true, selection: true }), 'exit-preview')
  assert.equal(escapeTarget({ modal: false, menu: false, preview: false, selection: true }), 'deselect')
  assert.equal(escapeTarget({ modal: false, menu: false, preview: false, selection: false }), 'none')
})

test('labels read the same way the keys work', () => {
  assert.equal(shortcutFor('add-menu', true), '⇧A')
  assert.equal(shortcutFor('add-menu', false), 'Shift + A')
  assert.equal(shortcutFor('undo', true), '⌘Z')
  assert.equal(shortcutFor('delete', true), '⌫')
  assert.equal(shortcutFor('shortcuts', false), '?')
  assert.equal(keysLabel(bindings.find((b) => b.action === 'redo')!, false), 'Ctrl + Shift + Z')
})

// --- selection ---------------------------------------------------------------

import type { SceneObject } from '../src/creative-scene/api.ts'
import {
  boxSelect,
  click,
  emptySelection,
  listClick,
  prune,
  selectAll,
  topmost,
} from '../src/creative-scene/input/selection.ts'

const sid = (n: number) => `sobj_${String(n).padStart(21, '0')}`
const so = (n: number, extra: Partial<SceneObject> = {}): SceneObject => ({
  id: sid(n), kind: 'primitive', name: '', parent_id: null, order: n,
  transform: { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }, visible: true, locked: false, ...extra,
})

test('Shift+click adds, then makes active, then removes the active object', () => {
  let s = click(emptySelection, sid(1), false)
  s = click(s, sid(2), true)
  assert.deepEqual(s, { selected: [sid(1), sid(2)], active: sid(2) })
  s = click(s, sid(1), true)
  assert.deepEqual(s, { selected: [sid(1), sid(2)], active: sid(1) }, 'selected but not active: becomes active')
  s = click(s, sid(1), true)
  assert.deepEqual(s, { selected: [sid(2)], active: sid(2) }, 'already active: deselected')
  assert.deepEqual(click(s, null, false), emptySelection, 'clicking empty space clears')
  assert.deepEqual(click(s, sid(3), false), { selected: [sid(3)], active: sid(3) })
})

test('box select replaces, adds or subtracts; select all skips locked and hidden objects', () => {
  const base = { selected: [sid(1)], active: sid(1) }
  assert.deepEqual(boxSelect(base, [sid(2), sid(3)], 'set').selected, [sid(2), sid(3)])
  assert.deepEqual(boxSelect(base, [sid(2)], 'add').selected, [sid(1), sid(2)])
  assert.deepEqual(boxSelect({ selected: [sid(1), sid(2)], active: sid(2) }, [sid(2)], 'subtract'), { selected: [sid(1)], active: sid(1) })
  const objects = [so(0, { locked: true }), so(1), so(2, { visible: false }), so(3)]
  assert.deepEqual(selectAll(objects, emptySelection).selected, [sid(1), sid(3)])
})

test('the object list toggles with mod and selects ranges with Shift', () => {
  const order = [sid(1), sid(2), sid(3), sid(4)]
  let s = listClick(emptySelection, sid(2), order, { mod: false, shift: false })
  s = listClick(s, sid(4), order, { mod: false, shift: true })
  assert.deepEqual(s.selected, [sid(2), sid(3), sid(4)])
  s = listClick(s, sid(3), order, { mod: true, shift: false })
  assert.deepEqual(s.selected, [sid(2), sid(4)])
})

test('a transform moves only the topmost selected objects, and refreshes prune missing ones', () => {
  const g = so(10, { kind: 'group' })
  const objects = [g, so(1, { parent_id: sid(10) }), so(2)]
  assert.deepEqual(topmost([sid(1), sid(10), sid(2)], objects), [sid(10), sid(2)])
  assert.deepEqual(prune({ selected: [sid(1), sid(9)], active: sid(9) }, objects), { selected: [sid(1)], active: sid(1) })
})

// --- numeric input -----------------------------------------------------------

import { numericKey, numericText, numericValues, startNumeric, type Numeric } from '../src/creative-scene/transform/numeric.ts'

function typeKeys(n: Numeric, codes: string[]): Numeric {
  for (const code of codes) n = numericKey(n, { code, shiftKey: false }) ?? n
  return n
}

test('typed values take digits, a decimal point, a sign flip, Backspace and Tab', () => {
  let n = typeKeys(startNumeric(3), ['Digit1', 'Period', 'Digit5', 'Tab', 'Minus', 'Digit2', 'Tab', 'Digit9', 'Backspace'])
  assert.deepEqual(numericValues(n), [1.5, -2, null])
  assert.equal(numericText(n, ['X', 'Y', 'Z']), 'X 1.5  Y -2  [Z 0]')
  n = numericKey(n, { code: 'Backspace', shiftKey: false })!
  assert.equal(n.index, 1, 'Backspace on an empty axis goes back')
  assert.equal(numericKey(n, { code: 'KeyG', shiftKey: false }), null, 'other keys go to the keymap')
  assert.deepEqual(numericValues(typeKeys(startNumeric(1), ['Period', 'Digit5'])), [0.5])
})

// --- modal transforms ---------------------------------------------------------

import { Matrix4, Quaternion, Ray, Vector2, Vector3 } from 'three'
import {
  cycleConstraint,
  evaluate,
  setPrecision,
  startModal,
  switchMode,
  targetFrom,
  type View,
} from '../src/creative-scene/transform/modal.ts'

// A front view: the camera looks down -Z; 100 px per metre around (500, 500).
const front: View = {
  ray: (p) => new Ray(new Vector3((p.x - 500) / 100, (500 - p.y) / 100, 100), new Vector3(0, 0, -1)),
  toScreen: (w) => new Vector2(500 + w.x * 100, 500 - w.y * 100),
  toViewer: () => new Vector3(0, 0, 1),
}
const at = (x: number, y = 0, z = 0) => ({ position: [x, y, z], rotation: [0, 0, 0, 1], scale: [1, 1, 1] })
const none = { snap: false, shift: false }
const close = (a: readonly number[], b: readonly number[], eps = 1e-6) =>
  assert.ok(a.every((v, i) => Math.abs(v - b[i]) < eps), `${JSON.stringify(a)} ≈ ${JSON.stringify(b)}`)

test('G moves in the view plane, along a locked axis, or in a locked plane', () => {
  const s = startModal('grab', [targetFrom(sid(1), at(0))], new Vector2(500, 500), front)
  close(evaluate(s, new Vector2(550, 400), none).results[0].position, [0.5, 1, 0])
  let x = cycleConstraint(s, 0, false)
  close(evaluate(x, new Vector2(550, 400), none).results[0].position, [0.5, 0, 0])
  x = cycleConstraint(cycleConstraint(s, 2, true), 0, true)
  assert.deepEqual(x.constraint, { kind: 'plane', axis: 0, space: 'global' })
  close(evaluate(x, new Vector2(550, 400), none).results[0].position, [0, 1, 0], 1e-6)
})

test('G Z 2 Enter moves exactly two metres along Z, whatever the pointer does', () => {
  let s = startModal('grab', [targetFrom(sid(1), at(1, 0, 0))], new Vector2(500, 500), front)
  s = cycleConstraint(s, 2, false)
  s = { ...s, numeric: typeKeys(s.numeric, ['Digit2']) }
  const e = evaluate(s, new Vector2(800, 100), none)
  close(e.results[0].position, [1, 0, 2])
  assert.deepEqual(e.values, [2])
})

test('pressing the same axis again uses the object\'s own axis, then unlocks', () => {
  const turned = { position: [0, 0, 0], rotation: [0, Math.SQRT1_2, 0, Math.SQRT1_2], scale: [1, 1, 1] }
  let s = startModal('grab', [targetFrom(sid(1), turned)], new Vector2(500, 500), front)
  s = cycleConstraint(s, 0, false)
  assert.equal(s.constraint.kind === 'axis' && s.constraint.space, 'global')
  s = cycleConstraint(s, 0, false)
  assert.equal(s.constraint.kind === 'axis' && s.constraint.space, 'local')
  s = { ...s, numeric: typeKeys(s.numeric, ['Digit1']) }
  close(evaluate(s, new Vector2(500, 500), none).results[0].position, [0, 0, -1], 1e-6)
  assert.deepEqual(cycleConstraint(s, 0, false).constraint, { kind: 'none' })
})

test('Ctrl snaps to 1 m, Ctrl+Shift to 0.1 m, and Shift alone slows movement from the press', () => {
  const s = startModal('grab', [targetFrom(sid(1), at(0))], new Vector2(500, 500), front)
  const x = cycleConstraint(s, 0, false)
  close(evaluate(x, new Vector2(573, 500), { snap: true, shift: false }).results[0].position, [1, 0, 0])
  close(evaluate(x, new Vector2(573, 500), { snap: true, shift: true }).results[0].position, [0.7, 0, 0])
  const slow = setPrecision(x, new Vector2(600, 500), true)
  close(evaluate(slow, new Vector2(700, 500), { snap: false, shift: true }).results[0].position, [1.1, 0, 0])
  const released = setPrecision(slow, new Vector2(700, 500), false)
  close(evaluate(released, new Vector2(700, 500), none).results[0].position, [1.1, 0, 0], 1e-6)
})

test('R turns around the view axis through the midpoint, snapping to 5°', () => {
  const s = startModal('rotate', [targetFrom(sid(1), at(-1)), targetFrom(sid(2), at(1))], new Vector2(600, 500), front)
  const e = evaluate(s, new Vector2(500, 400), none)
  assert.equal(Math.round(e.values[0]), 90)
  close(e.results[0].position, [0, -1, 0])
  close(e.results[1].position, [0, 1, 0])
  close(e.results[0].rotation, [0, 0, Math.SQRT1_2, Math.SQRT1_2])
  const snapped = evaluate(s, new Vector2(600 - 100 * (1 - Math.cos(0.82)), 500 - 100 * Math.sin(0.82)), { snap: true, shift: false })
  assert.equal(snapped.values[0], 45)
})

test('R Y 90 turns ninety degrees around Y; switching to S starts over', () => {
  let s = startModal('rotate', [targetFrom(sid(1), at(0))], new Vector2(600, 500), front)
  s = cycleConstraint(s, 1, false)
  s = { ...s, numeric: typeKeys(s.numeric, ['Digit9', 'Digit0']) }
  close(evaluate(s, new Vector2(600, 500), none).results[0].rotation, [0, Math.SQRT1_2, 0, Math.SQRT1_2])
  const sw = switchMode(s, 'scale', new Vector2(600, 500))
  assert.deepEqual(sw.constraint, { kind: 'none' })
  close(evaluate(sw, new Vector2(600, 500), none).results[0].scale, [1, 1, 1])
})

test('S scales from the midpoint, an axis lock scales the object\'s own axis, and results stay in range', () => {
  const s = startModal('scale', [targetFrom(sid(1), at(-1)), targetFrom(sid(2), at(1))], new Vector2(600, 500), front)
  const e = evaluate(s, new Vector2(700, 500), none)
  close(e.results[1].position, [2, 0, 0])
  close(e.results[1].scale, [2, 2, 2])
  const x = cycleConstraint(s, 0, false)
  close(evaluate(x, new Vector2(700, 500), none).results[0].scale, [2, 1, 1])
  const huge = { ...s, numeric: typeKeys(s.numeric, ['Digit5', 'Digit0', 'Digit0', 'Digit0']) }
  close(evaluate(huge, new Vector2(600, 500), none).results[0].scale, [1000, 1000, 1000])
})

test('a child is computed in world space and converted back to its parent\'s space', () => {
  const parentWorld = new Matrix4().compose(new Vector3(5, 0, 0), new Quaternion(0, Math.SQRT1_2, 0, Math.SQRT1_2), new Vector3(1, 1, 1))
  const child = targetFrom(sid(1), at(1), parentWorld)
  let s = startModal('grab', [child], new Vector2(500, 500), front)
  s = cycleConstraint(s, 0, false)
  s = { ...s, numeric: typeKeys(s.numeric, ['Digit1']) }
  const local = evaluate(s, new Vector2(500, 500), none).results[0]
  const world = parentWorld.clone().multiply(new Matrix4().compose(new Vector3(...local.position), new Quaternion(...local.rotation), new Vector3(...local.scale)))
  close(new Vector3().setFromMatrixPosition(world).toArray(), [6, 0, -1])
})

// --- navigation ---------------------------------------------------------------

import {
  buttonActions,
  classifyWheel,
  readPreference,
  resolveDevice,
  wheelAction,
  writePreference,
  type WheelSample,
} from '../src/creative-scene/input/navigation.ts'

const wheel = (deltaY: number, extra: Partial<WheelSample> = {}): WheelSample => ({
  deltaX: 0, deltaY, deltaMode: 0, ctrlKey: false, shiftKey: false, altKey: false, metaKey: false, t: 0, ...extra,
})

test('the wheel dollies with a mouse and orbits with a trackpad; a pinch always dollies', () => {
  assert.deepEqual(wheelAction(wheel(100), 'mouse'), { kind: 'dolly', amount: 100 })
  assert.deepEqual(wheelAction(wheel(3, { deltaMode: 1 }), 'mouse'), { kind: 'dolly', amount: 48 })
  assert.deepEqual(wheelAction(wheel(100, { shiftKey: true }), 'mouse'), { kind: 'pan', dx: 0, dy: 100 })
  assert.deepEqual(wheelAction(wheel(4, { deltaX: 2 }), 'trackpad'), { kind: 'orbit', dx: 2, dy: 4 })
  assert.deepEqual(wheelAction(wheel(4, { deltaX: 2, shiftKey: true }), 'trackpad'), { kind: 'pan', dx: 2, dy: 4 })
  for (const device of ['mouse', 'trackpad'] as const) {
    assert.equal(wheelAction(wheel(-3.5, { ctrlKey: true }), device).kind, 'dolly')
    assert.equal(wheelAction(wheel(-3, { metaKey: true }), device).kind, 'dolly')
  }
})

test('the device guess (a hypothesis until real recordings) and the manual choice', () => {
  assert.equal(classifyWheel([wheel(3, { deltaMode: 1 })]), 'mouse')
  assert.equal(classifyWheel([wheel(100), wheel(100)]), 'mouse')
  assert.equal(classifyWheel([wheel(4, { deltaX: 1.5 })]), 'trackpad')
  assert.equal(classifyWheel([wheel(2.25)]), 'trackpad')
  assert.equal(classifyWheel([wheel(2), wheel(3), wheel(1)]), 'trackpad')
  assert.equal(classifyWheel([wheel(-6, { ctrlKey: true })]), null, 'a pinch says nothing')
  assert.equal(resolveDevice('trackpad', 'mouse'), 'trackpad', 'the choice beats the guess')
  assert.equal(resolveDevice('auto', null), 'mouse')
  const store = new Map<string, string>()
  const storage = { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v), removeItem: (k: string) => void store.delete(k) }
  writePreference(storage, 'trackpad')
  assert.equal(readPreference(storage), 'trackpad')
  const broken = { getItem: () => { throw new Error('denied') }, setItem: () => { throw new Error('denied') }, removeItem: () => { throw new Error('denied') } }
  assert.equal(readPreference(broken), 'auto')
  writePreference(broken, 'mouse')
})

test('the middle button and Alt + left drag both navigate; the right button never does', () => {
  assert.deepEqual(buttonActions({ alt: false, shift: false, ctrl: false }), { left: 'none', middle: 'rotate' })
  assert.deepEqual(buttonActions({ alt: true, shift: false, ctrl: false }), { left: 'rotate', middle: 'rotate' })
  assert.deepEqual(buttonActions({ alt: true, shift: true, ctrl: false }), { left: 'truck', middle: 'truck' })
  assert.deepEqual(buttonActions({ alt: false, shift: false, ctrl: true }), { left: 'none', middle: 'dolly' })
})

// --- edits ----------------------------------------------------------------------

import { clearOp, duplicate, newPrimitive, rowOffsets, visibilityOp } from '../src/creative-scene/editing.ts'

test('a new primitive goes last at the root, named after its builtin', () => {
  const o = newPrimitive('box@1', [1, 0, 2], [so(0), so(3)], () => sid(9))
  assert.deepEqual([o.id, o.name, o.order, o.parent_id], [sid(9), '方块', 4, null])
  assert.deepEqual(o.transform.position, [1, 0, 2])
  assert.equal(o.locked, false)
})

test('a new ground goes to the origin, locked like the one a scene starts with', () => {
  const g = newPrimitive('ground@1', [1, 0, 2], [so(0)], () => sid(9))
  assert.deepEqual([g.name, g.locked, g.transform.position], ['地面', true, [0, 0, 0]])
})

test('duplicating copies descendants under new IDs and puts each copy last under its parent', () => {
  const objects = [so(10, { kind: 'group' }), so(1, { parent_id: sid(10), order: 0 }), so(2, { parent_id: sid(10), order: 1 }), so(3, { order: 11 })]
  let n = 100
  const copies = duplicate(objects, [sid(10), sid(3)], () => sid(n++))
  assert.equal(copies.length, 4)
  const g = copies.find((c) => c.kind === 'group')!
  assert.deepEqual([g.parent_id, g.order], [null, 12])
  assert.deepEqual(copies.filter((c) => c.parent_id === g.id).map((c) => c.order), [0, 1])
  assert.deepEqual(copies.find((c) => c.name === '' && c.order === 13)?.parent_id, null)
  assert.ok(copies.every((c) => !objects.some((o) => o.id === c.id)))
})

test('hide, hide unselected and reveal each make one update; clearing resets one part', () => {
  const objects = [so(1), so(2), so(3, { visible: false }), so(4, { locked: true })]
  assert.deepEqual(visibilityOp(objects, [sid(1)], 'hide'), { op: 'update_objects', updates: [{ object_id: sid(1), visible: false }] })
  assert.deepEqual(visibilityOp(objects, [sid(1)], 'hide-unselected'), { op: 'update_objects', updates: [{ object_id: sid(2), visible: false }] })
  assert.deepEqual(visibilityOp(objects, [], 'reveal'), { op: 'update_objects', updates: [{ object_id: sid(3), visible: true }] })
  const moved = so(5, { transform: { position: [1, 2, 3], rotation: [0, 0, 0, 1], scale: [2, 2, 2] } })
  assert.deepEqual(clearOp([moved], [sid(5)], 'location'), { op: 'update_objects', updates: [{ object_id: sid(5), transform: { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [2, 2, 2] } }] })
  assert.equal(clearOp([so(6)], [sid(6)], 'location'), null, 'already at the origin: nothing to clear')
  assert.deepEqual(clearOp([moved, so(6)], [sid(5), sid(6)], 'scale'), { op: 'update_objects', updates: [{ object_id: sid(5), transform: { position: [1, 2, 3], rotation: [0, 0, 0, 1], scale: [1, 1, 1] } }] })
})

// --- view space ------------------------------------------------------------------

import { axisHeadAt, viewDirections } from '../src/creative-scene/input/navigation.ts'
import { positionToDoc, positionToView, rotationToView, scaleToView, transformToDoc, transformToView } from '../src/creative-scene/space.ts'

const near = (a: readonly number[], b: readonly number[]) => a.every((v, i) => Math.abs(v - b[i]) < 1e-9)

test('the editor speaks Blender: document up is view Z, the document front faces view -Y', () => {
  assert.deepEqual(positionToView([0, 1, 0]), [0, 0, 1])
  assert.deepEqual(positionToView([0, 0, 1]), [0, -1, 0])
  assert.deepEqual(positionToDoc(positionToView([1, 2, 3])), [1, 2, 3])
  // G Z 2 in the editor raises the object two metres: document Y.
  const doc = positionToDoc([1, -3, 2 + 2])
  assert.deepEqual(doc, [1, 4, 3])
  assert.deepEqual(scaleToView([1, 2, 3]), [1, 3, 2])
})

test('a turn about the document vertical is a turn about view Z, and transforms round-trip', () => {
  const aboutUp = new Quaternion().setFromAxisAngle(new Vector3(0, 1, 0), Math.PI / 3)
  const view = rotationToView([aboutUp.x, aboutUp.y, aboutUp.z, aboutUp.w])
  const expected = new Quaternion().setFromAxisAngle(new Vector3(0, 0, 1), Math.PI / 3)
  assert.ok(near(view, [expected.x, expected.y, expected.z, expected.w]))
  const t = { position: [1, 2, 3], rotation: [0.1, 0.2, 0.3, Math.sqrt(1 - 0.14)], scale: [1, 2, 3] }
  const back = transformToDoc(transformToView(t))
  assert.ok(near(back.position, t.position) && near(back.rotation, t.rotation) && near(back.scale, t.scale))
  assert.deepEqual(transformToView({ position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }), { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }, 'identity stays identity: no -0, no turn')
})

test('number-row views stand where Blender puts them', () => {
  assert.deepEqual(viewDirections['view-front'], [0, -1, 0])
  assert.deepEqual(viewDirections['view-right'], [1, 0, 0])
  assert.deepEqual(viewDirections['view-top'], [0, 0, 1])
})

test('the axis widget finds the head under a press, preferring the one in front', () => {
  // Front view: right is +X, up is +Z, the viewer stands at -Y.
  const front = { right: { x: 1, y: 0, z: 0 }, up: { x: 0, y: 0, z: 1 }, toViewer: { x: 0, y: -1, z: 0 } }
  const c = { x: 1000, y: 100 }
  assert.deepEqual(axisHeadAt({ x: 1000, y: 60 }, c, front), [0, 0, 1], 'above the centre is +Z: the top view')
  assert.deepEqual(axisHeadAt({ x: 1042, y: 101 }, c, front), [1, 0, 0])
  assert.deepEqual(axisHeadAt({ x: 1000, y: 100 }, c, front), [0, -1, 0], '±Y overlap at the centre; -Y faces the viewer')
  assert.equal(axisHeadAt({ x: 900, y: 100 }, c, front), null)
})

import * as THREE from 'three'
import CameraControls from 'camera-controls'
import { orbitAngles } from '../src/creative-scene/input/navigation.ts'
import { parseView, readView, viewKey, ViewSaver, type SavedView } from '../src/creative-scene/viewState.ts'

test('a two-finger swipe turns the view the way the fingers go, like a middle-button drag', () => {
  // camera-controls measures its element at construction; Node has no DOM.
  ;(globalThis as Record<string, unknown>).DOMRect ??= class { x = 0; y = 0; width = 0; height = 0; top = 0; left = 0; right = 0; bottom = 0 }
  CameraControls.install({ THREE })
  const front = () => {
    const camera = new THREE.PerspectiveCamera(45, 1, 0.1, 1000)
    camera.up.set(0, 0, 1)
    const controls = new CameraControls(camera)
    void controls.setLookAt(0, -10, 0, 0, 0, 0, false)
    controls.update(0)
    return { camera, controls }
  }
  // Natural scrolling: fingers moving right report deltaX < 0, fingers moving up deltaY > 0.
  const right = front()
  void right.controls.rotate(...orbitAngles(-40, 0), false)
  right.controls.update(0)
  // The scene's front swings right, so the camera ends up on its left.
  assert.ok(right.camera.position.x < -1, `camera x ${right.camera.position.x}`)
  const up = front()
  void up.controls.rotate(...orbitAngles(0, 40), false)
  up.controls.update(0)
  // The scene's front tips up, so the camera looks from lower down.
  assert.ok(up.camera.position.z < -1, `camera z ${up.camera.position.z}`)
})

class MemoryStorage {
  data = new Map<string, string>()
  getItem(k: string) { return this.data.get(k) ?? null }
  setItem(k: string, v: string) { this.data.set(k, v) }
}

test('the view is kept per account and scene and only a well-formed one comes back', async () => {
  assert.equal(viewKey('acct-1', 'cwdoc_1'), 'creative-scene-view:acct-1:cwdoc_1')
  const view: SavedView = {
    camera: { position: [3, -8, 4], target: [1, 0, 0], ortho: true, zoom: 42 },
    selection: { selected: ['sobj_a', 'sobj_b'], active: 'sobj_b' },
    tool: 'move',
    toolbar: false,
    sidebar: true,
    sidebarTab: 'assets',
    history: true,
    lastCamera: 'sobj_cam',
    guides: ['thirds', 'diagonals'],
    display: 'rendered',
    wireBack: 'preview',
  }
  assert.deepEqual(parseView(JSON.parse(JSON.stringify(view))), view)
  const { lastCamera: _l, guides: _g, display: _d, wireBack: _w, sidebarTab: _t, ...before } = view
  assert.deepEqual(parseView(before), { ...before, sidebarTab: 'outliner', lastCamera: null, guides: [], display: null, wireBack: 'solid' }, 'a view saved before cameras and the asset panel comes back without them')
  assert.equal(parseView({ ...view, display: 'xray' })?.display, null, 'an unknown shading leaves it to the scene')
  assert.deepEqual(parseView({ ...view, guides: ['thirds', 'spiral'] })?.guides, ['thirds'], 'unknown guides are dropped')
  assert.deepEqual(parseView({ ...view, camera: null }), { ...view, camera: null })
  assert.equal(parseView({ ...view, tool: 'lasso' }), null)
  assert.equal(parseView({ ...view, selection: { selected: ['a'], active: 'b' } }), null, 'the active object is one of the selected')
  assert.equal(parseView({ ...view, camera: { ...view.camera, zoom: 0 } })?.camera, null, 'a broken camera falls back to the default view')
  assert.equal(parseView('nonsense'), null)

  const storage = new MemoryStorage() as unknown as Storage
  storage.setItem('k', '{broken')
  assert.equal(readView(storage, 'k'), null)
  const saver = new ViewSaver(storage, 'k', view)
  saver.camera({ position: [0, -5, 2], target: [0, 0, 0], ortho: false, zoom: 1 })
  saver.ui({ selection: { selected: [], active: null }, tool: 'box', toolbar: true, sidebar: false, sidebarTab: 'outliner', history: false, lastCamera: null, guides: ['center'], display: 'wireframe', wireBack: 'rendered' })
  assert.equal(storage.getItem('k'), '{broken', 'writes wait a moment')
  saver.flush()
  assert.deepEqual(readView(storage, 'k'), {
    camera: { position: [0, -5, 2], target: [0, 0, 0], ortho: false, zoom: 1 },
    selection: { selected: [], active: null },
    tool: 'box',
    toolbar: true,
    sidebar: false,
    sidebarTab: 'outliner',
    history: false,
    lastCamera: null,
    guides: ['center'],
    display: 'wireframe',
    wireBack: 'rendered',
  })
})

import { displayOf, tabAfterSelection, toggleWireframe } from '../src/creative-scene/state/sceneStore.ts'

test('the shading chosen here wins; until then the scene suggests it; Shift+Z goes to wireframe and back', () => {
  assert.equal(displayOf({ display: null }, { mode: 'lit' }), 'rendered')
  assert.equal(displayOf({ display: null }, { mode: 'clay' }), 'solid')
  assert.equal(displayOf({ display: 'preview' }, { mode: 'lit' }), 'preview')
  const wire = toggleWireframe({ wireBack: 'solid' }, 'rendered')
  assert.deepEqual(wire, { display: 'wireframe', wireBack: 'rendered' })
  assert.deepEqual(toggleWireframe(wire, 'wireframe'), { display: 'rendered', wireBack: 'rendered' })
})

test('models put in together stand in a row, gap apart, centred on the view', () => {
  assert.deepEqual(rowOffsets([2]), [0])
  assert.deepEqual(rowOffsets([1, 1], 0.5), [-0.75, 0.75])
  const [a, b, c] = rowOffsets([2, 1, 3], 0.3)
  assert.ok(Math.abs(b! - a! - (1 + 0.5 + 0.3)) < 1e-9, 'half of each width plus the gap between centres')
  assert.ok(Math.abs(c! - b! - (0.5 + 1.5 + 0.3)) < 1e-9)
  assert.ok(Math.abs(a! - 1 + 3.3) < 1e-9, 'the row is centred: left edge at -3.3')
})

test('the properties tab: selecting shows the object; clearing goes back to the scene tab stayed on, from the object tab only', () => {
  const on = (propertiesTab: 'render' | 'environment' | 'object', sceneTab: 'render' | 'environment' = 'environment') => ({ propertiesTab, sceneTab })
  assert.equal(tabAfterSelection(on('render', 'render'), false, true), 'object')
  assert.equal(tabAfterSelection(on('object', 'render'), true, false), 'render')
  assert.equal(tabAfterSelection(on('object'), true, false), 'environment')
  // Picked by hand while selected: kept, and kept on clearing.
  assert.equal(tabAfterSelection(on('environment', 'render'), true, false), 'environment')
  // No change in whether something is selected: the tab stays.
  assert.equal(tabAfterSelection(on('render', 'render'), true, true), 'render')
  assert.equal(tabAfterSelection(on('render', 'render'), false, false), 'render')
})
