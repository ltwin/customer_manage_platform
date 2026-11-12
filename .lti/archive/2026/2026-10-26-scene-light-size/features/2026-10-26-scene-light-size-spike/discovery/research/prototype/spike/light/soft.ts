// The candidate soft shadows (S0 group 2), put on a scene's lights before their first build. Each candidate is a
// method name; pcf leaves the product's shadows as they are.
//
//   pcf          the product's: three's PCF, 5 Vogel samples of radius shadow.radius texels, IGN by screen position
//   pcf-radius   the control: the same with a wider radius, alike everywhere (?radius= texels)
//   pcss         Fernando 2005: a blocker search, the penumbra by similar triangles, a PCF that wide; spot lights
//                (2D maps) for now. Kernels turn with the frame (IGN offset by frameId: TRAA averages them), and a
//                receiver plane depth bias from the screen derivatives keeps a wide kernel off the receiver itself
//                (Epic design「技术选型」阴影偏置). ?search= ?samples= samples, ?maxTexels= the widest penumbra;
//                ?unroll=1 writes the sample loops out a sample at a time, as the spike first did.
//                Point lights (cube maps) compare by hand on raw samples: WGSL has no textureLoad for a depth cube,
//                and three gives the map one sampler a shader (depth-read-backends.md). ?cubeEps= texels of bias.
//   smtrace      shadow map tracing (EEVEE's way): rays to points on the light, each marched through the depth map
//                and blocked where it passes through a surface; spot lights for now. ?rays= ?steps=, ?maxTexels= the
//                longest trace, ?epsM= metres a ray must be behind a surface, ?spacing= how much denser steps are near.
import { LightShadow, NearestFilter, type DirectionalLight, type Light, type Object3D, type PointLight, type SpotLight } from 'three'
import * as tsl from 'three/tsl'
import type { TSLNode } from '../../src/creative-scene/materials/patterns.ts'

export type Method = 'pcf' | 'pcf-radius' | 'pcss' | 'smtrace'

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { Fn, Loop, abs, clamp, cross, cubeTexture, dFdx, dFdy, dot, expression, float, interleavedGradientNoise, ivec2, length, max, min, normalize, perspectiveDepthToViewZ, reference, select, texture, textureLoad, uniform, vec2, vec3, vogelDiskSample } = T
const { frameId, positionWorld, renderGroup, screenCoordinate } = tsl as unknown as Record<string, TSLNode>

type Shadowing = DirectionalLight | PointLight | SpotLight
type Filtered = LightShadow & { filterNode?: unknown }


const query = new URLSearchParams(location.search)
const num = (k: string, d: number) => Number(query.get(k) ?? d)

type Builder = { renderer: { backend: { isWebGLBackend?: boolean } } }

/**
 * Whether a map is read raw throughout, compared by hand: on ?compare=off, and always under WebGL2, which has no raw
 * read of a sampler2DShadow while three gives a map one uniform a shader (depth-read-backends.md). The map then
 * keeps no compare function, and is read nearest: three filters a PCF map linearly (ShadowNode.js:368), which
 * leaves a depth texture without compare incomplete in WebGL2, every read 0.
 */
function rawOnly(depthTexture: unknown, builder: Builder): boolean {
  if (query.get('compare') !== 'off' && !builder.renderer.backend.isWebGLBackend) return false
  Object.assign(depthTexture as object, { compareFunction: null, minFilter: NearestFilter, magFilter: NearestFilter })
  return true
}

/** The kernel's turn this pixel and frame: IGN shifted every frame (Jimenez 2014's 5.588238 step), so TRAA averages it. */
const turn = () => interleavedGradientNoise(screenCoordinate.xy.add(float(frameId).mul(5.588238))).mul(Math.PI * 2)

/**
 * The depth gradient across the receiver's plane in shadow-map uv (receiver plane depth bias, Isidoro 2006): how
 * much the receiver's own depth changes over an offset, so a wide kernel compares against the plane, not its centre.
 */
