import * as tsl from 'three/tsl'
import { PhysicalLightingModel, type NodeBuilder } from 'three/webgpu'
import type { TSLNode } from '../../materials/patterns.ts'
import { horizonDirection, hybridLobe, type Sized } from './formulas.ts'

// The direct light of a sized light (Epic design「直接光的入口」). three's
// light nodes hand the lighting model one lightDirection, which the diffuse,
// the GGX, the clearcoat and the sheen all share; a sized light needs one for
// each layer. Its node hands the model sized (render/lightModel/nodes.ts);
// PhysicalLightingModel.direct, wrapped once like giSplit.ts wraps indirect,
// draws a light without it as before. With it, three parts:
//  - the base layer's specular: three's direct once more, toward the hybrid
//    lobe's direction at its roughness (an anisotropic one's alphaT widened
//    alike) and energy, the clearcoat off, its diffuse and its sheen let go
//    (the sheen still takes its energy from the specular, as in three's);
//  - the clearcoat: its own hybrid lobe (its roughness, its normal) added to
//    clearcoatSpecularDirect by BRDF_GGX as three's direct does (r186). The
//    clearcoat lit by the base's lobe stayed a point light's dot where the
//    base is rough (scene-light-radius review);
//  - the diffuse, and the sheen with it: three's direct toward Snyder's bent
//    direction (formulas.ts horizonDirection), the clearcoat off, its
//    specular let go.
//
// three has no public way to do this: direct() and the layers' flags are
// touched here only.

type Op = (...args: TSLNode[]) => TSLNode
const { BRDF_GGX, float, vec3 } = tsl as unknown as Record<string, Op>
const { alphaT, anisotropy, clearcoatNormalView, clearcoatRoughness, normalView, positionViewDirection, roughness } = tsl as unknown as Record<string, TSLNode>

type Reflected = { directDiffuse: TSLNode; directSpecular: TSLNode }
type DirectInput = { lightDirection: TSLNode; lightColor: TSLNode; reflectedLight: Reflected; sized?: Sized }
type Model = { clearcoat: boolean; sheen: boolean; anisotropy: boolean; clearcoatSpecularDirect: TSLNode; sheenSpecularDirect: TSLNode }
type Direct = (this: Model, input: DirectInput, builder: NodeBuilder) => void

/** three's clearcoat Fresnel (PhysicalLightingModel.js, r186): a dielectric's 0.04, to 1 at grazing. */
const clearcoatF0 = vec3(0.04)
const clearcoatF90 = float(1)

/** A layer's light gathered aside: what it adds is taken from it alone. */
const aside = (): Reflected => ({ directDiffuse: vec3(0).toVar(), directSpecular: vec3(0).toVar() })

/** Calls direct with the clearcoat and sheen flags as given, put back after. */
function withLayers(model: Model, layers: { clearcoat: boolean; sheen: boolean }, run: () => void) {
  const { clearcoat, sheen } = model
  model.clearcoat = layers.clearcoat
  model.sheen = layers.sheen
  try {
    run()
  } finally {
    model.clearcoat = clearcoat
    model.sheen = sheen
  }
}

/** Wraps PhysicalLightingModel.direct once; a light without sized draws as before. */
export function installDirect(): void {
  const proto = PhysicalLightingModel.prototype as unknown as { direct: Direct; sizedLights?: true }
  if (proto.sizedLights) return
  const direct = proto.direct
  proto.direct = function (this: Model, input: DirectInput, builder: NodeBuilder) {
    const { sized } = input
    if (!sized) return direct.call(this, input, builder)
    const clearcoat = this.clearcoat
    // The base layer's specular.
    const base = hybridLobe(sized, roughness, normalView, positionViewDirection)
    // Declared before roughness is widened: they read it.
    const direction = base.direction.toVar()
    const energy = base.energy.toVar()
    const kept = roughness.toVar()
    const keptT = this.anisotropy ? alphaT.toVar() : null
    roughness.assign(base.roughness)
    // three's alphaT as the material sets it up (MeshPhysicalNodeMaterial, r186), from the widened roughness.
    if (keptT) alphaT.assign(anisotropy.pow2().mix(roughness.pow2(), 1))
    const specular = aside()
    // The sheen's own light goes aside: the diffuse's pass adds it.
    const sheenLight = this.sheenSpecularDirect
    this.sheenSpecularDirect = vec3(0).toVar()
    try {
      withLayers(this, { clearcoat: false, sheen: this.sheen }, () =>
        direct.call(this, { ...input, lightDirection: direction, lightColor: input.lightColor.mul(energy), reflectedLight: specular }, builder),
      )
    } finally {
      this.sheenSpecularDirect = sheenLight
    }
    roughness.assign(kept)
    if (keptT) alphaT.assign(keptT)
    input.reflectedLight.directSpecular.addAssign(specular.directSpecular)
    // The clearcoat's own lobe.
    if (clearcoat) {
      const coat = hybridLobe(sized, clearcoatRoughness, clearcoatNormalView, positionViewDirection)
      const irradiance = clearcoatNormalView.dot(coat.direction).clamp().mul(input.lightColor).mul(coat.energy)
      this.clearcoatSpecularDirect.addAssign(
        irradiance.mul(BRDF_GGX({ lightDirection: coat.direction, f0: clearcoatF0, f90: clearcoatF90, roughness: coat.roughness, normalView: clearcoatNormalView } as unknown as TSLNode)),
      )
    }
    // The diffuse and the sheen, toward the part of the light above the horizon.
    const diffuse = aside()
    withLayers(this, { clearcoat: false, sheen: this.sheen }, () =>
      direct.call(this, { ...input, lightDirection: horizonDirection(sized, normalView), reflectedLight: diffuse }, builder),
    )
    input.reflectedLight.directDiffuse.addAssign(diffuse.directDiffuse)
  }
  proto.sizedLights = true
}
