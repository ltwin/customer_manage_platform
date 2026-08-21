import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ChangeEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ApiError } from '../api/client'
import type { components } from '../api/schema'
import {
  commitPlanIngestionSession,
  createPlanIngestionSession,
  getPlanIngestionSession,
  getShootPlan,
  listPlanAssets,
  newPlanningMutationKey,
  previewPlanIngestionSession,
  transitionPlanIngestionSession,
  uploadPlanAsset,
  type IngestionContentCandidate,
  type PlanIngestionSession,
  type PlanAsset,
  type ShootPlanDetail,
} from './api'
import { planningErrorMessage } from './presentation'
import { contentOverrideFields, restoredCandidateKind, shotDecisionShot, shotTagFields, shotTagPatch } from './ingestionCandidates'
import { droppedExcerpt, droppedLegendLine, dropReasonLabel } from './ingestionDropped'
import { mediaSourceOptions, rightsDeclarationFor, type MediaSourceClass } from './mediaRights'
import './planning.css'

type Step = 'source' | 'review' | 'commit'
type Candidate = IngestionContentCandidate
type CommitInput = components['schemas']['CommitPlanIngestionSessionInput']
type ShotDecision = NonNullable<CommitInput['shot_decisions']>[number]
type ReadinessDecision = NonNullable<CommitInput['readiness_decisions']>[number]
type LinkDecision = NonNullable<CommitInput['link_decisions']>[number]
type ReferenceDecision = NonNullable<CommitInput['reference_link_decisions']>[number]
type AssetDecision = NonNullable<CommitInput['asset_bindings']>[number]

