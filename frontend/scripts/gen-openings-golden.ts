// 生成 dashboard-v2 openings / 利用率 golden fixtures（DEC-9 双实现对拍基线）。
//
// 期望值由当前前端 TS 实现（src/pages/calendar/model.ts）计算——双实现并存期的行为
// 基线；Go 权威实现（backend/internal/schedule/openings.go）必须逐字段对上这些文件。
// 算法语义变化时：先在这里改/加用例，重跑本脚本，再让 Go 与 TS 两端同时过测试。
//
// 运行：cd frontend && node --experimental-transform-types scripts/gen-openings-golden.ts
import { mkdirSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

import { buildCalendarModel, calculateMonthOverview } from '../src/pages/calendar/model.ts'
import type { ScheduleAvailability, ScheduleSlotListItem } from '../src/api/client.ts'

const OUTPUT_DIR = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'api', 'golden', 'openings')

interface GoldenSlot {
  id: string
  type: 'shoot' | 'hold' | 'busy'
  /** UTC ISO 时刻（秒精度，Z） */
  start_at: string
  end_at: string
  /** 引用订单状态；shoot 且 cancelled 视为取消档期（不占空档、不计利用率分子） */
  order_status: string | null
}

interface GoldenCase {
  name: string
  description: string
  timezone: string
  availability: ScheduleAvailability
  slots: GoldenSlot[]
  /** 需要计算当日工作窗/空档的本地日期（YYYY-MM-DD） */
  dates: string[]
  /** 需要计算月度利用率概览的月份（YYYY-MM）；null 跳过 */
  month: string | null
}

const cases: GoldenCase[] = [
  {
    name: 'basic-weekday',
    description:
      '常规工作日：shoot/busy 占用、取消 shoot 不占用、未配置周日 working_window=null、周六独立窗口',
    timezone: 'Asia/Shanghai',
    availability: {
      weekly: {
        1: { start: '10:00', end: '19:00' },
        2: { start: '10:00', end: '19:00' },
        3: { start: '10:00', end: '19:00' },
        4: { start: '10:00', end: '19:00' },
        5: { start: '10:00', end: '19:00' },
        6: { start: '09:00', end: '20:00' },
        7: null,
      },
      min_opening_minutes: 120,
      turnaround_minutes: 60,
    },
    slots: [
      // 07-06 周一：12:00–14:00 shoot（+08 → 04:00–06:00Z）
      { id: 's1', type: 'shoot', start_at: '2026-07-06T04:00:00Z', end_at: '2026-07-06T06:00:00Z', order_status: null },
      // 15:00–16:00 busy 占用
      { id: 's2', type: 'busy', start_at: '2026-07-06T07:00:00Z', end_at: '2026-07-06T08:00:00Z', order_status: null },
      // 16:30–17:30 已取消 shoot：不占用 → 16:00–19:00 应为完整空档
      { id: 's3', type: 'shoot', start_at: '2026-07-06T08:30:00Z', end_at: '2026-07-06T09:30:00Z', order_status: 'cancelled' },
      // 07-11 周六：08:00–10:00 shoot 与 09:00–20:00 窗口相交 1 小时
      { id: 's4', type: 'shoot', start_at: '2026-07-11T00:00:00Z', end_at: '2026-07-11T02:00:00Z', order_status: null },
    ],
    dates: ['2026-07-06', '2026-07-12'],
    month: null,
  },
  {
    name: 'dst-window-resolution',
    description:
      'America/New_York DST：2026-03-08 春跳（01:15–02:45 → 窗口跨 gap，结束时刻推移到跳跃后）与 2026-11-01 秋回（01:15 歧义取较早、02:45 稳定）',
    timezone: 'America/New_York',
    availability: {
      weekly: {
        1: null,
        2: null,
        3: null,
        4: null,
        5: null,
        6: null,
        7: { start: '01:15', end: '02:45' },
      },
      min_opening_minutes: 30,
      turnaround_minutes: 60,
    },
    slots: [
      // 秋回日 01:30–01:45（墙钟歧义段，显式 EDT 偏移锚定 = 05:30–05:45Z）
      { id: 'd1', type: 'shoot', start_at: '2026-11-01T05:30:00Z', end_at: '2026-11-01T05:45:00Z', order_status: null },
    ],
    dates: ['2026-03-08', '2026-11-01'],
    month: null,
  },
  {
    name: 'openings-boundary',
    description:
      '空档边界：正好等于 min_opening_minutes 计入、差 1 分钟不计、相邻档期间无空档、整窗被占无空档、跨午夜档期按日分别裁剪',
    timezone: 'Asia/Shanghai',
    availability: {
      weekly: {
        1: { start: '10:00', end: '19:00' },
        2: { start: '10:00', end: '19:00' },
        3: { start: '10:00', end: '19:00' },
        4: { start: '10:00', end: '19:00' },
        5: { start: '10:00', end: '19:00' },
        6: { start: '10:00', end: '19:00' },
        7: { start: '10:00', end: '19:00' },
      },
      min_opening_minutes: 120,
      turnaround_minutes: 60,
    },
    slots: [
      // 周一：12:00 shoot / 14:00 hold 相邻 → 空档 10:00–12:00 恰好 120 分钟（计入）
      { id: 'b1', type: 'shoot', start_at: '2026-07-06T04:00:00Z', end_at: '2026-07-06T06:00:00Z', order_status: null },
      { id: 'b2', type: 'hold', start_at: '2026-07-06T06:00:00Z', end_at: '2026-07-06T08:00:00Z', order_status: null },
      // 17:59 shoot → 16:00–17:59 空档 119 分钟（不计入）
      { id: 'b3', type: 'shoot', start_at: '2026-07-06T09:59:00Z', end_at: '2026-07-06T11:00:00Z', order_status: null },
      // 周二：09:00–20:00 覆盖整窗 → 无空档
      { id: 'b4', type: 'busy', start_at: '2026-07-07T01:00:00Z', end_at: '2026-07-07T12:00:00Z', order_status: null },
      // 周三 18:00 → 周四 11:00 跨午夜：周三空档 10:00–18:00；周四空档 11:00–19:00
      { id: 'b5', type: 'shoot', start_at: '2026-07-08T10:00:00Z', end_at: '2026-07-09T03:00:00Z', order_status: null },
    ],
    dates: ['2026-07-06', '2026-07-07', '2026-07-08', '2026-07-09'],
    month: null,
  },
  {
    name: 'month-utilization',
    description:
      '月度利用率（DEC-2 口径）：分子=未取消 shoot+hold 与工作窗相交分钟（busy 不计）、分母不含未配置日、取消 shoot 不计 shoot_count、跨月内跨日档期按日分段、重叠档期记 conflict 日',
    timezone: 'Asia/Shanghai',
    availability: {
      weekly: {
        1: { start: '10:00', end: '19:00' },
        2: { start: '10:00', end: '19:00' },
        3: { start: '10:00', end: '19:00' },
        4: { start: '10:00', end: '19:00' },
        5: { start: '10:00', end: '19:00' },
        6: { start: '09:00', end: '20:00' },
        7: null,
      },
      min_opening_minutes: 120,
      turnaround_minutes: 60,
    },
    slots: [
      // 07-06 周一 shoot 12:00–14:00（120 分钟分子）
      { id: 'm1', type: 'shoot', start_at: '2026-07-06T04:00:00Z', end_at: '2026-07-06T06:00:00Z', order_status: null },
      // 07-07 周二 hold 10:00–12:00（120 分钟分子 + hold 日）
      { id: 'm2', type: 'hold', start_at: '2026-07-07T02:00:00Z', end_at: '2026-07-07T04:00:00Z', order_status: null },
      // 07-08 周三 busy 10:00–18:00：不计分子，但吞掉整天空档
      { id: 'm3', type: 'busy', start_at: '2026-07-08T02:00:00Z', end_at: '2026-07-08T10:00:00Z', order_status: null },
      // 07-09 周四 已取消 shoot 10:00–13:00：不计分子、不占空档、不计 shoot_count
      { id: 'm4', type: 'shoot', start_at: '2026-07-09T02:00:00Z', end_at: '2026-07-09T05:00:00Z', order_status: 'cancelled' },
      // 07-10 周五 18:00 → 07-11 周六 11:00 跨日 shoot：周五 1 小时 + 周六 2 小时分子，shoot_count 记 1
      { id: 'm5', type: 'shoot', start_at: '2026-07-10T10:00:00Z', end_at: '2026-07-11T03:00:00Z', order_status: null },
      // 07-14 周二 重叠双 shoot 12:00–14:00 / 13:00–15:00：conflict 日，分子 12:00–15:00
      { id: 'm6', type: 'shoot', start_at: '2026-07-14T04:00:00Z', end_at: '2026-07-14T06:00:00Z', order_status: null },
      { id: 'm7', type: 'shoot', start_at: '2026-07-14T05:00:00Z', end_at: '2026-07-14T07:00:00Z', order_status: null },
      // 07-12 周日（未配置日）shoot：不入分母与分子
      { id: 'm8', type: 'shoot', start_at: '2026-07-12T02:00:00Z', end_at: '2026-07-12T04:00:00Z', order_status: null },
    ],
    dates: [],
    month: '2026-07',
  },
]

