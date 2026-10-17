import type { components } from '../../api/schema'

// The editor's copy of backend/internal/creativeglb (Inspect): the same static
// GLB subset, checked in the same order, with the manifest derived from vertex
// data. The sandbox's output is untrusted, so nothing reaches GLTFLoader until
// this passes. Both sides run the fixtures in creativeglb/testdata; keep the
// two in step function by function.

export type Manifest = components['schemas']['CreativeModelManifest']

export type GLBReason =
  | 'container'
  | 'json'
  | 'name'
  | 'buffer'
  | 'scene_graph'
  | 'accessor'
  | 'index_range'
  | 'bounds_mismatch'
  | 'unsupported_feature'
  | 'limit'

export class GLBError extends Error {
  readonly reason: GLBReason
  readonly detail: string
  constructor(reason: GLBReason, detail: string) {
    super(`creativeglb: ${reason}: ${detail}`)
    this.reason = reason
    this.detail = detail
  }
}

export type Limits = {
  maxBytes: number
  maxTriangles: number
  maxNodes: number
  maxPrimitiveInstances: number
  maxSlots: number
  maxParts: number
  maxAnchors: number
}

export const defaultLimits: Readonly<Limits> = Object.freeze({
  maxBytes: 16 * 1024 * 1024,
  maxTriangles: 300000,
  maxNodes: 10000,
  maxPrimitiveInstances: 10000,
  maxSlots: 32,
  maxParts: 200,
  maxAnchors: 200,
})

const glbMagic = 0x46546c67
const chunkJSON = 0x4e4f534a
const chunkBIN = 0x004e4942
const compFloat = 5126
const compUByte = 5121
const compUShort = 5123
const compUInt = 5125
const modeTriangle = 4
const maxHintRunes = 64
const maxInt32 = 2147483647
const emissiveStrength = 'KHR_materials_emissive_strength'

const namePattern = /^[A-Za-z0-9_-]{1,64}$/
const componentSizes = new Map([[5120, 1], [compUByte, 1], [5122, 2], [compUShort, 2], [compUInt, 4], [compFloat, 4]])
const typeComponents = new Map([['SCALAR', 1], ['VEC2', 2], ['VEC3', 3], ['VEC4', 4]])
const topUnsupported = new Map([
  ['images', 'texture'], ['textures', 'texture'], ['samplers', 'texture'],
  ['skins', 'skin'], ['animations', 'animation'], ['cameras', 'camera'],
])
const nodeUnsupported = new Map([['camera', 'camera'], ['skin', 'skin'], ['weights', 'morph target']])
const none = new Map<string, string>()

function fail(reason: GLBReason, detail: string): never {
  throw new GLBError(reason, detail)
}

type Obj = Record<string, unknown>

