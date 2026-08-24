// DEC-9 golden 对拍（TS 侧）：共享 fixtures 位于 api/golden/openings/，
// Go 权威实现（backend/internal/schedule）与前端 TS 实现必须对同一批 fixtures
// 给出逐字段一致的结果。本测试锁住「fixtures 是当前 TS 实现的诚实快照」——
// 防止 fixtures 被手工篡改或 TS 实现漂移后忘记再生成。
import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import test from 'node:test'

import { buildCalendarModel, calculateMonthOverview } from '../src/pages/calendar/model.ts'
import type { ScheduleAvailability, ScheduleSlotListItem } from '../src/api/client.ts'

const GOLDEN_DIR = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'api', 'golden', 'openings')

interface GoldenSlot {
  id: string
  type: 'shoot' | 'hold' | 'busy'
  start_at: string
  end_at: string
  order_status: string | null
}

interface GoldenFixture {
  name: string
  description: string
  timezone: string
  availability: ScheduleAvailability
  slots: GoldenSlot[]
  dates: string[]
  month: string | null
  expected: {
    days: Array<{
      date: string
      working_window: { start: string; end: string; start_at: string; end_at: string } | null
      openings: Array<{ date: string; start: string; end: string; start_at: string; end_at: string }>
    }>
    errors: Array<{ date: string; message: string }>
    month_overview: {
      shoot_count: number
      hold_days: number
      conflict_days: number
      open_days: number
      utilization: number | null
    } | null
  }
}

function loadFixtures(): GoldenFixture[] {
  const files = readdirSync(GOLDEN_DIR)
    .filter((name) => name.endsWith('.json'))
    .sort()
  // 必备用例名单：DST/月利用率等关键语义被误删且未重生成时，守护测试直接失败。
  for (const required of [
    'basic-weekday.json',
    'dst-window-resolution.json',
    'openings-boundary.json',
    'month-utilization.json',
  ]) {
    if (!files.includes(required)) {
      assert.fail(`golden fixture ${required} 缺失：关键语义用例不得删除，需求变更走 gen-openings-golden.ts 重生成`)
    }
  }
  return files.map((name) => JSON.parse(readFileSync(join(GOLDEN_DIR, name), 'utf8')) as GoldenFixture)
}

test('openings golden fixtures 与当前 TS 实现逐字段一致（DEC-9）', () => {
  const fixtures = loadFixtures()
  assert.ok(fixtures.length >= 4, 'golden fixtures 至少应覆盖 4 个用例文件')
  for (const fixture of fixtures) {
    const slots = fixture.slots.map((slot) => slot as unknown as ScheduleSlotListItem)
    const model = buildCalendarModel(slots, fixture.dates, fixture.timezone, fixture.availability)
    const actualDays = model.days.map((day) => ({
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
    assert.deepEqual(actualDays, fixture.expected.days, `${fixture.name}: days 漂移`)

    let monthOverview: GoldenFixture['expected']['month_overview'] = null
    if (fixture.month) {
      const { overview } = calculateMonthOverview(slots, fixture.month, fixture.timezone, fixture.availability)
      monthOverview = {
        shoot_count: overview.shootCount,
        hold_days: overview.holdDays,
        conflict_days: overview.conflictDays,
        open_days: overview.openDays,
        utilization: overview.utilization,
      }
    }
    assert.deepEqual(monthOverview, fixture.expected.month_overview, `${fixture.name}: month_overview 漂移`)
    assert.deepEqual(model.errors, fixture.expected.errors, `${fixture.name}: errors 漂移`)
  }
})
