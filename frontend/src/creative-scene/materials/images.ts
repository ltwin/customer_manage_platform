import { NoColorSpace, RepeatWrapping, SRGBColorSpace, Texture } from 'three'
import { createStore } from 'zustand/vanilla'
import { mediaTicket } from '../../creative-canvas/editor/media.ts'
import { authorizedFetch } from '../../auth/session.ts'
import type { Scene } from '../api.ts'
import { texturedEntries } from './catalog.ts'
import { catalogMaterial, type Texturing } from './resolve.ts'

// The images a scene's material slots texture with (C5c1 design「图片缓存」),
// in two layers. A source is an image revision, or a built-in texture's file
// (tex:<file>, C5c2 design「图片来源扩展为两种」): its original file, fetched
// once and kept as it came, and its pixels while a texture needs them,
// decoded once whatever they are used as. A texture is a TextureKey: what a
// material samples, one three Texture for every device on the page, the same
// object from the first ask to the last release, so no material rebuilds its
// nodes when the image arrives.
//
// A texture shows a 1×1 placeholder until its image is here: white for a base
// colour, a flat normal, and an ORM of ones, so a slot looks as its constant
// parameters say. Pixels are let go once uploaded (design「显存预算」): the
// texture goes back to the placeholder image, which the devices that have the
// real one never upload again. A device that comes later either waits
// through loadSceneImages, which decodes the images again first, or uploads
// the placeholder, which has them decoded again and the texture rebuilt.

/** `color:<source>`, `normal:<source>` or `orm:<ao>|<roughness>|<metalness>`, each part `<source>.<channel>` or `-`; a source is a revision or `tex:<file>`. */
export type TextureKey = string

type Kind = 'color' | 'normal' | 'orm'
type Channel = 'r' | 'g' | 'b' | 'a'
type Part = { revision: string; channel: Channel }
type MapRef = { content_revision_id: string; channel?: Channel }

/** Longest side an image is decoded to. */
export const longSide = 2048
/** Images being fetched and decoded at once; the rest wait their turn. */
const decodeAtOnce = 4
const releaseAfterMs = 1000
/** How long an original may take to come: admission goes in the order asked for, so one that hangs would hold up the rest. */
export const fetchTimeoutMs = 120_000

/**
 * What the textures may take on a device, in theoretical bytes: width ×
 * height × 4 with mipmaps (owner 2026-11-03). Every texture on the page
 * counts, as any device may draw any of them, and the traced arrays of every
 * device (reserveBudget). A texture that does not fit keeps its placeholder
 * until others are released.
 */
export const imageBudget = { bytes: 2 * 1024 ** 3 }

export const textureBytes = (width: number, height: number) => Math.ceil((width * height * 4 * 4) / 3)

/** A decoded image: an ImageBitmap in the browser. */
export type Picture = { readonly width: number; readonly height: number; close(): void }
/** One channel of an ORM: which picture, and which of its channels. */
export type Layer = { picture: Picture; channel: Channel }

const decodeOptions = { imageOrientation: 'from-image', premultiplyAlpha: 'none', colorSpaceConversion: 'none' } as const

