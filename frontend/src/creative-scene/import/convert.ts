import {
  Box3,
  BufferGeometry,
  Color,
  Float32BufferAttribute,
  InstancedMesh,
  LinearSRGBColorSpace,
  Matrix4,
  Mesh,
  MeshStandardMaterial,
  SRGBColorSpace,
  Vector3,
  type BufferAttribute,
  type Group,
  type InterleavedBufferAttribute,
  type Material,
  type Object3D,
} from 'three'
import type { components } from '../../api/schema'
import { materialEntry, principledID } from '../materials/catalog.ts'
import { ImportRejected, type Association, type GLTFJSON, type MaterialJSON, type ParsedImport, type TextureInfoJSON } from './parse.ts'

// The file's scene and materials as the stored model and its slots (Epic
// 「导入的材质换算」, which has the rules row by row; design「换算的实现要点」).
// Pure apart from rewriting the parsed scene in place: the geometry ends up
// all floats, each mesh drawn by a bare material named slot:<id>, nothing left
// that the static subset refuses. What changed or was dropped goes in the report.

type SlotJSON = components['schemas']['CreativeSceneMaterialSlot']
type ParamValue = number | string | boolean

export type MapName = 'base_color' | 'normal' | 'roughness' | 'metalness' | 'ao' | 'emissive'

/** A map as an image of the file (its index in json.images) until the image is uploaded. */
export type MapRef = { image: number; channel?: 'r' | 'g' | 'b' }

/**
 * What an image is used for, which decides how it is re-encoded if too large
 * (images.ts): colour (base colour, emission) or data (normal, ORM).
 */
export type ImageUse = 'color' | 'data'

export type ConvertedSlot = {
  params: Record<string, ParamValue>
  maps: Partial<Record<MapName, MapRef>>
  normalStrength: number
  alphaMode: 'opaque' | 'clip' | 'blend'
  alphaCutoff: number
}

/** One line of the report the dialog shows; slot null for the whole file. */
export type ReportItem = { slot: string | null; level: 'changed' | 'ignored'; text: string }

export type Conversion = {
  /** The scene to write (write.ts), its materials slot:<id>. */
  scene: Group
  slots: ReadonlyMap<string, ConvertedSlot>
  /** Every image a slot uses, and the strictest use it has. */
  images: ReadonlyMap<number, ImageUse>
  report: readonly ReportItem[]
}

export type ConvertOptions = { originToBottomCenter: boolean }

const maxSlots = 32
const namePattern = /^[A-Za-z0-9_-]{1,64}$/
const repeatWrap = 10497
const wrapNames: Record<number, string> = { 33648: '镜像', 33071: '钳制' }
const kept = new Set(['position', 'normal', 'tangent', 'uv', 'uv1', 'color'])

/** The slot IDs of the file's materials in order; a primitive without one draws as `default`. */
export function slotNames(materials: readonly (MaterialJSON | undefined)[], used: ReadonlySet<number | null>): Map<number | null, string> {
  const taken = new Set<string>()
  const out = new Map<number | null, string>()
  const claim = (base: string) => {
    let id = base
    for (let n = 2; taken.has(id); n++) id = `${base.slice(0, 64 - `_${n}`.length)}_${n}`
    taken.add(id)
    return id
  }
  if (used.has(null)) out.set(null, claim('default'))
  materials.forEach((m, i) => {
    if (!used.has(i)) return
    const name = (m?.name ?? '').replace(/[^A-Za-z0-9_-]/g, '_').slice(0, 64)
    out.set(i, claim(name === '' ? `material_${i}` : name))
  })
  return out
}

/** glTF's linear colour factor as principled's sRGB hex. */
export function srgbHex(rgb: readonly number[] | undefined, fallback: readonly number[]): string {
  const [r, g, b] = rgb ?? fallback
  return `#${new Color().setRGB(r!, g!, b!, LinearSRGBColorSpace).getHexString(SRGBColorSpace)}`
}

