import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  joinLatest,
  openWindow,
  prependTurns,
  readSince,
  readTurns,
  turnPageSize,
  type ThreadMessage,
} from '../src/creative-canvas/editor/agentThread.ts'

type Role = 'user' | 'assistant' | 'tool'

// 按顺序给出每条消息的角色和运行，序号从 1 起。
function conversation(...turns: [string, Role[]][]): ThreadMessage[] {
  const all: ThreadMessage[] = []
  for (const [run, roles] of turns)
    for (const role of roles) {
      const ordinal = String(all.length + 1)
      all.push({
        id: `m${ordinal}`,
        ordinal,
        role,
        run_id: run,
        status: 'complete',
        body: { blocks: [{ type: 'text', text: `${run}:${role}:${ordinal}` }] },
      } as ThreadMessage)
    }
  return all
}

// 与后端分页一致：从最新往回，不含 before，每页按阅读顺序。
function server(messages: ThreadMessage[]) {
  const calls: { before: string; limit: number; read: number }[] = []
  const read = (before: string, limit: number) => {
    const older = messages.filter(
      (m) => !before || BigInt(m.ordinal) < BigInt(before),
    )
    const items = older.slice(-limit)
    calls.push({ before, limit, read: items.length })
    const more = older.length > limit
    return Promise.resolve({
      items,
      prev_cursor: more ? items[0].ordinal : '',
      conversation_revision: '1',
    })
  }
  return { read, calls }
}

const toolRun = (calls: number): Role[] => [
  'user',
  ...Array.from(
    { length: calls },
    () => ['assistant', 'tool'] as Role[],
  ).flat(),
  'assistant',
]

test('a run with many tool calls still opens on its question', async () => {
  // 线上出问题的形态：一轮 37 次工具调用，共 76 条，提问在第 1 条。
  const messages = conversation(['run', toolRun(37)])
  const { read, calls } = server(messages)
  const window = openWindow('c', await readTurns(read))
  assert.equal(window.items[0].role, 'user')
  assert.equal(window.items[0].ordinal, '1')
  assert.equal(window.before, '', 'the whole conversation was read')
  assert.equal(calls.length, 1, 'one page of the API maximum covers it')
  assert.ok(
    window.items.every((m) => m.role !== 'tool'),
    'tool results are not kept',
  )
  assert.equal(window.newest, '76')
})

test('a turn longer than one page keeps reading until its question', async () => {
  const messages = conversation(['old', toolRun(60)], ['run', toolRun(80)])
  const { read, calls } = server(messages)
  const window = openWindow('c', await readTurns(read))
  assert.equal(window.items[0].run_id, 'run')
  assert.equal(window.items[0].role, 'user')
  // 下次从已经读到的最早一条往前接，不重读开头之前那半轮。
  assert.equal(window.before, window.partial[0].ordinal)
  assert.ok(window.partial.every((m) => m.run_id === 'old'))
  assert.equal(calls.length, 2)
  assert.deepEqual(
    calls.map((c) => c.limit),
    [turnPageSize, turnPageSize],
  )
})

test('a supplement inside a run is not mistaken for the start of a turn', async () => {
  // 等待补充时摄影师说的话也是 user 消息，属于同一次运行。
  const filler = Array.from(
    { length: 60 },
    () => ['assistant', 'tool'] as Role[],
  ).flat()
  const messages = conversation(
    ['first', toolRun(60)],
    ['run', ['user', ...filler, 'user', ...filler, 'assistant']],
  )
  const { read } = server(messages)
  const window = openWindow('c', await readTurns(read))
  assert.equal(
    window.items[0].ordinal,
    '123',
    'the trigger, not the supplement',
  )
  assert.equal(window.items.filter((m) => m.role === 'user').length, 2)
})

