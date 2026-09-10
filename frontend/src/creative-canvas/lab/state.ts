import { createStore } from 'zustand/vanilla'
import { applyNodeChanges, applyEdgeChanges, addEdge } from '@xyflow/react'
import type { Node, Edge, NodeChange, EdgeChange, Connection } from '@xyflow/react'

// React Flow view-models for the development harness; not production API DTOs.
export type LabNode = Node<{ title: string; text: string }, 'content' | 'group'>
export type Fixture = { nodes: LabNode[]; edges: Edge[] }

export function fixture(large = false): Fixture {
  if (large) {
    const nodes: LabNode[] = Array.from({ length: 200 }, (_, i) => ({
      id: `sample-${i}`, type: 'content', position: { x: (i % 20) * 290, y: Math.floor(i / 20) * 220 },
      data: { title: `创作片段 ${i + 1}`, text: '固定尺寸与端口 · 受控节点状态' },
      width: 240, height: 155,
    }))
    const edges: Edge[] = []
    for (let step = 1; step <= 2; step++) {
      for (let i = 0; i + step < nodes.length && edges.length < 300; i++) {
        edges.push({ id: `edge-${i}-${i + step}`, source: `sample-${i}`, target: `sample-${i + step}`, sourceHandle: 'out', targetHandle: 'in' })
      }
    }
    return { nodes, edges }
  }
  return {
    nodes: [
      { id: 'group', type: 'group', position: { x: 110, y: 100 }, width: 660, height: 480, data: { title: '光线研究', text: '' }, style: { width: 660, height: 480 } },
      { id: 'nested', type: 'group', parentId: 'group', position: { x: 45, y: 195 }, width: 555, height: 235, data: { title: '细节', text: '' }, style: { width: 555, height: 235 } },
      { id: 'light', type: 'content', parentId: 'group', position: { x: 45, y: 25 }, width: 240, height: 155, data: { title: '午后的侧光', text: '让光从左侧进入，保留面部的明暗过渡。' } },
      { id: 'texture', type: 'content', parentId: 'nested', position: { x: 20, y: 40 }, width: 240, height: 155, data: { title: '材质与层次', text: '浅色织物、柔和阴影，以及一小块留白。' } },
      { id: 'direction', type: 'content', position: { x: 880, y: 170 }, width: 240, height: 155, data: { title: '创作方向', text: '收拢参考，保留自己的表达。' } },
    ],
    edges: [
      { id: 'light-direction', source: 'light', target: 'direction', sourceHandle: 'out', targetHandle: 'in' },
      { id: 'texture-direction', source: 'texture', target: 'direction', sourceHandle: 'out', targetHandle: 'in' },
    ],
  }
}

export function canConnect(nodes: LabNode[], edges: Edge[], connection: Connection): boolean {
  if (connection.source === connection.target || connection.sourceHandle !== 'out' || connection.targetHandle !== 'in') return false
  const source = nodes.find(n => n.id === connection.source)
  const target = nodes.find(n => n.id === connection.target)
  if (!source || !target || source.type === 'group' || target.type === 'group') return false
  if (edges.some(e => e.source === source.id && e.target === target.id)) return false
  const pending = [target.id], seen = new Set<string>()
  while (pending.length) {
    const id = pending.pop()!
    if (id === source.id) return false
    if (seen.has(id)) continue
    seen.add(id)
    for (const edge of edges) if (edge.source === id) pending.push(edge.target)
  }
  return true
}

export function createCanvasStore() {
  return createStore<Fixture & {
    onNodesChange: (changes: NodeChange<LabNode>[]) => void
    onEdgesChange: (changes: EdgeChange[]) => void
    onConnect: (connection: Connection) => void
    reset: (large?: boolean) => void
  }>((set, get) => ({
    ...fixture(),
    onNodesChange: changes => set(state => ({ nodes: applyNodeChanges(changes, state.nodes) })),
    onEdgesChange: changes => set(state => ({ edges: applyEdgeChanges(changes, state.edges) })),
    onConnect: connection => { const state = get(); if (canConnect(state.nodes, state.edges, connection)) set({ edges: addEdge(connection, state.edges) }) },
    reset: large => set(fixture(large)),
  }))
}
