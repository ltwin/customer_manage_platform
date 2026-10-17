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
