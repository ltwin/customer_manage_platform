// S0 group 2, throughput and limits (scene-trace-spike design「判定标准」组 2): over a reference scene or the stress
// scene, under the limits the page asked for,
//   prepare  each geometry's original BVH, proxy and proxy BVH from the worker pool, or from the geometry cache
//   pack     the original layer and the proxy layer (TraceLayer), each if it fits the device; bytes, depths, tier
//   ready    from the scene built to the first traced frame of both, uploads and compiles included
//   rays     the four ray kinds on each layer: GPU time (timestamps, median of repeats) and rays a millisecond
//   check    the original layer's primary hits against the raster's distance, pixel by pixel (the layer's own packing)
//   edit     dragging one object for 60 frames (ranged uploads, and whole as the library's updateTransforms), adding
//            an object of a geometry already packed, and of a new 300k-triangle geometry
import { Box3, BufferAttribute, BufferGeometry, DoubleSide, FloatType, Mesh, RenderTarget, Vector3, type Material, type Object3D } from 'three'
import { cameraPosition, positionWorld, vec4 } from 'three/tsl'
import { MeshBasicNodeMaterial, type WebGPURenderer } from 'three/webgpu'
import { MeshBVH } from 'three-mesh-bvh'
import { ClusteredBVH } from 'three-mesh-bvh/src/webgpu/ClusteredBVH.js'
import { generate } from '../../scripts/fixtures/scene-stress/generate.mjs'
import type { BuiltGeometry } from './build.worker.ts'
import { CapacityError, TraceLayer } from './layer.ts'
import { BuildPool, geometryCache } from './pool.ts'
import { rayKernels, type Kind } from './rays.ts'
import type { Built } from './scene.ts'

type Device = { limits: Record<string, number>; queue: { onSubmittedWorkDone(): Promise<void> } }
export type Source = { key: string; geometry: BufferGeometry; meshes: Mesh[]; epsilon: number }
export type Pair = { original: MeshBVH; proxy: MeshBVH; built: BuiltGeometry }

const median = (v: number[]) => [...v].sort((a, b) => a - b)[Math.floor(v.length / 2)]
const round = (v: number, d = 2) => Math.round(v * 10 ** d) / 10 ** d
const MiB = 2 ** 20

/** The proxies' error bound in metres for these timings (?eps=, 1 cm by default): group 5 picks ε_probe. */
let EPSILON = 0.01

/** Long tasks on the main thread while f runs: how long the page could not answer. */
async function blocking<T>(f: () => Promise<T>) {
  const tasks: number[] = []
  const observer = new PerformanceObserver((list) => list.getEntries().forEach((e) => tasks.push(e.duration)))
  observer.observe({ entryTypes: ['longtask'] })
  const started = performance.now()
  const value = await f()
  const wallMs = performance.now() - started
  await new Promise((r) => setTimeout(r, 0))
  observer.disconnect()
  return { value, wallMs, longTasks: tasks.length, longestTaskMs: Math.max(0, ...tasks), blockedMs: tasks.reduce((a, b) => a + b, 0) }
}

/** The scene's geometries, each once, with every mesh drawing it and its ε in its own units. */
function sources(scene: string, built: Built, epsilon: number): Source[] {
  const byGeometry = new Map<BufferGeometry, Mesh[]>()
  for (const m of built.meshes) byGeometry.set(m.geometry, [...(byGeometry.get(m.geometry) ?? []), m])
  return [...byGeometry].map(([geometry, meshes], i) => {
    if (!geometry.index) geometry.setIndex([...Array(geometry.attributes.position.count).keys()])
    if (!geometry.attributes.normal) geometry.computeVertexNormals()
    const scale = Math.max(...meshes.map((m) => m.matrixWorld.getMaxScaleOnAxis()))
    return { key: `${scene}/${i}/${geometry.index!.count / 3}/${epsilon}`, geometry, meshes, epsilon: epsilon / scale }
  })
}

function job(s: Source) {
  const f32 = (a: BufferAttribute) => (a.array instanceof Float32Array && a.itemSize === 3 ? a.array : new Float32Array(Array.from({ length: a.count * 3 }, (_, k) => a.getComponent(Math.floor(k / 3), k % 3))))
  return {
    key: s.key,
    positions: f32(s.geometry.attributes.position as BufferAttribute),
    normals: f32(s.geometry.attributes.normal as BufferAttribute),
    index: new Uint32Array(s.geometry.index!.array),
    epsilon: s.epsilon,
  }
}

