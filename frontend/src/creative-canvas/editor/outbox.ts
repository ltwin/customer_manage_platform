import type {
  Canvas,
  CanvasNode,
  ChangeResult,
  Content,
  GraphAction,
  GraphRead,
} from './api.ts'
import type { Draft } from './journal.ts'
import { structuralPreview } from './structuralPreview.ts'
import { canvasHistory, graphActionReadSet, parentFirst } from './graph.ts'

// An intent is one local edit: pending in the outbox, sent as the immutable
// job, or confirmed by a receipt the snapshot has not caught up with yet.
// The projection below paints intents over the snapshot so the canvas answers
// immediately; the server remains the only authority on what committed.
export type Receipt = {
  change_id: string
  inverse_of: string | null
  result_revision: string
  result_topology_revision: string
  object_results: ChangeResult['object_results']
  created_ids: string[]
  omitted_reference_ids: string[]
  id_mapping?: ChangeResult['id_mapping']
}
export type Intent = {
  id: string
  canvasID: string
  kind: 'actions' | 'undo' | 'redo'
  actions?: GraphAction[]
  // Display-only content for nodes created from library assets.
  preview?: Record<string, Content>
  draftKey?: string
  draftValue?: Draft
  state: 'pending' | 'sent' | 'confirmed'
  // Undo/redo target resolved when the request was built.
  changeID?: string
  receipt?: Receipt
}
export const intentKinds = ['actions', 'undo', 'redo'] as const
export const intentStates = ['pending', 'sent', 'confirmed'] as const

// Mirrors NodeDefinitions on the server; only paints a node before its snapshot.
const defaultActions: Record<string, string[]> = {
  'core.text': [
    'move',
    'resize',
    'rename',
    'duplicate',
    'remove',
    'edit',
    'reference',
    'save_to_library',
  ],
  'core.link': [
    'move',
    'resize',
    'rename',
    'duplicate',
    'remove',
    'edit',
    'reference',
    'save_to_library',
  ],
  'core.image': [
    'move',
    'resize',
    'rename',
    'duplicate',
    'remove',
    'edit',
    'reference',
    'maximize',
    'download',
    'replace',
    'save_to_library',
  ],
  'core.video': [
    'move',
    'resize',
    'rename',
    'duplicate',
    'remove',
    'edit',
    'reference',
    'maximize',
    'download',
    'replace',
    'save_to_library',
  ],
  'core.audio': [
    'move',
    'resize',
    'rename',
    'duplicate',
    'remove',
    'edit',
    'reference',
    'download',
    'replace',
    'save_to_library',
  ],
  'core.group': ['move', 'resize', 'rename', 'duplicate', 'remove', 'ungroup'],
}
export const defaultNodeWidth = 280,
  defaultNodeHeight = 180
// Same rule as the server's fitMediaSize: header + footer chrome around a
// picture that keeps its aspect ratio at the default width.
export function fitMediaHeight(content: Content | undefined): number {
  const original = content?.media?.find((m) => m.role === 'original')
  if (
    !content ||
    content.kind === 'audio' ||
    !original?.width ||
    !original.height ||
    original.width <= 0 ||
    original.height <= 0
  )
    return defaultNodeHeight
  return Math.round(
    Math.min(
      640,
      Math.max(140, 74 + (defaultNodeWidth * original.height) / original.width),
    ),
  )
}
const layoutOnly = (actions: GraphAction[] | undefined) =>
  !!actions?.length &&
  actions.every((a) => a.type === 'move_node' || a.type === 'resize_node')
const local = (id: string) => id.startsWith('pending:')

