import { createContext } from 'react'
import { create } from 'zustand'
import { ApiError } from '../../api/transport.ts'
import { watchStream } from '../../api/watchStream.ts'
import * as api from './api.ts'
import {
  applyEvent,
  isFinished,
  mergeSnapshot,
  type RunEvent,
  type RunView,
} from './agentBlocks.ts'
import type { AgentRun } from './agentState.ts'

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
}

export const useAgentRuns = create<RunsState>(() => ({
  runs: {},
  open: {},
  stopping: {},
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

// followRun reads the run once and, while it is unfinished, follows its event
// log. Calling it again for the same run is a no-op.
export function followRun(id: string) {
  if (followers.has(id)) return
  const controller = new AbortController()
  followers.set(id, controller)
  void follow(id, controller.signal).finally(() => {
    // A finished run keeps its entry: its snapshot never needs another read.
    if (controller.signal.aborted) followers.delete(id)
  })
}

// forgetRuns ends every stream and drops what was read, for when the canvas
// or the account changes.
export function forgetRuns() {
  for (const controller of followers.values()) controller.abort()
  followers.clear()
  useAgentRuns.setState({ runs: {}, open: {}, stopping: {} })
}

async function follow(id: string, signal: AbortSignal) {
  const load = async () => {
    const snapshot = await api.read<AgentRun>(
      `/agent-runs/${encodeURIComponent(id)}`,
      signal,
    )
    if (signal.aborted) return
    const old = useAgentRuns.getState().runs[id]
    put(id, { ...mergeSnapshot(old, snapshot), outdated: !!old?.outdated })
  }
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

// What rows need from the page around them. Only the rows that use it
// subscribe, so a canvas edit does not re-render the conversation.
export interface TurnContext {
  canvas?: api.Canvas | null
  // The page's account: restoring a scene from the run's change list writes as it.
  account?: string
  // Re-runs one failed read of a finished run; absent while it cannot be sent.
  retry?: (runID: string, stepID: string) => void
  // Titles of what the photographer referenced, for messages drawn in a run.
  titleOf?: (revisionID: string) => string | undefined
}
export const AgentTurnContext = createContext<TurnContext>({})
