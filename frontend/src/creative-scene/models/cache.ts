import { Mesh, type BufferGeometry, type Material, type Object3D } from 'three'
import { createStore, type StoreApi } from 'zustand/vanilla'
import type { components } from '../../api/schema'
import { readModelGenerator, type ModelGenerator } from '../api.ts'
import { mediaTicket } from '../../creative-canvas/editor/media.ts'
import { parseModelGLB, type MeshIdentity } from '../sandbox/gltf.ts'
import { inspect } from '../sandbox/inspect.ts'
import { parsePlaceholder } from '../state/apply.ts'
import { disposePieceViews } from '../render/pieces.ts'
import { mergeParts, noMerge } from './merge.ts'
import { applyFaces, fingerprints, fingerprintsNow, flipWinding, namedIn, ownedMeshes, slotOfMaterial, type Faces, type OwnedMesh } from './faces.ts'
import type { SceneSession } from '../state/sync.ts'

// Model GLBs by content revision: one ticket, one download, one inspection
// and one parse each. Instances draw the cached geometries, so a model used
// a hundred times is in memory once.

export type SceneModel = components['schemas']['CreativeSceneModel']

/** What the module's own material held, for a slot nothing else decides (materials/resolve.ts). */
export type MeshConstants = {
  color: string
  roughness: number
  metalness: number
  emissive: string
  emissiveIntensity: number
  vertexColors: boolean
}

/**
 * One drawable piece of a model, in model space: its part's transform is
 * baked into the geometry so every piece shares one space and procedural
 * materials run on across part seams. `slot` is the material's slot (its
 * name `slot:<id>`), null for a material the code made itself.
 */
export type ModelMeshPart = {
  geometry: BufferGeometry
  slot: string | null
  hint: string | null
  constants: MeshConstants
  /** For triangles a face assignment moved: the GLB slot they came from, drawn until their own slot has a material (materials/resolve.ts resolvePart). */
  origin?: { slot: string | null; hint: string | null }
}

/**
 * What a model draws: shared geometries in model space, parts that draw alike
 * merged (models/merge.ts). stale counts face assignment entries that no
 * longer fit their mesh and were not applied (models/faces.ts).
 */
export type ModelParts = { meshes: ModelMeshPart[]; stale?: number }

/**
 * A model baked in this editor. It is drawn from these bytes under its
 * placeholder until the receipt, and under its real revision after it,
 * until a read brings the server's summary.
 */
export type LocalModel = { glb: ArrayBuffer; summary: SceneModel; generator?: ModelGenerator }

export type Download = (revision: string) => Promise<ArrayBuffer>
export type ReadGenerator = (revision: string) => Promise<ModelGenerator>

async function downloadGLB(revision: string): Promise<ArrayBuffer> {
  const ticket = await mediaTicket(revision, 'original', 'display')
  const response = await fetch(ticket.url, { credentials: 'same-origin' })
  if (!response.ok) throw new Error(`模型加载失败（${response.status}）`)
  return response.arrayBuffer()
}

type StandardLike = Material & {
  color?: { getHexString(): string }
  roughness?: number
  metalness?: number
  emissive?: { getHexString(): string }
  emissiveIntensity?: number
  vertexColors?: boolean
}

function constantsOf(m: StandardLike): MeshConstants {
  return {
    color: `#${m.color?.getHexString() ?? 'cccccc'}`,
    roughness: m.roughness ?? 1,
    metalness: m.metalness ?? 0,
    emissive: `#${m.emissive?.getHexString() ?? '000000'}`,
    emissiveIntensity: m.emissiveIntensity ?? 1,
    vertexColors: !!m.vertexColors,
  }
}

/**
 * The model's parts, split by its face assignment when it has one. Without
 * one the loaded geometries take their transforms in place, as before face
 * assignments; with one each mesh is copied first (models/faces.ts
 * ownedMeshes), so pieces never share arrays a transform could hit twice.
 * topologies: the fingerprints of the meshes the assignment names, when
 * taken already (load takes them by the browser's own SHA-256).
 */
