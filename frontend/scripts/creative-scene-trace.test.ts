import test from 'node:test'
import assert from 'node:assert/strict'
import { BUDGET, buildsOriginal, capabilityOf, EPSILON_PROBE, epsilonAt, levelFor, tierOf, type DeviceFacts, type LayerSize } from '../src/creative-scene/render/trace/capacity.ts'
import { decodeNormal, encodeNormal, encodeNormals, NONE } from '../src/creative-scene/render/trace/normals.ts'

// scene-offscreen-reflections (S1): the world-space layer's pure parts.

const gib = 2 ** 30
const limits = { maxStorageBufferBindingSize: 2 * gib, maxBufferSize: 4 * gib, maxStorageBuffersPerShaderStage: 10 }

test('the layer is built on a WebGPU device with the storage the reflections bind (design「能力与设备上限」)', () => {
  const on = (d: Partial<DeviceFacts>) => capabilityOf({ backend: 'webgpu', compatibility: false, limits, ...d })
  assert.deepEqual(on({}), { supported: true, maxBinding: 2 * gib }, 'one buffer holds the lesser of the binding and buffer limits')
  assert.deepEqual(on({ limits: { ...limits, maxBufferSize: gib } }), { supported: true, maxBinding: gib })
  assert.deepEqual(on({ backend: 'webgl2', limits: null }), { supported: false, reason: 'compatibility' }, 'the WebGL2 backend is compatibility mode')
  assert.deepEqual(on({ compatibility: true }), { supported: false, reason: 'device' }, 'a compatibility-level WebGPU device (forceCompatibility)')
  assert.deepEqual(on({ unsupported: true }), { supported: false, reason: 'device' }, 'the e2e hook traceUnsupported')
  assert.deepEqual(on({ limits: null }), { supported: false, reason: 'device' })
  assert.deepEqual(on({ limits: { ...limits, maxStorageBuffersPerShaderStage: 5 } }), { supported: false, reason: 'device' }, 'six storage buffers in the fragment')
  assert.deepEqual(on({ limits: { ...limits, maxStorageBuffersPerShaderStage: 6 } }), { supported: true, maxBinding: 2 * gib })
  assert.deepEqual(on({ limits: { ...limits, maxStorageBuffersInFragmentStage: 4 } }), { supported: false, reason: 'device' }, 'the fragment stage’s own limit, where the browser has one')
})

test('the original is not built when its estimate is past the budget by half again (D9)', () => {
  // 40 B a triangle with a quarter to spare: 1.5 GiB of estimate is 32.2 M triangles.
  const edge = (1.5 * BUDGET) / (40 * 1.25)
  assert.equal(buildsOriginal(Math.floor(edge), BUDGET), true)
  assert.equal(buildsOriginal(Math.floor(edge) + 1, BUDGET), false)
  assert.equal(buildsOriginal(20_110_000, BUDGET), true, 'S0’s stress scene: built, then tiered by what it takes')
  assert.equal(buildsOriginal(1000, 1000), false, 'gpuTestHooks.traceBudget pressed down')
})

test('the tier follows the layer as built: the original in budget, else the proxies, else too large (design「分档」)', () => {
  const size = (bytes: number, largest = bytes / 2, depth = 40): LayerSize => ({ bytes, largest, depth })
  const binding = 2 * gib
  // Each row: [original, proxy, tier].
  const rows: [LayerSize | null, LayerSize | null, string][] = [
    [size(0.5 * gib), null, 'full'],
    [size(0.79 * gib), null, 'full'],
    [size(0.82 * gib), null, 'tooLarge'], // × 1.25 is past 1 GiB: build the proxies
    [size(0.82 * gib), size(0.2 * gib), 'proxy'],
    [size(0.5 * gib, 1.7 * gib), size(0.2 * gib), 'proxy'], // one buffer × 1.25 past the binding
    [size(0.5 * gib, 0.25 * gib, 61), size(0.2 * gib), 'proxy'], // the original’s traversal overruns the stack
    [null, size(0.2 * gib), 'proxy'], // not built (buildsOriginal)
    [null, size(3 * gib, 1.7 * gib), 'tooLarge'],
    [null, size(0.2 * gib, 0.1 * gib, 61), 'tooLarge'],
    [size(0.5 * gib, 0.25 * gib, 60), null, 'full'],
    [null, null, 'tooLarge'],
  ]
  for (const [original, proxy, tier] of rows) assert.equal(tierOf(original, proxy, BUDGET, binding), tier, JSON.stringify({ original, proxy }))
})

test('a proxy’s level follows the largest scale an instance of it has: within ε_probe where drawn, √2 a level (design「构建、缓存与提交」)', () => {
  assert.deepEqual([0.5, 1, 1.2, 1.5, 2, 3, 10].map(levelFor), [-2, 0, 1, 2, 2, 4, 7])
  for (const s of [0.3, 0.5, 1, 1.2, 1.5, 2, 3, 10, 37]) {
    const bound = epsilonAt(levelFor(s)) * s
    assert.ok(bound <= EPSILON_PROBE + 1e-12, `scaled ${s}: ${bound} where drawn`)
    assert.ok(bound > EPSILON_PROBE / Math.SQRT2 - 1e-12, `scaled ${s}: no finer than it needs`)
  }
})

test('a normal goes into a float exactly and comes back within 0.12° (D4)', () => {
  let worst = 0
  const check = (x: number, y: number, z: number) => {
    const w = encodeNormal(x, y, z)
    assert.ok(Number.isInteger(w) && w >= 0 && w < 2 ** 22 && Math.fround(w) === w, `${w} is an integer a float holds`)
    const l = Math.hypot(x, y, z)
    const [a, b, c] = decodeNormal(w)
    worst = Math.max(worst, (Math.acos(Math.min(1, (a * x + b * y + c * z) / l)) * 180) / Math.PI)
  }
  for (const n of [[0, 0, 1], [0, 0, -1], [1, 0, 0], [-1, 0, 0], [0, 1, 0], [0, -1, 0], [1, 1, 1], [-1, -1, -1], [3, -4, 0]]) check(n[0], n[1], n[2])
  // A fixed spread over the sphere (a Fibonacci lattice), lengths other than one among them.
  for (let i = 0; i < 20000; i++) {
    const z = 1 - (2 * (i + 0.5)) / 20000
    const r = Math.sqrt(1 - z * z)
    const phi = i * Math.PI * (3 - Math.sqrt(5))
    check(r * Math.cos(phi) * (1 + (i % 3)), r * Math.sin(phi) * (1 + (i % 3)), z * (1 + (i % 3)))
  }
  assert.ok(worst < 0.12, `worst ${worst.toFixed(3)}°`)
  assert.equal(encodeNormal(0, 0, 0), NONE, 'no direction')
  assert.deepEqual(Array.from(encodeNormals(null, 3)), [NONE, NONE, NONE], 'a geometry without normals')
  assert.deepEqual(Array.from(encodeNormals([0, 0, 1, 0, 0, -1], 2)), [encodeNormal(0, 0, 1), encodeNormal(0, 0, -1)])
})

