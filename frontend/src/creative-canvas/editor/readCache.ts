import { getAuthSnapshot } from '../../auth/session.ts'

// 页面切换会卸载整页；重挂载时先显示上次读到的结果，后台照常重读
// （stale-while-revalidate），列表不再每次都回到「正在加载」。
// 只存内存；键带会话纪元，换账号或登出后旧结果不再命中。超过上限时丢最久未写的。
export function readCache<T>(limit = 20) {
  const values = new Map<string, T>()
  const key = (path: string) => `${getAuthSnapshot().sessionEpoch}:${path}`
  return {
    get: (path: string): T | undefined => values.get(key(path)),
    set(path: string, value: T) {
      const k = key(path)
      values.delete(k)
      values.set(k, value)
      for (const old of values.keys()) {
        if (values.size <= limit) break
        values.delete(old)
      }
    },
  }
}
