import {
  ChevronRight,
  Clock,
  Folder,
  Heart,
  Images,
  Inbox,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'
import type * as api from '../editor/api.ts'
import { formatBytes } from '../editor/libraryState.ts'
import {
  VIEWS,
  groupTree,
  tagSections,
  type LibraryQuery,
  type View,
} from './query.ts'
import type { Facets, LibraryCatalog } from './useLibrary.ts'

const viewIcons: Record<View, typeof Images> = {
  all: Images,
  unclassified: Inbox,
  favorites: Heart,
  recent: Clock,
  trash: Trash2,
}

/**
 * 资料库树（V6-067 AC-02）：固定视图、最多两层的分组（计数包含子组）、
 * 按分类列出的标签（点一下加入筛选，数量随当前条件变化）、底部用量。
 */
export default function LibraryTree({
  query,
  catalog,
  error,
  facets,
  onView,
  onGroup,
  onTag,
}: {
  query: LibraryQuery
  catalog: LibraryCatalog | null
  error: string
  facets: Facets | null
  onView: (view: View) => void
  onGroup: (id: string) => void
  onTag: (id: string) => void
}) {
  const [closed, setClosed] = useState<Set<string>>(() => new Set())
  const groups = catalog?.groups ?? []
  const current = groups.find((g) => g.id === query.group)
  const tagCounts = new Map(facets?.tags.map((t) => [t.tag_id, t.count]))
  const usage = catalog?.usage
  return (
    <aside className="al-tree" aria-label="资料库">
      <div className="al-tree-top">
        <h1>资产库</h1>
      </div>
      <div className="al-tree-scroll">
        {error && (
          <p className="al-tree-error" role="alert">
            分组和标签读取失败：{error}
          </p>
        )}
        <nav aria-label="视图">
          {VIEWS.map((v) => {
            const Icon = viewIcons[v.key]
            return (
              <button
                key={v.key}
                type="button"
                className="al-row"
                aria-current={
                  !query.group && query.view === v.key ? 'page' : undefined
                }
                onClick={() => onView(v.key)}
              >
                <Icon size={15} />
                <span className="al-name">{v.label}</span>
              </button>
            )
          })}
        </nav>
        <section className="al-sec" aria-labelledby="alGroups">
          <header id="alGroups">分组</header>
          {!groups.length && catalog && (
            <p className="al-sec-empty">还没有分组</p>
          )}
          <ul className="al-groups">
            {groupTree(groups).map(({ group: g, children }) => {
              const open = !closed.has(g.id) || current?.parent_id === g.id
              return (
                <li key={g.id}>
                  <div className="al-row-wrap">
                    {children.length > 0 && (
                      <button
                        type="button"
                        className={`al-twist${open ? ' open' : ''}`}
                        aria-label={`子分组：${g.name}`}
                        aria-expanded={open}
                        onClick={() =>
                          setClosed((s) => {
                            const next = new Set(s)
                            if (open) next.add(g.id)
                            else next.delete(g.id)
                            return next
                          })
                        }
                      >
                        <ChevronRight size={13} />
                      </button>
                    )}
                    <GroupRow
                      group={g}
                      query={query}
                      count={catalog?.counts[g.id]}
                      onGroup={onGroup}
                    />
                  </div>
                  {children.length > 0 && open && (
                    <ul>
                      {children.map((c) => (
                        <li key={c.id}>
                          <GroupRow
                            group={c}
                            query={query}
                            count={catalog?.counts[c.id]}
                            onGroup={onGroup}
                            child
                          />
                        </li>
                      ))}
                    </ul>
                  )}
                </li>
              )
            })}
          </ul>
        </section>
        <section className="al-sec" aria-labelledby="alTags">
          <header id="alTags">标签</header>
          {!catalog?.tags.length && catalog && (
            <p className="al-sec-empty">还没有标签</p>
          )}
          {catalog &&
            tagSections(catalog.tags, catalog.categories).map((s) => (
              <div key={s.id ?? 'none'} role="group" aria-label={s.name}>
                <div className="al-tag-cat">{s.name}</div>
                {s.tags.map((t) => (
                  <button
                    key={t.id}
                    type="button"
                    className="al-row"
                    aria-pressed={query.tags.includes(t.id)}
                    onClick={() => onTag(t.id)}
                  >
                    <i className="al-tag-dot" style={{ background: t.color }} />
                    <span className="al-name">{t.name}</span>
                    <span className="al-n">{tagCounts.get(t.id) ?? ''}</span>
                  </button>
                ))}
              </div>
            ))}
        </section>
      </div>
      {usage && (
        <footer className="al-usage">
          <span>
            {usage.asset_count} 项 · 已用 {formatBytes(usage.stored_bytes)} /{' '}
            {formatBytes(usage.limit_bytes)}
          </span>
          <span
            className="al-meter"
            role="meter"
            aria-label="空间用量"
            aria-valuemin={0}
            aria-valuemax={usage.limit_bytes}
            aria-valuenow={Math.min(usage.stored_bytes, usage.limit_bytes)}
          >
            <i
              style={{
                width: `${Math.min(100, (usage.stored_bytes / Math.max(1, usage.limit_bytes)) * 100)}%`,
              }}
            />
          </span>
        </footer>
      )}
    </aside>
  )
}

function GroupRow({
  group,
  query,
  count,
  child = false,
  onGroup,
}: {
  group: api.Group
  query: LibraryQuery
  count: number | undefined
  child?: boolean
  onGroup: (id: string) => void
}) {
  return (
    <button
      type="button"
      className={`al-row${child ? ' al-child' : ''}`}
      aria-current={query.group === group.id ? 'page' : undefined}
      onClick={() => onGroup(group.id)}
    >
      <Folder size={15} />
      <span className="al-name">{group.name}</span>
      <span className="al-n">{count ?? ''}</span>
    </button>
  )
}
