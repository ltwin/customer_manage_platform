import { useCallback, useEffect, useRef, useState } from 'react'
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
import StateNotice from '../components/StateNotice'
import {
  beginPageRead,
  completePageRead,
  failPageRead,
  pageReadPresentation,
  readyPageData,
  type PageReadState,
} from '../components/pageReadState'

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
  const [readState, setReadState] = useState<PageReadState<{ items: Reminder[]; total: number }>>({
    kind: 'loading',
    message: '正在加载提醒',
  })
  const [actionId, setActionId] = useState<string | null>(null)
  const [scanning, setScanning] = useState(false)
  const [scanResult, setScanResult] = useState<ScanRemindersResult | null>(null)
  const [tick, setTick] = useState(0)
  const loadedStatusRef = useRef<ReminderStatus | 'all' | null>(null)

  const load = useCallback(() => {
    const preserveReady = loadedStatusRef.current === status
    loadedStatusRef.current = status
    setReadState((current) => beginPageRead(current, '正在加载提醒', preserveReady))
    listReminders({
      status: status === 'all' ? undefined : status,
      page: 1,
      pageSize: 50,
    })
      .then((res) => {
        setReadState(completePageRead(
          { items: res.items, total: res.total },
          res.items.length === 0,
          `${statusFilters.find(([key]) => key === status)?.[1] ?? '当前筛选'}下暂无提醒`,
        ))
      })
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 401) {
          setReadState({ kind: 'unauthorized' })
          navigate('/login', { replace: true })
          return
        }
        setReadState((current) => failPageRead(
          current,
          err instanceof Error ? err.message : '提醒加载失败',
          () => setTick((value) => value + 1),
        ))
      })
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

  const presentation = pageReadPresentation(readState)
  const data = readyPageData(readState)
  const items = data?.items ?? []
  const total = data?.total ?? 0

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

        {presentation.notice && <StateNotice {...presentation.notice} />}
        {presentation.showReadyData && items.length > 0 && (
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
