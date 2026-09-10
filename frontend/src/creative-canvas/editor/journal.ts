// The journal owns local recovery only. It never marks server data as saved.
export type Job = {
  path: string
  body: string
  operation: string
  state: 'ready' | 'unknown' | 'rejected'
  message: string
  draftKey?: string
  draftValue?: Draft
  position?: { key: string; token: string }
}
export type Draft = {
  value: string
  title: string
  kind: 'text' | 'link'
  dataRevision?: string
  contentRevision?: string | null
}
// A placement intent is separate from the immutable request currently in flight.
export type PositionDraft = {
  canvasID: string
  nodeID: string
  x: number
  y: number
  revision: string
  token: string
  synced: boolean
}
export type Journal = {
  version: 1
  job: Job | null
  drafts: Record<string, Draft>
  positions?: Record<string, PositionDraft>
}
export const emptyJournal = (): Journal => ({
  version: 1,
  job: null,
  drafts: {},
})

export interface JournalStorage {
  load(): Promise<Journal>
  save(value: Journal): Promise<void>
}
export function journalStorage(key: string): JournalStorage {
  async function database(): Promise<IDBDatabase> {
    return new Promise((resolve, reject) => {
      const open = indexedDB.open('creative-canvas-journal', 1)
      open.onupgradeneeded = () => {
        open.result.createObjectStore('journals')
      }
      open.onsuccess = () => resolve(open.result)
      open.onerror = () =>
        reject(new Error('无法打开本机草稿存储，请保留输入后重试'))
      open.onblocked = () =>
        reject(new Error('草稿存储被其他窗口占用，请关闭旧窗口后重试'))
    })
  }
  return {
    async load() {
      const db = await database()
      return new Promise<Journal>((resolve, reject) => {
        const tx = db.transaction('journals', 'readonly')
        const req = tx.objectStore('journals').get(key)
        tx.oncomplete = () => {
          db.close()
          const value = req.result as Journal | undefined
          if (
            value &&
            (value.version !== 1 ||
              !value.drafts ||
              typeof value.drafts !== 'object' ||
              !('job' in value) ||
              Object.values(value.drafts).some(
                (d) =>
                  !d ||
                  typeof d.value !== 'string' ||
                  typeof d.title !== 'string' ||
                  !['text', 'link'].includes(d.kind),
              ) ||
              (value.positions &&
                Object.values(value.positions).some(
                  (p) =>
                    !p ||
                    typeof p.canvasID !== 'string' ||
                    typeof p.nodeID !== 'string' ||
                    !Number.isFinite(p.x) ||
                    !Number.isFinite(p.y) ||
                    typeof p.revision !== 'string' ||
                    !/^[1-9][0-9]*$/.test(p.revision) ||
                    typeof p.token !== 'string' ||
                    typeof p.synced !== 'boolean',
                )) ||
              (value.job &&
                (typeof value.job.body !== 'string' ||
                  typeof value.job.operation !== 'string' ||
                  typeof value.job.path !== 'string' ||
                  !['ready', 'unknown', 'rejected'].includes(value.job.state))))
          ) {
            reject(new Error('本机草稿格式无法识别；请保留此浏览器数据'))
            return
          }
          resolve(value ?? emptyJournal())
        }
        tx.onabort = () => {
          db.close()
          reject(new Error('本机草稿读取失败'))
        }
      })
    },
    async save(value) {
      const db = await database()
      return new Promise<void>((resolve, reject) => {
        const tx = db.transaction('journals', 'readwrite')
        tx.objectStore('journals').put(structuredClone(value), key)
        tx.oncomplete = () => {
          db.close()
          resolve()
        }
        tx.onabort = () => {
          db.close()
          reject(new Error('本机草稿保存失败；请保留输入并确认原操作结果'))
        }
      })
    },
  }
}
