import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import {
  advanceChain,
  conversationSlot,
  conversationStep,
  messageSteps,
  pendingAgentChain,
  performAgentChain,
  replacePendingChain,
  sendRefusal,
  stepBody,
  isActiveRun,
  hasUnsettledCost,
  recalledModel,
  runFailure,
  egressNoticeVersion,
  noticeSeen,
  rememberNotice,
} from '../src/creative-canvas/editor/agentState.ts'
import type {
  AgentRun,
  PendingAgentChain,
} from '../src/creative-canvas/editor/agentState.ts'

function memoryStorage(initial: string | null = null) {
  const box = { raw: initial }
  return {
    box,
    getItem: () => box.raw,
    setItem: (_: string, value: string) => {
      box.raw = value
    },
    removeItem: () => {
      box.raw = null
    },
  }
}
function slotOf(storage: ReturnType<typeof memoryStorage>) {
  return {
    owns: (chain: PendingAgentChain) =>
      JSON.stringify(pendingAgentChain(storage.getItem())) ===
      JSON.stringify(chain),
    save: (expected: PendingAgentChain, next: PendingAgentChain | null) =>
      replacePendingChain(storage, 'key', expected, next),
  }
}
class Refused extends Error {
  constructor(
    readonly status: number,
    readonly code = '',
  ) {
    super(`refused ${status}`)
  }
}

