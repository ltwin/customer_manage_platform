import type { BufferAttribute, BufferGeometry, InterleavedBufferAttribute } from 'three'
import { mergeGeometries } from 'three/addons/utils/BufferGeometryUtils.js'
import { setPieces, type Piece } from '../render/pieces.ts'
import type { ModelMeshPart } from './cache.ts'

// A model's parts merged by what they draw with (scene-performance「合并
// part」): three's WebGPU renderer spends about 3 µs of CPU a draw, and a
// code-built model is often hundreds of parts, so parts of one slot, hint and
// constants become one geometry and one draw. Each part stays a piece of it
// (render/pieces.ts) for whatever must look or act as before the merge.

/** e2e only: the fixture page's ?nomerge=1, to draw a scene as before the merge beside it. */
export const noMerge = () => (globalThis as { __sceneNoMerge?: boolean }).__sceneNoMerge === true

function attributeKey(name: string, a: BufferAttribute | InterleavedBufferAttribute): string {
  return `${name}:${a.itemSize}:${a.normalized}:${a.array.constructor.name}:${(a as BufferAttribute).gpuType}`
}

/** What mergeGeometries needs alike (three r186): the attributes, the morph attributes, how morphs add up, an index or none. */
function layoutKey(g: BufferGeometry): string {
  const attributes = Object.keys(g.attributes)
    .sort()
    .map((n) => attributeKey(n, g.attributes[n]))
  const morphs = Object.entries(g.morphAttributes as Record<string, (BufferAttribute | InterleavedBufferAttribute)[]>)
    .sort(([a], [b]) => (a < b ? -1 : 1))
    .map(([n, list]) => `${n}[${list.length}]${list.map((a) => attributeKey(n, a)).join(',')}`)
  return `${attributes.join('|')}#${morphs.join('|')}#${g.morphTargetsRelative}#${g.index !== null}`
}

/** A change in how parts group renumbers the merged geometries: bump render/trace/cache.ts BUILDER_VERSION with it, or the local cache serves trees of other geometries. */
function groupKey(p: ModelMeshPart): string {
  const c = p.constants
  return JSON.stringify([p.slot, p.hint, c.color, c.roughness, c.metalness, c.emissive, c.emissiveIntensity, c.vertexColors, layoutKey(p.geometry)])
}

type Merge = (geometries: BufferGeometry[]) => BufferGeometry | null

const merge3: Merge = (geometries) => mergeGeometries(geometries, false) as BufferGeometry | null

/**
 * Parts that draw alike merged into one, in the order each group first
 * appears, its parts in their order. A group of one keeps its geometry; a
 * group three cannot merge after all (merge gives null) stays as its parts.
 */
export function mergeParts(parts: readonly ModelMeshPart[], merge: Merge = merge3): ModelMeshPart[] {
  if (noMerge()) return [...parts]
  const groups = new Map<string, ModelMeshPart[]>()
  for (const p of parts) {
    const key = groupKey(p)
    const group = groups.get(key)
    if (group) group.push(p)
    else groups.set(key, [p])
  }
  const out: ModelMeshPart[] = []
  for (const group of groups.values()) {
    if (group.length === 1) {
      out.push(group[0])
      continue
    }
    const pieces: Piece[] = []
    let start = 0
    for (const { geometry } of group) {
      // three's own bounds: the transparent sort goes by the sphere's centre.
      geometry.computeBoundingSphere()
      geometry.computeBoundingBox()
      const count = geometry.index ? geometry.index.count : geometry.attributes.position.count
      pieces.push({ start, count, sphere: geometry.boundingSphere!, box: geometry.boundingBox! })
      start += count
    }
    const merged = merge(group.map((p) => p.geometry))
    if (!merged) {
      out.push(...group)
      continue
    }
    setPieces(merged, pieces)
    for (const p of group) p.geometry.dispose()
    out.push({ ...group[0], geometry: merged })
  }
  return out
}
