import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  ApiError,
  dismissReminder,
  listReminders,
  markReminderDone,
  scanReminders,
} from '../api/client'
import type { Reminder, ReminderStatus, ScanRemindersResult } from '../api/client'
import { useShell } from '../components/shellContext'

const statusFilters: Array<[ReminderStatus | 'all', string]> = [
  ['pending', '待办'],
  ['done', '已完成'],
  ['dismissed', '已忽略'],
  ['all', '全部'],
]

const typeLabels: Record<Reminder['type'], string> = {
  birthday: '生日',
  follow_up: '回访',
  churn: '流失',
  custom: '自定义',
}

export default function RemindersPage() {
  const navigate = useNavigate()
  const { notify } = useShell()
  const [status, setStatus] = useState<ReminderStatus | 'all'>('pending')
  const [items, setItems] = useState<Reminder[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionId, setActionId] = useState<string | null>(null)
  const [scanning, setScanning] = useState(false)
  const [scanResult, setScanResult] = useState<ScanRemindersResult | null>(null)
  const [tick, setTick] = useState(0)

  const load = useCallback(() => {
    setLoading(true)
    setError(null)
    listReminders({
      status: status === 'all' ? undefined : status,
      page: 1,
      pageSize: 50,
    })
      .then((res) => {
        setItems(res.items)
        setTotal(res.total)
      })
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 401) {
          navigate('/login', { replace: true })
          return
        }
        setError(err instanceof Error ? err.message : '加载失败')
      })
      .finally(() => setLoading(false))
  }, [navigate, status])

  useEffect(() => {
    load()
  }, [load, tick])

  async function act(id: string, kind: 'done' | 'dismiss') {
    setActionId(id)
    try {
      if (kind === 'done') await markReminderDone(id)
      else await dismissReminder(id)
      notify(kind === 'done' ? '已标记完成' : '已忽略')
      setTick((n) => n + 1)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        navigate('/login', { replace: true })
        return
      }
      notify(err instanceof Error ? err.message : '操作失败')
    } finally {
      setActionId(null)
    }
  }

  async function onScan() {
    setScanning(true)
    setScanResult(null)
    try {
      const result = await scanReminders({})
      setScanResult(result)
      notify(`扫描完成：新建 ${result.created} / 跳过 ${result.skipped} / 自动忽略 ${result.auto_dismissed}`)
      setTick((n) => n + 1)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        navigate('/login', { replace: true })
        return
      }
      notify(err instanceof Error ? err.message : '扫描失败')
    } finally {
      setScanning(false)
    }
  }

  return (
    <>
      <header className="topbar">
        <div>
          <h1>提醒</h1>
          <div className="sub">生日 / 回访 / 流失 / 自定义 · 共 {total} 条</div>
        </div>
        <div className="topbar-actions">
          <button className="btn btn-primary" type="button" disabled={scanning} onClick={onScan}>
            {scanning ? '扫描中…' : '手动扫描'}
          </button>
        </div>
      </header>

      <main className="content">
        <div className="filter-row">
          {statusFilters.map(([key, label]) => (
            <button
              key={key}
              type="button"
              className={`chip${status === key ? ' active' : ''}`}
              onClick={() => setStatus(key)}
            >
              {label}
            </button>
          ))}
        </div>

        {scanResult && (
          <div className="card section-gap" role="status">
            最近扫描：新建 {scanResult.created} · 跳过 {scanResult.skipped} · 自动忽略{' '}
            {scanResult.auto_dismissed}
          </div>
        )}

        {loading && <div className="empty">加载中…</div>}
        {error && (
          <div className="form-error" role="alert">
            {error}
            <button className="btn btn-sm" type="button" onClick={() => setTick((n) => n + 1)}>
              重试
            </button>
          </div>
        )}
        {!loading && !error && items.length === 0 && <div className="empty">暂无提醒</div>}
        {!loading && !error && items.length > 0 && (
          <section className="card">
            <ul className="reminder-list">
              {items.map((item) => (
                <li key={item.id} className="reminder-row">
                  <div>
                    <span className="badge badge-muted">{typeLabels[item.type]}</span>{' '}
                    <span className="num">{item.due_date}</span>
                    <div>{item.content}</div>
                    <div className="sub">
                      {item.customer_id ? `客户 ${item.customer_id}` : '无客户'} · {item.status}
                    </div>
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
          </section>
        )}
      </main>
    </>
  )
}