export default function ShootPlanIngestionPage() {
  const { id = '', sessionId = '' } = useParams()
  const navigate = useNavigate()
  const isNew = sessionId === 'new'
  const [plan, setPlan] = useState<ShootPlanDetail | null>(null)
  const [session, setSession] = useState<PlanIngestionSession | null>(null)
  const [step, setStep] = useState<Step>('source')
  const [source, setSource] = useState('')
  const [candidates, setCandidates] = useState<Candidate[]>([])
  const [references, setReferences] = useState<components['schemas']['IngestionReferenceLinkCandidate'][]>([])
  const [dropped, setDropped] = useState<components['schemas']['IngestionDroppedCandidate'][]>([])
	const [readinessLinks, setReadinessLinks] = useState<Record<string, string[]>>({})
	const [readinessLinkAcks, setReadinessLinkAcks] = useState<Record<string, number>>({})
	const [referenceAcks, setReferenceAcks] = useState<Record<string, number>>({})
  const [assets, setAssets] = useState<PlanAsset[]>([])
  const [uploadSource, setUploadSource] = useState<MediaSourceClass>('customer_supplied')
  const [selectedAssets, setSelectedAssets] = useState<Set<string>>(new Set())
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [stale, setStale] = useState(false)
  const [feedback, setFeedback] = useState<string | null>(null)

  const applySession = useCallback((next: PlanIngestionSession) => {
    setSession(next)
    setSource(next.source_text ?? '')
		setCandidates(next.candidate_snapshot.content_candidates ?? [])
		setReferences(next.candidate_snapshot.reference_link_candidates ?? [])
		setDropped(next.candidate_snapshot.dropped_candidates ?? [])
		setReadinessLinks(next.candidate_snapshot.readiness_link_candidates.reduce<Record<string, string[]>>((links, candidate) => {
			const current = links[candidate.readiness_client_or_id_ref] ?? []
			if (candidate.action !== 'discard' && !current.includes(candidate.shot_client_or_id_ref)) {
				links[candidate.readiness_client_or_id_ref] = [...current, candidate.shot_client_or_id_ref]
			}
			return links
		}, {}))
		setReadinessLinkAcks(next.candidate_snapshot.readiness_link_candidates.reduce<Record<string, number>>((acks, candidate) => {
			if (candidate.acknowledged_source_change_revision != null) acks[candidate.candidate_id] = candidate.acknowledged_source_change_revision
			return acks
		}, {}))
		setReferenceAcks(next.candidate_snapshot.reference_link_candidates.reduce<Record<string, number>>((acks, candidate) => {
			if (candidate.acknowledged_source_change_revision != null) acks[candidate.candidate_id] = candidate.acknowledged_source_change_revision
			return acks
		}, {}))
		setSelectedAssets(new Set((next.candidate_snapshot.asset_binding_candidates ?? []).map((candidate) => candidate.asset_id)))
    setStep(next.state === 'editing' ? 'review' : 'commit')
  }, [])

  const load = useCallback(async () => {
    setLoading(true); setError(null); setStale(false)
    try {
      const [nextPlan, nextSession] = await Promise.all([
        getShootPlan(id),
        isNew ? Promise.resolve(null) : getPlanIngestionSession(id, sessionId),
      ])
      setPlan(nextPlan)
      const page = await listPlanAssets(id)
      setAssets(page.items)
      if (nextSession) applySession(nextSession)
      else setStep('source')
    } catch (cause) {
      setError(cause instanceof ApiError && cause.status === 404 ? '策划或摄取会话不存在，或不属于当前账号。' : planningErrorMessage(cause, '摄取工作台加载失败'))
    } finally { setLoading(false) }
  }, [applySession, id, isNew, sessionId])

  useEffect(() => { void load() }, [load])

  const activeCandidates = useMemo(() => candidates.filter((candidate) => candidate.action !== 'discard'), [candidates])
  const shotCandidates = useMemo(() => activeCandidates.filter((candidate) => candidate.kind === 'shot'), [activeCandidates])
  const readinessCandidates = useMemo(() => activeCandidates.filter((candidate) => candidate.kind === 'readiness'), [activeCandidates])
  const keptReferenceCount = references.filter((candidate) => candidate.action !== 'discard').length
  const selectedAssetList = assets.filter((asset) => selectedAssets.has(asset.id))
  const keptCount = activeCandidates.length + keptReferenceCount + selectedAssetList.length

  async function parseSource() {
	    if ((!source.trim() && assets.length === 0) || !plan) return
    if (session && !window.confirm('重新解析会按新原文重建候选列表：你编辑过的候选（标题、类型、规范标签与准备项字段）会保留你的修改，未动过的候选会被替换；来源有变化的候选需要重新确认后才能保存。确定重新解析吗？')) return
    setBusy(true); setFeedback(null); setError(null); setStale(false)
    try {
      const next = session
	        ? await previewPlanIngestionSession(id, session.id, { expected_session_revision: session.revision, source_text: source || undefined, staged_asset_intent_count: selectedAssetList.length, staged_asset_intents: selectedAssetList.map(assetBindingIntent) }, newPlanningMutationKey('ingestion-preview'))
	        : await createPlanIngestionSession(id, { expected_plan_revision: plan.revision, source_text: source || undefined, staged_asset_intent_count: selectedAssetList.length, staged_asset_intents: selectedAssetList.map(assetBindingIntent) }, newPlanningMutationKey('ingestion-create'))
      applySession(next)
      if (isNew) navigate(`/shoot-plans/${encodeURIComponent(id)}/ingestions/${encodeURIComponent(next.id)}`, { replace: true })
      setFeedback('候选已更新，请逐条确认。')
    } catch (cause) {
      if (cause instanceof ApiError && cause.code === 'ingestion_revision_conflict') setStale(true)
      setError(planningErrorMessage(cause, '解析失败，原文仍保留在当前页面。'))
    } finally { setBusy(false) }
  }

  async function uploadReference(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    if (!file || !plan) return
    setBusy(true); setError(null)
    try {
      const result = await uploadPlanAsset(id, plan.revision, file, rightsDeclarationFor(uploadSource, false), 'moodboard_display', newPlanningMutationKey('ingestion-asset'))
      setAssets((current) => [result.asset, ...current.filter((item) => item.id !== result.asset.id)])
      setSelectedAssets((current) => new Set(current).add(result.asset.id))
      setFeedback('参考图已暂存，提交时会与候选一起处理。')
    } catch (cause) { setError(planningErrorMessage(cause, '参考图上传失败')) } finally { setBusy(false); event.target.value = '' }
  }

	function editCandidate(idToEdit: string, patch: Partial<Candidate>) {
		const current = candidates.find((candidate) => candidate.candidate_id === idToEdit)
		if (current && ((patch.kind != null && patch.kind !== current.kind) || patch.action === 'discard')) removeCandidateLinks(idToEdit)
	    setCandidates((current) => current.map((candidate) => candidate.candidate_id === idToEdit ? { ...candidate, ...patch, user_modified: true, action: 'keep' } : candidate))
	  }

	  function discardCandidate(idToDiscard: string) {
		removeCandidateLinks(idToDiscard)
	    setCandidates((current) => current.map((candidate) => candidate.candidate_id === idToDiscard ? { ...candidate, action: 'discard', user_modified: true } : candidate))
	  }

  function editReference(idToEdit: string, patch: Partial<components['schemas']['IngestionReferenceLinkCandidate']>) {
    setReferences((current) => current.map((reference) => reference.candidate_id === idToEdit ? { ...reference, ...patch, user_modified: true } : reference))
  }

	function removeCandidateLinks(candidateID: string) {
		setReadinessLinks((current) => Object.fromEntries(Object.entries(current).flatMap(([readinessID, shotIDs]) => {
			const filtered = readinessID === candidateID ? [] : shotIDs.filter((shotID) => shotID !== candidateID)
			return filtered.length > 0 ? [[readinessID, filtered]] : []
		})))
	}

  function restoreDropped(item: components['schemas']['IngestionDroppedCandidate']) {
    if (candidates.some((candidate) => candidate.candidate_id === item.candidate_id)) return
    setCandidates((current) => [...current, {
      candidate_id: item.candidate_id,
      kind: restoredCandidateKind(item, candidates),
      source_line_refs: item.source_line_refs,
      source_fingerprint: '',
      original_excerpt: item.original ?? '',
      normalized_content: item.original ?? '',
      title: item.original ?? '恢复候选',
      action: 'keep', source_status: 'current', user_modified: true,
    }])
    setDropped((current) => current.filter((candidate) => candidate.candidate_id !== item.candidate_id))
  }

  function mergeIntoPrevious(index: number) {
    const current = activeCandidates[index]
    const previous = activeCandidates[index - 1]
    if (!current || !previous) return
    editCandidate(previous.candidate_id, { title: `${previous.title} · ${current.title}`, normalized_content: `${previous.normalized_content}\n${current.normalized_content}` })
    discardCandidate(current.candidate_id)
    setFeedback('已合并到上一条候选，提交前仍可撤销。')
  }

  function buildCommitInput(): CommitInput {
    const shots: ShotDecision[] = candidates.filter((candidate) => candidate.kind === 'shot').map((candidate) => candidate.action === 'discard'
      ? { candidate_id: candidate.candidate_id, action: 'discard', reason: '摄影师已丢弃' }
	      : { candidate_id: candidate.candidate_id, action: 'keep', client_ref: candidate.candidate_id, shot: shotDecisionShot(candidate) })
	    const readiness: ReadinessDecision[] = candidates.filter((candidate) => candidate.kind === 'readiness').map((candidate) => candidate.action === 'discard'
	      ? { candidate_id: candidate.candidate_id, action: 'discard', reason: '摄影师已丢弃' }
	      : { candidate_id: candidate.candidate_id, action: 'keep', client_ref: candidate.candidate_id, item: readinessItem(candidate) })
		const activeIDs = new Set(activeCandidates.map((candidate) => candidate.candidate_id))
		const selectedPairs = new Set(Object.entries(readinessLinks).flatMap(([readinessID, shotIDs]) => activeIDs.has(readinessID) ? shotIDs.filter((shotID) => activeIDs.has(shotID)).map((shotID) => `${readinessID}\u0000${shotID}`) : []))
		const links: LinkDecision[] = (session?.candidate_snapshot.readiness_link_candidates ?? []).map((link) => {
			const pair = `${link.readiness_client_or_id_ref}\u0000${link.shot_client_or_id_ref}`
			return { candidate_id: link.candidate_id, action: selectedPairs.has(pair) ? 'keep' as const : 'discard' as const, shot_client_or_id_ref: link.shot_client_or_id_ref, readiness_client_or_id_ref: link.readiness_client_or_id_ref, reason: selectedPairs.has(pair) ? undefined : '摄影师已取消关联' }
		})
    const referenceLinks: ReferenceDecision[] = references.map((candidate) => ({
      candidate_id: candidate.candidate_id,
      action: candidate.action === 'discard' ? 'discard' as const : 'keep' as const,
      raw_url: candidate.raw_url,
      label: candidate.label,
      target_kind: candidate.target_kind,
      target_client_or_id_ref: candidate.target_client_or_id_ref,
      reason: candidate.action === 'discard' ? '摄影师已丢弃' : undefined,
    }))
		const bindings: AssetDecision[] = selectedAssetList.map(assetBindingIntent)
	    return { expected_session_revision: session?.revision ?? 1, expected_plan_revision: plan?.revision ?? 1, shot_decisions: shots, readiness_decisions: readiness, link_decisions: links, reference_link_decisions: referenceLinks, asset_bindings: bindings }
	  }

	function assetBindingIntent(asset: PlanAsset): AssetDecision {
		return { candidate_id: `asset-${asset.id}`, asset_id: asset.id, generation: asset.current_generation, target_kind: 'plan', target_client_or_id_ref: id, purpose: 'moodboard_display' }
	}

	function readinessItem(candidate: Candidate): NonNullable<ReadinessDecision['item']> {
		const item: NonNullable<ReadinessDecision['item']> = { title: candidate.title }
		if (candidate.category && ['other', 'styling', 'location', 'prop_equipment'].includes(candidate.category)) item.category = candidate.category as NonNullable<ReadinessDecision['item']>['category']
		if (candidate.requirement && ['required', 'optional'].includes(candidate.requirement)) item.requirement = candidate.requirement as NonNullable<ReadinessDecision['item']>['requirement']
		if (candidate.preflight_status && ['checked', 'unchecked'].includes(candidate.preflight_status)) item.preflight_status = candidate.preflight_status as NonNullable<ReadinessDecision['item']>['preflight_status']
		if (candidate.responsibility_hint && ['customer', 'photographer', 'shared', 'unassigned'].includes(candidate.responsibility_hint)) item.responsibility_hint = candidate.responsibility_hint as NonNullable<ReadinessDecision['item']>['responsibility_hint']
		if (candidate.default_preparation_lead_days != null) item.default_preparation_lead_days = candidate.default_preparation_lead_days
		return item
	}

  async function previewEdits() {
    if (!session) return
    setBusy(true); setError(null); setFeedback(null); setStale(false)
    try {
      const next = await previewPlanIngestionSession(id, session.id, {
        expected_session_revision: session.revision,
        source_text: source,
			staged_asset_intent_count: selectedAssetList.length,
			staged_asset_intents: selectedAssetList.map(assetBindingIntent),
			content_overrides: candidates.map((candidate) => ({ candidate_id: candidate.candidate_id, kind: candidate.kind, title: candidate.title, action: candidate.action === 'discard' ? 'discard' : 'keep', acknowledge_source_change_revision: candidate.acknowledged_source_change_revision ?? undefined, ...contentOverrideFields(candidate) })),
			reference_link_overrides: references.map((reference) => ({ candidate_id: reference.candidate_id, label: reference.label, target_kind: reference.target_kind, target_client_or_id_ref: reference.target_client_or_id_ref, action: reference.action === 'discard' ? 'discard' : 'keep', acknowledge_source_change_revision: referenceAcks[reference.candidate_id] ?? reference.acknowledged_source_change_revision ?? undefined })),
		readiness_link_overrides: (session.candidate_snapshot.readiness_link_candidates ?? []).map((link) => ({ candidate_id: link.candidate_id, readiness_client_or_id_ref: link.readiness_client_or_id_ref, shot_client_or_id_ref: link.shot_client_or_id_ref, action: readinessLinks[link.readiness_client_or_id_ref]?.includes(link.shot_client_or_id_ref) ? 'keep' as const : 'discard' as const, acknowledge_source_change_revision: readinessLinkAcks[link.candidate_id] })),
			readiness_link_selections: Object.entries(readinessLinks).flatMap(([readinessID, shotIDs]) => activeCandidates.some((candidate) => candidate.candidate_id === readinessID && candidate.kind === 'readiness') ? shotIDs.filter((shotID) => activeCandidates.some((candidate) => candidate.candidate_id === shotID && candidate.kind === 'shot')).map((shotID) => ({ readiness_client_or_id_ref: readinessID, shot_client_or_id_ref: shotID })) : []),
      }, newPlanningMutationKey('ingestion-preview-edits'))
      applySession(next)
		setStep('commit')
      setFeedback('编辑已暂存，请确认保存。')
    } catch (cause) {
      if (cause instanceof ApiError && cause.code === 'ingestion_revision_conflict') setStale(true)
      setError(planningErrorMessage(cause, '编辑暂存失败，当前修改仍保留。'))
    } finally { setBusy(false) }
  }

  async function commit() {
    if (!session || !plan || keptCount === 0) return
    setBusy(true); setError(null); setFeedback(null)
    try {
      const currentPlan = await getShootPlan(id)
      const result = await commitPlanIngestionSession(id, session.id, { ...buildCommitInput(), expected_plan_revision: currentPlan.revision }, newPlanningMutationKey('ingestion-commit'))
      setPlan(currentPlan)
      setSession(result.session)
      setStep('commit')
      setFeedback(`已保存 ${result.plan_batch?.created_ids?.length ?? 0} 个核心候选、${result.reference_links?.length ?? 0} 条参考链接和 ${result.media_bindings?.length ?? 0} 个素材绑定。`)
    } catch (cause) {
      if (cause instanceof ApiError && cause.code === 'ingestion_revision_conflict') setStale(true)
      setError(planningErrorMessage(cause, '保存失败；当前编辑仍保留，可刷新后重试。'))
    } finally { setBusy(false) }
  }

  async function abandon() {
    if (!session || session.state !== 'editing') return
    setBusy(true); setError(null)
    try {
      const next = await transitionPlanIngestionSession(id, session.id, { expected_session_revision: session.revision, state: 'abandoned' }, newPlanningMutationKey('ingestion-abandon'))
      applySession(next)
      setFeedback('本次摄取已结束，原文与素材仍按保留规则可恢复查看。')
    } catch (cause) { setError(planningErrorMessage(cause, '结束摄取失败')) } finally { setBusy(false) }
  }

  if (loading) return <main className="content planning-content ingestion-page"><div className="planning-empty"><strong>正在加载摄取工作台</strong><span>正在恢复原文和候选。</span></div></main>
  if (error && !plan) return <main className="content planning-content ingestion-page"><div className="state-notice error"><strong>{error}</strong><button className="btn" type="button" onClick={() => void load()}>重试</button></div></main>
  if (!plan) return null

  return <>
    <header className="topbar">
      <div><div className="crumb"><Link to={`/shoot-plans/${encodeURIComponent(id)}`}>拍摄策划</Link> / 摄取工作台</div><h1>{plan.title} · 摄取</h1></div>
      <div className="topbar-actions"><Link className="btn" to={`/shoot-plans/${encodeURIComponent(id)}`}>返回工作台</Link>{session?.state === 'editing' && <button className="btn btn-danger-ghost" disabled={busy} type="button" onClick={() => void abandon()}>结束本次摄取</button>}</div>
    </header>
    <main className="content planning-content ingestion-page">
      <div className="ingestion-steps" aria-label="摄取步骤">
        <StepPill active={step === 'source'} done={step !== 'source'} number="1">粘贴 / 上传</StepPill>
        <StepPill active={step === 'review'} done={step === 'commit'} number="2">确认候选</StepPill>
        <StepPill active={step === 'commit'} done={false} number="3">确认保存</StepPill>
      </div>
      {stale && <div className="planning-feedback ingestion-stale" role="alert">服务端版本已变化。你的编辑仍保留在本页；刷新前请先复制需要保留的内容。</div>}
      {error && <div className="planning-feedback ingestion-error" role="alert">{error}</div>}
      {feedback && <div className="planning-feedback" role="status">{feedback}</div>}
	      {step === 'source' && <SourceStep source={source} setSource={setSource} assets={assets} busy={busy} uploadSource={uploadSource} setUploadSource={setUploadSource} onUpload={uploadReference} onParse={() => void parseSource()} hasSession={Boolean(session)} />}
	      {step === 'review' && <ReviewStep candidates={candidates} references={references} dropped={dropped} readinessCandidates={readinessCandidates} shotCandidates={shotCandidates} readinessLinkCandidates={session?.candidate_snapshot.readiness_link_candidates ?? []} readinessLinkAcks={readinessLinkAcks} links={readinessLinks} setLinks={setReadinessLinks} onAcknowledgeReadinessLink={(candidateID, revision) => setReadinessLinkAcks((current) => ({ ...current, [candidateID]: revision }))} onAcknowledgeReference={(candidateID, revision) => setReferenceAcks((current) => ({ ...current, [candidateID]: revision }))} referenceAcks={referenceAcks} onEdit={editCandidate} onDiscard={discardCandidate} onRestore={restoreDropped} onMerge={mergeIntoPrevious} onReferenceToggle={(candidateID) => setReferences((current) => current.map((candidate) => candidate.candidate_id === candidateID ? { ...candidate, action: candidate.action === 'discard' ? 'keep' : 'discard', user_modified: true } : candidate))} onReferenceEdit={editReference} assets={assets} selectedAssets={selectedAssets} setSelectedAssets={setSelectedAssets} onNext={() => void previewEdits()} onBack={() => setStep('source')} />}
      {step === 'commit' && <CommitStep session={session} candidates={activeCandidates} referenceCount={keptReferenceCount} assetCount={selectedAssetList.length} busy={busy} onCommit={() => void commit()} onBack={() => setStep(session?.state === 'editing' ? 'review' : 'source')} onWorkspace={() => navigate(`/shoot-plans/${encodeURIComponent(id)}`)} />}
    </main>
  </>
}

