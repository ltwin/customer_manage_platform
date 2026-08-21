import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { contentOverrideFields, restoredCandidateKind, shotDecisionShot } from '../src/planning/ingestionCandidates.ts'
import { droppedExcerpt, droppedLegendLine, dropReasonLabel, dropReasonNote } from '../src/planning/ingestionDropped.ts'

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

test('dropped candidates classify parser reasons instead of showing raw reason strings', () => {
  assert.equal(dropReasonLabel('blank'), '空白段')
  assert.equal(dropReasonLabel('duplicate'), '重复内容')
  assert.equal(dropReasonLabel('unsupported'), '无法识别')
  assert.equal(dropReasonLabel('over_limit'), '超出上限')
  assert.equal(dropReasonLabel('manual'), '手动丢弃')
  // 服务端未来的未知 reason 按原值透出，不猜测翻译。
  assert.equal(dropReasonLabel('future_kind'), 'future_kind')
  assert.equal(dropReasonNote('future_kind'), '解析层丢弃项，可恢复为候选')
})

test('dropped excerpts fold whitespace and truncate long originals', () => {
  assert.equal(droppedExcerpt(null), '')
  assert.equal(droppedExcerpt('   \n\t  '), '')
  assert.equal(droppedExcerpt('晨雾 石板路'), '晨雾 石板路')
  assert.equal(droppedExcerpt('第一行\n第二行'), '第一行 第二行')
  const long = '长'.repeat(80)
  assert.equal(droppedExcerpt(long), `${'长'.repeat(60)}…`)
})

test('dropped legend covers all parser reasons plus manual discard', () => {
  for (const label of ['重复内容', '无法识别', '超出上限', '空白段', '手动丢弃']) {
    assert.match(droppedLegendLine, new RegExp(label))
  }
  assert.match(droppedLegendLine, /＝/)
})

test('ingestion dropped section is collapsible and renders labels, excerpts, and winner hints', () => {
  const page = source('../src/planning/ShootPlanIngestionPage.tsx')
  const css = source('../src/planning/planning.css')

  assert.match(page, /<details className="ingestion-dropped-details"><summary>展开查看 \{dropped\.length\} 项丢弃内容<\/summary>/)
  assert.match(page, /分类对照：\{droppedLegendLine\}/)
  assert.match(page, /dropReasonLabel\(item\.reason\)/)
  assert.match(page, /droppedExcerpt\(item\.original\)/)
  assert.match(page, /重复项已并入保留候选/)
  // 原始 reason 字符串不再直接进入展示层。
  assert.doesNotMatch(page, /· \{item\.reason\}/)
  assert.match(css, /\.ingestion-dropped-details > summary \{ cursor: pointer/)
  assert.match(css, /\.ingestion-dropped-main \{ flex: 1/)
})

test('candidates discarded by the photographer show a manual-discard tag', () => {
  const page = source('../src/planning/ShootPlanIngestionPage.tsx')

  assert.match(page, /\{candidate\.action === 'discard' && <span className="tag tag-warn">手动丢弃<\/span>\}/)
})
