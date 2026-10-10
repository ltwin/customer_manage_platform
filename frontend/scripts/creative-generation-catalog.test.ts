import test from 'node:test'
import assert from 'node:assert/strict'
import {
  createGenerationCatalog,
  generationUnavailable,
  modelsForNode,
  staleAfter,
} from '../src/creative-canvas/editor/generationCatalog.ts'
import type { GenerationCatalog } from '../src/creative-canvas/editor/generation.ts'
import type { CanvasNode } from '../src/creative-canvas/editor/api.ts'

const catalog = (...keys: string[]): GenerationCatalog => ({
  models: keys.map((key) => ({
    key,
    label: key,
    kind: 'image',
    configuration_revision: '1',
    parameters: [],
  })),
  disabled_reason: '',
})

// 可控的读取与会话：每次 read 返回一个由测试决定何时结束的请求。
function harness() {
  let epoch = 1
  let now = 0
  let onAuth = () => {}
  const reads: {
    resolve: (c: GenerationCatalog) => void
    reject: (e: Error) => void
    signal: AbortSignal
  }[] = []
  const store = createGenerationCatalog({
    read: (signal) =>
      new Promise((resolve, reject) => reads.push({ resolve, reject, signal })),
    epoch: () => epoch,
    subscribeAuth: (listener) => {
      onAuth = listener
      return () => {}
    },
    now: () => now,
  })
  return {
    store,
    reads,
    login() {
      epoch += 1
      onAuth()
    },
    advance(ms: number) {
      now += ms
    },
  }
}

test('并发预取只发一次请求，读到后不再请求', async () => {
  const h = harness()
  const first = h.store.preload()
  const second = h.store.preload()
  h.store.refresh()
  assert.equal(h.reads.length, 1)
  h.reads[0]!.resolve(catalog('a'))
  await Promise.all([first, second])
  assert.deepEqual(h.store.getSnapshot().catalog, catalog('a'))
  await h.store.preload()
  assert.equal(h.reads.length, 1)
})

test('会话一变立即清空，旧会话的结果和失败都不写入', async () => {
  const h = harness()
  // A 读到目录后换成 B：清空并重读。
  const a = h.store.preload()
  h.reads[0]!.resolve(catalog('a'))
  await a
  h.login()
  assert.equal(h.store.getSnapshot().catalog, null)
  const b = h.store.preload()
  assert.equal(h.reads.length, 2)
  h.reads[1]!.resolve(catalog('b'))
  await b
  assert.deepEqual(h.store.getSnapshot().catalog, catalog('b'))

  // 退出后重新登录（同一账号也是新会话）：乱序返回的旧响应被丢弃。
  h.login()
  const stale = h.store.preload()
  assert.equal(h.reads.length, 3)
  h.login()
  assert.equal(h.reads[2]!.signal.aborted, true)
  const fresh = h.store.preload()
  h.reads[3]!.resolve(catalog('new'))
  h.reads[2]!.resolve(catalog('old'))
  await Promise.all([stale, fresh])
  assert.deepEqual(h.store.getSnapshot().catalog, catalog('new'))

  // 旧会话请求的失败也不改新会话的状态。
  h.login()
  const failing = h.store.preload()
  h.login()
  h.reads[4]!.reject(new Error('旧会话失败'))
  await failing
  assert.deepEqual(h.store.getSnapshot(), { catalog: null, error: '' })
})

test('刷新失败保留旧目录；没有旧目录时才报错', async () => {
  const h = harness()
  const first = h.store.preload()
  h.reads[0]!.reject(new Error('网络断开'))
  await first
  assert.deepEqual(h.store.getSnapshot(), { catalog: null, error: '网络断开' })
  const retry = h.store.refresh()
  h.reads[1]!.resolve(catalog('a'))
  await retry
  assert.deepEqual(h.store.getSnapshot(), { catalog: catalog('a'), error: '' })
  const again = h.store.refresh()
  h.reads[2]!.reject(new Error('网络断开'))
  await again
  assert.deepEqual(h.store.getSnapshot(), { catalog: catalog('a'), error: '' })
})

test('回到前台只在目录过期时重读', async () => {
  const h = harness()
  const first = h.store.preload()
  h.reads[0]!.resolve(catalog('a'))
  await first
  h.advance(staleAfter - 1)
  await h.store.refreshIfStale()
  assert.equal(h.reads.length, 1)
  h.advance(1)
  const stale = h.store.refreshIfStale()
  assert.equal(h.reads.length, 2)
  h.reads[1]!.resolve(catalog('b'))
  await stale
  assert.deepEqual(h.store.getSnapshot().catalog, catalog('b'))
})

test('节点不可用原因按版本、归档、账号、类型的顺序判断', () => {
  const node = (type_key: string, type_version = 1) =>
    ({ metadata: { type_key, type_version } }) as CanvasNode
  const ready = catalog('gpt-image')
  const image = node('core.image')
  assert.equal(generationUnavailable(ready, { archived: false }, image), '')
  // 能找到模型也要挡住：目录不再因节点不可用而清空。
  assert.equal(modelsForNode(ready, node('core.image', 2)).length, 1)
  assert.equal(
    generationUnavailable(ready, { archived: true }, node('core.image', 2)),
    '此节点版本暂不支持生成',
  )
  assert.equal(
    generationUnavailable(ready, { archived: true }, image),
    '项目已归档，恢复后可生成',
  )
  assert.equal(
    generationUnavailable(
      { models: [], disabled_reason: '账号当前不可用' },
      { archived: false },
      image,
    ),
    '账号当前不可用',
  )
  assert.equal(
    generationUnavailable(ready, { archived: false }, node('core.video')),
    '此节点类型尚未配置生成模型，仍可保存提示词和查看作品版本',
  )
  assert.deepEqual(modelsForNode(null, image), [])
})