// A burst of moves/resizes merges into the still-pending tail: later values
// win per node, so one command reaches the server.
export function coalesce(
  outbox: Intent[],
  next: Intent,
): { outbox: Intent[]; id: string } {
  const last = outbox.at(-1)
  if (
    last &&
    last.state === 'pending' &&
    last.canvasID === next.canvasID &&
    next.kind === 'actions' &&
    last.kind === 'actions' &&
    layoutOnly(last.actions) &&
    layoutOnly(next.actions)
  ) {
    const merged = [...last.actions!]
    for (const action of next.actions!) {
      const index = merged.findIndex(
        (m) =>
          m.type === action.type &&
          'node_id' in m &&
          'node_id' in action &&
          m.node_id === action.node_id,
      )
      if (index >= 0) merged[index] = action
      else merged.push(action)
    }
    return {
      outbox: [...outbox.slice(0, -1), { ...last, actions: merged }],
      id: last.id,
    }
  }
  return { outbox: [...outbox, next], id: next.id }
}

// Node ids that exist only in the projection until their creation commits.
export function localNodeIDs(intents: Intent[]): Set<string> {
  const ids = new Set<string>()
  for (const intent of intents) {
    if (intent.state === 'confirmed') continue
    for (const action of intent.actions ?? [])
      if (action.type === 'add_node') ids.add(action.node_id)
  }
  return ids
}

export function objectRead(
  o: ChangeResult['object_results'][number],
): GraphRead {
  return o.kind === 'node'
    ? {
        kind: 'node',
        id: o.id,
        placement_revision: o.placement_revision,
        data_revision: o.data_revision,
      }
    : { kind: o.kind, id: o.id, revision: o.revision }
}
export function receiptSummary(r: Receipt): Canvas['changes'][number] {
  return {
    id: r.change_id,
    inverse_of: r.inverse_of,
    change_group_id: null,
    result_revision: r.result_revision,
    read_set: r.object_results.map(objectRead),
  }
}

// Reads the server can verify: projected-only objects are left out.
export function serverReads(
  reads: GraphRead[],
  localIDs: Set<string>,
): GraphRead[] {
  return reads.filter((r) => !local(r.id) && !localIDs.has(r.id))
}
// Read set for one intent against the projected view (which already carries
// the newest confirmed revisions).
export function intentReadSet(
  view: Canvas,
  intent: Intent,
  changeID: string | undefined,
  localIDs: Set<string>,
): GraphRead[] {
  if (intent.kind === 'actions')
    return serverReads(graphActionReadSet(view, intent.actions ?? []), localIDs)
  const change = view.changes.find((c) => c.id === changeID)
  const byID = new Map<string, GraphRead>([
    ...view.nodes.map((n): [string, GraphRead] => [
      n.id,
      {
        kind: 'node',
        id: n.id,
        placement_revision: n.placement_revision,
        data_revision: n.data_revision,
      },
    ]),
    ...view.edges.map((e): [string, GraphRead] => [
      e.id,
      { kind: 'edge', id: e.id, revision: e.revision },
    ]),
    ...view.node_inputs.map((i): [string, GraphRead] => [
      i.id,
      { kind: 'input', id: i.id, revision: i.revision },
    ]),
    ...view.object_states.map((o): [string, GraphRead] => [
      o.id,
      objectRead(o),
    ]),
  ])
  return (change?.read_set ?? []).map((r) => byID.get(r.id) ?? r)
}