function receiverPlane(coord: TSLNode, most: TSLNode): TSLNode {
  const dx = dFdx(coord).toVar()
  const dy = dFdy(coord).toVar()
  const det = dx.x.mul(dy.y).sub(dx.y.mul(dy.x)).toVar()
  const g = vec2(dy.y.mul(dx.z).sub(dx.y.mul(dy.z)), dx.x.mul(dy.z).sub(dy.x.mul(dx.z)))
  // Flat in screen space (det ~ 0), or a silhouette's huge gradient: no plane, and a bound on how far it may lean,
  // most in the map's depth a uv (MapGeometry.slope).
  return select(abs(det).greaterThan(1e-12), clamp(g.div(det), vec2(most.negate()), vec2(most)), vec2(0))
}

/**
 * How a 2D shadow map's light stands to its receivers: depths in metres from the map's, the uv radius to search
 * for blockers of a receiver zR metres from the light, and the penumbra's uv radius for blockers at zB.
 */
type MapGeometry = {
  linear: (d: TSLNode) => TSLNode
  /**
   * The map's depth a uv for a receiver zR metres off that leans a metre deeper a metre across: the receiver
   * plane's bound is ?maxSlope= (20, about 87°) of these. A fixed bound in the map's units fitted one projection
   * and not another: 0.2 held a spot light's wall, and narrowed a sun cascade's penumbrae by half.
   */
  slope: (zR: TSLNode) => TSLNode
  search: (zR: TSLNode) => TSLNode
  penumbra: (zR: TSLNode, zB: TSLNode) => TSLNode
}

/**
 * The sample loops: a TSL Loop, one copy of its body in the shader whatever the count (?unroll=1: a copy a sample,
 * as the spike first wrote them). Unrolled, a material lit by the sun's 4 cascades and a point light carried some
 * 250 samples' code, and its shader built 3.4 times as long (soft-shadow-candidates.md「反应舱」).
 */
const unroll = query.get('unroll') === '1'

/**
 * A filter that builds its own Fn for each shadow it filters. three hoists the result of a Fn with a Loop in it to the
 * top of the shader when that Fn is called inside an If (VarNode.build, hasLoop), and marks the Fn, not the call: the
 * sun's 4 cascades share one filter (LightShadow.copy), the first marked it, and the other 3 were hoisted above the
 * If that sets their shadowPositionWorld: computed from nothing, lit everywhere. ShadowNode calls a filter once a
 * shadow (its _node), so each call makes the Fn with the loops anew. The outer one stays a Fn: PointShadowNode
 * passes its filter through a Fn's inputs, which turns a plain function into a node.
 */
const perShadow = (body: (inputs: Record<string, TSLNode>, builder: Builder) => TSLNode) =>
  Fn((inputs: Record<string, TSLNode>) => {
    // The inputs as a plain object again: the outer Fn's own does not pass on through a call.
    const own: Record<string, TSLNode> = {}
    for (const k of ['depthTexture', 'shadowCoord', 'bd3D', 'dp', 'shadow']) if (inputs[k] !== undefined) own[k] = inputs[k]
    return Fn(body)(own)
  })
function repeat(count: number, body: (i: TSLNode | number) => void) {
  if (unroll) for (let i = 0; i < count; i++) body(i)
  else (Loop as unknown as (n: number, f: (a: { i: TSLNode }) => void) => void)(count, ({ i }) => body(i))
}

/** A number the shadow holds (dotted paths reach its camera), read as each render starts. */
const held = (shadow: TSLNode, path: string, type = 'float') => reference(path, type, shadow).setGroup(renderGroup)

/**
 * A spot light's perspective map, the light of radius shadow.lightSize metres: uv per metre at depth d is
 * 1 / (2 d tan(fov/2)). Everything is read from the shadow it filters, so the filter carries over to a copy of the
 * shadow (SceneRenderer's stand-in) with nothing bound to the light it was made for.
 */
function spotGeometry(shadow: TSLNode): MapGeometry {
  const near = held(shadow, 'camera.near')
  const far = held(shadow, 'camera.far')
  const tanHalf = T.tan(held(shadow, 'camera.fov').mul(Math.PI / 360))
  const radius = held(shadow, 'lightSize')
  return {
    linear: (d) => perspectiveDepthToViewZ(d, near, far).negate(),
    // d(depth)/dz = near far / ((far − near) z²), and a uv is 2 z tan(fov/2) metres across.
    slope: (zR) => tanHalf.mul(2).mul(near).mul(far).div(far.sub(near).mul(zR)),
    search: (zR) => radius.mul(zR.sub(near)).div(zR.mul(near).mul(tanHalf).mul(2)),
    penumbra: (zR, zB) => radius.mul(zR.sub(zB)).div(zB.mul(zR).mul(tanHalf).mul(2)),
  }
}

