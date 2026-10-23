import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Euler, MathUtils, Quaternion } from 'three'
import type { ObjectUpdate, SceneObject } from '../api.ts'
import { builtinSpec, type ParamSpec } from '../builtins/catalog.ts'
import { aspectOf, equivalentFocal, fStopShortcuts, fStopThirds, presetOf, sensorPresets, sharpRange } from '../cameraOptics.ts'
import { cameraDefaults } from '../state/apply.ts'
import type { SceneModel } from '../models/cache.ts'
import { transformToDoc, transformToView } from '../space.ts'
import { ev100, isoThirds, shutterLabel, shutterThirds } from '../world/exposure.ts'

/**
 * A number that commits 300 ms after typing stops, on Enter or on blur, and
 * reverts on Esc. Only text the photographer changed is committed, so
 * focusing a field never writes back its rounded display.
 */
export function NumberField({ label, value, step, min, max, disabled, onCommit }: { label: string; value: number; step?: number; min?: number; max?: number; disabled?: boolean; onCommit: (v: number) => void }) {
  const [text, setText] = useState(String(round(value)))
  const [editing, setEditing] = useState(false)
  const timer = useRef<number | null>(null)
  const dirty = useRef(false)
  const latest = useRef({ text, value, min, max, onCommit })
  latest.current = { text, value, min, max, onCommit }
  useEffect(() => {
    if (!editing) setText(String(round(value)))
  }, [value, editing])
  /** Commits typed text; returns the committed number, or null for nothing to commit. */
  const commit = useCallback((raw: string): number | null => {
    const l = latest.current
    const v = Number(raw)
    if (!dirty.current || raw.trim() === '' || !Number.isFinite(v)) return null
    dirty.current = false
    const clamped = Math.min(l.max ?? Infinity, Math.max(l.min ?? -Infinity, v))
    if (clamped !== l.value) l.onCommit(clamped)
    return clamped
  }, [])
  const stopTimer = () => {
    if (timer.current !== null) window.clearTimeout(timer.current)
    timer.current = null
  }
  // A field closing mid-wait (selection change, editor closing) still saves.
  useEffect(
    () => () => {
      if (timer.current === null) return
      window.clearTimeout(timer.current)
      commit(latest.current.text)
    },
    [commit],
  )
  const finish = () => {
    stopTimer()
    setEditing(false)
    const done = commit(text)
    setText(String(round(done ?? latest.current.value)))
  }
  return (
    <label className="cc-scene-field">
      <span>{label}</span>
      <input
        type="number"
        inputMode="decimal"
        step={step ?? 'any'}
        min={min}
        max={max}
        value={text}
        disabled={disabled}
        onFocus={() => setEditing(true)}
        onChange={(e) => {
          const next = e.target.value
          setText(next)
          dirty.current = true
          stopTimer()
          timer.current = window.setTimeout(() => {
            timer.current = null
            commit(next)
          }, 300)
        }}
        onBlur={finish}
        onKeyDown={(e) => {
          if (e.key === 'Enter') e.currentTarget.blur()
          if (e.key === 'Escape') {
            stopTimer()
            dirty.current = false
            setText(String(round(value)))
            setEditing(false)
            e.currentTarget.blur()
            e.stopPropagation()
          }
        }}
      />
    </label>
  )
}

const round = (v: number) => Math.round(v * 1000) / 1000

function TextField({ value, disabled, onCommit }: { value: string; disabled?: boolean; onCommit: (v: string) => void }) {
  const [text, setText] = useState(value)
  useEffect(() => setText(value), [value])
  return (
    <input
      className="cc-scene-name"
      value={text}
      maxLength={80}
      disabled={disabled}
      aria-label="名称"
      onChange={(e) => setText(e.target.value)}
      onBlur={() => text !== value && onCommit(text)}
      onKeyDown={(e) => {
        if (e.key === 'Enter') e.currentTarget.blur()
        if (e.key === 'Escape') {
          setText(value)
          e.stopPropagation()
        }
      }}
    />
  )
}

