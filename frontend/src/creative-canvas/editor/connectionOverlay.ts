import type { Canvas, GraphAction } from './api.ts'
import type { Job } from './journal.ts'

type Connect = Extract<GraphAction, { type: 'connect_reference' }>
export type PendingConnection = Connect & { id: string }
type Projection = {
  path: string
  edges: PendingConnection[]
  confirmedRevision?: string
}
const record = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === 'object' && !Array.isArray(v)

// Visual projection only: the journal remains the sole owner of dispatch/recovery.
export class ConnectionOverlay {
  private operations = new Map<string, Projection>()

  stage(job: Job) {
    if (job.state === 'rejected' || this.operations.has(job.operation)) return
    let body: unknown
    try {
      body = JSON.parse(job.body)
    } catch {
      return
    }
    if (!record(body) || !record(body.payload)) return
    const payload = body.payload
    if (
      payload.type !== 'batch' ||
      !Array.isArray(payload.actions) ||
      !payload.actions.length
    )
      return
    const edges: PendingConnection[] = []
    for (const [index, action] of payload.actions.entries()) {
      // Mixed commands may create nodes; they have no local node projection yet.
      if (
        !record(action) ||
        action.type !== 'connect_reference' ||
        typeof action.source_node_id !== 'string' ||
        typeof action.target_node_id !== 'string' ||
        action.source_port !== 'output' ||
        action.target_port !== 'reference' ||
        action.role !== 'reference'
      )
        return
      edges.push({
        type: 'connect_reference',
        id: `pending:${job.operation}:${index}`,
        source_node_id: action.source_node_id,
        target_node_id: action.target_node_id,
        source_port: 'output',
        target_port: 'reference',
        role: 'reference',
      })
    }
    this.operations.set(job.operation, { path: job.path, edges })
  }

  confirm(job: Job, result: unknown) {
    const projection = this.operations.get(job.operation)
    if (!projection) return
    if (
      !record(result) ||
      typeof result.result_revision !== 'string' ||
      !/^[1-9][0-9]*$/.test(result.result_revision)
    )
      throw new Error('连线保存回执无法识别，请恢复原操作')
    projection.confirmedRevision = result.result_revision
  }

  remove(operation: string) {
    this.operations.delete(operation)
  }

  reconcile(canvas: Canvas) {
    for (const [operation, projection] of this.operations) {
      if (
        projection.path === `/canvases/${canvas.id}/commands` &&
        projection.confirmedRevision &&
        BigInt(canvas.revision) >= BigInt(projection.confirmedRevision)
      )
        this.operations.delete(operation)
    }
  }

  visible(canvas: Canvas): PendingConnection[] {
    if (canvas.archived) return []
    const live = new Set(canvas.nodes.map((n) => n.id))
    const key = (e: { source_node_id: string; target_node_id: string }) =>
      JSON.stringify([e.source_node_id, e.target_node_id])
    const seen = new Set(canvas.edges.map(key))
    const edges: PendingConnection[] = []
    for (const p of this.operations.values()) {
      if (
        p.path !== `/canvases/${canvas.id}/commands` ||
        (p.confirmedRevision &&
          BigInt(canvas.revision) >= BigInt(p.confirmedRevision))
      )
        continue
      for (const edge of p.edges) {
        if (
          !live.has(edge.source_node_id) ||
          !live.has(edge.target_node_id) ||
          seen.has(key(edge))
        )
          continue
        seen.add(key(edge))
        edges.push(edge)
      }
    }
    return edges
  }
}
