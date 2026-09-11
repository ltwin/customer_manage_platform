import { useEffect, useRef, useState } from 'react'
import { AudioLines, Film, ImageOff } from 'lucide-react'
import type { Content } from './api.ts'
import { mediaTicket, pickRendition, type MediaObject } from './media.ts'

// useMediaURL resolves a ticketed URL for one revision/rendition lazily.
export function useMediaURL(
  revision: string | null | undefined,
  media: MediaObject[] | undefined,
  prefer: 'display' | 'original',
  active: boolean,
): { url: string | null; error: string | null; retry: () => void } {
  const [state, setState] = useState<{
    key: string
    url: string | null
    error: string | null
  }>({ key: '', url: null, error: null })
  const [attempt, setAttempt] = useState(0)
  const rendition = pickRendition(media, prefer)
  const key = revision && rendition ? `${revision}:${rendition.role}` : ''
  useEffect(() => {
    if (!active || !key || !revision || !rendition) return
    let live = true
    void mediaTicket(revision, rendition.role as 'original' | 'display', 'display')
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
    url: state.key === key ? state.url : null,
    error: state.key === key ? state.error : null,
    retry: () => setAttempt((n) => n + 1),
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
}: {
  content: Content | undefined
  kind: string
  title: string
  className?: string
  controls?: boolean
  prefer?: 'display' | 'original'
  hoverPlay?: boolean
}) {
  const { ref, near } = useNearViewport<HTMLDivElement>()
  const video = useRef<HTMLVideoElement>(null)
  const hover = hoverPlay && kind === 'video'
  const { url, error, retry } = useMediaURL(
    content?.id,
    content?.media,
    kind === 'image' ? prefer : 'original',
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
          {kind === 'video' ? <Film size={22} /> : kind === 'audio' ? <AudioLines size={22} /> : null}
        </span>
      ) : kind === 'image' ? (
        <img
          src={url}
          alt={title}
          draggable={false}
          decoding="async"
          onError={() => setFailed(true)}
        />
      ) : kind === 'video' ? (
        <video
          ref={video}
          src={url}
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
          onError={() => setFailed(true)}
        />
      ) : controls ? (
        <div className="cc-audio nodrag">
          <AudioLines size={26} />
          <audio src={url} controls preload="metadata" onError={() => setFailed(true)} />
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
