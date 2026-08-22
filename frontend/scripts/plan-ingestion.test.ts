import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

import { contentOverrideFields, restoredCandidateKind, shotDecisionShot } from '../src/planning/ingestionCandidates.ts'
import { droppedExcerpt, droppedLegendLine, dropReasonLabel, dropReasonNote } from '../src/planning/ingestionDropped.ts'
import { staleDiffLines } from '../src/planning/ingestionConflict.ts'

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

test('reparse requires an explicit ConfirmDialog that documents edited-candidate retention', () => {
  const page = source('../src/planning/ShootPlanIngestionPage.tsx')

  // 已有会话时先弹统一确认框，无会话（首次解析）直接执行。
  assert.match(page, /if \(session\) setConfirmingReparse\(true\); else void parseSource\(\)/)
  assert.match(page, /confirmingReparse && \(\s*<ConfirmDialog/)
  assert.match(page, /title="重新解析并重建候选？"/)
  assert.match(page, /重新解析会按新原文重建候选列表/)
  assert.match(page, /会保留你的修改/)
  assert.match(page, /需要重新确认/)
  assert.doesNotMatch(page, /window\.confirm|globalThis\.confirm|globalThis\.prompt/)
})

test('ingestion transient success feedback goes through the shell toast, not inline status', () => {
  const page = source('../src/planning/ShootPlanIngestionPage.tsx')

  assert.match(page, /const \{ notify \} = useShell\(\)/)
  for (const message of ['候选已更新，请逐条确认。', '参考图已暂存，提交时会与候选一起处理。', '已合并到上一条候选，提交前仍可撤销。', '编辑已暂存，请确认保存。', '本次摄取已结束，原文与素材仍按保留规则可恢复查看。']) {
    assert.ok(page.includes(`notify('${message}')`), `toast missing: ${message}`)
  }
  assert.ok(page.includes('notify(`已保存 ${result.plan_batch?.created_ids?.length ?? 0} 个核心候选'))
  // 持久告警仍走内联块；瞬时成功不再有 planning-feedback role=status 渲染。
  assert.match(page, /planning-feedback ingestion-stale/)
  assert.match(page, /planning-feedback ingestion-error/)
  assert.doesNotMatch(page, /\{feedback && <div className="planning-feedback"/)
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

test('ingestion stale conflict exposes a diff panel between local edits and server session', () => {
  const page = source('../src/planning/ShootPlanIngestionPage.tsx')
  const module = source('../src/planning/ingestionConflict.ts')

  // 冲突时拉取最新会话并渲染可展开的差异面板。
  assert.match(page, /function markStale\(\)/)
  assert.match(page, /getPlanIngestionSession\(id, session\.id\)/)
  assert.match(page, /<details className="ingestion-stale-diff"><summary>查看本地与服务端的差异<\/summary>/)
  assert.match(page, /staleDiffLines\(localSide, serverSide\)/)
  // 差异只在内容确有变化时展示（拉回的会话与本地相同则不弹）。
  assert.match(page, /fresh\.revision !== session\.revision \|\| fresh\.source_checksum !== session\.source_checksum/)

  // 纯函数：版本漂移、候选/链接计数对照、校验和变化各自成行。
  const lines = staleDiffLines(
    { revision: 3, keptCandidates: 6, totalCandidates: 8, keptReferences: 2, totalReferences: 3, sourceChecksum: 'sha-a' },
    { revision: 5, keptCandidates: 9, totalCandidates: 9, keptReferences: 3, totalReferences: 3, sourceChecksum: 'sha-b' },
  )
  assert.ok(lines.some((line) => line.includes('本地基于第 3 版') && line.includes('第 5 版')))
  assert.ok(lines.some((line) => line.includes('本地保留 6 / 共 8 条') && line.includes('服务端最新解析共 9 条')))
  assert.ok(lines.some((line) => line.includes('本地保留 2 / 共 3 条') && line.includes('服务端最新 3 条')))
  assert.ok(lines.some((line) => line.includes('原文校验和：已变化')))
  // 版本相同且校验和一致时省略版本行、判定未变化。
  const sameLines = staleDiffLines(
    { revision: 4, keptCandidates: 1, totalCandidates: 2, keptReferences: 0, totalReferences: 1, sourceChecksum: 'sha-x' },
    { revision: 4, keptCandidates: 2, totalCandidates: 2, keptReferences: 1, totalReferences: 1, sourceChecksum: 'sha-x' },
  )
  assert.ok(!sameLines.some((line) => line.includes('会话版本')))
  assert.ok(sameLines.some((line) => line.includes('原文校验和：未变化')))
  assert.match(module, /export function staleDiffLines/)
})

test('ingestion review step offers select-all and a kept-count chip in the topbar', () => {
  const page = source('../src/planning/ShootPlanIngestionPage.tsx')

  assert.match(page, /function selectAllKept\(\)/)
  assert.match(page, /onSelectAll=\{selectAllKept\}/)
  assert.match(page, /onSelectAll: \(\) => void/)
  assert.match(page, /<button className="btn btn-ghost btn-sm" type="button" onClick=\{onSelectAll\}>全选<\/button>/)
  assert.ok(page.includes("notify('已全选')"))
  // 顶栏「已选 N 条」chip 只在确认候选步骤出现。
  assert.match(page, /\{step === 'review' && <span className="tag tag-accent">已选 \{keptCount\} 条<\/span>\}/)
})

test('ingestion commit summary breaks down candidates and pins the base plan revision', () => {
  const page = source('../src/planning/ShootPlanIngestionPage.tsx')

  assert.match(page, /shotCount=\{activeCandidates\.filter\(\(candidate\) => candidate\.kind === 'shot'\)\.length\}/)
  assert.match(page, /readinessCount=\{activeCandidates\.filter\(\(candidate\) => candidate\.kind === 'readiness'\)\.length\}/)
  assert.match(page, /planRevision=\{plan\.revision\}/)
  assert.match(page, /镜头 \{shotCount\} · 准备项 \{readinessCount\}/)
  assert.match(page, /基于策划第 \{planRevision\} 版提交；保存成功后策划会生成新版本/)
})
