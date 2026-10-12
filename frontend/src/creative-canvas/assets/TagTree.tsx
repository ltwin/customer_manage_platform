import { useState } from 'react'
import {
  ArrowRight,
  ChevronRight,
  Palette,
  Pencil,
  Plus,
  Tag,
  Trash2,
} from 'lucide-react'
import CanvasContextMenu, {
  type CanvasMenuItem,
} from '../editor/CanvasContextMenu.tsx'
import type * as api from '../editor/api.ts'
import ActionPopover from './ActionPopover.tsx'
import { SWATCHES } from './appearance.ts'
import type { Point } from './popover.ts'
import { tagSections } from './query.ts'
import { nameTaken, type TagCommand } from './tagCommands.ts'
import type { LibraryCatalog } from './useLibrary.ts'

type Editing =
  | { kind: 'newTag'; category: string | null }
  | { kind: 'newCategory' }
  | { kind: 'renameTag'; id: string }
  | { kind: 'renameCategory'; id: string }
type Menu = { at: Point; label: string; items: CanvasMenuItem[] }
type Pop = { kind: 'color' | 'move'; tag: api.Tag; at: Point }
const DRAG = 'application/creative-library-tag'
const at = (e: React.MouseEvent) => ({ x: e.clientX, y: e.clientY })

/**
 * 左栏的标签：按分类列出，点一下加入筛选；右键或「+」就地新建、改名、
 * 改颜色、移到分类和删除（删除先确认，由页面负责）。标签可拖到分类上。
 */
