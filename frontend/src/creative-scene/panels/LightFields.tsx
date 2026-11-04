import type { ObjectUpdate, SceneObject } from '../api.ts'
import { coneAngle, coneDegrees, lightLabels } from '../editing.ts'
import { emittingArea, lightDefaultsOf, maxLightIntensity, mergeLight, worldAreaScale } from '../state/apply.ts'
import type { ShadowState } from '../objects/shadowBudget.ts'
import type { DisplayMode } from '../state/sceneStore.ts'
import { NumberField } from './Properties.tsx'

type Light = NonNullable<SceneObject['light']>
type LightPatch = NonNullable<ObjectUpdate['light']>

/**
 * The light section (design「属性栏与复制材质」): type, colour, intensity
 * with its unit, shadows, and the fields of point and spot lights. A new
 * type keeps the fields it shares (the server's mergeLight). An area light
 * (data: 面光源) has its shape, width and height, its intensity a luminance
 * with its equivalent luminous intensity beside it, its face scaled by the
 * object's world transform (objects, the scene's). A type change writes the
 * intensity the server would convert to, so the two agree.
 */
export function LightFields({ object, objects, readOnly, display, shadow, onLight }: {
  object: SceneObject & { light: Light }
  objects: readonly SceneObject[]
  readOnly: boolean
  display: DisplayMode
  /** Whether it casts shadows on surfaces, as the budget picked (objects/shadowBudget.ts). */
  shadow: ShadowState | undefined
  onLight: (patch: LightPatch, label: string, merge?: string) => void
}) {
  const l = { ...lightDefaultsOf(object.light.type), ...object.light }
  const areaScale = l.type === 'area' ? worldAreaScale(objects, object.id) : 1
  const name = object.name || lightLabels[l.type]
  const retype = (type: Light['type']) => {
    const next = mergeLight(object.light, { type }, areaScale, worldAreaScale(objects, object.id))
    onLight({ type, ...(next ? { intensity: next.intensity } : {}) }, `「${name}」改为${lightLabels[type]}`)
  }
  const set = (patch: LightPatch, what: string, merge?: string) => onLight(patch, `调整「${name}」${what}`, merge)
  return (
    <fieldset className="cc-scene-light" disabled={readOnly} aria-label="灯光">
      <legend>灯光</legend>
      {display !== 'rendered' && <small>灯光在「最终渲染」里生效</small>}
      <label className="cc-scene-field">
        <span>类型</span>
        <select aria-label="灯光类型" value={l.type} onChange={(e) => retype(e.target.value as Light['type'])}>
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
      <NumberField
        label={l.type === 'directional' ? '强度' : l.type === 'area' ? '亮度（cd/m²）' : '强度（cd）'}
        value={l.intensity}
        step={l.type === 'directional' ? 0.1 : l.type === 'area' ? 100 : 1}
        min={0}
        max={maxLightIntensity(l.type)}
        onCommit={(v) => set({ intensity: v }, l.type === 'area' ? '亮度' : '强度', `${object.id}:light.intensity`)}
      />
      {l.type === 'area' && <small aria-label="等效发光强度">≈ {Math.round(l.intensity * emittingArea(l, areaScale))} cd</small>}
      <label className="cc-scene-field">
        <span>投射阴影</span>
        <input type="checkbox" checked={l.cast_shadow} onChange={(e) => onLight({ cast_shadow: e.target.checked }, `${e.target.checked ? '打开' : '关闭'}「${name}」阴影`)} />
      </label>
      {shadow === 'over_budget' && (
        <small className="cc-scene-shadow-over" title="表面最多用 8 张阴影贴图：天空的太阳占 4 张，投影的灯按物件列表的顺序依次占用，平行光 4 张、其他灯 1 张，放不下的灯照常照明但不投影。把这盏灯在物件列表里往前移，或关掉前面某盏灯的阴影，就能让它投影。">
          阴影超出上限
        </small>
      )}
      {l.type === 'directional' && (
        // No step: the default 0.545 lies on no step's grid, and a number input would call it a step mismatch.
        <NumberField label="角径（°，0 为硬影子）" value={l.angular_diameter_deg ?? 0} min={0} max={30} onCommit={(v) => set({ angular_diameter_deg: v }, '角径', `${object.id}:light.angular_diameter_deg`)} />
      )}
      {(l.type === 'point' || l.type === 'spot') && (
        <NumberField label="半径（m，0 为硬影子）" value={l.radius_m ?? 0} step={0.01} min={0} max={10} onCommit={(v) => set({ radius_m: v }, '半径', `${object.id}:light.radius_m`)} />
      )}
      {l.type === 'area' && (
        <>
          <label className="cc-scene-field">
            <span>形状</span>
            <select aria-label="面光源形状" value={l.shape} onChange={(e) => onLight({ shape: e.target.value as NonNullable<Light['shape']> }, `「${name}」改为${e.target.value === 'ellipse' ? '椭圆' : '矩形'}`)}>
              <option value="rectangle">矩形</option>
              <option value="ellipse">椭圆</option>
            </select>
          </label>
          <NumberField label="宽（m）" value={l.width} step={0.05} min={0.01} max={20} onCommit={(v) => set({ width: v }, '宽', `${object.id}:light.width`)} />
          <NumberField label="高（m）" value={l.height} step={0.05} min={0.01} max={20} onCommit={(v) => set({ height: v }, '高', `${object.id}:light.height`)} />
        </>
      )}
      {(l.type === 'point' || l.type === 'spot') && (
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
