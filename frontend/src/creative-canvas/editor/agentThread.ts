import type { components } from '../../api/schema'

type MessagePage = components['schemas']['CreativeAgentMessagePage']
export type ThreadMessage = MessagePage['items'][number]

// 往回读到的整轮。
export type Turns = {
  // 从某一轮的提问起，或者从会话开头起。
  messages: ThreadMessage[]
  // 再往前读的游标，空表示已经读到会话开头。
  before: string
  // 那一轮开头之前顺带读到的半轮，下次往前读时直接接上，不再重复下载。
  partial: ThreadMessage[]
}

// 读出来的一段会话：从某一轮的提问开始，一直到最新。
export type ThreadWindow = {
  conversation: string
  // 只留面板会画的提问和回答；工具结果占了大部分字节，读完就丢。
  items: ThreadMessage[]
  before: string
  partial: ThreadMessage[]
  // 读到过的最新序号（含工具消息），刷新时据此判断新读的能否接上。
  newest: string
}

export type PageReader = (
  before: string,
  limit: number,
) => Promise<MessagePage>

// 接口单页上限。一轮运行常有几十次工具调用，按上限读通常一次就能读到这一轮的提问。
export const turnPageSize = 100
// 刷新时多半只多了几条，先读一小页。
export const refreshPageSize = 30

const ordinal = (message: ThreadMessage) => BigInt(message.ordinal)

// 一轮从它的提问开始：提问之后同一运行里的补充也是 user 消息，但前一条属于同一运行。
// 返回最早的一轮开头（不含第 0 条，它前面是什么还不知道），没有则为 -1。
export function turnHead(messages: ThreadMessage[]): number {
  return messages.findIndex(
    (message, index) =>
      index > 0 &&
      message.role === 'user' &&
      (message.run_id ?? '') !== (messages[index - 1].run_id ?? ''),
  )
}

function visible(messages: ThreadMessage[]): ThreadMessage[] {
  return messages.filter(
    (message) => message.role === 'user' || message.role === 'assistant',
  )
}

// 往回读整轮：读到某一轮的开头或会话开头为止，不把半轮交给面板。
// from 是上次停下的位置；不给则从最新读起。
export async function readTurns(
  read: PageReader,
  from?: Pick<Turns, 'before' | 'partial'>,
): Promise<Turns> {
  let messages = from?.partial ?? []
  let cursor = from?.before ?? ''
  for (;;) {
    const page = await read(cursor, turnPageSize)
    messages = [...page.items, ...messages]
    if (!page.prev_cursor) return { messages, before: '', partial: [] }
    const head = turnHead(messages)
    if (head > 0)
      return {
        messages: messages.slice(head),
        before: page.prev_cursor,
        partial: messages.slice(0, head),
      }
    cursor = page.prev_cursor
  }
}

// 刷新：从最新往回读到与已读部分接上为止，已经往前翻出来的历史不丢。
export async function readSince(
  read: PageReader,
  newest: string,
): Promise<ThreadMessage[]> {
  const known = BigInt(newest) + 1n
  let messages: ThreadMessage[] = []
  let cursor = ''
  let limit = refreshPageSize
  for (;;) {
    const page = await read(cursor, limit)
    messages = [...page.items, ...messages]
    if (!page.prev_cursor || !messages.length || ordinal(messages[0]) <= known)
      return messages
    cursor = page.prev_cursor
    limit = turnPageSize
  }
}

export function openWindow(conversation: string, turns: Turns): ThreadWindow {
  return {
    conversation,
    items: visible(turns.messages),
    before: turns.before,
    partial: turns.partial,
    newest: turns.messages.at(-1)?.ordinal ?? '',
  }
}

// 新读到的从它最早一条起替换已读部分（状态、内容可能变了），更早的原样保留。
export function joinLatest(
  window: ThreadWindow,
  fresh: ThreadMessage[],
): ThreadWindow {
  if (!fresh.length) return window
  const from = ordinal(fresh[0])
  // 已读部分从某一轮开头起；新读到的比它更早的是半轮，不收。
  const start =
    window.before && window.items.length ? ordinal(window.items[0]) : null
  const kept = fresh.filter(
    (message) => start === null || ordinal(message) >= start,
  )
  const last = fresh.at(-1)?.ordinal ?? ''
  return {
    ...window,
    items: [
      ...window.items.filter((message) => ordinal(message) < from),
      ...visible(kept),
    ],
    newest:
      window.newest && BigInt(window.newest) > BigInt(last)
        ? window.newest
        : last,
  }
}

// 往前翻出来的整轮接在最前面。
export function prependTurns(window: ThreadWindow, older: Turns): ThreadWindow {
  const first = window.items[0]
  return {
    ...window,
    items: [
      ...visible(older.messages).filter(
        (message) => !first || ordinal(message) < ordinal(first),
      ),
      ...window.items,
    ],
    before: older.before,
    partial: older.partial,
  }
}
