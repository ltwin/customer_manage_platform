import { useSyncExternalStore } from 'react'
import { getAuthSnapshot, subscribeAuth } from '../../auth/session.ts'
import { read, type Canvas, type CanvasNode } from './api.ts'
import type { GenerationCatalog } from './generation.ts'

export type CatalogState = Readonly<{
  catalog: GenerationCatalog | null
  error: string
}>
type Deps = {
  read: (signal: AbortSignal) => Promise<GenerationCatalog>
  epoch: () => number
  subscribeAuth: (listener: () => void) => () => void
  now: () => number
}

/** 回到前台时，超过这个时长没读过就在后台重读。 */
export const staleAfter = 5 * 60 * 1000
const empty: CatalogState = { catalog: null, error: '' }

/**
 * 账号级生成模型目录：整个工作区共享一份。进画布时预取，回到前台或生成
 * 被拒时在后台刷新；刷新失败时保留上一份，刷新期间界面一直用旧目录。
 *
 * 目录只属于读到它的那次登录会话：会话一变（退出、换账号、重新登录）
 * 立即清空，旧请求的结果和失败都不再写入。
 */
export function createGenerationCatalog(deps: Deps) {
  let state = empty
  let owner = deps.epoch()
  let flight: { controller: AbortController; promise: Promise<void> } | null =
    null
  let readAt = 0
  const listeners = new Set<() => void>()
  const publish = (next: CatalogState) => {
    state = next
    listeners.forEach((l) => l())
  }
  const sync = () => {
    const epoch = deps.epoch()
    if (epoch === owner) return
    owner = epoch
    flight?.controller.abort()
    flight = null
    readAt = 0
    if (state !== empty) publish(empty)
  }
  deps.subscribeAuth(sync)

  function load(): Promise<void> {
    sync()
    if (flight) return flight.promise
    const epoch = owner
    const controller = new AbortController()
    const current = () =>
      deps.epoch() === epoch && flight?.controller === controller
    const promise = deps
      .read(controller.signal)
      .then(
        (catalog) => {
          if (!current()) return
          readAt = deps.now()
          publish({ catalog, error: '' })
        },
        (e: unknown) => {
          if (!current() || state.catalog) return
          publish({
            catalog: null,
            error: e instanceof Error ? e.message : '模型目录读取失败',
          })
        },
      )
      .finally(() => {
        if (flight?.controller === controller) flight = null
      })
    flight = { controller, promise }
    return promise
  }

  return {
    /** 已有目录或正在读取时不重复请求。 */
    preload(): Promise<void> {
      sync()
      if (state.catalog) return Promise.resolve()
      return load()
    },
    refresh: load,
    refreshIfStale(): Promise<void> {
      sync()
      if (state.catalog && deps.now() - readAt < staleAfter)
        return Promise.resolve()
      return load()
    },
    subscribe(listener: () => void) {
      listeners.add(listener)
      return () => {
        listeners.delete(listener)
      }
    },
    getSnapshot: () => state,
  }
}

const shared = createGenerationCatalog({
  read: (signal) => read<GenerationCatalog>('/generation/catalog', signal),
  epoch: () => getAuthSnapshot().sessionEpoch,
  subscribeAuth,
  now: Date.now,
})

export const refreshGenerationCatalog = () => void shared.refresh()

/** 工作区挂载时调用：预取一次，并在回到前台时按需刷新。返回清理函数。 */
export function keepGenerationCatalogFresh(): () => void {
  void shared.preload()
  const onVisible = () => {
    if (document.visibilityState === 'visible') void shared.refreshIfStale()
  }
  document.addEventListener('visibilitychange', onVisible)
  return () => document.removeEventListener('visibilitychange', onVisible)
}

export function useGenerationCatalog(): CatalogState {
  return useSyncExternalStore(shared.subscribe, shared.getSnapshot)
}

export const modelsForNode = (
  catalog: GenerationCatalog | null,
  node: CanvasNode,
) =>
  catalog?.models.filter((m) => `core.${m.kind}` === node.metadata.type_key) ??
  []

/**
 * 节点不能生成的原因，空串表示可以。只用于展示和挡住生成键；
 * 提交时服务端仍按节点版本、画布状态、账号能力和模型注册复核。
 */
export function generationUnavailable(
  catalog: GenerationCatalog,
  canvas: Pick<Canvas, 'archived'>,
  node: CanvasNode,
): string {
  if (node.metadata.type_version !== 1) return '此节点版本暂不支持生成'
  if (canvas.archived) return '项目已归档，恢复后可生成'
  if (catalog.models.length === 0 && catalog.disabled_reason)
    return catalog.disabled_reason
  if (modelsForNode(catalog, node).length === 0)
    return '此节点类型尚未配置生成模型，仍可保存提示词和查看作品版本'
  return ''
}

/** 生成提交被服务端明确拒绝、且可能源于目录过期的错误码。 */
export const catalogRejections: readonly string[] = [
  'creative_generation_conflict',
  'creative_action_unavailable',
  'creative_generation_unavailable',
  'creative_generation_parameters',
]
