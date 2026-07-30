import { useEffect, useState } from 'react'
import { BellRing } from 'lucide-react'
import EmptyState from './EmptyState'
import {
  ApiError,
  createReminder,
  dismissReminder,
  listReminders,
  markReminderDone,
} from '../api/client'
import type { Reminder } from '../api/client'
import { useShell } from './shellContext'

const typeLabels: Record<Reminder['type'], string> = {
  birthday: '生日',
  follow_up: '回访',
  churn: '流失',
  custom: '自定义',
}

interface Props {
  customerId: string
  onUnauthorized: () => void
  onChanged?: () => void
}

export default function CustomerRemindersPanel({ customerId, onUnauthorized, onChanged }: Props) {
  const { notify } = useShell()
  const [items, setItems] = useState<Reminder[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionId, setActionId] = useState<string | null>(null)
  const [content, setContent] = useState('')
  const [dueDate, setDueDate] = useState('')
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [tick, setTick] = useState(0)

  useEffect(() => {
    let active = true
    setLoading(true)
    setError(null)
    listReminders({ customerId, page: 1, pageSize: 50 })
      .then((res) => {
        if (!active) return
        setItems(res.items)
        setTotal(res.total)
      })
      .catch((err: unknown) => {
        if (!active) return
        if (err instanceof ApiError && err.status === 401) {
          onUnauthorized()
          return
        }
        setError(err instanceof Error ? err.message : '加载提醒失败')
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [customerId, onUnauthorized, tick])

  async function act(id: string, kind: 'done' | 'dismiss') {
    setActionId(id)
    try {
      if (kind === 'done') await markReminderDone(id)
      else await dismissReminder(id)
      notify(kind === 'done' ? '已标记完成' : '已忽略')
      setTick((n) => n + 1)
      onChanged?.()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized()
        return
      }
      notify(err instanceof Error ? err.message : '操作失败')
    } finally {
      setActionId(null)
    }
  }

  async function onCreate(e: React.FormEvent) {
    e.preventDefault()
    setFormError(null)
    if (!content.trim() || !dueDate) {
      setFormError('请填写内容与到期日')
      return
    }
    setSaving(true)
    try {
      await createReminder({
        type: 'custom',
        customer_id: customerId,
        content: content.trim(),
        due_date: dueDate,
      })
      setContent('')
      setDueDate('')
      notify('提醒已创建')
      setTick((n) => n + 1)
      onChanged?.()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized()
        return
      }
      setFormError(err instanceof Error ? err.message : '创建失败')
    } finally {
      setSaving(false)
    }
  }

  if (loading) return <div className="empty inline-empty">加载中…</div>
  if (error) {
    return (
      <div className="form-error" role="alert">
        {error}
        <button className="btn btn-sm" type="button" onClick={() => setTick((n) => n + 1)}>
          重试
        </button>
      </div>
    )
  }

  return (
    <div className="reminder-panel">
      <form className="inline-form" onSubmit={onCreate}>
        <input
          type="date"
          value={dueDate}
          onChange={(e) => setDueDate(e.target.value)}
          aria-label="到期日"
        />
        <input
          type="text"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          placeholder="自定义提醒内容"
          aria-label="提醒内容"
        />
        <button className="btn btn-primary btn-sm" type="submit" disabled={saving}>
          {saving ? '提交中' : '新增'}
        </button>
      </form>
      {formError && (
        <div className="form-error" role="alert">
          {formError}
        </div>
      )}

      {items.length === 0 ? (
        <EmptyState icon={BellRing} title="暂无提醒" hint="生日与回访会自动生成，也可手动添加" inline />
      ) : (
        <ul className="reminder-list">
          {items.map((item) => (
            <li key={item.id} className="reminder-row">
              <div>
                <span className={`badge badge-muted`}>{typeLabels[item.type]}</span>{' '}
                <span className="num">{item.due_date}</span>
                <div>{item.content}</div>
                <div className="sub">状态 · {item.status}</div>
              </div>
              {item.status === 'pending' && (
                <div className="row-actions">
                  <button
                    className="btn btn-sm"
                    type="button"
                    disabled={actionId === item.id}
                    onClick={() => act(item.id, 'done')}
                  >
                    完成
                  </button>
                  <button
                    className="btn btn-sm"
                    type="button"
                    disabled={actionId === item.id}
                    onClick={() => act(item.id, 'dismiss')}
                  >
                    忽略
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      <div className="sub">
        共 {total} 条{total > items.length ? ` · 当前仅显示前 ${items.length} 条` : ''}
      </div>
    </div>
  )
}
