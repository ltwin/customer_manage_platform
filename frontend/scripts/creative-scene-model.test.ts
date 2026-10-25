import test, { mock } from 'node:test'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import * as THREE from 'three'
import {
  checkSyntax,
  CompileFailure,
  fillParams,
  locate,
  normalizeSchema,
  readModule,
  runtimeError,
  seeded,
} from '../src/creative-scene/sandbox/compile.ts'
import { checkModule, SandboxHost, type FrameFactory } from '../src/creative-scene/sandbox/host.ts'
import {
  defaultLimits,
  GLBError,
  inspect,
  matches,
  type Manifest,
} from '../src/creative-scene/sandbox/inspect.ts'
import { createLib1 } from '../src/creative-scene/sandbox/lib.ts'
import { ModelCache, modelSummaries, partsOf, type ModelMeshPart, type SceneModel } from '../src/creative-scene/models/cache.ts'
import { mergeParts } from '../src/creative-scene/models/merge.ts'
import { parseGLB } from '../src/creative-scene/sandbox/gltf.ts'
import { disposePieceViews, expandByPieces, pieceViews, piecesOf } from '../src/creative-scene/render/pieces.ts'
import { edgesOf, shellOf } from '../src/creative-scene/objects/wireframe.ts'
import { toCreasedNormals } from 'three/addons/utils/BufferGeometryUtils.js'
import {
  localContent,
  localRevision,
  parsePlaceholder,
  withModelRevisions,
  type SceneState,
} from '../src/creative-scene/state/apply.ts'
import {
  applyIntent,
  asConfirmed,
  mapModelIDs,
  resolveModelIDs,
  sendable,
  waitsOnModels,
  type Intent,
} from '../src/creative-scene/state/intents.ts'
import type { SceneObject } from '../src/creative-scene/api.ts'
import {
  isCompileRequest,
  readSandboxMessage,
  type CompileRequest,
  type ModuleInfo,
} from '../src/creative-scene/sandbox/protocol.ts'

// Shared with backend/internal/creativeglb: both sides must accept and reject
// the same files with the same manifests and reasons.
const testdata = path.resolve(import.meta.dirname, '../../backend/internal/creativeglb/testdata')
type Case = { name: string; file: string; manifest?: Manifest; reason?: string }
const cases = JSON.parse(readFileSync(path.join(testdata, 'cases.json'), 'utf8')) as Case[]

function fixture(file: string): ArrayBuffer {
  const b = readFileSync(path.join(testdata, file))
  return b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength) as ArrayBuffer
}

function reasonOf(fn: () => unknown): string {
  try {
    fn()
  } catch (err) {
    if (err instanceof GLBError) return err.reason
    throw err
  }
  return 'accepted'
}

test('inspect agrees with the server on every shared fixture', () => {
  assert.ok(cases.length >= 22)
  assert.ok(cases.some((c) => c.name === 'exported-template'), 'the GLTFExporter fixture is listed')
  for (const c of cases) {
    const glb = fixture(c.file)
    if (c.manifest) {
      assert.deepEqual(inspect(glb), c.manifest, c.name)
    } else {
      assert.equal(reasonOf(() => inspect(glb)), c.reason, c.name)
    }
  }
})

test('inspect budgets', () => {
  const basic = fixture('basic.glb')
  const parts = fixture('parts-anchors.glb')
  const tight = [
    [basic, { maxBytes: basic.byteLength - 1 }],
    [parts, { maxNodes: 5 }],
    [parts, { maxPrimitiveInstances: 2 }],
    [basic, { maxSlots: 0 }],
    [parts, { maxParts: 1 }],
    [parts, { maxAnchors: 1 }],
    [basic, { maxTriangles: 11 }],
  ] as const
  for (const [glb, limits] of tight) {
    assert.equal(reasonOf(() => inspect(glb, { ...defaultLimits, ...limits })), 'limit', JSON.stringify(limits))
  }
  assert.doesNotThrow(() => inspect(parts))
})

// The basic fixture with its JSON chunk edited as text, as the server's
// TestMemberParsing does.
function edited(edit: (json: string) => string): Uint8Array {
  const src = new Uint8Array(fixture('basic.glb'))
  const view = new DataView(src.buffer)
  const jsonLen = view.getUint32(12, true)
  const json = new TextDecoder().decode(src.subarray(20, 20 + jsonLen)).trimEnd()
  const bin = src.subarray(20 + jsonLen + 8)
  const text = new TextEncoder().encode(edit(json))
  const padded = new Uint8Array(Math.ceil(text.length / 4) * 4).fill(0x20)
  padded.set(text)
  const out = new Uint8Array(20 + padded.length + 8 + bin.length)
  const w = new DataView(out.buffer)
  w.setUint32(0, 0x46546c67, true)
  w.setUint32(4, 2, true)
  w.setUint32(8, out.length, true)
  w.setUint32(12, padded.length, true)
  w.setUint32(16, 0x4e4f534a, true)
  out.set(padded, 20)
  w.setUint32(20 + padded.length, bin.length, true)
  w.setUint32(24 + padded.length, 0x004e4942, true)
  out.set(bin, 28 + padded.length)
  return out
}

test('inspect reads members exactly like the server', () => {
  const rows: [string, (s: string) => string, string][] = [
    ['unchanged', (s) => s, 'accepted'],
    ['case variant member', (s) => s.replace('"name":"part:body"', '"Name":"part:body"'), 'json'],
    ['duplicate member last wins', (s) => s.replace('"name":"part:body"', '"name":"part:左","name":"part:body"'), 'accepted'],
    ['null number', (s) => s.replace('"mesh":0', '"mesh":null'), 'json'],
    ['fractional index', (s) => s.replace('"mesh":0', '"mesh":0.5'), 'json'],
    ['integral float index', (s) => s.replace('"mesh":0', '"mesh":0.0'), 'accepted'],
    ['byte order mark', (s) => '﻿' + s, 'json'],
    ['asset version', (s) => s.replace('"version":"2.0"', '"version":"1.0"'), 'json'],
    ['required extension', (s) => s.replace('"asset":', '"extensionsRequired":["KHR_materials_emissive_strength"],"asset":'), 'unsupported_feature'],
    ['second scene', (s) => s.replace('"scenes":[{"nodes":[0]}]', '"scenes":[{"nodes":[0]},{"nodes":[]}]'), 'scene_graph'],
    ['matrix and TRS', (s) => s.replace('"mesh":0', '"mesh":0,"translation":[0,0,0],"matrix":[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1]'), 'scene_graph'],
    ['camera on node', (s) => s.replace('"mesh":0', '"mesh":0,"camera":0'), 'unsupported_feature'],
    ['joints attribute', (s) => s.replace('"POSITION":0', '"POSITION":0,"JOINTS_0":0'), 'unsupported_feature'],
    ['slot hint too long', (s) => s.replace('"hint":"stone"', `"hint":"${'石'.repeat(65)}"`), 'name'],
    ['prototype member', (s) => s.replace('"mesh":0', '"mesh":0,"__proto__":{}'), 'json'],
    ['no triangles', (s) => s.replace('"scenes":[{"nodes":[0]}]', '"scenes":[{"nodes":[]}]'), 'scene_graph'],
  ]
  for (const [name, edit, want] of rows) {
    const glb = edited(edit)
    assert.notDeepEqual(edit === rows[0][1] ? null : glb, new Uint8Array(fixture('basic.glb')), `${name}: edit applied`)
    assert.equal(reasonOf(() => inspect(glb)), want, name)
    if (want === 'accepted') assert.deepEqual(inspect(glb).parts, ['body'], name)
  }
})

