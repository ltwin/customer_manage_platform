import { HalfFloatType, UnsignedByteType, type Camera, type DataTexture, type Layers, type PerspectiveCamera, type Scene } from 'three'
import { float, mix, mrt, normalView, output as outputNode, packNormalToRGB, pass, roughness, sample, uniform, unpackRGBToNormal, vec2, vec3, vec4, velocity } from 'three/tsl'
import type { Node, PassNode, TextureNode, UniformNode } from 'three/webgpu'
import { denoise } from 'three/addons/tsl/display/DenoiseNode.js'
import { recurrentDenoise } from 'three/addons/tsl/display/RecurrentDenoiseNode.js'
import { ssr } from 'three/addons/tsl/display/SSRNode.js'
import { temporalReproject } from 'three/addons/tsl/display/TemporalReprojectNode.js'
import { traa } from 'three/addons/tsl/display/TRAANode.js'
import type { Quality } from './engines.ts'
import { giAmbient, giBase, giEnvSpec, giSplitContext, installGISplit } from './giSplit.ts'
import { ssgi } from './nodes/SSGINode.js'

// The real-time GI's node graph (R2 design「实时 GI 管线」, Epic「实时 GI」):
// screen-space indirect light, reflections and AO over the workbench's own
// lighting, for perspective views of the clay and lit looks.
//
//  - An opaque prepass draws the materials as they are (no override: their
//    roughness and bumped normals count) into packed normal + roughness and
//    velocity; its depth is the depth every effect reads.
//  - The lighting pass draws everything (glass and the grid too) with the
//    materials taken apart (giSplit.ts): the whole picture, the base colour,
//    and the indirect diffuse and specular light, 32 bytes a pixel.
//  - SSGI, temporally reprojected and recurrently denoised (design
//    「SSGI 降噪」), and for lit the stochastic SSR with three's own chain.
//  - The composite (design「合成」) puts the AO on the indirect light only and
//    the reflections in place of the environment's, then TRAA, outermost of
//    the temporal nodes, antialiases and steadies the whole.
// Nothing in the chain draws with Math.random but the AO's DenoiseNode, which
// is given the workbench's fixed noise.

/* eslint-disable @typescript-eslint/no-explicit-any -- three's typings leave the display nodes' TSL loosely typed */
type N = any

/** Where an SSR sample missed: the ray length three writes for the environment. */
const MISSED = 1e3

export type RealtimeGIOptions = {
  /** Screen-space reflections: the lit look has them, clay does not (its environment reflection is all it gets). */
  reflections: boolean
  /** The layers the prepass draws (the opaque scene) and the lighting pass draws (the picture). */
  prepassLayers: Layers
  pictureLayers: Layers
  /** The fixed-seed noise for the AO's DenoiseNode (sceneRenderer.ts fixedNoise). */
  noise: () => DataTexture
}

export type RealtimeGI = {
  /** The linear HDR picture, antialiased, before depth of field and tone mapping. */
  color: Node<'vec4'>
  /** The prepass's depth: the depth of field reads it. */
  depth: TextureNode
  /** The scene passes with the layers each draws, to compile for. */
  passes: { node: PassNode; layers: Layers }[]
  /** Frame.ao = false: the AO multiplies by one. */
  aoOn: UniformNode<'float', number>
  setQuality(quality: Quality): void
  dispose(): void
}

/**
 * The composite (design「合成」), on linear HDR values. Written with the
 * inputs' own methods only, so the unit test runs it on plain numbers.
 */
export function composite(i: { output: N; base: N; ambient: N; envSpec: N; gi: N; ao: N; contact: N; reflection: N; cover: N; split: N }): N {
  // Diffuse colour times (1 − metalness): what the indirect diffuse light is coloured by.
  const kd = i.base.rgb.mul(i.base.a.oneMinus())
  // Direct light and emission: the whole picture without its indirect light.
  const direct = i.output.rgb.sub(i.ambient.rgb).sub(i.envSpec.rgb)
  return direct
    // Contact shadows darken the direct light, where a material was taken apart: mix(1, contact, split).
    .mul(i.contact.sub(1).mul(i.split).add(1))
    .add(i.ambient.rgb.mul(i.ao))
    .add(i.envSpec.rgb.mul(i.ao).mul(i.cover.oneMinus()))
    .add(i.reflection.mul(i.split))
    .add(kd.mul(i.gi))
}