import { AmbientLight, BoxGeometry, BufferGeometry, DirectionalLight, DoubleSide, Float32BufferAttribute, FrontSide, Group, Mesh, MeshBasicMaterial, PointLight, Scene, type Material } from 'three'
import { collectLights, collectMeshes, type SlotOf } from '../src/creative-scene/render/trace/collect.ts'
import type { SlotMaterial } from '../src/creative-scene/materials/resolve.ts'

/** A scene of objects as SceneObjects mounts them, and the slot materials its meshes draw with. */
function sceneOf() {
  const slots = new Map<Material, SlotMaterial>()
  const slotted = (m: SlotMaterial, side = FrontSide) => {
    const material = new MeshBasicMaterial({ side })
    slots.set(material, m)
    return material
  }
  const slotOf: SlotOf = (m) => slots.get(m)
  const paint: SlotMaterial = { kind: 'pattern', id: 'paint@1', params: {} }
  const object = (id: string, ...children: Mesh[]) => {
    const g = new Group()
    g.userData = { objectID: id }
    g.add(...children)
    return g
  }
  const box = new BoxGeometry()
  box.userData.traceKey = 'builtin:box@1:{}'
  return { slots, slotted, slotOf, paint, object, box }
}

test('the layer traces what the opaque prepass draws, as scene objects hold it (design「收集与比对」)', () => {
  const { slotted, slotOf, paint, object, box } = sceneOf()
  const scene = new Scene()
  const glass = slotted({ kind: 'principled', params: { opacity: 1, transmission: 1 } })
  const veiled = slotted({ kind: 'principled', params: { opacity: 0.5, transmission: 0 } })
  const hidden = new Mesh(box, slotted(paint))
  const hiddenGroup = object('sobj_h', new Mesh(box, slotted(paint)))
  hiddenGroup.visible = false
  hidden.visible = false
  const aside = new Mesh(box, slotted(paint))
  aside.layers.set(30)
  // A range view of a merged geometry (render/pieces.ts): only see-through slots and ghosts draw them.
  const view = new BufferGeometry()
  view.setAttribute('position', box.attributes.position)
  view.setIndex(box.index)
  view.setDrawRange(0, 6)
  const plain = new Mesh(new BufferGeometry().setAttribute('position', new Float32BufferAttribute([0, 0, 0, 1, 0, 0, 0, 1, 0], 3)), slotted({ kind: 'unknown' }, DoubleSide))
  scene.add(
    object('sobj_b', new Mesh(box, slotted(paint)), new Mesh(box, glass), new Mesh(box, veiled), hidden, aside, new Mesh(view, slotted(paint))),
    object('sobj_a', plain, new Mesh(box, new MeshBasicMaterial())),
    hiddenGroup,
    new Mesh(box, slotted(paint)), // a placeholder: no scene object
  )
  scene.updateMatrixWorld()
  const traced = collectMeshes(scene, slotOf)
  assert.deepEqual(traced.map((t) => [t.objectID, t.part, t.key, t.cacheable, t.slots.map((s) => s.doubleSided), t.triangles]), [
    ['sobj_a', 0, `uuid:${plain.geometry.uuid}`, false, [true], 1],
    ['sobj_b', 0, 'builtin:box@1:{}', true, [false], 12],
  ])
})

test('a mesh of a material a group has a slot a root, each its group’s; none is traced if one of them is see-through (AC-S1-9)', () => {
  const { slotted, slotOf, paint, object } = sceneOf()
  // BoxGeometry has a group a face, material indices 0 to 5.
  const faces = new BoxGeometry()
  const wood: SlotMaterial = { kind: 'pattern', id: 'wood@1', params: {} }
  const a = slotted(paint)
  const b = slotted(wood, DoubleSide)
  const scene = new Scene()
  const twoTone = new Mesh(faces, [a, b, a, b, a, b])
  const glassy = new Mesh(faces, [a, a, a, a, a, slotted({ kind: 'principled', params: { transmission: 1 } })])
  scene.add(object('sobj_a', twoTone, glassy))
  scene.updateMatrixWorld()
  const [t, ...rest] = collectMeshes(scene, slotOf)
  assert.equal(rest.length, 0, 'not the mesh with a glass face')
  assert.equal(t.mesh, twoTone)
  assert.deepEqual(t.slots.map((s) => [s.material, s.doubleSided]), [[paint, false], [wood, true], [paint, false], [wood, true], [paint, false], [wood, true]])
  // Built a root a group, another build than one material's one root (design A13).
  assert.deepEqual([t.grouped, t.key], [true, `uuid:${faces.uuid}+groups`])
  scene.add(object('sobj_b', new Mesh(faces, a)))
  const one = collectMeshes(scene, slotOf)[1]
  assert.deepEqual([one.grouped, one.key, one.slots.length], [false, `uuid:${faces.uuid}`, 1], 'one material: the whole geometry, its groups aside')
})

test('the lights a hit point sees: visible, giving light, not the volumes’ stand-in; ambient added up', () => {
  const scene = new Scene()
  const sun = new DirectionalLight(0xffffff, 3)
  const off = new PointLight(0xffffff, 0)
  const hidden = new PointLight(0xffffff, 5)
  hidden.visible = false
  const standIn = new AmbientLight(0xffffff, 0.001)
  standIn.userData.volumesCast = true
  scene.add(sun, off, hidden, standIn, new AmbientLight(0xff0000, 0.5), new AmbientLight(0x0000ff, 0.25))
  const { lights, ambient } = collectLights(scene)
  assert.deepEqual(lights, [sun])
  assert.deepEqual([ambient.r, ambient.g, ambient.b], [0.5, 0, 0.25])
})