// projectCanvas paints live intents and newer receipts over the snapshot.
// Untouched objects keep their identity so memoized cards do not repaint.
export function projectCanvas(canvas: Canvas, intents: Intent[]): Canvas {
  let nodes = canvas.nodes,
    edges = canvas.edges,
    inputs = canvas.node_inputs,
    states = canvas.object_states,
    changes = canvas.changes,
    topology = canvas.topology_revision,
    changed = false
  const setNode = (id: string, update: (n: CanvasNode) => CanvasNode) => {
    const index = nodes.findIndex((n) => n.id === id)
    if (index < 0) return
    const next = update(nodes[index]!)
    if (next === nodes[index]) return
    nodes = nodes.map((n, i) => (i === index ? next : n))
    changed = true
  }
  const live = intents.filter(
    (i) =>
      i.canvasID === canvas.id &&
      !(
        i.receipt &&
        BigInt(i.receipt.result_revision) <= BigInt(canvas.revision)
      ),
  )
  for (const intent of live) {
    for (const [index, action] of (intent.actions ?? []).entries()) {
      switch (action.type) {
        case 'add_node': {
          if (nodes.some((n) => n.id === action.node_id)) break
          const preview = intent.preview?.[action.node_id]
          const content: Content | undefined =
            preview ??
            (action.content
              ? {
                  id: '',
                  content_id: '',
                  kind: action.content.kind,
                  sequence: '1',
                  payload: action.content.payload,
                  truncated: false,
                }
              : undefined)
          nodes = [
            ...nodes,
            {
              id: action.node_id,
              parent_id: action.parent_id ?? null,
              metadata: {
                type_key: action.type_key,
                type_version: 1,
                title: action.title ?? '',
                intent: '',
                x: action.x,
                y: action.y,
                width: defaultNodeWidth,
                height: fitMediaHeight(content),
                z_order: 0,
              },
              data: {
                schema_version: 1,
                config: {},
                content_id: content?.content_id || null,
                content_revision_id: content?.id || null,
                selected_version_id: null,
                document_id: null,
              },
              status: {
                content_state: content ? 'ready' : 'empty',
                generation_state: 'idle',
                active_execution_id: null,
                latest_execution_id: null,
                apply_state: null,
                error: null,
                status_revision: '1',
              },
              prompt: null,
              capabilities: {
                actions: defaultActions[action.type_key] ?? [],
                prompt_mode: 'draft',
                disabled_reason: null,
              },
              placement_revision: '1',
              data_revision: '1',
              ...(content ? { content } : {}),
            },
          ]
          changed = true
          break
        }
        case 'remove_nodes': {
          const closure = new Set(action.node_ids)
          for (const n of parentFirst(nodes))
            if (n.parent_id && closure.has(n.parent_id)) closure.add(n.id)
          if (!nodes.some((n) => closure.has(n.id))) break
          nodes = nodes.filter((n) => !closure.has(n.id))
          edges = edges.filter(
            (e) =>
              !closure.has(e.source_node_id) && !closure.has(e.target_node_id),
          )
          inputs = inputs.filter(
            (i) =>
              !closure.has(i.node_id) &&
              !(i.source_node_id && closure.has(i.source_node_id)),
          )
          changed = true
          break
        }
        case 'move_node':
          setNode(action.node_id, (n) =>
            n.metadata.x === action.x && n.metadata.y === action.y
              ? n
              : { ...n, metadata: { ...n.metadata, x: action.x, y: action.y } },
          )
          break
        case 'resize_node':
          setNode(action.node_id, (n) =>
            n.metadata.width === action.width &&
            n.metadata.height === action.height
              ? n
              : {
                  ...n,
                  metadata: {
                    ...n.metadata,
                    width: action.width,
                    height: action.height,
                  },
                },
          )
          break
        case 'update_metadata':
          setNode(action.node_id, (n) => ({
            ...n,
            metadata: {
              ...n.metadata,
              title: action.title,
              intent: action.intent ?? n.metadata.intent,
            },
          }))
          break
        case 'replace_content':
          setNode(action.node_id, (n) => ({
            ...n,
            status: { ...n.status, content_state: 'ready' },
            content: n.content
              ? { ...n.content, payload: action.payload, truncated: false }
              : {
                  id: '',
                  content_id: '',
                  kind: n.metadata.type_key.slice(5) as Content['kind'],
                  sequence: '1',
                  payload: action.payload,
                  truncated: false,
                },
          }))
          break
        case 'connect_reference': {
          if (
            edges.some(
              (e) =>
                e.source_node_id === action.source_node_id &&
                e.target_node_id === action.target_node_id,
            )
          )
            break
          edges = [
            ...edges,
            {
              id: `pending:${intent.id}:${index}`,
              source_node_id: action.source_node_id,
              target_node_id: action.target_node_id,
              source_port: 'output',
              target_port: 'reference',
              role: 'reference',
              ordinal: 0,
              revision: '1',
            },
          ]
          changed = true
          break
        }
        case 'disconnect_reference':
          if (!edges.some((e) => e.id === action.edge_id)) break
          edges = edges.filter((e) => e.id !== action.edge_id)
          changed = true
          break
        case 'group_nodes':
        case 'ungroup_nodes':
        case 'duplicate_selection': {
          const preview = structuralPreview(
            nodes,
            edges,
            intent,
            index,
            action,
          )
          changed ||= preview.nodes !== nodes || preview.edges !== edges
          nodes = preview.nodes
          edges = preview.edges
          break
        }
        default:
          // Versions and inputs wait for the snapshot.
          break
      }
    }
    const receipt = intent.receipt
    if (receipt) {
      changes = [receiptSummary(receipt), ...changes]
      if (BigInt(receipt.result_topology_revision) > BigInt(topology))
        topology = receipt.result_topology_revision
      changed = true
      for (const o of receipt.object_results) {
        if (o.kind === 'node') {
          if (!o.is_live) {
            if (nodes.some((n) => n.id === o.id)) {
              nodes = nodes.filter((n) => n.id !== o.id)
              changed = true
            }
            continue
          }
          setNode(o.id, (n) =>
            n.placement_revision === o.placement_revision &&
            n.data_revision === o.data_revision
              ? n
              : {
                  ...n,
                  placement_revision:
                    o.placement_revision ?? n.placement_revision,
                  data_revision: o.data_revision ?? n.data_revision,
                },
          )
        } else if (o.kind === 'edge') {
          if (!o.is_live) {
            if (edges.some((e) => e.id === o.id)) {
              edges = edges.filter((e) => e.id !== o.id)
              changed = true
            }
          } else if (o.revision)
            edges = edges.map((e) =>
              e.id === o.id && e.revision !== o.revision
                ? { ...e, revision: o.revision! }
                : e,
            )
        } else if (o.kind === 'input') {
          if (!o.is_live) inputs = inputs.filter((i) => i.id !== o.id)
          else if (o.revision)
            inputs = inputs.map((i) =>
              i.id === o.id && i.revision !== o.revision
                ? { ...i, revision: o.revision! }
                : i,
            )
        }
        states = states.some((s) => s.id === o.id)
          ? states.map((s) => (s.id === o.id ? o : s))
          : o.is_live
            ? states
            : [...states, o]
      }
    }
  }
  return changed
    ? {
        ...canvas,
        nodes,
        edges,
        node_inputs: inputs,
        object_states: states,
        changes,
        topology_revision: topology,
      }
    : canvas
}

