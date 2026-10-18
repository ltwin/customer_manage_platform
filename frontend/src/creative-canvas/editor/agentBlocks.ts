import type { components } from '../../api/schema'
import type { ContextUsage } from './agentContext.ts'
import { runLabels, type AgentRun } from './agentState.ts'

export type AgentBlock = components['schemas']['CreativeAgentBlock']
export type RunEvent = components['schemas']['CreativeAgentRunEvent']
type BlockUpdated = components['schemas']['CreativeAgentBlockUpdated']

// What one run looks like right now: the snapshot plus every event after it.
export interface RunView {
  run: AgentRun
  // Ordered by ordinal. Blocks are only appended, never removed or reordered.
  blocks: AgentBlock[]
  // The last event folded in; the stream resumes after it.
  seq: string
  // The last run-level event folded in. A snapshot at or past it has run
  // fields at least as new as ours.
  runSeq: string
  // The server speaks an envelope this page does not know.
  outdated: boolean
  // The context this run's last model request carried, from its events.
  usage?: ContextUsage
}

// 'resync': the local copy cannot absorb this event; re-read the snapshot.
export type Applied = RunView | 'resync' | 'outdated'

// Payload versions this page renders. A newer one degrades that block alone.
const supported: Record<string, number> = {
  text: 1,
  thinking: 1,
  tool: 1,
  plan: 1,
  notice: 1,
  compaction: 1,
}
const processTypes = new Set(['thinking', 'tool', 'plan'])

export function renderable(block: AgentBlock): boolean {
  return block.payload_version <= (supported[block.type] ?? 0)
}

export function fromSnapshot(snapshot: AgentRun): RunView {
  const { blocks, snapshot_seq: seq, ...run } = snapshot
  const at = seq ?? run.last_event_seq
  return { run, blocks: blocks ?? [], seq: at, runSeq: at, outdated: false }
}

// A snapshot read can race the stream. One behind our events still knows the
// run's own fields if it is not behind the last run-level event.
export function mergeSnapshot(
  old: RunView | undefined,
  snapshot: AgentRun,
): RunView {
  const next = fromSnapshot(snapshot)
  // Usage arrives only as events; a snapshot never carries it.
  if (!old || BigInt(next.seq) >= BigInt(old.seq))
    return old?.usage ? { ...next, usage: old.usage } : next
  if (BigInt(next.seq) >= BigInt(old.runSeq))
    return { ...old, run: next.run, runSeq: next.seq }
  return old
}

function isRunState(value: unknown): value is AgentRun['state'] {
  return typeof value === 'string' && value in runLabels
}

export function applyEvent(view: RunView, event: RunEvent): Applied {
  // Only the envelope version gates merging; block payloads carry their own.
  if ((event.schema_version as number) !== 1) return 'outdated'
  const seq = BigInt(event.seq)
  if (seq <= BigInt(view.seq)) return view
  if (seq !== BigInt(view.seq) + 1n) return 'resync'
  if (event.type === 'block.created') {
    const block = event.payload as AgentBlock
    const last = view.blocks.at(-1)
    if (last && BigInt(block.ordinal) <= BigInt(last.ordinal)) return 'resync'
    return { ...view, seq: event.seq, blocks: [...view.blocks, block] }
  }
  if (event.type === 'block.updated') {
    const change = event.payload as BlockUpdated
    const index = view.blocks.findLastIndex(
      (block) => block.id === change.block_id,
    )
    const old = view.blocks[index] as AgentBlock | undefined
    if (!old || BigInt(change.revision) !== BigInt(old.revision) + 1n)
      return 'resync'
    const block: AgentBlock = { ...old, revision: change.revision }
    if (change.state) block.state = change.state
    if (change.finished_at) block.finished_at = change.finished_at
    if (change.payload_version) block.payload_version = change.payload_version
    if (change.patch?.set) block.payload = change.patch.set
    else if (change.patch?.append) {
      const { path, text } = change.patch.append
      const before = old.payload[path]
      block.payload = {
        ...old.payload,
        [path]: (typeof before === 'string' ? before : '') + text,
      }
    }
    const blocks = view.blocks.slice()
    blocks[index] = block
    return { ...view, seq: event.seq, blocks }
  }
  if (event.type === 'context.usage')
    return { ...view, seq: event.seq, usage: event.payload as ContextUsage }
  // Run-level events name only what changed; the caller re-reads the snapshot
  // for the rest (revision, waiting token, timestamps).
  const { state, error_code: code, settlement_state: settlement } =
    event.payload
  const run = { ...view.run }
  if (isRunState(state)) run.state = state
  if (typeof code === 'string' && code) run.error_code = code
  if (
    settlement === 'not_started' ||
    settlement === 'pending' ||
    settlement === 'settled' ||
    settlement === 'unknown'
  )
    run.settlement_state = settlement
  return { ...view, run, seq: event.seq, runSeq: event.seq }
}

