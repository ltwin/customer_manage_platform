import type { SceneRender } from '../api.ts'

// The renderers' constants and pure functions (R2 design「模块职责与协作」):
// nothing here touches the GPU, so the unit tests take it as it is.

/** The scene's renderer (data contract render.engine); path tracing is planned, not selectable. */
export type Engine = SceneRender['engine']

/** How hard the real-time GI works: the viewport's frames, or a picture to keep. */
export type Quality = 'interactive' | 'final'

/**
 * The SSGI's indirect light, against three's default of 10, which washed
 * out the shade beside sunlit walls (R2 tasks 1.2). Its own integral counts
 * about a third of a lit hemisphere's light, so near 2.5 would keep energy;
 * at 2.5 the box room's ceiling stays black. Provisional: set with the
 * calibration (tasks 5.1) and the repaint comparison (AC-R2-10).
 */
export const GI_INTENSITY = 5

/** The exports' output resolution (data contract render.resolution). */
export type Resolution = SceneRender['resolution']

export const resolutions: readonly Resolution[] = ['1k', '2k', '4k', '8k']

/** The long side of an export at a resolution, in pixels. */
export const longSideOf = (resolution: Resolution): number => ({ '1k': 1024, '2k': 2048, '4k': 4096, '8k': 8192 })[resolution]

/** The scene's renderer; the server completes a scene stored before R2 (design R-1). */
export const engineOf = (render: SceneRender): Engine => render.engine

/**
 * The frames a real-time GI picture is drawn over before it is taken, so
 * its temporal nodes have a history to stand on. A stand-in for the K-frame
 * accumulation (R2 tasks 4.1), which replaces it.
 */
export const GI_WARMUP_FRAMES = 16

/** What is drawn: a viewport shading or an export pass. */
export type Look = 'solid' | 'rendered' | 'wireframe' | 'preview' | 'clay' | 'lit' | 'line'

/**
 * The renderer a picture is drawn with (R2 design R-2): the scene's for the
 * clay and lit looks in a perspective view, the workbench otherwise.
 * Wireframe, material preview and line art are the workbench's; so is an
 * orthographic view, which the real-time GI does not take (the viewport
 * says so). Exports and render_view always look through a perspective camera.
 */
export function effectiveEngine(engine: Engine, look: Look, perspective: boolean): Engine {
  if (!perspective) return 'workbench'
  return look === 'solid' || look === 'rendered' || look === 'clay' || look === 'lit' ? engine : 'workbench'
}

/**
 * The frames the viewport draws after it stops changing, before it stops
 * drawing (R2 design「收敛」): the real-time GI's temporal nodes settling;
 * the workbench needs the one. 16 to start with, set by the calibration
 * (tasks 5.1).
 */
export const convergeFrames = (engine: Engine): number => (engine === 'realtime_gi' ? 16 : 1)