type Transform = { offset: [number, number]; rotation: number; scale: [number, number] }
const identity: Transform = { offset: [0, 0], rotation: 0, scale: [1, 1] }

function transformOf(info: TextureInfoJSON): Transform & { texCoord?: number } {
  const t = info.extensions?.KHR_texture_transform as { offset?: [number, number]; rotation?: number; scale?: [number, number]; texCoord?: number } | undefined
  return { offset: t?.offset ?? [0, 0], rotation: t?.rotation ?? 0, scale: t?.scale ?? [1, 1], texCoord: t?.texCoord }
}

const sameTransform = (a: Transform, b: Transform) =>
  a.offset[0] === b.offset[0] && a.offset[1] === b.offset[1] && a.rotation === b.rotation && a.scale[0] === b.scale[0] && a.scale[1] === b.scale[1]

/** uv' = T · R · S · uv, glTF's KHR_texture_transform. */
export function applyTransform(t: Transform, u: number, v: number): [number, number] {
  const c = Math.cos(t.rotation)
  const s = Math.sin(t.rotation)
  const su = t.scale[0] * u
  const sv = t.scale[1] * v
  return [c * su + s * sv + t.offset[0], -s * su + c * sv + t.offset[1]]
}

/** A float, non-interleaved copy of a, as glTF's dequantisation reads it (three applies normalized). */
function floats(a: BufferAttribute | InterleavedBufferAttribute): Float32BufferAttribute {
  const out = new Float32Array(a.count * a.itemSize)
  for (let i = 0; i < a.count; i++) for (let c = 0; c < a.itemSize; c++) out[i * a.itemSize + c] = a.getComponent(i, c)
  return new Float32BufferAttribute(out, a.itemSize)
}

const isFloat = (a: BufferAttribute | InterleavedBufferAttribute) => !('isInterleavedBufferAttribute' in a && a.isInterleavedBufferAttribute) && a.array instanceof Float32Array && !a.normalized

/** Every attribute the store keeps as plain floats; the rest dropped (reported by the caller). */
export function dequantize(g: BufferGeometry): string[] {
  const dropped: string[] = []
  for (const [name, a] of Object.entries(g.attributes)) {
    if (!kept.has(name)) {
      g.deleteAttribute(name)
      dropped.push(name)
    } else if (!isFloat(a)) {
      g.setAttribute(name, floats(a))
    }
  }
  if (Object.keys(g.morphAttributes).length > 0) {
    g.morphAttributes = {}
    dropped.push('morph')
  }
  return dropped
}

type TextureRow = { map: MapName | 'orm'; label: string; info: TextureInfoJSON | undefined }

// Constants of the material extensions that principled has, by parameter.
type Ext = Record<string, unknown> | undefined
const extensionLabels: Record<string, string> = {
  KHR_materials_transmission: '透射',
  KHR_materials_volume: '体积',
  KHR_materials_clearcoat: '涂层',
  KHR_materials_sheen: '光泽',
  KHR_materials_specular: '高光',
  KHR_materials_iridescence: '薄膜',
  KHR_materials_anisotropy: '各向异性',
}
const handled = new Set([...Object.keys(extensionLabels), 'KHR_materials_ior', 'KHR_materials_emissive_strength', 'KHR_materials_unlit'])

class Reporter {
  readonly items: ReportItem[] = []
  private readonly seen = new Set<string>()
  add(slot: string | null, level: ReportItem['level'], text: string) {
    const key = `${slot}\u0000${text}`
    if (this.seen.has(key)) return
    this.seen.add(key)
    this.items.push({ slot, level, text })
  }
}

