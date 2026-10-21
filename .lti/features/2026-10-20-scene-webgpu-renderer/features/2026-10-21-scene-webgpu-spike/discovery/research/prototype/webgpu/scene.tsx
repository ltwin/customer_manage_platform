import { useLayoutEffect, useMemo, useRef } from 'react'
import { useThree } from '@react-three/fiber'
import { Color, Mesh, MeshStandardNodeMaterial, type DirectionalLight, type Material } from 'three/webgpu'
import type { Scene, SceneObject } from '../../src/creative-scene/api.ts'
import { primitiveGeometry } from '../../src/creative-scene/builtins/geometry.ts'
import { applyLight, buildLight, disposeLight } from '../../src/creative-scene/objects/lights.ts'
import { clay } from '../../src/creative-scene/objects/look.ts'
import { AMBIENT_FILL, sunLight } from '../../src/creative-scene/render/litLook.ts'
import { fitSunShadow } from '../../src/creative-scene/render/sunShadow.ts'
import { docToView } from '../../src/creative-scene/space.ts'
import { worldOf } from '../../src/creative-scene/state/world.ts'
import { sunDirection } from './doc.ts'

// The reference scene drawn plainly: primitives with one material each
// (clay, or what materialFor gives), the scene's lights, the sun with a
// shadow and the ambient. Prototypes swap in their own materials and passes.

export type MaterialFor = (o: SceneObject) => Material

export const clayMaterial: MaterialFor = () => new MeshStandardNodeMaterial({ color: new Color(clay), roughness: 1, metalness: 0 })

export function SceneContent({ doc, materialFor = clayMaterial, sceneLights = true }: { doc: Scene; materialFor?: MaterialFor; sceneLights?: boolean }) {
  const meshes = useMemo(() => {
    const world = worldOf(doc.objects)
    const out: Mesh[] = []
    for (const o of doc.objects) {
      if (o.kind !== 'primitive' || !o.primitive || o.visible === false) continue
      const geometry = primitiveGeometry(o.primitive.builtin, o.primitive.params)
      if (!geometry) continue
      const mesh = new Mesh(geometry, materialFor(o))
      mesh.name = o.id
      mesh.matrixAutoUpdate = false
      mesh.matrix.copy(world(o.id))
      mesh.castShadow = true
      mesh.receiveShadow = true
      out.push(mesh)
    }
    return out
  }, [doc, materialFor])
  useLayoutEffect(
    () => () => {
      for (const m of meshes) {
        m.geometry.dispose()
        ;(m.material as Material).dispose()
      }
    },
    [meshes],
  )
  return (
    <group quaternion={docToView}>
      {meshes.map((m) => (
        <primitive key={m.name} object={m} />
      ))}
      {sceneLights && <Lights doc={doc} />}
      <Sun doc={doc} />
    </group>
  )
}

function Lights({ doc }: { doc: Scene }) {
  const built = useMemo(() => {
    const world = worldOf(doc.objects)
    return doc.objects
      .filter((o) => o.kind === 'light' && o.light)
      .map((o) => {
        const b = buildLight(o.light!.type)
        applyLight(b, o.light!)
        b.light.matrixAutoUpdate = false
        b.light.matrix.copy(world(o.id))
        return { id: o.id, b }
      })
  }, [doc])
  useLayoutEffect(() => () => built.forEach(({ b }) => disposeLight(b)), [built])
  return (
    <>
      {built.map(({ id, b }) => (
        <primitive key={id} object={b.light}>
          {b.target && <primitive object={b.target} />}
        </primitive>
      ))}
    </>
  )
}

function Sun({ doc }: { doc: Scene }) {
  const env = doc.environment
  const procedural = env.sky.kind === 'procedural'
  const sun = useMemo(() => (procedural ? sunLight(env.sky.sun_elevation_deg ?? 35, env.sky.turbidity ?? 3) : null), [procedural, env])
  const at = sunDirection(doc).multiplyScalar(30)
  const scene = useThree((s) => s.scene)
  // ?fit=1: the editor's sunShadow.ts fit, which patches shadow.updateMatrices.
  const ref = useRef<DirectionalLight>(null)
  useLayoutEffect(() => (new URLSearchParams(location.search).get('fit') === '1' && ref.current ? fitSunShadow(ref.current) : undefined), [sun])
  useLayoutEffect(() => {
    scene.background = new Color(env.sky.kind === 'color' ? (env.sky.color ?? '#8a8f98') : '#9db8d0')
  }, [scene, env])
  return (
    <>
      <ambientLight color={env.ambient?.color ?? '#ffffff'} intensity={(env.ambient?.intensity ?? 0.4) * AMBIENT_FILL} />
      {sun && (
        <directionalLight
          ref={ref}
          position={[at.x, at.y, at.z]}
          color={sun.color}
          intensity={sun.intensity}
          castShadow
          shadow-mapSize={[2048, 2048]}
          shadow-camera-left={-15}
          shadow-camera-right={15}
          shadow-camera-top={15}
          shadow-camera-bottom={-15}
          shadow-camera-far={80}
          shadow-bias={0}
        />
      )}
    </>
  )
}
