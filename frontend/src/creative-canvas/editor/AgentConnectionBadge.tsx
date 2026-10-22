import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { activeUntil, listConnections, type Connection } from '../agents/api.ts'
import './agentConnection.css'

// 画布顶栏的「外部助手已连接」（epic creative-mcp-server W-4）：最近 5 分钟内调用过工具的
// 外部助手。服务端只在连接变为活跃、新建或断开时发失效通知，变为不活跃不通知，
// 所以到期由这里按最近调用时间自己判断。外部助手没开放（接口 404）时什么都不显示。

const pollMs = 60_000
const minGapMs = 10_000

function minutesAgo(iso: string | null | undefined, now: number): string {
  if (!iso) return ''
  const minutes = Math.max(0, Math.round((now - Date.parse(iso)) / 60000))
  return minutes < 1 ? '刚刚' : `${minutes} 分钟前`
}

export default function AgentConnectionBadge({ signal }: { signal?: unknown }) {
  const [items, setItems] = useState<Connection[]>([])
  const [now, setNow] = useState(() => Date.now())
  const [open, setOpen] = useState(false)
  const last = useRef(0)

  const load = useCallback((force: boolean) => {
    const at = Date.now()
    if (!force && at - last.current < minGapMs) return
    last.current = at
    listConnections({ active: true })
      .then((list) => {
        setNow(Date.now())
        setItems(list.items)
      })
      .catch(() => setItems([]))
  }, [])

  useEffect(() => {
    load(true)
    const timer = window.setInterval(() => load(true), pollMs)
    const visible = () => {
      if (document.visibilityState === 'visible') load(false)
    }
    document.addEventListener('visibilitychange', visible)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', visible)
    }
  }, [load])

  // 画布的实时失效通知也覆盖连接的变化：快照一变就顺带重读，最多每 10 秒一次。
  useEffect(() => {
    if (signal !== undefined) load(false)
  }, [signal, load])

  const active = useMemo(() => items.filter((c) => (activeUntil(c) ?? 0) > now), [items, now])
  // 下一个连接到期时重算一次，让指示按时消失。
  useEffect(() => {
    const next = Math.min(...active.map((c) => activeUntil(c) ?? Infinity))
    if (!Number.isFinite(next)) return
    const timer = window.setTimeout(() => setNow(Date.now()), Math.max(1000, next - Date.now()))
    return () => window.clearTimeout(timer)
  }, [active])

  if (!active.length) return null
  const first = active[0]
  const label = active.length === 1 ? first.display_name : `${first.display_name} 等 ${active.length} 个`
  return (
    <span className="cc-xa">
      <button
        type="button"
        className="cc-xa-pill"
        aria-expanded={open}
        aria-label={`${label}外部助手已连接，最近 5 分钟内调用过`}
        onClick={() => setOpen(!open)}
      >
        <i className="cc-xa-dot" aria-hidden="true" />
        <b>{label}</b>已连接
      </button>
      {open && (
        <>
          <button type="button" className="cc-menu-dismiss" aria-label="关闭" onClick={() => setOpen(false)} />
          <div className="cc-xa-pop" role="dialog" aria-label="外部助手">
            <ul>
              {active.map((c) => (
                <li key={c.id}>
                  <b>{c.display_name}</b>
                  <time dateTime={c.last_used_at ?? undefined}>{minutesAgo(c.last_used_at, now)}调用</time>
                </li>
              ))}
            </ul>
            <footer>
              <span>外部助手正在读取你的创意空间</span>
              <Link to="/creative/agents">管理连接</Link>
            </footer>
          </div>
        </>
      )}
    </span>
  )
}
