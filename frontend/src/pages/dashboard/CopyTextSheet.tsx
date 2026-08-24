import { useEffect, useRef, useState } from 'react'
import { useFocusTrap } from '../../components/useFocusTrap'

// dashboard-v2 文案复制弹层（ITEM-5）：内容可编辑后复制，粘贴到微信 / QQ / Telegram。
// 可约时间文案由 calendar/openings.ts 的 openingsText 生成，与 Calendar 同源。
export default function CopyTextSheet({
  title,
  sub,
  text,
  onClose,
}: {
  title: string
  sub: string
  text: string
  onClose(): void
}) {
  const [value, setValue] = useState(text)
  const [copied, setCopied] = useState(false)
  const dialogRef = useFocusTrap<HTMLElement>(true, onClose, true)
  const timerRef = useRef<number | null>(null)

  useEffect(() => {
    return () => {
      if (timerRef.current !== null) window.clearTimeout(timerRef.current)
    }
  }, [])

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
    } catch {
      // 剪贴板 API 不可用（非安全上下文等）时回退到选区复制
      const textarea = dialogRef.current?.querySelector('textarea')
      textarea?.focus()
      textarea?.select()
      document.execCommand('copy')
    }
    setCopied(true)
    if (timerRef.current !== null) window.clearTimeout(timerRef.current)
    timerRef.current = window.setTimeout(() => setCopied(false), 2200)
  }

  return (
    <div className="overlay open" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <section ref={dialogRef} className="dialog" role="dialog" aria-modal="true" aria-labelledby="dv2CopyTitle" tabIndex={-1} autoFocus>
        <h2 id="dv2CopyTitle">{title}</h2>
        <p className="dialog-sub">{sub}</p>
        <textarea
          className="dv2-copy-text"
          rows={6}
          aria-label="文案内容，可编辑"
          value={value}
          onChange={(event) => setValue(event.target.value)}
        />
        <div className="dv2-copy-hint">可直接编辑后再复制 · 粘贴到微信 / QQ / Telegram 发送</div>
        <div className="dialog-actions">
          <button className="btn" type="button" onClick={onClose}>
            {copied ? '已复制，去粘贴吧' : '关闭'}
          </button>
          <button className="btn btn-primary" type="button" onClick={() => { void copy() }}>
            复制文案
          </button>
        </div>
      </section>
    </div>
  )
}
