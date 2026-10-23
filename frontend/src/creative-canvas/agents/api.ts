// 外部助手连接：接入页的列表与断开、画布顶栏的「已连接」指示、授权确认页的提交。
import type { components, paths } from '../../api/schema'
import { ApiError, request } from '../../api/transport.ts'

export type Connection = components['schemas']['ExternalAgentConnection']
export type ConnectionList = components['schemas']['ExternalAgentConnectionList']
type DecisionBody = paths['/oauth/authorize/decision']['post']['requestBody']['content']['application/json']
type DecisionResult = components['schemas']['OAuthAuthorizeDecisionResult']

// MCP_ENABLED 关着时列表接口是 404：页面显示「暂未开放」，不当成错误。
export function isUnavailable(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404
}

export function listConnections(
  query: { active?: boolean; includeRevoked?: boolean },
  signal?: AbortSignal,
): Promise<ConnectionList> {
  const params = new URLSearchParams()
  if (query.active) params.set('active', 'true')
  if (query.includeRevoked) params.set('include_revoked', 'true')
  const suffix = params.size ? `?${params.toString()}` : ''
  return request<ConnectionList>(`/creative/agent-connections${suffix}`, { signal, cache: 'no-store' })
}

export function revokeConnection(id: string): Promise<Connection> {
  return request<Connection>(`/creative/agent-connections/${encodeURIComponent(id)}/revoke`, { method: 'POST' })
}

export function decideAuthorization(body: DecisionBody): Promise<DecisionResult> {
  return request<DecisionResult>('/oauth/authorize/decision', { method: 'POST', body: JSON.stringify(body) })
}

// 活跃 = 有效且最近 5 分钟内调用过；服务端只在变为活跃时通知，到期由页面自己判断。
export const activityWindowMs = 5 * 60 * 1000

export function activeUntil(c: Connection): number | null {
  if (c.revoked_at || !c.last_used_at) return null
  return Date.parse(c.last_used_at) + activityWindowMs
}
