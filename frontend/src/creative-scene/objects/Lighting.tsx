import { useEffect, useLayoutEffect, useMemo, useRef, type ComponentRef, type Ref } from 'react'
import { useThree } from '@react-three/fiber'
import { Sky } from '@react-three/drei'
import { PMREMGenerator, type DirectionalLight, type WebGLRenderTarget } from 'three'
import type { SceneEnvironment } from '../api.ts'
import { AMBIENT_FILL, sunLight } from '../render/litLook.ts'
import { roomEnvironment, setSky, skyEnvironment, sunDirection, viewSky } from '../render/sky.ts'
import { fitSunShadow } from '../render/sunShadow.ts'
import { positionToView } from '../space.ts'
import type { DisplayMode } from '../state/sceneStore.ts'
import { wireBackground } from './wireframe.ts'

// What lights the scene in each shading (design「显示模式」): wireframe has
// only a background; solid is the clay look, unchanged since C4; material
// preview is a fixed studio light the scene cannot change; rendered is the lit
// look the lit export also draws. The sun angles are document data (Y up);
// the lights sit in view space (Z up).

/** Material preview's backdrop; the asset viewer's 3D preview uses it too. */
export const previewBackground = '#5b5d61'

export function Lighting({ environment, display }: { environment: SceneEnvironment; display: DisplayMode }) {
  const { gl } = useThree()
  // Material preview's light is made once and kept while the editor is open.
  const room = useRef<ReturnType<typeof roomEnvironment> | null>(null)
  useEffect(
    () => () => {
      room.current?.dispose()
      room.current = null
    },
    [],
  )
  if (display === 'wireframe') return <color attach="background" args={[wireBackground]} />
  if (display === 'preview') return <PreviewLighting room={() => (room.current ??= roomEnvironment(gl))} />
  if (display === 'rendered') return <LitLighting environment={environment} />
  return <ClayLighting environment={environment} />
}

/**
 * Clay lighting: the scene's sky and ambient, one sun with shadows; tuned for
 * the clay look and kept as it was so clay exports do not change.
 */
export function ClayLighting({ environment }: { environment: SceneEnvironment }) {
  const sun = sunDirection(environment).multiplyScalar(60)
  const sky = environment.sky
  const skyRef = useRef<ComponentRef<typeof Sky>>(null)
  const sunRef = useRef<DirectionalLight>(null)
  const invalidate = useThree((s) => s.invalidate)
  useEffect(() => {
    skyRef.current?.material.uniforms.up.value.set(0, 0, 1)
    invalidate()
  }, [sky.kind, invalidate])
  // Before the first frame: an offscreen shot draws only one.
  useLayoutEffect(() => (sunRef.current ? fitSunShadow(sunRef.current) : undefined), [])
  return (
    <>
      {sky.kind === 'procedural' ? (
        <Sky ref={skyRef} sunPosition={positionToView([sun.x, Math.max(sun.y, 0.5), sun.z])} turbidity={sky.turbidity ?? 3} />
      ) : (
        <color attach="background" args={[sky.kind === 'color' ? (sky.color ?? '#8a8f98') : '#8a8f98']} />
      )}
      {environment.fog && <fog attach="fog" args={[environment.fog.color, environment.fog.near, environment.fog.far]} />}
      <ClayLights ambient={environment.ambient} sun={positionToView([sun.x, Math.max(sun.y, 2), sun.z])} up={[0, 0, 1]} sunRef={sunRef} />
    </>
  )
}

/**
 * The clay look's lights, without the scene around them: the ambient lifted
 * a little, a sky fill and one sun. A sun given a ref casts shadows, whose
 * bounds its owner fits.
 */
export function ClayLights({ ambient, sun, up, sunRef }: { ambient: { color: string; intensity: number }; sun: [number, number, number]; up: [number, number, number]; sunRef?: Ref<DirectionalLight> }) {
  return (
    <>
      <ambientLight color={ambient.color} intensity={ambient.intensity + 0.3} />
      <hemisphereLight args={['#ffffff', '#6b6760', 0.6]} position={up} />
      {/* The shadow camera's bounds and normal bias follow the scene (fitSunShadow). */}
      <directionalLight ref={sunRef} position={sun} intensity={2.2} castShadow={!!sunRef} shadow-mapSize={[2048, 2048]} />
    </>
  )
}

/** Material preview's neutral room on its own, made and freed with the component. */
export function RoomLighting() {
  const { gl } = useThree()
  const room = useRef<ReturnType<typeof roomEnvironment> | null>(null)
  useEffect(
    () => () => {
      room.current?.dispose()
      room.current = null
    },
    [],
  )
  return <PreviewLighting room={() => (room.current ??= roomEnvironment(gl))} />
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
function LitLighting({ environment }: { environment: SceneEnvironment }) {
  const { gl, scene, invalidate } = useThree()
  const procedural = environment.sky.kind === 'procedural'
  const sky = useMemo(() => viewSky(), [])
  useEffect(
    () => () => {
      sky.geometry.dispose()
      sky.material.dispose()
    },
    [sky],
  )
  useLayoutEffect(() => {
    setSky(sky, environment, true)
    invalidate()
  }, [sky, environment, invalidate])

  // The sky's light: made before the first frame, then at most every 200 ms.
  const latest = useRef(environment)
  latest.current = environment
  const lighting = useRef<{ pmrem: PMREMGenerator; target: WebGLRenderTarget | null; at: number; timer: number | null } | null>(null)
  useLayoutEffect(() => {
    const l = { pmrem: new PMREMGenerator(gl), target: null as WebGLRenderTarget | null, at: -Infinity, timer: null as number | null }
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
      {procedural ? <primitive object={sky} scale={1000} /> : <color attach="background" args={[environment.sky.color ?? '#8a8f98']} />}
      {procedural && <Sun environment={environment} />}
      {environment.fog && <fog attach="fog" args={[environment.fog.color, environment.fog.near, environment.fog.far]} />}
      <ambientLight color={environment.ambient.color} intensity={environment.ambient.intensity * AMBIENT_FILL} />
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
    />
  )
}
