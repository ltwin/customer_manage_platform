import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  buildExecutionWindow,
  inferExecutionWindowMode,
} from '../src/planning/executionWindow.ts'
import { shotHasExecutionHistory } from '../src/planning/history.ts'
import {
  mainShotAction,
  normalizeRunNotes,
  runOutcomeCounts,
  runShotState,
  validateSkipSubmission,
} from '../src/planning/runState.ts'
import {
  businessDraftGenerationMessage,
  businessDraftUnavailableMessage,
  optionalAbsoluteTargetPriceYuanToCents,
  unavailableReasonLabel,
  validateOptionalAbsoluteTargetPriceYuan,
} from '../src/planning/businessDraftInput.ts'
import {
  deriveTransitionGuidance,
  isTransitionGuidanceCode,
} from '../src/planning/transitionGuidance.ts'
import {
  crmSummaryLine,
  listStatusLines,
  windowRangeLabel,
} from '../src/planning/listCard.ts'
import {
  shotPositionLabel,
  shotReferenceLabel,
} from '../src/planning/share/shotReference.ts'
import {
  captureModeShortLabel,
  preparationMissingShotPositions,
  readinessTag,
  skipReasonLabel,
} from '../src/planning/outcomeLabels.ts'

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
  assert.match(shell, /label: '创意空间',\s*to: '\/creative'/)
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
  assert.match(panel, /<ConfirmDialog/)
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
  assert.match(ledger, /createShootPlan\(\{ title: '未命名策划', subject: '待补充' \}, newPlanningMutationKey\('create'\)\)/)
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

test('Run Mode keeps optional per-shot field notes that are submitted with captured and skipped results', () => {
  const runPage = source('../src/planning/ShootPlanRunPage.tsx')

  assert.equal(normalizeRunNotes('   '), undefined)
  assert.equal(normalizeRunNotes('  正装假发未带到场  '), '正装假发未带到场')
  assert.match(runPage, /<span>现场备注<\/span>/)
  assert.match(runPage, /placeholder="只记你需要的，不必填写"/)
  assert.match(runPage, /备注跟着这一镜保存，切换镜头不会丢/)
  assert.match(runPage, /maxLength=\{1000\}/)
  assert.match(runPage, /normalizeRunNotes\(shotNotes\[shot\.id\] \?\? ''\)/)
  assert.match(runPage, /\{ notes: note \}/)
})

test('Run Mode skips require an explicit reason and a supplementary note for other', () => {
  const runPage = source('../src/planning/ShootPlanRunPage.tsx')

  assert.equal(validateSkipSubmission('', '已写过备注'), '请先选择跳过原因，再确认跳过。')
  assert.equal(validateSkipSubmission('other', '   '), '选了「其他」时，需要写明补充说明。')
  assert.equal(validateSkipSubmission('other', '假发未带到场，改到补拍'), null)
  assert.equal(validateSkipSubmission('time_insufficient', ''), null)
  assert.match(runPage, /useState<ShootPlanSkipReason \| ''>\(''\)/)
  assert.match(runPage, /<option value="" disabled>选择跳过原因<\/option>/)
  assert.match(runPage, /validateSkipSubmission\(skipReason, note \?\? ''\)/)
  assert.match(runPage, /跳过必须写原因/)
})

test('Run Mode shot states map outcomes to segment states and counts', () => {
  assert.equal(runShotState({ current_outcome: null }), 'pending')
  assert.equal(runShotState({ current_outcome: { result: 'captured' } }), 'ok')
  assert.equal(runShotState({ current_outcome: { result: 'skipped' } }), 'skip')
  assert.equal(runShotState({ current_outcome: { result: 'cleared' } }), 'pending')
  assert.deepEqual(runOutcomeCounts([
    { current_outcome: { result: 'captured' } },
    { current_outcome: { result: 'skipped' } },
    { current_outcome: { result: 'skipped' } },
    { current_outcome: null },
  ]), { captured: 1, skipped: 2 })
})