import { PlaneGeometry } from 'three'
import { MeshoptSimplifier } from 'meshoptimizer'
import { buildSegment, type GeometryData, type Segment } from '../src/creative-scene/render/trace/build.worker.ts'
import { BUILT_WITH, cacheKey, evictions } from '../src/creative-scene/render/trace/cache.ts'
import { readFileSync } from 'node:fs'

/** A geometry's arrays as the pool hands them over. */
function dataOf(g: BufferGeometry): GeometryData {
  return {
    positions: Float32Array.from(g.attributes.position.array),
    normals: g.attributes.normal ? Float32Array.from(g.attributes.normal.array) : null,
    colors: g.attributes.color ? Float32Array.from(g.attributes.color.array) : null,
    colorSize: g.attributes.color?.itemSize ?? 0,
    index: g.index ? Uint32Array.from(g.index.array) : null,
    groups: g.groups.map(({ start, count }) => ({ start, count })),
  }
}

/** Each root's leaves as [first triangle, count], from the packed nodes. */
function leavesOf(s: Segment): [number, number][][] {
  const u16 = new Uint16Array(s.nodes.buffer, s.nodes.byteOffset, s.nodes.length * 2)
  let base = 0
  return s.roots.map((n) => {
    const leaves: [number, number][] = []
    for (let i = base; i < base + n; i++) if (u16[i * 16 + 15] === 0xffff) leaves.push([s.nodes[i * 8 + 6], u16[i * 16 + 14]])
    base += n
    return leaves
  })
}

test('a segment is the geometry’s tree packed from 0: every triangle in one leaf, every index a vertex of its own (design「构建、缓存与提交」)', () => {
  const plane = new PlaneGeometry(1, 1, 30, 30)
  const s = buildSegment({ build: { kind: 'original' }, geometry: dataOf(plane), epsilon: 0 })
  assert.equal(s.triangles, 1800)
  assert.equal(s.vertices.length, 31 * 31 * 4)
  assert.ok(s.index.every((v) => v < 31 * 31), 'indices count from the segment’s first vertex')
  const covered = new Uint8Array(s.triangles)
  for (const [first, count] of leavesOf(s)[0]) for (let t = first; t < first + count; t++) covered[t]++
  assert.ok(covered.every((c) => c === 1), 'each triangle in exactly one leaf')
  assert.ok(s.depth >= 2 && s.depth <= 40)
  assert.equal(s.colors, null)
  // w: the normal (0, 0, 1) of the plane, encoded.
  assert.ok(Array.from({ length: 31 * 31 }, (_, v) => s.vertices[4 * v + 3]).every((w) => w === encodeNormal(0, 0, 1)))
  // A geometry without an index draws its vertices in order.
  const flat = buildSegment({ build: { kind: 'original' }, geometry: { ...dataOf(plane.toNonIndexed()) }, epsilon: 0 })
  assert.equal(flat.triangles, 1800)
})

test('a geometry of several slots is a root a slot; a proxy is simplified slot by slot within its bound (D3, I8)', async () => {
  await MeshoptSimplifier.ready
  // A finely divided plane, its two halves two slots: flat, so nearly everything simplifies away, except the locked borders.
  const plane = new PlaneGeometry(1, 1, 40, 40)
  const half = plane.index!.count / 2
  plane.addGroup(0, half, 0)
  plane.addGroup(half, half, 1)
  const original = buildSegment({ build: { kind: 'original' }, geometry: dataOf(plane), epsilon: 0 })
  assert.equal(original.roots.length, 2)
  const [a, b] = leavesOf(original)
  const span = (leaves: [number, number][]) => [Math.min(...leaves.map((l) => l[0])), Math.max(...leaves.map((l) => l[0] + l[1]))]
  assert.deepEqual([span(a), span(b)], [[0, 1600], [1600, 3200]], 'each root keeps to its own slot’s triangles')
  const proxy = buildSegment({ build: { kind: 'proxy', level: 0 }, geometry: dataOf(plane), epsilon: 0.003 })
  assert.equal(proxy.roots.length, 2)
  assert.ok(proxy.triangles < original.triangles / 4, `${proxy.triangles} of ${original.triangles}`)
  assert.ok(proxy.vertices.length < original.vertices.length, 'only the vertices kept')
  // A bump taller than the bound stays.
  const bumpy = new PlaneGeometry(1, 1, 40, 40)
  const p = bumpy.attributes.position
  for (let i = 0; i < p.count; i++) p.setZ(i, 0.05 * Math.sin(p.getX(i) * 20) * Math.sin(p.getY(i) * 20))
  const kept = buildSegment({ build: { kind: 'proxy', level: 0 }, geometry: dataOf(bumpy), epsilon: 0.003 })
  assert.ok(kept.triangles > proxy.triangles * 4, 'what will not simplify within 3 mm stays')
  // No crack between the slots: each half keeps every vertex of the seam (y = 0), at the same place.
  const seam = (s: Segment, root: number) => {
    const at = new Set<string>()
    for (const [first, count] of leavesOf(s)[root])
      for (let t = first; t < first + count; t++)
        for (let k = 0; k < 3; k++) {
          const v = s.index[3 * t + k]
          const [x, y] = [s.vertices[4 * v], s.vertices[4 * v + 1]]
          if (Math.abs(y) < 1e-6) at.add(x.toFixed(6))
        }
    return [...at].sort()
  }
  assert.equal(seam(proxy, 0).length, 41, 'the seam’s 41 vertices kept on one side')
  assert.deepEqual(seam(proxy, 1), seam(proxy, 0), 'and on the other')
})

test('vertex colours go along as sRGB bytes, only for a geometry that has them (D5)', () => {
  const tri = new BufferGeometry()
  tri.setAttribute('position', new Float32BufferAttribute([0, 0, 0, 1, 0, 0, 0, 1, 0], 3))
  tri.setAttribute('color', new Float32BufferAttribute([1, 0, 0, 0, 0.25, 0, 0, 0, 1], 3))
  const s = buildSegment({ build: { kind: 'original' }, geometry: dataOf(tri), epsilon: 0 })
  assert.deepEqual(Array.from(s.colors!).map((c) => [c & 255, (c >> 8) & 255, (c >> 16) & 255, c >>> 24]), [[255, 0, 0, 255], [0, 137, 0, 255], [0, 0, 255, 255]])
  assert.ok(Array.from({ length: 3 }, (_, v) => s.vertices[4 * v + 3]).every((w) => w === NONE), 'no normals: the face’s stands in')
})

