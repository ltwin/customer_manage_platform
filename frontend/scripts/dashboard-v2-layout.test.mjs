import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

// dashboard-v2（ITEM-5）布局契约：三层结构的关键块 + 375px 移动端不崩的断点存在性。
// 与 avatar-layout.test.mjs 同模式（静态 CSS 断言，不起浏览器）。
const stylesheet = readFileSync(
  new URL('../src/pages/dashboard/dashboardV2.css', import.meta.url),
  'utf8',
)

test('dashboard v2 exposes the three-layer structural blocks', () => {
  for (const selector of [
    '.dv2-focus-strip',
    '.dv2-focus-next',
    '.dv2-timeline',
    '.dv2-tl-gap',
    '.dv2-dl-row',
    '.dv2-wf-bar',
    '.dv2-wf-seg',
    '.dv2-kpi-row',
    '.dv2-util-head',
    '.dv2-util-openings',
    '.dv2-open-chips',
    '.dv2-mx-row',
    '.dv2-mx-track',
    '.dv2-copy-text',
  ]) {
    assert.match(stylesheet, new RegExp(selector.replace('.', '\\.') + '\\s*\\{'), `missing ${selector}`)
  }
})

test('waterfall segments and matrix types carry distinct tone classes', () => {
  for (const cls of ['dv2-wf-confirmed', 'dv2-wf-receivable', 'dv2-wf-pipeline']) {
    assert.match(stylesheet, new RegExp('\\.' + cls + '\\s*\\{'), `missing ${cls}`)
  }
  for (const cls of ['dv2-mx-portrait', 'dv2-mx-cosplay', 'dv2-mx-other', 'dv2-mx-unattributed']) {
    assert.match(stylesheet, new RegExp('\\.' + cls + '\\s*\\{'), `missing ${cls}`)
  }
})

test('dashboard v2 narrows within the 430px mobile breakpoint', () => {
  const mobile = stylesheet.slice(stylesheet.indexOf('@media (max-width: 430px)'))
  assert.ok(mobile.length > 0, 'missing 430px media query')
  for (const selector of ['.dv2-kpi-row', '.dv2-mx-row', '.dv2-mx-track', '.dv2-timeline', '.dv2-focus-time']) {
    assert.match(mobile, new RegExp(selector.replace('.', '\\.') + '\\s*\\{'), `430px block missing ${selector}`)
  }
  assert.match(mobile, /\.dv2-mx-track\s*\{[^}]*min-width:\s*100%/, 'matrix track must span the full row on mobile')
})
