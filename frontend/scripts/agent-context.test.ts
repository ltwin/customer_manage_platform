import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  commandSegment,
  commandSuggestions,
  completeCommand,
  latestUsage,
  parseCommand,
  ringView,
  tokenLabel,
  type ContextUsage,
} from '../src/creative-canvas/editor/agentContext.ts'
import {
  applyEvent,
  currentStep,
  fromSnapshot,
  mergeSnapshot,
  renderable,
  type AgentBlock,
  type RunEvent,
} from '../src/creative-canvas/editor/agentBlocks.ts'
import type { AgentRun } from '../src/creative-canvas/editor/agentState.ts'

const usage = (over: Partial<ContextUsage> = {}): ContextUsage => ({
  model_key: 'flash',
  window_tokens: 100_000,
  compact_at_tokens: 80_000,
  used_tokens: 62_000,
  estimated: false,
  parts: [
    { key: 'system', tokens: 2_000 },
    { key: 'messages', tokens: 60_000 },
  ],
  measured_at: '2026-10-14T10:00:00Z',
  ...over,
})
const flash = { model_key: 'flash', context_window_tokens: 100_000 }

test('a whole /compact is a command with its focus; anything else is words', () => {
  assert.deepEqual(parseCommand('/compact'), { name: 'compact', args: '' })
  assert.deepEqual(parseCommand('  /compact  保留配色讨论 '), {
    name: 'compact',
    args: '保留配色讨论',
  })
  assert.equal(parseCommand('/compacted'), null)
  assert.equal(parseCommand('/clear'), null)
  assert.equal(parseCommand('请 /compact'), null)
  assert.deepEqual(commandSegment({ name: 'compact', args: '' }), {
    type: 'command',
    command: 'compact',
  })
  assert.deepEqual(commandSegment({ name: 'compact', args: '配色' }), {
    type: 'command',
    command: 'compact',
    text: '配色',
  })
})

test('a slash opens the menu, and Enter completes a partial name', () => {
  assert.deepEqual(
    commandSuggestions('/').map((c) => c.name),
    ['compact'],
  )
  assert.deepEqual(
    commandSuggestions('/co').map((c) => c.name),
    ['compact'],
  )
  assert.deepEqual(commandSuggestions('/x'), [])
  assert.deepEqual(commandSuggestions('/compact 配色'), [])
  assert.deepEqual(commandSuggestions('你好'), [])
  assert.equal(completeCommand('/co'), '/compact ')
  assert.equal(completeCommand('/'), '/compact ')
  // Already whole: Enter sends it rather than completing it again.
  assert.equal(completeCommand('/compact'), null)
  assert.equal(completeCommand('/compact 配色'), null)
})

test('a conversation that never sent anything shows an empty ring, not a guess', () => {
  const view = ringView(null, flash, 80)
  assert.equal(view.empty, true)
  assert.equal(view.percent, 0)
  assert.deepEqual(view.parts, [])
  assert.equal(view.window, 100_000)
})

test('the ring measures against the selected model and says when it guesses', () => {
  const view = ringView(usage(), flash, 80)
  assert.equal(view.empty, false)
  assert.equal(view.percent, 62)
  assert.equal(view.estimated, false)
  assert.equal(view.level, 'warm')
  assert.equal(view.remaining, 38_000)
  assert.equal(view.untilCompact, 18_000)
  assert.deepEqual(
    view.parts.map((p) => [p.label, p.percent]),
    [
      ['系统指令', 2],
      ['对话消息', 60],
    ],
  )
  // Another model counts differently and has its own window.
  const switched = ringView(
    usage(),
    { model_key: 'pro', context_window_tokens: 200_000 },
    80,
  )
  assert.equal(switched.estimated, true)
  assert.equal(switched.percent, 31)
  assert.equal(switched.level, 'calm')
  // After a compaction the total itself is a projection.
  assert.equal(ringView(usage({ estimated: true }), flash, 80).estimated, true)
})

test('the ring warns from the compaction line on, and only if it is on', () => {
  const full = usage({ used_tokens: 85_000 })
  assert.equal(ringView(full, flash, 80).level, 'hot')
  assert.equal(ringView(full, flash, 80).untilCompact, 0)
  assert.equal(ringView(full, flash, 0).level, 'warm')
  assert.equal(ringView(full, flash, 0).untilCompact, null)
  assert.equal(
    ringView(usage({ used_tokens: 10_000 }), flash, 80).level,
    'calm',
  )
})

test('the newest measurement wins whichever source it came from', () => {
  const page = usage({ used_tokens: 1, measured_at: '2026-10-14T10:00:00Z' })
  const event = usage({ used_tokens: 2, measured_at: '2026-10-14T10:05:00Z' })
  assert.equal(latestUsage([page, event, null, undefined])?.used_tokens, 2)
  assert.equal(latestUsage([null, undefined]), null)
})

test('token counts read in 万 from ten thousand on', () => {
  assert.equal(tokenLabel(8_500), '8,500')
  assert.equal(tokenLabel(124_000), '12.4 万')
})

const run: AgentRun = {
  id: 'run',
  kind: 'chat',
  conversation_id: 'conversation',
  canvas_id: 'canvas',
  trigger_message_id: 'message',
  egress_consent_id: 'consent',
  model_key: 'flash',
  state: 'running',
  settlement_state: 'pending',
  limits_version: '1',
  revision: '1',
  last_event_seq: '1',
  pruned_through_seq: '0',
  deadline_at: '2026-10-14T11:00:00Z',
  created_at: '2026-10-14T10:00:00Z',
}

test('a usage event is folded in without a snapshot, and a snapshot keeps it', () => {
  const view = fromSnapshot({ ...run, blocks: [], snapshot_seq: '1' })
  const event = {
    schema_version: 1,
    run_id: 'run',
    seq: '2',
    type: 'context.usage',
    created_at: '2026-10-14T10:01:00Z',
    payload: usage(),
  } as RunEvent
  const next = applyEvent(view, event)
  assert.ok(typeof next === 'object')
  assert.equal(next.usage?.used_tokens, 62_000)
  assert.equal(next.seq, '2')
  const merged = mergeSnapshot(next, { ...run, blocks: [], snapshot_seq: '3' })
  assert.equal(merged.usage?.used_tokens, 62_000)
})

test('a compaction block renders and names what the run is doing', () => {
  const block: AgentBlock = {
    id: 'ccbk_1',
    run_id: 'run',
    ordinal: '1',
    type: 'compaction',
    state: 'running',
    revision: '1',
    payload_version: 1,
    hidden: false,
    payload: { trigger: 'auto', tokens_before: 90_000 },
    started_at: '2026-10-14T10:00:00Z',
  }
  assert.equal(renderable(block), true)
  const view = {
    ...fromSnapshot({ ...run, blocks: [block], snapshot_seq: '1' }),
  }
  assert.equal(currentStep(view), '正在整理上下文')
})
