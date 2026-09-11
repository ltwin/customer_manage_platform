import type { CanvasNode, GraphAction, Canvas } from './api.ts'
import { parentFirst, selectionRoots, worldPoint } from './graph.ts'
import type { Intent } from './outbox.ts'

export const previewID = (intent: string, index: number, source = 'group') =>
  `pending:${intent}:${index}:${source}`

// Receipt aliases never enter API requests. The editor emits one group action
// per intent; duplicate receipts map each original node to its new identity.
export function previewAliases(intent: Intent): Record<string, string> {
  const aliases: Record<string, string> = {}
  const receipt = intent.receipt
  if (!receipt) return aliases
  for (const [index, action] of (intent.actions ?? []).entries()) {
    if (action.type === 'duplicate_selection')
      for (const [source, id] of Object.entries(receipt.id_mapping ?? {}))
        aliases[previewID(intent.id, index, source)] = id
    if (action.type === 'group_nodes' && intent.actions?.length === 1) {
      const id = receipt.created_ids.find((id) => id.startsWith('cwnode_'))
      if (id) aliases[previewID(intent.id, index)] = id
    }
  }
  return aliases
}

export function resolvePreviewID(
  id: string,
  aliases: Record<string, string>,
): string {
  // Multiple receipts can arrive before the next render. Resolve both a
  // copied provisional source and the copy's own eventual receipt.
  for (let step = 0; step <= Object.keys(aliases).length; step++) {
    let next = aliases[id]
    if (!next && id.startsWith('pending:')) {
      const source = Object.keys(aliases).find((source) =>
        id.endsWith(`:${source}`),
      )
      if (source) next = id.slice(0, -source.length) + aliases[source]
    }
    if (!next || next === id) break
    id = next
  }
  return id
}

export function remapActions(
  actions: GraphAction[],
  aliases: Record<string, string>,
): GraphAction[] {
  return actions.map((action) => {
    const next = { ...action }
    // Rewrite identifiers only, leaving text and configuration opaque.
    if ('node_id' in next)
      next.node_id = resolvePreviewID(next.node_id, aliases)
    if ('parent_id' in next && next.parent_id)
      next.parent_id = resolvePreviewID(next.parent_id, aliases)
    if ('source_node_id' in next)
      next.source_node_id = resolvePreviewID(next.source_node_id, aliases)
    if ('target_node_id' in next)
      next.target_node_id = resolvePreviewID(next.target_node_id, aliases)
    if ('edge_id' in next)
      next.edge_id = resolvePreviewID(next.edge_id, aliases)
    if ('node_ids' in next)
      next.node_ids = next.node_ids.map((id) => resolvePreviewID(id, aliases))
    if (next.type === 'set_node_inputs')
      next.inputs = next.inputs.map((input) => ({
        ...input,
        source_node_id: input.source_node_id
          ? resolvePreviewID(input.source_node_id, aliases)
          : input.source_node_id,
      }))
    return next
  })
}

