import type * as api from './api.ts'
import type { PickerOption } from './Picker.tsx'
export type Catalog = {
  groups: api.Group[]
  tags: api.Tag[]
  categories: api.Category[]
  hierarchy: string
  settings: api.LibrarySettings | null
}
export const emptyCatalog = (): Catalog => ({
  groups: [],
  tags: [],
  categories: [],
  hierarchy: '1',
  settings: null,
})
export const kindLabel: Record<string, string> = {
  text: '文字',
  link: '链接',
  image: '图片',
  video: '视频',
  audio: '音频',
  model: '模型',
}
// 宽高比来自已核实的原图尺寸；文字与音频按固定比例排版，模型缩略图是方形。
export function tileRatio(a: api.Asset): number {
  const m = a.content?.media?.find((x) => x.role === 'original') ?? a.content?.media?.[0]
  if ((a.kind === 'image' || a.kind === 'video') && m?.width && m.height)
    return m.height / m.width
  if (a.kind === 'model') return 1
  return a.kind === 'audio' ? 0.5 : 0.62
}
/** 分组按层级排成下拉选项：父分组在前，子分组紧随其后并缩进。 */
export function groupOptions(
  groups: api.Group[],
  exclude?: string,
): PickerOption[] {
  const out: PickerOption[] = []
  const visit = (parent: string | null, depth: number) => {
    for (const g of groups) {
      if ((g.parent_id ?? null) !== parent || g.id === exclude) continue
      out.push({ value: g.id, label: g.name, depth })
      visit(g.id, depth + 1)
    }
  }
  visit(null, 0)
  return out
}

/** 媒体时长：不到一小时写 m:ss，否则 h:mm:ss；没有时长返回空串。 */
export function formatDuration(ms: number | null | undefined): string {
  if (!ms || ms < 0) return ''
  const s = Math.round(ms / 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  return h ? `${h}:${pad(m)}:${pad(s % 60)}` : `${m}:${pad(s % 60)}`
}
export function mediaDuration(a: api.Asset): string {
  const m = a.content?.media?.find((x) => x.role === 'original') ?? a.content?.media?.[0]
  return formatDuration(m?.duration_ms)
}

/** 容量：1024 进位，保留一位小数，整数不带 .0。 */
export function formatBytes(n: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = Math.max(0, n)
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  const text = i === 0 ? String(Math.round(v)) : v.toFixed(1).replace(/\.0$/, '')
  return `${text} ${units[i]}`
}

/** 链接卡片只显示域名，不抓取网页；不是合法网址时原样返回。 */
export function linkDomain(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

/**
 * 音频卡片的装饰条：还没有真实波形（V6-074），高度由素材 id 决定，
 * 同一份素材每次画出来都一样。返回 0.2–1 的相对高度。
 */
export function audioBars(id: string, count = 36): number[] {
  let h = 2166136261
  for (let i = 0; i < id.length; i++) h = Math.imul(h ^ id.charCodeAt(i), 16777619)
  return Array.from({ length: count }, () => {
    h = Math.imul(h ^ (h >>> 15), 2246822507) >>> 0
    return 0.2 + ((h % 1000) / 1000) * 0.8
  })
}

/** 评分的文字说明，如「4.5 星」。 */
export const ratingText = (value: number) => `${value} 星`
