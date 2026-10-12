import { memo, useContext, useEffect, useState, type ReactNode } from 'react'
import {
  Check,
  ChevronRight,
  Lightbulb,
  ListChecks,
  Scan,
  Wrench,
  X,
} from 'lucide-react'
import type { components } from '../../api/schema'
import * as api from './api.ts'
import {
  layout,
  nodeRefs,
  planSteps,
  renderable,
  seconds,
  segmentOpen,
  segmentSummary,
  text,
  isFinished,
  type AgentBlock,
} from './agentBlocks.ts'
import { AgentTurnContext, setOpen, useAgentRuns } from './agentRuns.ts'
import { hasUnsettledCost, runFailure } from './agentState.ts'
import { AgentMarkdown } from './AgentMarkdown.tsx'
import { MediaFigure } from './MediaView.tsx'
import { isMediaKind } from './media.ts'

type Detail = components['schemas']['CreativeAgentBlockDetail']

function Chevron({ open }: { open: boolean }) {
  return (
    <span className={`cc-agent-chev${open ? ' open' : ''}`}>
      <ChevronRight size={12} />
    </span>
  )
}

function useOpen(id: string): boolean | undefined {
  return useAgentRuns((state) => state.open[id])
}

function ThinkingRow({ block }: { block: AgentBlock }) {
  const open = useOpen(block.id) ?? false
  const live = block.state === 'streaming'
  const summary = text(block.payload.summary)
  const took = Number(block.payload.duration_ms) || 0
  return (
    <div>
      <button
        className="cc-agent-row"
        aria-expanded={open}
        onClick={() => setOpen(block.id, !open)}
      >
        <span className="cc-agent-ico">
          <Lightbulb size={14} />
        </span>
        <span
          className={`cc-agent-row-title${live ? ' cc-agent-shimmer' : ''}`}
        >
          {live ? '思考中' : took ? `已思考 ${seconds(took)}` : '已思考'}
        </span>
        <Chevron open={open} />
      </button>
      {open ? (
        <div className="cc-agent-think">
          <AgentMarkdown source={summary} streaming={live} />
        </div>
      ) : (
        live &&
        summary && (
          <div className="cc-agent-peek">
            {summary.split('\n').filter(Boolean).at(-1)}
          </div>
        )
      )}
    </div>
  )
}

function toolMeta(block: AgentBlock): string {
  if (block.state === 'failed') return '失败'
  if (block.state === 'skipped') return '已跳过'
  if (block.state === 'interrupted') return '已停止'
  const took = Number(block.payload.duration_ms) || 0
  return block.state === 'succeeded' && took ? seconds(took) : ''
}

function ToolDetail({ block }: { block: AgentBlock }) {
  const [detail, setDetail] = useState<Detail | null>(null)
  const [failed, setFailed] = useState(false)
  // The result arrives when the tool finishes, so a finished state reads again.
  useEffect(() => {
    const abort = new AbortController()
    setFailed(false)
    void api
      .read<Detail>(
        `/agent-runs/${encodeURIComponent(block.run_id)}/blocks/${encodeURIComponent(block.id)}/detail`,
        abort.signal,
      )
      .then(setDetail)
      .catch(() => {
        if (!abort.signal.aborted) setFailed(true)
      })
    return () => abort.abort()
  }, [block.run_id, block.id, block.state])
  if (failed) return <p className="cc-agent-row-error">详情暂时无法读取</p>
  if (!detail) return <p className="cc-agent-peek">正在读取…</p>
  return (
    <dl className="cc-agent-detail">
      <dt>参数</dt>
      <dd>
        <pre>{JSON.stringify(detail.arguments, null, 2)}</pre>
      </dd>
      {detail.result !== undefined && (
        <>
          <dt>结果</dt>
          <dd>
            <pre>{detail.result}</pre>
          </dd>
        </>
      )}
    </dl>
  )
}

// Results a tool produced, shown under its row. Nodes that have left the
// canvas simply have no preview.
function ToolThumbs({ ids }: { ids: string[] }) {
  const { canvas } = useContext(AgentTurnContext)
  const nodes = ids.flatMap((id) => {
    const node = canvas?.nodes.find((item) => item.id === id)
    return node?.content && isMediaKind(node.content.kind) ? [node] : []
  })
  if (!nodes.length) return null
  return (
    <div className="cc-agent-thumbs">
      {nodes.map((node) =>
        node.content?.kind === 'image' ? (
          <MediaFigure
            key={node.id}
            content={node.content}
            kind="image"
            title={node.metadata.title || '生成结果'}
          />
        ) : (
          <figure key={node.id} className="cc-agent-media-result">
            <MediaFigure
              content={node.content}
              kind={node.content?.kind ?? ''}
              title={node.metadata.title || '生成结果'}
              controls
            />
            <figcaption>{node.metadata.title || '生成结果'}</figcaption>
          </figure>
        ),
      )}
    </div>
  )
}

function ToolRetry({ block }: { block: AgentBlock }) {
  const { retry } = useContext(AgentTurnContext)
  const finished = useAgentRuns((state) => {
    const view = state.runs[block.run_id]
    return !!view && isFinished(view.run)
  })
  const step = text(block.payload.step_id)
  if (!retry || !finished || !step) return null
  return (
    <button className="cc-agent-link" onClick={() => retry(block.run_id, step)}>
      重试这一步
    </button>
  )
}

