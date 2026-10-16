import { Check, TriangleAlert } from 'lucide-react'
import type * as api from './api.ts'
import { MediaFigure } from './MediaView.tsx'
import { cachedContent, useNodeVersions } from './versionCache.ts'

// v6 版本条：选中且有多个版本的媒体节点在卡片底部显示缩略图。
// 点缩略图只在卡片内预览，不改变下游；「设为主版本」才写回节点。
// 数据来自 versionCache：悬停已预取时直接出图；还没到时按快照里的版本数先画占位格。
// 最近一次生成失败时末尾多一个失败格：点它在卡片里看失败原因（重试按钮在卡片上），
// 下一次生成开始后它自然消失（owner 2026-10-16）。
export default function NodeVersionStrip({
  canvasID,
  nodeID,
  kind,
  count,
  dataRevision,
  viewing,
  disabled,
  pinned,
  failure,
  onPreview,
  onSelectVersion,
  onShowFailure,
}: {
  canvasID: string
  nodeID: string
  kind: string
  /** 画布快照里的版本数（version_summary.count），版本页到达前用来画占位。 */
  count: number
  dataRevision: string
  /** 正在卡片内预览的内容修订；null 表示显示主版本。 */
  viewing: string | null
  disabled: boolean
  pinned: boolean
  /** 最近一次生成失败的原因码与卡片是否正在显示失败态；没有失败时为 null。 */
  failure: { code: string; open: boolean } | null
  onPreview: (content: api.Content | null) => void
  onSelectVersion: (versionID: string, followLatest?: boolean) => void
  onShowFailure: (open: boolean) => void
}) {
  const versions = useNodeVersions(canvasID, nodeID, dataRevision)
  const extra = failure ? 1 : 0
  if (!versions && count + extra >= 2)
    return (
      <div
        className="cc-versions is-loading nodrag nopan"
        role="group"
        aria-label="作品版本"
        aria-busy="true"
        onPointerDown={(e) => e.stopPropagation()}
      >
        {Array.from({ length: Math.min(count + extra, 12) }, (_, i) => (
          <span key={i} className="cc-version-placeholder" />
        ))}
      </div>
    )
  const items = versions?.items ?? []
  if (items.length + extra < 2) return null
  const main = versions?.selected_version_id ?? null
  const viewingFailure = !viewing && !!failure?.open
  const current = viewingFailure
    ? null
    : viewing
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
        const content = cachedContent(v.content_revision_id)
        return (
          <button
            key={v.id}
            className={`${v.id === current ? 'is-on' : ''} ${v.id === main ? 'is-main' : ''}`}
            aria-label={`版本 ${v.version_no}${v.id === main ? '（主版本）' : ''}`}
            aria-pressed={v.id === current}
            onClick={() => {
              // 失败态下点主版本要明确切回内容，所以主版本也走预览。
              if (v.id === main) {
                onShowFailure(false)
                onPreview(null)
              } else onPreview(content ?? null)
            }}
          >
            {content && (
              <MediaFigure
                content={content}
                kind={kind}
                title={`版本 ${v.version_no}`}
              />
            )}
            {v.id === main && (
              <span className="cc-main-dot" aria-hidden="true">
                <Check size={9} strokeWidth={3} />
              </span>
            )}
          </button>
        )
      })}
      {failure && (
        <button
          className={`cc-version-failed ${viewingFailure ? 'is-on' : ''}`}
          aria-label="最近一次生成失败"
          aria-pressed={viewingFailure}
          onClick={() => {
            onPreview(null)
            onShowFailure(true)
          }}
        >
          <TriangleAlert size={14} strokeWidth={2} />
        </button>
      )}
      <span className="cc-versions-grow" />
      {viewingFailure ? (
        <span className="cc-main-state">最近一次生成失败</span>
      ) : viewingVersion && viewingVersion.id !== main ? (
        <button
          className="cc-set-main"
          disabled={disabled}
          onClick={() => {
            onSelectVersion(viewingVersion.id)
          }}
        >
          设为主版本
        </button>
      ) : pinned ? (
        <button
          className="cc-set-main"
          disabled={disabled}
          onClick={() => {
            const latest = items.reduce((a, b) =>
              BigInt(a.version_no) > BigInt(b.version_no) ? a : b,
            )
            onSelectVersion(latest.id, true)
          }}
        >
          改为跟随最新
        </button>
      ) : (
        <span className="cc-main-state">主版本 · 自动</span>
      )}
    </div>
  )
}
