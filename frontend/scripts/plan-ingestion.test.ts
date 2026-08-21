import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { contentOverrideFields, restoredCandidateKind, shotDecisionShot } from '../src/planning/ingestionCandidates.ts'

function source(path: string): string {
  return readFileSync(new URL(path, import.meta.url), 'utf8')
}

test('plan ingestion route and API use generated contract types', () => {
  const app = source('../src/App.tsx')
  const api = source('../src/planning/api.ts')
  const page = source('../src/planning/ShootPlanIngestionPage.tsx')

  assert.match(app, /shoot-plans\/:id\/ingestions\/:sessionId/)
  assert.match(api, /PlanIngestionSession = components\['schemas'\]\['PlanIngestionSession'\]/)
  for (const endpoint of ['createPlanIngestionSession', 'previewPlanIngestionSession', 'commitPlanIngestionSession']) {
    assert.match(api, new RegExp(`export function ${endpoint}`))
  }
  assert.match(page, /粘贴 \/ 上传/)
  assert.match(page, /确认候选/)
  assert.match(page, /确认保存/)
  assert.match(page, /source_line_refs/)
  assert.match(page, /candidate_snapshot\.readiness_link_candidates/)
  assert.doesNotMatch(page, /readiness-link-/)
  assert.match(page, /Record<string, string\[\]>/)
  assert.match(page, /multiple value=\{links\[/)
  assert.match(page, /candidate\.target_client_or_id_ref/)
  assert.match(page, /ReviewStep candidates=\{candidates\}/)
  assert.match(page, /const canContinue = keptCandidates\.length > 0/)
  assert.match(page, /参考链接/)
})

test('plan ingestion has no internal timer, AI, provider, or engineering note in formal UI', () => {
  const page = source('../src/planning/ShootPlanIngestionPage.tsx')
  assert.doesNotMatch(page, /计时|达标|AI|OCR|工程评审|provider|active_seconds/)
  assert.match(page, /newPlanningMutationKey\('ingestion-(?:create|preview|commit|abandon)'\)/)
})

test('plan ingestion responsive and keyboard contracts are present', () => {
  const css = source('../src/planning/planning.css')
  assert.match(css, /\.ingestion-page button:focus-visible/)
  assert.match(css, /\.ingestion-page \.planning-panel \{/)
  assert.doesNotMatch(css, /\.ingestion-page button:focus-visible[^\n]*var\(--accent-tint\)/)
  assert.match(css, /@media \(max-width: 768px\)[\s\S]*\.ingestion-grid, \.ingestion-review \{ grid-template-columns: 1fr; \}/)
  assert.match(css, /@media \(max-width: 430px\)/)
  assert.match(css, /@media \(pointer: coarse\)[\s\S]*\.ingestion-page button/)
})

test('restoring a dropped candidate inherits the duplicate winner kind instead of hardcoding shot', () => {
  const candidates = [
    { candidate_id: 'c-ready', kind: 'readiness' },
    { candidate_id: 'c-shot', kind: 'shot' },
  ]

  assert.equal(restoredCandidateKind({ candidate_id: 'd1', reason: 'exact_duplicate', source_line_refs: [4], winner_candidate_id: 'c-ready' }, candidates), 'readiness')
  assert.equal(restoredCandidateKind({ candidate_id: 'd2', reason: 'exact_duplicate', source_line_refs: [5], winner_candidate_id: 'c-shot' }, candidates), 'shot')
  assert.equal(restoredCandidateKind({ candidate_id: 'd3', reason: 'blank', source_line_refs: [15] }, candidates), 'shot')

  const page = source('../src/planning/ShootPlanIngestionPage.tsx')
  assert.match(page, /restoredCandidateKind\(item, candidates\)/)
  assert.doesNotMatch(page, /\bkind: 'shot',/)
})

test('ingestion candidates expose taxonomy and readiness field edits that persist through preview and commit', () => {
  const page = source('../src/planning/ShootPlanIngestionPage.tsx')
  const module = source('../src/planning/ingestionCandidates.ts')

  assert.equal(shotDecisionShot({ title: '石板路', normalized_content: '晨雾', framing_tag: 'medium', palette_tag: null }).framing_tag, 'medium')
  assert.equal(shotDecisionShot({ title: '侧身', normalized_content: '回头' }).palette_tag, null)
  assert.equal(contentOverrideFields({ category: 'styling', requirement: 'required' }).category, 'styling')
  assert.equal(contentOverrideFields({ framing_tag: 'soft' }).framing_tag, 'soft')
  assert.deepEqual(contentOverrideFields({}), {})

  for (const field of ['取景', '灯光方向', '灯光质感', '色调', '类型']) {
    assert.match(module, new RegExp(field))
  }
  for (const field of ['层级', '预期负责人', '是否必需', '核对提前量']) {
    assert.match(page, new RegExp(field))
  }
  assert.match(page, /shotDecisionShot\(candidate\)/)
  assert.match(page, /\.\.\.contentOverrideFields\(candidate\)/)
  assert.match(page, /<option value="">未填<\/option>/)
  assert.match(page, /原文没说的维度保持未填，系统不会替你猜/)
})

test('reference link candidates support label and target editing instead of a disabled select', () => {
  const page = source('../src/planning/ShootPlanIngestionPage.tsx')

  assert.match(page, /onReferenceEdit=/)
  assert.doesNotMatch(page, /select value=\{reference\.target_kind\} disabled/)
  assert.match(page, /aria-label="链接标注"/)
  assert.match(page, /aria-label="链接归属"/)
})

test('reparse requires an explicit confirmation that documents edited-candidate retention', () => {
  const page = source('../src/planning/ShootPlanIngestionPage.tsx')

  assert.match(page, /if \(session && !window\.confirm\(/)
  assert.match(page, /重新解析会按新原文重建候选列表/)
  assert.match(page, /会保留你的修改/)
  assert.match(page, /需要重新确认/)
})
