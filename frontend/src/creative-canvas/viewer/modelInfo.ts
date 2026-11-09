import type { components } from '../../api/schema'
import { materialEntry } from '../../creative-scene/materials/catalog.ts'
import { codeGenerator } from '../../creative-scene/models/generator.ts'
import type { Content } from '../editor/api.ts'

// 模型与材质资产的信息格式化：检查器、查看浮窗共用。不依赖 three（材质目录只是 JSON），资产库页照常不加载 3D 引擎。

type Manifest = components['schemas']['CreativeModelManifest']
type Generator = components['schemas']['CreativeModelGenerator']
type Materials = components['schemas']['CreativeSceneMaterialSlots']
type Faces = components['schemas']['CreativeModelFaces']
type MaterialSlot = Materials[string]

/** 几何模块按 `mount` 约定的原点，即模型的参考点。 */
export const originLabel: Record<Generator['mount'], string> = {
  floor: '底部中心',
  wall: '背面底边中心',
  ceiling: '顶部中心',
  free: '几何中心',
}

export type ModelInfo = {
  name: string
  origin: string
  /** 宽 × 高 × 深，米，保留两位。 */
  size: string
  triangles: string
  /** 槽位、提示，以及资产所配材质的名称（没配为空）。 */
  slots: { id: string; hint?: string; material?: string }[]
  params: { key: string; label: string; value: string }[]
}

/** 模型的清单（包围盒、槽位），3D 预览按它取景、配材质。 */
export function modelManifest(content: Content | undefined): Manifest | null {
  return content?.kind === 'model' && 'model' in content.payload
    ? content.payload.model.manifest
    : null
}

/** 资产存入时所配的材质；修订 v12 之前存的模型资产没有，按空对待。 */
export function modelMaterials(content: Content | undefined): Materials {
  return content?.kind === 'model' && 'model' in content.payload
    ? (content.payload.model.materials ?? {})
    : {}
}

/** 模型的面级材质分配（scene-face-materials）；没有时为 null。 */
export function modelFaces(content: Content | undefined): Faces | null {
  return content?.kind === 'model' && 'model' in content.payload
    ? (content.payload.model.faces ?? null)
    : null
}

/** 完整形态的模型才有信息；暂存形态和其他类型返回 null。 */
export function modelInfo(content: Content | undefined): ModelInfo | null {
  if (content?.kind !== 'model' || !('model' in content.payload)) return null
  const { manifest, generator, name_hint } = content.payload.model
  if (!generator) return null
  const materials = modelMaterials(content)
  return {
    name: name_hint ?? '',
    origin: originLabel[generator.mount],
    size: modelSize(manifest),
    triangles: manifest.triangles.toLocaleString('zh-CN'),
    slots: [...manifest.slots, ...faceSlots(content, manifest)].map((s) => {
      const preset = materials[s.id]?.preset
      return preset ? { ...s, material: materialEntry(preset)?.label ?? preset } : s
    }),
    params: Object.entries(codeGenerator(generator)?.params_schema ?? {})
      .map(([key, spec]) => ({
        key,
        label: spec.label || key,
        value: paramValue(codeGenerator(generator)?.params[key] ?? spec.default, spec.unit),
      }))
      .sort((a, b) => a.key.localeCompare(b.key)),
  }
}

/** 面分配新建、GLB 里没有的槽位，按名字排序，与场景的有效槽位一致（models/faces.ts effectiveSlots；这里不引 three）。 */
function faceSlots(content: Content, manifest: Manifest): Manifest['slots'] {
  const faces = modelFaces(content)
  const glb = new Set(manifest.slots.map((s) => s.id))
  const used = new Set(
    faces?.meshes.flatMap((m) =>
      Object.entries(m.assign)
        .filter(([, r]) => r.length)
        .map(([id]) => id),
    ),
  )
  return [...used]
    .filter((id) => !glb.has(id))
    .sort()
    .map((id) => {
      const hint = faces?.hints?.[id]
      return hint ? { id, hint } : { id }
    })
}

export function modelSize(m: Manifest): string {
  const edge = (i: number) => ((m.bounds.max[i] ?? 0) - (m.bounds.min[i] ?? 0)).toFixed(2)
  return `${edge(0)} × ${edge(1)} × ${edge(2)} 米`
}

function paramValue(v: unknown, unit?: string): string {
  if (typeof v === 'boolean') return v ? '是' : '否'
  if (typeof v === 'number') return unit ? `${v} ${unit}` : String(v)
  return typeof v === 'string' ? v : JSON.stringify(v)
}

/** 检查器与查看浮窗的信息行：标签与值。 */
export function modelRows(info: ModelInfo): [string, string][] {
  const rows: [string, string][] = [
    ['尺寸', info.size],
    ['参考点', info.origin],
    ['面数', info.triangles],
  ]
  if (info.slots.length)
    rows.push([
      '材质槽',
      info.slots
        .map((s) => (s.hint ? `${s.id}（${s.hint}）` : s.id) + (s.material ? `：${s.material}` : ''))
        .join('、'),
    ])
  for (const p of info.params) rows.push([p.label, p.value])
  return rows
}

/** 材质资产的槽位分配；其他种类返回 null。 */
export function assetMaterial(content: Content | undefined): MaterialSlot | null {
  return content?.kind === 'material' && 'material' in content.payload
    ? content.payload.material.slot
    : null
}

const mapLabel: Record<string, string> = {
  base_color: '基础色',
  normal: '法线',
  roughness: '糙度',
  metalness: '金属度',
  ao: 'AO',
}
const alphaLabel: Record<string, string> = {
  opaque: '不透明',
  clip: '透明裁切',
  blend: '半透明混合',
}

/** 检查器与查看浮窗的材质信息行：预设、贴图、映射方式、透明方式。 */
export function materialRows(slot: MaterialSlot): [string, string][] {
  const rows: [string, string][] = [
    ['材质', materialEntry(slot.preset)?.label ?? slot.preset],
  ]
  const maps = Object.keys(slot.maps ?? {})
  rows.push(['贴图', maps.length ? maps.map((k) => mapLabel[k] ?? k).join('、') : '无'])
  const m = slot.mapping
  if (m && maps.length)
    rows.push([
      '映射',
      m.kind === 'uv'
        ? `模型 UV · 重复 ${(m.repeat ?? [1, 1]).join(' × ')}`
        : `三平面投影 · ${m.size_m ?? 1} 米一块`,
    ])
  rows.push(['透明', alphaLabel[slot.alpha_mode ?? 'opaque'] ?? '不透明'])
  return rows
}
