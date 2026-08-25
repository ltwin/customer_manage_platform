import assert from 'node:assert/strict'
import test from 'node:test'

import {
  channelLabels,
  computeUpcomingOpenings,
  deliveryStageLabels,
  formatPercent,
  formatShortYuan,
  formatYuan,
  healthCopyText,
  localMinutesOfInstant,
  upcomingLocalDates,
  v2DeliveryRows,
  v2FocusModel,
  v2HealthCounts,
  v2HealthRows,
  v2HealthTierHint,
  v2HealthTierMeta,
  v2HealthTierOrder,
  v2MatrixModel,
  v2TimelineModel,
  v2WaterfallModel,
} from '../src/pages/dashboard/dashboardV2Model.ts'
import type {
  DashboardV2,
  OrderListItem,
  ScheduleSlotListItem,
} from '../src/api/client.ts'
import type { Opening } from '../src/pages/calendar/model.ts'

const timezone = 'Asia/Shanghai'

const shootSlot = (over: Record<string, unknown> = {}): ScheduleSlotListItem => ({
  id: 'slot-1',
  account_id: 'acc-1',
  created_at: '2026-07-01T00:00:00Z',
  start_at: '2026-07-14T06:00:00Z',
  end_at: '2026-07-14T08:30:00Z',
  type: 'shoot',
  order_id: 'ord-1',
  order_status: 'scheduled',
  customer_id: 'cus-1',
  customer_display_name: '阿茶',
  package_name: '日系写真',
  note: '棚拍 A 棚',
  ...over,
}) as ScheduleSlotListItem

const orderItem = (over: Record<string, unknown> = {}): OrderListItem => ({
  id: 'ord-1',
  account_id: 'acc-1',
  created_at: '2026-06-01T00:00:00Z',
  customer_id: 'cus-1',
  status: 'retouching',
  deposit_paid: true,
  balance_paid: false,
  amount_paid: 50000,
  channel_snapshot: 'douyin',
  delivery_due_is_override: false,
  shot_at: '2026-07-08T04:00:00Z',
  customer_display_name: '阿茶',
  ...over,
}) as OrderListItem

function buildV2(over: Partial<DashboardV2> = {}): DashboardV2 {
  return {
    next_shoot: null,
    today_slots: [],
    today_openings: { working_window: null, openings: [] },
    delivery_queue: { count: 0, items: [] },
    revenue_waterfall: {
      confirmed: { current: 0, previous: 0 },
      receivable: { total: 0, count: 0 },
      pipeline: { total: 0, count: 0 },
      cash_received_30d: 0,
    },
    schedule_utilization: {
      month: '2026-07',
      shoot_count: 0,
      hold_days: 0,
      open_days: 0,
      conflict_days: 0,
    },
    channel_matrix: { rows: [], grand_total: 0 },
    due_reminders: [],
    ...over,
  } as DashboardV2
}

test('formatYuan formats cents into zh-CN yuan display', () => {
  assert.equal(formatYuan(128000), '¥1,280')
  assert.equal(formatYuan(0), '¥0')
  assert.equal(formatYuan(null), '—')
  assert.equal(formatYuan(undefined), '—')
  assert.equal(formatShortYuan(21600000), '¥21.6万')
  assert.equal(formatShortYuan(98000), '¥980')
  assert.equal(formatPercent(0.5), '50%')
  assert.equal(formatPercent(0.6667), '66.67%')
  assert.equal(formatPercent(null), '—')
})

test('channel and delivery stage labels cover contract enums', () => {
  assert.equal(channelLabels.douyin, '抖音')
  assert.equal(channelLabels.xiaohongshu, '小红书')
  assert.equal(channelLabels.other, '其他')
  assert.equal(deliveryStageLabels.shot, '待选片')
  assert.equal(deliveryStageLabels.selected, '待精修')
  assert.equal(deliveryStageLabels.retouching, '精修中')
})

test('localMinutesOfInstant converts instant minutes in account timezone', () => {
  assert.equal(localMinutesOfInstant('2026-07-14T06:00:00Z', timezone), 14 * 60)
  assert.equal(
    localMinutesOfInstant('2026-07-14T16:00:00Z', timezone, '2026-07-14'),
    24 * 60,
  )
})

test('focus model reports in-progress countdown for today shoot', () => {
  const model = v2FocusModel(
    shootSlot(),
    timezone,
    new Date('2026-07-14T05:30:00Z'),
  )
  assert.equal(model.state, 'today')
  assert.equal(model.customerName, '阿茶')
  assert.equal(model.countdownLabel, '30 分钟后')
  assert.equal(model.startLabel, '14:00')
})

