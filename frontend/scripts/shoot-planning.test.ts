import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  buildExecutionWindow,
  inferExecutionWindowMode,
} from '../src/planning/executionWindow.ts'
import { shotHasExecutionHistory } from '../src/planning/history.ts'
import {
  businessDraftGenerationMessage,
  businessDraftUnavailableMessage,
  optionalAbsoluteTargetPriceYuanToCents,
  unavailableReasonLabel,
  validateOptionalAbsoluteTargetPriceYuan,
} from '../src/planning/businessDraftInput.ts'

function source(path: string): string {
  return readFileSync(new URL(path, import.meta.url), 'utf8')
}

test('business draft absolute target price preserves empty, explicit zero, and exact positive cents', () => {
  assert.equal(validateOptionalAbsoluteTargetPriceYuan(''), null)
  assert.equal(optionalAbsoluteTargetPriceYuanToCents(''), null)
  assert.equal(optionalAbsoluteTargetPriceYuanToCents('0'), 0)
  assert.equal(optionalAbsoluteTargetPriceYuanToCents('2860.50'), 286050)
  assert.equal(validateOptionalAbsoluteTargetPriceYuan('12.345'), '绝对目标价最多保留两位小数')
})

test('business draft generation reports generated and unavailable kinds separately', () => {
  assert.equal(businessDraftGenerationMessage({
    order_adjustment: { state: 'generated' },
    schedule_duration: { state: 'unavailable', reason: 'duration_unknown' },
  }), '订单价格草稿已生成；档期时长草稿不可用：预估时长仍未知。订单和档期尚未修改。')

  assert.equal(businessDraftUnavailableMessage({
    order_adjustment: { state: 'unavailable', reason: 'order_required' },
    schedule_duration: { state: 'unavailable', reason: 'duration_not_positive' },
  }), '无法生成经营草稿：订单价格草稿：需要先关联订单；档期时长草稿：预估时长必须大于零')
})

test('business draft unavailable reasons all have user-facing labels', () => {
  const reasons = [
    'order_required',
    'order_cancelled',
    'business_calculation_overflow',
    'duration_unknown',
    'duration_not_positive',
    'duration_out_of_range',
    'schedule_stage_ineligible',
    'schedule_slot_not_future',
  ] as const
  for (const reason of reasons) assert.notEqual(unavailableReasonLabel(reason), reason)
})

test('shot history acknowledgement is based on retained execution facts, not the current outcome projection', () => {
  const plan = { execution_history: [{ shot_id: 'shot-1' }, { shot_id: 'shot-2' }] }

  assert.equal(shotHasExecutionHistory(plan, 'shot-1'), true)
  assert.equal(shotHasExecutionHistory(plan, 'shot-3'), false)
  assert.equal(shotHasExecutionHistory({}, 'shot-1'), false)
})

test('shoot time defaults to an automatic two-hour live recognition buffer', () => {
  const window = buildExecutionWindow({
    startsAt: '2026-08-23T15:00',
    endsAt: '2026-08-23T21:30',
    timezone: 'Asia/Shanghai',
    mode: 'automatic',
    customLiveStartsAt: '',
    customLiveEndsAt: '',
  })

  assert.deepEqual(window, {
    startsAt: '2026-08-23T07:00:00Z',
    endsAt: '2026-08-23T13:30:00Z',
    timezone: 'Asia/Shanghai',
    liveWindowStartsAt: '2026-08-23T05:00:00Z',
    liveWindowEndsAt: '2026-08-23T15:30:00Z',
  })
})

test('custom live recognition range must contain the complete shoot time', () => {
  assert.throws(() => buildExecutionWindow({
    startsAt: '2026-08-17T00:00',
    endsAt: '2026-08-23T23:59',
    timezone: 'Asia/Shanghai',
    mode: 'custom',
    customLiveStartsAt: '2026-08-23T15:00',
    customLiveEndsAt: '2026-08-23T21:30',
  }), /现场识别范围需要包含完整的拍摄时间/)
})

test('saved two-hour buffers reopen in automatic mode while adjusted buffers remain custom', () => {
  assert.equal(inferExecutionWindowMode({
    starts_at: '2026-08-23T07:00:00Z',
    ends_at: '2026-08-23T13:30:00Z',
    live_window_starts_at: '2026-08-23T05:00:00Z',
    live_window_ends_at: '2026-08-23T15:30:00Z',
  }), 'automatic')
  assert.equal(inferExecutionWindowMode({
    starts_at: '2026-08-23T07:00:00Z',
    ends_at: '2026-08-23T13:30:00Z',
    live_window_starts_at: '2026-08-23T04:00:00Z',
    live_window_ends_at: '2026-08-23T15:30:00Z',
  }), 'custom')
})

