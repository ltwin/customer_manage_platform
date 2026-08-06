import { ApiError } from '../api/client'
import type { ShootPlanStatus } from './api'

const statusLabels: Record<ShootPlanStatus, string> = {
  draft: '草稿',
  ready: '已就绪',
  in_progress: '拍摄中',
  completed: '已完成',
  archived: '已归档',
}

export function planningErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    if (error.code === 'plan_revision_conflict') {
      return '策划已在其他页面更新，已保留你的输入；请核对最新版本后重新保存。'
    }
    if (error.code === 'execution_revision_conflict') return '执行结果已变化，请刷新执行历史后重试。'
    return error.message
  }
  return error instanceof Error ? error.message : fallback
}

export function shootPlanStatusLabel(status: ShootPlanStatus): string {
  return statusLabels[status]
}