export function isFinished(run: AgentRun): boolean {
  return ['succeeded', 'partial', 'failed', 'cancelled'].includes(run.state)
}

// A segment is the longest stretch of adjacent process blocks; any conclusion
// block ends it. It only ever grows at its tail, so its first block names it.
export type TurnItem =
  | { kind: 'block'; block: AgentBlock }
  | { kind: 'segment'; id: string; blocks: AgentBlock[] }

export function layout(blocks: AgentBlock[]): TurnItem[] {
  const items: TurnItem[] = []
  let run: AgentBlock[] = []
  const close = () => {
    if (run.length > 1) items.push({ kind: 'segment', id: run[0].id, blocks: run })
    else if (run.length) items.push({ kind: 'block', block: run[0] })
    run = []
  }
  for (const block of blocks) {
    if (block.hidden) continue
    if (processTypes.has(block.type)) {
      run.push(block)
      continue
    }
    close()
    items.push({ kind: 'block', block })
  }
  close()
  return items
}

export function seconds(ms: number): string {
  return `${Math.max(1, Math.round(ms / 1000))} 秒`
}

// The header of a folded segment. Time runs from its first block starting to
// its last one finishing, so waiting for the photographer is not counted.
export function segmentSummary(blocks: AgentBlock[]): string {
  const end = blocks.at(-1)?.finished_at
  const parts = [
    end
      ? `已处理 ${seconds(Date.parse(end) - Date.parse(blocks[0].started_at))}`
      : '处理中',
  ]
  const tools = blocks.filter((block) => block.type === 'tool')
  if (tools.length) parts.push(`${tools.length} 次操作`)
  const failed = tools.filter((block) => block.state === 'failed').length
  if (failed) parts.push(`${failed} 项失败`)
  if (blocks.some((block) => block.state === 'interrupted')) parts.push('已停止')
  return parts.join(' · ')
}

// The tail segment of a live run is the current work: its rows show and it has
// no header yet. Once something follows it or the run ends it folds, and from
// then on the photographer's choice stands.
export function segmentOpen(
  chosen: boolean | undefined,
  settled: boolean,
): boolean {
  return settled ? (chosen ?? false) : true
}

export function text(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

export interface PlanStep {
  key: string
  title: string
  state: string
}
export function planSteps(block: AgentBlock): PlanStep[] {
  const steps = block.payload.steps
  if (!Array.isArray(steps)) return []
  return steps.flatMap((step: unknown) =>
    typeof step === 'object' && step !== null
      ? [
          {
            key: text((step as Record<string, unknown>).key),
            title: text((step as Record<string, unknown>).title),
            state: text((step as Record<string, unknown>).state),
          },
        ]
      : [],
  )
}

export function nodeRefs(block: AgentBlock): string[] {
  const refs = block.payload.node_refs
  if (!Array.isArray(refs)) return []
  return refs.flatMap((ref: unknown) => {
    const id =
      typeof ref === 'object' && ref !== null
        ? text((ref as Record<string, unknown>).node_id)
        : ''
    return id ? [id] : []
  })
}

// What the status row says the run is doing right now.
export function currentStep(view: RunView): string {
  // Parked on a page job, its tool row stays open; the wait says more.
  if (view.run.state === 'waiting_client')
    return view.run.wait_reason === 'slot_busy'
      ? '等待其他助手运行结束'
      : runLabels.waiting_client
  const open = view.blocks.findLast(
    (block) => !block.hidden && !block.finished_at,
  )
  if (open?.type === 'tool') return text(open.payload.title) || '正在处理'
  if (open?.type === 'text') return '正在回复'
  if (open?.type === 'compaction') return '正在整理上下文'
  return view.run.state === 'running' ? '正在思考' : runLabels[view.run.state]
}