/**
 * A cascade's orthographic map, the sun a disc shadow.lightSize degrees across: depth runs linearly near..far, a
 * metre is 1 / width of uv, and blockers zR − zB above a receiver spread its edge (zR − zB) tan(θ/2) either way.
 * Each cascade's camera moves every frame (fitCascades); its shadow is a copy of the light's, the filter with it.
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
  const searchN = num('search', 16)
  const filterN = num('samples', 32)
  const maxTexels = num('maxTexels', 48)
  return perShadow(({ depthTexture, shadowCoord, shadow }, builder) => {
    const { linear, slope, search, penumbra } = geometry(shadow)
    const mapSize = reference('mapSize', 'vec2', shadow).setGroup(renderGroup)
    const texel = float(1).div(mapSize.x)
    const uv = shadowCoord.xy.toVar()
    const z = shadowCoord.z.toVar()
    const zR = linear(z).toVar()
    const plane = receiverPlane(shadowCoord, slope(zR).mul(num('maxSlope', 20))).toVar()
    const phi = turn().toVar()
    const eps = texel.mul(num('epsTexels', 2.5)).mul(abs(plane.x).add(abs(plane.y))).add(num('epsConst', 1e-4)).toVar()
    const searchUV = min(search(zR), texel.mul(maxTexels)).toVar()
    const blockers = float(0).toVar()
    const depthSum = float(0).toVar()
    // Raw depths: three declares one sampler a texture per shader, of the kind its first sampled use needs (S0 group
    // 3, depth-read-backends.md). ?read=load reads texels without a sampler (textureLoad), so the filter keeps the
    // hardware compare and its bilinear taps; ?read=raw samples raw depths throughout and compares by hand.
    const raw = rawOnly(depthTexture, builder) || query.get('read') === 'raw'
    // The texel the sampler's nearest pick reads: floor(uv · size), the same way up as uv (S0 measured both ways).
    const depthAt = (at: TSLNode) => (raw ? texture(depthTexture, at).x : textureLoad(depthTexture, ivec2(clamp(at.mul(mapSize), vec2(0), mapSize.sub(1)))).x)
    // The first use of the map in the shader decides its sampler: a compare first, so the filter's compares build.
    const centre = (raw ? float(1) : texture(depthTexture, uv).compare(z.sub(eps))).toVar()
    // The texel under the receiver searched too: a blocker the sparse disk misses still darkens the umbra.
    const searchAt = (o: TSLNode) => {
      const d = depthAt(uv.add(o))
      const blocks = select(d.lessThan(z.add(dot(plane, o)).sub(eps)), float(1), float(0))
      blockers.addAssign(blocks)
      depthSum.addAssign(blocks.mul(linear(d)))
    }
    searchAt(vec2(0))
    repeat(searchN, (i) => searchAt(vogelDiskSample(i, searchN, phi).mul(searchUV)))
    const zB = depthSum.div(max(blockers, 1)).toVar()
    const penumbraUV = clamp(penumbra(zR, zB), texel, texel.mul(maxTexels)).toVar()
    const lit = float(0).toVar()
    repeat(filterN, (i) => {
      const o = vogelDiskSample(i, filterN, phi).mul(penumbraUV)
      const against = z.add(dot(plane, o)).sub(eps)
      lit.addAssign(raw ? select(texture(depthTexture, uv.add(o)).x.lessThan(against), float(0), float(1)) : texture(depthTexture, uv.add(o)).compare(against))
    })
    // No blocker found: lit. The compares above run either way (no compare inside a per-pixel branch).
    if (query.get('softDebug') === 'phi') return phi.div(Math.PI * 2).add(centre.mul(0))
    if (query.get('softDebug') === 'load') {
      // Raw mode only: which texel addressing reads what the sampler reads at uv.
      const sampled = texture(depthTexture, uv).x
      const at = (v: TSLNode) => textureLoad(depthTexture, ivec2(clamp(v, vec2(0), vec2(1)).mul(mapSize.sub(1)))).x
      const same = (d: TSLNode) => select(abs(d.sub(sampled)).lessThan(1e-4), float(1), float(0))
      return same(at(uv)).mul(0.25).add(same(at(vec2(uv.x, float(1).sub(uv.y)))).mul(0.5)).add(0.125)
    }
    if (query.get('softDebug') === 'blockers') return blockers.div(searchN).add(centre.mul(0))
    // Whether the raw read and the receiver's depth look like depths 10 m off (about 0.95): white where above 0.9.
    if (query.get('softDebug') === 'rawAbove') return select(depthAt(uv).greaterThan(num('above', 0.9)), float(1), float(0)).add(centre.mul(0)).add(lit.mul(0))
    if (query.get('softDebug') === 'zAbove') return select(z.greaterThan(0.9), float(1), float(0)).add(centre.mul(0)).add(lit.mul(0))
    // The estimate's inputs, scaled into 0…1: depths ÷ 20 m, the kernel ÷ 64 texels.
    const scaled: Record<string, () => TSLNode> = { zB: () => zB.div(20), zR: () => zR.div(20), kernel: () => penumbraUV.div(texel).div(64) }
    const debug = scaled[query.get('softDebug') ?? '']
    if (debug) return debug().add(centre.mul(0)).add(lit.mul(0))
    return select(blockers.lessThan(0.5), centre.mul(0).add(1), lit.div(filterN))
  })
}

/**
 * Shadow map tracing for a perspective (spot) shadow map, light of radius r metres. A ray runs from the receiver to
 * a point on the light's disc; projected from the light's centre it is a straight line in the map, along which 1/z
 * is linear. It is blocked where, behind a surface at one step, it is past that surface's depth at the next (it went
 * through, rather than off the surface's edge), or when it is still behind one where the trace stops.
 */
