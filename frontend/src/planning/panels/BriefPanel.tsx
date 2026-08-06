import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { planningErrorMessage } from '../presentation'
import type { CommandRunner } from '../ShootPlanWorkspacePage'
import type { PlanCommand, ShootPlanDetail } from '../api'
import { instantToLocalDateTime, resolveLocalDateTime } from '../../components/schedule/timezone'

export default function BriefPanel({ plan, busy, runCommand }: { plan: ShootPlanDetail; busy: boolean; runCommand: CommandRunner }) {
  return (
    <div className="planning-panel-grid">
      <BriefEditor plan={plan} busy={busy} runCommand={runCommand} />
      <div className="planning-side-stack">
        <ScaleEditor plan={plan} busy={busy} runCommand={runCommand} />
        <WindowEditor plan={plan} busy={busy} runCommand={runCommand} />
      </div>
    </div>
  )
}

function BriefEditor({ plan, busy, runCommand }: { plan: ShootPlanDetail; busy: boolean; runCommand: CommandRunner }) {
  const [title, setTitle] = useState(plan.title)
  const [subject, setSubject] = useState(plan.subject)
  const [workTitle, setWorkTitle] = useState(plan.creative_brief.work_title ?? '')
  const [characterName, setCharacterName] = useState(plan.creative_brief.character_name ?? '')
  const [themeStatement, setThemeStatement] = useState(plan.creative_brief.theme_statement ?? '')
  const [mood, setMood] = useState(plan.creative_brief.mood ?? '')
  const [keywords, setKeywords] = useState((plan.creative_brief.visual_keywords ?? []).join('、'))
  const [error, setError] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const planID = useRef(plan.id)

  useEffect(() => {
    const planChanged = planID.current !== plan.id
    planID.current = plan.id
    if (dirty && !planChanged) return
    setTitle(plan.title)
    setSubject(plan.subject)
    setWorkTitle(plan.creative_brief.work_title ?? '')
    setCharacterName(plan.creative_brief.character_name ?? '')
    setThemeStatement(plan.creative_brief.theme_statement ?? '')
    setMood(plan.creative_brief.mood ?? '')
    setKeywords((plan.creative_brief.visual_keywords ?? []).join('、'))
    setDirty(false)
  }, [dirty, plan])

  async function save(event: FormEvent) {
    event.preventDefault()
    setError(null)
    const visualKeywords = keywords.split(/[、,，\n]/).map((value) => value.trim()).filter(Boolean)
    const command: PlanCommand = {
      expected_revision: plan.revision,
      operation: 'update_brief',
      title: title.trim(),
      subject: subject.trim(),
      creative_brief: {
        work_title: nullableText(workTitle),
        character_name: nullableText(characterName),
        theme_statement: nullableText(themeStatement),
        mood: nullableText(mood),
        visual_keywords: visualKeywords,
      },
    }
    try {
      await runCommand(command, 'brief')
      setDirty(false)
    } catch (cause) {
      setError(planningErrorMessage(cause, '创作 brief 保存失败'))
    }
  }

  return (
    <form className="card planning-panel" onSubmit={save}>
      <div className="planning-panel-head">
        <div><h2>创作意图</h2><p>拍摄主体是自由文本；创作语言不依赖客户或订单。</p></div>
        <button className="btn btn-primary btn-sm" type="submit" disabled={busy || plan.status === 'archived'}>保存 brief</button>
      </div>
      <label className="field"><span>标题</span><input className="input" value={title} maxLength={160} disabled={plan.status === 'archived'} onChange={(event) => { setTitle(event.target.value); setDirty(true) }} /></label>
      <label className="field"><span>拍摄主体</span><input className="input" value={subject} maxLength={240} disabled={plan.status === 'archived'} onChange={(event) => { setSubject(event.target.value); setDirty(true) }} /></label>
      <div className="field-row">
        <label className="field"><span>作品</span><input className="input" value={workTitle} maxLength={120} disabled={plan.status === 'archived'} onChange={(event) => { setWorkTitle(event.target.value); setDirty(true) }} /></label>
        <label className="field"><span>角色</span><input className="input" value={characterName} maxLength={120} disabled={plan.status === 'archived'} onChange={(event) => { setCharacterName(event.target.value); setDirty(true) }} /></label>
      </div>
      <label className="field"><span>创作 brief</span><textarea className="input planning-brief-text" value={themeStatement} maxLength={2000} disabled={plan.status === 'archived'} onChange={(event) => { setThemeStatement(event.target.value); setDirty(true) }} placeholder="角色理解、画面叙事、需要避免的表达……" /></label>
      <label className="field"><span>情绪与氛围</span><textarea className="input" value={mood} maxLength={500} disabled={plan.status === 'archived'} onChange={(event) => { setMood(event.target.value); setDirty(true) }} /></label>
      <label className="field"><span>视觉关键词</span><input className="input" value={keywords} disabled={plan.status === 'archived'} onChange={(event) => { setKeywords(event.target.value); setDirty(true) }} placeholder="使用逗号或顿号分隔，最多 20 个" /></label>
      {error && <p className="planning-inline-error" role="alert">{error}</p>}
    </form>
  )
}

