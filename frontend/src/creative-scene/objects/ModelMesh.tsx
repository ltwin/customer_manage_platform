import { useEffect, useMemo, useState } from 'react'
import { EdgeLines } from './EdgeLines.tsx'
import { Outline } from './Outline.tsx'
import { DoubleSide, MeshStandardMaterial } from 'three'
import { modelCache, type ModelParts, type SceneModel } from '../models/cache.ts'
import { clay, unavailableGrey } from './look.ts'
import { WireMesh } from './WireMesh.tsx'
import { SlotMesh } from './SlotMesh.tsx'
import { resolveSlot } from '../materials/resolve.ts'
import type { SceneObject } from '../api.ts'

// A model instance. Every instance of a revision draws the same cached
// geometries in clay; the module's own materials only name slots. Until the
// GLB is in, an outline of the manifest's bounds stands in; a model that
// cannot be shown is a grey block of the same size.

// Code-built models are often open (a lathe without caps), so both sides draw.
const clayMaterial = new MeshStandardMaterial({ color: clay, roughness: 0.92, metalness: 0, side: DoubleSide })
const unavailableMaterial = new MeshStandardMaterial({ color: unavailableGrey, roughness: 1, metalness: 0 })
// A model still being baked and saved, at the point it will land.
const ghostMaterial = new MeshStandardMaterial({ color: clay, roughness: 0.92, metalness: 0, side: DoubleSide, transparent: true, opacity: 0.45, depthWrite: false })
const noRaycast = () => {}

type Loaded = { revision: string; parts: ModelParts | null }

type Slots = NonNullable<SceneObject['material_slots']>

/** slots: the instance's material slots in the material looks, null for clay. */
/** boundsWhileLoading: a ghost outlines the manifest's bounds until its GLB is in, instead of drawing nothing. */
export function ModelMesh({ revision, summary, edge, ghost = false, wire = false, slots = null, boundsWhileLoading = false }: { revision: string; summary: SceneModel | undefined; edge: string | null; ghost?: boolean; wire?: boolean; slots?: Slots | null; boundsWhileLoading?: boolean }) {
  const usable = summary?.usable !== false
  // A model already in the cache draws from the first frame (an offscreen
  // screenshot renders exactly once).
  const [loaded, setLoaded] = useState<Loaded | null>(() => {
    const parts = usable ? modelCache.peek(revision) : undefined
    return parts === undefined ? null : { revision, parts }
  })
  useEffect(() => {
    if (!usable) return
    let live = true
    modelCache.load(revision).then(
      (parts) => live && setLoaded({ revision, parts }),
      () => live && setLoaded({ revision, parts: null }),
    )
    return () => {
      live = false
    }
  }, [revision, usable])
  const bounds = summary?.manifest?.bounds
  if (!usable || (loaded?.revision === revision && !loaded.parts)) return <BoundsBox bounds={bounds} edge={edge} unavailable />
  // While a new revision loads, the previous one stays on screen.
  if (!loaded?.parts) return ghost && !boundsWhileLoading ? null : <BoundsBox bounds={bounds} edge={edge} ghost={ghost} />
  if (wire && !ghost)
    return (
      <group>
        {loaded.parts.meshes.map((m, i) => (
          <WireMesh key={i} geometry={m.geometry} edge={edge} />
        ))}
      </group>
    )
  if (slots && !ghost) {
    const hints = new Map((summary?.manifest?.slots ?? []).map((x) => [x.id, x.hint]))
    return (
      <group>
        {loaded.parts.meshes.map((m, i) => (
          <SlotMesh key={i} geometry={m.geometry} material={resolveSlot(m.slot, slots, (m.slot !== null ? hints.get(m.slot) : undefined) ?? m.hint, m.constants).material} doubleSided castShadow>
            {edge && <Outline color={edge} />}
          </SlotMesh>
        ))}
      </group>
    )
  }
  return (
    <group>
      {loaded.parts.meshes.map((m, i) => (
        <mesh key={i} geometry={m.geometry} material={ghost ? ghostMaterial : clayMaterial} castShadow={!ghost} receiveShadow={!ghost} {...(ghost ? { raycast: noRaycast } : {})}>
          {edge && <Outline color={edge} />}
        </mesh>
      ))}
    </group>
  )
}

const fallback = { min: [-0.5, 0, -0.5], max: [0.5, 1, 0.5] }

type Bounds = NonNullable<SceneModel['manifest']>['bounds']

function BoundsBox({ bounds = fallback, edge, unavailable = false, ghost = false }: { bounds?: Bounds; edge: string | null; unavailable?: boolean; ghost?: boolean }) {
  const { center, size } = useMemo(() => {
    const size = [0, 1, 2].map((i) => Math.max(bounds.max[i] - bounds.min[i], 0.01)) as [number, number, number]
    const center = [0, 1, 2].map((i) => (bounds.max[i] + bounds.min[i]) / 2) as [number, number, number]
    return { center, size }
  }, [bounds])
  return (
    <mesh position={center} material={unavailable ? unavailableMaterial : undefined} {...(ghost ? { raycast: noRaycast } : {})}>
      <boxGeometry args={size} />
      {/* Loading: invisible but still clickable, with its edges drawn. */}
      {!unavailable && <meshBasicMaterial visible={false} />}
      {unavailable ? edge && <Outline color={edge} /> : <EdgeLines color={edge ?? '#8a8780'} />}
    </mesh>
  )
}
