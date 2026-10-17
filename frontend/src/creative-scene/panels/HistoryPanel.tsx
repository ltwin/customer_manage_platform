import { useEffect, useState } from 'react'
import { X } from 'lucide-react'
import { readRevision, readRevisions, type Scene } from '../api.ts'
import { groupRevisions, type HistoryGroup, type RevisionItem } from '../history.ts'
import { errorMessage } from '../../creative-canvas/editor/queue.ts'

const time = (iso: string) => new Date(iso).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
const actor: Record<string, string> = { photographer: '你', agent: '助手', system: '系统' }

/** Revision groups; a click previews one read-only. */
export function HistoryPanel({ documentID, revision, previewing, onPreview, onClose }: { documentID: string; revision: string; previewing: string | null; onPreview: (revision: string, scene: Scene) => void; onClose: () => void }) {
  const [items, setItems] = useState<RevisionItem[]>([])
  const [cursor, setCursor] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    readRevisions(documentID, null, controller.signal)
      .then((page) => {
        setItems(page.items)
        setCursor(page.next_cursor)
        setError('')
      })
      .catch((e: unknown) => !controller.signal.aborted && setError(errorMessage(e)))
      .finally(() => !controller.signal.aborted && setLoading(false))
    return () => controller.abort()
  }, [documentID, revision])
  const more = () => {
    if (!cursor) return
    setLoading(true)
    readRevisions(documentID, cursor)
      .then((page) => {
        setItems((old) => [...old, ...page.items])
        setCursor(page.next_cursor)
      })
      .catch((e: unknown) => setError(errorMessage(e)))
      .finally(() => setLoading(false))
  }
  const open = (g: HistoryGroup) => {
    readRevision(documentID, g.latest)
      .then((v) => onPreview(g.latest, (v.view as { scene: Scene }).scene))
      .catch((e: unknown) => setError(errorMessage(e)))
  }
  const groups = groupRevisions(items)
  return (
    <aside className="cc-scene-history" aria-label="历史版本">
      <header>
        <h3>历史版本</h3>
        <button className="btn" aria-label="关闭历史版本" onClick={onClose}>
          <X size={16} />
        </button>
      </header>
      {error && <p role="alert">{error}</p>}
      <ol>
        {groups.map((g) => (
          <li key={g.latest}>
            <button className={previewing === g.latest ? 'is-active' : ''} onClick={() => open(g)}>
              <strong>{g.label}</strong>
              <span>
                第 {g.revisions.at(-1)}{g.revisions.length > 1 ? `–${g.latest}` : ''} 版 · {actor[g.actor] ?? g.actor} · {time(g.to)}
                {g.objectCount ? ` · ${g.objectCount} 个物件` : ''}
              </span>
            </button>
          </li>
        ))}
      </ol>
      {cursor && (
        <button className="btn" disabled={loading} onClick={more}>
          加载更早的版本
        </button>
      )}
    </aside>
  )
}
