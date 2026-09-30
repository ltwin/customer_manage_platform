import type { components } from '../../api/schema'
import type { Canvas } from './api.ts'
import type { PromptReference } from './promptReferences.ts'

export type AgentMaterial = {
  id: string
  title: string
  kind: 'text' | 'image'
}
export function agentReferences(
  canvas: Canvas | null | undefined,
  saved: AgentMaterial[],
): PromptReference[] {
  const materials = new Map<string, PromptReference>()
  for (const node of canvas?.nodes ?? []) {
    const id = node.data.content_revision_id
    const kind = node.content?.kind
    if (!id || (kind !== 'text' && kind !== 'image')) continue
    materials.set(id, {
      key: id,
      sourceID: id,
      revisionID: id,
      title: node.metadata.title || '素材',
      kind,
      label: kind === 'image' ? '图片' : '文本',
      content: node.content,
      connected: false,
    })
  }
  for (const item of saved) {
    const previous = materials.get(item.id)
    materials.set(item.id, {
      ...previous,
      key: item.id,
      sourceID: item.id,
      revisionID: item.id,
      title: item.title,
      kind: item.kind,
      label: item.kind === 'image' ? '图片' : '文本',
      connected: false,
    })
  }
  return [...materials.values()]
}
export function agentSegments(
  text: string,
  references: PromptReference[],
): components['schemas']['CreativeAgentInstructionSegment'][] {
  const segments: components['schemas']['CreativeAgentInstructionSegment'][] =
    []
  let offset = 0
  for (const match of text.matchAll(/@\[([\w-]+)\]/g)) {
    if (!references.some((item) => item.revisionID === match[1]))
      throw new Error('引用素材已不可用，请重新选择')
    if (match.index! > offset)
      segments.push({ type: 'text', text: text.slice(offset, match.index) })
    segments.push({ type: 'content_ref', content_revision_id: match[1] })
    offset = match.index! + match[0].length
  }
  if (offset < text.length)
    segments.push({ type: 'text', text: text.slice(offset) })
  return segments
}
export function referencedMaterials(
  text: string,
  references: PromptReference[],
): AgentMaterial[] {
  const ids = new Set(
    [...text.matchAll(/@\[([\w-]+)\]/g)].map((match) => match[1]),
  )
  return references.flatMap((item) =>
    item.revisionID &&
    ids.has(item.revisionID) &&
    (item.kind === 'text' || item.kind === 'image')
      ? [{ id: item.revisionID, title: item.title, kind: item.kind }]
      : [],
  )
}
