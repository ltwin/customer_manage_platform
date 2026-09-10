import { useEffect, useState } from 'react'
import { journalStorage } from './journal.ts'
import { SaveQueue, errorMessage } from './queue.ts'
import * as api from './api.ts'

export function useJournal(account: string) {
  const [queue, setQueue] = useState<SaveQueue | null>(null)
  const [, redraw] = useState(0)
  const [error, setError] = useState('')
  useEffect(() => {
    let live = true
    const controller = new AbortController()
    const waiting = window.setTimeout(() => {
      if (live) setError('正在等待另一个窗口释放此草稿；关闭原窗口后会自动恢复')
    }, 1500)
    let activeQueue: SaveQueue | undefined
    let release: (() => void) | undefined
    const initialize = async () => {
      if (!navigator.locks)
        throw new Error('当前浏览器不支持安全的多窗口草稿，请使用新版浏览器')
      const tab =
        sessionStorage.getItem('creative-editor-tab') ?? crypto.randomUUID()
      sessionStorage.setItem('creative-editor-tab', tab)
      const acquire = async (): Promise<void> => {
        await navigator.locks.request(
          `creative-editor:${account}:${tab}`,
          { signal: controller.signal },
          async () => {
            if (!live) return
            window.clearTimeout(waiting)
            setError('')
            const q = new SaveQueue(
              journalStorage(`${account}:${tab}`),
              {
                send: (job) =>
                  api.send(account, job.path, job.body, job.operation),
                lookup: (operation) => {
                  if (api.currentAccount() !== account)
                    return Promise.reject(new Error('请重新登录原账号恢复保存'))
                  return api
                    .read<api.Receipt>(`/operations/${operation}`)
                    .then((receipt) => receipt.response)
                },
              },
              () => {
                if (live) redraw((n) => n + 1)
              },
            )
            activeQueue = q
            await q.load()
            if (!live) return
            setQueue(q)
            await new Promise<void>((resolve) => {
              release = resolve
            })
          },
        )
      }
      await acquire()
    }
    void initialize().catch((e: unknown) => {
      if (live && !controller.signal.aborted) setError(errorMessage(e))
    })
    return () => {
      live = false
      window.clearTimeout(waiting)
      controller.abort()
      if (activeQueue) void activeQueue.close().then(() => release?.())
      else release?.()
    }
  }, [account])
  return { queue, error }
}
