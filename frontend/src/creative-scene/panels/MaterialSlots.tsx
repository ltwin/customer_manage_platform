import { useEffect, useMemo, useState } from 'react'
import { MathUtils } from 'three'
import { useStore } from 'zustand'
import type { components } from '../../api/schema'
import type { ObjectUpdate, SceneObject } from '../api.ts'
import { builtinMaterial } from '../../creative-canvas/assets/builtin.ts'
import type { ParamSpec } from '../builtins/catalog.ts'
import { builtinSource, materialCatalog, materialEntry, principledID } from '../materials/catalog.ts'
import { imageStatus, textureSources, type TextureKey } from '../materials/images.ts'
import { assignmentOf, readClipboard, setSlot, shownSlot, slotsOf, toPrincipled, writeClipboard, type MaterialSlot } from '../materials/slotEdits.ts'
import { modelCache, type MeshConstants, type SceneModel } from '../models/cache.ts'
import { applyBuiltinFaces } from '../models/faces.ts'
import { primitiveGeometry } from '../builtins/geometry.ts'
import type { DisplayMode } from '../state/sceneStore.ts'
import { ParamField } from './Properties.tsx'
import { MaterialPicker } from './LibraryPicker.tsx'
import { TextureFields } from './TextureFields.tsx'

type SceneImage = components['schemas']['CreativeSceneImage']

const sourceLabel = { assigned: '', hint: '按提示', model: '模型自带', default: '默认', unknown: '未知材质' } as const
const originLabel = '沿用原槽位'

/** What face edit adds to the section (design「面编辑与指定」); absent outside it. */
export type FaceSlots = {
  /** Triangles selected now. */
  selected: number
  /** Slots 「+」 made, not saved until faces are assigned to them. */
  added: readonly string[]
  /** Other instances of the same model: assigning asks whether they change too. */
  shared: number
  assign(slot: string, applyToAll: boolean): void
  select(slot: string): void
  deselect(slot: string): void
  add(): void
}

/** Material assets on the slots (C5c1 design「材质资产的存入与使用」). */
export type SlotLibrary = {
  /** The material asset being dragged from the asset panel: the slot it is let go on takes a copy. */
  dragging: { title: string; slot: MaterialSlot } | null
  /** 存为材质资产; absent where the page cannot write the library. */
  save?: {
    /** Why it cannot run on this slot now, or null. */
    blocked(slot: string): string | null
    run(slot: string): void
  }
}

/** anisotropy_rotation is stored in radians and shown in degrees, like a spot light's angle. */
const inDegrees = (k: string) => k === 'anisotropy_rotation'
const degreeSpec = (spec: ParamSpec): ParamSpec => ({ ...spec, min: -180, max: 180, step: 1, unit: '°', default: 0 })

/**
 * The material section (design「属性栏与复制材质」): each slot of the active
 * object with where its material comes from, the material, and its fields.
 * With several objects selected, an edit goes to every one with a slot of
 * the same name.
 */