/** A built geometry back as trees: the original over the raster's own geometry (its index reordered, as placement's boundsTree does), the proxy over its own. */
function trees(s: Source, b: BuiltGeometry): Pair {
  s.geometry.setIndex(new BufferAttribute(b.original.index, 1))
  const original = MeshBVH.deserialize({ version: 1, roots: b.original.roots, index: b.original.index, indirectBuffer: null }, s.geometry, { setIndex: false })
  const proxyGeometry = new BufferGeometry()
  proxyGeometry.setAttribute('position', new BufferAttribute(b.proxy.positions, 3))
  proxyGeometry.setAttribute('normal', new BufferAttribute(b.proxy.normals, 3))
  proxyGeometry.setIndex(new BufferAttribute(b.proxy.index, 1))
  const proxy = MeshBVH.deserialize({ version: 1, roots: b.proxy.roots, index: b.proxy.index, indirectBuffer: null }, proxyGeometry, { setIndex: false })
  return { original, proxy, built: b }
}

/** Each geometry's trees at ε (metres): from the cache where it has them, the rest from the pool (and into the cache). */
export async function prepare(scene: string, built: Built, cache: 'clear' | 'use', epsilon = EPSILON) {
  if (cache === 'clear') await geometryCache.clear()
  const list = sources(scene, built, epsilon)
  const pool = new BuildPool()
  const cachedMs = { get: 0, put: 0 }
  const run = await blocking(async () => {
    let t = performance.now()
    const hits = await geometryCache.get(list.map((s) => s.key))
    cachedMs.get = performance.now() - t
    const pairs = await Promise.all(list.map(async (s) => trees(s, hits.get(s.key) ?? (await pool.run(job(s))))))
    return { pairs, hits: hits.size }
  })
  pool.dispose()
  const pairs = new Map(list.map((s, i) => [s.geometry, run.value.pairs[i]]))
  let t = performance.now()
  if (run.value.hits < list.length) for (const p of run.value.pairs) await geometryCache.put(p.built)
  cachedMs.put = performance.now() - t
  const all = run.value.pairs.map((p) => p.built)
  const sum = (f: (b: BuiltGeometry) => number) => all.reduce((n, b) => n + f(b), 0)
  const originalTriangles = sum((b) => b.original.index.length / 3)
  const proxyTriangles = sum((b) => b.proxy.triangles)
  return {
    pairs,
    list,
    report: {
      geometries: list.length,
      fromCache: run.value.hits,
      built: list.length - run.value.hits,
      workers: pool.size,
      wallMs: Math.round(run.wallMs),
      main: { longTasks: run.longTasks, longestTaskMs: Math.round(run.longestTaskMs), blockedMs: Math.round(run.blockedMs) },
      cacheMs: { get: Math.round(cachedMs.get), put: Math.round(cachedMs.put) },
      cacheBytes: await geometryCache.usage(),
      // Each geometry's own work, summed (it ran on pool.size workers at once).
      workerMs: { bvh: Math.round(sum((b) => b.ms.bvh)), simplify: Math.round(sum((b) => b.ms.simplify)), proxyBvh: Math.round(sum((b) => b.ms.proxyBvh)) },
      slowest: (({ key, ms, original, proxy }) => ({ key, ms, triangles: original.index.length / 3, proxyTriangles: proxy.triangles }))(all.reduce((a, b) => (b.ms.bvh + b.ms.simplify + b.ms.proxyBvh > a.ms.bvh + a.ms.simplify + a.ms.proxyBvh ? b : a), all[0])),
      triangles: { original: originalTriangles, proxy: proxyTriangles, ratio: round(proxyTriangles / originalTriangles, 4) },
      proxyError: { max: Math.max(...all.map((b, i) => b.proxy.error * list[i].meshes.reduce((s, m) => Math.max(s, m.matrixWorld.getMaxScaleOnAxis()), 0))), bound: epsilon },
      depth: { original: Math.max(...all.map((b) => b.original.depth)), proxy: Math.max(...all.map((b) => b.proxy.depth)) },
    },
  }
}

