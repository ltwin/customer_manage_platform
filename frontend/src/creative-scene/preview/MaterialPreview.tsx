import { useMemo, useRef } from 'react'
import { Canvas } from '@react-three/fiber'
import { AgXToneMapping } from 'three'
import type { components } from '../../api/schema'
import { RoomLighting } from '../objects/Lighting.tsx'
import { SlotMesh } from '../objects/SlotMesh.tsx'
import { ballBounds, ballGeometry } from '../materials/ball.ts'
import { drawnGeometries } from '../materials/instances.ts'
import { resolveSlot } from '../materials/resolve.ts'
import { createRenderer } from '../render/gpu.ts'
import { Controls, FrameStep, type Orbit } from './stage.tsx'
import { fov, framing, useSlotImages } from './stageView.ts'

// The asset viewer's stage for a material asset, loaded only when one is
// opened: its slot on the material ball (C5c1 design「材质资产的存入与使用」),
// in the material preview's room, drawn once its images are in (the static
// thumbnail meanwhile). Drag to turn, scroll to zoom, double-click to go back.

type MaterialSlot = components['schemas']['CreativeSceneMaterialSlot']

const view = framing({ bounds: ballBounds })

export default function MaterialPreview({ slot, title, thumbnail }: { slot: MaterialSlot; title: string; thumbnail: string | null }) {
  const materials = useMemo(() => ({ main: slot }), [slot])
  const drawn = useSlotImages(materials, true)
  const material = useMemo(() => resolveSlot('main', materials, null, null).material, [materials])
  const controls = useRef<Orbit>(null)
  return (
    <div className="v-model" onDoubleClick={() => controls.current?.reset()}>
      <Canvas
        frameloop="demand"
        gl={({ canvas }) => createRenderer({ canvas: canvas as HTMLCanvasElement, output: 'direct', role: 'preview' }).then((g) => g.renderer)}
        dpr={[1, 2]}
        camera={{ fov, position: view.position, near: view.near, far: view.far }}
        // AgX like the editor (render/sceneRenderer.ts); R3F would set ACES Filmic.
        onCreated={({ gl }) => {
          gl.toneMapping = AgXToneMapping
        }}
        aria-label={`「${title}」的 3D 预览`}
      >
        <RoomLighting />
        {drawn && drawnGeometries(ballGeometry(), material).map((g, i) => <SlotMesh key={i} geometry={g} material={material} slot="main" doubleSided={false} castShadow={false} />)}
        <Controls controls={controls} target={view.target} />
        <FrameStep />
      </Canvas>
      {!drawn && thumbnail && <img className="v-model-still" src={thumbnail} alt={title} />}
      <p className="v-model-note">拖到场景里物件的槽位上使用</p>
    </div>
  )
}
