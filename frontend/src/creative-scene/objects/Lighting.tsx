import { useEffect, useLayoutEffect, useMemo, useRef } from 'react'
import { useThree } from '@react-three/fiber'
import { Color, DirectionalLight, Scene, type Mesh } from 'three'
import { uniform } from 'three/tsl'
import { PMREMGenerator, type RenderTarget, type WebGPURenderer } from 'three/webgpu'
import type { SceneEnvironment } from '../api.ts'
import type { Engine } from '../render/engines.ts'
import { ambientFill, sunLight } from '../render/litLook.ts'
import { gpuOf } from '../render/gpu.ts'
import { disposeSky, litSky, physicalSkyMesh, roomEnvironment, setSky, skyEnvironment, sunDirection } from '../render/sky.ts'
import { STUDIO_AMBIENT, STUDIO_BACKGROUND, STUDIO_FILL, STUDIO_KEY_INTENSITY } from '../render/studio.ts'
import { fitSunShadow } from '../render/sunShadow.ts'
import { positionToView } from '../space.ts'
import { ASIDE_LAYER } from '../render/sceneRenderer.ts'
import type { DisplayMode } from '../state/sceneStore.ts'
import { wireBackground } from './wireframe.ts'
import { physicalSun, sunVector } from '../world/astro.ts'
import { sunAbove } from '../world/atmosphere/constants.ts'
import { CpuAtmosphere } from '../world/atmosphere/cpu.ts'
import { atmosphereLuts } from '../world/atmosphere/luts.ts'
import { discSolidAngle } from '../world/atmosphere/sky.ts'
import { environmentScale, type PhysicalExposure } from '../world/exposure.ts'
import { isPhysical, type PhysicalSky } from '../world/physicalSky.ts'

// What lights the scene in each shading (design「显示模式」): wireframe has
// only a background; solid is the clay look, a fixed studio light that turns
// with the view (render/studio.ts); material preview is a fixed room the
// scene cannot change; rendered is the lit look the lit export also draws.
// The sun angles are document data (Y up); the lights sit in view space (Z up).

/** Material preview's backdrop; the asset viewer's 3D preview uses it too. */
export const previewBackground = '#5b5d61'

/** engine: the renderer the picture is drawn with (render/engines.ts effectiveEngine); the lit fill follows it. */
export function Lighting({ environment, display, engine = 'workbench' }: { environment: SceneEnvironment; display: DisplayMode; engine?: Engine }) {
  if (display === 'wireframe') return <color attach="background" args={[wireBackground]} />
  if (display === 'preview') return <RoomLighting />
  if (display === 'rendered') return isPhysical(environment.sky) ? <PhysicalLighting sky={environment.sky} fog={environment.fog} /> : <LitLighting environment={environment} engine={engine} />
  return <StudioLighting />
}

/** A sky is drawn in the picture, not in the AO's normals: it has no surface there. */
function drawnAside<T extends Mesh>(sky: T): T {
  sky.layers.set(ASIDE_LAYER)
  return sky
}

/**
 * Clay lighting: the fixed studio light on a neutral background (render/studio.ts);
 * the scene's sky, sun, ambient, fog and lights do not count.
 */
function StudioLighting() {
  return (
    <>
      <color attach="background" args={[STUDIO_BACKGROUND]} />
      <StudioLights up={[0, 0, 1]} />
    </>
  )
}

/**
 * The studio's lights, without the background: a little ambient, a fill
 * from up (the vertical: Z in the viewport, Y in the asset viewer) and the
 * key light, which aimStudio turns with the camera. None casts shadows.
 */
export function StudioLights({ up }: { up: [number, number, number] }) {
  return (
    <>
      <ambientLight intensity={STUDIO_AMBIENT} />
      <hemisphereLight args={[STUDIO_FILL.sky, STUDIO_FILL.ground, STUDIO_FILL.intensity]} position={up} />
      <directionalLight intensity={STUDIO_KEY_INTENSITY} userData={{ studio: true }} />
    </>
  )
}

/** Material preview's neutral room, kept on the device for as long as it lives. */
export function RoomLighting() {
  const { gl } = useThree()
  return <PreviewLighting room={() => gpuOf(gl).cache('room', () => roomEnvironment(gl as unknown as WebGPURenderer))} />
}

/** A neutral room as the only light: materials read the same whatever the scene's lighting. */
function PreviewLighting({ room }: { room: () => ReturnType<typeof roomEnvironment> }) {
  const { scene, invalidate } = useThree()
  useLayoutEffect(() => {
    const texture = room().texture
    scene.environment = texture
    scene.environmentIntensity = 1
    invalidate()
    return () => {
      if (scene.environment === texture) scene.environment = null
    }
  }, [scene, room, invalidate])
  return <color attach="background" args={[previewBackground]} />
}

