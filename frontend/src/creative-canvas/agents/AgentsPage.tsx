import { Check, Copy } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { ApiError } from '../../api/transport.ts'
import '../editor/workspace.css'
import AppSidebar from '../home/AppSidebar.tsx'
import {
  activeUntil,
  isUnavailable,
  listConnections,
  revokeConnection,
  type Connection,
} from './api.ts'
import { hostGuides, starters, type HostGuide, type HostID } from './hostGuides.ts'
import './agents.css'

// 「外部助手」接入页（epic creative-mcp-server W-1）：左边按宿主给接入文字，
// 右边是已连接的外部助手和 MCP 地址。视觉依据 docs/prototypes/creative-mcp/web/connect.html。

type Load =
  | { kind: 'loading' }
  | { kind: 'unavailable' }
  | { kind: 'error'; message: string }
  | { kind: 'ready'; mcpUrl: string; items: Connection[] }

function useCopy() {
  const [copied, setCopied] = useState<string | null>(null)
  useEffect(() => {
    if (!copied) return
    const timer = window.setTimeout(() => setCopied(null), 1600)
    return () => window.clearTimeout(timer)
  }, [copied])
  const copy = useCallback((text: string) => {
    void navigator.clipboard?.writeText(text).then(() => setCopied(text))
  }, [])
  return { copied, copy }
}

function CopyButton({ text, label, primary, copied, onCopy }: {
  text: string
  label: string
  primary?: boolean
  copied: boolean
  onCopy: (text: string) => void
}) {
  return (
    <button type="button" className={primary ? 'btn btn-primary' : 'btn'} onClick={() => onCopy(text)}>
      {copied ? <Check size={14} /> : <Copy size={14} />}
      {copied ? '已复制' : label}
    </button>
  )
}

// 步骤里的 MCP 地址显示成代码样式，其余是普通文字。
function StepText({ text, mcpUrl }: { text: string; mcpUrl: string }) {
  const parts = text.split(mcpUrl)
  return (
    <>
      {parts.map((part, index) => (
        <span key={index}>
          {part}
          {index < parts.length - 1 && <code>{mcpUrl}</code>}
        </span>
      ))}
    </>
  )
}

function dateLabel(iso: string): string {
  const d = new Date(iso)
  return `${d.getMonth() + 1} 月 ${d.getDate()} 日`
}

function lastUsedLabel(c: Connection, now: number): string {
  if (!c.last_used_at) return '还没有调用过工具'
  const minutes = Math.max(0, Math.round((now - Date.parse(c.last_used_at)) / 60000))
  if (minutes < 1) return '刚刚调用'
  if (minutes < 60) return `${minutes} 分钟前调用`
  if (minutes < 24 * 60) return `${Math.round(minutes / 60)} 小时前调用`
  return `${dateLabel(c.last_used_at)}调用`
}

function Guide({ guide, mcpUrl, copied, onCopy, onSwitch }: {
  guide: HostGuide
  mcpUrl: string
  copied: string | null
  onCopy: (text: string) => void
  onSwitch: (id: HostID) => void
}) {
  return (
    <div className="xa-guide">
      {guide.agent && (
        <>
          <p className="xa-p2">{guide.agent.lead}</p>
          <div className="xa-copy-block">
            <pre>{guide.agent.text}</pre>
            <CopyButton primary text={guide.agent.text} label={guide.agent.label} copied={copied === guide.agent.text} onCopy={onCopy} />
          </div>
        </>
      )}
      {guide.steps && (
        <ol className="xa-sub-steps">
          {guide.steps.map((step) => (
            <li key={step}>
              <StepText text={step} mcpUrl={mcpUrl} />
            </li>
          ))}
        </ol>
      )}
      {guide.tip && <p className="xa-p3">{guide.tip}</p>}
      {guide.fallback && (
        <details className="xa-more xa-fallback">
          <summary>{guide.fallback.title}</summary>
          <p className="xa-p2">{guide.fallback.text}</p>
          <div className="xa-copy-block">
            <pre>{guide.fallback.copy.text}</pre>
            <CopyButton text={guide.fallback.copy.text} label={guide.fallback.copy.label} copied={copied === guide.fallback.copy.text} onCopy={onCopy} />
          </div>
          {guide.fallback.switchTo && (
            <p className="xa-p3">
              <button type="button" className="xa-link" onClick={() => onSwitch(guide.fallback?.switchTo ?? guide.id)}>
                {guide.fallback.switchTo === 'claude_code' ? '改用 Claude Code 本机配置 →' : '改用 Codex →'}
              </button>
            </p>
          )}
        </details>
      )}
    </div>
  )
}