/** Principled's parameters and maps for one glTF material. */
function convertMaterial(json: GLTFJSON, m: MaterialJSON | undefined, slot: string, report: Reporter): { slot: ConvertedSlot; transform: Transform; textures: TextureRow[] } {
  const spec = materialEntry(principledID)!.params
  const params: Record<string, ParamValue> = {}
  const set = (k: string, v: number, label: string) => {
    const { min, max } = spec[k] as { min?: number; max?: number }
    const clamped = Math.min(max ?? Infinity, Math.max(min ?? -Infinity, v))
    if (clamped !== v) report.add(slot, 'changed', `${label} ${round(v)} 超出 ${min}–${max}，已限制为 ${round(clamped)}`)
    params[k] = clamped
  }
  const pbr = m?.pbrMetallicRoughness ?? {}
  const base = pbr.baseColorFactor ?? [1, 1, 1, 1]
  params.color = srgbHex(base, [1, 1, 1])
  set('opacity', base[3] ?? 1, '不透明度')
  set('metalness', pbr.metallicFactor ?? 1, '金属度')
  set('roughness', pbr.roughnessFactor ?? 1, '糙度')
  params.emissive = srgbHex(m?.emissiveFactor, [0, 0, 0])
  const ext = (name: string): Ext => m?.extensions?.[name]
  const num = (e: Ext, k: string, d: number) => (typeof e?.[k] === 'number' ? (e[k] as number) : d)
  const strength = ext('KHR_materials_emissive_strength')
  if (strength) set('emissive_intensity', num(strength, 'emissiveStrength', 1), '自发光强度')
  const ior = ext('KHR_materials_ior')
  if (ior) set('ior', num(ior, 'ior', 1.5), 'IOR')
  const transmission = ext('KHR_materials_transmission')
  if (transmission) set('transmission', num(transmission, 'transmissionFactor', 0), '透射')
  const volume = ext('KHR_materials_volume')
  if (volume) set('thickness', num(volume, 'thicknessFactor', 0), '厚度')
  const clearcoat = ext('KHR_materials_clearcoat')
  if (clearcoat) {
    set('clearcoat', num(clearcoat, 'clearcoatFactor', 0), '涂层')
    set('clearcoat_roughness', num(clearcoat, 'clearcoatRoughnessFactor', 0), '涂层糙度')
  }
  const sheen = ext('KHR_materials_sheen')
  if (sheen) {
    // As three's loader: the extension's presence is the full sheen layer.
    set('sheen', 1, '光泽')
    params.sheen_color = srgbHex(sheen.sheenColorFactor as number[] | undefined, [0, 0, 0])
    set('sheen_roughness', num(sheen, 'sheenRoughnessFactor', 0), '光泽糙度')
  }
  const specular = ext('KHR_materials_specular')
  if (specular) {
    set('specular_intensity', num(specular, 'specularFactor', 1), '高光')
    params.specular_color = srgbHex(specular.specularColorFactor as number[] | undefined, [1, 1, 1])
  }
  const iridescence = ext('KHR_materials_iridescence')
  if (iridescence) {
    set('iridescence', num(iridescence, 'iridescenceFactor', 0), '薄膜')
    set('iridescence_thickness', num(iridescence, 'iridescenceThicknessMaximum', 400), '薄膜厚度')
  }
  const anisotropy = ext('KHR_materials_anisotropy')
  if (anisotropy) {
    set('anisotropy', num(anisotropy, 'anisotropyStrength', 0), '各向异性')
    set('anisotropy_rotation', num(anisotropy, 'anisotropyRotation', 0), '各向异性旋转')
  }
  for (const [name, e] of Object.entries(m?.extensions ?? {})) {
    if (extensionLabels[name] && Object.keys(e ?? {}).some((k) => k.endsWith('Texture'))) report.add(slot, 'ignored', `${extensionLabels[name]}贴图不支持，已忽略`)
    if (!handled.has(name)) report.add(slot, 'ignored', `材质扩展 ${name} 不支持，已忽略`)
  }
  if (ext('KHR_materials_unlit')) report.add(slot, 'changed', '原本是不受光照的材质（unlit），导入后按普通材质受光照显示')

  const alphaMode = m?.alphaMode === 'MASK' ? 'clip' : m?.alphaMode === 'BLEND' ? 'blend' : 'opaque'
  const textures: TextureRow[] = [
    { map: 'base_color', label: '基础色贴图', info: pbr.baseColorTexture },
    { map: 'orm', label: '金属度 / 糙度贴图', info: pbr.metallicRoughnessTexture },
    { map: 'normal', label: '法线贴图', info: m?.normalTexture },
    { map: 'ao', label: 'AO 贴图', info: m?.occlusionTexture },
    { map: 'emissive', label: '自发光贴图', info: m?.emissiveTexture },
  ]
  const maps: Partial<Record<MapName, MapRef>> = {}
  const live: TextureRow[] = []
  for (const row of textures) {
    if (!row.info) continue
    const t = transformOf(row.info)
    if ((t.texCoord ?? row.info.texCoord ?? 0) !== 0) {
      report.add(slot, 'ignored', `${row.label}用的是第二套 UV，已忽略`)
      continue
    }
    const image = imageOf(json, row.info.index)
    if (image === null) {
      report.add(slot, 'ignored', `${row.label}的图片格式不支持，已忽略`)
      continue
    }
    live.push(row)
    if (row.map === 'orm') {
      maps.roughness = { image, channel: 'g' }
      maps.metalness = { image, channel: 'b' }
    } else if (row.map === 'ao') {
      maps.ao = { image, channel: 'r' }
      const s = row.info.strength ?? 1
      if (s !== 1) report.add(slot, 'ignored', `AO 强度 ${round(s)} 已忽略，按 1 显示`)
    } else {
      maps[row.map] = { image }
    }
  }
  let normalStrength = 1
  if (maps.normal) {
    const s = m?.normalTexture?.scale ?? 1
    normalStrength = Math.min(4, Math.max(0, s))
    if (normalStrength !== s) report.add(slot, 'changed', `法线强度 ${round(s)} 超出 0–4，已限制为 ${round(normalStrength)}`)
  }
  // The base colour's transform is baked; the others are told if they differ.
  const transform = live.length > 0 ? transformOf((live.find((r) => r.map === 'base_color') ?? live[0]!).info!) : identity
  for (const row of live) if (!sameTransform(transformOf(row.info!), transform)) report.add(slot, 'changed', `${row.label}的贴图变换与基础色贴图不同，按基础色贴图的变换显示`)
  return { slot: { params, maps, normalStrength, alphaMode, alphaCutoff: m?.alphaCutoff ?? 0.5 }, transform, textures: live }
}

