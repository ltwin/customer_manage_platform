import { useCallback, useEffect, useRef, useState } from 'react'
import * as api from '../editor/api.ts'
import { mediaTicket, type MediaTicket } from '../editor/media.ts'
import { errorMessage } from '../editor/queue.ts'

export const emptyProjectPage: api.ProjectPage = {
  items: [],
  next_cursor: '',
  activity_counts: { running: 0, waiting: 0 },
}

// 侧栏「最近打开」与首页「继续创作」共用一次请求：按最近编辑取前 5 个。
export const recentLimit = 5

export const recentPath = `/projects?limit=${recentLimit}`

export function readRecentProjects(
  signal?: AbortSignal,
): Promise<api.ProjectPage> {
  return api.read<api.ProjectPage>(recentPath, signal)
}

// 有项目在运行时每 20 秒重读一次；只有等你确认的项目不轮询，它要等摄影师自己处理。
export const pollInterval = 20_000

export function hasRunning(page: api.ProjectPage): boolean {
  return page.activity_counts.running > 0 || page.items.some((p) => p.running)
}

// 切回标签页时重读；live 为真时每 20 秒轮询，标签页隐藏时跳过。
function useRefresh(live: boolean, reload: () => void) {
  const latest = useRef(reload)
  useEffect(() => {
    latest.current = reload
  })
  useEffect(() => {
    const visible = () => {
      if (document.visibilityState === 'visible') latest.current()
    }
    document.addEventListener('visibilitychange', visible)
    return () => document.removeEventListener('visibilitychange', visible)
  }, [])
  useEffect(() => {
    if (!live) return
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') latest.current()
    }, pollInterval)
    return () => clearInterval(timer)
  }, [live])
}

// 读一页项目：挂载时读，切回标签页时重读，有运行中的项目时轮询。
// 重读失败时保留已有的一页，只在一次都没读到时报错。
export function useProjectPage(path: string) {
  const [page, setPage] = useState<api.ProjectPage>()
  const [error, setError] = useState('')
  const [tick, setTick] = useState(0)
  const reload = () => setTick((value) => value + 1)

  useEffect(() => {
    const controller = new AbortController()
    api
      .read<api.ProjectPage>(path, controller.signal)
      .then((result) => {
        setPage(result)
        setError('')
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(e))
      })
    return () => controller.abort()
  }, [path, tick])

  useRefresh(!!page && hasRunning(page), reload)
  return { page, error: page ? '' : error, reload }
}

export type ProjectSort = 'edited_desc' | 'edited_asc' | 'name'
export const projectSorts: { value: ProjectSort; label: string }[] = [
  { value: 'edited_desc', label: '最近编辑' },
  { value: 'edited_asc', label: '最早编辑' },
  { value: 'name', label: '名称' },
]
export function parseSort(value: string | null): ProjectSort {
  return projectSorts.find((s) => s.value === value)?.value ?? 'edited_desc'
}

const pageSize = 30

export function projectListPath(
  archived: boolean,
  q: string,
  sort: ProjectSort,
  limit: number,
  cursor = '',
): string {
  const params = new URLSearchParams({
    archived: String(archived),
    sort,
    limit: String(limit),
  })
  if (q) params.set('q', q)
  if (cursor) params.set('cursor', cursor)
  return `/projects?${params}`
}

type Loaded = { key: string; value: api.ProjectPage }

