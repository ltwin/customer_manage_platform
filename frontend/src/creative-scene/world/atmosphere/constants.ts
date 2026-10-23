// The physical sky's atmosphere (Epic design「物理大气与 LUT」): Hillaire 2020
// (EGSR, "A Scalable and Production Ready Sky and Atmosphere Rendering
// Technique") with the Earth of its sample (sebh/UnrealEngineSkyAtmosphere,
// SetupEarthAtmosphere), lengths in km. The GPU's LUTs (luts.ts) and the
// CPU's tables (cpu.ts) both read these; e2e「W2 atmosphere drift」 catches
// one side changing without the other.

export type Vec3 = [number, number, number]

/** The planet's radius at sea level and the atmosphere's top, km. */
export const BOTTOM = 6360
export const TOP = 6460

/** Rayleigh scattering at sea level, per km, and its scale height. */
export const RAYLEIGH: Vec3 = [5.802e-3, 13.558e-3, 33.1e-3]
export const RAYLEIGH_H = 8

/**
 * Mie at sea level, per km, at Blender Nishita's scale: 5 × the sample's
 * (3.996e-3 scattering, 4.4e-3 extinction), whose haze no dust_density
 * shows (W2 spike, atmosphere-luts.md section 5). Single-scattering albedo
 * 0.908, as the sample's.
 */
export const MIE_SCATTERING = 5 * 3.996e-3
export const MIE_ABSORPTION = 5 * (4.4e-3 - 3.996e-3)
export const MIE_H = 1.2
export const MIE_G = 0.8
/**
 * Mie absorption per channel: mineral dust absorbs blue the most, so a dusty
 * sky and its light turn yellow. Not normalised: the mean is 1.6 × the base,
 * single-scattering albedo about 0.94 / 0.88 / 0.75 (R / G / B), as the
 * spike's preset pictures were drawn (design「大气 LUT 与重算」).
 */
export const MIE_ABSORPTION_TINT: Vec3 = [0.6, 1.2, 3.0]

/** Ozone absorption, per km at its peak, a tent 15 km either side of 25 km. */
export const OZONE: Vec3 = [0.65e-3, 1.881e-3, 0.085e-3]
export const OZONE_PEAK = 25
export const OZONE_HALF_WIDTH = 15

/** The planet's ground: its albedo in the multiple scattering and the sky view. */
export const GROUND_ALBEDO = 0.3

/** The sun's illuminance above the atmosphere, lux (the solar constant's usual value). */
export const SUN_LUX = 128000

/** How far above the ground the camera is: the horizon is 11 km off, hazed, never a hard edge (design「大气 LUT 与重算」). */
export const CAMERA_ABOVE_GROUND = 0.01

/** The LUTs' sizes and steps. The sky view is twice Hillaire's 192 × 108, so the glow about the sun does not band in an 8K export. */
export const TRANSMITTANCE = { w: 256, h: 64, steps: 40 } as const
export const MULTI = { w: 32, h: 32, dirs: 8, steps: 20 } as const
export const SKYVIEW = { w: 384, h: 216, steps: 30 } as const
/**
 * The aerial perspective's froxels (W4, Epic design「LUT 规格」): 32 slices of
 * 32 × 32 view directions side by side in a 1024 × 32 atlas, the slices
 * quadratically from the camera to maxKm (finer near), steps per froxel.
 */
export const AERIAL = { w: 32, h: 32, slices: 32, steps: 16, maxKm: 32 } as const

/** What the atmosphere's fields are (data: 物理天空). */
export type AtmosphereParams = { altitude_m: number; air_density: number; dust_density: number; ozone_density: number }

/** The presets: a preset writes these four fields (W2 design「大气预设」). */
export const PRESETS = {
  clear: { altitude_m: 0, air_density: 1, dust_density: 1, ozone_density: 1 },
  haze: { altitude_m: 0, air_density: 1, dust_density: 12, ozone_density: 1 },
  highland: { altitude_m: 4000, air_density: 1, dust_density: 0.2, ozone_density: 1 },
  dust: { altitude_m: 0, air_density: 1, dust_density: 40, ozone_density: 1 },
} as const satisfies Record<string, AtmosphereParams>

