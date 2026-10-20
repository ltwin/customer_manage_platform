import { PMREMGenerator, Scene, Vector3, type Texture, type WebGLRenderer, type WebGLRenderTarget } from 'three'
import { ColorEnvironment } from 'three/addons/environments/ColorEnvironment.js'
import { RoomEnvironment } from 'three/addons/environments/RoomEnvironment.js'
import { Sky } from 'three/addons/objects/Sky.js'
import type { SceneEnvironment } from '../api.ts'

// The lit look's sky (design「最终渲染的光照」): three's own Sky, which can
// leave the sun disc out. The background shows the disc; the environment map
// that lights and reflects does not, since the sun's directional light counts
// it already. Both draw with the same shader, so their colours match. The
// clay look keeps drei's Sky, unchanged since C4.

const viewDirection = 'vec3 direction = normalize( vWorldPosition - cameraPosition );'

/**
 * A Sky for the Z-up view space. three's shader takes Y as up; the view
 * direction turns to document space (view (x, y, z) → document (x, z, −y))
 * and the sun is given in document space. No clouds: they drift with time,
 * and an export must not depend on when it is taken.
 */
export function viewSky(): Sky {
  const sky = new Sky()
  const m = sky.material
  if (!m.fragmentShader.includes(viewDirection)) throw new Error('three Sky shader changed: no view direction line')
  m.fragmentShader = m.fragmentShader.replace(viewDirection, `${viewDirection}\n\t\t\tdirection = vec3( direction.x, direction.z, -direction.y );`)
  m.uniforms.cloudCoverage.value = 0
  m.uniforms.rayleigh.value = 0.5
  return sky
}

/** The sun's direction in document space (Y up) from the sky's angles. */
export function sunDirection(env: SceneEnvironment): Vector3 {
  const sky = env.sky
  const el = ((sky.sun_elevation_deg ?? 35) * Math.PI) / 180
  const az = ((sky.sun_azimuth_deg ?? 135) * Math.PI) / 180
  return new Vector3(Math.sin(az) * Math.cos(el), Math.sin(el), Math.cos(az) * Math.cos(el))
}

export function setSky(sky: Sky, env: SceneEnvironment, sunDisc: boolean) {
  const u = sky.material.uniforms
  u.sunPosition.value.copy(sunDirection(env))
  u.turbidity.value = env.sky.turbidity ?? 3
  u.showSunDisc.value = sunDisc ? 1 : 0
}

/** What lights a lit scene besides its lights: the sky without the sun disc, or the sky colour. */
export function skyEnvironment(pmrem: PMREMGenerator, env: SceneEnvironment): WebGLRenderTarget {
  if (env.sky.kind === 'procedural') {
    const scene = new Scene()
    const sky = viewSky()
    setSky(sky, env, false)
    sky.scale.setScalar(10)
    scene.add(sky)
    const target = pmrem.fromScene(scene, 0, 0.1, 100)
    sky.geometry.dispose()
    sky.material.dispose()
    return target
  }
  const colour = new ColorEnvironment(env.sky.kind === 'color' ? (env.sky.color ?? '#8a8f98') : '#8a8f98')
  const target = pmrem.fromScene(colour, 0)
  colour.dispose()
  return target
}

/** Material preview's neutral studio light: the same for every scene. */
export function roomEnvironment(gl: WebGLRenderer): { texture: Texture; dispose(): void } {
  const pmrem = new PMREMGenerator(gl)
  const room = new RoomEnvironment()
  const target = pmrem.fromScene(room, 0.04)
  room.dispose()
  pmrem.dispose()
  return { texture: target.texture, dispose: () => target.dispose() }
}
