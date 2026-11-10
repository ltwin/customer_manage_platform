import test, { mock } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { resolveObjectURL } from 'node:buffer'
import { Worker as NodeWorker } from 'node:worker_threads'
import { Box3, LoadingManager, Mesh, type MeshStandardMaterial, type Object3D } from 'three'
import { DRACOLoader } from 'three/examples/jsm/loaders/DRACOLoader.js'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'
import { applyTransform, convert, slotNames, slotPayload, type Conversion } from '../src/creative-scene/import/convert.ts'
import { ImportRejected, parseImport, type Decoders, type GLTFJSON, type MaterialJSON } from '../src/creative-scene/import/parse.ts'

// The GLB import (scene-glb-import group 4): files built here from JSON, and
// the Draco and meshopt samples in fixtures/glb-import.

const fixtures = path.join(import.meta.dirname, 'fixtures', 'glb-import')
const threeLibs = path.join(import.meta.dirname, '..', 'node_modules', 'three', 'examples', 'jsm', 'libs')

// DRACOLoader decodes in a Web Worker made from a blob: URL; Node runs the same
// script in a worker thread, with the few globals it uses.
const workerShim = `const { parentPort } = require('node:worker_threads');
globalThis.self = globalThis;
globalThis.postMessage = (m, t) => parentPort.postMessage(m, t);
parentPort.on('message', (data) => globalThis.onmessage({ data }));
`
class BrowserWorker {
  onmessage: ((e: { data: unknown }) => void) | null = null
  readonly #thread: Promise<NodeWorker>
  constructor(url: string) {
    this.#thread = resolveObjectURL(url)!.text().then((code) => {
      const thread = new NodeWorker(workerShim + code, { eval: true })
      thread.on('message', (data) => this.onmessage?.({ data }))
      return thread
    })
  }
  postMessage(message: unknown, transfer?: ArrayBuffer[]) {
    void this.#thread.then((t) => t.postMessage(message, transfer))
  }
  terminate() {
    void this.#thread.then((t) => t.terminate())
  }
}
;(globalThis as { Worker?: unknown }).Worker ??= BrowserWorker
// three's FileLoader, fetching the decoder, reports progress with it.
;(globalThis as { ProgressEvent?: unknown }).ProgressEvent ??= class extends Event {
  constructor(type: string, init: object = {}) {
    super(type)
    Object.assign(this, init)
  }
}

const dataURL = (bytes: Uint8Array, type: string) => `data:${type};base64,${Buffer.from(bytes).toString('base64')}`

const nodeDecoders: Decoders = {
  draco: () =>
    new DRACOLoader(new LoadingManager()).setDecoderPath({
      js: dataURL(readFileSync(path.join(threeLibs, 'draco', 'gltf', 'draco_wasm_wrapper.js')), 'text/javascript'),
      wasm: dataURL(readFileSync(path.join(threeLibs, 'draco', 'gltf', 'draco_decoder.wasm')), 'application/wasm'),
    }),
}

// 1 × 1 PNG, JPEG and WebP headers enough to be told apart.
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64')

/** A GLB of the given JSON and binary chunk. */
function glb(json: GLTFJSON, bin?: Uint8Array): ArrayBuffer {
  const text = Buffer.from(JSON.stringify(json))
  const jsonPad = Buffer.alloc((4 - (text.length % 4)) % 4, 0x20)
  const chunks = [Buffer.concat([text, jsonPad])]
  if (bin) chunks.push(Buffer.concat([Buffer.from(bin), Buffer.alloc((4 - (bin.byteLength % 4)) % 4)]))
  const parts: Buffer[] = []
  const header = Buffer.alloc(12)
  header.writeUInt32LE(0x46546c67, 0)
  header.writeUInt32LE(2, 4)
  chunks.forEach((c, i) => {
    const h = Buffer.alloc(8)
    h.writeUInt32LE(c.length, 0)
    h.writeUInt32LE(i === 0 ? 0x4e4f534a : 0x004e4942, 4)
    parts.push(h, c)
  })
  const body = Buffer.concat(parts)
  header.writeUInt32LE(12 + body.length, 8)
  const out = Buffer.concat([header, body])
  return out.buffer.slice(out.byteOffset, out.byteOffset + out.byteLength)
}