test('manifests match by the contract rules', () => {
  const base: Manifest = {
    bounds: { min: [0, 0, 0], max: [1, 2, 3] },
    triangles: 10,
    slots: [{ id: 'b' }, { id: 'a', hint: 'stone' }],
    parts: ['y', 'x'],
    anchors: [{ id: 'top', position: [0, 2, 0] }],
  }
  const variant = (f: (m: Manifest) => void): Manifest => {
    const m = structuredClone(base)
    f(m)
    return m
  }
  assert.ok(matches(base, variant(() => {})))
  assert.ok(matches(base, variant((m) => { m.slots.reverse(); m.parts.reverse() })))
  assert.ok(matches(base, variant((m) => { m.bounds.max[0] = 1 + 5e-5 })))
  assert.ok(matches(base, variant((m) => { m.slots[0].hint = '' })), 'empty hint equals no hint')
  assert.ok(!matches(base, variant((m) => { m.bounds.max[0] = 1 + 2e-4 })))
  assert.ok(!matches(base, variant((m) => { m.anchors[0].position[1] = 2.001 })))
  assert.ok(!matches(base, variant((m) => { m.triangles = 11 })))
  assert.ok(!matches(base, variant((m) => { m.slots[1].hint = 'wood' })))
  assert.ok(!matches(base, variant((m) => { m.parts = ['x'] })))
  assert.deepEqual(base.parts, ['y', 'x'], 'inputs are not reordered')
})

test('protocol messages are checked field by field', () => {
  assert.ok(isCompileRequest({ type: 'compile', id: 1, code: '', params: {}, lib_version: 1 }))
  assert.ok(!isCompileRequest({ type: 'compile', id: 1, code: '', params: {}, lib_version: 2 }))
  assert.ok(!isCompileRequest({ type: 'compile', id: 1.5, code: '', params: {}, lib_version: 1 }))
  assert.ok(!isCompileRequest({ type: 'compile', id: 1, code: '', params: [], lib_version: 1 }))

  assert.deepEqual(readSandboxMessage({ type: 'ready', extra: 1 }), { type: 'ready' })
  const glb = new ArrayBuffer(8)
  const ok = readSandboxMessage({ type: 'result', id: 3, ok: true, glb, module: {}, spoof: true })
  assert.deepEqual(ok, { type: 'result', id: 3, ok: true, glb, module: {} })
  const err = readSandboxMessage({ type: 'result', id: 3, ok: false, error: { kind: 'runtime', message: 'x', line: 2, column: 4, stack: 's' } })
  assert.deepEqual(err, { type: 'result', id: 3, ok: false, error: { kind: 'runtime', message: 'x', line: 2, column: 4 } })
  for (const bad of [
    null,
    'ready',
    { type: 'result', id: '3', ok: false, error: { kind: 'runtime', message: 'x' } },
    { type: 'result', id: 3, ok: true, glb: new Uint8Array(8), module: {} },
    { type: 'result', id: 3, ok: true, glb, module: null },
    { type: 'result', id: 3, ok: false, error: { kind: 'panic', message: 'x' } },
    { type: 'result', id: 3, ok: false, error: { kind: 'runtime', message: 'x'.repeat(2001) } },
    { type: 'result', id: 3, ok: false, error: { kind: 'runtime', message: 'x', line: -1 } },
    { type: 'result', id: 3, ok: 'yes' },
  ]) {
    assert.equal(readSandboxMessage(bad), null, JSON.stringify(bad))
  }
})

function failureOf(fn: () => unknown) {
  try {
    fn()
  } catch (err) {
    if (err instanceof CompileFailure) return err.detail
    throw err
  }
  return null
}

test('syntax errors carry line and column; imports are refused', () => {
  assert.deepEqual(failureOf(() => checkSyntax('export default function build() {\n  return (\n}\n')), {
    kind: 'syntax', message: 'Unexpected token', line: 3, column: 1,
  })
  assert.equal(failureOf(() => checkSyntax('export const mount = "floor"\nexport default () => null\n')), null)
  for (const [code, line] of [
    ['import * as T from "three"\n', 1],
    ['const x = 1\nexport { x } from "./other.js"\n', 2],
    ['export * from "https://example.com/x.js"\n', 1],
    ['export default async () => {\n  const m = 1\n  return import("https://example.com/x.js")\n}\n', 3],
  ] as const) {
    const f = failureOf(() => checkSyntax(code))
    assert.equal(f?.kind, 'invalid', code)
    assert.equal(f?.line, line, code)
  }
})

