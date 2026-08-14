import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

function source(path: string): string {
  return readFileSync(new URL(path, import.meta.url), 'utf8')
}

const openapi = source('../../api/openapi.yaml')
const schema = source('../src/api/schema.d.ts')
const card = source('../src/planning/panels/AssignmentReminderCard.tsx')
const api = source('../src/planning/api.ts')
const workspace = source('../src/planning/ShootPlanWorkspacePage.tsx')
const reminders = source('../src/pages/RemindersPage.tsx')
const dashboard = source('../src/pages/DashboardPage.tsx')

test('OpenAPI exposes GET assignment-reminders with required read-model fields', () => {
  assert.match(openapi, /\/shoot-plans\/\{id\}\/assignment-reminders:/)
  assert.match(openapi, /operationId: getShootPlanAssignmentReminders/)
  assert.match(openapi, /PlanAssignmentReminderView:/)
  for (const field of [
    'plan_id',
    'unscheduled_source_count',
    'group_id',
    'reminder_id',
    'reminder_status',
    'due_date',
    'item_count',
    'slot_id',
    'content',
    'recipient_kind',
    'delivery_modes',
  ]) {
    assert.match(openapi, new RegExp(field))
  }
  assert.match(openapi, /enum: \[account_owner\]/)
  assert.match(openapi, /enum: \[in_app, telegram_digest_if_bound\]/)
  assert.match(schema, /PlanAssignmentReminderView/)
  assert.match(schema, /"\/shoot-plans\/\{id\}\/assignment-reminders"/)
})

test('frontend types come from schema.d.ts and never hand-write DTO shapes', () => {
  assert.match(api, /export type PlanAssignmentReminderView = components\['schemas'\]\['PlanAssignmentReminderView'\]/)
  assert.match(api, /export type PlanAssignmentReminderGroup = components\['schemas'\]\['PlanAssignmentReminderGroup'\]/)
  assert.match(api, /getShootPlanAssignmentReminders/)
  assert.doesNotMatch(api, /type PlanAssignmentReminderView = \{/)
  assert.doesNotMatch(card, /account_id/)
  assert.doesNotMatch(api, /account_id/)
})

test('workspace v2 card covers empty / unscheduled / status copy without customer delivery claims', () => {
  assert.match(workspace, /AssignmentReminderCard/)
  assert.match(workspace, /tab === 'readiness'/)
  assert.match(card, /认领项检查提醒/)
  assert.match(card, /账号所有者/)
  assert.match(card, /不会向客户发任何消息/)
  assert.match(card, /等待未来拍摄档期/)
  assert.match(card, /reminder_status === 'pending'/)
  assert.match(card, /markReminderDone|onDone/)
  assert.match(card, /dismissReminder|onDismiss/)
  assert.doesNotMatch(card, /已通知客户|通知客户|客户投递|客户消息/)
  assert.doesNotMatch(card, /JSON\.parse\(.*content|content\.match|反解析/)
  assert.match(card, /getShootPlanAssignmentReminders|unscheduled_source_count/)
})

test('Reminders and Dashboard type label + plan deep link', () => {
  assert.match(reminders, /plan_assignment_checklist:\s*'认领项核对'/)
  assert.match(dashboard, /plan_assignment_checklist:\s*'认领项核对'/)
  assert.match(reminders, /\/shoot-plans\/\$\{item\.plan_id\}\?tab=readiness/)
  assert.match(dashboard, /\/shoot-plans\/\$\{reminder\.plan_id\}\?tab=readiness/)
  assert.match(reminders, /birthday:\s*'生日'/)
  assert.match(dashboard, /follow_up:\s*'回访'/)
  assert.match(reminders, /item\.customer_id/)
  assert.match(dashboard, /reminder\.customer_id/)
})

test('optional prototype readiness-reminder contract still present', async () => {
  const { spawnSync } = await import('node:child_process')
  const result = spawnSync(process.execPath, ['--test', 'scripts/planning-prototype-v2.test.mjs'], {
    cwd: process.cwd(),
    encoding: 'utf8',
  })
  assert.equal(result.status, 0, result.stdout + result.stderr)
  const prototype = source('../../docs/prototypes/creative-shoot-planning/v2/planning-workspace.html')
  assert.match(prototype, /data-od-id="readiness-reminder"/)
  assert.match(prototype, /认领项检查提醒/)
  assert.match(prototype, /不会向客户发任何消息/)
})
