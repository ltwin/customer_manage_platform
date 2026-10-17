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
