import { HalfFloatType, WebGLRenderTarget, type PerspectiveCamera, type Scene, type WebGLRenderer } from 'three'
import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js'
import { GTAOPass } from 'three/examples/jsm/postprocessing/GTAOPass.js'
import { OutputPass } from 'three/examples/jsm/postprocessing/OutputPass.js'
import { RenderPass } from 'three/examples/jsm/postprocessing/RenderPass.js'
import type { DepthOfField } from '../cameraOptics.ts'
import { DofPass } from './dofPass.ts'

// The clay export (design「导出渲染」): the viewport's clay, sky and sun with
// ambient occlusion so contacts and corners read, then the camera's depth of
// field when it is on. AO comes before the blur, so contact shadows out of
// focus soften with everything else. The editor viewport and a plain
// render_view stay without AO.

export class ClayRenderer {
  private readonly composer: EffectComposer
  private readonly gtao: GTAOPass
  private readonly dof: DofPass

  /** `camera` is the one camera every shot moves; the passes keep it. */
  constructor(gl: WebGLRenderer, scene: Scene, camera: PerspectiveCamera) {
    const target = new WebGLRenderTarget(1, 1, { type: HalfFloatType, samples: 4 })
    this.composer = new EffectComposer(gl, target)
    this.composer.setPixelRatio(1)
    this.composer.addPass(new RenderPass(scene, camera))
    this.gtao = new GTAOPass(scene, camera, 1, 1)
    this.composer.addPass(this.gtao)
    // GTAO draws the scene's depth for its own use; the blur reads it.
    this.dof = new DofPass(() => this.gtao.depthTexture)
    this.composer.addPass(this.dof)
    this.composer.addPass(new OutputPass())
  }

  /** Draws into the renderer's canvas at the given size; `dof` blurs, null keeps it sharp. */
  render(camera: PerspectiveCamera, width: number, height: number, dof: DepthOfField | null) {
    this.composer.setSize(width, height)
    this.dof.enabled = dof !== null
    if (dof) this.dof.setLens(dof, camera.near, camera.far)
    this.composer.render()
  }

  dispose() {
    this.dof.dispose()
    this.gtao.dispose()
    this.composer.dispose()
  }
}
