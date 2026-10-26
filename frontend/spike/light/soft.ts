// The candidate soft shadows (S0 group 2), put on a scene's lights before their first build. Each candidate is a
// method name; pcf leaves the product's shadows as they are.
import type { DirectionalLight, Light, LightShadow, Object3D, PointLight, SpotLight } from 'three'

export type Method = 'pcf' | 'pcf-radius'

type Shadowing = DirectionalLight | PointLight | SpotLight

/** What each method does to a light that casts: returns a line for the report. */
export const methods: Record<Method, (light: Shadowing, size: number) => string> = {
  pcf: () => 'product PCF',
  // The control: the product's PCF with a wider radius, the same everywhere (Epic design「不采用」).
  'pcf-radius': (light, size) => {
    const radius = Math.max(1, Number(new URLSearchParams(location.search).get('radius') ?? 4 * Math.max(size, 0.25) * 4))
    ;(light.shadow as LightShadow).radius = radius
    return `PCF radius ${radius} texels`
  },
}

/** Puts method on every light in root that casts; returns what was done, light by light. */
export function softShadows(root: Object3D, method: Method, size: number): string[] {
  const done: string[] = []
  root.traverse((o) => {
    const light = o as Light
    if (!light.isLight || !light.castShadow) return
    done.push(`${light.type} ${light.name || light.uuid.slice(0, 8)}: ${methods[method](light as Shadowing, size)}`)
  })
  return done
}
