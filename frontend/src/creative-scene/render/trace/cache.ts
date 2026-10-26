import type { Segment } from './build.worker.ts'

// The local cache of built geometries (scene-world-space-gi「一次性开销与
// 缓存」, S1 design「构建、缓存与提交」): segments as the GPU takes them,
// so a second opening copies instead of building (D2). One IndexedDB
// database, read and written in the build workers only. It speeds things up
// and nothing more (L5): when it cannot be opened, read or written, or the
// quota is short, there is no cache and the build goes on.

/**
 * Bumped whenever what a key stands for changes: the BVH's build or
 * parameters, the segment's layout, the normal encoding, or how
 * models/merge.ts groups a model's parts (the n in model:<revision>#n).
 * 2: a root a group only for a mesh drawn a material a group, and each
 * root's group with it (S1 design A13).
 */
export const BUILDER_VERSION = 2
/** The build's parameters, as the key records them: SAH, leaves of 10, depth 40 (the library's defaults). */
export const BVH_PARAMS = 'sah-10-40'
/**
 * The libraries a segment comes out of, as installed: three's primitives
 * and loaders make the geometry, three-mesh-bvh the tree in its own packed
 * format, meshoptimizer the proxies. A cache hit never sees the geometry, so
 * a key from other versions is another key; a unit test holds these to the
 * installed ones (scripts/creative-scene-trace.test.ts).
 */
export const BUILT_WITH = { three: '0.186.1', 'three-mesh-bvh': '0.9.15', meshoptimizer: '1.1.1' }

/** The original's tree, or a proxy simplified to an error level (proxies are tier 2's alone in S1, D3). */
export type BuildKind = { kind: 'original' } | { kind: 'proxy'; level: number }

const builtWith = Object.entries(BUILT_WITH)
  .map(([name, version]) => `${name}@${version}`)
  .join(',')

export function cacheKey(build: BuildKind, geometryKey: string): string {
  const what = build.kind === 'original' ? 'raw' : `proxy@${build.level}`
  return `${what}:${geometryKey}:v${BUILDER_VERSION}:${BVH_PARAMS}:${builtWith}`
}

/** The cache's ceiling across every key (Epic「一次性开销与缓存」). */
export const CACHE_CAP = 2 * 2 ** 30

/** A cached segment in the ledger: its size and when it was last used (ms since the epoch). */
export type Entry = { key: string; bytes: number; used: number }

/**
 * The entries to drop, least recently used first, so that bytes more fit
 * under the ceiling and in the quota left; null when they would not fit
 * even with every entry dropped.
 */
export function evictions(entries: readonly Entry[], bytes: number, cap: number, free: number): string[] | null {
  let total = entries.reduce((n, e) => n + e.bytes, 0)
  let room = free
  const drop: string[] = []
  const oldest = [...entries].sort((a, b) => a.used - b.used)
  while (total + bytes > cap || bytes > room) {
    const e = oldest.shift()
    if (!e) return null
    drop.push(e.key)
    total -= e.bytes
    room += e.bytes
  }
  return drop
}

export const segmentBytes = (s: Segment) => s.nodes.byteLength + s.index.byteLength + s.vertices.byteLength + (s.colors?.byteLength ?? 0)

/** What a write needs to know first, read while the build runs. */
export type Room = { entries: Entry[]; free: number }

/** The cache as one build job uses it, open from the job's start to its end. */
export type Cache = {
  /** The segment kept under key, or null; a hit counts as a use. */
  read(key: string): Promise<Segment | null>
  /** The ledger and the quota left, asked for as the build starts; null when they cannot be read, and nothing is written. */
  room(): Promise<Room | null>
  /**
   * Keeps segment under key, dropping the least recently used to make room;
   * not at all when it does not fit. IndexedDB copies the arrays when this
   * is called, so they may be transferred as soon as it returns; the
   * promise settles once the write is done.
   */
  write(room: Room, key: string, segment: Segment): Promise<void>
  close(): void
}

/** Milliseconds since the epoch, for the ledger: the static check keeps Date.now() out of render/ (pictures never read the clock). */
const now = () => performance.timeOrigin + performance.now()

const DB = 'scene-trace'
const SEGMENTS = 'segments'
const LEDGER = 'ledger'

function done<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

function settled(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve()
    tx.onerror = tx.onabort = () => reject(tx.error)
  })
}

/** The cache for one job, in a worker; null when there is none to be had. */
export async function openCache(): Promise<Cache | null> {
  let db: IDBDatabase
  try {
    db = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open(DB, 1)
      request.onupgradeneeded = () => {
        request.result.createObjectStore(SEGMENTS)
        request.result.createObjectStore(LEDGER, { keyPath: 'key' })
      }
      request.onsuccess = () => resolve(request.result)
      request.onerror = () => reject(request.error)
    })
  } catch {
    return null
  }
  return {
    async read(key) {
      try {
        const found = (await done(db.transaction(SEGMENTS, 'readonly').objectStore(SEGMENTS).get(key))) as Segment | undefined
        if (!found) return null
        const tx = db.transaction(LEDGER, 'readwrite')
        tx.objectStore(LEDGER).put({ key, bytes: segmentBytes(found), used: now() } satisfies Entry)
        // The order a little off only drops another entry first.
        settled(tx).catch(() => undefined)
        return found
      } catch {
        return null
      }
    },
    async room() {
      try {
        const [estimate, entries] = await Promise.all([navigator.storage.estimate(), done(db.transaction(LEDGER, 'readonly').objectStore(LEDGER).getAll()) as Promise<Entry[]>])
        return { entries, free: (estimate.quota ?? 0) - (estimate.usage ?? 0) }
      } catch {
        return null
      }
    },
    write(room, key, segment) {
      try {
        const bytes = segmentBytes(segment)
        const drop = evictions(room.entries, bytes, CACHE_CAP, room.free)
        if (drop === null) return Promise.resolve()
        const tx = db.transaction([SEGMENTS, LEDGER], 'readwrite')
        for (const k of drop) {
          tx.objectStore(SEGMENTS).delete(k)
          tx.objectStore(LEDGER).delete(k)
        }
        tx.objectStore(SEGMENTS).put(segment, key)
        tx.objectStore(LEDGER).put({ key, bytes, used: now() } satisfies Entry)
        return settled(tx).catch(() => undefined)
      } catch {
        return Promise.resolve()
      }
    },
    close: () => db.close(),
  }
}
