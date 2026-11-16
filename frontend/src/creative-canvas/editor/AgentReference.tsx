import { useEffect, useId, useRef, useState } from 'react'
import { Box } from 'lucide-react'
import type { components } from '../../api/schema'
import * as api from './api.ts'
import type { MessagePart } from './agentOutgoing.ts'
import { ReferenceContent, ReferencePreview } from './ReferencePreview.tsx'
import { cachedContent, loadContentRevision } from './versionCache.ts'

type Chip = Extract<MessagePart, { kind: 'chip' }>

export function AgentReference({ part, legacySelection = false }: { part: Chip; legacySelection?: boolean }) {
  const id = useId()
  const chip = useRef<HTMLSpanElement>(null)
  const [anchor, setAnchor] = useState<HTMLSpanElement | null>(null)
  const open = anchor !== null
  useEffect(() => {
    if (!open) return
    const close = () => setAnchor(null)
    const onScroll = () => {
      if (document.activeElement !== chip.current) close()
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close()
    }
    window.addEventListener('scroll', onScroll, true)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('scroll', onScroll, true)
      window.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <span
      ref={chip}
      className={legacySelection ? 'cc-agent-scene-tag' : 'cc-agent-chip'}
      data-scene={part.scene}
      tabIndex={0}
      aria-describedby={anchor ? id : undefined}
      onPointerEnter={(event) => {
        if (event.pointerType !== 'touch') setAnchor(event.currentTarget)
      }}
      onPointerLeave={(event) => {
        if (document.activeElement !== event.currentTarget) setAnchor(null)
      }}
      onFocus={(event) => setAnchor(event.currentTarget)}
      onBlur={() => setAnchor(null)}
      onKeyDown={(event) => {
        if (event.key === 'Escape' && anchor) {
          event.stopPropagation()
          setAnchor(null)
        }
      }}
    >
      {part.scene && <Box size={12} aria-hidden="true" />}
      {legacySelection && <span className="sr-only">选中的物件：</span>}
      {legacySelection ? <span>{part.title}</span> : part.title}
      {anchor && (
        <ReferencePreview
          id={id}
          anchor={anchor}
          title={part.sceneTitle ?? part.title}
          description={part.skill ? `Skill · 第 ${part.skill.version_number} 版` : part.scene && !legacySelection ? '场景引用' : undefined}
        >
          {part.scene ? (
            <p>{part.detail}</p>
          ) : part.skill ? (
            <SkillPreview key={part.skill.skill_version_id} skill={part.skill} />
          ) : part.revisionID ? (
            <MaterialPreview key={part.revisionID} revisionID={part.revisionID} title={part.title} />
          ) : (
            <p>引用详情不可用</p>
          )}
        </ReferencePreview>
      )}
    </span>
  )
}

function MaterialPreview({ revisionID, title }: { revisionID: string; title: string }) {
  const [content, setContent] = useState<api.Content | null | undefined>(() => cachedContent(revisionID))
  useEffect(() => {
    if (cachedContent(revisionID)) return
    let live = true
    void loadContentRevision(revisionID).then((value) => {
      if (live) setContent(value ?? null)
    })
    return () => {
      live = false
    }
  }, [revisionID])
  if (content) return <ReferenceContent content={content} title={title} />
  return <p>{content === null ? '素材暂不可预览' : '正在读取素材…'}</p>
}

function SkillPreview({ skill }: { skill: NonNullable<Chip['skill']> }) {
  const [description, setDescription] = useState('')
  useEffect(() => {
    const abort = new AbortController()
    void api
      .read<components['schemas']['CreativeAgentSkillVersion']>(
        `/agent/skills/${encodeURIComponent(skill.skill_id)}/versions/${encodeURIComponent(skill.skill_version_id)}`,
        abort.signal,
      )
      .then((value) => {
        if (!abort.signal.aborted) setDescription(value.description)
      })
      .catch(() => {})
    return () => abort.abort()
  }, [skill.skill_id, skill.skill_version_id])
  return description ? <p>{description}</p> : null
}
