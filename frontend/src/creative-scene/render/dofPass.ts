import { HalfFloatType, LinearFilter, ShaderMaterial, Vector2, WebGLRenderTarget, type Texture, type WebGLRenderer } from 'three'
import { FullScreenQuad, Pass } from 'three/examples/jsm/postprocessing/Pass.js'
import { maxBlurShare, type DepthOfField } from '../cameraOptics.ts'

// Depth of field by the physical circle of confusion (design「景深」), the
// same formula as cameraOptics.blurPixels. Three steps:
//   1. the largest blur of each 16-pixel tile, so a pixel knows how far a
//      blurred neighbour can reach it;
//   2. at half resolution, a gather over a Vogel disk as wide as the nearby
//      tiles' largest blur, keeping a sample only where its own blur disk
//      covers the pixel. A sample behind the pixel reaches no further than
//      the pixel's own blur, so a sharp foreground never takes in the
//      blurred background behind it;
//   3. a blend of the sharp and the blurred picture by the pixel's blur and
//      how much blurred foreground spills over it.
// three's BokehPass is not used: its blur grows linearly with depth and does
// not follow the f-number.

const tile = 16

const common = /* glsl */ `
#include <packing>
uniform sampler2D tDepth;
uniform float cameraNear;
uniform float cameraFar;
uniform float focalMM;
uniform float fStop;
uniform float focusMM;
uniform float sensorLongMM;
uniform float longSidePx;
uniform float maxBlurPx;

float distanceAt(vec2 uv) {
  float z = texture2D(tDepth, uv).x;
  return -perspectiveDepthToViewZ(z, cameraNear, cameraFar) * 1000.0;
}

// Signed: negative in front of the focus.
float blurAt(float d) {
  float f = focalMM;
  float s = focusMM;
  d = max(d, f * 1.001);
  float c = f * f / (fStop * (s - f)) * abs(d - s) / d;
  float px = min(c / sensorLongMM * longSidePx, maxBlurPx);
  return d < s ? -px : px;
}
`

const vertexShader = /* glsl */ `
varying vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}
`

const tileShader = /* glsl */ `
${common}
uniform vec2 resolution;
varying vec2 vUv;
void main() {
  vec2 start = floor(vUv * resolution / ${tile.toFixed(1)}) * ${tile.toFixed(1)};
  float largest = 0.0;
  for (int y = 0; y < 8; y++) {
    for (int x = 0; x < 8; x++) {
      vec2 at = (start + (vec2(float(x), float(y)) + 0.5) * ${(tile / 8).toFixed(1)}) / resolution;
      largest = max(largest, abs(blurAt(distanceAt(at))));
    }
  }
  gl_FragColor = vec4(largest, 0.0, 0.0, 1.0);
}
`

const gatherShader = /* glsl */ `
${common}
uniform sampler2D tColor;
uniform sampler2D tTiles;
uniform vec2 resolution;
uniform vec2 tileStep;
varying vec2 vUv;
const float golden = 2.39996323;
void main() {
  float reach = 1.0;
  for (int y = -1; y <= 1; y++) {
    for (int x = -1; x <= 1; x++) reach = max(reach, texture2D(tTiles, vUv + vec2(float(x), float(y)) * tileStep).r);
  }
  float dc = distanceAt(vUv);
  float bc = abs(blurAt(dc));
  vec3 sum = texture2D(tColor, vUv).rgb;
  float total = 1.0;
  float spill = 0.0;
  for (int i = 0; i < SAMPLES; i++) {
    float k = float(i) + 0.5;
    float r = sqrt(k / float(SAMPLES)) * reach;
    float a = k * golden;
    vec2 uv = vUv + vec2(cos(a), sin(a)) * r / resolution;
    float ds = distanceAt(uv);
    float bs = abs(blurAt(ds));
    float cover = ds > dc ? min(bs, bc) : bs;
    float w = clamp(cover - r + 1.0, 0.0, 1.0);
    sum += texture2D(tColor, uv).rgb * w;
    total += w;
    if (ds < dc) spill += w;
  }
  float mixAmount = clamp(max(bc - 1.0, 2.0 * spill / total), 0.0, 1.0);
  gl_FragColor = vec4(sum / total, mixAmount);
}
`

const blendShader = /* glsl */ `
uniform sampler2D tColor;
uniform sampler2D tBlur;
varying vec2 vUv;
void main() {
  vec4 sharp = texture2D(tColor, vUv);
  vec4 blur = texture2D(tBlur, vUv);
  gl_FragColor = vec4(mix(sharp.rgb, blur.rgb, blur.a), sharp.a);
}
`