function isObj(v: unknown): v is Obj {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

function asObject(v: unknown, what: string): Obj {
  if (!isObj(v)) fail('json', `${what} is not an object`)
  return v
}

function asObjects(v: unknown, what: string): Obj[] {
  if (!Array.isArray(v)) fail('json', `${what} is not an array`)
  return v.map((item, i) => asObject(item, `${what}[${i}]`))
}

function has(o: Obj, k: string): boolean {
  return Object.hasOwn(o, k)
}

// Members are checked in sorted order so the first failure, and so the
// reason, is the same as on the server.
function checkMembers(o: Obj, what: string, allowed: readonly string[], extensions: readonly string[], unsupported: Map<string, string>): void {
  for (const k of Object.keys(o).sort()) {
    if (k === 'extras' || allowed.includes(k)) continue
    if (k === 'extensions') {
      const ext = asObject(o[k], `${what}.extensions`)
      for (const name of Object.keys(ext).sort()) {
        if (!extensions.includes(name)) fail('unsupported_feature', `extension ${name} on ${what}`)
      }
      continue
    }
    const feature = unsupported.get(k)
    if (feature) fail('unsupported_feature', `${feature} (${what}.${k})`)
    fail('json', `unknown member ${what}.${k}`)
  }
}

function finiteValue(v: unknown, what: string): number {
  if (typeof v !== 'number' || !Number.isFinite(v)) fail('json', `${what} is not a finite number`)
  return v
}

function numberOf(o: Obj, k: string, what: string): number | undefined {
  return has(o, k) ? finiteValue(o[k], `${what}.${k}`) : undefined
}

// A non-negative integral number; 1.0 and 1e3 count, as in the browser.
function integerOf(o: Obj, k: string, what: string): number | undefined {
  const f = numberOf(o, k, what)
  if (f === undefined) return undefined
  if (f < 0 || !Number.isInteger(f) || f > maxInt32) fail('json', `${what}.${k} is not a non-negative integer`)
  return f
}

function indexOf(o: Obj, k: string, what: string, n: number): number {
  const i = integerOf(o, k, what)
  if (i === undefined) return -1
  if (i >= n) fail('json', `${what}.${k} = ${i} out of range (${n})`)
  return i
}

function numbersOf(o: Obj, k: string, what: string, n: number): number[] | undefined {
  if (!has(o, k)) return undefined
  const v = o[k]
  if (!Array.isArray(v) || v.length !== n) fail('json', `${what}.${k} is not an array of ${n} numbers`)
  return v.map((item, i) => finiteValue(item, `${what}.${k}[${i}]`))
}

function indicesOf(o: Obj, k: string, what: string, n: number): number[] {
  if (!has(o, k)) return []
  const v = o[k]
  if (!Array.isArray(v)) fail('json', `${what}.${k} is not an array`)
  return v.map((item, i) => indexOf({ v: item }, 'v', `${what}.${k}[${i}]`, n))
}

function strOf(o: Obj, k: string, what: string): string | undefined {
  if (!has(o, k)) return undefined
  const v = o[k]
  if (typeof v !== 'string') fail('json', `${what}.${k} is not a string`)
  return v
}

function boolOf(o: Obj, k: string, what: string): boolean {
  if (!has(o, k)) return false
  const v = o[k]
  if (typeof v !== 'boolean') fail('json', `${what}.${k} is not a boolean`)
  return v
}

function arrayOf(o: Obj, k: string, what: string): Obj[] | undefined {
  return has(o, k) ? asObjects(o[k], what) : undefined
}

type BufferView = { offset: number; length: number; stride: number }
type Accessor = {
  view: number
  offset: number
  component: number
  count: number
  comps: number
  normalized: boolean
  min?: number[]
  max?: number[]
}
type Material = { slot: string; hint: string; isSlot: boolean }
type Primitive = { position: number; indices: number; material: number }
type Node = { name: string; children: number[]; mesh: number; local: number[] }
type Doc = {
  bin: DataView | null
  views: BufferView[]
  accessors: Accessor[]
  materials: Material[]
  meshes: Primitive[][]
  nodes: Node[]
  roots: number[]
}

function splitContainer(glb: Uint8Array): { json: Uint8Array; bin: Uint8Array | null } {
  if (glb.byteLength < 20) fail('container', 'file shorter than a GLB header')
  const view = new DataView(glb.buffer, glb.byteOffset, glb.byteLength)
  const u32 = (at: number) => view.getUint32(at, true)
  if (u32(0) !== glbMagic) fail('container', 'magic is not glTF')
  if (u32(4) !== 2) fail('container', `container version ${u32(4)}`)
  if (u32(8) !== glb.byteLength) fail('container', `declared length ${u32(8)}, actual ${glb.byteLength}`)
  const jsonLen = u32(12)
  if (u32(16) !== chunkJSON) fail('container', 'first chunk is not JSON')
  const jsonEnd = 20 + jsonLen
  if (jsonLen === 0 || jsonLen % 4 !== 0 || jsonEnd > glb.byteLength) fail('container', `JSON chunk length ${jsonLen}`)
  const json = glb.subarray(20, jsonEnd)
  if (jsonEnd === glb.byteLength) return { json, bin: null }
  if (jsonEnd + 8 > glb.byteLength) fail('container', 'truncated chunk header')
  const binLen = u32(jsonEnd)
  if (u32(jsonEnd + 4) !== chunkBIN) fail('container', 'second chunk is not BIN')
  const binStart = jsonEnd + 8
  if (binLen % 4 !== 0 || binStart + binLen !== glb.byteLength) {
    fail('container', `BIN chunk length ${binLen} does not end the file`)
  }
  return { json, bin: glb.subarray(binStart) }
}

function parseDocument(jsonChunk: Uint8Array, bin: Uint8Array | null, limits: Limits): Doc {
  if (jsonChunk[0] === 0xef && jsonChunk[1] === 0xbb && jsonChunk[2] === 0xbf) {
    fail('json', 'JSON chunk starts with a byte order mark')
  }
  let text: string
  try {
    text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(jsonChunk)
  } catch {
    return fail('json', 'JSON chunk is not UTF-8')
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return fail('json', 'gltf is not an object')
  }
  const top = asObject(parsed, 'gltf')
  const allowed = ['asset', 'scene', 'scenes', 'nodes', 'meshes', 'materials', 'accessors', 'bufferViews', 'buffers', 'extensionsUsed', 'extensionsRequired']
  checkMembers(top, 'gltf', allowed, [], topUnsupported)
  checkAsset(top)
  checkExtensionLists(top)
  const bufferLen = parseBuffers(top, bin)
  const views = parseViews(top, bufferLen)
  const accessors = parseAccessors(top, views)
  const materials = parseMaterials(top)
  const meshes = parseMeshes(top, views, accessors, materials)
  const nodes = parseNodes(top, meshes.length, limits)
  const roots = parseScene(top, nodes.length)
  const doc: Doc = {
    bin: bin ? new DataView(bin.buffer, bin.byteOffset, bin.byteLength) : null,
    views,
    accessors,
    materials,
    meshes,
    nodes,
    roots,
  }
  checkGraph(doc)
  return doc
}

function checkAsset(top: Obj): void {
  if (!has(top, 'asset')) fail('json', 'missing asset')
  const asset = asObject(top.asset, 'asset')
  checkMembers(asset, 'asset', ['version', 'generator', 'copyright', 'minVersion'], [], none)
  if (asset.version !== '2.0') fail('json', 'asset.version must be 2.0')
  for (const k of ['generator', 'copyright', 'minVersion']) strOf(asset, k, 'asset')
}

function checkExtensionLists(top: Obj): void {
  for (const k of ['extensionsUsed', 'extensionsRequired']) {
    if (!has(top, k)) continue
    const v = top[k]
    // A null entry reads as "" on the server (Go string zero value).
    if (!Array.isArray(v) || !v.every((n) => n === null || typeof n === 'string')) {
      fail('json', `${k} is not an array of strings`)
    }
    for (const name of v as (string | null)[]) {
      if (k === 'extensionsRequired' || name !== emissiveStrength) {
        fail('unsupported_feature', `extension ${name ?? ''} (${k})`)
      }
    }
  }
}

// The usable buffer length, or -1 without a buffer. The only buffer is the
// BIN chunk; any uri is rejected.
function parseBuffers(top: Obj, bin: Uint8Array | null): number {
  const buffers = arrayOf(top, 'buffers', 'buffers')
  if (!buffers || buffers.length === 0) {
    if (bin !== null) fail('buffer', 'BIN chunk without a buffer')
    return -1
  }
  if (buffers.length > 1) fail('buffer', `${buffers.length} buffers`)
  const b = buffers[0]
  checkMembers(b, 'buffers[0]', ['byteLength', 'uri', 'name'], [], none)
  if (has(b, 'uri')) fail('buffer', 'buffer has a uri')
  const n = integerOf(b, 'byteLength', 'buffers[0]')
  if (n === undefined || n < 1) fail('buffer', 'buffer byteLength missing')
  if (bin === null || n > bin.byteLength || bin.byteLength - n > 3) {
    fail('buffer', `buffer byteLength ${n} does not match BIN chunk`)
  }
  strOf(b, 'name', 'buffers[0]')
  return n
}

function parseViews(top: Obj, bufferLen: number): BufferView[] {
  return (arrayOf(top, 'bufferViews', 'bufferViews') ?? []).map((v, i) => {
    const what = `bufferViews[${i}]`
    checkMembers(v, what, ['buffer', 'byteOffset', 'byteLength', 'byteStride', 'target', 'name'], [], none)
    const buf = integerOf(v, 'buffer', what)
    if (buf === undefined || buf !== 0 || bufferLen < 0) fail('buffer', `${what} does not point at the BIN buffer`)
    const offset = integerOf(v, 'byteOffset', what) ?? 0
    const length = integerOf(v, 'byteLength', what)
    if (length === undefined || length < 1 || offset + length > bufferLen) fail('buffer', `${what} lies outside the buffer`)
    const stride = integerOf(v, 'byteStride', what)
    if (stride !== undefined && (stride < 4 || stride > 252 || stride % 4 !== 0)) {
      fail('accessor', `${what}.byteStride ${stride}`)
    }
    const target = integerOf(v, 'target', what)
    if (target !== undefined && target !== 34962 && target !== 34963) fail('buffer', `${what}.target ${target}`)
    strOf(v, 'name', what)
    return { offset, length, stride: stride ?? 0 }
  })
}

function parseAccessors(top: Obj, views: BufferView[]): Accessor[] {
  const allowed = ['bufferView', 'byteOffset', 'componentType', 'normalized', 'count', 'type', 'max', 'min', 'name']
  return (arrayOf(top, 'accessors', 'accessors') ?? []).map((a, i) => {
    const what = `accessors[${i}]`
    checkMembers(a, what, allowed, [], new Map([['sparse', 'sparse accessor']]))
    const view = indexOf(a, 'bufferView', what, views.length)
    if (view < 0) fail('accessor', `${what} has no bufferView`)
    const component = integerOf(a, 'componentType', what) ?? 0
    const size = componentSizes.get(component) ?? 0
    if (size === 0) fail('accessor', `${what}.componentType ${component}`)
    const typ = strOf(a, 'type', what) ?? ''
    const comps = typeComponents.get(typ) ?? 0
    if (comps === 0) fail('accessor', `${what}.type ${JSON.stringify(typ)}`)
    const normalized = boolOf(a, 'normalized', what)
    if (normalized && component === compFloat) fail('accessor', `${what} normalizes floats`)
    const count = integerOf(a, 'count', what)
    const offset = integerOf(a, 'byteOffset', what) ?? 0
    const v = views[view]
    const elem = size * comps
    const stride = v.stride === 0 ? elem : v.stride
    if (count === undefined || count < 1 || count > v.length) fail('accessor', `${what}.count`)
    if (offset % size !== 0 || (v.offset + offset) % size !== 0) fail('accessor', `${what} is not aligned`)
    if (stride < elem) fail('accessor', `${what} stride ${stride} below element size ${elem}`)
    if (offset + stride * (count - 1) + elem > v.length) fail('accessor', `${what} exceeds its bufferView`)
    const min = numbersOf(a, 'min', what, comps)
    const max = numbersOf(a, 'max', what, comps)
    strOf(a, 'name', what)
    return { view, offset, component, count, comps, normalized, min, max }
  })
}

function parseMaterials(top: Obj): Material[] {
  const textures = new Map([['normalTexture', 'texture'], ['occlusionTexture', 'texture'], ['emissiveTexture', 'texture']])
  const allowed = ['name', 'pbrMetallicRoughness', 'emissiveFactor', 'alphaMode', 'alphaCutoff', 'doubleSided']
  return (arrayOf(top, 'materials', 'materials') ?? []).map((m, i) => {
    const what = `materials[${i}]`
    checkMembers(m, what, allowed, [emissiveStrength], textures)
    checkMaterialValues(m, what)
    const name = strOf(m, 'name', what) ?? ''
    if (!name.startsWith('slot:')) return { slot: '', hint: '', isSlot: false }
    const id = name.slice('slot:'.length)
    if (!namePattern.test(id)) fail('name', `material name ${JSON.stringify(name)} is not slot:<id>`)
    let hint = ''
    if (has(m, 'extras')) {
      // extras carries three.js userData; only a string hint is read.
      const extras = asObject(m.extras, `${what}.extras`)
      const h = strOf(extras, 'hint', `${what}.extras`)
      if (h !== undefined && [...h].length > maxHintRunes) fail('name', `${what} hint longer than ${maxHintRunes} characters`)
      hint = h ?? ''
    }
    return { slot: id, hint, isSlot: true }
  })
}

function checkMaterialValues(m: Obj, what: string): void {
  if (has(m, 'pbrMetallicRoughness')) {
    const pwhat = `${what}.pbrMetallicRoughness`
    const pbr = asObject(m.pbrMetallicRoughness, pwhat)
    const textures = new Map([['baseColorTexture', 'texture'], ['metallicRoughnessTexture', 'texture']])
    checkMembers(pbr, pwhat, ['baseColorFactor', 'metallicFactor', 'roughnessFactor'], [], textures)
    numbersOf(pbr, 'baseColorFactor', pwhat, 4)
    for (const k of ['metallicFactor', 'roughnessFactor']) numberOf(pbr, k, pwhat)
  }
  numbersOf(m, 'emissiveFactor', what, 3)
  numberOf(m, 'alphaCutoff', what)
  boolOf(m, 'doubleSided', what)
  const mode = strOf(m, 'alphaMode', what)
  if (mode !== undefined && mode !== 'OPAQUE' && mode !== 'MASK' && mode !== 'BLEND') {
    fail('json', `${what}.alphaMode ${JSON.stringify(mode)}`)
  }
  if (has(m, 'extensions')) {
    const ext = asObject(m.extensions, `${what}.extensions`)
    const ewhat = `${what}.${emissiveStrength}`
    const strength = asObject(has(ext, emissiveStrength) ? ext[emissiveStrength] : undefined, ewhat)
    checkMembers(strength, ewhat, ['emissiveStrength'], [], none)
    const v = numberOf(strength, 'emissiveStrength', ewhat)
    if (v !== undefined && v < 0) fail('json', `${ewhat}.emissiveStrength is negative`)
  }
}

function parseMeshes(top: Obj, views: BufferView[], accessors: Accessor[], materials: Material[]): Primitive[][] {
  return (arrayOf(top, 'meshes', 'meshes') ?? []).map((m, i) => {
    const what = `meshes[${i}]`
    checkMembers(m, what, ['primitives', 'name'], [], new Map([['weights', 'morph target']]))
    strOf(m, 'name', what)
    const prims = arrayOf(m, 'primitives', `${what}.primitives`)
    if (!prims || prims.length === 0) fail('json', `${what} has no primitives`)
    return prims.map((p, j) => parsePrimitive(p, `${what}.primitives[${j}]`, views, accessors, materials))
  })
}

// Accepted vertex attributes: component types (normalized integers only for
// COLOR_0) and component counts.
const attributeFormats = new Map<string, { components: number[]; counts: number[] }>([
  ['POSITION', { components: [compFloat], counts: [3] }],
  ['NORMAL', { components: [compFloat], counts: [3] }],
  ['TANGENT', { components: [compFloat], counts: [4] }],
  ['TEXCOORD_0', { components: [compFloat], counts: [2] }],
  ['TEXCOORD_1', { components: [compFloat], counts: [2] }],
  ['COLOR_0', { components: [compFloat, compUByte, compUShort], counts: [3, 4] }],
])

function parsePrimitive(p: Obj, what: string, views: BufferView[], accessors: Accessor[], materials: Material[]): Primitive {
  checkMembers(p, what, ['attributes', 'indices', 'material', 'mode'], [], new Map([['targets', 'morph target']]))
  const mode = integerOf(p, 'mode', what)
  if (mode !== undefined && mode !== modeTriangle) fail('unsupported_feature', `primitive mode ${mode} (${what})`)
  if (!has(p, 'attributes')) fail('json', `${what} has no attributes`)
  const attrs = asObject(p.attributes, `${what}.attributes`)
  let position = -1
  let count = -1
  for (const name of Object.keys(attrs).sort()) {
    const format = attributeFormats.get(name)
    if (!format) {
      if (name.startsWith('JOINTS_') || name.startsWith('WEIGHTS_')) fail('unsupported_feature', `skin attribute ${name} (${what})`)
      fail('unsupported_feature', `attribute ${name} (${what})`)
    }
    const idx = indexOf(attrs, name, `${what}.attributes`, accessors.length)
    const a = accessors[idx]
    const okFormat = format.components.includes(a.component) && format.counts.includes(a.comps)
    if (!okFormat || (a.component !== compFloat && !a.normalized)) {
      fail('accessor', `${what} attribute ${name} has an unsupported format`)
    }
    if (count >= 0 && a.count !== count) fail('accessor', `${what} attributes differ in count`)
    count = a.count
    if (name === 'POSITION') position = idx
  }
  if (position < 0) fail('accessor', `${what} has no POSITION`)
  const indices = indexOf(p, 'indices', what, accessors.length)
  if (indices >= 0) {
    const a = accessors[indices]
    if (a.comps !== 1 || a.normalized || (a.component !== compUByte && a.component !== compUShort && a.component !== compUInt)) {
      fail('accessor', `${what} indices are not unsigned integer scalars`)
    }
    if (views[a.view].stride !== 0) fail('accessor', `${what} index bufferView has a stride`)
  }
  const material = indexOf(p, 'material', what, materials.length)
  return { position, indices, material }
}

function parseNodes(top: Obj, meshes: number, limits: Limits): Node[] {
  const items = arrayOf(top, 'nodes', 'nodes') ?? []
  if (items.length > limits.maxNodes) fail('limit', `${items.length} nodes, limit ${limits.maxNodes}`)
  const allowed = ['name', 'children', 'mesh', 'matrix', 'translation', 'rotation', 'scale']
  return items.map((n, i) => {
    const what = `nodes[${i}]`
    checkMembers(n, what, allowed, [], nodeUnsupported)
    const name = strOf(n, 'name', what) ?? ''
    const children = indicesOf(n, 'children', what, items.length)
    const mesh = indexOf(n, 'mesh', what, meshes)
    const local = localMatrix(n, what)
    return { name, children, mesh, local }
  })
}

const norm = (v: number[]) => Math.sqrt(v[0] * v[0] + v[1] * v[1] + v[2] * v[2])

// The column-major local transform. TRS composes as three.js Matrix4.compose;
// a matrix must decompose (no shear), since three.js decomposes it.
function localMatrix(n: Obj, what: string): number[] {
  const m = numbersOf(n, 'matrix', what, 16)
  const t = numbersOf(n, 'translation', what, 3)
  const r = numbersOf(n, 'rotation', what, 4)
  const s = numbersOf(n, 'scale', what, 3)
  if (m) {
    if (t || r || s) fail('scene_graph', `${what} has both matrix and TRS`)
    if (m[3] !== 0 || m[7] !== 0 || m[11] !== 0 || m[15] !== 1) fail('scene_graph', `${what} matrix is not affine`)
    const cols = [m.slice(0, 3), m.slice(4, 7), m.slice(8, 11)]
    for (const [i, j] of [[0, 1], [0, 2], [1, 2]]) {
      const a = cols[i]
      const b = cols[j]
      const dot = a[0] * b[0] + a[1] * b[1] + a[2] * b[2]
      if (Math.abs(dot) > 1e-4 * norm(a) * norm(b)) fail('scene_graph', `${what} matrix has shear`)
    }
    return m
  }
  return compose(t ?? [0, 0, 0], r ?? [0, 0, 0, 1], s ?? [1, 1, 1])
}

function compose(t: number[], q: number[], s: number[]): number[] {
  const [x, y, z, w] = q
  const x2 = x + x
  const y2 = y + y
  const z2 = z + z
  const xx = x * x2
  const xy = x * y2
  const xz = x * z2
  const yy = y * y2
  const yz = y * z2
  const zz = z * z2
  const wx = w * x2
  const wy = w * y2
  const wz = w * z2
  return [
    (1 - (yy + zz)) * s[0], (xy + wz) * s[0], (xz - wy) * s[0], 0,
    (xy - wz) * s[1], (1 - (xx + zz)) * s[1], (yz + wx) * s[1], 0,
    (xz + wy) * s[2], (yz - wx) * s[2], (1 - (xx + yy)) * s[2], 0,
    t[0], t[1], t[2], 1,
  ]
}

function multiply(a: number[], b: number[]): number[] {
  const out = new Array<number>(16)
  for (let col = 0; col < 4; col++) {
    for (let row = 0; row < 4; row++) {
      out[col * 4 + row] = a[row] * b[col * 4] + a[4 + row] * b[col * 4 + 1] + a[8 + row] * b[col * 4 + 2] + a[12 + row] * b[col * 4 + 3]
    }
  }
  return out
}

function parseScene(top: Obj, nodes: number): number[] {
  const scenes = arrayOf(top, 'scenes', 'scenes') ?? []
  if (scenes.length !== 1) fail('scene_graph', `${scenes.length} scenes, exactly one required`)
  const s = integerOf(top, 'scene', 'gltf')
  if (s !== undefined && s !== 0) fail('scene_graph', `default scene ${s}`)
  checkMembers(scenes[0], 'scenes[0]', ['nodes', 'name'], [], none)
  strOf(scenes[0], 'name', 'scenes[0]')
  return indicesOf(scenes[0], 'nodes', 'scenes[0]', nodes)
}

// A forest: at most one parent per node, roots have none, no cycle anywhere.
function checkGraph(d: Doc): void {
  const parents = new Array<number>(d.nodes.length).fill(-1)
  d.nodes.forEach((n, i) => {
    for (const c of n.children) {
      if (parents[c] >= 0) fail('scene_graph', `nodes[${c}] has more than one parent`)
      parents[c] = i
    }
  })
  const seen = new Array<boolean>(d.nodes.length).fill(false)
  for (const r of d.roots) {
    if (parents[r] >= 0 || seen[r]) fail('scene_graph', `scene root nodes[${r}] is not a root`)
    seen[r] = true
  }
  const state = new Uint8Array(d.nodes.length) // 0 new, 1 on the current chain, 2 done
  for (let i = 0; i < d.nodes.length; i++) {
    const chain: number[] = []
    for (let j = i; j >= 0 && state[j] !== 2; j = parents[j]) {
      if (state[j] === 1) fail('scene_graph', `nodes[${j}] is part of a cycle`)
      state[j] = 1
      chain.push(j)
    }
    for (const j of chain) state[j] = 2
  }
}

type LocalBounds = { min: number[]; max: number[] }

function elementLayout(d: Doc, a: Accessor): { base: number; stride: number } {
  const v = d.views[a.view]
  const stride = v.stride === 0 ? (componentSizes.get(a.component) ?? 0) * a.comps : v.stride
  return { base: v.offset + a.offset, stride }
}

// Reads every POSITION value; the declared min and max must equal the
// computed range exactly (both are float32 values).
function positionBounds(d: Doc, idx: number, cache: Map<number, LocalBounds>): LocalBounds {
  const cached = cache.get(idx)
  if (cached) return cached
  const a = d.accessors[idx]
  const lb: LocalBounds = { min: [Infinity, Infinity, Infinity], max: [-Infinity, -Infinity, -Infinity] }
  const { base, stride } = elementLayout(d, a)
  const bin = d.bin as DataView
  for (let i = 0; i < a.count; i++) {
    const at = base + i * stride
    for (let c = 0; c < 3; c++) {
      const v = bin.getFloat32(at + 4 * c, true)
      if (!Number.isFinite(v)) fail('accessor', `accessors[${idx}] has a non-finite position`)
      lb.min[c] = Math.min(lb.min[c], v)
      lb.max[c] = Math.max(lb.max[c], v)
    }
  }
  if (!a.min || !a.max) fail('accessor', `accessors[${idx}] POSITION has no min/max`)
  for (let c = 0; c < 3; c++) {
    if (Math.fround(a.min[c]) !== lb.min[c] || Math.fround(a.max[c]) !== lb.max[c]) {
      fail('bounds_mismatch', `accessors[${idx}] declares [${a.min[c]}, ${a.max[c]}] but positions span [${lb.min[c]}, ${lb.max[c]}] on axis ${c}`)
    }
  }
  cache.set(idx, lb)
  return lb
}

// Counts one primitive and checks its index range.
function triangles(d: Doc, p: Primitive, cache: Map<number, number>): number {
  const vertices = d.accessors[p.position].count
  if (p.indices < 0) {
    if (vertices % 3 !== 0) fail('index_range', `${vertices} vertices is not a triangle list`)
    return vertices / 3
  }
  const a = d.accessors[p.indices]
  if (a.count % 3 !== 0) fail('index_range', `${a.count} indices is not a triangle list`)
  let highest = cache.get(p.indices)
  if (highest === undefined) {
    highest = 0
    const { base, stride } = elementLayout(d, a)
    const bin = d.bin as DataView
    for (let i = 0; i < a.count; i++) {
      const at = base + i * stride
      const v = a.component === compUByte ? bin.getUint8(at) : a.component === compUShort ? bin.getUint16(at, true) : bin.getUint32(at, true)
      highest = Math.max(highest, v)
    }
    cache.set(p.indices, highest)
  }
  if (highest >= vertices) fail('index_range', `index ${highest} with ${vertices} vertices`)
  return a.count / 3
}

// Merges the 8 transformed corners of a local box, matching three.js
// Box3.applyMatrix4 (Box3.setFromObject without precise).
function extend(b: { min: number[]; max: number[] }, lb: LocalBounds, m: number[]): void {
  for (let corner = 0; corner < 8; corner++) {
    const p = [0, 1, 2].map((c) => (corner & (1 << c) ? lb.max[c] : lb.min[c]))
    const w = [
      m[0] * p[0] + m[4] * p[1] + m[8] * p[2] + m[12],
      m[1] * p[0] + m[5] * p[1] + m[9] * p[2] + m[13],
      m[2] * p[0] + m[6] * p[1] + m[10] * p[2] + m[14],
    ]
    for (let c = 0; c < 3; c++) {
      b.min[c] = Math.min(b.min[c], w[c])
      b.max[c] = Math.max(b.max[c], w[c])
    }
  }
}

// Walks the scene depth-first (roots, then children, in array order) and
// derives everything from vertex data.
function manifestOf(d: Doc, limits: Limits): Manifest {
  const positions = new Map<number, LocalBounds>()
  const maxIndex = new Map<number, number>()
  let instances = 0
  let count = 0
  const bounds = { min: [Infinity, Infinity, Infinity], max: [-Infinity, -Infinity, -Infinity] }
  const slots = new Set<string>()
  const parts = new Set<string>()
  const anchorIDs = new Set<string>()
  const manifest: Manifest = { bounds, triangles: 0, slots: [], parts: [], anchors: [] }

  const identity = compose([0, 0, 0], [0, 0, 0, 1], [1, 1, 1])
  const stack: { node: number; world: number[] }[] = []
  for (let i = d.roots.length - 1; i >= 0; i--) stack.push({ node: d.roots[i], world: identity })
  for (let f = stack.pop(); f; f = stack.pop()) {
    const n = d.nodes[f.node]
    const world = multiply(f.world, n.local)
    collectName(n.name, world, manifest, parts, anchorIDs)
    if (n.mesh >= 0) {
      for (const p of d.meshes[n.mesh]) {
        instances++
        if (instances > limits.maxPrimitiveInstances) fail('limit', `more than ${limits.maxPrimitiveInstances} primitive instances`)
        const lb = positionBounds(d, p.position, positions)
        count += triangles(d, p, maxIndex)
        if (count > limits.maxTriangles) fail('limit', `more than ${limits.maxTriangles} triangles`)
        extend(bounds, lb, world)
        const m = p.material >= 0 ? d.materials[p.material] : null
        if (m?.isSlot && !slots.has(m.slot)) {
          slots.add(m.slot)
          manifest.slots.push(m.hint ? { id: m.slot, hint: m.hint } : { id: m.slot })
        }
      }
    }
    for (let i = n.children.length - 1; i >= 0; i--) stack.push({ node: n.children[i], world })
  }
  if (count === 0) fail('scene_graph', 'model has no triangles')
  if (manifest.slots.length > limits.maxSlots) fail('limit', `${manifest.slots.length} material slots, limit ${limits.maxSlots}`)
  if (manifest.parts.length > limits.maxParts) fail('limit', `${manifest.parts.length} parts, limit ${limits.maxParts}`)
  if (manifest.anchors.length > limits.maxAnchors) fail('limit', `${manifest.anchors.length} anchors, limit ${limits.maxAnchors}`)
  manifest.triangles = count
  return normalize(manifest)
}

// Parts are a set; an anchor keeps its first occurrence in traversal order.
function collectName(name: string, world: number[], m: Manifest, parts: Set<string>, anchors: Set<string>): void {
  if (name.startsWith('part:')) {
    const id = name.slice('part:'.length)
    if (!namePattern.test(id)) fail('name', `node name ${JSON.stringify(name)} is not part:<id>`)
    if (!parts.has(id)) {
      parts.add(id)
      m.parts.push(id)
    }
  }
  if (name.startsWith('anchor:')) {
    const id = name.slice('anchor:'.length)
    if (!namePattern.test(id)) fail('name', `node name ${JSON.stringify(name)} is not anchor:<id>`)
    if (!anchors.has(id)) {
      anchors.add(id)
      m.anchors.push({ id, position: [world[12], world[13], world[14]] })
    }
  }
}

const byID = (a: { id: string }, b: { id: string }) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)

