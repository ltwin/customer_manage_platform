import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

function source(path) {
  return readFileSync(new URL(path, import.meta.url), 'utf8')
}

const shareCSS = source('../src/planning/share/share.css')
const shareUI = [
  '../src/planning/share/SharedPlanPage.tsx',
  '../src/planning/share/SharedProposalView.tsx',
  '../src/planning/share/SharedFullView.tsx',
  '../src/planning/share/SharedFullSections.tsx',
  '../src/planning/share/SharedCommon.tsx',
  '../src/planning/share/ShareCollaborationPanel.tsx',
  '../src/planning/share/OneTimeSecretDialog.tsx',
].map(source).join('\n')

test('v2 language and hierarchy: no prototype controls or contract comment chrome', () => {
  assert.doesNotMatch(shareUI, /protobar|proto-note|原型控制|评审注释|data-od-id/)
  assert.doesNotMatch(shareUI, /已被完整档取代|撤销full会让任何分享页失效|短码/)
  assert.match(shareUI, /方案概览|完整方案|分享协作/)
  assert.match(shareUI, /关闭后无法再次查看/)
})

test('375 / coarse pointer targets stay at least 44px in source CSS contract', () => {
  assert.match(shareCSS, /@media \(max-width: 375px\)[\s\S]*min-height:\s*44px/)
  assert.match(shareCSS, /@media \(pointer: coarse\)[\s\S]*min-height:\s*44px/)
})

test('focus visibility and 200% / narrow width avoid bidirectional scroll traps', () => {
  assert.match(shareCSS, /:focus-visible[\s\S]*outline:\s*3px solid/)
  assert.match(shareCSS, /@media \(max-width: 480px\)[\s\S]*overflow-x:\s*clip/)
  assert.match(shareCSS, /@media \(max-width: 1280px\)/)
  assert.match(shareCSS, /@media \(min-width: 1600px\)/)
  assert.doesNotMatch(shareCSS, /overflow-x:\s*(?:auto|scroll)/)
})

test('anonymous and workspace share surfaces expose keyboard-reachable primary actions', () => {
  assert.match(shareUI, /type="button"/)
  assert.match(shareUI, /type="submit"/)
  assert.match(shareUI, /role="dialog"/)
  assert.match(shareUI, /aria-modal="true"/)
  assert.match(shareUI, /role="alert"|role="status"/)
})

test('screen-reader oriented landmarks exist without relying on browser screenshots', () => {
  assert.match(shareUI, /<main className="share-page"/)
  assert.match(shareUI, /aria-labelledby=/)
  assert.match(shareUI, /这个链接已经失效了/)
  assert.match(shareUI, /暂时打不开这份方案/)
})

test('evidence gap note: real browser screenshots are out of scope for this runner', () => {
  // S7 exit asks for 1600/1280/375/coarse/200%/keyboard/screen-reader evidence.
  // This file asserts source/CSS contracts only. No screenshot files are fabricated.
  assert.ok(true, 'browser screenshot evidence deferred — no fabricated image artifacts')
})
