import { useEffect, useLayoutEffect, useMemo, useRef } from 'react'
import { useThree } from '@react-three/fiber'
import { BoxGeometry, BufferGeometry, EdgesGeometry, Float32BufferAttribute, Ray, Vector3, type Group, type Intersection, type LineSegments, type OrthographicCamera, type PerspectiveCamera, type Raycaster } from 'three'
import type { SceneObject } from '../api.ts'
import { ASIDE_LAYER } from '../render/sceneRenderer.ts'
import type { Volume as Medium } from '../render/volumes/medium.ts'
import { volumeObjects } from '../render/volumes/state.ts'
import { volumeDefaults } from '../state/apply.ts'

// A volume object (W5, data: 体积物件; scene-volumes design「登记与出图」「编辑与
// 拾取」): its medium is drawn by the lit looks' volume pass (render/volumes/),
// not here. Here it registers its document-space group and fields on the
// scene, which the pass reads live, and in the viewport draws its bounds as
// lines on the aside layer, picked within a few screen pixels of a line so a
// volume filling a room does not take the clicks meant for what is in it.

/** How near a line a click picks it, in CSS pixels. */
const PICK_PX = 6

const boxEdges = new EdgesGeometry(new BoxGeometry(1, 1, 1))

/** Three great circles of the unit sphere (diameter 1). */
const sphereRings = (() => {
  const n = 64
  const points: number[] = []
  for (const axis of [0, 1, 2]) {
    for (let i = 0; i < n; i++) {
      for (const k of [i, i + 1]) {
        const a = (k / n) * Math.PI * 2
        const c = 0.5 * Math.cos(a)
        const s = 0.5 * Math.sin(a)
        points.push(...(axis === 0 ? [c, s, 0] : axis === 1 ? [0, c, s] : [c, 0, s]))
      }
    }
  }
  const g = new BufferGeometry()
  g.setAttribute('position', new Float32BufferAttribute(points, 3))
  return g
})()

const a = new Vector3()
const b = new Vector3()
const onRay = new Vector3()
const ray = new Ray()

/** World units a CSS pixel covers at distance t along the ray, for the raycaster's camera. */
function worldPerPixel(raycaster: Raycaster, t: number, heightPx: number): number {
  const camera = raycaster.camera as PerspectiveCamera | OrthographicCamera
  if ((camera as OrthographicCamera).isOrthographicCamera) {
    const o = camera as OrthographicCamera
    return (o.top - o.bottom) / o.zoom / heightPx
  }
  const p = camera as PerspectiveCamera
  return (2 * t * Math.tan((p.fov * Math.PI) / 360)) / p.zoom / heightPx
}

/** The bounds' lines: hit where the ray passes within PICK_PX of one, nearest first. */
function pickLines(this: LineSegments, raycaster: Raycaster, intersects: Intersection[], heightPx: number) {
  const position = this.geometry.getAttribute('position')
  ray.copy(raycaster.ray)
  let best: Intersection | null = null
  for (let i = 0; i + 1 < position.count; i += 2) {
    a.fromBufferAttribute(position, i).applyMatrix4(this.matrixWorld)
    b.fromBufferAttribute(position, i + 1).applyMatrix4(this.matrixWorld)
    const d = Math.sqrt(ray.distanceSqToSegment(a, b, onRay))
    const t = ray.origin.distanceTo(onRay)
    if (t < raycaster.near || t > raycaster.far || d > PICK_PX * worldPerPixel(raycaster, t, heightPx)) continue
    if (!best || t < best.distance) best = { distance: t, point: onRay.clone(), object: this }
  }
  if (best) intersects.push(best)
}

type Props = { id: string; volume: NonNullable<SceneObject['volume']>; rank: number; edge: string | null; helpers: boolean }

export function Volume({ id, volume, rank, edge, helpers }: Props) {
  const { scene, invalidate, size } = useThree()
  const group = useRef<Group>(null)
  const lines = useRef<LineSegments>(null)
  const medium = useMemo<Medium>(() => ({ ...volumeDefaults, ...volume }), [volume])
  useLayoutEffect(() => {
    const object = group.current
    if (!object) return
    const registry = volumeObjects(scene)
    const entry = { object, volume: medium, rank }
    registry.set(id, entry)
    invalidate()
    return () => {
      if (registry.get(id) === entry) registry.delete(id)
      invalidate()
    }
  }, [scene, id, medium, rank, invalidate])
  const height = useRef(size.height)
  height.current = size.height
  useEffect(() => {
    const l = lines.current
    if (!l) return
    l.raycast = function (raycaster, intersects) {
      pickLines.call(this, raycaster, intersects, height.current)
    }
  })
  return (
    <group ref={group}>
      {helpers && (
        <lineSegments ref={lines} layers={ASIDE_LAYER} geometry={medium.shape === 'sphere' ? sphereRings : boxEdges}>
          <lineBasicMaterial color={edge ?? '#7a8796'} />
        </lineSegments>
      )}
    </group>
  )
}
