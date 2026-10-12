import { useEffect, useRef, useState } from 'react'
import * as api from '../editor/api.ts'
import type { components } from '../../api/schema'
import { errorMessage } from '../editor/queue.ts'
import { conflict, operation, rejected } from './operations.ts'

export type Toast = {
  text: string
  action?: { label: string; run: () => void }
}
type Task = {
  readOnly?: boolean
  conflictText?: string
  retryRejected?: boolean
  run: () => Promise<unknown>
  optimistic?: api.Asset[]
  before?: api.Asset[]
  success: (result: unknown) => Toast
}
/** 页面内一次只提交一个动作；未知结果保留原请求供重试，防止后续修改越过它。 */
export function useActions(account: string, reload: () => void) {
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<Toast | null>(null)
  const [overrides, setOverrides] = useState<Map<string, api.Asset>>(new Map())
  const pending = useRef<Task | null>(null)
  const running = useRef(false)
  const live = useRef(true)
  useEffect(() => {
    live.current = true
    const leave = (e: BeforeUnloadEvent) => {
      if (pending.current || running.current) {
        e.preventDefault()
        e.returnValue = ''
      }
    }
    window.addEventListener('beforeunload', leave)
    return () => {
      live.current = false
      window.removeEventListener('beforeunload', leave)
    }
  }, [])
  function patch(assets: api.Asset[]) {
    if (live.current)
      setOverrides((old) => {
        const next = new Map(old)
        for (const a of assets) next.set(a.id, a)
        return next
      })
  }
  async function execute(task: Task) {
    if (running.current || (pending.current && pending.current !== task)) return
    running.current = true
    pending.current = task
    setBusy(true)
    if (task.optimistic) patch(task.optimistic)
    try {
      const result = await task.run()
      if (!live.current || api.currentAccount() !== account) return
      pending.current = null
      setToast(task.success(result))
      // 写入成功后的重读失败不改变写入结果，也不生成第二次写入。
      if (task.before?.length) {
        const fresh = await Promise.allSettled(
          task.before.map((a) =>
            api.read<api.Asset>(`/assets/${encodeURIComponent(a.id)}`),
          ),
        )
        patch(fresh.flatMap((r) => (r.status === 'fulfilled' ? [r.value] : [])))
      }
      reload()
    } catch (e) {
      if (!live.current) return
      if (task.before) patch(task.before)
      if (task.readOnly || rejected(e)) {
        pending.current = null
        if (task.before) {
          const fresh = await Promise.allSettled(
            task.before.map((a) =>
              api.read<api.Asset>(`/assets/${encodeURIComponent(a.id)}`),
            ),
          )
          patch(
            fresh.flatMap((r) => (r.status === 'fulfilled' ? [r.value] : [])),
          )
        }
        reload()
      }
      setToast({
        text: conflict(e)
          ? (task.conflictText ?? '已在别处修改，已刷新')
          : errorMessage(e),
        ...(!task.readOnly && (!rejected(e) || task.retryRejected)
          ? { action: { label: '重试', run: () => void execute(task) } }
          : {}),
      })
    } finally {
      running.current = false
      if (live.current) setBusy(false)
    }
  }
  function mutate(
    assets: api.Asset[],
    path: string,
    payload: unknown,
    optimistic: (a: api.Asset) => api.Asset,
    text: string,
  ) {
    if (
      assets.length < 1 ||
      assets.length > 100 ||
      assets.some((a) => a.deleted_at)
    )
      return
    void execute({
      run: operation(account, path, payload),
      before: assets,
      optimistic: assets.map(optimistic),
      success: () => ({ text }),
    })
  }
  function rate(assets: api.Asset[], value: number | null) {
    if (
      !assets.length ||
      assets.length > 100 ||
      assets.some((a) => a.deleted_at)
    )
      return
    const payload: components['schemas']['CreativeAssetRatePayload'] = {
      items: assets.map((a) => ({
        asset_id: a.id,
        expected_revision: a.revision,
        rating: value,
      })),
    }
    const run = operation<components['schemas']['CreativeAssetRateResult']>(
      account,
      '/assets/batch-rate',
      payload,
    )
    let result: components['schemas']['CreativeAssetRateResult']
    void execute({
      before: assets,
      optimistic: assets.map((a) => ({ ...a, rating: value })),
      run: async () => {
        result = await run()
      },
      success: () => ({
        text:
          value === null ? '已清除评分' : `${assets.length} 项评为 ${value} 星`,
        action: {
          label: '撤销',
          run: () => {
            const after = assets.map((a) => ({
              ...a,
              rating: value,
              revision: result.items.find((r) => r.id === a.id)!.revision,
            }))
            void execute({
              before: after,
              optimistic: assets.map((a, i) => ({
                ...a,
                revision: after[i]!.revision,
              })),
              run: operation(account, '/assets/batch-rate', {
                items: assets.map((a, i) => ({
                  asset_id: a.id,
                  expected_revision: after[i]!.revision,
                  rating: a.rating,
                })),
              } satisfies components['schemas']['CreativeAssetRatePayload']),
              success: () => ({ text: '已撤销评分' }),
            })
          },
        },
      }),
    })
  }
  return {
    busy,
    isBlocked: () => running.current || !!pending.current,
    blocked: busy || !!pending.current,
    toast,
    setToast: (notice: Toast | null) => {
      if (!pending.current && !running.current) setToast(notice)
    },
    overrides,
    execute,
    mutate,
    rate,
  }
}
