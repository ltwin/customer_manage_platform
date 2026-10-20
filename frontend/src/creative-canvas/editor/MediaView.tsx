import { useEffect, useRef, useState } from 'react'
import { AudioLines, Box, Film, ImageOff } from 'lucide-react'
import type { Content } from './api.ts'
import {
  cachedMediaTicket,
  invalidateMediaTicket,
  mediaTicket,
  pickRendition,
  type MediaObject,
  type TicketRole,
} from './media.ts'
import { getAuthSnapshot } from '../../auth/session.ts'

// 引用 chip 的缩略图：每份图片内容取一张展示图的地址，按内容 id 索引。
export function useThumbnails(
  contents: readonly (Content | undefined)[],
): Record<string, string> {
  const [thumbnails, setThumbnails] = useState<Record<string, string>>({})
  const images = contents.filter(
    (c): c is Content => c?.kind === 'image' && !!pickRendition(c.media, 'display'),
  )
  const key = images.map((c) => c.id).join(',')
  const latest = useRef(images)
  latest.current = images
  useEffect(() => {
    let live = true
    void Promise.all(
      latest.current.map(async (content) => {
        const media = pickRendition(content.media, 'display')!
        try {
          const ticket = await mediaTicket(
            content.id,
            media.role === 'original' ? 'original' : 'display',
            'display',
          )
          return { id: content.id, url: ticket.url }
        } catch {
          return null
        }
      }),
    ).then((values) => {
      if (live)
        setThumbnails(
          Object.fromEntries(values.flatMap((v) => (v ? [[v.id, v.url]] : []))),
        )
    })
    return () => {
      live = false
    }
  }, [key])
  return thumbnails
}

// useMediaURL resolves a ticketed URL for one revision/rendition lazily.
export function useMediaURL(
  revision: string | null | undefined,
  media: MediaObject[] | undefined,
  prefer: 'display' | 'original' | 'thumbnail',
  active: boolean,
): { url: string | null; error: string | null; retry: () => void } {
  const [state, setState] = useState<{
    key: string
    url: string | null
    error: string | null
  }>({ key: '', url: null, error: null })
  const [attempt, setAttempt] = useState(0)
  const rendition = pickRendition(media, prefer)
  const key =
    revision && rendition
      ? `${getAuthSnapshot().sessionEpoch}:${revision}:${rendition.role}`
      : ''
  useEffect(() => {
    if (!active || !key || !revision || !rendition) return
    let live = true
    void mediaTicket(
      revision,
      rendition.role as TicketRole,
      'display',
    )
      .then((ticket) => {
        if (live) setState({ key, url: ticket.url, error: null })
      })
      .catch((e: unknown) => {
        if (live)
          setState({
            key,
            url: null,
            error: e instanceof Error ? e.message : '媒体暂时不可用',
          })
      })
    return () => {
      live = false
    }
  }, [active, key, revision, rendition, attempt])
  return {
    url:
      state.key === key
        ? state.url
        : active && revision && rendition
          ? (cachedMediaTicket(
              revision,
              rendition.role as TicketRole,
            )?.url ?? null)
          : null,
    error: state.key === key ? state.error : null,
    retry: () => {
      if (revision && rendition)
        invalidateMediaTicket(
          revision,
          rendition.role as TicketRole,
        )
      setState({ key: '', url: null, error: null })
      setAttempt((n) => n + 1)
    },
  }
}

// useNearViewport defers ticket requests until the element is close to view.
export function useNearViewport<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  const [near, setNear] = useState(false)
  useEffect(() => {
    const el = ref.current
    if (!el || near) return
    if (!('IntersectionObserver' in window)) {
      setNear(true)
      return
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) setNear(true)
      },
      { rootMargin: '320px' },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [near])
  return { ref, near }
}