export function partsOf(root: Object3D, identities?: ReadonlyMap<Mesh, MeshIdentity>, faces?: Faces | null, topologies?: ReadonlyMap<Mesh, string>): ModelParts {
  if (faces && identities) {
    const { pieces, stale } = applyFaces(ownedMeshes(root, identities, topologies ?? fingerprintsNow(identities, namedIn(faces))), faces)
    const meshes = pieces.map((p): ModelMeshPart => {
      const material = p.mesh.material as StandardLike
      const own = typeof material.userData.hint === 'string' ? material.userData.hint : null
      const part: ModelMeshPart = { geometry: p.geometry, slot: p.slot, hint: own, constants: constantsOf(material) }
      if (!p.assigned) return part
      return { ...part, hint: p.slot !== null ? (faces.hints?.[p.slot] ?? null) : null, origin: { slot: slotOfMaterial(material), hint: own } }
    })
    return { meshes: mergeParts(meshes), stale: stale.length }
  }
  root.updateMatrixWorld(true)
  const found: Mesh[] = []
  root.traverse((o) => {
    if (o instanceof Mesh) found.push(o)
  })
  const uses = new Map<BufferGeometry, number>()
  for (const o of found) uses.set(o.geometry, (uses.get(o.geometry) ?? 0) + 1)
  const meshes = found.map((o): ModelMeshPart => {
    // A geometry several parts share is copied before its transform goes in.
    const geometry = (uses.get(o.geometry) ?? 0) > 1 ? o.geometry.clone() : o.geometry
    geometry.applyMatrix4(o.matrixWorld)
    if (o.matrixWorld.determinant() < 0) flipWinding(geometry)
    const material = (Array.isArray(o.material) ? o.material[0] : o.material) as StandardLike
    const slot = /^slot:(.+)$/.exec(material.name)?.[1] ?? null
    const hint = typeof material.userData.hint === 'string' ? material.userData.hint : null
    return { geometry, slot, hint, constants: constantsOf(material) }
  })
  return { meshes: mergeParts(meshes) }
}

export class ModelCache {
  readonly local: StoreApi<Readonly<Record<string, LocalModel>>> = createStore(() => ({}))
  /**
   * Summaries of models placed from elsewhere (an asset), until the scene's
   * next read brings the server's. Kept apart from local: those have bytes
   * load() draws from, these only say what the model is.
   */
  readonly known: StoreApi<Readonly<Record<string, SceneModel>>> = createStore(() => ({}))
  /** Revisions whose GLB could not be loaded, for the properties panel. */
  readonly failed: StoreApi<Readonly<Record<string, true>>> = createStore(() => ({}))
  private readonly download: Download
  private readonly readGenerator: ReadGenerator
  private readonly loads = new Map<string, Promise<ModelParts>>()
  private readonly ready = new Map<string, ModelParts>()
  private readonly generators = new Map<string, Promise<ModelGenerator>>()
  private owning: { revision: string; glb: Promise<ArrayBuffer>; meshes: Promise<OwnedMesh[]> } | null = null

  constructor(download: Download = downloadGLB, readGenerator: ReadGenerator = (r) => readModelGenerator(r)) {
    this.download = download
    this.readGenerator = readGenerator
  }

  /** How a revision was made, code included: from a local bake, or read once from the server. */
  generator(revision: string): Promise<ModelGenerator> {
    const local = this.local.getState()[revision]?.generator
    if (local) return Promise.resolve(local)
    const hit = this.generators.get(revision)
    if (hit) return hit
    const promise = parsePlaceholder(revision) ? Promise.reject(new Error('本机模型已不在，请重新生成')) : this.readGenerator(revision)
    promise.catch(() => {
      if (this.generators.get(revision) === promise) this.generators.delete(revision)
    })
    this.generators.set(revision, promise)
    return promise
  }

  /** What a placed asset's model is, shown until the server's summary arrives. */
  remember(revision: string, summary: SceneModel) {
    this.known.setState({ ...this.known.getState(), [revision]: summary })
  }

  /** The server has summaries of these now. */
  settle(revisions: readonly string[]) {
    const known = this.known.getState()
    if (!revisions.some((r) => r in known)) return
    const rest = { ...known }
    for (const r of revisions) delete rest[r]
    this.known.setState(rest, true)
  }

  /** Registers a freshly baked model under its placeholder revision. */
  register(revision: string, model: LocalModel) {
    this.local.setState({ ...this.local.getState(), [revision]: model })
  }

  /** Forgets a local model whose edit was dropped before it was saved. */
  unregister(revision: string) {
    const { [revision]: gone, ...rest } = this.local.getState()
    if (!gone) return
    this.local.setState(rest, true)
    this.loads.delete(revision)
    this.ready.delete(revision)
  }

  /** The receipt named a placeholder's model: keep its bytes and parse under the real IDs. */
  alias(from: string, revision: string, contentID: string) {
    const model = this.local.getState()[from]
    if (!model) return
    this.local.setState({ ...this.local.getState(), [revision]: { ...model, summary: { ...model.summary, content_id: contentID } } })
    const loading = this.loads.get(from)
    if (loading && !this.loads.has(revision)) this.loads.set(revision, loading)
    const parts = this.ready.get(from)
    if (parts && !this.ready.has(revision)) this.ready.set(revision, parts)
    // Saved: cached from now on under its revision.
    if (parts) traceKeys(parts, revision)
    else void loading?.then((p) => traceKeys(p, revision), () => undefined)
  }

  /** Follows a session's receipts; returns the disconnect. */
  follow(session: SceneSession): () => void {
    return session.observe({
      confirmed: (intent, receipt) => {
        for (const [ref, m] of Object.entries(receipt.models ?? {}))
          this.alias(`local:${intent.id}:${ref}`, m.content_revision_id, m.content_id)
      },
    })
  }

