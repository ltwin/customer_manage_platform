import AssetDescriptionInput from "./AssetDescriptionInput.tsx"
import { SHAPES, shapeOf } from './appearance.ts'
import { Plus, X } from 'lucide-react'
import type * as api from '../editor/api.ts'
import { TileFace } from '../editor/LibraryTiles.tsx'
import {
  kindLabel,
  mediaDuration,
  formatBytes,
} from '../editor/libraryState.ts'
import { StarRating } from '../editor/StarRating.tsx'
import type { LibraryCatalog } from './useLibrary.ts'
import { ActionButtons, type Actions } from './BatchBar.tsx'
import { anchorPoint } from './popover.ts'
const dateFormat = new Intl.DateTimeFormat('zh-CN', {
  dateStyle: 'medium',
  timeStyle: 'short',
})
export default function AssetInspector({
  assets,
  catalog,
  onRate,
  onColor,
  onMetadata,
  onOrganize,
  ...actions
}: Actions & {
  assets: api.Asset[]
  catalog: LibraryCatalog | null
  onColor: (color: string) => void
  onRate: (rating: number | null) => void
  onMetadata: (
    asset: api.Asset,
    patch: { title?: string; description?: string },
  ) => void
  onOrganize: (type: 'group' | 'tag', id: string, add: boolean) => void
}) {
  if (!assets.length)
    return (
      <aside className="al-inspector" aria-label="检查器">
        <div className="al-ins-empty">
          <strong>没有选中素材</strong>
          <p>点选查看详情，⌘ / Shift 点击或在空白处拖拽框选多个。</p>
          <p>1–5 评分　⌘A 全选　Esc 取消</p>
          <p>把素材拖到左侧分组上即可归入。</p>
        </div>
      </aside>
    )
  const single = assets.length === 1 ? assets[0]! : null
  const original = single?.content?.media?.find((m) => m.role === 'original')
  const duration = single ? mediaDuration(single) : ''
  const mixed = new Set(assets.map((a) => a.rating)).size > 1
  const disabled =
    actions.blocked || assets.length > 100 || assets.some((a) => !!a.deleted_at)
  return (
    <aside className="al-inspector" aria-label="检查器">
      <div className="al-ins-pad">
        {single ? (
          <>
            <div className="al-ins-preview">
              <TileFace asset={single} />
            </div>
            <div
              className="al-ins-fields"
              key={`${single.id}:${single.revision}:${single.title}:${single.description}`}
            >
              <input
                className="al-ins-title-input"
                aria-label="标题"
                defaultValue={single.title}
                maxLength={200}
                disabled={disabled}
                onBlur={(e) => {
                  const title = e.currentTarget.value.trim()
                  if (title && title !== single.title)
                    onMetadata(single, { title })
                  else e.currentTarget.value = single.title
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.nativeEvent.isComposing)
                    e.currentTarget.blur()
                  if (e.key === 'Escape') {
                    e.currentTarget.value = single.title
                    e.currentTarget.blur()
                  }
                }}
              />
              <StarRating
                value={single.rating}
                disabled={disabled}
                onChange={onRate}
              />
              <AssetDescriptionInput
                description={single.description}
                disabled={disabled}
                onSave={(description) => onMetadata(single, { description })}
              />
            </div>
          </>
        ) : (
          <>
            <div className="al-stack">
              {assets.slice(0, 3).map((a) => (
                <div key={a.id}>
                  <TileFace asset={a} />
                </div>
              ))}
            </div>
            <strong>已选 {assets.length} 项</strong>
            <section className="al-ins-sec">
              <header>
                评分<span>{mixed ? '各不相同，点选后统一' : ''}</span>
              </header>
              <StarRating
                value={mixed ? null : assets[0]!.rating}
                disabled={disabled}
                label="批量评分"
                onChange={onRate}
              />
            </section>
          </>
        )}
        {single && !single.unavailable && single.palette?.length > 0 && (
          <section className="al-ins-sec" aria-label="主色">
            <header>
              主色<span>点一下找相近颜色</span>
            </header>
            <div className="al-palette">
              {single.palette.map((c, i) => (
                <button
                  type="button"
                  key={`${i}:${c.hex}`}
                  style={{ background: c.hex }}
                  title={c.hex}
                  aria-label={`按 ${c.hex} 筛选`}
                  disabled={actions.blocked}
                  onClick={() => onColor(c.hex)}
                />
              ))}
            </div>
          </section>
        )}
        {assets.length > 100 && <p className="al-limit">一次最多处理 100 项</p>}
        {(['group', 'tag'] as const).map((type) => {
          const entries =
            type === 'group' ? (catalog?.groups ?? []) : (catalog?.tags ?? [])
          const field = type === 'group' ? 'group_ids' : 'tag_ids'
          const present = entries.filter((v) =>
            assets.some((a) => a[field].includes(v.id)),
          )
          return (
            <section
              className="al-ins-sec"
              aria-label={type === 'group' ? '分组' : '标签'}
              key={type}
            >
              <header>
                {type === 'group' ? '分组' : '标签'}
                {!single && <span>半透明表示只有部分素材有</span>}
              </header>
              <div className="al-chips">
                {present.map((entry) => {
                  const partial = !assets.every((a) =>
                    a[field].includes(entry.id),
                  )
                  return (
                    <span
                      className={`al-tagchip${partial ? ' partial' : ''}`}
                      key={entry.id}
                    >
                      {'color' in entry && (
                        <i
                          className="al-tag-dot"
                          style={{ background: entry.color }}
                        />
                      )}
                      <button
                        className="al-chip-name"
                        title={entry.name}
                        disabled={!partial || disabled}
                        aria-label={
                          partial
                            ? `补齐${type === 'group' ? '分组' : '标签'}：${entry.name}`
                            : entry.name
                        }
                        onClick={() => onOrganize(type, entry.id, true)}
                      >
                        {entry.name}
                      </button>
                      <button
                        disabled={disabled}
                        aria-label={`${type === 'group' ? '移出分组' : '移除标签'}：${entry.name}`}
                        onClick={() => onOrganize(type, entry.id, false)}
                      >
                        <X size={10} />
                      </button>
                    </span>
                  )
                })}
                {!present.length && single && (
                  <span className="al-muted">
                    {type === 'group' ? '未归类' : '没有标签'}
                  </span>
                )}
                <button
                  type="button"
                  className="al-addchip"
                  disabled={disabled}
                  onClick={(e) =>
                    actions.onAction(type, anchorPoint(e.currentTarget))
                  }
                >
                  <Plus size={12} />
                  {type === 'group' ? '归入分组' : '标签'}
                </button>
              </div>
            </section>
          )
        })}
        {single && (
          <dl className="al-props">
            <dt>类型</dt>
            <dd>{kindLabel[single.kind]}</dd>
            {original?.width && original.height ? (
              <>
                <dt>{single.kind === 'video' ? '编码尺寸' : '尺寸'}</dt>
                <dd>
                  {original.width} × {original.height}
                  {single.kind === 'image' && (
                    <>
                      {' '}
                      ·{' '}
                      {
                        SHAPES.find(
                          (s) =>
                            s.value ===
                            shapeOf(original.width, original.height),
                        )?.label
                      }
                    </>
                  )}
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
            <dd>{dateFormat.format(new Date(single.created_at))}</dd>
          </dl>
        )}
        <div className="al-ins-actions">
          <ActionButtons
            assets={assets}
            {...actions}
            blocked={actions.blocked || assets.length > 100}
          />
        </div>
      </div>
    </aside>
  )
}
