import { ACESFilmicToneMapping, Color, Layers, NoToneMapping, SRGBColorSpace, type Camera, type Object3D, type Scene } from 'three'
import { float, mix, mrt, normalView, pass, renderOutput, uniform, vec4 } from 'three/tsl'
import { PassNode, RenderPipeline, type Node } from 'three/webgpu'
import { denoise } from 'three/addons/tsl/display/DenoiseNode.js'
import { smaa } from 'three/addons/tsl/display/SMAANode.js'
import type { DepthOfField } from '../cameraOptics.ts'
import type { SceneGPU } from './gpu.ts'
import { ao } from './nodes/GTAONode.js'

// The rendered looks (design「导出渲染」, owner 2026-10-19): the scene as
// mounted, lit as its look says (objects/Lighting.tsx), with ambient
// occlusion so contacts and corners read, then the camera's depth of field
// when it is on. The editor viewport, render_view and the exports all draw
// through it, so what the viewport shows is what the export gets. AO comes
// before the blur, so contact shadows out of focus soften with everything
// else. Clay and lit differ only in what is mounted and in the exposure.
//
// The workbench (Epic design「渲染管线组成」): a prepass of the opaque
// surfaces gives the AO its normals and depth, so glass and the grid neither
// cast AO nor hide what is behind them; the picture is drawn with 4× MSAA
// and the AO multiplied over it, as the WebGL GTAOPass did. The canvas is
// linear and tone mapping happens in the output node (render/gpu.ts,
// 'pipeline'), so the transform gizmo, drawn by a pass of its own, is laid
// over the finished picture in sRGB.

/** Viewport helpers the clay look treats apart; an export has none. */
export type Overlays = {
  /** Drawn in the picture but kept out of the AO: the grid's plane would shade what stands on it. */
  aside: () => readonly (Object3D | null)[]
  /** Drawn after AO and depth of field, sharp and unshaded: the transform gizmo. */
  top: () => readonly (Object3D | null)[]
}

const noOverlays: Overlays = { aside: () => [], top: () => [] }

/** The layer the picture draws but the AO's prepass leaves out. */
export const ASIDE_LAYER = 30
/** The layer only the overlay pass draws; the gizmo's raycaster has to see it too. */
export const OVERLAY_LAYER = 31

const layers = (...ids: number[]) => {
  const l = new Layers()
  l.disableAll()
  for (const id of ids) l.enable(id)
  return l
}

export type Frame = {
  width: number
  height: number
  /** Device pixels per CSS pixel; an export draws at 1. */
  pixelRatio?: number
  /**
   * The pixel ratio the AO is worked out at, when lower than pixelRatio: the
   * viewport's at 1 (CSS pixels), a quarter of the work on a 2× screen, and
   * laid over the full-resolution picture. An export works it out in full.
   */
  aoPixelRatio?: number
  /** Blurs by this lens; null keeps the picture sharp. */
  dof: DepthOfField | null
  /** The long side of the camera's picture in device pixels, when it is not the whole frame. */
  pictureLongSide?: number
  /** Ambient occlusion; on unless turned off. */
  ao?: boolean
  /** toneMappingExposure for this frame: 2^exposure_ev for lit, 1 otherwise. */
  exposure?: number
  /** Wireframe has no surfaces to shade: no AO, no blur. */
  wireframe?: boolean
}

/**
 * A colour for something drawn straight onto the 'pipeline' canvas after
 * the picture (drei's axis widget): three writes it linear and nothing
 * encodes it, so it is given encoded once more to show as asked.
 */
export function overlayColor(hex: string): string {
  // Color decodes the hex to linear; written as is, that linear value shows
  // as if it were sRGB. Encoding twice makes what is written the sRGB value.
  const c = new Color(hex)
  const encode = (x: number) => (x <= 0.0031308 ? x * 12.92 : 1.055 * x ** (1 / 2.4) - 0.055)
  return `#${[c.r, c.g, c.b].map((x) => Math.round(Math.min(1, encode(encode(x))) * 255).toString(16).padStart(2, '0')).join('')}`
}

/** three's typings leave most display nodes untyped; these all give a colour. */
const vec4Of = (node: Node) => node as Node<'vec4'>

/**
 * A pass drawn without the scene's background, cleared to transparent: the
 * overlay pass, where only the gizmo may cover the picture, and wireframe's
 * picture, whose plain background is laid under it after tone mapping (the
 * WebGL viewport drew wireframe straight to the canvas, where the clear
 * colour is not tone mapped).
 */
class BackgroundlessPass extends PassNode {
  updateBefore(frame: Parameters<PassNode['updateBefore']>[0]) {
    const scene = this.scene as Scene
    const renderer = frame.renderer!
    const background = scene.background
    const alpha = renderer.getClearAlpha()
    scene.background = null
    renderer.setClearAlpha(0)
    try {
      return super.updateBefore(frame)
    } finally {
      scene.background = background
      renderer.setClearAlpha(alpha)
    }
  }
}

type Built = {
  key: string
  pipeline: RenderPipeline
  /** The AO node, when the look has one: its resolution follows aoPixelRatio. */
  ao: ReturnType<typeof ao> | null
  aoOn: { value: number } | null
  /** Wireframe's plain background, laid under the tone-mapped picture. */
  background: { value: Color }
  dispose(): void
}

