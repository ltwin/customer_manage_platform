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

/**
 * The SSGI's samples per pixel at each quality: slices × steps × 2 (R2
 * design「实时 GI 管线」), three's medium for both. The calibration (tasks
 * 5.1) tried 3 × 16 for a picture: 13% less noise for 44% more time, what
 * half as many frames again would give.
 */
export const ssgiQuality: Record<Quality, { slices: number; steps: number }> = {
  interactive: { slices: 2, steps: 8 },
  final: { slices: 2, steps: 8 },
}

/** The exports' output resolution (data contract render.resolution). */
export type Resolution = SceneRender['resolution']

export const resolutions: readonly Resolution[] = ['1k', '2k', '4k', '8k']

/** The long side of an export at a resolution, in pixels. */
export const longSideOf = (resolution: Resolution): number => ({ '1k': 1024, '2k': 2048, '4k': 4096, '8k': 8192 })[resolution]

export type ImageType = 'image/png' | 'image/jpeg'

/**
 * How an export image is encoded (R2 design「输出分辨率、编码与时限」): a 4K
 * or 8K clay or lit image as JPEG at 0.95, which keeps it within the media
 * upload's size; line art, all flat white and lines, and smaller images as PNG.
 */
export function imageTypeOf(pass: 'clay' | 'line' | 'lit', resolution: Resolution): { type: ImageType; quality?: number } {
  return pass !== 'line' && (resolution === '4k' || resolution === '8k') ? { type: 'image/jpeg', quality: 0.95 } : { type: 'image/png' }
}

/** The file extension uploads are named with. */
export const extensionOf = (type: string) => (type === 'image/jpeg' ? 'jpg' : 'png')

/** The scene's renderer; the server completes a scene stored before R2 (design R-1). */
export const engineOf = (render: SceneRender): Engine => render.engine

/** What a picture is for: an export, or the assistant's look (render_view). */
export type Use = 'export' | 'view'

/**
 * The frames a picture is averaged over (R2 design「累积与出图」): one for
 * the workbench; for the real-time GI, K for an export and K_view for
 * render_view, the same for every scene. The noise keeps falling as 1/√K
 * past 256 frames, so the owner set them by the time they take (tasks 5.1,
 * 2026-10-21): a 2K lit picture in about 3.6 s on the reference device.
 */
export function framesFor(engine: Engine, use: Use): number {
  if (engine === 'workbench') return 1
  return use === 'export' ? 64 : 16
}

/** The frames a picture draws: the real-time GI's settle first (convergeFrames), then the ones averaged. */
export const drawnFrames = (engine: Engine, use: Use): number => (engine === 'workbench' ? 1 : convergeFrames(engine) + framesFor(engine, use))

/** The reference device's time per pixel and frame, and per pixel encoded, rounded up (R0, design「输出分辨率、编码与时限」). */
const frameNs: Record<Engine, number> = { workbench: 15, realtime_gi: 30 }
const encodeNs: Record<'image/png' | 'image/jpeg', number> = { 'image/png': 40, 'image/jpeg': 15 }

/** How long a picture should take on the reference device, in ms. */
export function estimateMs(engine: Engine, width: number, height: number, frames: number, type: 'image/png' | 'image/jpeg'): number {
  return (width * height * (frames * frameNs[engine] + encodeNs[type])) / 1e6
}

/** A slow device gets eight times the reference's time, and never less than these. */
const slack = 8
const minShotMs = 30_000
const minViewMs = 60_000

/** One export image's time limit, drawn and encoded. */
export const shotBudgetMs = (engine: Engine, width: number, height: number, frames: number, type: 'image/png' | 'image/jpeg') =>
  Math.max(minShotMs, slack * estimateMs(engine, width, height, frames, type))

/** render_view's time limit: a JPEG of long side 1024, square at most, with the real-time GI's frames. */
export const viewBudgetMs = Math.max(minViewMs, slack * estimateMs('realtime_gi', 1024, 1024, drawnFrames('realtime_gi', 'view'), 'image/jpeg'))

/** Mounting the lit look and compiling its materials, within an export. */
export const lookBudgetMs = 60_000

/** An assistant's export ends within its job's 30 minutes, with five to spare for uploading and placing. */
export const jobRenderCapMs = 25 * 60_000

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
 * the workbench needs the one. Calibrated (tasks 5.1): the fewest of 8, 12,
 * 16, 24 and 32 after which two frames in a row differ by less than 0.2 on
 * average, in a 1600 × 1000 view of the box room and the room from a cold
 * start.
 */
export function convergeFrames(engine: Engine): number {
  return engine === 'realtime_gi' ? 12 : 1
}
