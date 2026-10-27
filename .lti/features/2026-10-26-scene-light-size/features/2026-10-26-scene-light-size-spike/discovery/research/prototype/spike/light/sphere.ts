// Sphere lights in the raster (S0 group 4, Epic design「高光」): point and spot lights of radius r, as the page
// installs them before anything builds. Their nodes hand the lighting model the way to the light's centre and r
// besides the usual direction and colour, and fall off with max(d, r) in place of d (the Epic's effective
// distance). PhysicalLightingModel.direct, wrapped, lights the specular layers (the base's and the clearcoat's) by
// the hybrid of a widened lobe and Karis 2013's representative point (hybrid()); the diffuse and the sheen toward
// the sphere as the surface's horizon cuts it (horizon(), Snyder's exact irradiance).
//   ?highlight=point   the product's: highlights from the centre whatever r (the falloff still takes max(d, r))
//   ?norm=hybrid       (default) widen the lobe for the sphere's size up to 1.5 α, move toward it beyond (hybrid())
//   ?norm=karis        Karis's point: α' = saturate(α + r / (normDiv · d)), normDiv 2 (or 3)
//   ?norm=quad         Karis's point, α'² = α² + (normK · r / d)² (normK 0.6); ?repK= the share of r it moves
//   ?norm=wide         no representative point: GGX at α'² = α² + (normK · r / d)², the light at its centre
import { PointLight, SpotLight, type Light } from 'three'
import * as tsl from 'three/tsl'
import { PhysicalLightingModel, PointLightNode, SpotLightNode, type WebGPURenderer } from 'three/webgpu'
import type { TSLNode } from '../../src/creative-scene/materials/patterns.ts'

type Op = (...args: TSLNode[]) => TSLNode
const T = tsl as unknown as Record<string, Op>
const { clamp, dot, float, length, max, normalize, reference, vec3 } = T
const { clearcoatRoughness, normalView, positionViewDirection, roughness, renderGroup } = tsl as unknown as Record<string, TSLNode>

const query = new URLSearchParams(location.search)

type Sphere = { lightVector: TSLNode; radius: TSLNode }
type DirectInput = { lightDirection: TSLNode; lightColor: TSLNode; reflectedLight: { directDiffuse: TSLNode; directSpecular: TSLNode }; sphere?: Sphere }
type LightNodeLike = {
  light: Light & { radius: number }
  decayExponentNode: TSLNode
  getLightVector(builder: unknown): TSLNode
}

/** The light's radius as each render starts: the spike keeps it on the light as `radius`. */
const radiusOf = (node: LightNodeLike) => reference('radius', 'float', node.light as unknown as TSLNode).setGroup(renderGroup)

/**
 * three's falloff at d made the one at max(d, r): 1 / max(d^decay, 0.01) becomes 1 / max(max(d, r)^decay, 0.01).
 * The cone and the cutoff window stay three's, at d (they differ only inside the sphere).
 */
const toEffective = (node: LightNodeLike, d: TSLNode, r: TSLNode) =>
  max(d.pow(node.decayExponentNode), 0.01).div(max(max(d, r).pow(node.decayExponentNode), 0.01))

class SizedPointLightNode extends PointLightNode {
  setupDirect(builder: unknown) {
    const self = this as unknown as LightNodeLike
    const base = (PointLightNode.prototype as unknown as { setupDirect(b: unknown): { lightColor: TSLNode } }).setupDirect.call(this, builder)
    const radius = radiusOf(self)
    const lightVector = self.getLightVector(builder).toVar()
    return { ...base, lightColor: base.lightColor.mul(toEffective(self, length(lightVector), radius)), sphere: { lightVector, radius } }
  }
}

class SizedSpotLightNode extends SpotLightNode {
  setupDirect(builder: unknown) {
    const self = this as unknown as LightNodeLike
    const base = (SpotLightNode.prototype as unknown as { setupDirect(b: unknown): { lightColor: TSLNode } }).setupDirect.call(this, builder)
    const radius = radiusOf(self)
    const lightVector = self.getLightVector(builder).toVar()
    return { ...base, lightColor: base.lightColor.mul(toEffective(self, length(lightVector), radius)), sphere: { lightVector, radius } }
  }
}

/**
 * The default (?norm=hybrid): the sphere's angular radius s = r / d split at m α (?hybridM=, 1.5): the part within
 * it widens the lobe in quadrature (α'² = α² + (k s_w)²), the rest moves the light toward the representative point
 * (a sphere of s_s d), its energy kept by (α' / α'')², α'' = α' + s_s / 2. A sphere small beside the lobe only
 * widens it; a large one shows as its mirror image, as Karis's point has it (sphere-highlight.md).
 */
