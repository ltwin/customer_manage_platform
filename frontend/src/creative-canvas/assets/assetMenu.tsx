import {
  ArrowUpRight,
  Download,
  Folder,
  Heart,
  Plus,
  Star,
  Tag,
  Trash2,
  Undo2,
  X,
} from 'lucide-react'
import type { CanvasMenuItem } from '../editor/CanvasContextMenu.tsx'
import type * as api from '../editor/api.ts'
import type { Action } from './BatchBar.tsx'

/** 素材右键菜单：独立页与画布抽屉共用；抽屉多一项「放入画布」。 */
export function assetMenuItems({
  assets,
  blocked,
  group,
  onAction,
  onRemoveFromGroup,
  onPlace,
}: {
  assets: api.Asset[]
  blocked: boolean
  group: string
  onAction: (kind: Action) => void
  onRemoveFromGroup: () => void
  onPlace?: () => void
}): { label: string; items: CanvasMenuItem[] } | null {
  const tooMany = assets.length > 100
  if (assets.every((a) => a.deleted_at))
    return {
      label: '回收站操作',
      items: [
        {
          label: '恢复',
          icon: <Undo2 size={14} />,
          disabled: blocked || tooMany,
          action: () => onAction('restore'),
        },
        {
          label: '永久删除',
          icon: <Trash2 size={14} />,
          danger: true,
          disabled: blocked || tooMany,
          action: () => onAction('purge'),
        },
      ],
    }
  if (assets.some((a) => a.deleted_at)) return null
  const unavailable = assets.some((a) => a.unavailable)
  const entries: { kind: Action; label: string; Icon: typeof Star }[] = [
    { kind: 'rate', label: '评分…', Icon: Star },
    { kind: 'project', label: '放入项目…', Icon: ArrowUpRight },
    { kind: 'group', label: '归入分组…', Icon: Folder },
    { kind: 'tag', label: '打标签…', Icon: Tag },
    {
      kind: 'favorite',
      label: assets.every((a) => a.is_favorite) ? '取消最爱' : '加入最爱',
      Icon: Heart,
    },
    { kind: 'download', label: '下载', Icon: Download },
    { kind: 'trash', label: '移到回收站', Icon: Trash2 },
  ]
  const items: CanvasMenuItem[] = entries.map(({ kind, label, Icon }) => ({
    label,
    icon: <Icon size={14} />,
    danger: kind === 'trash',
    disabled:
      blocked ||
      tooMany ||
      ((kind === 'project' || kind === 'download') && unavailable),
    action: () => onAction(kind),
  }))
  if (group)
    items.splice(5, 0, {
      label: '移出这个分组',
      icon: <X size={14} />,
      disabled: blocked || tooMany,
      action: onRemoveFromGroup,
    })
  if (onPlace)
    items.unshift(
      {
        label: '放入画布',
        icon: <Plus size={14} />,
        disabled: unavailable || tooMany,
        action: onPlace,
      },
      'separator',
    )
  return { label: '素材操作', items }
}
