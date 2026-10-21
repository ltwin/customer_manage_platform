import { Color, DepthTexture, NearestFilter, Vector2, type Fog, type FogExp2, type PerspectiveCamera, type Scene, type Texture } from 'three'
import * as tsl from 'three/tsl'
import { MeshBasicNodeMaterial, NodeMaterial, QuadMesh, RenderTarget } from 'three/webgpu'
import type { TSLNode } from '../materials/patterns.ts'
import type { SceneGPU } from './gpu.ts'

// Line art (design「导出渲染」): black edges on white where the surface
// normal turns or the depth jumps, from view-space normals and depth drawn
// at twice the picture's size. The edge shader samples that grid at four
// points per output pixel, which also scales it down to about 2-pixel lines.
// three's EdgesGeometry misses the outline of curved surfaces and draws
// 1-pixel lines; its Sobel shader works on colour, which clay barely has.
//
// The normals are packed by a basic node material, not MeshNormalNodeMaterial:
// that one decodes the packed normal from sRGB for the canvas, which a linear
// target keeps, and upward faces come out black (R0: this way the edges are
// within 0.003 of the WebGL pass).

/** Edge thresholds, tuned on the sample scenes. */
export const normalThreshold = 0.15
export const depthThreshold = 0.03

type Op = (...args: TSLNode[]) => TSLNode
const { directionToColor, dot, float, max, perspectiveDepthToViewZ, smoothstep, texture, uniform, vec2, vec3, vec4 } = tsl as unknown as Record<string, Op>
const { normalView, screenUV } = tsl as unknown as Record<string, TSLNode>
const Fn = tsl.Fn as unknown as (fn: () => TSLNode) => () => TSLNode

export class LineRenderer {
  private readonly target = new RenderTarget(1, 1, { minFilter: NearestFilter, magFilter: NearestFilter, depthTexture: new DepthTexture(1, 1) })
  private readonly normals = new MeshBasicNodeMaterial({ colorNode: directionToColor(normalView) })
  private readonly texel = uniform(new Vector2())
  private readonly radius = uniform(1)
  private readonly near = uniform(0.1)
  private readonly far = uniform(1000)
  private readonly edges = new NodeMaterial()
  private readonly quad = new QuadMesh(this.edges)

  constructor() {
    const normalTex = texture(this.target.texture)
    const depthTex = texture(this.target.depthTexture)
    const { texel, radius, near, far } = this
    const depthAt = (uv: TSLNode) => perspectiveDepthToViewZ(depthTex.sample(uv).x, near, far).negate()
    const normalAt = (uv: TSLNode) => normalTex.sample(uv).xyz.mul(2).sub(1)
    const edgeAt = (uv: TSLNode) => {
      const nc = normalAt(uv).toVar()
      const zc = depthAt(uv).toVar()
      let normalEdge: TSLNode = float(0)
      let depthEdge: TSLNode = float(0)
      for (let y = -1; y <= 1; y++) {
        for (let x = -1; x <= 1; x++) {
          const at = uv.add(vec2(x, y).mul(texel).mul(radius))
          normalEdge = max(normalEdge, float(1).sub(dot(nc, normalAt(at))))
          depthEdge = max(depthEdge, depthAt(at).sub(zc).abs().div(zc))
        }
      }
      return max(smoothstep(normalThreshold, normalThreshold * 1.5, normalEdge), smoothstep(depthThreshold, depthThreshold * 1.5, depthEdge))
    }
    this.edges.fragmentNode = Fn(() => {
      const h = texel.mul(0.5)
      const edge = edgeAt(screenUV.add(vec2(h.x.negate(), h.y.negate())))
        .add(edgeAt(screenUV.add(vec2(h.x, h.y.negate()))))
        .add(edgeAt(screenUV.add(vec2(h.x.negate(), h.y))))
        .add(edgeAt(screenUV.add(h)))
        .mul(0.25)
      return vec4(vec3(float(1).sub(edge)), 1)
    })()
  }

  /** Draws the scene's line art into the renderer's canvas at its current size. */
  render(gpu: SceneGPU, scene: Scene, camera: PerspectiveCamera, width: number, height: number) {
    const gl = gpu.renderer
    const scale = Math.max(width, height) * 2 <= gpu.maxTextureSize ? 2 : 1
    this.target.setSize(width * scale, height * scale)
    // Background, sky and fog are not lines: empty pixels sit at the far plane.
    const saved: { background: Color | Texture | null; fog: Fog | FogExp2 | null; override: typeof scene.overrideMaterial } = {
      background: scene.background as Color | Texture | null,
      fog: scene.fog,
      override: scene.overrideMaterial,
    }
    const clear = { color: gl.getClearColor(new Color()), alpha: gl.getClearAlpha() }
    scene.background = null
    scene.fog = null
    scene.overrideMaterial = this.normals
    try {
      gl.setRenderTarget(this.target)
      gl.setClearColor(0x8080ff, 1)
      gl.clear()
      gl.render(scene, camera)
    } finally {
      gl.setClearColor(clear.color, clear.alpha)
      scene.background = saved.background
      scene.fog = saved.fog
      scene.overrideMaterial = saved.override
    }
    this.texel.value.set(1 / (width * scale), 1 / (height * scale))
    // About 2 output pixels of line at any picture size.
    this.radius.value = Math.max(1, Math.round(((Math.max(width, height) / 1024) * scale) / 2))
    this.near.value = camera.near
    this.far.value = camera.far
    gl.setRenderTarget(null)
    this.quad.render(gl)
  }

  dispose() {
    this.target.depthTexture?.dispose()
    this.target.dispose()
    this.normals.dispose()
    this.edges.dispose()
  }
}
