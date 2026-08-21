import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { planningErrorMessage } from '../presentation'
import type { CommandRunner } from '../ShootPlanWorkspacePage'
import type { PlanCommand, ShootPlanDetail, ShootPlanShot } from '../api'
import { shotHasExecutionHistory } from '../history'

export default function ShotsPanel({ plan, busy, runCommand, focusShotID }: { plan: ShootPlanDetail; busy: boolean; runCommand: CommandRunner; focusShotID: string | null }) {
  const [editing, setEditing] = useState<ShootPlanShot | 'new' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [highlightID, setHighlightID] = useState<string | null>(null)
  const readonly = plan.status === 'archived'
  const appliedFocusID = useRef<string | null>(null)

  useEffect(() => {
    if (!focusShotID || appliedFocusID.current === focusShotID) return
    appliedFocusID.current = focusShotID
    const shot = plan.shots.find((candidate) => candidate.id === focusShotID)
    if (!shot) return
    document.getElementById(`planning-shot-${shot.id}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
    setEditing(shot)
    setHighlightID(shot.id)
  }, [focusShotID, plan.shots])

  useEffect(() => {
    if (!highlightID) return
    const timer = window.setTimeout(() => setHighlightID(null), 2400)
    return () => window.clearTimeout(timer)
  }, [highlightID])

  async function reorder(index: number, direction: -1 | 1) {
    const target = index + direction
    if (target < 0 || target >= plan.shots.length) return
    const ordered = plan.shots.map((shot) => shot.id)
    ;[ordered[index], ordered[target]] = [ordered[target]!, ordered[index]!]
    setError(null)
    try {
      await runCommand({ expected_revision: plan.revision, operation: 'reorder_shots', ordered_shot_ids: ordered }, 'reorder-shots')
    } catch (cause) {
      setError(planningErrorMessage(cause, '镜头顺序保存失败'))
    }
  }

  async function remove(shot: ShootPlanShot) {
    const hasHistory = shotHasExecutionHistory(plan, shot.id)
    const message = hasHistory
      ? '这条镜头已有执行记录。移除只会把它移出当前镜头表，历史记录和完成快照都会保留。确认移除吗？'
      : '移除后镜头不再出现在当前镜头表中。确认移除吗？'
    if (!globalThis.confirm(message)) return
    setError(null)
    try {
      await runCommand({ expected_revision: plan.revision, operation: 'remove_shot', shot_id: shot.id, acknowledge_execution_history: hasHistory }, 'remove-shot')
    } catch (cause) {
      setError(planningErrorMessage(cause, '移除镜头失败'))
    }
  }

  async function toggleReadiness(shot: ShootPlanShot, readinessID: string, linked: boolean) {
    setError(null)
    const command: PlanCommand = linked
      ? { expected_revision: plan.revision, operation: 'unlink_readiness', shot_id: shot.id, readiness_id: readinessID }
      : { expected_revision: plan.revision, operation: 'link_readiness', shot_id: shot.id, readiness_id: readinessID }
    try {
      await runCommand(command, linked ? 'unlink-readiness' : 'link-readiness')
    } catch (cause) {
      setError(planningErrorMessage(cause, '镜头准备项关联保存失败'))
    }
  }

  return (
    <section className="planning-section">
      <div className="planning-section-head">
        <div><p className="planning-eyebrow">镜头表</p><h2>顺序即现场执行顺序</h2><p>镜头结构与现场执行结果分开保存。</p></div>
        <button className="btn btn-primary" type="button" disabled={busy || readonly} onClick={() => setEditing('new')}>＋ 新增镜头</button>
      </div>
      {error && <p className="planning-inline-error" role="alert">{error}</p>}
      {plan.shots.length === 0 ? (
        <div className="planning-empty-card">还没有镜头。新增第一条镜头后，才能进入完整的现场执行流程。</div>
      ) : (
        <div className="planning-shot-list">
          {plan.shots.map((shot, index) => (
            <article className={`card planning-shot-card${highlightID === shot.id ? ' is-focused' : ''}`} key={shot.id} id={`planning-shot-${shot.id}`}>
              <div className="planning-shot-position">{shot.position}</div>
              <div className="planning-shot-body">
                <div className="planning-title-line"><h3>{shot.title}</h3><OutcomeBadge shot={shot} /></div>
                <div className="planning-shot-copy">
                  {shot.scene && <p><strong>场景</strong>{shot.scene}</p>}
                  {shot.action && <p><strong>动作</strong>{shot.action}</p>}
                  {shot.expression && <p><strong>表情</strong>{shot.expression}</p>}
                  {shot.composition && <p><strong>构图</strong>{shot.composition}</p>}
                  {shot.lighting_text && <p><strong>打光</strong>{shot.lighting_text}</p>}
                  {shot.notes && <p><strong>备注</strong>{shot.notes}</p>}
                </div>
                <div className="planning-meta">
                  {shot.framing_tag && <span>景别 {shot.framing_tag}</span>}
                  {shot.shot_type_tag && <span>类型 {shot.shot_type_tag}</span>}
                  <span>执行版本 {shot.execution_revision}</span>
                </div>
                {plan.readiness_items.length > 0 && (
                  <div className="planning-links">
                    <span>关联准备项</span>
                    {plan.readiness_items.map((item) => {
                      const linked = shot.readiness_item_ids.includes(item.id)
                      return <button type="button" className={`chip${linked ? ' active' : ''}`} disabled={busy || readonly} key={item.id} onClick={() => void toggleReadiness(shot, item.id, linked)}>{item.title}</button>
                    })}
                  </div>
                )}
              </div>
              <div className="planning-card-actions">
                <button className="btn btn-sm" type="button" disabled={busy || readonly || index === 0} aria-label={`上移镜头 ${shot.title}`} onClick={() => void reorder(index, -1)}>↑</button>
                <button className="btn btn-sm" type="button" disabled={busy || readonly || index === plan.shots.length - 1} aria-label={`下移镜头 ${shot.title}`} onClick={() => void reorder(index, 1)}>↓</button>
                <button className="btn btn-sm" type="button" disabled={busy || readonly} onClick={() => setEditing(shot)}>编辑</button>
                <button className="btn btn-danger-ghost btn-sm" type="button" disabled={busy || readonly} onClick={() => void remove(shot)}>移除</button>
              </div>
            </article>
          ))}
        </div>
      )}
      {editing && <ShotDialog plan={plan} shot={editing === 'new' ? null : editing} busy={busy} runCommand={runCommand} onClose={() => setEditing(null)} />}
    </section>
  )
}

function OutcomeBadge({ shot }: { shot: ShootPlanShot }) {
  const outcome = shot.current_outcome
  if (!outcome) return <span className="badge badge-muted">待执行</span>
  if (outcome.result === 'captured') return <span className="badge badge-success">已捕获</span>
  return <span className="badge badge-warning">已跳过 · {outcome.skip_reason ?? '未说明'}</span>
}

function ShotDialog({ plan, shot, busy, runCommand, onClose }: { plan: ShootPlanDetail; shot: ShootPlanShot | null; busy: boolean; runCommand: CommandRunner; onClose: () => void }) {
  const [title, setTitle] = useState(shot?.title ?? '')
  const [scene, setScene] = useState(shot?.scene ?? '')
  const [action, setAction] = useState(shot?.action ?? '')
  const [expression, setExpression] = useState(shot?.expression ?? '')
  const [composition, setComposition] = useState(shot?.composition ?? '')
  const [lighting, setLighting] = useState(shot?.lighting_text ?? '')
  const [notes, setNotes] = useState(shot?.notes ?? '')
  const [framing, setFraming] = useState(shot?.framing_tag ?? '')
  const [lightingDirection, setLightingDirection] = useState(shot?.lighting_direction_tag ?? '')
  const [lightingQuality, setLightingQuality] = useState(shot?.lighting_quality_tag ?? '')
  const [palette, setPalette] = useState(shot?.palette_tag ?? '')
  const [shotType, setShotType] = useState(shot?.shot_type_tag ?? '')
  const [error, setError] = useState<string | null>(null)

  async function save(event: FormEvent) {
    event.preventDefault()
    if (!title.trim()) { setError('镜头标题不能为空。'); return }
    setError(null)
    const command = {
      expected_revision: plan.revision,
      operation: 'upsert_shot',
      ...(shot ? { shot_id: shot.id } : {}),
      shot: {
        title: title.trim(), scene: nullable(scene), action: nullable(action), expression: nullable(expression),
        composition: nullable(composition), lighting_text: nullable(lighting), notes: nullable(notes),
        framing_tag: nullable(framing), lighting_direction_tag: nullable(lightingDirection),
        lighting_quality_tag: nullable(lightingQuality), palette_tag: nullable(palette), shot_type_tag: nullable(shotType),
      },
    } as PlanCommand
    try {
      await runCommand(command, shot ? 'update-shot' : 'create-shot')
      onClose()
    } catch (cause) {
      setError(planningErrorMessage(cause, '镜头保存失败'))
    }
  }

  return (
    <div className="overlay open" onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) onClose() }}>
      <form className="dialog planning-shot-dialog" role="dialog" aria-modal="true" aria-labelledby="shotDialogTitle" onSubmit={save}>
        <h2 id="shotDialogTitle">{shot ? '编辑镜头' : '新增镜头'}</h2>
        <p className="dialog-sub">只编辑镜头结构；现场结果在 Run Mode 或执行历史中记录。</p>
        <label className="field"><span>镜头标题</span><input className="input" autoFocus value={title} maxLength={160} onChange={(event) => setTitle(event.target.value)} /></label>
        <div className="field-row"><TextArea label="场景" value={scene} setValue={setScene} /><TextArea label="动作" value={action} setValue={setAction} /></div>
        <div className="field-row"><TextArea label="表情" value={expression} setValue={setExpression} /><TextArea label="构图" value={composition} setValue={setComposition} /></div>
        <TextArea label="打光" value={lighting} setValue={setLighting} />
        <TextArea label="备注" value={notes} setValue={setNotes} maxLength={2000} />
        <div className="planning-taxonomy-grid">
          <EnumSelect label="景别" value={framing} setValue={setFraming} values={['extreme_closeup','closeup','medium_closeup','medium','full','wide','extreme_wide','other']} />
          <EnumSelect label="光线方向" value={lightingDirection} setValue={setLightingDirection} values={['front','side','back','top','bottom','mixed','natural','other']} />
          <EnumSelect label="光质" value={lightingQuality} setValue={setLightingQuality} values={['hard','soft','mixed','natural','other']} />
          <EnumSelect label="色调" value={palette} setValue={setPalette} values={['warm','cool','neutral','monochrome','high_saturation','low_saturation','mixed','other']} />
          <EnumSelect label="镜头类型" value={shotType} setValue={setShotType} values={['portrait','action','interaction','environment','detail','silhouette','narrative','other']} />
        </div>
        {error && <p className="planning-inline-error" role="alert">{error}</p>}
        <div className="dialog-actions"><button className="btn" type="button" disabled={busy} onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy}>{busy ? '正在保存…' : '保存镜头'}</button></div>
      </form>
    </div>
  )
}

function TextArea({ label, value, setValue, maxLength = 1000 }: { label: string; value: string; setValue: (value: string) => void; maxLength?: number }) {
  return <label className="field"><span>{label}</span><textarea className="input" value={value} maxLength={maxLength} onChange={(event) => setValue(event.target.value)} /></label>
}

function EnumSelect({ label, value, setValue, values }: { label: string; value: string; setValue: (value: string) => void; values: string[] }) {
  return <label className="field"><span>{label}</span><select className="input" value={value} onChange={(event) => setValue(event.target.value)}><option value="">未设置</option>{values.map((item) => <option key={item} value={item}>{item}</option>)}</select></label>
}

function nullable(value: string): string | null {
  const trimmed = value.trim()
  return trimmed || null
}
