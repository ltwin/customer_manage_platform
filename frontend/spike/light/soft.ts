// The candidate soft shadows (S0 group 2), put on a scene's lights before their first build. Each candidate is a
// method name; pcf leaves the product's shadows as they are.
//
//   pcf          the product's: three's PCF, 5 Vogel samples of radius shadow.radius texels, IGN by screen position
//   pcf-radius   the control: the same with a wider radius, alike everywhere (?radius= texels)
//   pcss         Fernando 2005: a blocker search, the penumbra by similar triangles, a PCF that wide; spot lights
//                (2D maps) for now. Kernels turn with the frame (IGN offset by frameId: TRAA averages them), and a
//                receiver plane depth bias from the screen derivatives keeps a wide kernel off the receiver itself
//                (Epic design「技术选型」阴影偏置). ?search= ?samples= samples, ?maxTexels= the widest penumbra.
//                Point lights (cube maps) compare by hand on raw samples: WGSL has no textureLoad for a depth cube,
//                and three gives the map one sampler a shader (depth-read-backends.md). ?cubeEps= texels of bias.
//   smtrace      shadow map tracing (EEVEE's way): rays to points on the light, each marched through the depth map
//                and blocked where it passes through a surface; spot lights for now. ?rays= ?steps=, ?maxTexels= the
//                longest trace, ?epsM= metres a ray must be behind a surface, ?spacing= how much denser steps are near.
import { Vector3, type DirectionalLight, type Light, type LightShadow, type Object3D, type PointLight, type SpotLight } from 'three'
import * as tsl from 'three/tsl'
import type { TSLNode } from '../../src/creative-scene/materials/patterns.ts'

export type Method = 'pcf' | 'pcf-radius' | 'pcss' | 'smtrace'

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { Fn, abs, clamp, cross, cubeTexture, dFdx, dFdy, dot, expression, float, interleavedGradientNoise, ivec2, length, max, min, normalize, perspectiveDepthToViewZ, reference, select, texture, textureLoad, uniform, vec2, vec3, vogelDiskSample } = T
const { frameId, positionWorld, renderGroup, screenCoordinate } = tsl as unknown as Record<string, TSLNode>

type Shadowing = DirectionalLight | PointLight | SpotLight
type Filtered = LightShadow & { filterNode?: unknown }

const query = new URLSearchParams(location.search)
const num = (k: string, d: number) => Number(query.get(k) ?? d)

/** The kernel's turn this pixel and frame: IGN shifted every frame (Jimenez 2014's 5.588238 step), so TRAA averages it. */
const turn = () => interleavedGradientNoise(screenCoordinate.xy.add(float(frameId).mul(5.588238))).mul(Math.PI * 2)

/**
 * The depth gradient across the receiver's plane in shadow-map uv (receiver plane depth bias, Isidoro 2006): how
 * much the receiver's own depth changes over an offset, so a wide kernel compares against the plane, not its centre.
 */
function receiverPlane(coord: TSLNode): TSLNode {
  const dx = dFdx(coord).toVar()
  const dy = dFdy(coord).toVar()
  const det = dx.x.mul(dy.y).sub(dx.y.mul(dy.x)).toVar()
  const g = vec2(dy.y.mul(dx.z).sub(dx.y.mul(dy.z)), dx.x.mul(dy.z).sub(dy.x.mul(dx.z)))
  // Flat in screen space (det ~ 0), or a silhouette's huge gradient: no plane, and a bound on how far it may lean
  // (?planeMax=; a wall 11 m off at a grazing angle leans 0.08 a uv).
  const most = num('planeMax', 0.2)
  return select(abs(det).greaterThan(1e-12), clamp(g.div(det), vec2(-most), vec2(most)), vec2(0))
}

