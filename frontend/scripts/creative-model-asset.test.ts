import test from 'node:test'
import assert from 'node:assert/strict'
import type { Content } from '../src/creative-canvas/editor/api.ts'
import { assetMaterial, materialRows, modelInfo, modelRows } from '../src/creative-canvas/viewer/modelInfo.ts'

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
  assert.equal(info.origin, '底部中心')
  assert.equal(info.triangles, '12,000')
  assert.deepEqual(modelRows(info), [
    ['尺寸', '0.50 × 2.40 × 0.60 米'],
    ['参考点', '底部中心'],
    ['面数', '12,000'],
    ['材质槽', 'stone（stone）、trim'],
    ['fluted', '是'],
    ['高度', '2.4 m'],
  ])
})

test('face slots join the model\'s own, after them, with their hints', () => {
  const info = modelInfo(
    content({
      format: 'glb',
      manifest: { bounds: { min: [0, 0, 0], max: [1, 1, 1] }, triangles: 152, slots: [{ id: 'stone', hint: 'stone' }], parts: [], anchors: [] },
      materials: { face_1: { preset: 'paint@1', params: {} } },
      faces: {
        meshes: [{ path: [['', 0]], primitive: 0, topology: 't1-0', assign: { stone: [[0, 4]], face_1: [[4, 8]], face_2: [] } }],
        hints: { face_1: 'wood' },
      },
      generator: { language: 'three-module', contract_version: 1, lib_version: 1, code_sha256: 'sha256-' + 'a'.repeat(64), mount: 'floor', params_schema: {}, params: {} },
    }),
  )
  assert.deepEqual(info?.slots, [{ id: 'stone', hint: 'stone' }, { id: 'face_1', hint: 'wood', material: '漆面' }])
})

test('only a complete model has info', () => {
  assert.equal(modelInfo(content({ format: 'glb', manifest: { bounds: { min: [0, 0, 0], max: [1, 1, 1] }, triangles: 1, slots: [], parts: [], anchors: [] } })), null)
  assert.equal(modelInfo(undefined), null)
  assert.equal(modelInfo({ kind: 'text', payload: { body: 'x' } } as unknown as Content), null)
})

test('a material asset gives its slot and rows: preset, maps, mapping, alpha', () => {
  const slot = { preset: 'principled@1', params: {}, maps: { base_color: { content_revision_id: 'ccrv_a' }, normal: { content_revision_id: 'ccrv_b' } }, mapping: { kind: 'triplanar' as const, size_m: 2 }, alpha_mode: 'clip' as const }
  const material = { kind: 'material', payload: { material: { slot, source: { document_id: 'cwdoc_1', object_id: 'sobj_1', slot_id: 'main', revision: '3' } } } } as unknown as Content
  assert.equal(assetMaterial(material), slot)
  assert.equal(assetMaterial(undefined), null)
  assert.equal(assetMaterial({ kind: 'text', payload: { body: 'x' } } as unknown as Content), null)
  assert.deepEqual(materialRows(slot), [['材质', '原理化 BSDF'], ['贴图', '基础色、法线'], ['映射', '三平面投影 · 2 米一块'], ['透明', '透明裁切']])
  assert.deepEqual(materialRows({ preset: 'oak@1', params: {} }), [['材质', 'oak@1'], ['贴图', '无'], ['透明', '不透明']])
  assert.deepEqual(materialRows({ ...slot, mapping: { kind: 'uv', repeat: [2, 3] } })[2], ['映射', '模型 UV · 重复 2 × 3'])
})

test('models never reach the canvas', async () => {
  const { isCanvasAsset } = await import('../src/creative-canvas/editor/assetPlacement.ts')
  const { validatePlacementAssets, PlacementError } = await import('../src/creative-canvas/assets/placement.ts')
  const asset = (kind: string) => ({ id: `ccas_${kind}`, kind, unavailable: false, deleted_at: null }) as never
  assert.equal(isCanvasAsset(asset('model')), false)
  assert.equal(isCanvasAsset(asset('material')), false)
  assert.equal(isCanvasAsset(asset('image')), true)
  assert.throws(() => validatePlacementAssets([asset('image'), asset('model')]), (e: unknown) => e instanceof PlacementError && e.message === '模型只能放进 3D 场景')
  assert.doesNotThrow(() => validatePlacementAssets([asset('image'), asset('text')]))
})
