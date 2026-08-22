import { useEffect, useRef, useState } from 'react'

export interface ConfirmDialogProps {
  title: string
  body: string | readonly string[]
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
  // 传入即启用「需填写原因」变体：确认按钮在非空 trimming 输入前保持禁用。
  requiredInput?: { label: string; placeholder?: string; maxLength?: number }
  busy?: boolean
  onConfirm: (input: string) => void
  onCancel: () => void
}

// 统一确认对话框：替代散落的 window.confirm/prompt。复用全局 overlay/dialog
// 样式与既有模态范式（backdrop 点击、Escape 关闭、busy 期间不可关）。
export default function ConfirmDialog({
  title,
  body,
  confirmLabel,
  cancelLabel,
  danger,
  requiredInput,
  busy,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const [value, setValue] = useState('')
  const cancelRef = useRef<HTMLButtonElement | null>(null)

  useEffect(() => {
    cancelRef.current?.focus()
  }, [])

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === 'Escape' && !busy) onCancel()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [busy, onCancel])

  const lines = Array.isArray(body) ? body : [body]
  const inputMissing = requiredInput != null && value.trim() === ''

  return (
    <div className="overlay open" onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) onCancel() }}>
      <div className="dialog planning-confirm-dialog" role="alertdialog" aria-modal="true" aria-labelledby="planningConfirmTitle" aria-describedby="planningConfirmBody">
        <h2 id="planningConfirmTitle">{title}</h2>
        <div id="planningConfirmBody" className="dialog-sub">
          {lines.map((line, index) => <p key={index}>{line}</p>)}
        </div>
        {requiredInput && (
          <label className="planning-confirm-input">
            <span>{requiredInput.label}</span>
            <textarea
              rows={2}
              maxLength={requiredInput.maxLength ?? 200}
              value={value}
              placeholder={requiredInput.placeholder}
              onChange={(event) => setValue(event.target.value)}
            />
          </label>
        )}
        <div className="dialog-actions">
          <button ref={cancelRef} className="btn" type="button" disabled={busy} onClick={onCancel}>{cancelLabel ?? '取消'}</button>
          <button
            className={danger ? 'btn btn-danger' : 'btn btn-primary'}
            type="button"
            disabled={busy || inputMissing}
            onClick={() => onConfirm(value.trim())}
          >
            {confirmLabel ?? '确认'}
          </button>
        </div>
      </div>
    </div>
  )
}
