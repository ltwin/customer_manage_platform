import { LoadingManager, type Group, type Material, type Object3D } from 'three'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'
import type { DRACOLoader } from 'three/examples/jsm/loaders/DRACOLoader.js'
import { MeshoptDecoder } from 'meshoptimizer'
import { detectRig, skeletonOf, type Detection, type FileSkeleton } from '../rig/detect.ts'

// Reading a GLB the photographer picked (design「模块职责与协作」parse): the
// file's JSON is checked before any loader sees it, every image's own bytes are
// taken out as they are, and three parses a copy with the textures cut away, so
// it never fetches, decodes or re-encodes a picture. Draco and meshopt
// geometry is unpacked here; turning it into floats is convert.ts's.

/** Why a file cannot be imported; message is for the photographer. */
export type RejectReason = 'not_glb' | 'external_uri' | 'ktx2' | 'skin' | 'image_format' | 'unreadable' | 'limit'

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

export type AccessorJSON = { bufferView?: number; byteOffset?: number; componentType: number; normalized?: boolean; count: number; type: string; sparse?: unknown }

/** The parts of a glTF document the import reads. */
export type GLTFJSON = {
  asset?: { version?: string; copyright?: string }
  scene?: number
  scenes?: { nodes?: number[] }[]
  nodes?: { name?: string; mesh?: number; skin?: number; children?: number[]; camera?: number; extensions?: Record<string, unknown>; extras?: Record<string, unknown> }[]
  meshes?: { name?: string; primitives: PrimitiveJSON[] }[]
  materials?: MaterialJSON[]
  textures?: { source?: number; sampler?: number; extensions?: Record<string, { source?: number } | undefined> }[]
  images?: { uri?: string; bufferView?: number; mimeType?: string; name?: string }[]
  samplers?: { wrapS?: number; wrapT?: number; magFilter?: number; minFilter?: number }[]
  buffers?: { uri?: string; byteLength: number }[]
  bufferViews?: { buffer: number; byteOffset?: number; byteLength: number; byteStride?: number; extensions?: Record<string, unknown> }[]
  accessors?: AccessorJSON[]
  skins?: { joints: number[]; inverseBindMatrices?: number; skeleton?: number }[]
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

/**
 * A primitive's bone influences when it has more than one set (JOINTS_1 …):
 * read from the file's own accessors, as three's loader reads the first set
 * only and normalises it alone. Every set merged, each vertex keeps its four
 * largest, normalised (design「导入时保留骨骼并回写节点下标」).
 */
export type Influences = {
  /** Every set merged, for a primitive with more than one; null with one, which the loader reads right. */
  merged: { joints: Uint16Array; weights: Float32Array } | null
  /** Vertices that had more than four. */
  trimmed: number
  /** Vertices without any weight: the loader binds them wholly to their first joint. */
  unweighted: number
}

/** The file's skeleton and what was found on it, by the file's node indices. */
export type ImportRig = {
  skeleton: FileSkeleton
  detection: Detection
  /** File node index → the object three made of it. */
  objects: ReadonlyMap<number, Object3D>
}

export type ParsedImport = {
  /** The file's JSON as it is, textures and all. */
  json: GLTFJSON
  /** The file's scene (json.scene, else the first), its materials without textures. */
  scene: Group
  associations: ReadonlyMap<Object3D | Material, Association>
  /** Each image by its index in json.images. */
  images: ReadonlyMap<number, SourceImage>
  /** Null for a file without skins. */
  rig: ImportRig | null
  /** By `<mesh>/<primitive>`, each skinned primitive whose influences could be read from the file. */
  influences: ReadonlyMap<string, Influences>
}

/** The loaders parse borrows; the Draco one is made per file and disposed after it. */
export type Decoders = { draco: () => DRACOLoader }

const glbMagic = 0x46546c67
const chunkJSON = 0x4e4f534a
const chunkBIN = 0x004e4942

const reexport = '请在 Blender 里重新导出为 GLB（格式选「glTF 二进制」）'

export function readContainer(file: Uint8Array): { json: GLTFJSON; bin: Uint8Array | null } {
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
  const joints = new Set<number>()
  for (const skin of json.skins ?? []) {
    if (skin.joints.length > maxJointsPerSkin) throw new ImportRejected('limit', `模型的一副骨架有 ${skin.joints.length} 根骨骼，最多 ${maxJointsPerSkin} 根`)
    for (const j of skin.joints) joints.add(j)
  }
  if (joints.size > maxJoints) throw new ImportRejected('limit', `模型共有 ${joints.size} 根骨骼，最多 ${maxJoints} 根`)
}

// The skin subset's limits (data: 模型内容「接受的 GLB 子集」).
const maxJointsPerSkin = 256
const maxJoints = 512

/** A buffer of the file by index: the binary chunk for a buffer without a uri, else its data: URL. */
function buffers(json: GLTFJSON, bin: Uint8Array | null): (i: number) => Uint8Array {
  const cache = new Map<number, Uint8Array>()
  return (i) => {
    let b = cache.get(i)
    if (!b) {
      const def = json.buffers?.[i]
      b = def?.uri !== undefined ? decodeDataURL(def.uri).bytes : i === 0 ? bin ?? undefined : undefined
      if (!b) throw new ImportRejected('unreadable', `缓冲 ${i} 缺失，无法读取`)
      cache.set(i, b)
    }
    return b
  }
}

const componentBytes = new Map([
  [5121, 1],
  [5123, 2],
  [5125, 4],
  [5126, 4],
])

/** An accessor's values as floats, glTF's normalisation applied; null when they cannot be read here (sparse or compressed). */
function readAccessor(json: GLTFJSON, buffer: (i: number) => Uint8Array, index: number): Float32Array | null {
  const a = json.accessors?.[index]
  const view = a?.bufferView === undefined ? undefined : json.bufferViews?.[a.bufferView]
  const size = componentBytes.get(a?.componentType ?? 0)
  const n = { SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4 }[a?.type ?? ''] ?? 0
  if (!a || !view || !size || n === 0 || a.sparse !== undefined || view.extensions?.EXT_meshopt_compression || view.extensions?.KHR_meshopt_compression) return null
  const bytes = buffer(view.buffer)
  const data = new DataView(bytes.buffer, bytes.byteOffset + (view.byteOffset ?? 0), view.byteLength)
  const stride = view.byteStride ?? size * n
  const out = new Float32Array(a.count * n)
  const scale = a.normalized ? { 5121: 255, 5123: 65535, 5125: 4294967295 }[a.componentType as 5121] ?? 1 : 1
  for (let i = 0; i < a.count; i++) {
    for (let c = 0; c < n; c++) {
      const at = (a.byteOffset ?? 0) + i * stride + c * size
      if (at + size > data.byteLength) throw new ImportRejected('unreadable', `访问器 ${index} 的数据不完整`)
      const v = a.componentType === 5126 ? data.getFloat32(at, true) : a.componentType === 5125 ? data.getUint32(at, true) : a.componentType === 5123 ? data.getUint16(at, true) : data.getUint8(at)
      out[i * n + c] = v / scale
    }
  }
  return out
}

/**
 * Every skinned primitive's influences as the file has them, where they can
 * be read here: several sets merged, the four largest kept, normalised; and
 * the vertices left without weight. Several compressed sets cannot be read.
 */
function readInfluences(json: GLTFJSON, bin: Uint8Array | null): Map<string, Influences> {
  const buffer = buffers(json, bin)
  const out = new Map<string, Influences>()
  json.meshes?.forEach((mesh, m) =>
    mesh.primitives.forEach((p, k) => {
      const sets: number[] = []
      for (let s = 0; p.attributes[`JOINTS_${s}`] !== undefined && p.attributes[`WEIGHTS_${s}`] !== undefined; s++) sets.push(s)
      if (sets.length === 0) return
      const compressed = p.extensions?.KHR_draco_mesh_compression !== undefined
      const read = compressed ? [] : sets.map((s) => [readAccessor(json, buffer, p.attributes[`JOINTS_${s}`]!), readAccessor(json, buffer, p.attributes[`WEIGHTS_${s}`]!)] as const)
      if (compressed || read.some(([j, w]) => !j || !w)) {
        if (sets.length > 1) throw new ImportRejected('skin', `网格 ${mesh.name ?? m} 的每个顶点受超过 4 根骨骼影响，又经过压缩，无法读取。请在 Blender 里取消压缩后导出`)
        return
      }
      const count = read[0]![1]!.length / 4
      const joints = new Uint16Array(count * 4)
      const weights = new Float32Array(count * 4)
      const all: [number, number][] = []
      let trimmed = 0
      let unweighted = 0
      for (let v = 0; v < count; v++) {
        all.length = 0
        for (const [j, w] of read) for (let c = 0; c < 4; c++) if (w![v * 4 + c]! > 0) all.push([j![v * 4 + c]!, w![v * 4 + c]!])
        if (all.length === 0) unweighted++
        if (all.length > 4) trimmed++
        all.sort((x, y) => y[1] - x[1])
        const top = all.slice(0, 4)
        const sum = top.reduce((t, [, w]) => t + w, 0)
        top.forEach(([j, w], c) => {
          joints[v * 4 + c] = j
          weights[v * 4 + c] = w / sum
        })
        // As the loader does: no weight at all is all of it on the first joint.
        if (top.length === 0) {
          joints[v * 4] = read[0]![0]![v * 4]!
          weights[v * 4] = 1
        }
      }
      out.set(`${m}/${k}`, { merged: sets.length > 1 ? { joints, weights } : null, trimmed, unweighted })
    }),
  )
  return out
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
  const buffer = buffers(json, bin)
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

export function writeContainer(json: GLTFJSON, bin: Uint8Array | null): ArrayBuffer {
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

function rigOf(json: GLTFJSON, associations: ReadonlyMap<Object3D | Material, Association | undefined>): ImportRig | null {
  const skeleton = skeletonOf(json)
  if (!skeleton) return null
  const objects = new Map<number, Object3D>()
  for (const [o, a] of associations) if (a?.nodes !== undefined && 'isObject3D' in o) objects.set(a.nodes, o)
  return { skeleton, detection: detectRig(json), objects }
}

export async function parseImport(file: ArrayBuffer, decoders: Decoders): Promise<ParsedImport> {
  const { json, bin } = readContainer(new Uint8Array(file))
  precheck(json)
  const images = extractImages(json, bin)
  const influences = readInfluences(json, bin)
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
    const associations = gltf.parser.associations as Map<Object3D | Material, Association | undefined>
    return { json, scene: gltf.scene, associations: associations as Map<Object3D | Material, Association>, images, rig: rigOf(json, associations), influences }
  } catch (e) {
    if (e instanceof ImportRejected) throw e
    throw new ImportRejected('unreadable', `无法解析这个 GLB：${e instanceof Error ? e.message : String(e)}`)
  } finally {
    draco?.dispose()
  }
}