/**
 * One triangle whose material samples image 0 as its base colour; the image's
 * bytes follow the positions in the binary chunk unless image overrides it.
 */
function textured(image: NonNullable<GLTFJSON['images']>[number] | null, extra: Partial<GLTFJSON> = {}): ArrayBuffer {
  const positions = new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0])
  const bin = Buffer.concat([Buffer.from(positions.buffer), png, Buffer.alloc((4 - (png.length % 4)) % 4)])
  return glb(
    {
      asset: { version: '2.0' },
      scenes: [{ nodes: [0] }],
      nodes: [{ name: 'Tri', mesh: 0 }],
      meshes: [{ primitives: [{ attributes: { POSITION: 0 }, material: 0 }] }],
      materials: [{ name: 'Paper', pbrMetallicRoughness: { baseColorTexture: { index: 0 } } }],
      textures: [{ source: 0, sampler: 0 }],
      samplers: [{ wrapS: 10497, wrapT: 10497 }],
      images: [image ?? { bufferView: 1, mimeType: 'image/png' }],
      accessors: [{ bufferView: 0, componentType: 5126, count: 3, type: 'VEC3', min: [0, 0, 0], max: [1, 1, 0] }],
      bufferViews: [
        { buffer: 0, byteOffset: 0, byteLength: 36 },
        { buffer: 0, byteOffset: 36, byteLength: png.length },
      ],
      buffers: [{ byteLength: bin.length }],
      ...extra,
    },
    bin,
  )
}

const meshes = (scene: { traverse(f: (o: unknown) => void): void }) => {
  const out: Mesh[] = []
  scene.traverse((o) => {
    if (o instanceof Mesh) out.push(o)
  })
  return out
}

async function rejects(file: ArrayBuffer, reason: ImportRejected['reason'], loaderRuns = false) {
  const parse = mock.method(GLTFLoader.prototype, 'parse')
  try {
    await assert.rejects(parseImport(file, nodeDecoders), (e: unknown) => e instanceof ImportRejected && e.reason === reason)
    if (!loaderRuns) assert.equal(parse.mock.callCount(), 0, `${reason}: refused before GLTFLoader`)
  } finally {
    parse.mock.restore()
  }
}

test('external images and buffers are refused before the loader sees the file', async () => {
  await rejects(textured({ uri: 'textures/paper.png' }), 'external_uri')
  await rejects(textured({ uri: 'https://example.com/paper.png' }), 'external_uri')
  await rejects(
    glb({
      asset: { version: '2.0' },
      scenes: [{ nodes: [0] }],
      nodes: [{ mesh: 0 }],
      meshes: [{ primitives: [{ attributes: { POSITION: 0 } }] }],
      accessors: [{ bufferView: 0, componentType: 5126, count: 3, type: 'VEC3', min: [0, 0, 0], max: [1, 1, 0] }],
      bufferViews: [{ buffer: 0, byteLength: 36 }],
      buffers: [{ uri: 'model.bin', byteLength: 36 }],
    }),
    'external_uri',
  )
})

test('KTX2, skins, files that are not GLB and images in other formats are refused', async () => {
  await rejects(textured({ bufferView: 1, mimeType: 'image/ktx2' }), 'ktx2')
  await rejects(textured(null, { extensionsUsed: ['KHR_texture_basisu'] }), 'ktx2')
  await rejects(textured(null, { skins: [{ joints: [0] }] }), 'skinned')
  await rejects(new TextEncoder().encode('{"asset":{"version":"2.0"}}').buffer as ArrayBuffer, 'not_glb')
  await rejects(textured({ uri: dataURL(new Uint8Array([1, 2, 3, 4]), 'image/png') }), 'image_format')
  await rejects(textured({ bufferView: 1, mimeType: 'image/jpeg' }), 'image_format')
})

