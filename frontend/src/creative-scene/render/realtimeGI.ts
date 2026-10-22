import { DirectionalLight as RayLight, HalfFloatType, UnsignedByteType, Vector3, type Camera, type DataTexture, type DirectionalLight, type Layers, type PerspectiveCamera, type Scene, type SpotLight } from 'three'
import { convertToTexture, float, mix, mrt, normalView, output as outputNode, packNormalToRGB, pass, perspectiveDepthToViewZ, roughness, sample, smoothstep, uniform, unpackRGBToNormal, vec2, vec3, vec4, velocity } from 'three/tsl'
import { PassNode, type Node, type TextureNode, type UniformNode } from 'three/webgpu'
import { denoise } from 'three/addons/tsl/display/DenoiseNode.js'
import { recurrentDenoise } from 'three/addons/tsl/display/RecurrentDenoiseNode.js'
import { ssr } from 'three/addons/tsl/display/SSRNode.js'
import { sss } from 'three/addons/tsl/display/SSSNode.js'
import { temporalReproject } from 'three/addons/tsl/display/TemporalReprojectNode.js'
import { traa } from 'three/addons/tsl/display/TRAANode.js'
import { GI_INTENSITY, ssgiQuality, type Quality } from './engines.ts'
import { giAmbient, giBase, giEnvSpec, GILightingPass, installGISplit } from './giSplit.ts'
import { ssgi } from './nodes/SSGINode.js'

// The real-time GI's node graph (R2 design「实时 GI 管线」, Epic「实时 GI」):
// screen-space indirect light, reflections and AO over the workbench's own
// lighting, for perspective views of the lit look.
//
//  - An opaque prepass draws the materials as they are (no override: their
//    roughness and bumped normals count) into packed normal + roughness and
//    velocity; its depth is the depth every effect reads.
//  - The lighting pass draws the scene (glass too) with the materials taken
//    apart and without the fog (giSplit.ts): the whole picture, the base
//    colour, and the indirect diffuse and specular light, 32 bytes a pixel.
//  - SSGI, temporally reprojected and recurrently denoised (design
//    「SSGI 降噪」) in the viewport; in a picture to keep, filtered in space
//    only and averaged over frames (sceneRenderer.ts AccumulateNode): the
//    recurrent denoiser's output changes slowly from frame to frame, so the
//    average hardly lowers its noise (R2 tasks 5.1). The stochastic SSR
//    with three's own chain.
//  - The composite (design「合成」) puts the AO on the indirect light only and
//    the reflections in place of the environment's; then the fog, and the
//    viewport's helpers over it where nothing opaque is in front; then TRAA,
//    outermost of the temporal nodes, antialiases and steadies the whole.
// Nothing in the chain draws with Math.random but the DenoiseNodes, which are
// given the workbench's fixed noise.

/* eslint-disable @typescript-eslint/no-explicit-any -- three's typings leave the display nodes' TSL loosely typed */
type N = any

/** Where an SSR sample missed: the ray length three writes for the environment. */
const MISSED = 1e3

// The passes' outputs, one node each for every pipeline: three keys a pass's
// render state by its MRT node, and the scene's materials outlive a
// pipeline built per picture; a new MRT per picture would leave a set of
// render objects behind for every export.
const prepassOutputs = mrt({ output: vec4(packNormalToRGB(normalView), roughness), velocity })
const lightingOutputs = mrt({ output: outputNode, base: giBase, ambient: giAmbient, envSpec: giEnvSpec })
// Blended as the picture is: glass writes alpha 0 there, which keeps what is behind it.
for (const name of ['base', 'ambient', 'envSpec']) lightingOutputs.setBlendMode(name, lightingOutputs.getBlendMode('output'))

