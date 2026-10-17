import { Mesh, type BufferGeometry, type Matrix4, type Object3D } from 'three'
import { createStore, type StoreApi } from 'zustand/vanilla'
import type { components } from '../../api/schema'
import { mediaTicket } from '../../creative-canvas/editor/media.ts'
import { parseGLB } from '../sandbox/gltf.ts'
import { inspect } from '../sandbox/inspect.ts'
import { parsePlaceholder } from '../state/apply.ts'
import type { SceneSession } from '../state/sync.ts'

// Model GLBs by content revision: one ticket, one download, one inspection
// and one parse each. Instances draw the cached geometries, so a model used
// a hundred times is in memory once.

export type SceneModel = components['schemas']['CreativeSceneModel']

/** What a model draws: shared geometries, each with its transform inside the model. */
export type ModelParts = { meshes: { geometry: BufferGeometry; matrix: Matrix4 }[] }

/**
 * A model baked in this editor. It is drawn from these bytes under its
 * placeholder until the receipt, and under its real revision after it,
 * until a read brings the server's summary.
 */
export type LocalModel = { glb: ArrayBuffer; summary: SceneModel }

export type Download = (revision: string) => Promise<ArrayBuffer>

async function downloadGLB(revision: string): Promise<ArrayBuffer> {
  const ticket = await mediaTicket(revision, 'original', 'display')
  const response = await fetch(ticket.url, { credentials: 'same-origin' })
  if (!response.ok) throw new Error(`模型加载失败（${response.status}）`)
  return response.arrayBuffer()
}

function partsOf(root: Object3D): ModelParts {
  root.updateMatrixWorld(true)
  const meshes: ModelParts['meshes'] = []
  root.traverse((o) => {
    if (o instanceof Mesh) meshes.push({ geometry: o.geometry, matrix: o.matrixWorld.clone() })
  })
  return { meshes }
}

export class ModelCache {
  readonly local: StoreApi<Readonly<Record<string, LocalModel>>> = createStore(() => ({}))
  /** Revisions whose GLB could not be loaded, for the properties panel. */
  readonly failed: StoreApi<Readonly<Record<string, true>>> = createStore(() => ({}))
  private readonly download: Download
  private readonly loads = new Map<string, Promise<ModelParts>>()

  constructor(download: Download = downloadGLB) {
    this.download = download
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
  }

  /** The receipt named a placeholder's model: keep its bytes and parse under the real IDs. */
  alias(from: string, revision: string, contentID: string) {
    const model = this.local.getState()[from]
    if (!model) return
    this.local.setState({ ...this.local.getState(), [revision]: { ...model, summary: { ...model.summary, content_id: contentID } } })
    const loading = this.loads.get(from)
    if (loading && !this.loads.has(revision)) this.loads.set(revision, loading)
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
      () => {
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

  /** Drops everything; the editor closed. */
  clear() {
    const parts = [...this.loads.values()]
    this.loads.clear()
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
