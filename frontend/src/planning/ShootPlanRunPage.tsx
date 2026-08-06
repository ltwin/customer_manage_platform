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
import { planningErrorMessage } from './presentation'
import { applySavedShotResult, completedShotCount, nextPendingShotIndex } from './runState'
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
  const [skipReason, setSkipReason] = useState<ShootPlanSkipReason>('preparation_missing')
  const [saving, setSaving] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [savedMessage, setSavedMessage] = useState<string | null>(null)
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

  async function saveResult(result: 'captured' | 'skipped' | 'cleared') {
    if (state.kind !== 'ready') return
    const shot = state.view.opened.input.shots[currentIndex]
    if (!shot || (result === 'cleared' && !shot.current_outcome)) return
    const body: AppendShotResultInput = {
      expected_execution_revision: shot.execution_revision,
      session_id: state.view.opened.session.id,
      result,
      ...(result === 'skipped' ? { skip_reason: skipReason } : {}),
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
  const doneCount = completedShotCount(input)
  const allDone = input.shots.length > 0 && doneCount === input.shots.length

  return (
    <main className="run-mode-page">
      <header className="run-header">
        <div><p className="run-kicker">现场执行 · {captureModeLabel(view.opened.session.capture_mode)}</p><h1>{view.title}</h1></div>
        <Link className="run-exit" to={`/shoot-plans/${encodeURIComponent(id)}`}>退出现场模式</Link>
      </header>
      <section className="run-progress" aria-label="拍摄进度">
        <div><strong>{doneCount}</strong> / {input.shots.length} 已记录</div>
        <div className="run-progress-track"><span style={{ width: `${input.shots.length ? (doneCount / input.shots.length) * 100 : 0}%` }} /></div>
      </section>

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
            <ReadinessSummary shotID={shot.id} input={input} />
          </article>

          <section className="run-actions" aria-label="记录本镜结果">
            {savedMessage && <p className="run-saved" role="status">{savedMessage}</p>}
            {actionError && <div className="run-unsaved" role="alert"><strong>未保存</strong><span>{actionError}</span><span>当前镜头和已保存状态保持不变，可直接重试同一动作。</span></div>}
            <button className="run-button run-button-captured" type="button" disabled={saving} onClick={() => void saveResult('captured')}>{saving ? '正在保存…' : '✓ 完成拍摄'}</button>
            <div className="run-skip-row">
              <select aria-label="跳过原因" value={skipReason} disabled={saving} onChange={(event) => setSkipReason(event.target.value as ShootPlanSkipReason)}>
                {skipReasons.map((reason) => <option value={reason.value} key={reason.value}>{reason.label}</option>)}
              </select>
              <button className="run-button run-button-skipped" type="button" disabled={saving} onClick={() => void saveResult('skipped')}>跳过本镜</button>
            </div>
            <button className="run-button run-button-clear" type="button" disabled={saving || !shot.current_outcome} onClick={() => void saveResult('cleared')}>清除本镜结果</button>
          </section>
        </>
      )}

      {allDone && <section className="run-complete" role="status"><strong>全部镜头已有结果</strong><span>可以返回工作台检查执行历史并标记完成。</span></section>}
    </main>
  )
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
