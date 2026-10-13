import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  fitRatio,
  imageEditGate,
  isWholeImage,
  moveBox,
  ratioValue,
  reductionPercent,
  resizeBox,
  roundBox,
  runImageEdit,
  wholeImage,
  type EditTransport,
  type ImageEditCapabilities,
  type ImageEditRender,
} from '../src/creative-canvas/editor/imageEditModel.ts'

const source = { width: 4000, height: 3000 }

test('切换比例保持中心并取图内最大选区', () => {
  const box = fitRatio({ left: 3000, top: 200, width: 800, height: 600 }, 1, source)
  assert.deepEqual(box, { left: 1000, top: 0, width: 3000, height: 3000 })
  const tall = fitRatio(wholeImage(source), ratioValue('9:16', source)!, source)
  assert.equal(tall.height, 3000)
  assert.equal(Math.round(tall.width), 1688)
  assert.equal(Math.round(tall.left + tall.width / 2), 2000)
  assert.equal(ratioValue('original', source), 4 / 3)
  assert.equal(ratioValue('free', source), null)
})

test('移动选区不越出图片', () => {
  const box = { left: 100, top: 100, width: 1000, height: 1000 }
  assert.deepEqual(moveBox(box, -500, 99999, source), { left: 0, top: 2000, width: 1000, height: 1000 })
})

test('拖边角：自由比例对边不动，固定比例保持比例且限制在图内', () => {
  const box = { left: 1000, top: 1000, width: 1000, height: 1000 }
  assert.deepEqual(resizeBox(box, 'nw', -200, 300, null, source), { left: 800, top: 1300, width: 1200, height: 700 })
  // 往外拖到超出图片：宽被限制到右边固定时可达的最大值。
  assert.deepEqual(resizeBox(box, 'w', -5000, 0, null, source), { left: 0, top: 1000, width: 2000, height: 1000 })
  const square = resizeBox(box, 'se', 5000, 100, 1, source)
  assert.equal(square.width, square.height)
  assert.equal(square.top + square.height <= source.height, true)
  assert.deepEqual(square, { left: 1000, top: 1000, width: 2000, height: 2000 })
  // 只拖下边：宽以中线为轴两侧伸展，仍在图内。
  const down = resizeBox(box, 's', 0, 600, 1, source)
  assert.deepEqual(down, { left: 700, top: 1000, width: 1600, height: 1600 })
  // 不能缩成 0。
  const tiny = resizeBox(box, 'e', -5000, 0, null, source)
  assert.equal(tiny.width, 1)
})

test('取整后的框在图内、至少 1 像素，整图判定按取整结果', () => {
  assert.deepEqual(roundBox({ left: 3999.6, top: -0.4, width: 0.2, height: 3000.9 }, source), { left: 3999, top: 0, width: 1, height: 3000 })
  assert.equal(isWholeImage({ left: 0.3, top: 0.2, width: 3999.9, height: 2999.6 }, source), true)
  assert.equal(isWholeImage({ left: 1, top: 0, width: 3999, height: 3000 }, source), false)
  assert.equal(reductionPercent(1000, 613), 38.7)
  assert.equal(reductionPercent(0, 10), 0)
})

const caps: ImageEditCapabilities = {
  available: true,
  actions: ['compress', 'crop'],
  input_mime_types: ['image/jpeg', 'image/png', 'image/webp'],
  quality_min: 1,
  quality_max: 100,
  max_edge: 12000,
  max_pixels: 60_000_000,
  max_output_bytes: 25 << 20,
  ready_ttl_seconds: 1800,
}

test('编辑入口：不适用时隐藏，适用但受限时禁用并说明', () => {
  const base = { capabilities: caps, typeKey: 'core.image', hasContent: true, single: true, preparing: false, readOnly: false, generating: false, sourceMime: 'image/png', unavailable: false }
  assert.deepEqual(imageEditGate(base), { hidden: false, disabled: false })
  for (const hidden of [{ hasContent: false }, { single: false }, { typeKey: 'core.video' }, { capabilities: { ...caps, available: false } }])
    assert.deepEqual(imageEditGate({ ...base, ...hidden }), { hidden: true })
  const reasons = [
    [{ readOnly: true }, '只读'],
    [{ preparing: true }, '正在准备图片'],
    [{ sourceMime: 'image/gif' }, 'GIF 格式暂不支持'],
    [{ generating: true }, '正在生成'],
    [{ unavailable: true }, '无法读取'],
  ] as const
  for (const [change, reason] of reasons) {
    const gate = imageEditGate({ ...base, ...change })
    assert.ok(!gate.hidden && gate.disabled && gate.reason.includes(reason), JSON.stringify(gate))
  }
})

