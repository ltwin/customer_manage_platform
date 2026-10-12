import { memo, useEffect, useRef, useState } from 'react'
import type { MermaidInstance } from '@streamdown/mermaid'

let enginePromise: Promise<MermaidInstance> | undefined
function loadEngine() {
  enginePromise ??= import('@streamdown/mermaid')
    .then(({ mermaid }) =>
      mermaid.getMermaid({
        theme: 'dark',
        securityLevel: 'strict',
        startOnLoad: false,
        suppressErrorRendering: true,
        maxTextSize: 20_000,
        maxEdges: 300,
      }),
    )
    .catch((error: unknown) => {
      enginePromise = undefined
      throw error
    })
  return enginePromise
}
let nextDiagram = 0

export default memo(function MermaidDiagram({ source }: { source: string }) {
  const [rendered, setRendered] = useState<{
    source: string
    svg: string
  } | null>(null)
  const [failed, setFailed] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const [copyStatus, setCopyStatus] = useState('')
  const dialog = useRef<HTMLDialogElement>(null)
  const svg = rendered?.source === source ? rendered.svg : ''
  useEffect(() => {
    let stale = false
    setFailed(false)
    setCopyStatus('')
    dialog.current?.close()
    if (source.length > 20_000) {
      setFailed(true)
      return
    }
    const id = `cc-diagram-${++nextDiagram}`
    void loadEngine()
      .then((engine) => engine.render(id, source))
      .then((result) => {
        if (!stale) setRendered({ source, svg: result.svg })
      })
      .catch(() => {
        if (!stale) setFailed(true)
      })
    return () => {
      stale = true
    }
  }, [source, attempt])

  async function copy() {
    try {
      await navigator.clipboard.writeText(source)
      setCopyStatus('已复制')
    } catch {
      setCopyStatus('复制失败，请从源码中选择复制')
    }
  }
  return (
    <section className="cc-md-diagram" aria-label="Mermaid 图表">
      <div className="cc-md-diagram-bar">
        <span>Mermaid</span>
        <button
          type="button"
          onClick={() => {
            void copy()
          }}
        >
          复制源码
        </button>
        {svg && !failed && (
          <button type="button" onClick={() => dialog.current?.showModal()}>
            放大
          </button>
        )}
      </div>
      {copyStatus && (
        <small className="cc-md-copy-status" role="status">
          {copyStatus}
        </small>
      )}
      {failed ? (
        <div className="cc-md-fallback">
          <small>图表无法显示，保留源码</small>
          {source.length <= 20_000 && (
            <button
              type="button"
              onClick={() => setAttempt((value) => value + 1)}
            >
              重试
            </button>
          )}
          <pre>{source}</pre>
        </div>
      ) : svg ? (
        <>
          {/* Only SVG produced by Mermaid in strict mode reaches this sink. */}
          <div
            className="cc-md-diagram-image"
            dangerouslySetInnerHTML={{ __html: svg }}
          />
          <details className="cc-md-source">
            <summary>查看源码</summary>
            <pre>{source}</pre>
          </details>
        </>
      ) : (
        <div className="cc-md-fallback">
          <small>图表加载中…</small>
          <pre>{source}</pre>
        </div>
      )}
      <dialog ref={dialog} className="cc-md-dialog" aria-label="放大查看图表">
        <button type="button" onClick={() => dialog.current?.close()}>
          关闭
        </button>
        <div dangerouslySetInnerHTML={{ __html: svg }} />
      </dialog>
    </section>
  )
})
