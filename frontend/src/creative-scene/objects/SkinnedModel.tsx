import { useEffect, useLayoutEffect, useMemo, useState, type ReactNode } from 'react'
import { createPortal, useThree } from '@react-three/fiber'
import { useStore } from 'zustand'
import { Mesh, SkinnedMesh, type BufferGeometry, type Material } from 'three'
import { meshPart, type ModelMeshPart, type ModelSkeleton, type SceneModel } from '../models/cache.ts'
import { onGeometry, resolvePart } from '../materials/resolve.ts'
import { useSlotMaterial } from '../materials/useSlotMaterial.ts'
import { renderPolicy } from '../materials/principled.ts'
import { raycastPosed } from '../placement/raycast.ts'
import { posableOf, posedGeometries, rigKey, testPoses } from '../rig/pose.ts'
import type { Posed } from '../rig/skin.ts'
import type { SceneObject } from '../api.ts'
import { Outline } from './Outline.tsx'
import { disposeEdges, edgesOf, hiddenMaterial, wireColor } from './wireframe.ts'

// A skinned model's skeleton, drawn by one instance (design「带骨骼的模型怎样
// 加载和绘制」): a clone of the model's template with a skeleton of its own,
// in the object's current pose (rig/pose.ts), its skinned meshes drawn by
// three's GPU skinning. What does not draw on the GPU reads the same pose's
// static geometry in the model root's space, set on each skinned mesh's
// userData.posed: the world-space layer (render/trace/collect.ts), picking
// (placement/raycast.ts raycastPosed) and the selection outline. The meshes
// riding joints are ordinary meshes moving with them.

type Slots = NonNullable<SceneObject['material_slots']>

/** How the instance draws: clay, a ghost being placed, wireframe, or its slots' materials. */
export type SkinLook = { kind: 'clay' | 'ghost' | 'wire' } | { kind: 'slots'; slots: Slots; hints: ReadonlyMap<string, string | undefined> }

type Drawn = { mesh: Mesh; part: ModelMeshPart; skinned: boolean; id: string | null }

const noRaycast = () => {}

/** objectId: whose pose it takes; none for a ghost, at rest. */
export function SkinnedModel({ modelKey, skeleton, summary, objectId, look, edge, clay, ghost }: { modelKey: string; skeleton: ModelSkeleton; summary: SceneModel | undefined; objectId?: string; look: SkinLook; edge: string | null; clay: Material; ghost: Material }) {
  // A model's rig never changes, but a summary may come without it first (placed from an asset) and with it after.
  const rig = rigKey(summary?.rig)
  const p = useMemo(
    () => posableOf(skeleton.template, summary?.rig, summary?.manifest?.skeleton ?? undefined),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- the summary's other fields do not bear on the pose
    [skeleton, rig],
  )
  // The meshes and what their GLB materials say, before the look replaces the materials.
  const drawn = useMemo(() => {
    const ids = new Map(p.instance.skinned.map((s) => [s.mesh as Mesh, s.id]))
    const out: Drawn[] = []
    p.instance.root.traverse((o) => {
      if (o instanceof Mesh) out.push({ mesh: o, part: meshPart(o.geometry, o.material as Material), skinned: o instanceof SkinnedMesh, id: ids.get(o) ?? null })
    })
    return out
  }, [p])
  const q = useStore(testPoses, (s) => (objectId ? (s[objectId] ?? null) : null))
  const [geometries, setGeometries] = useState<ReadonlyMap<string, BufferGeometry>>(skeleton.rest)
  const invalidate = useThree((s) => s.invalidate)
  useLayoutEffect(() => {
    const held = posedGeometries(p, modelKey, skeleton.rest, q)
    for (const s of p.instance.skinned) s.mesh.userData.posed = { geometry: held.geometries.get(s.id)!, root: p.instance.root } satisfies Posed
    setGeometries(held.geometries)
    invalidate()
    return () => {
      for (const s of p.instance.skinned) delete s.mesh.userData.posed
      held.release()
    }
  }, [p, modelKey, skeleton, q, invalidate])
  const root = p.instance.root
  return (
    <>
      <primitive object={root} />
      {drawn.map((d, i) => {
        const geometry = d.id !== null ? geometries.get(d.id) : undefined
        return (
          <Skin key={i} drawn={d} look={look} clay={clay} ghost={ghost}>
            {edge && look.kind !== 'ghost' && look.kind !== 'wire' && (d.skinned ? createPortal(<Outline color={edge} geometry={geometry} />, root) : createPortal(<Outline color={edge} />, d.mesh))}
            {look.kind === 'wire' && createPortal(<Edges geometry={d.skinned ? geometry! : d.mesh.geometry} color={edge ?? wireColor} rest={!d.skinned || geometry === skeleton.rest.get(d.id!)} />, d.skinned ? root : d.mesh)}
          </Skin>
        )
      })}
    </>
  )
}

/** One of the instance's meshes, given its look's material; children go into the scene where they belong. */
function Skin({ drawn, look, clay, ghost, children }: { drawn: Drawn; look: SkinLook; clay: Material; ghost: Material; children?: ReactNode }) {
  if (look.kind === 'slots') return <SlotSkin drawn={drawn} slots={look.slots} hints={look.hints}>{children}</SlotSkin>
  const material = look.kind === 'clay' ? clay : look.kind === 'ghost' ? ghost : hiddenMaterial
  return (
    <Dress drawn={drawn} material={material} shadows={look.kind === 'clay'} pickable={look.kind !== 'ghost'}>
      {children}
    </Dress>
  )
}

function SlotSkin({ drawn, slots, hints, children }: { drawn: Drawn; slots: Slots; hints: ReadonlyMap<string, string | undefined>; children?: ReactNode }) {
  const resolved = resolvePart(drawn.part, slots, hints).material
  const material = useSlotMaterial(onGeometry(resolved, 'uv' in drawn.mesh.geometry.attributes), true)
  return (
    <Dress drawn={drawn} material={material} shadows={!renderPolicy(resolved).seeThrough} pickable>
      {children}
    </Dress>
  )
}

/** Puts the material on the mesh, as SlotMesh and ModelMesh draw theirs. */
function Dress({ drawn: { mesh, part, skinned }, material, shadows, pickable, children }: { drawn: Drawn; material: Material; shadows: boolean; pickable: boolean; children?: ReactNode }) {
  useLayoutEffect(() => {
    mesh.material = material
    mesh.castShadow = shadows
    mesh.receiveShadow = shadows
    mesh.userData.slot = part.slot
    mesh.raycast = !pickable ? noRaycast : skinned ? raycastPosed : Mesh.prototype.raycast
    // three's bounds are the rest skin's: a pose may leave them.
    if (skinned) mesh.frustumCulled = false
  }, [mesh, part, skinned, material, shadows, pickable])
  return <>{children}</>
}

/** A mesh's edges in wireframe; rest: the edges of a geometry the model keeps, freed with it; a pose's are freed here. */
function Edges({ geometry, color, rest }: { geometry: BufferGeometry; color: string; rest: boolean }) {
  const edges = useMemo(() => edgesOf(geometry), [geometry])
  useEffect(() => (rest ? undefined : () => disposeEdges(geometry)), [geometry, rest])
  return (
    <lineSegments geometry={edges} raycast={noRaycast}>
      <lineBasicMaterial color={color} />
    </lineSegments>
  )
}