function spotTrace(light: SpotLight, r: number) {
  const cam = light.shadow.camera
  const near = uniform(0.5).setGroup(renderGroup).onRenderUpdate(() => cam.near)
  const far = uniform(500).setGroup(renderGroup).onRenderUpdate(() => cam.far)
  const tanHalf = uniform(1).setGroup(renderGroup).onRenderUpdate(() => Math.tan((cam.fov * Math.PI) / 360))
  const radius = uniform(r)
  const raysN = num('rays', 8)
  const stepsN = num('steps', 16)
  const maxTexels = num('maxTexels', 48)
  const epsM = num('epsM', 0.03)
  const spacing = num('spacing', 2)
  return Fn(({ depthTexture, shadowCoord, shadow }: Record<string, TSLNode>) => {
    const mapSize = reference('mapSize', 'vec2', shadow).setGroup(renderGroup)
    const texel = float(1).div(mapSize.x)
    const uv = shadowCoord.xy.toVar()
    const z = shadowCoord.z.toVar()
    const linear = (d: TSLNode) => perspectiveDepthToViewZ(d, near, far).negate()
    const depthAt = (at: TSLNode) => linear(textureLoad(depthTexture, ivec2(clamp(at.mul(mapSize), vec2(0), mapSize.sub(1)))).x)
    const zR = linear(z).toVar()
    const plane = receiverPlane(shadowCoord, tanHalf.mul(2).mul(near).mul(far).div(far.sub(near).mul(zR)).mul(num('maxSlope', 20))).toVar()
    const eps = texel.mul(num('epsTexels', 2.5)).mul(abs(plane.x).add(abs(plane.y))).add(num('epsConst', 1e-4)).toVar()
    // The first use of the map decides its sampler (depth-read-backends.md); this compare also says whether the
    // receiver starts behind a surface, seen from the light's centre.
    const centre = texture(depthTexture, uv).compare(z.sub(eps)).toVar()
    const startBehind = select(centre.lessThan(0.5), float(1), float(0)).toVar()
    const startSurface = depthAt(uv).toVar()
    // The receiver in the light's frame: lateral metres at its depth.
    const lateral = uv.sub(0.5).mul(zR.mul(tanHalf).mul(2)).toVar()
    const reachNear = float(1).sub(near.div(zR)).toVar()
    const phi = turn().toVar()
    const visible = float(0).toVar()
    for (let k = 0; k < raysN; k++) {
      const q = vogelDiskSample(k, raysN, phi).mul(radius)
      // Where the ray meets the near plane, in the map; the trace stops maxTexels along the way to it.
      const atNear = lateral.add(q.sub(lateral).mul(reachNear)).div(near.mul(tanHalf).mul(2)).add(0.5)
      const way = atNear.sub(uv).toVar()
      const stop = min(float(1), texel.mul(maxTexels).div(max(length(way), 1e-8))).toVar()
      const behind = startBehind.toVar()
      const surface = startSurface.toVar()
      const hit = float(0).toVar()
      const lastRay = zR.toVar()
      for (let i = 1; i <= stepsN; i++) {
        // Denser near the receiver (?spacing= the power): a crossing close to it is a short way in the map.
        const t = stop.mul((i / stepsN) ** spacing)
        const zRay = float(1).div(float(1).div(zR).add(t.mul(float(1).div(near).sub(float(1).div(zR)))))
        const zs = depthAt(uv.add(way.mul(t))).toVar()
        const nowBehind = select(zRay.greaterThan(zs.add(epsM)), float(1), float(0)).toVar()
        // Behind the last step's surface then, in front of that surface's depth now: the ray went through it. And
        // the same seen from this end: this step's surface between the ray's depths then and now (not off the
        // receiver's own surface on the first step).
        const crossed = behind.mul(select(zRay.lessThan(surface.add(epsM)), float(1), float(0)))
        const met = i === 1 ? float(0) : select(lastRay.greaterThan(zs.add(epsM)), float(1), float(0)).mul(float(1).sub(nowBehind))
        hit.assign(max(hit, max(crossed, query.get('meet') === '0' ? float(0) : met)))
        behind.assign(nowBehind)
        surface.assign(zs)
        lastRay.assign(zRay)
      }
      visible.addAssign(float(1).sub(max(hit, behind)))
    }
    return visible.div(raysN).add(centre.mul(0))
  })
}

