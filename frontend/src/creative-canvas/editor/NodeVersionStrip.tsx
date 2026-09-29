import { useEffect, useRef, useState } from 'react'
import { Check } from 'lucide-react'
import type { components } from '../../api/schema'
import * as api from './api.ts'
import { MediaFigure } from './MediaView.tsx'

type Versions = components['schemas']['CreativeNodeVersionPage']

// v6 版本条：选中且有多个版本的媒体节点在卡片底部显示缩略图。
// 点缩略图只在卡片内预览，不改变下游；「设为主版本」才写回节点。
export default function NodeVersionStrip({
  canvasID,
  nodeID,
  kind,
  dataRevision,
  viewing,
  disabled,
  onPreview,
  onSelectVersion,
}: {
  canvasID: string
  nodeID: string
  kind: string
  dataRevision: string
  /** 正在卡片内预览的内容修订；null 表示显示主版本。 */
  viewing: string | null
  disabled: boolean
  onPreview: (content: api.Content | null) => void
  onSelectVersion: (versionID: string) => void
}) {
  const [versions, setVersions] = useState<Versions | null>(null)
  const [contents, setContents] = useState<Record<string, api.Content>>({})
  // 每个内容修订只读一次；失败的缩略图留空，不反复重试。
  const requested = useRef(new Set<string>())
  useEffect(() => {
    const controller = new AbortController()
    api
      .read<Versions>(
        `/canvases/${encodeURIComponent(canvasID)}/nodes/${encodeURIComponent(nodeID)}/versions`,
        controller.signal,
      )
      .then((v) => {
        if (!controller.signal.aborted) setVersions(v)
      })
      .catch(() => {
        /* 版本条是辅助视图，读取失败时不显示。 */
      })
    return () => controller.abort()
  }, [canvasID, nodeID, dataRevision])
  useEffect(() => {
    const missing = (versions?.items ?? []).filter(
      (v) => !requested.current.has(v.content_revision_id),
    )
    if (!missing.length) return
    for (const v of missing) requested.current.add(v.content_revision_id)
    const controller = new AbortController()
    void Promise.all(
      missing.map((v) =>
        api
          .read<api.Content>(
            `/content-revisions/${encodeURIComponent(v.content_revision_id)}`,
            controller.signal,
          )
          .catch(() => null),
      ),
    ).then((read) => {
      if (controller.signal.aborted) {
        for (const v of missing) requested.current.delete(v.content_revision_id)
        return
      }
      setContents((old) => {
        const next = { ...old }
        for (const c of read) if (c) next[c.id] = c
        return next
      })
    })
    return () => controller.abort()
  }, [versions])
  const items = versions?.items ?? []
  if (items.length < 2) return null
  const main = versions?.selected_version_id ?? null
  const current = viewing
    ? (items.find((v) => v.content_revision_id === viewing)?.id ?? main)
    : main
  const viewingVersion = items.find((v) => v.id === current)
  return (
    <div
      className="cc-versions nodrag nopan"
      role="group"
      aria-label="作品版本"
      onPointerDown={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
    >
      {items.map((v) => {
        const content = contents[v.content_revision_id]
        return (
          <button
            key={v.id}
            className={`${v.id === current ? 'is-on' : ''} ${v.id === main ? 'is-main' : ''}`}
            aria-label={`版本 ${v.version_no}${v.id === main ? '（主版本）' : ''}`}
            aria-pressed={v.id === current}
            onClick={() => onPreview(v.id === main ? null : (content ?? null))}
          >
            {content && (
              <MediaFigure content={content} kind={kind} title={`版本 ${v.version_no}`} />
            )}
            {v.id === main && (
              <span className="cc-main-dot" aria-hidden="true">
                <Check size={9} strokeWidth={3} />
              </span>
            )}
          </button>
        )
      })}
      <span className="cc-versions-grow" />
      {viewingVersion && viewingVersion.id !== main ? (
        <button
          className="cc-set-main"
          disabled={disabled}
          onClick={() => {
            onSelectVersion(viewingVersion.id)
            onPreview(null)
          }}
        >
          设为主版本
        </button>
      ) : (
        <span className="cc-main-state">主版本</span>
      )}
    </div>
  )
}
