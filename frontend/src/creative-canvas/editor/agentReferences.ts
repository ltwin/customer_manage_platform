import type { components } from '../../api/schema'
import type { ChatAttachment } from '../home/chatAttachments.ts'
import type { Canvas, Content } from './api.ts'
import type { PromptReference, SceneRefDraft } from './promptReferences.ts'

export type { SceneRefDraft }
// 一条消息最多引用几个不同的场景，与服务端 maxSceneRefs 一致；
// 每处引用最多 50 个物件。
export const maxSceneRefs = 5
export const maxSceneRefObjects = 50

export type AgentMaterial = {
  id: string
  title: string
  kind: 'text' | 'image' | 'character'
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
    if (!id || (kind !== 'text' && kind !== 'image' && kind !== 'character')) continue
    materials.set(id, {
      key: id,
      sourceID: id,
      revisionID: id,
      title: node.metadata.title || '素材',
      kind,
      label: kind === 'image' ? '图片' : kind === 'character' ? '角色卡' : '文本',
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
      label: item.kind === 'image' ? '图片' : item.kind === 'character' ? '角色卡' : '文本',
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
// 每插入一次场景引用生成一个本机键：同一个场景可以在一条消息里引用几次，
// 每次带不同的物件。键写进去就不再改，改引用是换一个新键。
export function sceneRefKey(): string {
  return `sref_${crypto.randomUUID().replace(/-/g, '').slice(0, 16)}`
}
// 场景引用标签上的字：标题 · 名称1、名称2 +N；没有名称的物件显示「未命名」。
export function sceneRefTitle(ref: SceneRefDraft): string {
  const title = ref.title || '未命名场景'
  if (!ref.objectIDs.length) return title
  const names = ref.objectIDs.map((_, i) => ref.names[i] || '未命名')
  const shown = names.slice(0, 2).join('、')
  return `${title} · ${shown}${names.length > 2 ? ` +${names.length - 2}` : ''}`
}
export function sceneReferences(
  refs: Readonly<Record<string, SceneRefDraft>>,
): PromptReference[] {
  return Object.entries(refs).map(([key, scene]) => ({
    key,
    sourceID: key,
    revisionID: null,
    title: sceneRefTitle(scene),
    kind: 'scene',
    label: '场景',
    scene,
    connected: false,
  }))
}
// 正文里实际用到的场景引用：存草稿和发送只带这些，删掉的引用留在表里给撤销用。
export function usedSceneRefs(
  text: string,
  refs: Readonly<Record<string, SceneRefDraft>>,
): Record<string, SceneRefDraft> {
  const used: Record<string, SceneRefDraft> = {}
  for (const match of text.matchAll(/@\[([\w-]+)\]/g)) {
    const ref = refs[match[1]]
    if (ref) used[match[1]] = ref
  }
  return used
}
// 正文里的场景引用，按出现顺序（同一个键出现两次就算两次），与 scene_ref 片段一一对应。
export function sceneRefsInOrder(
  text: string,
  refs: Readonly<Record<string, SceneRefDraft>>,
): SceneRefDraft[] {
  return [...text.matchAll(/@\[([\w-]+)\]/g)].flatMap((match) => {
    const ref = refs[match[1]]
    return ref ? [ref] : []
  })
}
// 正文里引用了几个不同的场景。
export function sceneCount(
  text: string,
  refs: Readonly<Record<string, SceneRefDraft>>,
): number {
  return new Set(
    Object.values(usedSceneRefs(text, refs)).map((ref) => ref.documentID),
  ).size
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
    if (item?.scene) {
      if (match.index! > offset)
        segments.push({ type: 'text', text: text.slice(offset, match.index) })
      segments.push({
        type: 'scene_ref',
        document_id: item.scene.documentID,
        ...(item.scene.objectIDs.length
          ? { object_ids: item.scene.objectIDs }
          : {}),
      })
      offset = match.index! + match[0].length
      continue
    }
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
    (item.kind === 'text' || item.kind === 'image' || item.kind === 'character')
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
