// W2 spike: the Earth atmosphere of Hillaire 2020 (EGSR, "A Scalable and
// Production Ready Sky and Atmosphere Rendering Technique") as its sample
// sets it up (sebh/UnrealEngineSkyAtmosphere, SetupEarthAtmosphere), in km.
// air / dust / ozone multiply the Rayleigh, Mie and ozone coefficients, as
// Blender's Nishita sky names them.

export type Vec3 = [number, number, number]

export type Atmosphere = {
  /** Scene Y = 0's height above sea level, metres. */
  altitudeM: number
  air: number
  dust: number
  ozone: number
  groundAlbedo: number
  /** Spike only: the Mie coefficients' base against the sample's (Blender's Nishita has about 5×). */
  mieScale?: number
  /** Spike only: Mie absorption per channel (mineral dust absorbs blue). */
  mieAbsorptionTint?: Vec3
}

export const clear: Atmosphere = { altitudeM: 0, air: 1, dust: 1, ozone: 1, groundAlbedo: 0.3 }

export const BOTTOM = 6360
export const TOP = 6460
export const RAYLEIGH: Vec3 = [5.802e-3, 13.558e-3, 33.1e-3]
export const RAYLEIGH_H = 8
export const MIE_SCATTERING = 3.996e-3
export const MIE_EXTINCTION = 4.4e-3
export const MIE_H = 1.2
export const MIE_G = 0.8
export const OZONE: Vec3 = [0.65e-3, 1.881e-3, 0.085e-3]
/** The sun's illuminance above the atmosphere, lux. */
export const SUN_LUX = 128000
/** Offset that keeps a point on the ground from being inside the planet. */
export const PLANET_OFFSET = 0.01

export const TRANSMITTANCE = { w: 256, h: 64, steps: 40 }
export const MULTI = { w: 32, h: 32, dirs: 8, steps: 20 }
export const SKYVIEW = { w: 192, h: 108, steps: 30 }

export const coefficients = (a: Atmosphere) => ({
  rayleigh: RAYLEIGH.map((x) => x * a.air) as Vec3,
  mieScattering: MIE_SCATTERING * a.dust * (a.mieScale ?? 1),
  mieAbsorption: ((a.mieAbsorptionTint ?? [1, 1, 1]).map((t) => t * (MIE_EXTINCTION - MIE_SCATTERING) * a.dust * (a.mieScale ?? 1)) as Vec3),
  ozone: OZONE.map((x) => x * a.ozone) as Vec3,
})

/** The camera's distance from the planet's centre, km. */
export const viewRadius = (a: Atmosphere, yMetres = 0) => BOTTOM + Math.max(PLANET_OFFSET, (a.altitudeM + yMetres) / 1000)
