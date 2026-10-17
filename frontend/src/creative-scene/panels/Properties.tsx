import { useCallback, useEffect, useRef, useState } from 'react'
import { Euler, MathUtils, Quaternion } from 'three'
import type { ObjectUpdate, SceneObject } from '../api.ts'
import { builtinSpec, type ParamSpec } from '../builtins/catalog.ts'
import { transformToDoc, transformToView } from '../space.ts'

/**
 * A number that commits 300 ms after typing stops, on Enter or on blur, and
 * reverts on Esc. Only text the photographer changed is committed, so
 * focusing a field never writes back its rounded display.
 */
function NumberField({ label, value, step, min, max, disabled, onCommit }: { label: string; value: number; step?: number; min?: number; max?: number; disabled?: boolean; onCommit: (v: number) => void }) {
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

function ParamField({ name, spec, value, disabled, onCommit }: { name: string; spec: ParamSpec; value: unknown; disabled: boolean; onCommit: (v: unknown) => void }) {
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
export function Properties({ object, count, readOnly, onUpdate }: { object: SceneObject | null; count: number; readOnly: boolean; onUpdate: (u: ObjectUpdate, label: string, merge?: string) => void }) {
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
      {object.light && <small>灯光：{object.light.type} · 强度 {object.light.intensity}（灯光设置随后续版本开放）</small>}
      {object.camera && <small>摄像机：{object.camera.focal_mm} mm · {object.camera.aspect}（机位设置随后续版本开放）</small>}
    </div>
  )
}
