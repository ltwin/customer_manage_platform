import { useEffect, useState } from 'react'
import type * as api from '../editor/api.ts'
import { coverMedia, type ProjectState } from './projectList.ts'
import './cover.css'

// 项目封面是默认画布的缩影：抽样节点按边界框等比缩进卡片，保留 8% 边距。
// 首页「继续创作」与项目页共用。

type CoverNode = api.Project['cover']['nodes'][number]

// 卡片是 16:10，用 160×100 的虚拟坐标排版，再换成百分比。
const boxWidth = 160
const boxHeight = 100
const margin = 0.08
const maxMedia = 6

type Placed = CoverNode & {
  left: number
  top: number
  w: number
  h: number
}

function layoutCover(nodes: CoverNode[]): Placed[] {
  if (!nodes.length) return []
  const minX = Math.min(...nodes.map((n) => n.x))
  const minY = Math.min(...nodes.map((n) => n.y))
  const maxX = Math.max(...nodes.map((n) => n.x + n.width))
  const maxY = Math.max(...nodes.map((n) => n.y + n.height))
  const spanX = Math.max(maxX - minX, 1)
  const spanY = Math.max(maxY - minY, 1)
  const scale = Math.min(
    (boxWidth * (1 - 2 * margin)) / spanX,
    (boxHeight * (1 - 2 * margin)) / spanY,
  )
  const offsetX = (boxWidth - spanX * scale) / 2
  const offsetY = (boxHeight - spanY * scale) / 2
  return nodes.map((n) => ({
    ...n,
    left: offsetX + (n.x - minX) * scale,
    top: offsetY + (n.y - minY) * scale,
    w: n.width * scale,
    h: n.height * scale,
  }))
}

const percent = (value: number, total: number) => `${(value / total) * 100}%`

export default function ProjectCover({
  project,
  state,
}: {
  project: api.Project
  state: ProjectState
}) {
  const placed = layoutCover(project.cover.nodes)
  const byID = new Map(placed.map((n) => [n.id, n]))
  // 分组垫在最下面；每个封面最多取 6 张图，其余媒体节点画成色块。
  const ordered = [
    ...placed.filter((n) => n.kind === 'group'),
    ...placed.filter((n) => n.kind !== 'group'),
  ]
  const media = new Set(
    placed
      .filter((n) => n.content_revision_id)
      .slice(0, maxMedia)
      .map((n) => n.id),
  )
  return (
    <div className="pc-cover">
      {placed.length ? (
        <>
          <svg
            viewBox={`0 0 ${boxWidth} ${boxHeight}`}
            preserveAspectRatio="none"
            aria-hidden="true"
          >
            {project.cover.edges.map((edge) => {
              const s = byID.get(edge.source)
              const t = byID.get(edge.target)
              if (!s || !t) return null
              const x1 = s.left + s.w
              const y1 = s.top + s.h / 2
              const x2 = t.left
              const y2 = t.top + t.h / 2
              const mid = (x1 + x2) / 2
              return (
                <path
                  key={`${edge.source}-${edge.target}`}
                  d={`M${x1} ${y1} C${mid} ${y1} ${mid} ${y2} ${x2} ${y2}`}
                />
              )
            })}
          </svg>
          {ordered.map((n) => (
            <CoverCell
              key={n.id}
              node={n}
              revision={media.has(n.id) ? n.content_revision_id : undefined}
            />
          ))}
          {project.cover.more && <span className="pc-more">部分节点</span>}
        </>
      ) : (
        <span className="pc-blank">空白画布</span>
      )}
      {state && (
        <span className={`pc-badge ${state}`}>
          {state === 'wait' ? '等你确认' : '运行中'}
        </span>
      )}
    </div>
  )
}

function CoverCell({
  node,
  revision,
}: {
  node: Placed
  revision: string | undefined
}) {
  const style = {
    left: percent(node.left, boxWidth),
    top: percent(node.top, boxHeight),
    width: percent(node.w, boxWidth),
    height: percent(node.h, boxHeight),
  }
  const kind =
    node.kind === 'text' ||
    node.kind === 'empty' ||
    node.kind === 'group' ||
    node.kind === 'image' ||
    node.kind === 'video'
      ? node.kind
      : 'other'
  return (
    <div className={`pc-node pc-${kind}`} style={style} data-kind={node.kind}>
      {kind === 'text' && (
        <>
          <i />
          <i />
          <i />
          <i />
        </>
      )}
      {revision && <CoverMedia revision={revision} />}
    </div>
  )
}

function CoverMedia({ revision }: { revision: string }) {
  const [media, setMedia] = useState<{
    revision: string
    url: string
    video: boolean
  }>()
  useEffect(() => {
    let active = true
    coverMedia(revision)
      .then((ticket) => {
        if (active)
          setMedia({
            revision,
            url: ticket.url,
            video: ticket.mime.startsWith('video/'),
          })
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [revision])
  if (media?.revision !== revision) return null
  return media.video ? (
    <video src={media.url} muted playsInline preload="metadata" />
  ) : (
    <img src={media.url} alt="" draggable={false} loading="lazy" />
  )
}