export function ParamField({ name, spec, value, disabled, onCommit }: { name: string; spec: ParamSpec; value: unknown; disabled: boolean; onCommit: (v: unknown) => void }) {
  if (spec.type === 'boolean')
    return (
      <label className="cc-scene-field">
        <span>{spec.label ?? name}</span>
        <input type="checkbox" checked={value === true} disabled={disabled} onChange={(e) => onCommit(e.target.checked)} />
      </label>
    )
  if (spec.type === 'enum')
    return (
      <label className="cc-scene-field">
        <span>{spec.label ?? name}</span>
        <select value={String(value)} disabled={disabled} onChange={(e) => onCommit(e.target.value)}>
          {(spec.options ?? []).map((o) => (
            <option key={o}>{o}</option>
          ))}
        </select>
      </label>
    )
  if (spec.type === 'color')
    return (
      <label className="cc-scene-field">
        <span>{spec.label ?? name}</span>
        <input type="color" value={String(value)} disabled={disabled} onChange={(e) => onCommit(e.target.value)} />
      </label>
    )
  return (
    <NumberField
      label={`${spec.label ?? name}${spec.unit ? `（${spec.unit}）` : ''}`}
      value={typeof value === 'number' ? value : Number(spec.default)}
      step={spec.step}
      min={spec.min}
      max={spec.max}
      disabled={disabled}
      onCommit={(v) => onCommit(spec.type === 'integer' ? Math.round(v) : v)}
    />
  )
}

/** The active object's fields; each change is one update. */
/**
 * Field commits carry a merge key, so typing in one field is one undo step
 * even when its commits wait to be sent together.
 */
export function Properties({ object, count, readOnly, model, modelFailed = false, onUpdate, objects = [], physical = false, focusDistance, picking = false, onPickFocus, save, children }: {
  object: SceneObject | null
  count: number
  readOnly: boolean
  /** The sky is physical: a camera's exposure counts (W2). */
  physical?: boolean
  /** The scene's objects, for a camera's focus list. */
  objects?: readonly SceneObject[]
  /** Where the active camera focuses, resolved from its focus object. */
  focusDistance?: number
  /** The viewport waits for a click on the focus object. */
  picking?: boolean
  onPickFocus?: () => void
  /** The model a model instance shows, when known. */
  model?: SceneModel
  /** Its GLB could not be loaded. */
  modelFailed?: boolean
  onUpdate: (u: ObjectUpdate, label: string, merge?: string) => void
  /** 「存入资产库」 for a model instance, with why it cannot be saved now; absent without a library. */
  save?: { blocked: string | null; run: () => void }
  /** Type-specific controls below the common ones, such as a model's parameters. */
  children?: ReactNode
}) {
  if (!object) return <div className="cc-scene-props is-empty">{count > 1 ? `已选中 ${count} 个物件` : '选中物件后在这里调整'}</div>
  // Shown and typed in Blender's axes (Z up); stored in the document's.
  const t = transformToView(object.transform)
  const euler = new Euler().setFromQuaternion(new Quaternion(...t.rotation), 'XYZ')
  const degrees = [euler.x, euler.y, euler.z].map((r) => MathUtils.radToDeg(r))
  const locked = readOnly || object.locked
  const setTransform = (part: 'position' | 'rotation' | 'scale', axis: number, v: number) => {
    const next: { position: number[]; rotation: number[]; scale: number[] } = { position: [...t.position], rotation: [...t.rotation], scale: [...t.scale] }
    if (part === 'rotation') {
      const d = [...degrees]
      d[axis] = v
      const [x, y, z] = d.map((deg) => MathUtils.degToRad(deg))
      const q = new Quaternion().setFromEuler(new Euler(x, y, z, 'XYZ'))
      next.rotation = [q.x, q.y, q.z, q.w]
    } else next[part][axis] = v
    onUpdate({ object_id: object.id, transform: transformToDoc(next) }, `调整「${object.name || '物件'}」`, `${object.id}:${part}.${axis}`)
  }
  const spec = object.primitive ? builtinSpec(object.primitive.builtin) : undefined
  return (
    <div key={object.id} className="cc-scene-props" aria-label="属性">
      <TextField value={object.name} disabled={readOnly} onCommit={(name) => onUpdate({ object_id: object.id, name }, '重命名', `${object.id}:name`)} />
      {count > 1 && <small>共选中 {count} 个物件，这里显示活动物件</small>}
      {(['position', 'rotation', 'scale'] as const).map((part) => (
        <fieldset key={part} disabled={locked}>
          <legend>{part === 'position' ? '位置（m）' : part === 'rotation' ? '旋转（°）' : '缩放'}</legend>
          {['X', 'Y', 'Z'].map((axis, i) => (
            <NumberField
              key={axis}
              label={axis}
              value={part === 'rotation' ? degrees[i] : (t[part][i] ?? 0)}
              step={part === 'scale' ? 0.1 : part === 'rotation' ? 1 : 0.1}
              min={part === 'scale' ? 0.001 : part === 'position' ? -100000 : undefined}
              max={part === 'scale' ? 1000 : part === 'position' ? 100000 : undefined}
              disabled={locked}
              onCommit={(v) => setTransform(part, i, v)}
            />
          ))}
        </fieldset>
      ))}
      <div className="cc-scene-flags">
        <label>
          <input type="checkbox" checked={object.visible} disabled={readOnly} onChange={(e) => onUpdate({ object_id: object.id, visible: e.target.checked }, e.target.checked ? '显示' : '隐藏')} />
          显示
        </label>
        <label>
          <input type="checkbox" checked={object.locked} disabled={readOnly} onChange={(e) => onUpdate({ object_id: object.id, locked: e.target.checked }, e.target.checked ? '锁定' : '解锁')} />
          锁定
        </label>
      </div>
      {spec && (
        <fieldset disabled={readOnly}>
          <legend>{spec.label}参数</legend>
          {Object.entries(spec.params).map(([name, p]) => (
            <ParamField
              key={name}
              name={name}
              spec={p}
              value={object.primitive?.params?.[name]}
              disabled={readOnly}
              onCommit={(v) => onUpdate({ object_id: object.id, primitive: { params: { [name]: v } } }, `调整「${object.name || spec.label}」参数`, `${object.id}:param.${name}`)}
            />
          ))}
        </fieldset>
      )}
      {object.kind === 'model_instance' && (
        <div className="cc-scene-model-info">
          <span>代码模型{model?.name_hint ? `：${model.name_hint}` : ''}</span>
          {(model?.usable === false || modelFailed) && <strong role="status">模型不可用</strong>}
        </div>
      )}
      {object.kind === 'model_instance' && save && (
        <div className="cc-scene-model-save">
          <button type="button" className="btn" disabled={!!save.blocked} onClick={save.run}>
            存入资产库…
          </button>
          {save.blocked && <span>{save.blocked}</span>}
        </div>
      )}
      {children}
      {object.camera && (
        <CameraFields
          object={object}
          objects={objects}
          readOnly={readOnly}
          physical={physical}
          focusDistance={focusDistance}
          picking={picking}
          onPickFocus={onPickFocus}
          onCamera={(camera, label, merge) => onUpdate({ object_id: object.id, camera }, label, merge ? `${object.id}:camera.${merge}` : undefined)}
          onHeight={(y) => onUpdate({ object_id: object.id, transform: { ...object.transform, position: [object.transform.position[0], y, object.transform.position[2]] } }, `调整「${object.name || '摄像机'}」高度`, `${object.id}:position.height`)}
        />
      )}
    </div>
  )
}

