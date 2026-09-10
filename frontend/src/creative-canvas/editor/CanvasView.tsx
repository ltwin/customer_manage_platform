import { memo, useCallback, useEffect, useRef, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  applyNodeChanges,
  PanOnScrollMode,
  useReactFlow,
  useKeyPress,
} from '@xyflow/react'
import type { Node, NodeProps, NodeChange } from '@xyflow/react'
import type { Asset, Canvas, CanvasNode } from './api.ts'

import type { PositionDraft } from './journal.ts'

type FlowNode = Node<{ item: CanvasNode }, 'content'>
const Card = memo(function Card({ data, selected }: NodeProps<FlowNode>) {
  const n = data.item
  const p = n.content?.payload
  return (
    <article className={`cc-node ${selected ? 'is-selected' : ''}`}>
      <span className="cc-kind">
        {n.type_key === 'core.text' ? '文字' : '链接'}
      </span>
      <h3>
        {n.title || (n.type_key === 'core.text' ? '未命名文字' : '未命名链接')}
      </h3>
      <p>
        {n.unavailable
          ? '当前无权展示此内容'
          : p && 'body' in p
            ? p.body
            : p && 'url' in p
              ? p.url
              : '选择节点，写下你的想法'}
      </p>
      {n.content?.truncated && <small>正文预览 · 编辑时载入全文</small>}
    </article>
  )
})
const nodeTypes = { content: Card }
export default function CanvasView({
  canvas,
  selected,
  onSelect,
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
  onSelect: (id: string) => void
  onMove: (node: CanvasNode, x: number, y: number) => void
  onDropAsset: (asset: Asset, x: number, y: number) => void
  disabled: boolean
  moveDisabled: boolean
  positions?: Record<string, PositionDraft>
  assets: Asset[]
  account: string
}) {
  const flow = useReactFlow<FlowNode>()
  const panHeld = useKeyPress('Space')
  const dragging = useRef<CanvasNode | null>(null)
  const [nodes, setNodes] = useState<FlowNode[]>([])
  const element = useRef<HTMLDivElement>(null)
  useEffect(() => {
    let width = 0
    const observer = new ResizeObserver(([entry]) => {
      const next = entry?.contentRect.width ?? 0
      if (width && Math.abs(next - width) > 20 && !dragging.current)
        void flow.fitView({ maxZoom: 1, padding: 0.25 })
      width = next
    })
    if (element.current) observer.observe(element.current)
    return () => observer.disconnect()
  }, [flow])

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
          old.draggable === !moveDisabled &&
          old.position.x === position.x &&
          old.position.y === position.y
        )
          return old
        return {
          ...old,
          id: n.id,
          type: 'content',
          position,
          data: old?.data.item === n ? old.data : { item: n },
          selected: n.id === selected,
          draggable: !moveDisabled,
        }
      })
      return next.length === previous.length &&
        next.every((n, i) => n === previous[i])
        ? previous
        : next
    })
  }, [canvas, selected, moveDisabled, positions])
  const changeNodes = useCallback((changes: NodeChange<FlowNode>[]) => {
    setNodes((ns) => applyNodeChanges(changes, ns))
  }, [])

  return (
    <div
      className="cc-flow"
      ref={element}
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
        nodesDraggable={!moveDisabled && !panHeld}
        deleteKeyCode={null}
        multiSelectionKeyCode={null}
        onNodesChange={changeNodes}
        onNodeClick={(_, n) => onSelect(n.id)}
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
        panOnDrag={[1]}
        panActivationKeyCode="Space"
        fitView
        fitViewOptions={{ maxZoom: 1, padding: 0.25 }}
        minZoom={0.15}
        maxZoom={2}
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
        <Background color="var(--canvas-dot)" gap={24} size={1} />
        <Controls showInteractive={false} />
      </ReactFlow>
      {!canvas.nodes.length && (
        <div className="cc-canvas-empty">
          <h2>从一句灵感开始</h2>
          <p>新增文字、链接，或从个人库放入内容。</p>
        </div>
      )}
      <p className="cc-gesture">
        滚轮上下平移 · Ctrl / ⌘ + 滚轮缩放 · 空格拖动平移
      </p>
    </div>
  )
}
