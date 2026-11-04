import test, { type TestContext } from 'node:test'
import assert from 'node:assert/strict'
import { NoColorSpace, SRGBColorSpace, type Texture } from 'three'
import type { Scene } from '../src/creative-scene/api.ts'
import {
  acquireTexture,
  budgetUsed,
  imageBudget,
  imagePlatform,
  imageStatus,
  loadSceneImages,
  ormKey,
  releaseTexture,
  resetImages,
  sceneTextureKeys,
  textureBytes,
  textureKeys,
  type Layer,
  type Picture,
} from '../src/creative-scene/materials/images.ts'

// materials/images.ts with the browser stood in for: fetching gives the
// revision back as the file, decoding makes a picture named after it, and
// gates hold a decode until a test opens them.

type Fake = Picture & { name: string; closed: boolean }
const fake = (name: string, width: number, height: number): Fake => ({ name, width, height, closed: false, close() { this.closed = true } })

let decodes: string[] = []
let composed: (string | null)[][] = []
let failing = new Set<string>()
let sizes: Record<string, [number, number]> = {}
let gates = new Map<string, Promise<void>>()
let decodingNow = 0
let mostAtOnce = 0

imagePlatform.fetch = async (revision) => {
  if (failing.has(revision)) throw new Error('HTTP 404')
  return new Blob([revision])
}
imagePlatform.decode = async (blob) => {
  const revision = await blob.text()
  decodes.push(revision)
  decodingNow++
  mostAtOnce = Math.max(mostAtOnce, decodingNow)
  await gates.get(revision)
  decodingNow--
  const [w, h] = sizes[revision] ?? [2, 2]
  return fake(revision, w, h)
}
imagePlatform.compose = async (layers: readonly (Layer | null)[], width, height) => {
  composed.push(layers.map((l) => l && `${(l.picture as Fake).name}.${l.channel}`))
  return fake('composite', width, height)
}
imagePlatform.placeholder = (rgba) => ({ width: 1, height: 1, rgba })

function fresh(t: TestContext) {
  resetImages()
  decodes = []
  composed = []
  failing = new Set()
  sizes = {}
  gates = new Map()
  decodingNow = 0
  mostAtOnce = 0
  t.mock.timers.reset()
  t.mock.timers.enable({ apis: ['setTimeout'] })
}

/** Lets every pending promise run; setImmediate is not mocked. */
async function flush() {
  for (let i = 0; i < 30; i++) await new Promise((r) => setImmediate(r))
}

function gate(revision: string): () => void {
  let open!: () => void
  gates.set(revision, new Promise<void>((r) => (open = r)))
  return open
}

const shown = (t: Texture) => (t.image as Fake).name ?? 'placeholder'
const disposals = (t: Texture) => {
  const n = { count: 0 }
  t.addEventListener('dispose', () => n.count++)
  return n
}
/** A device uploads the texture as it is now. */
const upload = (t: Texture) => t.onUpdate?.(t)

const sceneOf = (...slots: Record<string, unknown>[]) =>
  ({ objects: slots.map((maps, i) => ({ id: `o${i}`, material_slots: { main: { preset: 'principled@1', params: {}, maps } } })) }) as unknown as Pick<Scene, 'objects'>
const img = (id: string, channel?: string) => ({ content_revision_id: id, ...(channel ? { channel } : {}) })