test('module exports are normalized; values are filled, clamped and dropped', () => {
  const schema = normalizeSchema({
    height: { type: 'number', default: 6, min: 2, max: 20, step: 0.1, unit: 'm', label: '高度', extra: 1, render() {} },
    count: { type: 'integer', default: 3, min: 1, max: 9 },
    style: { type: 'enum', default: 'a', options: ['a', 'b'] },
    on: { type: 'boolean', default: false },
    tint: { type: 'color', default: '#aabbcc' },
    ['__proto__']: { type: 'boolean', default: true },
  })
  assert.deepEqual(Object.keys(schema.height).sort(), ['default', 'label', 'max', 'min', 'step', 'type', 'unit'])
  assert.ok(Object.hasOwn(schema, '__proto__'), 'a parameter named __proto__ stays a parameter')
  const values = fillParams(schema, { height: 99, count: 2.6, style: 'c', on: 'yes', tint: '#123456', ghost: 1 })
  assert.deepEqual({ ...values }, { height: 20, count: 3, style: 'a', on: false, tint: '#123456', ['__proto__']: true })
  assert.deepEqual(normalizeSchema(undefined), {})
  assert.equal(failureOf(() => normalizeSchema([1]))?.kind, 'invalid')
  assert.equal(failureOf(() => normalizeSchema({ a: 1 }))?.kind, 'invalid')
  assert.equal(failureOf(() => normalizeSchema({ a: { type: 'number', default: 1n } }))?.kind, 'invalid')

  const build = () => null
  assert.deepEqual(readModule({ mount: 'wall', default: build }, 1, [1], 1), {
    contract_version: 1, lib_version: 1, mount: 'wall', params_schema: {},
  })
  assert.equal(failureOf(() => readModule({ mount: 'wall', default: build, lib_version: 2 }, 1, [1], 1))?.kind, 'invalid')
  assert.equal(failureOf(() => readModule({ default: build }, 1, [1], 1))?.kind, 'invalid')
  assert.equal(failureOf(() => readModule({ mount: 'floor' }, 1, [1], 1))?.kind, 'invalid')
})

test('runtime errors point at the module line in Chromium and WebKit stacks', () => {
  const url = 'blob:null/1b2c3d4e-0000-4000-8000-000000000000'
  const chromium = `ReferenceError: x is not defined\n    at build (${url}:4:9)\n    at compile (blob:null/abc:1:2)`
  const webkit = `build@${url}:4:23\ncompile@blob:null/abc:1:2`
  assert.deepEqual(locate(chromium, url), { line: 4, column: 9 })
  assert.deepEqual(locate(webkit, url), { line: 4, column: 23 })
  assert.equal(locate('at foo (blob:null/other:1:1)', url), undefined)

  const err = new ReferenceError('x is not defined')
  err.stack = chromium
  assert.deepEqual(runtimeError(err, url), { kind: 'runtime', message: 'ReferenceError: x is not defined', line: 4, column: 9 })
  assert.deepEqual(runtimeError('plain', url), { kind: 'runtime', message: 'plain' })
  const hostile = { get message() { throw new Error('no') }, toString() { throw new Error('no') } }
  assert.deepEqual(runtimeError(hostile, url), { kind: 'runtime', message: '未知错误' })
  assert.equal(runtimeError(new Error('x'.repeat(5000)), url).message.length, 2000)
})

test('seeded random is deterministic', () => {
  const a = seeded(7)
  const b = seeded(7)
  const seq = [a(), a(), a()]
  assert.deepEqual([b(), b(), b()], seq)
  assert.ok(seq.every((v) => v >= 0 && v < 1))
  assert.notDeepEqual([seeded(8)()], [seq[0]])
})

const pillar: ModuleInfo = {
  contract_version: 1,
  lib_version: 1,
  mount: 'floor',
  params_schema: {
    height: { type: 'number', default: 3, min: 1, max: 12, step: 0.1, unit: 'm', label: '高度' },
    style: { type: 'enum', default: 'round', options: ['round', 'square'], label: '柱身' },
    broken: { type: 'boolean', default: false },
    tint: { type: 'color', default: '#d9d5cd' },
    count: { type: 'integer', default: 4, min: 0, max: 8 },
  },
  params: { height: 3, style: 'round', broken: false, tint: '#d9d5cd', count: 4 },
}

function moduleError(raw: unknown): string | null {
  try {
    checkModule(raw)
    return null
  } catch (err) {
    return (err as Error).message
  }
}

test('the host checks module info by the server rules', () => {
  assert.deepEqual(checkModule(pillar), pillar)
  const canonical = checkModule({
    ...pillar,
    extra: true,
    params_schema: { ...pillar.params_schema, height: { ...pillar.params_schema.height, color: 'red', unit: null } },
    params: { height: 4 },
  })
  assert.deepEqual(Object.keys(canonical).sort(), ['contract_version', 'lib_version', 'mount', 'params', 'params_schema'])
  assert.deepEqual(canonical.params_schema.height, { type: 'number', default: 3, min: 1, max: 12, step: 0.1, label: '高度' })
  assert.deepEqual(canonical.params, { height: 4, style: 'round', broken: false, tint: '#d9d5cd', count: 4 }, 'missing values filled')

  const proto = checkModule({ ...pillar, params_schema: { ['__proto__']: { type: 'boolean', default: true } }, params: {} })
  assert.ok(Object.hasOwn(proto.params_schema, '__proto__') && Object.hasOwn(proto.params, '__proto__'))

  const spec = (p: Record<string, unknown>) => ({ ...pillar, params_schema: { p }, params: {} })
  const many = Object.fromEntries(Array.from({ length: 33 }, (_, i) => [`p${i}`, { type: 'boolean', default: false }]))
  const rejected: [string, unknown][] = [
    ['not an object', null],
    ['contract version', { ...pillar, contract_version: 2 }],
    ['lib version', { ...pillar, lib_version: 2 }],
    ['mount', { ...pillar, mount: 'roof' }],
    ['too many', { ...pillar, params_schema: many, params: {} }],
    ['name', { ...pillar, params_schema: { '1x': { type: 'boolean', default: false } }, params: {} }],
    ['type', spec({ type: 'vector', default: 1 })],
    ['no range', spec({ type: 'number', default: 1, min: 0 })],
    ['min above max', spec({ type: 'number', default: 1, min: 2, max: 1 })],
    ['step', spec({ type: 'number', default: 1, min: 0, max: 2, step: 0 })],
    ['numeric options', spec({ type: 'integer', default: 1, min: 0, max: 2, options: [] })],
    ['default outside range', spec({ type: 'number', default: 3, min: 0, max: 2 })],
    ['integer default', spec({ type: 'integer', default: 1.5, min: 0, max: 2 })],
    ['enum without options', spec({ type: 'enum', default: 'a' })],
    ['enum duplicate', spec({ type: 'enum', default: 'a', options: ['a', 'a'] })],
    ['enum default', spec({ type: 'enum', default: 'c', options: ['a'] })],
    ['enum unit', spec({ type: 'enum', default: 'a', options: ['a'], unit: 'm' })],
    ['boolean with min', spec({ type: 'boolean', default: true, min: 0 })],
    ['boolean options', spec({ type: 'boolean', default: true, options: ['a'] })],
    ['color', spec({ type: 'color', default: 'red' })],
    ['label too long', spec({ type: 'boolean', default: true, label: '长'.repeat(41) })],
    ['unit too long', spec({ type: 'number', default: 1, min: 0, max: 2, unit: 'u'.repeat(17) })],
    ['undefined value', { ...pillar, params: { ghost: 1 } }],
    ['value outside range', { ...pillar, params: { height: 13 } }],
    ['value type', { ...pillar, params: { broken: 'yes' } }],
  ]
  for (const [name, raw] of rejected) assert.notEqual(moduleError(raw), null, name)
  assert.equal(moduleError(spec({ type: 'number', default: 3, min: 0, max: 2 })), '参数 p：默认值须在 0 到 2 之间')
})