test('older turns are prepended whole and without duplicates', async () => {
  const turns: [string, Role[]][] = Array.from({ length: 12 }, (_, i) => [
    `r${i}`,
    toolRun(10),
  ])
  const messages = conversation(...turns)
  const { read, calls } = server(messages)
  let window = openWindow('c', await readTurns(read))
  const firstRun = window.items[0].run_id
  assert.notEqual(firstRun, 'r0')
  while (window.before)
    window = prependTurns(window, await readTurns(read, window))
  const visible = messages.filter((m) => m.role !== 'tool')
  assert.deepEqual(
    window.items.map((m) => m.id),
    visible.map((m) => m.id),
  )
  // 一轮开头之前顺带读到的半轮留着接上，每条消息只下载一次。
  assert.equal(
    calls.reduce((sum, call) => sum + call.read, 0),
    messages.length,
  )
  assert.deepEqual(window.partial, [])
})

test('a refresh joins the new tail and keeps what was paged in', async () => {
  const messages = conversation(
    ...Array.from({ length: 8 }, (_, i): [string, Role[]] => [
      `r${i}`,
      toolRun(10),
    ]),
  )
  const before = server(messages)
  let window = openWindow('c', await readTurns(before.read))
  while (window.before)
    window = prependTurns(window, await readTurns(before.read, window))
  const paged = window.items.length
  // 刷新前最后一条还在流式输出，之后新开了一轮。
  const updated = messages.map((m) =>
    m.id === `m${messages.length}` ? { ...m, status: 'complete' } : m,
  )
  updated[updated.length - 1] = {
    ...updated[updated.length - 1],
    body: { blocks: [{ type: 'text', text: 'final' }] },
  }
  const grown = [
    ...updated,
    ...conversation(['next', toolRun(2)]).map((m) => ({
      ...m,
      id: `n${m.id}`,
      ordinal: String(messages.length + Number(m.ordinal)),
    })),
  ]
  const after = server(grown)
  window = joinLatest(window, await readSince(after.read, window.newest))
  assert.equal(after.calls[0].limit, 30, 'a refresh starts with a small page')
  assert.equal(after.calls.length, 1)
  assert.equal(window.items.length, paged + 4)
  assert.equal(window.before, '')
  assert.equal(
    window.items.find((m) => m.id === `m${messages.length}`)?.body.blocks[0]
      .text,
    'final',
  )
  assert.equal(window.newest, String(grown.length))
})

test('a refresh far behind keeps reading until it meets what was read', async () => {
  const messages = conversation(['a', toolRun(3)], ['b', toolRun(3)])
  const { read } = server(messages)
  const window = openWindow('c', await readTurns(read))
  const grown = [
    ...messages,
    ...conversation(['c', toolRun(40)]).map((m) => ({
      ...m,
      id: `n${m.id}`,
      ordinal: String(messages.length + Number(m.ordinal)),
    })),
  ]
  const after = server(grown)
  const joined = joinLatest(window, await readSince(after.read, window.newest))
  assert.equal(after.calls.length, 2)
  assert.deepEqual(
    joined.items.map((m) => m.id),
    grown.filter((m) => m.role !== 'tool').map((m) => m.id),
  )
})

test('a refresh does not pull half a turn in front of the paged window', async () => {
  // 窗口从某一轮开头起；刷新读到的一小页比它还早时，早出来的半轮不收。
  const messages = conversation(
    ['a', toolRun(60)],
    ['b', ['user', 'assistant']],
  )
  const { read } = server(messages)
  const window = openWindow('c', await readTurns(read))
  assert.equal(window.items[0].run_id, 'b')
  const joined = joinLatest(window, [...messages.slice(-30)])
  assert.equal(joined.items[0].run_id, 'b')
  assert.equal(joined.before, window.before)
})

test('an empty conversation reads once and has nothing before it', async () => {
  const { read, calls } = server([])
  const window = openWindow('c', await readTurns(read))
  assert.deepEqual(window.items, [])
  assert.equal(window.before, '')
  assert.equal(window.newest, '')
  assert.equal(calls.length, 1)
})
