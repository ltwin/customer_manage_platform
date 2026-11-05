import type { components } from '../../api/schema'
import { materialCatalog, texturedEntries } from '../../creative-scene/materials/catalog.ts'

// 资产库的「内置」来源（C5c2 data: 内置贴图库「展现」）：材质目录里的内置贴图材质，
// 只读、不分页，前端按名称和品类本地过滤。它们不是账号素材：没有资产 ID、修订和入库时间，
// 用到槽位上时写 {preset: id, params: {}}，与选程序化预设相同。

type Slot = components['schemas']['CreativeSceneMaterialSlot']

export type BuiltinMaterial = {
  id: string
  label: string
  category: string
  categoryLabel: string
  /** 一块贴图对应的真实宽度（米）。 */
  sizeM: number
  /** 离线渲染的材质球，make generate 从后端复制到 ./builtin/。 */
  thumbnail: string
  source: { site: string; asset: string; author: string; license: string }
}

const categoryLabels = new Map(materialCatalog.categories.map((c) => [c.id, c.label]))

/** 缩略图文件名是条目 ID 把 @ 换成 _。 */
export const builtinThumbnail = (id: string) => new URL(`./builtin/${id.replace('@', '_')}.webp`, import.meta.url).href

export const builtinMaterials: BuiltinMaterial[] = texturedEntries.map((m) => ({
  id: m.id,
  label: m.label,
  category: m.category ?? '',
  categoryLabel: categoryLabels.get(m.category ?? '') ?? '',
  sizeM: m.size_m ?? 1,
  thumbnail: builtinThumbnail(m.id),
  source: m.source!,
}))

export const builtinMaterial = (id: string) => builtinMaterials.find((m) => m.id === id)

/** 落到槽位上的写法：只引用预设，参数用它的缺省值。 */
export const builtinSlot = (id: string): Slot => ({ preset: id, params: {} })

/**
 * 按文字筛选：按空白拆成词，每个词（不分大小写）都要出现在名称、品类（ID 或中文名）
 * 或来源资产名里。空文字列出全部，顺序同目录。
 */
export function filterBuiltin(q: string, items: readonly BuiltinMaterial[] = builtinMaterials): BuiltinMaterial[] {
  const words = q.toLowerCase().split(/\s+/).filter(Boolean)
  return items.filter((m) => {
    const text = [m.label, m.category, m.categoryLabel, m.source.asset].join('\n').toLowerCase()
    return words.every((w) => text.includes(w))
  })
}

/** 按目录的品类顺序分组，空组不列。 */
export function groupBuiltin(items: readonly BuiltinMaterial[]): { category: string; label: string; items: BuiltinMaterial[] }[] {
  return materialCatalog.categories.flatMap((c) => {
    const inCategory = items.filter((m) => m.category === c.id)
    return inCategory.length ? [{ category: c.id, label: c.label, items: inCategory }] : []
  })
}
