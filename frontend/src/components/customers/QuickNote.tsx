import { useRef, useState } from 'react'
import { ApiError, addCustomerNote } from '../../api/client'
import { createNoteSubmitGate, noteInputAction } from './noteInteraction'

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
  const [feedback, setFeedback] = useState<string | null>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const submitGateRef = useRef(createNoteSubmitGate())

  function cancelAndReturnFocus() {
    setOpen(false)
    setContent('')
    setError(null)
    requestAnimationFrame(() => triggerRef.current?.focus())
  }

  async function submit() {
    setError(null)
    if (!content.trim()) return
    if (!submitGateRef.current.tryStart()) return
    setSaving(true)
    try {
      await addCustomerNote(customerId, content.trim())
      setContent('')
      setOpen(false)
      setFeedback('备注已保存')
      onSaved()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized()
        return
      }
      setError(err instanceof Error ? err.message : '备注保存失败')
    } finally {
      submitGateRef.current.finish()
      setSaving(false)
    }
  }

  if (!open) {
    return (
      <span className="quick-note quick-note-closed" onClick={(event) => event.stopPropagation()}>
        <button
          ref={triggerRef}
          className="btn btn-ghost"
          type="button"
          onClick={() => {
            setError(null)
            setFeedback(null)
            setOpen(true)
          }}
        >
          记备注
        </button>
        {feedback && <span className="note-feedback" role="status">{feedback}</span>}
      </span>
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
        onChange={(event) => {
          setContent(event.target.value)
          setError(null)
        }}
        onKeyDown={(event) => {
          const action = noteInputAction({
            key: event.key,
            isComposing: event.nativeEvent.isComposing,
          })
          if (action === 'submit') {
            event.preventDefault()
            void submit()
          }
          if (action === 'cancel') {
            event.preventDefault()
            cancelAndReturnFocus()
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
      {error && <span className="form-error" role="alert">{error}</span>}
    </span>
  )
}