function ScaleEditor({ plan, busy, runCommand }: { plan: ShootPlanDetail; busy: boolean; runCommand: CommandRunner }) {
  const [looks, setLooks] = useState(plan.public_scale.planned_look_count?.toString() ?? '')
  const [scenes, setScenes] = useState(plan.public_scale.planned_scene_count?.toString() ?? '')
  const [error, setError] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const planID = useRef(plan.id)
  useEffect(() => {
    const planChanged = planID.current !== plan.id
    planID.current = plan.id
    if (dirty && !planChanged) return
    setLooks(plan.public_scale.planned_look_count?.toString() ?? '')
    setScenes(plan.public_scale.planned_scene_count?.toString() ?? '')
    setDirty(false)
  }, [dirty, plan])

  async function save(event: FormEvent) {
    event.preventDefault()
    setError(null)
    try {
      await runCommand({
        expected_revision: plan.revision,
        operation: 'set_public_scale',
        planned_look_count: nullableCount(looks),
        planned_scene_count: nullableCount(scenes),
      }, 'public-scale')
      setDirty(false)
    } catch (cause) {
      setError(planningErrorMessage(cause, '公开规模保存失败'))
    }
  }

  return (
    <form className="card planning-panel" onSubmit={save}>
      <div className="planning-panel-head"><div><h2>公开规模</h2><p>镜头数由当前镜头表自动计算。</p></div><button className="btn btn-sm" disabled={busy || plan.status === 'archived'}>保存</button></div>
      <div className="field-row">
        <label className="field"><span>计划造型数</span><input className="input" type="number" min="1" max="999" value={looks} disabled={plan.status === 'archived'} onChange={(event) => { setLooks(event.target.value); setDirty(true) }} placeholder="未知则留空" /></label>
        <label className="field"><span>计划场景数</span><input className="input" type="number" min="1" max="999" value={scenes} disabled={plan.status === 'archived'} onChange={(event) => { setScenes(event.target.value); setDirty(true) }} placeholder="未知则留空" /></label>
      </div>
      <div className="planning-readonly-stat">当前镜头数 <strong>{plan.public_scale.planned_shot_count}</strong></div>
      {error && <p className="planning-inline-error" role="alert">{error}</p>}
    </form>
  )
}

