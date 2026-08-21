import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ApiError } from '../api/client'
import {
  appendShootPlanShotResult,
  getShootPlan,
  newPlanningMutationKey,
  openShootPlanRunSession,
  type AppendShotResultInput,
  type OpenRunSessionResult,
  type ShootPlanSkipReason,
} from './api'
import { fetchPlanAssetDisplay } from './api'
import { planningErrorMessage } from './presentation'
import { applySavedShotResult, completedShotCount, nextPendingShotIndex, normalizeRunNotes, runOutcomeCounts, runShotState, validateSkipSubmission } from './runState'
import './run.css'

type RunView = {
  title: string
  opened: OpenRunSessionResult
}

type RunPageState =
  | { kind: 'opening' }
  | { kind: 'error'; message: string }
  | { kind: 'ready'; view: RunView }

const skipReasons: Array<{ value: ShootPlanSkipReason; label: string }> = [
  { value: 'preparation_missing', label: '准备未完成' },
  { value: 'time_insufficient', label: '时间不足' },
  { value: 'location_unavailable', label: '场地不可用' },
  { value: 'subject_unavailable', label: '拍摄对象不可用' },
  { value: 'creative_change', label: '创作调整' },
  { value: 'technical_failure', label: '技术故障' },
  { value: 'other', label: '其他' },
]

