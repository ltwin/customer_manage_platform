import { useState } from 'react'
import { builtinSlot, type BuiltinMaterial } from '../assets/builtin.ts'
import MediaViewer from './MediaViewer.tsx'

const siteLabel: Record<string, string> = {
  polyhaven: 'Poly Haven',
  ambientcg: 'ambientCG',
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