/**
 * PCSS for a point light's cube map, light of radius r metres. Directions stand for positions: an offset o in the
 * plane across the way to the receiver is a direction bd3D + o, o in metres at a metre from the light. Depths are
 * radial distances (a face's depth over the direction's largest component), so samples on different faces agree.
 * The receiver's plane, from the screen derivatives of its place relative to the light, gives its distance along
 * every sampled direction: a wide kernel compares against the plane, not the receiver's own distance.
 */
function pointPCSS(rect = false) {
  const searchN = num('search', 16)
  const filterN = num('samples', 32)
  const maxTexels = num('maxTexels', 48)
  return perShadow(({ depthTexture, bd3D, shadow }, builder) => {
    const near = held(shadow, 'camera.near')
    const far = held(shadow, 'camera.far')
    const radius = held(shadow, 'lightSize')
    // PointShadowNode hands its filter the receiver only as a direction (and dp): its place less the light's,
    // whole. The shadow's camera stands where the light is (PointLightShadow.updateMatrices).
    const lightAt = held(shadow, 'camera.position', 'vec3')
    // Raw reads throughout here anyway; under WebGL2 the map must also drop its compare and read nearest.
    rawOnly(depthTexture, builder)
    // A read inside PointShadowNode's per-pixel If: WGSL needs an explicit level there, an integer for a depth cube,
    // which three r186 writes as a float for a filterable texture (WGSLNodeBuilder.generateTextureLevel); a
    // float-typed expression that reads 0u gets past it. GLSL takes an implicit level anywhere, and three's
    // textureLod for a depth texture leaves out the .x its plain texture() adds (GLSLNodeBuilder.generateTexture).
    const gl = builder.renderer.backend.isWebGLBackend
    const read = (d: TSLNode) => (gl ? cubeTexture(depthTexture, d) : cubeTexture(depthTexture, d).level(expression('0u', 'float'))).x
    const mapSize = reference('mapSize', 'vec2', shadow).setGroup(renderGroup)
    // A texel at the middle of a face, a metre from the light: the face spans 2 across.
    const texel = float(2).div(mapSize.x)
    const shadowPosition = positionWorld.sub(lightAt).toVar()
    const dR = length(shadowPosition).toVar()
    const normalOf = normalize(cross(dFdx(shadowPosition), dFdy(shadowPosition)))
    const n = select(length(cross(dFdx(shadowPosition), dFdy(shadowPosition))).greaterThan(1e-12), normalOf, bd3D.negate()).toVar()
    const planeDistance = dot(n, shadowPosition).toVar()
    const absDir = abs(bd3D)
    const tangent = normalize(cross(bd3D, select(absDir.x.greaterThan(absDir.z), vec3(0, 1, 0), vec3(1, 0, 0)))).toVar()
    const bitangent = cross(bd3D, tangent).toVar()
    // A rectangle's kernel (rect, the area lights): its axes as the receiver sees them, across the way to it, half
    // its width and height long, stretched by 2/√3 so a disc's samples spread like the rectangle's (the same second
    // moment). A disc's otherwise: radius r either way.
    const rectAxes = () => {
      const flat = (a: TSLNode) => a.sub(bd3D.mul(dot(bd3D, a)))
      const half = held(shadow, 'lightHalf', 'vec2').mul(2 / Math.sqrt(3))
      return { x: flat(held(shadow, 'lightAxisX', 'vec3')).mul(half.x).toVar(), y: flat(held(shadow, 'lightAxisY', 'vec3')).mul(half.y).toVar(), most: max(half.x, half.y) }
    }
    const axes = rect ? rectAxes() : null
    const along = (o: TSLNode) => normalize(bd3D.add(tangent.mul(o.x)).add(bitangent.mul(o.y)))
    // A unit-disc sample v at scale k (radians a metre of light): the disc's r k, or the rectangle's axes times k.
    const toward = (v: TSLNode, k: TSLNode) => (axes ? normalize(bd3D.add(axes.x.mul(v.x.mul(k))).add(axes.y.mul(v.y.mul(k)))) : along(v.mul(radius.mul(k))))
    const reach = axes ? axes.most : radius
    /** The radial distance the map holds along unit direction d. */
    const stored = (d: TSLNode) => {
      const a = abs(d)
      const axis = max(max(a.x, a.y), a.z)
      return perspectiveDepthToViewZ(read(d), near, far).negate().div(axis)
    }
    /** The receiver's plane's distance along d, held near the receiver's own where the plane runs along d. */
    const receiverAt = (d: TSLNode) => clamp(planeDistance.div(min(dot(n, d), -1e-4)), dR.mul(0.5), dR.mul(2))
    const eps = dR.mul(texel).mul(num('cubeEps', 1.5)).add(0.005).toVar()
    const phi = turn().toVar()
    // Scales (radians a metre of light), the widest reach held to maxTexels.
    const searchK = min(dR.sub(near).div(dR.mul(near)), texel.mul(maxTexels).div(reach)).toVar()
    const blockers = float(0).toVar()
    const distanceSum = float(0).toVar()
    const searchAlong = (d: TSLNode) => {
      const at = stored(d).toVar()
      const blocks = select(at.lessThan(receiverAt(d).sub(eps)), float(1), float(0))
      blockers.addAssign(blocks)
      distanceSum.addAssign(blocks.mul(at))
    }
    searchAlong(bd3D)
    repeat(searchN, (i) => searchAlong(toward(vogelDiskSample(i, searchN, phi), searchK)))
    const dB = distanceSum.div(max(blockers, 1)).toVar()
    const penumbraK = clamp(dR.sub(dB).div(dB.mul(dR)), texel.mul(0.5).div(reach), texel.mul(maxTexels).div(reach)).toVar()
    const lit = float(0).toVar()
    repeat(filterN, (i) => {
      const d = toward(vogelDiskSample(i, filterN, phi), penumbraK).toVar()
      lit.addAssign(select(stored(d).lessThan(receiverAt(d).sub(eps)), float(0), float(1)))
    })
    return select(blockers.lessThan(0.5), float(1), lit.div(filterN))
  })
}