export default function ShootPlanRunPage() {
  const { id = '' } = useParams()
  const [state, setState] = useState<RunPageState>({ kind: 'opening' })
  const [currentIndex, setCurrentIndex] = useState(0)
  const [shotNotes, setShotNotes] = useState<Record<string, string>>({})
  const [skipReason, setSkipReason] = useState<ShootPlanSkipReason | ''>('')
  const [skipError, setSkipError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [savedMessage, setSavedMessage] = useState<string | null>(null)
  const [listOpen, setListOpen] = useState(false)
  const openKey = useRef(newPlanningMutationKey('open-run'))
  const openContext = useRef<{ title: string; expectedRevision: number } | null>(null)
  const pendingActionKeys = useRef(new Map<string, string>())

  const open = useCallback(async () => {
    setState({ kind: 'opening' })
    setActionError(null)
    try {
      if (!openContext.current) {
        const detail = await getShootPlan(id)
        openContext.current = { title: detail.title, expectedRevision: detail.revision }
      }
      const context = openContext.current
      const opened = await openShootPlanRunSession(id, context.expectedRevision, openKey.current)
      setState({ kind: 'ready', view: { title: context.title, opened } })
      const firstPending = opened.input.shots.findIndex((shot) => !shot.current_outcome)
      setCurrentIndex(firstPending >= 0 ? firstPending : 0)
    } catch (error) {
      if (error instanceof ApiError && (error.code === 'plan_revision_conflict' || error.code === 'idempotency_conflict')) {
        openContext.current = null
        openKey.current = newPlanningMutationKey('open-run')
      }
      const message = error instanceof ApiError && error.code === 'invalid_plan_transition'
        ? '只有已就绪或拍摄中的策划可以进入现场模式。'
        : planningErrorMessage(error, '现场模式加载失败')
      setState({ kind: 'error', message })
    }
  }, [id])

  useEffect(() => {
    void open()
  }, [open])

  useEffect(() => {
    if (!listOpen) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setListOpen(false)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [listOpen])

  async function saveResult(result: 'captured' | 'skipped' | 'cleared') {
    if (state.kind !== 'ready') return
    const shot = state.view.opened.input.shots[currentIndex]
    if (!shot || (result === 'cleared' && !shot.current_outcome)) return
    const note = normalizeRunNotes(shotNotes[shot.id] ?? '')
    if (result === 'skipped') {
      const problem = validateSkipSubmission(skipReason, note ?? '')
      if (problem) {
        setSkipError(problem)
        setSavedMessage(null)
        return
      }
    }
    setSkipError(null)
    const body: AppendShotResultInput = {
      expected_execution_revision: shot.execution_revision,
      session_id: state.view.opened.session.id,
      result,
      ...(result === 'skipped' ? { skip_reason: skipReason as ShootPlanSkipReason } : {}),
      ...(result !== 'cleared' && note ? { notes: note } : {}),
      ...(shot.current_outcome ? { supersedes_event_id: shot.current_outcome.event_id } : {}),
    }
    const signature = JSON.stringify({ shotID: shot.id, body })
    const key = pendingActionKeys.current.get(signature) ?? newPlanningMutationKey(`run-${result}`)
    pendingActionKeys.current.set(signature, key)
    setSaving(true)
    setActionError(null)
    setSavedMessage(null)
    try {
      const response = await appendShootPlanShotResult(id, shot.id, body, key)
      pendingActionKeys.current.delete(signature)
      const nextInput = applySavedShotResult(state.view.opened.input, shot.id, response)
      setState({
        kind: 'ready',
        view: { ...state.view, opened: { ...state.view.opened, input: nextInput } },
      })
      setSavedMessage(result === 'captured' ? '已保存：完成拍摄' : result === 'skipped' ? '已保存：本镜跳过' : '已保存：结果已清除')
      setCurrentIndex(nextPendingShotIndex(nextInput, currentIndex))
    } catch (error) {
      const prefix = error instanceof ApiError && error.code === 'execution_revision_conflict'
        ? '该镜头已有新结果。'
        : '未保存。'
      setActionError(`${prefix}${planningErrorMessage(error, '请检查网络后重试同一动作。')}`)
    } finally {
      setSaving(false)
    }
  }

  if (state.kind === 'opening') {
    return <main className="run-mode-page run-mode-centered" aria-busy="true"><div className="run-state-card"><span className="run-pulse" />正在建立现场会话…</div></main>
  }

  if (state.kind === 'error') {
    return (
      <main className="run-mode-page run-mode-centered">
        <div className="run-state-card" role="alert">
          <h1>无法进入现场模式</h1>
          <p>{state.message}</p>
          <div className="run-state-actions"><button className="run-button run-button-primary" type="button" onClick={() => void open()}>重试</button><Link className="run-button" to={`/shoot-plans/${encodeURIComponent(id)}`}>返回工作台</Link></div>
        </div>
      </main>
    )
  }

  const { view } = state
  const input = view.opened.input
  const shot = input.shots[currentIndex]
  const counts = runOutcomeCounts(input.shots)
  const allDone = input.shots.length > 0 && completedShotCount(input) === input.shots.length

  return (
    <main className="run-mode-page">
      <header className="run-header">
        <div><p className="run-kicker">现场执行 · {captureModeLabel(view.opened.session.capture_mode)}</p><h1>{view.title}</h1></div>
        <Link className="run-exit" to={`/shoot-plans/${encodeURIComponent(id)}`}>退出现场模式</Link>
      </header>
      {input.shots.length > 0 && (
        <section className="run-progress" aria-label="镜头进度，可点击跳转">
          <div className="run-progress-segments">
            {input.shots.map((segmentShot, i) => (
              <button
                key={segmentShot.id}
                type="button"
                className={`run-seg run-seg-${i === currentIndex ? 'now' : runShotState(segmentShot)}`}
                aria-label={`第 ${i + 1} 镜${segmentShot.title ? ` · ${segmentShot.title}` : ''}，点击跳转`}
                onClick={() => setCurrentIndex(i)}
              />
            ))}
          </div>
          <p className="run-progress-caption">
            <button type="button" className="run-linklike" onClick={() => setListOpen(true)}>第 {currentIndex + 1} 镜 / 共 {input.shots.length} 镜 ▾</button>
            <span> · 已捕获 {counts.captured} · 已跳过 {counts.skipped}</span>
          </p>
        </section>
      )}

      {input.shots.length === 0 || !shot ? (
        <section className="run-state-card" role="alert"><h2>当前没有可执行镜头</h2><p>请返回工作台补充镜头并标记已就绪。</p></section>
      ) : (
        <>
          <nav className="run-shot-nav" aria-label="逐镜导航">
            <button className="run-button" type="button" disabled={saving || currentIndex === 0} onClick={() => setCurrentIndex((value) => value - 1)}>← 上一镜</button>
            <span>第 {currentIndex + 1} / {input.shots.length} 镜</span>
            <button className="run-button" type="button" disabled={saving || currentIndex === input.shots.length - 1} onClick={() => setCurrentIndex((value) => value + 1)}>下一镜 →</button>
          </nav>
          <article className="run-shot-card">
            <div className="run-shot-heading">
              <div><p className="run-kicker">镜头 {shot.position}</p><h2>{shot.title}</h2></div>
              <OutcomeBadge shot={shot} />
            </div>
            <div className="run-shot-facts">
              <RunFact label="场景" value={shot.scene} />
              <RunFact label="动作" value={shot.action} />
              <RunFact label="表情" value={shot.expression} />
              <RunFact label="构图" value={shot.composition} />
              <RunFact label="打光" value={shot.lighting_text} />
            </div>
            <ReferenceSheet planID={id} refs={shot.asset_access_refs ?? []} />
            <ReadinessSummary shotID={shot.id} input={input} />
            <label className="run-note-field">
              <span>现场备注</span>
              <textarea rows={3} maxLength={1000} value={shotNotes[shot.id] ?? ''} placeholder="只记你需要的，不必填写" onChange={(event) => setShotNotes((current) => ({ ...current, [shot.id]: event.target.value }))} />
              <small>备注跟着这一镜保存，切换镜头不会丢。</small>
            </label>
          </article>

          <section className="run-actions" aria-label="记录本镜结果">
            {savedMessage && <p className="run-saved" role="status">{savedMessage}</p>}
            {actionError && <div className="run-unsaved" role="alert"><strong>未保存</strong><span>{actionError}</span><span>当前镜头和已保存状态保持不变，可直接重试同一动作。</span></div>}
            <button className="run-button run-button-captured" type="button" disabled={saving} onClick={() => void saveResult('captured')}>{saving ? '正在保存…' : '✓ 完成拍摄'}</button>
            <div className="run-skip-row">
              <select aria-label="跳过原因" value={skipReason} disabled={saving} onChange={(event) => { setSkipReason(event.target.value as ShootPlanSkipReason | ''); setSkipError(null) }}>
                <option value="" disabled>选择跳过原因</option>
                {skipReasons.map((reason) => <option value={reason.value} key={reason.value}>{reason.label}</option>)}
              </select>
              <button className="run-button run-button-skipped" type="button" disabled={saving} onClick={() => void saveResult('skipped')}>跳过本镜</button>
            </div>
            <p className="run-skip-hint">跳过必须写原因；选「其他」时在现场备注写明补充说明。</p>
            {skipError && <p className="run-validation" role="alert">{skipError}</p>}
            <button className="run-button run-button-clear" type="button" disabled={saving || !shot.current_outcome} onClick={() => void saveResult('cleared')}>清除本镜结果</button>
          </section>
        </>
      )}

      {allDone && <section className="run-complete" role="status"><strong>全部镜头已有结果</strong><span>可以返回工作台检查执行历史并标记完成。</span></section>}

      {listOpen && input.shots.length > 0 && (
        <div
          className="run-sheet"
          role="dialog"
          aria-modal="true"
          aria-labelledby="runListTitle"
          onMouseDown={(event) => { if (event.target === event.currentTarget) setListOpen(false) }}
        >
          <div className="run-sheet-card">
            <h3 id="runListTitle">镜头清单</h3>
            <p className="run-sheet-hint">点任意一条直接跳转；顺序即执行顺序。</p>
            <ol className="run-shotlist">
              {input.shots.map((listShot, i) => {
                const listState = i === currentIndex ? 'now' : runShotState(listShot)
                return (
                  <li key={listShot.id}>
                    <button
                      type="button"
                      aria-current={i === currentIndex || undefined}
                      onClick={() => { setCurrentIndex(i); setListOpen(false) }}
                    >
                      <span className="run-shotlist-num">{String(i + 1).padStart(2, '0')}</span>
                      <span className="run-shotlist-title">{listShot.title}</span>
                      <span className={`run-shotlist-tag run-shotlist-tag-${listState}`}>{runStateLabel(listState)}</span>
                    </button>
                  </li>
                )
              })}
            </ol>
            <button className="run-button run-sheet-close" type="button" autoFocus onClick={() => setListOpen(false)}>返回</button>
          </div>
        </div>
      )}
    </main>
  )
}

function runStateLabel(state: 'ok' | 'skip' | 'pending' | 'now'): string {
  if (state === 'ok') return '已捕获'
  if (state === 'skip') return '已跳过'
  if (state === 'now') return '当前'
  return '待执行'
}

function ReferenceSheet({ planID, refs }: { planID: string; refs: OpenRunSessionResult['input']['shots'][number]['asset_access_refs'] }) {
  const [failed, setFailed] = useState(0)
  const reportFailure = useCallback(() => setFailed((value) => value + 1), [])
  useEffect(() => { setFailed(0) }, [planID, refs])
  if (!refs || refs.length === 0) return null
  return <section className="run-reference-sheet" aria-label="本镜参考素材"><div className="run-reference-heading"><strong>本镜参考</strong><span>{refs.length} 张</span></div><div className="run-reference-grid">{refs.map((ref) => <ReferenceImage key={`${ref.asset_id}-${ref.generation}`} planID={planID} ref={ref} onFail={reportFailure} />)}</div>{failed > 0 && <small className="run-reference-note">部分参考素材暂不可用，不影响现场记录。</small>}</section>
}

function ReferenceImage({ planID, ref, onFail }: { planID: string; ref: NonNullable<OpenRunSessionResult['input']['shots'][number]['asset_access_refs']>[number]; onFail: () => void }) {
  const [src, setSrc] = useState<string | null>(null)
  useEffect(() => { let active = true; let url = ''; fetchPlanAssetDisplay(planID, ref.asset_id, ref.display_checksum).then((blob) => { if (active) { url = URL.createObjectURL(blob); setSrc(url) } }).catch(() => { if (active) onFail() }); return () => { active = false; if (url) URL.revokeObjectURL(url) } }, [planID, ref.asset_id, ref.display_checksum, onFail])
  return src ? <img className="run-reference-image" src={src} alt={ref.display_name || '参考素材'} /> : <div className="run-reference-placeholder">参考图加载中</div>
}

function RunFact({ label, value }: { label: string; value?: string | null }) {
  if (!value) return null
  return <div><span>{label}</span><p>{value}</p></div>
}

function OutcomeBadge({ shot }: { shot: OpenRunSessionResult['input']['shots'][number] }) {
  const outcome = shot.current_outcome
  if (!outcome) return <span className="run-outcome run-outcome-pending">待执行</span>
  return <span className={`run-outcome run-outcome-${outcome.result}`}>{outcome.result === 'captured' ? '已拍摄' : '已跳过'}</span>
}

function ReadinessSummary({ shotID, input }: { shotID: string; input: OpenRunSessionResult['input'] }) {
  const readinessIDs = input.shots.find((shot) => shot.id === shotID)?.readiness_item_ids ?? []
  const linked = input.readiness_items.filter((item) => readinessIDs.includes(item.id))
  if (linked.length === 0) return null
  return (
    <div className="run-readiness">
      <strong>本镜准备</strong>
      {linked.map((item) => <span key={item.id} className={item.preflight_status === 'checked' ? 'is-checked' : 'is-missing'}>{item.preflight_status === 'checked' ? '✓' : '!'} {item.title}</span>)}
    </div>
  )
}

function captureModeLabel(mode: string): string {
  if (mode === 'live') return '现场记录'
  if (mode === 'backfill') return '事后补记'
  return '未判定时段'
}
