import type { WebGPURenderer } from 'three/webgpu'

// Frame stepping owned by whoever draws (Epic design「帧推进与确定性」).
// three's renderer runs its own rAF loop from init() (Animation.js) and
// advances nodeFrame there: frameId +1 and time by the wall clock. Nodes that
// update once per frame (TRAA's history, SSGI's and GTAO's rotations) only
// see frames that loop counted, so a synchronous run of K renders is one
// frame, and a hidden tab has none. Stopping the loop and stepping by hand
// makes frame k of every shot the same frame.

type NodeFrame = { frameId: number; time: number; deltaTime: number; lastTime?: number }
type Internals = { _animation: { stop(): void; start(): void }; _nodes: { nodeFrame: NodeFrame }; info: { frame: number } }

export type Frames = {
  /** Back to frame 0, time 0: the start of a shot. */
  reset(): void
  /** The next frame, dt seconds after the last. */
  step(dt?: number): number
  /** Gives the loop back to three. */
  release(): void
  readonly frameId: number
}

export function controlFrames(renderer: WebGPURenderer): Frames {
  const r = renderer as unknown as Internals
  const frame = r._nodes.nodeFrame
  r._animation.stop()
  return {
    reset() {
      frame.frameId = 0
      frame.time = 0
      frame.deltaTime = 0
      frame.lastTime = undefined
      r.info.frame = 0
    },
    step(dt = 1 / 60) {
      frame.frameId++
      frame.deltaTime = dt
      frame.time += dt
      r.info.frame = frame.frameId
      return frame.frameId
    },
    release() {
      r._animation.start()
    },
    get frameId() {
      return frame.frameId
    },
  }
}
