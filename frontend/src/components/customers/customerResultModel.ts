import type { CustomerListResponse } from '../../api/client'

export type CustomerListItem = CustomerListResponse['items'][number]

const statusBadges: Record<string, [string, string]> = {
  active: ['badge badge-success', '活跃'],
  archived: ['badge badge-muted', '已归档'],
  merged: ['badge badge-muted', '已合并'],
}

export function customerStatusBadge(status: string): [string, string] {
  return statusBadges[status] ?? ['badge badge-muted', status]
}

export function shortCustomerDate(value: string) {
  return value.slice(0, 10)
}