/** What the library's own top level (clusters of < 64 triangles a leaf) would hold for these meshes. */
function libraryLeaves(meshes: Mesh[], pairs: Map<BufferGeometry, Pair>) {
  const count = (bvh: MeshBVH) => (ClusteredBVH.prototype as unknown as { _countRelevantLeafNodes(this: { primitiveLimit: number }, b: MeshBVH): number })._countRelevantLeafNodes.call({ primitiveLimit: 64 }, bvh)
  const perGeometry = new Map([...pairs].map(([g, p]) => [g, count(p.original)]))
  return meshes.reduce((n, m) => n + perGeometry.get(m.geometry)!, 0)
}

function pack(name: 'original' | 'proxy', meshes: Mesh[], pairs: Map<BufferGeometry, Pair>, maxBinding: number) {
  const bvhOf = (o: Object3D) => pairs.get((o as Mesh).geometry)![name]
  const need = TraceLayer.need(meshes.map(bvhOf))
  const words = { nodes: 8 * 4, index: 4, attributes: 32 }
  const needBytes = { nodes: (need.nodes + need.tlasNodes) * words.nodes, index: need.indices * words.index, attributes: need.vertices * words.attributes }
  const layer = new TraceLayer(meshes, bvhOf, maxBinding)
  meshes.forEach((m, i) => layer.materialOf.set(m, i))
  const started = performance.now()
  try {
    layer.update()
  } catch (e) {
    if (!(e instanceof CapacityError)) throw e
    return { layer: null, report: { fits: false, buffer: e.buffer, bytes: e.bytes, limit: e.limit, needBytes, need } }
  }
  return {
    layer,
    report: { fits: true, packMs: Math.round(performance.now() - started), bytes: layer.bytes, mib: round(layer.bytes / MiB, 1), needBytes, need, depth: layer.depth, tlasNodes: layer.top._roots[0].byteLength / 32, uploadBytes: layer.take() },
  }
}

/** The box the probes fill: the scene's bounds, within 12 m of the camera across and 2 m up and down. */
function probeBox(built: Built) {
  const box = new Box3()
  built.meshes.forEach((m) => box.expandByObject(m))
  const eye = built.camera.position
  const reach = new Box3(eye.clone().sub(new Vector3(12, 2, 12)), eye.clone().add(new Vector3(12, 2, 12)))
  box.intersect(reach)
  return box.expandByScalar(-0.05)
}

