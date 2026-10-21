import { createElement, useLayoutEffect, useMemo, useRef } from 'react'
import { createRoot, extend, useFrame, useThree } from '@react-three/fiber'
import { CameraControls, GizmoHelper, GizmoViewport } from '@react-three/drei'
import * as THREE from 'three/webgpu'
import { ACESFilmicToneMapping, LinearSRGBColorSpace, NoToneMapping, PerspectiveCamera, RenderPipeline, RenderTarget, SRGBColorSpace, UnsignedByteType, WebGPURenderer, type Camera, type Scene as ThreeScene } from 'three/webgpu'
import { builtinAOContext, mrt, normalView, output, pass, renderOutput, screenUV, velocity } from 'three/tsl'
import { ao } from 'three/addons/tsl/display/GTAONode.js'
import { traa } from 'three/addons/tsl/display/TRAANode.js'
import type { Scene } from '../../src/creative-scene/api.ts'
import { controlFrames, type Frames } from './frames.ts'
import { referenceScene, shotCamera } from './doc.ts'
import { SceneContent } from './scene.tsx'
import { pixelsToPNG } from './materialsCase.ts'

// Group 1 (R0 task 3.1): the viewport on demand with the axis gizmo drawn
// over a RenderPipeline, and an offscreen shooter on its own R3F root whose
// frames are stepped by hand (frames.ts) or left to three's loop.

extend(THREE as unknown as Parameters<typeof extend>[0])

/** GTAO from a normal/depth pre-pass into the lighting, then TRAA: both move with frameId. */
export function temporalPipeline(renderer: WebGPURenderer, scene: ThreeScene, camera: Camera) {
  const pipeline = new RenderPipeline(renderer)
  const prePass = pass(scene, camera, { samples: 0 })
  prePass.setMRT(mrt({ output: normalView }))
  const aoPass = ao(prePass.getTextureNode('depth'), prePass.getTextureNode(), camera)
  const scenePass = pass(scene, camera, { samples: 0 })
  scenePass.setMRT(mrt({ output, velocity }))
  scenePass.contextNode = builtinAOContext(aoPass.getTextureNode().sample(screenUV).r)
  const aa = traa(scenePass.getTextureNode('output'), scenePass.getTextureNode('depth'), scenePass.getTextureNode('velocity'), camera)
  pipeline.outputNode = aa
  return {
    pipeline,
    dispose() {
      pipeline.dispose()
      aa.dispose()
      aoPass.dispose()
      prePass.dispose()
      scenePass.dispose()
    },
  }
}

// --- the viewport ---------------------------------------------------------------

const gizmo = new URLSearchParams(location.search).get('gizmo') !== '0'

export function ViewportCase({ doc }: { doc: Scene }) {
  return (
    <>
      <StartView doc={doc} />
      <CameraControls makeDefault />
      <SceneContent doc={doc} />
      <PipelineFrame />
      {gizmo && (
        <GizmoHelper alignment="top-right" margin={[72, 72]} renderPriority={2}>
          <GizmoViewport />
        </GizmoHelper>
      )}
    </>
  )
}

