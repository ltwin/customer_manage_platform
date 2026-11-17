import { createContext } from 'react'
import { create } from 'zustand'
import { ApiError } from '../../api/transport.ts'
import { watchStream } from '../../api/watchStream.ts'
import * as api from './api.ts'
import {
  applyEvent,
  blockTask,
  newerTask,
  isFinished,
  mergeSnapshot,
  type RunEvent,
  type RunView,
  type ToolTask,
} from './agentBlocks.ts'
import {
  agentStep,
  definitelyRejected,
  stepBody,
  type AgentRun,
  type AgentStep,
} from './agentState.ts'
import { notifyCanvasInvalidate } from './canvasEvents.ts'

// Runs live outside the panel: closing it does not stop following a run, and
// reopening it finds every block and every expanded row where they were.
interface RunsState {
  runs: Record<string, RunView | undefined>
  // Expanded/collapsed choices, keyed by block id (`segment:<first block id>`
  // for a segment). Absent means the photographer has not chosen.
  open: Record<string, boolean | undefined>
  // A stop the photographer asked for, until the run's own snapshot says it
  // ended: the view at the click, frozen as stopped, so text still streaming
  // in does not keep growing on screen. The stream folds into `runs` beneath.
  stopping: Record<string, RunView | undefined>
  taskControls: Record<string, TaskControl | undefined>
}

export const useAgentRuns = create<RunsState>(() => ({
  runs: {},
  open: {},
  stopping: {},
  taskControls: {},
}))

// shownRun is the view to draw: a stop on its way wins over the stream.
export function shownRun(state: RunsState, id: string): RunView | undefined {
  return state.stopping[id] ?? state.runs[id]
}

// freezeStopping shows the run as stopped from the click on. Its open blocks
// are interrupted the way the server closes them when the run ends.
export function freezeStopping(run: AgentRun) {
  useAgentRuns.setState((state) => {
    const view = state.runs[run.id] ?? {
      run,
      blocks: [],
      seq: '0',
      runSeq: '0',
      outdated: false,
    }
    const frozen: RunView = {
      ...view,
      run: { ...view.run, state: 'cancelled' },
      blocks: view.blocks.map((block) =>
        block.finished_at ? block : { ...block, state: 'interrupted' },
      ),
    }
    return { stopping: { ...state.stopping, [run.id]: frozen } }
  })
}

// releaseStopping goes back to the stream's view, as when the stop failed.
export function releaseStopping(id: string) {
  useAgentRuns.setState((state) => ({
    stopping: without(state.stopping, id),
  }))
}

function without<T>(record: Record<string, T>, id: string): Record<string, T> {
  return Object.fromEntries(
    Object.entries(record).filter(([key]) => key !== id),
  )
}

export function setOpen(id: string, open: boolean) {
  useAgentRuns.setState((state) => ({ open: { ...state.open, [id]: open } }))
}

// Once the run's own view has ended, a frozen stop gives way to it: what the
// server kept is what the conversation shows from then on.
function put(id: string, view: RunView) {
  useAgentRuns.setState((state) => ({
    runs: { ...state.runs, [id]: view },
    ...(isFinished(view.run) && state.stopping[id]
      ? { stopping: without(state.stopping, id) }
      : {}),
  }))
}

const pollMs = 2000
const followers = new Map<string, AbortController>()
let workspaceGeneration = 0

// followRun reads the run once and, while it is unfinished, follows its event
// log. Calling it again for the same run is a no-op.
export function followRun(id: string) {
  const current = useAgentRuns.getState().runs[id]
  if (followers.has(id) || (current && isFinished(current.run))) return
  const controller = new AbortController()
  followers.set(id, controller)
  void follow(id, controller.signal).finally(() => {
    if (followers.get(id) === controller) followers.delete(id)
  })
}

// forgetRuns ends every stream and drops what was read, for when the canvas
// or the account changes.
export function forgetRuns() {
  workspaceGeneration++
  for (const controller of followers.values()) controller.abort()
  followers.clear()
  useAgentRuns.setState({ runs: {}, open: {}, stopping: {}, taskControls: {} })
}

async function readRun(id: string, signal: AbortSignal) {
  const generation = workspaceGeneration
  const snapshot = await api.read<AgentRun>(
    `/agent-runs/${encodeURIComponent(id)}`,
    signal,
  )
  if (signal.aborted || generation !== workspaceGeneration) return
  const old = useAgentRuns.getState().runs[id]
  put(id, { ...mergeSnapshot(old, snapshot), outdated: !!old?.outdated })
}

export function refreshListedRuns(runs: AgentRun[], signal: AbortSignal) {
  // A canvas invalidation can arrive before (or during a break in) run SSE.
  // If the list has seen a newer event, read the blocks at that sequence.
  const cached = useAgentRuns.getState().runs
  for (const run of runs) {
    const view = cached[run.id]
    if (view && BigInt(run.last_event_seq) > BigInt(view.seq))
      void readRun(run.id, signal).catch(() => undefined)
  }
}

export function refreshTaskRuns(conversation: string, signal: AbortSignal) {
  // Canvas invalidation also covers finished runs: their event streams are
  // closed, but a task on one of their cards may still be running.
  const views = Object.values(useAgentRuns.getState().runs).filter(
    (view) => view?.run.conversation_id === conversation &&
      view.blocks.some((block) => blockTask(block)?.state === 'running'),
  )
  return Promise.allSettled(views.map((view) => readRun(view!.run.id, signal)))
}

