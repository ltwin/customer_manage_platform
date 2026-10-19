import { useEffect, useMemo } from 'react'
import { Edges, Outlines } from '@react-three/drei'
import { BufferGeometry, Float32BufferAttribute, MeshStandardMaterial } from 'three'
import type { Placeholder } from '../agentActivity.ts'
import type { Scene, SceneObject } from '../api.ts'
import { primitiveGeometry } from '../builtins/geometry.ts'
import { aspectOf, sharpRange, verticalFov } from '../cameraOptics.ts'
import { cameraFraming } from '../host/framing.ts'
import type { SceneModel } from '../models/cache.ts'
import { cameraDefaults } from '../state/apply.ts'
import { docToView, transformToView } from '../space.ts'
import { activeEdge, agentEdge, clay, selectedEdge } from './look.ts'
import { ModelMesh } from './ModelMesh.tsx'

// Draws the scene tree. Every object is a group named by its ID (picking and
// transforms find it by name); hiding an object hides only its own drawing,
// not its children, as in Blender. The viewport is always clay.
//
// Object groups carry view-space transforms (Z up, see space.ts), so moving,
// rotating and the object's own axes read as in Blender. What an object
// draws is authored in document space (Y up) and turns once inside it.


type Props = {
  scene: Scene
  /** Summaries of the models the scene names, by content revision. */
  models: Readonly<Record<string, SceneModel>>
  selected: ReadonlySet<string>
  active: string | null
  /** Objects the assistant is working on, outlined in its colour. */
  highlighted?: ReadonlySet<string>
  /** Models the assistant is adding, as translucent blocks where they will appear. */
  placeholders?: readonly Placeholder[]
  /** Light and camera icons; a screenshot leaves them out. */
  helpers?: boolean
  /** The camera the viewport looks through; its own frustum would be in the way. */
  through?: string | null
}

type Tree = Required<Omit<Props, 'scene' | 'placeholders' | 'through'>> & {
  through: string | null
  scene: Scene
  childrenOf: Map<string, SceneObject[]>
  placeholdersOf: Map<string, Placeholder[]>
}

const none: ReadonlySet<string> = new Set()
const noPlaceholders: readonly Placeholder[] = []

function grouped<T>(items: readonly T[], key: (item: T) => string | null): Map<string, T[]> {
  const out = new Map<string, T[]>()
  for (const item of items) {
    const k = key(item) ?? ''
    const list = out.get(k) ?? []
    list.push(item)
    out.set(k, list)
  }
  return out
}

export function SceneObjects({ scene, models, selected, active, highlighted = none, placeholders = noPlaceholders, helpers = true, through = null }: Props) {
  const childrenOf = useMemo(() => {
    const byParent = grouped(scene.objects, (o) => o.parent_id)
    for (const list of byParent.values()) list.sort((a, b) => a.order - b.order)
    return byParent
  }, [scene.objects])
  const placeholdersOf = useMemo(() => {
    // One whose parent went away waits at the top level.
    const ids = new Set(scene.objects.map((o) => o.id))
    return grouped(placeholders, (p) => (p.parentID && ids.has(p.parentID) ? p.parentID : null))
  }, [placeholders, scene.objects])
  const tree: Tree = { scene, childrenOf, placeholdersOf, models, selected, active, highlighted, helpers, through }
  return (
    <>
      {(childrenOf.get('') ?? []).map((o) => (
        <ObjectNode key={o.id} object={o} tree={tree} />
      ))}
      {(placeholdersOf.get('') ?? []).map((p) => (
        <PlaceholderBlock key={p.id} placeholder={p} />
      ))}
    </>
  )
}

