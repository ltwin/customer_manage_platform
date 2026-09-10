import type { ContentDraft } from './api.ts'
import type { Draft } from './journal.ts'
export function payload(
  kind: 'text' | 'link',
  value: string,
): ContentDraft['payload'] {
  return kind === 'text' ? { body: value } : { url: value }
}
export const blankDraft = (kind: 'text' | 'link' = 'text'): Draft => ({
  kind,
  title: '',
  value: '',
})
