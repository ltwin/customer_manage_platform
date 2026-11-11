import type { components } from '../../api/schema'
import type * as api from '../editor/api.ts'

export type Character = components['schemas']['CreativeCharacterPayload']

export const characterViews = [
  { value: 'front', label: '正面' },
  { value: 'three_quarter', label: '四分之三侧' },
  { value: 'side', label: '侧面' },
  { value: 'back', label: '背面' },
  { value: 'face', label: '脸部特写' },
  { value: 'full_body', label: '全身' },
  { value: 'detail', label: '细节' },
  { value: 'other', label: '其他' },
] as const satisfies ReadonlyArray<{
  value: Character['views'][number]['view']
  label: string
}>

export const characterViewLabel = (view: Character['views'][number]['view']) =>
  characterViews.find((item) => item.value === view)?.label ?? view

export function characterOf(content?: api.Content): Character | undefined {
  return content && 'character' in content.payload
    ? content.payload.character
    : undefined
}

export function characterIdentity(
  card: NonNullable<api.Asset['character']>,
): string {
  return [card.ip?.name, card.work, card.variant].filter(Boolean).join(' · ')
}
