import test from 'node:test'
import assert from 'node:assert/strict'
import { bendNormal, mappingOf, tangentNormal, triplanar, uvMapped, uvNormal, type Kit, type MappingIn } from '../src/creative-scene/materials/maps.ts'

// materials/maps.ts run on numbers: the same functions the shaders and the
// traced hits build from.

type V = number | number[]
const lift = (a: V, n: number) => (typeof a === 'number' ? Array<number>(n).fill(a) : a)
const zip = (a: V, b: V, f: (x: number, y: number) => number): V => {
  if (typeof a === 'number' && typeof b === 'number') return f(a, b)
  const n = Math.max(typeof a === 'number' ? 1 : a.length, typeof b === 'number' ? 1 : b.length)
  const [x, y] = [lift(a, n), lift(b, n)]
  return x.map((v, i) => f(v, y[i]))
}
const each = (a: V, f: (x: number) => number): V => (typeof a === 'number' ? f(a) : a.map(f))
const vec = (a: V) => a as number[]
const dot = (a: V, b: V) => vec(zip(a, b, (x, y) => x * y)).reduce((s, v) => s + v, 0)
const num: Kit<V> = {
  num: (v) => v,
  vec2: (x, y) => [x as number, y as number],
  vec3: (x, y, z) => [x as number, y as number, z as number],
  vec4: (xyz, w) => [...vec(xyz), w as number],
  add: (a, b) => zip(a, b, (x, y) => x + y),
  sub: (a, b) => zip(a, b, (x, y) => x - y),
  mul: (a, b) => zip(a, b, (x, y) => x * y),
  div: (a, b) => zip(a, b, (x, y) => x / y),
  max: (a, b) => zip(a, b, Math.max),
  abs: (a) => each(a, Math.abs),
  dot: (a, b) => dot(a, b),
  cross: (a, b) => {
    const [x, y] = [vec(a), vec(b)]
    return [x[1] * y[2] - x[2] * y[1], x[2] * y[0] - x[0] * y[2], x[0] * y[1] - x[1] * y[0]]
  },
  normalize: (a) => each(a, (v) => v / Math.sqrt(dot(a, a))),
  inverseSqrt: (a) => each(a, (v) => 1 / Math.sqrt(v)),
  side: (a) => each(a, (v) => (v < 0 ? -1 : 1)),
  x: (a) => vec(a)[0],
  y: (a) => vec(a)[1],
  z: (a) => vec(a)[2],
  w: (a) => vec(a)[3],
  xyz: (a) => vec(a).slice(0, 3),
}

const close = (a: V, b: V, what: string, eps = 1e-6) => assert.ok(vec(zip(a, b, (x, y) => Math.abs(x - y))).every((d) => d < eps), `${what}: ${JSON.stringify(a)} ≠ ${JSON.stringify(b)}`)
const mappingIn = (m: { scale: [number, number]; rotation: number; strength?: number }): MappingIn<V> => ({ scale: m.scale, cos: Math.cos(m.rotation), sin: Math.sin(m.rotation), strength: m.strength ?? 1 })
/** A normal map texel for the tangent-space normal n. */
const texelOf = (n: number[]) => [...n.map((v) => (v + 1) / 2), 1]
const rad = (deg: number) => (deg * Math.PI) / 180
/** Bent toward the texture's right: as three.js and Blender read a normal map, the surface leans right. */
const leanRight = [0.6, 0, 0.8]
const leanUp = [0, 0.6, 0.8]

test('mappings fill in their defaults: box projection, a metre a tile, no rotation, full strength', () => {
  assert.deepEqual(mappingOf(undefined), { kind: 'triplanar', size: 1, repeat: [1, 1], rotation: 0, strength: 1 })
  assert.deepEqual(mappingOf({ mapping: { kind: 'uv', repeat: [2, 3], rotation_deg: 180, normal_strength: 0.5 } }), { kind: 'uv', size: 1, repeat: [2, 3], rotation: Math.PI, strength: 0.5 })
})