/** How often a dragged sun may light the scene anew; the background follows at once. */
const environmentEveryMs = 200

/**
 * The lit look: the sky as background and as light (without its sun disc,
 * which the sun light counts), the sun, the ambient as a little fill, and fog.
 */
function LitLighting({ environment, engine }: { environment: SceneEnvironment; engine: Engine }) {
  const { gl, scene, invalidate } = useThree()
  const procedural = environment.sky.kind === 'procedural'
  const sky = useMemo(() => drawnAside(litSky()), [])
  useEffect(() => () => disposeSky(sky), [sky])
  useLayoutEffect(() => {
    setSky(sky, environment, true)
    invalidate()
  }, [sky, environment, invalidate])

  // The sky's light: made before the first frame, then at most every 200 ms.
  const latest = useRef(environment)
  latest.current = environment
  const lighting = useRef<{ pmrem: PMREMGenerator; target: RenderTarget | null; at: number; timer: number | null } | null>(null)
  useLayoutEffect(() => {
    const l = { pmrem: new PMREMGenerator(gl as unknown as WebGPURenderer), target: null as RenderTarget | null, at: -Infinity, timer: null as number | null }
    lighting.current = l
    return () => {
      if (l.timer !== null) window.clearTimeout(l.timer)
      if (l.target && scene.environment === l.target.texture) scene.environment = null
      l.target?.dispose()
      l.pmrem.dispose()
      lighting.current = null
    }
  }, [gl, scene])
  const skyKey = JSON.stringify(environment.sky)
  useLayoutEffect(() => {
    const l = lighting.current
    if (!l) return
    const run = () => {
      l.timer = null
      l.at = performance.now()
      const next = skyEnvironment(l.pmrem, latest.current)
      l.target?.dispose()
      l.target = next
      scene.environment = next.texture
      scene.environmentIntensity = 1
      invalidate()
    }
    if (l.timer !== null) window.clearTimeout(l.timer)
    const wait = l.at + environmentEveryMs - performance.now()
    if (wait <= 0) run()
    else l.timer = window.setTimeout(run, wait)
  }, [skyKey, scene, invalidate])

  return (
    <>
      {procedural ? <primitive object={sky} /> : <color attach="background" args={[environment.sky.color ?? '#8a8f98']} />}
      {procedural && <Sun environment={environment} />}
      {environment.fog && <fog attach="fog" args={[environment.fog.color, environment.fog.near, environment.fog.far]} />}
      <ambientLight color={environment.ambient.color} intensity={environment.ambient.intensity * ambientFill(engine)} />
    </>
  )
}

/** The lit sun: dimming and warming as it sets; shadows fitted like the clay sun's. */
function Sun({ environment }: { environment: SceneEnvironment }) {
  const ref = useRef<DirectionalLight>(null)
  useLayoutEffect(() => (ref.current ? fitSunShadow(ref.current) : undefined), [])
  const position = sunDirection(environment).multiplyScalar(60)
  const light = sunLight(environment.sky.sun_elevation_deg ?? 35, environment.sky.turbidity ?? 3)
  return (
    <directionalLight
      ref={ref}
      position={positionToView([position.x, position.y, position.z])}
      intensity={light.intensity}
      color={light.color}
      castShadow
      shadow-mapSize={[2048, 2048]}
      userData={{ sun: true }}
    />
  )
}

/**
 * The lit look under a physical sky (W2 design「大气 LUT 与重算」「预曝光的落点」):
 * the atmosphere's LUTs and the CPU's tables, remade when its four fields
 * change; the sky view, the sun light and the fog's light when anything of
 * the sky does; the sky light from the sky view without the disc, at most
 * every 200 ms like the other skies'. Everything is stored in physical
 * units or × s_env; the frame's exposure is written over it just before
 * drawing (world/exposure.ts applyExposure, from scene.userData.physical).
 * No ambient: the sky light is the fill. A new renderer (the device lost)
 * draws the LUTs and the sky light anew.
 */
