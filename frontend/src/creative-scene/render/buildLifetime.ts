/** Keeps a renderer and its resources alive until asynchronous compilation has stopped using them. */
export class BuildLifetime {
  private readonly pending = new Set<Promise<unknown>>()
  closed = false

  track<T>(work: Promise<T>): Promise<T> {
    this.pending.add(work)
    const done = () => { this.pending.delete(work) }
    void work.then(done, done)
    return work
  }

  async settled(): Promise<void> {
    while (this.pending.size > 0) await Promise.allSettled([...this.pending])
  }

  /** Runs immediately when idle, otherwise after every outstanding build and its cleanup. */
  release(cleanup: () => void): void | Promise<void> {
    if (this.pending.size === 0) cleanup()
    else return this.settled().then(cleanup)
  }

  close(cleanup: () => void): void | Promise<void> {
    if (this.closed) return
    this.closed = true
    return this.release(cleanup)
  }
}

const lifetimes = new WeakMap<object, BuildLifetime>()

export function buildLifetime(renderer: object): BuildLifetime {
  let lifetime = lifetimes.get(renderer)
  if (!lifetime) lifetimes.set(renderer, (lifetime = new BuildLifetime()))
  return lifetime
}
