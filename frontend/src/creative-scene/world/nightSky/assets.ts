import { SRGBColorSpace, TextureLoader, type Texture } from 'three'
import type { Scene } from '../../api.ts'
import { physicalMoon, physicalSun } from '../astro.ts'
import { isPhysical, type PhysicalSky } from '../physicalSky.ts'

// The night sky's static assets (W3 design「资源与确定性」): the bright star
// catalogue and the moon's albedo map, built by scripts/night-sky and loaded
// once, when a sky first needs them. An offscreen picture waits for them
// (host/render.ts openShooter); the viewport draws without them until they
// arrive. A load that fails is tried again the next time.

/** The stars of the Yale Bright Star Catalogue to V 6.5 (stars.bin), brightest first: J2000 right ascension and declination in degrees, V and B−V. */
export type StarCatalogue = { count: number; ra: Float32Array; dec: Float32Array; v: Float32Array; bv: Float32Array }

/** stars.bin as scripts/night-sky/build-stars.mjs writes it. */
export function parseStars(buffer: ArrayBuffer): StarCatalogue {
  const view = new DataView(buffer)
  if (new TextDecoder('latin1').decode(new Uint8Array(buffer, 0, 4)) !== 'BSC5') throw new Error('stars.bin: not a star catalogue')
  const count = view.getUint32(4, true)
  if (buffer.byteLength !== 8 + count * 10) throw new Error('stars.bin: truncated')
  const ra = new Float32Array(buffer.slice(8, 8 + count * 4))
  const dec = new Float32Array(buffer.slice(8 + count * 4, 8 + count * 8))
  const q = new Uint8Array(buffer, 8 + count * 8, count * 2)
  const v = new Float32Array(count)
  const bv = new Float32Array(count)
  for (let i = 0; i < count; i++) {
    v[i] = -1.5 + 0.05 * q[i]
    bv[i] = -0.5 + 0.02 * q[count + i]
  }
  return { count, ra, dec, v, bv }
}

/** How the assets are fetched; tests stand in their own. */
export const nightAssetLoaders = {
  stars: async (): Promise<StarCatalogue> => {
    const response = await fetch(new URL('./stars.bin', import.meta.url))
    if (!response.ok) throw new Error(`stars.bin: HTTP ${response.status}`)
    return parseStars(await response.arrayBuffer())
  },
  moon: async (): Promise<Texture> => {
    const texture = await new TextureLoader().loadAsync(new URL('./moon.webp', import.meta.url).href)
    texture.colorSpace = SRGBColorSpace
    return texture
  },
}

type Slot<T> = { value: T | null; loading: Promise<T> | null }
const stars: Slot<StarCatalogue> = { value: null, loading: null }
const moon: Slot<Texture> = { value: null, loading: null }

function load<T>(slot: Slot<T>, fetch: () => Promise<T>): Promise<T> {
  slot.loading ??= fetch().then(
    (v) => (slot.value = v),
    (error: unknown) => {
      slot.loading = null
      throw error
    },
  )
  return slot.loading
}

/** The star catalogue if it has arrived; otherwise starts loading it and returns null, calling arrived when it does. */
export function starsNow(arrived?: () => void): StarCatalogue | null {
  if (!stars.value) load(stars, nightAssetLoaders.stars).then(arrived, (e: unknown) => console.warn('night sky: the star catalogue did not load', e))
  return stars.value
}

/** The moon's albedo map if it has arrived; otherwise as starsNow. */
export function moonNow(arrived?: () => void): Texture | null {
  if (!moon.value) load(moon, nightAssetLoaders.moon).then(arrived, (e: unknown) => console.warn('night sky: the moon map did not load', e))
  return moon.value
}

/** What a physical sky draws that needs an asset: the stars once the sun is down, the moon's map while it is up. */
export function nightNeeds(sky: PhysicalSky): { stars: boolean; moon: boolean } {
  if (sky.sun_mode !== 'location') return { stars: false, moon: false }
  const night = sky.night
  const sunDown = physicalSun(sky).elevationDeg < 0
  const up = night.moon && physicalMoon(sky)!.elevationDeg > -1
  return { stars: night.stars && sunDown, moon: up }
}

/** Resolves once every asset the scene's sky needs is here; rejects when one fails to load. An offscreen picture must not come out without them. */
export async function loadNightAssets(scene: Pick<Scene, 'environment'>): Promise<void> {
  const sky = scene.environment.sky
  if (!isPhysical(sky)) return
  const needs = nightNeeds(sky)
  await Promise.all([needs.stars ? load(stars, nightAssetLoaders.stars) : null, needs.moon ? load(moon, nightAssetLoaders.moon) : null])
}

/** Forgets what was loaded: tests only. */
export function resetNightAssets() {
  for (const slot of [stars, moon] as Slot<unknown>[]) {
    slot.value = null
    slot.loading = null
  }
}