function PhysicalLighting({ sky, fog }: { sky: PhysicalSky; fog: SceneEnvironment['fog'] }) {
  const { gl, scene, invalidate } = useThree()
  const latest = useRef(sky)
  latest.current = sky
  const parts = useMemo(() => {
    const luts = atmosphereLuts()
    const lutScale = uniform(1)
    const background = drawnAside(physicalSkyMesh(luts, lutScale))
    const source = physicalSkyMesh(luts, null)
    const sun = new DirectionalLight()
    sun.castShadow = true
    sun.shadow.mapSize.set(2048, 2048)
    sun.userData = { sun: true, baseIntensity: 0 }
    const physical: PhysicalExposure = { skyLightScale: 1, fogColor: null, fogLux: 0 }
    return { luts, lutScale, background, source, sourceScene: new Scene().add(source), sun, physical, cpu: null as CpuAtmosphere | null, lutEnv: 1 }
  }, [])
  useEffect(
    () => () => {
      parts.luts.dispose()
      disposeSky(parts.background)
      disposeSky(parts.source)
      parts.sun.dispose()
    },
    [parts],
  )
  useLayoutEffect(() => fitSunShadow(parts.sun), [parts])
  useLayoutEffect(() => {
    scene.userData.physical = parts.physical
    return () => {
      if (scene.userData.physical === parts.physical) delete scene.userData.physical
      scene.environmentIntensity = 1
    }
  }, [scene, parts])

  const renderer = gl as unknown as WebGPURenderer
  const atmosphereKey = [sky.altitude_m, sky.air_density, sky.dust_density, sky.ozone_density].join()
  useLayoutEffect(() => {
    const s = latest.current
    parts.luts.setAtmosphere(s)
    parts.luts.renderAtmosphere(renderer)
    parts.cpu = new CpuAtmosphere(s)
  }, [parts, renderer, atmosphereKey])

  const skyKey = JSON.stringify(sky)
  useLayoutEffect(() => {
    const s = latest.current
    const where = physicalSun(s)
    const toward = sunVector(where.elevationDeg, where.azimuthDeg)
    const top = sunAbove(s.sun_strength, s.sun_temperature_k)
    const day = parts.cpu!.daylight(toward.y, top)
    const sEnv = environmentScale(day.horizontal)
    parts.luts.setSun(toward.y, [top[0] * sEnv, top[1] * sEnv, top[2] * sEnv])
    parts.luts.renderSkyView(renderer)
    parts.lutScale.value = sEnv
    parts.lutEnv = sEnv
    for (const mesh of [parts.background, parts.source]) mesh.sky.sun.value.copy(toward)
    const omega = discSolidAngle(s.sun_size_deg)
    parts.background.sky.sunRadiance.value.set(top[0] / omega, top[1] / omega, top[2] / omega)
    parts.background.sky.sunCosRadius.value = Math.cos(((s.sun_size_deg / 2) * Math.PI) / 180)
    parts.background.sky.disc.value = s.sun_disc ? 1 : 0
    // The sun light in lux on a surface facing it, its colour what the atmosphere let through.
    const peak = Math.max(...day.sun)
    parts.sun.userData.baseIntensity = peak
    const [r, g, b] = peak > 0 ? day.sun.map((c) => c / peak) : [1, 1, 1]
    parts.sun.color.setRGB(r, g, b)
    parts.sun.position.fromArray(positionToView([toward.x * 60, toward.y * 60, toward.z * 60]))
    parts.physical.fogLux = day.horizontal
    invalidate()
  }, [parts, renderer, skyKey, invalidate])

  const fogKey = fog ? fog.color : ''
  useLayoutEffect(() => {
    parts.physical.fogColor = fogKey ? new Color(fogKey) : null
    invalidate()
  }, [parts, fogKey, invalidate])

  // The sky light, from the sky view as it is now and at its s_env: made before the first frame, then at most every 200 ms.
  const lighting = useRef<{ pmrem: PMREMGenerator; target: RenderTarget | null; at: number; timer: number | null } | null>(null)
  useLayoutEffect(() => {
    const l = { pmrem: new PMREMGenerator(renderer), target: null as RenderTarget | null, at: -Infinity, timer: null as number | null }
    lighting.current = l
    return () => {
      if (l.timer !== null) window.clearTimeout(l.timer)
      if (l.target && scene.environment === l.target.texture) scene.environment = null
      l.target?.dispose()
      l.pmrem.dispose()
      lighting.current = null
    }
  }, [renderer, scene])
  useLayoutEffect(() => {
    const l = lighting.current
    if (!l) return
    const run = () => {
      l.timer = null
      l.at = performance.now()
      const next = l.pmrem.fromScene(parts.sourceScene, 0, 0.1, 100)
      l.target?.dispose()
      l.target = next
      scene.environment = next.texture
      parts.physical.skyLightScale = parts.lutEnv
      invalidate()
    }
    if (l.timer !== null) window.clearTimeout(l.timer)
    const wait = l.at + environmentEveryMs - performance.now()
    if (wait <= 0) run()
    else l.timer = window.setTimeout(run, wait)
  }, [parts, skyKey, scene, invalidate])

  return (
    <>
      <primitive object={parts.background} />
      <primitive object={parts.sun} />
      {fog && <fog attach="fog" args={[fog.color, fog.near, fog.far]} />}
    </>
  )
}
