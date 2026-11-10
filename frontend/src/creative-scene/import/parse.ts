import { LoadingManager, type Group, type Material, type Object3D } from 'three'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'
import type { DRACOLoader } from 'three/examples/jsm/loaders/DRACOLoader.js'
import { MeshoptDecoder } from 'meshoptimizer'

// Reading a GLB the photographer picked (design「模块职责与协作」parse): the
// file's JSON is checked before any loader sees it, every image's own bytes are
// taken out as they are, and three parses a copy with the textures cut away, so
// it never fetches, decodes or re-encodes a picture. Draco and meshopt
// geometry is unpacked here; turning it into floats is convert.ts's.

/** Why a file cannot be imported; message is for the photographer. */
export type RejectReason = 'not_glb' | 'external_uri' | 'ktx2' | 'skinned' | 'image_format' | 'unreadable' | 'limit'

export class ImportRejected extends Error {
  readonly reason: RejectReason
  constructor(reason: RejectReason, message: string) {
    super(message)
    this.reason = reason
  }
}

export type TextureInfoJSON = { index: number; texCoord?: number; scale?: number; strength?: number; extensions?: Record<string, unknown> }

export type MaterialJSON = {
  name?: string
  pbrMetallicRoughness?: {
    baseColorFactor?: number[]
    metallicFactor?: number
    roughnessFactor?: number
    baseColorTexture?: TextureInfoJSON
    metallicRoughnessTexture?: TextureInfoJSON
  }
  normalTexture?: TextureInfoJSON
  occlusionTexture?: TextureInfoJSON
  emissiveTexture?: TextureInfoJSON
  emissiveFactor?: number[]
  alphaMode?: 'OPAQUE' | 'MASK' | 'BLEND'
  alphaCutoff?: number
  doubleSided?: boolean
  extensions?: Record<string, Record<string, unknown> | undefined>
}

export type PrimitiveJSON = {
  attributes: Record<string, number>
  indices?: number
  material?: number
  mode?: number
  targets?: unknown[]
  extensions?: Record<string, unknown>
}

/** The parts of a glTF document the import reads. */
export type GLTFJSON = {
  asset?: { version?: string; copyright?: string }
  scene?: number
  scenes?: { nodes?: number[] }[]
  nodes?: { name?: string; mesh?: number; skin?: number; children?: number[]; camera?: number; extensions?: Record<string, unknown> }[]
  meshes?: { name?: string; primitives: PrimitiveJSON[] }[]
  materials?: MaterialJSON[]
  textures?: { source?: number; sampler?: number; extensions?: Record<string, { source?: number } | undefined> }[]
  images?: { uri?: string; bufferView?: number; mimeType?: string; name?: string }[]
  samplers?: { wrapS?: number; wrapT?: number; magFilter?: number; minFilter?: number }[]
  buffers?: { uri?: string; byteLength: number }[]
  bufferViews?: { buffer: number; byteOffset?: number; byteLength: number; extensions?: Record<string, unknown> }[]
  accessors?: unknown[]
  skins?: unknown[]
  animations?: unknown[]
  cameras?: unknown[]
  extensionsUsed?: string[]
  extensionsRequired?: string[]
  extensions?: Record<string, unknown>
}

export type ImageMIME = 'image/png' | 'image/jpeg' | 'image/webp'

/** One of the file's images as stored in it, never decoded. */
export type SourceImage = { bytes: Uint8Array; mime: ImageMIME }

/** What three made of a node, mesh, primitive or material: the indices into the file's JSON. */
export type Association = { nodes?: number; meshes?: number; primitives?: number; materials?: number }

export type ParsedImport = {
  /** The file's JSON as it is, textures and all. */
  json: GLTFJSON
  /** The file's scene (json.scene, else the first), its materials without textures. */
  scene: Group
  associations: ReadonlyMap<Object3D | Material, Association>
  /** Each image by its index in json.images. */
  images: ReadonlyMap<number, SourceImage>
}

/** The loaders parse borrows; the Draco one is made per file and disposed after it. */
export type Decoders = { draco: () => DRACOLoader }

const glbMagic = 0x46546c67
const chunkJSON = 0x4e4f534a
const chunkBIN = 0x004e4942

const reexport = '请在 Blender 里重新导出为 GLB（格式选「glTF 二进制」）'