/**
 * A shadow's copy keeps its filter and the light's size (three's LightShadow.copy leaves out filterNode): what S1's
 * shadow subclasses do by overriding copy() (Epic design, the stand-in attach point). Copies are made by
 * SunCascades for its cascades and by SceneRenderer.compile for its stand-in's lights. ?copyFilter=0 leaves three's
 * copy as it is, to see what the stand-in compiles without it.
 */
const carried = query.get('copyFilter') !== '0'
if (carried) {
  const copy = LightShadow.prototype.copy
  LightShadow.prototype.copy = function (this: Filtered & { lightSize?: number }, source: Filtered & { lightSize?: number }) {
    copy.call(this, source)
    if (source.filterNode) this.filterNode = source.filterNode
    if (source.lightSize !== undefined) this.lightSize = source.lightSize
    for (const k of ['lightAxisX', 'lightAxisY', 'lightHalf'] as const) if (k in source) (this as unknown as Record<string, unknown>)[k] = (source as unknown as Record<string, { clone(): unknown }>)[k].clone()
    return this
  } as typeof copy
}

/** PCSS on any shadow, of a light size big (metres): the area light's shadows (area.ts) take it so. */
export function pcssFor(shadow: LightShadow, kind: 'cube' | 'cube-rect' | 'spot', size: number) {
  const s = shadow as Filtered & { lightSize?: number }
  s.lightSize = size
  s.filterNode = kind === 'spot' ? mapPCSS(spotGeometry) : pointPCSS(kind === 'cube-rect')
}

