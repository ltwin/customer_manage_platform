import { Color } from 'three'

// The lit look's constants (design「最终渲染的光照」). They set the balance
// within one scene: sun, sky light and ambient fill. How bright a scene is
// overall is its own exposure compensation (render.exposure_ev). Assumed
// values until calibrated on the reference scenes (design 未决事项 1).

/** The sun's intensity high in the sky (three.js units). */
export const SUN_MAX = 3
/** scene.environmentIntensity for the sky's light. */
export const ENVIRONMENT_INTENSITY = 1
/** The sky map lights already; the scene's ambient only fills shadows a little. */
export const AMBIENT_FILL = 0.5

const low = new Color('#ffb46b')
const high = new Color('#fff4e6')

function smoothstep(a: number, b: number, x: number): number {
  const t = Math.min(1, Math.max(0, (x - a) / (b - a)))
  return t * t * (3 - 2 * t)
}

/** The sun at an elevation: fading out below the horizon, warmer when low. */
export function sunLight(elevationDeg: number): { intensity: number; color: Color } {
  return {
    intensity: SUN_MAX * smoothstep(-2, 20, elevationDeg),
    color: low.clone().lerp(high, smoothstep(0, 35, elevationDeg)),
  }
}

/** toneMappingExposure for the lit look. */
export const exposureOf = (render: { exposure_ev?: number } | undefined) => 2 ** (render?.exposure_ev ?? 0)

/**
 * What a light added from the menu starts at: a desk lamp's point light, a
 * stage spot, a fill directional light (candela for point and spot).
 * Calibrated with the constants above.
 */
export const NEW_LIGHT_INTENSITY = { directional: 1, point: 40, spot: 300 } as const
