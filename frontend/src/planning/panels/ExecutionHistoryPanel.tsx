import { useRef, useState } from 'react'
import ConfirmDialog from '../../components/ConfirmDialog'
import { planningErrorMessage } from '../presentation'
import { newPlanningMutationKey, voidShootPlanExecutionEvent, type ShootPlanDetail, type ShotExecutionFact } from '../api'

export default function ExecutionHistoryPanel({ plan, busy, onReload }: { plan: ShootPlanDetail; busy: boolean; onReload: () => Promise<void> }) {
  const history = plan.execution_history ?? []
  const voidedTargets = new Set(history.filter((fact) => fact.kind === 'void').map((fact) => fact.target_event_id))
  const [error, setError] = useState<string | null>(null)
  const [voiding, setVoiding] = useState<string | null>(null)
  const [voidTarget, setVoidTarget] = useState<Extract<ShotExecutionFact, { kind: 'result' }> | null>(null)
  const pendingKeys = useRef(new Map<string, string>())

  async function voidEvent(fact: Extract<ShotExecutionFact, { kind: 'result' }>, reason: string) {
    const shot = plan.shots.find((candidate) => candidate.id === fact.shot_id)
    if (!shot) { setError('这条事实对应的镜头已不在当前镜头表中，不能从工作台作废。'); return }
    const key = pendingKeys.current.get(fact.id) ?? newPlanningMutationKey('void-event')
    pendingKeys.current.set(fact.id, key)
    setVoiding(fact.id)
    setError(null)
    try {
      await voidShootPlanExecutionEvent(plan.id, fact.id, { expected_execution_revision: shot.execution_revision, reason }, key)
      pendingKeys.current.delete(fact.id)
      await onReload()
    } catch (cause) {
      setError(planningErrorMessage(cause, '作废执行事实失败；再次提交会重放同一请求。'))
    } finally {
      setVoiding(null)
    }
  }

  return (
    <section className="planning-section">
      <div className="planning-section-head"><div><p className="planning-eyebrow">执行历史</p><h2>追加事实与完成快照</h2><p>历史不可覆盖；纠错通过追加作废事实完成。</p></div></div>
      {error && <p className="planning-inline-error" role="alert">{error}</p>}
      {history.length === 0 ? <div className="planning-empty-card">尚无执行记录。现场结果会在 Run Mode 上线后从独立界面写入。</div> : (
        <div className="planning-history-list">
          {history.map((fact) => (
            <article className={`card planning-history-item planning-history-${fact.kind}`} key={fact.id}>
              <div className="planning-history-seq">#{fact.shot_event_seq}</div>
              <div className="planning-history-main">
                <div className="planning-title-line"><h3>{historyTitle(fact, plan)}</h3><span className={`badge ${fact.kind === 'void' ? 'badge-danger' : fact.result === 'captured' ? 'badge-success' : fact.result === 'skipped' ? 'badge-warning' : 'badge-muted'}`}>{historyKindLabel(fact)}</span></div>
                <p>{historyDescription(fact)}</p>
                <div className="planning-meta"><span>{fact.kind === 'result' ? formatDateTime(fact.checked_at) : formatDateTime(fact.voided_at)}</span><span>事实版本 {fact.revision}</span>{fact.kind === 'result' && <span>{captureModeLabel(fact.capture_mode)}</span>}</div>
              </div>
              {fact.kind === 'result' && plan.status === 'in_progress' && (
                <button className="btn btn-danger-ghost btn-sm" type="button" disabled={busy || voiding !== null || voidedTargets.has(fact.id)} onClick={() => setVoidTarget(fact)}>{voidedTargets.has(fact.id) ? '已作废' : voiding === fact.id ? '正在作废…' : '作废'}</button>
              )}
            </article>
          ))}
        </div>
      )}
      <div className="planning-finalizations">
        <h2>完成快照</h2>
        {(plan.finalizations ?? []).length === 0 ? <p className="muted-text">尚未完成过这份策划。</p> : (plan.finalizations ?? []).map((snapshot) => (
          <article className="card" key={snapshot.id}><div className="planning-title-line"><h3>第 {snapshot.finalization_revision} 次完成</h3><span className="badge badge-muted">策划第 {snapshot.plan_revision} 版</span></div><div className="planning-meta"><span>{snapshot.current_shot_ids.length} 个镜头</span><span>{snapshot.preparation_missing_event_ids.length} 条现场准备缺失</span><span>{formatDateTime(snapshot.finalized_at)}</span></div></article>
        ))}
      </div>
      {voidTarget && (
        <ConfirmDialog
          title="追加作废事实？"
          body={['作废不会删除原始记录，而是追加一条可审计的作废事实。', '当前镜头结果会按完整时间线重新计算。']}
          confirmLabel="追加作废事实"
          danger
          requiredInput={{ label: '作废原因', placeholder: '例如：误记录了一次拍摄', maxLength: 200 }}
          busy={busy || voiding !== null}
          onConfirm={(reason) => { const fact = voidTarget; setVoidTarget(null); void voidEvent(fact, reason) }}
          onCancel={() => setVoidTarget(null)}
        />
      )}
    </section>
  )
}

function historyTitle(fact: ShotExecutionFact, plan: ShootPlanDetail): string {
  const shotID = fact.shot_id
  return plan.shots.find((shot) => shot.id === shotID)?.title ?? `历史镜头 ${shotID}`
}

function historyKindLabel(fact: ShotExecutionFact): string {
  if (fact.kind === 'void') return '作废'
  if (fact.result === 'captured') return '已捕获'
  if (fact.result === 'skipped') return '已跳过'
  return '已清除'
}

function historyDescription(fact: ShotExecutionFact): string {
  if (fact.kind === 'void') return `作废目标 ${fact.target_event_id}：${fact.reason}`
  const parts = []
  if (fact.skip_reason) parts.push(`原因：${fact.skip_reason}`)
  if (fact.notes) parts.push(`备注：${fact.notes}`)
  if (fact.supersedes_event_id) parts.push(`替代：${fact.supersedes_event_id}`)
  return parts.join(' · ') || '无补充说明'
}

function captureModeLabel(mode: string): string {
  return mode === 'live' ? '现场完成' : mode === 'backfill' ? '事后补记' : '未判定现场模式'
}

function formatDateTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? value : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}
