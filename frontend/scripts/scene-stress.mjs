// Stress and equivalence scenes for the scene editor's e2e (scene-performance
// P1). Model GLBs are written here in Node with three's GLTFExporter and
// checked by the sandbox's inspect(), as a bake would be; a scene is
// { models, objects, environment? }: models by name ({ glb, manifest }), and
// objects as the scene document has them, a model instance naming its model
// by `model: { name }` until the e2e seeds it and puts in its revision.
// Everything comes from a fixed seed: the same arguments give the same bytes.
import {
  BoxGeometry,
  CylinderGeometry,
  Group,
  LatheGeometry,
  Mesh,
  MeshStandardMaterial,
  PlaneGeometry,
  Quaternion,
  Euler,
  SphereGeometry,
  Vector2,
} from 'three'
import { GLTFExporter } from 'three/addons/exporters/GLTFExporter.js'
import { inspect } from '../src/creative-scene/sandbox/inspect.ts'

// GLTFExporter reads its binary chunks through FileReader, which Node does not have.
globalThis.FileReader ??= class {
  readAsArrayBuffer(blob) {
    void blob.arrayBuffer().then((b) => {
      this.result = b
      this.onloadend?.()
      this.onload?.()
    })
  }
  readAsDataURL(blob) {
    void blob.arrayBuffer().then((b) => {
      this.result = `data:${blob.type || 'application/octet-stream'};base64,${Buffer.from(b).toString('base64')}`
      this.onloadend?.()
      this.onload?.()
    })
  }
}

/** The scene document's object ID for a short name (the e2e's gid): names end in x so p1 and p10 differ. */
export const objectID = (name) => `sobj_${name.padEnd(21, '0')}`

