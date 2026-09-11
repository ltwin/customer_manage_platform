import type { ContentDraft, Content } from './api.ts'
import type { Draft } from './journal.ts'
export type DraftKind = Draft['kind']
export function payload(
  kind: DraftKind,
  value: string,
): ContentDraft['payload'] {
  return kind === 'text'
    ? { body: value }
    : kind === 'link'
      ? { url: value }
      : value.trim()
        ? { caption: value }
        : {}
}
export const blankDraft = (kind: DraftKind = 'text'): Draft => ({
  kind,
  title: '',
  value: '',
})
// Text shown for any payload: body, url or media caption; never a storage key.
export function contentText(
  payload: Content['payload'] | undefined | null,
): string {
  if (!payload) return ''
  if ('body' in payload) return payload.body
  if ('url' in payload) return payload.url
  return payload.caption ?? ''
}
export const isMediaContent = (kind: string) =>
  kind === 'image' || kind === 'video' || kind === 'audio'
