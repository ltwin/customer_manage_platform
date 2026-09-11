import type { components } from '../../api/schema'
import { request } from '../../api/transport.ts'
import { read } from './api.ts'

export type MediaCapabilities =
  components['schemas']['CreativeMediaCapabilities']
export type Upload = components['schemas']['CreativeUpload']
export type UploadTarget = components['schemas']['CreativeUploadTarget']
export type UploadCandidate = components['schemas']['CreativeUploadCandidate']
export type MediaTicket = components['schemas']['CreativeMediaTicket']
export type MediaObject = components['schemas']['CreativeMediaObject']
export type MediaKind = 'image' | 'video' | 'audio'
export const mediaKinds: readonly MediaKind[] = ['image', 'video', 'audio']
export const isMediaKind = (kind: string): kind is MediaKind =>
  (mediaKinds as readonly string[]).includes(kind)

// Picks the rendition that fits the purpose; originals only for download or
// when no display rendition was derived.
export function pickRendition(
  media: MediaObject[] | undefined,
  prefer: 'display' | 'original',
): MediaObject | undefined {
  if (!media?.length) return undefined
  return (
    media.find((m) => m.role === prefer) ??
    media.find((m) => m.role === 'original')
  )
}

// The picker and validation share the server's enabled formats. File metadata
// is only a preflight check; the server still verifies the actual bytes.
export function mediaFileAccept(
  capabilities: MediaCapabilities,
  kind: MediaKind,
): string {
  return capabilities.formats
    .filter((format) => format.kind === kind)
    .flatMap((format) => [format.mime, ...format.extensions])
    .join(',')
}

const mimeAliases: Record<string, string> = {
  'image/jpg': 'image/jpeg',
  'audio/mp3': 'audio/mpeg',
  'audio/x-mp3': 'audio/mpeg',
  'audio/x-wav': 'audio/wav',
  'audio/wave': 'audio/wav',
  'audio/vnd.wave': 'audio/wav',
  'video/x-m4v': 'video/mp4',
}

export function classifyFile(
  file: File,
  capabilities: MediaCapabilities,
  expectedKind?: MediaKind,
): { kind: MediaKind; mime: string } | { error: string } {
  const extension = /\.[^.]+$/.exec(file.name)?.[0].toLowerCase() ?? ''
  const rawMime = file.type.split(';')[0].trim().toLowerCase()
  const declared = mimeAliases[rawMime] ?? rawMime
  const format = capabilities.formats.find((f) =>
    f.extensions.includes(extension),
  )
  if (!format) return { error: '暂不支持此文件格式' }
  if (expectedKind && format.kind !== expectedKind) {
    const label = { image: '图片', video: '视频', audio: '音频' }[expectedKind]
    const extensions = capabilities.formats
      .filter((f) => f.kind === expectedKind)
      .flatMap((f) => f.extensions)
      .join('、')
    return { error: `请选择${label}文件（${extensions}）` }
  }
  if (
    declared &&
    declared !== 'application/octet-stream' &&
    declared !== format.mime
  )
    return { error: '文件类型与扩展名不一致，请选择正确的媒体文件' }
  const limit =
    format.kind === 'image'
      ? capabilities.image_max_bytes
      : capabilities.av_max_bytes
  if (file.size > limit)
    return { error: `文件超过 ${Math.round(limit / 1048576)} MB 上限` }
  if (file.size < 1) return { error: '文件为空' }
  return { kind: format.kind, mime: format.mime }
}

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms))

export type UploadProgress = {
  stage: 'creating' | 'uploading' | 'verifying' | 'done' | 'failed'
  sent: number
  total: number
  upload?: Upload
  message?: string
}