test('UV mapping: a rotated texture\'s bumps turn with it', () => {
  // A floor (normal +z) whose UVs run with x and, glTF's rows from the top, against y: the image's up is +y.
  const N = [0, 0, 1]
  const uvAt = (x: number, y: number) => [x, -y]
  for (const deg of [0, 90, 180, 37]) {
    const m = mappingIn({ scale: [1, 1], rotation: rad(deg) })
    const b = (x: number, y: number) => uvMapped(num, uvAt(x, y), m)
    // Across a pixel: one unit of x, one of y.
    const db1 = num.sub(b(1, 0), b(0, 0))
    const db2 = num.sub(b(0, 1), b(0, 0))
    // Where the texture's right and up lie on the floor: along which (u', w') grows.
    const right = vec(num.normalize([vec(db1)[0], vec(db2)[0], 0]))
    const up = vec(num.normalize([vec(db1)[1], vec(db2)[1], 0]))
    const bent = uvNormal(num, N, [1, 0, 0], [0, 1, 0], db1, db2, tangentNormal(num, texelOf(leanRight), 1))
    close(bent, num.add(num.mul(right, 0.6), [0, 0, 0.8]), `${deg}°: leans to the texture's right`)
    const raised = uvNormal(num, N, [1, 0, 0], [0, 1, 0], db1, db2, tangentNormal(num, texelOf(leanUp), 1))
    close(raised, num.add(num.mul(up, 0.6), [0, 0, 0.8]), `${deg}°: leans to the texture's up`)
  }
  // 90°: the texture's right, +x at 0°, has turned to −y.
  const m = mappingIn({ scale: [1, 1], rotation: rad(90) })
  const b = (x: number, y: number) => uvMapped(num, uvAt(x, y), m)
  close(uvNormal(num, N, [1, 0, 0], [0, 1, 0], num.sub(b(1, 0), b(0, 0)), num.sub(b(0, 1), b(0, 0)), tangentNormal(num, texelOf(leanRight), 1)), [0, -0.6, 0.8], '90°')
})

/** The normal map's sample, the same texel everywhere; the colour map's, its coordinate, to see where it lands. */
const sampleWith = (normalTexel: number[]) => (which: string, coord: V) => (which === 'normal' ? normalTexel : [...vec(coord), 0, 1])

/** On a face of box projection: the texture coordinate at p, and the bent normal. */
function onFace(N: number[], p: number[], m: MappingIn<V>, normalTexel: number[]) {
  const s = triplanar(num, p, N, m, { color: true, normal: true, orm: false }, sampleWith(normalTexel))
  return { coord: vec(s.color).slice(0, 2), bent: bendNormal(num, N, s.bend!) }
}

test('box projection: on every face the texture reads the right way round, upright on walls, and its bumps follow it', () => {
  // Document axes, Y up (space.ts): the walls are the faces whose normal has no Y.
  const faces = [
    [1, 0, 0],
    [-1, 0, 0],
    [0, 1, 0],
    [0, -1, 0],
    [0, 0, 1],
    [0, 0, -1],
  ]
  for (const deg of [0, 90, 30]) {
    const m = mappingIn({ scale: [0.5, 0.5], rotation: rad(deg) })
    for (const N of faces) {
      // The surface's two axes, and where the texture's right and up lie along them: against its rows, which run from the top.
      const axes = [0, 1, 2].filter((i) => N[i] === 0).map((i) => [0, 1, 2].map((j) => (j === i ? 1 : 0)))
      const at = (p: number[]) => onFace(N, p, m, texelOf(leanRight)).coord
      const d = axes.map((a) => num.sub(at(a), at([0, 0, 0])))
      const right = vec(num.normalize(num.add(num.mul(axes[0], vec(d[0])[0]), num.mul(axes[1], vec(d[1])[0]))))
      const up = vec(num.normalize(num.add(num.mul(axes[0], -vec(d[0])[1]), num.mul(axes[1], -vec(d[1])[1]))))
      const face = `${deg}° on ${N}`
      close(num.cross(right, up), N, `${face}: not mirrored`)
      if (deg === 0 && N[1] === 0) close(up, [0, 1, 0], `${face}: upright`)
      close(onFace(N, [0.3, 0.2, 0.1], m, texelOf(leanRight)).bent, num.add(num.mul(right, 0.6), num.mul(N, 0.8)), `${face}: leans right`)
      close(onFace(N, [0.3, 0.2, 0.1], m, texelOf(leanUp)).bent, num.add(num.mul(up, 0.6), num.mul(N, 0.8)), `${face}: leans up`)
    }
  }
})

