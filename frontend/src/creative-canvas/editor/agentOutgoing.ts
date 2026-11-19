import type { components } from '../../api/schema'
import type { PendingAgentChain } from './agentState.ts'
import { sceneRefTitle, type SceneRefDraft } from './agentReferences.ts'

type Segment = components['schemas']['CreativeAgentInstructionSegment']
type Block =
  components['schemas']['CreativeAgentMessage']['body']['blocks'][number]

type MessageSkill = Pick<
  components['schemas']['CreativeAgentMessageSkill'],
  'skill_id' | 'skill_version_id' | 'display_name' | 'version_number'
>

// 摄影师消息的一段：文字、引用（Skill、素材或场景）、命令，或旧页面从 3D 场景发送时
// 选中的物体（names 按选中顺序，未命名的为空串）。已存的消息和刚发出的消息都画成这样。
// 场景引用的 chip 带 detail：完整的物件名单。
export type MessagePart =
  | { kind: 'text'; text: string }
  | {
      kind: 'chip'
      title: string
      scene?: true
      sceneTitle?: string
      detail?: string
      revisionID?: string
      skill?: MessageSkill
    }
  | { kind: 'command'; command: string; text?: string }
  | { kind: 'scene'; names: string[] }

// 刚发出、还没从服务端读回的一条消息。先画出来，读回真消息后让位。
export interface Outgoing {
  // 发出时所在的会话；随这次发送新建会话时为空，受理后换成服务端给的编号。
  conversation: string
  parts: MessagePart[]
  // 受理后才有：触发消息与运行的编号。
  trigger?: string
  run?: string
}

const unnamed = '引用素材'
const deletedObject = '已删除的物件'

function sceneChip(ref: SceneRefDraft): MessagePart {
  const names = ref.objectIDs.map((_, i) => ref.names[i] || '未命名')
  return {
    kind: 'chip',
    title: sceneRefTitle(ref),
    scene: true,
    sceneTitle: ref.title || '未命名场景',
    detail: names.length ? names.join('、') : '整个场景',
  }
}

// scenes 是正文里的场景引用，按出现顺序，与 scene_ref 片段一一对应。
export function outgoingParts(
  segments: Segment[],
  titleOf: (revisionID: string) => string | undefined,
  skill?: MessageSkill,
  scenes: SceneRefDraft[] = [],
): MessagePart[] {
  let scene = 0
  return segments.map((segment): MessagePart => {
    switch (segment.type) {
      case 'scene_ref': {
        const ref = scenes[scene++]
        return sceneChip(
          ref ?? {
            documentID: segment.document_id ?? '',
            title: '',
            objectIDs: segment.object_ids ?? [],
            names: [],
          },
        )
      }
      case 'skill_ref':
        return skill ? skillChip(skill) : { kind: 'chip', title: 'Skill' }
      case 'content_ref':
        return contentChip(segment.content_revision_id, titleOf)
      case 'command':
        return {
          kind: 'command',
          command: segment.command ?? '',
          ...(segment.text ? { text: segment.text } : {}),
        }
      default:
        return { kind: 'text', text: segment.text ?? '' }
    }
  })
}

export function messageParts(
  blocks: Block[],
  titleOf: (revisionID: string) => string | undefined,
): MessagePart[] {
  return blocks.map((block): MessagePart => {
    if (block.type === 'command')
      return { kind: 'command', command: block.command ?? '', text: block.text }
    if (block.type === 'text') return { kind: 'text', text: block.text ?? '' }
    if (block.type === 'scene_selection')
      return {
        kind: 'scene',
        names: block.scene?.objects.map((object) => object.name) ?? [],
      }
    // 场景引用显示发送时冻结的名称；发送时已经不在场景里的物件显示为「已删除的物件」。
    if (block.type === 'scene_ref' && block.scene)
      return sceneChip({
        documentID: block.scene.document_id,
        title: block.scene.title,
        objectIDs: block.scene.objects.map((object) => object.id),
        names: block.scene.objects.map((object) =>
          object.missing ? deletedObject : object.name,
        ),
      })
    if (block.skill) return skillChip(block.skill)
    return contentChip(block.ref_id, titleOf)
  })
}

function skillChip({ skill_id, skill_version_id, display_name, version_number }: MessageSkill): MessagePart {
  return {
    kind: 'chip',
    title: display_name,
    skill: { skill_id, skill_version_id, display_name, version_number },
  }
}

function contentChip(
  revisionID: string | undefined,
  titleOf: (id: string) => string | undefined,
): MessagePart {
  return {
    kind: 'chip',
    title: (revisionID && titleOf(revisionID)) || unnamed,
    ...(revisionID && { revisionID }),
  }
}

// 服务端受理了：记下触发消息和运行，会话换成服务端给的那个。
export function acceptOutgoing(
  outgoing: Outgoing,
  run: { id: string; conversation_id: string; trigger_message_id: string },
): Outgoing {
  return {
    ...outgoing,
    conversation: run.conversation_id,
    trigger: run.trigger_message_id,
    run: run.id,
  }
}

// 当前会话里要不要画这条：会话对得上，且读回的消息里还没有它。
export function outgoingIn(
  outgoing: Outgoing | null,
  conversation: string,
  thread: { id: string }[],
): Outgoing | null {
  if (!outgoing || outgoing.conversation !== conversation) return null
  if (
    outgoing.trigger &&
    thread.some((message) => message.id === outgoing.trigger)
  )
    return null
  return outgoing
}

// 插队或补充：写进一个已有的运行，不是一条新消息。
export function isRunInput(chain: PendingAgentChain): boolean {
  return /\/(steers|supplements)$/.test(chain.steps[0]?.path ?? '')
}

// 发送完要重读哪些：往已有会话里发出一个运行，只重读这个会话的消息和运行；
// 新建了会话，或回执不是运行，就连会话列表一起重读。插队和补充由运行流里的
// input 块画出来，不用重读。
export function refreshAfterSend(
  chain: PendingAgentChain,
  reply: unknown,
): 'none' | 'thread' | 'all' {
  if (isRunInput(chain)) return 'none'
  const created = chain.steps.some((step) =>
    step.path.endsWith('/conversations'),
  )
  const isRun =
    typeof reply === 'object' && reply !== null && 'trigger_message_id' in reply
  return !created && isRun ? 'thread' : 'all'
}
