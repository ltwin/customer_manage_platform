import { DirectionalLight as RayLight, HalfFloatType, Matrix4, SRGBColorSpace, UnsignedByteType, Vector3, type Camera, type Color, type DataTexture, type DirectionalLight, type Layers, type PerspectiveCamera, type Scene, type SpotLight } from 'three'
import { convertToTexture, Fn, float, getViewPosition, max, mix, mrt, normalize, normalView, output as outputNode, packNormalToRGB, pass, perspectiveDepthToViewZ, roughness, sample, screenUV, select, smoothstep, uniform, unpackRGBToNormal, vec2, vec3, vec4, velocity } from 'three/tsl'
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
import { ssrTrace } from './nodes/SSRTraceNode.js'
import type { HitShading } from './trace/hit.ts'
import { probeIrradiance, type ProbeNodes } from './probes/lookup.ts'
import { aerialBehind } from '../world/atmosphere/aerial.ts'
import { applyVolumes } from './volumes/composite.ts'
import { VolumeNode } from './volumes/march.ts'

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
//    the reflections in place of the environment's; then the fog, the aerial
//    perspective and the volumes (W5, render/volumes/), and the viewport's
//    helpers over it where nothing opaque is in front; then TRAA,
//    outermost of the temporal nodes, antialiases and steadies the whole.
// Nothing in the chain draws with Math.random but the DenoiseNodes, which are
// given the workbench's fixed noise.

/* eslint-disable @typescript-eslint/no-explicit-any -- three's typings leave the display nodes' TSL loosely typed */
type N = any

/**
 * The prepass roughness at and below which a reflection is a mirror's, not filtered in space where the layer draws:
 * three's getRoughness puts a floor of 0.0525 under it (the base mip of a 256 cubemap), and the normal's derivatives
 * add a little.
 */
export const MIRROR_ROUGHNESS = 0.06

/** Where an SSR sample missed: the ray length three writes for the environment. */
const MISSED = 1e3

/** The roughness below which the reflections retrace what the screen missed (S1 design「反射补追」, Epic U3): all of it in a picture to keep. */
export const retraceBelow: Record<Quality, number> = { interactive: 0.5, final: 1 }

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
  /** The layers the prepass draws: what has a surface. */
  layers: Layers
  /** The layers the lighting pass draws: those and the sky behind them. */
  pictureLayers: Layers
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
  /** The viewport's redraw: a new march for the volumes compiles ahead and draws then (volumes/march.ts). */
  redraw: (() => void) | null
  /**
   * The world-space layer, once it is ready (render/trace/base.ts): the
   * reflections retrace what the screen missed through it. None, three's
   * SSR as before (S1 L1).
   */
  trace: HitShading | null
  /**
   * The probes, once warm (S2 design「合成」): the composite takes their
   * irradiance in place of the environment's where they cover; ambient is the
   * scene's ambient lights as the base's tables last added them up (exposed,
   * L_amb). None, the screen-space indirect light as before (P1). ao, the
   * e2e's S2-U7 comparison alone (gpuTestHooks.probeAO): the probes' light
   * times the SSGI's AO at that radius in metres, contact shadows smaller
   * than a probe cell.
   */
  world: { probes: ProbeNodes; ambient: Color; fill: number; ao?: number | null } | null
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
  /** The reflections' material when they trace, its pipeline watched (SceneGPU.pipelineFailed); null without the layer. */
  traced: object | null
  dispose(): void
}

/**
 * The composite (design「合成」), on linear HDR values. Written with the
 * inputs' own methods only, so the unit test runs it on plain numbers.
 *
 * With the world-space layer (world; S2 design「合成」) the probes' light
 * takes the place of the environment map's irradiance in the material's
 * indirect diffuse light, as far as they cover the point: c (ambient's
 * alpha, giSplit.ts diffuseShare) takes the scene's ambient lights' share
 * (A_light, L_amb through the material) out of the rest (A_env); the probes
 * give kd · c · E / π without the screen's AO, which they need not have
 * (their visibility is their own); the ambient lights keep theirs with the AO
 * and the fill. The SSGI's light is not added: the probes carry the bounces.
 */
