import type { components } from '../../api/schema'
import type { ChatAttachment } from '../home/chatAttachments.ts'
import type { Canvas, Content } from './api.ts'
import type { PromptReference } from './promptReferences.ts'

export type AgentMaterial = {
  id: string
  title: string
  kind: 'text' | 'image'
  // 资产库选的带着内容，引用处能显示缩略图和预览。
  content?: Content
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
      content: item.content ?? previous?.content,
      connected: false,
    })
  }
  return [...materials.values()]
}
// 粘贴或上传的图片：输入框里按本机的 key 引用，上传完才有内容修订。
export function uploadReferences(
  items: readonly ChatAttachment[],
): PromptReference[] {
  return items.map((item) => ({
    key: item.key,
    sourceID: item.key,
    revisionID: item.state === 'ready' ? (item.revisionID ?? null) : null,
    title: item.title,
    kind: 'image',
    label: '图片',
    thumbnail: item.preview,
    ...(item.state === 'ready' ? {} : { state: item.state }),
    connected: false,
  }))
}
// 发出去和存起来的文字只认内容修订：把还按本机 key 写的引用换成修订 id。
export function revisionText(
  text: string,
  references: PromptReference[],
): string {
  return text.replace(/@\[([\w-]+)\]/g, (token, id: string) => {
    const revision = references.find((item) => item.sourceID === id)?.revisionID
    return revision ? `@[${revision}]` : token
  })
}
export function agentSegments(
  text: string,
  references: PromptReference[],
): components['schemas']['CreativeAgentInstructionSegment'][] {
  const segments: components['schemas']['CreativeAgentInstructionSegment'][] =
    []
  let offset = 0
  for (const match of text.matchAll(/@\[([\w-]+)\]/g)) {
    const item = references.find((r) => r.sourceID === match[1])
    if (!item?.revisionID)
      throw new Error(
        item?.state ? '参考图还没上传好' : '引用素材已不可用，请重新选择',
      )
    if (match.index! > offset)
      segments.push({ type: 'text', text: text.slice(offset, match.index) })
    segments.push({ type: 'content_ref', content_revision_id: item.revisionID })
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
    item.sourceID &&
    ids.has(item.sourceID) &&
    (item.kind === 'text' || item.kind === 'image')
      ? [
          {
            id: item.revisionID,
            title: item.title,
            kind: item.kind,
            ...(item.content ? { content: item.content } : {}),
          },
        ]
      : [],
  )
}
