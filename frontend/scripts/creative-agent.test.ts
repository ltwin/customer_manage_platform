import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  agentAction,
  consentedRunAction,
  replacePendingAction,
  pendingAgentAction,
  isActiveRun,
  hasUnsettledCost,
  pollDelay,
} from '../src/creative-canvas/editor/agentState.ts'
import type { AgentRun } from '../src/creative-canvas/editor/agentState.ts'

test('uncertain submissions retain the exact operation and bytes across refresh', () => {
  const body = JSON.stringify({
    operation_id: 'operation-one',
    client_created_at: '2026-09-17T00:00:00Z',
    payload: { text: '保留原文' },
  })
  const pending = {
    path: '/agent-runs/run-one/supplements',
    operation: 'operation-one',
    body,
  }
  assert.deepEqual(pendingAgentAction(JSON.stringify(pending)), pending)
  assert.throws(() =>
    pendingAgentAction(JSON.stringify({ ...pending, operation: 'different' })),
  )
  assert.throws(() =>
    pendingAgentAction(JSON.stringify({ ...pending, path: '/unrelated' })),
  )
})
test('consent recovery retains the frozen model, skill and original question', () => {
  const pending = {
    path: '/conversations/conversation-one/egress-consents',
    operation: 'operation-one',
    body: JSON.stringify({
      operation_id: 'operation-one',
      client_created_at: '2026-09-17T00:00:00Z',
      payload: {},
    }),
    nextOperation: 'stable-run-operation',
    nextCreatedAt: '2026-09-17T00:00:00Z',
    nextRun: {
      model_key: 'fixed',
      egress_consent_id: '',
      instruction: {
        schema_version: 1,
        instruction_segments: [
          { type: 'text', text: '原问题' },
          { type: 'skill_ref', skill_id: 'skill', skill_version_id: 'version' },
        ],
      },
    },
  }
  assert.deepEqual(pendingAgentAction(JSON.stringify(pending)), pending)
  assert.throws(() =>
    pendingAgentAction(
      JSON.stringify({ ...pending, nextRun: { model_key: 'fixed' } }),
    ),
  )
})
test('terminal execution and unresolved money remain separate', () => {
  const run: AgentRun = {
    id: 'run',
    conversation_id: 'c',
    canvas_id: 'canvas',
    trigger_message_id: 'm',
    egress_consent_id: 'consent',
    model_key: 'model',
    state: 'cancelled',
    settlement_state: 'unknown',
    limits_version: 'v',
    revision: '1',
    last_event_seq: '0',
    pruned_through_seq: '0',
    deadline_at: '',
    created_at: '',
  }
  assert.equal(isActiveRun(run), false)
  assert.equal(hasUnsettledCost(run), true)
  assert.equal(isActiveRun({ ...run, state: 'reconciling' }), true)
  assert.ok(pollDelay(true) > pollDelay(false))
})

test('consent confirmation reuses the successor identity and stale owners cannot erase it', () => {
  const command = agentAction(
    '/conversations/c/egress-consents',
    {},
    {
      model_key: 'm',
      egress_consent_id: '',
      instruction: {
        schema_version: 1,
        instruction_segments: [{ type: 'text', text: 'original' }],
      },
    },
    { conversation: 'c', model: 'm', skill: 's', text: 'original' },
  )
  const restored = pendingAgentAction(JSON.stringify(command))!
  const first = consentedRunAction(command, 'consent')
  const second = consentedRunAction(restored, 'consent')
  assert.deepEqual(first, second)
  let raw: string | null = JSON.stringify(command)
  const storage = {
    getItem: () => raw,
    setItem: (_: string, value: string) => {
      raw = value
    },
    removeItem: () => {
      raw = null
    },
  }
  assert.equal(replacePendingAction(storage, 'key', restored, first), true)
  assert.equal(replacePendingAction(storage, 'key', command, second), false)
  assert.equal(replacePendingAction(storage, 'key', command, null), false)
  assert.equal(raw, JSON.stringify(first))
  assert.deepEqual(pendingAgentAction(raw)?.draft, command.draft)
})