export default function TagTree({
  catalog,
  selected,
  counts,
  blocked,
  onTag,
  onCommand,
}: {
  catalog: LibraryCatalog | null
  selected: string[]
  counts: Map<string, number>
  blocked: boolean
  onTag: (id: string) => void
  onCommand: (command: TagCommand, done: () => void) => void
}) {
  const [editing, setEditing] = useState<Editing | null>(null)
  const [closed, setClosed] = useState<Set<string>>(() => new Set())
  const [menu, setMenu] = useState<Menu | null>(null)
  const [pop, setPop] = useState<Pop | null>(null)
  const tags = catalog?.tags ?? []
  const categories = catalog?.categories ?? []
  const done = () => setEditing(null)
  const sections = tagSections(tags, categories, true).filter(
    (s) =>
      s.id ||
      s.tags.length ||
      (editing?.kind === 'newTag' && editing.category === null),
  )
  function addTag(category: string | null) {
    if (category)
      setClosed((s) => {
        const next = new Set(s)
        next.delete(category)
        return next
      })
    setEditing({ kind: 'newTag', category })
  }
  function tagMenu(t: api.Tag, point: Point) {
    setMenu({
      at: point,
      label: `标签操作：${t.name}`,
      items: [
        {
          label: '重命名',
          icon: <Pencil size={14} />,
          disabled: blocked,
          action: () => setEditing({ kind: 'renameTag', id: t.id }),
        },
        {
          label: '颜色…',
          icon: <Palette size={14} />,
          disabled: blocked,
          action: () => setPop({ kind: 'color', tag: t, at: point }),
        },
        {
          label: '移到分类…',
          icon: <ArrowRight size={14} />,
          disabled: blocked,
          action: () => setPop({ kind: 'move', tag: t, at: point }),
        },
        'separator',
        {
          label: '删除标签',
          icon: <Trash2 size={14} />,
          danger: true,
          disabled: blocked,
          sub: t.asset_count
            ? `${t.asset_count} 项素材在用，删除前会确认`
            : undefined,
          action: () => onCommand({ kind: 'deleteTag', tag: t }, () => {}),
        },
      ],
    })
  }
  function categoryMenu(c: api.Category, count: number, point: Point) {
    setMenu({
      at: point,
      label: `分类操作：${c.name}`,
      items: [
        {
          label: '新建标签',
          icon: <Plus size={14} />,
          disabled: blocked,
          action: () => addTag(c.id),
        },
        {
          label: '重命名',
          icon: <Pencil size={14} />,
          disabled: blocked,
          action: () => setEditing({ kind: 'renameCategory', id: c.id }),
        },
        'separator',
        {
          label: '删除分类',
          icon: <Trash2 size={14} />,
          danger: true,
          disabled: blocked,
          sub: '标签保留，归入未分类',
          action: () =>
            onCommand(
              { kind: 'deleteCategory', category: c, tags: count },
              () => {},
            ),
        },
      ],
    })
  }
  function move(t: api.Tag, category: string | null) {
    if ((t.category_id ?? null) !== category)
      onCommand(
        { kind: 'editTag', tag: t, patch: { category_id: category } },
        () => {},
      )
  }
  function drop(category: string | null) {
    return {
      onDragOver: (e: React.DragEvent<HTMLElement>) => {
        if (!e.dataTransfer.types.includes(DRAG) || blocked) return
        e.preventDefault()
        e.dataTransfer.dropEffect = 'move'
        e.currentTarget.classList.add('is-drop')
      },
      onDragLeave: (e: React.DragEvent<HTMLElement>) =>
        e.currentTarget.classList.remove('is-drop'),
      onDrop: (e: React.DragEvent<HTMLElement>) => {
        e.currentTarget.classList.remove('is-drop')
        const t = tags.find((x) => x.id === e.dataTransfer.getData(DRAG))
        if (!t || blocked) return
        e.preventDefault()
        move(t, category)
      },
    }
  }
  const input = (
    label: string,
    initial: string,
    items: { id: string; name: string }[],
    except: string | undefined,
    submit: (name: string) => TagCommand | null,
  ) => (
    <NameInput
      label={label}
      initial={initial}
      blocked={blocked}
      taken={(name) => nameTaken(items, name, except)}
      onCancel={done}
      onSubmit={(name) => {
        const command = submit(name)
        if (command) onCommand(command, done)
        else done()
      }}
    />
  )
  return (
    <section className="al-sec" aria-labelledby="alTags">
      <header id="alTags">
        标签
        <button
          type="button"
          className="al-icon-btn"
          aria-label="新建标签或分类"
          aria-haspopup="menu"
          disabled={blocked || !catalog}
          onClick={(e) => {
            const r = e.currentTarget.getBoundingClientRect()
            setMenu({
              at: { x: r.left, y: r.bottom + 4 },
              label: '新建',
              items: [
                {
                  label: '新建标签',
                  icon: <Tag size={14} />,
                  action: () => addTag(null),
                },
                {
                  label: '新建分类',
                  icon: <Plus size={14} />,
                  action: () => setEditing({ kind: 'newCategory' }),
                },
              ],
            })
          }}
        >
          <Plus size={12} />
        </button>
      </header>
      {editing?.kind === 'newCategory' &&
        input('新分类名称', '', categories, undefined, (name) => ({
          kind: 'createCategory',
          name,
        }))}
      {catalog && !tags.length && !categories.length && !editing && (
        <p className="al-sec-empty">还没有标签</p>
      )}
      {sections.map((s) => {
        const c = categories.find((x) => x.id === s.id)
        const open = !closed.has(s.id ?? 'none')
        return (
          <div key={s.id ?? 'none'} role="group" aria-label={s.name}>
            {c && editing?.kind === 'renameCategory' && editing.id === c.id ? (
              input('分类名称', c.name, categories, c.id, (name) =>
                name === c.name
                  ? null
                  : { kind: 'renameCategory', category: c, name },
              )
            ) : (
              <button
                type="button"
                className={`al-tag-cat${open ? ' open' : ''}`}
                aria-expanded={open}
                onClick={() =>
                  setClosed((old) => {
                    const next = new Set(old)
                    const key = s.id ?? 'none'
                    if (open) next.add(key)
                    else next.delete(key)
                    return next
                  })
                }
                onContextMenu={(e) => {
                  e.preventDefault()
                  if (c) categoryMenu(c, s.tags.length, at(e))
                  else
                    setMenu({
                      at: at(e),
                      label: '未分类',
                      items: [
                        {
                          label: '新建标签',
                          icon: <Plus size={14} />,
                          disabled: blocked,
                          action: () => addTag(null),
                        },
                      ],
                    })
                }}
                {...drop(s.id)}
              >
                <ChevronRight size={12} />
                <span className="al-name">{s.name}</span>
              </button>
            )}
            {open &&
              s.tags.map((t) =>
                editing?.kind === 'renameTag' && editing.id === t.id ? (
                  <div key={t.id}>
                    {input('标签名称', t.name, tags, t.id, (name) =>
                      name === t.name
                        ? null
                        : { kind: 'editTag', tag: t, patch: { name } },
                    )}
                  </div>
                ) : (
                  <button
                    key={t.id}
                    type="button"
                    className="al-row"
                    aria-pressed={selected.includes(t.id)}
                    draggable={!blocked}
                    onDragStart={(e) => {
                      e.dataTransfer.setData(DRAG, t.id)
                      e.dataTransfer.effectAllowed = 'move'
                    }}
                    onClick={() => onTag(t.id)}
                    onContextMenu={(e) => {
                      e.preventDefault()
                      tagMenu(t, at(e))
                    }}
                  >
                    <i className="al-tag-dot" style={{ background: t.color }} />
                    <span className="al-name">{t.name}</span>
                    <span className="al-n">{counts.get(t.id) ?? ''}</span>
                  </button>
                ),
              )}
            {editing?.kind === 'newTag' &&
              editing.category === s.id &&
              input('新标签名称', '', tags, undefined, (name) => ({
                kind: 'createTag',
                name,
                category: s.id,
              }))}
          </div>
        )
      })}
      {menu && (
        <CanvasContextMenu
          x={menu.at.x}
          y={menu.at.y}
          label={menu.label}
          items={menu.items}
          onClose={() => setMenu(null)}
        />
      )}
      {pop && (
        <ActionPopover
          at={pop.at}
          label={pop.kind === 'color' ? '标签颜色' : '移到分类'}
          onClose={() => setPop(null)}
        >
          {pop.kind === 'color' ? (
            <div className="al-tag-swatches">
              {SWATCHES.map((s) => (
                <button
                  key={s.hex}
                  type="button"
                  aria-label={s.label}
                  aria-pressed={
                    pop.tag.color.toLowerCase() === s.hex.toLowerCase()
                  }
                  style={{ background: s.hex }}
                  onClick={() => {
                    if (pop.tag.color.toLowerCase() !== s.hex.toLowerCase())
                      onCommand(
                        {
                          kind: 'editTag',
                          tag: pop.tag,
                          patch: { color: s.hex },
                        },
                        () => {},
                      )
                    setPop(null)
                  }}
                />
              ))}
            </div>
          ) : (
            <div className="al-tag-moves">
              {[...categories, { id: null, name: '未分类' }].map((c) => (
                <button
                  key={c.id ?? 'none'}
                  type="button"
                  className="al-row"
                  aria-pressed={(pop.tag.category_id ?? null) === c.id}
                  onClick={() => {
                    move(pop.tag, c.id)
                    setPop(null)
                  }}
                >
                  <span className="al-name">{c.name}</span>
                </button>
              ))}
            </div>
          )}
        </ActionPopover>
      )}
    </section>
  )
}

