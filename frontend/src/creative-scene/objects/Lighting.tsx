import { useEffect, useLayoutEffect, useMemo, useRef } from 'react'
import { useThree } from '@react-three/fiber'
import type { DirectionalLight, Mesh } from 'three'
import { PMREMGenerator, type RenderTarget, type WebGPURenderer } from 'three/webgpu'
import type { SceneEnvironment } from '../api.ts'
import type { Engine } from '../render/engines.ts'
import { ambientFill, sunLight } from '../render/litLook.ts'
import { gpuOf } from '../render/gpu.ts'
import { disposeSky, litSky, roomEnvironment, setSky, skyEnvironment, sunDirection } from '../render/sky.ts'
import { STUDIO_AMBIENT, STUDIO_BACKGROUND, STUDIO_FILL, STUDIO_KEY_INTENSITY } from '../render/studio.ts'
import { fitSunShadow } from '../render/sunShadow.ts'
import { positionToView } from '../space.ts'
import { ASIDE_LAYER } from '../render/sceneRenderer.ts'
import type { DisplayMode } from '../state/sceneStore.ts'
import { wireBackground } from './wireframe.ts'

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
  if (display === 'rendered') return <LitLighting environment={environment} engine={engine} />
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