export type PresetName = keyof typeof PRESETS

/** The preset whose fields params has, if any. */
export function presetOf(params: AtmosphereParams): PresetName | null {
  for (const [name, p] of Object.entries(PRESETS) as [PresetName, AtmosphereParams][]) {
    if (p.altitude_m === params.altitude_m && p.air_density === params.air_density && p.dust_density === params.dust_density && p.ozone_density === params.ozone_density) return name
  }
  return null
}

export type Coefficients = { rayleigh: Vec3; mieScattering: number; mieAbsorption: Vec3; ozone: Vec3 }

/** The coefficients at sea level for an atmosphere's densities. */
export function coefficients(p: AtmosphereParams): Coefficients {
  return {
    rayleigh: RAYLEIGH.map((x) => x * p.air_density) as Vec3,
    mieScattering: MIE_SCATTERING * p.dust_density,
    mieAbsorption: MIE_ABSORPTION_TINT.map((t) => t * MIE_ABSORPTION * p.dust_density) as Vec3,
    ozone: OZONE.map((x) => x * p.ozone_density) as Vec3,
  }
}

/** The planet's ground under the scene: the terrain about it is as high as the scene's Y = 0, so the horizon does not sink on a plateau. Density still counts from sea level. */
export const groundRadius = (p: Pick<AtmosphereParams, 'altitude_m'>) => BOTTOM + Math.max(0, p.altitude_m) / 1000

/** The camera's distance from the planet's centre, km. */
export const viewRadius = (p: Pick<AtmosphereParams, 'altitude_m'>) => groundRadius(p) + CAMERA_ABOVE_GROUND

/** Linear Rec. 709 luminance. */
export const luminance = (c: Readonly<Vec3>) => 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]

/** The colour of a black body at kelvin, linear sRGB, from Kim et al.'s fit of the Planckian locus (1667–25000 K), clamped at 0. */
export function blackBody(kelvin: number): Vec3 {
  const t = Math.min(25000, Math.max(1667, kelvin))
  const x = t <= 4000 ? -0.2661239e9 / t ** 3 - 0.2343589e6 / t ** 2 + 0.8776956e3 / t + 0.17991 : -3.0258469e9 / t ** 3 + 2.1070379e6 / t ** 2 + 0.2226347e3 / t + 0.24039
  const y =
    t <= 2222
      ? -1.1063814 * x ** 3 - 1.3481102 * x ** 2 + 2.18555832 * x - 0.20219683
      : t <= 4000
        ? -0.9549476 * x ** 3 - 1.37418593 * x ** 2 + 2.09137015 * x - 0.16748867
        : 3.081758 * x ** 3 - 5.8733867 * x ** 2 + 3.75112997 * x - 0.37001483
  const X = x / y
  const Z = (1 - x - y) / y
  // 2000 K lies on sRGB's edge: blue comes out at or a hair below 0.
  return [3.2406 * X - 1.5372 - 0.4986 * Z, -0.9689 * X + 1.8758 + 0.0415 * Z, 0.0557 * X - 0.204 + 1.057 * Z].map((v) => Math.max(0, v)) as Vec3
}

const neutral = blackBody(6500)

/** The sun's colour at a colour temperature: 6500 K is (1, 1, 1); warmer below, cooler above; luminance 1, so it changes the colour, not the brightness. */
export function sunTint(kelvin: number): Vec3 {
  const c = blackBody(kelvin).map((v, i) => v / neutral[i]) as Vec3
  const l = luminance(c)
  return c.map((v) => v / l) as Vec3
}

/** The sun's illuminance above the atmosphere per channel, lux: SUN_LUX × its strength × its colour temperature's tint (W2 design「太阳的亮度与色温」). */
export function sunAbove(strength: number, kelvin: number): Vec3 {
  return sunTint(kelvin).map((t) => SUN_LUX * strength * t) as Vec3
}
