// 列表卡片三行结构的文案派生：数据来自列表投影的加性字段
// （crm_summary / execution_window_summary / execution_stats / readiness_summary）。

type ListCrmSummary = {
  customer_id: string | null
  customer_name: string | null
  order_id: string | null
  order_title: string | null
  order_status_at_link: string | null
}

const orderStatusLabels: Record<string, string> = {
  consulting: '咨询中',
  scheduled: '已定档',
  shot: '已拍摄',
  selected: '已选片',
  retouching: '精修中',
  delivered: '已交付',
  closed: '已完结',
  cancelled: '已取消',
}

export function crmSummaryLine(crm: ListCrmSummary | undefined): string {
  if (!crm || (!crm.customer_id && !crm.order_id)) return '无客户 · 无订单（灵感存档）'
  if (crm.order_title) {
    const status = crm.order_status_at_link
    const statusLabel = status ? (orderStatusLabels[status] ?? status) : ''
    return `关联订单 ${crm.order_title}${statusLabel ? `（${statusLabel}）` : ''}`
  }
  return crm.customer_name ? `客户 ${crm.customer_name}` : '无客户 · 无订单（灵感存档）'
}

export function windowRangeLabel(start: string, end: string, timeZone?: string): string {
  const startDate = new Date(start)
  const endDate = new Date(end)
  if (Number.isNaN(startDate.valueOf()) || Number.isNaN(endDate.valueOf())) return '执行时间待定'
  const options: Intl.DateTimeFormatOptions = { hour12: false, ...(timeZone ? { timeZone } : {}) }
  const day = new Intl.DateTimeFormat('en-CA', { ...options, month: '2-digit', day: '2-digit' }).format(startDate)
  const from = new Intl.DateTimeFormat('en-CA', { ...options, hour: '2-digit', minute: '2-digit' }).format(startDate)
  const to = new Intl.DateTimeFormat('en-CA', { ...options, hour: '2-digit', minute: '2-digit' }).format(endDate)
  return `${day} ${from}–${to}`
}

export function listStatusLines(plan: {
  public_scale: { planned_shot_count: number }
  execution_stats?: { captured_count: number; skipped_count: number }
  readiness_summary?: { required_total: number; required_unchecked: number }
}): string[] {
  const captured = plan.execution_stats?.captured_count ?? 0
  const skipped = plan.execution_stats?.skipped_count ?? 0
  const readiness = plan.readiness_summary
  const lines: string[] = []
  if (captured + skipped > 0) lines.push(`已捕获 ${captured} / 跳过 ${skipped}`)
  if (readiness && readiness.required_total > 0) {
    lines.push(`必需准备 ${readiness.required_total - readiness.required_unchecked}/${readiness.required_total} 已核对`)
  }
  if (plan.public_scale.planned_shot_count > 0 && captured + skipped === 0) {
    lines.push('尚未开始现场执行')
  }
  return lines
}
