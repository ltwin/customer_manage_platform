import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  agentAction,
  consentedRunAction,
  replacePendingAction,
  pendingAgentAction,
  isActiveRun,
  hasUnsettledCost,
  runFailure,
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

test('material references preserve order, immutable revisions and retry identity', async () => {
  const { agentReferences, agentSegments, referencedMaterials } =
    await import('../src/creative-canvas/editor/agentReferences.ts')
  const materials = [
    { id: 'ccrv_image', title: '构图', kind: 'image' as const },
    { id: 'ccrv_text', title: '方案', kind: 'text' as const },
  ]
  const refs = agentReferences(null, materials)
  const text = '将 @[ccrv_image] 用于 @[ccrv_text]，保留构图'
  const segments = agentSegments(text, refs)
  assert.deepEqual(segments, [
    { type: 'text', text: '将 ' },
    { type: 'content_ref', content_revision_id: 'ccrv_image' },
    { type: 'text', text: ' 用于 ' },
    { type: 'content_ref', content_revision_id: 'ccrv_text' },
    { type: 'text', text: '，保留构图' },
  ])
  assert.deepEqual(referencedMaterials(text, refs), materials)
  assert.throws(() => agentSegments('@[unknown]', refs))
  const original = agentAction(
    '/conversations/c/egress-consents',
    { selected_revision_ids: materials.map((item) => item.id) },
    {
      model_key: 'vision',
      egress_consent_id: '',
      instruction: { schema_version: 1, instruction_segments: segments },
    },
    { conversation: 'c', model: 'vision', skill: '', text, materials },
  )
  const restored = pendingAgentAction(JSON.stringify(original))!
  assert.deepEqual(restored, original)
  assert.deepEqual(
    consentedRunAction(restored, 'consent'),
    consentedRunAction(original, 'consent'),
  )
  assert.deepEqual(referencedMaterials('只使用 @[ccrv_text]', refs), [
    materials[1],
  ])
})

test('a refused request says what to do next', () => {
  assert.equal(runFailure('creative_model_providers_exhausted'), '当前负载较高，请稍后尝试')
  assert.equal(runFailure('creative_model_unavailable'), '当前模型暂无可用的服务渠道，请换一个模型')
  assert.equal(runFailure('creative_model_rejected'), '模型服务拒绝了这次请求，请重试或换一个模型')
  assert.equal(runFailure('creative_from_a_newer_server'), '本次执行未能完成，请重试')
  assert.equal(runFailure(undefined), '')
})