/** 回车或失焦确认，Esc 或空名称取消；重名时留在输入框里提示。 */
function NameInput({
  label,
  initial,
  blocked,
  taken,
  onSubmit,
  onCancel,
}: {
  label: string
  initial: string
  blocked: boolean
  taken: (name: string) => boolean
  onSubmit: (name: string) => void
  onCancel: () => void
}) {
  const [error, setError] = useState('')
  function commit(el: HTMLInputElement) {
    const name = el.value.trim()
    if (!name) return onCancel()
    if (taken(name)) return setError(`已有「${name}」，换个名称`)
    onSubmit(name)
  }
  return (
    <div className="al-tag-edit">
      <input
        className="al-group-input"
        aria-label={label}
        aria-invalid={!!error}
        defaultValue={initial}
        placeholder={label}
        maxLength={80}
        autoFocus
        disabled={blocked}
        onFocus={(e) => e.currentTarget.select()}
        onChange={() => setError('')}
        onKeyDown={(e) => {
          if (e.nativeEvent.isComposing) return
          if (e.key === 'Escape') {
            e.preventDefault()
            onCancel()
          } else if (e.key === 'Enter') {
            e.preventDefault()
            commit(e.currentTarget)
          }
        }}
        onBlur={(e) => {
          if (!error) commit(e.currentTarget)
        }}
      />
      {error && (
        <p className="al-tag-error" role="alert">
          {error}
        </p>
      )}
    </div>
  )
}
