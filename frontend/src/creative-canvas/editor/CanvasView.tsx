import { memo, useCallback, useEffect, useRef, useState } from 'react'
import {
  ReactFlow,
  Background,
  MiniMap,
  useViewport,
  applyNodeChanges,
  PanOnScrollMode,
  useReactFlow,
  useKeyPress,
} from '@xyflow/react'
import type { Node, NodeProps, NodeChange } from '@xyflow/react'
import type { Asset, Canvas, CanvasNode } from './api.ts'

import {
  Type,
  Link2,
  Ellipsis,
  Pencil,
  Minus,
  Plus,
  Scan,
  Map as MapIcon,
} from 'lucide-react'
import type { PositionDraft } from './journal.ts'

type FlowNode = Node<
  { item: CanvasNode; select: (id: string) => void },
  'content'
>
const Card = memo(function Card({ data, selected }: NodeProps<FlowNode>) {
  const n = data.item
  const p = n.content?.payload
  return (
    <article
      className={`cc-node ${n.type_key === 'core.text' ? 'cc-text-node' : 'cc-link-node'} ${selected ? 'is-selected' : ''}`}
    >
      <header className="cc-node-header">
        {n.type_key === 'core.text' ? <Type size={14} /> : <Link2 size={14} />}
        <h3>
          {n.title ||
            (n.type_key === 'core.text' ? '未命名文字' : '未命名链接')}
        </h3>
        <button
          className="cc-icon-button nodrag"
          aria-label="节点操作"
          onClick={() => data.select(n.id)}
          title="节点操作"
        >
          <Ellipsis size={16} />
        </button>
      </header>
      <div className="cc-node-content">
        <p>
          {n.unavailable
            ? '当前无权展示此内容'
            : p && 'body' in p
              ? p.body
              : p && 'url' in p
                ? p.url
                : '双击，写下你的想法。'}
        </p>
        {n.content?.truncated && <small>正文预览 · 编辑时载入全文</small>}
      </div>
    </article>
  )
})
const nodeTypes = { content: Card }
export default function CanvasView({
  canvas,
  selected,
  onSelect,
  onEdit,
  onAdd,
  panMode,
  onMove,
  onDropAsset,
  disabled,
  moveDisabled,
  positions,
  assets,
  account,
}: {
  canvas: Canvas
  selected: string | null
  onSelect: (id: string | null) => void
  onEdit: (id: string) => void
  onAdd: (x: number, y: number) => void
  panMode: boolean
  onMove: (node: CanvasNode, x: number, y: number) => void
  onDropAsset: (asset: Asset, x: number, y: number) => void
  disabled: boolean
  moveDisabled: boolean
  positions?: Record<string, PositionDraft>
  assets: Asset[]
  account: string
}) {
  const selectRef = useRef(onSelect)
  selectRef.current = onSelect
  const selectNode = useCallback((id: string) => selectRef.current(id), [])
  const flow = useReactFlow<FlowNode>()
  const viewport = useViewport()
  const [size, setSize] = useState({ width: 0, height: 0 })
  const [minimap, setMinimap] = useState(false)
  const panHeld = useKeyPress('Space')
  const dragging = useRef<CanvasNode | null>(null)
  const [nodes, setNodes] = useState<FlowNode[]>([])
  const element = useRef<HTMLDivElement>(null)
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
      const byID = new Map(previous.map((n) => [n.id, n]))
      const next = canvas.nodes.map((n): FlowNode => {
        const old = byID.get(n.id)
        const local = positions?.[`${canvas.id}:${n.id}`]
        const position =
          dragging.current?.id === n.id && old
            ? old.position
            : { x: local?.x ?? n.x, y: local?.y ?? n.y }
        if (
          old &&
          old.data.item === n &&
          old.selected === (n.id === selected) &&
          old.draggable === (!moveDisabled && !panMode && !panHeld) &&
          old.position.x === position.x &&
          old.position.y === position.y
        )
          return old
        return {
          ...old,
          id: n.id,
          type: 'content',
          position,
          data:
            old?.data.item === n ? old.data : { item: n, select: selectNode },
          selected: n.id === selected,
          draggable: !moveDisabled && !panMode && !panHeld,
        }
      })
      return next.length === previous.length &&
        next.every((n, i) => n === previous[i])
        ? previous
        : next
    })
  }, [canvas, selected, moveDisabled, positions, panMode, panHeld, selectNode])
  const changeNodes = useCallback((changes: NodeChange<FlowNode>[]) => {
    setNodes((ns) => applyNodeChanges(changes, ns))
  }, [])

  const active = nodes.find((n) => n.id === selected)
  const toolbarX = active
    ? Math.max(
        12,
        Math.min(
          size.width - 190,
          viewport.x + (active.position.x + 140) * viewport.zoom - 85,
        ),
      )
    : 0
  const toolbarY = active
    ? Math.max(
        12,
        Math.min(
          size.height - 100,
          viewport.y + active.position.y * viewport.zoom - 55,
        ),
      )
    : 0

  return (
    <div
      className="cc-flow"
      ref={element}
      onKeyDown={(e) => {
        if ((e.target as HTMLElement).closest('button,input,textarea,select'))
          return
        const delta: Record<string, [number, number]> = {
          ArrowLeft: [-20, 0],
          ArrowRight: [20, 0],
          ArrowUp: [0, -20],
          ArrowDown: [0, 20],
        }
        const step = delta[e.key]
        if (active && step && !moveDisabled) {
          e.preventDefault()
          e.stopPropagation()
          onMove(
            active.data.item,
            active.position.x + step[0],
            active.position.y + step[1],
          )
        } else if (e.key === 'Enter' && selected) {
          e.preventDefault()
          onEdit(selected)
        }
      }}
      onDragOver={(e) => {
        e.preventDefault()
        e.dataTransfer.dropEffect = disabled ? 'none' : 'copy'
      }}
      onDrop={(e) => {
        e.preventDefault()
        if (disabled) return
        const asset = assets.find(
          (a) => a.id === e.dataTransfer.getData('application/creative-asset'),
        )
        if (asset) {
          const point = flow.screenToFlowPosition({
            x: e.clientX,
            y: e.clientY,
          })
          onDropAsset(asset, point.x, point.y)
        }
      }}
    >
      <ReactFlow<FlowNode>
        nodes={nodes}
        edges={[]}
        nodeTypes={nodeTypes}
        nodesConnectable={false}
        nodesDraggable={!moveDisabled && !panHeld && !panMode}
        disableKeyboardA11y
        deleteKeyCode={null}
        multiSelectionKeyCode={null}
        onNodesChange={changeNodes}
        onNodeClick={(_, n) => onSelect(n.id)}
        onNodeDoubleClick={(_, n) => onEdit(n.id)}
        onPaneClick={() => onSelect(null)}
        onDoubleClick={(e) => {
          if (
            disabled ||
            !(e.target as HTMLElement).classList.contains('react-flow__pane')
          )
            return
          const point = flow.screenToFlowPosition({
            x: e.clientX,
            y: e.clientY,
          })
          onAdd(point.x, point.y)
        }}
        onNodeDragStart={(_, n) => {
          dragging.current = n.data.item
        }}
        onNodeDragStop={(_, n) => {
          const base = dragging.current ?? n.data.item
          dragging.current = null
          onMove(base, n.position.x, n.position.y)
        }}
        panOnScroll
        panOnScrollMode={PanOnScrollMode.Vertical}
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
        <Background color="var(--canvas-dot)" gap={22} size={1} />
        {minimap && (
          <MiniMap
            pannable
            zoomable
            nodeColor="#5a6764"
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
          <span>
            {active.data.item.type_key === 'core.text'
              ? '文字节点'
              : '链接节点'}
          </span>
          <button
            className="cc-icon-button"
            aria-label="编辑节点"
            title="编辑节点 · 双击"
            onClick={() => onEdit(active.id)}
          >
            <Pencil size={16} />
          </button>
        </div>
      )}
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
      {!canvas.nodes.length && (
        <div className="cc-canvas-empty">
          <h2>从一个念头开始。</h2>
          <p>双击画布，或从资产库放入第一份灵感。</p>
        </div>
      )}
      <p className="cc-gesture">
        滚轮上下平移 · Ctrl / ⌘ + 滚轮缩放 · 空格拖动平移
      </p>
    </div>
  )
}
