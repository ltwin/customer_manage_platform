import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import ConfirmDialog from '../../components/ConfirmDialog'
import { planningErrorMessage } from '../presentation'
import type { CommandRunner } from '../ShootPlanWorkspacePage'
import type { PlanCommand, ShootPlanDetail, ShootPlanReadinessItem } from '../api'
import { getShootPlanAssignments, type AssignmentManagementItem } from '../share/api'
import { preparationMissingShotPositions, readinessTag } from '../outcomeLabels'

const categoryLabels: Record<string, string> = { styling: '妆造', location: '场地', prop_equipment: '道具与设备', other: '其他' }
const responsibilityLabels: Record<string, string> = { photographer: '摄影师', customer: '拍摄对象', unassigned: '未分配' }

export default function ReadinessPanel({ plan, busy, runCommand }: { plan: ShootPlanDetail; busy: boolean; runCommand: CommandRunner }) {
  const [editing, setEditing] = useState<ShootPlanReadinessItem | 'new' | null>(null)
  const [removing, setRemoving] = useState<ShootPlanReadinessItem | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [assignments, setAssignments] = useState<AssignmentManagementItem[]>([])
  const readonly = plan.status === 'archived'

  useEffect(() => {
    let active = true
    getShootPlanAssignments(plan.id, { limit: 100 })
      .then((page) => { if (active) setAssignments(page.items) })
      .catch(() => undefined)
    return () => { active = false }
  }, [plan.id, plan.revision])

  const claimantByReadiness = new Map<string, string>()
  for (const assignment of assignments) {
    if (assignment.status === 'active' && assignment.assignment_kind === 'readiness'
      && assignment.target.kind === 'readiness' && assignment.target.readiness_item_id) {
      claimantByReadiness.set(assignment.target.readiness_item_id, assignment.claimed_by_display_name)
    }
  }

  async function toggle(item: ShootPlanReadinessItem) {
    setError(null)
    try {
      await runCommand({
        expected_revision: plan.revision,
        operation: 'set_preflight',
        readiness_id: item.id,
        preflight_status: item.preflight_status === 'checked' ? 'unchecked' : 'checked',
      }, 'set-preflight')
    } catch (cause) {
      setError(planningErrorMessage(cause, '准备项核对状态保存失败'))
    }
  }

  async function remove(item: ShootPlanReadinessItem) {
    setError(null)
    try {
      await runCommand({ expected_revision: plan.revision, operation: 'remove_readiness', readiness_id: item.id }, 'remove-readiness')
    } catch (cause) {
      setError(planningErrorMessage(cause, '移除准备项失败'))
    }
  }

  const required = plan.readiness_items.filter((item) => item.requirement === 'required')
  const requiredDone = required.filter((item) => item.preflight_status === 'checked').length
  const uncheckedRequired = required.filter((item) => item.preflight_status === 'unchecked')
  return (
    <section className="planning-section">
      <div className="planning-section-head">
        <div><p className="planning-eyebrow">准备项</p><h2>拍摄前核对清单</h2><p>只有显式标为“必需”的项目会阻塞草稿进入已就绪。</p></div>
        <button className="btn btn-primary" type="button" disabled={busy || readonly} onClick={() => setEditing('new')}>＋ 新增准备项</button>
      </div>
      <div className="planning-readiness-summary"><strong>{requiredDone} / {required.length}</strong><span>必需准备项已核对</span></div>
      {plan.status === 'draft' && uncheckedRequired.length > 0 && (
        <div className="planning-note planning-note-warn">
          <strong>完成核对前不能标记已就绪</strong>（标记按钮在右上角）。未核对：{uncheckedRequired.map((item) => item.title).join('、')}。
        </div>
      )}
      {error && <p className="planning-inline-error" role="alert">{error}</p>}
      {plan.readiness_items.length === 0 ? (
        <div className="planning-empty-card">还没有准备项。没有必需准备项时，不会阻塞“标记已就绪”。</div>
      ) : (
        (['styling', 'location', 'prop_equipment', 'other'] as const)
          .map((category) => ({
            category,
            label: categoryLabels[category] ?? category,
            items: plan.readiness_items.filter((item) => item.category === category),
          }))
          .filter((group) => group.items.length > 0)
          .map((group) => (
            <div className="planning-ready-group" key={group.category}>
              <p className="planning-eyebrow">{group.label}</p>
              <div className="planning-readiness-list">
                {group.items.map((item) => {
                  const linkedShots = plan.shots.filter((shot) => shot.readiness_item_ids.includes(item.id))
                  const claimant = claimantByReadiness.get(item.id)
                  const missingPositions = preparationMissingShotPositions(plan.shots, item.id)
                  const tag = readinessTag(item, missingPositions.length > 0)
                  return (
                    <article className="card planning-readiness-card" key={item.id}>
                      <label className="planning-check">
                        <input type="checkbox" checked={item.preflight_status === 'checked'} disabled={busy || readonly} onChange={() => void toggle(item)} />
                        <span className="sr-only">切换核对状态</span>
                      </label>
                      <div className="planning-readiness-main">
                        <div className="planning-title-line"><h3>{item.title}</h3><span className={tag.className}>{tag.label}</span></div>
                        <div className="planning-meta">
                          <span>{group.label}</span>
                          <span>责任提示：{responsibilityLabels[item.responsibility_hint] ?? item.responsibility_hint}</span>
                          {claimant && <span>客户认领 · {claimant}</span>}
                          {item.default_preparation_lead_days !== undefined && item.default_preparation_lead_days !== null && <span>建议提前 {item.default_preparation_lead_days} 天</span>}
                        </div>
                        {missingPositions.length > 0 && (
                          <p className="planning-site-missing">现场缺失 · 来自现场模式第 {missingPositions.join('、')} 镜跳过记录</p>
                        )}
                        {linkedShots.length > 0 && <p className="planning-linked-shots">关联镜头：{linkedShots.map((shot) => shot.title).join('、')}</p>}
                      </div>
                      <div className="planning-card-actions"><button className="btn btn-sm" type="button" disabled={busy || readonly} onClick={() => setEditing(item)}>编辑</button><button className="btn btn-danger-ghost btn-sm" type="button" disabled={busy || readonly} onClick={() => setRemoving(item)}>移除</button></div>
                    </article>
                  )
                })}
              </div>
            </div>
          ))
      )}
      {editing && <ReadinessDialog plan={plan} item={editing === 'new' ? null : editing} busy={busy} runCommand={runCommand} onClose={() => setEditing(null)} />}
      {removing && (
        <ConfirmDialog
          title={`移除准备项「${removing.title}」？`}
          body={`移除后会同时解除与 ${plan.shots.filter((shot) => shot.readiness_item_ids.includes(removing.id)).length} 个镜头的关联，但不会删除镜头。`}
          confirmLabel="移除准备项"
          danger
          busy={busy}
          onConfirm={() => { const item = removing; setRemoving(null); void remove(item) }}
          onCancel={() => setRemoving(null)}
        />
      )}
    </section>
  )
}