function WindowEditor({ plan, busy, runCommand }: { plan: ShootPlanDetail; busy: boolean; runCommand: CommandRunner }) {
  const window = plan.execution_window
  const initialTimezone = window?.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone ?? 'Asia/Shanghai'
  const [startsAt, setStartsAt] = useState(toLocalInput(window?.starts_at, initialTimezone))
  const [endsAt, setEndsAt] = useState(toLocalInput(window?.ends_at, initialTimezone))
  const [liveStartsAt, setLiveStartsAt] = useState(toLocalInput(window?.live_window_starts_at, initialTimezone))
  const [liveEndsAt, setLiveEndsAt] = useState(toLocalInput(window?.live_window_ends_at, initialTimezone))
  const [timezone, setTimezone] = useState(initialTimezone)
  const [error, setError] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const planID = useRef(plan.id)
  useEffect(() => {
    const planChanged = planID.current !== plan.id
    planID.current = plan.id
    if (dirty && !planChanged) return
    const nextTimezone = plan.execution_window?.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone ?? 'Asia/Shanghai'
    setStartsAt(toLocalInput(plan.execution_window?.starts_at, nextTimezone))
    setEndsAt(toLocalInput(plan.execution_window?.ends_at, nextTimezone))
    setLiveStartsAt(toLocalInput(plan.execution_window?.live_window_starts_at, nextTimezone))
    setLiveEndsAt(toLocalInput(plan.execution_window?.live_window_ends_at, nextTimezone))
    setTimezone(nextTimezone)
    setDirty(false)
  }, [dirty, plan])

  async function save(event: FormEvent) {
    event.preventDefault()
    setError(null)
    if (!startsAt || !endsAt || !liveStartsAt || !liveEndsAt || !timezone.trim()) {
      setError('开始、结束、现场窗口和时区都需要填写。')
      return
    }
    try {
      const normalizedTimezone = timezone.trim()
      await runCommand({
        expected_revision: plan.revision,
        operation: 'set_execution_window',
        starts_at: localInputToInstant(startsAt, normalizedTimezone),
        ends_at: localInputToInstant(endsAt, normalizedTimezone),
        timezone: normalizedTimezone,
        live_window_starts_at: localInputToInstant(liveStartsAt, normalizedTimezone),
        live_window_ends_at: localInputToInstant(liveEndsAt, normalizedTimezone),
      }, 'execution-window')
      setDirty(false)
    } catch (cause) {
      setError(planningErrorMessage(cause, '执行时间窗保存失败'))
    }
  }

  return (
    <form className="card planning-panel" onSubmit={save}>
      <div className="planning-panel-head"><div><h2>执行时间窗</h2><p>现场窗口用于服务端判定现场完成或事后补记。</p></div><span className="badge badge-muted">{window ? `第 ${window.revision} 版` : '未设置'}</span></div>
      <div className="field-row">
        <label className="field"><span>开始</span><input className="input" type="datetime-local" value={startsAt} disabled={plan.status === 'archived'} onChange={(event) => { setStartsAt(event.target.value); setDirty(true) }} /></label>
        <label className="field"><span>结束</span><input className="input" type="datetime-local" value={endsAt} disabled={plan.status === 'archived'} onChange={(event) => { setEndsAt(event.target.value); setDirty(true) }} /></label>
      </div>
      <div className="field-row">
        <label className="field"><span>现场窗口开始</span><input className="input" type="datetime-local" value={liveStartsAt} disabled={plan.status === 'archived'} onChange={(event) => { setLiveStartsAt(event.target.value); setDirty(true) }} /></label>
        <label className="field"><span>现场窗口结束</span><input className="input" type="datetime-local" value={liveEndsAt} disabled={plan.status === 'archived'} onChange={(event) => { setLiveEndsAt(event.target.value); setDirty(true) }} /></label>
      </div>
      <label className="field"><span>IANA 时区</span><input className="input" value={timezone} maxLength={255} disabled={plan.status === 'archived'} onChange={(event) => { setTimezone(event.target.value); setDirty(true) }} /></label>
      <div className="planning-form-actions">
        <button className="btn btn-primary btn-sm" disabled={busy || plan.status === 'archived'}>保存时间窗</button>
        {window && <button className="btn btn-danger-ghost btn-sm" type="button" disabled={busy || plan.status === 'archived'} onClick={() => {
          if (!globalThis.confirm('清除执行时间窗后，后续 Run Mode 记录将不再区分现场与补记。确认清除吗？')) return
          setError(null)
          void runCommand({ expected_revision: plan.revision, operation: 'clear_execution_window' }, 'clear-window')
            .then(() => setDirty(false))
            .catch((cause) => setError(planningErrorMessage(cause, '清除时间窗失败')))
        }}>清除时间窗</button>}
      </div>
      {error && <p className="planning-inline-error" role="alert">{error}</p>}
    </form>
  )
}

function nullableText(value: string): string | null {
  const trimmed = value.trim()
  return trimmed || null
}

function nullableCount(value: string): number | null {
  return value.trim() ? Number(value) : null
}

function toLocalInput(value: string | null | undefined, timezone: string): string {
  if (!value) return ''
  try {
    const local = instantToLocalDateTime(value, timezone)
    return `${local.date}T${local.time}`
  } catch {
    return ''
  }
}

function localInputToInstant(value: string, timezone: string): string {
  const [date, time] = value.split('T')
  if (!date || !time) throw new Error('日期或时间格式无效')
  return resolveLocalDateTime(date, time, timezone).instant
}
