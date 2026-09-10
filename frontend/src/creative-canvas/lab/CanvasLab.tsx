import { useEffect, useRef, useState } from 'react'
import { useStore } from 'zustand'
import { Background, Controls, Handle, NodeToolbar, PanOnScrollMode, Position, ReactFlow, SelectionMode, useReactFlow, useKeyPress } from '@xyflow/react'
import type { NodeProps } from '@xyflow/react'
import { createCanvasStore, canConnect } from './state.ts'
import type { LabNode } from './state.ts'

function ContentNode({ data, selected, id }: NodeProps<LabNode>) {
  return <article className="lab-node" data-node={id}>
    <Handle type="target" id="in" position={Position.Left} />
    <NodeToolbar isVisible={selected} position={Position.Top}><span className="lab-node-toolbar">已选中 · 固定参考端口</span></NodeToolbar>
    <span className="lab-node-kind">文字参考</span><h2>{data.title}</h2><p>{data.text}</p>
    <Handle type="source" id="out" position={Position.Right} />
  </article>
}
const nodeTypes = { content: ContentNode }

type Sample = { duration_ms: number; frames: number; p95_ms: number; max_ms: number; nodes: number; edges: number; browser: string; viewport: string; hardware_concurrency: number; interrupted: boolean }
export default function CanvasLab() {
  const [store] = useState(createCanvasStore)
  const state = useStore(store)
  const flow = useReactFlow()
  const panHeld = useKeyPress('Space')
  const [sampling, setSampling] = useState(false)
  const [sample, setSample] = useState<Sample | null>(null)
  const interrupted = useRef(false)
  useEffect(() => {
    if (!sampling) return
    const times: number[] = []; const start = performance.now(); let last = start; let frame = 0
    const record = (now: number) => { times.push(now - last); last = now; if (now - start >= 600_000) setSampling(false); else frame = requestAnimationFrame(record) }
    const markHidden = () => { if (document.hidden) interrupted.current = true }
    interrupted.current = document.hidden
    document.addEventListener('visibilitychange', markHidden)
    frame = requestAnimationFrame(record)
    return () => {
      cancelAnimationFrame(frame); document.removeEventListener('visibilitychange', markHidden)
      const sorted = [...times].sort((a, b) => a - b)
      setSample({ duration_ms: Math.round(performance.now() - start), frames: times.length, p95_ms: sorted[Math.max(0, Math.ceil(sorted.length * .95) - 1)] ?? 0, max_ms: sorted.at(-1) ?? 0, nodes: store.getState().nodes.length, edges: store.getState().edges.length, browser: navigator.userAgent, viewport: `${innerWidth}×${innerHeight}@${devicePixelRatio}`, hardware_concurrency: navigator.hardwareConcurrency, interrupted: interrupted.current })
    }
  }, [sampling, store])
  const reset = (large = false) => { if (sampling) return; store.getState().reset(large); requestAnimationFrame(() => void flow.fitView({ padding: .15 })) }
  const selected = state.nodes.filter(n => n.selected)
  return <main className="lab-shell">
    <header className="lab-header"><div><span className="lab-eyebrow">CREATIVE SPACE · ENGINEERING</span><h1>创意画布</h1></div><p>基础验证页 · 仅本地状态，不保存到业务库</p><a href="/">返回影约</a></header>
    <section className="lab-stage" aria-label="受控创意画布">
      <ReactFlow<LabNode> nodeTypes={nodeTypes} nodes={state.nodes} edges={state.edges} onNodesChange={state.onNodesChange} onEdgesChange={state.onEdgesChange} onConnect={state.onConnect} isValidConnection={c => canConnect(state.nodes, state.edges, { ...c, sourceHandle: c.sourceHandle ?? null, targetHandle: c.targetHandle ?? null })}
        nodesDraggable={!panHeld} nodesConnectable={!panHeld} selectionOnDrag selectionMode={SelectionMode.Partial} panOnDrag={[1]} panActivationKeyCode="Space" panOnScroll panOnScrollMode={PanOnScrollMode.Vertical} zoomOnScroll={false} zoomActivationKeyCode={['Control', 'Meta']} multiSelectionKeyCode={['Meta', 'Control']} deleteKeyCode={null} fitView colorMode="dark" onlyRenderVisibleElements>
        <Background color="#38414f" gap={22} size={1} /><Controls showInteractive={false} />
      </ReactFlow>
      <aside className="lab-panel"><strong>画布基线</strong><span>{state.nodes.length} 节点 · {state.edges.length} 连线 · 已选 {selected.length}</span><p>滚轮上下平移，Ctrl / ⌘ + 滚轮缩放。左键框选，中键或空格拖动平移。拖动组时，子节点保留相对坐标。</p><div className="lab-actions"><button disabled={sampling} onClick={() => reset()}>分组样例</button><button disabled={sampling} onClick={() => reset(true)}>200 / 300 基准</button></div><button onClick={() => setSampling(s => !s)}>{sampling ? '停止并查看采样' : '开始 10 分钟采样'}</button><output aria-live="polite">{sampling ? '采样中，请持续进行平移、缩放和拖动。' : sample ? `${(sample.duration_ms / 1000).toFixed(1)} 秒 · p95 ${sample.p95_ms.toFixed(1)} ms${sample.interrupted ? ' · 页面曾进入后台，需重测' : ''}` : '采样时请保持页面前台；短样本不等于完整性能验收。'}</output>{sample && <button onClick={() => { const blob = new Blob([JSON.stringify(sample, null, 2)], { type: 'application/json' }); const url = URL.createObjectURL(blob); const a = document.createElement('a'); a.href = url; a.download = 'creative-canvas-benchmark.json'; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000) }}>下载采样记录</button>}</aside>
      {selected.length === 1 && <aside className="lab-inspector" aria-label="所选节点坐标"><strong>{selected[0]!.data.title}</strong><span>父节点：{selected[0]!.parentId ?? '画布'}</span><code data-testid="node-position">x {Math.round(selected[0]!.position.x)} · y {Math.round(selected[0]!.position.y)}</code></aside>}
    </section>
  </main>
}

