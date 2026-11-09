import { getAuthSnapshot } from '../../auth/session.ts'

// 页面切换会卸载整页；重挂载时先显示上次读到的结果，后台照常重读
// （stale-while-revalidate），列表不再每次都回到「正在加载」。
// 只存内存；键带会话纪元，换账号或登出后旧结果不再命中。超过上限时丢最久未写的。
// 本标签页做过创意写操作（invalidateReads）或结果超过 maxAge 就不再使用：
// 摄影师刚加的图、刚建的项目不会先「消失」一下再冒出来，久别回来也不先看到很旧的列表。

const maxAge = 5 * 60_000
let writes = 0

/** 创意写操作成功后调用：此前缓存的列表都不再显示，回到正常加载。 */
export function invalidateReads() {
  writes += 1
}

/** 读取开始时取一次，写回缓存时带上：读取期间有过写操作，结果就不存。 */
export function readStamp(): number {
  return writes
}

type Entry<T> = { value: T; writes: number; at: number }

export function readCache<T>(limit = 20) {
  const values = new Map<string, Entry<T>>()
  const key = (path: string) => `${getAuthSnapshot().sessionEpoch}:${path}`
  return {
    get(path: string): T | undefined {
      const entry = values.get(key(path))
      return entry &&
        entry.writes === writes &&
        Date.now() - entry.at < maxAge
        ? entry.value
        : undefined
    },
    set(path: string, value: T, stamp: number) {
      if (stamp !== writes) return
      const k = key(path)
      values.delete(k)
      values.set(k, { value, writes, at: Date.now() })
      for (const old of values.keys()) {
        if (values.size <= limit) break
        values.delete(old)
      }
    },
  }
}
