import { useEffect, useRef, useState } from 'react'
import * as api from './api.ts'
import type { AgentMaterial } from './agentReferences.ts'

export function AgentMaterialPicker({
  onPick,
  onClose,
  imagesOnly = false,
}: {
  onPick: (item: AgentMaterial) => void
  onClose: () => void
  // 首页的参考图只收图片。
  imagesOnly?: boolean
}) {
  const kind = imagesOnly ? '&kind=image' : ''
  const dialog = useRef<HTMLDialogElement>(null)
  const [query, setQuery] = useState('')
  const [page, setPage] = useState<api.AssetPage | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const request = useRef(0)
  useEffect(() => {
    dialog.current?.showModal()
  }, [])
  useEffect(() => {
    const abort = new AbortController()
    const version = ++request.current
    setPage(null)
    setLoading(true)
    setError('')
    void api
      .read<api.AssetPage>(
        `/assets?limit=40&q=${encodeURIComponent(query)}${kind}`,
        abort.signal,
      )
      .then((result) => { if (!abort.signal.aborted && version === request.current) setPage(result) })
      .catch((e: unknown) => {
        if (!abort.signal.aborted)
          setError(e instanceof Error ? e.message : '素材加载失败')
      })
      .finally(() => {
        if (version === request.current) setLoading(false)
      })
    return () => abort.abort()
  }, [query, kind])
  async function more() {
    if (!page?.next_cursor || loading) return
    const version = request.current
    setLoading(true)
    try {
      const next = await api.read<api.AssetPage>(
        `/assets?limit=40&q=${encodeURIComponent(query)}${kind}&cursor=${encodeURIComponent(page.next_cursor)}`,
      )
      if (version === request.current)
        setPage({ ...next, items: [...page.items, ...next.items] })
    } catch (e) {
      if (version === request.current)
        setError(e instanceof Error ? e.message : '素材加载失败')
    } finally {
      if (version === request.current) setLoading(false)
    }
  }
  return (
    <dialog
      ref={dialog}
      className="cc-agent-material-dialog"
      aria-label="引用素材库"
      onCancel={onClose}
    >
      <header>
        <strong>引用素材库</strong>
        <button type="button" onClick={onClose}>
          关闭
        </button>
      </header>
      <input
        autoFocus
        aria-label="搜索素材"
        placeholder="搜索素材名称…"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      <p>
        {imagesOnly
          ? '只显示图片，引用固定版本；需要支持识图的模型。'
          : '支持文本和图片，引用固定版本；图片需要支持视觉输入的模型。'}
      </p>
      {error && <p role="alert">{error}</p>}
      <div className="cc-agent-material-list">
        {page?.items
          .filter(
            (item) =>
              item.content?.kind === 'text' || item.content?.kind === 'image',
          )
          .map((item) => (
            <button
              type="button"
              key={item.id}
              onClick={() => {
                const kind = item.content?.kind
                if (kind === 'text' || kind === 'image')
                  onPick({
                    id: item.content_revision_id,
                    title: item.title,
                    kind,
                  })
              }}
            >
              <span>{item.content?.kind === 'image' ? '图片' : '文本'}</span>
              {item.title}
            </button>
          ))}
      </div>
      {loading && <p role="status">正在加载…</p>}
      {!loading &&
        page &&
        !page.items.some(
          (item) =>
            item.content?.kind === 'text' || item.content?.kind === 'image',
        ) && <p>本页没有可引用的文本或图片。</p>}
      {page?.next_cursor && (
        <button type="button" disabled={loading} onClick={() => void more()}>
          加载更多
        </button>
      )}
    </dialog>
  )
}
