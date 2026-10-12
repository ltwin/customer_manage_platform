import {
  ArrowUpRight,
  Download,
  Folder,
  Heart,
  Star,
  Tag,
  Trash2,
  X,
} from 'lucide-react'
import type { Asset } from '../editor/api.ts'
import { anchorPoint, type Point } from './popover.ts'
export type Action =
  'group' | 'tag' | 'rate' | 'favorite' | 'project' | 'download' | 'trash'
export type Actions = {
  blocked: boolean
  onAction: (action: Action, at: Point) => void
}
export function ActionButtons({
  assets,
  blocked,
  onAction,
  compact = false,
}: Actions & { assets: Asset[]; compact?: boolean }) {
  const favorite = assets.every((a) => a.is_favorite)
  const entries = compact
    ? ([
        { key: 'group', label: '归入分组', Icon: Folder },
        { key: 'tag', label: '打标签', Icon: Tag },
        { key: 'rate', label: '评分', Icon: Star },
        { key: 'favorite', label: favorite ? '取消最爱' : '最爱', Icon: Heart },
        { key: 'project', label: '放入项目', Icon: ArrowUpRight },
        { key: 'trash', label: '回收站', Icon: Trash2 },
      ] as const)
    : ([
        { key: 'project', label: '放入项目…', Icon: ArrowUpRight },
        {
          key: 'favorite',
          label: favorite ? '取消最爱' : '加入最爱',
          Icon: Heart,
        },
        { key: 'download', label: '下载', Icon: Download },
        { key: 'trash', label: '移到回收站', Icon: Trash2 },
      ] as const)
  return (
    <>
      {entries.map(({ key, label, Icon }) => (
        <button
          type="button"
          key={key}
          className={`${compact ? 'al-chip' : 'btn'}${key === 'project' && !compact ? ' btn-primary wide' : ''}${key === 'trash' ? ' al-danger wide' : ''}`}
          disabled={
            blocked ||
            assets.some((a) => !!a.deleted_at) ||
            ((key === 'project' || key === 'download') &&
              assets.some((a) => a.unavailable))
          }
          onClick={(e) => onAction(key, anchorPoint(e.currentTarget))}
        >
          <Icon size={14} />
          {label}
        </button>
      ))}
    </>
  )
}
export default function BatchBar({
  assets,
  onClear,
  ...actions
}: Actions & { assets: Asset[]; onClear: () => void }) {
  if (!assets.length || assets.some((a) => a.deleted_at)) return null
  return (
    <div className="al-batch" role="toolbar" aria-label="批量操作">
      <strong>已选 {assets.length} 项</strong>
      {assets.length > 100 && <span>一次最多处理 100 项</span>}
      <ActionButtons
        assets={assets}
        {...actions}
        blocked={actions.blocked || assets.length > 100}
        compact
      />
      <button
        type="button"
        className="al-icon-btn"
        aria-label="取消选择"
        onClick={onClear}
      >
        <X size={15} />
      </button>
    </div>
  )
}
