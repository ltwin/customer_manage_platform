import { NearestFilter, type LightShadow } from 'three'
import * as tsl from 'three/tsl'
import type { TSLNode } from '../../materials/patterns.ts'
import { SceneAreaLight, SceneSphereLight, SceneSunLight, type SizedLight, type SizedShadow } from './lights.ts'

// Soft shadows of sized lights: PCSS (Epic design「软阴影的接入」, U1; S0
// soft-shadow-candidates.md). A blocker search over the shadow map, the
// penumbra from similar triangles, then a filter that wide: 16 search and 32
// filter samples on Vogel discs, turned per pixel (and per frame where frames
// are averaged), the penumbra at most MAX_TEXELS. A wide kernel compares
// against the receiver's plane (receiver plane depth bias), its lean held to
// MAX_SLOPE.
//
// The filter reads everything from the shadow it filters (shadow.camera's
// near, far, fov and bounds, the light's place, shadow.lightSize), by dotted
// references read as each render starts: it is bound to no light, so it goes
// with a copy of the shadow (the sun's cascades, a stand-in's lights;
// render/lightModel/lights.ts) and each copy reads its own.
//
// Maps are read raw on both backends and compared by hand (U7, S0
// depth-read-backends.md): a sized light's map keeps no compare function and
// is read nearest (rawDepth). three r186 gives a texture one sampler a shader,
// of the kind its first sampled use needs, so compare sampling and raw reads
// cannot share a map; without the compare, WebGL2 also needs the map nearest
// (three filters a PCF map linearly, ShadowNode.js:368, and a depth texture
// without compare is then incomplete: every read 0). A light of size 0 gets no
// filter: its map keeps three's compare and PCF, its shader the one before
// sizes (AC-S1-2).
//
// An area light (data: 面光源) casts a point light's cube map from its centre
// (Epic design U3), its kernel the face's own (scene-area-light design「软阴
// 影」): its two world axes, across the way to the receiver, at half its width
// and height. A rectangle's filter takes them 2/√3 longer, so a disc's samples
// spread as a uniform rectangle's do (the same second moment), its search √2,
// the ellipse round its corners; an ellipse's is a stretched disc as it is.
// Its export draws with more samples and a wider cap than its viewport (U5).

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { Fn, Loop, abs, clamp, cross, cubeTexture, dot, expression, float, interleavedGradientNoise, length, max, min, normalize, perspectiveDepthToViewZ, reference, select, texture, uint, uniform, vec2, vec3, vec4, vogelDiskSample } = T
const { frameId, normalWorldGeometry, positionWorld, renderGroup, screenCoordinate, shadowPositionWorld } = tsl as unknown as Record<string, TSLNode>

/** Blocker search samples. */
const SEARCH = 16
/** Filter samples. */
const FILTER = 32
/** The widest penumbra, texels (U5): wider is cut, the edge narrower but continuous. A sun's counts its coarsest cascade's texels (capTexel). */
const MAX_TEXELS = 48
/** An area light's export: search and filter samples, and its cap in texels (U5, as S0 measured the thresholds with). */
const FINAL = { search: 32, filter: 64, maxTexels: 128 }
/** How far a receiver's plane may lean, in units of a map's depth per uv for a surface at 45° (about 87°). */
const MAX_SLOPE = 20
/** The 2D maps' compare margin: texels of the plane's gradient, and a floor in the map's depth. */
const EPS_TEXELS = 2.5
const EPS_FLOOR = 1e-4
/** The cube maps' compare margin: texels at the receiver's distance, and a floor in metres. */
const CUBE_EPS_TEXELS = 1.5
const CUBE_EPS_FLOOR = 0.005

type Builder = { renderer: { backend: { isWebGLBackend?: boolean } } }
type Inputs = Record<string, TSLNode>

/** A number the shadow holds (dotted paths reach its camera), read as each render starts. */
const held = (shadow: TSLNode, path: string, type = 'float') => reference(path, type, shadow).setGroup(renderGroup)

/**
 * The map read raw: no compare function, nearest. Set as the filter builds,
 * before the map is first drawn: three's WebGL backend sets the compare mode
 * only when compareFunction is set, and setting it null later would not undo
 * it (S0 depth-read-backends.md).
 */
function rawDepth(depthTexture: TSLNode) {
  // The filter's input is the map, or a texture node holding it.
  const map = (depthTexture as unknown as { value?: object }).value ?? depthTexture
  Object.assign(map, { compareFunction: null, minFilter: NearestFilter, magFilter: NearestFilter })
}