// hoverPlay is the canvas preview mode for video nodes: the pointer drives
// playback (enter plays, leave pauses, a fresh enter resumes after a manual
// pause) while the native controls stay available. The video opts out of the
// nodrag guard so the node stays draggable from its surface; presses in the
// control-bar strip borrow the guard for the press only.
export function MediaFigure({
  content,
  kind,
  title,
  className,
  controls = false,
  prefer = 'display',
  hoverPlay = false,
  onReady,
}: {
  content: Content | undefined
  kind: string
  title: string
  className?: string
  controls?: boolean
  prefer?: 'display' | 'original'
  hoverPlay?: boolean
  /** 媒体真正可以显示时回调（图片解码完成、视频拿到首帧）。 */
  onReady?: () => void
}) {
  const { ref, near } = useNearViewport<HTMLDivElement>()
  const video = useRef<HTMLVideoElement>(null)
  const hover = hoverPlay && kind === 'video'
  const { url, error, retry } = useMediaURL(
    content?.id,
    content?.media,
    kind === 'image' ? prefer : kind === 'model' ? 'thumbnail' : 'original',
    near,
  )
  const [failed, setFailed] = useState(false)
  useEffect(() => setFailed(false), [url])
  const problem = error || (failed ? '媒体加载失败' : null)
  return (
    <div
      ref={ref}
      className={`cc-media ${className ?? ''} ${url && !problem ? 'is-ready' : ''}`}
      data-kind={kind}
      onMouseEnter={
        hover
          ? () => {
              void video.current?.play().catch(() => {})
            }
          : undefined
      }
      onMouseLeave={hover ? () => video.current?.pause() : undefined}
    >
      {problem ? (
        <button
          type="button"
          className="cc-media-retry nodrag"
          onClick={() => {
            setFailed(false)
            retry()
          }}
        >
          <ImageOff size={18} />
          <span>{problem} · 点击重试</span>
        </button>
      ) : !url ? (
        <span className="cc-media-placeholder" aria-hidden="true">
          {kind === 'video' ? (
            <Film size={22} />
          ) : kind === 'audio' ? (
            <AudioLines size={22} />
          ) : kind === 'model' ? (
            <Box size={22} />
          ) : null}
        </span>
      ) : kind === 'image' || kind === 'model' ? (
        <img
          src={url}
          alt={title}
          draggable={false}
          decoding="async"
          onLoad={onReady}
          onError={() => setFailed(true)}
        />
      ) : kind === 'video' ? (
        <video
          ref={video}
          src={url}
          aria-label={title}
          controls={controls}
          preload="metadata"
          playsInline
          muted={!controls}
          className={hover ? undefined : 'nodrag'}
          onPointerDownCapture={
            hover
              ? (event) => {
                  // The native control bar occupies the bottom strip of the
                  // video: presses there must reach the controls instead of
                  // starting a node drag, so lend the element the drag guard
                  // for the duration of that press.
                  const el = event.currentTarget
                  if (event.clientY < el.getBoundingClientRect().bottom - 48)
                    return
                  el.classList.add('nodrag')
                  const release = () => el.classList.remove('nodrag')
                  window.addEventListener('pointerup', release, { once: true })
                  window.addEventListener('pointercancel', release, {
                    once: true,
                  })
                }
              : undefined
          }
          onLoadedData={onReady}
          onError={() => setFailed(true)}
        />
      ) : controls ? (
        <div className="cc-audio nodrag">
          <AudioLines size={26} />
          <audio
            src={url}
            aria-label={title}
            controls
            preload="metadata"
            onLoadedMetadata={onReady}
            onError={() => setFailed(true)}
          />
        </div>
      ) : (
        <span className="cc-audio" aria-hidden="true">
          <AudioLines size={26} />
        </span>
      )}
    </div>
  )
}

// downloadMedia asks for a download ticket and lets the browser save the
// original bytes under the node/asset title; never modifies content.
export async function downloadMedia(
  content: Content,
  title: string,
): Promise<void> {
  const original = pickRendition(content.media, 'original')
  if (!original) throw new Error('没有可下载的媒体')
  const extension =
    {
      'image/jpeg': 'jpg',
      'image/png': 'png',
      'image/webp': 'webp',
      'video/mp4': 'mp4',
      'video/webm': 'webm',
      'audio/mpeg': 'mp3',
      'audio/wav': 'wav',
      'model/gltf-binary': 'glb',
    }[original.mime] ?? ''
  const base = Array.from(title || '媒体')
    .map((ch) =>
      ch.charCodeAt(0) < 32 || '\\/:*?"<>|'.includes(ch) ? '_' : ch,
    )
    .join('')
    .slice(0, 100)
  const name =
    extension && !base.toLowerCase().endsWith('.' + extension)
      ? `${base}.${extension}`
      : base
  const ticket = await mediaTicket(content.id, 'original', 'download', name)
  // Fetch through the ticket, then save the bytes: no navigation, and a
  // failed authorization surfaces as an error instead of a blank tab.
  const response = await fetch(ticket.url, { credentials: 'omit' })
  if (!response.ok) throw new Error('媒体暂时无法下载，请稍后重试')
  const blob = await response.blob()
  const objectURL = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = objectURL
  link.download = name
  document.body.append(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(objectURL), 60000)
}