// fakeServer 模拟候选：前 n 次查询仍在编码，然后进入 final 状态。
function fakeServer(opts: { final?: ImageEditRender['state']; encodingPolls?: number; applyErrors?: unknown[]; createErrors?: unknown[] } = {}) {
  const calls: string[] = []
  const applyBodies: unknown[] = []
  let polls = 0
  let state: ImageEditRender['state'] = 'queued'
  const render = (): ImageEditRender => ({
    id: 'cier_1', operation_id: 'op', canvas_id: 'c', node_id: 'n', source_revision_id: 'src', action: 'compress',
    parameters: { quality: 80 }, state, expires_at: '', created_at: '', source_size: 1000,
    output: state === 'ready' || state === 'applied' ? { mime: 'image/jpeg', width: 10, height: 10, size: 600, sha256: '' } : undefined,
  })
  let uuid = 0
  const t: EditTransport = {
    uuid: () => `op-${++uuid}`,
    sleep: async () => {},
    async create() {
      calls.push('create')
      const e = opts.createErrors?.shift()
      if (e) throw e
      return render()
    },
    async get() {
      calls.push('get')
      if (state === 'queued' && ++polls > (opts.encodingPolls ?? 1)) state = opts.final ?? 'ready'
      return render()
    },
    async discard() {
      calls.push('discard')
      if (state !== 'applied') state = 'discarded'
      return render()
    },
    async apply(_c, _n, body) {
      calls.push('apply')
      applyBodies.push(body)
      const e = opts.applyErrors?.shift()
      if (e === 'commit-then-lose') {
        state = 'applied'
        throw new TypeError('Failed to fetch')
      }
      if (e) throw e
      state = 'applied'
      return { version_id: 'v', content_revision_id: 'r' } as never
    },
  }
  return { t, calls, applyBodies, state: () => state }
}

const request = { canvasID: 'c', nodeID: 'n', sourceRevisionID: 'src', action: 'compress' as const, parameters: { quality: 80 }, dataRevision: () => '3' }

test('应用：编码完成后同步采用一次', async () => {
  const s = fakeServer()
  const phases: string[] = []
  const out = await runImageEdit(s.t, request, new AbortController().signal, (p) => phases.push(p))
  assert.equal(out.kind, 'applied')
  assert.deepEqual(phases, ['encoding', 'applying'])
  assert.equal(s.calls.filter((c) => c === 'create').length, 1)
  assert.equal(s.calls.filter((c) => c === 'apply').length, 1)
})

test('未减小与编码失败：不采用', async () => {
  assert.deepEqual(await runImageEdit(fakeServer({ final: 'not_reduced' }).t, request, new AbortController().signal), { kind: 'not_reduced', sourceSize: 1000 })
  const failed = await runImageEdit(fakeServer({ final: 'failed' }).t, request, new AbortController().signal)
  assert.equal(failed.kind, 'failed')
})

test('等待中取消：丢弃候选，不发采用', async () => {
  const s = fakeServer({ encodingPolls: 100 })
  const abort = new AbortController()
  s.t.sleep = async () => abort.abort()
  const out = await runImageEdit(s.t, request, abort.signal)
  assert.equal(out.kind, 'cancelled')
  assert.equal(s.state(), 'discarded')
  assert.ok(!s.calls.includes('apply'))
})

test('采用响应丢失：只查原候选并原样重发，不重新编码', async () => {
  const s = fakeServer({ applyErrors: ['commit-then-lose'] })
  const phases: string[] = []
  const out = await runImageEdit(s.t, request, new AbortController().signal, (p) => phases.push(p))
  assert.equal(out.kind, 'applied')
  assert.deepEqual(phases, ['encoding', 'applying', 'confirming'])
  assert.equal(s.calls.filter((c) => c === 'create').length, 1)

  // 还没提交就断了：重发的请求体与第一次完全相同。
  const lost = fakeServer({ applyErrors: [new TypeError('Failed to fetch')] })
  assert.equal((await runImageEdit(lost.t, request, new AbortController().signal)).kind, 'applied')
  assert.equal(lost.applyBodies.length, 2)
  assert.deepEqual(lost.applyBodies[0], lost.applyBodies[1])
})

test('采用被明确拒绝：说明原因并释放候选；元数据冲突换新数据版本重试', async () => {
  const changed = fakeServer({ applyErrors: [{ status: 409, code: 'creative_image_edit_source_changed', message: '' }] })
  const out = await runImageEdit(changed.t, request, new AbortController().signal)
  assert.ok(out.kind === 'failed' && out.message.includes('图片已变化'))
  assert.equal(changed.state(), 'discarded')

  let revision = 3
  const conflict = fakeServer({ applyErrors: [{ status: 409, code: 'creative_revision_conflict', message: '' }] })
  conflict.t.sleep = async () => void revision++
  const retried = await runImageEdit(conflict.t, { ...request, dataRevision: () => String(revision) }, new AbortController().signal)
  assert.equal(retried.kind, 'applied')
  const revisions = conflict.applyBodies.map((b) => Number((b as { payload: { expected_data_revision: string } }).payload.expected_data_revision))
  assert.equal(revisions.length, 2)
  assert.ok(revisions[1] > revisions[0], `revisions ${revisions.join(',')}`)
})

test('节点上已有未完成的编辑：带出候选标识，交给界面选择放弃', async () => {
  const s = fakeServer({ createErrors: [{ status: 409, code: 'creative_image_edit_node_busy', message: '', details: { render_id: 'cier_old' } }] })
  const out = await runImageEdit(s.t, request, new AbortController().signal)
  assert.deepEqual(out, { kind: 'failed', message: '这张图片还有一项未完成的编辑', busyRenderID: 'cier_old' })
})
