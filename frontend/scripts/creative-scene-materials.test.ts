import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { completeParams, hintPreset, materialCatalog, materialEntry, principledID } from '../src/creative-scene/materials/catalog.ts'

test('the frontend catalog is the backend one, copied by make generate', () => {
  const backend = readFileSync(new URL('../../backend/internal/creativescene/materials.json', import.meta.url), 'utf8')
  const frontend = readFileSync(new URL('../src/creative-scene/materials.gen.json', import.meta.url), 'utf8')
  assert.equal(frontend, backend)
  assert.equal(materialCatalog.materials.length, 15, 'principled and 14 presets')
  assert.equal(Object.keys(materialEntry(principledID)!.params).length, 20)
})

test('slots read complete: defaults fill in, numbers clamp into range, bad colours fall back', () => {
  const brick = materialEntry('stone_brick@1')!
  assert.deepEqual(completeParams(brick, { brick_size: 9, mortar_color: 'grey', roughness: 0.4 }), {
    color: '#8f8676',
    brick_size: 2,
    mortar_color: '#bdb5a6',
    roughness: 0.4,
    metalness: 0,
  })
  assert.equal(completeParams(materialEntry(principledID)!, { ior: 0.5 }).ior, 1)
})

test('slot hints map to presets whole, ignoring case and spaces', () => {
  assert.equal(hintPreset(' Stone '), 'stone@1')
  assert.equal(hintPreset('brick'), 'stone_brick@1')
  assert.equal(hintPreset('old stone'), undefined)
  assert.equal(hintPreset(null), undefined)
})

import { BoxGeometry, Group, Mesh, MeshStandardMaterial, Vector3 } from 'three'
import { flipWinding, partsOf, type MeshConstants } from '../src/creative-scene/models/cache.ts'
import { principledProps, seeThrough } from '../src/creative-scene/materials/principled.ts'
import { resolveSlot } from '../src/creative-scene/materials/resolve.ts'

const constants: MeshConstants = { color: '#336699', roughness: 0.3, metalness: 0.8, emissive: '#000000', emissiveIntensity: 1, vertexColors: false }

test('a slot resolves by the five rules, first match wins', () => {
  const assigned = { main: { preset: 'marble@1', params: { scale: 99 } }, lid: { preset: 'lava@2', params: {} } }
  const a = resolveSlot('main', assigned, 'wood', constants)
  assert.equal(a.source, 'assigned')
  assert.ok(a.material.kind === 'pattern' && a.material.id === 'marble@1' && a.material.params.scale === 10, 'clamped into range')
  assert.deepEqual(resolveSlot('lid', assigned, 'wood', constants), { slot: 'lid', source: 'unknown', material: { kind: 'unknown' } })
  const hinted = resolveSlot('body', assigned, ' Wood', constants)
  assert.ok(hinted.source === 'hint' && hinted.material.kind === 'pattern' && hinted.material.id === 'wood@1')
  const model = resolveSlot('body', assigned, 'unobtainium', constants)
  assert.ok(model.source === 'model' && model.material.kind === 'principled' && model.material.params.color === '#336699' && model.material.params.metalness === 0.8)
  const painted = resolveSlot(null, assigned, null, { ...constants, vertexColors: true }).material
  assert.ok(painted.kind === 'principled' && painted.vertexColors, 'unslotted code materials keep vertex colours')
  const primitive = resolveSlot('main', undefined, undefined, null)
  assert.ok(primitive.source === 'default' && primitive.material.kind === 'principled' && primitive.material.params.color === '#d9d5cd')
})

test('principled parameters go onto MeshPhysicalMaterial as three.js names them', () => {
  const p = resolveSlot('main', { main: { preset: 'principled@1', params: { opacity: 0.4, anisotropy_rotation: 1.2, iridescence_thickness: 600, sheen_color: '#ff0000' } } }, null, null)
  assert.ok(p.material.kind === 'principled')
  const props = principledProps(p.material.params)
  assert.equal(props.anisotropyRotation, 1.2, 'radians as stored')
  assert.deepEqual(props.iridescenceThicknessRange, [600, 600])
  assert.deepEqual([props.transparent, props.depthWrite], [true, false])
  assert.equal((props.sheenColor as { getHexString(): string }).getHexString(), 'ff0000')
  assert.equal(seeThrough(p.material.params), true)
  assert.equal(seeThrough({ ...p.material.params, opacity: 1, transmission: 0.5 }), true, 'glass is see-through')
  assert.equal(seeThrough({ ...p.material.params, opacity: 1 }), false)
})

