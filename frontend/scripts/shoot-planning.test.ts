import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { shotHasExecutionHistory } from '../src/planning/history.ts'

function source(path: string): string {
  return readFileSync(new URL(path, import.meta.url), 'utf8')
}

test('shot history acknowledgement is based on retained execution facts, not the current outcome projection', () => {
  const plan = { execution_history: [{ shot_id: 'shot-1' }, { shot_id: 'shot-2' }] }

  assert.equal(shotHasExecutionHistory(plan, 'shot-1'), true)
  assert.equal(shotHasExecutionHistory(plan, 'shot-3'), false)
  assert.equal(shotHasExecutionHistory({}, 'shot-1'), false)
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

  assert.match(app, /<RequireAuth>[\s\S]*<AppShell \/>[\s\S]*<Route path="\/shoot-plans"/)
  assert.match(app, /<Route path="\/shoot-plans\/:id"/)
  assert.match(app, /<Route path="\/settings"[^\n]*\/>\s*<\/Route>\s*<Route\s*path="\/shoot-plans\/:id\/run"[\s\S]*<RequireAuth>[\s\S]*<ShootPlanRunPage \/>/)
  assert.match(shell, /label: '策划', to: '\/shoot-plans'/)
})

test('workspace exposes only the four approved core sections and no later-feature entry points', () => {
  const workspace = source('../src/planning/ShootPlanWorkspacePage.tsx')

  assert.equal((workspace.match(/<TabButton\s/g) ?? []).length, 4)
  for (const coreTab of ['创作 brief', '镜头表', '准备项', '执行历史']) {
    assert.match(workspace, new RegExp(`>${coreTab}(?:\\s|<|\\{)`))
  }
  for (const deferredEntry of ['参考素材', '分享反馈', '经营草稿', '素材摄取', 'AI 脚本', 'AI 分镜']) {
    assert.doesNotMatch(workspace, new RegExp(`>${deferredEntry}<`))
  }
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
  assert.match(css, /@media \(max-width: 768px\)[\s\S]*grid-template-columns:\s*1fr/)
  assert.doesNotMatch(planningSources, /<table\b|overflow-x:\s*scroll/)
})