function toSlot(slot: GoldenSlot): ScheduleSlotListItem {
  // golden 只覆盖模型消费面（id/type/start_at/end_at/order_status），其余字段不参与
  return slot as unknown as ScheduleSlotListItem
}

function computeExpected(c: GoldenCase) {
  const slots = c.slots.map(toSlot)
  const model = buildCalendarModel(slots, c.dates, c.timezone, c.availability)
  const days = model.days.map((day) => ({
    date: day.date,
    working_window: day.workingWindow
      ? {
          start: day.workingWindow.start,
          end: day.workingWindow.end,
          start_at: day.workingWindow.startAt,
          end_at: day.workingWindow.endAt,
        }
      : null,
    openings: day.openings.map((opening) => ({
      date: opening.date,
      start: opening.start,
      end: opening.end,
      start_at: opening.startAt,
      end_at: opening.endAt,
    })),
  }))
  let month_overview: {
    shoot_count: number
    hold_days: number
    conflict_days: number
    open_days: number
    utilization: number | null
  } | null = null
  if (c.month) {
    const { overview } = calculateMonthOverview(slots, c.month, c.timezone, c.availability)
    month_overview = {
      shoot_count: overview.shootCount,
      hold_days: overview.holdDays,
      conflict_days: overview.conflictDays,
      open_days: overview.openDays,
      utilization: overview.utilization,
    }
  }
  return { days, errors: model.errors, month_overview }
}

mkdirSync(OUTPUT_DIR, { recursive: true })
for (const c of cases) {
  const fixture = { ...c, expected: computeExpected(c) }
  const path = join(OUTPUT_DIR, `${c.name}.json`)
  writeFileSync(path, `${JSON.stringify(fixture, null, 2)}\n`)
  console.log(`wrote ${path}`)
}
