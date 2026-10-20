import type { ObjectUpdate, SceneObject } from '../api.ts'
import { coneAngle, coneDegrees, lightLabels } from '../editing.ts'
import { lightDefaults } from '../state/apply.ts'
import type { DisplayMode } from '../state/sceneStore.ts'
import { NumberField } from './Properties.tsx'

type Light = NonNullable<SceneObject['light']>
type LightPatch = NonNullable<ObjectUpdate['light']>

/**
 * The light section (design「属性栏与复制材质」): type, colour, intensity
 * with its unit, shadows, and the fields of point and spot lights. A new
 * type keeps the fields it shares (the server's mergeLight).
 */
export function LightFields({ object, readOnly, display, onLight }: {
  object: SceneObject & { light: Light }
  readOnly: boolean
  display: DisplayMode
  onLight: (patch: LightPatch, label: string, merge?: string) => void
}) {
  const l = { ...lightDefaults, ...object.light }
  const name = object.name || lightLabels[l.type]
  const set = (patch: LightPatch, what: string, merge?: string) => onLight(patch, `调整「${name}」${what}`, merge)
  return (
    <fieldset className="cc-scene-light" disabled={readOnly} aria-label="灯光">
      <legend>灯光</legend>
      {display !== 'rendered' && <small>灯光在「最终渲染」里生效</small>}
      <label className="cc-scene-field">
        <span>类型</span>
        <select aria-label="灯光类型" value={l.type} onChange={(e) => onLight({ type: e.target.value as Light['type'] }, `「${name}」改为${lightLabels[e.target.value as Light['type']]}`)}>
          {(Object.keys(lightLabels) as Light['type'][]).map((t) => (
            <option key={t} value={t}>
              {lightLabels[t]}
            </option>
          ))}
        </select>
      </label>
      <label className="cc-scene-field">
        <span>颜色</span>
        <input type="color" aria-label="灯光颜色" value={l.color} onChange={(e) => set({ color: e.target.value }, '颜色', `${object.id}:light.color`)} />
      </label>
      <NumberField label={l.type === 'directional' ? '强度' : '强度（cd）'} value={l.intensity} step={l.type === 'directional' ? 0.1 : 1} min={0} max={1000} onCommit={(v) => set({ intensity: v }, '强度', `${object.id}:light.intensity`)} />
      <label className="cc-scene-field">
        <span>投射阴影</span>
        <input type="checkbox" checked={l.cast_shadow} onChange={(e) => onLight({ cast_shadow: e.target.checked }, `${e.target.checked ? '打开' : '关闭'}「${name}」阴影`)} />
      </label>
      {l.type !== 'directional' && (
        <>
          <NumberField label="距离（m，0 不截断）" value={l.distance} step={0.5} min={0} onCommit={(v) => set({ distance: v }, '距离', `${object.id}:light.distance`)} />
          <NumberField label="衰减" value={l.decay} step={0.1} min={0} max={10} onCommit={(v) => set({ decay: v }, '衰减', `${object.id}:light.decay`)} />
        </>
      )}
      {l.type === 'spot' && (
        <>
          <NumberField label="光锥角（°）" value={coneDegrees(l.angle)} step={1} min={1} max={180} onCommit={(v) => set({ angle: coneAngle(v) }, '光锥角', `${object.id}:light.angle`)} />
          <NumberField label="柔化" value={l.penumbra} step={0.05} min={0} max={1} onCommit={(v) => set({ penumbra: v }, '柔化', `${object.id}:light.penumbra`)} />
        </>
      )}
    </fieldset>
  )
}