test('cache keys and the least-recently-used ledger (design「构建、缓存与提交」)', () => {
  const libs = 'three@0.186.1,three-mesh-bvh@0.9.15,meshoptimizer@1.1.1'
  assert.equal(cacheKey({ kind: 'original' }, 'model:rev_1#0'), `raw:model:rev_1#0:v2:sah-10-40:${libs}`)
  assert.equal(cacheKey({ kind: 'proxy', level: 2 }, 'model:rev_1#0'), `proxy@2:model:rev_1#0:v2:sah-10-40:${libs}`)
  const entries = [{ key: 'old', bytes: 40, used: 1 }, { key: 'new', bytes: 30, used: 3 }, { key: 'mid', bytes: 20, used: 2 }]
  assert.deepEqual(evictions(entries, 10, 100, 1000), [], 'room enough')
  assert.deepEqual(evictions(entries, 20, 100, 1000), ['old'], 'over the ceiling: the least recently used first')
  assert.deepEqual(evictions(entries, 60, 100, 1000), ['old', 'mid'])
  assert.deepEqual(evictions(entries, 10, 100, 5), ['old'], 'short of quota')
  assert.equal(evictions(entries, 200, 100, 1000), null, 'larger than the ceiling: not kept at all')
  assert.equal(evictions([], 10, 100, 5), null, 'no quota and nothing to drop')
})

import { Matrix4, Quaternion, Ray, Raycaster, SphereGeometry, Vector3 } from 'three'
import { ArraysDropped, MATERIAL_WORD, Space, TraceLayer } from '../src/creative-scene/render/trace/layer.ts'

/** The layer's buffers as the traversal reads them (three-mesh-bvh 0.9.15 bvh_RaycastFirstHit), on the CPU: the nearest hit, its instance's material row. */
function traceCPU(layer: TraceLayer, origin: Vector3, direction: Vector3): { dist: number; row: number } | null {
  const { nodes: n32, index, vertices, transforms: t32 } = layer.arrays
  const f32 = new Float32Array(n32.buffer)
  const n16 = new Uint16Array(n32.buffer)
  const vf = new Float32Array(vertices.buffer)
  const tf = new Float32Array(t32.buffer)
  let best: { dist: number; row: number } | null = null
  const hitsBox = (i: number, o: Vector3, d: Vector3) => {
    let t0 = 0
    let t1 = Infinity
    for (let a = 0; a < 3; a++) {
      const inv = 1 / d.getComponent(a)
      let tn = (f32[i * 8 + a] - o.getComponent(a)) * inv
      let tf2 = (f32[i * 8 + 3 + a] - o.getComponent(a)) * inv
      if (tn > tf2) [tn, tf2] = [tf2, tn]
      t0 = Math.max(t0, tn)
      t1 = Math.min(t1, tf2)
    }
    return t0 <= t1
  }
  const tri = new Vector3()
  const [a, b, c] = [new Vector3(), new Vector3(), new Vector3()]
  const blas = (root: number, slot: number) => {
    const inv = new Matrix4().fromArray(tf, slot * 36 + 16)
    const o = origin.clone().applyMatrix4(inv)
    const d = direction.clone().transformDirection(inv)
    const ray = new Ray(o, d)
    const world = new Matrix4().fromArray(tf, slot * 36)
    const stack = [root]
    while (stack.length) {
      const i = stack.pop()!
      if (!hitsBox(i, o, d)) continue
      if (n16[i * 16 + 15] === 0xffff) {
        for (let t = n32[i * 8 + 6], end = t + n16[i * 16 + 14]; t < end; t++) {
          const v = (k: number, out: Vector3) => out.set(vf[index[3 * t + k] * 4], vf[index[3 * t + k] * 4 + 1], vf[index[3 * t + k] * 4 + 2])
          if (!ray.intersectTriangle(v(0, a), v(1, b), v(2, c), false, tri)) continue
          const dist = tri.applyMatrix4(world).distanceTo(origin)
          if (!best || dist < best.dist) best = { dist, row: t32[slot * 36 + MATERIAL_WORD] }
        }
      } else stack.push(i + 1, i + n32[i * 8 + 6])
    }
  }
  const stack = [0]
  while (stack.length) {
    const i = stack.pop()!
    if (!hitsBox(i, origin, direction)) continue
    if (n32[i * 8 + 7] >>> 24 === 0xff) blas(n32[i * 8 + 6], n32[i * 8 + 7] & 0xffffff)
    else stack.push(i + 1, i + n32[i * 8 + 6])
  }
  return best
}

test('a cache key names the libraries a segment came out of, as installed: an upgrade is another key (design A13)', () => {
  const installed = (name: string) => (JSON.parse(readFileSync(new URL(`../node_modules/${name}/package.json`, import.meta.url), 'utf8')) as { version: string }).version
  for (const [name, version] of Object.entries(BUILT_WITH)) assert.equal(version, installed(name), `${name} was upgraded: set cache.ts BUILT_WITH to ${installed(name)}`)
})

test('free ranges are taken first fit and given back merged, the tail shrinking with them', () => {
  const s = new Space(100)
  assert.deepEqual([s.take(10), s.take(20), s.take(30)], [0, 10, 30])
  s.give(10, 20)
  assert.equal(s.take(15), 10, 'first fit')
  assert.equal(s.wasted, 5)
  s.give(10, 15)
  s.give(30, 30)
  assert.deepEqual([s.tail, s.free], [10, []], 'merged, and back to the tail')
  assert.equal(s.take(91), null, 'no room')
})