export function realtimeGI(scene: Scene, camera: Camera, options: RealtimeGIOptions): RealtimeGI {
  installGISplit()
  const disposables: { dispose(): void }[] = []
  const own = <T extends { dispose(): void }>(n: T) => (disposables.push(n), n)
  const persp = camera as PerspectiveCamera

  const prepass = own(pass(scene, camera, { samples: 0 }))
  prepass.transparent = false
  prepass.setLayers(options.prepassLayers)
  prepass.setMRT(mrt({ output: vec4(packNormalToRGB(normalView), roughness), velocity }))
  prepass.getTexture('output').type = UnsignedByteType
  prepass.getTexture('velocity').type = HalfFloatType
  const normalPacked: N = prepass.getTextureNode()
  const depth = prepass.getTextureNode('depth')
  const vel: N = prepass.getTextureNode('velocity')
  // SSGI, SSR and DenoiseNode normalize what they sample; the temporal nodes unpack it themselves.
  const normal: N = sample((uv: N) => unpackRGBToNormal(normalPacked.sample(uv).rgb))

  const lit = own(pass(scene, camera, { samples: 0 }))
  lit.setLayers(options.pictureLayers)
  lit.contextNode = giSplitContext()
  const split = mrt({ output: outputNode, base: giBase, ambient: giAmbient, envSpec: giEnvSpec })
  // Blended as the picture is: glass writes alpha 0 there, which keeps what is behind it.
  for (const name of ['base', 'ambient', 'envSpec']) split.setBlendMode(name, split.getBlendMode('output'))
  lit.setMRT(split)
  const output: N = lit.getTextureNode('output')
  const base: N = lit.getTextureNode('base')
  const ambient: N = lit.getTextureNode('ambient')
  const envSpec: N = lit.getTextureNode('envSpec')
  lit.getTexture('base').type = UnsignedByteType

  const gi = own(ssgi(output, depth, normal, persp))
  const giAO: N = gi.getTextureNode()
  const reprojected: N = own(temporalReproject(giAO, depth, normalPacked, vel, camera, { mode: 'diffuse', accumulate: false }))
  const denoised: N = own(recurrentDenoise(reprojected, camera, { depth, normal: normalPacked, raw: giAO, mode: 'diffuse', accumulate: true }))
  // raw.a is the AO: it sets the kernel and keeps AO edges.
  denoised.alphaSource = 'ao'
  // The history is last frame's denoised result; the denoiser is the one accumulator.
  reprojected.setHistoryTexture(denoised)
  // The denoiser does not give back a denoised AO: the AO is filtered on its own.
  const aoClean: N = own(denoise(vec4(vec3(giAO.a), 1), depth, normal, camera))
  ;(aoClean.noiseNode as { value: DataTexture }).value = own(options.noise())
  const aoOn = uniform(1)
  const ao = mix(float(1), aoClean.r, aoOn)

  let reflection: N = vec3(0)
  let cover: N = float(0)
  const temporal: { flickerSuppression: UniformNode<'float', number> }[] = [reprojected, denoised]
  if (options.reflections) {
    const reflect: N = own(ssr(output, depth, normal, { stochastic: true, diffuseNode: base, metalnessNode: base.a, roughnessNode: normalPacked.a, camera }))
    const specReprojected: N = own(temporalReproject(reflect, depth, normalPacked, vel, camera, { mode: 'specular', accumulate: false }))
    const specDenoised: N = own(
      recurrentDenoise(specReprojected, camera, {
        depth,
        normal: normalPacked,
        raw: reflect,
        metalRoughness: sample((uv: N) => vec2(base.sample(uv).a, normalPacked.sample(uv).a)),
        mode: 'specular',
        accumulate: true,
      }),
    )
    specDenoised.alphaSource = 'raylength'
    // Multi-bounce: the reflection reads last frame's.
    reflect.setHistory(specDenoised, vel)
    specReprojected.setHistoryTexture(specDenoised)
    temporal.push(specReprojected, specDenoised)
    reflection = specDenoised.rgb
    // Where this frame's ray hit something on screen (design 未决事项 L2).
    cover = reflect.a.lessThan(MISSED).select(1, 0)
  }

  // Where the lighting pass took a material apart and the prepass saw a surface (not the background).
  const surface = ambient.a.mul(depth.r.lessThan(1).select(1, 0))
  const picture = vec4(
    composite({ output, base, ambient, envSpec, gi: denoised.rgb, ao, contact: float(1), reflection, cover, split: surface }),
    output.a,
  )
  const color: N = own(traa(picture, depth, vel, camera))

  return {
    color,
    depth,
    passes: [
      { node: lit, layers: options.pictureLayers },
      { node: prepass, layers: options.prepassLayers },
    ],
    aoOn,
    setQuality(quality) {
      // Flicker suppression weighs bright samples down: steadier while moving, darker highlights in an export.
      for (const t of temporal) t.flickerSuppression.value = quality === 'final' ? 0 : 1
    },
    dispose() {
      for (const d of disposables) d.dispose()
    },
  }
}
