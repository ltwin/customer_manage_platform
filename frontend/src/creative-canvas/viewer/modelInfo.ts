import type { components } from '../../api/schema'
import { materialEntry } from '../../creative-scene/materials/catalog.ts'
import type { Content } from '../editor/api.ts'

// 模型资产的信息格式化：检查器、查看浮窗共用。不依赖 three（材质目录只是 JSON），资产库页照常不加载 3D 引擎。

type Manifest = components['schemas']['CreativeModelManifest']
type Generator = components['schemas']['CreativeModelGenerator']
type Materials = components['schemas']['CreativeSceneMaterialSlots']

export const mountLabel: Record<Generator['mount'], string> = {
  floor: '落地',
  wall: '墙挂',
  ceiling: '吊顶',
  free: '自由放置',
}

export type ModelInfo = {
  name: string
  mount: string
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

/** 完整形态的模型才有信息；暂存形态和其他类型返回 null。 */
export function modelInfo(content: Content | undefined): ModelInfo | null {
  if (content?.kind !== 'model' || !('model' in content.payload)) return null
  const { manifest, generator, name_hint } = content.payload.model
  if (!generator) return null
  const materials = modelMaterials(content)
  return {
    name: name_hint ?? '',
    mount: mountLabel[generator.mount],
    size: modelSize(manifest),
    triangles: manifest.triangles.toLocaleString('zh-CN'),
    slots: manifest.slots.map((s) => {
      const preset = materials[s.id]?.preset
      return preset ? { ...s, material: materialEntry(preset)?.label ?? preset } : s
    }),
    params: Object.entries(generator.params_schema)
      .map(([key, spec]) => ({
        key,
        label: spec.label || key,
        value: paramValue(generator.params[key] ?? spec.default, spec.unit),
      }))
      .sort((a, b) => a.key.localeCompare(b.key)),
  }
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
    ['放置方式', info.mount],
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
