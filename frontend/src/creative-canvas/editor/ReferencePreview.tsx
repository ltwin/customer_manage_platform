import { useLayoutEffect, useState, type CSSProperties, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { CharacterImage } from '../assets/CharacterFields.tsx'
import { characterOf } from '../assets/character.ts'
import type { Content } from './api.ts'
import { contentText } from './content.ts'
import { MediaFigure } from './MediaView.tsx'
import './referencePreview.css'

function referencePreviewPosition(anchor: HTMLElement): CSSProperties {
  const rect = anchor.getBoundingClientRect()
  const above = rect.top - 8
  const below = window.innerHeight - rect.bottom - 8
  return {
    left: Math.max(8, Math.min(rect.left, window.innerWidth - 208)),
    maxHeight: Math.max(0, Math.max(above, below) - 8),
    ...(above > below
      ? { bottom: window.innerHeight - above }
      : { top: rect.bottom + 8 }),
  }
}

export function ReferencePreview({
  id,
  anchor,
  title,
  description,
  children,
}: {
  id?: string
  anchor: HTMLElement
  title: string
  description?: string
  children?: ReactNode
}) {
  const [position, setPosition] = useState<CSSProperties | null>(null)
  useLayoutEffect(() => {
    const place = () => setPosition(anchor.isConnected ? referencePreviewPosition(anchor) : null)
    place()
    window.addEventListener('scroll', place, true)
    window.addEventListener('resize', place)
    return () => {
      window.removeEventListener('scroll', place, true)
      window.removeEventListener('resize', place)
    }
  }, [anchor])
  if (!position) return null
  return createPortal(
    <div id={id} className="cc-generation-ref-peek" role="tooltip" style={position}>
      {children}
      <div>
        <b>{title}</b>
        {description && <span>{description}</span>}
      </div>
    </div>,
    document.querySelector('.cc-workspace') ?? document.body,
  )
}

export function ReferenceContent({ content, title }: { content: Content; title: string }) {
  const card = characterOf(content)
  if (card) {
    const cover = card.views[0]
    return cover ? <CharacterImage id={cover.content_revision_id} label={card.name} /> : <p>{card.name}</p>
  }
  if (['image', 'video', 'audio', 'model', 'material'].includes(content.kind))
    return <MediaFigure content={content} kind={content.kind} title={title} />
  return <p>{contentText(content.payload)}</p>
}
