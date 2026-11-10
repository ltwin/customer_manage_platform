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
  recalledModel,
  runFailure,
  egressPolicyVersion,
} from '../src/creative-canvas/editor/agentState.ts'
import type {
  AgentRun,
  PendingAgentChain,
} from '../src/creative-canvas/editor/agentState.ts'
import {
  emptyQueue,
  enqueue,
  markSending,
  nextQueueAction,
  putBack,
  readQueue,
  recoverQueue,
  removeQueued,
  resumeQueue,
  steerBlocked,
  steerSegments,
  writeQueue,
  type QueuedMessage,
} from '../src/creative-canvas/editor/agentQueue.ts'

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
// 停在结果未知的那条记着摄影师已经点了停止：刷新、重新发送、链往下走都不丢。
test('a stop asked for before the run existed stays with the uncertain chain', () => {
  const step = (path: string, operation: string) => ({
    path,
    operation,
    createdAt: '2026-11-10T00:00:00Z',
    payload: {},
  })
  const chain = {
    steps: [
      step('/canvases/canvas/conversations', 'conversation'),
      step('/conversations/{conversation}/runs', 'run'),
    ],
    stopped: true as const,
  }
  assert.deepEqual(pendingAgentChain(JSON.stringify(chain)), chain)
  assert.equal(advanceChain(chain, { id: 'conversation-id' })?.stopped, true)
  const plain = { steps: chain.steps }
  assert.equal(pendingAgentChain(JSON.stringify(plain))?.stopped, undefined)
  assert.equal(advanceChain(plain, { id: 'conversation-id' })?.stopped, undefined)
})
// 上线前存下的链里有逐条授权那一步：接口已经没有了，读出时去掉，随后那次运行
// 的 egress_consent_id 也一并去掉；其余请求逐字保留。
test('legacy records become a chain without the per-message grant', () => {
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
  const { egress_consent_id: _dropped, ...run } = nextRun
  assert.deepEqual(chain.steps, [
    {
      path: '/conversations/c/runs',
      operation: 'stable-run-operation',
      createdAt: '2026-09-17T00:00:01Z',
      payload: run,
    },
  ])
  assert.deepEqual(chain.draft, draft)
  // 新格式的链里留着授权步骤时同样去掉。
  const saved = pendingAgentChain(
    JSON.stringify({
      steps: [
        { path: '/conversations/c/egress-consents', operation: 'grant', createdAt: '2026-11-01T00:00:00Z', payload: {} },
        { path: '/conversations/c/runs', operation: 'run', createdAt: '2026-11-01T00:00:00Z', payload: { ...nextRun } },
      ],
    }),
  )!
  assert.deepEqual(saved.steps.map((step) => step.operation), ['run'])
  assert.equal('egress_consent_id' in saved.steps[0].payload, false)
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
test('a chain carries the new conversation into the later steps', () => {
  const steps = [
    conversationStep('canvas'),
    ...messageSteps(conversationSlot, {
      model: 'vision',
      segments: [{ type: 'text', text: '原问题' }],
    }),
  ]
  const chain = {
    steps,
    draft: { conversation: '', model: 'vision', skill: '', text: '原问题' },
  }
  assert.equal(steps[0].path, '/canvases/canvas/conversations')
  // 运行请求不带授权：服务端按账号的同意核对。
  assert.deepEqual(steps[1].payload, {
    model_key: 'vision',
    instruction: {
      schema_version: 1,
      instruction_segments: [{ type: 'text', text: '原问题' }],
    },
  })
  const opened = advanceChain(chain, { id: 'conversation' })!
  assert.deepEqual(
    opened.steps.map((step) => step.path),
    ['/conversations/conversation/runs'],
  )
  assert.equal(opened.draft?.conversation, 'conversation')
  // 操作身份在组装时就定了，往后推进不会换。
  assert.deepEqual(
    opened.steps.map((step) => step.operation),
    [steps[1].operation],
  )
  assert.equal(advanceChain(opened, { id: 'run' }), null)
  assert.throws(() => advanceChain(chain, {}))
  // 还没建对话的步骤不能排在最前面。
  assert.throws(() =>
    pendingAgentChain(JSON.stringify({ steps: steps.slice(1) })),
  )
})
test('an uncertain step keeps the chain; a retry replays the same identities', async () => {
  const chain: PendingAgentChain = {
    steps: [
      conversationStep('canvas'),
      ...messageSteps(conversationSlot, {
        model: 'm',
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
  const second = await performAgentChain(left, slotOf(storage), send)
  assert.equal(second.kind, 'done')
  assert.equal(storage.box.raw, null)
  assert.deepEqual(sent, [
    chain.steps[0].operation,
    chain.steps[1].operation,
    chain.steps[1].operation,
  ])
})
test('a refusal clears the chain and a stale owner touches nothing', async () => {
  const chain: PendingAgentChain = {
    steps: messageSteps('c', {
      model: 'm',
      segments: [],
    }),
    draft: { conversation: 'c', model: 'm', skill: '', text: '原文' },
  }
  for (const [status, kind, code] of [
    [422, 'rejected', ''],
    [404, 'rejected', ''],
    [409, 'rejected', ''],
    [408, 'unknown', ''],
    // 会话正忙、额度用尽是服务端整事务回滚后的明确拒绝：链清掉，面板不再锁住。
    [429, 'rejected', 'creative_agent_busy'],
    [429, 'rejected', 'creative_llm_budget_exceeded'],
    [429, 'unknown', ''],
    [503, 'unknown', ''],
  ] as const) {
    const storage = memoryStorage(JSON.stringify(chain))
    const outcome = await performAgentChain(chain, slotOf(storage), async () => {
      throw new Refused(status, code)
    })
    assert.equal(outcome.kind, kind, `status ${status} ${code}`)
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

test('pasted images sit in the text by their local key until uploaded', async () => {
  const { agentReferences, agentSegments, referencedMaterials, revisionText, uploadReferences } =
    await import('../src/creative-canvas/editor/agentReferences.ts')
  const library = { id: 'ccrv_lib', title: '样片', kind: 'image' as const, content: { id: 'ccc_lib' } as never }
  const uploading = { key: 'k-1', title: '草图.png', source: 'upload' as const, state: 'uploading' as const, preview: 'blob:1' }
  const ready = { key: 'k-2', title: '参考.png', source: 'upload' as const, state: 'ready' as const, revisionID: 'ccrv_up', preview: 'blob:2' }
  const refs = [...agentReferences(null, [library]), ...uploadReferences([uploading, ready])]
  // A library pick keeps its content, so the chip can show a thumbnail and preview.
  assert.equal(refs[0]!.content, library.content)
  assert.deepEqual(refs.slice(1).map((r) => [r.sourceID, r.revisionID, r.thumbnail, r.state]), [
    ['k-1', null, 'blob:1', 'uploading'],
    ['k-2', 'ccrv_up', 'blob:2', undefined],
  ])
  // Still uploading: cannot be sent, and says why.
  assert.throws(() => agentSegments('看 @[k-1]', refs), /还没上传好/)
  // Uploaded: sent as its revision, wherever it sits in the sentence.
  assert.deepEqual(agentSegments('把 @[k-2] 的光用到 @[ccrv_lib]', refs), [
    { type: 'text', text: '把 ' },
    { type: 'content_ref', content_revision_id: 'ccrv_up' },
    { type: 'text', text: ' 的光用到 ' },
    { type: 'content_ref', content_revision_id: 'ccrv_lib' },
  ])
  assert.equal(revisionText('把 @[k-2] 用到 @[k-1]', refs), '把 @[ccrv_up] 用到 @[k-1]')
  assert.deepEqual(referencedMaterials('@[k-2] @[ccrv_lib]', refs), [
    library,
    { id: 'ccrv_up', title: '参考.png', kind: 'image' },
  ])
})

test('a refused request says what to do next', () => {
  assert.equal(runFailure('creative_model_providers_exhausted'), '当前负载较高，请稍后尝试')
  assert.equal(runFailure('creative_model_unavailable'), '当前模型暂无可用的服务渠道，请换一个模型')
  assert.equal(runFailure('creative_model_rejected'), '模型服务拒绝了这次请求，请重试或换一个模型')
  assert.equal(runFailure('creative_canvas_integrity'), '画布数据校验未通过，已停止修改，请联系我们处理')
  assert.equal(runFailure('creative_from_a_newer_server'), '本次执行未能完成，请重试')
  // 停止是摄影师自己的操作，费用核实属于后台记账，不出现在对话里。
  assert.equal(runFailure('creative_reconciliation_closed'), '已停止')
  for (const file of ['AgentBlocks.tsx', 'AgentPanel.tsx', 'agentState.ts'])
    assert.doesNotMatch(
      readFileSync(new URL(`../src/creative-canvas/editor/${file}`, import.meta.url), 'utf8'),
      /费用仍|结束等待/,
    )
  assert.equal(runFailure(undefined), '')
})

test('the notice version moves together with the backend egress policy version', () => {
  const service = readFileSync(
    new URL('../../backend/internal/creativeagent/service.go', import.meta.url),
    'utf8',
  )
  const policy = /const policyVersion = "([^"]+)"/.exec(service)?.[1]
  assert.equal(policy, egressPolicyVersion)
})

function queued(id: string, extra: Partial<QueuedMessage> = {}): QueuedMessage {
  return {
    id,
    parts: [{ kind: 'text', text: id }],
    draft: { conversation: 'c', model: 'm', skill: '', text: id },
    send: { model: 'm', segments: [{ type: 'text', text: id }], scene: null },
    steer: [{ type: 'text', text: id }],
    hasImage: false,
    ...extra,
  }
}
function runIn(id: string, state: AgentRun['state']): AgentRun {
  return { id, state } as AgentRun
}

test('a succeeded run sends the head of the queue, once', () => {
  const queue = enqueue(enqueue(emptyQueue, queued('a')), queued('b'))
  const first = nextQueueAction(queue, runIn('r1', 'succeeded'), true)
  assert.equal(first.send?.id, 'a')
  assert.equal(first.queue.settledRun, 'r1')
  assert.equal(first.queue.items[0].sending, true)
  // The same snapshot again, a reconnect, a re-render: nothing more.
  assert.equal(nextQueueAction(first.queue, runIn('r1', 'succeeded'), true).send, undefined)
  // The next run ends: the next message, not the one already handed over.
  const next = nextQueueAction(removeQueued(first.queue, 'a'), runIn('r2', 'succeeded'), true)
  assert.equal(next.send?.id, 'b')
})

test('a run that is going or waiting sends nothing', () => {
  const queue = enqueue(emptyQueue, queued('a'))
  for (const state of ['queued', 'running', 'waiting_client', 'reconciling', 'waiting_input'] as const)
    assert.equal(nextQueueAction(queue, runIn('r', state), true).send, undefined)
})

test('a busy panel waits for the next look', () => {
  const queue = enqueue(emptyQueue, queued('a'))
  const busy = nextQueueAction(queue, runIn('r', 'succeeded'), false)
  assert.equal(busy.send, undefined)
  assert.equal(busy.queue.settledRun, undefined)
  assert.equal(nextQueueAction(busy.queue, runIn('r', 'succeeded'), true).send?.id, 'a')
})

test('stopped, failed and partial runs pause the queue until it is resumed', () => {
  for (const state of ['cancelled', 'failed', 'partial'] as const) {
    const queue = enqueue(emptyQueue, queued('a'))
    const paused = nextQueueAction(queue, runIn('r', state), true)
    assert.equal(paused.send, undefined)
    assert.equal(paused.queue.paused, true)
    assert.equal(nextQueueAction(paused.queue, runIn('r2', 'succeeded'), true).send, undefined)
    const resumed = resumeQueue(paused.queue)
    assert.equal(resumed.send?.id, 'a')
    assert.equal(resumed.queue.paused, false)
  }
})

test('a refused send goes back to the head and pauses', () => {
  const queue = markSending(enqueue(enqueue(emptyQueue, queued('a')), queued('b')), 'a')
  const back = putBack(removeQueued(queue, 'a'), queued('a', { sending: true }), true)
  assert.deepEqual(back.items.map((item) => item.id), ['a', 'b'])
  assert.equal(back.items[0].sending, undefined)
  assert.equal(back.paused, true)
})

test('after a reload a message the chain holds stays in the queue as sending, one never handed over is back', () => {
  const queue = { ...markSending(enqueue(emptyQueue, queued('a')), 'a'), settledRun: 'r1' }
  const chain: PendingAgentChain = {
    steps: messageSteps('c', { model: 'm', segments: [{ type: 'text', text: 'a' }] }),
    draft: { conversation: 'c', model: 'm', skill: '', text: 'a', queueItem: 'a' },
  }
  // The chain still decides; a refusal on resend puts it back at the head.
  const handed = recoverQueue(queue, chain)
  assert.equal(handed, queue)
  assert.equal(nextQueueAction(handed, runIn('r1', 'succeeded'), true).send, undefined)
  const kept = recoverQueue(queue, null)
  assert.equal(kept.items.length, 1)
  assert.equal(kept.items[0].sending, undefined)
  // Never sent: the finished run is weighed again, so the message still goes out.
  assert.equal(kept.settledRun, undefined)
  assert.equal(nextQueueAction(kept, runIn('r1', 'succeeded'), true).send?.id, 'a')
  // The chain keeps naming its queue item across a reload.
  assert.equal(pendingAgentChain(JSON.stringify(chain))?.draft?.queueItem, 'a')
})

test('the queue survives storage and a broken storage is an empty queue', () => {
  const box: Record<string, string> = {}
  const storage = {
    getItem: (k: string) => box[k] ?? null,
    setItem: (k: string, v: string) => { box[k] = v },
    removeItem: (k: string) => { delete box[k] },
  }
  const queue = { ...enqueue(emptyQueue, queued('a')), settledRun: 'r1' }
  writeQueue(storage, 'k', queue)
  assert.deepEqual(readQueue(storage, 'k'), queue)
  writeQueue(storage, 'k', emptyQueue)
  assert.equal(box.k, undefined)
  const broken = {
    getItem: () => { throw new Error('denied') },
    setItem: () => { throw new Error('denied') },
    removeItem: () => { throw new Error('denied') },
  }
  assert.deepEqual(readQueue(broken, 'k'), emptyQueue)
  assert.doesNotThrow(() => writeQueue(broken, 'k', queue))
  box.k = '{not json'
  assert.deepEqual(readQueue(storage, 'k'), emptyQueue)
})

test('only words and references steer; a skill keeps the message queued', () => {
  assert.deepEqual(
    steerSegments([{ type: 'text', text: '看这张' }, { type: 'content_ref', content_revision_id: 'ccrv_a' }]),
    [{ type: 'text', text: '看这张' }, { type: 'content_ref', content_revision_id: 'ccrv_a' }],
  )
  assert.equal(steerSegments([{ type: 'skill_ref', skill_id: 's', skill_version_id: 'v' }, { type: 'text', text: 'x' }]), null)
  // The blank between two references is not a segment of its own.
  assert.deepEqual(
    steerSegments([
      { type: 'content_ref', content_revision_id: 'ccrv_a' },
      { type: 'text', text: ' ' },
      { type: 'content_ref', content_revision_id: 'ccrv_b' },
    ]),
    [
      { type: 'content_ref', content_revision_id: 'ccrv_a' },
      { type: 'content_ref', content_revision_id: 'ccrv_b' },
    ],
  )
  assert.equal(steerBlocked(queued('a', { steer: null }), true), '选了 Skill 的消息只能排队')
  assert.equal(steerBlocked(queued('a', { hasImage: true }), false), '当前模型不支持图片，只能排队')
  assert.equal(steerBlocked(queued('a', { hasImage: true }), true), '')
})

test('a steer is a step the chain can replay', () => {
  const chain: PendingAgentChain = {
    steps: [{ path: '/agent-runs/ccrn_1/steers', operation: 'op', createdAt: '2026-11-11T00:00:00Z', payload: { instruction_segments: [] } }],
  }
  assert.equal(pendingAgentChain(JSON.stringify(chain))?.steps[0].path, '/agent-runs/ccrn_1/steers')
})

test('scene references sit in the text by local key and go out in place as scene_ref', async () => {
  const { agentReferences, agentSegments, sceneReferences, sceneRefKey, sceneRefTitle, usedSceneRefs, sceneCount, revisionText, maxSceneRefs } =
    await import('../src/creative-canvas/editor/agentReferences.ts')
  const forty = (prefix: string) => Array.from({ length: 40 }, (_, i) => `sobj_${prefix}${i}`)
  const metal = { documentID: 'cwdoc_living', title: '客厅', objectIDs: forty('m'), names: forty('m').map((_, i) => `金属${i}`) }
  const walnut = { documentID: 'cwdoc_living', title: '客厅', objectIDs: forty('w'), names: forty('w').map((_, i) => `木${i}`) }
  const whole = { documentID: 'cwdoc_bed', title: '卧室', objectIDs: [], names: [] }
  const removed = { documentID: 'cwdoc_gone', title: '旧场景', objectIDs: [], names: [] }
  const keys = [sceneRefKey(), sceneRefKey(), sceneRefKey(), sceneRefKey()]
  assert.equal(new Set(keys).size, 4)
  assert.ok(keys.every((key) => /^sref_[\w-]+$/.test(key)))
  const table = { [keys[0]]: metal, [keys[1]]: walnut, [keys[2]]: whole, [keys[3]]: removed }
  const refs = [...agentReferences(null, []), ...sceneReferences(table)]
  // The removed reference stays in the table (for undo) but is not in the text.
  const text = `把 @[${keys[0]}] 改成金属，@[${keys[1]}] 改成胡桃木，@[${keys[2]}] 不动`
  assert.deepEqual(agentSegments(text, refs), [
    { type: 'text', text: '把 ' },
    { type: 'scene_ref', document_id: 'cwdoc_living', object_ids: metal.objectIDs },
    { type: 'text', text: ' 改成金属，' },
    { type: 'scene_ref', document_id: 'cwdoc_living', object_ids: walnut.objectIDs },
    { type: 'text', text: ' 改成胡桃木，' },
    { type: 'scene_ref', document_id: 'cwdoc_bed' },
    { type: 'text', text: ' 不动' },
  ])
  assert.deepEqual(usedSceneRefs(text, table), { [keys[0]]: metal, [keys[1]]: walnut, [keys[2]]: whole })
  assert.equal(sceneCount(text, table), 2)
  assert.equal(maxSceneRefs, 5)
  // Local keys are left alone when the text is saved: they are not revisions.
  assert.equal(revisionText(text, refs), text)
  assert.equal(sceneRefTitle(whole), '卧室')
  assert.equal(sceneRefTitle({ ...metal, objectIDs: metal.objectIDs.slice(0, 2) }), '客厅 · 金属0、金属1')
  assert.equal(sceneRefTitle(metal), '客厅 · 金属0、金属1 +38')
  assert.equal(sceneRefTitle({ documentID: 'd', title: '', objectIDs: ['a'], names: [''] }), '未命名场景 · 未命名')

  // A draft keeps only the references its text uses, and comes back whole.
  const chain = {
    steps: messageSteps('c', { model: 'm', imageInput: false, revisionIDs: [], segments: agentSegments(text, refs) }),
    draft: { conversation: 'c', model: 'm', skill: '', text, scenes: usedSceneRefs(text, table) },
  }
  assert.deepEqual(pendingAgentChain(JSON.stringify(chain)), chain)
  assert.throws(
    () => pendingAgentChain(JSON.stringify({ ...chain, draft: { ...chain.draft, scenes: { bad: whole } } })),
    /场景引用记录损坏/,
  )
  assert.throws(
    () => pendingAgentChain(JSON.stringify({ ...chain, draft: { ...chain.draft, scenes: { [keys[0]]: { ...whole, objectIDs: [1] } } } })),
    /场景引用记录损坏/,
  )
})
