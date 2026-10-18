import { useMemo } from 'react'
import { Box, Camera, Eye, EyeOff, Folder, Lightbulb, Lock, LockOpen, Shapes } from 'lucide-react'
import type { SceneObject } from '../api.ts'
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

export function Outliner({
  objects,
  selection,
  readOnly,
  onSelect,
  onToggle,
}: {
  objects: readonly SceneObject[]
  selection: Selection
  readOnly: boolean
  onSelect: (id: string, mods: { mod: boolean; shift: boolean }, order: string[]) => void
  onToggle: (id: string, field: 'visible' | 'locked', value: boolean) => void
}) {
  const list = useMemo(() => rows(objects), [objects])
  const order = list.map((r) => r.object.id)
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
            className={`cc-scene-row ${selected ? 'is-selected' : ''} ${selection.active === o.id ? 'is-active' : ''} ${o.visible ? '' : 'is-hidden'}`}
            style={{ paddingLeft: 8 + depth * 14 }}
            onClick={(e) => onSelect(o.id, { mod: e.metaKey || e.ctrlKey, shift: e.shiftKey }, order)}
          >
            <Icon size={14} />
            <span className="cc-scene-row-name">{o.name || '未命名'}</span>
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
