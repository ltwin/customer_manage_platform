import test, { mock } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { resolveObjectURL } from 'node:buffer'
import { Worker as NodeWorker } from 'node:worker_threads'
import { LoadingManager, Mesh, type MeshStandardMaterial } from 'three'
import { DRACOLoader } from 'three/examples/jsm/loaders/DRACOLoader.js'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'
import { ImportRejected, parseImport, type Decoders, type GLTFJSON } from '../src/creative-scene/import/parse.ts'

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
