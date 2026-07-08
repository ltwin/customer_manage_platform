import { useState } from 'react'
import { ApiError, addCustomerNote } from '../../api/client'

type Props = {
  customerId: string
  onSaved: () => void
  onUnauthorized: () => void
}

// 列表页行内快捷备注（A14）：点「记备注」展开输入 → 保存，共 2 步。
export default function QuickNote({ customerId, onSaved, onUnauthorized }: Props) {
  const [open, setOpen] = useState(false)
  const [content, setContent] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    // in-flight 守护：Enter 连按不产生重复备注（review REV-002）。
    if (saving) return
    setError(null)
    if (!content.trim()) return
    setSaving(true)
    try {
      await addCustomerNote(customerId, content.trim())
      setContent('')
      setOpen(false)
      onSaved()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized()
        return
      }
      setError(err instanceof Error ? err.message : '备注保存失败')
    } finally {
      setSaving(false)
    }
  }

  if (!open) {
    return (
      <button
        className="btn btn-ghost"
        type="button"
        onClick={(event) => {
          event.stopPropagation()
          setOpen(true)
        }}
      >
        记备注
      </button>
    )
  }

  return (
    <span className="quick-note" onClick={(event) => event.stopPropagation()}>
      <input
        className="input"
        maxLength={500}
        value={content}
        placeholder="随手记一条…"
        autoFocus
        onChange={(event) => setContent(event.target.value)}
        onKeyDown={(event) => {
          // isComposing：中文输入法确认候选词的 Enter 不触发提交（review REV-003）。
          if (event.key === 'Enter' && !event.nativeEvent.isComposing) {
            event.preventDefault()
            void submit()
          }
          if (event.key === 'Escape') {
            setOpen(false)
            setContent('')
          }
        }}
      />
      <button
        className="btn btn-primary"
        type="button"
        onClick={() => { void submit() }}
        disabled={saving || !content.trim()}
      >
        存
      </button>
      {error && <span className="form-error">{error}</span>}
    </span>
  )
}