type FakeFrame = { posted: CompileRequest[]; send: (data: unknown) => void; destroyed: boolean }

function fakeFrames() {
  const frames: FakeFrame[] = []
  const factory: FrameFactory = (onMessage) => {
    const frame: FakeFrame = { posted: [], send: onMessage, destroyed: false }
    frames.push(frame)
    return { post: (r) => frame.posted.push(r), destroy: () => { frame.destroyed = true } }
  }
  return { frames, factory }
}

const settle = () => new Promise((resolve) => setImmediate(resolve))
const templateGLB = () => fixture('exported-template.glb')
const templateManifest = cases.find((c) => c.name === 'exported-template')?.manifest

test('the host waits for the page, matches ids and recomputes everything', async () => {
  const { frames, factory } = fakeFrames()
  const host = new SandboxHost(factory)
  const code = 'export default 1'
  const pending = host.compile({ code, params: { height: 4 }, libVersion: 1 })
  await settle()
  assert.equal(frames.length, 1)
  assert.equal(frames[0].posted.length, 0, 'nothing is posted before the page is ready')
  frames[0].send({ type: 'ready' })
  await settle()
  const [request] = frames[0].posted
  assert.deepEqual(request, { type: 'compile', id: request.id, code, params: { height: 4 }, lib_version: 1 })

  frames[0].send({ type: 'result', id: request.id + 1, ok: false, error: { kind: 'runtime', message: 'stale' } })
  frames[0].send({ type: 'result', id: request.id, ok: true, glb: 'not a buffer', module: pillar })
  frames[0].send({ type: 'result', id: request.id, ok: true, glb: templateGLB(), module: { ...pillar, extra: 1 } })
  const result = await pending
  assert.ok(result.ok)
  assert.deepEqual(result.module, pillar)
  assert.deepEqual(result.manifest, templateManifest)
  assert.equal(result.codeSHA256, 'sha256-' + createHash('sha256').update(code).digest('hex'))
  host.dispose()
  assert.ok(frames[0].destroyed)
})

test('the host reports sandbox errors, bad modules and GLBs outside the subset', async () => {
  const { frames, factory } = fakeFrames()
  const host = new SandboxHost(factory)
  const reply = async (message: (id: number) => unknown) => {
    const pending = host.compile({ code: 'x', params: {}, libVersion: 1 })
    await settle()
    if (frames.length === 1 && frames[0].posted.length === 0) frames[0].send({ type: 'ready' })
    await settle()
    const f = frames[frames.length - 1]
    f.send(message(f.posted[f.posted.length - 1].id))
    return pending
  }
  assert.deepEqual(await reply((id) => ({ type: 'result', id, ok: false, error: { kind: 'syntax', message: 'Unexpected token', line: 3, column: 1 } })), {
    ok: false, error: { kind: 'syntax', message: 'Unexpected token', line: 3, column: 1 },
  })
  const bad = await reply((id) => ({ type: 'result', id, ok: true, glb: templateGLB(), module: { ...pillar, mount: 'roof' } }))
  assert.deepEqual(bad, { ok: false, error: { kind: 'invalid', message: 'mount 须为 floor、wall、ceiling 或 free' } })
  const lines = await reply((id) => ({ type: 'result', id, ok: true, glb: fixture('bad-lines.glb'), module: pillar }))
  assert.ok(!lines.ok && lines.error.kind === 'invalid' && lines.error.message.includes('unsupported_feature'))
  const big = await reply((id) => ({ type: 'result', id, ok: true, glb: fixture('bad-too-many-triangles.glb'), module: pillar }))
  assert.ok(!big.ok && big.error.kind === 'limit')

  const before = frames.reduce((n, f) => n + f.posted.length, 0)
  const tooLong = await host.compile({ code: 'x'.repeat(64 * 1024 + 1), params: {}, libVersion: 1 })
  assert.deepEqual(tooLong, { ok: false, error: { kind: 'limit', message: '代码超过 64 KiB' } })
  assert.equal(frames.reduce((n, f) => n + f.posted.length, 0), before, 'oversized code is never sent')
  host.dispose()
})

test('a compile that runs past the timeout replaces the page; compiles never overlap', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const { frames, factory } = fakeFrames()
  const host = new SandboxHost(factory, 5000)
  const first = host.compile({ code: 'while (true) {}', params: {}, libVersion: 1 })
  const second = host.compile({ code: 'export default 2', params: {}, libVersion: 1 })
  await settle()
  frames[0].send({ type: 'ready' })
  await settle()
  assert.equal(frames[0].posted.length, 1, 'the second compile waits for the first')
  t.mock.timers.tick(5000)
  assert.deepEqual(await first, { ok: false, error: { kind: 'timeout', message: '编译超过 5 秒，已中止' } })
  assert.ok(frames[0].destroyed)
  assert.equal(frames.length, 2, 'a new page was started')
  frames[0].send({ type: 'result', id: frames[0].posted[0].id, ok: false, error: { kind: 'runtime', message: 'late' } })
  frames[1].send({ type: 'ready' })
  await settle()
  const [request] = frames[1].posted
  assert.equal(request.code, 'export default 2')
  frames[1].send({ type: 'result', id: request.id, ok: false, error: { kind: 'runtime', message: 'boom', line: 1, column: 1 } })
  assert.deepEqual(await second, { ok: false, error: { kind: 'runtime', message: 'boom', line: 1, column: 1 } })
  host.dispose()
  mock.timers.reset()
})

test('a page that never loads fails the compile and is replaced', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const { frames, factory } = fakeFrames()
  const host = new SandboxHost(factory)
  const pending = host.compile({ code: 'x', params: {}, libVersion: 1 })
  await settle()
  t.mock.timers.tick(10000)
  const result = await pending
  assert.ok(!result.ok && result.error.kind === 'timeout')
  assert.ok(frames[0].destroyed && frames.length === 2)
  host.dispose()
})