test('images come out as the bytes stored, from the binary chunk or a data: URL', async () => {
  for (const image of [null, { uri: dataURL(png, 'image/png') }]) {
    const parsed = await parseImport(textured(image), nodeDecoders)
    const got = parsed.images.get(0)
    assert.ok(got)
    assert.equal(got.mime, 'image/png')
    assert.deepEqual(Buffer.from(got.bytes), png)
    // three loads the material and never the picture.
    const [mesh] = meshes(parsed.scene)
    assert.ok(mesh)
    assert.equal((mesh.material as MeshStandardMaterial).map, null)
    assert.equal(parsed.associations.get(mesh.material as MeshStandardMaterial)?.materials, 0)
    // The JSON handed back is the file's own, textures included.
    assert.equal(parsed.json.materials?.[0]?.pbrMetallicRoughness?.baseColorTexture?.index, 0)
  }
})

test('Draco and meshopt geometry is unpacked', async () => {
  for (const name of ['draco.glb', 'meshopt.glb']) {
    const file = readFileSync(path.join(fixtures, name))
    const parsed = await parseImport(file.buffer.slice(file.byteOffset, file.byteOffset + file.byteLength) as ArrayBuffer, nodeDecoders)
    const found = meshes(parsed.scene)
    assert.deepEqual(found.map((m) => (m.material as MeshStandardMaterial).name).sort(), ['Body', 'Trim'], name)
    for (const m of found) {
      const position = m.geometry.getAttribute('position')
      assert.ok(position.count > 0, `${name}: ${m.name} has vertices`)
      assert.ok(m.geometry.index && m.geometry.index.count > 0, `${name}: ${m.name} has triangles`)
    }
    const body = found.find((m) => (m.material as MeshStandardMaterial).name === 'Body')!
    assert.ok(body.geometry.getAttribute('color'), `${name}: COLOR_0 kept`)
  }
})

// --- convert (task 4.2) ---

type Typed = Float32Array | Int16Array | Uint16Array | Int8Array | Uint8Array | Uint32Array
const componentTypes = new Map<Function, number>([
  [Float32Array, 5126],
  [Int16Array, 5122],
  [Uint16Array, 5123],
  [Int8Array, 5120],
  [Uint8Array, 5121],
  [Uint32Array, 5125],
])
const sizes: Record<string, number> = { SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4 }

/**
 * A GLB whose accessors each get a bufferView of their own (POSITION's min/max
 * filled in) and whose images follow them; json supplies the rest.
 */
function build(json: GLTFJSON, accessors: { array: Typed; type: string; normalized?: boolean }[], imageBytes: Uint8Array[] = []): ArrayBuffer {
  const chunks: Buffer[] = []
  const bufferViews: NonNullable<GLTFJSON['bufferViews']> = []
  let offset = 0
  const push = (bytes: Uint8Array) => {
    bufferViews.push({ buffer: 0, byteOffset: offset, byteLength: bytes.byteLength })
    const pad = Buffer.alloc((4 - (bytes.byteLength % 4)) % 4)
    chunks.push(Buffer.from(bytes.buffer, bytes.byteOffset, bytes.byteLength), pad)
    offset += bytes.byteLength + pad.length
  }
  const acc = accessors.map((a, i) => {
    push(new Uint8Array(a.array.buffer, a.array.byteOffset, a.array.byteLength))
    const n = sizes[a.type]!
    const min: number[] = []
    const max: number[] = []
    for (let c = 0; c < n; c++) {
      let lo = Infinity
      let hi = -Infinity
      for (let k = c; k < a.array.length; k += n) {
        lo = Math.min(lo, a.array[k]!)
        hi = Math.max(hi, a.array[k]!)
      }
      min.push(lo)
      max.push(hi)
    }
    return { bufferView: i, componentType: componentTypes.get(a.array.constructor)!, count: a.array.length / n, type: a.type, min, max, ...(a.normalized ? { normalized: true } : {}) }
  })
  const images = imageBytes.map((b) => {
    push(b)
    return { bufferView: bufferViews.length - 1, mimeType: sniffType(b) }
  })
  const bin = Buffer.concat(chunks)
  return glb({ asset: { version: '2.0' }, scenes: [{ nodes: [0] }], accessors: acc, bufferViews, buffers: [{ byteLength: bin.length }], ...(images.length > 0 ? { images } : {}), ...json }, bin)
}
const sniffType = (b: Uint8Array) => (b[0] === 0x89 ? 'image/png' : b[0] === 0xff ? 'image/jpeg' : 'image/webp')