function ConnectionRow({ c, now, onRevoked }: { c: Connection; now: number; onRevoked: (c: Connection) => void }) {
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const until = activeUntil(c)
  const active = until !== null && until > now
  async function revoke() {
    setBusy(true)
    setError(null)
    try {
      onRevoked(await revokeConnection(c.id))
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '断开失败，请稍后重试')
    } finally {
      setBusy(false)
    }
  }
  return (
    <li>
      <span className="xa-mark" aria-hidden="true">{c.display_name.slice(0, 1)}</span>
      <div className="xa-c-main">
        <b>{c.display_name}</b>
        <small>授权于 {dateLabel(c.created_at)}</small>
        <small className="xa-last">
          {c.revoked_at ? (
            <>已断开</>
          ) : (
            <>
              <i className={active ? 'xa-dot on' : 'xa-dot'} aria-hidden="true" />
              {active ? `活跃 · ${lastUsedLabel(c, now)}` : lastUsedLabel(c, now)}
            </>
          )}
        </small>
      </div>
      {!c.revoked_at && !confirming && (
        <button type="button" className="btn xa-sm" onClick={() => setConfirming(true)}>
          断开
        </button>
      )}
      {confirming && !c.revoked_at && (
        <div className="xa-confirm">
          <p>断开 {c.display_name}？它要重新授权才能再用。</p>
          {error && <p role="alert" className="xa-error">{error}</p>}
          <div>
            <button type="button" className="btn xa-sm xa-danger" disabled={busy} onClick={() => void revoke()}>
              {busy ? '正在断开…' : '断开'}
            </button>
            <button type="button" className="btn xa-sm" disabled={busy} onClick={() => setConfirming(false)}>
              取消
            </button>
          </div>
        </div>
      )}
    </li>
  )
}