export async function runThroughput(renderer: WebGPURenderer, built: Built, scene: string, query: URLSearchParams) {
  const device = (renderer.backend as unknown as { device: Device }).device
  const maxBinding = Math.min(device.limits.maxStorageBufferBindingSize, device.limits.maxBufferSize)
  const cache = query.get('cache') === 'clear' ? 'clear' : 'use'
  EPSILON = Number(query.get('eps') ?? 0.01)
  const parts = new Set((query.get('part') ?? 'rays').split(','))
  const width = 1536, height = 1024
  const result: Record<string, unknown> = { scene, maxBinding, cache, epsilon: EPSILON }

  const readyStarted = performance.now()
  const prepared = await prepare(scene, built, cache)
  result.prepare = prepared.report
  const meshes = built.meshes
  if (scene !== 'stress' || parts.has('leaves')) result.libraryTopLeaves = libraryLeaves(meshes, prepared.pairs)
  const layers: Partial<Record<'original' | 'proxy', TraceLayer>> = {}
  const packs: Record<string, unknown> = {}
  for (const name of ['original', 'proxy'] as const) {
    const p = pack(name, meshes, prepared.pairs, maxBinding)
    packs[name] = p.report
    if (p.layer) layers[name] = p.layer
  }
  result.pack = packs
  // Epic「能力与容量」: 1 both fit (reflections trace the original), 2 only the proxy, none neither.
  result.tier = layers.original && layers.proxy ? 1 : layers.proxy ? 2 : 0
  const grid: [number, number, number] = [16, 8, 16]
  const probes = { box: probeBox(built), grid, rays: 64 }
  const kernels = Object.fromEntries(Object.entries(layers).map(([name, layer]) => [name, rayKernels(layer, width, height, probes)]))
  for (const k of Object.values(kernels)) k.aim(built.camera)

  // Ready: the first traced frame of each layer (upload, compile and trace) and a raster frame, waited for.
  const firstStarted = performance.now()
  for (const k of Object.values(kernels)) renderer.compute(k.kernels.primary)
  renderer.render(built.scene, built.camera)
  await device.queue.onSubmittedWorkDone()
  result.ready = { totalMs: Math.round(performance.now() - readyStarted), firstFrameMs: Math.round(performance.now() - firstStarted), sceneBuildMs: Math.round(built.buildMs) }
  await renderer.resolveTimestampsAsync('compute')
  await renderer.resolveTimestampsAsync('render')

  const time = async (kernel: unknown, frame: number) => {
    for (const k of Object.values(kernels)) k.reseed(frame)
    renderer.compute(kernel as never)
    return (await renderer.resolveTimestampsAsync('compute')) as number
  }
  const read = async (k: (typeof kernels)[string]) => new Float32Array(await renderer.getArrayBufferAsync(k.out.value as never))

  if (parts.has('rays')) {
    const rays: Record<string, unknown> = {}
    for (const [name, k] of Object.entries(kernels)) {
      const row: Record<string, unknown> = {}
      // The G-buffer the hemisphere and reflection rays start from, and how many pixels it has.
      await time(k.kernels.primary, 0)
      const g = await read(k)
      const hitPixels = g.reduce((n, d) => n + (d >= 0 ? 1 : 0), 0)
      for (const kind of ['primary', 'hemisphere', 'reflection', 'probe', 'shadow'] as Kind[]) {
        if (kind !== 'primary') await time(k.kernels.primary, 0)
        // Warm-up, then repeats: the GPU's clocks vary from run to run (integration.md).
        for (let f = 0; f < 3; f++) await time(k.kernels[kind], f)
        const ms: number[] = []
        for (let f = 3; f < 14; f++) {
          ms.push(await time(k.kernels[kind], f))
        }
        const out = await read(k)
        const traced = kind === 'hemisphere' || kind === 'reflection' || kind === 'shadow' ? hitPixels : k.rays[kind]
        const hits = out.subarray(0, k.rays[kind]).reduce((n, d) => n + (d >= 0 ? 1 : 0), 0)
        const m = median(ms)
        row[kind] = { rays: traced, hitRate: round(hits / traced, 3), medianMs: round(m, 3), minMs: round(Math.min(...ms), 3), maxMs: round(Math.max(...ms), 3), raysPerMs: Math.round(traced / m) }
      }
      rays[name] = row
    }
    result.rays = rays
  }

  if (parts.has('check') && kernels.original) {
    await time(kernels.original.kernels.primary, 0)
    result.check = await distanceCheck(renderer, built, width, height, await read(kernels.original))
  }

  if (parts.has('edit')) result.edit = await edit(renderer, device, built, layers, kernels, prepared, scene)
  for (const layer of Object.values(layers)) layer.dispose()
  return result
}

/**
 * Traced primary distances (-1 a miss) against the raster's, with one material for every mesh (its distance from the
 * camera): off where one hits and the other does not, or the distances differ by more than 0.5%; pixels beside a jump in
 * the raster's distance (an edge) are left out. WebGPU reads the target back from its top row, as the trace's rows run.
 */
async function distanceCheck(renderer: WebGPURenderer, built: Built, width: number, height: number, traced: Float32Array) {
  const target = new RenderTarget(width, height, { type: FloatType })
  const keep = built.meshes.map((m) => m.material)
  const background = built.scene.background
  const material = new MeshBasicNodeMaterial()
  material.colorNode = vec4(positionWorld.sub(cameraPosition).length(), 1, 0, 1)
  // Both faces, as the traversal hits them: the stress scene's surfaces are open (tiles, columns' ends).
  material.side = DoubleSide
  built.meshes.forEach((m) => (m.material = material))
  built.scene.background = null
  renderer.setClearColor(0x000000, 0)
  renderer.setRenderTarget(target)
  renderer.clear()
  renderer.render(built.scene, built.camera)
  renderer.setRenderTarget(null)
  const raster = (await renderer.readRenderTargetPixelsAsync(target, 0, 0, width, height)) as Float32Array
  built.meshes.forEach((m, i) => (m.material = keep[i] as Material))
  built.scene.background = background
  target.dispose()
  const d = (p: number) => (raster[4 * p + 1] > 0 ? raster[4 * p] : -1)
  let compared = 0, edges = 0, off = 0, missMismatch = 0, nearer = 0
  for (let y = 1; y < height - 1; y++) {
    for (let x = 1; x < width - 1; x++) {
      const p = y * width + x
      const rd = d(p)
      if ([p - 1, p + 1, p - width, p + width].some((q) => (d(q) < 0) !== (rd < 0) || Math.abs(d(q) - rd) > 0.02 * Math.max(rd, d(q), 1e-3))) {
        edges++
        continue
      }
      compared++
      const td = traced[p]
      if ((td >= 0) !== (rd >= 0)) {
        missMismatch++
        off++
      } else if (rd >= 0 && Math.abs(td - rd) > 0.005 * rd) {
        off++
        if (td < rd) nearer++
      }
    }
  }
  return { compared, edges, off, missMismatch, tracedNearer: nearer, offFraction: off / compared }
}

