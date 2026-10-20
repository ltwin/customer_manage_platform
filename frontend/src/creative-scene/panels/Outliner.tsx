import { useMemo, useState, type DragEvent } from 'react'
import { Box, Camera, Eye, EyeOff, Folder, Lightbulb, Lock, LockOpen, Shapes } from 'lucide-react'
import type { SceneObject } from '../api.ts'
import { canDrop, draggedObjects, type DropPlace } from '../grouping.ts'
import type { Selection } from '../input/selection.ts'

type Row = { object: SceneObject; depth: number }

/** The object tree in sibling order, flattened for display. */
function rows(objects: readonly SceneObject[]): Row[] {
  const byParent = new Map<string, SceneObject[]>()
  for (const o of objects) {
    const key = o.parent_id ?? ''
    byParent.set(key, [...(byParent.get(key) ?? []), o])
  }
  const out: Row[] = []
  const walk = (parent: string, depth: number) => {
    for (const o of (byParent.get(parent) ?? []).sort((a, b) => a.order - b.order)) {
      out.push({ object: o, depth })
      walk(o.id, depth + 1)
    }
  }
  walk('', 0)
  return out
}

const icon = { primitive: Box, model_instance: Shapes, light: Lightbulb, camera: Camera, group: Folder } as const

/**
 * Where on a row a drop lands: the top and bottom quarter go before and
 * after it, the middle half of a group goes inside; other rows split in two.
 */
function placeAt(e: DragEvent<HTMLElement>, group: boolean): DropPlace {
  const r = e.currentTarget.getBoundingClientRect()
  const y = (e.clientY - r.top) / Math.max(r.height, 1)
  if (group) return y < 0.25 ? 'before' : y > 0.75 ? 'after' : 'inside'
  return y < 0.5 ? 'before' : 'after'
}

export function Outliner({
  objects,
  selection,
  highlighted,
  readOnly,
  onSelect,
  onToggle,
  onDrop,
}: {
  objects: readonly SceneObject[]
  selection: Selection
  /** Objects the assistant is working on. */
  highlighted: ReadonlySet<string>
  readOnly: boolean
  onSelect: (id: string, mods: { mod: boolean; shift: boolean }, order: string[]) => void
  onToggle: (id: string, field: 'visible' | 'locked', value: boolean) => void
  /** Rows dropped before, after or inside another row: a reparent keeping world positions. */
  onDrop: (moving: string[], target: string, place: DropPlace) => void
}) {
  const list = useMemo(() => rows(objects), [objects])
  const order = list.map((r) => r.object.id)
  const [moving, setMoving] = useState<string[] | null>(null)
  const [over, setOver] = useState<{ id: string; place: DropPlace } | null>(null)
  const end = () => {
    setMoving(null)
    setOver(null)
  }
  return (
    <div className="cc-scene-outliner" role="tree" aria-label="物件列表">
      {list.map(({ object: o, depth }) => {
        const Icon = icon[o.kind as keyof typeof icon] ?? Box
        const selected = selection.selected.includes(o.id)
        return (
          <div
            key={o.id}
            role="treeitem"
            aria-selected={selected}
            className={`cc-scene-row ${selected ? 'is-selected' : ''} ${selection.active === o.id ? 'is-active' : ''} ${o.visible ? '' : 'is-hidden'} ${over?.id === o.id ? `is-drop-${over.place}` : ''}`}
            style={{ paddingLeft: 8 + depth * 14 }}
            onClick={(e) => onSelect(o.id, { mod: e.metaKey || e.ctrlKey, shift: e.shiftKey }, order)}
            draggable={!readOnly}
            onDragStart={(e) => {
              e.dataTransfer.effectAllowed = 'move'
              e.dataTransfer.setData('text/plain', o.id)
              setMoving(draggedObjects(objects, selection, o.id))
            }}
            onDragOver={(e) => {
              if (!moving) return
              const place = placeAt(e, o.kind === 'group')
              if (!canDrop(objects, moving, o.id, place)) {
                if (over?.id === o.id) setOver(null)
                return
              }
              e.preventDefault()
              e.dataTransfer.dropEffect = 'move'
              if (over?.id !== o.id || over.place !== place) setOver({ id: o.id, place })
            }}
            onDragLeave={() => over?.id === o.id && setOver(null)}
            onDrop={(e) => {
              e.preventDefault()
              const place = placeAt(e, o.kind === 'group')
              if (moving && canDrop(objects, moving, o.id, place)) onDrop(moving, o.id, place)
              end()
            }}
            onDragEnd={end}
          >
            <Icon size={14} />
            <span className="cc-scene-row-name">{o.name || '未命名'}</span>
            {highlighted.has(o.id) && <span className="cc-scene-row-agent">助手正在修改</span>}
            <button
              aria-label={o.visible ? '隐藏' : '显示'}
              disabled={readOnly}
              onClick={(e) => {
                e.stopPropagation()
                onToggle(o.id, 'visible', !o.visible)
              }}
            >
              {o.visible ? <Eye size={13} /> : <EyeOff size={13} />}
            </button>
            <button
              aria-label={o.locked ? '解锁' : '锁定'}
              disabled={readOnly}
              onClick={(e) => {
                e.stopPropagation()
                onToggle(o.id, 'locked', !o.locked)
              }}
            >
              {o.locked ? <Lock size={13} /> : <LockOpen size={13} />}
            </button>
          </div>
        )
      })}
    </div>
  )
}