/**
 * 1 where frames are averaged (the real-time GI's TRAA, the workbench's
 * accumulation; sceneRenderer.ts sets it each render): the kernel turns every
 * frame. 0 in the workbench's viewport, which averages none: a still noise,
 * not one that changes with every redraw.
 */
export const shadowTurning = /*@__PURE__*/ uniform(0)

/** The kernel's turn this pixel and frame: IGN shifted every frame by Jimenez 2014's 5.588238 step, the frame taken mod 64 as there (fp32 keeps the noise's digits). */
const turn = () => interleavedGradientNoise(screenCoordinate.xy.add(float(frameId.mod(uint(64))).mul(shadowTurning).mul(5.588238))).mul(Math.PI * 2)

/** Two unit ways along the surface whose geometric normal is n. */
function alongSurface(n: TSLNode): [TSLNode, TSLNode] {
  const t = normalize(cross(n, select(abs(n.y).lessThan(0.99), vec3(0, 1, 0), vec3(1, 0, 0)))).toVar()
  return [t, cross(n, t)]
}

/**
 * The depth gradient across the receiver's plane in shadow-map uv (receiver
 * plane depth bias, Isidoro 2006): how much the receiver's own depth changes
 * over an offset, so a wide kernel compares against the plane, not its centre.
 * The plane is the surface's geometric normal taken through the shadow's
 * matrix as three takes the receiver (ShadowNode.setupShadowCoord: x/w,
 * 1 − y/w, z/w), not screen derivatives: those span two surfaces at a
 * silhouette, and the cascades' and a point light's per-pixel branches leave
 * them undefined. Edge-on to the light: a bound on how far it may lean, most
 * in the map's depth a uv.
 */
function receiverPlane(shadow: TSLNode, most: TSLNode): TSLNode {
  const matrix = held(shadow, 'matrix', 'mat4')
  const clip = matrix.mul(vec4(shadowPositionWorld, 1)).toVar()
  const at = clip.xyz.div(clip.w).toVar()
  /** The coordinate's change a metre along world way t. */
  const change = (t: TSLNode) => {
    const d = matrix.mul(vec4(t, 0))
    const c = d.xyz.sub(at.mul(d.w)).div(clip.w)
    return vec3(c.x, c.y.negate(), c.z)
  }
  const [t1, t2] = alongSurface(normalWorldGeometry)
  const dx = change(t1).toVar()
  const dy = change(t2).toVar()
  const det = dx.x.mul(dy.y).sub(dx.y.mul(dy.x)).toVar()
  const g = vec2(dy.y.mul(dx.z).sub(dx.y.mul(dy.z)), dx.x.mul(dy.z).sub(dy.x.mul(dx.z)))
  return clamp(g.div(select(abs(det).greaterThan(1e-20), det, 1e-20)), vec2(most.negate()), vec2(most))
}

/**
 * A filter that builds its own Fn with the sample loops for each shadow it
 * filters. three r186 hoists the result of a Fn with a Loop in it to the top
 * of the shader when the Fn is called inside an If (VarNode.build, hasLoop),
 * and marks the Fn, not the call: the sun's cascades share one filter, the
 * first marked it, and the others were hoisted above the If that sets their
 * shadowPositionWorld, lit everywhere (lesson tsl-loop-fn-hoisted-in-branch).
 * ShadowNode calls a filter once a shadow, so each call makes the inner Fn
 * anew. The outer one stays a Fn: PointShadowNode passes its filter through a
 * Fn's inputs, which turns a plain function into a node.
 */
const perShadow = (body: (inputs: Inputs, builder: Builder) => TSLNode) =>
  Fn((inputs: Inputs) => {
    // The inputs as a plain object again: the outer Fn's own does not pass on through a call.
    const own: Inputs = {}
    for (const k of ['depthTexture', 'shadowCoord', 'bd3D', 'dp', 'shadow']) if (inputs[k] !== undefined) own[k] = inputs[k]
    return Fn(body)(own)
  })

const repeat = (count: number, body: (i: TSLNode) => void) => (Loop as unknown as (n: number, f: (a: { i: TSLNode }) => void) => void)(count, ({ i }) => body(i))

