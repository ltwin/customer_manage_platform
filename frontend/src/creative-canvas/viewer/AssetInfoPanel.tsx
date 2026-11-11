import { CharacterImage } from '../assets/CharacterFields.tsx'
import { characterIdentity, characterOf, characterViewLabel } from '../assets/character.ts'
import { Fragment } from 'react'
import AssetDescriptionInput from "../assets/AssetDescriptionInput.tsx"
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
import { assetMaterial, materialRows, modelInfo, modelRows } from './modelInfo.ts'
import { isCanvasAsset } from '../editor/assetPlacement.ts'

export default function AssetInfoPanel({
  asset,
  catalog,
  organization,
  blocked,
  onRate,
  onPlace,
  onEditCharacter,
  foot,
}: {
  asset: Asset
  catalog: LibraryCatalog | null
  organization: ReturnType<typeof useOrganization>
  blocked: boolean
  onRate: (rating: number | null) => void
  onPlace?: () => void
  onEditCharacter?: () => void
  /** 带着任务打开时（挑素材引用），底部换成任务自己的操作。 */
  foot?: React.ReactNode
}) {
  const original = asset.content?.media?.find((m) => m.role === 'original')
  const duration = mediaDuration(asset)
  const model = modelInfo(asset.content)
  const material = assetMaterial(asset.content)
  const character = characterOf(asset.content)
  // 模型和材质只能用在 3D 场景：不给「放入项目」「放入画布」。
  const canvasPlaceable = isCanvasAsset(asset)
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
        {asset.kind === 'character' ? (
          <h2 className="v-title">{asset.title}</h2>
        ) : (
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
        )}
        <StarRating
          size={22}
          value={asset.rating}
          disabled={disabled}
          onChange={onRate}
        />
        <dl className="v-rows">
          {[
            ...(model ? modelRows(model) : []),
            ...(material ? materialRows(material) : []),
          ].map(([label, value], i) => (
              <Fragment key={i}>
                <dt>{label}</dt>
                <dd>{value}</dd>
              </Fragment>
            ))}
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
        {character && (
          <>
            <section className="v-sec">
              <header>外貌描述</header>
              <p className="v-character-appearance">{character.appearance}</p>
              {asset.character && <small>{characterIdentity(asset.character)}</small>}
              {!!character.aliases?.length && <small>别名 · {character.aliases.join('、')}</small>}
            </section>
            <section className="v-sec">
              <header>参考图</header>
              <div className="v-character-views">
                {character.views.map((view, index) => (
                  <figure key={view.content_revision_id}>
                    <CharacterImage id={view.content_revision_id} label={characterViewLabel(view.view)} />
                    <figcaption>
                      {characterViewLabel(view.view)}{index === 0 ? ' · 封面' : ''}
                      {view.note && <div>{view.note}</div>}
                    </figcaption>
                  </figure>
                ))}
              </div>
            </section>
            {!!character.sources.length && (
              <section className="v-sec">
                <header>原始素材</header>
                <div className="v-character-views">
                  {character.sources.map((source, index) => (
                    <figure key={`${source.content_revision_id}:${index}`}>
                      <CharacterImage id={source.content_revision_id} label={`原始素材 ${index + 1}`} />
                    </figure>
                  ))}
                </div>
              </section>
            )}
            {(character.origin || character.attribution || !!character.source_urls?.length) && (
              <section className="v-sec">
                <header>来源备注</header>
                {character.origin && <small>{character.origin === 'original' ? '原创' : '作品衍生'}</small>}
                {character.attribution && <p className="v-character-appearance">{character.attribution}</p>}
                {character.source_urls?.map((url, index) => (
                  <a key={`${url}:${index}`} href={url} target="_blank" rel="noreferrer">来源链接 {index + 1}</a>
                ))}
              </section>
            )}
          </>
        )}
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
          <AssetDescriptionInput
            key={`${asset.id}:${asset.description}`}
            description={asset.description}
            disabled={disabled}
            onSave={(description) => organization.metadata(asset, { description })}
          />
        </section>
      </div>
      <div className="v-foot">
        {foot ?? (asset.deleted_at ? (
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
            <button
              className="btn danger"
              disabled={blocked}
              onClick={(e) =>
                organization.action('purge', anchorPoint(e.currentTarget), [
                  asset,
                ])
              }
            >
              <Trash2 size={14} />
              永久删除
            </button>
          </>
        ) : (
          <>
            {canvasPlaceable && (
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
            )}
            {asset.kind === 'character' ? (
              onEditCharacter && character && (
                <button type="button" className="btn btn-primary wide" disabled={disabled || asset.unavailable} onClick={onEditCharacter}>
                  编辑角色卡
                </button>
              )
            ) : (
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
            )}
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
            {onPlace && canvasPlaceable && (
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
        ))}
      </div>
    </>
  )
}
