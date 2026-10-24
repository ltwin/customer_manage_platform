import type { ObjectUpdate, SceneObject } from '../api.ts'
import { volumeLabels } from '../editing.ts'
import { volumeDefaults } from '../state/apply.ts'
import type { DisplayMode } from '../state/sceneStore.ts'
import { NumberField } from './Properties.tsx'

type Volume = NonNullable<SceneObject['volume']>
type VolumePatch = NonNullable<ObjectUpdate['volume']>

/** The density slider's log scale, 0.001…100 per metre, its leftmost step 0. */
const densityLog = { min: -3.5, zero: -3.25, max: 2 }

const toSlider = (d: number) => (d <= 0 ? densityLog.min : Math.log10(d))
const fromSlider = (v: number) => (v <= densityLog.zero ? 0 : Math.min(100, Math.max(0.001, Number((10 ** v).toPrecision(2)))))

/**
 * A density by number and on a log slider: fog is a few hundredths a metre,
 * smoke a few per metre.
 */
function Density({ label, value, onCommit }: { label: string; value: number; onCommit: (v: number) => void }) {
  return (
    <div className="cc-scene-slider">
      <NumberField label={label} value={value} step={0.01} min={0} max={100} onCommit={onCommit} />
      <input type="range" aria-label={label} min={densityLog.min} max={densityLog.max} step={0.01} value={toSlider(value)} onChange={(e) => onCommit(fromSlider(Number(e.target.value)))} />
    </div>
  )
}

/**
 * The volume section (W5, data: 体积物件): shape, scattering colour and
 * density, anisotropy, absorption colour and density, edge falloff, and
 * whether it shadows surfaces. The medium shows in the rendered view only.
 */
export function VolumeFields({ object, readOnly, display, onVolume }: {
  object: SceneObject & { volume: Volume }
  readOnly: boolean
  display: DisplayMode
  onVolume: (patch: VolumePatch, label: string, merge?: string) => void
}) {
  const v = { ...volumeDefaults, ...object.volume }
  const name = object.name || '体积'
  const set = (patch: VolumePatch, what: string, key?: keyof Volume) => onVolume(patch, `调整「${name}」${what}`, key && `${object.id}:volume.${key}`)
  return (
    <fieldset className="cc-scene-volume" disabled={readOnly} aria-label="体积">
      <legend>体积</legend>
      {display !== 'rendered' && <small>介质在「最终渲染」里显示，这里只有范围线框</small>}
      <label className="cc-scene-field">
        <span>形状</span>
        <select aria-label="体积形状" value={v.shape} onChange={(e) => onVolume({ shape: e.target.value as Volume['shape'] }, `「${name}」改为${volumeLabels[e.target.value as keyof typeof volumeLabels]}`)}>
          {(Object.keys(volumeLabels) as (keyof typeof volumeLabels)[]).map((s) => (
            <option key={s} value={s}>
              {volumeLabels[s]}
            </option>
          ))}
        </select>
      </label>
      <label className="cc-scene-field">
        <span>散射颜色</span>
        <input type="color" aria-label="散射颜色" value={v.scatter_color} onChange={(e) => set({ scatter_color: e.target.value }, '散射颜色', 'scatter_color')} />
      </label>
      <Density label="散射密度" value={v.scatter_density} onCommit={(d) => set({ scatter_density: d }, '散射密度', 'scatter_density')} />
      <div className="cc-scene-slider">
        <NumberField label="各向异性" value={v.anisotropy} step={0.05} min={-0.95} max={0.95} onCommit={(g) => set({ anisotropy: g }, '各向异性', 'anisotropy')} />
        <input type="range" aria-label="各向异性" min={-0.95} max={0.95} step={0.05} value={v.anisotropy} onChange={(e) => set({ anisotropy: Number(e.target.value) }, '各向异性', 'anisotropy')} />
      </div>
      <label className="cc-scene-field">
        <span>吸收颜色</span>
        <input type="color" aria-label="吸收颜色" value={v.absorption_color} onChange={(e) => set({ absorption_color: e.target.value }, '吸收颜色', 'absorption_color')} />
      </label>
      <Density label="吸收密度" value={v.absorption_density} onCommit={(d) => set({ absorption_density: d }, '吸收密度', 'absorption_density')} />
      <NumberField label="边缘柔化" value={v.edge_falloff} step={0.05} min={0} max={1} onCommit={(e) => set({ edge_falloff: e }, '边缘柔化', 'edge_falloff')} />
      <label className="cc-scene-field">
        <span>投射阴影</span>
        <input type="checkbox" aria-label="投射阴影" checked={v.cast_shadow} onChange={(e) => onVolume({ cast_shadow: e.target.checked }, `${e.target.checked ? '打开' : '关闭'}「${name}」投射阴影`)} />
      </label>
      <small>密度每米：0.05–0.3 是薄雾，2 以上是浓烟；各向异性为正时逆光更亮</small>
    </fieldset>
  )
}