/** Sorts slots, parts and anchors by id, in place. */
export function normalize(m: Manifest): Manifest {
  m.slots.sort(byID)
  m.parts.sort()
  m.anchors.sort(byID)
  return m
}

/**
 * Validates a GLB against the static subset and computes its manifest.
 * Throws GLBError with the same reason the server would give.
 */
export function inspect(glb: ArrayBuffer | Uint8Array, limits: Limits = defaultLimits): Manifest {
  const bytes = glb instanceof Uint8Array ? glb : new Uint8Array(glb)
  if (bytes.byteLength > limits.maxBytes) fail('limit', `file is ${bytes.byteLength} bytes, limit ${limits.maxBytes}`)
  const { json, bin } = splitContainer(bytes)
  return manifestOf(parseDocument(json, bin, limits), limits)
}

/** Coordinate tolerance (metres) when comparing manifests from different implementations. */
export const TOLERANCE = 1e-4

function near(a: readonly number[], b: readonly number[]): boolean {
  return a.length === 3 && b.length === 3 && a.every((v, i) => Math.abs(v - b[i]) <= TOLERANCE)
}

/**
 * The contract comparison: after sorting by id, ids, hints and the triangle
 * count are exact; bounds and anchor positions agree within TOLERANCE.
 */
export function matches(x: Manifest, y: Manifest): boolean {
  const copy = (m: Manifest): Manifest =>
    normalize({ ...m, slots: [...m.slots], parts: [...m.parts], anchors: [...m.anchors] })
  const a = copy(x)
  const b = copy(y)
  return (
    a.triangles === b.triangles &&
    near(a.bounds.min, b.bounds.min) &&
    near(a.bounds.max, b.bounds.max) &&
    a.slots.length === b.slots.length &&
    a.slots.every((s, i) => s.id === b.slots[i].id && (s.hint ?? '') === (b.slots[i].hint ?? '')) &&
    a.parts.length === b.parts.length &&
    a.parts.every((p, i) => p === b.parts[i]) &&
    a.anchors.length === b.anchors.length &&
    a.anchors.every((s, i) => s.id === b.anchors[i].id && near(s.position, b.anchors[i].position))
  )
}