/** How images are fetched, decoded and composed; tests stand in their own. */
export const imagePlatform = {
  async fetch(revision: string): Promise<Blob> {
    const file = builtinFile(revision)
    if (file) {
      const response = await authorizedFetch(builtinTextureURL(file))
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      return response.blob()
    }
    const ticket = await mediaTicket(revision, 'original', 'display')
    const response = await fetch(ticket.url, { credentials: 'same-origin' })
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    return response.blob()
  },
  /**
   * The original's pixels as stored, at most longSide on the long side: no
   * colour management, so normal and ORM data stay what they were written
   * as, and alpha not premultiplied.
   */
  async decode(blob: Blob): Promise<Picture> {
    const full = await createImageBitmap(blob, decodeOptions)
    const scale = longSide / Math.max(full.width, full.height)
    if (scale >= 1) return full
    try {
      return await createImageBitmap(full, { ...decodeOptions, resizeWidth: Math.max(1, Math.round(full.width * scale)), resizeHeight: Math.max(1, Math.round(full.height * scale)), resizeQuality: 'high' })
    } finally {
      full.close()
    }
  },
  /**
   * An ORM from up to three layers, ambient occlusion, roughness and
   * metalness into red, green and blue, each scaled to width × height; a
   * missing one is 1. Read as stored (pixelsOf): a 2D canvas would
   * premultiply, losing a layer's colour channels where its alpha is low,
   * as a metalness kept beside a smoothness in alpha is.
   */
  async compose(layers: readonly (Layer | null)[], width: number, height: number): Promise<Picture> {
    const out = new ImageData(width, height)
    out.data.fill(255)
    const read = new Map<Picture, Uint8Array | Uint8ClampedArray>()
    layers.forEach((layer, i) => {
      if (!layer) return
      let src = read.get(layer.picture)
      if (!src) {
        src = pixelsOf(layer.picture as ImageBitmap, width, height)
        read.set(layer.picture, src)
      }
      const c = 'rgba'.indexOf(layer.channel)
      for (let p = 0; p < width * height; p++) out.data[p * 4 + i] = src[p * 4 + c]
    })
    return createImageBitmap(out, { premultiplyAlpha: 'none', colorSpaceConversion: 'none' })
  },
  placeholder(rgba: readonly [number, number, number, number]): unknown {
    return new ImageData(new Uint8ClampedArray(rgba), 1, 1)
  },
}

let reader: WebGL2RenderingContext | null | undefined

/**
 * A picture's pixels scaled to width × height, rows from the top, as stored.
 * Read back from a WebGL2 texture, which nothing is drawn with (no shader:
 * drawing is WebGPU's, creative-scene-shading.test.ts), uploaded with
 * nothing premultiplied; scaled here as linear filtering would. A page
 * without WebGL2 reads through a 2D canvas, which premultiplies.
 */
function pixelsOf(picture: ImageBitmap, width: number, height: number): Uint8Array | Uint8ClampedArray {
  reader ??= typeof OffscreenCanvas === 'undefined' ? null : new OffscreenCanvas(1, 1).getContext('webgl2')
  const own = reader ? readBack(reader, picture) : null
  if (!own) {
    const ctx = new OffscreenCanvas(width, height).getContext('2d', { willReadFrequently: true })!
    ctx.drawImage(picture, 0, 0, width, height)
    return ctx.getImageData(0, 0, width, height).data
  }
  return picture.width === width && picture.height === height ? own : scaled(own, picture.width, picture.height, width, height)
}

/** The picture's own pixels, rows from the top: a texture's level 0 read through a framebuffer. */
function readBack(gl: WebGL2RenderingContext, picture: ImageBitmap): Uint8Array | null {
  const texture = gl.createTexture()
  const fb = gl.createFramebuffer()
  try {
    gl.bindTexture(gl.TEXTURE_2D, texture)
    gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, false)
    gl.pixelStorei(gl.UNPACK_COLORSPACE_CONVERSION_WEBGL, gl.NONE)
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false)
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, gl.RGBA, gl.UNSIGNED_BYTE, picture)
    gl.bindFramebuffer(gl.FRAMEBUFFER, fb)
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, texture, 0)
    if (gl.checkFramebufferStatus(gl.FRAMEBUFFER) !== gl.FRAMEBUFFER_COMPLETE) return null
    // The image's top row is the texture's first, which readPixels gives first.
    const pixels = new Uint8Array(picture.width * picture.height * 4)
    gl.readPixels(0, 0, picture.width, picture.height, gl.RGBA, gl.UNSIGNED_BYTE, pixels)
    return pixels
  } finally {
    gl.bindFramebuffer(gl.FRAMEBUFFER, null)
    gl.deleteFramebuffer(fb)
    gl.deleteTexture(texture)
  }
}

