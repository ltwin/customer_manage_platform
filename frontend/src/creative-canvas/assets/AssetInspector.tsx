import type * as api from '../editor/api.ts'
import { TileFace } from '../editor/LibraryTiles.tsx'
import {
  kindLabel,
  mediaDuration,
  formatBytes,
} from '../editor/libraryState.ts'
import { Stars } from '../editor/StarRating.tsx'
import type { LibraryCatalog } from './useLibrary.ts'

const dateFormat = new Intl.DateTimeFormat('zh-CN', {
  dateStyle: 'medium',
  timeStyle: 'short',
})

/**
 * 检查器：本步只读，显示选中素材的预览、标题、评分、备注、分组、标签和属性。
 * 编辑、多选和操作按钮随选择与整理（067.2）补上。
 */
export default function AssetInspector({
  asset,
  catalog,
}: {
  asset: api.Asset | null
  catalog: LibraryCatalog | null
}) {
  if (!asset)
    return (
      <aside className="al-inspector" aria-label="检查器">
        <div className="al-ins-empty">
          <strong>没有选中素材</strong>
          <p>点一下素材，在这里查看详情。</p>
        </div>
      </aside>
    )
  const original = asset.content?.media?.find((m) => m.role === 'original')
  const groups = asset.group_ids.flatMap(
    (id) => catalog?.groups.find((g) => g.id === id) ?? [],
  )
  const tags = asset.tag_ids.flatMap(
    (id) => catalog?.tags.find((t) => t.id === id) ?? [],
  )
  const duration = mediaDuration(asset)
  return (
    <aside className="al-inspector" aria-label="检查器">
      <div className="al-ins-pad">
        <div className="al-ins-preview">
          <TileFace asset={asset} />
        </div>
        <div className="al-ins-title">
          <h3>{asset.title}</h3>
          <Stars value={asset.rating} size={13} />
        </div>
        {asset.description && (
          <p className="al-ins-note">{asset.description}</p>
        )}
        <section className="al-ins-sec" aria-label="分组">
          <header>分组</header>
          <div className="al-chips">
            {groups.length ? (
              groups.map((g) => (
                <span key={g.id} className="al-tagchip">
                  {g.name}
                </span>
              ))
            ) : (
              <span className="al-muted">未归类</span>
            )}
          </div>
        </section>
        <section className="al-ins-sec" aria-label="标签">
          <header>标签</header>
          <div className="al-chips">
            {tags.length ? (
              tags.map((t) => (
                <span key={t.id} className="al-tagchip">
                  <i className="al-tag-dot" style={{ background: t.color }} />
                  {t.name}
                </span>
              ))
            ) : (
              <span className="al-muted">没有标签</span>
            )}
          </div>
        </section>
        <dl className="al-props">
          <dt>类型</dt>
          <dd>{kindLabel[asset.kind]}</dd>
          {original?.width && original.height ? (
            <>
              <dt>尺寸</dt>
              <dd>
                {original.width} × {original.height}
              </dd>
            </>
          ) : null}
          {duration && (
            <>
              <dt>时长</dt>
              <dd>{duration}</dd>
            </>
          )}
          {original && (
            <>
              <dt>大小</dt>
              <dd>{formatBytes(original.byte_size)}</dd>
            </>
          )}
          <dt>添加于</dt>
          <dd>{dateFormat.format(new Date(asset.created_at))}</dd>
        </dl>
      </div>
    </aside>
  )
}