/** The image a texture shows: EXT_texture_webp's source before the plain one; null when there is none we can store. */
function imageOf(json: GLTFJSON, texture: number): number | null {
  const t = json.textures?.[texture]
  if (!t) return null
  const webp = t.extensions?.EXT_texture_webp?.source
  const source = webp ?? t.source
  return source !== undefined && json.images?.[source] ? source : null
}

const round = (v: number) => Math.round(v * 1000) / 1000

/** Instanced meshes become one mesh per instance, as the static subset has no instancing. */
function expandInstances(scene: Group, report: Reporter) {
  const instanced: InstancedMesh[] = []
  scene.traverse((o) => {
    if (o instanceof InstancedMesh) instanced.push(o)
  })
  const matrix = new Matrix4()
  for (const im of instanced) {
    const parent = im.parent!
    im.updateMatrix()
    for (let i = 0; i < im.count; i++) {
      im.getMatrixAt(i, matrix)
      const copy = new Mesh(im.geometry, im.material)
      copy.name = im.name
      copy.applyMatrix4(matrix.premultiply(im.matrix))
      parent.add(copy)
    }
    parent.remove(im)
  }
  if (instanced.length > 0) report.add(null, 'changed', '实例化的网格已展开成独立的网格')
}

export function convert(parsed: ParsedImport, options: ConvertOptions): Conversion {
  const { json, scene } = parsed
  const associations = parsed.associations as ReadonlyMap<Object3D | Material, Association>
  const report = new Reporter()
  if ((json.scenes?.length ?? 0) > 1) report.add(null, 'ignored', `文件有 ${json.scenes!.length} 个场景，只导入了第 ${(json.scene ?? 0) + 1} 个`)
  if ((json.animations?.length ?? 0) > 0) report.add(null, 'ignored', '动画已丢掉')
  if ((json.cameras?.length ?? 0) > 0) report.add(null, 'ignored', '相机已丢掉')
  if ((json.extensionsUsed ?? []).includes('KHR_lights_punctual')) report.add(null, 'ignored', '灯光已丢掉')

  expandInstances(scene, report)
  // Everything but meshes and the groups holding them goes: cameras, lights,
  // points and lines.
  const strays: Object3D[] = []
  scene.traverse((o) => {
    if (o === scene) return
    const isMesh = o instanceof Mesh
    if (!isMesh && o.type !== 'Group' && o.type !== 'Object3D') strays.push(o)
    o.userData = {}
  })
  for (const o of strays) {
    if (o.type === 'Points' || o.type.startsWith('Line')) report.add(null, 'ignored', '点和线不支持，已丢掉')
    o.removeFromParent()
  }

  const meshes: Mesh[] = []
  scene.traverse((o) => {
    if (o instanceof Mesh) meshes.push(o)
  })
  // Read before the materials are replaced below.
  const indices = new Map(meshes.map((m) => [m, associations.get(m.material as Material)?.materials ?? null]))
  const materialIndex = (mesh: Mesh): number | null => indices.get(mesh)!
  const used = new Set(indices.values())
  const names = slotNames(json.materials ?? [], used)
  if (names.size > maxSlots) throw new ImportRejected('limit', `模型有 ${names.size} 种材质，最多 ${maxSlots} 种。请在 Blender 里合并材质后再导出`)

  // Floats first, once per geometry, so world positions never move.
  const geometries = new Set(meshes.map((m) => m.geometry as BufferGeometry))
  for (const g of geometries) {
    for (const name of dequantize(g)) report.add(null, 'ignored', name === 'morph' ? '形态键已丢掉，按基础形状保存' : `顶点属性 ${name} 不支持，已丢掉`)
  }

  const slots = new Map<string, ConvertedSlot>()
  const images = new Map<number, ImageUse>()
  const converted = new Map<number | null, { slot: ConvertedSlot; transform: Transform; textures: TextureRow[] }>()
  for (const [index, id] of names) {
    const c = convertMaterial(json, index === null ? undefined : json.materials?.[index], id, report)
    converted.set(index, c)
    slots.set(id, c.slot)
    for (const [map, ref] of Object.entries(c.slot.maps) as [MapName, MapRef][]) {
      const use: ImageUse = map === 'base_color' || map === 'emissive' ? 'color' : 'data'
      if (images.get(ref.image) !== 'data') images.set(ref.image, use)
    }
  }

  // Each slot's transform baked into its own copy of a geometry it shares.
  const baked = new Map<BufferGeometry, Map<string, BufferGeometry>>()
  const slotMaterials = new Map<string, MeshStandardMaterial>()
  for (const mesh of meshes) {
    const index = materialIndex(mesh)
    const id = names.get(index)!
    const c = converted.get(index)!
    const g = mesh.geometry as BufferGeometry
    const vertexColors = g.getAttribute('color') !== undefined
    if (vertexColors) c.slot.params.vertex_color = true
    if (Object.keys(c.slot.maps).length > 0 && !g.getAttribute('uv')) report.add(id, 'changed', '模型没有 UV，贴图按三平面投影显示')
    if (!sameTransform(c.transform, identity) && g.getAttribute('uv')) {
      let copies = baked.get(g)
      if (!copies) baked.set(g, (copies = new Map()))
      let copy = copies.get(id)
      if (!copy) {
        copy = g.clone()
        const uv = copy.getAttribute('uv') as BufferAttribute
        for (let i = 0; i < uv.count; i++) {
          const [u, v] = applyTransform(c.transform, uv.getX(i), uv.getY(i))
          uv.setXY(i, u, v)
        }
        copies.set(id, copy)
      }
      mesh.geometry = copy
    }
    let material = slotMaterials.get(id)
    if (!material) slotMaterials.set(id, (material = new MeshStandardMaterial({ name: `slot:${id}` })))
    mesh.material = material
    mesh.morphTargetInfluences = undefined
    mesh.morphTargetDictionary = undefined
  }

  // Wrapping: rendered as REPEAT, so a mirrored or clamped texture is told
  // only where the (baked) UVs leave [0, 1] along its axis.
  for (const [index, c] of converted) {
    const id = names.get(index)!
    for (const row of c.textures) {
      const t = json.textures?.[row.info!.index]
      const sampler = t?.sampler === undefined ? undefined : json.samplers?.[t.sampler]
      const wraps = [sampler?.wrapS ?? repeatWrap, sampler?.wrapT ?? repeatWrap]
      if (wraps.every((w) => w === repeatWrap)) continue
      const outside = meshes.some((mesh) => {
        if (names.get(materialIndex(mesh)) !== id) return false
        const uv = (mesh.geometry as BufferGeometry).getAttribute('uv')
        if (!uv) return false
        for (let i = 0; i < uv.count; i++) {
          for (const axis of [0, 1] as const) {
            if (wraps[axis] === repeatWrap) continue
            const x = axis === 0 ? uv.getX(i) : uv.getY(i)
            if (x < -1e-6 || x > 1 + 1e-6) return true
          }
        }
        return false
      })
      if (!outside) continue
      const kind = wrapNames[wraps.find((w) => w !== repeatWrap)!] ?? '非重复'
      report.add(id, 'changed', `${row.label}原本是${kind}寻址，导入后按重复显示`)
    }
  }

  // Names the static subset reads as parts or anchors, kept only if well formed.
  scene.traverse((o) => {
    const m = /^(part|anchor):(.*)$/.exec(o.name)
    if (m && !namePattern.test(m[2]!)) o.name = `_${o.name}`
  })

  if (options.originToBottomCenter) toBottomCenter(scene)
  return { scene, slots, images, report: report.items }
}

