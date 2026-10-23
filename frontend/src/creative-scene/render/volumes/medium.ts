import { Color } from 'three'
import type { components } from '../../../api/schema'

// A volume object's medium (W5, data: 体积物件; scene-volumes design「领域模型
// 与业务规则」): a unit box or sphere in its object's local space, scattering
// and absorbing as Blender's Volume Scatter and Volume Absorption do. Free
// of raster state, so a path tracer can read the same medium later.

export type Volume = Required<components['schemas']['CreativeSceneVolume']>
export type Rgb = [number, number, number]

/** Per metre, linear: σs, σa and σt = σs + σa. */
export type Coefficients = { scatter: Rgb; absorb: Rgb; extinction: Rgb }

const linear = (hex: string): Rgb => {
  const c = new Color(hex)
  return [c.r, c.g, c.b]
}

/** σs = scatter_density × scatter_color, σa = absorption_density × (1 − absorption_color), on linear colours. */
export function coefficients(v: Volume): Coefficients {
  const s = linear(v.scatter_color)
  const a = linear(v.absorption_color)
  const scatter = s.map((c) => c * v.scatter_density) as Rgb
  const absorb = a.map((c) => (1 - c) * v.absorption_density) as Rgb
  return { scatter, absorb, extinction: [scatter[0] + absorb[0], scatter[1] + absorb[1], scatter[2] + absorb[2]] }
}

/**
 * The analytic chord's density factor: along a chord from the edge to the
 * centre the falloff ramps over edge_falloff of it, so the mean is
 * 1 − edge_falloff / 2. Self-shadowing and cast shadows use it; a wide
 * falloff no longer reads too dark.
 */
export const chordFactor = (v: Volume) => 1 - v.edge_falloff / 2