/** mulberry32: the same seed, the same numbers. */
function random(seed) {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const slotMaterials = (ids) => Object.fromEntries(ids.map((id, i) => [id, new MeshStandardMaterial({ name: `slot:${id}`, color: [0xcccccc, 0x8a8a8a, 0xb09070][i % 3] })]))

/** A model's GLB and manifest from a tree of meshes, as a bake writes them. */
async function bake(root) {
  const glb = Buffer.from(await new GLTFExporter().parseAsync(root, { binary: true }))
  return { glb, manifest: inspect(new Uint8Array(glb)) }
}

const placed = (position, turn = 0, scale = 1) => ({
  position,
  rotation: new Quaternion().setFromEuler(new Euler(0, turn, 0)).toArray(),
  scale: [scale, scale, scale],
})

const instance = (name, order, model, transform, slots = {}) => ({ id: objectID(name), kind: 'model_instance', name, order, transform, model: { name: model }, material_slots: slots })

/**
 * A code model of many parts (AC-1): parts boxes, each its own geometry as
 * a code-built model makes them, on a grid a metre wide, their slots in
 * turn; instances of it 2 × 2, 4 times the size.
 */
export async function multiPart({ parts = 1000, slots = 2, instances = 4 } = {}) {
  const root = new Group()
  const ids = Array.from({ length: slots }, (_, i) => String.fromCharCode(97 + i))
  const materials = slotMaterials(ids)
  const side = Math.ceil(Math.sqrt(parts))
  const cell = 1 / side
  for (let i = 0; i < parts; i++) {
    const m = new Mesh(new BoxGeometry(cell * 0.8, cell * 0.8, cell * 0.8), materials[ids[i % slots]])
    m.position.set((i % side) * cell - 0.5, cell / 2, Math.floor(i / side) * cell - 0.5)
    root.add(m)
  }
  const objects = Array.from({ length: instances }, (_, i) => instance(`m${i}x`, 10 + i, 'parts', placed([(i % 2) * 5 - 2.5, 0, Math.floor(i / 2) * 5 - 2.5], 0, 4)))
  return { models: { parts: await bake(root) }, objects }
}

/** n box primitives of one size on a grid. */
export function primitives(n = 1000) {
  const objects = []
  for (let i = 0; i < n; i++) objects.push({ id: objectID(`q${String(i).padStart(4, '0')}x`), kind: 'primitive', name: `b${i}`, order: 10 + i, transform: placed([(i % 32) * 0.5 - 8, 0, Math.floor(i / 32) * 0.5 - 8]), primitive: { builtin: 'box@1', params: { width: 0.3, height: 0.3, depth: 0.3 } } })
  return { models: {}, objects }
}

const profile = [new Vector2(0.15, 0), new Vector2(0.3, 0.1), new Vector2(0.22, 0.5), new Vector2(0.1, 0.8)]
/** Low-poly shapes, mostly boxes, as code-built furniture, fences and shelves are. */
const shapes = [
  () => new BoxGeometry(0.3, 0.3, 0.3),
  () => new BoxGeometry(0.6, 0.1, 0.3),
  () => new BoxGeometry(0.08, 0.8, 0.08),
  () => new CylinderGeometry(0.1, 0.1, 0.6, 8),
  () => new SphereGeometry(0.15, 8, 6),
  () => new LatheGeometry(profile, 8),
]
const shapeWeights = [0.35, 0.2, 0.2, 0.1, 0.07, 0.08]

/** The materials an instance's slot is given over the model's own: what creators recolour things with. */
const overridePalette = [
  { preset: 'principled@1', params: { color: '#a04030', roughness: 0.6 } },
  { preset: 'principled@1', params: { color: '#3060a0', roughness: 0.5 } },
  { preset: 'principled@1', params: { color: '#e0d8c8', roughness: 0.8 } },
  { preset: 'wood@1', params: {} },
  { preset: 'metal@1', params: {} },
  { preset: 'fabric@1', params: {} },
]

/**
 * The scale scene of the Epic's AC-3: 2000 instances of 30 code models of
 * 20–300 parts in 1–3 slots, 300 primitives, under 5 million triangles, the
 * sun and 2 spots and a point light casting shadows. A model of more parts
 * has fewer instances, as a scene has many chairs and few staircases.
 * `overrides` of the instances have their slots' materials replaced: by
 * default the share in the development database's scenes on 2026-10-25
 * (55 of 127 model instances, 15 scenes; P1 evidence).
 */
export async function scaleScene({ overrides = 0.43, seed = 7 } = {}) {
  const rand = random(seed)
  const pick = (weights) => {
    let r = rand()
    for (let i = 0; i < weights.length; i++) if ((r -= weights[i]) < 0) return i
    return weights.length - 1
  }
  const models = {}
  const kinds = []
  for (let k = 0; k < 30; k++) {
    const parts = 20 + Math.round((280 * k) / 29)
    const slots = 1 + (k % 3)
    const ids = Array.from({ length: slots }, (_, i) => String.fromCharCode(97 + i))
    const materials = slotMaterials(ids)
    const root = new Group()
    for (let i = 0; i < parts; i++) {
      const m = new Mesh(shapes[pick(shapeWeights)](), materials[ids[Math.floor(rand() * slots)]])
      m.position.set(rand() * 1.6 - 0.8, rand() * 1.2, rand() * 1.6 - 0.8)
      m.rotation.set(0, rand() * Math.PI, 0)
      root.add(m)
    }
    models[`k${k}`] = await bake(root)
    kinds.push({ name: `k${k}`, parts, slots: ids })
  }
  const weights = kinds.map((k) => 1 / k.parts)
  const total = weights.reduce((a, b) => a + b, 0)
  const counts = weights.map((w) => Math.floor((2000 * w) / total))
  for (let i = 0; counts.reduce((a, b) => a + b, 0) < 2000; i++) counts[i % counts.length]++
  const objects = []
  let n = 0
  kinds.forEach((kind, k) => {
    for (let j = 0; j < counts[k]; j++, n++) {
      // Overrides fall evenly through the instances: every overrides-th share of them.
      const recoloured = Math.floor((n + 1) * overrides) > Math.floor(n * overrides)
      const slots = recoloured ? Object.fromEntries(kind.slots.map((id) => [id, overridePalette[Math.floor(rand() * overridePalette.length)]])) : {}
      objects.push(instance(`i${String(n).padStart(4, '0')}x`, 10 + n, kind.name, placed([(n % 50) * 1.4 - 35 + rand() * 0.4, 0, Math.floor(n / 50) * 1.4 - 28 + rand() * 0.4], rand() * Math.PI * 2), slots))
    }
  })
  const builtins = [
    ['box@1', { width: 0.5, height: 0.5, depth: 0.5 }],
    ['cylinder@1', { radius_top: 0.2, radius_bottom: 0.2, height: 0.8, radial_segments: 16 }],
    ['sphere@1', { radius: 0.3, segments: 16 }],
  ]
  for (let i = 0; i < 300; i++) {
    const [builtin, params] = builtins[i % 3]
    objects.push({ id: objectID(`q${String(i).padStart(3, '0')}x`), kind: 'primitive', name: `p${i}`, order: 3000 + i, transform: placed([(i % 20) * 1.6 - 16, 0, Math.floor(i / 20) * 1.6 + 30]), primitive: { builtin, params } })
  }
  const light = (name, order, position, rotation, light) => ({ id: objectID(name), kind: 'light', name, order, transform: { position, rotation, scale: [1, 1, 1] }, light })
  const down = [-Math.SQRT1_2, 0, 0, Math.SQRT1_2]
  objects.push(
    light('spota', 4000, [-8, 6, 0], down, { type: 'spot', color: '#ffffff', intensity: 400, cast_shadow: true, distance: 0, decay: 2, angle: Math.PI / 5, penumbra: 0.2 }),
    light('spotb', 4001, [8, 6, 0], down, { type: 'spot', color: '#ffe0c0', intensity: 400, cast_shadow: true, distance: 0, decay: 2, angle: Math.PI / 5, penumbra: 0.2 }),
    light('pointa', 4002, [0, 3, 6], [0, 0, 0, 1], { type: 'point', color: '#ffd9b0', intensity: 80, cast_shadow: true, distance: 0, decay: 2 }),
  )
  return { models, objects, environment: { sky: { kind: 'procedural', sun_elevation_deg: 45, sun_azimuth_deg: 135, turbidity: 3 } } }
}

/**
 * The equivalence scene (P1 AC-2), two sets far apart, each with its camera.
 *  - A, no two parts touching: three overlapping panes of one glass slot in
 *    a frame, a model of procedural stone 2.5 times its size, an L of wall
 *    with a post in the sun (one box over the L would cover its corner).
 *  - B: boxes stacked face to face and floor tiles edge to edge, where
 *    surfaces of two parts meet at one depth.
 */
export async function equivalence() {
  const add = (root, geometry, material, at, turn = 0) => {
    const m = new Mesh(geometry, material)
    m.position.set(...at)
    m.rotation.y = turn
    root.add(m)
  }
  const glass = new Group()
  const g = slotMaterials(['glass', 'frame'])
  // Above the base, not on it: nothing in A meets at one depth.
  for (let i = 0; i < 3; i++) add(glass, new BoxGeometry(1.2, 1.4, 0.02), g.glass, [i * 0.25 - 0.25, 0.85 + i * 0.1, i * 0.3], i * 0.2)
  add(glass, new BoxGeometry(1.8, 0.1, 1.2), g.frame, [0, 0.05, 0.3])
  add(glass, new BoxGeometry(0.08, 1.6, 0.08), g.frame, [-0.85, 0.8, -0.2])

  const stone = new Group()
  const s = slotMaterials(['stone'])
  add(stone, new BoxGeometry(0.6, 0.3, 0.6), s.stone, [0, 0.15, 0])
  add(stone, new CylinderGeometry(0.15, 0.2, 0.8, 12), s.stone, [0, 0.72, 0], 0.4)
  add(stone, new SphereGeometry(0.18, 12, 8), s.stone, [0, 1.25, 0])

  const wall = new Group()
  const w = slotMaterials(['wall'])
  add(wall, new BoxGeometry(3, 1.6, 0.15), w.wall, [0, 0.8, -1.5])
  add(wall, new BoxGeometry(0.15, 1.5, 3), w.wall, [1.5, 0.75, 0])
  add(wall, new BoxGeometry(0.12, 2.2, 0.12), w.wall, [-1, 1.1, 1])

  const stacked = new Group()
  const b = slotMaterials(['block'])
  for (const [y, turn] of [
    [0.25, 0],
    [0.75, 0],
    [1.25, 0.4],
  ])
    add(stacked, new BoxGeometry(0.5, 0.5, 0.5), b.block, [0, y, 0], turn)
  for (const x of [1, 1.5]) {
    const tile = new Mesh(new PlaneGeometry(0.5, 0.5), b.block)
    tile.rotation.x = -Math.PI / 2
    tile.position.set(x, 0.01, 0)
    stacked.add(tile)
  }

  const models = { glass: await bake(glass), stone: await bake(stone), wall: await bake(wall), stacked: await bake(stacked) }
  const pane = { preset: 'principled@1', params: { color: '#a8c8e0', opacity: 0.4, roughness: 0.1 } }
  const camera = (name, order, position, pitch) => ({ id: objectID(name), kind: 'camera', name, order, transform: { position, rotation: new Quaternion().setFromEuler(new Euler(pitch, 0, 0)).toArray(), scale: [1, 1, 1] }, camera: { focal_mm: 35 } })
  const objects = [
    instance('glassx', 10, 'glass', placed([-2, 0, 0], 0.3), { glass: pane }),
    instance('stonex', 11, 'stone', placed([0.5, 0, -0.5], 0.5, 2.5), { stone: { preset: 'marble@1', params: {} } }),
    instance('wallx', 12, 'wall', placed([2.5, 0, -2], -0.4)),
    camera('shota', 20, [0, 1.6, 6], -0.16),
    instance('stackedx', 13, 'stacked', placed([30, 0, 0], 0.2)),
    camera('shotb', 21, [30.6, 1.2, 3], -0.2),
  ]
  return { models, objects, environment: { sky: { kind: 'procedural', sun_elevation_deg: 40, sun_azimuth_deg: 150, turbidity: 3 } } }
}