function hybrid(this: { clearcoat: boolean; sheen: boolean }, input: DirectInput, builder: unknown) {
  const { lightVector, radius } = input.sphere!
  const k = Number(query.get('normK') ?? 0.6)
  const m = Number(query.get('hybridM') ?? 1.5)
  const d = length(lightVector)
  const s = radius.div(d)
  const widen = (r: TSLNode) => {
    const a = r.mul(r)
    const sw = T.min(s, a.mul(m))
    const ss = s.sub(sw)
    const a1 = a.mul(a).add(sw.mul(k).pow(2)).sqrt()
    // The moved part's energy as Karis keeps it, α'' = α' + s_s / 2 (?shiftNorm=quad: in quadrature, too bright).
    const a2 = (query.get('shiftNorm') ?? 'karis') === 'karis' ? a1.add(ss.div(2)) : a1.mul(a1).add(ss.mul(k).pow(2)).sqrt()
    return { rough: a1.sqrt(), shift: ss.mul(d), energy: a1.div(max(a2, 1e-4)).pow(2) }
  }
  const base = roughness.toVar()
  const coat = clearcoatRoughness.toVar()
  const w = widen(base)
  const R = positionViewDirection.negate().reflect(normalView).toVar()
  const centreToRay = dot(lightVector, R).mul(R).sub(lightVector).toVar()
  const toward = normalize(lightVector.add(centreToRay.mul(clamp(w.shift.div(max(length(centreToRay), 1e-6)), 0, 1))))
  roughness.assign(w.rough)
  clearcoatRoughness.assign(widen(coat).rough)
  const specular = { directDiffuse: vec3(0).toVar(), directSpecular: vec3(0).toVar() }
  // The sheen goes with the diffuse below: a lobe as wide as velvet's sees the sphere as the diffuse does.
  const { clearcoat, sheen } = this
  this.sheen = false
  try {
    directOf.call(this, { ...input, lightDirection: toward, lightColor: input.lightColor.mul(w.energy), reflectedLight: specular }, builder)
  } finally {
    this.sheen = sheen
  }
  roughness.assign(base)
  clearcoatRoughness.assign(coat)
  input.reflectedLight.directSpecular.addAssign(specular.directSpecular)
  // The diffuse (and the sheen) toward the sphere as the horizon cuts it; the clearcoat counted above.
  const diffuse = { directDiffuse: vec3(0).toVar(), directSpecular: vec3(0).toVar() }
  this.clearcoat = false
  try {
    directOf.call(this, { ...input, lightDirection: horizon(lightVector, radius), reflectedLight: diffuse }, builder)
  } finally {
    this.clearcoat = clearcoat
  }
  input.reflectedLight.directDiffuse.addAssign(diffuse.directDiffuse)
}

/**
 * The way the diffuse takes to a sphere light, which may sit partly below the surface's horizon (U6):
 *   ?horizon=snyder (default)  the sphere's irradiance, clipped by the horizon, exactly (Snyder 1996, as Lagarde and
 *                              de Rousiers 2014 write it): as a cosine F, the direction bent toward the normal so
 *                              N·L' = F (I/d² F is the irradiance)
 *   ?horizon=rep               the centre moved r toward the normal: the sphere's point the surface faces most
 *   ?horizon=none              the centre, as a point light
 */
function horizon(lightVector: TSLNode, radius: TSLNode): TSLNode {
  const mode = query.get('horizon') ?? 'snyder'
  const L = normalize(lightVector)
  if (mode === 'none') return L
  if (mode === 'rep') return normalize(lightVector.add(normalView.mul(radius)))
  const sin2 = T.min(radius.div(length(lightVector)).pow(2), 0.9999).toVar()
  const cosT = clamp(dot(normalView, L), -1, 1).toVar()
  const sinT = T.sqrt(float(1).sub(cosT.mul(cosT))).toVar()
  const x = T.sqrt(float(1).div(sin2).sub(1)).toVar()
  const y = clamp(x.negate().mul(cosT).div(max(sinT, 1e-4)), -1, 1).toVar()
  const sinTy = sinT.mul(T.sqrt(float(1).sub(y.mul(y)))).toVar()
  const clipped = cosT.mul(T.acos(y)).sub(x.mul(sinTy)).mul(sin2).add(T.atan(sinTy.div(x)))
  const unclipped = sin2.mul(Math.PI).mul(max(cosT, 0))
  const E = max(T.select(cosT.mul(cosT).greaterThan(sin2), unclipped, clipped), 0)
  const F = clamp(E.div(sin2.mul(Math.PI)), 0, 1).toVar()
  // L less its part along N, made a unit: the way to bend along (any tangent where L is the normal).
  const along = L.sub(normalView.mul(cosT))
  const tangent = T.select(length(along).greaterThan(1e-4), normalize(along), normalize(T.cross(normalView, vec3(0, 1, 0.1))))
  return normalize(normalView.mul(F).add(tangent.mul(T.sqrt(float(1).sub(F.mul(F))))))
}

/** three's own PhysicalLightingModel.direct, as it was before the wrap. */
let directOf: (this: unknown, input: DirectInput, builder: unknown) => void

let wrapped = false

