import test from 'node:test'
import assert from 'node:assert/strict'
import { existsSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { builtinCategories, builtinMaterial, builtinMaterials, builtinSlot, filterBuiltin, groupBuiltin } from '../src/creative-canvas/assets/builtin.ts'

const ids = (items: readonly { id: string }[]) => items.map((m) => m.id)

test('every built-in material is listed with its category, size and thumbnail', () => {
  assert.equal(builtinMaterials.length, 24)
  const brick = builtinMaterial('red_brick@1')!
  assert.equal(brick.label, '红砖')
  assert.equal(brick.category, 'brick')
  assert.equal(brick.categoryLabel, '砖墙')
  assert.ok(brick.sizeM > 0)
  assert.equal(brick.source.license, 'CC0')
  for (const m of builtinMaterials) assert.ok(existsSync(fileURLToPath(m.thumbnail)), `${m.id} has no thumbnail`)
  assert.deepEqual(builtinSlot('red_brick@1'), { preset: 'red_brick@1', params: {} })
  // A built-in model's own textures are not offered, but are there for the slots that use them.
  assert.ok(!builtinMaterials.some((m) => m.id === 'ubc_female_body@1'))
  assert.equal(builtinMaterial('ubc_female_body@1')?.label, '女素体皮肤')
  assert.ok(!builtinCategories.some((c) => c.id === 'character'), 'no empty 角色 button')
  assert.equal(builtinCategories.length, 10)
})

test('the filter matches name, category and source asset, case aside, every word', () => {
  assert.deepEqual(ids(filterBuiltin('BRICK')), ['red_brick@1', 'brick_wall_001@1', 'medieval_blocks_03@1'])
  assert.deepEqual(ids(filterBuiltin('砖墙')), ['red_brick@1', 'brick_wall_001@1', 'medieval_blocks_03@1'])
  assert.deepEqual(ids(filterBuiltin('木地板')), ['wood_floor@1'])
  assert.deepEqual(ids(filterBuiltin('  metal   rust ')), ['rusty_metal_02@1'])
  assert.deepEqual(filterBuiltin('玻璃'), [])
  assert.equal(filterBuiltin('').length, 24)
})

test('groups follow the catalog’s category order and skip empty ones', () => {
  const groups = groupBuiltin(filterBuiltin('砖'))
  assert.deepEqual(
    groups.map((g) => [g.label, ids(g.items)]),
    [
      ['砖墙', ['red_brick@1', 'brick_wall_001@1', 'medieval_blocks_03@1']],
      ['瓷砖', ['floor_tiles_06@1', 'terrazzo_tiles@1']],
    ],
  )
  assert.deepEqual(groupBuiltin(builtinMaterials).map((g) => g.category), ['wood', 'stone', 'brick', 'concrete', 'metal', 'fabric', 'ground', 'tiles', 'roofing', 'leather'])
})
