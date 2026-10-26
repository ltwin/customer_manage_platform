import { Color, DoubleSide, type BufferGeometry, type Light, type Material, type Mesh, type Object3D } from 'three'
import { isSeeThrough } from '../../materials/instances.ts'
import type { SlotMaterial } from '../../materials/resolve.ts'

// What the world-space layer traces, read off the mounted three scene each
// frame (S1 design「收集与比对」, L4): the meshes the opaque prepass draws,
// as the scene objects hold them, and the lights. Nothing here reads the
// scene document or keeps anything: the base compares a frame's with the
// last (base.ts sync, instances).

/** What a root of a traced mesh is drawn with. */
export type Slot = {
  material: SlotMaterial
  /** A one-sided material's back faces are culled by the raster: a ray passes them (D8). */
  doubleSided: boolean
}

/** A mesh the layer traces. */
export type Traced = {
  mesh: Mesh
  /** The scene object it belongs to (its group's userData.objectID). */
  objectID: string
  /** Its place among that object's traced meshes, in the scene graph's order. */
  part: number
  geometry: BufferGeometry
  /**
   * What is built of the geometry: its traceKey, or its uuid for this session
   * alone, marked +groups when the mesh draws a material a group (a root a
   * group; one material draws it as one root).
   */
  key: string
  /** Whether a build of it may go into the local cache: only a geometry with a traceKey. */
  cacheable: boolean
  /** Whether the mesh draws a material a geometry group. */
  grouped: boolean
  /** A group's each when the mesh draws a material a geometry group (a root finds its own by build.worker.ts rootGroups); else the one. */
  slots: Slot[]
  triangles: number
}

/** The material a mesh draws its slot with (materials/instances.ts slotOf): none for clay, ghosts, placeholders and helpers. */
export type SlotOf = (material: Material) => SlotMaterial | undefined

/** The whole geometry is drawn, not a range of it (render/pieces.ts range views, which only see-through slots and ghosts draw). */
function drawnWhole(g: BufferGeometry): boolean {
  const count = g.index ? g.index.count : g.attributes.position.count
  return g.drawRange.start === 0 && g.drawRange.count >= count
}

/**
 * What a mesh's roots are drawn with: a material a group, the one its
 * index picks; else the mesh's one material. null when any is not traced:
 * no slot material (clay, ghosts, placeholders, helpers) or a see-through one.
 */
function slotsOf(mesh: Mesh, slotOf: SlotOf): Slot[] | null {
  const pick = (m: Material | undefined): Slot | null => {
    const s = m && slotOf(m)
    return s && !isSeeThrough(s) ? { material: s, doubleSided: m.side === DoubleSide } : null
  }
  if (!Array.isArray(mesh.material)) {
    const one = pick(mesh.material)
    return one && [one]
  }
  const materials = mesh.material
  const groups = mesh.geometry?.groups ?? []
  if (groups.length === 0) return null
  const slots = groups.map((g) => pick(materials[g.materialIndex ?? 0]))
  return slots.every((s) => s !== null) ? (slots as Slot[]) : null
}

const trianglesOf = (g: BufferGeometry) => Math.floor((g.index ? g.index.count : g.attributes.position.count) / 3)

/**
 * The meshes to trace, as the opaque prepass draws them: visible all the way
 * up, on layer 0 (helpers, the sky and the volumes' outlines are not), with
 * an opaque, non-transmissive slot material, drawing a whole geometry; and
 * belonging to a scene object (placeholders do not). In the order of their
 * scene object's ID, then their place in it (L3).
 */
export function collectMeshes(scene: Object3D, slotOf: SlotOf): Traced[] {
  const out: Traced[] = []
  const parts = new Map<string, number>()
  const visit = (o: Object3D, objectID: string | null) => {
    if (!o.visible) return
    const id = typeof o.userData.objectID === 'string' ? o.userData.objectID : objectID
    const mesh = o as Mesh
    if (id !== null && mesh.isMesh && !(mesh as { isInstancedMesh?: boolean }).isInstancedMesh && !(mesh as { isBatchedMesh?: boolean }).isBatchedMesh && o.layers.isEnabled(0)) {
      const slots = slotsOf(mesh, slotOf)
      const g = mesh.geometry
      if (slots && g?.attributes.position && drawnWhole(g)) {
        const part = parts.get(id) ?? 0
        parts.set(id, part + 1)
        const traceKey = g.userData.traceKey
        const grouped = Array.isArray(mesh.material)
        out.push({
          mesh,
          objectID: id,
          part,
          geometry: g,
          key: `${typeof traceKey === 'string' ? traceKey : `uuid:${g.uuid}`}${grouped ? '+groups' : ''}`,
          cacheable: typeof traceKey === 'string',
          grouped,
          slots,
          triangles: trianglesOf(g),
        })
      }
    }
    for (const c of o.children) visit(c, id)
  }
  visit(scene, null)
  return out.sort((a, b) => (a.objectID < b.objectID ? -1 : a.objectID > b.objectID ? 1 : a.part - b.part))
}

/** The lights a hit point is lit by. */
export type Lighting = {
  /** Directional, point and spot lights that give light, in the scene graph's order. */
  lights: Light[]
  /** The ambient lights added up, colour times intensity. */
  ambient: Color
}

/**
 * The scene's lights as the lit materials see them: visible, giving light,
 * and not W5's stand-in (userData.volumesCast, an ambient light of
 * intensity 0 that only switches the volumes' shadows on). Intensities as
 * applyExposure left them.
 */
export function collectLights(scene: Object3D): Lighting {
  const lights: Light[] = []
  const ambient = new Color(0, 0, 0)
  const visit = (o: Object3D) => {
    if (!o.visible) return
    const l = o as Light & { isAmbientLight?: boolean; isDirectionalLight?: boolean; isPointLight?: boolean; isSpotLight?: boolean }
    if (l.isLight && l.intensity > 0 && !o.userData.volumesCast) {
      if (l.isAmbientLight) ambient.add(l.color.clone().multiplyScalar(l.intensity))
      else if (l.isDirectionalLight || l.isPointLight || l.isSpotLight) lights.push(l)
    }
    for (const c of o.children) visit(c)
  }
  visit(scene)
  return { lights, ambient }
}
