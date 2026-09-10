import { createPortal } from 'react-dom'
import { type ReactNode } from 'react'
import { X } from 'lucide-react'
import { useFocusTrap } from '../../components/useFocusTrap'
export default function StudioDialog({
  title,
  onClose,
  children,
}: {
  title: string
  onClose: () => void
  children: ReactNode
}) {
  const ref = useFocusTrap<HTMLDivElement>(true, onClose)
  return createPortal(
    <div
      className="overlay open cc-dialog-overlay"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        ref={ref}
        className="dialog cc-editor-dialog cc-glass"
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
