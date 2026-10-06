import test from 'node:test'
import assert from 'node:assert/strict'
import { nodeLabelMeta, needsModelLabel } from '../src/creative-canvas/editor/labelMeta.ts'
import { defaultNodeSize } from '../src/creative-canvas/editor/outbox.ts'
import { copyTitle } from '../src/creative-canvas/editor/structuralPreview.ts'
import type { CanvasNode } from '../src/creative-canvas/editor/api.ts'

const node = (typeKey: string, over: Partial<CanvasNode> = {}, state = 'idle'): CanvasNode =>
  ({
    id: 'n',
    parent_id: null,
    metadata: { type_key: typeKey, type_version: 1, title: 'n', intent: '', x: 0, y: 0, width: 300, height: 300, z_order: 0 },
    prompt: null,
    capabilities: { actions: [], prompt_mode: 'draft', disabled_reason: null },
    status: { content_state: 'ready', generation_state: state, active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
    placement_revision: '1',
    data_revision: '1',
    data: { schema_version: 1, config: {}, content_id: null, content_revision_id: null, selected_version_id: null, document_id: null },
    ...over,
  }) as CanvasNode

test('label meta follows PRD CV-10', () => {
  const many = { version_summary: { count: 3, main_ordinal: 2, main_model_key: 'seedream' } }
  const one = { version_summary: { count: 1, main_ordinal: 1, main_model_key: 'seedream' } }
  const labels = { seedream: 'Seedream 4' }
  assert.equal(nodeLabelMeta(node('core.image', many), labels), '主版本 2/3')
  assert.equal(nodeLabelMeta(node('core.video', one), labels), 'Seedream 4')
  assert.equal(nodeLabelMeta(node('core.image', one), {}), 'seedream', 'unknown model falls back to key')
  assert.equal(nodeLabelMeta(node('core.image', { version_summary: { count: 1, main_ordinal: 1, main_model_key: null } }), labels), '')
  assert.equal(nodeLabelMeta(node('core.image'), labels), '', 'no versions, no meta')
  for (const state of ['queued', 'running', 'reconciling'])
    assert.equal(nodeLabelMeta(node('core.image', many, state), labels), '', `${state} hides meta`)
  assert.equal(nodeLabelMeta(node('core.text', many), labels), '', 'text never shows meta')
  assert.equal(nodeLabelMeta(node('core.link', many), labels), '')
  assert.equal(nodeLabelMeta(node('core.group', many), labels), '')
  assert.equal(needsModelLabel(node('core.image', one)), true)
  assert.equal(needsModelLabel(node('core.image', many)), false)
})

test('default sizes match the server table', () => {
  assert.deepEqual(defaultNodeSize('core.text'), { width: 300, height: 220 })
  assert.deepEqual(defaultNodeSize('core.image'), { width: 300, height: 300 })
  assert.deepEqual(defaultNodeSize('core.video'), { width: 300, height: 300 })
  assert.deepEqual(defaultNodeSize('core.link'), { width: 300, height: 140 })
  assert.deepEqual(defaultNodeSize('core.audio'), { width: 300, height: 120 })
})

test('copy title adds suffix within 200 characters', () => {
  assert.equal(copyTitle('晨光'), '晨光 副本')
  assert.equal(copyTitle(''), '副本')
  const long = copyTitle('长'.repeat(200))
  assert.equal(Array.from(long).length, 200)
  assert.ok(long.endsWith(' 副本'))
  // 代理对按一个字计，与服务端 rune 计数一致。
  assert.equal(Array.from(copyTitle('🎞'.repeat(199))).length, 200)
})
