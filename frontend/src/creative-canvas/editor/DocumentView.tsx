import { useEffect, useRef, useState } from 'react'
import { Minimize2, FileText } from 'lucide-react'
import { read, type CreativeDocument } from './api.ts'
import { errorMessage } from './queue.ts'
export default function DocumentView({
  id,
  onClose,
}: {
  id: string
  onClose: () => void
}) {
  const [document, setDocument] = useState<CreativeDocument | null>(null),
    [error, setError] = useState(''),
    [attempt, setAttempt] = useState(0)
  const close = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    const previous = window.document.activeElement
    close.current?.focus()
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected)
        previous.focus()
    }
  }, [])
  useEffect(() => {
    const controller = new AbortController()
    setDocument(null)
    setError('')
    void read<CreativeDocument>(
      `/documents/${encodeURIComponent(id)}`,
      controller.signal,
    )
      .then((d) => {
        if (!controller.signal.aborted) setDocument(d)
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(e))
      })
    return () => controller.abort()
  }, [id, attempt])
  return (
    <section
      className="cc-maximized-document cc-glass"
      aria-label="文档最大化"
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.stopPropagation()
          onClose()
        }
      }}
    >
      <header>
        <FileText size={18} />
        <h2>{document?.title ?? '文档'}</h2>
        <button ref={close} className="btn" onClick={onClose}>
          <Minimize2 size={16} />
          还原节点
        </button>
      </header>
      <div className="cc-document-body">
        {error ? (
          <div role="alert">
            <p>{error}</p>
            <button className="btn" onClick={() => setAttempt((n) => n + 1)}>
              重新读取
            </button>
          </div>
        ) : document ? (
          <>
            <small>独立文档 · 开发验证</small>
            <p>
              {'body' in document.content.payload
                ? document.content.payload.body
                : ''}
            </p>
          </>
        ) : (
          <p>正在读取文档…</p>
        )}
      </div>
    </section>
  )
}
