// The candidate soft shadows (S0 group 2), put on a scene's lights before their first build. Each candidate is a
// method name; pcf leaves the product's shadows as they are.
//
//   pcf          the product's: three's PCF, 5 Vogel samples of radius shadow.radius texels, IGN by screen position
//   pcf-radius   the control: the same with a wider radius, alike everywhere (?radius= texels)
//   pcss         Fernando 2005: a blocker search, the penumbra by similar triangles, a PCF that wide; spot lights
//                (2D maps) for now. Kernels turn with the frame (IGN offset by frameId: TRAA averages them), and a
//                receiver plane depth bias from the screen derivatives keeps a wide kernel off the receiver itself
//                (Epic design「技术选型」阴影偏置). ?search= ?samples= samples, ?maxTexels= the widest penumbra.
import type { DirectionalLight, Light, LightShadow, Object3D, PointLight, SpotLight } from 'three'
import * as tsl from 'three/tsl'
import type { TSLNode } from '../../src/creative-scene/materials/patterns.ts'

export type Method = 'pcf' | 'pcf-radius' | 'pcss'

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { Fn, abs, clamp, dFdx, dFdy, dot, float, interleavedGradientNoise, ivec2, max, min, perspectiveDepthToViewZ, reference, select, texture, textureLoad, uniform, vec2, vogelDiskSample } = T
const { frameId, renderGroup, screenCoordinate } = tsl as unknown as Record<string, TSLNode>

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
  // Flat in screen space (det ~ 0), or a silhouette's huge gradient: no plane, and a bound on how far it may lean.
  return select(abs(det).greaterThan(1e-12), clamp(g.div(det), vec2(-0.05), vec2(0.05)), vec2(0))
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
    for (let i = 0; i < searchN; i++) {
      const o = vogelDiskSample(i, searchN, phi).mul(searchUV)
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
    return select(blockers.lessThan(0.5), centre.mul(0).add(1), lit.div(filterN))
  })
}

/** What each method does to a light that casts: returns a line for the report. */
export const methods: Record<Method, (light: Shadowing, size: number) => string> = {
  pcf: () => 'product PCF',
  // The control: the product's PCF with a wider radius, the same everywhere (Epic design「不采用」).
  'pcf-radius': (light) => {
    const radius = num('radius', 4)
    ;(light.shadow as LightShadow).radius = radius
    return `PCF radius ${radius} texels`
  },
  pcss: (light, size) => {
    if (!(light as SpotLight).isSpotLight) return 'product PCF (pcss: spot lights only so far)'
    ;(light.shadow as Filtered).filterNode = spotPCSS(light as SpotLight, size)
    return `PCSS r ${size} m, ${num('search', 16)} + ${num('samples', 32)} samples, at most ${num('maxTexels', 48)} texels`
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
