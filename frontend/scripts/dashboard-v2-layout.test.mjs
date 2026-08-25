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
    3,
    'todo+delivery, revenue+utilization and health+matrix must each form a paired two-col row on desktop (they stack below 1080px)',
  )
})

test('customer health card pairs with the channel matrix and discloses its criteria', () => {
  const page = readFileSync(new URL('../src/pages/DashboardPage.tsx', import.meta.url), 'utf8')
  assert.match(page, /健康度分层/, 'the L3 customer-assets row must render the health cohort card')
  assert.match(
    page,
    /判定口径/,
    'the health card must disclose the tier criteria (ratio thresholds and baselines) instead of a black box',
  )
  // 口径说明必须引用后端回显的 thresholds，而不是前端写死——参数化后口径与判定要快照一致
  assert.match(
    page,
    /health\.thresholds|customer_health\.thresholds|healthThresholds/,
    'the criteria disclosure must reference the thresholds echoed back by the API, not hardcoded constants',
  )
  // 交叉引用：高危层标注已有 churn 提醒的客户数（两套流失口径分离 + 桥接）
  assert.match(
    page,
    /type === 'churn'|type: 'churn'/,
    'the at_risk tier must cross-reference churn reminders from due_reminders (separate-but-bridged cadence policies)',
  )
  const stylesheet = readFileSync(
    new URL('../src/pages/dashboard/dashboardV2.css', import.meta.url),
    'utf8',
  )
  for (const selector of ['.dv2-cohort-bar', '.dv2-cohort-seg', '.dv2-risk-row', '.dv2-risk-gauge']) {
    assert.match(stylesheet, new RegExp(selector.replace('.', '\\.') + '\\s*\\{'), `missing ${selector}`)
  }
})

test('receivable sheet queries the delivered-unsettled scope matching the card count', () => {
  const page = readFileSync(new URL('../src/pages/DashboardPage.tsx', import.meta.url), 'utf8')
  // 卡片计数 = delivered ∧ !balance_paid（roadmap §4.3 窄口径）；弹层取数必须同口径组合查询，
  // 否则副标题「N 笔已交付未结清」与列表行数对不上（2026-08-25 远端实测反馈）
  assert.match(
    page,
    /listOrders\(\{ status: 'delivered', unpaidBalance: true, pageSize: 100 \}\)/,
    'the receivable sheet must compose status=delivered with unpaid_balance so the rows match the card count and total',
  )
  assert.match(
    page,
    /未交付阶段的未结清/,
    'the sheet must disclose that pre-delivery unpaid orders live in the orders-page 未收尾款 filter',
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
