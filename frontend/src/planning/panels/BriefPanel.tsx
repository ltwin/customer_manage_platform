import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { Settings2 } from 'lucide-react'
import ConfirmDialog from '../../components/ConfirmDialog'
import { useShell } from '../../components/shellContext'
import { instantToLocalDateTime } from '../../components/schedule/timezone'
import { planningErrorMessage } from '../presentation'
import type { CommandRunner } from '../ShootPlanWorkspacePage'
import type { PlanCommand, ShootPlanDetail } from '../api'
import CrmLinkPanel from './CrmLinkPanel'
import {
  buildExecutionWindow,
  inferExecutionWindowMode,
  type ExecutionWindowMode,
} from '../executionWindow'

export default function BriefPanel({ plan, busy, runCommand }: { plan: ShootPlanDetail; busy: boolean; runCommand: CommandRunner }) {
  const { timezone: accountTimezone } = useShell()
  return (
    <>
      {/* 整行提示块，必须放在两列网格之外：planning-panel-grid 按行自动排列，
          混入网格子元素会把 BriefEditor 挤进窄侧栏并产生大段空白。 */}
      <details className="planning-flow-help">
        <summary>进度怎么流转</summary>
        <ol>
          <li>草稿 → 已就绪：只检查你显式标为「必需」的准备项</li>
          <li>已就绪 → 拍摄中：进入 Run Mode 或手动开始</li>
          <li>拍摄中 → 已完成：每个镜头要么已捕获、要么已跳过（跳过须写原因）</li>
          <li>已完成后想再改，需要先「重新打开」</li>
        </ol>
        <p>状态只往前推进；被拒时页面会告诉你差什么。</p>
      </details>
      <div className="planning-panel-grid">
      <BriefEditor plan={plan} busy={busy} runCommand={runCommand} />
      <div className="planning-side-stack">
        <ScaleEditor plan={plan} busy={busy} runCommand={runCommand} />
        <WindowEditor plan={plan} busy={busy} runCommand={runCommand} accountTimezone={accountTimezone} />
        <CrmLinkPanel plan={plan} busy={busy} runCommand={runCommand} />
      </div>
      </div>
    </>
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
        <label className="field"><span>计划造型数</span><input className="input" type="number" min="1" max="999" value={looks} disabled={plan.status === 'archived'} onChange={(event) => { setLooks(event.target.value); setDirty(true) }} placeholder="未知则留空" /><small>签发 proposal/full 后可在客户页显示；未知时留空并隐藏。</small></label>
        <label className="field"><span>计划场景数</span><input className="input" type="number" min="1" max="999" value={scenes} disabled={plan.status === 'archived'} onChange={(event) => { setScenes(event.target.value); setDirty(true) }} placeholder="未知则留空" /><small>这是计划规模，不从付费场地数量推断。</small></label>
      </div>
      <div className="planning-readonly-stat">当前镜头数 <strong>{plan.public_scale.planned_shot_count}</strong></div>
      {error && <p className="planning-inline-error" role="alert">{error}</p>}
    </form>
  )
}

// 时间窗「来源」三态说明：档期投影跟随 / 手动维护 / 未设置。投影的采纳与抑制
// 操作仍在 CRM 关联卡（同一语义的执行入口），这里给出一致的来源状态提示。
function windowSourceNote(plan: ShootPlanDetail, hasWindow: boolean): string {
  const projection = plan.crm?.schedule_projection
  if (!hasWindow) return '尚未设置：可在此手动填写，或关联订单档期后从 CRM 卡采纳档期投影。'
  if (projection?.status === 'active_applied') return '来源：跟随订单档期投影（在 CRM 关联卡管理抑制或重新采纳）。'
  if (projection?.status === 'active_unapplied') return '来源：手动维护；存在未采纳的档期投影，可在 CRM 关联卡查看。'
  return '来源：手动维护；未关联可用的订单档期投影。'
}