/** RGBA pixels w × h scaled to width × height, each sampled at its centre between the four nearest, edges clamped. */
export function scaled(src: ArrayLike<number>, w: number, h: number, width: number, height: number): Uint8Array {
  const out = new Uint8Array(width * height * 4)
  const at = (n: number, size: number, to: number) => {
    const t = Math.min(Math.max(((n + 0.5) * size) / to - 0.5, 0), size - 1)
    const i = Math.floor(t)
    return [i, Math.min(i + 1, size - 1), t - i] as const
  }
  const xs = Array.from({ length: width }, (_, x) => at(x, w, width))
  for (let y = 0; y < height; y++) {
    const [y0, y1, fy] = at(y, h, height)
    for (let x = 0; x < width; x++) {
      const [x0, x1, fx] = xs[x]
      for (let c = 0; c < 4; c++) {
        const top = src[(y0 * w + x0) * 4 + c] * (1 - fx) + src[(y0 * w + x1) * 4 + c] * fx
        const bottom = src[(y1 * w + x0) * 4 + c] * (1 - fx) + src[(y1 * w + x1) * 4 + c] * fx
        out[(y * width + x) * 4 + c] = Math.round(top * (1 - fy) + bottom * fy)
      }
    }
  }
  return out
}

const part = (r: MapRef | undefined) => (r ? `${r.content_revision_id}.${r.channel ?? 'r'}` : '-')

export const colorKey = (revision: string): TextureKey => `color:${revision}`
export const normalKey = (revision: string): TextureKey => `normal:${revision}`

/** The ORM of a slot's ambient occlusion, roughness and metalness maps; null when it has none of them. */
export function ormKey(ao: MapRef | undefined, roughness: MapRef | undefined, metalness: MapRef | undefined): TextureKey | null {
  return ao || roughness || metalness ? `orm:${part(ao)}|${part(roughness)}|${part(metalness)}` : null
}

/** Where a built-in texture's file is read (api: 读取内置贴图). */
export const builtinTextureURL = (file: string) => `/api/v1/creative/scene-textures/${encodeURIComponent(file)}`

/** The file a built-in source names; null for an image revision. */
function builtinFile(source: string): string | null {
  return source.startsWith('tex:') ? source.slice(4) : null
}

/** The built-in materials by the files they are made of, for what a failure says. */
const materialsOfFile = new Map<string, string[]>()
for (const m of texturedEntries) {
  for (const t of Object.values(m.maps ?? {})) {
    const names = materialsOfFile.get(t.file) ?? []
    if (!names.includes(m.label)) materialsOfFile.set(t.file, [...names, m.label])
  }
}

/**
 * The textures a principled slot samples, by what it samples them for. The
 * emission map is read as the base colour is, sRGB from the top row, so it
 * shares that texture when both name one image, as a glTF's often do.
 */
export function textureKeys(t: Texturing | undefined): { color?: TextureKey; normal?: TextureKey; orm?: TextureKey; emissive?: TextureKey } {
  const maps = t?.maps
  if (!maps) return {}
  const orm = ormKey(maps.ao, maps.roughness, maps.metalness)
  return {
    ...(maps.base_color ? { color: colorKey(maps.base_color.content_revision_id) } : {}),
    ...(maps.normal ? { normal: normalKey(maps.normal.content_revision_id) } : {}),
    ...(orm ? { orm } : {}),
    ...(maps.emissive ? { emissive: colorKey(maps.emissive.content_revision_id) } : {}),
  }
}

type Slots = NonNullable<Scene['objects'][number]['material_slots']>

/** Every texture these material slots sample, sorted: a principled slot's maps, and a built-in textured material's own. */
export function slotTextureKeys(slots: Iterable<Slots>): TextureKey[] {
  const keys = new Set<TextureKey>()
  for (const s of slots) {
    for (const slot of Object.values(s)) {
      const m = catalogMaterial(slot.preset, slot.params, slot)
      if (m?.kind === 'principled') for (const k of Object.values(textureKeys(m.texturing))) keys.add(k)
    }
  }
  return [...keys].sort()
}

/** Every texture the scene's material slots sample, sorted. */
export function sceneTextureKeys(scene: Pick<Scene, 'objects'>): TextureKey[] {
  return slotTextureKeys(scene.objects.map((o) => o.material_slots ?? {}))
}