  /**
   * The model's parts. Bytes from the server pass the same inspection as a
   * fresh bake before GLTFLoader sees them; a failed load is not kept, so a
   * later attempt tries again.
   */
  load(revision: string, faces: Faces | null | undefined): Promise<ModelParts> {
    const hit = this.loads.get(revision)
    if (hit) return hit
    const promise = (async () => {
      const local = this.local.getState()[revision]
      if (!local && parsePlaceholder(revision)) throw new Error('本机模型已不在，请重新生成')
      const glb = local ? local.glb : await this.download(revision)
      inspect(glb)
      const { scene, identities } = await parseModelGLB(glb)
      const parts = partsOf(scene, identities, faces, faces ? await fingerprints(identities, namedIn(faces)) : undefined)
      if (!parsePlaceholder(revision)) traceKeys(parts, revision)
      return parts
    })()
    promise.then(
      (parts) => {
        if (this.loads.get(revision) === promise) this.ready.set(revision, parts)
        const { [revision]: was, ...rest } = this.failed.getState()
        if (was) this.failed.setState(rest, true)
      },
      () => {
        if (this.loads.get(revision) === promise) this.loads.delete(revision)
        this.failed.setState({ ...this.failed.getState(), [revision]: true })
      },
    )
    this.loads.set(revision, promise)
    return promise
  }

  /**
   * Every mesh of a revision with its identity and fingerprint, in arrays of
   * its own, for face edit: parsed apart from the drawn parts, which loading
   * split and merged. One revision is kept, the one being edited.
   */
  owned(revision: string): Promise<OwnedMesh[]> {
    if (this.owning?.revision === revision) return this.owning.meshes
    const glb = this.bytes(revision)
    const meshes = (async () => {
      const bytes = await glb
      inspect(bytes)
      const { scene, identities } = await parseModelGLB(bytes)
      return ownedMeshes(scene, identities, await fingerprints(identities))
    })()
    meshes.catch(() => {
      if (this.owning?.meshes === meshes) this.owning = null
    })
    this.owning = { revision, glb, meshes }
    return meshes
  }

  /**
   * A revision's GLB: a local bake's, the one face edit read, or downloaded.
   * A face assignment derives a revision over the same bytes, drawn from
   * them until the server's copy is read.
   */
  bytes(revision: string): Promise<ArrayBuffer> {
    const local = this.local.getState()[revision]
    if (local) return Promise.resolve(local.glb)
    if (this.owning?.revision === revision) return this.owning.glb
    if (parsePlaceholder(revision)) return Promise.reject(new Error('本机模型已不在，请重新生成'))
    return this.download(revision)
  }

  /**
   * A model already loaded, without waiting, so a first frame can draw it:
   * its parts, null when its last load failed, undefined when not loaded yet.
   */
  peek(revision: string): ModelParts | null | undefined {
    const parts = this.ready.get(revision)
    if (parts) return parts
    return this.failed.getState()[revision] ? null : undefined
  }

  /** Drops one model and frees its geometries; nothing may be drawing it. */
  forget(revision: string) {
    const loading = this.loads.get(revision)
    this.loads.delete(revision)
    this.ready.delete(revision)
    if (loading) disposeParts([loading])
  }

  /** Drops everything; the editor closed. */
  clear() {
    const parts = [...this.loads.values()]
    this.loads.clear()
    this.ready.clear()
    // Face edit's copy: the GLB, the meshes and their BVHs.
    this.owning = null
    this.local.setState({}, true)
    this.known.setState({}, true)
    disposeParts(parts)
  }
}

/**
 * Names each merged geometry for the world-space layer's cache
 * (render/trace): model:<revision>#<its place after the merge>. A local
 * bake not saved yet has no name and is built anew each session; nor does
 * the e2e's unmerged drawing, whose geometries are other ones under the
 * same places.
 */
function traceKeys(parts: ModelParts, revision: string) {
  if (noMerge()) return
  parts.meshes.forEach((m, i) => (m.geometry.userData.traceKey = `model:${revision}#${i}`))
}

/** Frees each geometry once, as its load settles: its range views first, which share its buffers. */
function disposeParts(parts: Promise<ModelParts>[]) {
  const seen = new Set<BufferGeometry>()
  for (const p of parts)
    void p.then(
      ({ meshes }) => {
        for (const m of meshes) {
          if (seen.has(m.geometry)) continue
          seen.add(m.geometry)
          disposePieceViews(m.geometry)
          m.geometry.dispose()
        }
      },
      () => {},
    )
}

export const modelCache = new ModelCache()

/**
 * What the editor knows about the models a scene names: the server's
 * summaries win, then local bakes, then what placed assets said.
 */
export function modelSummaries(
  server: Readonly<Record<string, SceneModel>> | undefined,
  local: Readonly<Record<string, LocalModel>>,
  known: Readonly<Record<string, SceneModel>> = {},
): Record<string, SceneModel> {
  const out: Record<string, SceneModel> = { ...known }
  for (const [id, m] of Object.entries(local)) out[id] = m.summary
  return { ...out, ...server }
}
