import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { ApiError } from '../../api/client.ts'
import ConfirmDialog from '../../components/ConfirmDialog'
import { isPackagePriceYuanInputAllowed } from '../../pages/packagePrice.ts'
import type { ShootPlanDetail } from '../api'
import {
  decideShootPlanBusinessDraft,
  generateShootPlanBusinessDrafts,
  type BusinessDraftDecisionInput,
  type OrderAdjustmentDraftView,
  type ScheduleDurationDraftView,
} from '../api'
import {
  businessDraftGenerationMessage,
  businessDraftUnavailableMessage,
  optionalAbsoluteTargetPriceYuanToCents,
  validateOptionalAbsoluteTargetPriceYuan,
} from '../businessDraftInput.ts'
import type { CommandRunner } from '../ShootPlanWorkspacePage'
import { planningErrorMessage } from '../presentation'

type FactsDraft = {
  rentedLocationCount: string
  assistantCount: string
  retouchedPhotoCount: string
  estimatedDurationMinutes: string
}

export default function BusinessPanel({
  plan,
  busy,
  runCommand,
  getMutationKey,
  acknowledgeMutation,
  onReload,
}: {
  plan: ShootPlanDetail
  busy: boolean
  runCommand: CommandRunner
  getMutationKey(signature: string, scope: string): string
  acknowledgeMutation(signature: string): void
  onReload: () => Promise<void>
}) {
  const navigate = useNavigate()
  const [facts, setFacts] = useState<FactsDraft>(() => factsFromPlan(plan))
  const [dirty, setDirty] = useState(false)
  const [absoluteTargetPriceYuan, setAbsoluteTargetPriceYuan] = useState('')
  const [working, setWorking] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [confirmingDecision, setConfirmingDecision] = useState<{ draft: OrderAdjustmentDraftView | ScheduleDurationDraftView; decision: BusinessDraftDecisionInput['decision'] } | null>(null)

  useEffect(() => {
    if (!dirty) setFacts(factsFromPlan(plan))
  }, [dirty, plan])

  const readOnly = plan.status === 'archived' || plan.status === 'completed'
  const disabled = busy || working || readOnly

  async function saveFacts() {
    setWorking(true)
    setError(null)
    setMessage(null)
    try {
      await runCommand({
        expected_revision: plan.revision,
        operation: 'set_business_facts',
        expected_business_facts_revision: plan.business.facts.revision,
        facts: {
          rented_location_count: parseOptionalInteger(facts.rentedLocationCount),
          assistant_count: parseOptionalInteger(facts.assistantCount),
          retouched_photo_count: parseOptionalInteger(facts.retouchedPhotoCount),
          estimated_duration_minutes: parseOptionalInteger(facts.estimatedDurationMinutes),
        },
      }, 'business-facts')
      setDirty(false)
      setMessage('经营事实已保存。')
    } catch (cause) {
      setError(planningErrorMessage(cause, '经营事实保存失败'))
    } finally {
      setWorking(false)
    }
  }

  async function generateDrafts() {
    const validationError = validateOptionalAbsoluteTargetPriceYuan(absoluteTargetPriceYuan)
    if (validationError) {
      setError(validationError)
      setMessage(null)
      return
    }
    setWorking(true)
    setError(null)
    setMessage(null)
    const body = {
      expected_plan_revision: plan.revision,
      expected_business_facts_revision: plan.business.facts.revision,
      draft_kinds: ['order_adjustment', 'schedule_duration'],
      absolute_target_price: optionalAbsoluteTargetPriceYuanToCents(absoluteTargetPriceYuan),
    } satisfies Parameters<typeof generateShootPlanBusinessDrafts>[1]
    const signature = `business-generate:${plan.id}:${JSON.stringify(body)}`
    const key = getMutationKey(signature, 'business-generate')
    try {
      const result = await generateShootPlanBusinessDrafts(plan.id, body, key)
      acknowledgeMutation(signature)
      setMessage(businessDraftGenerationMessage(result))
      try {
        await onReload()
      } catch {
        setError('草稿生成已提交，但最新页面加载失败；请刷新页面查看结果。')
      }
    } catch (cause) {
      if (shouldRefreshBusinessDraft(cause)) await onReload().catch(() => undefined)
      const unavailableMessage = cause instanceof ApiError && cause.code === 'business_draft_unavailable'
        ? businessDraftUnavailableMessage(cause.details)
        : null
      setError(unavailableMessage ?? planningErrorMessage(cause, '经营草稿生成失败'))
    } finally {
      setWorking(false)
    }
  }

  async function requestDecision(
    draft: OrderAdjustmentDraftView | ScheduleDurationDraftView,
    decision: BusinessDraftDecisionInput['decision'],
  ): Promise<void> {
    if (decision !== 'dismiss' && draft.required_acknowledgement) {
      setConfirmingDecision({ draft, decision })
      return
    }
    await decide(draft, decision)
  }

  async function decide(
    draft: OrderAdjustmentDraftView | ScheduleDurationDraftView,
    decision: BusinessDraftDecisionInput['decision'],
  ) {
    let acknowledgement: BusinessDraftDecisionInput['acknowledgement']
    if (decision !== 'dismiss' && draft.required_acknowledgement) {
      acknowledgement = draft.required_acknowledgement
    }
    setWorking(true)
    setError(null)
    setMessage(null)
    const body = {
      expected_draft_revision: draft.revision,
      decision,
      acknowledgement,
    } satisfies BusinessDraftDecisionInput
    const signature = `business-decision:${plan.id}:${draft.id}:${JSON.stringify(body)}`
    const key = getMutationKey(signature, `business-${decision}`)
    try {
      await decideShootPlanBusinessDraft(plan.id, draft.id, body, key)
      acknowledgeMutation(signature)
      setMessage(decision === 'dismiss' ? '草稿已忽略。' : '草稿已应用并保留审计记录。')
      try {
        await onReload()
      } catch {
        setError('草稿操作已提交，但最新页面加载失败；请刷新页面查看结果。')
      }
    } catch (cause) {
      if (shouldRefreshBusinessDraft(cause)) await onReload().catch(() => undefined)
      setError(planningErrorMessage(cause, '经营草稿处理失败'))
    } finally {
      setWorking(false)
    }
  }

  function openCalendar(draft: ScheduleDurationDraftView) {
    navigate('/calendar', {
      state: {
        kind: 'planning-schedule-prefill-v1',
        order_id: plan.crm?.order_id ?? '',
        basis_minutes: draft.basis_minutes,
      },
    })
  }

  return (
    <section className="planning-panel planning-business-panel" aria-labelledby="business-heading">
      <div className="planning-panel-header">
        <div>
          <p className="planning-eyebrow">仅你可见</p>
          <h2 id="business-heading">经营草稿</h2>
        </div>
        <div className="planning-business-generation-actions">
          <label className="planning-field planning-business-target-price">
            <span>绝对目标价（元，可选）</span>
            <input
              inputMode="decimal"
              value={absoluteTargetPriceYuan}
              disabled={disabled}
              placeholder="按当前订单价计算"
              onChange={(event) => {
                const value = event.target.value
                if (!isPackagePriceYuanInputAllowed(value)) return
                setAbsoluteTargetPriceYuan(value)
                setError(null)
              }}
            />
          </label>
          <button className="btn btn-primary" type="button" disabled={disabled || dirty} onClick={() => void generateDrafts()}>
            重新生成草稿
          </button>
        </div>
      </div>

      {readOnly && <p className="planning-muted">已完成或归档的策划只保留历史经营信息；重新打开后才能修改。</p>}
      {message && <div className="planning-feedback" role="status">{message}</div>}
      {error && <div className="planning-action-error" role="alert">{error}</div>}

      <div className="planning-business-facts">
        <div className="planning-section-heading">
          <h3>复杂度事实</h3>
          <span>留空表示未知，0 表示明确为零</span>
        </div>
        <div className="planning-form-grid planning-business-form-grid">
          <NumberField label="付费场地数" value={facts.rentedLocationCount} max={100} disabled={disabled} onChange={(value) => editFact('rentedLocationCount', value)} />
          <NumberField label="助理人数" value={facts.assistantCount} max={100} disabled={disabled} onChange={(value) => editFact('assistantCount', value)} />
          <NumberField label="精修张数" value={facts.retouchedPhotoCount} max={100000} disabled={disabled} onChange={(value) => editFact('retouchedPhotoCount', value)} />
          <NumberField label="预估时长（分钟）" value={facts.estimatedDurationMinutes} max={10080} disabled={disabled} onChange={(value) => editFact('estimatedDurationMinutes', value)} />
        </div>
        <div className="planning-business-public-inputs">
          <span>计划造型：{plan.business.public_inputs.planned_look_count ?? '未知'}</span>
          <span>当前镜头：{plan.business.public_inputs.current_shot_count}</span>
          <span>规则：{shortRuleVersion(plan.business.effective_rules.rule_version)}</span>
        </div>
        <div className="planning-inline-actions">
          <button className="btn" type="button" disabled={disabled || !dirty} onClick={() => void saveFacts()}>保存事实</button>
          {dirty && <button className="btn btn-ghost" type="button" disabled={disabled} onClick={() => { setFacts(factsFromPlan(plan)); setDirty(false) }}>撤销输入</button>}
        </div>
      </div>

      <div className="planning-business-grid">
        <OrderDraftCard draft={plan.business.order_adjustment} disabled={disabled} onDecision={requestDecision} />
        <ScheduleDraftCard draft={plan.business.schedule_duration} disabled={disabled} onDecision={requestDecision} onOpenCalendar={openCalendar} />
      </div>
      {confirmingDecision && confirmingDecision.draft.required_acknowledgement && (
        <ConfirmDialog
          title="确认应用这份草稿？"
          body={confirmingDecision.draft.required_acknowledgement.effects.map(effectLabel)}
          confirmLabel="确认继续"
          busy={working}
          onConfirm={() => { const pending = confirmingDecision; setConfirmingDecision(null); void decide(pending.draft, pending.decision) }}
          onCancel={() => setConfirmingDecision(null)}
        />
      )}
    </section>
  )

  function editFact(field: keyof FactsDraft, value: string) {
    setFacts((current) => ({ ...current, [field]: value }))
    setDirty(true)
    setError(null)
  }
}