test('box projection: across a 45° slope the bent normal turns without a jump', () => {
  const m = mappingIn({ scale: [1, 1], rotation: 0 })
  let last: V | null = null
  let worst = 0
  for (let deg = 0; deg <= 90; deg += 0.25) {
    // From a wall facing +x over the edge to the top, facing +y.
    const N = [Math.cos(rad(deg)), Math.sin(rad(deg)), 0]
    const bent = onFace(N, [0.3, 0.2, 0.1], m, texelOf(leanUp)).bent
    if (last) worst = Math.max(worst, Math.acos(Math.min(1, dot(bent, last))))
    last = bent
  }
  // The geometric normal turns 0.25° a step; the bent one no more than a few times that.
  assert.ok(worst < rad(1.5), `largest step ${(worst * 180) / Math.PI}°`)
})

test('a normal map\'s strength scales its bend; 0 leaves the surface flat', () => {
  // A floor: Blender's +z face, its texture's right along Blender's −y, the document's +z.
  const N = [0, 1, 0]
  close(onFace(N, [0.3, 0.2, 0], mappingIn({ scale: [1, 1], rotation: 0, strength: 0 }), texelOf(leanRight)).bent, N, 'flat')
  const half = onFace(N, [0.3, 0.2, 0], mappingIn({ scale: [1, 1], rotation: 0, strength: 0.5 }), texelOf(leanRight)).bent
  close(half, num.normalize([0, 1, 0.3 / 0.8]), 'half the slope')
})

import { imagePlatform, resetImages } from '../src/creative-scene/materials/images.ts'
import { keyOf, makeMaterial } from '../src/creative-scene/materials/instances.ts'
import { catalogMaterial, onGeometry, type Texturing } from '../src/creative-scene/materials/resolve.ts'

// Building materials here never loads an image: fetches wait forever.
imagePlatform.fetch = () => new Promise(() => {})
imagePlatform.placeholder = (rgba) => ({ width: 1, height: 1, rgba })

/** A node graph as the shader is built from it: every node's kind, settings and inputs, not which texture or what uniform value. */
function shape(node: unknown, seen = new Map<object, number>()): string {
  const n = node as { isNode?: boolean; constructor: { name: string }; _getChildren(ignores: Set<unknown>): Iterable<{ property: string; childNode: unknown }> } | null
  if (!n?.isNode) return JSON.stringify(node ?? null)
  if (seen.has(n)) return `#${seen.get(n)}`
  seen.set(n, seen.size)
  const own = Object.entries(n).filter(([k, v]) => !/^(_|id$|uuid$|version$|value$)/.test(k) && v !== null && typeof v !== 'object' && typeof v !== 'function')
  const inputs = [...n._getChildren(new Set())].map(({ property, childNode }) => `${property}=${shape(childNode, seen)}`)
  return `${n.constructor.name}{${own.map(([k, v]) => `${k}:${String(v)}`).join(',')}|${inputs.join(',')}}`
}
const nodesOf = (m: ReturnType<typeof makeMaterial>) => {
  const seen = new Map<object, number>()
  return (['colorNode', 'normalNode', 'roughnessNode', 'metalnessNode', 'aoNode', 'opacityNode', 'alphaTestNode', 'maskShadowNode'] as const).map((k) => `${k}: ${shape(m[k], seen)}`).join('\n')
}
const textured = (t: Texturing, params: Record<string, unknown> = {}) => catalogMaterial('principled@1', params, t)!

const brick: Texturing = {
  maps: { base_color: { content_revision_id: 'ccr_brick' }, normal: { content_revision_id: 'ccr_brick_n' }, roughness: { content_revision_id: 'ccr_orm', channel: 'g' }, metalness: { content_revision_id: 'ccr_orm', channel: 'b' } },
  mapping: { kind: 'triplanar', size_m: 2, rotation_deg: 0, normal_strength: 1 },
}

