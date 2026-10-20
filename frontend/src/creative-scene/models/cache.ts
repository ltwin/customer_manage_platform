import { Mesh, type BufferAttribute, type BufferGeometry, type Material, type Object3D } from 'three'
import { createStore, type StoreApi } from 'zustand/vanilla'
import type { components } from '../../api/schema'
import { readModelGenerator, type ModelGenerator } from '../api.ts'
import { mediaTicket } from '../../creative-canvas/editor/media.ts'
import { parseGLB } from '../sandbox/gltf.ts'
import { inspect } from '../sandbox/inspect.ts'
import { parsePlaceholder } from '../state/apply.ts'
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
export type ModelMeshPart = { geometry: BufferGeometry; slot: string | null; hint: string | null; constants: MeshConstants }

/** What a model draws: shared geometries in model space. */
export type ModelParts = { meshes: ModelMeshPart[] }

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
 * Swaps the second and third corner of every triangle. A mirrored part's
 * transform turned its faces inside out; three.js would flip them back while
 * the transform was the object's, but baked into the geometry it is not.
 */
export function flipWinding(geometry: BufferGeometry) {
  const index = geometry.index
  if (index) {
    for (let i = 0; i + 2 < index.count; i += 3) {
      const b = index.getX(i + 1)
      index.setX(i + 1, index.getX(i + 2))
      index.setX(i + 2, b)
    }
    index.needsUpdate = true
    return
  }
  for (const attribute of Object.values(geometry.attributes) as BufferAttribute[]) {
    const n = attribute.itemSize
    const a = attribute.array
    for (let i = 0; i + 2 < attribute.count; i += 3)
      for (let k = 0; k < n; k++) {
        const x = a[(i + 1) * n + k]
        a[(i + 1) * n + k] = a[(i + 2) * n + k]
        a[(i + 2) * n + k] = x
      }
    attribute.needsUpdate = true
  }
}

export function partsOf(root: Object3D): ModelParts {
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
  return { meshes }
}

export class ModelCache {
  readonly local: StoreApi<Readonly<Record<string, LocalModel>>> = createStore(() => ({}))
  /** Revisions whose GLB could not be loaded, for the properties panel. */
  readonly failed: StoreApi<Readonly<Record<string, true>>> = createStore(() => ({}))
  private readonly download: Download
  private readonly readGenerator: ReadGenerator
  private readonly loads = new Map<string, Promise<ModelParts>>()
  private readonly ready = new Map<string, ModelParts>()
  private readonly generators = new Map<string, Promise<ModelGenerator>>()

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
  load(revision: string): Promise<ModelParts> {
    const hit = this.loads.get(revision)
    if (hit) return hit
    const promise = (async () => {
      const local = this.local.getState()[revision]
      if (!local && parsePlaceholder(revision)) throw new Error('本机模型已不在，请重新生成')
      const glb = local ? local.glb : await this.download(revision)
      inspect(glb)
      return partsOf(await parseGLB(glb))
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
   * A model already loaded, without waiting, so a first frame can draw it:
   * its parts, null when its last load failed, undefined when not loaded yet.
   */
  peek(revision: string): ModelParts | null | undefined {
    const parts = this.ready.get(revision)
    if (parts) return parts
    return this.failed.getState()[revision] ? null : undefined
  }

  /** Drops everything; the editor closed. */
  clear() {
    const parts = [...this.loads.values()]
    this.loads.clear()
    this.ready.clear()
    this.local.setState({}, true)
    const seen = new Set<BufferGeometry>()
    for (const p of parts)
      void p.then(
        ({ meshes }) => {
          for (const m of meshes) {
            if (seen.has(m.geometry)) continue
            seen.add(m.geometry)
            m.geometry.dispose()
          }
        },
        () => {},
      )
  }
}

export const modelCache = new ModelCache()

/**
 * What the editor knows about the models a scene names: the server's
 * summaries win, local bakes fill in until a read brings them.
 */
export function modelSummaries(
  server: Readonly<Record<string, SceneModel>> | undefined,
  local: Readonly<Record<string, LocalModel>>,
): Record<string, SceneModel> {
  const out: Record<string, SceneModel> = {}
  for (const [id, m] of Object.entries(local)) out[id] = m.summary
  return { ...out, ...server }
}
