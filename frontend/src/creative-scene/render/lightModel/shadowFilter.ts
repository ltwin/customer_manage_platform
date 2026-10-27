import { NearestFilter, type LightShadow } from 'three'
import * as tsl from 'three/tsl'
import type { TSLNode } from '../../materials/patterns.ts'
import { SceneSphereLight, SceneSunLight, type SizedLight, type SizedShadow } from './lights.ts'

// Soft shadows of sized lights: PCSS (Epic design「软阴影的接入」, U1; S0
// soft-shadow-candidates.md). A blocker search over the shadow map, the
// penumbra from similar triangles, then a filter that wide: 16 search and 32
// filter samples on Vogel discs, turned per pixel and frame, the penumbra at
// most MAX_TEXELS. A wide kernel compares against the receiver's plane
// (receiver plane depth bias), its lean held to MAX_SLOPE.
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

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { Fn, Loop, abs, clamp, cross, cubeTexture, dFdx, dFdy, dot, expression, float, interleavedGradientNoise, length, max, min, normalize, perspectiveDepthToViewZ, reference, select, texture, vec2, vec3, vogelDiskSample } = T
const { frameId, positionWorld, renderGroup, screenCoordinate } = tsl as unknown as Record<string, TSLNode>

/** Blocker search samples. */
const SEARCH = 16
/** Filter samples. */
const FILTER = 32
/** The widest penumbra, texels (U5): wider is cut, the edge narrower but continuous. */
const MAX_TEXELS = 48
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

/** The kernel's turn this pixel and frame: IGN shifted every frame (Jimenez 2014's 5.588238 step), which TRAA and the workbench's accumulation average. */
const turn = () => interleavedGradientNoise(screenCoordinate.xy.add(float(frameId).mul(5.588238))).mul(Math.PI * 2)

/**
 * The depth gradient across the receiver's plane in shadow-map uv (receiver
 * plane depth bias, Isidoro 2006): how much the receiver's own depth changes
 * over an offset, so a wide kernel compares against the plane, not its centre.
 * Flat in screen space, or a silhouette's huge gradient: no plane, and a bound
 * on how far it may lean, most in the map's depth a uv.
 */
