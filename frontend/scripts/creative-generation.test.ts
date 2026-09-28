import test from 'node:test'
import assert from 'node:assert/strict'
import {
  assertGenerationSnapshot,
  parameterValue,
  promptFromNode,
} from '../src/creative-canvas/editor/generation.ts'
import type { Canvas, CanvasNode } from '../src/creative-canvas/editor/api.ts'

function fixture(): Canvas {
  const node: CanvasNode = {
    id: 'n',
    parent_id: null,
    placement_revision: '1',
    data_revision: '1',
    metadata: {
      type_key: 'core.text',
      type_version: 1,
      title: '文字',
      intent: '',
      x: 0,
      y: 0,
      width: 260,
      height: 160,
      z_order: 0,
    },
    data: {
      schema_version: 1,
      config: {},
      content_id: null,
      content_revision_id: null,
      selected_version_id: null,
      document_id: null,
    },
    status: {
      content_state: 'empty',
      generation_state: 'idle',
      active_execution_id: null,
      latest_execution_id: null,
      status_revision: '1',
    },
    capabilities: { actions: ['edit'], prompt_mode: 'generate' },
    prompt: null,
  }
  return {
    id: 'c',
    project_id: 'p',
    project_name: '创作',
    project_revision: '1',
    archived: false,
    revision: '1',
    topology_revision: '1',
    nodes: [node],
    edges: [],
    node_inputs: [],
    changes: [],
    object_states: [],
  }
}
test('生成必须固定保存后的草稿revision与点击时作品读集', () => {
  const before = fixture(),
    fresh = structuredClone(before)
  fresh.nodes[0].prompt = {
    draft_id: 'd',
    draft_revision: '1',
    action_key: 'gateway.generation',
    model_key: 'm',
    text: '描述',
    parameters: {},
    references: [],
  }
  assert.equal(assertGenerationSnapshot(before, fresh, 'n', '1').id, 'n')
  fresh.nodes[0].prompt.draft_revision = '2'
  assert.throws(
    () => assertGenerationSnapshot(before, fresh, 'n', '1'),
    /提示词已变化/,
  )
  fresh.nodes[0].prompt.draft_revision = '1'
  fresh.nodes[0].data_revision = '2'
  assert.throws(() => assertGenerationSnapshot(before, fresh, 'n', '1'), /节点/)
})
test('草稿参数与引用拷贝，编辑不修改服务端快照', () => {
  const c = fixture()
  c.nodes[0].prompt = {
    draft_id: 'd',
    draft_revision: '1',
    action_key: 'gateway.generation',
    model_key: 'm',
    text: '描述',
    parameters: { count: 2 },
    references: [],
  }
  const d = promptFromNode(c, c.nodes[0])
  d.parameters.count = 3
  d.text = 'changed'
  assert.equal(c.nodes[0].prompt.parameters.count, 2)
  assert.equal(c.nodes[0].prompt.text, '描述')
})
test('整数不允许悄悄丢精度，零和false不会变为默认值', () => {
  const field = {
    key: 'count',
    label: '数量',
    type: 'integer' as const,
    required: true,
    options: [],
  }
  assert.equal(parameterValue(field, '0'), 0)
  assert.equal(parameterValue(field, ''), undefined)
  assert.throws(() => parameterValue(field, '9007199254740993'))
  assert.throws(() => parameterValue(field, '1.5'))
  assert.equal(parameterValue({ ...field, type: 'boolean' }, 'false'), false)
})

test('明确的生成配置冲突可解除队列，失联后的原操作仍保持身份', async () => {
  const { SaveQueue } = await import('../src/creative-canvas/editor/queue.ts')
  const { emptyJournal } =
    await import('../src/creative-canvas/editor/journal.ts')
  for (const rejectionCode of [
    'creative_generation_conflict',
    'creative_generation_parameters',
    'creative_budget_exceeded',
    'creative_generation_facts_pending',
    'creative_generation_unavailable',
    'creative_action_unavailable',
  ])
    for (const uncertain of [false, true]) {
      const sent: { operation: string; body: string }[] = []
      const q = new SaveQueue(
        { load: async () => emptyJournal(), save: async () => {} },
        {
          send: async (job) => {
            sent.push({ operation: job.operation, body: job.body })
            throw Object.assign(new Error('configuration changed'), {
              status: uncertain && sent.length === 1 ? 503 : 409,
              code:
                uncertain && sent.length === 1 ? 'unavailable' : rejectionCode,
            })
          },
          lookup: async () => {
            throw Object.assign(new Error('not found'), { status: 404 })
          },
        },
        () => {},
      )
      await q.load()
      await q.enqueue('/canvases/c/executions', {
        configuration_revision: 'old',
      })
      if (uncertain) {
        assert.equal(q.value.job?.state, 'unknown')
        await q.flush(true)
        assert.deepEqual(sent[0], sent[1])
      }
      assert.equal(q.value.job?.state, 'rejected')
      await q.dismissRejected()
      assert.equal(q.value.job, null)
    }
})
