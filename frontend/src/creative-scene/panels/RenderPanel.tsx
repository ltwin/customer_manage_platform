import type { SceneRender } from '../api.ts'
import { longSideOf, resolutions, type Engine, type Resolution } from '../render/engines.ts'
import type { TraceStatus } from '../render/trace/base.ts'
import type { Backend, DisplayMode } from '../state/sceneStore.ts'
import { Slider } from './EnvironmentPanel.tsx'

type RenderPatch = Partial<SceneRender>

const engineNames: Record<Engine, string> = { workbench: '工作台', realtime_gi: '实时 GI' }

/**
 * The world-space layer's note beside the backend (S1 design「降级与提示」, S2 design「提示文字」): none when it draws
 * in full or is switched off by hand. Ready with its probes still warming, it still prepares; at tier 2 only the
 * reflections take the simplified geometry, the probes trace it at either tier.
 */
function traceNote(trace: TraceStatus, warming: boolean): string | null {
  switch (trace.state) {
    case 'off':
      return trace.reason === 'compatibility' ? '兼容模式：未绘制屏幕外的光照与反射' : trace.reason === 'device' ? '当前设备不支持屏幕外的光照与反射' : null
    case 'preparing':
      return '正在准备屏幕外的光照与反射'
    case 'ready':
      return warming ? '正在准备屏幕外的光照与反射' : trace.tier === 'proxy' ? '场景较大：屏幕外的反射使用简化几何' : null
    case 'tooLarge':
      return '场景过大：未绘制屏幕外的光照与反射'
    case 'failed':
      return '屏幕外的光照与反射暂时不可用'
  }
}

/**
 * The render tab (R2 design「属性区选项卡」): the renderer, the exports'
 * output resolution, the lit look's exposure (under a physical sky the
 * default camera's, for views without a camera: ambient metering with a
 * compensation or by hand at an EV100, W2 and Epic revision v5) and, read
 * only, the renderer backend the viewport draws with and, in the real-time
 * GI, its world-space layer's state. Each field writes set_render with only
 * itself; the exposure drag folds by its merge key as before.
 */
export function RenderPanel({ render, backend, trace, warming, readOnly, display, physical, onRender }: {
  render: SceneRender
  backend: Backend | null
  /** The viewport's world-space layer, once known (sceneStore ui.trace). */
  trace: TraceStatus | null
  /** Its probes still warming in the background (sceneStore ui.probesWarming). */
  warming: boolean
  readOnly: boolean
  display: DisplayMode
  /** The sky is physical: the lit look is exposed by the default camera's exposure, not compensated. */
  physical: boolean
  onRender: (patch: RenderPatch, label: string, merge?: string) => void
}) {
  const manual = render.exposure_mode === 'manual'
  const gi = render.engine === 'realtime_gi'
  const note = gi && trace !== null ? traceNote(trace, warming) : null
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
        {display !== 'rendered' && <small>渲染器只作用于「最终渲染」和材质光照导出</small>}
        {gi && (trace?.state === 'ready' ? <small>画面外的物体也会出现在反射里，也会把光反弹进画面。正交视图按工作台显示。</small> : <small>只计算画面里可见的表面：画面外和被遮挡的物体不产生反弹光，也不出现在反射里。正交视图按工作台显示。</small>)}
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
        {render.resolution === '8k' && gi && <small>8K 实时 GI 的材质光照每张约需一分钟以上，需要较大显存，设备画不了时导出会失败。</small>}
      </fieldset>
      <fieldset disabled={readOnly}>
        <legend>曝光</legend>
        {display !== 'rendered' && <small>曝光只作用于「最终渲染」和材质光照导出</small>}
        {!physical && <Slider label="曝光补偿（EV）" value={render.exposure_ev} min={-10} max={10} step={0.1} onCommit={(v) => onRender({ exposure_ev: v }, '调整曝光补偿', 'render.exposure_ev')} />}
        <div className="cc-scene-segmented" role="group" aria-label="默认摄像机曝光">
          <button type="button" aria-pressed={!manual} disabled={!physical} onClick={() => manual && onRender({ exposure_mode: 'ambient' }, '默认摄像机改为环境测光')}>
            环境测光
          </button>
          <button type="button" aria-pressed={manual} disabled={!physical} onClick={() => !manual && onRender({ exposure_mode: 'manual' }, '默认摄像机改为手动曝光')}>
            手动
          </button>
        </div>
        {manual ? (
          <Slider label="默认曝光（EV100）" value={render.exposure_ev100} min={-10} max={20} step={1 / 3} disabled={!physical} onCommit={(v) => onRender({ exposure_ev100: v }, '调整默认曝光', 'render.exposure_ev100')} />
        ) : (
          <Slider label="环境测光补偿（档）" value={render.exposure_compensation} min={-5} max={5} step={1 / 3} disabled={!physical} onCommit={(v) => onRender({ exposure_compensation: v }, '调整环境测光补偿', 'render.exposure_compensation')} />
        )}
        {physical ? (
          <>
            {manual ? <small>参考：晴天 15 · 阴天 12–13 · 日落 9–12 · 蓝调时刻 5–8 · 城市夜景 3–5 · 月光 −3 · 银河 −7</small> : <small>按场景的环境光测光，不看画面：入夜后平滑变暗，满月之夜约比正午暗 2.5 档、新月之夜约 3.5 档</small>}
            <small>只用于视口的自由视角和助手的临时视点，摄像机用自己的曝光</small>
          </>
        ) : (
          <small>默认摄像机的曝光只在物理天空下生效</small>
        )}
      </fieldset>
      <fieldset>
        <legend>渲染后端</legend>
        <output aria-label="渲染后端">{backend === null ? '—' : backend.kind === 'webgpu' && !backend.compatibility ? 'WebGPU' : '兼容模式'}</output>
        {note && <small>{note}</small>}
      </fieldset>
    </div>
  )
}