export default function AgentsPage() {
  const [load, setLoad] = useState<Load>({ kind: 'loading' })
  const [host, setHost] = useState<HostID>('claude_code')
  const [now, setNow] = useState(() => Date.now())
  const { copied, copy } = useCopy()

  useEffect(() => {
    const previous = document.title
    document.title = '外部助手 · 创意空间'
    return () => {
      document.title = previous
    }
  }, [])

  const reload = useCallback((signal?: AbortSignal) => {
    listConnections({ includeRevoked: false }, signal)
      .then((list) => {
        setNow(Date.now())
        setLoad({ kind: 'ready', mcpUrl: list.mcp_url, items: list.items })
      })
      .catch((error: unknown) => {
        if (signal?.aborted) return
        if (isUnavailable(error)) setLoad({ kind: 'unavailable' })
        else setLoad({ kind: 'error', message: error instanceof ApiError ? error.message : '读取连接失败，请稍后重试' })
      })
  }, [])

  useEffect(() => {
    const abort = new AbortController()
    reload(abort.signal)
    // 活跃状态按最近调用时间到期，页面开着时每分钟刷新一次。
    const timer = window.setInterval(() => reload(), 60_000)
    const visible = () => {
      if (document.visibilityState === 'visible') reload()
    }
    document.addEventListener('visibilitychange', visible)
    return () => {
      abort.abort()
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', visible)
    }
  }, [reload])

  const mcpUrl = load.kind === 'ready' ? load.mcpUrl : ''
  const guides = useMemo(() => hostGuides(mcpUrl), [mcpUrl])
  const guide = guides.find((g) => g.id === host) ?? guides[0]

  return (
    <div className="cc-workspace cs-app">
      <AppSidebar current="agents" />
      <main className="cs-main">
        <div className="xa-wrap">
          <header className="xa-head">
            <h1>外部助手</h1>
            <p>
              在 Claude、Claude Code、Codex 或 ChatGPT 里使用你的创意空间。现在可以读取画布、素材和资产库；修改画布、生成图片、搭 3D 场景会陆续开放。外部助手的对话不花你的额度。
            </p>
          </header>
          {load.kind === 'loading' && <p className="xa-state" role="status">正在读取…</p>}
          {load.kind === 'unavailable' && (
            <p className="xa-state" role="status">
              外部助手暂未开放。
            </p>
          )}
          {load.kind === 'error' && (
            <div className="xa-state" role="alert">
              <p>{load.message}</p>
              <button type="button" className="btn" onClick={() => reload()}>
                重试
              </button>
            </div>
          )}
          {load.kind === 'ready' && (
            <div className="xa-cols">
              <section className="xa-setup" aria-labelledby="xaSetupTitle">
                <h2 id="xaSetupTitle" className="xa-sr">接入步骤</h2>
                <ol className="xa-steps">
                  <li>
                    <h3>
                      <span className="xa-n">1</span>选择你用的助手
                    </h3>
                    <div className="xa-hosts" role="radiogroup" aria-label="宿主">
                      {guides.map((g) => (
                        <button key={g.id} type="button" role="radio" aria-checked={g.id === host} onClick={() => setHost(g.id)}>
                          <span className="xa-mark" aria-hidden="true">{g.mark}</span>
                          <span className="xa-hn">{g.name}</span>
                          <span className="xa-hs">{g.sub}</span>
                        </button>
                      ))}
                    </div>
                  </li>
                  <li>
                    <h3>
                      <span className="xa-n">2</span>
                      {guide.title}
                    </h3>
                    <Guide guide={guide} mcpUrl={load.mcpUrl} copied={copied} onCopy={copy} onSwitch={setHost} />
                    <p className="xa-p3">宿主的菜单名称以它当前的界面为准。</p>
                  </li>
                  <li>
                    <h3>
                      <span className="xa-n">3</span>授权你的账号
                    </h3>
                    <p className="xa-p2">
                      宿主会打开创意空间的授权页。确认是你的账号，点「允许」。授权后可以随时在右边断开。
                    </p>
                  </li>
                  <li>
                    <h3>
                      <span className="xa-n">4</span>试一句
                    </h3>
                    <div className="xa-starters">
                      {starters.map((s) => (
                        <button key={s} type="button" className="xa-starter" onClick={() => copy(s)}>
                          {copied === s ? <Check size={13} /> : <Copy size={13} />}
                          {s}
                        </button>
                      ))}
                    </div>
                  </li>
                </ol>
              </section>
              <aside className="xa-side-col">
                <section className="xa-panel" aria-labelledby="xaConnTitle">
                  <h2 id="xaConnTitle">已连接</h2>
                  <ul className="xa-conns">
                    {load.items.length === 0 && (
                      <li className="xa-empty">还没有外部助手连接。按左边的步骤接入后，会出现在这里。</li>
                    )}
                    {load.items.map((c) => (
                      <ConnectionRow
                        key={c.id}
                        c={c}
                        now={now}
                        // 断开的连接留在原处显示「已断开」，下一次刷新列表时才移走。
                        onRevoked={(revoked) =>
                          setLoad((prev) =>
                            prev.kind === 'ready'
                              ? { ...prev, items: prev.items.map((item) => (item.id === revoked.id ? revoked : item)) }
                              : prev,
                          )
                        }
                      />
                    ))}
                  </ul>
                  {load.items.length >= 50 && <p className="xa-p3">只显示最近 50 个。</p>}
                </section>
                <section className="xa-panel" aria-labelledby="xaUrlTitle">
                  <h2 id="xaUrlTitle">MCP 地址</h2>
                  <div className="xa-field">
                    <code>{load.mcpUrl}</code>
                    <button type="button" className="xa-icon-btn" aria-label="复制 MCP 地址" onClick={() => copy(load.mcpUrl)}>
                      {copied === load.mcpUrl ? <Check size={14} /> : <Copy size={14} />}
                    </button>
                  </div>
                  <p className="xa-p3">手动配置时用这个地址。授权方式是 OAuth，不需要填密钥。</p>
                </section>
                <section className="xa-panel" aria-labelledby="xaNoteTitle">
                  <h2 id="xaNoteTitle">须知</h2>
                  <ul className="xa-notes">
                    <li>授权后，外部助手读到的内容会发给它使用的 AI 模型服务。</li>
                    <li>外部助手看不到客户、订单、日程等经营数据。</li>
                    <li>断开后，它的下一次调用立即失效，需要重新授权。</li>
                  </ul>
                </section>
              </aside>
            </div>
          )}
        </div>
      </main>
    </div>
  )
}
