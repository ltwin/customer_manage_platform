import { useCallback, useEffect, useRef, useState } from 'react'
import { read } from './api.ts'
import {
  cancelUpload,
  classifyFile,
  discardCandidate,
  readableFileName,
  settledUpload,
  uploadFile,
  type MediaCapabilities,
  type Upload,
  type UploadProgress,
  type UploadTarget,
} from './media.ts'

export type UploadItem = {
  id: string
  name: string
  size: number
  target: UploadTarget
  file: File
  stage: UploadProgress['stage'] | 'queued' | 'rejected' | 'cancelled'
  sent: number
  message: string
  upload?: Upload
  /** 拖入画布的一批：与建节点同一撤销组，撤销时整批取消。 */
  batch?: string
  /** 只是说明为什么没导入，没有可重试的目标。 */
  final?: boolean
}

const concurrency = 3
// 撤销之后才完成的上传：已绑定的内容随撤销组回退；落成候选的直接放弃。
function dropLate(upload: Upload) {
  const p = upload.publication
  if (upload.binding?.status === 'needs_review' && p?.kind === 'candidate')
    void discardCandidate(p.id, p.revision).catch(() => {})
}
const settled = (stage: UploadItem['stage']) =>
  stage === 'done' ||
  stage === 'failed' ||
  stage === 'rejected' ||
  stage === 'cancelled'

// useUploads runs queued items with bounded concurrency, keeps every item
// visible until dismissed, and reports each finished upload exactly once.
// Starting is driven by an effect over the item list, so retries (which only
// change a stage) and batch additions both schedule work without a side
// effect inside a state updater.
export function useUploads(onDone: (item: UploadItem) => void) {
  const [capabilities, setCapabilities] = useState<MediaCapabilities | null>(
    null,
  )
  const [capabilitiesError, setCapabilitiesError] = useState('')
  const [items, setItems] = useState<UploadItem[]>([])
  const started = useRef(new Set<string>())
  // 进行中会话的中止器与最新会话，撤销时据此取消。
  const running = useRef(new Map<string, { abort: AbortController; upload?: Upload }>())
  const cancelled = useRef(new Set<string>())
  const done = useRef(onDone)
  done.current = onDone
  useEffect(() => {
    const controller = new AbortController()
    void read<MediaCapabilities>('/media-capabilities', controller.signal)
      .then((c) => {
        if (!controller.signal.aborted) setCapabilities(c)
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted)
          setCapabilitiesError(
            e instanceof Error ? e.message : '媒体能力不可用',
          )
      })
    return () => controller.abort()
  }, [])
  const patch = useCallback((id: string, value: Partial<UploadItem>) => {
    setItems((old) => old.map((i) => (i.id === id ? { ...i, ...value } : i)))
  }, [])
  useEffect(() => {
    if (!capabilities) return
    let busy = items.filter(
      (i) => !settled(i.stage) && i.stage !== 'queued',
    ).length
    for (const item of items) {
      if (busy >= concurrency) break
      if (item.stage !== 'queued' || started.current.has(item.id)) continue
      started.current.add(item.id)
      busy++
      const live = { abort: new AbortController(), upload: undefined as Upload | undefined }
      running.current.set(item.id, live)
      patch(item.id, { stage: 'creating' })
      void uploadFile(
        item.file,
        item.target,
        capabilities,
        (p) => {
          if (p.upload) live.upload = p.upload
          if (!live.abort.signal.aborted)
            patch(item.id, { stage: p.stage, sent: p.sent, upload: p.upload })
        },
        live.abort.signal,
      )
        .then((upload) => {
          if (item.batch && cancelled.current.has(item.batch)) {
            dropLate(upload)
            return
          }
          const finished = {
            ...item,
            stage: 'done' as const,
            upload,
            sent: item.size,
          }
          patch(item.id, finished)
          done.current(finished)
        })
        .catch((e: unknown) => {
          if (live.abort.signal.aborted) return
          patch(item.id, {
            stage: 'failed',
            message: e instanceof Error ? e.message : '上传失败',
          })
        })
        .finally(() => {
          started.current.delete(item.id)
          running.current.delete(item.id)
        })
    }
  }, [items, capabilities, patch])
  const start = useCallback(
    (
      files: File[],
      target: (file: File, index: number) => UploadTarget,
      batch?: string,
    ) => {
      if (!capabilities) return []
      const items: UploadItem[] = files
        .slice(0, capabilities.batch_limit)
        .map((file, i) => {
          const classified = classifyFile(file, capabilities)
          return {
            id: crypto.randomUUID(),
            name: readableFileName(file.name),
            size: file.size,
            target: target(file, i),
            file,
            stage: 'error' in classified ? 'rejected' : 'queued',
            sent: 0,
            message: 'error' in classified ? classified.error : '',
            batch,
          }
        })
      setItems((old) => [...old, ...items])
      return items
    },
    [capabilities],
  )
  // 拖入时不建节点的文件：逐项说明原因，不能重试（没有目标节点）。
  const reject = useCallback(
    (rejected: { file: File; message: string }[]) => {
      if (!rejected.length) return
      setItems((old) => [
        ...old,
        ...rejected.map(({ file, message }) => ({
          id: crypto.randomUUID(),
          name: readableFileName(file.name),
          size: file.size,
          target: { kind: 'asset' as const },
          file,
          stage: 'rejected' as const,
          sent: 0,
          message,
          final: true,
        })),
      ])
    },
    [],
  )
  // 撤销导入：没开始的直接取消，进行中的中止本机请求并取消服务器会话。
  const cancelBatch = useCallback(
    (batch: string) => {
      cancelled.current.add(batch)
      for (const item of items) {
        if (item.batch !== batch || settled(item.stage)) continue
        const live = running.current.get(item.id)
        live?.abort.abort()
        const upload = live?.upload ?? item.upload
        if (upload)
          void cancelUpload(upload).catch(() =>
            settledUpload(upload.id)
              .then(dropLate)
              .catch(() => {}),
          )
      }
      setItems((old) =>
        old.map((i) =>
          i.batch === batch && !settled(i.stage)
            ? { ...i, stage: 'cancelled', message: '已撤销' }
            : i,
        ),
      )
    },
    [items],
  )
  // A retry is a brand-new session for the same file and target.
  const retry = useCallback((id: string) => {
    setItems((old) =>
      old.map((i) =>
        i.id === id && !i.final && (i.stage === 'failed' || i.stage === 'rejected')
          ? { ...i, stage: 'queued', sent: 0, message: '', upload: undefined }
          : i,
      ),
    )
  }, [])
  const dismiss = useCallback(() => {
    setItems((old) => old.filter((i) => !settled(i.stage)))
  }, [])
  const isCancelled = useCallback(
    (batch: string) => cancelled.current.has(batch),
    [],
  )
  const busy = items.some((i) => !settled(i.stage))
  return {
    capabilities,
    capabilitiesError,
    items,
    start,
    reject,
    cancelBatch,
    isCancelled,
    retry,
    dismiss,
    busy,
  }
}
