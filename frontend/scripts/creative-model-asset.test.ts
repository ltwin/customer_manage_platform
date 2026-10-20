import test from 'node:test'
import assert from 'node:assert/strict'
import type { Content } from '../src/creative-canvas/editor/api.ts'
import { modelInfo, modelRows } from '../src/creative-canvas/viewer/modelInfo.ts'

const content = (model: unknown, kind = 'model') =>
  ({ id: 'ccrv_m', content_id: 'ccnt_m', kind, sequence: '1', truncated: false, payload: { model } }) as unknown as Content

test('model info formats size, mount, triangles, slots and parameters', () => {
  const info = modelInfo(
    content({
      format: 'glb',
      name_hint: '哥特束柱',
      manifest: { bounds: { min: [-0.25, 0, -0.3], max: [0.25, 2.4, 0.3] }, triangles: 12000, slots: [{ id: 'stone', hint: 'stone' }, { id: 'trim' }], parts: [], anchors: [] },
      generator: {
        language: 'three-module', contract_version: 1, lib_version: 1, code_sha256: 'sha256-' + 'a'.repeat(64), mount: 'floor',
        params_schema: { height: { type: 'number', default: 2, min: 1, max: 4, unit: 'm', label: '高度' }, fluted: { type: 'boolean', default: true } },
        params: { height: 2.4 },
      },
    }),
  )
  assert.ok(info)
  assert.equal(info.name, '哥特束柱')
  assert.equal(info.size, '0.50 × 2.40 × 0.60 米')
  assert.equal(info.mount, '落地')
  assert.equal(info.triangles, '12,000')
  assert.deepEqual(modelRows(info), [
    ['尺寸', '0.50 × 2.40 × 0.60 米'],
    ['放置方式', '落地'],
    ['面数', '12,000'],
    ['材质槽', 'stone（stone）、trim'],
    ['fluted', '是'],
    ['高度', '2.4 m'],
  ])
})

test('only a complete model has info', () => {
  assert.equal(modelInfo(content({ format: 'glb', manifest: { bounds: { min: [0, 0, 0], max: [1, 1, 1] }, triangles: 1, slots: [], parts: [], anchors: [] } })), null)
  assert.equal(modelInfo(undefined), null)
  assert.equal(modelInfo({ kind: 'text', payload: { body: 'x' } } as unknown as Content), null)
})