function NumberField({ label, value, max, disabled, onChange }: { label: string; value: string; max: number; disabled: boolean; onChange: (value: string) => void }) {
  return (
    <label className="planning-field">
      <span>{label}</span>
      <input type="number" min="0" max={max} step="1" inputMode="numeric" value={value} disabled={disabled} placeholder="未知" onChange={(event) => onChange(event.target.value)} />
    </label>
  )
}

function OrderDraftCard({ draft, disabled, onDecision }: { draft: OrderAdjustmentDraftView | null; disabled: boolean; onDecision: (draft: OrderAdjustmentDraftView, decision: BusinessDraftDecisionInput['decision']) => Promise<void> }) {
  if (!draft) return <EmptyDraft title="订单价格草稿" />
  const actionable = draft.status === 'fresh' && draft.required_acknowledgement !== null
  return (
    <article className="planning-business-card">
      <DraftHeader title="订单价格草稿" status={draft.status} />
      <div className="planning-money-summary">
        <span>当前价格 {formatMoney(draft.base_price)}</span>
        <strong>建议价格 {formatMoney(draft.proposed_total)}</strong>
      </div>
      <div className="planning-business-lines">
        {draft.lines.map((line) => (
          <div key={line.kind} className="planning-business-line">
            <span>{line.label}</span>
            <span>{line.quantity ?? '未知'} × {formatMoney(line.unit_amount)}</span>
            <strong>{formatMoney(line.amount)}</strong>
          </div>
        ))}
      </div>
      <Warnings warnings={draft.warnings} staleReason={draft.stale_reason} />
      <div className="planning-inline-actions">
        <button className="btn btn-primary" type="button" disabled={disabled || !actionable} onClick={() => void onDecision(draft, 'apply_order_adjustment')}>应用到订单</button>
        {draft.status !== 'applied' && <button className="btn btn-ghost" type="button" disabled={disabled} onClick={() => void onDecision(draft, 'dismiss')}>忽略</button>}
      </div>
    </article>
  )
}