// Whether every effect of an intent is painted locally; anything else asks
// for a fresh snapshot once its receipt arrives.
export function projected(intent: Pick<Intent, 'kind' | 'actions'>): boolean {
  const painted = new Set([
    'add_node',
    'remove_nodes',
    'move_node',
    'resize_node',
    'update_metadata',
    'replace_content',
    'connect_reference',
    'disconnect_reference',
  ])
  return (
    intent.kind === 'actions' &&
    (intent.actions ?? []).every((a) => painted.has(a.type))
  )
}

// What undo/redo would do next: cancel the still-pending tail, or reverse the
// newest change the server knows about.
export function historyOf(view: Canvas, outbox: Intent[], redo: Intent[]) {
  const mine = outbox.filter((i) => i.canvasID === view.id)
  const tail = mine.at(-1)
  const server = canvasHistory(view.changes)
  const cancelUndo =
    !!tail && tail.state === 'pending' && tail.kind === 'actions'
  const cancelRedo = !!tail && tail.state === 'pending' && tail.kind === 'undo'
  const inFlight = mine.some((i) => i.state !== 'confirmed')
  return {
    canUndo: cancelUndo || inFlight || !!server.undo,
    canRedo:
      cancelRedo ||
      redo.some((i) => i.canvasID === view.id) ||
      (!inFlight && !!server.redo),
    undo: server.undo,
    redo: server.redo,
  }
}
