import { ArrowUpRight, Download, Plus, Trash2, Undo2, X } from 'lucide-react'
import type { Asset } from '../editor/api.ts'
import {
  kindLabel,
  mediaDuration,
  formatBytes,
} from '../editor/libraryState.ts'
import { StarRating } from '../editor/StarRating.tsx'
import type { LibraryCatalog } from '../assets/useLibrary.ts'
import type { useOrganization } from '../assets/useOrganization.tsx'
import { anchorPoint } from '../assets/popover.ts'

export default function AssetInfoPanel({
  asset,
  catalog,
  organization,
  blocked,
  onRate,
  onPlace,
}: {
  asset: Asset
  catalog: LibraryCatalog | null
  organization: ReturnType<typeof useOrganization>
  blocked: boolean
  onRate: (rating: number | null) => void
  onPlace?: () => void
}) {
  const original = asset.content?.media?.find((m) => m.role === 'original')
  const duration = mediaDuration(asset)
  const disabled = blocked || !!asset.deleted_at
  return (
    <>
      <div className="v-body">
        <span className="v-kind">
          {kindLabel[asset.kind]}
          {original?.width && original.height
            ? ` · ${original.width}×${original.height}`
            : ''}
          {duration ? ` · ${duration}` : ''}
          {asset.deleted_at ? ' · 在回收站' : ''}
        </span>
        <input
          key={`${asset.id}:${asset.title}`}
          className="v-title"
          aria-label="标题"
          defaultValue={asset.title}
          maxLength={200}
          disabled={disabled}
          onBlur={(e) => {
            const title = e.currentTarget.value.trim()
            if (title && title !== asset.title)
              organization.metadata(asset, { title })
            else e.currentTarget.value = asset.title
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.nativeEvent.isComposing)
              e.currentTarget.blur()
            if (e.key === 'Escape') {
              e.stopPropagation()
              e.currentTarget.value = asset.title
              e.currentTarget.blur()
            }
          }}
        />
        <StarRating
          size={22}
          value={asset.rating}
          disabled={disabled}
          onChange={onRate}
        />
        <dl className="v-rows">
          <dt>添加于</dt>
          <dd>
            {new Intl.DateTimeFormat('zh-CN', {
              dateStyle: 'medium',
              timeStyle: 'short',
            }).format(new Date(asset.created_at))}
          </dd>
          {original && (
            <>
              <dt>大小</dt>
              <dd>{formatBytes(original.byte_size)}</dd>
            </>
          )}
        </dl>
        {(['group', 'tag'] as const).map((kind) => {
          const entries =
            kind === 'group' ? (catalog?.groups ?? []) : (catalog?.tags ?? [])
          const ids = kind === 'group' ? asset.group_ids : asset.tag_ids
          return (
            <section
              className="v-sec"
              key={kind}
              aria-label={kind === 'group' ? '分组' : '标签'}
            >
              <header>{kind === 'group' ? '分组' : '标签'}</header>
              <div className="al-chips">
                {entries
                  .filter((e) => ids.includes(e.id))
                  .map((entry) => (
                    <span className="al-tagchip" key={entry.id}>
                      {'color' in entry && (
                        <i
                          className="al-tag-dot"
                          style={{ background: entry.color }}
                        />
                      )}
                      <span className="al-chip-name" title={entry.name}>
                        {entry.name}
                      </span>
                      <button
                        disabled={disabled}
                        aria-label={`${kind === 'group' ? '移出分组' : '移除标签'}：${entry.name}`}
                        onClick={() =>
                          organization.organize([asset], kind, entry.id, false)
                        }
                      >
                        <X size={10} />
                      </button>
                    </span>
                  ))}
                <button
                  className="al-addchip"
                  disabled={disabled || !catalog}
                  onClick={(e) =>
                    organization.action(kind, anchorPoint(e.currentTarget), [
                      asset,
                    ])
                  }
                >
                  <Plus size={12} />
                  {kind === 'group' ? '归入分组' : '标签'}
                </button>
              </div>
            </section>
          )
        })}
        <section className="v-sec">
          <header>备注</header>
          <textarea
            key={`${asset.id}:${asset.description}`}
            className="al-ins-note-input"
            aria-label="备注"
            defaultValue={asset.description}
            placeholder="为什么收藏它、准备用在哪…"
            maxLength={2000}
            disabled={disabled}
            onBlur={(e) => {
              if (e.currentTarget.value !== asset.description)
                organization.metadata(asset, {
                  description: e.currentTarget.value,
                })
            }}
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                e.stopPropagation()
                e.currentTarget.value = asset.description
                e.currentTarget.blur()
              }
            }}
          />
        </section>
      </div>
      <div className="v-foot">
        {asset.deleted_at ? (
          <>
            <p className="v-trashed">已移到回收站</p>
            <button
              className="btn btn-primary wide"
              disabled={blocked}
              onClick={() => organization.restore(asset)}
            >
              <Undo2 size={14} />
              恢复
            </button>
          </>
        ) : (
          <>
            <button
              className="btn btn-primary wide"
              disabled={disabled || asset.unavailable}
              onClick={(e) =>
                organization.action('project', anchorPoint(e.currentTarget), [
                  asset,
                ])
              }
            >
              <ArrowUpRight size={14} />
              放入项目…
            </button>
            <button
              className="btn"
              disabled={disabled || asset.unavailable}
              onClick={(e) =>
                organization.action('download', anchorPoint(e.currentTarget), [
                  asset,
                ])
              }
            >
              <Download size={14} />
              下载
            </button>
            <button
              className="btn danger"
              disabled={disabled}
              onClick={(e) =>
                organization.action('trash', anchorPoint(e.currentTarget), [
                  asset,
                ])
              }
            >
              <Trash2 size={14} />
              移到回收站
            </button>
            {onPlace && (
              <button
                className="btn wide"
                disabled={disabled || asset.unavailable}
                onClick={onPlace}
              >
                <Plus size={14} />
                放入画布
              </button>
            )}
          </>
        )}
      </div>
    </>
  )
}