export class SceneRenderer {
  private readonly gpu: SceneGPU
  private readonly scene: Scene
  private readonly overlays: Overlays
  private built: Built | null = null
  private width = -1
  private height = -1
  private pixelRatio = -1
  private aoPixelRatio = -1

  /** `camera` is the one camera every frame is drawn from; the passes keep it. */
  private readonly camera: Camera

  constructor(gpu: SceneGPU, scene: Scene, camera: Camera, overlays: Overlays = noOverlays) {
    this.gpu = gpu
    this.camera = camera
    this.scene = scene
    this.overlays = overlays
  }

  private build(wireframe: boolean): Built {
    const { scene, camera, gpu } = this
    const disposables: { dispose(): void }[] = []
    const own = <T extends { dispose(): void }>(n: T) => (disposables.push(n), n)
    const msaa = gpu.compatibility ? 0 : 4
    const beauty = own(wireframe ? new BackgroundlessPass(PassNode.COLOR, scene, camera, { samples: msaa }) : pass(scene, camera, { samples: msaa }))
    beauty.setLayers(layers(0, ASIDE_LAYER))
    let picture: Node<'vec4'> = beauty
    const background = uniform(new Color())
    let aoNode: ReturnType<typeof ao> | null = null
    let aoOn: { value: number } | null = null
    if (!wireframe) {
      // Multisampled depth cannot be sampled by the AO: the prepass has none.
      const prepass = own(pass(scene, camera, { samples: 0 }))
      prepass.transparent = false
      prepass.setLayers(layers(0))
      prepass.setMRT(mrt({ output: normalView }))
      const normal = prepass.getTextureNode()
      const depth = prepass.getTextureNode('depth')
      aoNode = own(ao(depth, normal, camera))
      // GTAOPass's Poisson denoise, as it was set up there.
      const clean = own(denoise(aoNode.getTextureNode(), depth, normal, camera))
      clean.lumaPhi.value = 10
      clean.depthPhi.value = 2
      clean.normalPhi.value = 3
      clean.radius.value = 8
      // Frame.ao = false multiplies by one rather than rebuilding.
      const on = uniform(1)
      aoOn = on
      picture = vec4(beauty.rgb.mul(mix(float(1), vec4Of(clean).r, on)), beauty.a)
    }
    let output = vec4Of(renderOutput(picture, ACESFilmicToneMapping, SRGBColorSpace))
    if (wireframe) output = vec4(output.rgb.add(vec4Of(renderOutput(vec4(background, 1), NoToneMapping, SRGBColorSpace)).rgb.mul(output.a.oneMinus())), 1)
    if (msaa === 0) output = vec4Of(own(smaa(output)))
    const pipeline = new RenderPipeline(gpu.renderer)
    pipeline.outputColorTransform = false
    pipeline.outputNode = vec4(output.rgb, 1)
    if (this.overlays !== noOverlays) {
      const overlay = own(new BackgroundlessPass(PassNode.COLOR, scene, camera, { samples: msaa }))
      overlay.setLayers(layers(OVERLAY_LAYER))
      // Premultiplied sRGB over the finished picture.
      const top = vec4Of(renderOutput(overlay, NoToneMapping, SRGBColorSpace))
      pipeline.outputNode = vec4(output.rgb.mul(top.a.oneMinus()).add(top.rgb), 1)
    }
    return {
      key: wireframe ? 'wire' : 'shaded',
      pipeline,
      ao: aoNode,
      aoOn,
      background,
      dispose() {
        pipeline.dispose()
        for (const d of disposables) d.dispose()
      },
    }
  }

  /** Draws a frame into the renderer's canvas. */
  render(camera: Camera, frame: Frame) {
    if (camera !== this.camera) throw new Error('SceneRenderer 只画构造时的摄像机')
    const renderer = this.gpu.renderer
    const key = frame.wireframe ? 'wire' : 'shaded'
    if (this.built?.key !== key) {
      this.built?.dispose()
      this.built = this.build(frame.wireframe === true)
      this.width = -1
    }
    const built = this.built
    const ratio = frame.pixelRatio ?? 1
    const aoRatio = Math.min(frame.aoPixelRatio ?? ratio, ratio)
    if (ratio !== this.pixelRatio || aoRatio !== this.aoPixelRatio || frame.width !== this.width || frame.height !== this.height) {
      this.pixelRatio = ratio
      this.aoPixelRatio = aoRatio
      this.width = frame.width
      this.height = frame.height
      renderer.setPixelRatio(ratio)
      renderer.setSize(frame.width, frame.height, false)
      if (built.ao) built.ao.resolutionScale = aoRatio / ratio
    }
    if (built.aoOn) built.aoOn.value = frame.ao === false ? 0 : 1
    const bg = this.scene.background
    built.background.value.copy(bg instanceof Color ? bg : renderer.getClearColor(new Color()))
    for (const o of this.overlays.aside()) o?.traverse((c) => c.layers.set(ASIDE_LAYER))
    for (const o of this.overlays.top()) o?.traverse((c) => c.layers.set(OVERLAY_LAYER))
    // The output node reads the exposure from the renderer as it draws.
    const exposure = renderer.toneMappingExposure
    renderer.toneMappingExposure = frame.exposure ?? 1
    try {
      built.pipeline.render()
    } finally {
      renderer.toneMappingExposure = exposure
    }
  }

  dispose() {
    this.built?.dispose()
    this.built = null
  }
}
