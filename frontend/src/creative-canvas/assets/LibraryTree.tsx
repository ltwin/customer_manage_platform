import {
  ChevronRight,
  Clock,
  Folder,
  Heart,
  Images,
  Inbox,
  Trash2,
  Plus,
  Pencil,
} from 'lucide-react'
import { useState } from 'react'
import CanvasContextMenu from '../editor/CanvasContextMenu.tsx'
import type * as api from '../editor/api.ts'
import { formatBytes } from '../editor/libraryState.ts'
import { VIEWS, groupTree, type LibraryQuery, type View } from './query.ts'
import type { Point } from './popover.ts'
import type { TagCommand } from './tagCommands.ts'
import TagTree from './TagTree.tsx'
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
 * 按分类列出的标签（点一下加入筛选，数量随当前条件变化，可就地整理）、底部用量。
 */
export default function LibraryTree({
  query,
  catalog,
  error,
  facets,
  onView,
  onGroup,
  onTag,
  onDropAssets,
  onDeleteGroup,
  onSaveGroup,
  onTagCommand,
  blocked,
  importButton,
}: {
  query: LibraryQuery
  catalog: LibraryCatalog | null
  error: string
  facets: Facets | null
  onView: (view: View) => void
  onGroup: (id: string) => void
  onTag: (id: string) => void
  onDropAssets: (ids: string[], group: string) => void
  onDeleteGroup: (group: api.Group) => void
  onSaveGroup: (
    group: api.Group | null,
    parent: string | null,
    name: string,
    done: () => void,
  ) => void
  onTagCommand: (command: TagCommand, done: () => void) => void
  importButton?: React.ReactNode
  blocked: boolean
}) {
  const [editor, setEditor] = useState<{
    group: api.Group | null
    parent: string | null
  } | null>(null)
  const [groupMenu, setGroupMenu] = useState<{
    group: api.Group
    at: Point
  } | null>(null)
  function nameInput() {
    return (
      editor && (
        <input
          className="al-group-input"
          aria-label={editor.group ? '分组名称' : '新分组名称'}
          defaultValue={editor.group?.name ?? ''}
          placeholder="分组名称"
          maxLength={80}
          autoFocus
          disabled={blocked}
          onFocus={(e) => e.currentTarget.select()}
          onKeyDown={(e) => {
            if (e.nativeEvent.isComposing) return
            if (e.key === 'Escape') {
              e.preventDefault()
              setEditor(null)
            }
            if (e.key === 'Enter') {
              e.preventDefault()
              e.currentTarget.blur()
            }
          }}
          onBlur={(e) => {
            const name = e.currentTarget.value.trim()
            if (!name || name === editor.group?.name) {
              setEditor(null)
              return
            }
            onSaveGroup(editor.group, editor.parent, name, () =>
              setEditor(null),
            )
          }}
        />
      )
    )
  }
  const onGroupMenu = (group: api.Group, at: Point) =>
    setGroupMenu({ group, at })
  const [closed, setClosed] = useState<Set<string>>(() => new Set())
  const groups = catalog?.groups ?? []
  const current = groups.find((g) => g.id === query.group)
  const tagCounts = new Map(facets?.tags.map((t) => [t.tag_id, t.count]))
  const usage = catalog?.usage
  return (
    <aside className="al-tree" aria-label="资料库">
      {groupMenu && (
        <CanvasContextMenu
          x={groupMenu.at.x}
          y={groupMenu.at.y}
          label="分组操作"
          onClose={() => setGroupMenu(null)}
          items={[
            {
              label: '重命名',
              icon: <Pencil size={14} />,
              disabled: blocked,
              action: () =>
                setEditor({
                  group: groupMenu.group,
                  parent: groupMenu.group.parent_id,
                }),
            },
            {
              label: '新建子分组',
              icon: <Plus size={14} />,
              disabled: blocked || !!groupMenu.group.parent_id,
              sub: groupMenu.group.parent_id ? '最多两层' : undefined,
              action: () =>
                setEditor({ group: null, parent: groupMenu.group.id }),
            },
            'separator',
            {
              label: '删除分组',
              icon: <Trash2 size={14} />,
              danger: true,
              disabled: blocked,
              sub: '素材仍在库里，子分组升到上一级',
              action: () => onDeleteGroup(groupMenu.group),
            },
          ]}
        />
      )}
      <div className="al-tree-top">
        <h1>资产库</h1>
        {importButton}
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
          <header id="alGroups">
            分组
            <button
              type="button"
              className="al-icon-btn"
              aria-label="新建分组"
              disabled={blocked}
              onClick={() => setEditor({ group: null, parent: null })}
            >
              <Plus size={12} />
            </button>
          </header>
          {editor && !editor.group && !editor.parent && nameInput()}
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
                    {editor?.group?.id === g.id ? (
                      nameInput()
                    ) : (
                      <GroupRow
                        group={g}
                        query={query}
                        count={catalog?.counts[g.id]}
                        onGroup={onGroup}
                        onDropAssets={onDropAssets}
                        onGroupMenu={onGroupMenu}
                      />
                    )}
                  </div>
                  {editor &&
                    !editor.group &&
                    editor.parent === g.id &&
                    nameInput()}
                  {children.length > 0 && open && (
                    <ul>
                      {children.map((c) => (
                        <li key={c.id}>
                          {editor?.group?.id === c.id ? (
                            nameInput()
                          ) : (
                            <GroupRow
                              group={c}
                              query={query}
                              count={catalog?.counts[c.id]}
                              onGroup={onGroup}
                              onDropAssets={onDropAssets}
                              onGroupMenu={onGroupMenu}
                              child
                            />
                          )}
                        </li>
                      ))}
                    </ul>
                  )}
                </li>
              )
            })}
          </ul>
        </section>
        <TagTree
          catalog={catalog}
          selected={query.tags}
          counts={tagCounts}
          blocked={blocked}
          onTag={onTag}
          onCommand={onTagCommand}
        />
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
  onDropAssets,
  onGroupMenu,
}: {
  group: api.Group
  query: LibraryQuery
  count: number | undefined
  child?: boolean
  onGroup: (id: string) => void
  onDropAssets: (ids: string[], group: string) => void
  onGroupMenu: (group: api.Group, at: Point) => void
}) {
  return (
    <button
      type="button"
      className={`al-row${child ? ' al-child' : ''}`}
      aria-current={query.group === group.id ? 'page' : undefined}
      // 画布抽屉里，节点拖到分组上即存入该组（editor/libraryDrop.ts）。
      data-drop-group={group.id}
      onClick={() => onGroup(group.id)}
      onContextMenu={(e) => {
        e.preventDefault()
        onGroupMenu(group, { x: e.clientX, y: e.clientY })
      }}
      onDragOver={(e) => {
        if (
          e.dataTransfer.types.includes(
            'application/creative-library-selection',
          )
        ) {
          e.preventDefault()
          e.dataTransfer.dropEffect = 'copy'
          e.currentTarget.classList.add('is-drop')
        }
      }}
      onDragLeave={(e) => e.currentTarget.classList.remove('is-drop')}
      onDrop={(e) => {
        e.preventDefault()
        e.currentTarget.classList.remove('is-drop')
        const ids = [
          ...new Set(
            e.dataTransfer
              .getData('application/creative-library-selection')
              .split(',')
              .filter(Boolean),
          ),
        ]
        if (ids.length) onDropAssets(ids, group.id)
      }}
    >
      <Folder size={15} />
      <span className="al-name">{group.name}</span>
      <span className="al-n">{count ?? ''}</span>
    </button>
  )
}