test('the layer traces what three’s raycaster hits: segments rebased, the top level a leaf per instance, rows per instance (design「层与同步」)', () => {
  const layer = new TraceLayer(2 ** 31, 'test')
  const box = buildSegment({ build: { kind: 'original' }, geometry: dataOf(new BoxGeometry(1, 1, 1)), epsilon: 0 })
  const ball = buildSegment({ build: { kind: 'original' }, geometry: dataOf(new SphereGeometry(0.6, 24, 16)), epsilon: 0 })
  layer.commit('box', box, 3)
  layer.commit('ball', ball, 3)
  const at = (x: number, y: number, z: number, s = 1, turn = 0) => new Matrix4().compose(new Vector3(x, y, z), new Quaternion().setFromAxisAngle(new Vector3(0, 1, 0), turn), new Vector3(s, s, s))
  const instances = [
    { matrixWorld: at(0, 0, 0), segment: 'box', rows: [7] },
    { matrixWorld: at(3, 0, 0, 2, 0.5), segment: 'box', rows: [8] },
    { matrixWorld: at(-3, 0, 0), segment: 'ball', rows: [9] },
  ]
  layer.setInstances(instances)
  // The same scene for three's raycaster.
  const scene = new Scene()
  const meshes = [new Mesh(new BoxGeometry(1, 1, 1)), new Mesh(new BoxGeometry(1, 1, 1)), new Mesh(new SphereGeometry(0.6, 24, 16))]
  meshes.forEach((m, i) => {
    m.applyMatrix4(instances[i].matrixWorld)
    scene.add(m)
  })
  scene.updateMatrixWorld()
  const check = (what: string) => {
    let rays = 0
    for (let x = -5; x <= 5; x += 0.19)
      for (let y = -2; y <= 2; y += 0.41) {
        const origin = new Vector3(x, y, 6)
        const direction = new Vector3(0.05, -0.03, -1).normalize()
        const want = new Raycaster(origin, direction).intersectObjects(scene.children)[0]
        const got = traceCPU(layer, origin, direction)
        assert.equal(got === null, want === undefined, `${what}: (${x.toFixed(2)}, ${y.toFixed(2)}) hit or miss alike`)
        if (got && want) {
          assert.ok(Math.abs(got.dist - want.distance) < 1e-4, `${what}: distance ${got.dist} against ${want.distance}`)
          assert.equal(got.row, instances[meshes.indexOf(want.object as Mesh)].rows[0], `${what}: the instance's row`)
          rays++
        }
      }
    assert.ok(rays > 30, `${what}: ${rays} hits`)
  }
  check('as committed')
  // Moved: refit.
  instances[0].matrixWorld = at(0, 1, -1, 1, 1)
  meshes[0].matrix.identity().decompose(meshes[0].position, meshes[0].quaternion, meshes[0].scale)
  meshes[0].applyMatrix4(instances[0].matrixWorld)
  scene.updateMatrixWorld()
  layer.move([{ at: 0, matrixWorld: instances[0].matrixWorld }])
  check('moved')
  // A segment released, another committed into its room and past it: the buffers grow and keep what they hold.
  const nodesBefore = (layer as unknown as { stores: { nodes: unknown } }).stores.nodes
  layer.release('ball')
  const fine = buildSegment({ build: { kind: 'original' }, geometry: dataOf(new SphereGeometry(0.6, 96, 64)), epsilon: 0 })
  layer.commit('fine', fine, 3)
  instances[2].segment = 'fine'
  meshes[2].geometry = new SphereGeometry(0.6, 96, 64)
  layer.setInstances(instances)
  assert.equal((layer as unknown as { stores: { nodes: { value: unknown } } }).stores.nodes, nodesBefore, 'the storage node kept')
  assert.equal((layer as unknown as { stores: { nodes: { value: { array: unknown } } } }).stores.nodes.value.array, layer.arrays.nodes, 'its value the grown buffer')
  check('grown')
  // A free range below a segment in use: compacted, the segment moves down, rebased again.
  layer.commit('pad', box, 3)
  layer.commit('last', ball, 3)
  layer.release('pad')
  instances[2].segment = 'last'
  meshes[2].geometry = new SphereGeometry(0.6, 24, 16)
  layer.setInstances(instances)
  check('over a free range')
  assert.ok(layer.fragmentation > 0, 'a free range below the tail')
  layer.compact()
  assert.equal(layer.fragmentation, 0, 'none once compacted')
  assert.ok(layer.needsTop, 'the top level is laid again')
  layer.setInstances(instances)
  check('compacted')
  layer.setInstances([])
  assert.equal(traceCPU(layer, new Vector3(0, 0, 6), new Vector3(0, 0, -1)), null, 'no instance, no hit')
})

test('each root of an instance finds its own slot’s row (AC-S1-9)', () => {
  const layer = new TraceLayer(2 ** 31, 'test')
  // A face a group: px, nx, py, ny, pz, nz.
  const faces = buildSegment({ build: { kind: 'original' }, geometry: dataOf(new BoxGeometry(1, 1, 1)), epsilon: 0 })
  assert.equal(faces.roots.length, 6)
  assert.deepEqual(faces.rootGroups, [0, 1, 2, 3, 4, 5])
  layer.commit('faces', faces, 6)
  layer.setInstances([{ matrixWorld: new Matrix4(), segment: 'faces', rows: [10, 11, 12, 13, 14, 15] }])
  const along = (o: [number, number, number], d: [number, number, number]) => traceCPU(layer, new Vector3(...o), new Vector3(...d))?.row
  assert.equal(along([3, 0.1, 0.2], [-1, 0, 0]), 10, '+x')
  assert.equal(along([-3, 0.1, 0.2], [1, 0, 0]), 11, '−x')
  assert.equal(along([0.1, 3, 0.2], [0, -1, 0]), 12, '+y')
  assert.equal(along([0.1, 0.2, 3], [0, 0, -1]), 14, '+z')
  assert.equal(along([0.1, 0.2, -3], [0, 0, 1]), 15, '−z')
  // The library roots groups by where they lie, an empty one dropped: out of order, a root still finds its own group.
  const box = dataOf(new BoxGeometry(1, 1, 1))
  const shuffled = buildSegment({ build: { kind: 'original' }, geometry: { ...box, groups: [{ start: 18, count: 18 }, { start: 0, count: 0 }, { start: 0, count: 18 }] }, epsilon: 0 })
  assert.deepEqual(shuffled.rootGroups, [0, 2], 'the second half, then the first; the empty group has no root')
  layer.release('faces')
  layer.commit('shuffled', shuffled, 2)
  layer.setInstances([{ matrixWorld: new Matrix4(), segment: 'shuffled', rows: shuffled.rootGroups.map((g) => 20 + g) }])
  // BoxGeometry's index: px, nx, py in its first half, ny, pz, nz in its second.
  assert.equal(along([0.1, 0.2, 3], [0, 0, -1]), 20, '+z: the second half, group 0')
  assert.equal(along([3, 0.1, 0.2], [-1, 0, 0]), 22, '+x: the first half, group 2')
  const overlapping = buildSegment({ build: { kind: 'original' }, geometry: { ...box, groups: [{ start: 0, count: Infinity }, { start: 0, count: 18 }] }, epsilon: 0 })
  assert.deepEqual([overlapping.rootGroups, overlapping.triangles], [[0, 1], 18], 'overlapping groups each keep their triangles, as the raster draws both; a count of Infinity runs to the end')
  assert.equal(buildSegment({ build: { kind: 'original' }, geometry: { ...box, groups: [] }, epsilon: 0 }).roots.length, 1, 'one material: one root, its groups aside')
})

