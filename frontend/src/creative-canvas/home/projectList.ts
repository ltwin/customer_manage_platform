import * as api from '../editor/api.ts'

export const emptyProjectPage: api.ProjectPage = {
  items: [],
  next_cursor: '',
  activity_counts: { running: 0, waiting: 0 },
}

// 侧栏「最近打开」与首页「继续创作」共用一次请求：按最近编辑取前 5 个。
export const recentLimit = 5

export function readRecentProjects(
  signal?: AbortSignal,
): Promise<api.ProjectPage> {
  return api.read<api.ProjectPage>(`/projects?limit=${recentLimit}`, signal)
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

// 缩略图取封面抽样里的第一张图片；没有就留空白方块。
export function firstImage(project: api.Project): string | undefined {
  return project.cover.nodes.find(
    (node) => node.kind === 'image' && node.content_revision_id,
  )?.content_revision_id
}