/** Moves the scene's top-level nodes so the bottom centre of its bounds is the origin (AC-8). */
export function toBottomCenter(scene: Object3D): void {
  const box = new Box3().setFromObject(scene, true)
  if (box.isEmpty()) return
  const offset = new Vector3((box.min.x + box.max.x) / 2, box.min.y, (box.min.z + box.max.z) / 2)
  for (const child of scene.children) child.position.sub(offset)
  scene.updateMatrixWorld(true)
}

/** The slots as the import request sends them, each image named by its uploaded revision. */
export function slotPayload(slots: ReadonlyMap<string, ConvertedSlot>, revisionOf: (image: number) => string): Record<string, SlotJSON> {
  const out: Record<string, SlotJSON> = {}
  for (const [id, s] of slots) {
    const slot: SlotJSON = { preset: principledID, params: { ...s.params } }
    const entries = Object.entries(s.maps) as [MapName, MapRef][]
    if (entries.length > 0) {
      slot.maps = Object.fromEntries(entries.map(([k, r]) => [k, r.channel ? { content_revision_id: revisionOf(r.image), channel: r.channel } : { content_revision_id: revisionOf(r.image) }]))
      slot.mapping = { kind: 'uv', repeat: [1, 1], rotation_deg: 0, normal_strength: s.normalStrength }
    }
    if (s.alphaMode !== 'opaque') slot.alpha_mode = s.alphaMode
    if (s.alphaMode === 'clip') slot.alpha_cutoff = Math.min(1, Math.max(0, s.alphaCutoff))
    out[id] = slot
  }
  return out
}