async function follow(id: string, signal: AbortSignal) {
  const load = () => readRun(id, signal)
  const view = () => useAgentRuns.getState().runs[id]
  while (!view()) {
    try {
      await load()
    } catch (error) {
      if (signal.aborted) return
      if (error instanceof ApiError && [401, 403, 404].includes(error.status))
        return
      await new Promise((resolve) => setTimeout(resolve, pollMs))
    }
    if (signal.aborted) return
  }
  if (isFinished(view()!.run)) return

  const stream = new AbortController()
  signal.addEventListener('abort', () => stream.abort(), { once: true })
  // While the stream cannot connect, the snapshot is polled instead.
  let poll: ReturnType<typeof setInterval> | undefined
  const stopPolling = () => {
    clearInterval(poll)
    poll = undefined
  }
  const refresh = () =>
    load()
      .catch(() => undefined)
      .then(() => {
        const now = view()
        if (now && isFinished(now.run)) stream.abort()
      })
  try {
    await watchStream({
      connect: (cursor, connection) =>
        api.connectRunEvents(id, cursor, connection),
      signal: stream.signal,
      cursor: () => view()?.seq,
      onEvent: (frame) => {
        stopPolling()
        const old = view()
        if (!old) return 'snapshot'
        const event = JSON.parse(frame.data) as RunEvent
        const next = applyEvent(old, event)
        if (next === 'outdated') {
          // Nothing later can be merged either; fall back to snapshots.
          put(id, { ...old, outdated: true })
          stream.abort()
          return
        }
        if (next === 'resync') return 'snapshot'
        if (next !== old) put(id, next)
        if (event.type === 'background_tool.completed') notifyCanvasInvalidate()
        // Run-level events name only what changed; read the rest. Usage is
        // whole in its event and changes nothing a snapshot would show.
        if (!event.type.startsWith('block.') && event.type !== 'context.usage')
          void refresh()
      },
      onSnapshotRequired: load,
      onError: (error) => {
        if (error.terminal) return
        poll ??= setInterval(() => void refresh(), pollMs)
      },
    })
  } finally {
    stopPolling()
  }
  while (!signal.aborted && view()?.outdated && !isFinished(view()!.run)) {
    await new Promise((resolve) => setTimeout(resolve, pollMs))
    await load().catch(() => undefined)
  }
}

export type TaskControl = {
  action: 'detach' | 'cancel'
  step: AgentStep
  phase: 'sending' | 'unknown' | 'rejected' | 'confirmed'
  receipt?: ToolTask
  error?: string
}

function setTaskControl(handle: string, value: TaskControl) {
  useAgentRuns.setState((state) => ({
    taskControls: { ...state.taskControls, [handle]: value },
  }))
}

function currentTask(task: ToolTask): ToolTask {
  const view = useAgentRuns.getState().runs[task.run_id]
  const current = view?.blocks.map(blockTask).find((entry) => entry?.handle === task.handle)
  return newerTask(task, current)
}

function confirms(task: ToolTask, action: TaskControl['action']): boolean {
  return task.state !== 'running' || (action === 'detach' && task.mode === 'background')
}

export async function controlToolTask(
  account: string,
  task: ToolTask,
  action: TaskControl['action'],
) {
  const before = useAgentRuns.getState().taskControls[task.handle]
  if (before?.phase === 'sending') return
  const step = before?.phase === 'unknown' && before.action === action
    ? before.step
    : agentStep(`/agent-tool-tasks/${encodeURIComponent(task.handle)}/${action}`, {})
  const pending: TaskControl = { action, step, phase: 'sending', receipt: before?.receipt }
  setTaskControl(task.handle, pending)
  const owns = () => useAgentRuns.getState().taskControls[task.handle] === pending

  const send = () => api.send<ToolTask>(account, step.path, stepBody(step), step.operation, AbortSignal.timeout(8000))
  const refresh = () => readRun(task.run_id, AbortSignal.timeout(5000)).catch(() => undefined)
  try {
    let receipt: ToolTask
    try {
      receipt = await send()
    } catch (error) {
      if (definitelyRejected(error) || !owns()) throw error
      await refresh()
      if (!owns()) return
      const seen = currentTask(task)
      receipt = confirms(seen, action) ? seen : await send()
    }
    if (owns()) {
      setTaskControl(task.handle, { ...pending, phase: 'confirmed', receipt })
      notifyCanvasInvalidate()
    }
  } catch (error) {
    if (!owns()) return
    if (definitelyRejected(error)) {
      setTaskControl(task.handle, { ...pending, phase: 'rejected', error: error.message })
      void refresh()
      return
    }
    await refresh()
    if (!owns()) return
    const seen = currentTask(task)
    if (confirms(seen, action)) {
      setTaskControl(task.handle, { ...pending, phase: 'confirmed', receipt: seen })
      return
    }
    setTaskControl(task.handle, { ...pending, phase: 'unknown', error: '暂时无法确认结果，请重试确认。' })
  }
}

// What rows need from the page around them. Only the rows that use it
// subscribe, so a canvas edit does not re-render the conversation.
export interface TurnContext {
  canvas?: api.Canvas | null
  // The page's account: restoring a scene from the run's change list writes as it.
  account?: string
  taskReadOnly?: boolean
  // Re-runs one failed read of a finished run; absent while it cannot be sent.
  retry?: (runID: string, stepID: string) => void
  // Titles of what the photographer referenced, for messages drawn in a run.
  titleOf?: (revisionID: string) => string | undefined
}
export const AgentTurnContext = createContext<TurnContext>({})