const aspects = ['3:2', '2:3', '4:3', '3:4', '16:9', '9:16', '1:1', '4:5', '5:4', '21:9']
const focusKinds: readonly SceneObject['kind'][] = ['primitive', 'model_instance', 'group']
const metres = (m: number) => (m === Infinity ? '∞' : `${m < 10 ? m.toFixed(2) : m.toFixed(1)} m`)

/**
 * A camera's lens, depth of field and sensor (design「摄像机与相机视图」).
 * Every change is one update; sliders merge their commits like the number
 * fields do.
 */
function CameraFields({ object, objects, readOnly, physical, focusDistance, picking, onPickFocus, onCamera, onHeight }: {
  object: SceneObject
  objects: readonly SceneObject[]
  readOnly: boolean
  physical: boolean
  focusDistance?: number
  picking: boolean
  onPickFocus?: () => void
  onCamera: (camera: NonNullable<ObjectUpdate['camera']>, label: string, merge?: string) => void
  onHeight: (y: number) => void
}) {
  const [more, setMore] = useState(false)
  const c = { ...cameraDefaults, ...object.camera }
  const name = object.name || '摄像机'
  const target = c.focus_object_id ? objects.find((o) => o.id === c.focus_object_id) : undefined
  const byObject = c.focus_object_id !== ''
  const distance = focusDistance ?? c.focus
  const range = sharpRange(c.focal_mm, c.f_stop, distance, c.sensor_mm)
  const preset = presetOf(c.sensor_mm)
  const candidates = objects.filter((o) => focusKinds.includes(o.kind) && o.primitive?.builtin !== 'ground@1')
  const stopIndex = fStopThirds.reduce((best, f, i) => (Math.abs(f - c.f_stop) < Math.abs(fStopThirds[best] - c.f_stop) ? i : best), 0)
  return (
    <fieldset className="cc-scene-camera" disabled={readOnly} aria-label="摄像机">
      <legend>摄像机</legend>
      <label className="cc-scene-field">
        <span>标签</span>
        <input defaultValue={c.label} key={c.label} maxLength={40} onBlur={(e) => e.target.value !== c.label && onCamera({ label: e.target.value }, `调整「${name}」标签`)} />
      </label>
      <NumberField label="焦距（mm）" value={c.focal_mm} step={1} min={8} max={800} onCommit={(v) => onCamera({ focal_mm: v }, `调整「${name}」焦距`, 'focal')} />
      <small>等效全画幅 {Math.round(equivalentFocal(c.focal_mm, c.sensor_mm) * 10) / 10} mm</small>
      <label className="cc-scene-field">
        <span>画幅</span>
        <select aria-label="画幅" value={c.aspect} onChange={(e) => onCamera({ aspect: e.target.value }, `调整「${name}」画幅`)}>
          {(aspects.includes(c.aspect) ? aspects : [c.aspect, ...aspects]).map((a) => (
            <option key={a} value={a}>
              {a}
              {aspectOf(a) < 1 ? '（竖）' : ''}
            </option>
          ))}
        </select>
      </label>
      <NumberField label="高度（m）" value={object.transform.position[1]} step={0.1} min={-100000} max={100000} disabled={readOnly || object.locked} onCommit={onHeight} />

      <label className="cc-scene-field">
        <span>景深</span>
        <input type="checkbox" checked={c.depth_of_field} onChange={(e) => onCamera({ depth_of_field: e.target.checked }, `${e.target.checked ? '打开' : '关闭'}「${name}」景深`)} />
      </label>
      {c.depth_of_field && (
        <>
          <label className="cc-scene-field">
            <span>光圈 f/{c.f_stop}</span>
            <input
              type="range"
              min={0}
              max={fStopThirds.length - 1}
              step={1}
              value={stopIndex}
              aria-label="光圈"
              onChange={(e) => onCamera({ f_stop: fStopThirds[Number(e.target.value)] }, `调整「${name}」光圈`, 'f_stop')}
            />
          </label>
          <div className="cc-scene-stops" role="group" aria-label="常用光圈">
            {fStopShortcuts.map((f) => (
              <button key={f} type="button" className="btn" aria-pressed={c.f_stop === f} onClick={() => onCamera({ f_stop: f }, `调整「${name}」光圈`)}>
                f/{f}
              </button>
            ))}
          </div>
          <div className="cc-scene-field" role="radiogroup" aria-label="对焦方式">
            <span>对焦</span>
            <label>
              <input type="radio" checked={!byObject} onChange={() => onCamera({ focus_object_id: '', focus: distance }, `「${name}」按距离对焦`)} />
              距离
            </label>
            <label>
              <input type="radio" checked={byObject} disabled={!candidates.length} onChange={() => candidates[0] && onCamera({ focus_object_id: candidates[0].id }, `「${name}」对焦到物件`)} />
              物件
            </label>
          </div>
          {byObject ? (
            <div className="cc-scene-field">
              <select value={c.focus_object_id} aria-label="对焦物件" onChange={(e) => onCamera({ focus_object_id: e.target.value }, `「${name}」对焦到物件`)}>
                {!target && <option value={c.focus_object_id}>（已删除）</option>}
                {candidates.map((o) => (
                  <option key={o.id} value={o.id}>
                    {o.name || o.id}
                  </option>
                ))}
              </select>
              <button type="button" className="btn" aria-pressed={picking} onClick={onPickFocus}>
                {picking ? '点选中…' : '拾取'}
              </button>
            </div>
          ) : (
            <NumberField label="对焦距离（m）" value={c.focus} step={0.1} min={0.01} onCommit={(v) => onCamera({ focus: v }, `调整「${name}」对焦距离`, 'focus')} />
          )}
          {byObject && !target && <small role="status">对焦物件已删除，按 {metres(c.focus)} 对焦</small>}
          <small>
            对焦 {metres(distance)} · 清晰范围 {metres(range.near)} – {metres(range.far)}
          </small>
        </>
      )}
      <CameraExposure camera={c} name={name} physical={physical} onCamera={onCamera} />
      <button type="button" className="btn cc-scene-more" aria-expanded={more} onClick={() => setMore(!more)}>
        {more ? '收起' : '更多'}
      </button>
      {more && (
        <>
          <label className="cc-scene-field">
            <span>传感器</span>
            <select
              aria-label="传感器预设"
              value={preset ?? 'custom'}
              onChange={(e) => {
                const p = sensorPresets.find((x) => x.label === e.target.value)
                if (p) onCamera({ sensor_mm: [...p.size] }, `「${name}」传感器改为${p.label}`)
              }}
            >
              {sensorPresets.map((p) => (
                <option key={p.label} value={p.label}>
                  {p.label}
                </option>
              ))}
              {!preset && <option value="custom">自定义</option>}
            </select>
          </label>
          <NumberField label="传感器宽（mm）" value={c.sensor_mm[0]} step={0.1} min={1} max={100} onCommit={(v) => onCamera({ sensor_mm: [v, c.sensor_mm[1]] }, `调整「${name}」传感器`, 'sensor')} />
          <NumberField label="传感器高（mm）" value={c.sensor_mm[1]} step={0.1} min={1} max={100} onCommit={(v) => onCamera({ sensor_mm: [c.sensor_mm[0], v] }, `调整「${name}」传感器`, 'sensor')} />
          <NumberField label="near（m）" value={c.near} step={0.01} min={0.001} onCommit={(v) => onCamera({ near: v }, `调整「${name}」近裁剪`, 'near')} />
          <NumberField label="far（m）" value={c.far} step={10} min={0.01} onCommit={(v) => onCamera({ far: v }, `调整「${name}」远裁剪`, 'far')} />
        </>
      )}
    </fieldset>
  )
}

