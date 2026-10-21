import { useEffect, useMemo } from 'react'
import { useStore } from 'zustand'
import { createStore, type StoreApi } from 'zustand/vanilla'
import { ApiError } from '../../api/transport.ts'
import { onCanvasInvalidate } from '../../creative-canvas/editor/canvasEvents.ts'
import { jobApi, leaseEveryMs, newEditorSessionID, pageJobKinds, type JobApi, type SceneJob } from './jobs.ts'
import { probeRender } from '../render/probe.ts'
import { LeaveGuard } from './leaveGuard.ts'
import { backgroundTicker, type Ticker } from './ticker.ts'

// The page's one claim loop for the assistant's scene jobs. It lives on the
// workspace page without three.js: the executor (sandbox, thumbnails,
// screenshots) loads with the first job. An open scene editor only asks for
// its scene to go first; two loops in one page would race each other. Tabs
// do not coordinate: whoever claims first runs the job, and the lease brings
// it back if that tab goes away.

/** The executor as loaded for this page. */
export type PageRunner = { run(job: SceneJob): Promise<unknown>; dispose(): void }

export type LauncherDeps = {
  api: JobApi
  ticker: Ticker
  /** Whether this device can run jobs; asked once, before the first claim. */
  probe(): Promise<boolean>
  /** Loads the executor; called on the first job. */
  load(): Promise<PageRunner>
}

export type HostState = {
  /** The job this page works on now. */
  holding: { id: string; documentID: string; kind: SceneJob['kind'] } | null
  /** The first job this page took over after another page let it go, for 「已继续助手的任务」. */
  resumed: string | null
  /** False when the device probe failed or the executor would not load: this page claims nothing. */
  capable: boolean
}

const throttleMs = 500

export class SceneJobLauncher {
  readonly store: StoreApi<HostState> = createStore<HostState>(() => ({ holding: null, resumed: null, capable: true }))
  private readonly deps: LauncherDeps
  private readonly preferred: string[] = []
  private probing: Promise<void> | null = null
  private tookOver = false
  private busy = false
  private again = false
  private stopped = true
  private runner: Promise<PageRunner> | null = null
  private throttled: (() => void) | null = null

  constructor(deps: LauncherDeps) {
    this.deps = deps
  }

  start(): () => void {
    this.stopped = false
    this.poke()
    return () => this.stop()
  }

  /** The open editor's scene: its jobs go before the account's oldest. */
  prefer(documentID: string): () => void {
    this.preferred.push(documentID)
    this.poke()
    return () => {
      const i = this.preferred.lastIndexOf(documentID)
      if (i >= 0) this.preferred.splice(i, 1)
    }
  }

  /** A canvas notification: something may have been queued. At most one claim per 500 ms. */
  invalidated(): void {
    if (this.throttled || this.stopped) return
    this.throttled = this.deps.ticker.after(throttleMs, () => {
      this.throttled = null
      this.poke()
    })
  }

  /** Claims until nothing waits, one job at a time; a nudge while busy claims again afterwards. */
  poke(): void {
    void this.loop()
  }

  private async loop() {
    if (this.stopped || !(await this.capable())) return
    if (this.busy) {
      this.again = true
      return
    }
    this.busy = true
    try {
      while (!this.stopped && this.store.getState().capable) {
        this.again = false
        let job: SceneJob | null
        try {
          job = await this.claim()
        } catch {
          // Offline or refused: the next notification, focus or reconnect tries again.
          break
        }
        if (job) await this.execute(job)
        else if (!this.again) break
      }
    } finally {
      this.busy = false
      if (this.stopped) this.release()
    }
  }

  private async capable(): Promise<boolean> {
    this.probing ??= this.deps.probe().then(
      (ok) => {
        if (!ok) this.store.setState({ capable: false })
      },
      () => this.store.setState({ capable: false }),
    )
    await this.probing
    return this.store.getState().capable
  }