function ObjectNode({ object: o, tree }: { object: SceneObject; tree: Tree }) {
  const { models, selected, active, highlighted, helpers } = tree
  const t = useMemo(() => transformToView(o.transform), [o.transform])
  const edge = o.id === active ? activeEdge : selected.has(o.id) ? selectedEdge : highlighted.has(o.id) ? agentEdge : null
  return (
    <group name={o.id} userData={{ objectID: o.id }} position={t.position} quaternion={t.rotation} scale={t.scale}>
      {o.kind === 'group' && (helpers && !tree.childrenOf.get(o.id)?.length ? <EmptyMarker edge={edge} /> : edge && <GroupMarker />)}
      <group visible={o.visible} quaternion={docToView}>
        {o.kind === 'primitive' && o.primitive && (
          <PrimitiveMesh builtin={o.primitive.builtin} params={o.primitive.params} edge={edge} />
        )}
        {o.kind === 'model_instance' && o.model?.content_revision_id && (
          <ModelMesh revision={o.model.content_revision_id} summary={models[o.model.content_revision_id]} edge={edge} />
        )}
        {helpers && o.kind === 'light' && <LightIcon color={o.light?.color ?? '#ffffff'} spot={o.light?.type !== 'point'} edge={edge} />}
        {helpers && o.kind === 'camera' && o.camera && o.id !== tree.through && <CameraFrustum scene={tree.scene} object={o} edge={edge} />}
      </group>
      {(tree.childrenOf.get(o.id) ?? []).map((c) => (
        <ObjectNode key={c.id} object={c} tree={tree} />
      ))}
      {(tree.placeholdersOf.get(o.id) ?? []).map((p) => (
        <PlaceholderBlock key={p.id} placeholder={p} />
      ))}
    </group>
  )
}

// Tinted the assistant's colour: clay on a clay ground would hardly show.
const placeholderMaterial = new MeshStandardMaterial({ color: agentEdge, roughness: 0.92, metalness: 0, transparent: true, opacity: 0.2, depthWrite: false })
const noRaycast = () => {}

/** A model the assistant is still baking: a translucent metre block standing where it will appear; it cannot be picked. */
function PlaceholderBlock({ placeholder }: { placeholder: Placeholder }) {
  const t = useMemo(() => transformToView(placeholder.transform), [placeholder.transform])
  return (
    <group name={`placeholder:${placeholder.id}`} position={t.position} quaternion={t.rotation} scale={t.scale}>
      <group quaternion={docToView}>
        <mesh position={[0, 0.5, 0]} material={placeholderMaterial} raycast={noRaycast}>
          <boxGeometry args={[1, 1, 1]} />
          <Edges color={agentEdge} />
        </mesh>
      </group>
    </group>
  )
}

function PrimitiveMesh({ builtin, params, edge }: { builtin: string; params?: Record<string, unknown>; edge: string | null }) {
  const key = JSON.stringify(params ?? {})
  // eslint-disable-next-line react-hooks/exhaustive-deps -- key stands for params
  const geometry = useMemo(() => primitiveGeometry(builtin, params), [builtin, key])
  useEffect(() => () => geometry?.dispose(), [geometry])
  if (!geometry) return null
  const flat = builtin === 'ground@1' || builtin === 'plane@1'
  return (
    <mesh geometry={geometry} castShadow={!flat} receiveShadow>
      <meshStandardMaterial name="slot:main" color={clay} roughness={0.92} metalness={0} side={flat ? 2 : 0} />
      {/* A hull outline reads on curved shapes; flat ones have no hull. */}
      {edge && (flat ? <Edges threshold={20} lineWidth={2} color={edge} /> : <Outlines thickness={3} color={edge} />)}
    </mesh>
  )
}

function LightIcon({ color, spot, edge }: { color: string; spot: boolean; edge: string | null }) {
  return (
    <group>
      <mesh>
        <sphereGeometry args={[0.15, 16, 8]} />
        <meshBasicMaterial color={edge ?? color} />
      </mesh>
      {spot && (
        <mesh position={[0, 0, -0.35]} rotation={[-Math.PI / 2, 0, 0]}>
          <coneGeometry args={[0.12, 0.4, 12, 1, true]} />
          <meshBasicMaterial color={edge ?? color} wireframe />
        </mesh>
      )}
    </group>
  )
}

/**
 * A frustum outline looking down -Z with the export's field of view, a small
 * cross where it focuses and, with depth of field on, ticks where the sharp
 * range starts and ends.
 */