test('an export’s layer drops its arrays once drawn: the top level and transforms stay to write, a segment or a new layout is refused (design A5)', () => {
  const layer = new TraceLayer(2 ** 31, 'test')
  const box = buildSegment({ build: { kind: 'original' }, geometry: dataOf(new BoxGeometry(1, 1, 1)), epsilon: 0 })
  layer.commit('box', box, 2)
  const instances = [
    { matrixWorld: new Matrix4(), segment: 'box', rows: [1] },
    { matrixWorld: new Matrix4().makeTranslation(3, 0, 0), segment: 'box', rows: [2] },
  ]
  layer.setInstances(instances)
  const bytes = layer.bytes
  const nodes = layer.arrays.nodes.slice()
  layer.take()
  layer.dropArrays()
  assert.equal(layer.bytes, bytes, 'the bytes as allocated')
  // Two elements each: a pipeline built later still reads the vertices as an array of structs (three: more than one).
  assert.deepEqual([layer.arrays.index.length, layer.arrays.vertices.length, layer.arrays.colors.length], [2, 8, 2], 'the geometry let go')
  const kept = layer.arrays.nodes
  assert.ok(kept.length >= 8 && kept.length <= nodes.length - box.nodes.length, `the nodes up to the trees: ${kept.length} of ${nodes.length}`)
  assert.deepEqual(kept, nodes.subarray(0, kept.length), 'the top level as it was')
  const head = layer.arrays.nodes.slice()
  layer.move([{ at: 1, matrixWorld: new Matrix4().makeTranslation(5, 0, 0) }])
  assert.notDeepEqual(layer.arrays.nodes, head, 'a move refits the top level')
  assert.ok(layer.take() > 0, 'and uploads it')
  layer.setInstances([{ ...instances[0], rows: [4] }])
  assert.equal(layer.arrays.transforms[MATERIAL_WORD], 4, 'a row written')
  assert.throws(() => layer.commit('ball', buildSegment({ build: { kind: 'original' }, geometry: dataOf(new SphereGeometry(1, 8, 6)), epsilon: 0 })), ArraysDropped)
  const many = Array.from({ length: 40 }, (_, i) => ({ matrixWorld: new Matrix4().makeTranslation(i, 0, 0), segment: 'box', rows: [i] }))
  assert.throws(() => layer.setInstances(many), ArraysDropped, 'more instances than the top level has room for')
})

import { primitiveGeometry } from '../src/creative-scene/builtins/geometry.ts'
import { makeMaterial, slotOf } from '../src/creative-scene/materials/instances.ts'

test('a builtin is cached under its id and its parameters filled in and sorted; a material instance knows its slot material', () => {
  const a = primitiveGeometry('box@1', { width: 2 })!
  assert.match(a.userData.traceKey, /^builtin:box@1:\{.*"width":2.*\}$/)
  assert.equal(a.userData.traceKey, primitiveGeometry('box@1', { width: 2, height: 1 })!.userData.traceKey, 'a default given or left out is the same geometry')
  assert.notEqual(a.userData.traceKey, primitiveGeometry('box@1', { width: 3 })!.userData.traceKey)
  const paint: SlotMaterial = { kind: 'pattern', id: 'paint@1', params: { color: '#808080', roughness: 0.5, metalness: 0 } }
  const m = makeMaterial(paint, false)
  assert.equal(slotOf(m), paint)
  assert.equal(slotOf(new MeshBasicMaterial()), undefined, 'clay, ghosts and helpers have none')
})

import { Color, SpotLight } from 'three'
import { HEADER, LIGHT, MATERIAL_ROW, PRINCIPLED, UNKNOWN, presets, TraceTables } from '../src/creative-scene/render/trace/tables.ts'

