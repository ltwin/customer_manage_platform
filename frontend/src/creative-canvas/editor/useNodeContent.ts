import { useEffect, useState } from 'react'
import * as api from './api.ts'
import type { Draft } from './journal.ts'
import { blankDraft, contentText } from './content.ts'
import { errorMessage } from './queue.ts'

// A failed node read belongs to that selection; it cannot disable the canvas.
export function useNodeContent(node: api.CanvasNode | undefined) {
  const [full, setFull] = useState<Draft | null>(null)
  const [error, setError] = useState('')
  const [retryTick, setRetryTick] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    setFull(null)
    setError('')
    if (!node) return
    const base: Draft = {
      ...blankDraft(
        (node.metadata.type_key.startsWith('core.')
          ? node.metadata.type_key.slice(5)
          : 'text') as Draft['kind'],
      ),
      title: node.metadata.title,
      dataRevision: node.data_revision,
      contentRevision: node.data.content_revision_id,
    }
    if (!node.data.content_revision_id) {
      setFull(base)
      return
    }
    void api
      .read<api.Content>(
        `/content-revisions/${encodeURIComponent(node.data.content_revision_id)}`,
        controller.signal,
      )
      .then((r) => {
        if (!controller.signal.aborted)
          setFull({
            ...base,
            value: contentText(r.payload),
          })
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(e))
      })
    return () => controller.abort()
  }, [node, retryTick])
  return { full, error, retry: () => setRetryTick(value => value + 1) }
}