function parse(key: TextureKey): { kind: Kind; parts: (Part | null)[] } {
  const at = key.indexOf(':')
  const kind = key.slice(0, at) as Kind
  const rest = key.slice(at + 1)
  if (kind !== 'orm') return { kind, parts: [{ revision: rest, channel: 'r' }] }
  return {
    kind,
    parts: rest.split('|').map((p) => {
      if (p === '-') return null
      const dot = p.lastIndexOf('.')
      return { revision: p.slice(0, dot), channel: p.slice(dot + 1) as Channel }
    }),
  }
}

/** The sources a texture is made of: image revisions, or built-in files as tex:<file>. */
export function textureSources(key: TextureKey): string[] {
  return [...new Set(parse(key).parts.flatMap((p) => (p ? [p.revision] : [])))]
}

/** One image's revisions used as they are: a base colour, a normal map, or a standard ORM (one image, r g b in order). */
function direct(kind: Kind, parts: readonly (Part | null)[]): boolean {
  if (kind !== 'orm') return true
  const [ao, rough, metal] = parts
  return !!ao && !!rough && !!metal && ao.revision === rough.revision && rough.revision === metal.revision && ao.channel === 'r' && rough.channel === 'g' && metal.channel === 'b'
}

const placeholderPixels: Record<Kind, readonly [number, number, number, number]> = {
  color: [255, 255, 255, 255],
  normal: [128, 128, 255, 255],
  orm: [255, 255, 255, 255],
}
const placeholders = new Map<Kind, unknown>()
const placeholderOf = (kind: Kind) => {
  if (!placeholders.has(kind)) placeholders.set(kind, imagePlatform.placeholder(placeholderPixels[kind]))
  return placeholders.get(kind)
}

// ---- sources

type Source = { refs: number; blob: Promise<Blob> | null; picture: Promise<Picture> | null; borrowers: number }
const sources = new Map<string, Source>()

/** Files the editor has itself, by revision: an import's images, just uploaded, are not downloaded again. */
const local = new Map<string, Blob>()

/** Uses blob for revision's original until forgetLocalImage (design「缩略图里的贴图」). */
export function provideLocalImage(revision: string, blob: Blob) {
  local.set(revision, blob)
}

export function forgetLocalImage(revision: string) {
  local.delete(revision)
}

let decoding = 0
const waiting: (() => void)[] = []
async function limited<T>(job: () => Promise<T>): Promise<T> {
  // A finished job hands its turn to the next in line rather than freeing it.
  if (decoding < decodeAtOnce) decoding++
  else await new Promise<void>((go) => waiting.push(go))
  try {
    return await job()
  } finally {
    const next = waiting.shift()
    if (next) next()
    else decoding--
  }
}

/** Why an image could not be used, naming it. */
class ImageError extends Error {}

/** p, or a failure saying why once ms have gone by first. */
function within<T>(ms: number, p: Promise<T>, why: string): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined
  const late = new Promise<never>((_, reject) => (timer = setTimeout(() => reject(new Error(why)), ms)))
  return Promise.race([p, late]).finally(() => clearTimeout(timer))
}

/** The revision's pixels, decoded if no texture has them now; give them back with giveBack. */
function borrow(revision: string): Promise<Picture> {
  const s = sources.get(revision)!
  s.borrowers++
  if (!s.picture) {
    const p = limited(async () => {
      const own = local.get(revision)
      s.blob ??= own ? Promise.resolve(own) : within(fetchTimeoutMs, imagePlatform.fetch(revision), '原图下载超时')
      return imagePlatform.decode(await s.blob)
    })
    s.picture = p
    // A failure is tried again the next time.
    p.catch(() => {
      if (s.picture === p) s.picture = null
      s.blob = null
    })
  }
  return s.picture.catch((err: unknown) => {
    // A built-in texture that does not come is a deployment problem: the photographer is told which material, not the HTTP status.
    const file = builtinFile(revision)
    if (file) throw new ImageError(`内置贴图${(materialsOfFile.get(file) ?? []).map((n) => `「${n}」`).join('')}加载失败`)
    throw new ImageError(`图片 ${revision} 加载失败（${err instanceof Error ? err.message : String(err)}）`)
  })
}