test('model parts are baked into model space; a mirrored part keeps its faces facing out', () => {
  const shared = new BoxGeometry(1, 1, 1)
  const slot = new MeshStandardMaterial({ name: 'slot:body', color: '#ff0000' })
  slot.userData.hint = 'stone'
  const root = new Group()
  const left = new Mesh(shared, slot)
  left.position.set(-2, 0, 0)
  const right = new Mesh(shared, new MeshStandardMaterial({ roughness: 0.25 }))
  right.position.set(2, 0, 0)
  right.scale.set(-1, 1, 1)
  root.add(left, right)
  const { meshes } = partsOf(root)
  assert.notEqual(meshes[0].geometry, meshes[1].geometry, 'a shared geometry is copied before its transform goes in')
  assert.deepEqual([meshes[0].slot, meshes[0].hint, meshes[0].constants.color], ['body', 'stone', '#ff0000'])
  assert.deepEqual([meshes[1].slot, meshes[1].constants.roughness], [null, 0.25])
  meshes[1].geometry.computeBoundingBox()
  assert.equal(meshes[1].geometry.boundingBox!.min.x, 1.5)
  // The +X face of the mirrored box now faces −X in model space: its first
  // triangle's winding normal must agree with its stored normal.
  const g = meshes[1].geometry
  const [i0, i1, i2] = [g.index!.getX(0), g.index!.getX(1), g.index!.getX(2)]
  const p = (i: number) => new Vector3().fromBufferAttribute(g.attributes.position, i)
  const wound = p(i1).sub(p(i0)).cross(p(i2).sub(p(i0))).normalize()
  const stored = new Vector3().fromBufferAttribute(g.attributes.normal, i0)
  assert.ok(wound.dot(stored) > 0.99, `winding ${wound.toArray()} vs normal ${stored.toArray()}`)
  // Flipping twice is the original order.
  const before = Array.from(g.index!.array)
  flipWinding(g)
  flipWinding(g)
  assert.deepEqual(Array.from(g.index!.array), before)
})

import { uniform, vec3, float } from 'three/tsl'
import { instanceCount, keyOf, makeMaterial, materialFor, release, retain } from '../src/creative-scene/materials/instances.ts'
import { patternFns } from '../src/creative-scene/materials/patterns.ts'
import { catalogMaterial } from '../src/creative-scene/materials/resolve.ts'

test('every preset in the catalog has its TSL function, and it gives all five outputs', () => {
  const pattern = materialCatalog.materials.filter((m) => m.kind === 'pattern')
  assert.equal(pattern.length, 14)
  assert.deepEqual(Object.keys(patternFns).sort(), pattern.map((m) => m.id).sort())
  for (const m of pattern) {
    const pm = Object.fromEntries(Object.entries(m.params).map(([k, spec]) => [k, spec.type === 'color' ? uniform(vec3(0.5)) : uniform(0.5)]))
    const out = patternFns[m.id]({ pos: vec3(1, 2, 3), normal: vec3(0, 0, 1), footprint: float(0.001), pm })
    for (const k of ['albedo', 'rough', 'metal', 'height', 'emission'] as const) assert.ok(out[k]?.isNode, `${m.id}: ${k}`)
  }
})

test('instances are shared by key; a preset has one program whatever its parameters', () => {
  const a = catalogMaterial('marble@1', { scale: 2 })!
  const b = catalogMaterial('marble@1', { scale: 2, roughness: 0.25 })!
  const c = catalogMaterial('marble@1', { scale: 3 })!
  assert.equal(keyOf(a, false), keyOf(b, false), 'defaults filled in alike')
  assert.notEqual(keyOf(a, false), keyOf(c, false))
  assert.notEqual(keyOf(a, false), keyOf(a, true))
  // Parameters are uniforms: the graphs differ only in their values, which three leaves out of the program's key.
  const ma = makeMaterial(a, false)
  const mc = makeMaterial(c, false)
  for (const m of [ma, mc]) {
    assert.equal(m.name, 'marble@1')
    for (const node of [m.colorNode, m.roughnessNode, m.metalnessNode, m.normalNode, m.emissiveNode]) assert.ok(node?.isNode)
  }
  const key = keyOf(a, false)
  const before = instanceCount()
  const shared = materialFor(key, a, false)
  assert.equal(materialFor(key, a, false), shared)
  retain(key)
  release(key)
  assert.equal(instanceCount(), before + 1, 'kept a moment after the last user goes')
  const glass = makeMaterial(catalogMaterial('principled@1', { transmission: 1, roughness: 0 })!, true)
  assert.deepEqual([glass.transmission, glass.side], [1, 2])
  assert.equal(makeMaterial({ kind: 'unknown' }, false).color.getHexString(), '8a8a8a')
})

