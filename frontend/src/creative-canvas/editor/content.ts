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


export function validateContentValue(kind: DraftKind, value: string): string {
  if (['text', 'link'].includes(kind) && !value.trim())
    return kind === 'text' ? '请填写正文' : '请填写链接'
  if (isMediaContent(kind) && value.length > 2000) return '说明最多 2000 字'
  if (kind === 'link') {
    try {
      const url = new URL(value)
      if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password)
        return '请输入不含账号口令的 HTTP 或 HTTPS 链接'
    } catch {
      return '请输入不含账号口令的 HTTP 或 HTTPS 链接'
    }
  }
  return ''
}
