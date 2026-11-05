import test from 'node:test'
import assert from 'node:assert/strict'
import { Material, Mesh, RenderTarget } from 'three'
import { BuildLifetime, buildLifetime } from '../src/creative-scene/render/buildLifetime.ts'
import { ViewportTextureNode, type WebGPURenderer } from 'three/webgpu'

const deferred = () => {
  let resolve!: () => void
  const promise = new Promise<void>((r) => { resolve = r })
  return { promise, resolve }
}

// Exercise the browser's yielding path with controllable task boundaries, without a GPU or wall-clock sleeps.
let yieldPage = () => Promise.resolve()
Object.defineProperty(globalThis, 'scheduler', { configurable: true, value: { yield: () => yieldPage() } })
const { drawBuilt, builds } = await import('../src/creative-scene/render/deferredBuild.ts')
const { ProbeVolume } = await import('../src/creative-scene/render/probes/volume.ts')

function renderer() {
  const material = new Material()
  const cache = new Map<string, { usedTimes: number }>()
  const stats = { created: 0, stages: 0, draws: 0 }
  const object = { material, object: new Mesh(), lightsNode: null, onDispose() {} }
  material.addEventListener('dispose', () => object.onDispose())
  const target = new RenderTarget()
  let current: RenderTarget | null = target
  let mrt: unknown = null
  const beforeNodes: ViewportTextureNode[] = []
  const pipelineEvents: string[] = []
  let pipelineReady = true
  let pipelineError = false
  const pipeline = {}
  const r = {
    backend: { get: () => ({ error: pipelineError }) },
    contextNode: {},
    getRenderTarget: () => current,
    setRenderTarget: (t: RenderTarget | null) => { current = t },
    getActiveCubeFace: () => 0,
    getActiveMipmapLevel: () => 0,
    getMRT: () => mrt,
    setMRT: (m: unknown) => { mrt = m },
    _objects: { get: () => object },
    _nodes: {
      get: () => ({}),
      getForRender: () => ({ updateBeforeNodes: beforeNodes }),
      getNodeFrameForRender: () => ({ updateBeforeNode: () => { pipelineEvents.push('copy') } }),
      getForRenderCacheKey: () => 'same-key',
      nodeBuilderCache: cache,
      _createNodeBuilder: () => {
        stats.created++
        return {
          context: {}, flowNodes: { vertex: [], fragment: [{ build: () => { stats.stages++ } }], compute: [] },
          prebuild() {}, setBuildStage() {}, setShaderStage() {}, flowNode() { stats.stages++ },
          buildCode() {}, buildUpdateNodes() {},
        }
      },
      _createNodeBuilderState: () => ({ usedTimes: 0 }),
    },
    _geometries: { updateForRender() {} },
    _bindings: { getForRender() { pipelineEvents.push('bind') } },
    _pipelines: {
      isReady: () => pipelineReady,
      getForRender(_object: unknown, promises: Promise<void>[]) {
        pipelineEvents.push('compile')
        if (pipelineError) promises.push(Promise.resolve())
        else pipelineReady = true
        return pipeline
      },
    },
    _renderObjectDirect() { stats.draws++ },
  }
  const gpu = r as unknown as WebGPURenderer
  return { gpu, material, target, object, cache, stats, beforeNodes, pipelineEvents, unready: (failed = false) => { pipelineReady = false; pipelineError = failed }, draw: () => drawBuilt(gpu, () => r._renderObjectDirect()) }
}

test('glass copies its current framebuffer before precompiled bindings allocate its texture', async () => {
  const r = renderer()
  r.beforeNodes.push(new ViewportTextureNode())
  r.unready()
  r.draw()
  await builds(r.gpu)
  assert.equal(r.draw(), true)
  assert.deepEqual(r.pipelineEvents, ['copy', 'bind', 'compile'])
})

test('a refused GPU pipeline whose promise resolves stops waiting and reaches the normal failure path', async () => {
  const r = renderer()
  r.unready(true)
  r.draw()
  await builds(r.gpu)
  assert.equal(r.draw(), false, 'pipeline compilation started')
  await builds(r.gpu)
  assert.equal(r.draw(), true, 'a resolved failure is terminal, not another compile')
  assert.equal(r.stats.draws, 1, 'the renderer can run its existing failure checks')
  assert.equal(r.pipelineEvents.filter(e => e === 'compile').length, 1)
})

