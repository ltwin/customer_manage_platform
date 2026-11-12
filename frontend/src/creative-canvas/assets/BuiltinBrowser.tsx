import { Maximize2, Search } from 'lucide-react'
import { useState } from 'react'
import type { BuiltinModel } from '../../creative-scene/models/catalog.ts'
import {
  builtinCategories,
  builtinMaterials,
  builtinModelCategories,
  builtinModelThumbnail,
  filterBuiltin,
  filterBuiltinModels,
  groupBuiltin,
  type BuiltinMaterial,
} from './builtin.ts'

/**
 * 资产库的「内置材质」（C5c2）：只读网格，按品类分组，名称和品类在本地筛选。
 * 没有选择、整理、导入和拖到分组；双击或「查看」打开查看浮窗。
 */
export default function BuiltinBrowser({
  size,
  onOpen,
}: {
  size: number
  onOpen: (items: BuiltinMaterial[], id: string) => void
}) {
  const [q, setQ] = useState('')
  const [category, setCategory] = useState('')
  const found = filterBuiltin(q).filter(
    (m) => !category || m.category === category,
  )
  const groups = groupBuiltin(found)
  const open = (id: string) => onOpen(found, id)
  return (
    <section className="al-board" aria-label="内置材质" tabIndex={-1}>
      <header className="al-head">
        <div className="al-crumb">
          <h2>内置材质</h2>
          <small>{found.length} 项</small>
        </div>
        <label className="al-search">
          <Search size={14} />
          <input
            type="search"
            aria-label="搜索内置材质"
            placeholder="搜索名称、品类"
            maxLength={200}
            value={q}
            onChange={(e) => setQ(e.currentTarget.value)}
            onKeyDown={(e) => {
              if (e.key !== 'Escape' || e.nativeEvent.isComposing || !q) return
              e.preventDefault()
              setQ('')
            }}
          />
        </label>
      </header>
      <div className="al-filters">
        <div className="al-seg" role="group" aria-label="品类">
          <button
            type="button"
            aria-pressed={!category}
            onClick={() => setCategory('')}
          >
            全部<small>{builtinMaterials.length}</small>
          </button>
          {builtinCategories.map((c) => (
            <button
              key={c.id}
              type="button"
              aria-pressed={category === c.id}
              onClick={() => setCategory(c.id)}
            >
              {c.label}
            </button>
          ))}
        </div>
      </div>
      <div className="al-scroll">
        <p className="al-builtin-note">
          平台提供的 CC0 真实照片贴图，可在 3D 场景里拖到物件或材质槽上使用；不能评分、整理或删除。
        </p>
        {groups.map((g) => (
          <section
            key={g.category}
            className="al-builtin-group"
            aria-label={g.label}
          >
            <h3>{g.label}</h3>
            <div
              className="al-tiles is-grid"
              style={{ '--al-size': `${size}px` } as React.CSSProperties}
            >
              {g.items.map((m) => (
                <BuiltinTile
                  key={m.id}
                  id={m.id}
                  kind="material"
                  label={m.label}
                  thumbnail={m.thumbnail}
                  caption={`${m.sizeM} m`}
                  onOpen={open}
                />
              ))}
            </div>
          </section>
        ))}
        {!found.length && (
          <div className="al-empty">
            <strong>没有符合条件的内置材质</strong>
            <span>换个名称或品类试试。</span>
          </div>
        )}
      </div>
    </section>
  )
}

function BuiltinTile({
  id,
  kind,
  label,
  thumbnail,
  caption,
  onOpen,
}: {
  id: string
  kind: 'material' | 'model'
  label: string
  thumbnail: string
  caption: string
  onOpen: (id: string) => void
}) {
  return (
    <div
      className="al-tile"
      data-builtin={id}
      data-kind={kind}
      role="button"
      tabIndex={0}
      aria-label={label}
      onDoubleClick={(e) => {
        if (!(e.target instanceof Element) || !e.target.closest('button'))
          onOpen(id)
      }}
      onKeyDown={(e) => {
        if (e.target !== e.currentTarget || !['Enter', ' '].includes(e.key))
          return
        e.preventDefault()
        onOpen(id)
      }}
    >
      <button
        type="button"
        className="al-view"
        aria-label={`查看：${label}`}
        onClick={(e) => {
          e.stopPropagation()
          onOpen(id)
        }}
      >
        <Maximize2 size={13} />
      </button>
      <div className="al-thumb">
        <img src={thumbnail} alt="" loading="lazy" draggable={false} />
      </div>
      <div className="al-cap">
        <span>{label}</span>
        <small>{caption}</small>
      </div>
    </div>
  )
}

/**
 * 资产库的「内置模型」（scene-pose-system）：只读网格，名称和品类在本地筛选；
 * 双击或「查看」打开查看浮窗。放进场景在 3D 场景的资产面板里做。
 */
export function BuiltinModelBrowser({
  size,
  onOpen,
}: {
  size: number
  onOpen: (items: BuiltinModel[], id: string) => void
}) {
  const [q, setQ] = useState('')
  const found = filterBuiltinModels(q)
  const open = (id: string) => onOpen(found, id)
  return (
    <section className="al-board" aria-label="内置模型" tabIndex={-1}>
      <header className="al-head">
        <div className="al-crumb">
          <h2>内置模型</h2>
          <small>{found.length} 项</small>
        </div>
        <label className="al-search">
          <Search size={14} />
          <input
            type="search"
            aria-label="搜索内置模型"
            placeholder="搜索名称、品类"
            maxLength={200}
            value={q}
            onChange={(e) => setQ(e.currentTarget.value)}
            onKeyDown={(e) => {
              if (e.key !== 'Escape' || e.nativeEvent.isComposing || !q) return
              e.preventDefault()
              setQ('')
            }}
          />
        </label>
      </header>
      <div className="al-scroll">
        <p className="al-builtin-note">
          平台提供的 CC0 素体，带人形骨骼和自己的材质，可在 3D 场景的资产面板「内置」里放进场景；不能评分、整理或删除。
        </p>
        {found.length ? (
          <div
            className="al-tiles is-grid"
            style={{ '--al-size': `${size}px` } as React.CSSProperties}
          >
            {found.map((m) => (
              <BuiltinTile
                key={m.id}
                id={m.id}
                kind="model"
                label={m.label}
                thumbnail={builtinModelThumbnail(m.id)}
                caption={builtinModelCategories[m.category] ?? m.category}
                onOpen={open}
              />
            ))}
          </div>
        ) : (
          <div className="al-empty">
            <strong>没有符合条件的内置模型</strong>
            <span>换个名称试试。</span>
          </div>
        )}
      </div>
    </section>
  )
}