// A unit quad in the XY plane: positions, UVs (0–1, or scaled by uvScale), indices.
const quadPositions = new Float32Array([0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0])
const quadUVs = (scale = 1, shift = 0) => new Float32Array([0, 1, 1, 1, 1, 0, 0, 0].map((v) => v * scale + shift))
const quadIndices = new Uint16Array([0, 1, 2, 0, 2, 3])
const jpeg = Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 0x4a, 0x46, 0x49, 0x46, 0, 1])
const webp = Buffer.from([0x52, 0x49, 0x46, 0x46, 4, 0, 0, 0, 0x57, 0x45, 0x42, 0x50])

/** One quad per material, each its own node, sharing positions, UVs and indices. */
function quads(materials: MaterialJSON[], extra: Partial<GLTFJSON> = {}, opts: { uv?: Float32Array | null; color?: boolean; images?: Uint8Array[] } = {}): ArrayBuffer {
  const accessors: { array: Typed; type: string }[] = [
    { array: quadPositions, type: 'VEC3' },
    { array: quadIndices, type: 'SCALAR' },
  ]
  const attributes: Record<string, number> = { POSITION: 0 }
  if (opts.uv !== null) {
    accessors.push({ array: opts.uv ?? quadUVs(), type: 'VEC2' })
    attributes.TEXCOORD_0 = accessors.length - 1
  }
  if (opts.color) {
    accessors.push({ array: new Float32Array(12).fill(0.5), type: 'VEC3' })
    attributes.COLOR_0 = accessors.length - 1
  }
  const nodes = materials.map((_, i) => ({ name: `n${i}`, mesh: i }))
  const meshes = materials.map((_, i) => ({ primitives: [{ attributes, indices: 1, material: i }] }))
  return build({ scenes: [{ nodes: nodes.map((_, i) => i) }], nodes, meshes, materials, ...extra }, accessors, opts.images ?? [])
}

const textureRefs = (n: number): Partial<GLTFJSON> => ({ textures: Array.from({ length: n }, (_, i) => ({ source: i })) })

async function converted(file: ArrayBuffer, options = { originToBottomCenter: false }) {
  return convert(await parseImport(file, nodeDecoders), options)
}
const texts = (c: Conversion, slot: string | null = null) => c.report.filter((r) => r.slot === slot).map((r) => r.text)

test('slot names: sanitised, numbered when empty or taken, `default` for primitives without a material', () => {
  const names = slotNames([{ name: 'Body Paint' }, { name: '' }, { name: 'Body_Paint' }, {}, { name: 'unused' }, { name: 'default' }], new Set([0, 1, 2, 3, 5, null]))
  assert.deepEqual([...names], [
    [null, 'default'],
    [0, 'Body_Paint'],
    [1, 'material_1'],
    [2, 'Body_Paint_2'],
    [3, 'material_3'],
    [5, 'default_2'],
  ])
  assert.equal(slotNames([{ name: 'x'.repeat(80) }, { name: 'x'.repeat(70) }], new Set([0, 1])).get(1), `${'x'.repeat(62)}_2`)
})

test('more than 32 materials are refused', async () => {
  const many = Array.from({ length: 33 }, (_, i) => ({ name: `m${i}` }))
  await assert.rejects(converted(quads(many)), (e: unknown) => e instanceof ImportRejected && e.reason === 'limit')
})

