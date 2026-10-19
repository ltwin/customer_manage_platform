import {
  DepthTexture,
  MeshNormalMaterial,
  NearestFilter,
  ShaderMaterial,
  Vector2,
  WebGLRenderTarget,
  type Color,
  type Fog,
  type FogExp2,
  type PerspectiveCamera,
  type Scene,
  type Texture,
  type WebGLRenderer,
} from 'three'
import { FullScreenQuad } from 'three/examples/jsm/postprocessing/Pass.js'

// Line art (design「导出渲染」): black edges on white where the surface
// normal turns or the depth jumps, from view-space normals and depth drawn
// at twice the picture's size. The edge shader samples that grid at four
// points per output pixel, which also scales it down to about 2-pixel lines.
// three's EdgesGeometry misses the outline of curved surfaces and draws
// 1-pixel lines; its Sobel shader works on colour, which clay barely has.

/** Edge thresholds, tuned on the sample scenes. */
export const normalThreshold = 0.15
export const depthThreshold = 0.03

const vertexShader = /* glsl */ `
varying vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}
`

const edgeShader = /* glsl */ `
#include <packing>
uniform sampler2D tNormal;
uniform sampler2D tDepth;
uniform vec2 texel;
uniform float radius;
uniform float cameraNear;
uniform float cameraFar;
uniform float normalThreshold;
uniform float depthThreshold;
varying vec2 vUv;

float depthAt(vec2 uv) {
  float z = texture2D(tDepth, uv).x;
  return -perspectiveDepthToViewZ(z, cameraNear, cameraFar);
}

float edgeAt(vec2 uv) {
  vec3 nc = texture2D(tNormal, uv).xyz * 2.0 - 1.0;
  float zc = depthAt(uv);
  float normalEdge = 0.0;
  float depthEdge = 0.0;
  for (int y = -1; y <= 1; y++) {
    for (int x = -1; x <= 1; x++) {
      vec2 at = uv + vec2(float(x), float(y)) * texel * radius;
      vec3 n = texture2D(tNormal, at).xyz * 2.0 - 1.0;
      float z = depthAt(at);
      normalEdge = max(normalEdge, 1.0 - dot(nc, n));
      depthEdge = max(depthEdge, abs(z - zc) / zc);
    }
  }
  float a = smoothstep(normalThreshold, normalThreshold * 1.5, normalEdge);
  float b = smoothstep(depthThreshold, depthThreshold * 1.5, depthEdge);
  return max(a, b);
}

void main() {
  vec2 h = texel * 0.5;
  float edge = (edgeAt(vUv + vec2(-h.x, -h.y)) + edgeAt(vUv + vec2(h.x, -h.y)) + edgeAt(vUv + vec2(-h.x, h.y)) + edgeAt(vUv + vec2(h.x, h.y))) * 0.25;
  gl_FragColor = vec4(vec3(1.0 - edge), 1.0);
}
`

export class LineRenderer {
  private readonly target = new WebGLRenderTarget(1, 1, { minFilter: NearestFilter, magFilter: NearestFilter, depthTexture: new DepthTexture(1, 1) })
  private readonly normals = new MeshNormalMaterial()
  private readonly quad = new FullScreenQuad(
    new ShaderMaterial({
      uniforms: {
        tNormal: { value: null },
        tDepth: { value: null },
        texel: { value: new Vector2() },
        radius: { value: 1 },
        cameraNear: { value: 0.1 },
        cameraFar: { value: 1000 },
        normalThreshold: { value: normalThreshold },
        depthThreshold: { value: depthThreshold },
      },
      vertexShader,
      fragmentShader: edgeShader,
    }),
  )

  /** Draws the scene's line art into the renderer's canvas at its current size. */
  render(gl: WebGLRenderer, scene: Scene, camera: PerspectiveCamera, width: number, height: number) {
    const max = gl.capabilities.maxTextureSize
    const scale = Math.max(width, height) * 2 <= max ? 2 : 1
    this.target.setSize(width * scale, height * scale)
    // Background, sky and fog are not lines: empty pixels sit at the far plane.
    const saved: { background: Color | Texture | null; fog: Fog | FogExp2 | null; override: typeof scene.overrideMaterial } = {
      background: scene.background as Color | Texture | null,
      fog: scene.fog,
      override: scene.overrideMaterial,
    }
    scene.background = null
    scene.fog = null
    scene.overrideMaterial = this.normals
    const clear = gl.getClearAlpha()
    gl.setRenderTarget(this.target)
    gl.setClearColor(0x8080ff, 1)
    gl.clear()
    gl.render(scene, camera)
    gl.setClearAlpha(clear)
    scene.background = saved.background
    scene.fog = saved.fog
    scene.overrideMaterial = saved.override

    const u = (this.quad.material as ShaderMaterial).uniforms
    u.tNormal.value = this.target.texture
    u.tDepth.value = this.target.depthTexture
    u.texel.value.set(1 / (width * scale), 1 / (height * scale))
    // About 2 output pixels of line at any picture size.
    u.radius.value = Math.max(1, Math.round(((Math.max(width, height) / 1024) * scale) / 2))
    u.cameraNear.value = camera.near
    u.cameraFar.value = camera.far
    gl.setRenderTarget(null)
    this.quad.render(gl)
  }

  dispose() {
    this.target.depthTexture?.dispose()
    this.target.dispose()
    this.normals.dispose()
    this.quad.material.dispose()
    this.quad.dispose()
  }
}