test('Run Mode progress is a clickable segmented bar opening the shot list sheet', () => {
  const runPage = source('../src/planning/ShootPlanRunPage.tsx')
  const css = source('../src/planning/run.css')

  assert.match(runPage, /aria-label="镜头进度，可点击跳转"/)
  assert.match(runPage, /run-seg run-seg-\$\{i === currentIndex \? 'now' : runShotState\(/)
  assert.match(runPage, /setCurrentIndex\(i\)/)
  assert.match(runPage, /setListOpen\(true\)/)
  assert.match(runPage, /第 \{currentIndex \+ 1\} 镜 \/ 共 \{input\.shots\.length\} 镜 ▾/)
  assert.match(runPage, /已捕获 \{counts\.captured\} · 已跳过 \{counts\.skipped\}/)

  assert.match(runPage, /role="dialog"\s+aria-modal="true"\s+aria-labelledby="runListTitle"/)
  assert.match(runPage, /点任意一条直接跳转；顺序即执行顺序/)
  assert.match(runPage, /aria-current=\{i === currentIndex \|\| undefined\}/)
  assert.match(runPage, /Escape/)
  for (const tag of ['已捕获', '已跳过', '当前', '待执行']) assert.match(runPage, new RegExp(tag))

  assert.match(css, /\.run-seg-ok::after[\s\S]*?background:/)
  assert.match(css, /\.run-seg-skip::after/)
  assert.match(css, /\.run-seg-now::after/)
  assert.match(css, /@media \(pointer: coarse\)[\s\S]*?\.run-seg \{ min-height: 44px; \}/)
  assert.match(css, /\.run-sheet-card/)
  assert.doesNotMatch(css, /run-progress-track/)
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

test('transition guidance derives what is missing from fresh plan data', () => {
  const plan = {
    readiness_items: [
      { requirement: 'required', preflight_status: 'checked', title: '已核对项' },
      { requirement: 'required', preflight_status: 'unchecked', title: '旧书店 15:00 时段确认' },
      { requirement: 'optional', preflight_status: 'unchecked', title: '干冰机' },
    ],
    shots: [
      { current_outcome: { result: 'captured' }, title: '全景' },
      { current_outcome: null, title: '正装立像' },
      { current_outcome: null, title: '侧脸特写' },
    ],
  }

  assert.deepEqual(deriveTransitionGuidance('readiness_incomplete', plan), {
    kind: 'readiness',
    missingTitles: ['旧书店 15:00 时段确认'],
  })
  assert.deepEqual(deriveTransitionGuidance('shots_incomplete', plan), {
    kind: 'shots',
    total: 3,
    remaining: 2,
  })
  assert.deepEqual(deriveTransitionGuidance('shots_incomplete', { readiness_items: [], shots: [] }), {
    kind: 'shots',
    total: 0,
    remaining: 0,
  })
  assert.equal(deriveTransitionGuidance('archived_read_only', plan), null)
})

test('transition guidance falls back to retry when fresh data no longer shows the blocker', () => {
  const resolved = {
    readiness_items: [{ requirement: 'required', preflight_status: 'checked', title: '已核对项' }],
    shots: [{ current_outcome: { result: 'skipped' }, title: '全景' }],
  }

  assert.deepEqual(deriveTransitionGuidance('readiness_incomplete', resolved), { kind: 'retry' })
  assert.deepEqual(deriveTransitionGuidance('shots_incomplete', resolved), { kind: 'retry' })
})

test('transition guidance codes are exactly the two server rejection codes', () => {
  assert.equal(isTransitionGuidanceCode('readiness_incomplete'), true)
  assert.equal(isTransitionGuidanceCode('shots_incomplete'), true)
  assert.equal(isTransitionGuidanceCode('plan_revision_conflict'), false)
  assert.equal(isTransitionGuidanceCode('invalid_plan_transition'), false)
})

test('workspace transition rejections render guidance cards with jump actions instead of bare errors', () => {
  const page = source('../src/planning/ShootPlanWorkspacePage.tsx')

  assert.match(page, /deriveTransitionGuidance\(/)
  assert.match(page, /isTransitionGuidanceCode\(/)
  assert.match(page, /还不能标记已就绪/)
  assert.match(page, /去准备项核对/)
  assert.match(page, /还不能标记完成/)
  assert.match(page, /既没捕获也没跳过/)
  assert.match(page, /去镜头表看看/)
  assert.match(page, /进入现场模式/)
  assert.match(page, /还没有镜头/)
})

test('readiness panel warns which required items block marking ready while the action stays in the topbar', () => {
  const panel = source('../src/planning/panels/ReadinessPanel.tsx')

  assert.match(panel, /完成核对前不能标记已就绪/)
  assert.match(panel, /标记按钮在右上角/)
})

test('feedback go-edit-shot focuses, highlights, and opens the referenced shot in the shots tab', () => {
  const page = source('../src/planning/ShootPlanWorkspacePage.tsx')
  const shots = source('../src/planning/panels/ShotsPanel.tsx')
  const share = source('../src/planning/share/ShareCollaborationPanel.tsx')

  assert.match(page, /focusShotID=\{/)
  assert.match(page, /shotLabels=\{/)
  assert.match(shots, /focusShotID/)
  assert.match(shots, /id=\{`planning-shot-\$\{shot\.id\}`\}/)
  assert.match(shots, /scrollIntoView/)
  assert.match(shots, /is-focused/)
  assert.match(share, /shotReferenceLabel\(/)
  assert.match(share, /removedShotLabel/)
  assert.match(source('../src/planning/share/shotReference.ts'), /已移除的镜头/)
  assert.doesNotMatch(share, /第 \$\{item\.target\.shot_id\} 镜/)
})

test('shot reference labels use two-digit positions with titles', () => {
  assert.equal(shotReferenceLabel(7, '正装立像'), '第 07 镜 · 正装立像')
  assert.equal(shotReferenceLabel(112, 'X'), '第 112 镜 · X')
  assert.equal(shotPositionLabel(7), '第 07 镜')
})

test('plan list card summaries derive CRM, window, and progress lines from enriched list fields', () => {
  assert.equal(crmSummaryLine(undefined), '无客户 · 无订单（灵感存档）')
  assert.equal(crmSummaryLine({
    customer_id: 'cus_1', customer_name: '阿晚', order_id: null, order_title: null, order_status_at_link: null,
  }), '客户 阿晚')
  assert.equal(crmSummaryLine({
    customer_id: 'cus_1', customer_name: '阿晚', order_id: 'ord_1', order_title: 'ORD-2098', order_status_at_link: 'scheduled',
  }), '关联订单 ORD-2098（已定档）')
  assert.equal(crmSummaryLine({
    customer_id: 'cus_1', customer_name: '阿晚', order_id: 'ord_1', order_title: null, order_status_at_link: null,
  }), '客户 阿晚')

  assert.equal(windowRangeLabel('2026-08-16T01:30:00Z', '2026-08-16T08:30:00Z', 'Asia/Shanghai'), '08-16 09:30–16:30')
  assert.equal(windowRangeLabel('not-a-date', '2026-08-16T08:30:00Z'), '执行时间待定')

  assert.deepEqual(listStatusLines({
    public_scale: { planned_shot_count: 12, planned_look_count: null, planned_scene_count: null },
    execution_stats: { captured_count: 5, skipped_count: 1 },
    readiness_summary: { required_total: 6, required_unchecked: 0 },
  }), ['已捕获 5 / 跳过 1', '必需准备 6/6 已核对'])
  assert.deepEqual(listStatusLines({
    public_scale: { planned_shot_count: 9, planned_look_count: null, planned_scene_count: null },
    execution_stats: { captured_count: 0, skipped_count: 0 },
    readiness_summary: { required_total: 6, required_unchecked: 0 },
  }), ['必需准备 6/6 已核对', '尚未开始现场执行'])
  assert.deepEqual(listStatusLines({
    public_scale: { planned_shot_count: 5, planned_look_count: null, planned_scene_count: null },
  }), ['尚未开始现场执行'])
})

test('plan list page renders the enriched three-line card structure', () => {
  const page = source('../src/planning/ShootPlansPage.tsx')

  assert.match(page, /crmSummaryLine\(/)
  assert.match(page, /windowRangeLabel\(/)
  assert.match(page, /listStatusLines\(/)
  assert.match(page, /未设执行时间/)
})

test('Run Mode main action is stateful for one-step undo and re-judge', () => {
  assert.deepEqual(mainShotAction(null), { result: 'captured', label: '✓ 完成拍摄', tone: 'primary' })
  assert.deepEqual(mainShotAction({ result: 'captured' }), { result: 'cleared', label: '已拍摄 · 点击撤销', tone: 'secondary' })
  assert.deepEqual(mainShotAction({ result: 'skipped' }), { result: 'captured', label: '已跳过 · 改为已拍摄', tone: 'secondary' })
  assert.deepEqual(mainShotAction({ result: 'cleared' }), { result: 'captured', label: '✓ 完成拍摄', tone: 'primary' })

  const runPage = source('../src/planning/ShootPlanRunPage.tsx')
  assert.match(runPage, /const mainAction = mainShotAction\(shot\?\.current_outcome\)/)
  assert.match(runPage, /saveResult\(mainAction\.result\)/)
  assert.match(runPage, /run-button-undo/)
})

test('Run Mode shot card renders all five taxonomy tags with unfilled placeholders', () => {
  const runPage = source('../src/planning/ShootPlanRunPage.tsx')

  assert.match(runPage, /shotTagFields\.map/)
  assert.match(runPage, /\{field\.label\} \{shotTagLabel\(field\.key, value\)\}/)
  assert.match(runPage, /className=\{`run-tag\$\{value \? '' : ' is-empty'\}`\}/)
})

test('Run Mode completion card shows split stats and end-of-session actions', () => {
  const runPage = source('../src/planning/ShootPlanRunPage.tsx')

  assert.match(runPage, /已捕获 \{counts\.captured\} · 已跳过 \{counts\.skipped\} · 共 \{input\.shots\.length\} 镜/)
  assert.match(runPage, /结束本场 · 回工作台/)
  assert.match(runPage, /再看看/)
  assert.match(runPage, /已跳过的镜头会保留原因，之后可安排补拍/)
  assert.match(runPage, /setCompleteDismissed/)
})

test('outcome labels map skip reasons, capture modes, and readiness requirement states', () => {
  assert.equal(skipReasonLabel('preparation_missing'), '准备物料缺失')
  assert.equal(skipReasonLabel('other'), '其他')
  assert.equal(skipReasonLabel(null), '未说明')
  assert.equal(skipReasonLabel('weird'), '未说明')
  assert.equal(captureModeShortLabel('live'), '现场')
  assert.equal(captureModeShortLabel('backfill'), '补记')
  assert.equal(captureModeShortLabel(undefined), '未判定时段')

  assert.deepEqual(readinessTag({ requirement: 'required', preflight_status: 'checked' }, true), { label: '必需 · 现场缺失', className: 'badge badge-danger' })
  assert.deepEqual(readinessTag({ requirement: 'optional', preflight_status: 'checked' }, true), { label: '现场缺失', className: 'badge badge-danger' })
  assert.deepEqual(readinessTag({ requirement: 'required', preflight_status: 'unchecked' }, false), { label: '必需 · 待核对', className: 'badge badge-warning' })
  assert.deepEqual(readinessTag({ requirement: 'required', preflight_status: 'checked' }, false), { label: '必需', className: 'badge badge-accent' })
  assert.deepEqual(readinessTag({ requirement: 'optional', preflight_status: 'unchecked' }, false), { label: '可选', className: 'badge badge-muted' })
})

test('preparation-missing site flags derive only from linked skipped shots', () => {
  const shots = [
    { position: 5, readiness_item_ids: ['r1'], current_outcome: { result: 'skipped', skip_reason: 'preparation_missing' } },
    { position: 6, readiness_item_ids: ['r1'], current_outcome: { result: 'skipped', skip_reason: 'time_insufficient' } },
    { position: 7, readiness_item_ids: ['r1', 'r2'], current_outcome: { result: 'skipped', skip_reason: 'preparation_missing' } },
    { position: 8, readiness_item_ids: ['r1'], current_outcome: null },
    { position: 9, readiness_item_ids: [], current_outcome: { result: 'skipped', skip_reason: 'preparation_missing' } },
  ]
  assert.deepEqual(preparationMissingShotPositions(shots, 'r1'), [5, 7])
  assert.deepEqual(preparationMissingShotPositions(shots, 'r2'), [7])
  assert.deepEqual(preparationMissingShotPositions(shots, 'r3'), [])
})

test('workspace shot cards show full taxonomy, capture-mode badges, capture time, and duplicate action', () => {
  const shots = source('../src/planning/panels/ShotsPanel.tsx')

  assert.match(shots, /shotTagFields\.map/)
  assert.match(shots, /\{field\.label\} \{shotTagLabel\(field\.key, value\)\}/)
  assert.match(shots, /已捕获 · \{captureModeShortLabel\(outcome\.capture_mode\)\}/)
  assert.match(shots, /skipReasonLabel\(outcome\.skip_reason\)/)
  assert.match(shots, /捕获于/)
  assert.match(shots, /setDuplicating/)
  assert.match(shots, /复制为新镜头/)
})

test('readiness list groups by category with claimant names and on-site missing flags', () => {
  const readiness = source('../src/planning/panels/ReadinessPanel.tsx')

  assert.match(readiness, /getShootPlanAssignments/)
  assert.match(readiness, /planning-ready-group/)
  assert.match(readiness, /客户认领 · \{claimant\}/)
  assert.match(readiness, /readinessTag\(/)
  assert.match(readiness, /现场缺失 · 来自现场模式第/)
  assert.match(readiness, /preparationMissingShotPositions\(/)
})

test('ConfirmDialog unifies destructive confirmations with dialog a11y and busy guards', () => {
  const dialog = source('../src/components/ConfirmDialog.tsx')
  const css = source('../src/planning/planning.css')

  assert.match(dialog, /role="alertdialog"\s+aria-modal="true"\s+aria-labelledby="planningConfirmTitle"\s+aria-describedby="planningConfirmBody"/)
  assert.match(dialog, /useFocusTrap<HTMLDivElement>\(true, onCancel, !busy\)/)
  assert.match(dialog, /event\.target === event\.currentTarget && !busy/)
  const focusTrap = source('../src/components/useFocusTrap.ts')
  assert.match(focusTrap, /event\.key === 'Escape' && closeEnabledRef\.current/)
  assert.match(focusTrap, /returnTarget\.focus/)
  assert.match(focusTrap, /focusableElements\(container\)/)
  // prompt 变体：requiredInput 存在时空输入禁用确认，提交 trimmed 值。
  assert.match(dialog, /requiredInput != null && value\.trim\(\) === ''/)
  assert.match(dialog, /onConfirm\(value\.trim\(\)\)/)
  assert.match(dialog, /danger \? 'btn btn-danger' : 'btn btn-primary'/)
  assert.match(css, /\.planning-confirm-input textarea \{/)
})

test('planning pages contain no native confirm/prompt dialogs', () => {
  const files = [
    '../src/planning/ShootPlanWorkspacePage.tsx',
    '../src/planning/ShootPlanIngestionPage.tsx',
    '../src/planning/ShootPlanRunPage.tsx',
    '../src/planning/panels/BriefPanel.tsx',
    '../src/planning/panels/ShotsPanel.tsx',
    '../src/planning/panels/ReadinessPanel.tsx',
    '../src/planning/panels/ExecutionHistoryPanel.tsx',
    '../src/planning/panels/BusinessPanel.tsx',
    '../src/planning/panels/CrmLinkPanel.tsx',
  ]
  for (const file of files) {
    assert.doesNotMatch(source(file), /window\.confirm|globalThis\.confirm|window\.prompt|globalThis\.prompt|window\.alert/, file)
  }
})

test('destructive actions route through ConfirmDialog with danger tone and specific copy', () => {
  const workspace = source('../src/planning/ShootPlanWorkspacePage.tsx')
  const shots = source('../src/planning/panels/ShotsPanel.tsx')
  const readiness = source('../src/planning/panels/ReadinessPanel.tsx')
  const history = source('../src/planning/panels/ExecutionHistoryPanel.tsx')
  const brief = source('../src/planning/panels/BriefPanel.tsx')
  const business = source('../src/planning/panels/BusinessPanel.tsx')

  assert.match(workspace, /title="确认归档这份策划？"/)
  assert.match(workspace, /title="重新打开这份策划？"/)
  assert.match(shots, /title=\{`移除镜头「\$\{removing\.title\}」？`\}/)
  assert.match(shots, /acknowledge_execution_history: hasHistory/)
  assert.match(readiness, /title=\{`移除准备项「\$\{removing\.title\}」？`\}/)
  // 作废用 requiredInput 变体替代原生 prompt+confirm 两连。
  assert.match(history, /requiredInput=\{\{ label: '作废原因'/)
  assert.match(history, /void voidEvent\(fact, reason\)/)
  assert.match(brief, /title="清除拍摄时间？"/)
  assert.match(business, /isOrder && draft\.proposed_total != null \? `把建议总价 \$\{formatMoney\(draft\.proposed_total\)\} 写入订单？` : '确认应用这份草稿？'/)
  assert.match(business, /effects\.map\(effectLabel\)/)
})

test('workspace success feedback toasts through the shell while recovery warnings stay inline', () => {
  const workspace = source('../src/planning/ShootPlanWorkspacePage.tsx')

  assert.match(workspace, /const \{ notify \} = useShell\(\)/)
  assert.ok(workspace.includes("notify('已保存最新版本。')"))
  assert.ok(workspace.includes("notify('策划状态已更新。')"))
  // 需要用户后续动作的告警保留页面级 planning-feedback。
  assert.match(workspace, /setFeedback\('更改已提交，但最新页面加载失败；请刷新页面查看结果。'\)/)
  assert.match(workspace, /setFeedback\('策划已在其他页面更新。页面已刷新，请核对保留的输入后重新保存。'\)/)
})

test('run header shows session meta with plan revision tag and a low-frequency clock', () => {
  const runPage = source('../src/planning/ShootPlanRunPage.tsx')
  const css = source('../src/planning/run.css')

  assert.match(runPage, /<div className="run-meta" aria-label="本场会话信息">/)
  assert.match(runPage, /第 \{input\.plan_revision\} 版/)
  assert.match(runPage, /function RunClock\(\)/)
  // 时钟只做现场参考：15s 低频刷新，避免整页每秒重渲。
  assert.match(runPage, /window\.setInterval\(\(\) => setNow\(new Date\(\)\), 15000\)/)
  assert.match(css, /\.run-meta-tag\.is-live::before/)
})

test('run reference sheet explains empty state and compare-only usage; skip reasons match prototype wording', () => {
  const runPage = source('../src/planning/ShootPlanRunPage.tsx')

  assert.match(runPage, /本镜未绑定参考素材/)
  assert.match(runPage, /原作素材仅用于现场比对，不做生成/)
  // Run 选择器与工作台标签共用原型措辞（准备物料缺失/时间不够/主体不可用/创作方向变更）。
  assert.match(runPage, /label: '准备物料缺失'/)
  assert.match(runPage, /label: '时间不够'/)
  assert.match(runPage, /label: '主体不可用'/)
  assert.match(runPage, /label: '创作方向变更'/)
  assert.equal(skipReasonLabel('time_insufficient'), '时间不够')
  assert.equal(skipReasonLabel('creative_change'), '创作方向变更')
})

test('workspace window editor states the execution-window source three ways', () => {
  const brief = source('../src/planning/panels/BriefPanel.tsx')

  assert.match(brief, /function windowSourceNote\(plan: ShootPlanDetail, hasWindow: boolean\)/)
  assert.match(brief, /尚未设置：可在此手动填写，或关联订单档期后从 CRM 卡采纳档期投影。/)
  assert.match(brief, /来源：跟随订单档期投影（在 CRM 关联卡管理抑制或重新采纳）。/)
  assert.match(brief, /来源：手动维护；存在未采纳的档期投影，可在 CRM 关联卡查看。/)
  assert.match(brief, /来源：手动维护；未关联可用的订单档期投影。/)
})

test('business draft lines carry a basis column, suggested total, and amount comparison in apply confirm', () => {
  const business = source('../src/planning/panels/BusinessPanel.tsx')

  assert.match(business, /<span className="planning-line-basis">\{line\.source_fact\.field\} · \{line\.source_fact\.rule_key\}<\/span>/)
  assert.match(business, /套系基准价/)
  assert.match(business, /<span>建议总价<\/span>/)
  assert.match(business, /把建议总价 \$\{formatMoney\(draft\.proposed_total\)\} 写入订单？/)
  assert.match(business, /订单价格将从 \$\{formatMoney\(draft\.base_price\)\} 更新为 \$\{formatMoney\(draft\.proposed_total\)\}/)
  assert.match(business, /客户不会收到任何自动通知/)
})

test('assignment claims show readiness state tags and revoke confirm explains reminder cancellation', () => {
  const panel = source('../src/planning/share/ShareCollaborationPanel.tsx')
  const workspace = source('../src/planning/ShootPlanWorkspacePage.tsx')

  assert.match(panel, /'site_missing' \| 'to_check' \| 'checked'/)
  assert.match(panel, /现场缺失<\/span>/)
  assert.match(panel, /待核对<\/span>/)
  assert.match(panel, /对应的核对提醒会一并撤销/)
  assert.match(panel, /「待核对」在拍摄前核对完成，「现场缺失」来自现场模式的跳过记录/)
  // 状态与准备项面板同源派生（现场缺失优先，其次待核对）。
  assert.match(workspace, /if \(preparationMissingShotPositions\(plan\.shots, item\.id\)\.length > 0\) map\[item\.id\] = 'site_missing'/)
  assert.match(workspace, /readinessStates=\{readinessStates\}/)
})

test('public scale hints and the four-step progression panel match the prototype copy', () => {
  const brief = source('../src/planning/panels/BriefPanel.tsx')

  assert.match(brief, /签发 proposal\/full 后可在客户页显示；未知时留空并隐藏。/)
  assert.match(brief, /这是计划规模，不从付费场地数量推断。/)
  assert.match(brief, /<details className="planning-flow-help">/)
  assert.match(brief, /进度怎么流转/)
  // 回归：整行提示必须位于两列网格之外，混进网格会打乱自动排列（BriefEditor 被挤进窄侧栏）。
  assert.match(brief, /<\/details>\s*<div className="planning-panel-grid">/)
  for (const step of ['草稿 → 已就绪', '已就绪 → 拍摄中', '拍摄中 → 已完成', '已完成后想再改，需要先「重新打开」']) {
    assert.ok(brief.includes(step), step)
  }
})

test('plan list creates a blank plan in one click instead of a required-fields modal', () => {
  const plans = source('../src/planning/ShootPlansPage.tsx')

  assert.match(plans, /async function createBlank\(next: 'ingestion' \| 'workspace'\)/)
  assert.match(plans, /createShootPlan\(\{ title: '未命名策划', subject: '待补充' \}, newPlanningMutationKey\('create'\)\)/)
  assert.ok(plans.includes("notify(next === 'ingestion' ? '已新建策划，接着把聊天记录整理成方案。' : '已新建空白策划（客户与订单均可留空）')"))
  assert.doesNotMatch(plans, /CreatePlanDialog/)
  assert.doesNotMatch(plans, /标题和拍摄主体都需要填写/)
})

// 建案漏斗的分母构成：主入口一步直达整理页，零成本空白建案仍然保留。
test('plan list offers ingestion as the primary create path with blank creation kept as secondary', () => {
  const plans = source('../src/planning/ShootPlansPage.tsx')

  assert.match(plans, /btn btn-primary[\s\S]{0,200}createBlank\('ingestion'\)[\s\S]{0,80}＋ 从聊天整理/)
  assert.match(plans, /createBlank\('workspace'\)[\s\S]{0,60}空白建案/)
  assert.match(plans, /navigate\(next === 'ingestion' \? `\$\{path\}\/ingestions\/new` : path\)/)
  // 两个入口共用一个 creating 标记，创建期间互斥，避免连点建出两份空案。
  assert.match(plans, /useState<'ingestion' \| 'workspace' \| null>\(null\)/)
  assert.equal((plans.match(/disabled=\{creating !== null\}/g) ?? []).length, 2)
  assert.match(plans, /还没有拍摄策划。把和客户聊过的记录粘进「从聊天整理」/)
})

// U7a：列表不再固定停在第一页，超过 pageSize 的策划要能续加载出来。
test('plan list pages through the ledger instead of capping at one fixed page', () => {
  const plans = source('../src/planning/ShootPlansPage.tsx')

  assert.match(plans, /const planPageSize = 50/)
  assert.match(plans, /type PlanListPage = \{ items: ShootPlanListItem\[\]; total: number; page: number \}/)
  assert.match(plans, /显示 \{list\.items\.length\} \/ \{list\.total\} 份策划/)
  assert.match(plans, /list\.items\.length < list\.total/)
  assert.match(plans, /加载更多（\$\{list\.items\.length\}\/\$\{list\.total\}）/)
  // 下一页是追加，不是替换。
  assert.match(plans, /items: \[\.\.\.data\.items, \.\.\.result\.items\], total: result\.total, page: nextPage/)
  // 换查询条件作废在途请求，旧条件的下一页不得追进新列表。
  assert.match(plans, /loadMoreRequestSeq\.current \+= 1\s*\n\s*setLoadingMore\(false\)/)
  assert.match(plans, /loadMoreRequestSeq\.current === requestSeq && queryRef\.current === requestQuery/)
  // 下一页失败走 stale 分支，已加载的策划留在页面上。
  assert.match(plans, /failPageRead\(current, planningErrorMessage\(error, '加载更多失败，仍显示已加载的策划'\)/)
})

// U7b：关键词与排序都在服务端完成，前端不得取一页再自己筛或自己排。
test('plan list搜索与排序走生成契约，过滤与排序都在服务端', () => {
  const plans = source('../src/planning/ShootPlansPage.tsx')
  const api = source('../src/planning/api.ts')
  const schema = source('../src/api/schema.d.ts')

  assert.match(schema, /ShootPlanListSort: "updated_at_desc" \| "updated_at_asc" \| "created_at_desc"/)
  assert.match(api, /export type ShootPlanListSort = components\['schemas'\]\['ShootPlanListSort'\]/)
  assert.match(api, /if \(params\.q\) search\.set\('q', params\.q\)/)
  assert.match(api, /if \(params\.sort\) search\.set\('sort', params\.sort\)/)

  assert.match(plans, /type PlanListQuery = \{ q: string; sort: ShootPlanListSort; filter: 'all' \| ShootPlanStatus \}/)
  assert.match(plans, /q: keyword\.trim\(\)/)
  assert.match(plans, /没有匹配「\$\{query\.q\}」的拍摄策划/)
  // 首页与下一页共用同一个请求构造，参数不会两处各写一遍而漂移。
  assert.match(plans, /listShootPlans\(listRequest\(query, 1\)\)/)
  assert.match(plans, /listShootPlans\(listRequest\(requestQuery, nextPage\)\)/)
  for (const label of ['最近更新', '最久未更新', '最新创建']) {
    assert.ok(plans.includes(label), label)
  }
  // 客户端只送排序键，绝不送列名。
  assert.doesNotMatch(plans, /updated_at DESC|order_by|ORDER BY/)
  // 搜索框与排序框都有可见标签（e2e 断言页面零个无标签控件）。
  assert.match(plans, /<span>搜索<\/span>\s*\n\s*<input className="input" type="search"/)
  assert.match(plans, /<span>排序<\/span>/)
})

// 用户可见文案里不允许再出现英文功能名或同一功能的旧称。
test('planning UI copy uses 现场模式 and 从聊天整理 instead of Run Mode and 摄取工作台', () => {
  for (const path of [
    '../src/planning/ShootPlansPage.tsx',
    '../src/planning/ShootPlanWorkspacePage.tsx',
    '../src/planning/ShootPlanRunPage.tsx',
    '../src/planning/ShootPlanIngestionPage.tsx',
    '../src/planning/panels/BriefPanel.tsx',
    '../src/planning/panels/ShotsPanel.tsx',
    '../src/planning/panels/ReadinessPanel.tsx',
    '../src/planning/panels/ExecutionHistoryPanel.tsx',
    '../src/planning/share/ShareCollaborationPanel.tsx',
  ]) {
    assert.doesNotMatch(source(path), /Run Mode|摄取工作台/, path)
  }
  assert.match(source('../src/planning/ShootPlanIngestionPage.tsx'), /拍摄策划<\/Link> \/ 从聊天整理/)
})