function box(x = 0): THREE.Mesh {
  const m = new THREE.Mesh(new THREE.BoxGeometry(1, 1, 1), new THREE.MeshStandardMaterial())
  m.position.x = x
  return m
}

function worldBox(o: THREE.Object3D): { min: number[]; max: number[] } {
  const b = new THREE.Box3().setFromObject(o)
  const r = (v: THREE.Vector3) => v.toArray().map((n) => Math.round(n * 1000) / 1000 + 0)
  return { min: r(b.min), max: r(b.max) }
}

test('lib@1', () => {
  const lib = createLib1()
  const stone = lib.material('stone', 'stone')
  assert.equal(lib.material('stone'), stone, 'one material per slot within a compile')
  assert.equal(stone.name, 'slot:stone')
  assert.equal(stone.userData.hint, 'stone')
  assert.equal(lib.material('bare').userData.hint, undefined)
  assert.notEqual(createLib1().material('stone'), stone, 'a new compile starts over')
  assert.throws(() => lib.material('bad name'), /lib\.material/)
  assert.ok(Object.isFrozen(lib))

  const union = lib.union(box(0), box(2))
  assert.deepEqual(union.position.toArray(), [0, 0, 0], 'results sit at the origin')
  assert.deepEqual(worldBox(union), { min: [-0.5, -0.5, -0.5], max: [2.5, 0.5, 0.5] })
  const shifted = box(0)
  const parent = new THREE.Group()
  parent.position.y = 3
  parent.add(shifted)
  const cutter = box(0.5)
  cutter.position.y = 3
  assert.deepEqual(worldBox(lib.subtract(shifted, cutter)), { min: [-0.5, 2.5, -0.5], max: [0, 3.5, 0.5] }, 'operands keep their world transforms')
  assert.deepEqual(worldBox(lib.subtract(shifted, box(0.5))), { min: [-0.5, 2.5, -0.5], max: [0.5, 3.5, 0.5] }, 'a cutter elsewhere cuts nothing')
  assert.deepEqual(worldBox(lib.intersect(box(0), box(0.5))), { min: [0, -0.5, -0.5], max: [0.5, 0.5, 0.5] })
  assert.throws(() => lib.union(box(0), new THREE.Group() as unknown as THREE.Mesh), /lib\.union/)

  const square: [number, number][] = [[0, 0], [1, 0], [1, 1], [0, 1]]
  const slab = new THREE.Mesh(lib.extrude(square, 0.2))
  assert.deepEqual(worldBox(slab), { min: [0, 0, 0], max: [1, 1, 0.2] })
  assert.deepEqual(worldBox(new THREE.Mesh(lib.extrude(square, 0.2, { bevel: 0.05 }))), { min: [-0.05, -0.05, -0.05], max: [1.05, 1.05, 0.25] })
  assert.throws(() => lib.extrude(square, 0), /depth/)
  const vase = new THREE.Mesh(lib.lathe([[0, 0], [0.5, 0], [0.3, 1]], 4))
  assert.equal((vase.geometry as THREE.LatheGeometry).parameters.segments, 4)
  assert.throws(() => lib.lathe([[0, 0]], 8), /lib\.lathe/)

  const row = lib.array(box(1), 3, [2, 0, 0])
  assert.deepEqual(row.children.map((c) => c.position.x), [1, 3, 5])
  assert.equal((row.children[0] as THREE.Mesh).geometry, (row.children[2] as THREE.Mesh).geometry, 'copies share geometry')
  assert.throws(() => lib.array(box(), 0, [1, 0, 0]), /count/)

  const ring = lib.radial(box(), 4, 2)
  const at = ring.children.map((c) => c.position.toArray().map((n) => Math.round(n * 1000) / 1000 + 0))
  assert.deepEqual(at, [[0, 0, 2], [2, 0, 0], [0, 0, -2], [-2, 0, 0]])
  const turned = new THREE.Euler().setFromQuaternion(ring.children[1].quaternion)
  assert.ok(Math.abs(turned.y - Math.PI / 2) < 1e-9, 'each copy turns with its angle')
  assert.deepEqual(lib.radial(box(), 2, 1, 'x').children[0].position.toArray(), [0, 1, 0])
  assert.throws(() => lib.radial(box(), 2, 1, 'w' as 'x'), /axis/)
})

const objectID = (n: number) => `sobj_${String(n).padStart(21, '0')}`
const modelInstance = (n: number, model: SceneObject['model']): SceneObject => ({
  id: objectID(n), kind: 'model_instance', name: '', parent_id: null, order: n,
  transform: { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }, visible: true, locked: false,
  model, material_slots: {},
})
const emptyScene: SceneState = {
  title: '场景',
  scene: {
    environment: { sky: { kind: 'procedural', sun_elevation_deg: 35, sun_azimuth_deg: 135, turbidity: 3 }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null },
    render: { mode: 'clay' },
    objects: [],
  },
}
const commitOp = { op: 'commit_model', ref: 'm', glb_upload_id: 'ccup_1', thumbnail_upload_id: 'ccup_2', generator: {}, manifest: {} } as unknown as Intent['ops'][number]
const draft = (id: string, ops: Intent['ops'], extra: Partial<Intent> = {}): Intent =>
  ({ id, createdAt: '2026-10-17T00:00:00Z', ops, expected: {}, state: 'pending', label: '编辑', ...extra })