test('focus model reports later date for non-today next shoot', () => {
  const model = v2FocusModel(
    shootSlot({ start_at: '2026-07-20T02:00:00Z', end_at: '2026-07-20T05:00:00Z' }),
    timezone,
    new Date('2026-07-14T05:30:00Z'),
  )
  assert.equal(model.state, 'later')
  assert.equal(model.whenLabel, '7/20（周一）')
})

test('focus model falls back to empty state without next shoot', () => {
  const model = v2FocusModel(null, timezone, new Date('2026-07-14T05:30:00Z'))
  assert.equal(model.state, 'none')
})

test('timeline model orders items and includes server openings', () => {
  const data = buildV2({
    today_slots: [
      shootSlot({ id: 's-b', start_at: '2026-07-14T09:00:00Z', end_at: '2026-07-14T10:00:00Z' }),
      shootSlot({ id: 's-a', start_at: '2026-07-14T02:00:00Z', end_at: '2026-07-14T03:30:00Z' }),
    ],
    today_openings: {
      working_window: { start: '09:00', end: '21:00', start_at: '2026-07-14T01:00:00Z', end_at: '2026-07-14T13:00:00Z' },
      openings: [
        {
          date: '2026-07-14',
          start: '11:30',
          end: '13:00',
          start_at: '2026-07-14T03:30:00Z',
          end_at: '2026-07-14T05:00:00Z',
        },
      ],
    },
  })
  const model = v2TimelineModel(
    data.today_slots,
    data.today_openings.working_window,
    data.today_openings.openings,
    timezone,
    new Date('2026-07-14T04:00:00Z'),
  )
  assert.equal(model.items[0]?.id, 's-a')
  assert.equal(model.items[1]?.id, 's-b')
  assert.equal(model.dayStart, 9 * 60)
  assert.equal(model.dayEnd, 21 * 60)
  assert.deepEqual(model.openings, [{ start: '11:30', end: '13:00' }])
  assert.equal(model.nowMin, 12 * 60)
})

test('timeline model expands bounds beyond working window for early slots', () => {
  const model = v2TimelineModel(
    [shootSlot({ start_at: '2026-07-14T00:30:00Z', end_at: '2026-07-14T01:30:00Z' })],
    null,
    [],
    timezone,
    new Date('2026-07-14T04:00:00Z'),
  )
  assert.ok(model.dayStart <= 8 * 60 + 30)
  assert.ok(model.dayEnd >= 20 * 60)
})

test('waterfall model builds segments, trend and kpis from contract data', () => {
  const model = v2WaterfallModel({
    confirmed: { current: 90000, previous: 60000, change_ratio: 0.5 },
    receivable: { total: 50000, count: 2 },
    pipeline: { total: 193000, count: 7 },
    cash_received_30d: 115000,
    average_order_value: 90000,
    repeat_customer_ratio_90d: 0.6667,
    oldest_receivable_age_days: 3,
  })
  assert.equal(model.confirmedCents, 90000)
  assert.equal(model.trendLabel, '+50% 环比')
  assert.equal(model.trendUp, true)
  const confirmed = model.segments.find((segment) => segment.key === 'confirmed')
  const receivable = model.segments.find((segment) => segment.key === 'receivable')
  const pipeline = model.segments.find((segment) => segment.key === 'pipeline')
  assert.ok(confirmed && receivable && pipeline)
  assert.equal(confirmed.valueCents, 90000)
  assert.equal(receivable.note, '2 笔已交付未结清')
  assert.equal(pipeline.note, '7 笔已定档～精修中')
  const total = 90000 + 50000 + 193000
  assert.equal(Math.round(pipeline!.percent), Math.round((193000 / total) * 100))
  const kpiLabels = model.kpis.map((kpi) => kpi.label)
  assert.deepEqual(kpiLabels, ['近 30 天已收现金', '客单价', '90 天复购占比', '待收账龄'])
  const aov = model.kpis.find((kpi) => kpi.label === '客单价')
  assert.equal(aov?.value, '¥900')
  const repeat = model.kpis.find((kpi) => kpi.label === '90 天复购占比')
  assert.equal(repeat?.value, '66.67%')
  const aging = model.kpis.find((kpi) => kpi.label === '待收账龄')
  assert.equal(aging?.value, '3 天')
})

test('waterfall model handles zero-previous trend and missing derived metrics', () => {
  const model = v2WaterfallModel({
    confirmed: { current: 0, previous: 0 },
    receivable: { total: 0, count: 0 },
    pipeline: { total: 0, count: 0 },
    cash_received_30d: 0,
  })
  assert.equal(model.trendLabel, '上期无数据')
  assert.equal(model.trendUp, null)
  const aov = model.kpis.find((kpi) => kpi.label === '客单价')
  assert.equal(aov?.value, '—')
  const aging = model.kpis.find((kpi) => kpi.label === '待收账龄')
  assert.equal(aging?.value, '—')
})