function StepPill({ active, done, number, children }: { active: boolean; done: boolean; number: string; children: string }) {
  return <span className={`ingestion-step${active ? ' is-active' : ''}${done ? ' is-done' : ''}`}><b>{done ? '✓' : number}</b>{children}</span>
}

function SourceStep({ source, setSource, assets, busy, uploadSource, setUploadSource, onUpload, onParse, hasSession }: { source: string; setSource: (value: string) => void; assets: PlanAsset[]; busy: boolean; uploadSource: MediaSourceClass; setUploadSource: (value: MediaSourceClass) => void; onUpload: (event: ChangeEvent<HTMLInputElement>) => void; onParse: () => void; hasSession: boolean }) {
  return <div className="ingestion-grid"><section className="planning-panel"><div className="panel-heading"><div><h2>原文与参考素材</h2><p className="muted">粘贴已经沟通过的内容。系统只按稳定规则拆分，不抓取链接内容。</p></div><div className="ingestion-upload-controls"><select className="ingestion-upload-source" aria-label="素材来源" value={uploadSource} disabled={busy} onChange={(event) => setUploadSource(event.target.value as MediaSourceClass)}>{mediaSourceOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select><label className="btn btn-secondary">上传参考图<input className="visually-hidden" type="file" accept="image/jpeg,image/png,image/webp" onChange={onUpload} disabled={busy} /></label></div></div><label className="ingestion-source-label" htmlFor="ingestion-source">聊天记录 / 备忘录</label><textarea id="ingestion-source" className="ingestion-source" value={source} onChange={(event) => setSource(event.target.value)} placeholder="例如：角色、动作、场地、打光和参考链接……" spellCheck={false} /><p className="ingestion-hint">保留来源行号，便于之后核对。</p><button className="btn btn-primary ingestion-primary" disabled={busy || (!source.trim() && assets.length === 0)} type="button" onClick={onParse}>{hasSession ? '重新解析并查看候选' : '解析并查看候选'}</button></section><section className="planning-panel"><div className="panel-heading"><div><h2>已暂存参考图 <span className="tag">{assets.length}</span></h2><p className="muted">保存成功前仍是暂存状态，失败可继续重试。</p></div></div>{assets.length === 0 ? <div className="ingestion-empty">还没有暂存参考图</div> : <ul className="ingestion-asset-list">{assets.map((asset) => <li key={asset.id}><span>{asset.display_name || '参考图'}</span><span className={`tag ${asset.state === 'staged' ? 'tag-warn' : 'tag-ok'}`}>{asset.state === 'staged' ? '暂存' : asset.state}</span></li>)}</ul>}</section></div>
}

function ReviewStep({ candidates, references, dropped, readinessCandidates, shotCandidates, readinessLinkCandidates, readinessLinkAcks, referenceAcks, links, setLinks, onAcknowledgeReadinessLink, onAcknowledgeReference, onEdit, onDiscard, onRestore, onMerge, onReferenceToggle, onReferenceEdit, assets, selectedAssets, setSelectedAssets, onNext, onBack }: { candidates: Candidate[]; references: components['schemas']['IngestionReferenceLinkCandidate'][]; dropped: components['schemas']['IngestionDroppedCandidate'][]; readinessCandidates: Candidate[]; shotCandidates: Candidate[]; readinessLinkCandidates: components['schemas']['IngestionReadinessLinkCandidate'][]; readinessLinkAcks: Record<string, number>; referenceAcks: Record<string, number>; links: Record<string, string[]>; setLinks: (value: Record<string, string[]>) => void; onAcknowledgeReadinessLink: (candidateID: string, revision: number) => void; onAcknowledgeReference: (candidateID: string, revision: number) => void; onEdit: (id: string, patch: Partial<Candidate>) => void; onDiscard: (id: string) => void; onRestore: (item: components['schemas']['IngestionDroppedCandidate']) => void; onMerge: (index: number) => void; onReferenceToggle: (id: string) => void; onReferenceEdit: (id: string, patch: Partial<components['schemas']['IngestionReferenceLinkCandidate']>) => void; assets: PlanAsset[]; selectedAssets: Set<string>; setSelectedAssets: (value: Set<string>) => void; onNext: () => void; onBack: () => void }) {
  const keptCandidates = candidates.filter((candidate) => candidate.action !== 'discard')
  const keptReferences = references.filter((reference) => reference.action !== 'discard')
  const canContinue = keptCandidates.length > 0 || keptReferences.length > 0 || selectedAssets.size > 0
  return <div className="ingestion-review"><div className="ingestion-review-main"><div className="panel-heading"><div><h2>确认候选</h2><p className="muted">逐条改类、编辑标题或丢弃；来源行号始终保留。</p></div><span className="tag tag-accent">保留 {keptCandidates.length}</span></div>{candidates.length === 0 && <div className="ingestion-empty">当前没有候选，请返回上一步重新解析。</div>}{candidates.map((candidate, index) => <article className="ingestion-candidate" key={candidate.candidate_id}><div className="ingestion-candidate-top"><label className="ingestion-check"><input type="checkbox" checked={candidate.action !== 'discard'} onChange={() => candidate.action === 'discard' ? onEdit(candidate.candidate_id, { action: 'keep' }) : onDiscard(candidate.candidate_id)} /><span className="sr-only">保留候选</span></label><div><div className="ingestion-source-ref">来源行 {candidate.source_line_refs.join(', ')}</div><input className="ingestion-title-input" aria-label="候选标题" value={candidate.title} onChange={(event) => onEdit(candidate.candidate_id, { title: event.target.value })} /><p className="ingestion-candidate-text">{candidate.normalized_content}</p></div><select aria-label="候选类型" value={candidate.kind} onChange={(event) => onEdit(candidate.candidate_id, { kind: event.target.value as Candidate['kind'] })}><option value="shot">镜头</option><option value="readiness">准备项</option></select></div><div className="ingestion-candidate-actions"><button className="btn btn-ghost btn-sm" type="button" disabled={index === 0} onClick={() => onMerge(index)}>合并到上一条</button>{candidate.source_status !== 'current' && <button className="btn btn-ghost btn-sm" type="button" onClick={() => onEdit(candidate.candidate_id, { acknowledged_source_change_revision: candidate.source_change_revision ?? undefined })}>确认来源变化</button>}{candidate.action === 'discard' && <span className="tag tag-warn">手动丢弃</span>}{candidate.user_modified && <span className="tag">已编辑</span>}</div>{candidate.kind === 'shot' && <div className="ingestion-candidate-fields">{shotTagFields.map((field) => <label key={field.key}><span>{field.label}</span><select aria-label={field.label} value={candidate[field.key] ?? ''} onChange={(event) => onEdit(candidate.candidate_id, shotTagPatch(field.key, event.target.value))}><option value="">未填</option>{field.values.map((value) => <option key={value} value={value}>{value}</option>)}</select></label>)}<p className="ingestion-field-hint">原文没说的维度保持未填，系统不会替你猜。</p></div>}{candidate.kind === 'readiness' && <div className="ingestion-candidate-fields"><label><span>层级</span><select aria-label="层级" value={candidate.category ?? ''} onChange={(event) => onEdit(candidate.candidate_id, { category: event.target.value || null })}><option value="">未填</option><option value="styling">妆造</option><option value="location">场地</option><option value="prop_equipment">道具与器材</option><option value="other">其他</option></select></label><label><span>预期负责人</span><select aria-label="预期负责人" value={candidate.responsibility_hint ?? ''} onChange={(event) => onEdit(candidate.candidate_id, { responsibility_hint: event.target.value || null })}><option value="">未填</option><option value="customer">客户</option><option value="photographer">我</option><option value="unassigned">暂不指定</option></select></label><label><span>是否必需</span><select aria-label="是否必需" value={candidate.requirement ?? ''} onChange={(event) => onEdit(candidate.candidate_id, { requirement: event.target.value || null })}><option value="">未填</option><option value="required">必需</option><option value="optional">可选</option></select></label><label><span>核对提前量（天）</span><input type="number" min={0} max={365} aria-label="核对提前量" value={candidate.default_preparation_lead_days ?? ''} placeholder="未设置" onChange={(event) => onEdit(candidate.candidate_id, { default_preparation_lead_days: event.target.value === '' ? null : Number(event.target.value) })} /></label><p className="ingestion-field-hint">「预期负责人」和提前量只是未来认领的默认输入；客户正式认领后才生成摄影师核对提醒。</p></div>}</article>)}<section className="ingestion-subsection"><div className="panel-heading"><div><h3>参考链接 {keptReferences.length}</h3><p className="muted">只保存链接本身，不读取或抓取内容。</p></div></div>{references.map((reference) => <div className="ingestion-reference" key={reference.candidate_id}><input type="checkbox" checked={reference.action !== 'discard'} onChange={() => onReferenceToggle(reference.candidate_id)} /><span className="ingestion-reference-main"><input className="ingestion-label-input" aria-label="链接标注" value={reference.label ?? ''} placeholder={new URL(reference.raw_url).hostname} onChange={(event) => onReferenceEdit(reference.candidate_id, { label: event.target.value || null })} /><small>{reference.raw_url} · 来源行 {reference.source_line_refs.join(', ')}</small></span><span className="ingestion-reference-target"><select aria-label="链接归属" value={reference.target_kind} onChange={(event) => { if (event.target.value === 'plan') onReferenceEdit(reference.candidate_id, { target_kind: 'plan', target_client_or_id_ref: null }); else if (shotCandidates[0]) onReferenceEdit(reference.candidate_id, { target_kind: 'shot', target_client_or_id_ref: shotCandidates[0].candidate_id }) }}><option value="plan">整案</option><option value="shot">镜头</option></select>{reference.target_kind === 'shot' && <select aria-label="归属镜头" value={reference.target_client_or_id_ref ?? ''} onChange={(event) => onReferenceEdit(reference.candidate_id, { target_client_or_id_ref: event.target.value || null })}>{shotCandidates.map((shot) => <option key={shot.candidate_id} value={shot.candidate_id}>{shot.title}</option>)}</select>}</span>{reference.source_status !== 'current' && <button className="btn btn-ghost btn-sm" type="button" disabled={reference.source_change_revision == null || referenceAcks[reference.candidate_id] === reference.source_change_revision} onClick={() => reference.source_change_revision != null && onAcknowledgeReference(reference.candidate_id, reference.source_change_revision)}>确认链接来源变化</button>}</div>)}</section><section className="ingestion-subsection"><div className="panel-heading"><div><h3>已丢弃</h3><p className="muted">解析保守保留的内容可以恢复为镜头候选。</p></div>{dropped.length > 0 && <span className="tag tag-warn">{dropped.length}</span>}</div>{dropped.length === 0 ? <div className="ingestion-empty">没有丢弃项</div> : <details className="ingestion-dropped-details"><summary>展开查看 {dropped.length} 项丢弃内容</summary><p className="ingestion-field-hint">分类对照：{droppedLegendLine}。</p>{dropped.map((item) => <div className="ingestion-dropped" key={item.candidate_id}><span className="tag">{dropReasonLabel(item.reason)}</span><span className="ingestion-dropped-main">行 {item.source_line_refs.join(', ')}{droppedExcerpt(item.original) ? ` · ${droppedExcerpt(item.original)}` : ''}{item.winner_candidate_id ? ' · 重复项已并入保留候选' : ''}</span><button className="btn btn-ghost btn-sm" type="button" onClick={() => onRestore(item)}>恢复</button></div>)}</details>}</section></div><aside className="ingestion-review-side"><section className="planning-panel"><h3>准备项关联镜头</h3><p className="muted">关联是显式选择，允许 0..N。</p>{readinessCandidates.length === 0 ? <div className="ingestion-empty">暂无准备项</div> : readinessCandidates.map((readiness) => <div className="ingestion-link-row" key={readiness.candidate_id}><span>{readiness.title}</span><select multiple value={links[readiness.candidate_id] ?? []} onChange={(event) => setLinks({ ...links, [readiness.candidate_id]: Array.from(event.target.selectedOptions, (option) => option.value) })}><option value="" disabled>选择关联镜头</option>{shotCandidates.map((shot) => <option key={shot.candidate_id} value={shot.candidate_id}>{shot.title}</option>)}</select>{readinessLinkCandidates.filter((link) => link.readiness_client_or_id_ref === readiness.candidate_id && link.source_status !== 'current').map((link) => <button className="btn btn-ghost btn-sm" key={link.candidate_id} type="button" disabled={link.source_change_revision == null || readinessLinkAcks[link.candidate_id] === link.source_change_revision} onClick={() => link.source_change_revision != null && onAcknowledgeReadinessLink(link.candidate_id, link.source_change_revision)}>确认关联来源变化</button>)}</div>)}</section><section className="planning-panel"><h3>暂存参考图</h3><p className="muted">选择要在同一事务中挂到整案的素材。</p>{assets.length === 0 ? <div className="ingestion-empty">暂无素材</div> : assets.map((asset) => <label className="ingestion-link-row" key={asset.id}><span>{asset.display_name || '参考图'}</span><input type="checkbox" checked={selectedAssets.has(asset.id)} onChange={() => { const next = new Set(selectedAssets); if (next.has(asset.id)) next.delete(asset.id); else next.add(asset.id); setSelectedAssets(next) }} /></label>)}</section><div className="ingestion-side-actions"><button className="btn" type="button" onClick={onBack}>返回原文</button><button className="btn btn-primary" type="button" disabled={!canContinue} onClick={onNext}>查看保存摘要</button></div></aside></div>
}

function CommitStep({ session, candidates, referenceCount, assetCount, busy, onCommit, onBack, onWorkspace }: { session: PlanIngestionSession | null; candidates: Candidate[]; referenceCount: number; assetCount: number; busy: boolean; onCommit: () => void; onBack: () => void; onWorkspace: () => void }) {
  const finished = session != null && session.state !== 'editing'
  const committed = session?.state === 'committed'
  return <section className="planning-panel ingestion-commit"><div className="panel-heading"><div><h2>{committed ? '已保存摄取结果' : finished ? '本次摄取已结束' : '确认保存'}</h2><p className="muted">所有核心候选、参考链接和素材绑定会一次提交；失败时不会产生半条镜头。</p></div><span className={`tag ${committed ? 'tag-ok' : finished ? 'tag' : 'tag-accent'}`}>{committed ? '已保存' : finished ? '已结束' : '待提交'}</span></div><div className="ingestion-summary-grid"><div><b>{candidates.length}</b><span>核心候选</span></div><div><b>{referenceCount}</b><span>参考链接</span></div><div><b>{assetCount}</b><span>素材绑定</span></div></div>{finished ? <button className="btn btn-primary" type="button" onClick={onWorkspace}>返回策划工作台</button> : <div className="ingestion-side-actions"><button className="btn" type="button" onClick={onBack}>继续编辑</button><button className="btn btn-primary" disabled={busy || !session} type="button" onClick={onCommit}>确认保存</button></div>}</section>
}