function CameraFrustum({ scene, object, edge }: { scene: Scene; object: SceneObject; edge: string | null }) {
  const camera = { ...cameraDefaults, ...object.camera }
  const { aspect, focal_mm: focal, sensor_mm: sensor, depth_of_field: dof, f_stop: fStop } = camera
  // Where it focuses depends on where the focus object is: the same answer the export gets.
  const framing = useMemo(() => cameraFraming(scene, object.id), [scene, object.id])
  const focus = 'error' in framing ? camera.focus : (framing.optics?.focusM ?? camera.focus)
  const sensorKey = sensor.join(',')
  const geometry = useMemo(() => {
    const ratio = aspectOf(aspect)
    const length = 0.6
    const halfH = Math.tan((verticalFov(focal, sensorKey.split(',').map(Number), ratio) * Math.PI) / 360) * length
    const halfW = halfH * ratio
    const corners = [
      [-halfW, -halfH, -length],
      [halfW, -halfH, -length],
      [halfW, halfH, -length],
      [-halfW, halfH, -length],
    ]
    const lines: number[] = []
    for (const c of corners) lines.push(0, 0, 0, ...c)
    for (let i = 0; i < 4; i++) lines.push(...corners[i], ...corners[(i + 1) % 4])
    // A small triangle marks the camera's up side.
    lines.push(-halfW * 0.4, halfH * 1.1, -length, 0, halfH * 1.5, -length, 0, halfH * 1.5, -length, halfW * 0.4, halfH * 1.1, -length)
    const g = new BufferGeometry()
    g.setAttribute('position', new Float32BufferAttribute(lines, 3))
    return g
  }, [aspect, focal, sensorKey])
  useEffect(() => () => geometry.dispose(), [geometry])
  const marks = useMemo(() => {
    const cross = 0.08
    const lines = [-cross, 0, -focus, cross, 0, -focus, 0, -cross, -focus, 0, cross, -focus]
    if (dof) {
      const range = sharpRange(focal, fStop, focus, sensorKey.split(',').map(Number))
      for (const d of [range.near, Math.min(range.far, camera.far)]) lines.push(-cross * 1.5, 0, -d, cross * 1.5, 0, -d)
      lines.push(0, 0, -range.near, 0, 0, -Math.min(range.far, camera.far))
    }
    const g = new BufferGeometry()
    g.setAttribute('position', new Float32BufferAttribute(lines, 3))
    return g
  }, [focus, dof, focal, fStop, sensorKey, camera.far])
  useEffect(() => () => marks.dispose(), [marks])
  return (
    <group>
      <lineSegments geometry={geometry}>
        <lineBasicMaterial color={edge ?? '#3b3b3b'} />
      </lineSegments>
      <lineSegments geometry={marks} raycast={noRaycast}>
        <lineBasicMaterial color={edge ?? (dof ? '#c2410c' : '#3b3b3b')} />
      </lineSegments>
      {/* An invisible body so the frustum can be clicked. */}
      <mesh position={[0, 0, -0.3]}>
        <boxGeometry args={[0.3, 0.3, 0.6]} />
        <meshBasicMaterial visible={false} />
      </mesh>
    </group>
  )
}

/** A new code model being baked, translucent at its drop point; it cannot be picked. */
export function GhostModel({ revision, position, models }: { revision: string; position: number[]; models: Readonly<Record<string, SceneModel>> }) {
  const t = useMemo(() => transformToView({ position, rotation: [0, 0, 0, 1], scale: [1, 1, 1] }), [position])
  return (
    <group position={t.position}>
      <group quaternion={docToView}>
        <ModelMesh revision={revision} summary={models[revision]} edge={null} ghost />
      </group>
    </group>
  )
}

/** Groups draw nothing; while selected they show their axes. */
function GroupMarker() {
  return <axesHelper args={[0.5]} />
}

const emptyCross = (() => {
  const g = new BufferGeometry()
  const r = 0.25
  g.setAttribute('position', new Float32BufferAttribute([-r, 0, 0, r, 0, 0, 0, -r, 0, 0, r, 0, 0, 0, -r, 0, 0, r], 3))
  return g
})()

/**
 * A group without children is an empty (空物体): an always-shown cross with
 * a body to click, as Blender's plain axes. With children it is an ordinary
 * group again.
 */
function EmptyMarker({ edge }: { edge: string | null }) {
  return (
    <group>
      <lineSegments geometry={emptyCross}>
        <lineBasicMaterial color={edge ?? '#5b5b5b'} />
      </lineSegments>
      <mesh>
        <sphereGeometry args={[0.15, 8, 6]} />
        <meshBasicMaterial visible={false} />
      </mesh>
    </group>
  )
}
