import { useCallback, useEffect, useState } from 'react'
import { ApiError, dismissReminder, markReminderDone } from '../../api/client'
import { getShootPlanAssignmentReminders } from '../api'
import type { PlanAssignmentReminderGroup, PlanAssignmentReminderView } from '../api'
import { planningErrorMessage } from '../presentation'

const deliveryModeLabel: Record<PlanAssignmentReminderGroup['delivery_modes'][number], string> = {
  in_app: '站内',
  telegram_digest_if_bound: 'Telegram digest（若已绑定）',
}

const statusLabel: Record<PlanAssignmentReminderGroup['reminder_status'], string> = {
  pending: '待核对',
  done: '已完成',
  dismissed: '已忽略',
}

type LoadState =
  | { kind: 'loading' }
  | { kind: 'ready'; view: PlanAssignmentReminderView }
  | { kind: 'error'; message: string }

export default function AssignmentReminderCard({ planID }: { planID: string }) {
  const [state, setState] = useState<LoadState>({ kind: 'loading' })
  const [actionID, setActionID] = useState<string | null>(null)
  const [tick, setTick] = useState(0)

  const reload = useCallback(() => setTick((value) => value + 1), [])

  useEffect(() => {
    let active = true
    setState({ kind: 'loading' })
    getShootPlanAssignmentReminders(planID)
      .then((view) => {
        if (active) setState({ kind: 'ready', view })
      })
      .catch((cause: unknown) => {
        if (!active) return
        setState({
          kind: 'error',
          message: planningErrorMessage(cause, '认领项检查提醒加载失败'),
        })
      })
    return () => {
      active = false
    }
  }, [planID, tick])

  async function act(reminderID: string, kind: 'done' | 'dismiss') {
    setActionID(reminderID)
    try {
      if (kind === 'done') await markReminderDone(reminderID)
      else await dismissReminder(reminderID)
      reload()
    } catch (cause) {
      const message =
        cause instanceof ApiError
          ? cause.message
          : planningErrorMessage(cause, kind === 'done' ? '标记完成失败' : '忽略失败')
      setState({ kind: 'error', message })
    } finally {
      setActionID(null)
    }
  }

  return (
    <section className="card planning-panel" data-testid="assignment-reminder-card" aria-labelledby="assignment-reminder-title">
      <div className="planning-panel-head">
        <div>
          <h2 id="assignment-reminder-title">认领项检查提醒</h2>
          <p>提醒收件人固定为账号所有者；系统不会向客户发任何消息。</p>
        </div>
      </div>

      {state.kind === 'loading' && (
        <p className="muted-text" role="status">正在加载认领项检查提醒…</p>
      )}

      {state.kind === 'error' && (
        <div className="planning-inline-error" role="alert">
          <p>{state.message}</p>
          <button className="btn btn-sm" type="button" onClick={reload}>重试</button>
        </div>
      )}

      {state.kind === 'ready' && (
        <AssignmentReminderBody
          view={state.view}
          actionID={actionID}
          onDone={(id) => void act(id, 'done')}
          onDismiss={(id) => void act(id, 'dismiss')}
        />
      )}
    </section>
  )
}

function AssignmentReminderBody({
  view,
  actionID,
  onDone,
  onDismiss,
}: {
  view: PlanAssignmentReminderView
  actionID: string | null
  onDone: (reminderID: string) => void
  onDismiss: (reminderID: string) => void
}) {
  const unscheduled = view.unscheduled_source_count > 0
  const empty = view.groups.length === 0 && !unscheduled

  if (empty) {
    return (
      <div className="planning-empty-card" role="status">
        当前没有认领项检查提醒。正式认领准备项后，会在这里出现核对提醒。
      </div>
    )
  }

  return (
    <div className="planning-assignment-reminder-stack">
      {unscheduled && view.groups.length === 0 && (
        <div className="planning-empty-card" role="status">
          等待未来拍摄档期。已有认领项，但尚未关联可计算到期日的未来拍摄档期，因此暂无提醒日期。
        </div>
      )}
      {unscheduled && view.groups.length > 0 && (
        <p className="hint" role="status">
          另有 {view.unscheduled_source_count} 项认领等待未来拍摄档期，暂无提醒日期。
        </p>
      )}
      {view.groups.map((group) => (
        <article className="planning-assignment-reminder-group" key={group.group_id}>
          <dl className="planning-crm-kv">
            <div>
              <dt>提醒内容</dt>
              <dd>{group.content}</dd>
            </div>
            <div>
              <dt>触发时间</dt>
              <dd>
                <span className="num">{group.due_date}</span>
                {' · '}
                {statusLabel[group.reminder_status]}
              </dd>
            </div>
            <div>
              <dt>收件人</dt>
              <dd>账号所有者 · {group.delivery_modes.map((mode) => deliveryModeLabel[mode]).join(' + ')}</dd>
            </div>
            <div>
              <dt>聚合口径</dt>
              <dd>同策划 / 同档期 / 同到期日合并为一张核对清单（{group.item_count} 项）</dd>
            </div>
          </dl>
          <p className="hint">
            匿名昵称永不会成为投递地址。档期改期、订单取消、策划归档或认领撤销都会自动重算或撤销该提醒。
          </p>
          {group.reminder_status === 'pending' && (
            <div className="planning-card-actions" style={{ marginTop: 12 }}>
              <button
                className="btn btn-sm"
                type="button"
                disabled={actionID === group.reminder_id}
                onClick={() => onDone(group.reminder_id)}
              >
                完成
              </button>
              <button
                className="btn btn-sm btn-ghost"
                type="button"
                disabled={actionID === group.reminder_id}
                onClick={() => onDismiss(group.reminder_id)}
              >
                忽略
              </button>
            </div>
          )}
        </article>
      ))}
    </div>
  )
}
