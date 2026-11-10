import { useEffect, useState } from 'react'
import type { Content } from './api.ts'
import {
  cachedMediaTicket,
  invalidateMediaTicket,
  mediaTicket,
  pickRendition,
} from './media.ts'
import type { ViewerImageCache } from '../viewer/imageCache.ts'
import { getAuthSnapshot, subscribeAuth } from '../../auth/session.ts'

/** 只负责画布原图预览；缓存由画布持有，弹窗关闭只释放当前读者。 */
export default function PreviewImage({
  content,
  title,
  images,
}: {
  content: Content
  title: string
  images: ViewerImageCache
}) {
  const rendition = pickRendition(content.media, 'original')
  const available = !!rendition
  const role = rendition?.role === 'original' ? 'original' : 'display'
  const [url, setURL] = useState<string | null>(() => {
    const ticket = cachedMediaTicket(content.id, role)
    return ticket ? images.peek(ticket.url) ?? null : null
  })
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    let live = true
    const controller = new AbortController()
    let release: (() => void) | undefined
    if (!available) {
      setError('图片暂时不可用')
      return
    }
    const epoch = getAuthSnapshot().sessionEpoch
    const unsubscribe = subscribeAuth(() => {
      if (getAuthSnapshot().sessionEpoch !== epoch) controller.abort()
    })
    void mediaTicket(content.id, role, 'display')
      .then(async (ticket) => {
        controller.signal.throwIfAborted()
        const lease = await images.load(
          ticket.url,
          controller,
          Date.parse(ticket.expires_at),
        )
        if (!live || controller.signal.aborted) {
          lease.release()
          return
        }
        release = lease.release
        setURL(lease.url)
      })
      .catch((e: unknown) => {
        if (live) setError(e instanceof Error ? e.message : '媒体加载失败')
      })
    return () => {
      live = false
      unsubscribe()
      controller.abort()
      release?.()
    }
  }, [content.id, role, available, images, attempt])
  return (
    <div className={`cc-media ${url && !error ? 'is-ready' : ''}`} data-kind="image">
      {error ? (
        <button
          className="cc-media-retry"
          onClick={() => {
            const ticket = cachedMediaTicket(content.id, role)
            if (ticket) images.invalidate(ticket.url)
            invalidateMediaTicket(content.id, role)
            setURL(null)
            setError('')
            setAttempt((n) => n + 1)
          }}
        >
          {error} · 点击重试
        </button>
      ) : url ? (
        <img
          src={url}
          alt={title}
          draggable={false}
          decoding="async"
          onError={() => {
            const ticket = cachedMediaTicket(content.id, role)
            if (ticket) images.invalidate(ticket.url)
            setError('媒体加载失败')
          }}
        />
      ) : (
        <span role="status">正在加载图片…</span>
      )}
    </div>
  )
}
