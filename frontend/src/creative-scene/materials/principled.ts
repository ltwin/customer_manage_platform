import { Color, type MeshPhysicalMaterialParameters } from 'three'
import type { ParamValue } from '../builtins/catalog.ts'
import { defaultAlphaCutoff, type SlotMaterial } from './resolve.ts'

// The principled BSDF's parameters as MeshPhysicalNodeMaterial properties: the
// names are three.js's in snake case, so most go across as they are. Two
// convert: colours, and the film thickness, which three takes as a range.
// Features left at 0 (clearcoat, sheen, transmission, iridescence,
// anisotropy) cost nothing: three.js compiles their branches out.

const num = (p: Record<string, ParamValue>, k: string) => p[k] as number
const col = (p: Record<string, ParamValue>, k: string) => new Color(p[k] as string)

export function principledProps(p: Record<string, ParamValue>, policy: RenderPolicy): MeshPhysicalMaterialParameters {
  return {
    color: col(p, 'color'),
    metalness: num(p, 'metalness'),
    roughness: num(p, 'roughness'),
    ior: num(p, 'ior'),
    opacity: num(p, 'opacity'),
    transparent: policy.blended,
    depthWrite: policy.depthWrite,
    specularIntensity: num(p, 'specular_intensity'),
    specularColor: col(p, 'specular_color'),
    transmission: num(p, 'transmission'),
    thickness: num(p, 'thickness'),
    clearcoat: num(p, 'clearcoat'),
    clearcoatRoughness: num(p, 'clearcoat_roughness'),
    sheen: num(p, 'sheen'),
    sheenRoughness: num(p, 'sheen_roughness'),
    sheenColor: col(p, 'sheen_color'),
    emissive: col(p, 'emissive'),
    emissiveIntensity: num(p, 'emissive_intensity'),
    anisotropy: num(p, 'anisotropy'),
    // Radians, as three.js and the spot light's angle; the panel shows degrees.
    anisotropyRotation: num(p, 'anisotropy_rotation'),
    iridescence: num(p, 'iridescence'),
    iridescenceThicknessRange: [num(p, 'iridescence_thickness'), num(p, 'iridescence_thickness')],
  }
}

/**
 * How a slot's meshes are drawn (design「材质渲染策略」): every part of the
 * renderer that treats see-through meshes apart reads it here.
 */
export type RenderPolicy = {
  /**
   * With the transparent meshes: three's render lists take material.transparent
   * or transmission there. Drawn piece by piece (render/pieces.ts), cast no
   * shadow, and kept out of the AO's and realtime GI's prepasses and the
   * world-space layer (render/trace/collect.ts).
   */
  seeThrough: boolean
  /** material.transparent: blended over what is behind. */
  blended: boolean
  depthWrite: boolean
  /** Fragments whose opacity × texture alpha falls below it are cut away; null when none are. */
  alphaCutoff: number | null
}

const solid: RenderPolicy = { seeThrough: false, blended: false, depthWrite: true, alphaCutoff: null }

export function renderPolicy(m: SlotMaterial): RenderPolicy {
  if (m.kind !== 'principled') return solid
  const opacity = num(m.params, 'opacity')
  const glass = num(m.params, 'transmission') > 0
  switch (m.texturing?.alpha_mode) {
    case 'clip':
      // What is left is opaque; glass with holes stays glass.
      return { seeThrough: glass, blended: false, depthWrite: true, alphaCutoff: m.texturing.alpha_cutoff ?? defaultAlphaCutoff }
    case 'blend':
      return { seeThrough: true, blended: true, depthWrite: false, alphaCutoff: null }
    default:
      // As before alpha modes: glass at full opacity writes depth.
      return { seeThrough: opacity < 1 || glass, blended: opacity < 1, depthWrite: opacity >= 1, alphaCutoff: null }
  }
}
