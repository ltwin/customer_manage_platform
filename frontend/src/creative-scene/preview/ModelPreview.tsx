import { useEffect, useMemo, useRef, useState, type ComponentRef, type RefObject } from 'react'
import { Canvas, useFrame } from '@react-three/fiber'
import { OrbitControls } from '@react-three/drei'
import { DoubleSide, MathUtils, MeshStandardMaterial, Vector3 } from 'three'
import type { components } from '../../api/schema'
import type { ModelParts } from '../models/cache.ts'
import { clay } from '../objects/look.ts'
import { ClayLights, RoomLighting, previewBackground } from '../objects/Lighting.tsx'
import { SlotMesh } from '../objects/SlotMesh.tsx'
import { resolveSlot } from '../materials/resolve.ts'
import { previewModels } from './cache.ts'
import { createRenderer, gpuOf } from '../render/gpu.ts'

// The asset viewer's 3D stage, loaded only when a model is opened: drag to
// turn, scroll to zoom, double-click to go back. 「材质」, where it opens like
// the card thumbnail, is the materials the asset was saved with, a slot
// without one resolving as in the editor, in the material preview's room;
// 「白模」 is the editor's clay.

type Manifest = components['schemas']['CreativeModelManifest']
type Materials = components['schemas']['CreativeSceneMaterialSlots']
type Look = 'clay' | 'material'
type Orbit = ComponentRef<typeof OrbitControls>

const fov = 30
const clayMaterial = new MeshStandardMaterial({ color: clay, roughness: 0.92, metalness: 0, side: DoubleSide })

/** The thumbnail's view: three-quarter from the front (+Z), centred on the bounds. */
function framing(manifest: Manifest) {
  const min = new Vector3().fromArray(manifest.bounds.min)
  const max = new Vector3().fromArray(manifest.bounds.max)
  const center = min.clone().add(max).multiplyScalar(0.5)
  const radius = Math.max(min.distanceTo(max) / 2, 0.01)
  // Looser than the thumbnail's 1.05: the stage has room for the buttons below.
  const distance = (radius / Math.sin(MathUtils.degToRad(fov / 2))) * 1.35
  const position = center.clone().add(new Vector3(0.9, 0.7, 1.4).normalize().multiplyScalar(distance))
  return { position: position.toArray(), target: center.toArray(), near: distance / 100, far: distance * 20 }
}

export default function ModelPreview({ revision, manifest, materials, title, thumbnail }: { revision: string; manifest: Manifest; materials: Materials; title: string; thumbnail: string | null }) {
  const [look, setLook] = useState<Look>('material')
  const [loaded, setLoaded] = useState<{ revision: string; parts: ModelParts | null } | null>(null)
  useEffect(() => {
    let live = true
    previewModels.load(revision).then(
      (parts) => live && setLoaded({ revision, parts }),
      () => live && setLoaded({ revision, parts: null }),
    )
    return () => {
      live = false
    }
  }, [revision])
  const parts = loaded?.revision === revision ? loaded.parts : undefined
  const view = useMemo(() => framing(manifest), [manifest])
  const controls = useRef<Orbit>(null)
  if (parts === null)
    return (
      <div className="v-model is-failed" role="alert">
        {thumbnail && <img src={thumbnail} alt={title} />}
        <p>无法预览</p>
      </div>
    )
  return (
    <div className="v-model" onDoubleClick={() => controls.current?.reset()}>
      <Canvas
        key={revision}
        frameloop="demand"
        gl={({ canvas }) => createRenderer({ canvas: canvas as HTMLCanvasElement, output: 'direct', role: 'preview' }).then((g) => g.renderer)}
        dpr={[1, 2]}
        camera={{ fov, position: view.position, near: view.near, far: view.far }}
        aria-label={`「${title}」的 3D 预览`}
      >
        {look === 'clay' ? (
          <>
            <color attach="background" args={[previewBackground]} />
            <ClayLights ambient={{ color: '#ffffff', intensity: 0.4 }} sun={[3, 5, 4]} up={[0, 1, 0]} />
          </>
        ) : (
          <RoomLighting />
        )}
        {parts && <Meshes parts={parts} manifest={manifest} materials={materials} look={look} />}
        <Controls controls={controls} target={view.target} />
        <FrameStep />
      </Canvas>
      {!parts && thumbnail && <img className="v-model-still" src={thumbnail} alt={title} />}
      <div className="v-model-looks" role="group" aria-label="显示方式">
        <button type="button" aria-pressed={look === 'clay'} onClick={() => setLook('clay')}>
          白模
        </button>
        <button type="button" aria-pressed={look === 'material'} onClick={() => setLook('material')}>
          材质
        </button>
      </div>
      {look === 'material' && <p className="v-model-note">放进场景后可以换材质</p>}
    </div>
  )
}

function Meshes({ parts, manifest, materials, look }: { parts: ModelParts; manifest: Manifest; materials: Materials; look: Look }) {
  const hints = useMemo(() => new Map(manifest.slots.map((s) => [s.id, s.hint])), [manifest])
  return (
    <group>
      {parts.meshes.map((m, i) =>
        look === 'clay' ? (
          <mesh key={i} geometry={m.geometry} material={clayMaterial} />
        ) : (
          <SlotMesh key={i} geometry={m.geometry} material={resolveSlot(m.slot, materials, (m.slot !== null ? hints.get(m.slot) : undefined) ?? m.hint, m.constants).material} doubleSided castShadow={false} />
        ),
      )}
    </group>
  )
}

/** Orbit around the model's centre; the opening view is what a double-click returns to. */
/** Steps the renderer's frame before R3F draws one (render/gpu.ts owns the count). */
function FrameStep() {
  useFrame((state) => gpuOf(state.gl).advanceFrame(), -1)
  return null
}

function Controls({ controls, target }: { controls: RefObject<Orbit | null>; target: number[] }) {
  useEffect(() => {
    controls.current?.saveState()
  }, [controls, target])
  return <OrbitControls ref={controls} makeDefault target={target as [number, number, number]} enablePan={false} />
}