/**
 * How a 2D map's light stands to its receivers: depths in metres from the
 * map's, the map's depth a uv for a receiver zR metres off that leans a metre
 * deeper a metre across (the plane's bound is MAX_SLOPE of these), the uv
 * radius to search for blockers of a receiver at zR, and the penumbra's uv
 * radius for blockers at zB.
 */
type MapGeometry = {
  linear: (d: TSLNode) => TSLNode
  slope: (zR: TSLNode) => TSLNode
  /** The widest penumbra and search, uv. */
  cap: TSLNode
  search: (zR: TSLNode) => TSLNode
  penumbra: (zR: TSLNode, zB: TSLNode) => TSLNode
}

/** A spot light's perspective map, the light lightSize metres in radius: a uv at depth z is 2 z tan(fov/2) metres across. */
function spotGeometry(shadow: TSLNode): MapGeometry {
  const near = held(shadow, 'camera.near')
  const far = held(shadow, 'camera.far')
  const tanHalf = T.tan(held(shadow, 'camera.fov').mul(Math.PI / 360))
  const radius = held(shadow, 'lightSize')
  return {
    linear: (d) => perspectiveDepthToViewZ(d, near, far).negate(),
    // d(depth)/dz = near far / ((far − near) z²).
    slope: (zR) => tanHalf.mul(2).mul(near).mul(far).div(far.sub(near).mul(zR)),
    cap: float(MAX_TEXELS).div(held(shadow, 'mapSize', 'vec2').x),
    search: (zR) => radius.mul(zR.sub(near)).div(zR.mul(near).mul(tanHalf).mul(2)),
    penumbra: (zR, zB) => radius.mul(zR.sub(zB)).div(zB.mul(zR).mul(tanHalf).mul(2)),
  }
}

/**
 * A cascade's orthographic map, the sun a disc lightSize degrees across:
 * depth runs linearly near..far, a metre is 1 / width of uv, and blockers
 * zR − zB above a receiver spread its edge (zR − zB) tan(θ/2) either way. Each
 * cascade's camera moves every frame (fitCascades); its shadow is a copy of
 * the light's, the filter with it, its lightSize kept up by fitCascades.
 * Its cap counts the coarsest cascade's texels (capTexel, metres), so a wide
 * penumbra is cut alike on both sides of a cascade's edge.
 */
function sunGeometry(shadow: TSLNode): MapGeometry {
  const near = held(shadow, 'camera.near')
  const far = held(shadow, 'camera.far')
  const width = held(shadow, 'camera.right').sub(held(shadow, 'camera.left')).div(held(shadow, 'camera.zoom'))
  const tanHalf = T.tan(held(shadow, 'lightSize').mul(Math.PI / 360))
  return {
    linear: (d) => near.add(d.mul(far.sub(near))),
    slope: () => width.div(far.sub(near)),
    cap: max(held(shadow, 'capTexel').div(width), float(1).div(held(shadow, 'mapSize', 'vec2').x)).mul(MAX_TEXELS),
    search: (zR) => zR.sub(near).mul(tanHalf).div(width),
    penumbra: (zR, zB) => zR.sub(zB).mul(tanHalf).div(width),
  }
}

/** PCSS on a 2D shadow map (a spot light's, or a sun cascade's), the light's geometry from the shadow it filters. */
function mapPCSS(geometry: (shadow: TSLNode) => MapGeometry) {
  return perShadow(({ depthTexture, shadowCoord, shadow }) => {
    rawDepth(depthTexture)
    const { linear, slope, cap, search, penumbra } = geometry(shadow)
    const mapSize = held(shadow, 'mapSize', 'vec2')
    const texel = float(1).div(mapSize.x)
    const uv = shadowCoord.xy.toVar()
    const z = shadowCoord.z.toVar()
    const zR = linear(z).toVar()
    const plane = receiverPlane(shadow, slope(zR).mul(MAX_SLOPE)).toVar()
    const phi = turn().toVar()
    const eps = texel.mul(EPS_TEXELS).mul(abs(plane.x).add(abs(plane.y))).add(EPS_FLOOR).toVar()
    const searchUV = min(search(zR), cap).toVar()
    const depthAt = (at: TSLNode) => texture(depthTexture, at).x
    const blockers = float(0).toVar()
    const depthSum = float(0).toVar()
    const searchAt = (o: TSLNode) => {
      const d = depthAt(uv.add(o))
      const blocks = select(d.lessThan(z.add(dot(plane, o)).sub(eps)), float(1), float(0))
      blockers.addAssign(blocks)
      depthSum.addAssign(blocks.mul(linear(d)))
    }
    // The texel under the receiver searched too: a blocker the sparse disc misses still darkens the umbra.
    searchAt(vec2(0))
    repeat(SEARCH, (i) => searchAt(vogelDiskSample(i, SEARCH, phi).mul(searchUV)))
    const zB = depthSum.div(max(blockers, 1)).toVar()
    const penumbraUV = clamp(penumbra(zR, zB), texel, max(cap, texel)).toVar()
    const lit = float(0).toVar()
    repeat(FILTER, (i) => {
      const o = vogelDiskSample(i, FILTER, phi).mul(penumbraUV)
      lit.addAssign(select(depthAt(uv.add(o)).lessThan(z.add(dot(plane, o)).sub(eps)), float(0), float(1)))
    })
    // No blocker found: lit.
    return select(blockers.lessThan(0.5), float(1), lit.div(FILTER))
  })
}