function ReadinessDialog({ plan, item, busy, runCommand, onClose }: { plan: ShootPlanDetail; item: ShootPlanReadinessItem | null; busy: boolean; runCommand: CommandRunner; onClose: () => void }) {
  const [title, setTitle] = useState(item?.title ?? '')
  const [category, setCategory] = useState<string>(item?.category ?? 'other')
  const [requirement, setRequirement] = useState<string>(item?.requirement ?? 'optional')
  const [preflight, setPreflight] = useState<string>(item?.preflight_status ?? 'unchecked')
  const [responsibility, setResponsibility] = useState<string>(item?.responsibility_hint ?? 'unassigned')
  const [leadDays, setLeadDays] = useState(item?.default_preparation_lead_days?.toString() ?? '')
  const [error, setError] = useState<string | null>(null)

  async function save(event: FormEvent) {
    event.preventDefault()
    if (!title.trim()) { setError('准备项标题不能为空。'); return }
    setError(null)
    const command = {
      expected_revision: plan.revision,
      operation: 'upsert_readiness',
      ...(item ? { readiness_id: item.id } : {}),
      item: {
        title: title.trim(), category, requirement, preflight_status: preflight,
        responsibility_hint: responsibility,
        default_preparation_lead_days: leadDays.trim() ? Number(leadDays) : null,
      },
    } as PlanCommand
    try {
      await runCommand(command, item ? 'update-readiness' : 'create-readiness')
      onClose()
    } catch (cause) {
      setError(planningErrorMessage(cause, '准备项保存失败'))
    }
  }

  return (
    <div className="overlay open" onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) onClose() }}>
      <form className="dialog planning-dialog" role="dialog" aria-modal="true" aria-labelledby="readinessDialogTitle" onSubmit={save}>
        <h2 id="readinessDialogTitle">{item ? '编辑准备项' : '新增准备项'}</h2>
        <p className="dialog-sub">拍摄前核对与现场跳过原因是两套独立事实。</p>
        <label className="field"><span>标题</span><input className="input" autoFocus value={title} maxLength={240} onChange={(event) => setTitle(event.target.value)} /></label>
        <div className="field-row">
          <Select label="类别" value={category} setValue={setCategory} options={categoryLabels} />
          <Select label="层级" value={requirement} setValue={setRequirement} options={{ required: '必需', optional: '可选' }} />
        </div>
        <div className="field-row">
          <Select label="拍摄前核对" value={preflight} setValue={setPreflight} options={{ unchecked: '未核对', checked: '已核对' }} />
          <Select label="责任提示" value={responsibility} setValue={setResponsibility} options={responsibilityLabels} />
        </div>
        <label className="field"><span>默认建议提前天数</span><input className="input" type="number" min="0" max="365" value={leadDays} onChange={(event) => setLeadDays(event.target.value)} placeholder="未知则留空" /></label>
        {error && <p className="planning-inline-error" role="alert">{error}</p>}
        <div className="dialog-actions"><button className="btn" type="button" disabled={busy} onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy}>{busy ? '正在保存…' : '保存准备项'}</button></div>
      </form>
    </div>
  )
}

function Select({ label, value, setValue, options }: { label: string; value: string; setValue: (value: string) => void; options: Record<string, string> }) {
  return <label className="field"><span>{label}</span><select className="input" value={value} onChange={(event) => setValue(event.target.value)}>{Object.entries(options).map(([option, text]) => <option key={option} value={option}>{text}</option>)}</select></label>
}