  private async claim(): Promise<SceneJob | null> {
    const documentID = this.preferred.at(-1)
    if (documentID) {
      const job = await this.deps.api.claim({ documentID, kinds: pageJobKinds })
      if (job) return job
    }
    return this.deps.api.claim({ kinds: pageJobKinds })
  }

  private async execute(job: SceneJob) {
    const first = job.attempt > 0 && !this.tookOver
    if (first) this.tookOver = true
    this.store.setState({ holding: { id: job.id, documentID: job.document_id, kind: job.kind }, ...(first ? { resumed: job.id } : {}) })
    try {
      const runner = await this.loadWhileLeasing(job)
      if (runner) await runner.run(job)
    } finally {
      this.store.setState({ holding: null })
    }
  }

  /** The executor, renewing the lease meanwhile: on a slow network the chunk can outlast the 30-second lease. */
  private async loadWhileLeasing(job: SceneJob): Promise<PageRunner | null> {
    let lost = false
    const stop = this.deps.ticker.every(leaseEveryMs, () => {
      this.deps.api.lease(job.id, job.claim_token).catch((err: unknown) => {
        if (err instanceof ApiError && (err.status === 409 || err.status === 404)) lost = true
      })
    })
    try {
      this.runner ??= this.deps.load()
      const runner = await this.runner
      return lost ? null : runner
    } catch (err) {
      // A chunk that failed to load stays failed for this page: stop claiming,
      // and tell the assistant now instead of after three lease timeouts.
      this.store.setState({ capable: false })
      if (!lost) {
        const message = `页面没能加载渲染模块：${err instanceof Error ? err.message : String(err)}`
        await this.deps.api
          .complete(job.id, crypto.randomUUID(), new Date().toISOString(), { claim_token: job.claim_token, outcome: 'failed', error: { code: 'host_error', message } })
          .catch(() => {})
      }
      return null
    } finally {
      stop()
    }
  }

  /** Stops claiming; a job under way finishes first (leaving the page within the app does not cut it off). */
  private stop() {
    this.stopped = true
    this.throttled?.()
    this.throttled = null
    if (!this.busy) this.release()
  }

  private release() {
    const runner = this.runner
    this.runner = null
    void runner?.then(
      (r) => r.dispose(),
      () => {},
    )
  }
}

/** A renderer (WebGL2 or WebGPU), Workers, a precise pointer and a desktop-wide window: a tablet's canvas page would claim jobs only to fail them. */
export async function probeDevice(): Promise<boolean> {
  try {
    if (typeof Worker === 'undefined') return false
    if (!window.matchMedia('(pointer: fine)').matches || window.innerWidth < 1024) return false
    return await probeRender()
  } catch {
    return false
  }
}

export function pageLauncher(account: string): SceneJobLauncher {
  const api = jobApi(account, newEditorSessionID())
  return new SceneJobLauncher({
    api,
    ticker: backgroundTicker(),
    probe: probeDevice,
    load: () => import('./pageExecutor.ts').then((m) => m.createPageExecutor(api)),
  })
}

/**
 * The workspace page's renderer: one launcher for the account, nudged by
 * canvas notifications, focus and reconnects, and the close guard while it
 * holds a job.
 */
export function useSceneHost(account: string): { launcher: SceneJobLauncher; state: HostState } {
  const launcher = useMemo(() => pageLauncher(account), [account])
  const guard = useMemo(() => new LeaveGuard(), [])
  useEffect(() => {
    const stop = launcher.start()
    const visible = () => {
      if (!document.hidden) launcher.poke()
    }
    const poke = () => launcher.poke()
    const unsubscribe = onCanvasInvalidate(() => launcher.invalidated())
    document.addEventListener('visibilitychange', visible)
    window.addEventListener('online', poke)
    return () => {
      unsubscribe()
      document.removeEventListener('visibilitychange', visible)
      window.removeEventListener('online', poke)
      stop()
    }
  }, [launcher])
  useEffect(() => guard.start(), [guard])
  const state = useStore(launcher.store)
  useEffect(() => guard.setHolding(!!state.holding), [guard, state.holding])
  return { launcher, state }
}