test('model placeholders: format, projection and rewriting', () => {
  assert.equal(localRevision('i1', 'm'), 'local:i1:m')
  assert.equal(localContent('i1', 'm'), 'local-content:i1:m')
  assert.deepEqual(parsePlaceholder('local:9b2c-11:pillar_2'), { intent: '9b2c-11', ref: 'pillar_2', content: false })
  assert.deepEqual(parsePlaceholder('local-content:i1:m'), { intent: 'i1', ref: 'm', content: true })
  for (const id of ['ccrv_1', 'local:i1', 'local:i1:Bad', 'local::m', 'local:a:b:c']) assert.equal(parsePlaceholder(id), null, id)

  const add = { op: 'add_objects' as const, objects: [modelInstance(1, { ref: 'm' }), modelInstance(2, { content_revision_id: 'ccrv_x' })] }
  const set = { op: 'set_instance_model' as const, object_id: objectID(2), model: { ref: 'm' }, scope: 'this' as const }
  const resolved = withModelRevisions([commitOp, add, set], (ref) => `rev-${ref}`)
  assert.deepEqual((resolved[1] as typeof add).objects.map((o) => o.model), [{ content_revision_id: 'rev-m' }, { content_revision_id: 'ccrv_x' }])
  assert.deepEqual((resolved[2] as typeof set).model, { content_revision_id: 'rev-m' })
  assert.equal(resolved[0], commitOp)

  const a = draft('ia', [commitOp, add, set])
  const shown = applyIntent(emptyScene, a)
  assert.deepEqual(shown.scene.objects.map((o) => o.model?.content_revision_id), ['local:ia:m', 'local:ia:m'], 'an intent shows its own models by placeholder')
  assert.equal(waitsOnModels(a).size, 0, 'its own {ref}s are not waits')
  assert.ok(sendable(a))

  const copy = draft('ib', [{ op: 'add_objects', objects: [modelInstance(3, { content_revision_id: 'local:ia:m' })] }])
  const retune = draft('ic', [{ ...commitOp, base_content_id: 'local-content:ia:m' } as Intent['ops'][number]], {
    projection: { put: [modelInstance(4, { content_revision_id: 'local:ia:m' })], fields: { [objectID(5)]: { model: { content_revision_id: 'local:ia:m' } } }, removed: [] },
  })
  assert.deepEqual([...waitsOnModels(copy)], ['ia'])
  assert.deepEqual([...waitsOnModels(retune)], ['ia'], 'base_content_id and projections count')
  assert.ok(!sendable(copy) && !sendable(retune), 'a placeholder keeps an intent from going out')

  const models = { m: { content_id: 'ccnt_1', content_revision_id: 'ccrv_1' } }
  const fixed = resolveModelIDs(retune, 'ia', models)
  assert.equal((fixed.ops[0] as { base_content_id: string }).base_content_id, 'ccnt_1')
  assert.equal(fixed.projection!.put[0].model?.content_revision_id, 'ccrv_1')
  assert.equal(fixed.projection!.fields[objectID(5)].model?.content_revision_id, 'ccrv_1')
  assert.ok(sendable(fixed))
  assert.equal(resolveModelIDs(retune, 'other', models), retune, 'other receipts leave it alone')
  assert.deepEqual(mapModelIDs(copy, (id) => id.toUpperCase()).ops, [{ op: 'add_objects', objects: [modelInstance(3, { content_revision_id: 'LOCAL:IA:M' })] }])

  const confirmed = asConfirmed(a, models)
  assert.deepEqual(applyIntent(emptyScene, confirmed).scene.objects.map((o) => o.model?.content_revision_id), ['ccrv_1', 'ccrv_1'])
})

test('the model cache downloads, inspects and parses each revision once', async () => {
  const downloads: string[] = []
  let failNext = false
  const cache = new ModelCache(async (revision) => {
    downloads.push(revision)
    if (failNext) {
      failNext = false
      throw new Error('ticket expired')
    }
    return revision === 'ccrv_bad' ? fixture('bad-lines.glb') : templateGLB()
  })
  const [a, b] = await Promise.all([cache.load('ccrv_1'), cache.load('ccrv_1')])
  assert.equal(a, b)
  assert.deepEqual(downloads, ['ccrv_1'], 'one download for any number of instances')
  assert.equal(a.meshes.length, 2, 'base and capital share the trim slot and are drawn as one; the shaft is stone')
  // Each part's transform is baked into its geometry (model space).
  const top = a.meshes.map((m) => {
    m.geometry.computeBoundingBox()
    return m.geometry.boundingBox!.max.y
  })
  assert.ok(Math.max(...top) > 2.9375, `parts keep their place inside the model: tops ${top}`)

  await assert.rejects(cache.load('ccrv_bad'), (e: Error) => e instanceof GLBError && e.reason === 'unsupported_feature', 'server bytes are inspected too')
  assert.equal(cache.failed.getState()['ccrv_bad'], true)

  failNext = true
  await assert.rejects(cache.load('ccrv_2'))
  assert.equal(cache.failed.getState()['ccrv_2'], true)
  await cache.load('ccrv_2')
  assert.equal(cache.failed.getState()['ccrv_2'], undefined, 'a failed load is retried and clears its mark')

  const summary: SceneModel = { content_id: 'local-content:i1:m', usable: true, manifest: null, generator: null, name_hint: '柱' }
  cache.register('local:i1:m', { glb: templateGLB(), summary })
  const before = downloads.length
  const local = await cache.load('local:i1:m')
  assert.equal(downloads.length, before, 'a local bake needs no download')
  cache.alias('local:i1:m', 'ccrv_9', 'ccnt_9')
  assert.equal(await cache.load('ccrv_9'), local, 'the receipt\'s revision reuses the parsed model')
  assert.equal(cache.local.getState()['ccrv_9'].summary.content_id, 'ccnt_9')
  await assert.rejects(cache.load('local:gone:m'), /本机模型已不在/)

  const server: Record<string, SceneModel> = { ccrv_9: { ...summary, content_id: 'ccnt_9', name_hint: '服务端' } }
  const merged = modelSummaries(server, cache.local.getState())
  assert.equal(merged.ccrv_9.name_hint, '服务端', 'the server summary wins')
  assert.equal(merged['local:i1:m'].name_hint, '柱')
  cache.unregister('local:i1:m')
  assert.equal(cache.local.getState()['local:i1:m'], undefined)
})

test('the preview cache keeps the eight most recently opened models and frees the rest', async () => {
  const { PreviewModels } = await import('../src/creative-scene/preview/cache.ts')
  const downloads: string[] = []
  const models = new PreviewModels(8, async (revision) => {
    downloads.push(revision)
    return templateGLB()
  })
  const first = await models.load('ccrv_0')
  let freed = 0
  for (const m of first.meshes) m.geometry.addEventListener('dispose', () => freed++)
  for (let i = 1; i < 8; i++) await models.load(`ccrv_${i}`)
  assert.equal(await models.load('ccrv_0'), first, 'opening it again keeps it')
  await models.load('ccrv_8')
  assert.equal(freed, 0, 'the least recently opened is ccrv_1, not ccrv_0')
  await models.load('ccrv_9')
  assert.equal(await models.load('ccrv_0'), first)
  assert.equal(downloads.filter((r) => r === 'ccrv_0').length, 1)
  for (let i = 10; i < 18; i++) await models.load(`ccrv_${i}`)
  await new Promise((r) => setTimeout(r))
  assert.equal(freed, first.meshes.length, 'an evicted model frees its geometries')
  await models.load('ccrv_0')
  assert.equal(downloads.filter((r) => r === 'ccrv_0').length, 2, 'an evicted model is downloaded again')
})