/** What each method does to a light that casts: returns a line for the report. */
/** ?normalBias= metres on the PCF methods: their stripes off, so their edges can be measured against PCSS's. */
function normalBias(light: Shadowing): string {
  if (!query.has('normalBias')) return ''
  light.shadow.normalBias = num('normalBias', 0)
  return `, normalBias ${light.shadow.normalBias} m`
}

export const methods: Record<Method, (light: Shadowing, size: number) => string> = {
  pcf: (light) => `product PCF${normalBias(light)}`,
  // The control: the product's PCF with a wider radius, the same everywhere (Epic design「不采用」).
  'pcf-radius': (light) => {
    const radius = num('radius', 4)
    ;(light.shadow as LightShadow).radius = radius
    return `PCF radius ${radius} texels${normalBias(light)}`
  },
  pcss: (light, size) => {
    // A light of no size keeps three's PCF and its map's compare: no filter, the shader the product has today
    // (integration.md「尺寸为 0」). Its size moving off 0 puts the filter on, and its materials build again.
    if (size === 0) return 'product PCF (size 0)'
    const shadow = light.shadow as Filtered & { lightSize?: number }
    shadow.lightSize = size
    const how = `${num('search', 16)} + ${num('samples', 32)} samples, at most ${num('maxTexels', 48)} texels`
    if ((light as PointLight).isPointLight) {
      shadow.filterNode = pointPCSS()
      return `PCSS (cube, raw compares) r ${size} m, ${how}`
    }
    if ((light as DirectionalLight).isDirectionalLight) {
      // SunCascades clones the light's shadow for each cascade as it first builds: the filter goes with the copy.
      if (!carried) return 'product PCF (pcss: the sun needs ?copyFilter on)'
      shadow.filterNode = mapPCSS(sunGeometry)
      return `PCSS (cascades) θ ${size}°, ${how}`
    }
    if (!(light as SpotLight).isSpotLight) return 'product PCF (pcss: no such light)'
    shadow.filterNode = mapPCSS(spotGeometry)
    return `PCSS r ${size} m, ${how}`
  },
  smtrace: (light, size) => {
    if (!(light as SpotLight).isSpotLight) return 'product PCF (smtrace: spot lights only so far)'
    ;(light.shadow as Filtered).filterNode = spotTrace(light as SpotLight, size)
    return `shadow map tracing r ${size} m, ${num('rays', 8)} rays × ${num('steps', 16)} steps, at most ${num('maxTexels', 48)} texels`
  },
}

/** Puts method on every light in root that casts; returns what was done, light by light. */
export function softShadows(root: Object3D, method: Method, size: number): string[] {
  const done: string[] = []
  root.traverse((o) => {
    const light = o as Light
    if (!light.isLight || !light.castShadow) return
    // The sun's size is an angle (degrees across): ?sunSize= where a scene has both kinds, else the same number.
    const own = (light as DirectionalLight).isDirectionalLight ? num('sunSize', size) : size
    done.push(`${light.type} ${light.name || light.uuid.slice(0, 8)}: ${methods[method](light as Shadowing, own)}`)
  })
  return done
}
