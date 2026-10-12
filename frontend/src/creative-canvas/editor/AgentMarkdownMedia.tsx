import { useState, type ReactNode } from 'react'
import { linkedMediaKind, mediaSource } from './markdownMedia.ts'

export function MarkdownImage({
  src,
  alt,
  title,
}: {
  src?: string
  alt?: string
  title?: string
}) {
  const source = mediaSource(src)
  if (!source)
    return (
      <span className="cc-md-image-alt">[图片：{alt || '地址不可用'}]</span>
    )
  return (
    <ImagePreview key={source} src={source} alt={alt || '图片'} title={title} />
  )
}

function ImagePreview({
  src,
  alt,
  title,
}: {
  src: string
  alt: string
  title?: string
}) {
  const [failed, setFailed] = useState(false)
  const [attempt, setAttempt] = useState(0)
  if (failed)
    return (
      <span className="cc-md-image-alt" role="status">
        图片加载失败：{alt}{' '}
        <button
          type="button"
          onClick={() => {
            setAttempt((value) => value + 1)
            setFailed(false)
          }}
        >
          重试
        </button>
      </span>
    )
  return (
    <img
      key={attempt}
      className="cc-md-image"
      src={src}
      alt={alt}
      title={title}
      loading="lazy"
      decoding="async"
      referrerPolicy="no-referrer"
      onError={() => setFailed(true)}
    />
  )
}

export function MarkdownLink({
  href,
  children,
}: {
  href?: string
  children?: ReactNode
}) {
  const kind = linkedMediaKind(href)
  // Partial streaming links have no usable destination yet.
  const link =
    href === 'streamdown:incomplete-link' ? (
      <span>{children}</span>
    ) : (
      <a href={href} target="_blank" rel="noopener noreferrer">
        {children}
      </a>
    )
  if (!kind || !href) return link
  return (
    <MediaPreview key={href} src={href} kind={kind}>
      {link}
    </MediaPreview>
  )
}

function MediaPreview({
  src,
  kind,
  children,
}: {
  src: string
  kind: 'video' | 'audio'
  children: ReactNode
}) {
  const [failed, setFailed] = useState(false)
  const [attempt, setAttempt] = useState(0)
  const props = {
    src,
    controls: true,
    preload: 'metadata',
    onError: () => setFailed(true),
  }
  return (
    <span className="cc-md-media" data-kind={kind}>
      {failed ? (
        <span role="status">
          媒体无法播放，可打开原链接查看。{' '}
          <button
            type="button"
            onClick={() => {
              setAttempt((value) => value + 1)
              setFailed(false)
            }}
          >
            重试
          </button>
        </span>
      ) : kind === 'video' ? (
        <video key={attempt} {...props} playsInline aria-label="视频播放器" />
      ) : (
        <audio key={attempt} {...props} aria-label="音频播放器" />
      )}
      <span className="cc-md-media-link">{children}</span>
    </span>
  )
}
