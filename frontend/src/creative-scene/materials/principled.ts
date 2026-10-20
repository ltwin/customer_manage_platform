import { Color, type MeshPhysicalMaterialParameters } from 'three'
import type { ParamValue } from '../builtins/catalog.ts'

// The principled BSDF's parameters as MeshPhysicalMaterial properties: the
// names are three.js's in snake case, so most go across as they are. Two
// convert: colours, and the film thickness, which three takes as a range.
// Features left at 0 (clearcoat, sheen, transmission, iridescence,
// anisotropy) cost nothing: three.js compiles their branches out.

const num = (p: Record<string, ParamValue>, k: string) => p[k] as number
const col = (p: Record<string, ParamValue>, k: string) => new Color(p[k] as string)

export function principledProps(p: Record<string, ParamValue>): MeshPhysicalMaterialParameters {
  const opacity = num(p, 'opacity')
  return {
    color: col(p, 'color'),
    metalness: num(p, 'metalness'),
    roughness: num(p, 'roughness'),
    ior: num(p, 'ior'),
    opacity,
    transparent: opacity < 1,
    depthWrite: opacity >= 1,
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

/** See-through: drawn after the opaque meshes and kept out of AO. */
export function seeThrough(p: Record<string, ParamValue>): boolean {
  return num(p, 'opacity') < 1 || num(p, 'transmission') > 0
}