function giveBack(revision: string) {
  const s = sources.get(revision)
  if (!s) return
  s.borrowers--
  if (s.borrowers > 0) return
  // No texture wants it any more: it went while its pixels were coming (free).
  if (s.refs === 0) sources.delete(revision)
  if (!s.picture) return
  const p = s.picture
  s.picture = null
  p.then((x) => x.close(), () => {})
}

// ---- textures

type State = 'loading' | 'ready' | 'asleep' | 'failed' | 'over'

type Entry = {
  key: TextureKey
  kind: Kind
  parts: (Part | null)[]
  revisions: string[]
  texture: Texture
  refs: number
  holds: number
  timer: ReturnType<typeof setTimeout> | null
  state: State
  error: string | null
  /** Reserved in the budget; 0 until admitted. */
  bytes: number
  /** The image the texture shows: borrowed from its source, or a composite of its own. */
  shown: { picture: Picture; borrowed: boolean } | null
  /** A device uploaded the placeholder: the real image needs the texture rebuilt. */
  placeholderUploaded: boolean
  /** A device has the real image. */
  realUploaded: boolean
  /** Bumped by every load and by freeing: a load that finds it changed lets go of what it made. */
  generation: number
  waiters: { resolve: () => void; reject: (e: Error) => void }[]
}

const entries = new Map<TextureKey, Entry>()
let used = 0
/** Admission goes in the order textures were asked for, whatever order their images decode in. */
let turn: Promise<unknown> = Promise.resolve()

/** What the editor tells the photographer: textures whose image failed, and those over the budget, in the order asked. */
export const imageStatus = createStore<{ failed: Readonly<Record<TextureKey, string>>; over: readonly TextureKey[] }>(() => ({ failed: {}, over: [] }))

function publish() {
  const failed: Record<TextureKey, string> = {}
  const over: TextureKey[] = []
  for (const e of entries.values()) {
    if (e.state === 'failed') failed[e.key] = e.error!
    else if (e.state === 'over') over.push(e.key)
  }
  imageStatus.setState({ failed, over })
}

function setState(e: Entry, state: State, error: string | null = null) {
  const listed = (s: State) => s === 'failed' || s === 'over'
  const changed = listed(e.state) || listed(state)
  e.state = state
  e.error = error
  if (state === 'ready' || listed(state)) {
    for (const w of e.waiters.splice(0)) {
      if (state === 'ready') w.resolve()
      else w.reject(new ImageError(state === 'over' ? `图片 ${e.revisions.join('、')} 超出贴图显存预算` : error!))
    }
  }
  if (changed) publish()
}

function newEntry(key: TextureKey): Entry {
  const { kind, parts } = parse(key)
  const texture = new Texture(placeholderOf(kind) as HTMLImageElement)
  texture.name = key
  texture.wrapS = texture.wrapT = RepeatWrapping
  // Rows from the top, as glTF's UVs and ImageBitmap have them.
  texture.flipY = false
  texture.colorSpace = kind === 'color' ? SRGBColorSpace : NoColorSpace
  // Floors seen at a grazing angle stay sharp.
  texture.anisotropy = 8
  texture.needsUpdate = true
  const revisions = [...new Set(parts.flatMap((p) => (p ? [p.revision] : [])))]
  const e: Entry = { key, kind, parts, revisions, texture, refs: 0, holds: 0, timer: null, state: 'loading', error: null, bytes: 0, shown: null, placeholderUploaded: false, realUploaded: false, generation: 0, waiters: [] }
  texture.onUpdate = () => uploaded(e)
  for (const r of revisions) {
    const s = sources.get(r) ?? { refs: 0, blob: null, picture: null, borrowers: 0 }
    s.refs++
    sources.set(r, s)
  }
  return e
}