function readContainer(file: Uint8Array): { json: GLTFJSON; bin: Uint8Array | null } {
  const view = new DataView(file.buffer, file.byteOffset, file.byteLength)
  if (file.byteLength < 20 || view.getUint32(0, true) !== glbMagic) throw new ImportRejected('not_glb', `这不是 GLB 文件，${reexport}`)
  if (view.getUint32(4, true) !== 2) throw new ImportRejected('not_glb', `只支持 glTF 2.0 的 GLB，${reexport}`)
  const total = Math.min(view.getUint32(8, true), file.byteLength)
  let json: GLTFJSON | null = null
  let bin: Uint8Array | null = null
  for (let at = 12; at + 8 <= total; ) {
    const length = view.getUint32(at, true)
    const type = view.getUint32(at + 4, true)
    const body = file.subarray(at + 8, at + 8 + length)
    if (body.byteLength !== length) break
    if (type === chunkJSON && json === null) {
      try {
        json = JSON.parse(new TextDecoder().decode(body)) as GLTFJSON
      } catch {
        throw new ImportRejected('unreadable', '文件里的 glTF 数据损坏，无法读取')
      }
    } else if (type === chunkBIN && bin === null) {
      bin = body
    }
    at += 8 + length
  }
  if (json === null || typeof json !== 'object') throw new ImportRejected('unreadable', '文件里的 glTF 数据损坏，无法读取')
  return { json, bin }
}

const isData = (uri: string) => /^data:/i.test(uri)

/**
 * Everything refused before a loader runs (Epic「导入的材质换算」资源位置, KTX2,
 * skins): a uri other than a data: URL would be fetched with the page's
 * cookies, and the rest cannot be stored.
 */
function precheck(json: GLTFJSON): void {
  const external = [...(json.buffers ?? []), ...(json.images ?? [])].find((r) => r.uri !== undefined && !isData(r.uri))
  if (external?.uri !== undefined) {
    throw new ImportRejected('external_uri', `模型引用了外部文件 ${external.uri}，只能导入资源都内嵌的 GLB。${reexport}，不要选「分离」`)
  }
  const basis = (json.extensionsUsed ?? []).includes('KHR_texture_basisu') || (json.images ?? []).some((i) => i.mimeType === 'image/ktx2' || (i.uri !== undefined && /^data:image\/ktx2/i.test(i.uri)))
  if (basis) throw new ImportRejected('ktx2', '模型的贴图是 KTX2 / Basis 压缩格式，暂不支持。请在 Blender 里把贴图换成 PNG 或 JPEG 再导出')
  const skinned =
    (json.skins?.length ?? 0) > 0 ||
    (json.nodes ?? []).some((n) => n.skin !== undefined) ||
    (json.meshes ?? []).some((m) => m.primitives.some((p) => Object.keys(p.attributes).some((a) => /^(JOINTS|WEIGHTS)_/.test(a))))
  if (skinned) throw new ImportRejected('skinned', '模型带骨骼蒙皮，带骨骼的角色要等后续版本支持')
}

function decodeDataURL(uri: string): { bytes: Uint8Array; type: string } {
  const comma = uri.indexOf(',')
  const head = uri.slice(5, comma < 0 ? undefined : comma)
  const body = comma < 0 ? '' : uri.slice(comma + 1)
  const type = head.split(';')[0]!.toLowerCase()
  if (/;base64$/i.test(head)) {
    const raw = atob(body)
    const bytes = new Uint8Array(raw.length)
    for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i)
    return { bytes, type }
  }
  return { bytes: new TextEncoder().encode(decodeURIComponent(body)), type }
}

/** The format the bytes start as, whatever the file says. */
export function sniff(bytes: Uint8Array): ImageMIME | null {
  const at = (i: number, s: number[]) => s.every((b, k) => bytes[i + k] === b)
  if (at(0, [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])) return 'image/png'
  if (at(0, [0xff, 0xd8, 0xff])) return 'image/jpeg'
  if (at(0, [0x52, 0x49, 0x46, 0x46]) && at(8, [0x57, 0x45, 0x42, 0x50])) return 'image/webp'
  return null
}

