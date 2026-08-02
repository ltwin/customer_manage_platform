import assert from 'node:assert/strict'
import test from 'node:test'

import {
  applyAvailabilityDraft,
  fromSettings,
  validateAvailabilityDraft,
} from '../src/pages/settings/availabilityDraft.ts'
import type { Settings, UpdateSettingsBody } from '../src/api/client.ts'

const settingsFixture: Settings = {
  timezone: 'Asia/Shanghai',
  birthday_lead_days: 4,
  follow_up_after_days: 8,
  digest_hour: 10,
  churn_thresholds: [
    { shoot_type: 'portrait', days: 120 },
    { shoot_type: 'cosplay', days: 150 },
    { shoot_type: 'other', days: 180 },
  ],
  availability: {
    weekly: {
      1: { start: '09:30', end: '18:30' },
      2: null,
      3: { start: '10:00', end: '19:00' },
      4: null,
      5: { start: '11:00', end: '20:00' },
      6: { start: '09:00', end: '17:00' },
      7: null,
    },
    min_opening_minutes: 90,
    turnaround_minutes: 45,
  },
}

test('availability draft hydrates all seven days and does not mutate server settings', () => {
  const draft = fromSettings(settingsFixture)

  assert.deepEqual(draft, {
    weekly: settingsFixture.availability.weekly,
    minOpeningMinutes: 90,
    turnaroundMinutes: 45,
  })

  const monday = draft.weekly[1]
  assert.ok(monday)
  monday.start = '08:00'
  draft.weekly[2] = { start: '10:00', end: '12:00' }

  assert.equal(settingsFixture.availability.weekly[1]?.start, '09:30')
  assert.equal(settingsFixture.availability.weekly[2], null)
})

test('availability draft validates HH:MM windows and requires end after start', () => {
  assert.deepEqual(validateAvailabilityDraft(fromSettings(settingsFixture)), { ok: true })

  const malformed = fromSettings(settingsFixture)
  malformed.weekly[1] = { start: '9:30', end: '18:30' }
  assert.deepEqual(validateAvailabilityDraft(malformed), {
    ok: false,
    field: 'weekly.1.start',
    message: '周一开始时间须为 HH:MM',
  })

  const reversed = fromSettings(settingsFixture)
  reversed.weekly[3] = { start: '19:00', end: '10:00' }
  assert.deepEqual(validateAvailabilityDraft(reversed), {
    ok: false,
    field: 'weekly.3',
    message: '周三结束时间须晚于开始时间',
  })

  const outOfRange = fromSettings(settingsFixture)
  outOfRange.weekly[5] = { start: '11:00', end: '24:00' }
  assert.deepEqual(validateAvailabilityDraft(outOfRange), {
    ok: false,
    field: 'weekly.5.end',
    message: '周五结束时间须为 HH:MM',
  })
})

test('availability draft enforces opening and turnaround minute bounds', () => {
  const lowerBounds = fromSettings(settingsFixture)
  lowerBounds.minOpeningMinutes = 15
  lowerBounds.turnaroundMinutes = 0
  assert.deepEqual(validateAvailabilityDraft(lowerBounds), { ok: true })

  const upperBounds = fromSettings(settingsFixture)
  upperBounds.minOpeningMinutes = 480
  upperBounds.turnaroundMinutes = 240
  assert.deepEqual(validateAvailabilityDraft(upperBounds), { ok: true })

  const shortOpening = fromSettings(settingsFixture)
  shortOpening.minOpeningMinutes = 14
  assert.deepEqual(validateAvailabilityDraft(shortOpening), {
    ok: false,
    field: 'minOpeningMinutes',
    message: '最小可报空档须为 15–480 分钟的整数',
  })

  const longTurnaround = fromSettings(settingsFixture)
  longTurnaround.turnaroundMinutes = 241
  assert.deepEqual(validateAvailabilityDraft(longTurnaround), {
    ok: false,
    field: 'turnaroundMinutes',
    message: '转场缓冲须为 0–240 分钟的整数',
  })
})

test('availability serializer replaces only availability in the current writable settings snapshot', () => {
  const currentBody: UpdateSettingsBody = {
    timezone: 'America/New_York',
    birthday_lead_days: 6,
    follow_up_after_days: 12,
    digest_hour: 21,
    churn_thresholds: [
      { shoot_type: 'portrait', days: 91 },
      { shoot_type: 'cosplay', days: 121 },
      { shoot_type: 'other', days: 151 },
    ],
    availability: settingsFixture.availability,
  }
  const draft = fromSettings(settingsFixture)
  draft.weekly[2] = { start: '13:00', end: '17:30' }
  draft.weekly[7] = null
  draft.minOpeningMinutes = 75
  draft.turnaroundMinutes = 30

  const body = applyAvailabilityDraft(currentBody, draft)

  assert.deepEqual(body, {
    ...currentBody,
    availability: {
      weekly: draft.weekly,
      min_opening_minutes: 75,
      turnaround_minutes: 30,
    },
  })
  assert.notStrictEqual(body, currentBody)
  assert.notStrictEqual(body.churn_thresholds, currentBody.churn_thresholds)
  assert.notStrictEqual(body.availability?.weekly, draft.weekly)

  body.availability!.weekly[2]!.start = '14:00'
  assert.equal(draft.weekly[2]?.start, '13:00')
  assert.equal(currentBody.availability?.weekly[2], null)
})

// D11（account-center-hardening）：旧 SettingsPage 整表 freeze / settingsSaveInFlightRef
// 断言已删除；由 test:account-center 的 A6/A11 fieldset disabled 与 A5c settingsSaveQueue 承接。
