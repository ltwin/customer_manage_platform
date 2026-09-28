import type { components } from '../../api/schema'
import type { Canvas, CanvasNode } from './api.ts'
import { graphReadSet } from './graph.ts'

export type Prompt = components['schemas']['CreativeSavePromptPayload']
export type GenerationCatalog =
  components['schemas']['CreativeGenerationCatalog']
export type GenerationParameter =
  components['schemas']['CreativeGenerationParameter']
export type Versions = components['schemas']['CreativeNodeVersionPage']
export type Execution = components['schemas']['CreativeNodeExecution']
export type InputSource = components['schemas']['CreativeInputSource']

export function promptFromNode(canvas: Canvas, node: CanvasNode): Prompt {
  return {
    type: 'save_prompt',
    node_id: node.id,
    expected_draft_revision: node.prompt?.draft_revision ?? null,
    expected_topology_revision: canvas.topology_revision,
    read_set: graphReadSet(canvas),
    action_key: 'gateway.generation',
    text: node.prompt?.text ?? '',
    model_key: node.prompt?.model_key ?? '',
    parameters: structuredClone(node.prompt?.parameters ?? {}),
    references: (node.prompt?.references ?? []).map((r) => ({
      slot: 'reference',
      ordinal: r.ordinal,
      role: 'reference',
      source_node_id: r.source_node_id,
      content_revision_id: r.content_revision_id,
    })),
  }
}

export function parameterValue(
  field: GenerationParameter,
  input: string,
): string | number | boolean | undefined {
  if (input === '') return undefined
  if (field.type === 'boolean') {
    if (!['true', 'false'].includes(input))
      throw new Error(`${field.label}格式不正确`)
    return input === 'true'
  }
  if (field.type === 'number' || field.type === 'integer') {
    const n = Number(input)
    if (
      !Number.isFinite(n) ||
      (field.type === 'integer' && !Number.isSafeInteger(n))
    )
      throw new Error(
        `${field.label}需要有效${field.type === 'integer' ? '整数' : '数值'}`,
      )
    return n
  }
  return input
}

export function generationReadSet(canvas: Canvas, nodeID: string) {
  const ids = new Set([nodeID])
  for (const r of canvas.node_inputs)
    if (r.node_id === nodeID && r.source_node_id) ids.add(r.source_node_id)
  for (const e of canvas.edges)
    if (e.target_node_id === nodeID) ids.add(e.source_node_id)
  return canvas.nodes
    .filter((n) => ids.has(n.id))
    .map((n) => ({
      kind: 'node' as const,
      id: n.id,
      data_revision: n.data_revision,
    }))
}

export const executionLabel = (state: string) =>
  ({
    idle: '尚未生成',
    queued: '等待生成',
    running: '正在生成',
    succeeded: '生成已完成',
    failed: '生成未全部完成',
    cancelled: '已停止',
    reconciling: '结果待核实',
  })[state] ?? '状态待确认'

export function assertGenerationSnapshot(
  before: Canvas,
  fresh: Canvas,
  nodeID: string,
  revision: string,
) {
  const target = fresh.nodes.find((n) => n.id === nodeID)
  const previous = before.nodes.find((n) => n.id === nodeID)
  if (
    !target?.prompt ||
    target.prompt.draft_revision !== revision ||
    target.data_revision !== previous?.data_revision
  )
    throw new Error('节点或提示词已变化，请核对后再生成')
  const externalReferences = (c: Canvas) =>
    JSON.stringify(
      [
        ...c.edges
          .filter((e) => e.target_node_id === nodeID)
          .map((e) => `edge:${e.id}:${e.revision}`),
        ...c.node_inputs
          .filter((i) => i.node_id === nodeID && !i.draft_id)
          .map((i) => `input:${i.id}:${i.revision}`),
      ].sort(),
    )
  if (externalReferences(before) !== externalReferences(fresh))
    throw new Error('参考关系已变化，请核对后再生成')
  for (const ref of generationReadSet(fresh, nodeID)) {
    if (
      before.nodes.find((n) => n.id === ref.id)?.data_revision !==
      ref.data_revision
    )
      throw new Error('参考节点已变化，请核对后再生成')
  }
  return target
}
