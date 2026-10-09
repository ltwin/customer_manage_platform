import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  applyEvent,
  currentStep,
  fromSnapshot,
  layout,
  mergeSnapshot,
  renderable,
  segmentOpen,
  segmentSummary,
  type AgentBlock,
  type RunEvent,
  type RunView,
} from '../src/creative-canvas/editor/agentBlocks.ts'
import type { AgentRun } from '../src/creative-canvas/editor/agentState.ts'
import {
  parseInline,
  parseMarkdown,
} from '../src/creative-canvas/editor/markdown.ts'

const run: AgentRun = {
  id: 'run',
  conversation_id: 'conversation',
  canvas_id: 'canvas',
  trigger_message_id: 'message',
  egress_consent_id: 'consent',
  model_key: 'model',
  state: 'running',
  settlement_state: 'pending',
  limits_version: '1',
  revision: '1',
  last_event_seq: '0',
  pruned_through_seq: '0',
  deadline_at: '2026-10-09T01:00:00Z',
  created_at: '2026-10-09T00:00:00Z',
}
function block(
  ordinal: number,
  type: string,
  state: string,
  payload: Record<string, unknown> = {},
  more: Partial<AgentBlock> = {},
): AgentBlock {
  return {
    id: `ccbk_${ordinal}`,
    run_id: 'run',
    ordinal: String(ordinal),
    type,
    state,
    revision: '1',
    payload_version: 1,
    hidden: false,
    payload,
    started_at: `2026-10-09T00:00:${String(ordinal).padStart(2, '0')}Z`,
    ...more,
  }
}
function event(seq: number, type: string, payload: object): RunEvent {
  return {
    schema_version: 1,
    run_id: 'run',
    seq: String(seq),
    type,
    created_at: '2026-10-09T00:00:00Z',
    payload: payload as RunEvent['payload'],
  }
}
function fold(view: RunView, events: RunEvent[]): RunView {
  for (const item of events) {
    const next = applyEvent(view, item)
    assert.equal(typeof next, 'object', `event ${item.seq} must merge`)
    view = next as RunView
  }
  return view
}
const empty = (): RunView => fromSnapshot({ ...run, blocks: [], snapshot_seq: '0' })

test('events rebuild the list the snapshot would return', () => {
  const view = fold(empty(), [
    event(1, 'block.created', block(1, 'text', 'streaming', { markdown: '' })),
    event(2, 'block.updated', {
      block_id: 'ccbk_1',
      revision: '2',
      patch: { append: { path: 'markdown', text: '你好' } },
    }),
    event(3, 'block.updated', {
      block_id: 'ccbk_1',
      revision: '3',
      patch: { append: { path: 'markdown', text: '，世界' } },
    }),
    event(4, 'block.created', block(2, 'tool', 'running', { title: '读取画布' })),
    event(5, 'block.updated', {
      block_id: 'ccbk_1',
      revision: '4',
      state: 'complete',
      finished_at: '2026-10-09T00:00:09Z',
      payload_version: 1,
      patch: { set: { markdown: '你好，世界。' } },
    }),
  ])
  assert.equal(view.seq, '5')
  assert.deepEqual(view.blocks, [
    block(1, 'text', 'complete', { markdown: '你好，世界。' }, {
      revision: '4',
      finished_at: '2026-10-09T00:00:09Z',
    }),
    block(2, 'tool', 'running', { title: '读取画布' }),
  ])
})

test('an untouched block keeps its identity so only the changed one re-renders', () => {
  const before = fold(empty(), [
    event(1, 'block.created', block(1, 'tool', 'succeeded')),
    event(2, 'block.created', block(2, 'text', 'streaming', { markdown: '' })),
  ])
  const after = fold(before, [
    event(3, 'block.updated', {
      block_id: 'ccbk_2',
      revision: '2',
      patch: { append: { path: 'markdown', text: 'a' } },
    }),
  ])
  assert.equal(after.blocks[0], before.blocks[0])
  assert.notEqual(after.blocks[1], before.blocks[1])
  assert.equal(after.run, before.run)
})

test('a gap, a skipped revision or an unknown block asks for the snapshot', () => {
  const view = fold(empty(), [
    event(1, 'block.created', block(1, 'text', 'streaming', { markdown: '' })),
  ])
  const update = { block_id: 'ccbk_1', revision: '2', state: 'complete' }
  assert.equal(applyEvent(view, event(3, 'block.updated', update)), 'resync')
  assert.equal(
    applyEvent(view, event(2, 'block.updated', { ...update, revision: '3' })),
    'resync',
  )
  assert.equal(
    applyEvent(view, event(2, 'block.updated', { ...update, block_id: 'ccbk_9' })),
    'resync',
  )
  assert.equal(
    applyEvent(view, event(2, 'block.created', block(1, 'text', 'streaming'))),
    'resync',
  )
  // Replayed events are already in the list.
  assert.equal(applyEvent(view, event(1, 'block.created', block(1, 'x', 'y'))), view)
  assert.equal(
    applyEvent(view, { ...event(2, 'block.updated', update), schema_version: 2 as 1 }),
    'outdated',
  )
})

