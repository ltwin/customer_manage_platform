import type { SceneEnvironment } from '../api.ts'

// The physical sky's fields (data: 物理天空) without the astronomy: the
// editor shell and the client's copy of the operations use them, and must
// not load astronomy-engine, which only the renderer does.

type Sky = SceneEnvironment['sky']

/** A physical sky's night sky with every field (W3, data: 夜空). */
export type NightSky = Required<NonNullable<Sky['night']>>

/** A physical sky's aerial perspective with every field (W4, data: 空气透视). */
export type AerialPerspective = Required<NonNullable<Sky['aerial_perspective']>>

/** A physical sky with every field, as the server stores it. */
export type PhysicalSky = Required<Omit<Sky, 'turbidity' | 'hdri' | 'color' | 'night' | 'aerial_perspective'>> & {
  kind: 'physical'
  night: NightSky
  aerial_perspective: AerialPerspective
}

/** What the server stores for the night sky's fields left out: everything drawn, at the strength and colour worked out. */
export const nightSkyDefaults = {
  moon: true,
  stars: true,
  milky_way: true,
  moon_strength: 1,
  moon_temperature_k: 6500,
  stars_strength: 1,
  stars_temperature_k: 6500,
  night_look: 1,
} as const satisfies NightSky

/** What the server stores for the aerial perspective's fields left out: on, at the true distance. */
export const aerialDefaults = { enabled: true, distance_scale: 1 } as const satisfies AerialPerspective

/** What the server stores for the fields a physical sky leaves out (backend sky.go): Shanghai at noon under a clear sky. The date has none. */
export const physicalSkyDefaults = {
  sun_mode: 'location',
  latitude_deg: 31.2304,
  longitude_deg: 121.4737,
  timezone: 'Asia/Shanghai',
  place: '',
  local_time: '12:00',
  north_azimuth_deg: 180,
  sun_elevation_deg: 35,
  sun_azimuth_deg: 135,
  altitude_m: 0,
  air_density: 1,
  dust_density: 1,
  ozone_density: 1,
  sun_disc: true,
  sun_size_deg: 0.545,
  sun_strength: 1,
  sun_temperature_k: 6500,
  night: nightSkyDefaults,
  aerial_perspective: aerialDefaults,
} as const satisfies Omit<PhysicalSky, 'kind' | 'date'>

/** As the server's completeSky. Other skies, and a physical sky with part of a place (the server refuses it), stay as they are. */
export function completeSky(sky: Sky): Sky {
  if (sky.kind !== 'physical') return sky
  const place = [sky.latitude_deg, sky.longitude_deg, sky.timezone].filter((v) => v !== undefined).length
  if (place !== 0 && place !== 3) return sky
  return { ...physicalSkyDefaults, ...sky, night: { ...nightSkyDefaults, ...sky.night }, aerial_perspective: { ...aerialDefaults, ...sky.aerial_perspective } }
}

/** Whether sky is a physical sky with every field, the only kind the renderer draws as one. */
export function isPhysical(sky: Sky): sky is PhysicalSky {
  return (
    sky.kind === 'physical' &&
    sky.date !== undefined &&
    Object.keys(physicalSkyDefaults).every((k) => sky[k as keyof Sky] !== undefined) &&
    Object.keys(nightSkyDefaults).every((k) => sky.night?.[k as keyof NightSky] !== undefined) &&
    Object.keys(aerialDefaults).every((k) => sky.aerial_perspective?.[k as keyof AerialPerspective] !== undefined)
  )
}
