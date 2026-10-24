// S0 group 2: build.worker.ts × the machine's cores, one geometry a worker at a time, and a prototype geometry
// cache in IndexedDB (Epic design「一次性开销与缓存」: one build per model revision per device).
import type { BuildJob, BuiltGeometry } from './build.worker.ts'

export class BuildPool {
  private workers: Worker[]
  private idle: Worker[]
  private queue: { job: BuildJob; resolve(b: BuiltGeometry): void; reject(e: unknown): void }[] = []

  constructor(readonly size = Math.max(2, navigator.hardwareConcurrency - 1)) {
    this.workers = Array.from({ length: size }, () => new Worker(new URL('./build.worker.ts', import.meta.url), { type: 'module' }))
    this.idle = [...this.workers]
  }

  run(job: BuildJob): Promise<BuiltGeometry> {
    return new Promise((resolve, reject) => {
      this.queue.push({ job, resolve, reject })
      this.next()
    })
  }

  private next() {
    while (this.idle.length && this.queue.length) {
      const worker = this.idle.pop()!
      const { job, resolve, reject } = this.queue.shift()!
      worker.onmessage = ({ data }: MessageEvent<BuiltGeometry>) => {
        this.idle.push(worker)
        resolve(data)
        this.next()
      }
      worker.onerror = (e) => {
        this.idle.push(worker)
        reject(new Error(e.message))
        this.next()
      }
      // Copied, not transferred: the raster keeps drawing the same arrays meanwhile.
      worker.postMessage(job)
    }
  }

  dispose() {
    this.workers.forEach((w) => w.terminate())
  }
}

const DB = 'scene-trace-spike'
const STORE = 'geometry'

function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB, 1)
    request.onupgradeneeded = () => request.result.createObjectStore(STORE)
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

function done<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

export const geometryCache = {
  async clear() {
    const db = await open()
    await done(db.transaction(STORE, 'readwrite').objectStore(STORE).clear())
    db.close()
  },
  async get(keys: string[]): Promise<Map<string, BuiltGeometry>> {
    const db = await open()
    const store = db.transaction(STORE, 'readonly').objectStore(STORE)
    const found = await Promise.all(keys.map((k) => done(store.get(k) as IDBRequest<BuiltGeometry | undefined>)))
    db.close()
    return new Map(found.flatMap((b, i) => (b ? [[keys[i], b] as const] : [])))
  },
  async put(built: BuiltGeometry) {
    const db = await open()
    await done(db.transaction(STORE, 'readwrite').objectStore(STORE).put(built, built.key))
    db.close()
  },
  async usage() {
    const estimate = await navigator.storage.estimate()
    return estimate.usage ?? 0
  },
}
