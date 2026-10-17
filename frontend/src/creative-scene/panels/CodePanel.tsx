import { useEffect, useRef, useState } from 'react'
import { Code, X } from 'lucide-react'
import type { EditStatus } from '../modelEdits.ts'
import type { CodeEditor } from './codeEditor.ts'

// The right-hand drawer for a code model's module text, shared with the
// history panel's place. The text is the panel's own until 「生成」: nothing
// is saved by typing. A compile error marks its line.

export function CodePanel({ title, isNew, loadCode, status, readOnly, onGenerate, onClose }: {
  title: string
  /** A model not created yet: the button says 「生成」. */
  isNew: boolean
  /** The code to start from: the template, or the model's own. */
  loadCode: () => Promise<string>
  status: EditStatus | undefined
  readOnly: boolean
  onGenerate: (code: string) => void
  onClose: () => void
}) {
  const host = useRef<HTMLDivElement>(null)
  const editor = useRef<CodeEditor | null>(null)
  const code = useRef('')
  const [ready, setReady] = useState(false)
  const [loadError, setLoadError] = useState('')
  const load = useRef(loadCode)

  useEffect(() => {
    let live = true
    Promise.all([load.current(), import('./codeEditor.ts')])
      .then(([text, { createCodeEditor }]) => {
        if (!live || !host.current) return
        code.current = text
        editor.current = createCodeEditor(host.current, text, (doc) => {
          code.current = doc
        })
        setReady(true)
      })
      .catch((e: unknown) => live && setLoadError(e instanceof Error ? e.message : '代码加载失败'))
    return () => {
      live = false
      editor.current?.destroy()
      editor.current = null
    }
  }, [])

  const error = status?.state === 'failed' ? status : null
  const line = error?.error?.line
  useEffect(() => {
    editor.current?.setErrorLine(line ?? null)
  }, [line, ready])

  const busy = status?.state === 'baking' || status?.state === 'uploading'
  const progress = status?.state === 'baking' ? '正在生成…' : status?.state === 'uploading' ? '正在保存模型…' : status?.state === 'queued' ? '已生成' : ''
  return (
    <aside className="cc-scene-code" aria-label="代码">
      <header>
        <Code size={16} />
        <h3>{title}</h3>
        <button className="btn" aria-label="关闭代码面板" onClick={onClose}>
          <X size={16} />
        </button>
      </header>
      <div ref={host} className="cc-scene-code-editor">
        {!ready && !loadError && <p className="cc-scene-code-loading">正在载入代码…</p>}
      </div>
      <footer>
        {loadError && <p role="alert" className="cc-scene-code-error">{loadError}</p>}
        {error && (
          <p role="alert" className="cc-scene-code-error">
            {line ? `第 ${line} 行：` : ''}
            {error.message}
          </p>
        )}
        <div className="cc-scene-code-actions">
          <span role="status">{progress}</span>
          <button className="btn btn-primary" disabled={!ready || busy || readOnly} onClick={() => onGenerate(code.current)}>
            {isNew ? '生成' : '重新生成'}
          </button>
        </div>
      </footer>
    </aside>
  )
}
