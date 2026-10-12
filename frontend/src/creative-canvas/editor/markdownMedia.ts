// Keep Markdown's sanitized URL, but only embed ordinary web resources.
export function mediaSource(value: string | undefined): string | undefined {
  if (!value?.trim()) return undefined
  try {
    const url = new URL(value, 'https://markdown.invalid/')
    if (
      !['https:', 'http:'].includes(url.protocol) ||
      url.username ||
      url.password
    )
      return undefined
    return value.trim()
  } catch {
    return undefined
  }
}

export function linkedMediaKind(
  href: string | undefined,
): 'video' | 'audio' | undefined {
  const source = mediaSource(href)
  if (!source) return undefined
  const path = new URL(
    source,
    'https://markdown.invalid/',
  ).pathname.toLowerCase()
  if (/\.(mp4|webm|m4v|mov|ogv)$/.test(path)) return 'video'
  if (/\.(mp3|wav|ogg|oga|m4a|aac|flac)$/.test(path)) return 'audio'
  return undefined
}