/**
 * A code-built model's tree (scene-performance P1): parts of slot a that draw
 * alike (0, 2 and 6, one of them under a material object of its own), and
 * parts of slot a that must stay apart from them: another slot (1), no index
 * (3), other constants (4), vertex colours (5).
 */
function partsModel(): THREE.Group {
  const root = new THREE.Group()
  const material = (slot: string, params: THREE.MeshStandardMaterialParameters = {}) => new THREE.MeshStandardMaterial({ name: `slot:${slot}`, color: '#aa0000', ...params })
  const a = material('a')
  const add = (geometry: THREE.BufferGeometry, m: THREE.Material, at: [number, number, number], turn = 0) => {
    const mesh = new THREE.Mesh(geometry, m)
    mesh.position.set(...at)
    mesh.rotation.set(0.3 * turn, turn, 0)
    root.add(mesh)
  }
  const coloured = new THREE.BoxGeometry(1, 1, 1)
  coloured.setAttribute('color', new THREE.Float32BufferAttribute(new Array(24 * 3).fill(1), 3))
  add(new THREE.BoxGeometry(1, 2, 1), a, [-2, 0, 0])
  add(new THREE.SphereGeometry(0.5, 8, 6), material('b'), [0, 1, 0])
  add(new THREE.CylinderGeometry(0.2, 0.4, 1, 7), material('a'), [2, 0, 0], 0.7)
  add(new THREE.BoxGeometry(1, 1, 1).toNonIndexed(), a, [0, 0, 3])
  add(new THREE.BoxGeometry(1, 1, 1), material('a', { roughness: 0.3 }), [0, 0, -3])
  add(coloured, a, [3, 0, 3])
  add(new THREE.TorusGeometry(0.5, 0.1, 6, 12), a, [-3, 0, -3], 1.1)
  return root
}

/** The parts as they were before the merge: the e2e fixture's ?nomerge=1. */
function unmerged(root: THREE.Object3D): ModelMeshPart[] {
  const g = globalThis as { __sceneNoMerge?: boolean }
  g.__sceneNoMerge = true
  try {
    return partsOf(root).meshes
  } finally {
    delete g.__sceneNoMerge
  }
}

const bounds = (sphere: THREE.Sphere, box: THREE.Box3) => [...sphere.center.toArray(), sphere.radius, ...box.min.toArray(), ...box.max.toArray()]

test("a model's parts that draw alike are merged; each part stays a piece with the bounds three gave it", () => {
  const before = unmerged(partsModel())
  assert.equal(before.length, 7, 'the e2e switch draws every part on its own, as before')
  const { meshes } = partsOf(partsModel())
  assert.deepEqual(
    meshes.map((m) => [m.slot, m.constants.roughness, m.geometry.index !== null, 'color' in m.geometry.attributes]),
    [
      ['a', 1, true, false],
      ['b', 1, true, false],
      ['a', 1, false, false],
      ['a', 0.3, true, false],
      ['a', 1, true, true],
    ],
    'groups in the order each first appears',
  )
  const merged = meshes[0].geometry
  const members = [before[0], before[2], before[6]].map((m) => m.geometry)
  const pieces = piecesOf(merged)!
  assert.equal(pieces.length, 3)
  let at = 0
  let base = 0
  pieces.forEach((p, i) => {
    const g = members[i]
    assert.deepEqual([p.start, p.count], [at, g.index!.count])
    g.computeBoundingSphere()
    g.computeBoundingBox()
    assert.deepEqual(bounds(p.sphere, p.box), bounds(g.boundingSphere!, g.boundingBox!), 'the bounds three sorts and culls by, bit for bit')
    const triangles = Array.from(merged.index!.array.slice(p.start, p.start + p.count))
    assert.deepEqual(triangles, Array.from(g.index!.array, (v) => v + base), "a piece's triangles are its part's")
    at += p.count
    base += g.attributes.position.count
  })
  assert.equal(at, merged.index!.count, 'the pieces run end to end over the whole index')
  assert.deepEqual(Array.from(merged.attributes.position.array), members.flatMap((g) => Array.from(g.attributes.position.array)))
  for (const m of meshes.slice(1)) assert.equal(piecesOf(m.geometry), undefined, 'a part alone keeps its geometry')
  assert.equal(piecesOf(merged.clone()), undefined, 'a clone has no pieces: all of it is one')
})

test('a group three cannot merge after all stays as its parts', () => {
  const parts = unmerged(partsModel())
  const out = mergeParts(parts, () => null)
  assert.deepEqual(
    out.map((p) => p.geometry),
    [0, 2, 6, 1, 3, 4, 5].map((i) => parts[i].geometry),
  )
  for (const p of out) assert.equal(piecesOf(p.geometry), undefined)
})

test('range views share the merged buffers, draw their piece and are made once', () => {
  const { meshes } = partsOf(partsModel())
  const merged = meshes[0].geometry
  const pieces = piecesOf(merged)!
  const views = pieceViews(merged)
  assert.equal(pieceViews(merged), views, 'made once, kept with the geometry')
  assert.deepEqual(pieceViews(meshes[1].geometry), [meshes[1].geometry], 'a geometry without pieces is its own view')
  views.forEach((v, i) => {
    assert.equal(v.index, merged.index)
    for (const name of Object.keys(merged.attributes)) assert.equal(v.attributes[name], merged.attributes[name], `${name} shared`)
    assert.deepEqual([v.drawRange.start, v.drawRange.count], [pieces[i].start, pieces[i].count])
    assert.deepEqual(bounds(v.boundingSphere!, v.boundingBox!), bounds(pieces[i].sphere, pieces[i].box))
    assert.notEqual(v.boundingSphere, pieces[i].sphere, 'its own copy: three computes into what it finds there')
    assert.deepEqual(piecesOf(v), [pieces[i]])
  })
  let freed = 0
  for (const v of views) v.addEventListener('dispose', () => freed++)
  merged.addEventListener('dispose', () => assert.fail('the merged geometry goes with its model, not with its views'))
  disposePieceViews(merged)
  assert.equal(freed, views.length)
})