test('constants: base colour to sRGB with alpha as opacity, metal and roughness, emission, alpha modes', async () => {
  const c = await converted(
    quads([
      { name: 'a', pbrMetallicRoughness: { baseColorFactor: [1, 0.2158605, 0, 0.5], metallicFactor: 0.25, roughnessFactor: 0.75 }, emissiveFactor: [0, 0, 1], alphaMode: 'BLEND', doubleSided: true },
      { name: 'b', alphaMode: 'MASK', alphaCutoff: 0.3, extensions: { KHR_materials_emissive_strength: { emissiveStrength: 200 } } },
      { name: 'c', alphaMode: 'MASK' },
    ]),
  )
  assert.deepEqual(c.slots.get('a')?.params, { color: '#ff8000', opacity: 0.5, metalness: 0.25, roughness: 0.75, emissive: '#0000ff' })
  assert.equal(c.slots.get('a')?.alphaMode, 'blend')
  // glTF's defaults, not principled's: white, fully metal and rough.
  assert.deepEqual(c.slots.get('b')?.params, { color: '#ffffff', opacity: 1, metalness: 1, roughness: 1, emissive: '#000000', emissive_intensity: 100 })
  assert.deepEqual(texts(c, 'b'), ['自发光强度 200 超出 0–100，已限制为 100'])
  const payload = slotPayload(c.slots, () => 'unused')
  assert.deepEqual(payload.b, { preset: 'principled@1', params: c.slots.get('b')!.params, alpha_mode: 'clip', alpha_cutoff: 0.3 })
  assert.equal(payload.c?.alpha_cutoff, 0.5)
  assert.equal(payload.a?.alpha_mode, 'blend')
  // doubleSided is dropped without a word: models always draw both sides.
  assert.deepEqual(texts(c, 'a'), [])
})

test('maps: base colour, ORM sharing one image, normal strength, AO strength, emission; the payload names revisions', async () => {
  const c = await converted(
    quads(
      [
        {
          name: 'm',
          pbrMetallicRoughness: { baseColorTexture: { index: 0 }, metallicRoughnessTexture: { index: 1 } },
          occlusionTexture: { index: 1, strength: 0.5 },
          normalTexture: { index: 2, scale: 6 },
          emissiveTexture: { index: 3 },
        },
      ],
      textureRefs(4),
      { images: [png, png, png, jpeg] },
    ),
  )
  const slot = c.slots.get('m')!
  assert.deepEqual(slot.maps, { base_color: { image: 0 }, roughness: { image: 1, channel: 'g' }, metalness: { image: 1, channel: 'b' }, ao: { image: 1, channel: 'r' }, normal: { image: 2 }, emissive: { image: 3 } })
  assert.equal(slot.normalStrength, 4)
  assert.deepEqual([...c.images].sort(), [[0, 'color'], [1, 'data'], [2, 'data'], [3, 'color']])
  assert.deepEqual(texts(c, 'm').sort(), ['AO 强度 0.5 已忽略，按 1 显示', '法线强度 6 超出 0–4，已限制为 4'].sort())
  const payload = slotPayload(c.slots, (i) => `ccrv_img${i}`)
  assert.deepEqual(payload.m?.maps, {
    base_color: { content_revision_id: 'ccrv_img0' },
    roughness: { content_revision_id: 'ccrv_img1', channel: 'g' },
    metalness: { content_revision_id: 'ccrv_img1', channel: 'b' },
    ao: { content_revision_id: 'ccrv_img1', channel: 'r' },
    normal: { content_revision_id: 'ccrv_img2' },
    emissive: { content_revision_id: 'ccrv_img3' },
  })
  assert.deepEqual(payload.m?.mapping, { kind: 'uv', repeat: [1, 1], rotation_deg: 0, normal_strength: 4 })
})

test('an image used as colour and as data is encoded as data; EXT_texture_webp is the source it names', async () => {
  const c = await converted(
    quads(
      [{ name: 'm', pbrMetallicRoughness: { baseColorTexture: { index: 0 } }, normalTexture: { index: 1 } }],
      { textures: [{ source: 0 }, { source: 0, extensions: { EXT_texture_webp: { source: 1 } } }], extensionsUsed: ['EXT_texture_webp'] },
      { images: [png, webp] },
    ),
  )
  assert.deepEqual(c.slots.get('m')?.maps, { base_color: { image: 0 }, normal: { image: 1 } })
  const shared = await converted(quads([{ name: 'm', pbrMetallicRoughness: { baseColorTexture: { index: 0 } }, normalTexture: { index: 0 } }], textureRefs(1), { images: [png] }))
  assert.deepEqual([...shared.images], [[0, 'data']])
})