test('run events move the state; a late snapshot fills in the rest', () => {
  const live = fold(empty(), [
    event(1, 'block.created', block(1, 'text', 'streaming', { markdown: '' })),
    event(2, 'run.state_changed', { state: 'waiting_input' }),
    event(3, 'block.updated', {
      block_id: 'ccbk_1',
      revision: '2',
      patch: { append: { path: 'markdown', text: 'a' } },
    }),
  ])
  assert.equal(live.run.state, 'waiting_input')
  assert.equal(live.runSeq, '2')
  // Read after the state change but before the last append: run fields only.
  const behind = mergeSnapshot(live, {
    ...run,
    state: 'waiting_input',
    waiting_token: 'token',
    blocks: [block(1, 'text', 'streaming', { markdown: '' })],
    snapshot_seq: '2',
  })
  assert.equal(behind.run.waiting_token, 'token')
  assert.equal(behind.blocks, live.blocks)
  assert.equal(behind.seq, '3')
  // Read before the state change: it knows nothing we do not.
  assert.equal(mergeSnapshot(live, { ...run, blocks: [], snapshot_seq: '1' }), live)
  const ahead = mergeSnapshot(live, { ...run, blocks: [], snapshot_seq: '9' })
  assert.equal(ahead.seq, '9')
  assert.deepEqual(ahead.blocks, [])
})

test('adjacent process blocks form one segment; a conclusion block ends it', () => {
  const blocks = [
    block(1, 'thinking', 'complete'),
    block(2, 'tool', 'succeeded'),
    block(3, 'tool', 'succeeded', {}, { hidden: true }),
    block(4, 'plan', 'active'),
    block(5, 'text', 'complete'),
    block(6, 'tool', 'failed'),
    block(7, 'text', 'streaming'),
    block(8, 'tool', 'running'),
    block(9, 'tool', 'running'),
  ]
  const items = layout(blocks)
  assert.deepEqual(
    items.map((item) =>
      item.kind === 'segment'
        ? [item.id, item.blocks.map((b) => b.ordinal).join()]
        : item.block.ordinal,
    ),
    [['ccbk_1', '1,2,4'], '5', '6', '7', ['ccbk_8', '8,9']],
  )
  // A segment grows at its tail and keeps the name its open state hangs on.
  const grown = layout([...blocks, block(10, 'tool', 'running')])
  assert.equal(grown.at(-1)?.kind === 'segment' && grown.at(-1)?.id, 'ccbk_8')
})

test('a folded segment reports its own time, operations and failures', () => {
  const tool = (ordinal: number, state: string, finished?: string) =>
    block(ordinal, 'tool', state, {}, finished ? { finished_at: finished } : {})
  assert.equal(
    segmentSummary([
      block(1, 'thinking', 'complete', {}, { finished_at: '2026-10-09T00:00:02Z' }),
      tool(2, 'succeeded', '2026-10-09T00:00:05Z'),
      tool(3, 'failed', '2026-10-09T00:00:13Z'),
    ]),
    '已处理 12 秒 · 2 次操作 · 1 项失败',
  )
  assert.equal(
    segmentSummary([
      tool(1, 'succeeded', '2026-10-09T00:00:02Z'),
      tool(2, 'interrupted', '2026-10-09T00:00:03Z'),
    ]),
    '已处理 2 秒 · 2 次操作 · 已停止',
  )
  assert.equal(segmentOpen(undefined, false), true)
  assert.equal(segmentOpen(undefined, true), false)
  assert.equal(segmentOpen(true, true), true)
})

test('only what this page understands is rendered as itself', () => {
  assert.equal(renderable(block(1, 'text', 'complete')), true)
  assert.equal(renderable(block(1, 'text', 'complete', {}, { payload_version: 2 })), false)
  assert.equal(renderable(block(1, 'question', 'waiting')), false)
})

test('the status row names the step in progress', () => {
  const view = (blocks: AgentBlock[]) => ({ ...empty(), blocks })
  assert.equal(currentStep(view([])), '正在思考')
  assert.equal(
    currentStep(view([block(1, 'tool', 'running', { title: '读取画布' })])),
    '读取画布',
  )
  assert.equal(currentStep(view([block(1, 'text', 'streaming')])), '正在回复')
})

test('markdown covers what the assistant writes', () => {
  assert.deepEqual(
    parseMarkdown('## 方案\n先看 **主光**，再定 `f/2.8`。\n第二行\n\n1. 清晨\n2. 逆光\n\n- a\n- b\n\n```\nx = 1\n```'),
    [
      { type: 'heading', level: 2, content: ['方案'] },
      {
        type: 'paragraph',
        content: ['先看 ', { bold: ['主光'] }, '，再定 ', { code: 'f/2.8' }, '。\n第二行'],
      },
      { type: 'list', ordered: true, start: 1, items: [['清晨'], ['逆光']] },
      { type: 'list', ordered: false, start: 1, items: [['a'], ['b']] },
      { type: 'code', text: 'x = 1' },
    ],
  )
})

test('half-written markers stay plain text until they close', () => {
  assert.deepEqual(parseInline('这是 **重点'), ['这是 **重点'])
  assert.deepEqual(parseInline('这是 **重点**'), ['这是 ', { bold: ['重点'] }])
  assert.deepEqual(parseInline('用 `f/2'), ['用 `f/2'])
  assert.deepEqual(parseInline('<b>x</b>'), ['<b>x</b>'])
  // An open fence already reads as code, so the block does not jump on close.
  assert.deepEqual(parseMarkdown('```\nx = 1'), [{ type: 'code', text: 'x = 1' }])
})
