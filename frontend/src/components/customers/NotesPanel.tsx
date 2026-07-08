import { useState } from 'react'
import { ApiError, addCustomerNote } from '../../api/client'
import type { CustomerDetail } from '../../api/client'

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
  const readonly = customer.status === 'merged'

  async function submit() {
    // in-flight 守护：Enter 连按不产生重复备注（review REV-002）。
    if (saving) return
    setError(null)
    if (!content.trim()) return
    setSaving(true)
    try {
      await addCustomerNote(customer.id ?? '', content.trim())
      setContent('')
      onChanged()
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
            onChange={(event) => setContent(event.target.value)}
            onKeyDown={(event) => {
              // isComposing：中文输入法确认候选词的 Enter 不触发提交（review REV-003）。
              if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
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
      {error && <div className="form-error">{error}</div>}
      {customer.notes.length === 0 ? (
        <div className="empty">暂无备注</div>
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