function extractImages(json: GLTFJSON, bin: Uint8Array | null): Map<number, SourceImage> {
  const buffers = new Map<number, Uint8Array>()
  const buffer = (i: number): Uint8Array => {
    let b = buffers.get(i)
    if (!b) {
      const def = json.buffers?.[i]
      b = def?.uri !== undefined ? decodeDataURL(def.uri).bytes : i === 0 ? bin ?? undefined : undefined
      if (!b) throw new ImportRejected('unreadable', `缓冲 ${i} 缺失，无法读取`)
      buffers.set(i, b)
    }
    return b
  }
  const out = new Map<number, SourceImage>()
  json.images?.forEach((image, i) => {
    let bytes: Uint8Array
    let declared: string | undefined = image.mimeType
    if (image.uri !== undefined) {
      const data = decodeDataURL(image.uri)
      bytes = data.bytes
      declared = data.type || declared
    } else {
      const view = image.bufferView === undefined ? undefined : json.bufferViews?.[image.bufferView]
      if (!view) throw new ImportRejected('unreadable', `图片 ${image.name ?? i} 没有数据`)
      const offset = view.byteOffset ?? 0
      bytes = buffer(view.buffer).subarray(offset, offset + view.byteLength)
      if (bytes.byteLength !== view.byteLength) throw new ImportRejected('unreadable', `图片 ${image.name ?? i} 的数据不完整`)
    }
    const actual = sniff(bytes)
    if (actual === null || (declared !== undefined && declared !== '' && declared !== actual && !(declared === 'image/jpg' && actual === 'image/jpeg'))) {
      throw new ImportRejected('image_format', `图片 ${image.name ?? i} 不是 PNG、JPEG 或 WebP，暂不支持。请在 Blender 里换成 PNG 或 JPEG 再导出`)
    }
    out.set(i, { bytes, mime: actual })
  })
  return out
}

/** Drops every texture reference (glTF names them all *Texture) so three loads the materials alone. */
function withoutTextures(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(withoutTextures)
  if (value === null || typeof value !== 'object') return value
  const out: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(value)) if (!/Texture$/.test(k)) out[k] = withoutTextures(v)
  return out
}

const textureExtensions = new Set(['KHR_texture_transform', 'EXT_texture_webp', 'EXT_texture_avif'])

function strip(json: GLTFJSON): GLTFJSON {
  const keep = (names?: string[]) => names?.filter((n) => !textureExtensions.has(n))
  // A material extension left out of extensionsUsed would crash three's loader; listed, it is read as the file meant.
  const used = new Set([...(keep(json.extensionsUsed) ?? []), ...(json.materials ?? []).flatMap((m) => Object.keys(m.extensions ?? {}))])
  const out: GLTFJSON = { ...json, materials: json.materials?.map((m) => withoutTextures(m) as MaterialJSON), extensionsUsed: [...used], extensionsRequired: keep(json.extensionsRequired) }
  delete out.images
  delete out.textures
  delete out.samplers
  return out
}

function writeContainer(json: GLTFJSON, bin: Uint8Array | null): ArrayBuffer {
  const text = new TextEncoder().encode(JSON.stringify(json))
  const jsonLength = (text.byteLength + 3) & ~3
  const binLength = bin ? (bin.byteLength + 3) & ~3 : 0
  const total = 12 + 8 + jsonLength + (bin ? 8 + binLength : 0)
  const out = new Uint8Array(total)
  const view = new DataView(out.buffer)
  view.setUint32(0, glbMagic, true)
  view.setUint32(4, 2, true)
  view.setUint32(8, total, true)
  view.setUint32(12, jsonLength, true)
  view.setUint32(16, chunkJSON, true)
  out.set(text, 20)
  out.fill(0x20, 20 + text.byteLength, 20 + jsonLength)
  if (bin) {
    view.setUint32(20 + jsonLength, binLength, true)
    view.setUint32(24 + jsonLength, chunkBIN, true)
    out.set(bin, 28 + jsonLength)
  }
  return out.buffer
}

const allowed = /^(blob|data):/i

export async function parseImport(file: ArrayBuffer, decoders: Decoders): Promise<ParsedImport> {
  const { json, bin } = readContainer(new Uint8Array(file))
  precheck(json)
  const images = extractImages(json, bin)
  // The second line, as sandbox/gltf.ts: nothing left in the file is fetched.
  let blocked: string | null = null
  const manager = new LoadingManager()
  manager.setURLModifier((url) => {
    if (allowed.test(url)) return url
    blocked ??= url
    return 'data:,'
  })
  const loader = new GLTFLoader(manager)
  const used = json.extensionsUsed ?? []
  const draco = used.includes('KHR_draco_mesh_compression') ? decoders.draco() : null
  if (draco) loader.setDRACOLoader(draco)
  if (used.includes('EXT_meshopt_compression')) {
    await MeshoptDecoder.ready
    loader.setMeshoptDecoder(MeshoptDecoder)
  }
  try {
    const gltf = await loader.parseAsync(writeContainer(strip(json), bin), '')
    if (blocked !== null) throw new ImportRejected('external_uri', `模型引用了外部文件 ${blocked}，只能导入资源都内嵌的 GLB`)
    return { json, scene: gltf.scene, associations: gltf.parser.associations as Map<Object3D | Material, Association>, images }
  } catch (e) {
    if (e instanceof ImportRejected) throw e
    throw new ImportRejected('unreadable', `无法解析这个 GLB：${e instanceof Error ? e.message : String(e)}`)
  } finally {
    draco?.dispose()
  }
}