test('one table: a header, a material row a traced mesh, a light row a light (design「内部类型」, D6)', () => {
  const tables = new TraceTables()
  const f = () => (tables.node as unknown as { value: { array: Float32Array } }).value.array
  const vec4 = (i: number) => [...f().subarray(i * 4, i * 4 + 4)]
  const near = (got: number[], want: number[], what: string) => assert.ok(got.every((v, i) => Math.abs(v - want[i]) < 1e-5), `${what}: ${got} against ${want}`)
  const row = (r: number, v: number) => vec4(HEADER + r * MATERIAL_ROW + v)
  tables.setMaterial(0, { kind: 'principled', params: { color: '#ff0000', roughness: 0.3, metalness: 1, emissive: '#000000', emissive_intensity: 0 }, vertexColors: true }, true)
  tables.setMaterial(1, { kind: 'pattern', id: 'plaster@1', params: { color: '#d8d0c2', mottle: 0.4, roughness: 0.9, metalness: 0 } }, false)
  tables.setMaterial(2, { kind: 'unknown' }, false)
  assert.deepEqual(row(0, 0), [PRINCIPLED, 1, 1, 0], 'principled, double-sided, vertex colours')
  near(row(0, 1), [1, 0, 0, 0], 'its colour, linear')
  near(row(0, 2), [0.3, 0, 0, 0], 'its roughness')
  assert.deepEqual(row(1, 0), [2 + presets.indexOf('plaster@1'), 0, 0, 0], 'a preset by its place')
  near(row(1, 2), [0.4, 0, 0, 0], 'its parameters in the catalog’s order')
  assert.deepEqual(row(2, 0), [UNKNOWN, 0, 0, 0])
  // A preset the hit has no function for: as the raster draws it, principled with its colour, roughness and metalness.
  tables.setMaterial(3, { kind: 'pattern', id: 'nosuch@1', params: { color: '#00ff00', roughness: 0.25 } }, false)
  assert.deepEqual(row(3, 0), [PRINCIPLED, 0, 0, 0])
  near(row(3, 1), [0, 1, 0, 0], 'its colour')
  near([row(3, 2)[0], row(3, 3)[0]], [0.25, 0], 'its roughness, metalness 0')
  // Past its room: a larger buffer behind the same node, keeping the rows.
  tables.setMaterial(40, { kind: 'unknown' }, false)
  assert.deepEqual(row(0, 0), [PRINCIPLED, 1, 1, 0], 'kept when grown')
  const scene = new Scene()
  const sun = Object.assign(new DirectionalLight('#ffffff', 2), { castShadow: true })
  sun.position.set(0, 10, 0)
  const point = new PointLight('#ff0000', 3, 5, 2)
  point.position.set(1, 2, 3)
  const spot = new SpotLight('#ffffff', 4, 10, Math.PI / 4, 0.5, 2)
  spot.position.set(0, 5, 0)
  scene.add(sun, sun.target, point, spot, spot.target)
  scene.updateMatrixWorld()
  tables.setLights({ lights: [sun, point, spot], ambient: new Color(0.1, 0.2, 0.3) }, scene)
  const [lights, first, materials] = vec4(0)
  assert.equal(lights, 3)
  assert.equal(first, HEADER + materials * MATERIAL_ROW, 'the lights after the materials’ room')
  near(vec4(1), [0.1, 0.2, 0.3, 0], 'ambient')
  near(vec4(first), [LIGHT.directional, 1, 0, 0], 'a sun that casts')
  near(vec4(first + 1), [0, 1, 0, 0], 'towards it')
  near(vec4(first + 2), [2, 2, 2, 0], 'colour × intensity')
  near(vec4(first + 4), [LIGHT.point, 0, 5, 2], 'a point light: its distance and decay')
  near(vec4(first + 5), [1, 2, 3, 0], 'where it is')
  near(vec4(first + 8), [LIGHT.spot, 0, 10, 2], 'a spot')
  near(vec4(first + 9), [0, 5, 0, Math.cos(Math.PI / 4)], 'where it is, its cone')
  assert.ok(Math.abs(vec4(first + 10)[3] - Math.cos((Math.PI / 4) * 0.5)) < 1e-6, 'where its penumbra starts')
  near(vec4(first + 11), [0, -1, 0, 0], 'the way it shines')
})

import { TraceBase, type TraceStatus } from '../src/creative-scene/render/trace/base.ts'
import { GeometryFailed, geometryData, type Builds } from '../src/creative-scene/render/trace/pool.ts'
import type { SceneGPU } from '../src/creative-scene/render/gpu.ts'

// The base's orchestration (S1 design「底座的状态」, A13): an export's base, which packs once everything is back,
// on a device of its own and builds of its own, both here.

/** A device as the base sees it: how much a buffer binds, the buffers let go, its errors. */
function deviceOf(maxBinding = 2 ** 31) {
  const released = new Set<object>()
  const listeners = new Set<(message: string) => void>()
  const gpu = {
    trace: { supported: true, maxBinding },
    release: (a: object) => released.add(a),
    onError: (l: (message: string) => void) => (listeners.add(l), () => listeners.delete(l)),
  }
  return {
    gpu: gpu as unknown as SceneGPU,
    released,
    error: (message: string) => listeners.forEach((l) => l(message)),
  }
}

/** Builds as the pool's, done here; a key in fails is refused with its error. */
function buildsOf(fails = new Map<string, Error>()): Builds {
  return {
    async build(source, build, epsilon = 0) {
      const e = fails.get(source.key)
      if (e) throw e
      return { segment: buildSegment({ build, epsilon, geometry: geometryData(source.geometry, source.grouped) }), cached: false, ms: 0 }
    },
  }
}

/** Syncs, the builds coming back between them, till the status is as wanted. */
async function until(base: TraceBase, done: (s: TraceStatus) => boolean, what: string) {
  for (let i = 0; i < 400; i++) {
    base.sync()
    if (done(base.state)) return
    await new Promise((resolve) => setTimeout(resolve, 1))
  }
  assert.fail(`${what}: still ${JSON.stringify(base.state)}`)
}

const ready = (tier: 'full' | 'proxy') => (s: TraceStatus) => s.state === 'ready' && s.tier === tier && s.pending === 0

function sceneWith() {
  const scene = new Scene()
  const red: SlotMaterial = { kind: 'pattern', id: 'paint@1', params: { color: '#ff0000', roughness: 0.5, metalness: 0 } }
  const blue: SlotMaterial = { kind: 'pattern', id: 'paint@1', params: { color: '#0000ff', roughness: 0.5, metalness: 0 } }
  const materials = { red: makeMaterial(red, false), blue: makeMaterial(blue, false) }
  const add = (id: string, mesh: Mesh) => {
    const g = new Group()
    g.userData = { objectID: id }
    g.add(mesh)
    scene.add(g)
    scene.updateMatrixWorld()
    return mesh
  }
  const remove = (mesh: Mesh) => {
    scene.remove(mesh.parent!)
    scene.updateMatrixWorld()
  }
  const keyed = (key: string, g: BufferGeometry = new BoxGeometry()) => ((g.userData.traceKey = key), g)
  return { scene, red, blue, materials, add, remove, keyed }
}