export type RealtimeGIOptions = {
  /** The viewport's, or a picture to keep's: the SSGI's samples, its denoising and the flicker suppression. */
  quality: Quality
  /** The layers the prepass and the lighting pass draw. */
  layers: Layers
  /**
   * The viewport's helpers (the grid, light icons, camera frustums), drawn by
   * a pass of their own: in the lighting pass they would be in the picture
   * the SSGI and the SSR sample, as light on the surface behind them. None in
   * a picture to keep.
   */
  aside: { color: TextureNode; depth: TextureNode } | null
  /** The fixed-seed noise for the AO's DenoiseNode (sceneRenderer.ts fixedNoise). */
  noise: () => DataTexture
  /**
   * The light whose contact shadows are traced on screen (mainLight), asked
   * every frame: the sun setting or another light taking over changes no
   * pipeline. None, none traced.
   */
  mainLight: () => DirectionalLight | SpotLight | null
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
  prepass.setLayers(options.layers)
  prepass.setMRT(prepassOutputs)
  prepass.getTexture('output').type = UnsignedByteType
  prepass.getTexture('velocity').type = HalfFloatType
  const normalPacked: N = prepass.getTextureNode()
  const depth = prepass.getTextureNode('depth')
  const vel: N = prepass.getTextureNode('velocity')
  // SSGI, SSR and DenoiseNode normalize what they sample; the temporal nodes unpack it themselves.
  const normal: N = sample((uv: N) => unpackRGBToNormal(normalPacked.sample(uv).rgb))

  const lit = own(new GILightingPass(PassNode.COLOR, scene, camera, { samples: 0 }))
  lit.setLayers(options.layers)
  lit.setMRT(lightingOutputs)
  const output: N = lit.getTextureNode('output')
  const base: N = lit.getTextureNode('base')
  const ambient: N = lit.getTextureNode('ambient')
  const envSpec: N = lit.getTextureNode('envSpec')
  lit.getTexture('base').type = UnsignedByteType

  const final = options.quality === 'final'
  const gi = own(ssgi(output, depth, normal, persp))
  gi.giIntensity.value = GI_INTENSITY
  gi.sliceCount.value = ssgiQuality[options.quality].slices
  gi.stepCount.value = ssgiQuality[options.quality].steps
  const giAO: N = gi.getTextureNode()
  // Filtered in space with depth and normals, as the AO is; the fixed noise keeps a picture the same every time.
  // A node made into a texture here is freed with the rest; one the display nodes convert themselves would be left behind.
  const filtered = (n: N): N => {
    const d: N = own(denoise(own(convertToTexture(n)), depth, normal, camera))
    ;(d.noiseNode as { value: DataTexture }).value = own(options.noise())
    return d
  }
  // Flicker suppression weighs bright samples down: steadier while moving, darker highlights in a picture.
  const flicker = final ? 0 : 1
  let indirect: N
  if (final) {
    indirect = filtered(vec4(giAO.rgb, 1)).rgb
  } else {
    const reprojected: N = own(temporalReproject(giAO, depth, normalPacked, vel, camera, { mode: 'diffuse', accumulate: false }))
    const denoised: N = own(recurrentDenoise(reprojected, camera, { depth, normal: normalPacked, raw: giAO, mode: 'diffuse', accumulate: true }))
    // raw.a is the AO: it sets the kernel and keeps AO edges.
    denoised.alphaSource = 'ao'
    // The history is last frame's denoised result; the denoiser is the one accumulator.
    reprojected.setHistoryTexture(denoised)
    reprojected.flickerSuppression.value = flicker
    denoised.flickerSuppression.value = flicker
    indirect = denoised.rgb
  }
  // The recurrent denoiser does not give back a denoised AO: the AO is filtered on its own.
  const aoClean: N = filtered(vec4(vec3(giAO.a), 1))
  const aoOn = uniform(1)
  const ao = mix(float(1), aoClean.r, aoOn)

  const reflect: N = own(ssr(output, depth, normal, { stochastic: true, diffuseNode: base, metalnessNode: base.a, roughnessNode: normalPacked.a, camera }))
  const specReprojected: N = own(temporalReproject(reflect.getTextureNode(), depth, normalPacked, vel, camera, { mode: 'specular', accumulate: false }))
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
  specReprojected.flickerSuppression.value = flicker
  specDenoised.flickerSuppression.value = flicker
  const reflection: N = specDenoised.rgb
  // Where this frame's ray hit something on screen (design 未决事项 L2).
  const cover: N = reflect.a.lessThan(MISSED).select(1, 0)

  // Contact shadows from the main light, along its direction (a spot's as if parallel: the rays are short).
  // SSSNode traces maxDistance times light − target, not made a unit: the sun stands 60 m from its target
  // (objects/Lighting.tsx), and its rays would run 6 m. It traces along a stand-in a unit from the origin,
  // set to the main light's direction every frame, which also gives that direction in view space.
  const ray = new RayLight()
  ray.matrixAutoUpdate = false
  const at = new Vector3()
  const to = new Vector3()
  const toLight = uniform(new Vector3())
  const contactOn = uniform(0)
  const shadow: N = own(sss(depth, camera, ray))
  const before = shadow.updateBefore.bind(shadow)
  shadow.updateBefore = (frame: unknown) => {
    const main = options.mainLight()
    contactOn.value = main ? 1 : 0
    // Nothing traced: the composite weighs the last trace by zero.
    if (!main) return undefined
    main.updateWorldMatrix(true, false)
    main.target.updateWorldMatrix(true, false)
    at.setFromMatrixPosition(main.matrixWorld).sub(to.setFromMatrixPosition(main.target.matrixWorld)).normalize()
    ray.matrixWorld.makeTranslation(at)
    toLight.value.copy(at).transformDirection(camera.matrixWorldInverse)
    return before(frame)
  }
  // A new offset every frame, which TRAA and the accumulation average.
  shadow.useTemporalFiltering = true
  // Only where the main light reaches: the composite darkens all the direct light by it, and a surface turned
  // away from the main light (its rays start inside it) shaded every other light on it in grain, cut off
  // where the rays left the screen (R2 tasks 7.3, the studio set's back wall with the sun behind it).
  const contact: N = mix(float(1), shadow.r, normal.dot(toLight).mul(4).clamp(0, 1).mul(contactOn))

  // Where the prepass saw a surface (not the background), and the lighting pass took a material apart there.
  const opaque = depth.r.lessThan(1).select(1, 0)
  const shaded = composite({ output, base, ambient, envSpec, gi: indirect, ao, contact, reflection, cover, split: ambient.a.mul(opaque) })
  // The fog as three's rangeFogFactor has it, by the view depth of the opaque surface: glass takes the fog of
  // what is behind it. The background has none, as in three, where the sky box is too near for the fog.
  const { fog } = lit
  const cameraNear = uniform(0).onRenderUpdate(() => persp.near)
  const cameraFar = uniform(0).onRenderUpdate(() => persp.far)
  const fogged: N = mix(shaded, fog.color, smoothstep(fog.near, fog.far, perspectiveDepthToViewZ(depth.r, cameraNear, cameraFar).negate()).mul(fog.on).mul(opaque))
  let picture: N = vec4(fogged, output.a)
  if (options.aside) {
    // Premultiplied, in front of the opaque scene as one pass's depth test would have it; over glass always.
    const { color: over, depth: overDepth } = options.aside
    const front: N = overDepth.r.lessThanEqual(depth.r).select(1, 0)
    picture = vec4(fogged.mul(over.a.mul(front).oneMinus()).add(over.rgb.mul(front)), output.a)
  }
  const color: N = own(traa(own(convertToTexture(picture)), depth, vel, camera))
  // TRAA's stand-in for the previous depth, drawn with on the first frame and then replaced: its dispose() leaves it.
  own(color._previousDepthNode.value as { dispose(): void })

  return {
    color,
    depth,
    passes: [
      { node: lit, layers: options.layers },
      { node: prepass, layers: options.layers },
    ],
    aoOn,
    dispose() {
      for (const d of disposables) d.dispose()
    },
  }
}