test('shoot time keeps system-level live recognition settings in a collapsed advanced section', () => {
  const panel = source('../src/planning/panels/BriefPanel.tsx')

  assert.match(panel, /<h2>拍摄时间<\/h2>/)
  assert.match(panel, /<span>拍摄开始<\/span>/)
  assert.match(panel, /<span>拍摄结束<\/span>/)
  assert.match(panel, /<details className="planning-advanced-settings">/)
  assert.match(panel, /<summary><Settings2[^>]*aria-hidden="true"[^>]*\/><span>高级设置<\/span>/)
  assert.match(panel, /现场范围自动设置/)
  assert.match(panel, /拍摄地点时区/)
  assert.doesNotMatch(panel, /<span>IANA 时区<\/span>|<span>现场窗口开始<\/span>|<span>现场窗口结束<\/span>/)
})

test('planning API reuses generated OpenAPI types and never accepts account scope or capture mode from callers', () => {
  const api = source('../src/planning/api.ts')

  assert.match(api, /import type \{ components, paths \} from '\.\.\/api\/schema'/)
  assert.doesNotMatch(api, /interface\s+(?:Create|Update|Plan|Shot|Readiness)[A-Za-z]*Input/)
  assert.doesNotMatch(api, /account_id/)
  assert.doesNotMatch(api, /capture_mode/)
})

test('planning workspace stays in AppShell while Run Mode is independently authenticated outside it', () => {
  const app = source('../src/App.tsx')
  const shell = source('../src/components/AppShell.tsx')

  assert.match(app, /<RequireAuth status=\{auth\.status\}>[\s\S]*<AppShell \/>[\s\S]*<Route path="\/shoot-plans"/)
  assert.match(app, /<Route path="\/shoot-plans\/:id"/)
  assert.match(app, /<Route path="\/settings"[^\n]*\/>[\s\S]*<Route path="\/shoot-plans\/:id\/run" element=\{<RequireAuth status=\{auth\.status\}><ShootPlanRunPage \/><\/RequireAuth>\} \/>/)
  assert.match(shell, /label: '策划', to: '\/shoot-plans'/)
})

test('workspace exposes the approved core sections, share collaboration, and private business workbench', () => {
  const workspace = source('../src/planning/ShootPlanWorkspacePage.tsx')

  assert.equal((workspace.match(/<TabButton\s/g) ?? []).length, 7)
  for (const coreTab of ['创作 brief', '镜头表', '准备项', '参考素材', '分享协作', '执行历史', '经营草稿']) {
    assert.match(workspace, new RegExp(`>${coreTab}(?:\\s|<|\\{)`))
  }
  for (const deferredEntry of ['分享反馈', '素材摄取', 'AI 脚本', 'AI 分镜']) {
    assert.doesNotMatch(workspace, new RegExp(`>${deferredEntry}<`))
  }
})