test('uncertain submissions retain the exact operation and bytes across refresh', () => {
  const chain = {
    steps: [
      {
        path: '/agent-runs/run-one/supplements',
        operation: 'operation-one',
        createdAt: '2026-09-17T00:00:00Z',
        payload: { text: '保留原文' },
      },
    ],
  }
  const restored = pendingAgentChain(JSON.stringify(chain))!
  assert.deepEqual(restored, chain)
  assert.equal(
    stepBody(restored.steps[0]),
    JSON.stringify({
      operation_id: 'operation-one',
      client_created_at: '2026-09-17T00:00:00Z',
      payload: { text: '保留原文' },
    }),
  )
  const step = chain.steps[0]
  assert.throws(() =>
    pendingAgentChain(JSON.stringify({ steps: [{ ...step, operation: '' }] })),
  )
  assert.throws(() =>
    pendingAgentChain(
      JSON.stringify({ steps: [{ ...step, path: '/unrelated' }] }),
    ),
  )
  assert.throws(() => pendingAgentChain(JSON.stringify({ steps: [] })))
})
test('legacy records become a chain that replays the original bytes', () => {
  const body = JSON.stringify({
    operation_id: 'operation-one',
    client_created_at: '2026-09-17T00:00:00Z',
    payload: { purpose: 'creative_assistance', selected_revision_ids: [] },
  })
  const nextRun = {
    model_key: 'fixed',
    egress_consent_id: '',
    instruction: {
      schema_version: 1,
      instruction_segments: [
        { type: 'text', text: '原问题' },
        { type: 'skill_ref', skill_id: 'skill', skill_version_id: 'version' },
      ],
    },
  }
  const draft = { conversation: 'c', model: 'fixed', skill: 'version', text: '原问题' }
  const legacy = {
    path: '/conversations/c/egress-consents',
    operation: 'operation-one',
    body,
    nextOperation: 'stable-run-operation',
    nextCreatedAt: '2026-09-17T00:00:01Z',
    nextRun,
    draft,
  }
  const chain = pendingAgentChain(JSON.stringify(legacy))!
  assert.equal(chain.steps.length, 2)
  assert.equal(stepBody(chain.steps[0]), body)
  assert.deepEqual(chain.steps[1], {
    path: '/conversations/c/runs',
    operation: 'stable-run-operation',
    createdAt: '2026-09-17T00:00:01Z',
    payload: nextRun,
  })
  assert.deepEqual(chain.draft, draft)
  // 一步的旧记录（补充、取消）照样能读。
  const one = pendingAgentChain(
    JSON.stringify({ path: '/agent-runs/r/cancel', operation: 'operation-one', body }),
  )!
  assert.equal(one.steps.length, 1)
  assert.equal(stepBody(one.steps[0]), body)
  // 转换不了的按记录损坏处理。
  assert.throws(() =>
    pendingAgentChain(JSON.stringify({ ...legacy, operation: 'different' })),
  )
  assert.throws(() =>
    pendingAgentChain(JSON.stringify({ ...legacy, nextOperation: undefined })),
  )
  assert.throws(() =>
    pendingAgentChain(JSON.stringify({ ...legacy, path: '/agent-runs/r/retry' })),
  )
})
test('a chain carries the new conversation and grant into the later steps', () => {
  const steps = [
    conversationStep('canvas'),
    ...messageSteps(conversationSlot, {
      model: 'vision',
      imageInput: true,
      revisionIDs: ['ccrv_a'],
      segments: [{ type: 'text', text: '原问题' }],
    }),
  ]
  const chain = {
    steps,
    draft: { conversation: '', model: 'vision', skill: '', text: '原问题' },
  }
  assert.equal(steps[0].path, '/canvases/canvas/conversations')
  assert.deepEqual(steps[1].payload, {
    purpose: 'creative_assistance',
    mode: 'selected_revisions',
    data_classes: ['text', 'canvas_metadata', 'image_preview'],
    selected_revision_ids: ['ccrv_a'],
  })
  const opened = advanceChain(chain, { id: 'conversation' })!
  assert.deepEqual(
    opened.steps.map((step) => step.path),
    ['/conversations/conversation/egress-consents', '/conversations/conversation/runs'],
  )
  assert.equal(opened.draft?.conversation, 'conversation')
  const granted = advanceChain(opened, { id: 'consent' })!
  assert.equal(granted.steps[0].payload.egress_consent_id, 'consent')
  // 操作身份在组装时就定了，往后推进不会换。
  assert.deepEqual(
    granted.steps.map((step) => step.operation),
    [steps[2].operation],
  )
  assert.equal(advanceChain(granted, { id: 'run' }), null)
  assert.throws(() => advanceChain(chain, {}))
  // 还没建对话的步骤不能排在最前面。
  assert.throws(() =>
    pendingAgentChain(JSON.stringify({ steps: steps.slice(1) })),
  )
  assert.deepEqual(
    messageSteps('c', {
      model: 'text',
      imageInput: false,
      revisionIDs: [],
      segments: [],
    })[0].payload.data_classes,
    ['text', 'canvas_metadata'],
  )
})
test('an uncertain step keeps the chain; a retry replays the same identities', async () => {
  const chain: PendingAgentChain = {
    steps: [
      conversationStep('canvas'),
      ...messageSteps(conversationSlot, {
        model: 'm',
        imageInput: false,
        revisionIDs: [],
        segments: [{ type: 'text', text: 'hi' }],
      }),
    ],
  }
  const storage = memoryStorage(JSON.stringify(chain))
  const sent: string[] = []
  let fail = true
  const send = async (path: string, body: string, operation: string) => {
    sent.push(operation)
    assert.equal(JSON.parse(body).operation_id, operation)
    if (path.endsWith('/runs') && fail) {
      fail = false
      throw new TypeError('Failed to fetch')
    }
    return { id: path.endsWith('/conversations') ? 'conv' : `id-${sent.length}` }
  }
  const first = await performAgentChain(chain, slotOf(storage), send)
  assert.equal(first.kind, 'unknown')
  const left = pendingAgentChain(storage.box.raw)!
  assert.equal(left.steps.length, 1)
  assert.equal(left.steps[0].path, '/conversations/conv/runs')
  assert.equal(left.steps[0].payload.egress_consent_id, 'id-2')
  const second = await performAgentChain(left, slotOf(storage), send)
  assert.equal(second.kind, 'done')
  assert.equal(storage.box.raw, null)
  assert.deepEqual(sent, [
    chain.steps[0].operation,
    chain.steps[1].operation,
    chain.steps[2].operation,
    chain.steps[2].operation,
  ])
})
test('a refusal clears the chain and a stale owner touches nothing', async () => {
  const chain: PendingAgentChain = {
    steps: messageSteps('c', {
      model: 'm',
      imageInput: false,
      revisionIDs: [],
      segments: [],
    }),
    draft: { conversation: 'c', model: 'm', skill: '', text: '原文' },
  }
  for (const [status, kind] of [
    [422, 'rejected'],
    [404, 'rejected'],
    [409, 'rejected'],
    [408, 'unknown'],
    [429, 'unknown'],
    [503, 'unknown'],
  ] as const) {
    const storage = memoryStorage(JSON.stringify(chain))
    const outcome = await performAgentChain(chain, slotOf(storage), async () => {
      throw new Refused(status)
    })
    assert.equal(outcome.kind, kind, `status ${status}`)
    assert.equal(storage.box.raw === null, kind === 'rejected')
    if (outcome.kind === 'rejected')
      assert.deepEqual(outcome.chain.draft, chain.draft)
  }
  // 发出后槽位被别处换掉：这边既不推进也不清除。
  const storage = memoryStorage(JSON.stringify(chain))
  const other = JSON.stringify({ steps: [conversationStep('canvas')] })
  const outcome = await performAgentChain(chain, slotOf(storage), async () => {
    storage.box.raw = other
    return { id: 'consent' }
  })
  assert.equal(outcome.kind, 'lost')
  assert.equal(storage.box.raw, other)
  const refused = await performAgentChain(chain, slotOf(storage), async () => {
    throw new Refused(422)
  })
  assert.equal(refused.kind, 'lost')
  assert.equal(storage.box.raw, other)
})
test('home refusals name the reason', () => {
  assert.equal(
    sendRefusal(new Refused(404, 'not_found'), true),
    '参考图已过期，请重新添加',
  )
  assert.equal(sendRefusal(new Refused(404, 'not_found'), false), 'refused 404')
  assert.equal(
    sendRefusal(new Refused(422, 'creative_model_capability_missing'), true),
    '当前模型不支持这次请求，请换一个模型',
  )
  assert.equal(
    sendRefusal(new Refused(409, 'archived_read_only'), false),
    '项目已归档，恢复后才能发送',
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

test('returning to a conversation picks up the model its last run used', () => {
  const run = (
    id: string,
    conversation_id: string,
    model_key: string,
    created_at: string,
  ): AgentRun => ({
    id,
    conversation_id,
    canvas_id: 'canvas',
    trigger_message_id: id,
    egress_consent_id: 'consent',
    model_key,
    state: 'succeeded',
    settlement_state: 'settled',
    limits_version: 'v',
    revision: '1',
    last_event_seq: '0',
    pruned_through_seq: '0',
    deadline_at: '',
    created_at,
  })
  const model = (model_key: string, available = true) => ({
    model_key,
    display_name: model_key,
    capability: {
      image_input: true,
      tool_calling: true,
      tool_choice: true,
      json_object: true,
      temperature: true,
      context_tokens: 1000,
      max_output_tokens: 100,
    },
    catalog_version: 'v',
    available,
    context_tokens: 1000,
    max_output_tokens: 100,
  })
  // 列表顺序不算数：按时间取最近一次，其他会话的运行不参与。
  const runs = [
    run('old', 'a', 'fast', '2026-10-10T08:00:00.5Z'),
    run('new', 'a', 'deep', '2026-10-10T08:00:00.25Z'),
    run('other', 'b', 'fast', '2026-10-10T09:00:00Z'),
  ]
  assert.equal(recalledModel(runs, 'a', [model('fast'), model('deep')]), 'fast')
  runs[1] = run('new', 'a', 'deep', '2026-10-10T08:00:01Z')
  assert.equal(recalledModel(runs, 'a', [model('fast'), model('deep')]), 'deep')
  // 已下架或暂不可用的模型不沿用，交给面板原来的默认值。
  assert.equal(recalledModel(runs, 'a', [model('fast')]), '')
  assert.equal(recalledModel(runs, 'a', [model('deep', false)]), '')
  assert.equal(recalledModel(runs, 'c', [model('fast')]), '')
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
  const chain = {
    steps: messageSteps('c', {
      model: 'vision',
      imageInput: true,
      revisionIDs: materials.map((item) => item.id),
      segments,
    }),
    draft: { conversation: 'c', model: 'vision', skill: '', text, materials },
  }
  const restored = pendingAgentChain(JSON.stringify(chain))!
  assert.deepEqual(restored, chain)
  assert.deepEqual(
    advanceChain(restored, { id: 'consent' }),
    advanceChain(chain, { id: 'consent' }),
  )
  assert.deepEqual(referencedMaterials('只使用 @[ccrv_text]', refs), [
    materials[1],
  ])
})

test('a refused request says what to do next', () => {
  assert.equal(runFailure('creative_model_providers_exhausted'), '当前负载较高，请稍后尝试')
  assert.equal(runFailure('creative_model_unavailable'), '当前模型暂无可用的服务渠道，请换一个模型')
  assert.equal(runFailure('creative_model_rejected'), '模型服务拒绝了这次请求，请重试或换一个模型')
  assert.equal(runFailure('creative_canvas_integrity'), '画布数据校验未通过，已停止修改，请联系我们处理')
  assert.equal(runFailure('creative_from_a_newer_server'), '本次执行未能完成，请重试')
  assert.equal(runFailure(undefined), '')
})

test('the first-send notice is remembered per account and per notice version', () => {
  const items = new Map<string, string>()
  const storage = {
    getItem: (key: string) => items.get(key) ?? null,
    setItem: (key: string, value: string) => void items.set(key, value),
  } as Storage
  assert.equal(noticeSeen(storage, 'acc_a'), false)
  rememberNotice(storage, 'acc_a')
  assert.equal(noticeSeen(storage, 'acc_a'), true)
  // 另一个账号在同一台设备上仍要看一次。
  assert.equal(noticeSeen(storage, 'acc_b'), false)
  // 说明升级了版本：看过旧版的也要再看一次。
  items.set('creative-egress-notice:acc_a', 'egress-2026-01-1')
  assert.equal(noticeSeen(storage, 'acc_a'), false)
  // 读不到本机存储时宁可多弹：当作没看过，写入失败也不打断发送。
  const broken = {
    getItem: () => {
      throw new Error('denied')
    },
    setItem: () => {
      throw new Error('denied')
    },
  } as unknown as Storage
  assert.equal(noticeSeen(broken, 'acc_a'), false)
  assert.doesNotThrow(() => rememberNotice(broken, 'acc_a'))
  assert.equal(noticeSeen(null, 'acc_a'), false)
})

test('the notice version moves together with the backend egress policy version', () => {
  const service = readFileSync(
    new URL('../../backend/internal/creativeagent/service.go', import.meta.url),
    'utf8',
  )
  const policy = /const policyVersion = "([^"]+)"/.exec(service)?.[1]
  assert.equal(policy, egressNoticeVersion)
})
