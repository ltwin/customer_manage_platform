// The stress scene's generator (generate.mjs): the size its spec asks for, the same bytes for the same seed,
// and surfaces fit to draw. Run with node --test.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { counts, generate } from './generate.mjs'

const spec = JSON.parse(readFileSync(new URL('spec.json', import.meta.url), 'utf8'))
const digest = (g) => createHash('sha256').update(g.positions).update(g.normals).update(g.indices).digest('hex')

test('the spec gives about 20 million triangles of distinct geometry in 2000 instances', () => {
  const c = counts(spec)
  assert.ok(Math.abs(c.triangles / 20e6 - 1) <= 0.05, `${c.triangles} triangles`)
  assert.equal(c.instances, 2000)
  assert.equal(c.geometries, 60)
  const { geometries, instances } = generate(spec, { only: [] })
  assert.equal(geometries.length, 0)
  assert.equal(instances.length, c.instances)
})

test('the same seed gives the same geometry and layout, and a geometry is the same drawn alone', () => {
  const ids = ['blob-00', 'knot-15', 'terrain-43']
  const a = generate(spec, { only: ids })
  const b = generate(spec, { only: ids })
  assert.deepEqual(a.geometries.map(digest), b.geometries.map(digest))
  assert.deepEqual(a.instances, b.instances)
  const alone = generate(spec, { only: ['knot-15'] }).geometries[0]
  assert.equal(digest(alone), digest(a.geometries[1]))
  const other = generate({ ...spec, seed: spec.seed + 1 }, { only: ['blob-00'] }).geometries[0]
  assert.notEqual(digest(other), digest(a.geometries[0]), 'another seed, another blob')
})

test('each kind of surface is fit to draw: finite, unit normals, indices in range, standing on the floor', () => {
  const started = performance.now()
  const { geometries } = generate(spec, { only: ['room', 'blob-01', 'knot-16', 'column-29', 'terrain-44'] })
  console.log(`4 surfaces of about 300 000 triangles in ${Math.round(performance.now() - started)} ms`)
  for (const g of geometries) {
    const vertices = g.positions.length / 3
    let minY = Infinity
    for (let i = 0; i < g.positions.length; i++) assert.ok(Number.isFinite(g.positions[i]), `${g.id}: finite`)
    for (let i = 1; i < g.positions.length; i += 3) minY = Math.min(minY, g.positions[i])
    if (g.id !== 'room') assert.equal(minY, 0, `${g.id}: stands on y = 0`)
    for (let v = 0; v < vertices; v++) {
      const l = Math.hypot(g.normals[3 * v], g.normals[3 * v + 1], g.normals[3 * v + 2])
      assert.ok(Math.abs(l - 1) < 1e-3, `${g.id}: normal ${v} of length ${l}`)
    }
    for (const i of g.indices) assert.ok(i < vertices, `${g.id}: index in range`)
    // Wound with the normals: the first triangle's face normal agrees with its vertices'.
    const [a, b, c] = [g.indices[0], g.indices[1], g.indices[2]].map((i) => [0, 1, 2].map((k) => g.positions[3 * i + k]))
    const face = [0, 1, 2].map((k) => ((b[(k + 1) % 3] - a[(k + 1) % 3]) * (c[(k + 2) % 3] - a[(k + 2) % 3]) - (b[(k + 2) % 3] - a[(k + 2) % 3]) * (c[(k + 1) % 3] - a[(k + 1) % 3])))
    const n = [0, 1, 2].map((k) => g.normals[3 * g.indices[0] + k])
    if (Math.hypot(...face) > 1e-12) assert.ok(face[0] * n[0] + face[1] * n[1] + face[2] * n[2] > 0, `${g.id}: wound with its normals`)
  }
})