/** The specular layers from the representative point, the diffuse from the centre (once for the page). */
function wrapDirect() {
  if (wrapped) return
  wrapped = true
  const proto = PhysicalLightingModel.prototype as unknown as { direct(this: Layers, input: DirectInput, builder: unknown): void }
  type Layers = { clearcoat: boolean; sheen: boolean }
  const direct = proto.direct
  directOf = direct as typeof directOf
  const normDiv = Number(query.get('normDiv') ?? 2)
  const normK = Number(query.get('normK') ?? 0.6)
  const repK = Number(query.get('repK') ?? 1)
  proto.direct = function (this: Layers, input: DirectInput, builder: unknown) {
    if (!input.sphere || query.get('highlight') === 'point') return direct.call(this, input, builder)
    const { lightVector, radius } = input.sphere
    if (query.get('norm') === 'wide') {
      // The lobe widened in quadrature, the light still at its centre: α'² = α² + (normK r / d)², the specular
      // layers' roughness swapped for the call (GGX keeps its own energy), the clearcoat's alike.
      const across = radius.div(length(lightVector)).mul(normK)
      const widen = (r: TSLNode) => r.mul(r).pow(2).add(across.pow(2)).sqrt().sqrt()
      const base = roughness.toVar()
      const coat = clearcoatRoughness.toVar()
      roughness.assign(widen(base))
      clearcoatRoughness.assign(widen(coat))
      direct.call(this, input, builder)
      roughness.assign(base)
      clearcoatRoughness.assign(coat)
      return
    }
    if ((query.get('norm') ?? 'hybrid') === 'hybrid') return hybrid.call(this, input, builder)
    const R = positionViewDirection.negate().reflect(normalView).toVar()
    const centreToRay = dot(lightVector, R).mul(R).sub(lightVector).toVar()
    // ?repK= the share of r the representative point may move off the centre (1: Karis).
    const closest = lightVector.add(centreToRay.mul(clamp(radius.mul(repK).div(max(length(centreToRay), 1e-6)), 0, 1)))
    const toward = normalize(closest)
    const a = roughness.mul(roughness)
    // The lobe widened by the sphere's size: Karis adds r / (normDiv d) to α; ?norm=quad adds (k r / d) in
    // quadrature, as two lobes' widths add when one blurs the other (?normK=, 0.6 fitted to the reference).
    const across = radius.div(length(lightVector))
    const widened = query.get('norm') === 'quad' ? a.mul(a).add(across.mul(normK).pow(2)).sqrt() : clamp(a.add(across.div(normDiv)), 0, 1)
    const energy = a.div(max(widened, 1e-4)).pow(2)
    // Specular layers: the direct term once more toward the representative point, its diffuse let go.
    const specular = { directDiffuse: vec3(0).toVar(), directSpecular: vec3(0).toVar() }
    direct.call(this, { ...input, lightDirection: toward, lightColor: input.lightColor.mul(energy), reflectedLight: specular }, builder)
    input.reflectedLight.directSpecular.addAssign(specular.directSpecular)
    // Diffuse from the centre, the clearcoat and sheen (accumulated on the model) already counted above.
    const diffuse = { directDiffuse: vec3(0).toVar(), directSpecular: vec3(0).toVar() }
    const { clearcoat, sheen } = this
    this.clearcoat = this.sheen = false
    try {
      direct.call(this, { ...input, reflectedLight: diffuse }, builder)
    } finally {
      this.clearcoat = clearcoat
      this.sheen = sheen
    }
    input.reflectedLight.directDiffuse.addAssign(diffuse.directDiffuse)
  }
}

/**
 * Point and spot lights in root become spheres of radius r (as the reference's are): their nodes registered for
 * the renderer before anything builds, the lighting model wrapped. Returns a line for the report.
 */
export function sphereLights(renderer: WebGPURenderer, root: { traverse(f: (o: unknown) => void): void }, r: number): string {
  // addLight refuses a light class three already maps (NodeLibrary.addClass: a warning, nothing set), so the
  // spike writes the map; S1's own light subclasses get their nodes through addLight (lookup is by exact class).
  const { lightNodes } = (renderer as unknown as { library: { lightNodes: WeakMap<object, unknown> } }).library
  lightNodes.set(PointLight, SizedPointLightNode)
  lightNodes.set(SpotLight, SizedSpotLightNode)
  wrapDirect()
  root.traverse((o) => {
    const l = o as PointLight & { radius?: number }
    if (l.isPointLight || (l as unknown as SpotLight).isSpotLight) l.radius = r
  })
  const norm = query.get('norm') ?? 'hybrid'
  return query.get('highlight') === 'point'
    ? `sphere lights r ${r} m: falloff from max(d, r), highlights from the centre`
    : `sphere lights r ${r} m: falloff from max(d, r), highlights ${norm === 'hybrid' ? 'widened, then moved toward the representative point (hybrid)' : `by ?norm=${norm}`}, diffuse by ${query.get('horizon') ?? 'snyder'}`
}
