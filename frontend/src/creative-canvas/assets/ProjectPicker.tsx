import { useEffect, useState } from 'react'
import { Folder, Plus, Search } from 'lucide-react'
import * as api from '../editor/api.ts'
import { projectNameLimit } from '../home/projectName.ts'
import { errorMessage } from '../editor/queue.ts'
export type ProjectTarget = { canvas: string } | { name: string }
export default function ProjectPicker({
  onChoose,
}: {
  onChoose: (target: ProjectTarget) => void
}) {
  const [q, setQ] = useState('')
  const [page, setPage] = useState<api.ProjectPage | null>(null)
  const [searchable, setSearchable] = useState(false)
  const [error, setError] = useState('')
  const [tick, setTick] = useState(0)
  const [cursor, setCursor] = useState('')
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState('')
  const [loading, setLoading] = useState(true)
  useEffect(() => {
    const abort = new AbortController()
    setLoading(true)
    const params = new URLSearchParams({
      archived: 'false',
      sort: 'edited_desc',
      limit: '30',
      q,
    })
    if (cursor) params.set('cursor', cursor)
    api
      .read<api.ProjectPage>(`/projects?${params}`, abort.signal)
      .then((next) => {
        if (abort.signal.aborted) return
        setPage((old) => ({
          ...next,
          items:
            cursor && old
              ? [
                  ...old.items,
                  ...next.items.filter(
                    (p) => !old.items.some((x) => x.id === p.id),
                  ),
                ]
              : next.items,
        }))
        if (next.items.length > 8 || next.next_cursor) setSearchable(true)
        setError('')
      })
      .catch((e: unknown) => {
        if (!abort.signal.aborted) setError(errorMessage(e))
      })
      .finally(() => {
        if (!abort.signal.aborted) setLoading(false)
      })
    return () => abort.abort()
  }, [q, cursor, tick])
  return (
    <>
      <div className="al-pop-heading">放到哪个项目的画布上</div>
      {searchable && (
        <label className="cl-find">
          <Search size={13} />
          <input
            aria-label="搜索项目"
            placeholder="搜索项目"
            value={q}
            onChange={(e) => {
              setQ(e.currentTarget.value)
              setCursor('')
              setPage(null)
            }}
          />
        </label>
      )}
      <div className="al-pop-list">
        {page?.items.map((p) => (
          <button
            type="button"
            key={p.id}
            disabled={loading}
            onClick={() => onChoose({ canvas: p.default_canvas_id })}
          >
            <Folder size={14} />
            <span>{p.name}</span>
          </button>
        ))}
        {loading && <p role="status">正在读取项目…</p>}
        {error && (
          <p role="alert">
            {error}
            <button type="button" onClick={() => setTick((n) => n + 1)}>
              重试
            </button>
          </p>
        )}
        {page && !page.items.length && !loading && <p>还没有匹配的项目</p>}
        {page?.next_cursor && (
          <button
            type="button"
            disabled={loading}
            onClick={() => setCursor(page.next_cursor)}
          >
            加载更多
          </button>
        )}
      </div>
      <div className="al-pop-foot">
        {creating ? (
          <form
            onSubmit={(e) => {
              e.preventDefault()
              const trimmed = name.trim()
              if (!trimmed || [...trimmed].length > projectNameLimit) {
                setError(`项目名称请输入 1–${projectNameLimit} 个字`)
                return
              }
              onChoose({ name: trimmed })
            }}
          >
            <input
              autoFocus
              aria-label="新项目名称"
              placeholder="项目名称"
              maxLength={projectNameLimit}
              value={name}
              onChange={(e) => setName(e.currentTarget.value)}
            />
            <button type="submit" className="btn btn-primary">
              新建并放入
            </button>
          </form>
        ) : (
          <button type="button" onClick={() => setCreating(true)}>
            <Plus size={14} />
            新建项目并放入
          </button>
        )}
      </div>
    </>
  )
}
