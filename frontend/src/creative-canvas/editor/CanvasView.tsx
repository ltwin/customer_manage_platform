import {
  memo,
  useCallback,
  useEffect,
  useRef,
  useState,
  type MutableRefObject,
  type ReactNode,
} from 'react'
import {
  ReactFlow,
  Background,
  MiniMap,
  useViewport,
  applyNodeChanges,
  PanOnScrollMode,
  useReactFlow,
  useKeyPress,
  SelectionMode,
  Handle,
  Position,
  NodeResizer,
  BaseEdge,
  EdgeLabelRenderer,
  ViewportPortal,
  getBezierPath,
} from '@xyflow/react'
import type {
  Node,
  NodeProps,
  NodeChange,
  EdgeProps,
  Edge,
  InternalNode,
  ReactFlowInstance,
  ConnectionLineComponentProps,
} from '@xyflow/react'
import {
  Type,
  Link2,
  Pencil,
  Maximize2,
  FileText,
  Minus,
  Plus,
  Scan,
  Ellipsis,
  SquareDashedMousePointer,
  Map as MapIcon,
  Folder,
  FolderPlus,
  FolderMinus,
  Copy,
  Trash2,
  Scissors,
  Undo2,
  Redo2,
  Image as ImageIcon,
  Film,
  AudioLines,
  Download,
  Library,
  Upload,
  Sparkles,
  LoaderCircle,
} from 'lucide-react'
import {
  localMedia,
  useLocalMedia,
  useLocalMediaVersion,
  type LocalMedia,
} from './localMedia.ts'
import type { Asset, Canvas, CanvasNode, GraphAction } from './api.ts'
import { MediaFigure } from './MediaView.tsx'
import { isMediaKind, type MediaKind } from './media.ts'
import { parentFirst, selectionRoots, worldPoint } from './graph.ts'
import { contentText } from './content.ts'
import { attachMagneticPorts } from './magneticPorts.ts'
import CanvasContextMenu, { type CanvasMenuItem } from './CanvasContextMenu.tsx'
import NodeVersionStrip from './NodeVersionStrip.tsx'
import { defaultNodeSize, fitMediaHeight, nodeSizeLimits } from './outbox.ts'
import { libraryDropZone, markLibraryDrop } from './libraryDrop.ts'
import type { Content } from './api.ts'
import * as api from './api.ts'
import { needsModelLabel, nodeLabelMeta } from './labelMeta.ts'
import {
  fitViewport,
  nodeBounds,
  panelInsets,
  validViewport,
  zoomLimits,
} from './viewport.ts'
import type { GenerationCatalog } from './generation.ts'
import {
  InlineNodeTitle,
  InlineNodeText,
  type InlineContentEditing,
} from './InlineNodeEditor.tsx'

type FlowNode = Node<
  {
    item: CanvasNode
    renaming: boolean
    rename: (id: string | null) => void
    saveTitle: (id: string, title: string) => void
    inlineContent?: InlineContentEditing
    select: (id: string) => void
    port: (
      id: string,
      side: 'input' | 'output',
      point: { x: number; y: number },
    ) => void
    resize: (
      id: string,
      width: number,
      height: number,
      x: number,
      y: number,
    ) => void
    disabled: boolean
    canvasID: string
    selectVersion: (
      id: string,
      versionID: string,
      followLatest?: boolean,
    ) => void
    /** 只有单选时才显示版本条，多选不打扰。 */
    previewContent: Content | null
    previewVersion: (id: string, content: Content | null) => void
    solo: boolean
    /** 卡片外标签右侧的版本/模型元信息，空串不显示。 */
    meta: string
    /** 空媒体节点显示的比例：本机草稿优先，其次已保存的提示词。 */
    ratio: string
    /** 空媒体节点里的两个入口都填充这个节点本身。 */
    fillEmpty: (id: string, kind: MediaKind, via: 'upload' | 'library') => void
    /** 本机预览读出的尺寸；变化时按比例重算高度。 */
    localSize: string
  },
  'content'
>
type AddMenuState = {
  screen: { x: number; y: number }
  world: { x: number; y: number }
  link?: { id: string; side: 'input' | 'output' }
  dropped?: boolean
}
const NODE_W = defaultNodeSize('core.text').width,
  NODE_H = defaultNodeSize('core.text').height
// 复制偏移（PRD CV-A0）。
const COPY_OFFSET = 40
export const nodeKind = (typeKey: string): string =>
  typeKey.startsWith('core.') ? typeKey.slice(5) : typeKey
const connectableTypes = [
  'core.text',
  'core.link',
  'core.image',
  'core.video',
  'core.audio',
]
const kindLabel: Record<string, string> = {
  text: '文字',
  link: '文字',
  image: '图片',
  video: '视频',
  audio: '音频',
  group: '布局分组',
}
export const untitled = (typeKey: string) =>
  typeKey === 'core.group'
    ? '未命名分组'
    : `未命名${kindLabel[nodeKind(typeKey)] ?? '节点'}`
// 空媒体节点显示草稿里的比例；未选时与默认画幅一致。
function emptyRatio(ratio: string) {
  if (ratio === 'auto') return '自动'
  return ratio || '1:1'
}

function KindIcon({ typeKey, size = 14 }: { typeKey: string; size?: number }) {
  switch (typeKey) {
    case 'core.group':
      return <Folder size={size} />
    case 'internal.document':
      return <FileText size={size} />
    case 'core.text':
    case 'core.link':
      return <Type size={size} />
    case 'core.image':
      return <ImageIcon size={size} />
    case 'core.video':
      return <Film size={size} />
    case 'core.audio':
      return <AudioLines size={size} />
    default:
      return <Link2 size={size} />
  }
}
// 本机文件预览：上传中变暗加转圈和进度；服务器内容到达后叠在下面等它加载完。
function LocalPreview({ local, done = false }: { local: LocalMedia; done?: boolean }) {
  const percent = Math.round((local.progress ?? 0) * 100)
  return (
    <div
      className={`cc-local-media ${done ? 'is-done' : ''} ${local.failed ? 'is-failed' : ''}`}
      role="status"
      aria-label={local.failed ? '上传失败' : done ? '正在显示' : `上传中 ${percent}%`}
    >
      {local.kind === 'image' ? (
        <img src={local.url} alt="" draggable={false} />
      ) : local.kind === 'video' ? (
        <video src={local.url} muted playsInline preload="metadata" />
      ) : (
        <AudioLines size={26} />
      )}
      {!done && (
        <span className="cc-local-state">
          {local.failed ? (
            '上传失败 · 可在导入列表重试'
          ) : (
            <>
              <LoaderCircle size={22} className="cc-spin" />
              <small>{percent}%</small>
            </>
          )}
        </span>
      )}
    </div>
  )
}