test('a texture is one object from the first ask to the last release, freed a moment after', async (t) => {
  fresh(t)
  const a = acquireTexture('color:ccr_a')
  assert.equal(shown(a), 'placeholder', 'a placeholder until the image is here')
  assert.equal(acquireTexture('color:ccr_a'), a)
  await flush()
  assert.equal(shown(a), 'ccr_a', 'the same object shows the image')
  const disposed = disposals(a)
  releaseTexture('color:ccr_a')
  t.mock.timers.tick(5000)
  assert.equal(disposed.count, 0, 'still held once')
  releaseTexture('color:ccr_a')
  t.mock.timers.tick(999)
  acquireTexture('color:ccr_a')
  releaseTexture('color:ccr_a')
  t.mock.timers.tick(999)
  assert.equal(disposed.count, 0, 'asked for again within the moment: kept')
  t.mock.timers.tick(1)
  assert.equal(disposed.count, 1)
  assert.equal(budgetUsed(), 0, 'its bytes back in the budget')
  assert.notEqual(acquireTexture('color:ccr_a'), a, 'a new one after it is freed')
  await flush()
  assert.deepEqual(decodes, ['ccr_a', 'ccr_a'])
})

test('one image as base colour and as roughness: two textures in their own colour spaces, whatever order they load in', async (t) => {
  const keys = textureKeys({ maps: { base_color: img('ccr_a'), roughness: img('ccr_a') } })
  assert.deepEqual(keys, { color: 'color:ccr_a', orm: 'orm:-|ccr_a.r|-' })
  const outcomes = []
  for (const order of [[keys.color!, keys.orm!], [keys.orm!, keys.color!]]) {
    fresh(t)
    for (const k of order) acquireTexture(k)
    await flush()
    const color = acquireTexture(keys.color!)
    const orm = acquireTexture(keys.orm!)
    assert.notEqual(color, orm)
    outcomes.push({ color: [shown(color), color.colorSpace], orm: [shown(orm), orm.colorSpace], decodes, composed })
  }
  assert.deepEqual(outcomes[0], outcomes[1])
  assert.deepEqual(outcomes[0], {
    color: ['ccr_a', SRGBColorSpace],
    orm: ['composite', NoColorSpace],
    decodes: ['ccr_a'],
    composed: [[null, 'ccr_a.r', null]],
  })
})

test('an ORM keys by its sources and their channels; a standard ORM is its image as it is', async (t) => {
  fresh(t)
  const standard = ormKey(img('ccr_orm', 'r'), img('ccr_orm', 'g'), img('ccr_orm', 'b'))!
  const swapped = ormKey(img('ccr_orm', 'g'), img('ccr_orm', 'r'), img('ccr_orm', 'b'))!
  assert.equal(standard, 'orm:ccr_orm.r|ccr_orm.g|ccr_orm.b')
  assert.notEqual(swapped, standard)
  assert.equal(ormKey(undefined, img('ccr_r'), undefined), 'orm:-|ccr_r.r|-', 'a channel defaults to r; a missing map is -')
  assert.equal(ormKey(undefined, undefined, undefined), null)
  const s = acquireTexture(standard)
  const w = acquireTexture(swapped)
  await flush()
  assert.equal(shown(s), 'ccr_orm', 'no composite')
  assert.equal(shown(w), 'composite')
  assert.deepEqual(composed, [['ccr_orm.g', 'ccr_orm.r', 'ccr_orm.b']])
  assert.deepEqual(decodes, ['ccr_orm'], 'one image decoded once')
})

test('over the budget: textures are admitted in the order asked for, the rest keep their placeholder and are listed', async (t) => {
  fresh(t)
  const budget = imageBudget.bytes
  t.after(() => (imageBudget.bytes = budget))
  sizes = { ccr_a: [4, 4], ccr_b: [4, 4], ccr_c: [4, 4] }
  imageBudget.bytes = 2 * textureBytes(4, 4)
  const openA = gate('ccr_a')
  const [a, b, c] = ['ccr_a', 'ccr_b', 'ccr_c'].map((r) => acquireTexture(`color:${r}`))
  await flush()
  assert.deepEqual([shown(a), shown(b), shown(c)], ['placeholder', 'placeholder', 'placeholder'], 'b and c wait for a, asked first')
  openA()
  await flush()
  assert.deepEqual([shown(a), shown(b), shown(c)], ['ccr_a', 'ccr_b', 'placeholder'])
  assert.deepEqual(imageStatus.getState().over, ['color:ccr_c'])
  await assert.rejects(loadSceneImages(sceneOf({ base_color: img('ccr_a') }, { base_color: img('ccr_c') })), /ccr_c 超出贴图显存预算/)
  releaseTexture('color:ccr_a')
  t.mock.timers.tick(1000)
  await flush()
  assert.equal(shown(c), 'ccr_c', 'admitted once a is freed')
  assert.deepEqual(imageStatus.getState().over, [])
  assert.equal(budgetUsed(), 2 * textureBytes(4, 4))
})

