// The renderers' constants and pure functions (R2 design「模块职责与协作」):
// nothing here touches the GPU, so the unit tests take it as it is.

/** The scene's renderer (data contract render.engine); path tracing is planned, not selectable. */
export type Engine = 'workbench' | 'realtime_gi'

/** How hard the real-time GI works: the viewport's frames, or a picture to keep. */
export type Quality = 'interactive' | 'final'

/** The scene's renderer as stored; a scene from before R2 has none, which is the workbench. */
export const engineOf = (render: object | undefined): Engine => ((render as { engine?: unknown } | undefined)?.engine === 'realtime_gi' ? 'realtime_gi' : 'workbench')

/**
 * The frames a real-time GI picture is drawn over before it is taken, so
 * its temporal nodes have a history to stand on. A stand-in for the K-frame
 * accumulation (R2 tasks 4.1), which replaces it.
 */
export const GI_WARMUP_FRAMES = 16