const Card = memo(function Card({ data, selected }: NodeProps<FlowNode>) {
  // 版本预览只存在于卡片内；取消选中即回到主版本，下游始终用主版本。
  const preview = data.previewContent
  const changePreview = data.previewVersion
  const previewNodeID = data.item.id
  const setPreview = useCallback(
    (content: Content | null) => changePreview(previewNodeID, content),
    [changePreview, previewNodeID],
  )
  const showVersions = !!selected && data.solo
  // 拖动缩放时的实时尺寸提示，只是卡片内显示用。
  const [resizing, setResizing] = useState<{
    width: number
    height: number
  } | null>(null)
  useEffect(() => {
    if (preview && preview.id === data.item.content?.id) setPreview(null)
  }, [preview, data.item.content?.id, setPreview])
  // 拖入或上传的文件在服务器内容加载出来之前直接用本机文件显示。
  const local = useLocalMedia(data.item.id)
  const serverReady = useCallback(
    () => localMedia.clear(previewNodeID),
    [previewNodeID],
  )
  useEffect(() => {
    // 音频卡片只显示图标，没有可等的加载完成事件。
    if (local && data.item.content && local.kind === 'audio') serverReady()
  }, [local, data.item.content, serverReady])
  const n = data.item,
    p = n.content?.payload,
    kind = nodeKind(n.metadata.type_key),
    media = isMediaKind(kind),
    group = n.metadata.type_key === 'core.group',
    proportional = kind === 'image' || kind === 'video',
    known = [
      'core.text',
      'core.link',
      'core.image',
      'core.video',
      'core.audio',
      'core.group',
      'internal.document',
    ].includes(n.metadata.type_key)
  return (
    <article
      className={`cc-node ${group ? 'cc-group-node' : media ? `cc-media-node cc-${kind}-node` : ['core.text', 'core.link'].includes(n.metadata.type_key) ? 'cc-text-node' : 'cc-link-node'} ${selected ? 'is-selected' : ''}`}
    >
      {/* 只有单选的普通节点能缩放（PRD CV-17）；组由子节点撑开。 */}
      <NodeResizer
        isVisible={
          !!selected &&
          data.solo &&
          !group &&
          !data.disabled &&
          n.capabilities.actions.includes('resize')
        }
        minWidth={nodeSizeLimits.minWidth}
        maxWidth={nodeSizeLimits.maxWidth}
        // 等比的媒体只约束宽度，高度跟内容比例走。
        minHeight={proportional ? 0 : nodeSizeLimits.minHeight}
        keepAspectRatio={proportional}
        onResize={(_, size) =>
          setResizing({
            width: Math.round(size.width),
            height: Math.round(size.height),
          })
        }
        onResizeEnd={(_, size) => {
          setResizing(null)
          data.resize(n.id, size.width, size.height, size.x, size.y)
        }}
      />
      {resizing && (
        <span className="cc-resize-tip" role="status">
          {resizing.width} × {resizing.height}
        </span>
      )}
      <header
        className="cc-node-header"
        onClick={() => data.select(n.id)}
        onDoubleClick={(event) => {
          event.stopPropagation()
          if (!data.disabled && n.capabilities.actions.includes('rename'))
            data.rename(n.id)
        }}
      >
        <KindIcon typeKey={n.metadata.type_key} />
        {data.renaming ? (
          <InlineNodeTitle
            title={n.metadata.title}
            disabled={data.disabled}
            onSave={(title) => data.saveTitle(n.id, title)}
            onClose={() => data.rename(null)}
          />
        ) : (
          <h3 title={n.metadata.title || untitled(n.metadata.type_key)}>
            {n.metadata.title || untitled(n.metadata.type_key)}
          </h3>
        )}
        {data.meta && !data.renaming && (
          <span className="cc-node-meta">{data.meta}</span>
        )}
      </header>
      {!group && media && (
        <div className="cc-node-content cc-node-media">
          {!known || n.status.content_state === 'unavailable' ? (
            <p>{!known ? '此节点类型当前只读' : '当前无权展示此内容'}</p>
          ) : n.content ? (
            <>
              <MediaFigure
                content={preview ?? n.content}
                kind={kind}
                title={n.metadata.title || untitled(n.metadata.type_key)}
                controls={kind !== 'image'}
                hoverPlay={kind === 'video'}
                onReady={local ? serverReady : undefined}
              />
              {local && <LocalPreview local={local} done />}
              {preview && (
                <span className="cc-preview-flag">
                  预览版本 · 下游仍用主版本
                </span>
              )}
              {showVersions && kind !== 'audio' && (
                <NodeVersionStrip
                  canvasID={data.canvasID}
                  nodeID={n.id}
                  kind={kind}
                  dataRevision={`${n.data_revision}:${n.status.status_revision}`}
                  viewing={preview?.id ?? null}
                  disabled={data.disabled}
                  pinned={n.data.config.main_version_mode === 'pinned'}
                  onPreview={setPreview}
                  onSelectVersion={(versionID, followLatest) =>
                    data.selectVersion(n.id, versionID, followLatest)
                  }
                />
              )}
            </>
          ) : local ? (
            <LocalPreview local={local} />
          ) : (
            <div
              className="cc-media-empty"
              aria-label={`空${kindLabel[kind]}节点`}
              role="group"
            >
              <KindIcon typeKey={n.metadata.type_key} size={36} />
              {kind !== 'audio' && (
                <span className="cc-empty-ratio">
                  {emptyRatio(data.ratio)}
                </span>
              )}
              {kind !== 'audio' && (
                <span className="cc-empty-hint">
                  选中后输入提示词生成
                  <br />
                  或拖入{kindLabel[kind]}
                </span>
              )}
              {!data.disabled && n.capabilities.actions.includes('replace') && (
                <span className="cc-empty-actions nodrag">
                  <button
                    type="button"
                    aria-label="上传到此节点"
                    onClick={(e) => {
                      e.stopPropagation()
                      data.fillEmpty(n.id, kind as MediaKind, 'upload')
                    }}
                  >
                    上传
                  </button>
                  <button
                    type="button"
                    aria-label="从资产库填充"
                    onClick={(e) => {
                      e.stopPropagation()
                      data.fillEmpty(n.id, kind as MediaKind, 'library')
                    }}
                  >
                    资产库
                  </button>
                </span>
              )}
            </div>
          )}
        </div>
      )}
      {!group && !media && (
        <div className="cc-node-content">
          {data.inlineContent ? (
            data.inlineContent.draft ? (
              <InlineNodeText
                {...data.inlineContent}
                draft={data.inlineContent.draft}
              />
            ) : (
              <p role="status">正在读取完整正文…</p>
            )
          ) : known &&
            n.status.content_state !== 'unavailable' &&
            !contentText(p) ? (
            <div
              className="cc-node-empty"
              role="img"
              aria-label="双击编辑节点"
              title="双击编辑节点"
            >
              <KindIcon typeKey={n.metadata.type_key} size={40} />
              <span className="cc-empty-hint">
                {n.metadata.type_key === 'core.link'
                  ? '双击填写链接地址'
                  : '双击输入文字'}
              </span>
            </div>
          ) : (
            <p>
              {!known
                ? '此节点类型当前只读'
                : n.status.content_state === 'unavailable'
                  ? '当前无权展示此内容'
                  : contentText(p)}
            </p>
          )}
          {!data.inlineContent && n.content?.truncated && (
            <small>正文预览 · 编辑时载入全文</small>
          )}
        </div>
      )}
      {!group &&
        connectableTypes.includes(n.metadata.type_key) &&
        (['input', 'output'] as const).map((side) => (
          <Handle
            key={side}
            id={side === 'input' ? 'reference' : 'output'}
            type={side === 'input' ? 'target' : 'source'}
            position={side === 'input' ? Position.Left : Position.Right}
            className={`cc-reference-handle cc-port-${side}`}
            isConnectable={
              !data.disabled && n.capabilities.actions.includes('move')
            }
          >
            <span className="cc-port-target">
              <button
                tabIndex={selected ? 0 : -1}
                disabled={
                  data.disabled || !n.capabilities.actions.includes('move')
                }
                aria-label={side === 'input' ? '添加上游参考' : '添加下游节点'}
                title={side === 'input' ? '添加上游参考' : '添加下游节点'}
                aria-haspopup="dialog"
                onClick={(e) => {
                  e.stopPropagation()
                  const rect = e.currentTarget.getBoundingClientRect()
                  data.port(
                    n.id,
                    side,
                    e.detail === 0
                      ? {
                          x: rect.left + rect.width / 2,
                          y: rect.top + rect.height / 2,
                        }
                      : { x: e.clientX, y: e.clientY },
                  )
                }}
              >
                <Plus size={18} />
              </button>
            </span>
          </Handle>
        ))}
    </article>
  )
})
function edgeAnchor(node: InternalNode<FlowNode>, side: 'input' | 'output') {
  const p = node.internals.positionAbsolute
  return {
    x: p.x + (side === 'output' ? (node.measured.width ?? NODE_W) : 0),
    y: p.y + (node.measured.height ?? NODE_H) / 2,
  }
}
// Preview and drop resolve the same painted node, with an 18 screen-pixel halo.
function connectionTarget(
  flow: ReactFlowInstance<FlowNode>,
  fromID: string,
  screen: { x: number; y: number },
) {
  const source = document.querySelector(
    `[data-id="${CSS.escape(fromID)}"].react-flow__node`,
  )
  const surface = source?.closest('.react-flow')
  const hit = document.elementFromPoint(screen.x, screen.y)
  if (!surface || !hit || !surface.contains(hit)) return null
  const eligible = (id: string | undefined) => {
    const n = id ? flow.getInternalNode(id) : undefined
    return n &&
      n.id !== fromID &&
      !n.data.disabled &&
      connectableTypes.includes(n.data.item.metadata.type_key) &&
      n.data.item.capabilities.actions.includes('move')
      ? n
      : null
  }
  const painted = hit.closest<HTMLElement>('.react-flow__node')
  if (painted && !painted.classList.contains('cc-group-container'))
    return eligible(painted.dataset.id)
  // Floating tools/selection controls must never connect through to the canvas.
  if (!hit.closest('.react-flow__pane,.cc-group-container')) return null
  let best: InternalNode<FlowNode> | null = null,
    distance = 18
  const point = flow.screenToFlowPosition(screen),
    zoom = flow.getViewport().zoom
  for (const current of flow.getNodes()) {
    const n = current.hidden ? null : eligible(current.id)
    if (!n) continue
    const p = n.internals.positionAbsolute
    const d =
      Math.hypot(
        Math.max(p.x - point.x, 0, point.x - p.x - (n.measured.width ?? NODE_W)),
        Math.max(p.y - point.y, 0, point.y - p.y - (n.measured.height ?? NODE_H)),
      ) * zoom
    if (d < distance) {
      best = n
      distance = d
    }
  }
  return best
}
// 拖线松手打开添加菜单时，虚线停在松手点，菜单关闭即消失（选定后由真实连线替换）。
function DraftLink({
  link,
  to,
}: {
  link: { id: string; side: 'input' | 'output' }
  to: { x: number; y: number }
}) {
  const flow = useReactFlow<FlowNode>()
  const node = flow.getInternalNode(link.id)
  if (!node) return null
  const output = link.side === 'output',
    from = edgeAnchor(node, link.side)
  const [path] = getBezierPath({
    sourceX: output ? from.x : to.x,
    sourceY: output ? from.y : to.y,
    sourcePosition: Position.Right,
    targetX: output ? to.x : from.x,
    targetY: output ? to.y : from.y,
    targetPosition: Position.Left,
  })
  return (
    <ViewportPortal>
      <svg className="cc-draft-link" aria-hidden="true">
        <path d={path} />
      </svg>
    </ViewportPortal>
  )
}
function ConnectionPreview(props: ConnectionLineComponentProps<FlowNode>) {
  const flow = useReactFlow<FlowNode>(),
    viewport = flow.getViewport()
  const screen = flow.flowToScreenPosition({
    x: (props.pointer.x - viewport.x) / viewport.zoom,
    y: (props.pointer.y - viewport.y) / viewport.zoom,
  })
  const target = connectionTarget(flow, props.fromNode.id, screen)
  const output = props.fromHandle.type === 'source'
  const from = edgeAnchor(props.fromNode, output ? 'output' : 'input')
  const to = target
    ? edgeAnchor(target, output ? 'input' : 'output')
    : {
        x: (props.pointer.x - viewport.x) / viewport.zoom,
        y: (props.pointer.y - viewport.y) / viewport.zoom,
      }
  const [path] = getBezierPath({
    sourceX: from.x,
    sourceY: from.y,
    sourcePosition: output ? Position.Right : Position.Left,
    targetX: to.x,
    targetY: to.y,
    targetPosition: output ? Position.Left : Position.Right,
  })
  return (
    <>
      {target && (
        <rect
          className="cc-connection-target"
          x={target.internals.positionAbsolute.x}
          y={target.internals.positionAbsolute.y}
          width={target.measured.width}
          height={target.measured.height}
          rx={13}
        />
      )}
      <path
        className={`react-flow__connection-path cc-connection-preview ${target ? 'is-snapped' : ''}`}
        d={path}
      />
      {target && (
        <circle
          className="cc-connection-snap"
          cx={to.x}
          cy={to.y}
          r={4 / viewport.zoom}
        />
      )}
    </>
  )
}
type ReferenceFlowEdge = Edge<{
  showCut: boolean
  running: boolean
  disconnect: (byDoubleClick: boolean) => void
}>
function ReferenceEdge(props: EdgeProps<ReferenceFlowEdge>) {
  const { zoom } = useViewport()
  const flow = useReactFlow<FlowNode>()
  const source = flow.getInternalNode(props.source),
    target = flow.getInternalNode(props.target)
  const from = source
    ? edgeAnchor(source, 'output')
    : { x: props.sourceX, y: props.sourceY }
  const to = target
    ? edgeAnchor(target, 'input')
    : { x: props.targetX, y: props.targetY }
  const [path, labelX, labelY] = getBezierPath({
    ...props,
    sourceX: from.x,
    sourceY: from.y,
    targetX: to.x,
    targetY: to.y,
  })
  return (
    <>
      <BaseEdge {...props} path={path} />
      {props.selected && !props.data?.running && (
        <path d={path} className="cc-reference-flow" />
      )}
      {props.data?.showCut && (
        <EdgeLabelRenderer>
          <button
            type="button"
            className="cc-edge-cut nodrag nopan"
            style={{
              left: labelX,
              top: labelY,
              transform: `translate(-50%, -50%) scale(${1 / zoom})`,
            }}
            aria-label="删除连线"
            title="删除连线，可撤销"
            onPointerDown={(event) => event.stopPropagation()}
            onDoubleClick={(event) => event.stopPropagation()}
            onClick={(event) => {
              event.stopPropagation()
              // 双击连线中点时第二击会落在这个按钮上，按双击断线处理并给出撤销提示。
              props.data?.disconnect(event.detail >= 2)
            }}
          >
            <Scissors size={16} aria-hidden="true" />
          </button>
        </EdgeLabelRenderer>
      )}
    </>
  )
}
const pointOf = (e: MouseEvent | TouchEvent): [number, number] => {
  const p = 'changedTouches' in e ? e.changedTouches[0] : e
  return p ? [p.clientX, p.clientY] : [NaN, NaN]
}
const nodeTypes = { content: Card },
  edgeTypes = { reference: ReferenceEdge }
