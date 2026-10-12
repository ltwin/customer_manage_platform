import { Globe, Play } from 'lucide-react'
import type * as api from './api.ts'
import { MediaFigure } from './MediaView.tsx'
import { contentText } from './content.ts'
import { audioBars, linkDomain, mediaDuration, tileRatio } from './libraryState.ts'
import './cards.css'

const isVisual = (kind: string) => kind === 'image' || kind === 'video'

/**
 * 卡片主体（V6-060）：图片和视频显示画面，视频带时长；文字、链接、音频各有自己的样式，
 * 不用通用图标代替。画布抽屉和资产库页共用。链接只显示域名，不抓取网页。
 */
export function TileFace({ asset: a }: { asset: api.Asset }) {
  if (isVisual(a.kind)) {
    if (a.unavailable || !a.content) return <p className="cl-tile-note">{a.unavailable ? '当前无权展示' : ''}</p>
    const time = a.kind === 'video' ? mediaDuration(a) : ''
    return (
      <>
        <MediaFigure content={a.content} kind={a.kind} title={a.title} className="cl-media" />
        {a.kind === 'video' && (
          <span className="cl-video-badge">
            <Play size={10} />
            {time}
          </span>
        )}
      </>
    )
  }
  const text = a.unavailable ? '' : contentText(a.content?.payload)
  if (a.kind === 'audio')
    return (
      <div className="cl-card cl-card-audio">
        <span className="cl-wave" aria-hidden="true">
          {audioBars(a.id).map((h, i) => (
            <i key={i} style={{ height: `${Math.round(h * 100)}%` }} />
          ))}
        </span>
        <span className="cl-card-row">
          <span className="cl-play" aria-hidden="true">
            <Play size={10} />
          </span>
          <b>{a.title}</b>
          <small>{a.unavailable ? '当前无权展示' : mediaDuration(a)}</small>
        </span>
      </div>
    )
  if (a.kind === 'link')
    return (
      <div className="cl-card cl-card-link">
        <b>{a.title}</b>
        <small>
          <Globe size={12} />
          {a.unavailable ? '当前无权展示' : linkDomain(text)}
        </small>
      </div>
    )
  return (
    <div className="cl-card cl-card-text">
      <b>{a.title}</b>
      <p>{a.unavailable ? '当前无权展示' : text}</p>
    </div>
  )
}

// 瀑布流：按累计高度分到最矮的一列，保持从左到右、从上到下的阅读顺序。
export function Masonry({
  items,
  columns,
  tile,
}: {
  items: api.Asset[]
  columns: number
  tile: (a: api.Asset) => React.ReactNode
}) {
  const cols: api.Asset[][] = Array.from({ length: columns }, () => [])
  const heights = Array.from({ length: columns }, () => 0)
  for (const a of items) {
    const i = heights.indexOf(Math.min(...heights))
    cols[i]!.push(a)
    heights[i]! += tileRatio(a)
  }
  return (
    <div className="cl-masonry">
      {cols.map((c, i) => (
        <div key={i} className="cl-masonry-col">
          {c.map(tile)}
        </div>
      ))}
    </div>
  )
}
