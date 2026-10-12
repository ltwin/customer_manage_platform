import type { operations } from '../../api/schema'

type AssetQuery = operations['listCreativeAssets']['parameters']['query']
export type Shape = NonNullable<NonNullable<AssetQuery>['shape']>
export const SHAPES: { value: Shape; label: string }[] = [
  { value: 'landscape', label: '横幅' },
  { value: 'portrait', label: '竖幅' },
  { value: 'square', label: '方形' },
]
export const SWATCHES = [
  { hex: '#d9a07a', label: '橙' },
  { hex: '#c9655d', label: '红' },
  { hex: '#d9c38a', label: '黄' },
  { hex: '#8fa37a', label: '绿' },
  { hex: '#6f9fa8', label: '青' },
  { hex: '#5f7ea8', label: '蓝' },
  { hex: '#9b86b0', label: '紫' },
  { hex: '#c98a96', label: '粉' },
  { hex: '#8a6550', label: '棕' },
  { hex: '#f1efe9', label: '白' },
  { hex: '#a1a1a7', label: '灰' },
  { hex: '#1d1d1f', label: '黑' },
]
export function shapeOf(
  width?: number | null,
  height?: number | null,
): Shape | '' {
  if (!width || !height || width <= 0 || height <= 0) return ''
  if (Math.max(width, height) * 10 <= Math.min(width, height) * 11)
    return 'square'
  return width > height ? 'landscape' : 'portrait'
}
export const colorValue = (value: string | null) =>
  value && /^#[\da-f]{6}$/i.test(value) ? value.toLowerCase() : ''