export function composite(i: { output: N; base: N; ambient: N; envSpec: N; gi: N; ao: N; contact: N; reflection: N; cover: N; split: N; world?: { probe: N; ambientLight: N; fill: N; probeAO?: N } }): N {
  // Diffuse colour times (1 − metalness): what the indirect diffuse light is coloured by.
  const kd = i.base.rgb.mul(i.base.a.oneMinus())
  // Direct light and emission: the whole picture without its indirect light.
  const direct = i.output.rgb.sub(i.ambient.rgb).sub(i.envSpec.rgb)
  const lit = direct
    // Contact shadows darken the direct light, where a material was taken apart: mix(1, contact, split).
    .mul(i.contact.sub(1).mul(i.split).add(1))
    .add(i.envSpec.rgb.mul(i.ao).mul(i.cover.oneMinus()))
    .add(i.reflection.mul(i.split))
  if (!i.world) return lit.add(i.ambient.rgb.mul(i.ao)).add(kd.mul(i.gi))
  const { probe, ambientLight, fill, probeAO } = i.world
  const share = kd.mul(i.ambient.a).mul(1 / Math.PI)
  const aLight = share.mul(ambientLight)
  const aEnv = i.ambient.rgb.sub(aLight).max(0)
  const covered = probe.a
  const fromProbes = probeAO ? share.mul(probe.rgb).mul(probeAO) : share.mul(probe.rgb)
  // mix(A_env · ao, D_probe, coverage) + A_light · fill · ao
  const diffuse = aEnv.mul(i.ao).mul(covered.oneMinus()).add(fromProbes.mul(covered)).add(aLight.mul(fill).mul(i.ao))
  return lit.add(diffuse)
}

/**
 * The probes' light on screen (S2 design「屏幕采样 pass」): a pass of its own,
 * each pixel's point and normal from the prepass, its irradiance from the
 * probes, exposed, and their coverage: vec4(E, coverage). Its own texture: the
 * composite's pass is near WebGPU's 16 textures a stage, and it can be drawn
 * at half the resolution if the budget asks for it. The camera's matrices are
 * uniforms of its own: the pass draws a quad with a camera of its own.
 */