test('vertex colours: a slot whose primitives carry COLOR_0 turns vertex_color on', async () => {
  const c = await converted(quads([{ name: 'painted' }], {}, { color: true }))
  assert.equal(c.slots.get('painted')?.params.vertex_color, true)
  const plain = await converted(quads([{ name: 'plain' }]))
  assert.equal(plain.slots.get('plain')?.params.vertex_color, undefined)
})

test('extension constants go to parameters, clamped; their textures, unlit and unknown extensions are reported', async () => {
  const c = await converted(
    quads([
      {
        name: 'm',
        extensions: {
          KHR_materials_transmission: { transmissionFactor: 0.8, transmissionTexture: { index: 0 } },
          KHR_materials_ior: { ior: 1.2 },
          KHR_materials_volume: { thicknessFactor: 20 },
          KHR_materials_clearcoat: { clearcoatFactor: 0.5, clearcoatRoughnessFactor: 0.25 },
          KHR_materials_sheen: { sheenColorFactor: [1, 1, 1], sheenRoughnessFactor: 0.5 },
          KHR_materials_specular: { specularFactor: 0.5, specularColorFactor: [1, 0, 0] },
          KHR_materials_iridescence: { iridescenceFactor: 1, iridescenceThicknessMaximum: 600 },
          KHR_materials_anisotropy: { anisotropyStrength: 0.4, anisotropyRotation: 1 },
          KHR_materials_dispersion: { dispersion: 1 },
        },
      },
      { name: 'flat', extensions: { KHR_materials_unlit: {} } },
    ]),
  )
  const p = c.slots.get('m')!.params
  assert.deepEqual(
    { ...p, color: undefined, opacity: undefined, metalness: undefined, roughness: undefined, emissive: undefined },
    {
      color: undefined,
      opacity: undefined,
      metalness: undefined,
      roughness: undefined,
      emissive: undefined,
      transmission: 0.8,
      ior: 1.2,
      thickness: 10,
      clearcoat: 0.5,
      clearcoat_roughness: 0.25,
      sheen: 1,
      sheen_color: '#ffffff',
      sheen_roughness: 0.5,
      specular_intensity: 0.5,
      specular_color: '#ff0000',
      iridescence: 1,
      iridescence_thickness: 600,
      anisotropy: 0.4,
      anisotropy_rotation: 1,
    },
  )
  assert.deepEqual(texts(c, 'm').sort(), ['厚度 20 超出 0–10，已限制为 10', '透射贴图不支持，已忽略', '材质扩展 KHR_materials_dispersion 不支持，已忽略'].sort())
  assert.deepEqual(texts(c, 'flat'), ['原本是不受光照的材质（unlit），导入后按普通材质受光照显示'])
})

test('texture transforms are baked into a copy of the UVs; a different one on another map is reported', async () => {
  const transform = { KHR_texture_transform: { offset: [0.5, 0], rotation: Math.PI / 2, scale: [2, 2] } }
  const file = quads(
    [
      { name: 'moved', pbrMetallicRoughness: { baseColorTexture: { index: 0, extensions: transform } }, normalTexture: { index: 0 } },
      { name: 'still', pbrMetallicRoughness: { baseColorTexture: { index: 0 } } },
    ],
    { ...textureRefs(1), extensionsUsed: ['KHR_texture_transform'] },
    { images: [png] },
  )
  const c = await converted(file)
  const [moved, still] = ['moved', 'still'].map((id) => meshes(c.scene).find((m) => (m.material as MeshStandardMaterial).name === `slot:${id}`)!)
  // uv' = T R S uv: (1, 1) → S (2, 2) → R(90°) (2, -2) → T (2.5, -2).
  const uv = moved!.geometry.getAttribute('uv')
  const at = (i: number) => [uv.getX(i), uv.getY(i)].map((v) => Math.round(v * 1e6) / 1e6)
  assert.deepEqual(at(1), applyTransform({ offset: [0.5, 0], rotation: Math.PI / 2, scale: [2, 2] }, 1, 1).map((v) => Math.round(v * 1e6) / 1e6))
  assert.deepEqual(at(1), [2.5, -2])
  // The other slot shared the geometry and keeps the UVs it had.
  assert.notEqual(moved!.geometry, still!.geometry)
  assert.deepEqual([still!.geometry.getAttribute('uv').getX(1), still!.geometry.getAttribute('uv').getY(1)], [1, 1])
  assert.deepEqual(texts(c, 'moved'), ['法线贴图的贴图变换与基础色贴图不同，按基础色贴图的变换显示'])
})

