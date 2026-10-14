import { useEffect, useState } from 'react'
import type { components } from '../../api/schema'
import * as api from './api.ts'
import { mediaTicket, pickRendition } from './media.ts'
import { getAuthSnapshot } from '../../auth/session.ts'

export type Versions = components['schemas']['CreativeNodeVersionPage']

// 版本条数据缓存：选中节点时版本条要立刻出现。
// - 版本页按节点缓存；修订键变化时先显示旧页，后台刷新（stale-while-revalidate）。
// - 内容修订不可变，按 id 缓存；媒体票据由 media.ts 按有效期缓存。
// - 悬停时预取版本页、内容和缩略图票据，点下去通常已就绪。
// 媒体响应是 no-store，图片字节预取不会被 <img> 复用，所以只预热到票据。

interface Entry {
  key: string
  page: Versions | null
  loading: { key: string; promise: Promise<void> } | null
  listeners: Set<() => void>
}

const entries = new Map<string, Entry>()
const contents = new Map<string, api.Content>()
const contentReads = new Map<string, Promise<void>>()
const contentLimit = 400
const warmLimit = 8

const scoped = (id: string) => `${getAuthSnapshot().sessionEpoch}:${id}`

function entryOf(nodeID: string): Entry {
  const k = scoped(nodeID)
  let e = entries.get(k)
  if (!e) {
    e = { key: '', page: null, loading: null, listeners: new Set() }
    entries.set(k, e)
  }
  return e
}

export function cachedContent(id: string): api.Content | undefined {
  return contents.get(scoped(id))
}

function readContent(id: string): Promise<void> {
  const k = scoped(id)
  if (contents.has(k)) return Promise.resolve()
  let read = contentReads.get(k)
  if (!read) {
    read = api
      .read<api.Content>(`/content-revisions/${encodeURIComponent(id)}`)
      .then((c) => {
        contents.set(k, c)
        if (contents.size > contentLimit)
          contents.delete(contents.keys().next().value as string)
      })
      .catch(() => {
        /* 缩略图是辅助视图，读取失败时留空。 */
      })
      .finally(() => contentReads.delete(k))
    contentReads.set(k, read)
  }
  return read
}

function warmTicket(id: string) {
  const c = cachedContent(id)
  const rendition = pickRendition(c?.media, 'display')
  if (!c || !rendition || (c.kind !== 'image' && c.kind !== 'video')) return
  void mediaTicket(
    c.id,
    rendition.role as 'original' | 'display',
    'display',
  ).catch(() => {})
}

/** 读取版本页及各版本内容；同一节点同一修订键的并发请求合并为一次。 */
export function loadVersions(
  canvasID: string,
  nodeID: string,
  key: string,
): Promise<void> {
  const e = entryOf(nodeID)
  if (e.page && e.key === key) return Promise.resolve()
  if (e.loading?.key === key) return e.loading.promise
  const promise = api
    .read<Versions>(
      `/canvases/${encodeURIComponent(canvasID)}/nodes/${encodeURIComponent(nodeID)}/versions`,
    )
    .then(async (page) => {
      // 内容先到再发布，版本条不会先出现一排空格子。
      await Promise.all(
        page.items.map((v) => readContent(v.content_revision_id)),
      )
      for (const v of page.items.slice(0, warmLimit))
        warmTicket(v.content_revision_id)
      e.page = page
      e.key = key
      for (const notify of e.listeners) notify()
    })
    .catch(() => {
      /* 版本条是辅助视图，读取失败时保留旧页。 */
    })
    .finally(() => {
      if (e.loading?.promise === promise) e.loading = null
    })
  e.loading = { key, promise }
  return promise
}

/** 悬停预取：只对已知有多个版本的节点调用。 */
export function prefetchVersions(
  canvasID: string,
  nodeID: string,
  key: string,
) {
  void loadVersions(canvasID, nodeID, key)
}

/** 版本条使用的数据：缓存命中立即返回，键变化时后台刷新并保留旧页。 */
export function useNodeVersions(
  canvasID: string,
  nodeID: string,
  key: string,
): Versions | null {
  const [, rerender] = useState(0)
  useEffect(() => {
    const e = entryOf(nodeID)
    const notify = () => rerender((n) => n + 1)
    e.listeners.add(notify)
    void loadVersions(canvasID, nodeID, key)
    return () => {
      e.listeners.delete(notify)
    }
  }, [canvasID, nodeID, key])
  return entryOf(nodeID).page
}
