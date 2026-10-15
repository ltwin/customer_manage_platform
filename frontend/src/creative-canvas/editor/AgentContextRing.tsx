import { useEffect, useId, useRef, useState } from 'react'
import { tokenLabel, type RingView } from './agentContext.ts'

const radius = 7
const circumference = 2 * Math.PI * radius

// The composer's context ring: how full the conversation's context is, and on
// hover or tap, what it is made of. A conversation that has sent nothing shows
// an empty ring and no guess.
export function AgentContextRing({
  view,
  compactPercent,
  onCompact,
  compactDisabled,
}: {
  view: RingView
  compactPercent: number
  onCompact: () => void
  // Why compacting is not offered right now; absent when it is.
  compactDisabled?: string
}) {
  const [open, setOpen] = useState(false)
  const [hovered, setHovered] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const panel = useId()
  const shown = open || hovered
  // A tap outside closes what a tap opened.
  useEffect(() => {
    if (!open) return
    const close = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', close)
    return () => document.removeEventListener('pointerdown', close)
  }, [open])
  const approx = view.estimated ? '约 ' : ''
  const label = view.empty
    ? '上下文用量：还没有发送过消息'
    : `上下文已用 ${approx}${view.percent}%`
  return (
    <div
      className="cc-agent-ring-wrap"
      ref={root}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          setOpen(false)
          setHovered(false)
        }
      }}
    >
      <button
        type="button"
        className="cc-agent-ring"
        data-level={view.empty ? 'empty' : view.level}
        aria-label={label}
        aria-expanded={shown}
        aria-controls={panel}
        onClick={() => setOpen((value) => !value)}
      >
        <svg viewBox="0 0 18 18" aria-hidden="true">
          <circle className="cc-agent-ring-track" cx="9" cy="9" r={radius} />
          {!view.empty && (
            <circle
              className="cc-agent-ring-fill"
              cx="9"
              cy="9"
              r={radius}
              strokeDasharray={circumference}
              strokeDashoffset={circumference * (1 - view.percent / 100)}
            />
          )}
        </svg>
        {!view.empty && (
          <span className="cc-agent-ring-text">{view.percent}%</span>
        )}
      </button>
      {shown && (
        <div
          className="cc-agent-ring-panel"
          id={panel}
          role="dialog"
          aria-label="上下文用量"
        >
          {view.empty ? (
            <>
              <p className="cc-agent-ring-head">上下文</p>
              <p className="cc-agent-ring-note">
                发送第一条消息后显示上下文用量
              </p>
              {view.window > 0 && (
                <p className="cc-agent-ring-note">
                  当前模型每轮最多 {tokenLabel(view.window)} token
                </p>
              )}
            </>
          ) : (
            <>
              <p className="cc-agent-ring-head">
                上下文 {approx}
                {tokenLabel(view.used)} / {tokenLabel(view.window)} ·{' '}
                {view.percent}%
              </p>
              <div className="cc-agent-ring-bar" aria-hidden="true">
                <span
                  style={{ width: `${view.percent}%` }}
                  data-level={view.level}
                />
                {compactPercent > 0 && (
                  <i style={{ left: `${compactPercent}%` }} />
                )}
              </div>
              <ul className="cc-agent-ring-parts">
                {view.parts.map((part) => (
                  <li key={part.key}>
                    <span>{part.label}</span>
                    <span>约 {tokenLabel(part.tokens)}</span>
                    <span>{part.percent}%</span>
                  </li>
                ))}
              </ul>
              <p className="cc-agent-ring-note">
                剩余 {tokenLabel(view.remaining)}
                {view.untilCompact === 0
                  ? ' · 下一轮会先自动压缩'
                  : view.untilCompact
                    ? ` · 再用约 ${tokenLabel(view.untilCompact)} 后自动压缩`
                    : ''}
              </p>
              {view.estimated && (
                <p className="cc-agent-ring-note">
                  总数是估算，下一次回复后更新为实际用量
                </p>
              )}
            </>
          )}
          <button
            type="button"
            className="cc-agent-ring-compact"
            disabled={!!compactDisabled}
            title={compactDisabled}
            onClick={() => {
              setOpen(false)
              setHovered(false)
              onCompact()
            }}
          >
            立即压缩
          </button>
          {compactDisabled && (
            <p className="cc-agent-ring-note">{compactDisabled}</p>
          )}
        </div>
      )}
    </div>
  )
}