// uploadFile drives one session end to end. Each write carries a fresh
// operation id so a replay is exact; part PUTs are retried per part.
export async function uploadFile(
  file: File,
  target: UploadTarget,
  capabilities: MediaCapabilities,
  onProgress: (p: UploadProgress) => void,
  signal?: AbortSignal,
): Promise<Upload> {
  const classified = classifyFile(file, capabilities)
  if ('error' in classified) throw new Error(classified.error)
  onProgress({ stage: 'creating', sent: 0, total: file.size })
  const operation = crypto.randomUUID()
  let upload = await request<Upload>('/creative/uploads', {
    method: 'POST',
    headers: { 'Idempotency-Key': operation },
    signal,
    body: JSON.stringify({
      operation_id: operation,
      client_created_at: new Date().toISOString(),
      payload: {
        file_name: readableFileName(file.name),
        kind: classified.kind,
        mime: classified.mime,
        size: file.size,
        target,
      },
    }),
  })
  upload = await poll(upload.id, (u) => u.state !== 'created', signal)
  if (upload.state !== 'uploading')
    throw new Error(describeFailure(upload) ?? '上传会话未能初始化')
  const numbers = Array.from({ length: upload.part_count }, (_, i) => i + 1)
  let sent = 0
  onProgress({ stage: 'uploading', sent, total: file.size, upload })
  for (let i = 0; i < numbers.length; i += 3) {
    const batch = numbers.slice(i, i + 3)
    const authorized = await request<
      components['schemas']['CreativeUploadPartAuthorizations']
    >(
      `/creative/uploads/${encodeURIComponent(upload.id)}/part-authorizations`,
      { method: 'POST', signal, body: JSON.stringify({ part_numbers: batch }) },
    )
    const session = upload
    await Promise.all(
      authorized.parts.map(async (part) => {
        const start = (part.part_number - 1) * session.part_size
        const chunk = file.slice(
          start,
          Math.min(start + session.part_size, file.size),
        )
        for (let attempt = 0; ; attempt++) {
          try {
            const res = await fetch(part.url, {
              method: part.method,
              headers: part.headers,
              body: chunk,
              signal,
              credentials: 'omit',
            })
            if (!res.ok)
              throw new Error(`分片 ${part.part_number} 上传失败（${res.status}）`)
            break
          } catch (e) {
            if (attempt >= 2 || signal?.aborted) throw e
            await wait(400 * (attempt + 1))
          }
        }
        sent += chunk.size
        onProgress({ stage: 'uploading', sent, total: file.size, upload: session })
      }),
    )
  }
  const completeOperation = crypto.randomUUID()
  upload = await request<Upload>(
    `/creative/uploads/${encodeURIComponent(upload.id)}/complete`,
    {
      method: 'POST',
      headers: { 'Idempotency-Key': completeOperation },
      signal,
      body: JSON.stringify({
        operation_id: completeOperation,
        client_created_at: new Date().toISOString(),
        payload: { expected_revision: upload.revision, parts: [] },
      }),
    },
  )
  onProgress({ stage: 'verifying', sent: file.size, total: file.size, upload })
  upload = await poll(
    upload.id,
    (u) => ['ready', 'failed', 'expired', 'cancelled'].includes(u.state),
    signal,
  )
  if (upload.state !== 'ready') {
    const message = describeFailure(upload) ?? '上传失败'
    onProgress({ stage: 'failed', sent: file.size, total: file.size, upload, message })
    throw new Error(message)
  }
  onProgress({ stage: 'done', sent: file.size, total: file.size, upload })
  return upload
}

async function poll(
  id: string,
  done: (u: Upload) => boolean,
  signal?: AbortSignal,
): Promise<Upload> {
  for (let attempt = 0; attempt < 600; attempt++) {
    const upload = await read<Upload>(
      `/uploads/${encodeURIComponent(id)}`,
      signal,
    )
    if (done(upload)) return upload
    await wait(Math.min(250 + attempt * 50, 1500))
    if (signal?.aborted) throw new Error('已取消')
  }
  throw new Error('等待服务器处理超时，请稍后在上传状态中查看')
}

export function describeFailure(upload: Upload): string | null {
  if (upload.state === 'ready') return null
  const code = upload.error_code
  const messages: Record<string, string> = {
    media_unsupported: '文件内容不是支持的媒体格式',
    size_limit: '文件超过大小上限',
    size_mismatch: '文件大小与声明不一致，请重新选择',
    parts_incomplete: '分片未完整上传，请重试',
    init_failed: '存储初始化失败，请联系管理员检查存储配置',
    quota_exceeded: '媒体存储额度不足',
    promote_interrupted: '媒体处理被中断，请重试',
    publish_failed: '媒体发布失败，请重试',
    complete_failed: '存储合并失败，请稍后重试',
    verify_failed: '媒体校验失败',
    promote_failed: '媒体写入失败，请稍后重试',
    expired: '上传会话已过期',
    cancelled: '上传已取消',
  }
  // Only the reason class reaches the client; details stay in worker logs.
  return messages[code] ?? (upload.state === 'failed' ? '上传失败' : null)
}

// Browsers name files dragged from web pages by their URL segment; decode a
// percent-encoded name so titles read naturally, keeping it when malformed.
export function readableFileName(name: string): string {
  if (!name.includes('%')) return name
  try {
    return decodeURIComponent(name)
  } catch {
    return name
  }
}

// Tickets are cached per revision/role/purpose until shortly before expiry.
const tickets = new Map<string, Promise<MediaTicket>>()
export function mediaTicket(
  revision: string,
  role: 'original' | 'display',
  purpose: 'display' | 'download',
  fileName?: string,
): Promise<MediaTicket> {
  const key = `${revision}:${role}:${purpose}:${fileName ?? ''}`
  const cached = tickets.get(key)
  if (cached) return cached
  const promise = request<MediaTicket>('/creative/media-access-tickets', {
    method: 'POST',
    body: JSON.stringify({
      content_revision_id: revision,
      role,
      purpose,
      ...(fileName ? { file_name: fileName } : {}),
    }),
  }).then((ticket) => {
    const ttl = new Date(ticket.expires_at).getTime() - Date.now() - 30000
    setTimeout(() => tickets.delete(key), Math.max(1000, ttl))
    return ticket
  })
  promise.catch(() => tickets.delete(key))
  tickets.set(key, promise)
  return promise
}
export function clearMediaTickets() {
  tickets.clear()
}