function load(e: Entry) {
  const gen = ++e.generation
  setState(e, 'loading')
  let lent = false
  const giveAll = () => {
    if (lent) return
    lent = true
    e.revisions.forEach(giveBack)
  }
  const decoded = Promise.all(e.revisions.map(borrow))
  const admitted = (turn = turn.then(() => decoded.then((pictures) => admit(e, gen, pictures), () => false)))
  void (async () => {
    try {
      const pictures = await decoded
      const fits = await admitted
      if (e.generation !== gen) return giveAll()
      if (!fits) {
        giveAll()
        return setState(e, 'over')
      }
      if (direct(e.kind, e.parts)) return show(e, { picture: pictures[0], borrowed: true })
      const { width, height } = sizeOf(pictures)
      const of = new Map(e.revisions.map((r, i) => [r, pictures[i]]))
      const picture = await imagePlatform.compose(e.parts.map((p) => p && { picture: of.get(p.revision)!, channel: p.channel }), width, height)
      giveAll()
      if (e.generation !== gen) return picture.close()
      show(e, { picture, borrowed: false })
    } catch (err) {
      giveAll()
      if (e.generation === gen) setState(e, 'failed', err instanceof ImageError ? err.message : `贴图 ${e.key} 加载失败（${String(err)}）`)
    }
  })()
}

/** An ORM is as large as its largest layer. */
function sizeOf(pictures: readonly Picture[]) {
  return { width: Math.max(...pictures.map((p) => p.width)), height: Math.max(...pictures.map((p) => p.height)) }
}

function admit(e: Entry, gen: number, pictures: readonly Picture[]): boolean {
  if (e.generation !== gen) return false
  if (e.bytes > 0) return true
  const { width, height } = sizeOf(pictures)
  const bytes = textureBytes(width, height)
  if (used + bytes > imageBudget.bytes) return false
  used += bytes
  e.bytes = bytes
  return true
}

const shownListeners = new Set<() => void>()

/** Calls back whenever a texture's image arrives: a view that draws on demand draws again. Returns the unsubscribe. */
export function onImageShown(listener: () => void): () => void {
  shownListeners.add(listener)
  return () => void shownListeners.delete(listener)
}

function show(e: Entry, shown: NonNullable<Entry['shown']>) {
  // three cannot upload a larger image into the 1×1 it made: dispose, and every device makes it again.
  if (e.placeholderUploaded) {
    e.texture.dispose()
    e.placeholderUploaded = false
    e.realUploaded = false
  }
  e.shown = shown
  e.texture.image = shown.picture
  // Kept while the pixels are let go: box projection sizes a tile by it (instances.ts).
  e.texture.userData.aspect = shown.picture.width / shown.picture.height
  setState(e, 'ready')
  for (const l of shownListeners) l()
}

function letGo(e: Entry) {
  const shown = e.shown
  e.shown = null
  if (!shown) return
  if (shown.borrowed) e.revisions.forEach(giveBack)
  else shown.picture.close()
}

function uploaded(e: Entry) {
  if (e.texture.image === placeholderOf(e.kind)) {
    e.placeholderUploaded = true
    // A device that came after the pixels were let go.
    if (e.state === 'asleep') load(e)
    return
  }
  e.realUploaded = true
  // After the frame: the device uploading now has its copy.
  if (e.holds === 0) setTimeout(() => sleep(e), 0)
}

/** Lets the pixels go once every device that needs them has them; the texture keeps showing on those devices. */
function sleep(e: Entry) {
  if (e.state !== 'ready' || e.holds > 0 || !e.realUploaded || entries.get(e.key) !== e) return
  e.texture.image = placeholderOf(e.kind) as HTMLImageElement
  letGo(e)
  setState(e, 'asleep')
}

function free(e: Entry) {
  entries.delete(e.key)
  e.generation++
  used -= e.bytes
  letGo(e)
  e.texture.dispose()
  for (const r of e.revisions) {
    const s = sources.get(r)
    // One still borrowed goes when given back, its pixels closed then.
    if (s && --s.refs === 0 && s.borrowers === 0) sources.delete(r)
  }
  if (e.state === 'failed' || e.state === 'over') publish()
  if (e.bytes > 0) readmit()
}

