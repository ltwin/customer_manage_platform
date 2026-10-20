import type { Scene } from '../api.ts'
import { proceduralSky, withFog, withSkyKind } from '../editing.ts'
import type { DisplayMode } from '../state/sceneStore.ts'
import { NumberField } from './Properties.tsx'

type Environment = Scene['environment']
type RenderPatch = Partial<Scene['render']>

/** A number with a slider beside it, for what is dragged to judge: the sun and the exposure. */
function Slider({ label, value, min, max, step, onCommit }: { label: string; value: number; min: number; max: number; step: number; onCommit: (v: number) => void }) {
  return (
    <div className="cc-scene-slider">
      <NumberField label={label} value={value} step={step} min={min} max={max} onCommit={onCommit} />
      <input type="range" aria-label={label} min={min} max={max} step={step} value={value} onChange={(e) => onCommit(Number(e.target.value))} />
    </div>
  )
}

/**
 * The environment (design「属性栏与复制材质」), where the properties go when
 * nothing is active: sky, ambient light, fog and the lit look's exposure.
 * set_environment writes the environment whole, so each field edit sends
 * the current one with that field changed; a drag's commits fold by their
 * merge key while unsent.
 */
export function EnvironmentPanel({ environment, exposure, readOnly, display, onEnvironment, onRender }: {
  environment: Environment
  exposure: number
  readOnly: boolean
  display: DisplayMode
  onEnvironment: (env: Environment, label: string, merge?: string) => void
  onRender: (patch: RenderPatch, label: string, merge?: string) => void
}) {
  const { sky, ambient, fog } = environment
  const set = (env: Environment, what: string, field?: string) => onEnvironment(env, `调整${what}`, field && `environment.${field}`)
  const setSky = (patch: Partial<Environment['sky']>, what: string, field: string) => set({ ...environment, sky: { ...sky, ...patch } }, what, field)
  return (
    <div className="cc-scene-props cc-scene-environment" aria-label="环境">
      <fieldset disabled={readOnly}>
        <legend>天空</legend>
        <label className="cc-scene-field">
          <span>类型</span>
          <select
            aria-label="天空类型"
            value={sky.kind}
            onChange={(e) => {
              const kind = e.target.value
              if (kind === 'procedural' || kind === 'color') onEnvironment(withSkyKind(environment, kind), kind === 'color' ? '天空改为纯色' : '天空改为程序化天空')
            }}
          >
            <option value="procedural">程序化天空</option>
            <option value="color">纯色</option>
            {sky.kind === 'hdri' && <option value="hdri">HDRI（暂不支持）</option>}
          </select>
        </label>
        {sky.kind === 'procedural' && (
          <>
            <Slider label="太阳高度（°）" value={sky.sun_elevation_deg ?? proceduralSky.sun_elevation_deg!} min={-10} max={90} step={1} onCommit={(v) => setSky({ sun_elevation_deg: v }, '太阳高度', 'sun_elevation_deg')} />
            <Slider label="太阳方位（°）" value={sky.sun_azimuth_deg ?? proceduralSky.sun_azimuth_deg!} min={0} max={360} step={1} onCommit={(v) => setSky({ sun_azimuth_deg: v }, '太阳方位', 'sun_azimuth_deg')} />
            <NumberField label="浑浊度" value={sky.turbidity ?? proceduralSky.turbidity!} step={0.5} min={1} max={20} onCommit={(v) => setSky({ turbidity: v }, '天空浑浊度', 'turbidity')} />
          </>
        )}
        {sky.kind === 'color' && (
          <label className="cc-scene-field">
            <span>颜色</span>
            <input type="color" aria-label="天空颜色" value={sky.color ?? '#000000'} onChange={(e) => setSky({ color: e.target.value }, '天空颜色', 'sky_color')} />
          </label>
        )}
        {sky.kind === 'hdri' && <small>暂无内置 HDRI，请改用程序化天空或纯色</small>}
      </fieldset>
      <fieldset disabled={readOnly}>
        <legend>环境光</legend>
        <label className="cc-scene-field">
          <span>颜色</span>
          <input type="color" aria-label="环境光颜色" value={ambient.color} onChange={(e) => set({ ...environment, ambient: { ...ambient, color: e.target.value } }, '环境光颜色', 'ambient_color')} />
        </label>
        <NumberField label="强度" value={ambient.intensity} step={0.05} min={0} max={10} onCommit={(v) => set({ ...environment, ambient: { ...ambient, intensity: v } }, '环境光强度', 'ambient_intensity')} />
      </fieldset>
      <fieldset disabled={readOnly}>
        <legend>雾</legend>
        <label className="cc-scene-field">
          <span>开启</span>
          <input type="checkbox" aria-label="雾" checked={!!fog} onChange={(e) => onEnvironment(withFog(environment, e.target.checked), e.target.checked ? '打开雾' : '关闭雾')} />
        </label>
        {fog && (
          <>
            <label className="cc-scene-field">
              <span>颜色</span>
              <input type="color" aria-label="雾颜色" value={fog.color} onChange={(e) => set({ ...environment, fog: { ...fog, color: e.target.value } }, '雾颜色', 'fog_color')} />
            </label>
            <NumberField label="起始（m）" value={fog.near} step={1} min={0.01} max={fog.far - 0.01} onCommit={(v) => set({ ...environment, fog: { ...fog, near: v } }, '雾起始距离', 'fog_near')} />
            <NumberField label="结束（m）" value={fog.far} step={1} min={fog.near + 0.01} onCommit={(v) => set({ ...environment, fog: { ...fog, far: v } }, '雾结束距离', 'fog_far')} />
          </>
        )}
      </fieldset>
      <fieldset disabled={readOnly}>
        <legend>曝光</legend>
        {display !== 'rendered' && <small>曝光补偿只作用于「最终渲染」和材质光照导出</small>}
        <Slider label="曝光补偿（EV）" value={exposure} min={-10} max={10} step={0.1} onCommit={(v) => onRender({ exposure_ev: v }, '调整曝光补偿', 'render.exposure_ev')} />
      </fieldset>
    </div>
  )
}