export function structuralPreview(
  nodes: CanvasNode[],
  edges: Canvas['edges'],
  intent: Intent,
  index: number,
  action: GraphAction,
): { nodes: CanvasNode[]; edges: Canvas['edges'] } {
  const aliases = previewAliases(intent)
  const idFor = (source?: string) => {
    const id = previewID(intent.id, index, source)
    return aliases[id] ?? id
  }
  if (action.type === 'group_nodes') {
    if (intent.actions?.length !== 1) return { nodes, edges }
    const roots = selectionRoots(nodes, action.node_ids)
    if (!roots.length) return { nodes, edges }
    const byID = new Map(nodes.map((n) => [n.id, n]))
    const ancestors = (id: string) => {
      const result: string[] = []
      let parent = byID.get(id)?.parent_id
      while (parent && !result.includes(parent)) {
        result.push(parent)
        parent = byID.get(parent)?.parent_id
      }
      return result
    }
    const parent =
      ancestors(roots[0]!).find((p) =>
        roots.every((id) => ancestors(id).includes(p)),
      ) ?? null
    const points = roots.map((id) => ({
      node: byID.get(id)!,
      ...worldPoint(nodes, id),
    }))
    const x = Math.min(...points.map((p) => p.x)) - 24
    const y = Math.min(...points.map((p) => p.y)) - 52
    const origin = parent ? worldPoint(nodes, parent) : { x: 0, y: 0 }
    const id = idFor()
    const base = points[0]!.node
    const group: CanvasNode = {
      ...base,
      id,
      parent_id: parent,
      content: undefined,
      prompt: null,
      metadata: {
        ...base.metadata,
        type_key: 'core.group',
        title: '未命名分组',
        intent: '',
        x: x - origin.x,
        y: y - origin.y,
        width:
          Math.max(...points.map((p) => p.x + p.node.metadata.width)) - x + 24,
        height:
          Math.max(...points.map((p) => p.y + p.node.metadata.height)) - y + 24,
      },
      data: {
        schema_version: 1,
        config: {},
        content_id: null,
        content_revision_id: null,
        selected_version_id: null,
        document_id: null,
      },
      status: {
        content_state: 'empty',
        generation_state: 'idle',
        active_execution_id: null,
        latest_execution_id: null,
        apply_state: null,
        error: null,
        status_revision: '1',
      },
      placement_revision: '1',
      data_revision: '1',
      capabilities: {
        actions: ['move', 'resize', 'rename', 'duplicate', 'remove', 'ungroup'],
        prompt_mode: 'draft',
        disabled_reason: null,
      },
    }
    return {
      nodes: [
        ...nodes.map((n) => {
          const point = points.find((p) => p.node.id === n.id)
          return point
            ? {
                ...n,
                parent_id: id,
                metadata: { ...n.metadata, x: point.x - x, y: point.y - y },
              }
            : n
        }),
        group,
      ],
      edges,
    }
  }
  if (action.type === 'ungroup_nodes') {
    const group = nodes.find(
      (n) => n.id === action.node_id && n.metadata.type_key === 'core.group',
    )
    if (!group) return { nodes, edges }
    return {
      nodes: nodes
        .filter((n) => n.id !== group.id)
        .map((n) =>
          n.parent_id === group.id
            ? {
                ...n,
                parent_id: group.parent_id,
                metadata: {
                  ...n.metadata,
                  x: n.metadata.x + group.metadata.x,
                  y: n.metadata.y + group.metadata.y,
                },
              }
            : n,
        ),
      edges,
    }
  }
  if (action.type === 'duplicate_selection') {
    if (intent.receipt && !Object.keys(intent.receipt.id_mapping ?? {}).length)
      return { nodes, edges }
    const roots = new Set(selectionRoots(nodes, action.node_ids))
    const closure = new Set(roots)
    for (const n of parentFirst(nodes))
      if (n.parent_id && closure.has(n.parent_id)) closure.add(n.id)
    const copies = nodes
      .filter((n) => closure.has(n.id))
      .map((n): CanvasNode => ({
        ...n,
        id: idFor(n.id),
        parent_id:
          n.parent_id && closure.has(n.parent_id)
            ? idFor(n.parent_id)
            : n.parent_id,
        metadata: {
          ...n.metadata,
          x: n.metadata.x + (roots.has(n.id) ? action.dx : 0),
          y: n.metadata.y + (roots.has(n.id) ? action.dy : 0),
        },
        data: { ...n.data, selected_version_id: null },
        status: {
          ...n.status,
          generation_state: 'idle',
          active_execution_id: null,
          latest_execution_id: null,
          apply_state: null,
          error: null,
          status_revision: '1',
        },
        placement_revision: '1',
        data_revision: '1',
      }))
    const copiedEdges = edges
      .filter(
        (e) => closure.has(e.source_node_id) && closure.has(e.target_node_id),
      )
      .map((e) => ({
        ...e,
        id: previewID(intent.id, index, e.id),
        source_node_id: idFor(e.source_node_id),
        target_node_id: idFor(e.target_node_id),
        revision: '1',
      }))
    return {
      nodes: [...nodes, ...copies],
      edges: [...edges, ...copiedEdges],
    }
  }
  return { nodes, edges }
}
