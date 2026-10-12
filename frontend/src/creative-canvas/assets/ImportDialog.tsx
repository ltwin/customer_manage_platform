import { useState } from 'react'
import { useFocusTrap } from '../../components/useFocusTrap'

export default function ImportDialog({
  kind,
  busy,
  blocked,
  initialValue,
  initialTitle,
  errorMessage,
  onRetry,
  onClose,
  onSave,
}: {
  kind: 'text' | 'link'
  busy: boolean
  blocked: boolean
  initialValue: string
  initialTitle: string
  errorMessage: string
  onRetry?: () => void
  onClose: () => void
  onSave: (value: string, title: string) => void
}) {
  const [title, setTitle] = useState(initialTitle),
    [value, setValue] = useState(initialValue),
    [error, setError] = useState('')
  const ref = useFocusTrap<HTMLDivElement>(true, onClose, !blocked)
  return (
    <div className="overlay open">
      <div
        ref={ref}
        className="dialog al-import-dialog"
        role="dialog"
        aria-modal="true"
        aria-label={kind === 'link' ? '添加链接' : '新建文字'}
        tabIndex={-1}
      >
        <h2>{kind === 'link' ? '添加链接' : '新建文字'}</h2>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (blocked) return
            try {
              setError('')
              onSave(value, title)
            } catch (error) {
              setError(error instanceof Error ? error.message : '无法添加素材')
            }
          }}
        >
          <label>
            标题（可选）
            <input
              maxLength={200}
              value={title}
              disabled={blocked}
              onChange={(e) => setTitle(e.currentTarget.value)}
            />
          </label>
          <label>
            {kind === 'link' ? '网址' : '文字内容'}
            {kind === 'link' ? (
              <input
                type="url"
                placeholder="https://"
                maxLength={4096}
                required
                disabled={blocked}
                value={value}
                onChange={(e) => setValue(e.currentTarget.value)}
              />
            ) : (
              <textarea
                rows={8}
                required
                disabled={blocked}
                maxLength={100000}
                value={value}
                onChange={(e) => setValue(e.currentTarget.value)}
              />
            )}
          </label>
          {(error || errorMessage) && (
            <p role="alert">{error || errorMessage}</p>
          )}
          <div className="dialog-actions">
            {onRetry && (
              <button
                type="button"
                className="btn btn-primary"
                disabled={busy}
                onClick={onRetry}
              >
                重试原操作
              </button>
            )}
            <button
              type="button"
              className="btn"
              disabled={blocked}
              onClick={onClose}
            >
              取消
            </button>
            <button
              className="btn btn-primary"
              type="submit"
              disabled={blocked || !value.trim()}
            >
              添加
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