// 项目页的分页列表。查询变化时从第一页读起；重读（切回标签页、轮询、写操作之后）
// 按已加载的数量一次读回，最多 100 个，不丢已经翻过的页。
export function useProjectList(
  archived: boolean,
  q: string,
  sort: ProjectSort,
) {
  const key = projectListPath(archived, q, sort, pageSize)
  const [loaded, setLoaded] = useState<Loaded>()
  const [error, setError] = useState('')
  const [moreLoading, setMoreLoading] = useState(false)
  const generation = useRef(0)
  const latest = useRef(loaded)
  useEffect(() => {
    latest.current = loaded
  })
  const page = loaded?.key === key ? loaded.value : undefined

  const read = useCallback(
    (reset: boolean) => {
      const id = ++generation.current
      const count =
        !reset && latest.current?.key === key
          ? latest.current.value.items.length
          : 0
      const limit = Math.min(100, Math.max(pageSize, count))
      api
        .read<api.ProjectPage>(projectListPath(archived, q, sort, limit))
        .then((value) => {
          if (id !== generation.current) return
          setLoaded({ key, value })
          setError('')
        })
        .catch((e: unknown) => {
          if (id === generation.current) setError(errorMessage(e))
        })
        .finally(() => {
          if (id === generation.current) setMoreLoading(false)
        })
    },
    [key, archived, q, sort],
  )
  useEffect(() => {
    read(true)
    return () => {
      generation.current += 1
    }
  }, [read])
  const reload = useCallback(() => read(false), [read])
  useRefresh(!!page && hasRunning(page), reload)

  async function more() {
    if (!page?.next_cursor || moreLoading) return
    const id = generation.current
    setMoreLoading(true)
    try {
      const value = await api.read<api.ProjectPage>(
        projectListPath(archived, q, sort, pageSize, page.next_cursor),
      )
      if (id !== generation.current) return
      setLoaded((previous) =>
        previous?.key === key
          ? {
              key,
              value: {
                ...value,
                items: [
                  ...previous.value.items,
                  ...value.items.filter(
                    (p) => !previous.value.items.some((old) => old.id === p.id),
                  ),
                ],
              },
            }
          : previous,
      )
    } catch (e) {
      if (id === generation.current) setError(errorMessage(e))
    } finally {
      if (id === generation.current) setMoreLoading(false)
    }
  }

  // 写操作成功后先在本机改掉这一项（改名、移出当前标签），再重读校正排序。
  function patch(change: (items: api.Project[]) => api.Project[]) {
    setLoaded((previous) =>
      previous?.key === key
        ? {
            key,
            value: { ...previous.value, items: change(previous.value.items) },
          }
        : previous,
    )
  }

  return { page, error, moreLoading, more, reload, patch }
}

// 卡片上的最近编辑时间。
export function editedAgo(iso: string, now = Date.now()): string {
  const at = new Date(iso)
  const minutes = Math.floor((now - at.getTime()) / 60_000)
  if (minutes < 1) return '刚刚编辑'
  if (minutes < 60) return `${minutes} 分钟前编辑`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} 小时前编辑`
  const days = Math.floor(hours / 24)
  if (days < 7) return `${days} 天前编辑`
  const sameYear = at.getFullYear() === new Date(now).getFullYear()
  return `${at.toLocaleDateString('zh-CN', {
    ...(sameYear ? {} : { year: 'numeric' }),
    month: 'long',
    day: 'numeric',
  })}编辑`
}

// 问候语：数量为 0 的那一半不写，两个都是 0 时只显示问候。
export function greeting(
  hour: number,
  name: string,
  counts: api.ProjectPage['activity_counts'],
): string {
  const greet =
    hour < 6
      ? '夜深了'
      : hour < 11
        ? '早上好'
        : hour < 13
          ? '中午好'
          : hour < 18
            ? '下午好'
            : '晚上好'
  const status = [
    counts.running > 0 && `${counts.running} 个项目在运行`,
    counts.waiting > 0 && `${counts.waiting} 个等你确认`,
  ]
    .filter(Boolean)
    .join('，')
  return `${greet}${name ? `，${name}` : ''}${status ? ` · ${status}` : ''}`
}

export type ProjectState = 'wait' | 'run' | null

// 展示规则（design D3）：等你确认优先于运行中。归档与轮询只看 running，不看这里。
export function projectState(project: api.Project): ProjectState {
  if (project.waiting) return 'wait'
  if (project.running) return 'run'
  return null
}

export function canvasPath(project: api.Project): string {
  return `/creative/canvases/${encodeURIComponent(project.default_canvas_id)}`
}

// 封面取图：先要 display 版本，没有再退回 original。和画布卡片共用票据缓存。
export function coverMedia(revision: string): Promise<MediaTicket> {
  return mediaTicket(revision, 'display', 'display').catch(() =>
    mediaTicket(revision, 'original', 'display'),
  )
}

// 缩略图取封面抽样里的第一张图片；没有就留空白方块。
export function firstImage(project: api.Project): string | undefined {
  return project.cover.nodes.find(
    (node) => node.kind === 'image' && node.content_revision_id,
  )?.content_revision_id
}