test('an export’s layer is laid out once for all it holds, the top level’s room with it; one material draws a geometry as one root (design A9, A13)', async () => {
  const { gpu } = deviceOf()
  const { scene, red, blue, materials, add, keyed } = sceneWith()
  const faces = keyed('builtin:box@1:{"faces":1}')
  const twoTone = [materials.red, materials.blue, materials.red, materials.blue, materials.red, materials.blue]
  for (let i = 0; i < 20; i++) add(`sobj_${String(i).padStart(2, '0')}`, new Mesh(faces, twoTone)).position.set(i * 2, 0, 0)
  add('sobj_z', new Mesh(faces, materials.red)).position.set(-3, 0, 0)
  scene.updateMatrixWorld()
  const base = new TraceBase(gpu, scene, { incremental: false, builds: buildsOf() })
  assert.ok(await base.ready(5000))
  assert.deepEqual(base.state, { state: 'ready', tier: 'full', pending: 0 })
  const layer = base.handle()!.layer
  assert.equal(layer.layouts, 1, 'one layout, the top level’s 121 leaves made room for with the rest')
  assert.equal(layer.placed.get('builtin:box@1:{"faces":1}+groups')!.rootBases.length, 6, 'a root a group, drawn a material a group')
  assert.equal(layer.placed.get('builtin:box@1:{"faces":1}')!.rootBases.length, 1, 'one material: one root')
  const slotAt = (o: [number, number, number], d: [number, number, number]) => base.slots[traceCPU(layer, new Vector3(...o), new Vector3(...d))!.row].material
  assert.equal(slotAt([0.1, 0.2, 3], [0, 0, -1]), red, '+z, the group of material index 4')
  assert.equal(slotAt([0.1, 0.2, -3], [0, 0, 1]), blue, '−z, index 5')
  assert.equal(slotAt([-3.1, 0.2, 3], [0, 0, -1]), red, 'the one-material box')
})

test('the tier: an original the device cannot bind goes to the proxies, and is tried again once the scene is well below it (design「分档」, D9)', async () => {
  await MeshoptSimplifier.ready
  // A finely divided flat plane: its proxy is a sliver of it. Its original's index needs some 100 KB with room to spare.
  const { gpu } = deviceOf(40_000)
  const { scene, materials, add } = sceneWith()
  const mesh = add('sobj_a', new Mesh(new PlaneGeometry(1, 1, 60, 60), materials.red))
  const base = new TraceBase(gpu, scene, { incremental: false, builds: buildsOf() })
  await until(base, ready('proxy'), 'to the proxies')
  mesh.geometry = new PlaneGeometry(1, 1, 4, 4)
  await until(base, ready('full'), 'back to the original')
})

test('more instances than the device binds: the top level is judged, not thrown out of the frame (design A13)', async () => {
  const { gpu } = deviceOf(20_000)
  const { scene, materials, add, keyed } = sceneWith()
  const box = keyed('builtin:box@1:{}')
  for (let i = 0; i < 10; i++) add(`sobj_${String(i).padStart(3, '0')}`, new Mesh(box, materials.red))
  const base = new TraceBase(gpu, scene, { incremental: false, builds: buildsOf() })
  await until(base, ready('full'), 'ten boxes')
  // A hundred transform slots need some 27 KB with room to spare.
  for (let i = 10; i < 100; i++) add(`sobj_${String(i).padStart(3, '0')}`, new Mesh(box, materials.red))
  await until(base, (s) => s.state === 'tooLarge', 'a hundred: the proxies are no smaller')
})

test('a geometry whose build fails is left out and the rest traced; the layer fails as a whole on what touches all of it, retried once a scene change (design A13)', async () => {
  const { gpu, error } = deviceOf()
  const { scene, materials, add, remove, keyed } = sceneWith()
  const fails = new Map<string, Error>([['bad', new GeometryFailed('坏的几何')]])
  const warn = console.warn
  console.warn = () => {}
  try {
    add('sobj_a', new Mesh(keyed('a'), materials.red))
    add('sobj_b', new Mesh(keyed('bad'), materials.red))
    add('sobj_c', new Mesh(keyed('c'), materials.red))
    const base = new TraceBase(gpu, scene, { incremental: false, builds: buildsOf(fails) })
    await until(base, ready('full'), 'the rest')
    assert.deepEqual([...base.handle()!.layer.placed.keys()].sort(), ['a', 'c'])
    // A worker that did not load: the layer fails, its layers go; a scene change tries again.
    fails.set('lost', new Error('构建线程出错'))
    const lost = add('sobj_d', new Mesh(keyed('lost'), materials.red))
    await until(base, (s) => s.state === 'failed', 'a worker lost')
    assert.equal(base.handle(), null)
    fails.delete('lost')
    remove(lost)
    await until(base, ready('full'), 'retried')
    // Ready again: the next failure is retried too.
    fails.set('lost', new Error('构建线程出错'))
    const again = add('sobj_d', new Mesh(keyed('lost'), materials.red))
    await until(base, (s) => s.state === 'failed', 'lost again')
    fails.delete('lost')
    remove(again)
    await until(base, ready('full'), 'retried again')
    // The device refusing the layer's buffers (they are named for it).
    error('[Buffer "trace_original_nodes"] Out of memory while creating buffer')
    assert.equal(base.state.state, 'failed')
    error('something else')
    add('sobj_e', new Mesh(keyed('e'), materials.red))
    await until(base, ready('full'), 'retried after the device error')
    // The reflections' shader refused on the device (SceneRenderer, SceneGPU.pipelineFailed): for good.
    base.fail(true)
    add('sobj_f', new Mesh(keyed('f'), materials.red))
    for (let i = 0; i < 5; i++) base.sync()
    assert.equal(base.state.state, 'failed', 'not tried again')
  } finally {
    console.warn = warn
  }
})

test('buffers laid out again go at once, a dropped layer’s at the next sync (the frame that dropped it may draw it); an export’s layer that let its arrays go packs again (design A5, A13)', async () => {
  const { gpu, released } = deviceOf()
  const { scene, materials, add, keyed } = sceneWith()
  add('sobj_a', new Mesh(keyed('a'), materials.red))
  const base = new TraceBase(gpu, scene, { incremental: false, builds: buildsOf() })
  assert.ok(await base.ready(5000))
  const first = base.handle()!
  const nodesOf = (h: { layer: TraceLayer }) => (h.layer.stores.nodes as unknown as { value: object }).value
  const kept = nodesOf(first)
  assert.ok(released.size >= 4 && !released.has(kept), 'the first layout’s buffers let go, not the ones in use')
  base.drawn()
  assert.ok(first.layer.dropped)
  // A new geometry for a layer that let its arrays go: packed again in a new layer.
  add('sobj_b', new Mesh(keyed('b', new SphereGeometry(1, 8, 6)), materials.red))
  base.sync()
  assert.ok(!released.has(kept), 'not in the sync that dropped it')
  await until(base, ready('full'), 'packed again')
  assert.notEqual(base.handle()!.key, first.key)
  assert.ok(released.has(kept), 'at a later sync')
})