test('a texture on the second UV set is dropped; maps without UVs are told they show triplanar', async () => {
  const c = await converted(quads([{ name: 'm', pbrMetallicRoughness: { baseColorTexture: { index: 0 } }, emissiveTexture: { index: 0, texCoord: 1 } }], textureRefs(1), { images: [png] }))
  assert.deepEqual(c.slots.get('m')?.maps, { base_color: { image: 0 } })
  assert.deepEqual(texts(c, 'm'), ['自发光贴图用的是第二套 UV，已忽略'])
  const bare = await converted(quads([{ name: 'm', pbrMetallicRoughness: { baseColorTexture: { index: 0 } } }], textureRefs(1), { images: [png], uv: null }))
  assert.deepEqual(texts(bare, 'm'), ['模型没有 UV，贴图按三平面投影显示'])
})

test('mirrored and clamped wrapping is reported only where the UVs leave [0, 1]', async () => {
  for (const [wrap, kind] of [
    [33648, '镜像'],
    [33071, '钳制'],
  ] as const) {
    for (const [uv, told] of [
      [quadUVs(), false],
      [quadUVs(2, -0.5), true],
    ] as const) {
      const c = await converted(quads([{ name: 'm', pbrMetallicRoughness: { baseColorTexture: { index: 0 } } }], { textures: [{ source: 0, sampler: 0 }], samplers: [{ wrapS: wrap, wrapT: 10497 }] }, { images: [png], uv }))
      assert.deepEqual(texts(c, 'm'), told ? [`基础色贴图原本是${kind}寻址，导入后按重复显示`] : [], `${kind} ${told}`)
    }
  }
})

const worldBox = (scene: Object3D) => {
  const b = new Box3().setFromObject(scene, true)
  return [...b.min.toArray(), ...b.max.toArray()].map((v) => Math.round(v * 1e4) / 1e4)
}
const allFloat = (scene: Object3D) =>
  meshes(scene).every((m) => Object.values(m.geometry.attributes).every((a) => !('isInterleavedBufferAttribute' in a) && a.array instanceof Float32Array && !a.normalized))

