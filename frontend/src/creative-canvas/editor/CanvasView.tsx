import {
  memo,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
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
  getBezierPath,
  MarkerType,
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
  Map as MapIcon,
  Folder,
  FolderPlus,
  FolderMinus,
  Copy,
  Trash2,
  Scissors,
  Undo2,
  Redo2,
  X,
  Image as ImageIcon,
  Film,
  AudioLines,
  Download,
  RefreshCw,
  Library,
  Upload,
} from 'lucide-react'
import type { Asset, Canvas, CanvasNode, GraphAction } from './api.ts'
import { MediaFigure } from './MediaView.tsx'
import { isMediaKind, type MediaKind } from './media.ts'
import { parentFirst, selectionRoots, worldPoint } from './graph.ts'
import { contentText } from './content.ts'
import { attachMagneticPorts } from './magneticPorts.ts'
import CanvasContextMenu, { type CanvasMenuItem } from './CanvasContextMenu.tsx'
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
  },
  'content'
>
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
function KindIcon({
  typeKey,
  size = 14,
}: {
  typeKey: string
  size?: number
}) {
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
const Card = memo(function Card({ data, selected }: NodeProps<FlowNode>) {
  const n = data.item,
    p = n.content?.payload,
    kind = nodeKind(n.metadata.type_key),
    media = isMediaKind(kind),
    group = n.metadata.type_key === 'core.group',
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
      <NodeResizer
        isVisible={
          !!selected &&
          !data.disabled &&
          n.capabilities.actions.includes('resize')
        }
        minWidth={160}
        minHeight={100}
        onResizeEnd={(_, size) =>
          data.resize(n.id, size.width, size.height, size.x, size.y)
        }
      />
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
      </header>
      {!group && media && (
        <div className="cc-node-content cc-node-media">
          {!known || n.status.content_state === 'unavailable' ? (
            <p>
              {!known ? '此节点类型当前只读' : '当前无权展示此内容'}
            </p>
          ) : n.content ? (
            <MediaFigure
              content={n.content}
              kind={kind}
              title={n.metadata.title || untitled(n.metadata.type_key)}
              controls={kind !== 'image'}
              hoverPlay={kind === 'video'}
            />
          ) : (
            <div
              className="cc-media-empty"
              role="img"
              aria-label={`空${kindLabel[kind]}节点`}
            >
              <KindIcon typeKey={n.metadata.type_key} size={44} />
            </div>
          )}
        </div>
      )}
      {!group && !media && (
        <div className="cc-node-content">
          {data.inlineContent ? (
            data.inlineContent.draft ? (
              <InlineNodeText {...data.inlineContent} draft={data.inlineContent.draft} />
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
          {!data.inlineContent && n.content?.truncated && <small>正文预览 · 编辑时载入全文</small>}
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
    x: p.x + (side === 'output' ? (node.measured.width ?? 280) : 0),
    y: p.y + (node.measured.height ?? 180) / 2,
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
        Math.max(p.x - point.x, 0, point.x - p.x - (n.measured.width ?? 280)),
        Math.max(p.y - point.y, 0, point.y - p.y - (n.measured.height ?? 180)),
      ) * zoom
    if (d < distance) {
      best = n
      distance = d
    }
  }
  return best
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
        className="react-flow__connection-path cc-connection-preview"
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
  disconnect: () => void
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
      {props.selected && <path d={path} className="cc-reference-flow" />}
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
              props.data?.disconnect()
            }}
          >
            <Scissors size={16} aria-hidden="true" />
          </button>
        </EdgeLabelRenderer>
      )}
    </>
  )
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
  inlineContent,
  onAdd,
  panMode,
  onMove,
  onMoveSelection,
  onDropAsset,
  onDropAssets,
  onDropFiles,
  onUploadToNode,
  onDownload,
  onSaveToLibrary,
  onPreview,
  onActions,
  onUndo,
  onRedo,
  canUndo,
  canRedo,
  disabled,
  moveDisabled,
  assets,
  account,
}: {
  canvas: Canvas
  selected: string | null
  selection: string[]
  onSelection: (ids: string[]) => void
  onSelect: (id: string | null) => void
  onEdit: (id: string) => void
  inlineContent?: InlineContentEditing
  onAdd: (x: number, y: number) => void
  panMode: boolean
  onMove: (node: CanvasNode, x: number, y: number) => void
  onMoveSelection: (moves: { node: CanvasNode; x: number; y: number }[]) => void
  onDropAsset: (asset: Asset, x: number, y: number) => void
  onDropAssets: (assets: Asset[], x: number, y: number) => void
  onDropFiles: (files: File[], x: number, y: number) => void
  onUploadToNode: (id: string, kind: MediaKind) => void
  onDownload: (id: string) => void
  onSaveToLibrary: (id: string) => void
  onPreview: (id: string) => void
  onActions: (actions: GraphAction[]) => void
  onUndo: () => void
  onRedo: () => void
  canUndo: boolean
  canRedo: boolean
  disabled: boolean
  moveDisabled: boolean
  assets: Asset[]
  account: string
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
  })
  callbacks.current = {
    onSelect,
    onSelection,
    onActions,
    canvasNodes: canvas.nodes,
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
    panHeld = useKeyPress('Space')
  const [size, setSize] = useState({ width: 0, height: 0 }),
    [minimap, setMinimap] = useState(false),
    [nodes, setNodes] = useState<FlowNode[]>([])
  const [portMenu, setPortMenu] = useState<{
    id: string
    side: 'input' | 'output'
    x: number
    y: number
    screen: { x: number; y: number }
  } | null>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const [selectedEdge, setSelectedEdge] = useState<string | null>(null)
  const [contextMenu, setContextMenu] = useState<{
    x: number; y: number; nodeIDs: string[]; edgeID?: string
  } | null>(null)
  const [renamingNode, setRenamingNode] = useState<string | null>(null)
  const renameNode = useCallback((id: string | null) => {
    if (id && disabled) return
    if (id) callbacks.current.onSelection([id])
    setRenamingNode(id)
  }, [disabled])
  const saveTitle = useCallback((id: string, title: string) => {
    const current = callbacks.current.canvasNodes.find((n) => n.id === id)
    if (current)
      callbacks.current.onActions([{
        type: 'update_metadata', node_id: id, title, intent: current.metadata.intent,
      }])
  }, [])

  const element = useRef<HTMLDivElement>(null),
    dragging = useRef(new Set<string>()),
    dragBases = useRef(new Map<string, CanvasNode>())
  const selectNode = useCallback((id: string) => {
    setSelectedEdge(null)
    callbacks.current.onSelect(id)
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
      setPortMenu({
        id,
        side,
        screen,
        x:
          p.x + (side === 'output' ? (node.measured.width ?? 280) + 200 : -200),
        y: p.y + (node.measured.height ?? 180) / 2,
      })
    },
    [flow],
  )
  const magnetBlocked = useRef(false)
  magnetBlocked.current = disabled || panMode || panHeld || !!portMenu
  useEffect(() => {
    if (!element.current) return
    return attachMagneticPorts(
      element.current,
      () => flow.getViewport().zoom,
      () => magnetBlocked.current || dragging.current.size > 0,
    )
  }, [flow])
  useLayoutEffect(() => {
    const menu = menuRef.current,
      surface = element.current
    if (!portMenu || !menu || !surface) return
    // Menu coordinates are screen coordinates, independent of new-node placement.
    const place = () => {
      const bounds = surface.getBoundingClientRect()
      menu.style.maxHeight = `${Math.max(0, bounds.height - 24)}px`
      menu.style.left = `${Math.max(12, Math.min(bounds.width - menu.offsetWidth - 12, portMenu.screen.x - bounds.left))}px`
      menu.style.top = `${Math.max(12, Math.min(bounds.height - menu.offsetHeight - 12, portMenu.screen.y - bounds.top))}px`
    }
    place()
    const observer = new ResizeObserver(place)
    observer.observe(surface)
    observer.observe(menu)
    return () => observer.disconnect()
  }, [portMenu])
  const resize = useCallback(
    (id: string, width: number, height: number, x: number, y: number) => {
      const source = callbacks.current.canvasNodes.find((n) => n.id === id)
      if (!source) return
      resizePreviews.current.set(id, { item: source, x, y, width, height })
      const actions: GraphAction[] = [
        { type: 'move_node', node_id: id, x, y },
        { type: 'resize_node', node_id: id, width, height },
      ]
      if (source.metadata.type_key === 'core.group') {
        for (const child of callbacks.current.canvasNodes.filter(
          (n) => n.parent_id === id,
        )) {
          const px = child.metadata.x + source.metadata.x - x
          const py = child.metadata.y + source.metadata.y - y
          resizePreviews.current.set(child.id, {
            item: child,
            x: px,
            y: py,
            width: child.metadata.width,
            height: child.metadata.height,
          })
          actions.push({ type: 'move_node', node_id: child.id, x: px, y: py })
        }
      }
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
          draggable =
            !moveDisabled &&
            !panMode &&
            !panHeld &&
            (!isSelected || roots.has(n.id)) &&
            n.capabilities.actions.includes('move')
        if (
          old &&
          old.data.item === n &&
          old.selected === isSelected &&
          old.draggable === draggable &&
          old.data.disabled === disabled &&
          old.data.renaming === (renamingNode === n.id) &&
          old.data.inlineContent === (inlineContent?.nodeID === n.id ? inlineContent : undefined) &&
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
            (old?.data.item === n ? old.height : undefined) ??
            n.metadata.height,
          parentId: n.parent_id ?? undefined,
          style: {
            width: n.metadata.width,
            height: n.metadata.height,
          },
          data: {
            item: n,
            renaming: renamingNode === n.id,
            rename: renameNode,
            saveTitle,
            inlineContent: inlineContent?.nodeID === n.id ? inlineContent : undefined,
            select: selectNode,
            port: openPort,
            resize,
            disabled,
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
  ])
  useEffect(() => {
    if (!portMenu) return
    const previous = document.activeElement
    menuRef.current?.querySelector<HTMLButtonElement>('button')?.focus()
    const outside = (event: PointerEvent) => {
      if (
        event.target instanceof window.Node &&
        !menuRef.current?.contains(event.target)
      )
        setPortMenu(null)
    }
    document.addEventListener('pointerdown', outside)
    return () => {
      document.removeEventListener('pointerdown', outside)
      if (previous instanceof HTMLElement && previous.isConnected)
        previous.focus()
    }
  }, [portMenu])
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
  const singleGroupSelected =
    selection.length === 1 && active?.data.item.metadata.type_key === 'core.group'
  const toolbarX = Math.max(
      12,
      Math.min(
        size.width - 340,
        viewport.x +
          (point.x + (active?.width ?? 280) / 2) * viewport.zoom -
          160,
      ),
    ),
    toolbarY = Math.max(
      12,
      Math.min(size.height - 100, viewport.y + (point.y - 30) * viewport.zoom - 54),
    )
  const expanded = new Set(selection)
  for (let changed = true; changed; ) {
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
  const edges: ReferenceFlowEdge[] = canvas.edges.map(
    (e) => ({
      id: e.id,
      selectable: persistedEdges.has(e.id),
      focusable: persistedEdges.has(e.id),
      source: e.source_node_id,
      target: e.target_node_id,
      sourceHandle: e.source_port,
      targetHandle: e.target_port,
      type: 'reference',
      data: {
        showCut:
          selectedEdge === e.id && persistedEdges.has(e.id) && !disabled,
        disconnect: () => {
          act([{ type: 'disconnect_reference', edge_id: e.id }])
          setSelectedEdge(null)
        },
      },
      selected:
        selectedEdge === e.id ||
        expanded.has(e.source_node_id) ||
        expanded.has(e.target_node_id),
      markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14 },
      style: {
        pointerEvents: persistedEdges.has(e.id) ? 'auto' : 'none',
        stroke: 'var(--accent)',
        strokeWidth: 1.3,
        opacity:
          selectedEdge === e.id ||
          expanded.has(e.source_node_id) ||
          expanded.has(e.target_node_id)
            ? 0.9
            : 0.42,
      },
    }),
  )
  const act = (actions: GraphAction[]) => {
    if (!disabled) onActions(actions)
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
  function createConnected(kind: 'text' | MediaKind) {
    if (!portMenu) return
    const id = `cwnode_${crypto.randomUUID()}`,
      isOutput = portMenu.side === 'output'
    act([
      {
        type: 'add_node',
        node_id: id,
        type_key: `core.${kind}`,
        x: portMenu.x - (isOutput ? 0 : 280),
        y: portMenu.y - 90,
      },
      {
        type: 'connect_reference',
        source_node_id: isOutput ? portMenu.id : id,
        target_node_id: isOutput ? id : portMenu.id,
        source_port: 'output',
        target_port: 'reference',
        role: 'reference',
      },
    ])
    setPortMenu(null)
  }
  const contextItems: CanvasMenuItem[] = []
  if (contextMenu) {
    const ids = contextMenu.nodeIDs
    const target = canvas.nodes.find((n) => n.id === ids[0])
    const one = ids.length === 1
    const groupOnly = one && target?.metadata.type_key === 'core.group'
    const locked =
      disabled ||
      ids.some(
        (id) =>
          !canvas.nodes
            .find((n) => n.id === id)
            ?.capabilities.actions.includes('move'),
      )
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
    } else if (target) {
      if (one) {
        contextItems.push({
          label: groupOnly ? '重命名分组' : '编辑节点',
          icon: <Pencil size={16} />,
          disabled: disabled || (groupOnly ? !target.capabilities.actions.includes('rename') : !target.capabilities.actions.includes('edit')),
          action: () => (groupOnly ? renameNode(target.id) : onEdit(target.id)),
        })
        if (!groupOnly)
          contextItems.push({
            label: '重命名',
            icon: <FileText size={16} />,
            disabled:
              disabled || !target.capabilities.actions.includes('rename'),
            action: () => renameNode(target.id),
          })
        const kind = nodeKind(target.metadata.type_key)
        if (isMediaKind(kind)) {
          if (target.capabilities.actions.includes('replace'))
            contextItems.push({
              label: target.content ? '替换媒体' : '上传媒体',
              icon: <Upload size={16} />,
              disabled,
              action: () => onUploadToNode(target.id, kind),
            })
          if (
            target.content &&
            target.capabilities.actions.includes('maximize')
          )
            contextItems.push({
              label: '放大预览',
              icon: <Maximize2 size={16} />,
              action: () => onPreview(target.id),
            })
          if (
            target.content &&
            target.capabilities.actions.includes('download')
          )
            contextItems.push({
              label: '下载媒体',
              icon: <Download size={16} />,
              action: () => onDownload(target.id),
            })
        }
        if (
          target.content &&
          target.capabilities.actions.includes('save_to_library')
        )
          contextItems.push({
            label: '存入个人资产库',
            icon: <Library size={16} />,
            disabled,
            action: () => onSaveToLibrary(target.id),
          })
      }
      if (groupOnly)
        contextItems.push({
          label: '解组',
          icon: <FolderMinus size={16} />,
          shortcut: '⇧⌘G',
          disabled: locked,
          action: () => act([{ type: 'ungroup_nodes', node_id: target.id }]),
        })
      else
        contextItems.push({
          label: '打组',
          icon: <FolderPlus size={16} />,
          shortcut: '⌘G',
          disabled: locked,
          action: () => act([{ type: 'group_nodes', node_ids: ids }]),
        })
      contextItems.push({
        label: ids.length > 1 ? '复制选中节点' : '复制节点',
        icon: <Copy size={16} />,
        shortcut: '⌘D',
        disabled: locked,
        action: () =>
          act([{ type: 'duplicate_selection', node_ids: ids, dx: 36, dy: 36 }]),
      })
      contextItems.push({
        label: groupOnly
          ? '移除分组及其中节点'
          : ids.length > 1
            ? '移除选中节点'
            : '移除节点',
        icon: <Trash2 size={16} />,
        disabled: locked,
        danger: true,
        action: () =>
          act([{ type: 'remove_nodes', node_ids: ids, group_mode: 'subtree' }]),
      })
    } else if (!ids.length) {
      const at = flow.screenToFlowPosition({
        x: contextMenu.x,
        y: contextMenu.y,
      })
      for (const kind of ['text', 'image', 'video', 'audio'] as const)
        contextItems.push({
          label: `新增${kindLabel[kind]}节点`,
          icon: <KindIcon typeKey={`core.${kind}`} size={16} />,
          disabled,
          action: () => {
            const id = `cwnode_${crypto.randomUUID()}`
            act([
              {
                type: 'add_node',
                node_id: id,
                type_key: `core.${kind}`,
                x: at.x,
                y: at.y,
              },
            ])
            onSelection([id])
          },
        })
      contextItems.push({
        label: '适应全部节点',
        icon: <Scan size={16} />,
        action: () => {
          void flow.fitView({ padding: 0.25, maxZoom: 1 })
        },
      })
    }
    contextItems.push(
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
    )
  }
  return (
    <div
      className="cc-flow"
      ref={element}
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
        setPortMenu(null)
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
        if (
          (e.target as HTMLElement).closest('button,input,textarea,select') ||
          e.nativeEvent.isComposing
        )
          return
        const command = e.metaKey || e.ctrlKey,
          key = e.key.toLowerCase()
        if (command && key === 'g') {
          e.preventDefault()
          if (
            e.shiftKey && singleGroupSelected && active
          )
            act([{ type: 'ungroup_nodes', node_id: active.id }])
          else if (!e.shiftKey && selection.length && !singleGroupSelected)
            act([{ type: 'group_nodes', node_ids: selection }])
          return
        }
        if (command && key === 'd') {
          e.preventDefault()
          act([
            {
              type: 'duplicate_selection',
              node_ids: selection,
              dx: 36,
              dy: 36,
            },
          ])
          return
        }
        if (e.key === 'Delete' || e.key === 'Backspace') {
          e.preventDefault()
          removeSelection()
          return
        }
        const delta: Record<string, [number, number]> = {
            ArrowLeft: [-20, 0],
            ArrowRight: [20, 0],
            ArrowUp: [0, -20],
            ArrowDown: [0, 20],
          },
          step = delta[e.key]
        if (active && step && !moveDisabled) {
          e.preventDefault()
          e.stopPropagation()
          moveSelection(...step)
        } else if (e.key === 'Enter' && selected) {
          e.preventDefault()
          if (active?.data.item.metadata.type_key === 'core.group') renameNode(selected)
          else onEdit(selected)
        } else if (e.key === 'Escape') {
          setPortMenu(null)
          onSelection([])
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
        const raw = e.dataTransfer.getData('application/creative-asset')
        const ids = raw ? raw.split(',').filter(Boolean) : []
        if (ids.length) {
          const dropped = ids
            .map((id) => assets.find((a) => a.id === id))
            .filter((a): a is Asset => !!a)
          if (dropped.length === 1) onDropAsset(dropped[0]!, point.x, point.y)
          else if (dropped.length) onDropAssets(dropped, point.x, point.y)
          return
        }
        const files = Array.from(e.dataTransfer.files ?? [])
        if (files.length) onDropFiles(files, point.x, point.y)
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
          if (n.data.item.metadata.type_key === 'core.group') renameNode(n.id)
          else onEdit(n.id)
        }}
        onPaneClick={() => {
          onSelect(null)
          onSelection([])
          setPortMenu(null)
          setSelectedEdge(null)
        }}
        onDoubleClick={(e) => {
          if (
            disabled ||
            !(e.target as HTMLElement).classList.contains('react-flow__pane')
          )
            return
          const p = flow.screenToFlowPosition({ x: e.clientX, y: e.clientY })
          onAdd(p.x, p.y)
        }}
        onNodeDragStart={(_, n, ns) => {
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
        onNodeDragStop={(_, n, ns) => {
          const list = ns.length ? ns : [n]
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
            act([
              {
                type: 'connect_reference',
                source_node_id: connection.source,
                target_node_id: connection.target,
                source_port: 'output',
                target_port: 'reference',
                role: 'reference',
              },
            ])
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
            act([
              {
                type: 'connect_reference',
                source_node_id: output ? state.fromNode.id : target.id,
                target_node_id: output ? target.id : state.fromNode.id,
                source_port: 'output',
                target_port: 'reference',
                role: 'reference',
              },
            ])
            return
          }
          const hit = document.elementFromPoint(screen.x, screen.y)
          if (
            !hit?.closest('.react-flow__pane') ||
            hit.closest('.react-flow__node')
          )
            return
          const p = flow.screenToFlowPosition({
            x: pointer.clientX,
            y: pointer.clientY,
          })
          setPortMenu({
            id: state.fromNode.id,
            side: state.fromHandle?.type === 'target' ? 'input' : 'output',
            ...p,
            screen: { x: pointer.clientX, y: pointer.clientY },
          })
        }}
        onEdgeClick={(_, edge) => {
          onSelect(null)
          onSelection([])
          setPortMenu(null)
          setSelectedEdge(persistedEdges.has(edge.id) ? edge.id : null)
        }}
        onEdgeDoubleClick={(_, edge) => {
          if (persistedEdges.has(edge.id))
            act([{ type: 'disconnect_reference', edge_id: edge.id }])
        }}
        panOnScroll
        panOnScrollMode={PanOnScrollMode.Free}
        zoomOnScroll={false}
        zoomActivationKeyCode={['Control', 'Meta']}
        panOnDrag={panMode ? true : [1]}
        panActivationKeyCode="Space"
        fitView
        fitViewOptions={{ maxZoom: 1, padding: 0.25 }}
        minZoom={0.15}
        maxZoom={2}
        zoomOnDoubleClick={false}
        onInit={(instance) => {
          try {
            const raw = localStorage.getItem(
              `creative-viewport:${account}:${canvas.id}`,
            )
            if (raw) {
              const v = JSON.parse(raw) as {
                x: number
                y: number
                zoom: number
              }
              if (
                [v.x, v.y, v.zoom].every(Number.isFinite) &&
                v.zoom >= 0.15 &&
                v.zoom <= 2
              )
                void instance.setViewport(v)
            }
          } catch {
            /* Viewport preferences are optional. */
          }
        }}
        onMoveStart={() => { setPortMenu(null); setContextMenu(null) }}
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
        <Background gap={24} size={1} color="#64757030" />
        {minimap && (
          <MiniMap
            pannable
            zoomable
            nodeColor="#607a7180"
            maskColor="#14161988"
            style={{ background: '#252b30' }}
          />
        )}
      </ReactFlow>
      {active && (
        <div
          className="cc-selection-tools cc-glass"
          role="toolbar"
          aria-label="节点操作"
          style={{ left: toolbarX, top: toolbarY }}
        >
          {selection.length > 1 && <span>{selection.length} 个节点</span>}
          {selection.length === 1 && active.data.item.metadata.type_key === 'internal.document' && (
            <button
              className="cc-icon-button"
              aria-label="最大化文档"
              title="最大化文档"
              onClick={() => onEdit(active.id)}
            >
              <Maximize2 size={16} />
            </button>
          )}
          {selection.length === 1 &&
            active.data.item.capabilities.actions.includes('maximize') &&
            isMediaKind(nodeKind(active.data.item.metadata.type_key)) &&
            active.data.item.content && (
              <button
                className="cc-icon-button"
                aria-label="放大预览"
                title="放大预览"
                onClick={() => onPreview(active.id)}
              >
                <Maximize2 size={16} />
              </button>
            )}
          {selection.length === 1 &&
            active.data.item.capabilities.actions.includes('download') &&
            active.data.item.content && (
              <button
                className="cc-icon-button"
                aria-label="下载媒体"
                title="下载原件"
                onClick={() => onDownload(active.id)}
              >
                <Download size={16} />
              </button>
            )}
          {selection.length === 1 &&
            active.data.item.capabilities.actions.includes('replace') && (
              <button
                className="cc-icon-button"
                aria-label={active.data.item.content ? '替换媒体' : '上传媒体'}
                title={active.data.item.content ? '替换媒体' : '上传媒体'}
                disabled={disabled}
                onClick={() =>
                  onUploadToNode(
                    active.id,
                    nodeKind(active.data.item.metadata.type_key) as MediaKind,
                  )
                }
              >
                {active.data.item.content ? (
                  <RefreshCw size={16} />
                ) : (
                  <Upload size={16} />
                )}
              </button>
            )}
          {selection.length === 1 &&
            active.data.item.capabilities.actions.includes('save_to_library') &&
            active.data.item.content && (
              <button
                className="cc-icon-button"
                aria-label="存入个人资产库"
                title="存入个人资产库"
                disabled={disabled}
                onClick={() => onSaveToLibrary(active.id)}
              >
                <Library size={16} />
              </button>
            )}
          {!singleGroupSelected && (
            <button
              className="cc-icon-button"
              aria-label="打组"
              title="打组 · ⌘G"
              disabled={disabled || selectionReadOnly}
              onClick={() => act([{ type: 'group_nodes', node_ids: selection }])}
            >
              <FolderPlus size={16} />
            </button>
          )}
          {selection.length === 1 &&
            active.data.item.metadata.type_key === 'core.group' && (
              <button
                className="cc-icon-button"
                aria-label="解组"
                title="解组 · ⇧⌘G"
                disabled={disabled || selectionReadOnly}
                onClick={() =>
                  act([{ type: 'ungroup_nodes', node_id: active.id }])
                }
              >
                <FolderMinus size={16} />
              </button>
            )}
          <button
            className="cc-icon-button"
            aria-label="复制节点"
            title="复制 · ⌘D"
            disabled={disabled || selectionReadOnly}
            onClick={() =>
              act([
                {
                  type: 'duplicate_selection',
                  node_ids: selection,
                  dx: 36,
                  dy: 36,
                },
              ])
            }
          >
            <Copy size={16} />
          </button>
          <button
            className="cc-icon-button"
            aria-label={
              active.data.item.metadata.type_key === 'core.group'
                ? '移除分组及其中节点'
                : '移除节点'
            }
            title="移除选区，可撤销"
            disabled={disabled || selectionReadOnly}
            onClick={removeSelection}
          >
            <Trash2 size={16} />
          </button>
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
      {portMenu && (
        <div
          className="cc-port-menu cc-glass"
          ref={menuRef}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              e.stopPropagation()
              setPortMenu(null)
            }
          }}
          role="dialog"
          aria-label="创建并连接节点"
        >
          <div>
            <span>
              {portMenu.side === 'input' ? '添加上游参考' : '添加下游节点'}
            </span>
            <button
              className="cc-icon-button"
              aria-label="关闭连接菜单"
              onClick={() => setPortMenu(null)}
            >
              <X size={14} />
            </button>
          </div>
          <button disabled={disabled} onClick={() => createConnected('text')}>
            <Type size={16} />
            文字
          </button>
          <button disabled={disabled} onClick={() => createConnected('image')}>
            <ImageIcon size={16} />
            图片
          </button>
          <button disabled={disabled} onClick={() => createConnected('video')}>
            <Film size={16} />
            视频
          </button>
          <button disabled={disabled} onClick={() => createConnected('audio')}>
            <AudioLines size={16} />
            音频
          </button>
          <label className="cc-connect-existing">
            连接已有节点
            <select
              className="input"
              aria-label="连接已有节点"
              value=""
              disabled={disabled}
              onChange={(e) => {
                const id = e.target.value
                if (!id) return
                act([
                  {
                    type: 'connect_reference',
                    source_node_id:
                      portMenu.side === 'output' ? portMenu.id : id,
                    target_node_id:
                      portMenu.side === 'output' ? id : portMenu.id,
                    source_port: 'output',
                    target_port: 'reference',
                    role: 'reference',
                  },
                ])
                setPortMenu(null)
              }}
            >
              <option value="">选择节点…</option>
              {canvas.nodes
                .filter(
                  (n) =>
                    n.id !== portMenu.id &&
                    n.capabilities.actions.includes('reference'),
                )
                .map((n) => (
                  <option key={n.id} value={n.id}>
                    {n.metadata.title || untitled(n.metadata.type_key)}
                  </option>
                ))}
            </select>
          </label>
        </div>
      )}
      <div className="cc-history-tools cc-glass">
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
      </div>
      <div className="cc-viewport-controls cc-glass">
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
          onClick={() => void flow.fitView({ maxZoom: 1, padding: 0.25 })}
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
