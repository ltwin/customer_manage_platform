import { useRef, useState } from 'react'
import { StickyNote } from 'lucide-react'
import EmptyState from '../EmptyState'
import { ApiError, addCustomerNote } from '../../api/client'
import type { CustomerDetail } from '../../api/client'
import { createNoteSubmitGate, noteInputAction } from './noteInteraction'

type Props = {
  customer: CustomerDetail
  onChanged: () => void
  onUnauthorized: () => void
}

// 备注 tab（A14）：输入 → 保存两步完成追加；列表由服务端按创建倒序返回。
export default function NotesPanel({ customer, onChanged, onUnauthorized }: Props) {
  const [content, setContent] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [feedback, setFeedback] = useState<string | null>(null)
  const submitGateRef = useRef(createNoteSubmitGate())
  const readonly = customer.status === 'merged'

  async function submit() {
    setError(null)
    if (!content.trim()) return
    if (!submitGateRef.current.tryStart()) return
    setSaving(true)
    try {
      await addCustomerNote(customer.id ?? '', content.trim())
      setContent('')
      setFeedback('备注已保存')
      onChanged()
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

  return (
    <div>
      {!readonly && (
        <div className="field">
          <label htmlFor="note-content">随手备注（≤500 字，Enter 保存）</label>
          <textarea
            id="note-content"
            className="input"
            rows={2}
            maxLength={500}
            value={content}
            onChange={(event) => {
              setContent(event.target.value)
              setError(null)
              setFeedback(null)
            }}
            onKeyDown={(event) => {
              const action = noteInputAction({
                key: event.key,
                shiftKey: event.shiftKey,
                isComposing: event.nativeEvent.isComposing,
                multiline: true,
              })
              if (action === 'submit') {
                event.preventDefault()
                void submit()
              }
            }}
          />
          <div className="topbar-actions">
            <button
              className="btn btn-primary"
              type="button"
              onClick={() => { void submit() }}
              disabled={saving || !content.trim()}
            >
              {saving ? '保存中…' : '保存备注'}
            </button>
          </div>
        </div>
      )}
      {error && <div className="form-error" role="alert">{error}</div>}
      {feedback && <div className="note-feedback" role="status">{feedback}</div>}
      {customer.notes.length === 0 ? (
        <EmptyState icon={StickyNote} title="暂无备注" hint="记一条沟通要点，下次开单不用回忆" inline />
      ) : (
        customer.notes.map((note) => (
          <div className="identity-item" key={note.id}>
            <span className="rmk num">{(note.created_at ?? '').slice(0, 16).replace('T', ' ')}</span>
            <span className="handle">{note.content}</span>
          </div>
        ))
      )}
    </div>
  )
}