test('quantised geometry becomes plain floats where it was: integer positions under a scaled node, normalised UVs, interleaving', async () => {
  // SHORT positions (not normalised) with the scale on the node, as gltfpack writes them.
  const ints = build(
    {
      nodes: [{ mesh: 0, scale: [0.01, 0.01, 0.01] }],
      meshes: [{ primitives: [{ attributes: { POSITION: 0, TEXCOORD_0: 1 }, indices: 2 }] }],
      extensionsUsed: ['KHR_mesh_quantization'],
      extensionsRequired: ['KHR_mesh_quantization'],
    },
    [
      { array: new Int16Array([0, 0, 0, 100, 0, 0, 100, 100, 0, 0, 100, 0]), type: 'VEC3' },
      { array: new Uint16Array([0, 65535, 65535, 65535, 65535, 0, 0, 0]), type: 'VEC2', normalized: true },
      { array: quadIndices, type: 'SCALAR' },
    ],
  )
  const parsed = await parseImport(ints, nodeDecoders)
  const before = worldBox(parsed.scene)
  const c = convert(parsed, { originToBottomCenter: false })
  assert.deepEqual(worldBox(c.scene), before)
  assert.deepEqual(before, [0, 0, 0, 1, 1, 0])
  assert.ok(allFloat(c.scene))
  const uv = meshes(c.scene)[0]!.geometry.getAttribute('uv')
  assert.deepEqual([uv.getX(1), uv.getY(1)], [1, 1])

  // Position and normal interleaved in one bufferView.
  const interleaved = new Float32Array([0, 0, 0, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 1])
  const bytes = Buffer.from(interleaved.buffer)
  const file = glb(
    {
      asset: { version: '2.0' },
      scenes: [{ nodes: [0] }],
      nodes: [{ mesh: 0 }],
      meshes: [{ primitives: [{ attributes: { POSITION: 0, NORMAL: 1 } }] }],
      accessors: [
        { bufferView: 0, byteOffset: 0, componentType: 5126, count: 3, type: 'VEC3', min: [0, 0, 0], max: [1, 1, 0] },
        { bufferView: 0, byteOffset: 12, componentType: 5126, count: 3, type: 'VEC3' },
      ],
      bufferViews: [{ buffer: 0, byteLength: bytes.length, byteStride: 24 } as NonNullable<GLTFJSON['bufferViews']>[number]],
      buffers: [{ byteLength: bytes.length }],
    },
    bytes,
  )
  const ic = await converted(file)
  assert.ok(allFloat(ic.scene))
  const g = meshes(ic.scene)[0]!.geometry
  assert.deepEqual(Array.from(g.getAttribute('position').array), [0, 0, 0, 1, 0, 0, 1, 1, 0])
  assert.deepEqual(Array.from(g.getAttribute('normal').array), [0, 0, 1, 0, 0, 1, 0, 0, 1])

  // The meshopt sample: quantised by gltfpack's rules.
  const sample = readFileSync(path.join(fixtures, 'meshopt.glb'))
  const mp = await parseImport(sample.buffer.slice(sample.byteOffset, sample.byteOffset + sample.byteLength) as ArrayBuffer, nodeDecoders)
  const mpBefore = worldBox(mp.scene)
  const mc = convert(mp, { originToBottomCenter: false })
  assert.ok(allFloat(mc.scene))
  assert.deepEqual(worldBox(mc.scene), mpBefore)
})

test('morph targets, animations, cameras, lights, other scenes and points are dropped and reported', async () => {
  const c = await converted(
    build(
      {
        scene: 1,
        scenes: [{ nodes: [] }, { nodes: [0, 1, 2] }],
        nodes: [{ name: 'shape', mesh: 0 }, { name: 'eye', camera: 0 }, { name: 'dots', mesh: 1 }],
        meshes: [{ primitives: [{ attributes: { POSITION: 0 }, indices: 1, targets: [{ POSITION: 2 }] }] }, { primitives: [{ attributes: { POSITION: 0 }, mode: 0 }] }],
        cameras: [{ type: 'perspective', perspective: { yfov: 1, znear: 0.1 } }],
        animations: [{ channels: [], samplers: [] }],
        extensionsUsed: ['KHR_lights_punctual'],
      } as GLTFJSON,
      [
        { array: quadPositions, type: 'VEC3' },
        { array: quadIndices, type: 'SCALAR' },
        { array: new Float32Array(12), type: 'VEC3' },
      ],
    ),
  )
  assert.deepEqual(texts(c).sort(), ['动画已丢掉', '相机已丢掉', '灯光已丢掉', '文件有 2 个场景，只导入了第 2 个', '形态键已丢掉，按基础形状保存', '点和线不支持，已丢掉'].sort())
  const left = meshes(c.scene)
  assert.equal(left.length, 1)
  assert.deepEqual(left[0]!.geometry.morphAttributes, {})
  let others = 0
  c.scene.traverse((o) => {
    if (o !== c.scene && !(o instanceof Mesh) && o.type !== 'Object3D' && o.type !== 'Group') others++
  })
  assert.equal(others, 0)
})

test('the origin stays where the file has it, or moves to the bottom centre', async () => {
  const file = () =>
    build({ nodes: [{ mesh: 0, translation: [3, 2, 1] }], meshes: [{ primitives: [{ attributes: { POSITION: 0 }, indices: 1 }] }] }, [
      { array: quadPositions, type: 'VEC3' },
      { array: quadIndices, type: 'SCALAR' },
    ])
  assert.deepEqual(worldBox((await converted(file())).scene), [3, 2, 1, 4, 3, 1])
  assert.deepEqual(worldBox((await converted(file(), { originToBottomCenter: true })).scene), [-0.5, 0, 0, 0.5, 1, 0])
})
