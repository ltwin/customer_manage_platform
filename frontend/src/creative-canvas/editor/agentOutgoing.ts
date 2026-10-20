import type { components } from '../../api/schema'
import type { PendingAgentChain } from './agentState.ts'

type Segment = components['schemas']['CreativeAgentInstructionSegment']
type Block =
  components['schemas']['CreativeAgentMessage']['body']['blocks'][number]

// 用户消息的一段：文字、引用（Skill 或素材）、命令，或从 3D 场景发送时选中的物体
// （names 按选中顺序，未命名的为空串）。已存的消息和刚发出的消息都画成这样。
export type MessagePart =
  | { kind: 'text'; text: string }
  | { kind: 'chip'; title: string }
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

export function outgoingParts(
  segments: Segment[],
  titleOf: (revisionID: string) => string | undefined,
  skillName?: string,
  sceneNames: string[] = [],
): MessagePart[] {
  const parts = segments.map((segment): MessagePart => {
    switch (segment.type) {
      case 'skill_ref':
        return { kind: 'chip', title: skillName ?? 'Skill' }
      case 'content_ref':
        return {
          kind: 'chip',
          title: titleOf(segment.content_revision_id ?? '') ?? unnamed,
        }
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
  // 服务端把选中的物体存在消息末尾，这里照样放。
  return sceneNames.length
    ? [...parts, { kind: 'scene', names: sceneNames }]
    : parts
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
    return {
      kind: 'chip',
      title:
        block.skill?.display_name ?? titleOf(block.ref_id ?? '') ?? unnamed,
    }
  })
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

// 发送完要重读哪些：往已有会话里发出一个运行，只重读这个会话的消息和运行；
// 新建了会话，或回执不是运行，就连会话列表一起重读。
export function refreshAfterSend(
  chain: PendingAgentChain,
  reply: unknown,
): 'thread' | 'all' {
  const created = chain.steps.some((step) =>
    step.path.endsWith('/conversations'),
  )
  const isRun =
    typeof reply === 'object' && reply !== null && 'trigger_message_id' in reply
  return !created && isRun ? 'thread' : 'all'
}
