import { useState } from 'react'
import { MathUtils } from 'three'
import type { ObjectUpdate, SceneObject } from '../api.ts'
import type { ParamSpec } from '../builtins/catalog.ts'
import { materialCatalog, materialEntry, principledID } from '../materials/catalog.ts'
import { assignmentOf, readClipboard, resolvedSlot, setSlot, slotsOf, toPrincipled, writeClipboard, type MaterialSlot } from '../materials/slotEdits.ts'
import { modelCache, type MeshConstants, type SceneModel } from '../models/cache.ts'
import type { DisplayMode } from '../state/sceneStore.ts'
import { ParamField } from './Properties.tsx'

const sourceLabel = { assigned: '', hint: '按提示', model: '模型自带', default: '默认', unknown: '未知材质' } as const

/** anisotropy_rotation is stored in radians and shown in degrees, like a spot light's angle. */
const inDegrees = (k: string) => k === 'anisotropy_rotation'
const degreeSpec = (spec: ParamSpec): ParamSpec => ({ ...spec, min: -180, max: 180, step: 1, unit: '°', default: 0 })

/**
 * The material section (design「属性栏与复制材质」): each slot of the active
 * object with where its material comes from, the material, and its fields.
 * With several objects selected, an edit goes to every one with a slot of
 * the same name.
 */
export function MaterialSlots({ object, model, objects, selected, models, readOnly, display, storage, onUpdates }: {
  object: SceneObject
  model: SceneModel | undefined
  objects: readonly SceneObject[]
  selected: readonly string[]
  models: Readonly<Record<string, SceneModel>>
  readOnly: boolean
  display: DisplayMode
  storage: Storage | undefined
  onUpdates: (updates: ObjectUpdate[], label: string, merge?: string) => void
}) {
  const slots = slotsOf(object, model)
  const [open, setOpen] = useState<string | null>(slots.length === 1 ? slots[0].slot : null)
  const [notice, setNotice] = useState('')
  if (!slots.length) return null
  const name = object.name || '物件'
  const slotNames = (o: SceneObject) => slotsOf(o, o.model?.content_revision_id ? models[o.model.content_revision_id] : undefined).map((s) => s.slot)
  const constantsOf = (o: SceneObject, slot: string): MeshConstants | null => {
    const rev = o.model?.content_revision_id
    return (rev && modelCache.peek(rev)?.meshes.find((m) => m.slot === slot)?.constants) || null
  }
  const hintOf = (o: SceneObject, slot: string) => slotsOf(o, o.model?.content_revision_id ? models[o.model.content_revision_id] : undefined).find((s) => s.slot === slot)?.hint ?? null
  const edit = (slot: string, label: string, next: (o: SceneObject, current: MaterialSlot | null) => MaterialSlot | null, merge?: string) => {
    const updates = setSlot(objects, selected, object.id, slot, slotNames, next)
    if (updates.length) onUpdates(updates, label, merge)
  }
  return (
    <fieldset className="cc-scene-materials" disabled={readOnly} aria-label="材质">
      <legend>材质</legend>
      {display !== 'preview' && display !== 'rendered' && <small>材质在「材质预览」和「最终渲染」里显示</small>}
      {notice && <small role="status">{notice}</small>}
      {slots.map(({ slot, hint }) => {
        const resolved = resolvedSlot(object, slot, hint, constantsOf(object, slot))
        const assigned = object.material_slots?.[slot]
        const current = resolved.material.kind === 'pattern' ? resolved.material.id : resolved.material.kind === 'principled' ? principledID : (assigned?.preset ?? '')
        const entry = materialEntry(current)
        const params = resolved.material.kind === 'unknown' ? {} : resolved.material.params
        const expanded = open === slot
        return (
          <div key={slot} className="cc-scene-slot" aria-label={`槽位 ${slot}`}>
            <div className="cc-scene-slot-head">
              <button type="button" className="cc-scene-slot-name" aria-expanded={expanded} onClick={() => setOpen(expanded ? null : slot)}>
                {slot}
                {hint ? `（${hint}）` : ''}
              </button>
              {sourceLabel[resolved.source] && <span className={`cc-scene-slot-source is-${resolved.source}`}>{sourceLabel[resolved.source]}</span>}
            </div>
            <label className="cc-scene-field">
              <span>材质</span>
              <select
                aria-label={`${slot} 的材质`}
                value={entry ? current : ''}
                onChange={(e) => edit(slot, `设置「${name}」材质`, () => ({ preset: e.target.value, params: {} }))}
              >
                {!entry && <option value="">未知材质（{assigned?.preset}）</option>}
                {materialCatalog.materials.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.label}
                  </option>
                ))}
              </select>
            </label>
            <div className="cc-scene-slot-tools">
              <button type="button" className="btn" title="复制这个槽位的材质" onClick={() => {
                // What is assigned goes as stored; a hinted or model material as it resolves.
                const a = assigned ?? assignmentOf(resolved)
                if (a) writeClipboard(storage, a)
                setNotice(a ? '已复制材质' : '未知材质不能复制')
              }}>
                复制材质
              </button>
              <button type="button" className="btn" title="把复制的材质粘贴到这个槽位" onClick={() => {
                const m = readClipboard(storage)
                if (!m) return setNotice('还没有复制材质')
                edit(slot, `粘贴材质到「${name}」`, () => structuredClone(m))
                setNotice('')
              }}>
                粘贴材质
              </button>
              {entry && entry.kind === 'pattern' && (
                <button type="button" className="btn" onClick={() => {
                  const a = assignmentOf(resolved)
                  if (!a) return
                  const { slot: converted, lost } = toPrincipled(a)
                  edit(slot, `「${name}」材质转为原理化`, () => converted)
                  setNotice(lost ? '已转为原理化 BSDF，图案不保留' : '已转为原理化 BSDF')
                }}>
                  转为原理化
                </button>
              )}
            </div>
            {expanded && entry && (
              <div className="cc-scene-slot-params">
                {Object.entries(entry.params).map(([k, spec]) => (
                  <ParamField
                    key={k}
                    name={k}
                    spec={inDegrees(k) ? degreeSpec(spec) : spec}
                    value={inDegrees(k) ? MathUtils.radToDeg(params[k] as number) : params[k]}
                    disabled={readOnly}
                    onCommit={(raw) => {
                      const v = inDegrees(k) ? MathUtils.degToRad(raw as number) : raw
                      edit(
                        slot,
                        `调整「${name}」材质`,
                        (o, cur) => {
                          const base = cur ?? assignmentOf(resolvedSlot(o, slot, hintOf(o, slot), constantsOf(o, slot)))
                          // Each object keeps its own material; one without this parameter stays as it is.
                          if (!base || !materialEntry(base.preset)?.params[k]) return cur
                          return { ...base, params: { ...(base.params ?? {}), [k]: v } }
                        },
                        `${object.id}:slot.${slot}.${k}`,
                      )
                    }}
                  />
                ))}
              </div>
            )}
          </div>
        )
      })}
    </fieldset>
  )
}
