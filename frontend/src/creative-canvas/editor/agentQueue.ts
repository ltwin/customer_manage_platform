import type { components } from '../../api/schema'
import type { SceneContext } from '../../creative-scene/context.ts'
import type { MessagePart } from './agentOutgoing.ts'
import type { AgentDraft, AgentRun, PendingAgentChain } from './agentState.ts'

type Segment = components['schemas']['CreativeAgentInstructionSegment']
export type SteerSegment = components['schemas']['CreativeAgentSupplementSegment']

// 运行中写下的一条消息：排队等下一轮，或插进正在跑的这一轮。只存在这个
// 标签页里（owner 2026-11-10），服务端不知道有它。
export type QueuedMessage = {
  id: string
  parts: MessagePart[]
  draft: AgentDraft
  // 作为新一轮发出时原样交给 messageSteps，与当时直接发送的请求一致。
  send: {
    model: string
    segments: Segment[]
    scene: SceneContext | null
  }
  // 插队用的片段；选了 Skill 的消息只能排队（一轮的 Skill 在开始时已定）。
  steer: SteerSegment[] | null
  hasImage: boolean
  // 正在交给待确认链：送达才移出队列，被拒放回队首；刷新时按有没有链来恢复。
  sending?: true
}
export type ConversationQueue = {
  items: QueuedMessage[]
  paused: boolean
  // 已据之处理过的最近一次终态运行，重连、重复快照、刷新都不会再发一次。
  settledRun?: string
}
export const emptyQueue: ConversationQueue = { items: [], paused: false }

export const agentQueueKey = (account: string, conversation: string) =>
  `creative-agent-queue:${account}:${conversation}`

type QueueStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

// 读不出来就当没有：队列只是这个标签页的便利，坏了不能挡住发送。
export function readQueue(storage: QueueStorage | null, key: string): ConversationQueue {
  try {
    const raw = storage?.getItem(key)
    if (!raw) return emptyQueue
    const value: unknown = JSON.parse(raw)
    if (
      typeof value !== 'object' ||
      value === null ||
      !Array.isArray((value as ConversationQueue).items)
    )
      return emptyQueue
    const queue = value as ConversationQueue
    return {
      items: queue.items,
      paused: queue.paused === true,
      ...(typeof queue.settledRun === 'string'
        ? { settledRun: queue.settledRun }
        : {}),
    }
  } catch {
    return emptyQueue
  }
}
export function writeQueue(
  storage: QueueStorage | null,
  key: string,
  queue: ConversationQueue,
) {
  try {
    if (!queue.items.length && !queue.paused && !queue.settledRun)
      storage?.removeItem(key)
    else storage?.setItem(key, JSON.stringify(queue))
  } catch {
    // 存储不可用时只留在内存里。
  }
}

export function enqueue(queue: ConversationQueue, item: QueuedMessage): ConversationQueue {
  return { ...queue, items: [...queue.items, item] }
}
export function removeQueued(queue: ConversationQueue, id: string): ConversationQueue {
  return { ...queue, items: queue.items.filter((item) => item.id !== id) }
}
export function markSending(queue: ConversationQueue, id: string): ConversationQueue {
  return {
    ...queue,
    items: queue.items.map((item) =>
      item.id === id ? { ...item, sending: true as const } : item,
    ),
  }
}
// 明确被拒：放回队首；自动发出的被拒还要暂停，免得一直撞同一堵墙。
export function putBack(
  queue: ConversationQueue,
  item: QueuedMessage,
  pause = false,
): ConversationQueue {
  const { sending: _sending, ...rest } = item
  return {
    ...queue,
    items: [rest, ...queue.items.filter((other) => other.id !== item.id)],
    ...(pause ? { paused: true } : {}),
  }
}

const settled = new Set<AgentRun['state']>(['succeeded'])
const pausing = new Set<AgentRun['state']>(['cancelled', 'failed', 'partial'])

// 运行快照变化时求值：成功结束发出队首一条；停止、失败、中途出错暂停。
// ready 是面板空闲、没有待确认链；没空时下次快照再看。
export function nextQueueAction(
  queue: ConversationQueue,
  latest: AgentRun | undefined,
  ready: boolean,
): { queue: ConversationQueue; send?: QueuedMessage } {
  if (!latest || latest.id === queue.settledRun) return { queue }
  const automatic = latest.trigger_kind === 'background_tool'
  const interrupted = pausing.has(latest.state)
  if (interrupted && !automatic)
    return { queue: { ...queue, paused: true, settledRun: latest.id } }
  if (!settled.has(latest.state) && !interrupted) return { queue }
  const head = queue.items.find((item) => !item.sending)
  if (queue.paused || !head || !ready) return { queue }
  return {
    queue: { ...markSending(queue, head.id), settledRun: latest.id },
    send: head,
  }
}

// 「继续发送」：解除暂停并发出队首。
export function resumeQueue(
  queue: ConversationQueue,
  latestID?: string,
): { queue: ConversationQueue; send?: QueuedMessage } {
  const head = queue.items.find((item) => !item.sending)
  const resumed = { ...queue, paused: false, ...(latestID ? { settledRun: latestID } : {}) }
  return head
    ? { queue: markSending(resumed, head.id), send: head }
    : { queue: resumed }
}

// 刷新后：带 sending 的一条，若待确认链就是它，仍由链负责，照旧显示发送中；
// 没有链说明还没落盘、请求没有发出，它仍在队首，那一轮也要重新算一次，
// 否则成功结束的那一轮已记为处理过，这条就再也不会自动发出。
export function recoverQueue(
  queue: ConversationQueue,
  pending: PendingAgentChain | null,
): ConversationQueue {
  const owner = pending?.draft?.queueItem
  let unsent = false
  const items = queue.items.map((item) => {
    if (!item.sending || item.id === owner) return item
    unsent = true
    const { sending: _sending, ...rest } = item
    return rest
  })
  if (!unsent) return queue
  const { settledRun: _settled, ...rest } = queue
  return { ...rest, items }
}

// 插队按钮为什么不能用；能用时为空。
export function steerBlocked(
  item: QueuedMessage,
  imageInput: boolean,
): string {
  if (!item.steer) return '选了 Skill 的消息只能排队'
  if (item.hasImage && !imageInput) return '当前模型不支持图片，只能排队'
  return ''
}

// 去掉 Skill 的片段才能插队；有 Skill 时整条只能排队。引用之间只隔着空格时，
// 那段空白不单独成段：插队和补充不收空白的文字片段。
export function steerSegments(segments: Segment[]): SteerSegment[] | null {
  if (segments.some((segment) => segment.type !== 'text' && segment.type !== 'content_ref'))
    return null
  return segments.flatMap((segment): SteerSegment[] =>
    segment.type === 'text'
      ? segment.text?.trim()
        ? [{ type: 'text', text: segment.text }]
        : []
      : [{ type: 'content_ref', content_revision_id: segment.content_revision_id ?? '' }],
  )
}
