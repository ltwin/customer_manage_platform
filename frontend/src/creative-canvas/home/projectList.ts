import { useEffect, useState } from 'react'
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

  useEffect(() => {
    const visible = () => {
      if (document.visibilityState === 'visible') reload()
    }
    document.addEventListener('visibilitychange', visible)
    return () => document.removeEventListener('visibilitychange', visible)
  }, [])

  const live = !!page && hasRunning(page)
  useEffect(() => {
    if (!live) return
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') reload()
    }, pollInterval)
    return () => clearInterval(timer)
  }, [live])

  return { page, error: page ? '' : error, reload }
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
