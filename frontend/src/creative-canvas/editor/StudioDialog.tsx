import { createPortal } from 'react-dom'
import { type ReactNode } from 'react'
import { X } from 'lucide-react'
import { useFocusTrap } from '../../components/useFocusTrap'
export default function StudioDialog({
  title,
  onClose,
  children,
  overlayClassName,
  closeDisabled = false,
}: {
  title: string
  onClose: () => void
  children: ReactNode
  overlayClassName?: string
  closeDisabled?: boolean
}) {
  const ref = useFocusTrap<HTMLDivElement>(true, onClose, !closeDisabled)
  return createPortal(
    <div
      className={`overlay open cc-dialog-overlay ${overlayClassName ?? ''}`}
      onMouseDown={(e) => {
        if (!closeDisabled && e.target === e.currentTarget) onClose()
      }}
    >
      <div
        ref={ref}
        className="dialog cc-editor-dialog cc-surface"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
      >
        <header>
          <h2>{title}</h2>
          <button
            className="cc-icon-button"
            aria-label="收起编辑面板"
            title="关闭，保留本机草稿"
            disabled={closeDisabled}
            onClick={onClose}
          >
            <X size={18} />
          </button>
        </header>
        <div className="cc-dialog-body">{children}</div>
      </div>
    </div>,
    document.querySelector('.cc-workspace') ?? document.body,
  )
}
