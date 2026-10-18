import type { After } from '../sandbox/host.ts'

export type { After }

// Timers for work that goes on in a hidden tab. Chrome slows a page's own
// timers to once a minute after it has been hidden for five minutes, which
// would stretch a 10-second lease renewal past the 30-second lease; timers
// inside a Worker keep their pace. Without Worker support the page's own
// timers are used.

export type Ticker = {
  /** Calls `fn` once after `ms`; the returned function cancels it. */
  after: After
  /** Resolves after `ms`; rejects early when the signal aborts. */
  wait(ms: number, signal?: AbortSignal): Promise<void>
  /** Calls `fn` every `ms` until the returned stop is called. */
  every(ms: number, fn: () => void): () => void
}


const workerSource = `
const timers = new Map()
onmessage = (e) => {
  const { id, ms } = e.data
  if (ms === undefined) { clearTimeout(timers.get(id)); timers.delete(id); return }
  timers.set(id, setTimeout(() => { timers.delete(id); postMessage(id) }, ms))
}
`

function pageAfter(ms: number, fn: () => void): () => void {
  const timer = setTimeout(fn, ms)
  return () => clearTimeout(timer)
}

function workerAfter(): After | null {
  let worker: Worker
  try {
    const url = URL.createObjectURL(new Blob([workerSource], { type: 'text/javascript' }))
    worker = new Worker(url)
    URL.revokeObjectURL(url)
  } catch {
    return null
  }
  const pending = new Map<number, () => void>()
  let seq = 0
  worker.onmessage = (e: MessageEvent<number>) => {
    const fn = pending.get(e.data)
    pending.delete(e.data)
    fn?.()
  }
  return (ms, fn) => {
    const id = ++seq
    pending.set(id, fn)
    worker.postMessage({ id, ms })
    return () => {
      if (pending.delete(id)) worker.postMessage({ id })
    }
  }
}

export function tickerFrom(after: After): Ticker {
  return {
    after,
    wait: (ms, signal) =>
      new Promise((resolve, reject) => {
        if (signal?.aborted) return reject(new Error('已取消'))
        const onAbort = () => {
          cancel()
          reject(new Error('已取消'))
        }
        const cancel = after(ms, () => {
          signal?.removeEventListener('abort', onAbort)
          resolve()
        })
        signal?.addEventListener('abort', onAbort, { once: true })
      }),
    every: (ms, fn) => {
      let cancel = () => {}
      let stopped = false
      const schedule = () => {
        cancel = after(ms, () => {
          if (stopped) return
          schedule()
          fn()
        })
      }
      schedule()
      return () => {
        stopped = true
        cancel()
      }
    },
  }
}

let shared: Ticker | null = null

/** One Worker-paced ticker per page. */
export function backgroundTicker(): Ticker {
  shared ??= tickerFrom(typeof Worker === 'undefined' ? pageAfter : (workerAfter() ?? pageAfter))
  return shared
}
