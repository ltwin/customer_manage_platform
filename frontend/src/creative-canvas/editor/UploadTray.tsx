import { RotateCcw, X } from 'lucide-react'
import type { UploadItem } from './uploads.ts'

const stageText: Record<UploadItem['stage'], string> = {
  queued: '等待中',
  creating: '准备中',
  uploading: '上传中',
  verifying: '校验中',
  done: '已完成',
  failed: '失败',
  rejected: '未上传',
}
export default function UploadTray({
  items,
  onRetry,
  onDismiss,
}: {
  items: UploadItem[]
  onRetry: (id: string) => void
  onDismiss: () => void
}) {
  if (!items.length) return null
  const active = items.some((i) => !['done', 'failed', 'rejected'].includes(i.stage))
  return (
    <section className="cc-upload-tray cc-glass" aria-label="导入文件" aria-live="polite">
      <header>
        <span>导入文件</span>
        <button
          className="cc-icon-button"
          aria-label="收起导入列表"
          disabled={active}
          onClick={onDismiss}
        >
          <X size={14} />
        </button>
      </header>
      <ul>
        {items.map((i) => {
          const done = i.stage === 'done'
          const failed = i.stage === 'failed' || i.stage === 'rejected'
          const percent = i.size ? Math.round((i.sent / i.size) * 100) : 0
          return (
            <li key={i.id} className={failed ? 'is-failed' : done ? 'is-done' : ''}>
              <span className="cc-upload-name" title={i.name}>
                {i.name}
              </span>
              <small>
                {failed
                  ? i.message || stageText[i.stage]
                  : i.stage === 'uploading'
                    ? `${stageText[i.stage]} ${percent}%`
                    : i.upload?.binding?.status === 'needs_review'
                      ? '已上传 · 目标已变化，待处理'
                      : stageText[i.stage]}
              </small>
              {failed && (
                <button
                  className="cc-icon-button"
                  aria-label={`重试上传：${i.name}`}
                  onClick={() => onRetry(i.id)}
                >
                  <RotateCcw size={13} />
                </button>
              )}
              {!failed && !done && (
                <i className="cc-upload-bar" style={{ width: `${percent}%` }} />
              )}
            </li>
          )
        })}
      </ul>
    </section>
  )
}
