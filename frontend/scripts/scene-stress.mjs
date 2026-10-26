// Stress and equivalence scenes for the scene editor's e2e (scene-performance
// P1). Model GLBs are written here in Node with three's GLTFExporter and
// checked by the sandbox's inspect(), as a bake would be; a scene is
// { models, objects, environment? }: models by name ({ glb, manifest }), and
// objects as the scene document has them, a model instance naming its model
// by `model: { name }` until the e2e seeds it and puts in its revision.
// Everything comes from a fixed seed: the same arguments give the same bytes.
import { readFileSync } from 'node:fs'
import {
  BoxGeometry,
  BufferAttribute,
  BufferGeometry,
  CylinderGeometry,
  Group,
  LatheGeometry,
  Matrix4,
  Mesh,
  MeshStandardMaterial,
  PlaneGeometry,
  Quaternion,
  Euler,
  SphereGeometry,
  Vector2,
  Vector3,
} from 'three'
import { GLTFExporter } from 'three/addons/exporters/GLTFExporter.js'
import { defaultLimits, inspect } from '../src/creative-scene/sandbox/inspect.ts'
import { cells, generate } from './fixtures/scene-stress/generate.mjs'

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

/**
 * A model at the triangle limit (300 000, sandbox/inspect.ts) in many parts:
 * 2000 spheres of 144 triangles in 2 slots, a metre wide.
 */