/** PCSS for a perspective (spot) shadow map: light of radius r metres. */
function spotPCSS(light: SpotLight, r: number) {
  const cam = light.shadow.camera
  const near = uniform(0.5).setGroup(renderGroup).onRenderUpdate(() => cam.near)
  const far = uniform(500).setGroup(renderGroup).onRenderUpdate(() => cam.far)
  const tanHalf = uniform(1).setGroup(renderGroup).onRenderUpdate(() => Math.tan((cam.fov * Math.PI) / 360))
  const radius = uniform(r)
  const searchN = num('search', 16)
  const filterN = num('samples', 32)
  const maxTexels = num('maxTexels', 48)
  return Fn(({ depthTexture, shadowCoord, shadow }: Record<string, TSLNode>) => {
    const mapSize = reference('mapSize', 'vec2', shadow).setGroup(renderGroup)
    const texel = float(1).div(mapSize.x)
    const uv = shadowCoord.xy.toVar()
    const z = shadowCoord.z.toVar()
    const linear = (d: TSLNode) => perspectiveDepthToViewZ(d, near, far).negate()
    const zR = linear(z).toVar()
    const plane = receiverPlane(shadowCoord).toVar()
    const phi = turn().toVar()
    const eps = texel.mul(num('epsTexels', 2.5)).mul(abs(plane.x).add(abs(plane.y))).add(num('epsConst', 1e-4)).toVar()
    // uv per metre at depth d: 1 / (2 d tan(fov/2)).
    const searchUV = min(radius.mul(zR.sub(near)).div(zR.mul(near).mul(tanHalf).mul(2)), texel.mul(maxTexels)).toVar()
    const blockers = float(0).toVar()
    const depthSum = float(0).toVar()
    // Raw depths: three declares one sampler a texture per shader, of the kind its first sampled use needs (S0 group
    // 3, depth-read-backends.md). ?read=load reads texels without a sampler (textureLoad), so the filter keeps the
    // hardware compare and its bilinear taps; ?read=raw samples raw depths throughout and compares by hand.
    const raw = query.get('read') === 'raw'
    // The texel the sampler's nearest pick reads: floor(uv · size), the same way up as uv (S0 measured both ways).
    const depthAt = (at: TSLNode) => (raw ? texture(depthTexture, at).x : textureLoad(depthTexture, ivec2(clamp(at.mul(mapSize), vec2(0), mapSize.sub(1)))).x)
    // The first use of the map in the shader decides its sampler: a compare first, so the filter's compares build.
    const centre = (raw ? float(1) : texture(depthTexture, uv).compare(z.sub(eps))).toVar()
    // The texel under the receiver searched too: a blocker the sparse disk misses still darkens the umbra.
    for (let i = -1; i < searchN; i++) {
      const o = i < 0 ? vec2(0) : vogelDiskSample(i, searchN, phi).mul(searchUV)
      const d = depthAt(uv.add(o))
      const blocks = select(d.lessThan(z.add(dot(plane, o)).sub(eps)), float(1), float(0))
      blockers.addAssign(blocks)
      depthSum.addAssign(blocks.mul(linear(d)))
    }
    const zB = depthSum.div(max(blockers, 1)).toVar()
    const penumbraUV = clamp(radius.mul(zR.sub(zB)).div(zB.mul(zR).mul(tanHalf).mul(2)), texel, texel.mul(maxTexels)).toVar()
    const lit = float(0).toVar()
    for (let i = 0; i < filterN; i++) {
      const o = vogelDiskSample(i, filterN, phi).mul(penumbraUV)
      const against = z.add(dot(plane, o)).sub(eps)
      lit.addAssign(raw ? select(texture(depthTexture, uv.add(o)).x.lessThan(against), float(0), float(1)) : texture(depthTexture, uv.add(o)).compare(against))
    }
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
    // The estimate's inputs, scaled into 0…1: depths ÷ 20 m, the kernel ÷ 64 texels, near ÷ 10, far ÷ 1000.
    const scaled: Record<string, () => TSLNode> = { zB: () => zB.div(20), zR: () => zR.div(20), kernel: () => penumbraUV.div(texel).div(64), near: () => near.div(10), far: () => far.div(1000) }
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
    const plane = receiverPlane(shadowCoord).toVar()
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
function pointPCSS(light: PointLight, r: number) {
  const cam = light.shadow.camera
  const near = uniform(0.5).setGroup(renderGroup).onRenderUpdate(() => cam.near)
  const far = uniform(500).setGroup(renderGroup).onRenderUpdate(() => cam.far)
  const radius = uniform(r)
  // PointShadowNode hands its filter the receiver only as a direction (and dp): its place less the light's, whole.
  const lightAt = uniform(new Vector3()).setGroup(renderGroup).onRenderUpdate(() => light.getWorldPosition(new Vector3()))
  const searchN = num('search', 16)
  const filterN = num('samples', 32)
  const maxTexels = num('maxTexels', 48)
  return Fn(({ depthTexture, bd3D, shadow }: Record<string, TSLNode>) => {
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
    const along = (o: TSLNode) => normalize(bd3D.add(tangent.mul(o.x)).add(bitangent.mul(o.y)))
    /** The radial distance the map holds along unit direction d. */
    const stored = (d: TSLNode) => {
      const a = abs(d)
      const axis = max(max(a.x, a.y), a.z)
      // A depth cube's level is an integer in WGSL, but three r186 writes a filterable texture's level as a float
      // (WGSLNodeBuilder.generateTextureLevel): a float-typed expression that reads 0u gets past it.
      return perspectiveDepthToViewZ(cubeTexture(depthTexture, d).level(expression('0u', 'float')).x, near, far).negate().div(axis)
    }
    /** The receiver's plane's distance along d, held near the receiver's own where the plane runs along d. */
    const receiverAt = (d: TSLNode) => clamp(planeDistance.div(min(dot(n, d), -1e-4)), dR.mul(0.5), dR.mul(2))
    const eps = dR.mul(texel).mul(num('cubeEps', 1.5)).add(0.005).toVar()
    const phi = turn().toVar()
    const searchU = min(radius.mul(dR.sub(near)).div(dR.mul(near)), texel.mul(maxTexels)).toVar()
    const blockers = float(0).toVar()
    const distanceSum = float(0).toVar()
    for (let i = -1; i < searchN; i++) {
      const d = i < 0 ? bd3D : along(vogelDiskSample(i, searchN, phi).mul(searchU))
      const at = stored(d).toVar()
      const blocks = select(at.lessThan(receiverAt(d).sub(eps)), float(1), float(0))
      blockers.addAssign(blocks)
      distanceSum.addAssign(blocks.mul(at))
    }
    const dB = distanceSum.div(max(blockers, 1)).toVar()
    const penumbra = clamp(radius.mul(dR.sub(dB)).div(dB.mul(dR)), texel.mul(0.5), texel.mul(maxTexels)).toVar()
    const lit = float(0).toVar()
    for (let i = 0; i < filterN; i++) {
      const d = along(vogelDiskSample(i, filterN, phi).mul(penumbra))
      lit.addAssign(select(stored(d).lessThan(receiverAt(d).sub(eps)), float(0), float(1)))
    }
    return select(blockers.lessThan(0.5), float(1), lit.div(filterN))
  })
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
    if ((light as PointLight).isPointLight) {
      ;(light.shadow as Filtered).filterNode = pointPCSS(light as PointLight, size)
      return `PCSS (cube, raw compares) r ${size} m, ${num('search', 16)} + ${num('samples', 32)} samples, at most ${num('maxTexels', 48)} texels`
    }
    if (!(light as SpotLight).isSpotLight) return 'product PCF (pcss: spot and point lights so far)'
    ;(light.shadow as Filtered).filterNode = spotPCSS(light as SpotLight, size)
    return `PCSS r ${size} m, ${num('search', 16)} + ${num('samples', 32)} samples, at most ${num('maxTexels', 48)} texels`
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
    done.push(`${light.type} ${light.name || light.uuid.slice(0, 8)}: ${methods[method](light as Shadowing, size)}`)
  })
  return done
}