export default function CanvasView({
  canvas,
  selected,
  selection,
  onSelection,
  onSelect,
  onEdit,
  onCreated,
  onFocusPrompt,
  draftRatios,
  inlineContent,
  onPickFiles,
  onOpenLibrary,
  addRequest,
  panMode,
  onMove,
  onMoveSelection,
  onDropAsset,
  onDropAssets,
  onDropFiles,
  onUploadToNode,
  onDownload,
  onSaveToLibrary,
  onSaveNodes,
  onSaveNodesAsk,
  onFillNode,
  onUploadFileToNode,
  onLibraryFill,
  onPickReferences,
  centerRef,
  onPreview,
  onDraggingChange,
  onActions,
  onUndo,
  onRedo,
  canUndo,
  canRedo,
  disabled,
  moveDisabled,
  assets,
  account,
  onNotice,
}: {
  canvas: Canvas
  selected: string | null
  selection: string[]
  onSelection: (ids: string[]) => void
  onSelect: (id: string | null) => void
  onEdit: (id: string) => void
  /** 新建节点后：文本进入编辑，媒体把焦点交给生成输入框（PRD CV-53）。 */
  onCreated: (id: string, kind: 'text' | MediaKind, prompt?: string) => void
  /** 双击空媒体节点：只聚焦生成输入框，不上传也不生成（PRD CV-54）。 */
  onFocusPrompt: (id: string) => void
  /** 节点生成面板里尚未保存的比例，按节点ID。 */
  draftRatios?: Record<string, string>
  inlineContent?: InlineContentEditing
  onPickFiles: (x: number, y: number) => void
  onOpenLibrary: () => void
  /** 工具轨「+」或 T/I/O/A 快捷键：递增 seq 打开添加菜单，kind 存在时直接创建。 */
  addRequest: {
    seq: number
    kind?: 'text' | MediaKind
    anchor?: { x: number; y: number }
  }
  panMode: boolean
  onMove: (node: CanvasNode, x: number, y: number) => void
  onMoveSelection: (moves: { node: CanvasNode; x: number; y: number }[]) => void
  onDropAsset: (asset: Asset, x: number, y: number) => void
  onDropAssets: (assets: Asset[], x: number, y: number) => void
  onDropFiles: (files: File[], x: number, y: number) => void
  onUploadToNode: (id: string, kind: MediaKind) => void
  onDownload: (id: string) => void
  onSaveToLibrary: (id: string) => void
  /** 节点拖到资产抽屉上松手：存入资产库，groupID 为空即不归类。 */
  onSaveNodes: (ids: string[], groupID: string | null) => void
  /** 多选「存入资产库…」：先选分组再批量入库。 */
  onSaveNodesAsk: (ids: string[]) => void
  onFillNode: (id: string, asset: Asset) => void
  onUploadFileToNode: (id: string, file: File) => void
  onLibraryFill: (id: string, kind: MediaKind) => void
  onPickReferences: (id: string) => void
  /** 画布可见区域的世界坐标中心，供资产库放入时使用。 */
  centerRef: MutableRefObject<(() => { x: number; y: number }) | null>
  onPreview: (id: string, revision?: string) => void
  onDraggingChange: (dragging: boolean) => void
  onActions: (actions: GraphAction[]) => void
  onUndo: () => void
  onRedo: () => void
  canUndo: boolean
  canRedo: boolean
  disabled: boolean
  moveDisabled: boolean
  assets: Asset[]
  account: string
  /** 画布内操作的说明提示，例如连线被拒的原因、双击断线可撤销。 */
  onNotice: (message: string) => void
}) {
  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      const target = event.target
      if (
        disabled ||
        event.defaultPrevented ||
        event.isComposing ||
        !(event.metaKey || event.ctrlKey) ||
        event.altKey ||
        !(target instanceof HTMLElement) ||
        target.isContentEditable ||
        target.closest(
          'input,textarea,select,[role="textbox"],[role="dialog"],[aria-modal="true"]',
        )
      )
        return
      const key = event.key.toLowerCase()
      if (key !== 'z' && !(event.ctrlKey && key === 'y')) return
      event.preventDefault()
      if (event.shiftKey || key === 'y') {
        if (canRedo) onRedo()
      } else if (canUndo) onUndo()
    }
    window.addEventListener('keydown', keydown)
    return () => window.removeEventListener('keydown', keydown)
  }, [disabled, canUndo, canRedo, onUndo, onRedo])
  const callbacks = useRef({
    onSelect,
    onSelection,
    onActions,
    canvasNodes: canvas.nodes,
    selection,
  })
  callbacks.current = {
    onSelect,
    onSelection,
    onActions,
    canvasNodes: canvas.nodes,
    selection,
  }
  const resizePreviews = useRef(
    new Map<
      string,
      {
        item: CanvasNode
        x: number
        y: number
        width: number
        height: number
      }
    >(),
  )
  const flow = useReactFlow<FlowNode>(),
    viewport = useViewport(),
    spaceHeld = useKeyPress('Space'),
    // Space 只在没有选中时平移；有选中时交给全屏预览（PRD CV-01、§7 快捷键）。
    panHeld = spaceHeld && !selection.length
  const [size, setSize] = useState({ width: 0, height: 0 }),
    [minimap, setMinimap] = useState(false),
    [nodes, setNodes] = useState<FlowNode[]>([])
  // 工具轨、双击空白、右键空白、端口共用一个添加菜单。
  // link：从端口出发时要连到的节点；dropped：拖线松手时新节点端口落在松手点。
  const [addMenu, setAddMenu] = useState<AddMenuState | null>(null)
  const createNodeRef = useRef<
    (kind: 'text' | MediaKind, menu: AddMenuState) => void
  >(() => {})
  const [selectedEdge, setSelectedEdge] = useState<string | null>(null)
  const [contextMenu, setContextMenu] = useState<{
    x: number
    y: number
    nodeIDs: string[]
    edgeID?: string
  } | null>(null)
  const [renamingNode, setRenamingNode] = useState<string | null>(null)
  const renameNode = useCallback(
    (id: string | null) => {
      if (id && disabled) return
      if (id) callbacks.current.onSelection([id])
      setRenamingNode(id)
    },
    [disabled],
  )
  const saveTitle = useCallback((id: string, title: string) => {
    const current = callbacks.current.canvasNodes.find((n) => n.id === id)
    if (current)
      callbacks.current.onActions([
        {
          type: 'update_metadata',
          node_id: id,
          title,
          intent: current.metadata.intent,
        },
      ])
  }, [])

  const element = useRef<HTMLDivElement>(null),
    dragging = useRef(new Set<string>()),
    dragBases = useRef(new Map<string, CanvasNode>())
  // 单击已在多选集合中的节点只切换焦点节点，不打散多选（PRD CV-20）。
  const selectNode = useCallback((id: string) => {
    setSelectedEdge(null)
    callbacks.current.onSelect(id)
    const current = callbacks.current.selection
    if (!(current.length > 1 && current.includes(id)))
      callbacks.current.onSelection([id])
  }, [])
  const openPort = useCallback(
    (
      id: string,
      side: 'input' | 'output',
      screen: { x: number; y: number },
    ) => {
      const node = flow.getInternalNode(id)
      if (!node) return
      const p = node.internals.positionAbsolute
      setAddMenu({
        screen,
        link: { id, side },
        // 未拖线时新节点放在来源节点旁 120 世界像素。
        world: {
          x:
            side === 'output'
              ? p.x + (node.measured.width ?? NODE_W) + 120
              : p.x - 120,
          y: p.y + (node.measured.height ?? NODE_H) / 2,
        },
      })
    },
    [flow],
  )
  const lastAddRequest = useRef(addRequest.seq)
  useEffect(() => {
    if (addRequest.seq === lastAddRequest.current) return
    lastAddRequest.current = addRequest.seq
    const bounds = element.current?.getBoundingClientRect()
    if (!bounds) return
    const center = {
      x: bounds.left + bounds.width / 2,
      y: bounds.top + bounds.height / 2,
    }
    const menu = {
      screen: addRequest.anchor ?? center,
      world: flow.screenToFlowPosition(center),
    }
    if (addRequest.kind) createNodeRef.current(addRequest.kind, menu)
    else setAddMenu(menu)
  }, [addRequest, flow])
  // 适应画布：包住全部节点（含 28px 标题区），避开资产库和助手面板（PRD CV-03、AL-02）。
  const fitCanvas = useCallback(
    (duration = 240, ids?: string[]) => {
      const stage = element.current?.getBoundingClientRect()
      const all = callbacks.current.canvasNodes
      const bounds = nodeBounds(
        ids ? all.filter((n) => ids.includes(n.id)) : all,
      )
      if (!stage || !bounds) return
      const library = document.getElementById('creative-library')
      const agent = document.querySelector('.cc-agent-panel')
      const insets = panelInsets(
        stage,
        library && !library.hidden ? library.getBoundingClientRect() : null,
        agent ? agent.getBoundingClientRect() : null,
      )
      void flow.setViewport(fitViewport(bounds, stage, insets), { duration })
    },
    [flow],
  )
  useEffect(() => {
    // 资产抽屉盖住左侧时，放入点往右让，落在看得见的地方。
    centerRef.current = () => {
      const b = element.current?.getBoundingClientRect()
      if (!b) return { x: 0, y: 0 }
      const drawer = document.getElementById('creative-library')
      const left =
        drawer && !drawer.hidden ? drawer.getBoundingClientRect().right : b.left
      return flow.screenToFlowPosition({
        x: (Math.max(left, b.left) + b.right) / 2,
        y: b.top + b.height / 2,
      })
    }
    return () => {
      centerRef.current = null
    }
  }, [centerRef, flow])
  const magnetBlocked = useRef(false)
  magnetBlocked.current =
    disabled || panMode || panHeld || !!addMenu || !!contextMenu
  useEffect(() => {
    if (!element.current) return
    return attachMagneticPorts(
      element.current,
      () => flow.getViewport().zoom,
      () => magnetBlocked.current || dragging.current.size > 0,
    )
  }, [flow])
  useEffect(() => {
    const release = () => onDraggingChange(false)
    window.addEventListener('pointerup', release)
    window.addEventListener('blur', release)
    return () => {
      window.removeEventListener('pointerup', release)
      window.removeEventListener('blur', release)
      release()
    }
  }, [onDraggingChange])
  // 本机预览尺寸读出后重算节点高度。
  const localVersion = useLocalMediaVersion()
  const [cardPreviews, setCardPreviews] = useState<
    Record<string, Content | null>
  >({})
  useEffect(() => {
    setCardPreviews((old) => {
      let changed = false
      const next = { ...old }
      for (const id of Object.keys(next))
        if (next[id] && !(selection.length === 1 && selection[0] === id)) {
          next[id] = null
          changed = true
        }
      return changed ? next : old
    })
  }, [selection])
  const previewVersion = useCallback(
    (id: string, content: Content | null) =>
      setCardPreviews((old) =>
        old[id] === content ? old : { ...old, [id]: content },
      ),
    [],
  )
  const selectVersion = useCallback(
    (id: string, versionID: string, followLatest = false) => {
      callbacks.current.onActions([
        {
          follow_latest: followLatest,
          type: 'select_version',
          node_id: id,
          version_id: versionID,
        },
      ])
    },
    [],
  )
  const emptyCallbacks = useRef({ onUploadToNode, onLibraryFill })
  emptyCallbacks.current = { onUploadToNode, onLibraryFill }
  const fillEmpty = useCallback(
    (id: string, kind: MediaKind, via: 'upload' | 'library') => {
      callbacks.current.onSelection([id])
      if (via === 'upload') emptyCallbacks.current.onUploadToNode(id, kind)
      else emptyCallbacks.current.onLibraryFill(id, kind)
    },
    [],
  )
  const resize = useCallback(
    (id: string, width: number, height: number, x: number, y: number) => {
      const source = callbacks.current.canvasNodes.find((n) => n.id === id)
      if (!source) return
      resizePreviews.current.set(id, { item: source, x, y, width, height })
      const actions: GraphAction[] = [
        { type: 'move_node', node_id: id, x, y },
        { type: 'resize_node', node_id: id, width, height },
      ]
      callbacks.current.onActions(actions)
    },
    [],
  )
  useEffect(() => {
    const observer = new ResizeObserver(([entry]) => {
      if (entry)
        setSize({
          width: entry.contentRect.width,
          height: entry.contentRect.height,
        })
    })
    if (element.current) observer.observe(element.current)
    return () => observer.disconnect()
  }, [])
  // 单版本节点显示模型名：目录按画布读一次，失败时直接显示模型 key。
  const [modelLabels, setModelLabels] = useState<Record<string, string>>({})
  const labelSource = canvas.nodes.find(needsModelLabel)?.id
  const hasLabels = Object.keys(modelLabels).length > 0
  useEffect(() => {
    if (!labelSource || hasLabels) return
    const controller = new AbortController()
    api
      .read<GenerationCatalog>(
        `/canvases/${encodeURIComponent(canvas.id)}/nodes/${encodeURIComponent(labelSource)}/generation-catalog`,
        controller.signal,
      )
      .then((catalog) => {
        if (!controller.signal.aborted)
          setModelLabels(
            Object.fromEntries(catalog.models.map((m) => [m.key, m.label])),
          )
      })
      .catch(() => {
        /* 元信息是辅助展示，读不到目录时显示模型 key。 */
      })
    return () => controller.abort()
  }, [canvas.id, labelSource, hasLabels])
  useEffect(() => {
    setNodes((previous) => {
      const byID = new Map(previous.map((n) => [n.id, n])),
        roots = new Set(selectionRoots(canvas.nodes, selection))
      const next = parentFirst(canvas.nodes).map((n): FlowNode => {
        const preview = resizePreviews.current.get(n.id)
        if (preview && preview.item !== n) resizePreviews.current.delete(n.id)
        const resized = preview?.item === n ? preview : undefined
        const old = byID.get(n.id),
          position =
            dragging.current.has(n.id) && old
              ? old.position
              : {
                  x: resized?.x ?? n.metadata.x,
                  y: resized?.y ?? n.metadata.y,
                },
          isSelected = selection.includes(n.id),
          previewContent =
            isSelected && selection.length === 1
              ? (cardPreviews[n.id] ?? null)
              : null,
          kind = nodeKind(n.metadata.type_key),
          meta = nodeLabelMeta(n, modelLabels),
          savedRatio = n.prompt?.parameters?.aspect_ratio,
          ratio =
            draftRatios?.[n.id] ??
            (typeof savedRatio === 'string' ? savedRatio : ''),
          local = n.content ? null : localMedia.get(n.id),
          localSize = local?.width ? `${local.width}x${local.height}` : '',
          mediaHeight =
            kind === 'image' || kind === 'video'
              ? local?.width && local.height
                ? (n.metadata.width * local.height) / local.width
                : fitMediaHeight(
                    previewContent ?? n.content,
                    n.metadata.width,
                    kind,
                    n.metadata.height,
                  )
              : n.metadata.height,
          draggable =
            !moveDisabled &&
            !panMode &&
            !panHeld &&
            (!isSelected || roots.has(n.id)) &&
            n.capabilities.actions.includes('move')
        if (
          old &&
          old.data.item === n &&
          old.data.previewContent === previewContent &&
          old.data.localSize === localSize &&
          old.selected === isSelected &&
          old.draggable === draggable &&
          old.data.disabled === disabled &&
          old.data.solo === (isSelected && selection.length === 1) &&
          old.data.meta === meta &&
          old.data.ratio === ratio &&
          old.data.renaming === (renamingNode === n.id) &&
          old.data.inlineContent ===
            (inlineContent?.nodeID === n.id ? inlineContent : undefined) &&
          old.position.x === position.x &&
          old.position.y === position.y &&
          old.parentId === (n.parent_id ?? undefined)
        )
          return old
        return {
          ...old,
          id: n.id,
          type: 'content',
          className:
            n.metadata.type_key === 'core.group'
              ? 'cc-group-container'
              : undefined,
          position,
          width:
            resized?.width ??
            (old?.data.item === n ? old.width : undefined) ??
            n.metadata.width,
          height:
            resized?.height ??
            (old?.data.item === n &&
            old.data.previewContent === previewContent &&
            old.data.localSize === localSize
              ? old.height
              : undefined) ??
            mediaHeight,
          parentId: n.parent_id ?? undefined,
          style: {
            width: n.metadata.width,
            height: mediaHeight,
          },
          data: {
            item: n,
            renaming: renamingNode === n.id,
            rename: renameNode,
            saveTitle,
            inlineContent:
              inlineContent?.nodeID === n.id ? inlineContent : undefined,
            select: selectNode,
            port: openPort,
            resize,
            disabled,
            canvasID: canvas.id,
            selectVersion,
            previewContent,
            previewVersion,
            solo: isSelected && selection.length === 1,
            meta,
            ratio,
            fillEmpty,
            localSize,
          },
          selected: isSelected,
          draggable,
        }
      })
      return next.length === previous.length &&
        next.every((n, i) => n === previous[i])
        ? previous
        : next
    })
  }, [
    canvas,
    selection,
    renamingNode,
    renameNode,
    saveTitle,
    inlineContent,
    moveDisabled,
    panMode,
    panHeld,
    selectNode,
    openPort,
    resize,
    disabled,
    selectVersion,
    fillEmpty,
    cardPreviews,
    previewVersion,
    modelLabels,
    localVersion,
    draftRatios,
  ])
  const pointerDown = useRef<{ x: number; y: number } | null>(null)
  const selectionStart = useRef<{ x: number; y: number } | null>(null)
  const selectionBox = useRef<{
    x: number
    y: number
    width: number
    height: number
  } | null>(null)
  const selectionRef = useRef(selection)
  selectionRef.current = selection
  const changeNodes = useCallback((changes: NodeChange<FlowNode>[]) => {
    setNodes((nodes) => applyNodeChanges(changes, nodes))
    // Only direct selection intents update the page. Mirroring the internal
    // selection observer back into controlled nodes loops during parent changes.
    const selections = changes.filter((c) => c.type === 'select')
    if (selections.length) {
      const ids = new Set(selectionRef.current)
      for (const c of selections) {
        if (c.selected) ids.add(c.id)
        else ids.delete(c.id)
      }
      selectionRef.current = [...ids]
      callbacks.current.onSelection(selectionRef.current)
    }
  }, [])
  const active = nodes.find((n) => n.id === selected),
    viewNodes = nodes.map((n) => ({
      ...n.data.item,
      metadata: { ...n.data.item.metadata, x: n.position.x, y: n.position.y },
    })),
    point = active ? worldPoint(viewNodes, active.id) : { x: 0, y: 0 },
    selectionReadOnly = selection.some(
      (id) =>
        !nodes
          .find((n) => n.id === id)
          ?.data.item.capabilities.actions.includes('move'),
    )
  // v6：至少两个独立节点（组算一个）才能打组。
  const canGroupIDs = (ids: string[]) =>
    selectionRoots(canvas.nodes, ids).length >= 2
  const canGroup = canGroupIDs(selection)
  const singleGroupSelected =
    selection.length === 1 &&
    active?.data.item.metadata.type_key === 'core.group'
  const toolbarX = Math.max(
      12,
      Math.min(
        size.width - 340,
        viewport.x +
          (point.x + (active?.width ?? NODE_W) / 2) * viewport.zoom -
          160,
      ),
    ),
    toolbarY = Math.max(
      12,
      Math.min(
        size.height - 100,
        viewport.y + (point.y - 30) * viewport.zoom - 54,
      ),
    )
  const expanded = new Set(selection)
  for (let changed = true; changed;) {
    changed = false
    for (const n of canvas.nodes)
      if (n.parent_id && expanded.has(n.parent_id) && !expanded.has(n.id)) {
        expanded.add(n.id)
        changed = true
      }
  }
  // Edges still waiting for their receipt are painted but not yet addressable.
  const persistedEdges = new Set(
    canvas.edges.filter((e) => !e.id.startsWith('pending:')).map((e) => e.id),
  )
  const running = new Set(
    canvas.nodes
      .filter((n) =>
        ['queued', 'running', 'reconciling'].includes(
          n.status.generation_state,
        ),
      )
      .map((n) => n.id),
  )
  // v6 连线：无箭头的中性白线；相关选中时提亮并沿数据流向走白光，运行中为琥珀虚线。
  const edges: ReferenceFlowEdge[] = canvas.edges.map((e) => {
    const hot =
        expanded.has(e.source_node_id) || expanded.has(e.target_node_id),
      live = running.has(e.target_node_id)
    return {
      id: e.id,
      selectable: persistedEdges.has(e.id),
      focusable: persistedEdges.has(e.id),
      source: e.source_node_id,
      target: e.target_node_id,
      sourceHandle: e.source_port,
      targetHandle: e.target_port,
      type: 'reference',
      className: live ? 'cc-edge-running' : undefined,
      data: {
        showCut: selectedEdge === e.id && persistedEdges.has(e.id) && !disabled,
        running: live,
        disconnect: (byDoubleClick: boolean) => {
          act([{ type: 'disconnect_reference', edge_id: e.id }])
          setSelectedEdge(null)
          if (byDoubleClick) onNotice('已断开连线，⌘Z 可撤销')
        },
      },
      selected: selectedEdge === e.id || hot,
      style: {
        pointerEvents: persistedEdges.has(e.id) ? 'auto' : 'none',
        stroke: live
          ? 'var(--run)'
          : selectedEdge === e.id
            ? 'var(--primary)'
            : hot
              ? 'rgba(255, 255, 255, 0.6)'
              : 'rgba(255, 255, 255, 0.22)',
        strokeWidth: 1.5,
        strokeDasharray: live ? '5 5' : undefined,
      },
    }
  })
  const act = (actions: GraphAction[]) => {
    if (!disabled) onActions(actions)
  }
  // 连线规则在本机先拦下并说明原因（PRD CV-35/37），不把必然被服务端拒绝的请求发出去。
  function connectionProblem(sourceID: string, targetID: string): string {
    if (sourceID === targetID) return '不能把节点连到它自己'
    const kind = (id: string) =>
      canvas.nodes.find((n) => n.id === id)?.metadata.type_key
    if (kind(sourceID) === 'core.group' || kind(targetID) === 'core.group')
      return '组不能作为连线的端点，请连到组内的节点'
    if (
      canvas.edges.some(
        (e) => e.source_node_id === sourceID && e.target_node_id === targetID,
      )
    )
      return '这两个节点已经连接'
    const next = new Map<string, string[]>()
    for (const e of canvas.edges)
      next.set(e.source_node_id, [
        ...(next.get(e.source_node_id) ?? []),
        e.target_node_id,
      ])
    // 目标节点能沿连线走回源节点，说明新连线会形成引用环。
    const seen = new Set<string>()
    const stack = [targetID]
    while (stack.length) {
      const id = stack.pop()!
      if (id === sourceID) return '这条连线会形成引用环，引用只能单向传递'
      if (seen.has(id)) continue
      seen.add(id)
      stack.push(...(next.get(id) ?? []))
    }
    return ''
  }
  function connect(sourceID: string, targetID: string) {
    const problem = connectionProblem(sourceID, targetID)
    if (problem) return onNotice(problem)
    act([
      {
        type: 'connect_reference',
        source_node_id: sourceID,
        target_node_id: targetID,
        source_port: 'output',
        target_port: 'reference',
        role: 'reference',
      },
    ])
  }
  function removeSelection() {
    if (selectedEdge) {
      if (persistedEdges.has(selectedEdge))
        act([{ type: 'disconnect_reference', edge_id: selectedEdge }])
      setSelectedEdge(null)
      return
    }
    if (selection.length)
      act([
        { type: 'remove_nodes', node_ids: selection, group_mode: 'subtree' },
      ])
  }
  function moveSelection(dx: number, dy: number) {
    const roots = selectionRoots(canvas.nodes, selection)
    const moves = nodes
      .filter((n) => roots.includes(n.id))
      .map((n) => ({
        node: n.data.item,
        x: n.position.x + dx,
        y: n.position.y + dy,
      }))
    if (
      moves.length === 1 &&
      !moves[0].node.parent_id &&
      moves[0].node.metadata.type_key !== 'core.group'
    )
      onMove(moves[0].node, moves[0].x, moves[0].y)
    else onMoveSelection(moves)
  }
  // 新节点位置：拖线松手时端口正好落在松手点；从端口点「+」时放在来源旁；其余以指针为中心。
  function createNode(kind: 'text' | MediaKind, menu: AddMenuState) {
    const id = `cwnode_${crypto.randomUUID()}`,
      link = menu.link,
      output = link?.side === 'output'
    const size = defaultNodeSize(`core.${kind}`)
    const x = link
        ? output
          ? menu.world.x
          : menu.world.x - size.width
        : menu.world.x - size.width / 2,
      y = menu.world.y - size.height / 2
    const actions: GraphAction[] = [
      { type: 'add_node', node_id: id, type_key: `core.${kind}`, x, y },
    ]
    if (link)
      actions.push({
        type: 'connect_reference',
        source_node_id: output ? link.id : id,
        target_node_id: output ? id : link.id,
        source_port: 'output',
        target_port: 'reference',
        role: 'reference',
      })
    act(actions)
    onSelection([id])
    onSelect(id)
    onCreated(id, kind)
  }
  // v6 派生：在来源右侧 120 世界像素新建下游节点，逐个连线并选中它，生成面板随之出现。
  // 派生预填（PRD CV-72）：文本→图片 `@文本`；图片→视频 `@源 镜头缓慢推进，`；多选→依次 `@`。
  function derive(sourceIDs: string[], kind: 'image' | 'video') {
    const sources = sourceIDs
      .map((id) => flow.getInternalNode(id))
      .filter((n): n is InternalNode<FlowNode> => !!n)
    if (!sources.length) return
    const right = Math.max(
      ...sources.map(
        (n) => n.internals.positionAbsolute.x + (n.measured.width ?? NODE_W),
      ),
    )
    const cy =
      sources.reduce(
        (sum, n) =>
          sum +
          n.internals.positionAbsolute.y +
          (n.measured.height ?? NODE_H) / 2,
        0,
      ) / sources.length
    const id = `cwnode_${crypto.randomUUID()}`
    act([
      {
        type: 'add_node',
        node_id: id,
        type_key: `core.${kind}`,
        x: right + 120,
        y: cy - defaultNodeSize(`core.${kind}`).height / 2,
      },
      ...sources.map((n): GraphAction => ({
        type: 'connect_reference',
        source_node_id: n.id,
        target_node_id: id,
        source_port: 'output',
        target_port: 'reference',
        role: 'reference',
      })),
    ])
    onSelection([id])
    onSelect(id)
    const mentions = sources.map((n) => `@[${n.id}]`).join(' ')
    onCreated(id, kind, kind === 'video' ? `${mentions} 镜头缓慢推进，` : `${mentions} `)
  }
  createNodeRef.current = createNode
  function addMenuItems(menu: AddMenuState): CanvasMenuItem[] {
    const from =
      menu.link?.side === 'output'
        ? canvas.nodes.find((n) => n.id === menu.link?.id)
        : undefined
    const fromText =
      from && ['core.text', 'core.link'].includes(from.metadata.type_key)
    const kinds = [
      ['text', '文字', 'T', ''],
      ['image', '图片', 'I', fromText ? '以上游文本为提示词' : ''],
      ['video', '视频', 'O', ''],
      ['audio', '音频', 'A', ''],
    ] as const
    const items: CanvasMenuItem[] = [
      {
        section: !menu.link
          ? '添加节点'
          : menu.link.side === 'output'
            ? '生成下游节点'
            : '添加上游参考',
      },
      ...kinds.map(([kind, label, hint, sub]) => ({
        label,
        icon: <KindIcon typeKey={`core.${kind}`} size={16} />,
        shortcut: hint || undefined,
        sub: sub || undefined,
        disabled,
        action: () => createNode(kind, menu),
      })),
    ]
    if (!menu.link)
      items.push(
        'separator',
        {
          label: '上传文件',
          icon: <Upload size={16} />,
          disabled,
          action: () => onPickFiles(menu.world.x, menu.world.y),
        },
        {
          label: '从资产库添加',
          icon: <Library size={16} />,
          action: onOpenLibrary,
        },
      )
    return items
  }
  function selectAll() {
    const roots = canvas.nodes.filter((n) => !n.parent_id).map((n) => n.id)
    setSelectedEdge(null)
    setAddMenu(null)
    onSelection(roots)
    onSelect(roots[0] ?? null)
  }
  // 节点/选区菜单：右键与工具条「更多」共用（PRD CV-74/80/81/82）。
  function nodeMenuItems(ids: string[]): CanvasMenuItem[] {
    const items: CanvasMenuItem[] = []
    const target = canvas.nodes.find((n) => n.id === ids[0])
    if (!target) return items
    const one = ids.length === 1
    const groupOnly = one && target.metadata.type_key === 'core.group'
    const locked =
      disabled ||
      ids.some(
        (id) =>
          !canvas.nodes
            .find((n) => n.id === id)
            ?.capabilities.actions.includes('move'),
      )
    const groups = ids.filter(
      (id) =>
        canvas.nodes.find((n) => n.id === id)?.metadata.type_key ===
        'core.group',
    )
    const copy: CanvasMenuItem = {
      label: groupOnly ? '复制整组' : '复制',
      icon: <Copy size={16} />,
      shortcut: '⌘D',
      disabled: locked,
      action: () =>
        act([
          {
            type: 'duplicate_selection',
            node_ids: ids,
            dx: COPY_OFFSET,
            dy: COPY_OFFSET,
          },
        ]),
    }
    const remove: CanvasMenuItem = {
      label: groupOnly ? '删除分组及内容' : '删除',
      icon: <Trash2 size={16} />,
      shortcut: '⌫',
      disabled: locked,
      danger: true,
      action: () =>
        act([{ type: 'remove_nodes', node_ids: ids, group_mode: 'subtree' }]),
    }
    const ungroup: CanvasMenuItem = {
      label: '解组',
      icon: <FolderMinus size={16} />,
      shortcut: '⇧⌘G',
      disabled: locked,
      action: () =>
        act(groups.map((id) => ({ type: 'ungroup_nodes' as const, node_id: id }))),
    }
    if (groupOnly) {
      items.push(
        {
          label: '重命名分组',
          icon: <Pencil size={16} />,
          disabled: disabled || !target.capabilities.actions.includes('rename'),
          action: () => renameNode(target.id),
        },
        ungroup,
        copy,
        {
          label: '适应到分组',
          icon: <Scan size={16} />,
          action: () => fitCanvas(240, [target.id]),
        },
        'separator',
        remove,
      )
      return items
    }
    if (!one) {
      if (canGroupIDs(ids))
        items.push({
          label: '打组',
          icon: <FolderPlus size={16} />,
          shortcut: '⌘G',
          disabled: locked,
          action: () => act([{ type: 'group_nodes', node_ids: ids }]),
        })
      if (groups.length) items.push(ungroup)
      items.push(copy, {
        label: '存入资产库…',
        icon: <Library size={16} />,
        disabled,
        action: () => onSaveNodesAsk(ids),
      }, 'separator', remove)
      return items
    }
    const kind = nodeKind(target.metadata.type_key)
    items.push({
      label: '重命名',
      icon: <Pencil size={16} />,
      disabled: disabled || !target.capabilities.actions.includes('rename'),
      action: () => renameNode(target.id),
    })
    if (target.metadata.type_key === 'core.text')
      items.push({
        label: '编辑文字',
        icon: <FileText size={16} />,
        shortcut: 'Enter',
        disabled: disabled || !target.capabilities.actions.includes('edit'),
        action: () => onEdit(target.id),
      })
    else if (
      ['core.link', 'internal.document'].includes(target.metadata.type_key)
    )
      items.push({
        label: target.metadata.type_key === 'core.link' ? '编辑链接' : '最大化文档',
        icon: <FileText size={16} />,
        disabled: disabled || !target.capabilities.actions.includes('edit'),
        action: () => onEdit(target.id),
      })
    // 媒体说明的编辑入口（PRD 没列，但它是改说明的唯一途径，保留）。
    if (isMediaKind(kind) && target.content && target.capabilities.actions.includes('edit'))
      items.push({
        label: '编辑说明',
        icon: <FileText size={16} />,
        disabled,
        action: () => onEdit(target.id),
      })
    if (isMediaKind(kind) && target.content) {
      if (target.capabilities.actions.includes('maximize'))
        items.push({
          label: '全屏查看',
          icon: <Maximize2 size={16} />,
          shortcut: 'Space',
          action: () => onPreview(target.id, cardPreviews[target.id]?.id),
        })
      if (target.capabilities.actions.includes('download'))
        items.push({
          label: '下载',
          icon: <Download size={16} />,
          action: () => onDownload(target.id),
        })
    }
    if (isMediaKind(kind) && target.capabilities.actions.includes('replace'))
      items.push({
        label: target.content ? '替换媒体' : '上传媒体',
        icon: <Upload size={16} />,
        disabled,
        action: () => onUploadToNode(target.id, kind),
      })
    if (connectableTypes.includes(target.metadata.type_key))
      items.push({
        label: '从资产库添加参考',
        icon: <Library size={16} />,
        disabled,
        action: () => onPickReferences(target.id),
      })
    items.push(copy)
    if (target.content && target.capabilities.actions.includes('save_to_library'))
      items.push({
        label: '存入资产库…',
        icon: <Library size={16} />,
        disabled,
        action: () => onSaveToLibrary(target.id),
      })
    items.push('separator', remove)
    return items
  }
  const contextItems: CanvasMenuItem[] = []
  if (contextMenu) {
    if (contextMenu.edgeID) {
      const edgeID = contextMenu.edgeID
      contextItems.push({
        label: '删除连线',
        icon: <Scissors size={16} />,
        disabled: disabled || !persistedEdges.has(edgeID),
        danger: true,
        action: () => {
          act([{ type: 'disconnect_reference', edge_id: edgeID }])
          setSelectedEdge(null)
        },
      })
    } else if (contextMenu.nodeIDs.length) {
      contextItems.push(...nodeMenuItems(contextMenu.nodeIDs))
    } else {
      const at = flow.screenToFlowPosition({
        x: contextMenu.x,
        y: contextMenu.y,
      })
      // 空白处（PRD CV-83）：添加 / 上传 / 资产库，撤销、重做，全选，适应画布。
      contextItems.push(
        ...addMenuItems({ screen: contextMenu, world: at }),
        'separator',
        {
          label: '撤销',
          icon: <Undo2 size={16} />,
          shortcut: '⌘Z',
          disabled: disabled || !canUndo,
          action: onUndo,
        },
        {
          label: '重做',
          icon: <Redo2 size={16} />,
          shortcut: '⇧⌘Z',
          disabled: disabled || !canRedo,
          action: onRedo,
        },
        'separator',
        {
          label: '全选',
          icon: <SquareDashedMousePointer size={16} />,
          shortcut: '⌘A',
          action: selectAll,
        },
        {
          label: '适应画布',
          icon: <Scan size={16} />,
          shortcut: '⇧1',
          action: () => fitCanvas(),
        },
      )
    }
  }
  return (
    <div
      className="cc-flow"
      ref={element}
      // 点画布空白时焦点落到画布上，⇧1、⌘± 等画布快捷键才收得到。
      tabIndex={-1}
      onPointerDownCapture={(e) => {
        pointerDown.current = { x: e.clientX, y: e.clientY }
      }}
      onPointerMove={(e) => {
        const a = selectionStart.current
        if (!a) return
        const b = flow.screenToFlowPosition({ x: e.clientX, y: e.clientY })
        selectionBox.current = {
          x: Math.min(a.x, b.x),
          y: Math.min(a.y, b.y),
          width: Math.abs(a.x - b.x),
          height: Math.abs(a.y - b.y),
        }
      }}
      onContextMenu={(event) => {
        const hit = event.target instanceof Element ? event.target : null
        if (
          !hit ||
          hit.closest(
            'input,textarea,select,video,audio,[contenteditable="true"],[role="dialog"],[role="menu"]',
          )
        )
          return
        const nodeID = hit.closest<HTMLElement>('.react-flow__node')?.dataset.id
        const edgeID = hit.closest<HTMLElement>('.react-flow__edge')?.dataset.id
        if (
          !nodeID &&
          !edgeID &&
          !hit.closest('.react-flow__pane,.react-flow__nodesselection')
        )
          return
        event.preventDefault()
        event.stopPropagation()
        setAddMenu(null)
        const ids = nodeID
          ? selection.includes(nodeID)
            ? selection
            : [nodeID]
          : hit.closest('.react-flow__nodesselection')
            ? selection
            : []
        onSelection(ids)
        if (nodeID) onSelect(nodeID)
        setSelectedEdge(edgeID ?? null)
        setContextMenu({
          x: event.clientX,
          y: event.clientY,
          nodeIDs: ids,
          edgeID,
        })
      }}
      onKeyDown={(e) => {
        const target = e.target as HTMLElement
        // 按钮本身不用 Esc：焦点停在端口等按钮上时 Esc 仍逐层生效（PRD CV-22）。
        if (
          target.closest('input,textarea,select') ||
          (target.closest('button') && e.key !== 'Escape') ||
          e.nativeEvent.isComposing
        )
          return
        const command = e.metaKey || e.ctrlKey,
          key = e.key.toLowerCase()
        if (command && key === 'g') {
          e.preventDefault()
          if (e.shiftKey && singleGroupSelected && active)
            act([{ type: 'ungroup_nodes', node_id: active.id }])
          else if (!e.shiftKey && canGroup)
            act([{ type: 'group_nodes', node_ids: selection }])
          return
        }
        if (e.shiftKey && e.code === 'Digit1') {
          e.preventDefault()
          fitCanvas()
          return
        }
        // ⌘+ / ⌘- 以屏幕中心按 1.2 倍缩放，⌘0 回到 100%（PRD CV-02）；拦下浏览器页面缩放。
        if (command && !e.altKey && ['=', '+', '-', '0'].includes(e.key)) {
          e.preventDefault()
          if (e.key === '-') void flow.zoomOut()
          else if (e.key === '0') void flow.zoomTo(1)
          else void flow.zoomIn()
          return
        }
        // 单选一个有内容的媒体节点时 Space 打开全屏，与双击、工具条入口一致。
        if (e.key === ' ' && !command && !e.repeat) {
          const only = selection.length === 1 ? canvas.nodes.find((n) => n.id === selection[0]) : undefined
          if (
            only?.content &&
            isMediaKind(nodeKind(only.metadata.type_key)) &&
            only.capabilities.actions.includes('maximize')
          ) {
            e.preventDefault()
            onPreview(only.id, cardPreviews[only.id]?.id)
          }
          return
        }
        // ⌘A 全选：选中所有顶层节点与组，组内子节点随组计入（PRD CV-22）。
        if (command && key === 'a') {
          e.preventDefault()
          selectAll()
          return
        }
        if (command && key === 'd') {
          e.preventDefault()
          act([
            {
              type: 'duplicate_selection',
              node_ids: selection,
              dx: COPY_OFFSET,
              dy: COPY_OFFSET,
            },
          ])
          return
        }
        if (e.key === 'Delete' || e.key === 'Backspace') {
          e.preventDefault()
          removeSelection()
          return
        }
        // v6：方向键每次 1px，按住 Shift 10px。
        const d = e.shiftKey ? 10 : 1,
          delta: Record<string, [number, number]> = {
            ArrowLeft: [-d, 0],
            ArrowRight: [d, 0],
            ArrowUp: [0, -d],
            ArrowDown: [0, d],
          },
          step = delta[e.key]
        if (active && step && !moveDisabled) {
          e.preventDefault()
          e.stopPropagation()
          moveSelection(...step)
        } else if (e.key === 'Enter' && selected) {
          e.preventDefault()
          if (active?.data.item.metadata.type_key === 'core.group')
            renameNode(selected)
          else onEdit(selected)
        } else if (e.key === 'Escape') {
          // 逐层：先关浮层，再清空选中（PRD CV-22）。
          if (addMenu || contextMenu) {
            setAddMenu(null)
            setContextMenu(null)
          } else {
            setSelectedEdge(null)
            onSelection([])
          }
        }
      }}
      onDragOver={(e) => {
        e.preventDefault()
        e.dataTransfer.dropEffect = disabled ? 'none' : 'copy'
      }}
      onDrop={(e) => {
        e.preventDefault()
        if (disabled) return
        const point = flow.screenToFlowPosition({
          x: e.clientX,
          y: e.clientY,
        })
        // 落在空的同类媒体节点上就填进去，不另建节点。
        const hitID = (e.target as Element)
          .closest?.('.react-flow__node')
          ?.getAttribute('data-id')
        const hit = canvas.nodes.find((n) => n.id === hitID)
        const fillable = (kind: string) =>
          !!hit &&
          !hit.content &&
          hit.metadata.type_key === `core.${kind}` &&
          isMediaKind(kind) &&
          hit.capabilities.actions.includes('replace')
        let dropped: Asset[] = []
        try {
          const json = e.dataTransfer.getData('application/creative-asset+json')
          if (json) dropped = JSON.parse(json) as Asset[]
        } catch {
          dropped = []
        }
        if (!dropped.length) {
          const raw = e.dataTransfer.getData('application/creative-asset')
          dropped = (raw ? raw.split(',').filter(Boolean) : [])
            .map((id) => assets.find((a) => a.id === id))
            .filter((a): a is Asset => !!a)
        }
        if (dropped.length) {
          if (dropped.length === 1 && hit && fillable(dropped[0]!.kind))
            onFillNode(hit.id, dropped[0]!)
          else if (dropped.length === 1)
            onDropAsset(dropped[0]!, point.x, point.y)
          else onDropAssets(dropped, point.x, point.y)
          return
        }
        const files = Array.from(e.dataTransfer.files ?? [])
        const fileKind = files[0]?.type.split('/')[0] ?? ''
        if (files.length === 1 && hit && fillable(fileKind))
          onUploadFileToNode(hit.id, files[0]!)
        else if (files.length) onDropFiles(files, point.x, point.y)
      }}
    >
      <ReactFlow<FlowNode>
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        connectionLineComponent={ConnectionPreview}
        connectionRadius={0}
        nodesConnectable={!disabled}
        nodesDraggable={!moveDisabled && !panHeld && !panMode}
        disableKeyboardA11y
        deleteKeyCode={null}
        multiSelectionKeyCode={['Shift', 'Meta', 'Control']}
        selectionOnDrag={!panMode && !panHeld}
        selectionMode={SelectionMode.Partial}
        onNodesChange={changeNodes}
        onNodeClick={() => setSelectedEdge(null)}
        onNodeDoubleClick={(_, n) => {
          const item = n.data.item
          if (item.metadata.type_key === 'core.group') return
          // v6：双击有内容的媒体全屏查看，文字就地编辑。
          if (
            isMediaKind(nodeKind(item.metadata.type_key)) &&
            item.content &&
            item.capabilities.actions.includes('maximize')
          )
            onPreview(n.id, cardPreviews[n.id]?.id)
          else if (isMediaKind(nodeKind(item.metadata.type_key)) && !item.content)
            onFocusPrompt(n.id)
          else onEdit(n.id)
        }}
        onSelectionStart={(e) => {
          // React Flow 在越过拖动阈值后才触发，起点取按下时的位置。
          selectionStart.current = flow.screenToFlowPosition(
            pointerDown.current ?? { x: e.clientX, y: e.clientY },
          )
          selectionBox.current = null
        }}
        onSelectionEnd={() => {
          // 框选时组要完整框住才选中；已选中组时不再重复选中组内节点。
          const rect = selectionBox.current
          selectionStart.current = null
          selectionBox.current = null
          if (!rect) return
          const inside = (id: string) => {
            const n = flow.getInternalNode(id)
            if (!n) return false
            const p = n.internals.positionAbsolute
            return (
              p.x >= rect.x &&
              p.y >= rect.y &&
              p.x + (n.measured.width ?? 0) <= rect.x + rect.width &&
              p.y + (n.measured.height ?? 0) <= rect.y + rect.height
            )
          }
          const kept = selectionRef.current.filter((id) => {
            const n = callbacks.current.canvasNodes.find((v) => v.id === id)
            return n?.metadata.type_key !== 'core.group' || inside(id)
          })
          const roots = selectionRoots(callbacks.current.canvasNodes, kept)
          if (roots.length !== selectionRef.current.length) {
            selectionRef.current = roots
            callbacks.current.onSelection(roots)
          }
        }}
        onPaneClick={() => {
          onSelect(null)
          onSelection([])
          setAddMenu(null)
          setSelectedEdge(null)
        }}
        onDoubleClick={(e) => {
          if (
            disabled ||
            !(e.target as HTMLElement).classList.contains('react-flow__pane')
          )
            return
          const screen = { x: e.clientX, y: e.clientY }
          setAddMenu({ screen, world: flow.screenToFlowPosition(screen) })
        }}
        onNodeDragStart={(_, n, ns) => {
          onDraggingChange(true)
          const roots = selectionRoots(
            canvas.nodes,
            ns.length ? ns.map((v) => v.id) : [n.id],
          )
          dragging.current = new Set(roots)
          dragBases.current = new Map(
            canvas.nodes
              .filter((v) => roots.includes(v.id))
              .map((v) => [v.id, v]),
          )
        }}
        onNodeDrag={(e) => markLibraryDrop(libraryDropZone(...pointOf(e)))}
        onNodeDragStop={(e, n, ns) => {
          onDraggingChange(false)
          const list = ns.length ? ns : [n]
          const zone = libraryDropZone(...pointOf(e))
          markLibraryDrop(null)
          if (zone) {
            // 拖进资产库是存入，不是移动：节点回到原处。
            const bases = dragBases.current
            dragging.current.clear()
            flow.setNodes((all) =>
              all.map((v) => {
                const base = bases.get(v.id)
                return base
                  ? {
                      ...v,
                      position: { x: base.metadata.x, y: base.metadata.y },
                    }
                  : v
              }),
            )
            onSaveNodes(
              [...bases.keys()].filter(
                (id) => bases.get(id)?.metadata.type_key !== 'core.group',
              ),
              zone.groupID,
            )
            return
          }
          const moves = list
            .filter((v) => dragging.current.has(v.id))
            .map((v) => ({
              node: dragBases.current.get(v.id) ?? v.data.item,
              x: v.position.x,
              y: v.position.y,
            }))
          dragging.current.clear()
          if (
            moves.length === 1 &&
            !moves[0].node.parent_id &&
            moves[0].node.metadata.type_key !== 'core.group'
          )
            onMove(moves[0].node, moves[0].x, moves[0].y)
          else if (moves.length) onMoveSelection(moves)
        }}
        onConnect={(connection) => {
          if (
            connection.source &&
            connection.target &&
            connection.sourceHandle === 'output' &&
            connection.targetHandle === 'reference'
          )
            connect(connection.source, connection.target)
        }}
        connectOnClick={false}
        onConnectEnd={(event, state) => {
          if (state.isValid || !state.fromNode || disabled) return
          const pointer =
            'changedTouches' in event ? event.changedTouches[0] : event
          if (!pointer) return
          const screen = { x: pointer.clientX, y: pointer.clientY }
          const target = connectionTarget(flow, state.fromNode.id, screen)
          if (target) {
            const output = state.fromHandle?.type === 'source'
            connect(
              output ? state.fromNode.id : target.id,
              output ? target.id : state.fromNode.id,
            )
            return
          }
          const hit = document.elementFromPoint(screen.x, screen.y)
          // 松手在自己或组上：connectionTarget 不会选中它们，这里说明为什么没连上。
          const dropped = hit
            ?.closest<HTMLElement>('.react-flow__node')
            ?.getAttribute('data-id')
          if (dropped) {
            const problem =
              dropped === state.fromNode.id
                ? '不能把节点连到它自己'
                : canvas.nodes.find((n) => n.id === dropped)?.metadata
                      .type_key === 'core.group'
                  ? '组不能作为连线的端点，请连到组内的节点'
                  : ''
            if (problem) {
              onNotice(problem)
              return
            }
          }
          if (
            !hit?.closest('.react-flow__pane') ||
            hit.closest('.react-flow__node')
          )
            return
          // 虚线留在松手点，直到菜单选定（变实线）或取消（消失）。
          setAddMenu({
            screen,
            world: flow.screenToFlowPosition(screen),
            link: {
              id: state.fromNode.id,
              side: state.fromHandle?.type === 'target' ? 'input' : 'output',
            },
            dropped: true,
          })
        }}
        onEdgeClick={(_, edge) => {
          onSelect(null)
          onSelection([])
          setAddMenu(null)
          setSelectedEdge(persistedEdges.has(edge.id) ? edge.id : null)
        }}
        onEdgeDoubleClick={(_, edge) => {
          if (!persistedEdges.has(edge.id)) return
          act([{ type: 'disconnect_reference', edge_id: edge.id }])
          if (!disabled) onNotice('已断开连线，⌘Z 可撤销')
        }}
        panOnScroll
        panOnScrollMode={PanOnScrollMode.Free}
        zoomOnScroll={false}
        zoomActivationKeyCode={['Control', 'Meta']}
        panOnDrag={panMode ? true : [1]}
        panActivationKeyCode={selection.length ? null : 'Space'}
        minZoom={zoomLimits.min}
        maxZoom={zoomLimits.max}
        zoomOnDoubleClick={false}
        onInit={(instance) => {
          try {
            const raw = localStorage.getItem(
              `creative-viewport:${account}:${canvas.id}`,
            )
            const v: unknown = raw ? JSON.parse(raw) : null
            if (validViewport(v)) {
              void instance.setViewport(v)
              return
            }
          } catch {
            /* Viewport preferences are optional. */
          }
          // 没有或无效的本机视口：适应画布（PRD CV-04）。
          fitCanvas(0)
        }}
        onMoveStart={() => {
          setAddMenu(null)
          setContextMenu(null)
        }}
        onMoveEnd={(_, v) => {
          try {
            localStorage.setItem(
              `creative-viewport:${account}:${canvas.id}`,
              JSON.stringify(v),
            )
          } catch {
            /* Draft storage is separate and mandatory. */
          }
        }}
      >
        <Background gap={24} size={1} color="rgba(255, 255, 255, 0.075)" />
        {addMenu?.dropped && addMenu.link && (
          <DraftLink link={addMenu.link} to={addMenu.world} />
        )}
        {minimap && (
          <MiniMap
            pannable
            zoomable
            nodeColor="#3a3a3e"
            maskColor="rgba(11, 11, 12, 0.6)"
          />
        )}
      </ReactFlow>
      {/* 文字编辑期间隐藏工具条，结束编辑后恢复（PRD CV-60）。按选区显示 CV-70 规定的按钮。 */}
      {active && !inlineContent && (
        <div
          className="cc-selection-tools cc-surface"
          role="toolbar"
          aria-label="节点操作"
          style={{ left: toolbarX, top: toolbarY }}
        >
          {(() => {
            const one = selection.length === 1 ? active.data.item : undefined
            const type = one?.metadata.type_key
            const kind = one ? nodeKind(one.metadata.type_key) : ''
            const media = !!one && isMediaKind(kind)
            const sep = (key: string) => <span key={key} className="cc-tool-sep" />
            const tool = (label: string, icon: ReactNode, run: () => void, off = disabled) => (
              <button key={label} className="cc-tool" disabled={off} onClick={run}>
                {icon}
                {label}
              </button>
            )
            const icon = (label: string, node: ReactNode, run: () => void, title = label, off = false) => (
              <button key={label} className="cc-icon-button" aria-label={label} title={title} disabled={off} onClick={run}>
                {node}
              </button>
            )
            const copy = (label: string) =>
              icon(label, <Copy size={16} />, () =>
                act([{ type: 'duplicate_selection', node_ids: selection, dx: COPY_OFFSET, dy: COPY_OFFSET }]),
              `${label} · ⌘D`, disabled || selectionReadOnly)
            // 「更多」在按钮下方打开与右键相同的菜单（PRD CV-74）。
            const more = icon('更多', <Ellipsis size={16} />, () => {
              const b = document.querySelector<HTMLElement>('.cc-selection-tools [aria-label="更多"]')?.getBoundingClientRect()
              setAddMenu(null)
              setContextMenu({ x: b ? b.left : toolbarX, y: b ? b.bottom + 6 : toolbarY + 40, nodeIDs: selection })
            })
            if (!one) {
              const refs = selectionRoots(canvas.nodes, selection)
                .map((id) => canvas.nodes.find((n) => n.id === id))
                .filter((n): n is CanvasNode => !!n && connectableTypes.includes(n.metadata.type_key))
              const groups = selection.filter((id) => canvas.nodes.find((n) => n.id === id)?.metadata.type_key === 'core.group')
              return [
                <span key="count">已选 {selection.length} 项</span>,
                sep('s0'),
                canGroup && tool('打组', <FolderPlus size={16} />, () => act([{ type: 'group_nodes', node_ids: selection }]), disabled || selectionReadOnly),
                groups.length > 0 && tool('解组', <FolderMinus size={16} />, () => act(groups.map((id) => ({ type: 'ungroup_nodes' as const, node_id: id }))), disabled || selectionReadOnly),
                refs.length > 1 && tool('合并为参考生成', <Sparkles size={16} />, () => derive(refs.map((n) => n.id), 'image')),
                sep('s1'),
                copy('复制'),
                icon('删除', <Trash2 size={16} />, removeSelection, '删除 · ⌫', disabled || selectionReadOnly),
              ]
            }
            if (type === 'core.group')
              return [
                tool('重命名', <Pencil size={16} />, () => renameNode(one.id), disabled || !one.capabilities.actions.includes('rename')),
                tool('解组', <FolderMinus size={16} />, () => act([{ type: 'ungroup_nodes', node_id: one.id }]), disabled || selectionReadOnly),
                sep('s0'),
                copy('复制整组'),
                more,
              ]
            if (type === 'core.text')
              return [
                one.capabilities.actions.includes('edit') && tool('编辑', <Pencil size={16} />, () => onEdit(one.id)),
                contentText(one.content?.payload) && tool('生成图片', <ImageIcon size={16} />, () => derive([one.id], 'image')),
                sep('s0'),
                more,
              ]
            if (media && !one.content)
              return [
                one.capabilities.actions.includes('replace') &&
                  tool('上传', <Upload size={16} />, () => onUploadToNode(one.id, kind as MediaKind)),
                sep('s0'),
                more,
              ]
            if (media && one.content)
              return [
                type === 'core.image' && tool('生成视频', <Film size={16} />, () => derive([one.id], 'video')),
                type === 'core.image' && sep('s0'),
                one.capabilities.actions.includes('maximize') &&
                  icon('全屏查看', <Maximize2 size={16} />, () => onPreview(one.id, cardPreviews[one.id]?.id), '全屏查看 · Space'),
                one.capabilities.actions.includes('save_to_library') &&
                  icon('存入资产库', <Library size={16} />, () => onSaveToLibrary(one.id), '存入资产库', disabled),
                one.capabilities.actions.includes('download') &&
                  icon('下载', <Download size={16} />, () => onDownload(one.id), '下载原件'),
                more,
              ]
            // 链接、文档等：主要动作 + 更多；链接/音频的完整工具条由 V6-009/010 交付。
            return [
              type === 'internal.document' && icon('最大化文档', <Maximize2 size={16} />, () => onEdit(one.id)),
              type === 'core.link' && one.capabilities.actions.includes('edit') && tool('编辑', <Pencil size={16} />, () => onEdit(one.id)),
              more,
            ]
          })()}
        </div>
      )}
      {contextMenu && (
        <CanvasContextMenu
          key={`${contextMenu.x}:${contextMenu.y}:${contextMenu.nodeIDs.join(',')}:${contextMenu.edgeID ?? ''}`}
          x={contextMenu.x}
          y={contextMenu.y}
          label={
            contextMenu.edgeID
              ? '连线右键菜单'
              : contextMenu.nodeIDs.length
                ? '节点右键菜单'
                : '画布右键菜单'
          }
          items={contextItems}
          onClose={() => setContextMenu(null)}
        />
      )}
      {addMenu && (
        <CanvasContextMenu
          key={`${addMenu.screen.x}:${addMenu.screen.y}:${addMenu.link?.id ?? ''}`}
          x={addMenu.screen.x}
          y={addMenu.screen.y}
          label={addMenu.link ? '创建并连接节点' : '添加节点'}
          items={addMenuItems(addMenu)}
          onClose={() => setAddMenu(null)}
        />
      )}
      <div className="cc-canvas-controls" role="toolbar" aria-label="画布视图">
        <button
          aria-label="撤销"
          title="撤销 · ⌘Z"
          disabled={!canUndo || disabled}
          onClick={onUndo}
        >
          <Undo2 size={17} />
        </button>
        <button
          aria-label="重做"
          title="重做 · ⇧⌘Z"
          disabled={!canRedo || disabled}
          onClick={onRedo}
        >
          <Redo2 size={17} />
        </button>
        <span />
        <button aria-label="缩小" onClick={() => void flow.zoomOut()}>
          <Minus size={17} />
        </button>
        <button
          className="cc-zoom-level"
          aria-label="恢复百分之百缩放"
          onClick={() => void flow.zoomTo(1)}
        >
          {Math.round(viewport.zoom * 100)}%
        </button>
        <button aria-label="放大" onClick={() => void flow.zoomIn()}>
          <Plus size={17} />
        </button>
        <button
          aria-label="适应全部节点"
          onClick={() => fitCanvas()}
        >
          <Scan size={17} />
        </button>
        <span />
        <button
          aria-label="切换小地图"
          aria-pressed={minimap}
          onClick={() => setMinimap(!minimap)}
        >
          <MapIcon size={17} />
        </button>
      </div>
    </div>
  )
}
