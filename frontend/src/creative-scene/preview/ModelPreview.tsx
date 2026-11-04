import { useEffect, useMemo, useRef, useState } from 'react'
import { Canvas } from '@react-three/fiber'
import { AgXToneMapping, DoubleSide, MeshStandardMaterial } from 'three'
import type { components } from '../../api/schema'
import type { ModelParts } from '../models/cache.ts'
import type { Faces } from '../models/faces.ts'
import { clay } from '../objects/look.ts'
import { RoomLighting, StudioLights, previewBackground } from '../objects/Lighting.tsx'
import { SlotMesh } from '../objects/SlotMesh.tsx'
import { resolvePart } from '../materials/resolve.ts'
import { drawnGeometries } from '../materials/instances.ts'
import { previewModels } from './cache.ts'
import { createRenderer } from '../render/gpu.ts'
import { Controls, FrameStep, type Orbit } from './stage.tsx'
import { fov, framing, useSlotImages } from './stageView.ts'

// The asset viewer's 3D stage, loaded only when a model is opened: drag to
// turn, scroll to zoom, double-click to go back. 「材质」, where it opens like
// the card thumbnail, is the materials the asset was saved with, a slot
// without one resolving as in the editor, in the material preview's room,
// drawn once their images are in (the static thumbnail meanwhile); 「白模」 is
// the editor's clay: its studio light, without AO.

type Manifest = components['schemas']['CreativeModelManifest']
type Materials = components['schemas']['CreativeSceneMaterialSlots']
type Look = 'clay' | 'material'

const clayMaterial = new MeshStandardMaterial({ color: clay, roughness: 0.92, metalness: 0, side: DoubleSide })

export default function ModelPreview({ revision, manifest, materials, faces, title, thumbnail }: { revision: string; manifest: Manifest; materials: Materials; faces: Faces | null; title: string; thumbnail: string | null }) {
  const [look, setLook] = useState<Look>('material')
  const [loaded, setLoaded] = useState<{ revision: string; parts: ModelParts | null } | null>(null)
  useEffect(() => {
    let live = true
    previewModels.load(revision, faces).then(
      (parts) => live && setLoaded({ revision, parts }),
      () => live && setLoaded({ revision, parts: null }),
    )
    return () => {
      live = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- a revision's faces never change
  }, [revision])
  const parts = loaded?.revision === revision ? loaded.parts : undefined
  const imagesIn = useSlotImages(materials, look === 'material')
  const drawn = !!parts && imagesIn
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
        // R3F sets ACES Filmic once it has the renderer; AgX like the editor (render/sceneRenderer.ts).
        onCreated={({ gl }) => {
          gl.toneMapping = AgXToneMapping
        }}
        aria-label={`「${title}」的 3D 预览`}
      >
        {look === 'clay' ? (
          <>
            <color attach="background" args={[previewBackground]} />
            <StudioLights up={[0, 1, 0]} />
          </>
        ) : (
          <RoomLighting />
        )}
        {drawn && <Meshes parts={parts} manifest={manifest} materials={materials} look={look} />}
        <Controls controls={controls} target={view.target} />
        <FrameStep opening={view.turn} />
      </Canvas>
      {!drawn && thumbnail && <img className="v-model-still" src={thumbnail} alt={title} />}
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
      {parts.meshes.map((m, i) => {
        if (look === 'clay') return <mesh key={i} geometry={m.geometry} material={clayMaterial} />
        const material = resolvePart(m, materials, hints).material
        // See-through: piece by piece, as the editor draws it (objects/ModelMesh.tsx).
        return drawnGeometries(m.geometry, material).map((g, j) => <SlotMesh key={`${i}:${j}`} geometry={g} material={material} slot={m.slot} doubleSided castShadow={false} />)
      })}
    </group>
  )
}
