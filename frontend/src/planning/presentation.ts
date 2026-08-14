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
    if (error.code === 'customer_link_conflict') return '已关联其他客户，请先解除后再关联。'
    if (error.code === 'order_link_conflict') return '已关联其他订单，请先解除后再关联。'
    if (error.code === 'projection_revision_conflict') return '档期投影已变化，请刷新后重试。'
    if (error.code === 'projection_missing') return '当前没有可采纳的档期投影。'
    if (error.code === 'projection_not_active') return '当前档期投影不可采纳。'
    if (error.code === 'projection_not_future') return '档期已结束，不能作为未来拍摄时间。'
    if (error.code === 'source_changed') return '关联的客户或订单刚刚发生变化，请刷新后重试。'
    if (error.code === 'order_still_linked') return '请先解除订单关联再解除客户。'
    return error.message
  }
  return error instanceof Error ? error.message : fallback
}

export function shootPlanStatusLabel(status: ShootPlanStatus): string {
  return statusLabels[status]
}
