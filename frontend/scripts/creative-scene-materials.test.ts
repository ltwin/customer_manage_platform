import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { builtinSource, completeParams, hintPreset, materialCatalog, materialEntry, principledID, texturedEntries } from '../src/creative-scene/materials/catalog.ts'

test('the frontend catalog is the backend one, copied by make generate', () => {
  const backend = readFileSync(new URL('../../backend/internal/creativescene/materials.json', import.meta.url), 'utf8')
  const frontend = readFileSync(new URL('../src/creative-scene/materials.gen.json', import.meta.url), 'utf8')
  assert.equal(frontend, backend)
  assert.equal(materialCatalog.materials.length, 39, 'principled, 14 presets and 24 built-in textured materials')
  assert.equal(Object.keys(materialEntry(principledID)!.params).length, 21, 'the 20 three.js fields and vertex_color')
})

test('a built-in textured material has principled’s parameters with its own defaults', () => {
  const wood = materialEntry('wood_floor@1')!
  assert.equal(wood.kind, 'textured')
  assert.deepEqual(Object.keys(wood.params).sort(), Object.keys(materialEntry(principledID)!.params).sort())
  assert.equal(wood.params.color.default, '#ffffff')
  assert.equal(wood.params.roughness.default, 1)
  assert.equal(wood.params.ior.default, 1.5)
  assert.equal(materialEntry(principledID)!.params.color.default, '#cccccc', 'principled keeps its own')
  assert.equal(texturedEntries.length, 24)
  for (const m of texturedEntries) assert.ok(materialCatalog.categories.some((c) => c.id === m.category), m.id)
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
import { partsOf, type MeshConstants } from '../src/creative-scene/models/cache.ts'
import { flipWinding } from '../src/creative-scene/models/faces.ts'
import { principledProps, renderPolicy } from '../src/creative-scene/materials/principled.ts'
import { catalogMaterial, resolveSlot, type Texturing } from '../src/creative-scene/materials/resolve.ts'

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

test('a built-in textured material draws as principled with its textures, the slot’s mapping and alpha mode', () => {
  const wood = materialEntry('wood_floor@1')!
  const file = (k: 'base_color' | 'normal' | 'roughness') => builtinSource(wood.maps![k]!.file)
  const plain = catalogMaterial('wood_floor@1', { color: '#eeeeee' })!
  assert.equal(plain.kind, 'principled')
  if (plain.kind !== 'principled') return
  assert.equal(plain.preset, 'wood_floor@1')
  assert.equal(plain.params.color, '#eeeeee')
  assert.equal(plain.params.roughness, 1, 'the entry’s default')
  assert.deepEqual(plain.texturing?.maps?.base_color, { content_revision_id: file('base_color') })
  assert.deepEqual(plain.texturing?.maps?.roughness, { content_revision_id: file('roughness'), channel: 'g' })
  assert.match(file('normal'), /^tex:[0-9a-f]{64}\.jpg$/)
  assert.deepEqual(plain.texturing?.mapping, { kind: 'triplanar', size_m: 1.7, rotation_deg: 0, normal_strength: 1 }, 'its real size when the slot names none')
  assert.equal(plain.texturing?.alpha_mode, undefined)

  const slot: Texturing = { mapping: { kind: 'uv', repeat: [2, 2], rotation_deg: 0, normal_strength: 1 }, alpha_mode: 'clip' }
  const written = catalogMaterial('wood_floor@1', {}, slot)!
  assert.ok(written.kind === 'principled')
  assert.deepEqual(written.texturing?.mapping, slot.mapping)
  assert.equal(written.texturing?.alpha_mode, 'clip')
  assert.equal(written.texturing?.alpha_cutoff, 0.5)

  // Made explicit again it is the preset, not principled with the catalog's images.
  const assigned = { preset: 'wood_floor@1', params: { color: '#eeeeee' }, mapping: slot.mapping }
  const back = assignmentOf(resolveSlot('main', { main: assigned }, null, null))!
  assert.equal(back.preset, 'wood_floor@1')
  assert.equal('maps' in back, false)
  assert.deepEqual(back.mapping, slot.mapping)
})

test('principled parameters go onto MeshPhysicalMaterial as three.js names them', () => {
  const p = resolveSlot('main', { main: { preset: 'principled@1', params: { opacity: 0.4, anisotropy_rotation: 1.2, iridescence_thickness: 600, sheen_color: '#ff0000' } } }, null, null)
  assert.ok(p.material.kind === 'principled')
  const props = principledProps(p.material.params, renderPolicy(p.material))
  assert.equal(props.anisotropyRotation, 1.2, 'radians as stored')
  assert.deepEqual(props.iridescenceThicknessRange, [600, 600])
  assert.deepEqual([props.transparent, props.depthWrite], [true, false])
  assert.equal((props.sheenColor as { getHexString(): string }).getHexString(), 'ff0000')
})

// The decision table of design「材质渲染策略」, row by row: [alpha mode, opacity, transmission] → policy.
const policyRows: [Texturing['alpha_mode'], number, number, ReturnType<typeof renderPolicy>][] = [
  [undefined, 1, 0, { seeThrough: false, blended: false, depthWrite: true, alphaCutoff: null }],
  [undefined, 0.4, 0, { seeThrough: true, blended: true, depthWrite: false, alphaCutoff: null }],
  [undefined, 1, 0.5, { seeThrough: true, blended: false, depthWrite: true, alphaCutoff: null }],
  ['opaque', 0.4, 0.5, { seeThrough: true, blended: true, depthWrite: false, alphaCutoff: null }],
  ['clip', 0.4, 0, { seeThrough: false, blended: false, depthWrite: true, alphaCutoff: 0.5 }],
  ['clip', 1, 0.5, { seeThrough: true, blended: false, depthWrite: true, alphaCutoff: 0.5 }],
  ['blend', 1, 0, { seeThrough: true, blended: true, depthWrite: false, alphaCutoff: null }],
  ['blend', 0.4, 0.5, { seeThrough: true, blended: true, depthWrite: false, alphaCutoff: null }],
]

test('renderPolicy follows the decision table, and three lists as transparent exactly what it calls see-through', () => {
  for (const [mode, opacity, transmission, want] of policyRows) {
    const m = catalogMaterial('principled@1', { opacity, transmission }, mode ? { alpha_mode: mode } : undefined)!
    const policy = renderPolicy(m)
    assert.deepEqual(policy, want, `${mode} ${opacity} ${transmission}`)
    // The prepasses leave out three's transparent list: material.transparent, or transmission (render/sceneRenderer.ts).
    const props = principledProps(m.kind === 'principled' ? m.params : {}, policy)
    assert.equal(props.transparent === true || (props.transmission ?? 0) > 0, policy.seeThrough, `${mode} ${opacity} ${transmission}: three's list`)
  }
  const fine = catalogMaterial('principled@1', { opacity: 0.4 }, { alpha_mode: 'clip', alpha_cutoff: 0.2 })!
  assert.equal(renderPolicy(fine).alphaCutoff, 0.2)
  for (const m of [catalogMaterial('marble@1')!, { kind: 'unknown' } as const]) assert.deepEqual(renderPolicy(m), policyRows[0][3], 'presets and unknown are solid')
})

test('opaque, written or not, decides as see-through did: opacity < 1 or transmission > 0', () => {
  const seeThrough = (p: { opacity: number; transmission: number }) => p.opacity < 1 || p.transmission > 0
  for (const opacity of [0, 0.4, 0.999, 1]) {
    for (const transmission of [0, 0.001, 1]) {
      for (const slot of [undefined, { alpha_mode: 'opaque' as const }]) {
        const m = catalogMaterial('principled@1', { opacity, transmission }, slot)!
        const policy = renderPolicy(m)
        assert.equal(policy.seeThrough, seeThrough({ opacity, transmission }))
        // principledProps set these from opacity alone before alpha modes.
        assert.deepEqual([policy.blended, policy.depthWrite], [opacity < 1, opacity >= 1])
        assert.ok(m.kind === 'principled' && m.texturing === undefined, 'opaque is the default, left out')
      }
    }
  }
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

import { uniform, vec3, float, fwidth } from 'three/tsl'
import { instanceCount, keyOf, makeMaterial, materialFor, release, retain } from '../src/creative-scene/materials/instances.ts'
import { patternFns } from '../src/creative-scene/materials/patterns.ts'

test('every preset in the catalog has its TSL function, and it gives all five outputs', () => {
  const pattern = materialCatalog.materials.filter((m) => m.kind === 'pattern')
  assert.equal(pattern.length, 14)
  assert.deepEqual(Object.keys(patternFns).sort(), pattern.map((m) => m.id).sort())
  for (const m of pattern) {
    const pm = Object.fromEntries(Object.entries(m.params).map(([k, spec]) => [k, spec.type === 'color' ? uniform(vec3(0.5)) : uniform(0.5)]))
    const out = patternFns[m.id]({ pos: vec3(1, 2, 3), width: fwidth, normal: vec3(0, 0, 1), footprint: float(0.001), pm })
    for (const k of ['albedo', 'rough', 'metal', 'height', 'emission'] as const) assert.ok(out[k]?.isNode, `${m.id}: ${k}`)
  }
})

test('a preset takes the width of a pixel from its input, never from fwidth itself (scene-world-space-gi I7)', () => {
  // The presets that antialias: those with joints (brick, tile, planks), stone's cracks, marble's veins, wood's rings, fabric's weave.
  const antialiased = ['stone_brick@1', 'stone@1', 'marble@1', 'tile@1', 'wood@1', 'wood_plank@1', 'fabric@1']
  for (const m of materialCatalog.materials.filter((x) => x.kind === 'pattern')) {
    const pm = Object.fromEntries(Object.entries(m.params).map(([k, spec]) => [k, spec.type === 'color' ? uniform(vec3(0.5)) : uniform(0.5)]))
    let asked = 0
    patternFns[m.id]({ pos: vec3(1, 2, 3), width: (x: unknown) => (asked++, fwidth(x as never)), normal: vec3(0, 0, 1), footprint: float(0.001), pm })
    assert.equal(asked > 0, antialiased.includes(m.id), m.id)
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
  // Shadow maps take the faces toward the light: no lit ring where a one-sided caster touches the ground.
  for (const m of [ma, glass, makeMaterial({ kind: 'unknown' }, false)]) assert.equal(m.shadowSide, 2)
})

test('the key holds the whole slot: textures, mapping and alpha mode, whatever order they were written in', () => {
  const brick = { content_revision_id: 'ccr_brick' }
  const slot = { maps: { base_color: brick, roughness: { content_revision_id: 'ccr_orm', channel: 'g' as const } }, mapping: { kind: 'triplanar' as const, size_m: 1, rotation_deg: 0, normal_strength: 1 } }
  const key = (s?: Texturing) => keyOf(catalogMaterial('principled@1', { roughness: 0.5 }, s)!, false)
  const plain = key()
  const textured = key(slot)
  assert.notEqual(textured, plain)
  assert.equal(key({ mapping: slot.mapping, maps: { roughness: { channel: 'g', content_revision_id: 'ccr_orm' }, base_color: brick } }), textured, 'key order does not matter')
  assert.equal(key({ alpha_mode: 'opaque' }), plain, 'opaque written out is the default')
  assert.equal(key({ maps: {} }), plain)
  const clip = key({ ...slot, alpha_mode: 'clip' })
  assert.deepEqual(new Set([textured, clip, key({ ...slot, alpha_mode: 'blend' })]).size, 3, 'switching the alpha mode alone changes the key')
  assert.equal(key({ ...slot, alpha_mode: 'clip', alpha_cutoff: 0.5 }), clip, 'the default cutoff filled in')
  assert.notEqual(key({ ...slot, alpha_mode: 'clip', alpha_cutoff: 0.3 }), clip)
  assert.notEqual(key({ ...slot, maps: { ...slot.maps, base_color: { content_revision_id: 'ccr_moss' } } }), textured, 'another image')
  assert.notEqual(key({ ...slot, maps: { ...slot.maps, roughness: { content_revision_id: 'ccr_orm', channel: 'b' } } }), textured, 'another channel')
  assert.notEqual(key({ ...slot, mapping: { kind: 'uv', repeat: [1, 1], rotation_deg: 0, normal_strength: 1 } }), textured, 'another mapping')
  // A preset takes no textures: whatever a slot holds, it keys as its parameters.
  assert.equal(keyOf(catalogMaterial('marble@1', {}, slot)!, false), keyOf(catalogMaterial('marble@1')!, false))
})

import type { SceneObject } from '../src/creative-scene/api.ts'
import { assignmentOf, copyMaterials, readClipboard, setSlot, toPrincipled, withAlpha, withChannel, withMap, withMapping, writeClipboard } from '../src/creative-scene/materials/slotEdits.ts'

const obj = (n: number, slots: Record<string, { preset: string; params: Record<string, unknown> } & Record<string, unknown>> = {}): SceneObject => ({
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

// C5c1 5.1: the material panel's texture fields, each an edit of the whole slot.
const plain = { preset: 'principled@1', params: { color: '#808080', roughness: 0.4 } }
const rev = (n: number) => `ccrv_img${n}`

test('an image set on an input keeps the rest of the slot; replaced, its channel stays; the last one cleared takes the mapping along', () => {
  const one = withMap(plain, 'base_color', rev(1))
  assert.deepEqual(one, { ...plain, maps: { base_color: { content_revision_id: rev(1) } }, mapping: { kind: 'triplanar', size_m: 1, rotation_deg: 0, normal_strength: 1 } })
  const two = withChannel(withMap(one, 'roughness', rev(2)), 'roughness', 'g')
  assert.deepEqual(two.maps?.roughness, { content_revision_id: rev(2), channel: 'g' })
  assert.deepEqual(withMap(two, 'roughness', rev(3)).maps?.roughness, { content_revision_id: rev(3), channel: 'g' }, 'replaced: the channel stays')
  const clipped = withAlpha(two, 'clip')
  const bare = withMap(withMap(clipped, 'roughness', null), 'base_color', null)
  assert.deepEqual(bare, { ...plain, alpha_mode: 'clip', alpha_cutoff: 0.5 }, 'no images: no maps, no mapping; the alpha mode stays')
  assert.equal(withChannel(plain, 'ao', 'b'), plain, 'a channel without an image changes nothing')
})

test('switching the mapping keeps rotation and strength and drops what the other kind alone may have', () => {
  const box = withMapping(withMap(plain, 'normal', rev(1)), { size_m: 0.5, rotation_deg: 30, normal_strength: 2 })
  assert.deepEqual(box.mapping, { kind: 'triplanar', size_m: 0.5, rotation_deg: 30, normal_strength: 2 })
  const uv = withMapping(box, { kind: 'uv' })
  assert.deepEqual(uv.mapping, { kind: 'uv', repeat: [1, 1], rotation_deg: 30, normal_strength: 2 })
  assert.deepEqual(withMapping(uv, { repeat: [2, 3] }).mapping, { kind: 'uv', repeat: [2, 3], rotation_deg: 30, normal_strength: 2 })
  assert.deepEqual(withMapping(uv, { kind: 'triplanar' }).mapping, { kind: 'triplanar', size_m: 1, rotation_deg: 30, normal_strength: 2 })
  assert.equal(withMapping(plain, { size_m: 2 }), plain, 'no images: no mapping to set')
})

test('a built-in textured slot takes a mapping without maps, its own size the default', () => {
  const brick = materialEntry('red_brick@1')!
  const bare = { preset: 'red_brick@1', params: {} }
  assert.deepEqual(withMapping(bare, { rotation_deg: 90 }).mapping, { kind: 'triplanar', size_m: brick.size_m, rotation_deg: 90, normal_strength: 1 })
  const uv = withMapping(bare, { kind: 'uv' })
  assert.deepEqual(uv.mapping, { kind: 'uv', repeat: [1, 1], rotation_deg: 0, normal_strength: 1 })
  assert.equal(withMapping(uv, { kind: 'triplanar' }).mapping?.size_m, brick.size_m, 'back to box projection: the material’s real size')
  assert.equal(withMapping(bare, {}).maps, undefined, 'it never gains maps')
})

test('alpha modes: clip with its cutoff, blend and opaque without one; opaque written as nothing', () => {
  assert.deepEqual(withAlpha(plain, 'clip'), { ...plain, alpha_mode: 'clip', alpha_cutoff: 0.5 })
  assert.deepEqual(withAlpha(plain, 'clip', 0.3), { ...plain, alpha_mode: 'clip', alpha_cutoff: 0.3 })
  assert.deepEqual(withAlpha(withAlpha(plain, 'clip', 0.3), 'clip'), { ...plain, alpha_mode: 'clip', alpha_cutoff: 0.3 }, 'clip again keeps its cutoff')
  assert.deepEqual(withAlpha(withAlpha(plain, 'clip'), 'blend'), { ...plain, alpha_mode: 'blend' })
  assert.deepEqual(withAlpha(withAlpha(plain, 'blend'), 'opaque'), plain)
})

test('a texture edit is one update_objects per object, the whole slot map; a hinted slot made explicit keeps its textures', () => {
  const textured = withAlpha(withMap(plain, 'base_color', rev(1)), 'clip')
  const a = obj(11, { main: textured, trim: plain })
  names[a.id] = ['main', 'trim']
  const updates = setSlot([a], [a.id], a.id, 'main', slotNames, (_, cur) => withMap(cur!, 'normal', rev(2)))
  assert.deepEqual(updates, [{ object_id: a.id, material_slots: { main: { ...textured, maps: { base_color: { content_revision_id: rev(1) }, normal: { content_revision_id: rev(2) } } }, trim: plain } }])
  const { params: _params, ...explicit } = assignmentOf(resolveSlot('main', { main: textured }, null, null))!
  const { params: _given, ...given } = textured
  assert.deepEqual(explicit, given)
})

test('the clipboard carries a slot’s textures, mapping and alpha mode', () => {
  const data = new Map<string, string>()
  const storage = { getItem: (k: string) => data.get(k) ?? null, setItem: (k: string, v: string) => void data.set(k, v) } as unknown as Storage
  const textured = withAlpha(withMapping(withMap(plain, 'base_color', rev(1)), { kind: 'uv' }), 'blend')
  writeClipboard(storage, textured)
  assert.deepEqual(readClipboard(storage), textured)
})