test('delivery rows map stage labels, overdue copy and gauge progress', () => {
  const rows = v2DeliveryRows(
    [
      { order: orderItem(), days_left: -4, overdue: true },
      { order: orderItem({ id: 'ord-2', status: 'shot', shot_at: '2026-07-12T04:00:00Z', delivery_due_at: '2026-07-26' }), days_left: 12, overdue: false },
      { order: orderItem({ id: 'ord-3', status: 'shot', shot_at: null, delivery_due_at: undefined }), days_left: undefined, overdue: false },
    ],
    14,
    timezone,
    '2026-07-14',
  )
  assert.equal(rows[0]?.stageLabel, '精修中')
  assert.equal(rows[0]?.dueLabel, '已逾期 4 天')
  assert.equal(rows[0]?.progressPct, 100)
  assert.equal(rows[1]?.dueLabel, '剩 12 天')
  assert.ok(rows[1]!.progressPct! > 0 && rows[1]!.progressPct! < 100)
  assert.equal(rows[2]?.dueLabel, '无应交日')
  assert.equal(rows[2]?.progressPct, null)
})

test('matrix model maps labels, per-type segments and grand total percent', () => {
  const model = v2MatrixModel({
    rows: [
      {
        channel: 'douyin',
        portrait: 120000,
        cosplay: 60000,
        other: 0,
        unattributed: 20000,
        total: 200000,
        order_count: 5,
        customer_count: 3,
      },
      {
        channel: 'xiaohongshu',
        portrait: 0,
        cosplay: 100000,
        other: 0,
        unattributed: 0,
        total: 100000,
        order_count: 2,
        customer_count: 2,
      },
    ],
    grand_total: 300000,
  })
  assert.equal(model.rows[0]?.channelLabel, '抖音')
  assert.equal(model.rows[0]?.totalPercent, 67)
  const douyinSegments = model.rows[0]?.segments.filter((segment) => segment.cents > 0)
  assert.equal(douyinSegments?.length, 3)
  const unattributed = model.rows[0]?.segments.find((segment) => segment.type === 'unattributed')
  assert.equal(unattributed?.label, '未归因')
  assert.equal(unattributed?.cents, 20000)
  assert.equal(model.insight, '抖音 贡献 67% 收入，写真 占大头。')
})

test('upcomingLocalDates steps date-only days without timezone drift', () => {
  const dates = upcomingLocalDates('2026-07-31', 3)
  assert.deepEqual(dates, ['2026-07-31', '2026-08-01', '2026-08-02'])
})

test('computeUpcomingOpenings reuses calendar model openings for future days', () => {
  const availability = {
    weekly: {
      1: { start: '09:00', end: '21:00' },
      2: { start: '09:00', end: '21:00' },
      3: { start: '09:00', end: '21:00' },
      4: { start: '09:00', end: '21:00' },
      5: { start: '09:00', end: '21:00' },
      6: { start: '09:00', end: '21:00' },
      7: { start: '09:00', end: '21:00' },
    },
    min_opening_minutes: 90,
    turnaround_minutes: 30,
  }
  const openings: Opening[] = computeUpcomingOpenings(
    [],
    '2026-07-14',
    2,
    timezone,
    availability,
  )
  assert.equal(openings.length, 2)
  assert.equal(openings[0]?.date, '2026-07-14')
  assert.equal(openings[0]?.start, '09:00')
  assert.equal(openings[0]?.end, '21:00')
})

// ============ customer_health（客户资产·健康度分层）============

const healthFixture = {
  total: 6,
  thresholds: { sleeping_ratio: 1.2, at_risk_ratio: 2, lost_ratio: 3.5, fallback_cadence_days: 120 },
  tiers: {
    active: {
      count: 1,
      items: [{
        customer_id: 'cus-a', display_name: '阿茶', channel: 'douyin',
        created_at: '2026-07-01', shots: 2, since_days: 14, cadence_days: 16,
        ratio: 0.88, baseline: 'personal', settled_ltv: 50000, unsettled_paid: 30000,
      }],
    },
    sleeping: { count: 1, items: [] },
    at_risk: {
      count: 2,
      items: [
        {
          customer_id: 'cus-r1', display_name: '夜见', channel: 'weibo',
          created_at: '2025-10-01', shots: 1, since_days: 296, cadence_days: 120,
          ratio: 2.47, baseline: 'fallback', settled_ltv: 300000, unsettled_paid: 0,
        },
        {
          customer_id: 'cus-r2', display_name: '苏晚', channel: 'xiaohongshu',
          created_at: '2025-12-01', shots: 1, since_days: 266, cadence_days: 120,
          ratio: 2.22, baseline: 'fallback', settled_ltv: 120000, unsettled_paid: 8000,
        },
      ],
    },
    lost: { count: 1, items: [] },
    new: {
      count: 1,
      items: [{
        customer_id: 'cus-n', display_name: '小新', channel: 'referral',
        created_at: '2026-08-01', shots: 0, baseline: 'none',
        settled_ltv: 0, unsettled_paid: 0,
      }],
    },
  },
} as DashboardV2['customer_health']