test('a failure names the image, and is tried again the next time', async (t) => {
  fresh(t)
  failing.add('ccr_bad')
  const bad = acquireTexture('color:ccr_bad')
  await flush()
  assert.equal(shown(bad), 'placeholder')
  assert.match(imageStatus.getState().failed['color:ccr_bad'], /图片 ccr_bad 加载失败（HTTP 404）/)
  await assert.rejects(loadSceneImages(sceneOf({ base_color: img('ccr_a') }, { normal: img('ccr_bad') })), /ccr_bad/)
  t.mock.timers.tick(1000)
  failing.clear()
  const hold = await loadSceneImages(sceneOf({ base_color: img('ccr_bad') }))
  assert.equal(shown(bad), 'ccr_bad')
  assert.deepEqual(imageStatus.getState().failed, {})
  hold.release()
})

test('pixels are let go once uploaded; a device that comes later has them decoded again', async (t) => {
  fresh(t)
  const a = acquireTexture('color:ccr_a')
  const disposed = disposals(a)
  upload(a)
  await flush()
  assert.equal(disposed.count, 1, 'the placeholder was uploaded: rebuilt for the real size')
  const first = a.image as Fake
  upload(a)
  t.mock.timers.tick(1)
  await flush()
  assert.ok(first.closed, 'closed after the upload')
  assert.equal(shown(a), 'placeholder', 'devices that have it keep it; the image is not uploaded again')

  // An offscreen device waits for the scene's images, held until it closes.
  const hold = await loadSceneImages(sceneOf({ base_color: img('ccr_a') }))
  const second = a.image as Fake
  assert.equal(shown(a), 'ccr_a')
  assert.equal(disposed.count, 1, 'no device has the placeholder: nothing rebuilt')
  upload(a)
  t.mock.timers.tick(1)
  await flush()
  assert.ok(!second.closed, 'held')
  hold.release()
  await flush()
  assert.ok(second.closed)

  // A device that uploads the placeholder has the image decoded again, and the texture rebuilt.
  upload(a)
  await flush()
  assert.equal(shown(a), 'ccr_a')
  assert.equal(disposed.count, 2)
  assert.deepEqual(decodes, ['ccr_a', 'ccr_a', 'ccr_a'])
})

test('at most four images are fetched and decoded at once', async (t) => {
  fresh(t)
  const revisions = ['ccr_1', 'ccr_2', 'ccr_3', 'ccr_4', 'ccr_5', 'ccr_6']
  const opens = revisions.map(gate)
  for (const r of revisions) acquireTexture(`normal:${r}`)
  await flush()
  assert.equal(decodes.length, 4)
  for (const open of opens) {
    open()
    await flush()
  }
  assert.equal(decodes.length, 6)
  assert.equal(mostAtOnce, 4)
})

test('a scene samples the principled slots\' maps, each texture once, sorted', () => {
  const scene = sceneOf({ base_color: img('ccr_b'), metalness: img('ccr_m', 'b') }, { base_color: img('ccr_a'), normal: img('ccr_n') }, { base_color: img('ccr_b') })
  ;(scene.objects[0].material_slots as Record<string, unknown>).brick = { preset: 'stone_brick@1', params: {}, maps: { base_color: img('ccr_x') } }
  assert.deepEqual(sceneTextureKeys(scene), ['color:ccr_a', 'color:ccr_b', 'normal:ccr_n', 'orm:-|-|ccr_m.b'])
})
