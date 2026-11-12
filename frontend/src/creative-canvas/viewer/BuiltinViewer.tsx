import { useState } from 'react'
import { builtinModelCategories, builtinModelThumbnail, builtinSlot, type BuiltinMaterial } from '../assets/builtin.ts'
import type { BuiltinModel } from '../../creative-scene/models/catalog.ts'
import { builtinKey } from '../../creative-scene/models/key.ts'
import MediaViewer from './MediaViewer.tsx'

const siteLabel: Record<string, string> = {
  polyhaven: 'Poly Haven',
  ambientcg: 'ambientCG',
  quaternius: 'Quaternius',
}

/**
 * 内置贴图材质的查看浮窗（C5c2）：同一个外框和材质球预览，信息栏只读——
 * 没有标题编辑、评分、分组和标签，列出品类、尺寸和来源。
 */
export default function BuiltinViewer({
  items,
  initialID,
  onClose,
  foot,
}: {
  items: BuiltinMaterial[]
  initialID: string
  onClose: () => void
  /** 带着任务打开时（给槽位选材质），底部换成任务自己的操作。 */
  foot?: (m: BuiltinMaterial) => React.ReactNode
}) {
  const [index, setIndex] = useState(() =>
    Math.max(
      0,
      items.findIndex((m) => m.id === initialID),
    ),
  )
  const m = items[index]!
  return (
    <MediaViewer
      item={{
        id: m.id,
        kind: 'material',
        title: m.label,
        builtin: { slot: builtinSlot(m.id), thumbnail: m.thumbnail },
      }}
      index={index}
      count={items.length}
      blocked={false}
      loading={false}
      error=""
      onRetry={() => {}}
      onStep={(d) =>
        setIndex((i) => Math.min(items.length - 1, Math.max(0, i + d)))
      }
      onClose={onClose}
      onRate={() => {}}
      label="查看内置材质"
      panel={
        <>
          <div className="v-body">
            <span className="v-kind">内置材质 · {m.source.license}</span>
            <h2 className="v-title v-title-text">{m.label}</h2>
            <dl className="v-rows">
              <dt>品类</dt>
              <dd>{m.categoryLabel}</dd>
              <dt>尺寸</dt>
              <dd>{m.sizeM} 米一块</dd>
              <dt>来源</dt>
              <dd title={m.source.asset}>
                {siteLabel[m.source.site] ?? m.source.site} · {m.source.asset}
              </dd>
              <dt>作者</dt>
              <dd title={m.source.author}>{m.source.author}</dd>
            </dl>
            <p className="v-note">
              平台内置的真实照片贴图，可直接用到场景物件的材质槽；不能整理或删除。
            </p>
          </div>
          {foot && <div className="v-foot">{foot(m)}</div>}
        </>
      }
    />
  )
}

/**
 * 内置模型的查看浮窗（scene-pose-system）：同一个外框，静止姿势的 3D 预览带条目自己的材质，
 * 信息栏只读，列出品类、骨骼和来源。
 */
export function BuiltinModelViewer({
  items,
  initialID,
  onClose,
}: {
  items: BuiltinModel[]
  initialID: string
  onClose: () => void
}) {
  const [index, setIndex] = useState(() =>
    Math.max(
      0,
      items.findIndex((m) => m.id === initialID),
    ),
  )
  const m = items[index]!
  const bones = Object.keys(m.rig?.humanoid ?? {}).length
  return (
    <MediaViewer
      item={{
        id: m.id,
        kind: 'model',
        title: m.label,
        builtinModel: {
          key: builtinKey(m.id),
          manifest: m.manifest,
          materials: m.materials,
          thumbnail: builtinModelThumbnail(m.id),
        },
      }}
      index={index}
      count={items.length}
      blocked={false}
      loading={false}
      error=""
      onRetry={() => {}}
      onStep={(d) =>
        setIndex((i) => Math.min(items.length - 1, Math.max(0, i + d)))
      }
      onClose={onClose}
      onRate={() => {}}
      label="查看内置模型"
      panel={
        <div className="v-body">
          <span className="v-kind">内置模型 · {m.source.license}</span>
          <h2 className="v-title v-title-text">{m.label}</h2>
          <dl className="v-rows">
            <dt>品类</dt>
            <dd>{builtinModelCategories[m.category] ?? m.category}</dd>
            <dt>骨骼</dt>
            <dd>{bones ? `人形，${bones} 根标准骨骼` : '无'}</dd>
            <dt>三角形</dt>
            <dd>{m.manifest.triangles.toLocaleString()}</dd>
            <dt>来源</dt>
            <dd title={m.source.asset}>
              <a href={m.source.url} target="_blank" rel="noopener noreferrer">
                {siteLabel[m.source.site] ?? m.source.site} · {m.source.asset}
              </a>
            </dd>
            <dt>作者</dt>
            <dd title={m.source.author}>{m.source.author}</dd>
          </dl>
          <p className="v-note">
            平台内置的素体，可在 3D 场景的资产面板「内置」或「添加 &gt; 素体」放进场景；不能整理或删除。
          </p>
        </div>
      }
    />
  )
}