export class DofPass extends Pass {
  private readonly depth: () => Texture
  private readonly tiles = new WebGLRenderTarget(1, 1, { type: HalfFloatType, minFilter: LinearFilter, magFilter: LinearFilter })
  private readonly blurred = new WebGLRenderTarget(1, 1, { type: HalfFloatType, minFilter: LinearFilter, magFilter: LinearFilter })
  private readonly tileQuad: FullScreenQuad
  private readonly gatherQuad: FullScreenQuad
  private readonly blendQuad: FullScreenQuad
  private readonly size = new Vector2(1, 1)

  /** `depth` is the scene's depth for the frame being drawn, read when the pass runs. */
  constructor(depth: () => Texture, samples = 128) {
    super()
    this.depth = depth
    const lens = () => ({
      tDepth: { value: null },
      cameraNear: { value: 0.1 },
      cameraFar: { value: 1000 },
      focalMM: { value: 35 },
      fStop: { value: 2.8 },
      focusMM: { value: 10000 },
      sensorLongMM: { value: 36 },
      longSidePx: { value: 1024 },
      maxBlurPx: { value: 40 },
      resolution: { value: new Vector2() },
    })
    this.tileQuad = new FullScreenQuad(new ShaderMaterial({ uniforms: lens(), vertexShader, fragmentShader: tileShader }))
    this.gatherQuad = new FullScreenQuad(
      new ShaderMaterial({
        defines: { SAMPLES: samples },
        uniforms: { ...lens(), tColor: { value: null }, tTiles: { value: null }, tileStep: { value: new Vector2() } },
        vertexShader,
        fragmentShader: gatherShader,
      }),
    )
    this.blendQuad = new FullScreenQuad(new ShaderMaterial({ uniforms: { tColor: { value: null }, tBlur: { value: null } }, vertexShader, fragmentShader: blendShader }))
  }

  /**
   * The lens of the frame about to be drawn; distances are along the camera's
   * axis. `longSidePx` is the long side of the camera's picture in pixels:
   * the whole frame for an export, the camera view's frame in the viewport.
   */
  setLens(dof: DepthOfField, near: number, far: number, longSidePx = Math.max(this.size.x, this.size.y)) {
    for (const quad of [this.tileQuad, this.gatherQuad]) {
      const u = (quad.material as ShaderMaterial).uniforms
      u.cameraNear.value = near
      u.cameraFar.value = far
      u.focalMM.value = dof.focalMM
      u.fStop.value = dof.fStop
      u.focusMM.value = dof.focusM * 1000
      u.sensorLongMM.value = dof.sensorLongMM
      u.longSidePx.value = longSidePx
      u.maxBlurPx.value = maxBlurShare * longSidePx
    }
  }

  setSize(width: number, height: number) {
    this.size.set(width, height)
    this.tiles.setSize(Math.ceil(width / tile), Math.ceil(height / tile))
    this.blurred.setSize(Math.max(1, Math.round(width / 2)), Math.max(1, Math.round(height / 2)))
    for (const quad of [this.tileQuad, this.gatherQuad]) (quad.material as ShaderMaterial).uniforms.resolution.value.set(width, height)
    ;(this.gatherQuad.material as ShaderMaterial).uniforms.tileStep.value.set(tile / width, tile / height)
  }

  render(renderer: WebGLRenderer, writeBuffer: WebGLRenderTarget, readBuffer: WebGLRenderTarget) {
    const depth = this.depth()
    const tileU = (this.tileQuad.material as ShaderMaterial).uniforms
    tileU.tDepth.value = depth
    renderer.setRenderTarget(this.tiles)
    this.tileQuad.render(renderer)

    const gatherU = (this.gatherQuad.material as ShaderMaterial).uniforms
    gatherU.tDepth.value = depth
    gatherU.tColor.value = readBuffer.texture
    gatherU.tTiles.value = this.tiles.texture
    renderer.setRenderTarget(this.blurred)
    this.gatherQuad.render(renderer)

    const blendU = (this.blendQuad.material as ShaderMaterial).uniforms
    blendU.tColor.value = readBuffer.texture
    blendU.tBlur.value = this.blurred.texture
    renderer.setRenderTarget(this.renderToScreen ? null : writeBuffer)
    this.blendQuad.render(renderer)
  }

  dispose() {
    this.tiles.dispose()
    this.blurred.dispose()
    for (const quad of [this.tileQuad, this.gatherQuad, this.blendQuad]) {
      quad.material.dispose()
      quad.dispose()
    }
  }
}