function ToolRow({ block }: { block: AgentBlock }) {
  const open = useOpen(block.id) ?? false
  const running = block.state === 'running'
  const key = text(block.payload.tool_key)
  const refs = nodeRefs(block)
  return (
    <div className={`cc-agent-tool is-${block.state}`}>
      <button
        className="cc-agent-row"
        aria-expanded={open}
        onClick={() => setOpen(block.id, !open)}
      >
        <span className="cc-agent-ico">
          {running ? (
            <span className="cc-agent-spin" />
          ) : block.state === 'failed' ? (
            <X size={14} />
          ) : key.startsWith('read') ? (
            <Scan size={14} />
          ) : (
            <Wrench size={14} />
          )}
        </span>
        <span className="cc-agent-row-title">
          {text(block.payload.title) || key}
        </span>
        <span className="cc-agent-row-meta">{toolMeta(block)}</span>
        <Chevron open={open} />
      </button>
      {block.state === 'failed' && key === 'read_skill_resource' && (
        <ToolRetry block={block} />
      )}
      {refs.length > 0 && <ToolThumbs ids={refs} />}
      {open && <ToolDetail block={block} />}
    </div>
  )
}

function PlanCard({ block }: { block: AgentBlock }) {
  const steps = planSteps(block)
  const done = steps.filter((step) => step.state === 'done').length
  return (
    <div className="cc-agent-plan">
      <div className="cc-agent-plan-head">
        <ListChecks size={13} />
        <span>计划</span>
        <span className="cc-agent-plan-count">
          {done}/{steps.length}
        </span>
      </div>
      <ol>
        {steps.map((step) => (
          <li key={step.key} className={`is-${step.state}`}>
            <span className="cc-agent-plan-box">
              {step.state === 'done' ? (
                <Check size={10} />
              ) : step.state === 'doing' && block.state === 'active' ? (
                <span className="cc-agent-spin" />
              ) : null}
            </span>
            <span>{step.title}</span>
            {step.state === 'skipped' && (
              <span className="cc-agent-row-meta">已跳过</span>
            )}
          </li>
        ))}
      </ol>
    </div>
  )
}

// One block. A block object is replaced only when it changes, so a streamed
// answer re-renders its own block and nothing else.
export const BlockView = memo(function BlockView({
  block,
}: {
  block: AgentBlock
}) {
  let body: ReactNode
  if (!renderable(block))
    body = <p className="cc-agent-peek">此内容需要更新页面后查看</p>
  else if (block.type === 'text')
    body = (
      <AgentMarkdown
        source={text(block.payload.markdown)}
        streaming={block.state === 'streaming'}
      />
    )
  else if (block.type === 'thinking') body = <ThinkingRow block={block} />
  else if (block.type === 'tool') body = <ToolRow block={block} />
  else if (block.type === 'plan') body = <PlanCard block={block} />
  else body = <p className="cc-agent-note">{text(block.payload.message)}</p>
  return (
    <div
      className="cc-agent-block"
      data-block={block.id}
      data-type={block.type}
      data-state={block.state}
    >
      {body}
    </div>
  )
})

function Segment({
  id,
  blocks,
  settled,
}: {
  id: string
  blocks: AgentBlock[]
  settled: boolean
}) {
  // The segment is named after its first block; its own choice needs its own key.
  const key = `segment:${id}`
  const open = segmentOpen(useOpen(key), settled)
  const rows = blocks.map((block) => <BlockView key={block.id} block={block} />)
  if (!settled) return <div data-segment={id}>{rows}</div>
  return (
    <div data-segment={id}>
      <button
        className="cc-agent-fold"
        aria-expanded={open}
        onClick={() => setOpen(key, !open)}
      >
        <Chevron open={open} />
        {segmentSummary(blocks)}
      </button>
      {open && <div className="cc-agent-fold-body">{rows}</div>}
    </div>
  )
}

// The blocks of one run, in the order they happened.
export function RunTurn({ runID }: { runID: string }) {
  const view = useAgentRuns((state) => state.runs[runID])
  if (!view) return <TurnLoading />
  const items = layout(view.blocks)
  const finished = isFinished(view.run)
  const { run } = view
  return (
    <div className="cc-agent-turn" data-run={runID} data-state={run.state}>
      {items.map((item, index) =>
        item.kind === 'block' ? (
          <BlockView key={item.block.id} block={item.block} />
        ) : (
          <Segment
            key={item.id}
            id={item.id}
            blocks={item.blocks}
            settled={finished || index < items.length - 1}
          />
        ),
      )}
      {view.outdated && (
        <p className="cc-agent-note">助手有新的内容形式，刷新页面后查看。</p>
      )}
      {finished && run.state !== 'succeeded' && (
        <p className="cc-agent-note">
          {run.state === 'cancelled'
            ? '已停止'
            : runFailure(run.error_code) || '本次执行未能完成'}
          {hasUnsettledCost(run) && '；已发生的费用仍在核实'}
        </p>
      )}
    </div>
  )
}

// The outline of an answer while its run is still being read.
export function TurnLoading() {
  return (
    <div className="cc-agent-loading-answer" aria-hidden="true">
      <span className="cc-agent-bone" />
      <span className="cc-agent-bone" />
      <span className="cc-agent-bone is-short" />
    </div>
  )
}