function receiverPlane(coord: TSLNode, most: TSLNode): TSLNode {
  const dx = dFdx(coord).toVar()
  const dy = dFdy(coord).toVar()
  const det = dx.x.mul(dy.y).sub(dx.y.mul(dy.x)).toVar()
  const g = vec2(dy.y.mul(dx.z).sub(dx.y.mul(dy.z)), dx.x.mul(dy.z).sub(dy.x.mul(dx.z)))
  return select(abs(det).greaterThan(1e-12), clamp(g.div(det), vec2(most.negate()), vec2(most)), vec2(0))
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
 */
function sunGeometry(shadow: TSLNode): MapGeometry {
  const near = held(shadow, 'camera.near')
  const far = held(shadow, 'camera.far')
  const width = held(shadow, 'camera.right').sub(held(shadow, 'camera.left')).div(held(shadow, 'camera.zoom'))
  const tanHalf = T.tan(held(shadow, 'lightSize').mul(Math.PI / 360))
  return {
    linear: (d) => near.add(d.mul(far.sub(near))),
    slope: () => width.div(far.sub(near)),
    search: (zR) => zR.sub(near).mul(tanHalf).div(width),
    penumbra: (zR, zB) => zR.sub(zB).mul(tanHalf).div(width),
  }
}

/** PCSS on a 2D shadow map (a spot light's, or a sun cascade's), the light's geometry from the shadow it filters. */
function mapPCSS(geometry: (shadow: TSLNode) => MapGeometry) {
  return perShadow(({ depthTexture, shadowCoord, shadow }) => {
    rawDepth(depthTexture)
    const { linear, slope, search, penumbra } = geometry(shadow)
    const mapSize = held(shadow, 'mapSize', 'vec2')
    const texel = float(1).div(mapSize.x)
    const uv = shadowCoord.xy.toVar()
    const z = shadowCoord.z.toVar()
    const zR = linear(z).toVar()
    const plane = receiverPlane(shadowCoord, slope(zR).mul(MAX_SLOPE)).toVar()
    const phi = turn().toVar()
    const eps = texel.mul(EPS_TEXELS).mul(abs(plane.x).add(abs(plane.y))).add(EPS_FLOOR).toVar()
    const searchUV = min(search(zR), texel.mul(MAX_TEXELS)).toVar()
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
    const penumbraUV = clamp(penumbra(zR, zB), texel, texel.mul(MAX_TEXELS)).toVar()
    const lit = float(0).toVar()
    repeat(FILTER, (i) => {
      const o = vogelDiskSample(i, FILTER, phi).mul(penumbraUV)
      lit.addAssign(select(depthAt(uv.add(o)).lessThan(z.add(dot(plane, o)).sub(eps)), float(0), float(1)))
    })
    // No blocker found: lit.
    return select(blockers.lessThan(0.5), float(1), lit.div(FILTER))
  })
}

/**
 * PCSS on a point light's cube map, the light lightSize metres in radius.
 * Directions stand for positions: an offset o in the plane across the way to
 * the receiver is a direction bd3D + o, o in metres at a metre from the light.
 * Depths are radial distances (a face's depth over the direction's largest
 * component), so samples on different faces agree. The receiver's plane, from
 * the screen derivatives of its place relative to the light, gives its
 * distance along every sampled direction.
 */
function cubePCSS() {
  return perShadow(({ depthTexture, bd3D, shadow }, builder) => {
    rawDepth(depthTexture)
    const near = held(shadow, 'camera.near')
    const far = held(shadow, 'camera.far')
    const radius = held(shadow, 'lightSize')
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
    const across = cross(dFdx(shadowPosition), dFdy(shadowPosition))
    const n = select(length(across).greaterThan(1e-12), normalize(across), bd3D.negate()).toVar()
    const planeDistance = dot(n, shadowPosition).toVar()
    const axis = abs(bd3D)
    const tangent = normalize(cross(bd3D, select(axis.x.greaterThan(axis.z), vec3(0, 1, 0), vec3(1, 0, 0)))).toVar()
    const bitangent = cross(bd3D, tangent).toVar()
    /** A unit-disc sample v at scale k (radians a metre of light): radius r k across the way to the receiver. */
    const toward = (v: TSLNode, k: TSLNode) => normalize(bd3D.add(tangent.mul(v.x.mul(radius).mul(k))).add(bitangent.mul(v.y.mul(radius).mul(k))))
    /** The radial distance the map holds along unit direction d. */
    const stored = (d: TSLNode) => {
      const a = abs(d)
      return perspectiveDepthToViewZ(read(d), near, far).negate().div(max(max(a.x, a.y), a.z))
    }
    /** The receiver's plane's distance along d, held near the receiver's own where the plane runs along d. */
    const receiverAt = (d: TSLNode) => clamp(planeDistance.div(min(dot(n, d), -1e-4)), dR.mul(0.5), dR.mul(2))
    const eps = dR.mul(texel).mul(CUBE_EPS_TEXELS).add(CUBE_EPS_FLOOR).toVar()
    const phi = turn().toVar()
    const searchK = min(dR.sub(near).div(dR.mul(near)), texel.mul(MAX_TEXELS).div(radius)).toVar()
    const blockers = float(0).toVar()
    const distanceSum = float(0).toVar()
    const searchAlong = (d: TSLNode) => {
      const at = stored(d).toVar()
      const blocks = select(at.lessThan(receiverAt(d).sub(eps)), float(1), float(0))
      blockers.addAssign(blocks)
      distanceSum.addAssign(blocks.mul(at))
    }
    searchAlong(bd3D)
    repeat(SEARCH, (i) => searchAlong(toward(vogelDiskSample(i, SEARCH, phi), searchK)))
    const dB = distanceSum.div(max(blockers, 1)).toVar()
    const penumbraK = clamp(dR.sub(dB).div(dB.mul(dR)), texel.mul(0.5).div(radius), texel.mul(MAX_TEXELS).div(radius)).toVar()
    const lit = float(0).toVar()
    repeat(FILTER, (i) => {
      const d = toward(vogelDiskSample(i, FILTER, phi), penumbraK).toVar()
      lit.addAssign(select(stored(d).lessThan(receiverAt(d).sub(eps)), float(0), float(1)))
    })
    return select(blockers.lessThan(0.5), float(1), lit.div(FILTER))
  })
}

/**
 * Puts the soft shadow on a sized light that casts, before its first build
 * (three keeps the shadow node a light first builds): the filter for its kind
 * and its size on its shadow. A light of size 0 is left as three draws it. A
 * directional light's goes on before cascadeSunShadow: CSMShadowNode clones
 * light.shadow, filter and all, as it first builds.
 */
export function attachSoftShadow(light: SizedLight) {
  const shadow = light.shadow as LightShadow & SizedShadow
  shadow.lightSize = light.size
  if (!(light.size > 0) || shadow.filterNode) return
  shadow.filterNode = light instanceof SceneSphereLight ? cubePCSS() : mapPCSS(light instanceof SceneSunLight ? sunGeometry : spotGeometry)
}