/** A frame as the viewport would draw it once the layer is in: the proxy's probes, the original's reflections, the raster. */
async function frame(renderer: WebGPURenderer, device: Device, built: Built, kernels: Record<string, ReturnType<typeof rayKernels>>, before: () => number) {
  const started = performance.now()
  const bytes = before()
  if (kernels.proxy) renderer.compute(kernels.proxy.kernels.probe)
  const reflect = kernels.original ?? kernels.proxy
  if (reflect) {
    renderer.compute(reflect.kernels.primary)
    renderer.compute(reflect.kernels.reflection)
  }
  renderer.render(built.scene, built.camera)
  const cpuMs = performance.now() - started
  await device.queue.onSubmittedWorkDone()
  const wallMs = performance.now() - started
  const gpuMs = ((await renderer.resolveTimestampsAsync('compute')) ?? 0) + ((await renderer.resolveTimestampsAsync('render')) ?? 0)
  return { cpuMs, wallMs, gpuMs, bytes }
}

type Frame = { cpuMs: number; wallMs: number; gpuMs: number; bytes: number }
const summary = (frames: Frame[]) => ({
  frames: frames.length,
  medianWallMs: round(median(frames.map((f) => f.wallMs))),
  maxWallMs: round(Math.max(...frames.map((f) => f.wallMs))),
  medianCpuMs: round(median(frames.map((f) => f.cpuMs))),
  medianGpuMs: round(median(frames.map((f) => f.gpuMs))),
  maxGpuMs: round(Math.max(...frames.map((f) => f.gpuMs))),
  bytesPerFrame: Math.round(median(frames.map((f) => f.bytes))),
})

