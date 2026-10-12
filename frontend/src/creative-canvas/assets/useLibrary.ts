import { useCallback, useEffect, useRef, useState } from 'react'
import type { components } from '../../api/schema'
import * as api from '../editor/api.ts'
import { errorMessage } from '../editor/queue.ts'
import { listPath, type LibraryQuery } from './query.ts'

export type Usage = components['schemas']['CreativeLibraryUsage']
export type Facets = components['schemas']['CreativeAssetFacets']
export type LibraryCatalog = {
  hierarchy: string
  tagHierarchy: string
  groups: api.Group[]
  counts: Record<string, number>
  tags: api.Tag[]
  categories: api.Category[]
  usage: Usage | null
}

const PAGE = 60

/**
 * 资料库树要的目录：分组（带计数）、标签、标签分类和用量。
 * refresh 变化时重读；读失败保留上一份，只报错。
 */
export function useCatalog(refresh: number) {
  const [catalog, setCatalog] = useState<LibraryCatalog | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    const controller = new AbortController()
    const { signal } = controller
    Promise.all([
      api.read<api.GroupPage>('/asset-groups', signal),
      api.read<api.TagPage>('/tags', signal),
      api.read<api.CategoryPage>('/tag-categories', signal),
      // 用量读不到不影响浏览，底部不显示就是了。
      api.read<Usage>('/library-usage', signal).catch(() => null),
    ])
      .then(([groups, tags, categories, usage]) => {
        if (signal.aborted) return
        setError('')
        setCatalog({
          hierarchy: groups.hierarchy_revision,
          tagHierarchy: tags.hierarchy_revision,
          groups: groups.items,
          counts: groups.asset_counts,
          tags: tags.items,
          categories: categories.items,
          usage,
        })
      })
      .catch((e: unknown) => {
        if (!signal.aborted) setError(errorMessage(e))
      })
    return () => controller.abort()
  }, [refresh])
  return { catalog, error }
}

type ListState = {
  key: string
  // 只有首屏读取成功后，才能使用这一轮刷新返回的游标。
  loadedRefresh: number
  page: api.AssetPage | null
  facets: Facets | null
  error: string
  more: 'idle' | 'loading' | 'error'
  // 翻页时资料库已经变了：已加载的部分和下一页对不上，需要从头再读。
  stale: boolean
}

/**
 * 当前条件的素材列表。首屏带计数（同一快照），滚动到底部附近再按游标取下一页。
 * 条件变化时用请求序号丢弃过期的响应；refresh 变化时原地重读，不先清空。
 */
export function useAssetList(query: LibraryQuery, refresh: number) {
  const key = listPath(query, PAGE)
  const [state, setState] = useState<ListState>({
    key,
    loadedRefresh: -1,
    page: null,
    facets: null,
    error: '',
    more: 'idle',
    stale: false,
  })
  const seq = useRef(0)
  const current = state.key === key ? state : null

  useEffect(() => {
    const controller = new AbortController()
    const mine = ++seq.current
    setState((s) =>
      s.key === key
        ? { ...s, error: '' }
        : // 计数先沿用上一次的，等新响应到了再换，避免类型分段和标签数量闪一下。
          {
            key,
            loadedRefresh: -1,
            page: null,
            facets: s.facets,
            error: '',
            more: 'idle',
            stale: false,
          },
    )
    api
      .read<api.AssetPage>(key, controller.signal)
      .then((page) => {
        if (mine !== seq.current) return
        setState({
          key,
          loadedRefresh: refresh,
          page,
          facets: page.facets ?? null,
          error: '',
          more: 'idle',
          stale: false,
        })
      })
      .catch((e: unknown) => {
        if (mine === seq.current && !controller.signal.aborted)
          setState((s) => ({ ...s, key, error: errorMessage(e) }))
      })
    return () => controller.abort()
  }, [key, refresh])

  const page = current?.page ?? null
  const busy = current?.more === 'loading'
  const ready = current?.loadedRefresh === refresh
  const loadMore = useCallback(() => {
    if (!page?.next_cursor || !ready || busy || current?.stale) return
    const mine = seq.current
    setState((s) => ({ ...s, more: 'loading' }))
    api
      .read<api.AssetPage>(listPath(query, PAGE, page.next_cursor))
      .then((next) => {
        if (mine !== seq.current) return
        setState((s) => {
          if (!s.page) return s
          if (next.library_revision !== s.page.library_revision)
            return { ...s, more: 'idle', stale: true }
          const seen = new Set(s.page.items.map((a) => a.id))
          return {
            ...s,
            more: 'idle',
            page: {
              ...s.page,
              items: [
                ...s.page.items,
                ...next.items.filter((a) => !seen.has(a.id)),
              ],
              next_cursor: next.next_cursor,
            },
          }
        })
      })
      .catch(() => {
        if (mine === seq.current) setState((s) => ({ ...s, more: 'error' }))
      })
  }, [page, ready, busy, current?.stale, query])

  return {
    page,
    facets: current?.facets ?? null,
    error: current?.error ?? '',
    loading: !page && !current?.error,
    more: current?.more ?? 'idle',
    stale: current?.stale ?? false,
    loadMore,
  }
}