export function MaterialSlots({ object, model, objects, selected, models, images, canvasID, readOnly, display, storage, onUpdates, faces, library }: {
  object: SceneObject
  model: SceneModel | undefined
  objects: readonly SceneObject[]
  selected: readonly string[]
  models: Readonly<Record<string, SceneModel>>
  /** The scene's images as its last read described them: one not usable is flagged on its slot. */
  images: Readonly<Record<string, SceneImage>> | undefined
  /** The canvas the scene sits on: its images are offered for textures. */
  canvasID: string | undefined
  readOnly: boolean
  display: DisplayMode
  storage: Storage | undefined
  onUpdates: (updates: ObjectUpdate[], label: string, merge?: string) => void
  faces?: FaceSlots
  library?: SlotLibrary
}) {
  const slots = slotsOf(object, model)
  const builtinKey = object.kind === 'primitive' ? JSON.stringify([object.primitive?.builtin, object.primitive?.params, object.primitive?.faces]) : ''
  const revision = object.model?.content_revision_id
  // A model whose parts come in after the panel: stale is read again then.
  const [partsIn, setPartsIn] = useState<string | null>(null)
  useEffect(() => {
    if (!revision || model?.usable === false || modelCache.peek(revision) !== undefined) return
    let live = true
    modelCache.load(revision, model?.faces).then(
      () => live && setPartsIn(revision),
      () => undefined,
    )
    return () => {
      live = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- a revision's faces never change
  }, [revision, model?.usable])
  const stale = useMemo(() => {
    if (object.kind === 'primitive') {
      const geometry = object.primitive?.faces ? primitiveGeometry(object.primitive.builtin, object.primitive.params) : null
      const out = geometry ? applyBuiltinFaces(geometry, object.primitive?.faces).stale : false
      geometry?.dispose()
      return out
    }
    return !!(revision && (modelCache.peek(revision)?.stale ?? 0) > 0)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- builtinKey stands for the builtin's geometry and faces, partsIn for the model's parts
  }, [builtinKey, revision, partsIn])
  const loading = useStore(imageStatus)
  const [open, setOpen] = useState<string | null>(slots.length === 1 ? slots[0].slot : null)
  const [notice, setNotice] = useState('')
  /** A shared model's slot waiting for 「只改这个 / 所有实例」. */
  const [asking, setAsking] = useState<string | null>(null)
  /** The slot 「材质资产…」 is choosing for. */
  const [choosing, setChoosing] = useState<string | null>(null)
  /** The slot a dragged material is over. */
  const [dropOn, setDropOn] = useState<string | null>(null)
  // In face edit a model without slots still needs 「+ 新槽位」 for its first.
  if (!slots.length && !faces) return null
  const name = object.name || '物件'
  const slotNames = (o: SceneObject) => slotsOf(o, o.model?.content_revision_id ? models[o.model.content_revision_id] : undefined).map((s) => s.slot)
  const constantsOf = (o: SceneObject, slot: string): MeshConstants | null => {
    const rev = o.model?.content_revision_id
    return (rev && modelCache.peek(rev)?.meshes.find((m) => m.slot === slot)?.constants) || null
  }
  // A slot a face assignment brought draws as the slot its faces came from
  // until it has a material of its own (materials/resolve.ts resolvePart).
  const originOf = (o: SceneObject, slot: string): { slot: string | null; hint: string | null } | null => {
    if (o.kind === 'primitive') return { slot: 'main', hint: null }
    const rev = o.model?.content_revision_id
    return (rev && modelCache.peek(rev)?.meshes.find((m) => m.slot === slot && m.origin)?.origin) || null
  }
  /** A slot of o as it draws, shown and edited alike (slotEdits.ts shownSlot). */
  const drawn = (o: SceneObject, slot: string) => {
    const s = slotsOf(o, o.model?.content_revision_id ? models[o.model.content_revision_id] : undefined).find((x) => x.slot === slot) ?? { slot, hint: null, faces: false }
    return shownSlot(o, s, s.faces ? originOf(o, slot) : null, constantsOf(o, slot))
  }
  const edit = (slot: string, label: string, next: (o: SceneObject, current: MaterialSlot | null) => MaterialSlot | null, merge?: string) => {
    const updates = setSlot(objects, selected, object.id, slot, slotNames, next)
    if (updates.length) onUpdates(updates, label, merge)
  }
  /** Why an image cannot be drawn: the server says it is gone, or this page could not load it or had no room for it (images.ts). */
  const unusable = (revision: string): string | null => {
    if (images?.[revision]?.usable === false) return '图片不可用：已被删除或无权读取'
    // By the textures' own revisions. Their messages name revisions for an export's failure (host/render.ts), not for the panel.
    const uses = (key: TextureKey) => textureSources(key).includes(revision)
    if (Object.keys(loading.failed).some(uses)) return '图片加载失败，暂按材质参数显示'
    if (loading.over.some(uses)) return '贴图超出显存预算，暂按材质参数显示'
    return null
  }
  /** Why a built-in material's textures (C5c2) do not draw: one did not load (say the library is not imported), or there was no room. */
  const builtinUnusable = (id: string): string | null => {
    const sources = Object.values(materialEntry(id)?.maps ?? {}).map((t) => builtinSource(t.file))
    const uses = (key: TextureKey) => textureSources(key).some((s) => sources.includes(s))
    if (Object.keys(loading.failed).some(uses)) return '内置贴图加载失败，暂按材质参数显示'
    if (loading.over.some(uses)) return '贴图超出显存预算，暂按材质参数显示'
    return null
  }
  /** Whether a slot's geometry has UVs: a primitive's always; a model's once its parts are in. */
  const hasUV = (slot: string): boolean | null => {
    if (object.kind === 'primitive') return true
    const parts = revision ? modelCache.peek(revision)?.meshes.filter((p) => p.slot === slot) : undefined
    return parts?.length ? parts.every((p) => 'uv' in p.geometry.attributes) : null
  }
  /** A material asset's slot, copied onto this slot (and the selection's slots of the same name). */
  const applyMaterial = (slot: string, title: string, m: MaterialSlot) => edit(slot, `用材质「${title}」（「${name}」${slot}）`, () => structuredClone(m))
  const dropping = !readOnly && !!library?.dragging
  const assign = (slot: string) => {
    if (!faces) return
    if (faces.shared > 0) return setAsking(slot)
    faces.assign(slot, false)
  }
  /** 指定 / 选择 / 取消选择 on a slot, and the shared model's question. */
  const faceTools = (slot: string, saved: boolean) =>
    faces && (
      <>
        <div className="cc-scene-slot-tools" aria-label={`${slot} 的面`}>
          <button type="button" className="btn" disabled={!faces.selected} title="把选中的面指定到这个槽位" onClick={() => assign(slot)}>
            指定
          </button>
          {saved && (
            <>
              <button type="button" className="btn" title="选中这个槽位的面" onClick={() => faces.select(slot)}>
                选择
              </button>
              <button type="button" className="btn" title="从选区里去掉这个槽位的面" onClick={() => faces.deselect(slot)}>
                取消选择
              </button>
            </>
          )}
        </div>
        {asking === slot && (
          <div className="cc-scene-shared" role="group" aria-label="共用模型">
            <span>与 {faces.shared} 个实例共用</span>
            <button type="button" className="btn" onClick={() => (setAsking(null), faces.assign(slot, false))}>
              只改这个
            </button>
            <button type="button" className="btn" onClick={() => (setAsking(null), faces.assign(slot, true))}>
              所有实例
            </button>
          </div>
        )}
      </>
    )
  return (
    <fieldset className="cc-scene-materials" disabled={readOnly} aria-label="材质">
      <legend>材质</legend>
      {display !== 'preview' && display !== 'rendered' && <small>材质在「材质预览」和「最终渲染」里显示</small>}
      {notice && <small role="status">{notice}</small>}
      {stale && <small role="status">部分面分配已失效：网格结构变了，这些面按模型原来的槽位显示</small>}
      {slots.map(({ slot, hint, faces }) => {
        const assigned = object.material_slots?.[slot]
        const origin = faces && !assigned ? originOf(object, slot) : null
        const resolved = drawn(object, slot)
        const current =
          resolved.material.kind === 'pattern' ? resolved.material.id : resolved.material.kind === 'principled' ? (resolved.material.preset ?? principledID) : (assigned?.preset ?? '')
        const entry = materialEntry(current)
        // A built-in textured material (C5c2): its name and ball over the list, its mapping and alpha below, no texture inputs.
        const builtin = entry?.kind === 'textured' ? builtinMaterial(current) : undefined
        const params = resolved.material.kind === 'unknown' ? {} : resolved.material.params
        const expanded = open === slot
        return (
          <div
            key={slot}
            className={`cc-scene-slot${dropping && dropOn === slot ? ' is-drop' : ''}`}
            aria-label={`槽位 ${slot}`}
            onDragOver={(e) => {
              if (!dropping || !e.dataTransfer.types.includes('application/creative-asset+json')) return
              e.preventDefault()
              e.dataTransfer.dropEffect = 'copy'
              setDropOn(slot)
            }}
            onDragLeave={(e) => {
              if (!(e.relatedTarget instanceof Node && e.currentTarget.contains(e.relatedTarget))) setDropOn(null)
            }}
            onDrop={(e) => {
              setDropOn(null)
              const dragged = library?.dragging
              if (!dropping || !dragged) return
              e.preventDefault()
              applyMaterial(slot, dragged.title, dragged.slot)
            }}
          >
            <div className="cc-scene-slot-head">
              <button type="button" className="cc-scene-slot-name" aria-expanded={expanded} onClick={() => setOpen(expanded ? null : slot)}>
                {slot}
                {hint ? `（${hint}）` : ''}
              </button>
              {origin ? <span className="cc-scene-slot-source is-origin">{originLabel}</span> : sourceLabel[resolved.source] && <span className={`cc-scene-slot-source is-${resolved.source}`}>{sourceLabel[resolved.source]}</span>}
            </div>
            {faceTools(slot, true)}
            {builtin && (
              <div className="cc-scene-builtin" aria-label={`${slot} 的内置材质`}>
                <img src={builtin.thumbnail} alt="" />
                <span>
                  <strong>{builtin.label}</strong>
                  <small>
                    内置材质 · {builtin.categoryLabel} · {builtin.sizeM} 米一块
                  </small>
                </span>
              </div>
            )}
            {builtin && builtinUnusable(current) && <small role="status">{builtinUnusable(current)}</small>}
            <label className="cc-scene-field">
              <span>材质</span>
              <select
                aria-label={`${slot} 的材质`}
                value={entry ? current : ''}
                onChange={(e) => edit(slot, `设置「${name}」材质`, () => ({ preset: e.target.value, params: {} }))}
              >
                {!entry && <option value="">未知材质（{assigned?.preset}）</option>}
                {builtin && <option value={current}>内置材质</option>}
                {materialCatalog.materials.filter((m) => m.kind !== 'textured').map((m) => (
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
              {library && (
                <>
                  <button type="button" className="btn" title="从资产库选一个材质用到这个槽位" disabled={readOnly} onClick={() => setChoosing(slot)}>
                    材质资产…
                  </button>
                  {library.save && (
                    <button type="button" className="btn" title={library.save.blocked(slot) ?? '把这个槽位的材质存进资产库，可以用到别的场景'} disabled={!!library.save.blocked(slot)} onClick={() => library.save?.run(slot)}>
                      存为材质资产…
                    </button>
                  )}
                </>
              )}
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
            {expanded && (entry?.kind === 'principled' || builtin) && (
              <TextureFields
                slot={slot}
                m={assignmentOf(resolved) ?? { preset: principledID, params: {} }}
                builtin={!!builtin}
                hasUV={hasUV(slot)}
                unusable={unusable}
                canvasID={canvasID}
                disabled={readOnly}
                onEdit={(label, change, merge) =>
                  edit(
                    slot,
                    `${label}（「${name}」）`,
                    (o, cur) => {
                      const base = cur ?? assignmentOf(drawn(o, slot))
                      // A selected object with another material in this slot keeps it.
                      return base && base.preset === current ? change(base) : cur
                    },
                    merge && `${object.id}:slot.${merge}`,
                  )
                }
              />
            )}
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
                          const base = cur ?? assignmentOf(drawn(o, slot))
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
      {faces?.added.map((slot) => (
        <div key={slot} className="cc-scene-slot" aria-label={`槽位 ${slot}`}>
          <div className="cc-scene-slot-head">
            <span className="cc-scene-slot-name">{slot}</span>
            <span className="cc-scene-slot-source">新槽位，指定后保存</span>
          </div>
          {faceTools(slot, false)}
        </div>
      ))}
      {faces && (
        <button type="button" className="btn" title="新建一个槽位，再把选中的面指定过去" onClick={faces.add}>
          + 新槽位
        </button>
      )}
      {choosing && (
        <MaterialPicker
          title={`选择用到 ${choosing} 的材质`}
          onClose={() => setChoosing(null)}
          onPick={(picked) => {
            const slot = choosing
            setChoosing(null)
            applyMaterial(slot, picked.title, picked.slot)
          }}
        />
      )}
    </fieldset>
  )
}
