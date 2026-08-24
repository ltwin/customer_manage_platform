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

test('dashboard v2 uses the wide content modifier and pairs list cards in two-col rows', () => {
  const page = readFileSync(new URL('../src/pages/DashboardPage.tsx', import.meta.url), 'utf8')
  assert.match(
    page,
    /className="content content-wide"/,
    'the dashboard main must opt into the wide content modifier so landscape screens fill instead of leaving side blanks',
  )
  const pairs = page.match(/className="two-col section-gap"/g) ?? []
  assert.equal(
    pairs.length,
    2,
    'todo+delivery and revenue+utilization must each form a paired two-col row on desktop (they stack below 1080px)',
  )
})

test('schedule utilization renders the backend percent value directly', () => {
  const page = readFileSync(new URL('../src/pages/DashboardPage.tsx', import.meta.url), 'utf8')
  assert.doesNotMatch(
    page,
    /formatPercent\(\s*utilization\.utilization/,
    'schedule_utilization.utilization is a 0-100 percent (openapi contract), but formatPercent expects a 0-1 ratio and would scale it ×100',
  )
  assert.match(
    page,
    /utilization\.utilization == null \? '—' : `\$\{utilization\.utilization\}%`/,
    'the utilization headline must append % to the backend percent value, in parity with CalendarToolbar',
  )
})