import type { SceneObject } from '../src/creative-scene/api.ts'
import { copyMaterials, readClipboard, setSlot, toPrincipled, writeClipboard } from '../src/creative-scene/materials/slotEdits.ts'

const obj = (n: number, slots: Record<string, { preset: string; params: Record<string, unknown> }> = {}): SceneObject => ({
  id: `sobj_${String(n).padStart(21, '0')}`, kind: 'model_instance', name: '', parent_id: null, order: n,
  transform: { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }, visible: true, locked: false, material_slots: slots as SceneObject['material_slots'],
})
const names: Record<string, string[]> = {}
const slotNames = (o: SceneObject) => names[o.id] ?? ['main']

test('a material set with several selected goes to every one with a slot of that name, in one update each', () => {
  const [a, b, c] = [obj(1), obj(2), obj(3)]
  names[a.id] = ['stone', 'trim']
  names[b.id] = ['stone']
  names[c.id] = ['main']
  const brick = () => ({ preset: 'stone_brick@1', params: {} })
  const updates = setSlot([a, b, c], [a.id, b.id, c.id], a.id, 'stone', slotNames, brick)
  assert.deepEqual(updates.map((u) => u.object_id), [a.id, b.id], 'the box has no stone slot')
  assert.deepEqual(updates[0].material_slots, { stone: brick() })
  const only = setSlot([a, b, c], [a.id, b.id, c.id], a.id, 'trim', slotNames, brick)
  assert.deepEqual(only.map((u) => u.object_id), [a.id], 'only the active object has trim')
})

test('转为原理化 keeps tint, roughness and metalness; emissive keeps its glow; patterns say they are dropped', () => {
  assert.deepEqual(toPrincipled({ preset: 'stone_brick@1', params: { color: '#112233' } }), {
    slot: { preset: 'principled@1', params: { color: '#112233', roughness: 0.85, metalness: 0 } },
    lost: true,
  })
  const glow = toPrincipled({ preset: 'emissive@1', params: { strength: 20 } })
  assert.deepEqual([glow.slot.params.emissive, glow.slot.params.emissive_intensity, glow.lost], ['#ffd9a0', 20, false])
})

test('复制材质到选中物件 copies by slot name, or one slot to all of them', () => {
  const pillar = obj(1, { stone: { preset: 'marble@1', params: { scale: 2 } } })
  const twin = obj(2, { trim: { preset: 'paint@1', params: {} } })
  const other = obj(3)
  const box = { ...obj(4, { main: { preset: 'wood@1', params: {} } }), kind: 'primitive' as const }
  names[pillar.id] = ['stone', 'trim']
  names[twin.id] = ['stone', 'trim']
  names[other.id] = ['body']
  names[box.id] = ['main']
  const all = [pillar, twin, other, box]
  const byName = copyMaterials(all, pillar.id, [pillar.id, twin.id, other.id], slotNames)
  assert.deepEqual(byName, [{ object_id: twin.id, material_slots: { stone: { preset: 'marble@1', params: { scale: 2 } } } }], 'unassigned trim clears; the body-only model is left alone')
  const fromBox = copyMaterials(all, box.id, [box.id, pillar.id, other.id], slotNames)
  assert.deepEqual(fromBox.map((u) => [u.object_id, Object.keys(u.material_slots!)]), [[pillar.id, ['stone', 'trim']], [other.id, ['body']]])
})

test('the material clipboard keeps a preset and its parameters in this browser', () => {
  const data = new Map<string, string>()
  const storage = { getItem: (k: string) => data.get(k) ?? null, setItem: (k: string, v: string) => void data.set(k, v) } as unknown as Storage
  assert.equal(readClipboard(storage), null)
  writeClipboard(storage, { preset: 'tile@1', params: { tile_size: 0.6 } })
  assert.deepEqual(readClipboard(storage), { preset: 'tile@1', params: { tile_size: 0.6 } })
  data.set('creative-scene:material-clipboard', '{broken')
  assert.equal(readClipboard(storage), null)
})
