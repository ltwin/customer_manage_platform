import type { SceneRender } from '../api.ts'
import { longSideOf, resolutions, type Engine, type Resolution } from '../render/engines.ts'
import type { Backend, DisplayMode } from '../state/sceneStore.ts'
import { Slider } from './EnvironmentPanel.tsx'

type RenderPatch = Partial<SceneRender>

const engineNames: Record<Engine, string> = { workbench: '工作台', realtime_gi: '实时 GI' }

/**
 * The render tab (R2 design「属性区选项卡」): the renderer, the exports'
 * output resolution, the lit look's exposure and, read only, the renderer
 * backend the viewport draws with. Each field writes set_render with only
 * itself; the exposure drag folds by its merge key as before.
 */
export function RenderPanel({ render, backend, readOnly, display, onRender }: {
  render: SceneRender
  backend: Backend | null
  readOnly: boolean
  display: DisplayMode
  onRender: (patch: RenderPatch, label: string, merge?: string) => void
}) {
  return (
    <div className="cc-scene-props cc-scene-render" aria-label="渲染" role="tabpanel">
      <fieldset disabled={readOnly}>
        <legend>渲染器</legend>
        <label className="cc-scene-field">
          <select
            aria-label="渲染器"
            value={render.engine}
            onChange={(e) => {
              const engine = e.target.value as Engine
              if (engine in engineNames) onRender({ engine }, `渲染器改为${engineNames[engine]}`)
            }}
          >
            <option value="workbench">工作台</option>
            <option value="realtime_gi">实时 GI</option>
            <option value="path_tracing" disabled>
              路径追踪 · 规划中
            </option>
          </select>
        </label>
        {render.engine === 'realtime_gi' && <small>只计算画面里可见的表面：画面外和被遮挡的物体不产生反弹光，也不出现在反射里。正交视图按工作台显示。</small>}
      </fieldset>
      <fieldset disabled={readOnly}>
        <legend>输出分辨率</legend>
        <div className="cc-scene-segmented" role="group" aria-label="输出分辨率">
          {resolutions.map((r: Resolution) => (
            <button key={r} type="button" aria-pressed={render.resolution === r} onClick={() => render.resolution !== r && onRender({ resolution: r }, `输出分辨率改为 ${r.toUpperCase()}`)}>
              {r.toUpperCase()}
            </button>
          ))}
        </div>
        <small>导出图长边 {longSideOf(render.resolution)} px</small>
        {render.resolution === '8k' && render.engine === 'realtime_gi' && <small>8K 实时 GI 每张约需一分钟以上，需要较大显存，设备画不了时导出会失败。</small>}
      </fieldset>
      <fieldset disabled={readOnly}>
        <legend>曝光</legend>
        {display !== 'rendered' && <small>曝光补偿只作用于「最终渲染」和材质光照导出</small>}
        <Slider label="曝光补偿（EV）" value={render.exposure_ev} min={-10} max={10} step={0.1} onCommit={(v) => onRender({ exposure_ev: v }, '调整曝光补偿', 'render.exposure_ev')} />
      </fieldset>
      <fieldset>
        <legend>渲染后端</legend>
        <output aria-label="渲染后端">{backend === null ? '—' : backend.kind === 'webgpu' && !backend.compatibility ? 'WebGPU' : '兼容模式'}</output>
      </fieldset>
    </div>
  )
}
