import type { components } from '../../api/schema'
/** 粘贴只将完整 HTTP(S) 地址识别为链接，其他内容保留为文字。 */
export function pastedKind(text: string): 'link' | 'text' {
  try {
    const url = new URL(text.trim())
    return /^https?:$/.test(url.protocol) && !/\s/.test(text.trim())
      ? 'link'
      : 'text'
  } catch {
    return 'text'
  }
}
export function contentImport(
  kind: 'link' | 'text',
  value: string,
  title: string,
  group: string,
): components['schemas']['CreativeAssetCreatePayload'] {
  const text = value.trim()
  if (!text) throw new Error(kind === 'link' ? '请填写网址' : '请填写文字内容')
  if (kind === 'link' && pastedKind(text) !== 'link')
    throw new Error('请输入完整的 http:// 或 https:// 网址')
  if (kind === 'text' && [...value].length > 100000)
    throw new Error('文字内容最多 100000 字，请缩短后再试')
  if (kind === 'link') {
    if ([...text].length > 4096)
      throw new Error('网址最多 4096 字，请缩短后再试')
    const url = new URL(text)
    if (url.username || url.password || /^https?:\/\/[^/?#]*@/i.test(text))
      throw new Error('网址不能包含账号或密码')
  }
  return {
    title:
      title.trim() ||
      (kind === 'link'
        ? new URL(text).hostname
        : [...text.split('\n')[0]!].slice(0, 200).join('')),
    content: {
      kind,
      payload: kind === 'link' ? { url: text } : { body: value },
      ...(kind === 'link'
        ? {
            rights: {
              source_class: 'unknown_web',
              rights_basis: 'citation_or_display',
            },
          }
        : {}),
    },
    group_ids: group ? [group] : [],
  }
}
export function remainingTime(purgeAfter: string | null, now = Date.now()) {
  if (!purgeAfter) return '不会自动清除'
  const days = Math.ceil((Date.parse(purgeAfter) - now) / 86400000)
  return days > 0 ? `还剩 ${days} 天` : '已到期，等待清除'
}