async function edit(
  renderer: WebGPURenderer,
  device: Device,
  built: Built,
  layers: Partial<Record<'original' | 'proxy', TraceLayer>>,
  kernels: Record<string, ReturnType<typeof rayKernels>>,
  prepared: Awaited<ReturnType<typeof prepare>>,
  scene: string,
) {
  const all = Object.values(layers)
  const take = () => all.reduce((n, l) => n + l.take(), 0)
  const out: Record<string, unknown> = {}
  // The raster alone, as the viewport draws it today.
  const raster: Frame[] = []
  for (let f = 0; f < 30; f++) raster.push(await frame(renderer, device, built, {}, () => 0))
  out.rasterOnly = summary(raster)
  const still: typeof raster = []
  for (let f = 0; f < 30; f++) still.push(await frame(renderer, device, built, kernels, take))
  out.still = summary(still)

  // Drag: the object nearest the camera's aim, 5 cm a frame.
  const aim = built.camera.getWorldDirection(new Vector3()).multiplyScalar(4).add(built.camera.position)
  const small = built.meshes.filter((m) => new Box3().setFromObject(m).getSize(new Vector3()).length() < 6)
  const moved = small.reduce((a, b) => (b.getWorldPosition(new Vector3()).distanceTo(aim) < a.getWorldPosition(new Vector3()).distanceTo(aim) ? b : a))
  const drag = async (whole: boolean) => {
    const frames: Frame[] = []
    const updateMs: number[] = []
    for (let f = 0; f < 60; f++) {
      frames.push(
        await frame(renderer, device, built, kernels, () => {
          const t = performance.now()
          moved.matrixAutoUpdate = false
          moved.matrix.elements[12] += 0.05 * (f < 30 ? 1 : -1)
          moved.updateMatrixWorld(true)
          let bytes = 0
          for (const l of all) {
            l.updateTransforms(whole ? null : [moved])
            bytes += l.take()
            if (whole) {
              // As the library's updateTransforms: the whole node buffer, and every slot, every frame.
              const nodes = (l.storage as unknown as { nodes: { proxyNode: { value: { array: Uint32Array; updateRanges: unknown[]; needsUpdate: boolean } } } }).nodes.proxyNode.value
              nodes.updateRanges.length = 0
              nodes.needsUpdate = true
              bytes = bytes - l.top._roots[0].byteLength + nodes.array.byteLength
            }
          }
          updateMs.push(performance.now() - t)
          return bytes
        }),
      )
    }
    return { ...summary(frames), updateMedianMs: round(median(updateMs)), updateMaxMs: round(Math.max(...updateMs)) }
  }
  out.drag = { object: moved.name || built.meshes.indexOf(moved), ranged: await drag(false), wholeAsLibrary: await drag(true) }

  // Add an object of a geometry already packed: the top level again.
  const copy = new Mesh(moved.geometry, moved.material)
  copy.matrixAutoUpdate = false
  copy.matrix.copy(moved.matrix).elements[14] += 0.6
  built.scene.add(copy)
  copy.updateMatrixWorld(true)
  {
    let ms = 0
    const f = await frame(renderer, device, built, kernels, () => {
      const t = performance.now()
      for (const l of all) {
        ;(l as unknown as { objects: Object3D[] }).objects.push(copy)
        l.materialOf.set(copy, built.meshes.length)
        l.buildTop()
      }
      ms = performance.now() - t
      return take()
    })
    out.addInstance = { mainMs: round(ms), bytes: f.bytes, frame: { wallMs: round(f.wallMs), cpuMs: round(f.cpuMs), gpuMs: round(f.gpuMs) } }
  }

  // Add an object of a new geometry: 300k triangles, built in the pool, written at the tails, the top level again.
  const spec = await (await fetch('/scripts/fixtures/scene-stress/spec.json')).json()
  const g = generate(spec, { only: ['blob-00'] }).geometries.find((x: { id: string }) => x.id === 'blob-00')
  const geometry = new BufferGeometry()
  geometry.setAttribute('position', new BufferAttribute(g.positions, 3))
  geometry.setAttribute('normal', new BufferAttribute(g.normals, 3))
  geometry.setIndex(new BufferAttribute(g.indices, 1))
  const added = new Mesh(geometry, moved.material)
  added.position.copy(aim)
  added.scale.setScalar(scene === 'stress' ? 1 : 0.3)
  built.scene.add(added)
  added.updateMatrixWorld(true)
  const source: Source = { key: `${scene}/added/${g.indices.length / 3}/${EPSILON}`, geometry, meshes: [added], epsilon: EPSILON / added.matrixWorld.getMaxScaleOnAxis() }
  const pool = new BuildPool(1)
  const built1 = await blocking(async () => pool.run(job(source)))
  pool.dispose()
  const pair = trees(source, built1.value)
  prepared.pairs.set(geometry, pair)
  {
    const steps: Record<string, unknown> = {}
    const f = await frame(renderer, device, built, kernels, () => {
      for (const [name, l] of Object.entries(layers) as ['original' | 'proxy', TraceLayer][]) {
        const t = performance.now()
        const fits = l.writeSegment(pair[name])
        const segmentMs = performance.now() - t
        const segmentBytes = l.take()
        ;(l as unknown as { objects: Object3D[] }).objects.push(added)
        l.materialOf.set(added, built.meshes.length + 1)
        let grew = false
        if (!fits) {
          // Out of room: everything laid out again, larger.
          l.update(1.5)
          grew = true
        } else l.buildTop()
        steps[name] = { fits, grew, segmentMs: round(segmentMs), segmentBytes, topMs: round(performance.now() - t - segmentMs), bytes: segmentBytes + l.take() }
      }
      return Object.values(steps).reduce((n: number, s) => n + (s as { bytes: number }).bytes, 0)
    })
    const after = await frame(renderer, device, built, kernels, take)
    out.addGeometry = {
      worker: { wallMs: Math.round(built1.wallMs), ms: built1.value.ms, mainBlockedMs: Math.round(built1.blockedMs), triangles: g.indices.length / 3, proxyTriangles: built1.value.proxy.triangles },
      steps,
      bytes: f.bytes,
      frame: { wallMs: round(f.wallMs), cpuMs: round(f.cpuMs), gpuMs: round(f.gpuMs) },
      next: { wallMs: round(after.wallMs), cpuMs: round(after.cpuMs), gpuMs: round(after.gpuMs) },
    }
  }
  // The library's update(): everything packed and uploaded again, as for any added or removed object.
  out.wholeRepack = Object.fromEntries(Object.entries(layers).map(([name, l]) => {
    const t = performance.now()
    l.update()
    return [name, { ms: Math.round(performance.now() - t), bytes: l.take() }]
  }))
  return out
}
