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

import { ShaderLib } from 'three'
import { canInject, injectPattern, patternsSupported } from '../src/creative-scene/materials/inject.ts'
import { instanceCount, keyOf, makeMaterial, materialFor, release, retain } from '../src/creative-scene/materials/instances.ts'
import { patternGLSL } from '../src/creative-scene/materials/patterns.ts'
import { catalogMaterial } from '../src/creative-scene/materials/resolve.ts'

test('this three.js has every chunk the presets inject at; every preset has its shader', () => {
  assert.equal(patternsSupported, true)
  assert.equal(canInject(ShaderLib.physical.vertexShader, ShaderLib.physical.fragmentShader.replace('#include <normal_fragment_maps>', '')), false, 'a renamed chunk is noticed')
  for (const m of materialCatalog.materials) if (m.kind === 'pattern') assert.ok(patternGLSL[m.id], `${m.id} has no shader`)
  const shader = { vertexShader: ShaderLib.physical.vertexShader, fragmentShader: ShaderLib.physical.fragmentShader }
  assert.equal(injectPattern(shader, materialEntry('stone_brick@1')!), true)
  assert.match(shader.vertexShader, /vPatPos = position \* patScale;/)
  assert.match(shader.fragmentShader, /uniform float pm_brick_size;\nuniform vec3 pm_mortar_color;/)
  for (const line of ['PatOut pat = patternEval();', 'roughnessFactor = pat.rough;', 'metalnessFactor = pat.metal;', 'normal = patBumpNormal(-vViewPosition, normal, pat.height);', 'totalEmissiveRadiance += pat.emission;'])
    assert.ok(shader.fragmentShader.includes(line), line)
  const broken = { vertexShader: 'void main() {}', fragmentShader: 'void main() {}' }
  assert.equal(injectPattern(broken, materialEntry('stone_brick@1')!), false)
  assert.equal(broken.fragmentShader, 'void main() {}', 'left alone when an anchor is missing')
})

test('instances are shared by key; a preset has one program whatever its parameters', () => {
  const a = catalogMaterial('marble@1', { scale: 2 })!
  const b = catalogMaterial('marble@1', { scale: 2, roughness: 0.25 })!
  const c = catalogMaterial('marble@1', { scale: 3 })!
  assert.equal(keyOf(a, false), keyOf(b, false), 'defaults filled in alike')
  assert.notEqual(keyOf(a, false), keyOf(c, false))
  assert.notEqual(keyOf(a, false), keyOf(a, true))
  const ma = makeMaterial(a, false)
  const mc = makeMaterial(c, false)
  assert.equal(ma.customProgramCacheKey(), mc.customProgramCacheKey())
  assert.equal(ma.customProgramCacheKey(), 'pattern:marble@1')
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