export async function capModel() {
  const root = new Group()
  const materials = slotMaterials(['a', 'b'])
  const side = Math.ceil(Math.sqrt(2000))
  const cell = 1 / side
  for (let i = 0; i < 2000; i++) {
    const m = new Mesh(new SphereGeometry(cell * 0.45, 12, 7), materials[i % 2 ? 'b' : 'a'])
    m.position.set((i % side) * cell - 0.5, cell / 2, Math.floor(i / side) * cell - 0.5)
    root.add(m)
  }
  return bake(root)
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
 *  - A, where no two surfaces of different parts or objects are ever at one
 *    depth (nothing touches, crosses or stands on the ground: a centimetre or
 *    two apart): three overlapping panes of one glass slot over a frame, a
 *    model of procedural stone 2.5 times its size, an L of wall with a post
 *    in the sun (one box over the L would cover its corner).
 *  - B, where they are: boxes stacked face to face on the ground, floor
 *    tiles edge to edge, a rod through a block. Which of two surfaces at one
 *    depth shows went by the objects' sort and goes by the parts' order now.
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
  for (let i = 0; i < 3; i++) add(glass, new BoxGeometry(1.2, 1.4, 0.02), g.glass, [i * 0.25 - 0.25, 0.85 + i * 0.1, i * 0.3], i * 0.2)
  add(glass, new BoxGeometry(1.8, 0.1, 1.2), g.frame, [0, 0.06, 0.3])
  add(glass, new BoxGeometry(0.08, 1.6, 0.08), g.frame, [-0.85, 0.93, -0.2])

  const stone = new Group()
  const s = slotMaterials(['stone'])
  add(stone, new BoxGeometry(0.6, 0.3, 0.6), s.stone, [0, 0.16, 0])
  add(stone, new CylinderGeometry(0.15, 0.2, 0.8, 12), s.stone, [0, 0.73, 0], 0.4)
  add(stone, new SphereGeometry(0.18, 12, 8), s.stone, [0, 1.33, 0])

  const wall = new Group()
  const w = slotMaterials(['wall'])
  add(wall, new BoxGeometry(2.9, 1.6, 0.15), w.wall, [-0.05, 0.81, -1.5])
  add(wall, new BoxGeometry(0.15, 1.5, 3), w.wall, [1.5, 0.76, 0])
  add(wall, new BoxGeometry(0.12, 2.2, 0.12), w.wall, [-1, 1.11, 1])

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
  add(stacked, new BoxGeometry(0.4, 0.4, 0.4), b.block, [-0.8, 0.2, 0], 0.3)
  add(stacked, new CylinderGeometry(0.06, 0.06, 1.2, 12), b.block, [-0.8, 0.5, 0])

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

const maxTriangles = defaultLimits.maxTriangles

/**
 * S0's hall (fixtures/scene-stress/spec.json) as models fit to the sandbox's
 * limit: each plain surface a model, its triangles brought down from 300 000
 * to the most its grid keeps within 300 000; each hero of 1.5 million cut into
 * models of at most 300 000, placed together. About 20 million triangles.
 */
export const hallSpec = (() => {
  const spec = JSON.parse(readFileSync(new URL('fixtures/scene-stress/spec.json', import.meta.url), 'utf8'))
  const within = (type, triangles) => {
    let t = triangles
    while (2 * cells(type, t)[0] * cells(type, t)[1] > maxTriangles) t -= 1000
    return t
  }
  return { ...spec, geometries: spec.geometries.map((f) => (f.hero ? f : { ...f, triangles: within(f.type, f.triangles) })) }
})()

/** A surface cut into pieces of at most max triangles, each with only the vertices it uses. */
function pieces({ positions, normals, indices }, max) {
  const out = []
  const remap = new Int32Array(positions.length / 3)
  for (let t = 0; t < indices.length; t += max * 3) {
    const triangles = indices.subarray(t, Math.min(indices.length, t + max * 3))
    remap.fill(-1)
    const index = new Uint32Array(triangles.length)
    let used = 0
    for (let k = 0; k < triangles.length; k++) {
      if (remap[triangles[k]] < 0) remap[triangles[k]] = used++
      index[k] = remap[triangles[k]]
    }
    const p = new Float32Array(used * 3)
    const n = new Float32Array(used * 3)
    for (let v = 0; v < remap.length; v++) {
      if (remap[v] < 0) continue
      p.set(positions.subarray(v * 3, v * 3 + 3), remap[v] * 3)
      n.set(normals.subarray(v * 3, v * 3 + 3), remap[v] * 3)
    }
    out.push({ positions: p, normals: n, indices: index })
  }
  return out
}

/** A model of one surface in one slot, as a bake writes it. */
function surfaceModel({ positions, normals, indices }) {
  const g = new BufferGeometry()
  g.setAttribute('position', new BufferAttribute(positions, 3))
  g.setAttribute('normal', new BufferAttribute(normals, 3))
  g.setIndex(new BufferAttribute(indices, 1))
  const root = new Group()
  root.add(new Mesh(g, slotMaterials(['a']).a))
  return bake(root)
}

/**
 * S0's stress scene in the editor (S1 tasks 8.1): the hall's floor and walls as
 * boxes, its 2000 instances of the surfaces as model instances (43% of them
 * recoloured, as scaleScene), its camera, and a mirror hung behind the knot
 * hero above the clutter, tilted to show the camera the knot. only (geometry
 * ids) bakes just those models; the objects are all there either way.
 */
export async function hallScene({ only } = {}) {
  const { instances } = generate(hallSpec, { only: [] })
  /** Model names by geometry id, numbered as generate() numbers them: a hero's pieces. */
  const named = {}
  let serial = 0
  for (const family of hallSpec.geometries) {
    const [nu, nv] = cells(family.type, family.triangles)
    const count = Math.ceil((2 * nu * nv) / maxTriangles)
    for (let k = 0; k < family.count; k++, serial++) {
      const id = `${family.type}-${String(serial).padStart(2, '0')}`
      named[id] = count > 1 ? Array.from({ length: count }, (_, p) => `${id}.${p}`) : [id]
    }
  }
  const models = {}
  for (const id of only ?? Object.keys(named)) {
    const [surface] = generate(hallSpec, { only: [id] }).geometries
    const parts = named[id].length > 1 ? pieces(surface, maxTriangles) : [surface]
    for (const [k, part] of parts.entries()) models[named[id][k]] = await surfaceModel(part)
  }
  const rand = random(hallSpec.seed)
  const objects = []
  const { width, depth, height, wall } = hallSpec.room
  const box = (name, order, position, [w, h, d], slots = {}, turn = 0) => ({ id: objectID(name), kind: 'primitive', name, order, transform: placed(position, turn), primitive: { builtin: 'box@1', params: { width: w, height: h, depth: d } }, material_slots: slots })
  // box@1 stands on y = 0: the floor's top at 0, the walls on it.
  objects.push(
    box('hallfloor', 1, [0, -wall, 0], [width, wall, depth]),
    box('wallback', 2, [0, 0, -depth / 2 - wall / 2], [width, height, wall]),
    box('wallfront', 3, [0, 0, depth / 2 + wall / 2], [width, height, wall]),
    box('wallleft', 4, [-width / 2 - wall / 2, 0, 0], [wall, height, depth]),
    box('wallright', 5, [width / 2 + wall / 2, 0, 0], [wall, height, depth]),
  )
  let order = 10
  let knot = null
  instances.forEach((inst, n) => {
    if (inst.geometry === 'room') return
    const m = inst.matrix
    const scale = Math.hypot(m[0], m[1], m[2])
    const transform = placed([m[12], m[13], m[14]], Math.atan2(m[8], m[0]), scale)
    const recoloured = Math.floor((n + 1) * 0.43) > Math.floor(n * 0.43)
    const slots = recoloured ? { a: overridePalette[Math.floor(rand() * overridePalette.length)] } : {}
    named[inst.geometry].forEach((model, k) => objects.push(instance(named[inst.geometry].length > 1 ? `h${n}p${k}x` : `h${String(n).padStart(4, '0')}x`, order++, model, transform, slots)))
      // The knot hero's middle: it stands on the floor, about 1.4 m high at its scale.
    if (named[inst.geometry].length > 1 && inst.geometry.startsWith('knot-')) knot = new Vector3(m[12], 0.7, m[14])
  })
  const { position: eye, target, focal_mm } = hallSpec.camera
  const look = new Quaternion().setFromRotationMatrix(new Matrix4().lookAt(new Vector3(...eye), new Vector3(...target), new Vector3(0, 1, 0)))
  objects.push({ id: objectID('hallcam'), kind: 'camera', name: 'hall', order: order++, transform: { position: eye, rotation: look.toArray(), scale: [1, 1, 1] }, camera: { focal_mm } })
  // The mirror 3 m behind the knot and 2.6 m up, over the clutter from the camera; its face half way between the
  // camera and the knot, so the camera sees the knot in it. A box turns about the middle of its base.
  const centre = new Vector3(knot.x, 2.6, knot.z - 3)
  const face = new Vector3(...eye).sub(centre).normalize().add(knot.clone().sub(centre).normalize()).normalize()
  const tilt = new Quaternion().setFromEuler(new Euler(Math.asin(-face.y), Math.atan2(face.x, face.z), 0, 'YXZ'))
  const [w, h] = [3, 1.8]
  const base = centre.clone().sub(new Vector3(0, h / 2, 0).applyQuaternion(tilt))
  objects.push({ ...box('hallmirror', order++, base.toArray(), [w, h, 0.05], { main: { preset: 'principled@1', params: { color: '#ffffff', metalness: 1, roughness: 0 } } }), transform: { position: base.toArray(), rotation: tilt.toArray(), scale: [1, 1, 1] } })
  return { models, objects, environment: { sky: { kind: 'procedural', sun_elevation_deg: 45, sun_azimuth_deg: 135, turbidity: 3 } } }
}
