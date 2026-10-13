// 对话附件（参考图）：首页一句话开始交付，画布助手（V6-043）复用。
// 上传走暂存（target.kind = staged）：只发布内容，不入库、不建节点；
// 消息发出后修订由消息引用保留。从资产库选的直接用素材的内容修订。
import { useCallback, useEffect, useRef, useState } from 'react'
import { read } from '../editor/api.ts'
import {
  cancelUpload,
  classifyFile,
  uploadFile,
  type MediaCapabilities,
  type Upload,
} from '../editor/media.ts'

export type AttachmentSource = 'upload' | 'library'
export type ChatAttachment = {
  key: string
  title: string
  source: AttachmentSource
  state: 'uploading' | 'ready' | 'failed'
  revisionID?: string
  // 上传文件内容的 SHA-256，判重用；从资产库选的按修订判重。
  digest?: string
  error?: string
  // 本机文件：重试时重新上传，预览用它的对象 URL。
  file?: File
  preview?: string
}
// 存进本机记录的部分：刷新后不用重新上传。
export type SavedAttachment = {
  id: string
  title: string
  kind: 'image'
  source: AttachmentSource
  // 兼容没有摘要的旧记录；新上传在刷新后仍按文件内容判重。
  digest?: string
}

export const attachmentLimit = 6
export const attachmentMessages = {
  duplicate: '这张图已经添加过',
  preparing: '正在准备上传，请稍候再添加',
}
// 一次最多 6 张，同时不超过助手目录给的附件数和图片预览数。
export function attachmentCap(limits?: {
  attachments: number
  image_preview_count: number
}): number {
  return Math.min(
    attachmentLimit,
    limits?.attachments ?? attachmentLimit,
    limits?.image_preview_count ?? attachmentLimit,
  )
}
// 判定一批新附件能不能加：重复的跳过并提示；去重后超出上限就整批拒绝。
export function admitAttachments(
  existing: readonly Pick<ChatAttachment, 'digest' | 'revisionID'>[],
  incoming: readonly { digest?: string; revisionID?: string }[],
  cap: number,
): { accepted: number[]; message: string } {
  const digests = new Set(existing.flatMap((item) => item.digest ?? []))
  const revisions = new Set(existing.flatMap((item) => item.revisionID ?? []))
  const accepted: number[] = []
  let duplicate = false
  incoming.forEach((item, index) => {
    if (
      (item.digest && digests.has(item.digest)) ||
      (item.revisionID && revisions.has(item.revisionID))
    ) {
      duplicate = true
      return
    }
    if (item.digest) digests.add(item.digest)
    if (item.revisionID) revisions.add(item.revisionID)
    accepted.push(index)
  })
  if (existing.length + accepted.length > cap)
    return { accepted: [], message: `最多 ${cap} 张` }
  return { accepted, message: duplicate ? attachmentMessages.duplicate : '' }
}
export function savedAttachments(items: readonly ChatAttachment[]) {
  return items.flatMap((item): SavedAttachment[] =>
    item.state === 'ready' && item.revisionID
      ? [
          {
            id: item.revisionID,
            title: item.title,
            kind: 'image',
            source: item.source,
            ...(item.digest ? { digest: item.digest } : {}),
          },
        ]
      : [],
  )
}