/** A cube PCSS's kernel: a sphere light's disc, or an area light's face. */
type Kernel = 'disc' | 'rect' | 'ellipse'
type CubeOptions = { kernel?: Kernel; search?: number; filter?: number; maxTexels?: number }

/** An area light's kernel against a disc's: its filter's, and its search's (the module's head). */
const STRETCH: Record<Exclude<Kernel, 'disc'>, { filter: number; search: number }> = {
  rect: { filter: 2 / Math.sqrt(3), search: Math.SQRT2 },
  ellipse: { filter: 1, search: 1 },
}

/**
 * PCSS on a point light's cube map, the light lightSize metres in radius, or
 * an area light's from its centre, its face's axes for the kernel's.
 * Directions stand for positions: an offset o in the plane across the way to
 * the receiver is a direction bd3D + o, o in metres at a metre from the light.
 * Depths are radial distances (a face's depth over the direction's largest
 * component), so samples on different faces agree. The receiver's plane, its
 * geometric normal turned toward the light, gives its distance along every
 * sampled direction.
 */
function cubePCSS({ kernel = 'disc', search = SEARCH, filter = FILTER, maxTexels = MAX_TEXELS }: CubeOptions = {}) {
  return perShadow(({ depthTexture, bd3D, shadow }, builder) => {
    rawDepth(depthTexture)
    const near = held(shadow, 'camera.near')
    const far = held(shadow, 'camera.far')
    // PointShadowNode hands its filter the receiver only as a direction: its place less the light's, whole. The
    // shadow's camera stands where the light is (PointLightShadow.updateMatrices).
    const lightAt = held(shadow, 'camera.position', 'vec3')
    // A read inside PointShadowNode's per-pixel If: WGSL needs an explicit level there, an integer for a depth
    // cube, which three r186 writes as a float for a filterable texture (WGSLNodeBuilder.generateTextureLevel); a
    // float-typed expression that reads 0u gets past it. GLSL takes an implicit level anywhere, and three's
    // textureLod for a depth texture leaves out the .x its plain texture() adds (GLSLNodeBuilder.generateTexture).
    // Each has an e2e (AC-S1-10): the light's shading goes missing when its shader does not build.
    const gl = builder.renderer.backend.isWebGLBackend
    const read = (d: TSLNode) => (gl ? cubeTexture(depthTexture, d) : cubeTexture(depthTexture, d).level(expression('0u', 'float'))).x
    const mapSize = held(shadow, 'mapSize', 'vec2')
    // A texel at the middle of a face, a metre from the light: the face spans 2 across.
    const texel = float(2).div(mapSize.x)
    const shadowPosition = positionWorld.sub(lightAt).toVar()
    const dR = length(shadowPosition).toVar()
    const n = normalWorldGeometry.mul(select(dot(normalWorldGeometry, shadowPosition).greaterThan(0), float(-1), float(1))).toVar()
    const planeDistance = dot(n, shadowPosition).toVar()
    /**
     * The kernel's two axes across the way to the receiver, each times scale metres of light, at stretch c, and its
     * longest, which the caps count: a disc's unit ways times its radius; an area light's world axes across bd3D at
     * half its width and height.
     */
    let axes: (c: number) => [TSLNode, TSLNode]
    let longest: (c: number) => TSLNode
    let scale: TSLNode = float(1)
    if (kernel === 'disc') {
      const radius = held(shadow, 'lightSize')
      const axis = abs(bd3D)
      const tangent = normalize(cross(bd3D, select(axis.x.greaterThan(axis.z), vec3(0, 1, 0), vec3(1, 0, 0)))).toVar()
      const bitangent = cross(bd3D, tangent).toVar()
      axes = () => [tangent, bitangent]
      longest = () => radius
      scale = radius
    } else {
      const half = held(shadow, 'half', 'vec2')
      const across = (a: TSLNode) => a.sub(bd3D.mul(dot(a, bd3D)))
      const x = across(held(shadow, 'axisX', 'vec3')).mul(half.x).toVar()
      const y = across(held(shadow, 'axisY', 'vec3')).mul(half.y).toVar()
      axes = (c) => [x.mul(c), y.mul(c)]
      longest = (c) => max(half.x, half.y).mul(c)
    }
    const stretch = kernel === 'disc' ? { filter: 1, search: 1 } : STRETCH[kernel]
    /** A unit-disc sample v at scale k (radians a metre of light) on the kernel's axes. */
    const toward = (v: TSLNode, k: TSLNode, [a, b]: [TSLNode, TSLNode]) => normalize(bd3D.add(a.mul(v.x.mul(scale).mul(k))).add(b.mul(v.y.mul(scale).mul(k))))
    /** The radial distance the map holds along unit direction d. */
    const stored = (d: TSLNode) => {
      const a = abs(d)
      return perspectiveDepthToViewZ(read(d), near, far).negate().div(max(max(a.x, a.y), a.z))
    }
    /** The receiver's plane's distance along d, held near the receiver's own where the plane runs along d. */
    const receiverAt = (d: TSLNode) => clamp(planeDistance.div(min(dot(n, d), -1e-4)), dR.mul(0.5), dR.mul(2))
    const eps = dR.mul(texel).mul(CUBE_EPS_TEXELS).add(CUBE_EPS_FLOOR).toVar()
    const phi = turn().toVar()
    const searchAxes = axes(stretch.search)
    const searchK = min(dR.sub(near).div(dR.mul(near)), texel.mul(maxTexels).div(longest(stretch.search))).toVar()
    const blockers = float(0).toVar()
    const distanceSum = float(0).toVar()
    const searchAlong = (d: TSLNode) => {
      const at = stored(d).toVar()
      const blocks = select(at.lessThan(receiverAt(d).sub(eps)), float(1), float(0))
      blockers.addAssign(blocks)
      distanceSum.addAssign(blocks.mul(at))
    }
    searchAlong(bd3D)
    repeat(search, (i) => searchAlong(toward(vogelDiskSample(i, search, phi), searchK, searchAxes)))
    const dB = distanceSum.div(max(blockers, 1)).toVar()
    const filterAxes = axes(stretch.filter)
    const reach = longest(stretch.filter)
    const penumbraK = clamp(dR.sub(dB).div(dB.mul(dR)), texel.mul(0.5).div(reach), texel.mul(maxTexels).div(reach)).toVar()
    const lit = float(0).toVar()
    repeat(filter, (i) => {
      const d = toward(vogelDiskSample(i, filter, phi), penumbraK, filterAxes).toVar()
      lit.addAssign(select(stored(d).lessThan(receiverAt(d).sub(eps)), float(0), float(1)))
    })
    return select(blockers.lessThan(0.5), float(1), lit.div(filter))
  })
}

/**
 * Puts the soft shadow on a sized light that casts, before its first build
 * (three keeps the shadow node a light first builds): the filter for its kind
 * and its size on its shadow. A light of size 0 is left as three draws it. A
 * directional light's goes on before cascadeSunShadow: CSMShadowNode clones
 * light.shadow, filter and all, as it first builds. An area light always has
 * a size: its kernel is its shape's, with an export's samples where final
 * (the viewport and an export build their lights apart, objects/SceneLights.tsx).
 */
export function attachSoftShadow(light: SizedLight | SceneAreaLight, final = false) {
  const shadow = light.shadow as LightShadow & SizedShadow
  if (light instanceof SceneAreaLight) {
    shadow.filterNode ??= cubePCSS({ kernel: light.shape === 'ellipse' ? 'ellipse' : 'rect', ...(final ? FINAL : {}) })
    return
  }
  shadow.lightSize = light.size
  if (!(light.size > 0) || shadow.filterNode) return
  shadow.filterNode = light instanceof SceneSphereLight ? cubePCSS() : mapPCSS(light instanceof SceneSunLight ? sunGeometry : spotGeometry)
}

