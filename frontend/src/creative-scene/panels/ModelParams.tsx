import { useEffect, useRef, useState } from 'react'
import type { ModelGenerator } from '../api.ts'
import { NumberField } from './Properties.tsx'

// The parameters a model's code declares, as controls in the properties
// panel. A change rebakes the model (modelEdits); until it is queued the
// panel shows the new value, and a failed bake puts the old one back.

type Spec = ModelGenerator['params_schema'][string]

export function ModelParams({ generator, sharedWith, readOnly, onCommit, onEditCode }: {
  generator: Omit<ModelGenerator, 'code'> | null
  /** Other instances in the scene showing the same revision. */
  sharedWith: number
  readOnly: boolean
  /** Resolves false when the bake failed and the old values are back. */
  onCommit: (params: Record<string, unknown>, applyToAll: boolean, name: string) => Promise<boolean>
  onEditCode: () => void
}) {
  const [pending, setPending] = useState<Record<string, unknown> | null>(null)
  const [applyToAll, setApplyToAll] = useState(false)
  const params = generator?.params
  useEffect(() => setPending(null), [params])
  const latest = useRef(0)
  if (!generator) return null
  const values = pending ?? generator.params
  const commit = (name: string, value: unknown) => {
    const next = { ...values, [name]: value }
    const n = ++latest.current
    setPending(next)
    void onCommit(next, sharedWith > 0 && applyToAll, name).then((ok) => {
      if (!ok && latest.current === n) setPending(null)
    })
  }
  const entries = Object.entries(generator.params_schema)
  return (
    <fieldset className="cc-scene-model-params" disabled={readOnly}>
      <legend>模型参数</legend>
      {sharedWith > 0 && (
        <div className="cc-scene-shared">
          <span>与 {sharedWith} 个实例共用</span>
          <label>
            <input type="checkbox" checked={applyToAll} onChange={(e) => setApplyToAll(e.target.checked)} />
            应用到全部
          </label>
        </div>
      )}
      {entries.length === 0 && <small>这个模型没有可调参数</small>}
      {entries.map(([name, spec]) => (
        <ModelParamField key={name} name={name} spec={spec} value={values[name]} disabled={readOnly} onCommit={(v) => commit(name, v)} />
      ))}
      <button type="button" className="btn" onClick={onEditCode}>
        编辑代码
      </button>
    </fieldset>
  )
}

function ModelParamField({ name, spec, value, disabled, onCommit }: { name: string; spec: Spec; value: unknown; disabled: boolean; onCommit: (v: unknown) => void }) {
  const label = spec.label || name
  if (spec.type === 'boolean')
    return (
      <label className="cc-scene-field">
        <span>{label}</span>
        <input type="checkbox" checked={value === true} disabled={disabled} onChange={(e) => onCommit(e.target.checked)} />
      </label>
    )
  if (spec.type === 'enum')
    return (
      <label className="cc-scene-field">
        <span>{label}</span>
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
        <span>{label}</span>
        <input type="color" value={String(value)} disabled={disabled} onChange={(e) => onCommit(e.target.value)} />
      </label>
    )
  return <NumberParam label={`${label}${spec.unit ? `（${spec.unit}）` : ''}`} spec={spec} value={typeof value === 'number' ? value : Number(spec.default)} disabled={disabled} onCommit={onCommit} />
}

/** A number box and a slider over the declared range; both commit 300 ms after the last change. */
function NumberParam({ label, spec, value, disabled, onCommit }: { label: string; spec: Spec; value: number; disabled: boolean; onCommit: (v: number) => void }) {
  const integer = spec.type === 'integer'
  const min = spec.min ?? 0
  const max = spec.max ?? 1
  const step = spec.step ?? (integer ? 1 : (max - min) / 100 || 0.01)
  const [slide, setSlide] = useState<number | null>(null)
  const timer = useRef<number | null>(null)
  useEffect(() => () => {
    if (timer.current !== null) window.clearTimeout(timer.current)
  }, [])
  const clamp = (v: number) => Math.min(max, Math.max(min, integer ? Math.round(v) : v))
  return (
    <div className="cc-scene-number-param">
      <NumberField label={label} value={slide ?? value} step={step} min={min} max={max} disabled={disabled} onCommit={(v) => onCommit(clamp(v))} />
      <input
        type="range"
        aria-label={`${label}滑块`}
        min={min}
        max={max}
        step={step}
        value={slide ?? value}
        disabled={disabled}
        onChange={(e) => {
          const v = clamp(Number(e.target.value))
          setSlide(v)
          if (timer.current !== null) window.clearTimeout(timer.current)
          timer.current = window.setTimeout(() => {
            timer.current = null
            setSlide(null)
            if (v !== value) onCommit(v)
          }, 300)
        }}
      />
    </div>
  )
}
