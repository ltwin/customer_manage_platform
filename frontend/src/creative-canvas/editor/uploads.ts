import { useCallback, useEffect, useRef, useState } from 'react'
import { read } from './api.ts'
import {
  classifyFile,
  uploadFile,
  type ContentRights,
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
  rights: ContentRights
  file: File
  stage: UploadProgress['stage'] | 'queued' | 'rejected'
  sent: number
  message: string
  upload?: Upload
}

const concurrency = 3
const settled = (stage: UploadItem['stage']) =>
  stage === 'done' || stage === 'failed' || stage === 'rejected'

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
    let running = items.filter(
      (i) => !settled(i.stage) && i.stage !== 'queued',
    ).length
    for (const item of items) {
      if (running >= concurrency) break
      if (item.stage !== 'queued' || started.current.has(item.id)) continue
      started.current.add(item.id)
      running++
      patch(item.id, { stage: 'creating' })
      void uploadFile(
        item.file,
        item.target,
        item.rights,
        capabilities,
        (p) =>
          patch(item.id, { stage: p.stage, sent: p.sent, upload: p.upload }),
      )
        .then((upload) => {
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
          patch(item.id, {
            stage: 'failed',
            message: e instanceof Error ? e.message : '上传失败',
          })
        })
        .finally(() => {
          started.current.delete(item.id)
        })
    }
  }, [items, capabilities, patch])
  const start = useCallback(
    (
      files: File[],
      target: (file: File, index: number) => UploadTarget,
      rights: ContentRights,
    ) => {
      if (!capabilities) return []
      const batch: UploadItem[] = files
        .slice(0, capabilities.batch_limit)
        .map((file, i) => {
          const classified = classifyFile(file, capabilities)
          return {
            id: crypto.randomUUID(),
            name: file.name,
            size: file.size,
            target: target(file, i),
            rights,
            file,
            stage: 'error' in classified ? 'rejected' : 'queued',
            sent: 0,
            message: 'error' in classified ? classified.error : '',
          }
        })
      setItems((old) => [...old, ...batch])
      return batch
    },
    [capabilities],
  )
  // A retry is a brand-new session for the same file and target.
  const retry = useCallback((id: string) => {
    setItems((old) =>
      old.map((i) =>
        i.id === id && (i.stage === 'failed' || i.stage === 'rejected')
          ? { ...i, stage: 'queued', sent: 0, message: '', upload: undefined }
          : i,
      ),
    )
  }, [])
  const dismiss = useCallback(() => {
    setItems((old) => old.filter((i) => !settled(i.stage)))
  }, [])
  const busy = items.some((i) => !settled(i.stage))
  return { capabilities, capabilitiesError, items, start, retry, dismiss, busy }
}