/** Room for those that did not fit, in the order they were asked for. */
function readmit() {
  for (const o of entries.values()) if (o.state === 'over') load(o)
}

/**
 * Bytes the page's other textures take from the image budget: the traced
 * arrays (render/trace/maps.ts), counted with the images as the design's
 * 「显存预算」 says. false when they do not fit; nothing is taken then.
 */
export function reserveBudget(bytes: number): boolean {
  if (used + bytes > imageBudget.bytes) return false
  used += bytes
  return true
}

/** Gives back what reserveBudget took: textures that did not fit try again. */
export function releaseBudget(bytes: number) {
  used -= bytes
  if (bytes > 0) readmit()
}

/** The texture for key, the same object every time; it shows a placeholder until its image is here. Release it with releaseTexture. */
export function acquireTexture(key: TextureKey): Texture {
  let e = entries.get(key)
  if (!e) {
    e = newEntry(key)
    entries.set(key, e)
    load(e)
  }
  e.refs++
  if (e.timer !== null) clearTimeout(e.timer)
  e.timer = null
  return e.texture
}

/** Freed a moment after the last user goes: switching back and forth does not load it again. */
export function releaseTexture(key: TextureKey) {
  const e = entries.get(key)
  if (!e) return
  e.refs = Math.max(0, e.refs - 1)
  if (e.refs > 0) return
  if (e.timer !== null) clearTimeout(e.timer)
  e.timer = setTimeout(() => {
    e.timer = null
    if (e.refs === 0 && entries.get(key) === e) free(e)
  }, releaseAfterMs)
}

function whenReady(e: Entry): Promise<void> {
  if (e.state === 'ready') return Promise.resolve()
  return new Promise((resolve, reject) => {
    e.waiters.push({ resolve, reject })
    // Pixels let go are decoded again; a failure is tried again, and one over the budget looks for room again.
    if (e.state !== 'loading') load(e)
  })
}

/** The scene's textures held with their pixels until release: a device mounted meanwhile uploads them. */
export type ImageHold = { release(): void }

/**
 * Resolves once every texture the scene's slots sample has its image, held
 * until released; an offscreen picture waits for this before it mounts
 * (design「等图片就绪」). Rejects, holding nothing, when any image failed
 * or did not fit the budget, saying which.
 */
export function loadSceneImages(scene: Pick<Scene, 'objects'>): Promise<ImageHold> {
  return holdImages(sceneTextureKeys(scene))
}

/** loadSceneImages for the slots of a picture outside a scene: a card's or preview's model. */
export function loadSlotImages(slots: Slots): Promise<ImageHold> {
  return holdImages(slotTextureKeys([slots]))
}

/** loadSceneImages for these textures: the world-space layer's, which copies each into its own (render/trace/maps.ts). */
export async function holdImages(keys: readonly TextureKey[]): Promise<ImageHold> {
  const held = keys.map((key) => {
    acquireTexture(key)
    const e = entries.get(key)!
    e.holds++
    return e
  })
  let released = false
  const hold: ImageHold = {
    release() {
      if (released) return
      released = true
      for (const e of held) {
        e.holds--
        sleep(e)
        releaseTexture(e.key)
      }
    },
  }
  const results = await Promise.allSettled(held.map(whenReady))
  const reasons = results.flatMap((r) => (r.status === 'rejected' ? [r.reason instanceof Error ? r.reason.message : String(r.reason)] : []))
  if (reasons.length === 0) return hold
  hold.release()
  throw new Error([...new Set(reasons)].join('；'))
}

/** Forgets every texture and source: tests only. */
export function resetImages() {
  for (const e of entries.values()) if (e.timer !== null) clearTimeout(e.timer)
  entries.clear()
  sources.clear()
  local.clear()
  placeholders.clear()
  used = 0
  turn = Promise.resolve()
  imageStatus.setState({ failed: {}, over: [] })
}

/** Bytes the textures hold in the budget: tests only. */
export const budgetUsed = () => used