test('a material disposed before its queued build starts is never compiled or cached', async () => {
  const r = renderer()
  assert.equal(r.draw(), false)
  r.material.dispose()
  await builds(r.gpu)
  assert.equal(r.stats.created, 0)
  assert.equal(r.cache.size, 0)
})

test('material, render-object, target and device disposal stop a build at the next yield', async () => {
  for (const kind of ['material', 'object', 'target', 'device']) {
    const entered = deferred()
    const resume = deferred()
    const r = renderer()
    let released = false
    yieldPage = () => { entered.resolve(); return resume.promise }
    r.draw()
    await entered.promise
    if (kind === 'material') r.material.dispose()
    if (kind === 'object') r.object.onDispose()
    if (kind === 'target') r.target.dispose()
    if (kind === 'device') buildLifetime(r.gpu).close(() => { released = true })
    assert.equal(released, false, 'device release waits for the builder')
    resume.resolve()
    await builds(r.gpu)
    await Promise.resolve()
    assert.equal(r.stats.stages, 0, kind)
    assert.equal(r.cache.size, 0, kind)
    if (kind === 'device') assert.equal(released, true)
  }
  yieldPage = () => Promise.resolve()
})

test('an unconsumed shader is removed when its render object goes away', async () => {
  const r = renderer()
  r.draw()
  await builds(r.gpu)
  assert.equal(r.cache.get('same-key')?.usedTimes, 0)
  r.material.dispose()
  assert.equal(r.cache.size, 0)
})

test('renderers with equal material cache keys build independently', async () => {
  const a = renderer()
  const b = renderer()
  a.draw()
  b.draw()
  await Promise.all([builds(a.gpu), builds(b.gpu)])
  assert.equal(a.stats.created, 1)
  assert.equal(b.stats.created, 1)
  assert.equal(a.draw(), true)
  assert.equal(b.draw(), true)
})

test('renderer release follows pending compilation and the cleanup it schedules', async () => {
  const lifetime = new BuildLifetime()
  const ready = deferred()
  const events: string[] = []
  const task = lifetime.track(ready.promise.then(() => { events.push('compiled') }))
  lifetime.track(task.then(() => { events.push('kernels disposed') }))
  lifetime.close(() => { events.push('device disposed') })
  assert.equal(lifetime.closed, true)
  assert.deepEqual(events, [])
  ready.resolve()
  await lifetime.settled()
  await Promise.resolve()
  assert.deepEqual(events, ['compiled', 'kernels disposed', 'device disposed'])
})

test('retired probes keep kernels, buffers and atlases until compute compilation has completed', async () => {
  for (const failed of [false, true]) {
    const ready = deferred()
    const events: string[] = []
    const bindings = new Map<object, { bindings?: unknown[] }>()
    const r = {
      _bindings: { get(c: object) { let d = bindings.get(c); if (!d) bindings.set(c, (d = {})); return d } },
      _nodes: { get: () => ({}) },
    }
    const kernels = {
      ready: false, compiled: Promise.resolve(),
      ...Object.fromEntries(['rays', 'update', 'rescale'].map((name) => {
        const c = { dispose() {
          assert.ok(r._bindings.get(c).bindings, 'disposing an unfinished shader must not trigger a synchronous rebuild')
          events.push(name)
        } }
        return [name, c]
      })),
    }
    const pending = new BuildLifetime()
    kernels.compiled = buildLifetime(r).track(pending.track(ready.promise.then(() => {
      if (!failed) events.push('pipeline created')
      kernels.ready = true
    })))
    const volume = Object.assign(Object.create(ProbeVolume.prototype), {
      renderer: r, builds: pending, kernels, disposed: false, buffers: [{ value: {} }],
      data: { atlases: { color: { dispose: () => events.push('atlas released') } } },
      release: () => events.push('buffer released'),
    }) as InstanceType<typeof ProbeVolume>
    volume.dispose()
    volume.dispose()
    buildLifetime(r).close(() => { events.push('device released') })
    assert.deepEqual(events, [], 'nothing is released while the compiler can still allocate')
    ready.resolve()
    await buildLifetime(r).settled()
    await Promise.resolve()
    assert.deepEqual(events, [...(failed ? [] : ['pipeline created']), 'rays', 'update', 'rescale', 'buffer released', 'atlas released', 'device released'])
  }
})