/** A stop list with the current value in it, should the assistant have written one between stops. */
const withCurrent = (stops: readonly number[], v: number) => (stops.includes(v) ? stops : [...stops, v].sort((a, b) => a - b))

/**
 * A camera's exposure under a physical sky (W2 design「编辑界面」, Epic
 * revision v5): ambient metering with a compensation, or by hand at ISO, shutter
 * and aperture in thirds of a stop with the EV100 they make. The aperture is
 * the depth of field's too, set here with the depth of field off; under the
 * ambient metering it only blurs. Greyed under any other sky, where the
 * scene's compensation exposes.
 */
function CameraExposure({ camera, name, physical, onCamera }: {
  camera: typeof cameraDefaults
  name: string
  physical: boolean
  onCamera: (camera: NonNullable<ObjectUpdate['camera']>, label: string, merge?: string) => void
}) {
  const manual = camera.exposure_mode === 'manual'
  return (
    <fieldset className="cc-scene-camera-exposure" disabled={!physical} aria-label="曝光">
      <legend>曝光</legend>
      <div className="cc-scene-segmented" role="group" aria-label="曝光模式">
        <button type="button" aria-pressed={!manual} onClick={() => manual && onCamera({ exposure_mode: 'ambient' }, `「${name}」改为环境测光`)}>
          环境测光
        </button>
        <button type="button" aria-pressed={manual} onClick={() => !manual && onCamera({ exposure_mode: 'manual' }, `「${name}」改为手动曝光`)}>
          手动
        </button>
      </div>
      {!manual && (
        <>
          <NumberField label="曝光补偿（档）" value={camera.exposure_compensation} min={-5} max={5} step={1 / 3} onCommit={(v) => onCamera({ exposure_compensation: v }, `调整「${name}」曝光补偿`, 'exposure_compensation')} />
          <small>按场景的环境光测光，不看画面；入夜后只比正午暗 2–4 档；光圈只决定景深</small>
        </>
      )}
      {manual && (
        <>
          <label className="cc-scene-field">
            <span>ISO</span>
            <select aria-label="ISO" value={camera.iso} onChange={(e) => onCamera({ iso: Number(e.target.value) }, `调整「${name}」ISO`)}>
              {withCurrent(isoThirds, camera.iso).map((v) => (
                <option key={v} value={v}>
                  {v}
                </option>
              ))}
            </select>
          </label>
          <label className="cc-scene-field">
            <span>快门</span>
            <select aria-label="快门" value={camera.shutter_s} onChange={(e) => onCamera({ shutter_s: Number(e.target.value) }, `调整「${name}」快门`)}>
              {withCurrent(shutterThirds, camera.shutter_s).map((v) => (
                <option key={v} value={v}>
                  {shutterLabel(v)}
                </option>
              ))}
            </select>
          </label>
        </>
      )}
      <label className="cc-scene-field">
        <span>光圈</span>
        <select aria-label="光圈" value={camera.f_stop} onChange={(e) => onCamera({ f_stop: Number(e.target.value) }, `调整「${name}」光圈`)}>
          {withCurrent(fStopThirds, camera.f_stop).map((v) => (
            <option key={v} value={v}>
              f/{v}
            </option>
          ))}
        </select>
      </label>
      {manual && (
        <span className="cc-scene-field">
          <span>EV100</span>
          <output aria-label="EV100">{ev100(camera.f_stop, camera.shutter_s, camera.iso).toFixed(1)}</output>
        </span>
      )}
      {!physical && <small>只在物理天空下生效</small>}
    </fieldset>
  )
}
