import type { Canvas, CanvasNode, GraphRead, GraphAction } from './api.ts'
export function graphReadSet(
  canvas: Canvas,
  extraIDs: string[] = [],
): GraphRead[] {
  return [
    ...(canvas.object_states ?? [])
      .filter((i) => extraIDs.includes(i.id))
      .map(
        ({
          id,
          kind,
          placement_revision,
          data_revision,
          revision,
        }): GraphRead => ({
          id,
          kind,
          placement_revision,
          data_revision,
          revision,
        }),
      ),
    ...canvas.nodes.map((n): GraphRead => ({
      kind: 'node',
      id: n.id,
      placement_revision: n.placement_revision,
      data_revision: n.data_revision,
    })),
    ...(canvas.edges ?? []).map((e): GraphRead => ({
      kind: 'edge',
      id: e.id,
      revision: e.revision,
    })),
    ...(canvas.node_inputs ?? []).map((i): GraphRead => ({
      kind: 'input',
      id: i.id,
      revision: i.revision,
    })),
  ]
}
export function selectionRoots(nodes: CanvasNode[], ids: string[]): string[] {
  const byID = new Map(nodes.map((n) => [n.id, n]))
  const selected = new Set(ids)
  return [...selected].filter((id) => {
    let parent = byID.get(id)?.parent_id
    const seen = new Set<string>()
    while (parent && !seen.has(parent)) {
      if (selected.has(parent)) return false
      seen.add(parent)
      parent = byID.get(parent)?.parent_id
    }
    return byID.has(id)
  })
}
export function worldPoint(
  nodes: CanvasNode[],
  id: string,
): { x: number; y: number } {
  const byID = new Map(nodes.map((n) => [n.id, n]))
  const node = byID.get(id)
  let x = node?.metadata.x ?? 0,
    y = node?.metadata.y ?? 0,
    parent = node?.parent_id
  const seen = new Set<string>([id])
  while (parent && !seen.has(parent)) {
    seen.add(parent)
    const n = byID.get(parent)
    x += n?.metadata.x ?? 0
    y += n?.metadata.y ?? 0
    parent = n?.parent_id
  }
  return { x, y }
}
export function parentFirst(nodes: CanvasNode[]): CanvasNode[] {
  const byID = new Map(nodes.map((n) => [n.id, n]))
  const result: CanvasNode[] = []
  const seen = new Set<string>()
  const visit = (n: CanvasNode) => {
    if (seen.has(n.id)) return
    seen.add(n.id)
    const parent = n.parent_id ? byID.get(n.parent_id) : null
    if (parent) visit(parent)
    result.push(n)
  }
  nodes.forEach(visit)
  return result
}
// 撤销栈的一项是一步可理解的编辑。同一撤销组的连续改动（拖入文件的建节点
// 与随后的上传绑定）合成一项：撤销按组发出，服务器合成一条反向改动，
// 反向改动指向组内最后一条。
type Step = { ids: string[]; group: string | null }
export function canvasHistory(changes: Canvas['changes']) {
  const undo: Step[] = [],
    redo: string[] = []
  const owner = (id: string | null) => undo.findIndex((s) => !!id && s.ids.includes(id))
  for (const change of [...(changes ?? [])].reverse()) {
    if (!change.inverse_of) {
      const top = undo.at(-1)
      if (change.change_group_id && top?.group === change.change_group_id)
        top.ids.push(change.id)
      else undo.push({ ids: [change.id], group: change.change_group_id ?? null })
      redo.length = 0
      continue
    }
    const original = owner(change.inverse_of)
    if (original >= 0) {
      undo.splice(original, 1)
      redo.push(change.id)
      continue
    }
    const reversed = redo.indexOf(change.inverse_of)
    if (reversed >= 0) {
      redo.splice(reversed, 1)
      undo.push({ ids: [change.id], group: null })
    }
  }
  const top = undo.at(-1)
  // 服务器按组撤销会回退组内全部改动，所以只有这一项装着整组时才按组发；
  // 组被别的改动隔开时退回逐条撤销。
  const whole =
    !!top?.group &&
    top.ids.length > 1 &&
    (changes ?? []).filter((c) => !c.inverse_of && c.change_group_id === top.group)
      .length === top.ids.length
  return {
    undo: top?.ids.at(-1),
    undoGroup: whole ? top.group! : undefined,
    /** 栈顶这一步所属的撤销组（只有一条改动时也有），撤销时据此取消上传。 */
    undoStepGroup: top?.group ?? undefined,
    undoIDs: top?.ids ?? [],
    redo: redo.at(-1),
  }
}

// Version history belongs to its node; only copy/remove/version commands read it.
export function graphActionReadSet(
  canvas: Canvas,
  actions: GraphAction[],
): GraphRead[] {
  const versions = new Set<string>()
  for (const action of actions) {
    if ('version_id' in action) versions.add(action.version_id)
    if (action.type !== 'duplicate_selection' && action.type !== 'remove_nodes')
      continue
    const closure = new Set(action.node_ids)
    for (const node of parentFirst(canvas.nodes)) {
      if (node.parent_id && closure.has(node.parent_id)) closure.add(node.id)
      if (
        action.type === 'duplicate_selection' &&
        closure.has(node.id) &&
        node.data.selected_version_id
      )
        versions.add(node.data.selected_version_id)
    }
    if (action.type === 'remove_nodes') {
      for (const state of canvas.object_states ?? []) {
        if (
          state.kind === 'version' &&
          state.is_live &&
          state.node_id &&
          closure.has(state.node_id)
        )
          versions.add(state.id)
      }
    }
  }
  return graphReadSet(canvas, [...versions])
}