test('v2HealthTierOrder exposes the five cohort tiers in display order', () => {
  assert.deepEqual(v2HealthTierOrder, ['active', 'sleeping', 'at_risk', 'lost', 'new'])
  assert.equal(v2HealthTierMeta('at_risk').label, '高危')
  assert.equal(v2HealthTierMeta('new').label, '新客')
})

test('v2HealthTierHint reflects the effective thresholds instead of hardcoded defaults', () => {
  const defaults = healthFixture.thresholds
  assert.equal(v2HealthTierHint('at_risk', defaults), '超节奏 2–3.5 倍 · 按累计贡献排序')
  const custom = { sleeping_ratio: 1.5, at_risk_ratio: 2.5, lost_ratio: 4, fallback_cadence_days: 90 }
  assert.equal(v2HealthTierHint('sleeping', custom), '超节奏 1.5–2.5 倍')
  assert.equal(v2HealthTierHint('lost', custom), '超节奏 4 倍以上 · 按累计贡献排序')
})

test('v2HealthRows renders personal-baseline rows with dual money columns', () => {
  const rows = v2HealthRows(healthFixture, 'active', '2026-08-24')
  assert.equal(rows.length, 1)
  const row = rows[0]
  assert.equal(row.displayName, '阿茶')
  assert.equal(row.channelLabel, '抖音')
  assert.equal(row.ratioText, '0.88×')
  assert.equal(row.sinceDays, 14)
  assert.equal(row.cadenceDays, 16)
  assert.equal(row.fallbackBaseline, false)
  assert.equal(row.settledText, '¥500')
  assert.equal(row.unsettledText, '¥300')
  assert.equal(row.gaugePercent, 25) // 0.88 / lost_ratio 3.5 × 100
})

test('v2HealthRows marks fallback-baseline rows and renders new-customer rows without ratio', () => {
  const atRisk = v2HealthRows(healthFixture, 'at_risk', '2026-08-24')
  assert.equal(atRisk.length, 2)
  assert.equal(atRisk[0].customerId, 'cus-r1') // 服务端已按 settled_ltv 排序，前端保序
  assert.equal(atRisk[0].fallbackBaseline, true)
  assert.equal(atRisk[0].unsettledText, null) // unsettled_paid=0 不显示次要行
  assert.equal(atRisk[1].unsettledText, '¥80')

  const fresh = v2HealthRows(healthFixture, 'new', '2026-08-24')
  assert.equal(fresh.length, 1)
  assert.equal(fresh[0].ratioText, null)
  assert.equal(fresh[0].sinceDays, null)
  assert.equal(fresh[0].createdDaysAgo, 23)
})

test('v2HealthCounts derives cohort bar shares from tier counts', () => {
  const counts = v2HealthCounts(healthFixture)
  assert.deepEqual(
    counts.map((entry) => [entry.key, entry.count]),
    [['active', 1], ['sleeping', 1], ['at_risk', 2], ['lost', 1], ['new', 1]],
  )
  assert.equal(counts[0].percent, 17) // round(1/6*100)
})

test('healthCopyText speaks the tier language with concrete facts', () => {
  const rows = v2HealthRows(healthFixture, 'at_risk', '2026-08-24')
  const winback = healthCopyText('winback', rows[0])
  assert.ok(winback.includes('夜见'), 'winback copy must address the customer by name')
  assert.ok(winback.includes('296'), 'winback copy must mention days since last shoot')

  const fresh = v2HealthRows(healthFixture, 'new', '2026-08-24')
  const first = healthCopyText('first', fresh[0])
  assert.ok(first.includes('小新'))
  assert.ok(first.includes('23'), 'first-order copy must mention days since signup')

  const active = v2HealthRows(healthFixture, 'active', '2026-08-24')
  const care = healthCopyText('care', active[0])
  assert.ok(care.includes('阿茶'), 'care copy must address the customer by name')
})
