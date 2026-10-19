import { HalfFloatType, Vector2, WebGLRenderTarget, type Camera, type Object3D, type PerspectiveCamera, type Scene, type WebGLRenderer } from 'three'
import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js'
import { GTAOPass } from 'three/examples/jsm/postprocessing/GTAOPass.js'
import { OutputPass } from 'three/examples/jsm/postprocessing/OutputPass.js'
import { Pass } from 'three/examples/jsm/postprocessing/Pass.js'
import { RenderPass } from 'three/examples/jsm/postprocessing/RenderPass.js'
import type { DepthOfField } from '../cameraOptics.ts'
import { DofPass } from './dofPass.ts'

// The clay look (design「导出渲染」, owner 2026-10-19): the scene as mounted,
// its sky and sun, with ambient occlusion so contacts and corners read, then
// the camera's depth of field when it is on. The editor viewport, render_view
// and the clay export all draw through it, so depth of field is the only
// switch and what the viewport shows is what the export gets. AO comes
// before the blur, so contact shadows out of focus soften with everything
// else.

/** Viewport helpers the clay look treats apart; an export has none. */
export type Overlays = {
  /** Drawn in the picture but kept out of the AO: the grid's plane would shade what stands on it. */
  aside: () => readonly (Object3D | null)[]
  /** Drawn after AO and depth of field, sharp and unshaded: the transform gizmo. */
  top: () => readonly (Object3D | null)[]
}

const noOverlays: Overlays = { aside: () => [], top: () => [] }

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
}

export class ClayRenderer {
  private readonly gl: WebGLRenderer
  private readonly composer: EffectComposer
  private readonly gtao: GTAOPass
  private readonly dof: DofPass
  private readonly overlays: Overlays
  private readonly size = new Vector2(-1, -1)
  private pixelRatio = -1
  private aoPixelRatio = -1

  /** `camera` is the one camera every frame is drawn from; the passes keep it. */
  constructor(gl: WebGLRenderer, scene: Scene, camera: Camera, overlays: Overlays = noOverlays) {
    this.gl = gl
    this.overlays = overlays
    const target = new WebGLRenderTarget(1, 1, { type: HalfFloatType, samples: 4 })
    this.composer = new EffectComposer(gl, target)
    this.composer.addPass(new RenderPass(scene, camera))
    // AO draws the scene once more for its depth and normals: without the
    // helpers set aside, and without drawing the shadow maps again.
    let hidden: Object3D[] = []
    let shadows = true
    this.composer.addPass(
      new Hook(() => {
        hidden = overlays.aside().filter((o): o is Object3D => o?.visible === true)
        for (const o of hidden) o.visible = false
        shadows = gl.shadowMap.autoUpdate
        gl.shadowMap.autoUpdate = false
      }),
    )
    this.gtao = new GTAOPass(scene, camera, 1, 1)
    if (!(camera as PerspectiveCamera).isPerspectiveCamera) this.gtao.enabled = lookAlongZ(this.gtao.gtaoMaterial)
    this.composer.addPass(this.gtao)
    this.composer.addPass(
      new Hook(() => {
        for (const o of hidden) o.visible = true
        hidden = []
        gl.shadowMap.autoUpdate = shadows
      }),
    )
    // GTAO draws the scene's depth for its own use; the blur reads it.
    this.dof = new DofPass(() => this.gtao.depthTexture)
    this.composer.addPass(this.dof)
    this.composer.addPass(new OutputPass())
  }

  /** Draws a frame into the renderer's canvas. */
  render(camera: Camera, frame: Frame) {
    const ratio = frame.pixelRatio ?? 1
    const aoRatio = Math.min(frame.aoPixelRatio ?? ratio, ratio)
    if (ratio !== this.pixelRatio || aoRatio !== this.aoPixelRatio || frame.width !== this.size.x || frame.height !== this.size.y) {
      this.pixelRatio = ratio
      this.aoPixelRatio = aoRatio
      this.size.set(frame.width, frame.height)
      this.composer.setPixelRatio(ratio)
      this.composer.setSize(frame.width, frame.height)
      if (aoRatio !== ratio) this.gtao.setSize(Math.max(1, Math.round(frame.width * aoRatio)), Math.max(1, Math.round(frame.height * aoRatio)))
    }
    const dof = (camera as PerspectiveCamera).isPerspectiveCamera ? frame.dof : null
    this.dof.enabled = dof !== null
    if (dof) {
      const persp = camera as PerspectiveCamera
      this.dof.setLens(dof, persp.near, persp.far, frame.pictureLongSide ?? Math.max(frame.width, frame.height) * ratio)
    }
    const top = this.overlays.top().filter((o): o is Object3D => o?.visible === true)
    for (const o of top) o.visible = false
    try {
      this.composer.render()
    } finally {
      for (const o of top) o.visible = true
    }
    if (!top.length) return
    const autoClear = this.gl.autoClear
    this.gl.autoClear = false
    this.gl.setRenderTarget(null)
    this.gl.clearDepth()
    for (const o of top) this.gl.render(o, camera)
    this.gl.autoClear = autoClear
  }

  dispose() {
    this.dof.dispose()
    this.gtao.dispose()
    this.composer.dispose()
  }
}

/** GTAOShader's view direction, which holds for a perspective camera only. */
export const perspectiveViewDirection = 'vec3 viewDir = normalize(-viewPos.xyz);'

/**
 * An orthographic camera looks along -Z everywhere. GTAOPass takes the view
 * direction as the way back to the camera's origin, which for the viewport's
 * orthographic views (near at -5000) can point away from the camera and turns
 * the AO black. False when three's shader no longer has the line: the view
 * then goes without AO rather than black.
 */
export function lookAlongZ(material: { fragmentShader: string; needsUpdate: boolean }): boolean {
  if (!material.fragmentShader.includes(perspectiveViewDirection)) return false
  material.fragmentShader = material.fragmentShader.replace(perspectiveViewDirection, 'vec3 viewDir = vec3(0.0, 0.0, 1.0);')
  material.needsUpdate = true
  return true
}

/** Runs a step between passes without drawing. */
class Hook extends Pass {
  private readonly run: () => void
  constructor(run: () => void) {
    super()
    this.run = run
    this.needsSwap = false
  }
  render() {
    this.run()
  }
}