function StartView({ doc }: { doc: Scene }) {
  const size = useThree((s) => s.size)
  const set = useThree((s) => s.set)
  const camera = useMemo(() => {
    const c = shotCamera(doc, null, size.width / size.height).camera
    c.up.set(0, 0, 1)
    return c
    // The first size only: the controls own the camera after that.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [doc])
  useLayoutEffect(() => set({ camera }), [camera, set])
  return null
}

// ?overlay=direct: the renderer itself neither tone maps nor encodes, the
// pipeline does both in its output node, so later renders to the canvas
// (the gizmo's Hud) skip three's frame buffer target and draw over it.
const direct = new URLSearchParams(location.search).get('overlay') === 'direct'

/** The scene through the pipeline at priority 1, as the editor's LookView; counts the frames drawn. */
function PipelineFrame() {
  const { gl, scene, camera } = useThree()
  const built = useMemo(() => {
    const renderer = gl as unknown as WebGPURenderer
    const b = temporalPipeline(renderer, scene, camera)
    if (direct) {
      renderer.toneMapping = NoToneMapping
      renderer.outputColorSpace = LinearSRGBColorSpace
      b.pipeline.outputColorTransform = false
      b.pipeline.outputNode = renderOutput(b.pipeline.outputNode, ACESFilmicToneMapping, SRGBColorSpace)
    }
    return b
  }, [gl, scene, camera])
  useLayoutEffect(() => () => built.dispose(), [built])
  const drawn = useRef(0)
  useFrame(() => {
    built.pipeline.render()
    drawn.current++
    ;(window as unknown as { __drawn: number }).__drawn = drawn.current
  }, 1)
  return null
}

// --- the offscreen shooter --------------------------------------------------------

export type ShotResult = { hash: string; png: string; frameIds: number[]; ms: number; hidden: boolean }

/** A scene mounted on a canvas that is never shown; shots render K frames into a target and read it back. */
export async function openShooter(doc: Scene, options: { control: boolean }) {
  const canvas = document.createElement('canvas')
  const camera = new PerspectiveCamera()
  const root = createRoot(canvas)
  let renderer: WebGPURenderer | null = null
  await root.configure({
    gl: async (props) => {
      renderer = new WebGPURenderer({ canvas: props.canvas as HTMLCanvasElement, antialias: false, forceWebGL: new URLSearchParams(location.search).get('backend') === 'webgl' })
      await renderer.init()
      renderer.toneMapping = ACESFilmicToneMapping
      renderer.shadowMap.enabled = true
      return renderer
    },
    shadows: true,
    frameloop: 'never',
    dpr: 1,
    size: { width: 1, height: 1, top: 0, left: 0 },
    camera,
  })
  let store: ReturnType<typeof root.render> | undefined
  await new Promise<void>((resolve) => {
    store = root.render(createElement(Mounted, { doc, onCommit: resolve }))
  })
  const scene = store!.getState().scene
  const gl = renderer!
  const frames: Frames | null = options.control ? controlFrames(gl) : null
  return {
    backend: (gl.backend as { isWebGPUBackend?: boolean }).isWebGPUBackend ? 'webgpu' : 'webgl2',
    /** K frames of one shot; between frames, wait (ms) lets the page's own loop run, as an async shot would. */
    async shoot(width: number, height: number, k: number, wait = 0): Promise<ShotResult> {
      const started = performance.now()
      gl.setSize(width, height, false)
      const shot = shotCamera(doc, null, width / height).camera
      camera.copy(shot)
      camera.updateMatrixWorld()
      const target = new RenderTarget(width, height, { type: UnsignedByteType, depthBuffer: false })
      const built = temporalPipeline(gl, scene, camera)
      const frameIds: number[] = []
      frames?.reset()
      try {
        for (let i = 0; i < k; i++) {
          frames?.step()
          frameIds.push((gl as unknown as { _nodes: { nodeFrame: { frameId: number } } })._nodes.nodeFrame.frameId)
          gl.setRenderTarget(target)
          built.pipeline.render()
          gl.setRenderTarget(null)
          if (wait) await new Promise((r) => setTimeout(r, wait))
        }
        const pixels = (await gl.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Uint8Array
        return { hash: await sha256(pixels), png: toPNG(pixels, width, height, gl), frameIds, ms: performance.now() - started, hidden: document.hidden }
      } finally {
        built.dispose()
        target.dispose()
      }
    },
    close() {
      frames?.release()
      root.unmount()
      gl.dispose()
    },
  }
}

function Mounted({ doc, onCommit }: { doc: Scene; onCommit(): void }) {
  useLayoutEffect(() => onCommit(), [onCommit])
  return <SceneContent doc={doc} />
}

async function sha256(bytes: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', bytes)
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('').slice(0, 16)
}

/** Read-back rows: bottom-up and tight on WebGL, top-down and padded to 256 bytes on WebGPU (pixelsToPNG). */
function toPNG(pixels: Uint8Array, width: number, height: number, gl: WebGPURenderer): string {
  return pixelsToPNG(pixels, width, height, !(gl.backend as { isWebGPUBackend?: boolean }).isWebGPUBackend)
}

/** For measurement scripts: window.__shooter.run({ scene, control, k, wait, width, height, times }). */
export function exposeShooter() {
  ;(window as unknown as { __shooter: unknown }).__shooter = {
    async run(o: { scene: string; control: boolean; k: number; wait?: number; width?: number; height?: number; times?: number }) {
      const shooter = await openShooter(referenceScene(o.scene), { control: o.control })
      try {
        const out: ShotResult[] = []
        for (let i = 0; i < (o.times ?? 2); i++) out.push(await shooter.shoot(o.width ?? 960, o.height ?? 640, o.k, o.wait ?? 0))
        return { backend: shooter.backend, shots: out }
      } finally {
        shooter.close()
      }
    },
  }
}
