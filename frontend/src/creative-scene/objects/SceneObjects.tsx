import { useEffect, useMemo } from 'react'
import { Edges, Outlines } from '@react-three/drei'
import { BufferGeometry, Float32BufferAttribute } from 'three'
import type { Scene, SceneObject } from '../api.ts'
import { primitiveGeometry } from '../builtins/geometry.ts'
import { docToView, transformToView } from '../space.ts'

// Draws the scene tree. Every object is a group named by its ID (picking and
// transforms find it by name); hiding an object hides only its own drawing,
// not its children, as in Blender. The viewport is always clay in C1.
//
// Object groups carry view-space transforms (Z up, see space.ts), so moving,
// rotating and the object's own axes read as in Blender. What an object
// draws is authored in document space (Y up) and turns once inside it.

const clay = '#d9d5cd'
// Blender's selection colours: the active object lighter than the rest.
const selectedEdge = '#ed5700'
const activeEdge = '#ffaf29'

type Props = {
  scene: Scene
  selected: ReadonlySet<string>
  active: string | null
}

export function SceneObjects({ scene, selected, active }: Props) {
  const children = useMemo(() => {
    const byParent = new Map<string, SceneObject[]>()
    for (const o of scene.objects) {
      const key = o.parent_id ?? ''
      const list = byParent.get(key) ?? []
      list.push(o)
      byParent.set(key, list)
    }
    for (const list of byParent.values()) list.sort((a, b) => a.order - b.order)
    return byParent
  }, [scene.objects])
  return (
    <>
      {(children.get('') ?? []).map((o) => (
        <ObjectNode key={o.id} object={o} childrenOf={children} selected={selected} active={active} />
      ))}
    </>
  )
}

function ObjectNode({
  object: o,
  childrenOf,
  selected,
  active,
}: {
  object: SceneObject
  childrenOf: Map<string, SceneObject[]>
  selected: ReadonlySet<string>
  active: string | null
}) {
  const t = useMemo(() => transformToView(o.transform), [o.transform])
  const edge = o.id === active ? activeEdge : selected.has(o.id) ? selectedEdge : null
  return (
    <group name={o.id} userData={{ objectID: o.id }} position={t.position} quaternion={t.rotation} scale={t.scale}>
      {o.kind === 'group' && edge && <GroupMarker />}
      <group visible={o.visible} quaternion={docToView}>
        {o.kind === 'primitive' && o.primitive && (
          <PrimitiveMesh builtin={o.primitive.builtin} params={o.primitive.params} edge={edge} />
        )}
        {o.kind === 'light' && <LightIcon color={o.light?.color ?? '#ffffff'} spot={o.light?.type !== 'point'} edge={edge} />}
        {o.kind === 'camera' && o.camera && <CameraFrustum aspect={o.camera.aspect} focal={o.camera.focal_mm} sensor={o.camera.sensor_mm} edge={edge} />}
      </group>
      {(childrenOf.get(o.id) ?? []).map((c) => (
        <ObjectNode key={c.id} object={c} childrenOf={childrenOf} selected={selected} active={active} />
      ))}
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

/** A frustum outline looking down -Z, sized by focal length and aspect. */
function CameraFrustum({ aspect, focal, sensor, edge }: { aspect: string; focal: number; sensor: number[]; edge: string | null }) {
  const geometry = useMemo(() => {
    const [w, h] = aspect.split(':').map(Number)
    const length = 0.6
    const halfH = ((sensor[1] ?? 24) / 2 / focal) * length
    const halfW = halfH * ((w || 3) / (h || 2))
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
  }, [aspect, focal, sensor])
  useEffect(() => () => geometry.dispose(), [geometry])
  return (
    <group>
      <lineSegments geometry={geometry}>
        <lineBasicMaterial color={edge ?? '#3b3b3b'} />
      </lineSegments>
      {/* An invisible body so the frustum can be clicked. */}
      <mesh position={[0, 0, -0.3]}>
        <boxGeometry args={[0.3, 0.3, 0.6]} />
        <meshBasicMaterial visible={false} />
      </mesh>
    </group>
  )
}

/** Groups draw nothing; while selected they show their axes. */
function GroupMarker() {
  return <axesHelper args={[0.5]} />
}