async function sha256(file: File): Promise<string> {
  const hash = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
  return Array.from(new Uint8Array(hash), (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('')
}

export function useChatAttachments(cap: number) {
  const [items, setItems] = useState<ChatAttachment[]>([])
  const [message, setMessage] = useState('')
  const [preparing, setPreparing] = useState(0)
  const mounted = useRef(false)
  const draftEpoch = useRef(0)
  const [capabilities, setCapabilities] = useState<MediaCapabilities | null>(
    null,
  )
  const itemsRef = useRef(items)
  itemsRef.current = items
  // 进行中的上传：移除时据此中止并取消会话。
  const running = useRef(
    new Map<string, { abort: AbortController; upload?: Upload }>(),
  )
  useEffect(() => {
    const controller = new AbortController()
    void read<MediaCapabilities>('/media-capabilities', controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) setCapabilities(value)
      })
      .catch(() => undefined)
    return () => controller.abort()
  }, [])
  useEffect(() => {
    mounted.current = true
    const live = running.current
    return () => {
      mounted.current = false
      for (const { abort } of live.values()) abort.abort()
      for (const item of itemsRef.current)
        if (item.preview) URL.revokeObjectURL(item.preview)
    }
  }, [])
  const patch = useCallback(
    (key: string, value: Partial<ChatAttachment>) =>
      setItems((old) =>
        old.map((item) => (item.key === key ? { ...item, ...value } : item)),
      ),
    [],
  )
  const upload = useCallback(
    (key: string, file: File, media: MediaCapabilities) => {
      const live = {
        abort: new AbortController(),
        upload: undefined as Upload | undefined,
      }
      running.current.set(key, live)
      patch(key, { state: 'uploading', error: undefined })
      void uploadFile(
        file,
        { kind: 'staged' },
        media,
        (progress) => {
          if (progress.upload) live.upload = progress.upload
        },
        live.abort.signal,
      )
        .then((done) => {
          if (live.abort.signal.aborted) return
          if (!done.content_revision_id) throw new Error('上传结果缺少内容')
          patch(key, { state: 'ready', revisionID: done.content_revision_id })
        })
        .catch((e: unknown) => {
          if (live.abort.signal.aborted) return
          patch(key, {
            state: 'failed',
            error: e instanceof Error ? e.message : '上传失败',
          })
        })
        .finally(() => {
          if (running.current.get(key) === live) running.current.delete(key)
        })
    },
    [patch],
  )
  async function addFiles(files: File[]) {
    setMessage('')
    if (!files.length) return
    if (!capabilities) {
      setMessage(attachmentMessages.preparing)
      return
    }
    for (const file of files) {
      const checked = classifyFile(file, capabilities, 'image')
      if ('error' in checked) {
        setMessage(checked.error)
        return
      }
    }
    // 读文件和计算摘要也属于发送前的准备，不能等上传开始才锁住发送。
    const epoch = draftEpoch.current
    setPreparing((count) => count + 1)
    try {
      const digests = await Promise.all(files.map(sha256))
      if (!mounted.current || epoch !== draftEpoch.current) return
      // 算摘要期间列表可能变了，以当时的列表为准。
      const { accepted, message: note } = admitAttachments(
        itemsRef.current,
        digests.map((digest) => ({ digest })),
        cap,
      )
      setMessage(note)
      const added = accepted.map((index) => ({
        key: crypto.randomUUID(),
        title: files[index].name,
        source: 'upload' as const,
        state: 'uploading' as const,
        digest: digests[index],
        file: files[index],
        preview: URL.createObjectURL(files[index]),
      }))
      if (!added.length) return
      itemsRef.current = [...itemsRef.current, ...added]
      setItems(itemsRef.current)
      for (const item of added) upload(item.key, item.file, capabilities)
    } catch (error) {
      if (mounted.current && epoch === draftEpoch.current)
        setMessage(
          error instanceof Error ? error.message : '无法读取参考图，请重新添加',
        )
    } finally {
      if (mounted.current && epoch === draftEpoch.current) setPreparing((count) => count - 1)
    }
  }
  function addRevision(revisionID: string, title: string) {
    const { accepted, message: note } = admitAttachments(
      itemsRef.current,
      [{ revisionID }],
      cap,
    )
    setMessage(note)
    if (!accepted.length) return
    itemsRef.current = [
      ...itemsRef.current,
      { key: revisionID, title, source: 'library', state: 'ready', revisionID },
    ]
    setItems(itemsRef.current)
  }
  function retry(key: string) {
    const item = itemsRef.current.find((entry) => entry.key === key)
    if (!item?.file || item.state !== 'failed' || !capabilities) return
    setMessage('')
    upload(key, item.file, capabilities)
  }
  function remove(key: string) {
    const live = running.current.get(key)
    if (live) {
      running.current.delete(key)
      live.abort.abort()
      // 会话已经建好的话一并取消，不留占着额度的会话；已经结束的服务端会拒绝。
      if (live.upload) void cancelUpload(live.upload).catch(() => undefined)
    }
    const item = itemsRef.current.find((entry) => entry.key === key)
    if (item?.preview) URL.revokeObjectURL(item.preview)
    itemsRef.current = itemsRef.current.filter((entry) => entry.key !== key)
    setItems(itemsRef.current)
    setMessage('')
  }
  // 刷新后恢复修订、标题和判重摘要，附件已经是可发送的状态。
  const restore = useCallback((saved: readonly SavedAttachment[]) => {
    itemsRef.current = saved.map((item) => ({
      key: item.id,
      title: item.title,
      source: item.source,
      state: 'ready',
      revisionID: item.id,
      ...(item.digest ? { digest: item.digest } : {}),
    }))
    setItems(itemsRef.current)
  }, [])
  function clear() {
    draftEpoch.current++
    setPreparing(0)
    for (const item of itemsRef.current) remove(item.key)
  }
  return {
    items,
    message,
    ready: !!capabilities,
    uploading: preparing > 0 || items.some((item) => item.state === 'uploading'),
    failed: items.some((item) => item.state === 'failed'),
    addFiles,
    addRevision,
    retry,
    remove,
    restore,
    clear,
  }
}
