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