test('business workbench keeps facts, drafts, and destructive acknowledgement on private generated APIs', () => {
  const panel = source('../src/planning/panels/BusinessPanel.tsx')
  const api = source('../src/planning/api.ts')

  assert.match(panel, /仅你可见/)
  assert.match(panel, /留空表示未知，0 表示明确为零/)
  assert.match(panel, /draft_kinds:\s*\['order_adjustment', 'schedule_duration'\]/)
  assert.match(panel, /draft\.required_acknowledgement/)
  assert.match(panel, /window\.confirm/)
  assert.match(panel, /kind:\s*'planning-schedule-prefill-v1'/)
  assert.doesNotMatch(panel, /localStorage|sessionStorage|searchParams|business_draft_id/)
  assert.match(api, /`\/shoot-plans\/\$\{encodeURIComponent\(planID\)\}\/business-drafts`/)
  assert.match(api, /business-drafts\/\$\{encodeURIComponent\(draftID\)\}\/apply`/)
})

test('shared plan and planning list surfaces contain no private business signal', () => {
  const openapi = source('../../api/openapi.yaml')
  const sharedSchemas = openapi.slice(
    openapi.indexOf('    SharedCreativeBriefV1:'),
    openapi.indexOf('    ShareViewLevel:'),
  )
  const listOperation = openapi.slice(
    openapi.indexOf('  /shoot-plans:\n'),
    openapi.indexOf('    post:\n', openapi.indexOf('  /shoot-plans:\n')),
  )
  const anonymousHandler = source('../../backend/internal/platform/httpapi/plan_share_anonymous.go')
  const listRepository = source('../../backend/internal/shootplanning/repository.go')
  const listQuery = listRepository.slice(
    listRepository.indexOf('func (PostgresRepository) List('),
    listRepository.indexOf('func (PostgresRepository) Detail('),
  )
  const sharedDOM = [
    '../src/planning/share/api.ts',
    '../src/planning/share/SharedCommon.tsx',
    '../src/planning/share/SharedProposalView.tsx',
    '../src/planning/share/SharedFullSections.tsx',
    '../src/planning/share/SharedFullView.tsx',
    '../src/planning/share/SharedPlanPage.tsx',
  ].map(source).join('\n')
  const listDOM = source('../src/planning/ShootPlansPage.tsx')
  const privateSignals = /planning_business|business|经营|报价|价格|rule_version|draft|facts/i

  assert.ok(sharedSchemas.length > 0)
  assert.ok(listOperation.length > 0)
  assert.ok(listQuery.length > 0)
  assert.doesNotMatch(sharedSchemas, privateSignals)
  assert.doesNotMatch(anonymousHandler, privateSignals)
  assert.doesNotMatch(sharedDOM, privateSignals)
  assert.doesNotMatch(listOperation, privateSignals)
  assert.doesNotMatch(listQuery, /planning_business|business/i)
  assert.doesNotMatch(listDOM, /business|经营|报价|价格/i)
})

test('calendar consumes the planning prefill once and submits only the ordinary schedule body', () => {
  const calendar = source('../src/pages/CalendarPage.tsx')
  const dialog = source('../src/components/schedule/ScheduleSlotDialog.tsx')
  const prefill = source('../src/components/schedule/businessPrefill.ts')
  const client = source('../src/api/client.ts')

  assert.match(calendar, /parsePlanningSchedulePrefill\(location\.state\)/)
  assert.match(calendar, /replace:\s*true, state:\s*null/)
  assert.match(calendar, /listOrders\(\{ id: prefill\.order_id, page: 1, pageSize: 1 \}\)/)
  assert.match(calendar, /fetchCustomer\(order\.customer_id\)/)
  assert.match(dialog, /startDate:\s*''[\s\S]*startTime:\s*''/)
  assert.match(dialog, /businessDurationMinutes\?: number/)
  assert.match(dialog, /businessDurationMinutes: prefill\.basisMinutes/)
  assert.match(dialog, /applyBusinessDurationStart\(next, timezone\)/)
  assert.match(dialog, /businessDurationMinutes: undefined/)
  assert.doesNotMatch(dialog, /businessDurationLinked/)
  assert.match(prefill, /Temporal\.Instant\.from\(resolved\.instant\)\.add\(\{ minutes: basisMinutes \}\)/)
  assert.match(client, /if \(params\.id\) search\.set\('id', params\.id\)/)
  assert.match(dialog, /function createSlotBody[\s\S]*start_at:[\s\S]*end_at:[\s\S]*order_id/)
  assert.doesNotMatch(dialog.match(/function createSlotBody[\s\S]*?\n\}/)?.[0] ?? '', /basis|prefill|business|draft_id/)
})

test('business settings submit a complete replacement map with inherit, unknown, and explicit zero modes', () => {
  const section = source('../src/account/settings/PlanningBusinessRulesSection.tsx')
  const page = source('../src/account/settings/AccountSettingsPage.tsx')
  const draft = source('../src/account/settings/businessRulesDraft.ts')

  assert.match(section, /只影响新生成草稿/)
  for (const mode of ['继承', '未知', '自定义']) assert.match(section, new RegExp(`>${mode}<`))
  assert.match(section, /expected_revision:\s*settings\.planning_business_rule_revision/)
  assert.match(section, /overrides:\s*businessRuleOverridesFromDraft\(draft\)/)
  assert.match(section, /reason\.status === 409[\s\S]*onRefresh\(\)/)
  assert.match(page, /onRefresh=\{\(\) => setReloadTick/)
  assert.match(draft, /if \(entry\.mode === 'inherit'\) continue/)
  assert.match(draft, /result\[descriptor\.key\] = null/)
  assert.match(draft, /packagePriceYuanToCents/)
})

test('business draft generation response is a kind-specific closed union', () => {
  const openapi = source('../../api/openapi.yaml')
  const generated = source('../src/api/schema.d.ts')

  assert.match(openapi, /OrderBusinessDraftGenerationItem:[\s\S]*oneOf:[\s\S]*GeneratedOrderBusinessDraftItem[\s\S]*UnavailableOrderBusinessDraftItem/)
  assert.match(openapi, /ScheduleBusinessDraftGenerationItem:[\s\S]*oneOf:[\s\S]*GeneratedScheduleBusinessDraftItem[\s\S]*UnavailableScheduleBusinessDraftItem/)
  assert.match(generated, /OrderBusinessDraftUnavailableReason: "order_required" \| "order_cancelled" \| "business_calculation_overflow";/)
  assert.match(generated, /ScheduleBusinessDraftUnavailableReason: "order_required" \| "duration_unknown" \| "duration_not_positive" \| "duration_out_of_range" \| "schedule_stage_ineligible" \| "schedule_slot_not_future";/)
  assert.doesNotMatch(openapi, /^    BusinessDraftGenerationItem:/m)
  assert.doesNotMatch(openapi, /^    UnavailableBusinessDraftItem:/m)
})

test('business mutation retries retain canonical keys until 2xx and do not misreport reload failures', () => {
  const panel = source('../src/planning/panels/BusinessPanel.tsx')
  const workspace = source('../src/planning/ShootPlanWorkspacePage.tsx')

  assert.match(panel, /business-generate:\$\{plan\.id\}:\$\{JSON\.stringify\(body\)\}/)
  assert.match(panel, /business-decision:\$\{plan\.id\}:\$\{draft\.id\}:\$\{JSON\.stringify\(body\)\}/)
  assert.equal((panel.match(/getMutationKey\(signature,/g) ?? []).length, 2)
  assert.equal((panel.match(/acknowledgeMutation\(signature\)/g) ?? []).length, 2)
  assert.doesNotMatch(panel, /newPlanningMutationKey/)
  assert.match(panel, /acknowledgeMutation\(signature\)[\s\S]*草稿生成已提交，但最新页面加载失败/)
  assert.match(panel, /acknowledgeMutation\(signature\)[\s\S]*草稿操作已提交，但最新页面加载失败/)
  assert.match(panel, /shouldRefreshBusinessDraft\(cause\)[\s\S]*await onReload\(\)/)
  assert.match(workspace, /const getMutationKey = useCallback[\s\S]*pendingKeys\.current\.set\(signature, key\)/)
  assert.match(workspace, /<BusinessPanel[\s\S]*key=\{plan\.id\}[\s\S]*getMutationKey=\{getMutationKey\}/)
})

test('archive confirmation submits the exact server projection instead of reconstructing effects', () => {
  const workspace = source('../src/planning/ShootPlanWorkspacePage.tsx')

  assert.match(workspace, /const acknowledgement = plan\.required_archive_acknowledgement/)
  assert.match(workspace, /payload: acknowledgement/)
  assert.doesNotMatch(workspace, /effects:\s*\[/)
})

test('revision conflicts refresh server state while dirty brief, scale, and window drafts remain local', () => {
  const workspace = source('../src/planning/ShootPlanWorkspacePage.tsx')
  const brief = source('../src/planning/panels/BriefPanel.tsx')
  const presentation = source('../src/planning/presentation.ts')

  assert.match(workspace, /error\.code === 'plan_revision_conflict'[\s\S]*await load\(true\)/)
  assert.match(workspace, /页面已刷新，请核对保留的输入后重新保存/)
  assert.equal((brief.match(/if \(dirty && !planChanged\) return/g) ?? []).length, 3)
  assert.equal((brief.match(/const \[dirty, setDirty\] = useState\(false\)/g) ?? []).length, 3)
  assert.match(presentation, /已保留你的输入；请核对最新版本后重新保存/)
})

test('planning mutations always carry an idempotency key and retain scoped retry keys', () => {
  const api = source('../src/planning/api.ts')
  const workspace = source('../src/planning/ShootPlanWorkspacePage.tsx')
  const ledger = source('../src/planning/ShootPlansPage.tsx')

  assert.ok((api.match(/'Idempotency-Key': idempotencyKey/g) ?? []).length >= 6)
  assert.match(workspace, /pendingKeys\.current\.get\(signature\) \?\? newPlanningMutationKey\(scope\)/)
  assert.match(workspace, /pendingKeys\.current\.delete\(signature\)/)
  assert.match(ledger, /const \[key\] = useState\(\(\) => newPlanningMutationKey\('create'\)\)/)
})

test('Run Mode updates saved state only after a successful response and keeps failed actions retryable', () => {
  const runPage = source('../src/planning/ShootPlanRunPage.tsx')
  const runState = source('../src/planning/runState.ts')

  assert.match(runPage, /const response = await appendShootPlanShotResult[\s\S]*pendingActionKeys\.current\.delete[\s\S]*applySavedShotResult/)
  assert.match(runPage, /pendingActionKeys\.current\.get\(signature\) \?\? newPlanningMutationKey/)
  assert.match(runPage, /当前镜头和已保存状态保持不变，可直接重试同一动作/)
  assert.match(runPage, /openContext\.current[\s\S]*openShootPlanRunSession\(id, context\.expectedRevision, openKey\.current\)/)
  assert.match(runState, /execution_fact_revision: response\.execution_fact_revision/)
  assert.match(runState, /execution_revision: response\.execution_revision/)
})

test('Run Mode DOM is execution-only and exposes captured, skipped, cleared, and navigation controls', () => {
  const runPage = source('../src/planning/ShootPlanRunPage.tsx')

  for (const action of ['完成拍摄', '跳过本镜', '清除本镜结果', '上一镜', '下一镜']) {
    assert.match(runPage, new RegExp(action))
  }
  for (const structuralControl of ['新增镜头', '编辑镜头', '移除镜头', '新增准备项', '编辑准备项', '关联准备项', '上传素材', '分享反馈', '经营草稿']) {
    assert.doesNotMatch(runPage, new RegExp(structuralControl))
  }
  assert.doesNotMatch(runPage, /AppShell|capture_mode\s*:/)
})

test('Run Mode has mobile, coarse-pointer, focus, and high-contrast light contracts', () => {
  const css = source('../src/planning/run.css')

  assert.match(css, /@media \(max-width: 520px\)/)
  assert.match(css, /@media \(pointer: coarse\)[\s\S]*min-height:\s*52px/)
  assert.match(css, /\.run-button:focus-visible[\s\S]*outline:\s*4px solid/)
  assert.match(css, /--run-text:\s*#111827/)
  assert.match(css, /--run-surface:\s*#ffffff/)
  assert.doesNotMatch(css, /overflow-x:\s*(?:auto|scroll)/)
})

test('planning responsive contract avoids dense tables and preserves coarse-pointer targets', () => {
  const css = source('../src/planning/planning.css')
  const accountMenuCSS = source('../src/account/account-menu.css')
  const planningSources = [
    '../src/planning/ShootPlansPage.tsx',
    '../src/planning/ShootPlanWorkspacePage.tsx',
    '../src/planning/panels/BriefPanel.tsx',
    '../src/planning/panels/ShotsPanel.tsx',
    '../src/planning/panels/ReadinessPanel.tsx',
    '../src/planning/panels/ExecutionHistoryPanel.tsx',
  ].map(source).join('\n')

  assert.match(css, /@media \(max-width: 430px\)/)
  assert.match(css, /@media \(pointer: coarse\)[\s\S]*min-height:\s*44px/)
  assert.match(css, /\.planning-panel \.field,[\s\S]*?\.planning-panel \.field-row,[\s\S]*?\.planning-panel \.input \{\s*min-width:\s*0;/)
  assert.match(css, /\.planning-panel \.input \{\s*width:\s*100%;/)
  assert.match(css, /\.planning-workspace \.btn,\s*\.planning-workspace \.tab \{\s*min-height:\s*44px;/)
  assert.match(css, /\.planning-workspace-topbar \.btn \{\s*min-height:\s*44px;/)
  assert.match(css, /\.planning-workspace \.btn\.btn-sm,[\s\S]*?\.planning-workspace \.tab,[\s\S]*?\.planning-workspace \.planning-panel \.btn \{\s*min-height:\s*44px;/)
  assert.match(css, /@media \(max-width: 768px\)[\s\S]*grid-template-columns:\s*1fr/)
  assert.match(css, /@media \(max-width: 768px\)[\s\S]*\.planning-workspace-topbar[\s\S]*flex-wrap:\s*wrap/)
  assert.match(accountMenuCSS, /@media \(max-width: 860px\)[\s\S]*\.main > \.topbar[\s\S]*padding-right:\s*4\.5rem/)
  assert.match(css, /\.planning-business-form-grid\s*\{[\s\S]*?display:\s*grid;[\s\S]*?grid-template-columns:\s*repeat\(4, minmax\(0, 1fr\)\)/)
  assert.match(css, /\.planning-business-form-grid \.planning-field\s*\{[\s\S]*?display:\s*grid;[\s\S]*?min-width:\s*0/)
  assert.match(css, /\.planning-business-form-grid input\s*\{[\s\S]*?width:\s*100%;[\s\S]*?min-width:\s*0/)
  assert.doesNotMatch(planningSources, /<table\b|overflow-x:\s*scroll/)
})