function ScheduleDraftCard({ draft, disabled, onDecision, onOpenCalendar }: { draft: ScheduleDurationDraftView | null; disabled: boolean; onDecision: (draft: ScheduleDurationDraftView, decision: BusinessDraftDecisionInput['decision']) => Promise<void>; onOpenCalendar: (draft: ScheduleDurationDraftView) => void }) {
  if (!draft) return <EmptyDraft title="档期时长草稿" />
  const updateExisting = draft.target_mode === 'update_existing'
  const actionable = draft.status === 'fresh' && draft.required_acknowledgement !== null
  return (
    <article className="planning-business-card">
      <DraftHeader title="档期时长草稿" status={draft.status} />
      <div className="planning-duration-summary">
        <span>依据 {draft.basis_minutes} 分钟</span>
        {updateExisting ? (
          <strong>{formatDateTime(draft.original_end_at)} → {formatDateTime(draft.proposed_end_at)}</strong>
        ) : (
          <strong>尚无拍摄档期</strong>
        )}
      </div>
      <Warnings warnings={draft.warnings} staleReason={draft.stale_reason} />
      <div className="planning-inline-actions">
        {updateExisting ? (
          <button className="btn btn-primary" type="button" disabled={disabled || !actionable} onClick={() => void onDecision(draft, 'apply_schedule_duration')}>更新档期结束时间</button>
        ) : (
          <button className="btn btn-primary" type="button" disabled={disabled || draft.status !== 'fresh'} onClick={() => onOpenCalendar(draft)}>去日历选择时间</button>
        )}
        {draft.status !== 'applied' && <button className="btn btn-ghost" type="button" disabled={disabled} onClick={() => void onDecision(draft, 'dismiss')}>忽略</button>}
      </div>
    </article>
  )
}

