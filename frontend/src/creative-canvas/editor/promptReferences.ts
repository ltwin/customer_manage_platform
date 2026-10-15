import type { Asset, Canvas, CanvasNode, Content } from './api.ts'
import type { Prompt } from './generation.ts'

// 展示信息来自画布快照；编号排序与服务端生成准入保持一致。
export type PromptReference = {
  key: string
  sourceID: string | null
  title: string
  kind: string
  label: string
  content?: Content
  revisionID: string | null
  connected: boolean
  edgeID?: string
  draftIndex?: number
  // 还没有内容修订的本机图片（助手里刚粘贴或上传的）：用本机预览，上传完才能发送。
  thumbnail?: string
  state?: 'uploading' | 'failed'
}
const mention = () => /@\[([\w-]+)\]/g
export function cleanMentions(text: string, ids: ReadonlySet<string>) {
  return text.replace(mention(), (token: string, id: string) =>
    ids.has(id) ? token : '',
  )
}
export function mentionToken(id: string) {
  return `@[${id}]`
}

export function promptReferences(
  canvas: Canvas,
  node: CanvasNode,
  prompt: Prompt,
  assets: Asset[] = [],
): PromptReference[] {
  const connected = new Set(
    canvas.edges
      .filter((e) => e.target_node_id === node.id)
      .map((e) => e.source_node_id),
  )
  const values = [
    ...prompt.references.map((r, i) => ({
      rank: 0,
      ordinal: r.ordinal,
      slot: r.slot,
      id:
        node.prompt?.references.find(
          (old) =>
            old.ordinal === r.ordinal &&
            old.source_node_id === r.source_node_id &&
            old.content_revision_id === r.content_revision_id,
        )?.id ?? String(i),
      sourceID: r.source_node_id,
      revision: r.content_revision_id,
      edgeID: undefined,
      draftIndex: i,
    })),
    ...canvas.node_inputs
      .filter((r) => r.node_id === node.id && !r.draft_id)
      .map((r) => ({
        rank: 1,
        ordinal: r.ordinal,
        slot: r.slot,
        id: r.id,
        sourceID: r.source_node_id,
        revision: r.content_revision_id,
        edgeID: undefined,
        draftIndex: undefined,
      })),
    ...canvas.edges
      .filter((e) => e.target_node_id === node.id)
      .map((e) => ({
        rank: 2,
        ordinal: e.ordinal,
        slot: e.target_port,
        id: e.id,
        sourceID: e.source_node_id,
        revision: null,
        edgeID: e.id,
        draftIndex: undefined,
      })),
  ].sort(
    (a, b) =>
      a.rank - b.rank ||
      (a.slot < b.slot ? -1 : a.slot > b.slot ? 1 : 0) ||
      a.ordinal - b.ordinal ||
      (a.id < b.id ? -1 : a.id > b.id ? 1 : 0),
  )
  const counts = new Map<string, number>()
  return values.map((r) => {
    const source = canvas.nodes.find((n) => n.id === r.sourceID)
    const asset = assets.find((a) => a.content_revision_id === r.revision)
    const content = source?.content ?? asset?.content
    const kind =
      content?.kind ?? source?.metadata.type_key.replace('core.', '') ?? 'image'
    const number = (counts.get(kind) ?? 0) + 1
    counts.set(kind, number)
    const prefixes: Record<string, string> = {
      image: '图',
      video: '视频',
      audio: '音频',
      text: '文本',
      link: '链接',
    }
    const prefix = prefixes[kind] ?? '参考'
    return {
      key: `${r.rank}:${r.id}`,
      sourceID: r.sourceID ?? null,
      title: source?.metadata.title || asset?.title || '参考素材',
      kind,
      label: `${prefix}${number}`,
      content,
      revisionID: source?.data.content_revision_id ?? r.revision ?? null,
      connected: !!r.sourceID && connected.has(r.sourceID),
      edgeID: r.edgeID,
      draftIndex: r.draftIndex,
    }
  })
}

export function connectedReferences(refs: PromptReference[]) {
  const seen = new Set<string>()
  return refs.filter((r) => {
    if (!r.connected || !r.sourceID || seen.has(r.sourceID)) return false
    seen.add(r.sourceID)
    return true
  })
}

// 与服务端的 32000 rune 限额一致，不能截断代理对或一个引用 token。
export function limitPrompt(text: string, max = 32000): string {
  const runes = Array.from(text)
  if (runes.length <= max) return text
  let result = runes.slice(0, max).join('')
  const token = result.lastIndexOf('@[')
  if (token >= 0 && !result.slice(token).includes(']'))
    result = result.slice(0, token)
  return result
}