function WindowEditor({ plan, busy, runCommand, accountTimezone }: { plan: ShootPlanDetail; busy: boolean; runCommand: CommandRunner; accountTimezone: string | null }) {
  const window = plan.execution_window
  const initialTimezone = window?.timezone ?? accountTimezone ?? 'Asia/Shanghai'
  const initialMode = inferExecutionWindowMode(window)
  const [startsAt, setStartsAt] = useState(toLocalInput(window?.starts_at, initialTimezone))
  const [endsAt, setEndsAt] = useState(toLocalInput(window?.ends_at, initialTimezone))
  const [customLiveStartsAt, setCustomLiveStartsAt] = useState(initialMode === 'custom' ? toLocalInput(window?.live_window_starts_at, initialTimezone) : '')
  const [customLiveEndsAt, setCustomLiveEndsAt] = useState(initialMode === 'custom' ? toLocalInput(window?.live_window_ends_at, initialTimezone) : '')
  const [timezone, setTimezone] = useState(initialTimezone)
  const [mode, setMode] = useState<ExecutionWindowMode>(initialMode)
  const [error, setError] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const [confirmingClear, setConfirmingClear] = useState(false)
  const planID = useRef(plan.id)
  useEffect(() => {
    const planChanged = planID.current !== plan.id
    planID.current = plan.id
    if (dirty && !planChanged) return
    const nextTimezone = plan.execution_window?.timezone ?? accountTimezone ?? 'Asia/Shanghai'
    const nextMode = inferExecutionWindowMode(plan.execution_window)
    setStartsAt(toLocalInput(plan.execution_window?.starts_at, nextTimezone))
    setEndsAt(toLocalInput(plan.execution_window?.ends_at, nextTimezone))
    setCustomLiveStartsAt(nextMode === 'custom' ? toLocalInput(plan.execution_window?.live_window_starts_at, nextTimezone) : '')
    setCustomLiveEndsAt(nextMode === 'custom' ? toLocalInput(plan.execution_window?.live_window_ends_at, nextTimezone) : '')
    setTimezone(nextTimezone)
    setMode(nextMode)
    setDirty(false)
  }, [accountTimezone, dirty, plan])

  async function save(event: FormEvent) {
    event.preventDefault()
    setError(null)
    try {
      const resolved = buildExecutionWindow({
        startsAt,
        endsAt,
        timezone,
        mode,
        customLiveStartsAt,
        customLiveEndsAt,
      })
      await runCommand({
        expected_revision: plan.revision,
        operation: 'set_execution_window',
        starts_at: resolved.startsAt,
        ends_at: resolved.endsAt,
        timezone: resolved.timezone,
        live_window_starts_at: resolved.liveWindowStartsAt,
        live_window_ends_at: resolved.liveWindowEndsAt,
      }, 'execution-window')
      setDirty(false)
    } catch (cause) {
      setError(planningErrorMessage(cause, '拍摄时间保存失败'))
    }
  }

  return (
    <form className="card planning-panel" onSubmit={save}>
      <div className="planning-panel-head"><div><h2>拍摄时间</h2><p>用于拍摄安排和现场记录。</p></div><span className="badge badge-muted">{window ? `第 ${window.revision} 版` : '未设置'}</span></div><p className="planning-field-help planning-window-source">{windowSourceNote(plan, window != null)}</p>
      <div className="field-row">
        <label className="field"><span>拍摄开始</span><input className="input" type="datetime-local" value={startsAt} disabled={plan.status === 'archived'} onChange={(event) => { setStartsAt(event.target.value); setDirty(true) }} /></label>
        <label className="field"><span>拍摄结束</span><input className="input" type="datetime-local" value={endsAt} disabled={plan.status === 'archived'} onChange={(event) => { setEndsAt(event.target.value); setDirty(true) }} /></label>
      </div>
      <p className="planning-timezone-summary">时间按 {timezoneDisplayName(timezone)} 记录</p>
      <details className="planning-advanced-settings">
        <summary><Settings2 size={16} aria-hidden="true" /><span>高级设置</span><small>{mode === 'automatic' ? '现场范围自动设置' : '现场范围已自定义'}</small></summary>
        <div className="planning-advanced-body">
          <div className="field">
            <span>现场识别范围</span>
            <div className="planning-segmented" role="group" aria-label="现场识别范围设置方式">
              <button type="button" className={mode === 'automatic' ? 'is-active' : ''} aria-pressed={mode === 'automatic'} disabled={plan.status === 'archived'} onClick={() => { setMode('automatic'); setDirty(true) }}>自动</button>
              <button type="button" className={mode === 'custom' ? 'is-active' : ''} aria-pressed={mode === 'custom'} disabled={plan.status === 'archived'} onClick={() => {
                const defaults = automaticLiveInputs(startsAt, endsAt, timezone)
                setMode('custom')
                if (!customLiveStartsAt && defaults) setCustomLiveStartsAt(defaults.startsAt)
                if (!customLiveEndsAt && defaults) setCustomLiveEndsAt(defaults.endsAt)
                setDirty(true)
              }}>自定义</button>
            </div>
            <small className="planning-field-help">{mode === 'automatic' ? automaticRangeDescription(startsAt, endsAt, timezone) : '自定义范围必须包含完整的拍摄时间。'}</small>
          </div>
          {mode === 'custom' && <div className="field-row">
            <label className="field"><span>现场开始</span><input className="input" type="datetime-local" value={customLiveStartsAt} disabled={plan.status === 'archived'} onChange={(event) => { setCustomLiveStartsAt(event.target.value); setDirty(true) }} /></label>
            <label className="field"><span>现场结束</span><input className="input" type="datetime-local" value={customLiveEndsAt} disabled={plan.status === 'archived'} onChange={(event) => { setCustomLiveEndsAt(event.target.value); setDirty(true) }} /></label>
          </div>}
          <label className="field"><span>拍摄地点时区</span><input className="input" list="planning-timezones" value={timezone} maxLength={255} disabled={plan.status === 'archived'} onChange={(event) => { setTimezone(event.target.value); setDirty(true) }} /><small className="planning-field-help">默认使用账号时区；跨时区拍摄时可修改。</small></label>
          <datalist id="planning-timezones">
            <option value="Asia/Shanghai">中国标准时间</option>
            <option value="Asia/Hong_Kong">香港时间</option>
            <option value="Asia/Tokyo">日本标准时间</option>
            <option value="Asia/Seoul">韩国标准时间</option>
            <option value="Europe/London">伦敦时间</option>
            <option value="America/Los_Angeles">洛杉矶时间</option>
            <option value="America/New_York">纽约时间</option>
          </datalist>
        </div>
      </details>
      <div className="planning-form-actions">
        <button className="btn btn-primary btn-sm" disabled={busy || plan.status === 'archived'}>保存拍摄时间</button>
        {window && <button className="btn btn-danger-ghost btn-sm" type="button" disabled={busy || plan.status === 'archived'} onClick={() => setConfirmingClear(true)}>清除拍摄时间</button>}
      </div>
      {error && <p className="planning-inline-error" role="alert">{error}</p>}
      {confirmingClear && (
        <ConfirmDialog
          title="清除拍摄时间？"
          body="清除拍摄时间后，后续现场记录将不再区分现场完成与事后补记。"
          confirmLabel="清除拍摄时间"
          danger
          busy={busy}
          onConfirm={() => {
            setConfirmingClear(false)
            setError(null)
            void runCommand({ expected_revision: plan.revision, operation: 'clear_execution_window' }, 'clear-window')
              .then(() => setDirty(false))
              .catch((cause) => setError(planningErrorMessage(cause, '清除拍摄时间失败')))
          }}
          onCancel={() => setConfirmingClear(false)}
        />
      )}
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

function automaticLiveInputs(startsAt: string, endsAt: string, timezone: string): { startsAt: string; endsAt: string } | null {
  try {
    const resolved = buildExecutionWindow({ startsAt, endsAt, timezone, mode: 'automatic', customLiveStartsAt: '', customLiveEndsAt: '' })
    return {
      startsAt: toLocalInput(resolved.liveWindowStartsAt, resolved.timezone),
      endsAt: toLocalInput(resolved.liveWindowEndsAt, resolved.timezone),
    }
  } catch {
    return null
  }
}

function automaticRangeDescription(startsAt: string, endsAt: string, timezone: string): string {
  const range = automaticLiveInputs(startsAt, endsAt, timezone)
  if (!range) return '保存时按拍摄开始前 2 小时至结束后 2 小时自动设置。'
  return `现场时段将自动设为 ${formatLocalInput(range.startsAt)} 至 ${formatLocalInput(range.endsAt)}。`
}

function formatLocalInput(value: string): string {
  const [date = '', time = ''] = value.split('T')
  return `${date.replace(/-/g, '/')} ${time}`
}

function timezoneDisplayName(timezone: string): string {
  const normalized = timezone.trim()
  if (!normalized) return '未设置的时区'
  try {
    const date = new Date()
    const name = new Intl.DateTimeFormat('zh-CN', { timeZone: normalized, timeZoneName: 'long' })
      .formatToParts(date).find((part) => part.type === 'timeZoneName')?.value
    const offset = new Intl.DateTimeFormat('zh-CN', { timeZone: normalized, timeZoneName: 'longOffset' })
      .formatToParts(date).find((part) => part.type === 'timeZoneName')?.value.replace('GMT', 'UTC')
    return name && offset ? `${name}（${offset}）` : normalized
  } catch {
    return normalized
  }
}