function DraftHeader({ title, status }: { title: string; status: string }) {
  const labels: Record<string, string> = { fresh: '待确认', stale: '已过期', applied: '已应用', dismissed: '已忽略' }
  return <div className="planning-section-heading"><h3>{title}</h3><span className={`planning-draft-status is-${status}`}>{labels[status] ?? status}</span></div>
}

function EmptyDraft({ title }: { title: string }) {
  return <article className="planning-business-card is-empty"><h3>{title}</h3><p>保存复杂度事实后生成草稿。</p></article>
}

function Warnings({ warnings, staleReason }: { warnings: string[]; staleReason?: string }) {
  const items = [...warnings.map(warningLabel)]
  if (staleReason) items.unshift(`当前草稿已失效：${staleLabel(staleReason)}`)
  if (items.length === 0) return null
  return <ul className="planning-business-warnings">{items.map((item) => <li key={item}>{item}</li>)}</ul>
}

function factsFromPlan(plan: ShootPlanDetail): FactsDraft {
  const facts = plan.business.facts
  return {
    rentedLocationCount: inputValue(facts.rented_location_count),
    assistantCount: inputValue(facts.assistant_count),
    retouchedPhotoCount: inputValue(facts.retouched_photo_count),
    estimatedDurationMinutes: inputValue(facts.estimated_duration_minutes),
  }
}

function inputValue(value: number | null): string { return value === null ? '' : String(value) }

function parseOptionalInteger(value: string): number | null {
  const trimmed = value.trim()
  if (trimmed === '') return null
  const parsed = Number(trimmed)
  if (!Number.isSafeInteger(parsed) || parsed < 0) throw new Error('请输入非负整数')
  return parsed
}

function formatMoney(value: number | null): string {
  if (value === null) return '未知'
  return `¥${(value / 100).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

function formatDateTime(value: string | null): string {
  if (!value) return '未知'
  return new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}

function shortRuleVersion(value: string): string { return value.split(':').slice(0, 2).join(' · ') }

function warningLabel(value: string): string {
  const labels: Record<string, string> = {
    unknown_source_fact: '部分数量仍未知',
    unknown_rate: '部分费率尚未设置',
    unknown_adjustment_lines_excluded: '未知调整项未计入建议价格',
    no_material_change: '建议与当前内容相同，无需应用',
  }
  return labels[value] ?? value
}

function staleLabel(value: string): string {
  const labels: Record<string, string> = {
    superseded: '已有更新草稿', expired: '超过有效期', plan_revision_changed: '策划已更新',
    business_facts_revision_changed: '复杂度事实已更新', crm_connection_changed: '订单关联已更新',
    crm_projection_changed: '档期投影已更新', rule_version_changed: '经营规则已更新',
    order_target_missing: '订单已不存在', order_stage_ineligible: '订单阶段不适用',
    order_target_changed: '订单价格或状态已更新', slot_target_missing: '档期已不存在',
    schedule_stage_ineligible: '当前阶段不适用档期建议', slot_target_changed: '档期已更新',
  }
  return labels[value] ?? value
}

function shouldRefreshBusinessDraft(cause: unknown): boolean {
  return cause instanceof ApiError && (
    cause.code === 'stale_business_draft'
    || cause.code === 'business_revision_conflict'
    || cause.code === 'plan_revision_conflict'
    || cause.code === 'validation_failed'
  )
}

function effectLabel(value: string): string {
  const labels: Record<string, string> = {
    order_price_updated: '将更新订单价格', price_adjustment_audit_recorded: '将保留价格调整审计',
    schedule_unchanged: '不会修改档期', schedule_end_updated: '将更新档期结束时间',
    order_price_unchanged: '不会修改订单价格', customer_not_notified: '不会自动通知客户',
  }
  return `• ${labels[value] ?? value}`
}
