// 分享协作区 409 的领域语义分流：只有 stale/revision 类冲突才表示
// 「页面已刷新，核对后重试可解决」；资格与状态类 409 保留各自的领域文案，
// 不再被「页面已刷新」话术误导性覆盖。

const STALE_REFRESH_CODES = new Set([
  'plan_revision_conflict',
  'projection_revision_conflict',
  'share_stale',
  'expiry_quote_stale',
  'feedback_stale',
  'shot_revision_conflict',
  'assignment_stale',
  'offer_stale',
  'source_changed',
])

export function isStaleRefreshCode(code: string): boolean {
  return STALE_REFRESH_CODES.has(code)
}

// 完整档资格说明（口径见 planshare/eligibility.go：订单状态 ∈
// scheduled/shot/selected/retouching/delivered/closed；取消后链接 404 不降级）。
export const fullViewEligibilityNote =
  '完整档资格：订单已定档（或之后的任何阶段）才能签发完整档；咨询中或已取消只能发方案概览。订单取消后完整档链接会直接失效，不会自动降级成概览。'

export function shareConflictMessage(code: string, serverMessage: string, refreshFallback: string): string {
  if (code === 'full_view_not_eligible') {
    return '还不能签发完整档：需要先关联客户与订单，且订单状态在「已定档」到「已完结」之间；咨询中或已取消只能发方案概览。可在「创作 brief」的 CRM 关联卡完成关联。'
  }
  if (isStaleRefreshCode(code)) return refreshFallback
  return serverMessage || refreshFallback
}