/** Boxes stacked face to face, the top one turned, a torus beside them and two floor tiles edge to edge: one slot. */
function stacked(): THREE.Group {
  const root = new THREE.Group()
  const a = new THREE.MeshStandardMaterial({ name: 'slot:a' })
  for (const [y, turn] of [
    [0.5, 0],
    [1.5, 0],
    [2.5, 0.4],
  ]) {
    const box = new THREE.Mesh(new THREE.BoxGeometry(1, 1, 1), a)
    box.position.y = y
    box.rotation.y = turn
    root.add(box)
  }
  const torus = new THREE.Mesh(new THREE.TorusGeometry(0.5, 0.15, 8, 16), a)
  torus.position.set(2, 0.5, 0)
  root.add(torus)
  for (const x of [3, 4]) {
    const tile = new THREE.Mesh(new THREE.PlaneGeometry(1, 1), a)
    tile.rotation.x = -Math.PI / 2
    tile.position.set(x, 0, 2)
    root.add(tile)
  }
  return root
}

const values = (g: THREE.BufferGeometry, name: string) => Array.from(g.attributes[name].array)

test('outline shells and wire edges are made per piece: the same as each part on its own', () => {
  const before = unmerged(stacked()).map((p) => p.geometry)
  const { meshes } = partsOf(stacked())
  assert.equal(meshes.length, 1)
  const merged = meshes[0].geometry

  const shell = shellOf(merged)
  const shells = before.map((g) => toCreasedNormals(g, Math.PI))
  for (const name of ['position', 'normal'])
    assert.deepEqual(
      values(shell, name),
      shells.flatMap((g) => values(g, name)),
      `shell ${name}: normals never averaged across two parts`,
    )
  let start = 0
  assert.deepEqual(
    piecesOf(shell)!.map((p) => [p.start, p.count, p.box]),
    shells.map((g, i) => {
      const at = [start, g.attributes.position.count, piecesOf(merged)![i].box]
      start += g.attributes.position.count
      return at
    }),
    "the shell's pieces: its own ranges, the parts' boxes",
  )

  const edges = edgesOf(merged)
  assert.equal(edgesOf(merged), edges, 'worked out once per geometry')
  const each = before.flatMap((g) => values(new THREE.EdgesGeometry(g, 30), 'position'))
  assert.deepEqual(values(edges, 'position'), each, 'edges where parts touch show as they did')
  assert.notDeepEqual(values(new THREE.EdgesGeometry(merged, 30), 'position'), each, 'across the whole geometry the seam between the tiles would go')
  assert.notDeepEqual(values(toCreasedNormals(merged, Math.PI), 'normal'), shells.flatMap((g) => values(g, 'normal')), 'and the shell would round over the stacked boxes')

  const view = pieceViews(merged)[2]
  assert.deepEqual(values(shellOf(view), 'position'), values(shells[2], 'position'), "a range view's shell is its piece alone")

  const loose = new THREE.BoxGeometry(1, 1, 1).toNonIndexed()
  const normal = loose.attributes.normal
  shellOf(loose)
  assert.equal(loose.attributes.normal, normal, 'a geometry without an index keeps its own normals')
})

test('framing a turned merged model bounds what its parts did, selection shells included', () => {
  const mount = (parts: ModelMeshPart[]) => {
    const group = new THREE.Group()
    group.quaternion.setFromEuler(new THREE.Euler(0.4, 0.9, 0.2))
    group.position.set(1, 0, -2)
    group.scale.set(1.5, 1, 2)
    for (const p of parts) {
      const mesh = new THREE.Mesh(p.geometry)
      const shell = new THREE.Mesh(shellOf(p.geometry))
      mesh.add(shell)
      group.add(mesh)
    }
    for (const g of pieceViews(parts[0].geometry)) group.add(new THREE.Mesh(g))
    return group
  }
  const box = (b: THREE.Box3) => [...b.min.toArray(), ...b.max.toArray()]
  const before = box(new THREE.Box3().expandByObject(mount(unmerged(partsModel()))))
  const merged = partsOf(partsModel()).meshes
  assert.deepEqual(box(expandByPieces(new THREE.Box3(), mount(merged))), before)
  assert.notDeepEqual(box(new THREE.Box3().expandByObject(mount(merged))), before, "three's own, with one box over the slot, bounds more")
})

test('the stress scenes come out byte for byte the same each time, at their sizes', async () => {
  const { equivalence, multiPart, primitives, scaleScene } = await import('./scene-stress.mjs')
  const a = await multiPart()
  assert.ok(a.models.parts.glb.equals((await multiPart()).models.parts.glb), 'the same arguments, the same bytes')
  assert.equal(a.models.parts.manifest.triangles, 1000 * 12)
  assert.deepEqual(a.models.parts.manifest.slots.map((s: { id: string }) => s.id), ['a', 'b'])
  assert.equal(a.objects.length, 4)
  const bytes = a.models.parts.glb
  assert.equal(partsOf(await parseGLB(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength))).meshes.length, 2, '1000 parts in 2 slots draw as 2')

  assert.equal(primitives().objects.length, 1000)

  const big = await scaleScene()
  const again = await scaleScene()
  for (const [name, m] of Object.entries(big.models)) assert.ok(m.glb.equals(again.models[name].glb), name)
  assert.deepEqual(big.objects, again.objects)
  const instances = big.objects.filter((o: { kind: string }) => o.kind === 'model_instance')
  assert.equal(Object.keys(big.models).length, 30)
  assert.equal(instances.length, 2000)
  assert.equal(big.objects.filter((o: { kind: string }) => o.kind === 'primitive').length, 300)
  const triangles = instances.reduce((n: number, o: { model: { name: string } }) => n + big.models[o.model.name].manifest.triangles, 0)
  assert.ok(triangles <= 5e6, `${triangles} triangles in the instances`)
  const recoloured = instances.filter((o: { material_slots: object }) => Object.keys(o.material_slots).length > 0).length
  assert.equal(recoloured, 860, '43% of the instances have their slots recoloured')
  assert.equal(big.objects.filter((o: { kind: string; light?: { cast_shadow: boolean } }) => o.kind === 'light' && o.light?.cast_shadow).length, 3, 'with the sun, four lights cast shadows')

  const eq = await equivalence()
  for (const name of ['glass', 'stone', 'wall', 'stacked']) assert.ok(eq.models[name].glb.equals((await equivalence()).models[name].glb), name)
  assert.equal(eq.objects.filter((o: { kind: string }) => o.kind === 'camera').length, 2, 'a camera on each set')
})