test('another image only: a new instance, but the same graph, so the same program', () => {
  resetImages()
  const moss: Texturing = { ...brick, maps: { ...brick.maps, base_color: { content_revision_id: 'ccr_moss' }, normal: { content_revision_id: 'ccr_moss_n' } } }
  assert.notEqual(keyOf(textured(moss), false), keyOf(textured(brick), false))
  const a = makeMaterial(textured(brick), false)
  const b = makeMaterial(textured(moss), false)
  assert.equal(nodesOf(b), nodesOf(a))
  // So does another size, rotation or strength: uniforms.
  const turned = makeMaterial(textured({ ...brick, mapping: { kind: 'triplanar', size_m: 0.5, rotation_deg: 45, normal_strength: 2 } }), false)
  assert.equal(nodesOf(turned), nodesOf(a))
  // A map more or fewer, the other mapping, another alpha mode: another program.
  const others = [
    { ...brick, maps: { ...brick.maps, normal: undefined } },
    { ...brick, mapping: { kind: 'uv' as const, repeat: [1, 1], rotation_deg: 0, normal_strength: 1 } },
    { ...brick, alpha_mode: 'clip' as const },
    { ...brick, alpha_mode: 'blend' as const },
  ]
  for (const t of others) assert.notEqual(nodesOf(makeMaterial(textured(t), false)), nodesOf(a))
})

test('the alpha modes: opaque ignores the texture\'s alpha, clip cuts it in the picture and the shadow maps, blend multiplies it in', () => {
  resetImages()
  const opaque = makeMaterial(textured(brick), false)
  assert.ok(opaque.colorNode && opaque.normalNode && opaque.roughnessNode && opaque.metalnessNode && opaque.aoNode)
  assert.deepEqual([opaque.opacityNode, opaque.alphaTestNode, opaque.maskShadowNode, opaque.transparent], [null, null, null, false])
  const clip = makeMaterial(textured({ ...brick, alpha_mode: 'clip', alpha_cutoff: 0.3 }), false)
  assert.ok(clip.opacityNode && clip.alphaTestNode && clip.maskShadowNode)
  assert.equal((clip.alphaTestNode as unknown as { value: number }).value, 0.3)
  assert.deepEqual([clip.transparent, clip.depthWrite], [false, true])
  const blend = makeMaterial(textured({ ...brick, alpha_mode: 'blend' }), false)
  assert.ok(blend.opacityNode)
  assert.deepEqual([blend.alphaTestNode, blend.maskShadowNode, blend.transparent, blend.depthWrite], [null, null, true, false])
  // A clip slot with no maps: opacity alone against the cutoff.
  const bare = makeMaterial(textured({ alpha_mode: 'clip' }, { opacity: 0.2 }), false)
  assert.ok(bare.alphaTestNode && bare.maskShadowNode && !bare.colorNode)
})

test('no maps and no alpha mode: built as before', () => {
  resetImages()
  const plain = makeMaterial(catalogMaterial('principled@1', { roughness: 0.4 })!, false)
  for (const k of ['colorNode', 'normalNode', 'roughnessNode', 'metalnessNode', 'aoNode', 'opacityNode', 'alphaTestNode', 'maskShadowNode'] as const) assert.equal(plain[k], null, k)
})

test('UV mapping on a geometry without UVs draws with box projection, a metre a tile', () => {
  const uvMapped = textured({ ...brick, mapping: { kind: 'uv', repeat: [3, 3], rotation_deg: 30, normal_strength: 0.5 } })
  assert.equal(onGeometry(uvMapped, true), uvMapped)
  const fallback = onGeometry(uvMapped, false)
  assert.ok(fallback.kind === 'principled')
  assert.deepEqual(fallback.texturing?.mapping, { kind: 'triplanar', size_m: 1, rotation_deg: 30, normal_strength: 0.5 })
  const box = textured(brick)
  assert.equal(onGeometry(box, false), box, 'box projection needs no UVs')
})