function probeLight(world: NonNullable<RealtimeGIOptions['world']>, depth: N, normal: N, camera: Camera, own: <T extends { dispose(): void }>(n: T) => T) {
  const projectionInverse = uniform(new Matrix4()).onRenderUpdate(() => camera.projectionMatrixInverse)
  const cameraWorld = uniform(new Matrix4()).onRenderUpdate(() => camera.matrixWorld)
  const eye = uniform(new Vector3()).onRenderUpdate(() => new Vector3().setFromMatrixPosition(camera.matrixWorld))
  const light = Fn(() => {
    const d = depth.r.toVar()
    const P = cameraWorld.mul(vec4(getViewPosition(screenUV, d, projectionInverse), 1)).xyz.toVar()
    const n = normalize(cameraWorld.mul(vec4(normal.sample(screenUV).xyz, 0)).xyz).toVar()
    const { pre, coverage } = probeIrradiance(world.probes, P, n, normalize((eye as N).sub(P)))
    return select(d.lessThan(1), vec4(pre.div(max(coverage, 1e-6)), coverage), vec4(0))
  })()
  const probe: N = own(convertToTexture(light))
  const ambientLight = uniform(new Vector3()).onRenderUpdate(() => new Vector3(world.ambient.r, world.ambient.g, world.ambient.b))
  return { probe, ambientLight, fill: uniform(world.fill) }
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
  lit.setLayers(options.pictureLayers)
  lit.setMRT(lightingOutputs)
  const output: N = lit.getTextureNode('output')
  const base: N = lit.getTextureNode('base')
  const ambient: N = lit.getTextureNode('ambient')
  const envSpec: N = lit.getTextureNode('envSpec')
  // 8 bits a channel, sRGB-encoded: a dark albedo keeps its precision where it carries the probes' light (S2
  // design「合成」, D17), at no more bytes a pixel. The GPU encodes on write and decodes on sampling; the alpha
  // (metalness) stays linear.
  lit.getTexture('base').type = UnsignedByteType
  lit.getTexture('base').colorSpace = SRGBColorSpace

  const final = options.quality === 'final'
  const world = options.world
  const gi = own(ssgi(output, depth, normal, persp))
  // With the probes the SSGI gives the AO alone: its light is not composited (Epic I6, S2 design D10).
  gi.giIntensity.value = world ? 0 : GI_INTENSITY
  if (world?.ao) {
    gi.useScreenSpaceSampling.value = false
    gi.radius.value = world.ao
  }
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
  let indirect: N = vec3(0)
  if (world) {
    // The SSGI's light goes nowhere: no denoising chain for it.
  } else if (final) {
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

  const reflected = { stochastic: true, diffuseNode: base, metalnessNode: base.a, roughnessNode: normalPacked.a, camera }
  const reflect: N = own(options.trace ? ssrTrace(output, depth, normal, { ...reflected, trace: options.trace, rMax: retraceBelow[options.quality] }) : ssr(output, depth, normal, reflected))
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
  // A mirror's reflection is left out of the denoiser where the layer draws (S1 design A11): its lobe has little
  // noise to filter, while the spatial filter's radius has a floor (√roughness at least 0.01) and grows with the
  // ray's length, so a retraced ray's mirror took eight taps from up to some 7 pixels off, other objects' colours
  // among them. The denoiser's temporal accumulation goes with it (specDenoised is both): a mirror's edges are
  // antialiased by the TRAA alone. Up to MIRROR_ROUGHNESS this frame's colour, faded into the denoised one by
  // twice that.
  const reflection: N = options.trace ? mix(reflect.rgb, specDenoised.rgb, smoothstep(MIRROR_ROUGHNESS, MIRROR_ROUGHNESS * 2, normalPacked.a)) : specDenoised.rgb
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

  // Where the prepass saw a surface (not the background), and the lighting pass took a material apart there
  // (its c, above 0: giSplit.ts diffuseShare).
  const opaque = depth.r.lessThan(1).select(1, 0)
  const shaded = composite({ output, base, ambient, envSpec, gi: indirect, ao, contact, reflection, cover, split: ambient.a.greaterThan(0).select(opaque, 0), world: world ? { ...probeLight(world, depth, normal, camera, own), probeAO: world.ao ? ao : undefined } : undefined })
  // The fog as three's rangeFogFactor has it, by the view depth of the opaque surface: glass takes the fog of
  // what is behind it. The background has none, as in three, where the sky box is too near for the fog.
  // Then the aerial perspective (W4, world/atmosphere/aerial.ts), likewise by the opaque surface.
  const { fog } = lit
  const cameraNear = uniform(0).onRenderUpdate(() => persp.near)
  const cameraFar = uniform(0).onRenderUpdate(() => persp.far)
  const aerial: N = aerialBehind(
    mix(shaded, fog.color, smoothstep(fog.near, fog.far, perspectiveDepthToViewZ(depth.r, cameraNear, cameraFar).negate()).mul(fog.on).mul(opaque)),
    depth,
    camera,
  )
  // The volumes after the lighting pass, which draws the shadow maps they read (same fixed noise: an export needs no settled frames).
  // With the helpers over it, over the picture so far as a texture of its own: in the one pass, its textures, the
  // volumes' and the helpers' were 17, over WebGPU's 16 a stage, and the viewport drew nothing. An export has no helpers.
  const fogged: N = applyVolumes(options.aside ? own(convertToTexture(aerial)) : aerial, depth, camera, own(new VolumeNode(depth, camera, [output], options.redraw)))
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
      { node: lit, layers: options.pictureLayers },
      { node: prepass, layers: options.layers },
    ],
    aoOn,
    // Our fork's own material (nodes/SSRTraceNode.js), not upstream's API.
    traced: options.trace ? (reflect as unknown as { _ssrMaterial: object })._ssrMaterial : null,
    dispose() {
      for (const d of disposables) d.dispose()
    },
  }
}
