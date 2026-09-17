import type { components } from '../../api/schema'

export type AgentRun = components['schemas']['CreativeAgentRun']
export const runLabels: Record<AgentRun['state'], string> = {
  queued: '排队中',
  running: '正在处理',
  waiting_input: '等待补充',
  waiting_apply: '等待采纳',
  reconciling: '正在核实调用结果',
  succeeded: '已完成',
  partial: '部分完成',
  failed: '未完成',
  cancelled: '已取消',
}
export function isActiveRun(run: AgentRun): boolean {
  return ['queued', 'running', 'reconciling'].includes(run.state)
}
export function hasUnsettledCost(run: AgentRun): boolean {
  return (
    run.settlement_state === 'unknown' || run.settlement_state === 'pending'
  )
}
export function pollDelay(hidden: boolean): number {
  return hidden ? 15000 : 2000
}

// Persist the exact command before sending it. An uncertain response may only
// retry this identity, including after refresh, never create a second run.
export type PendingAgentAction = {
  path: string
  operation: string
  body: string
  nextOperation?: string
  nextCreatedAt?: string
  draft?: AgentDraft
  nextRun?: components['schemas']['CreativeAgentRunPayload']
}
export type AgentDraft = {
  conversation: string
  model: string
  skill: string
  text: string
}
export function ownsPendingAction(
  storage: Pick<Storage, 'getItem'>,
  key: string,
  expected: PendingAgentAction | null,
): boolean {
  const saved = pendingAgentAction(storage.getItem(key))
  // Decode both sides: JSON object property order is not command identity.
  return (
    JSON.stringify(saved) ===
    JSON.stringify(
      pendingAgentAction(expected ? JSON.stringify(expected) : null),
    )
  )
}
export function replacePendingAction(
  storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>,
  key: string,
  expected: PendingAgentAction | null,
  next: PendingAgentAction | null,
): boolean {
  if (!ownsPendingAction(storage, key, expected)) return false
  if (next) storage.setItem(key, JSON.stringify(next))
  else storage.removeItem(key)
  return true
}
export function agentAction(
  path: string,
  payload: object,
  nextRun?: PendingAgentAction['nextRun'],
  draft?: AgentDraft,
): PendingAgentAction {
  const operation = crypto.randomUUID()
  const createdAt = new Date().toISOString()
  return {
    path,
    operation,
    body: JSON.stringify({
      operation_id: operation,
      client_created_at: createdAt,
      payload,
    }),
    ...(nextRun
      ? {
          nextRun,
          nextOperation: crypto.randomUUID(),
          nextCreatedAt: createdAt,
        }
      : {}),
    ...(draft ? { draft } : {}),
  }
}
export function consentedRunAction(
  command: PendingAgentAction,
  consentID: string,
): PendingAgentAction {
  if (!command.nextRun || !command.nextOperation || !command.nextCreatedAt)
    throw new Error('待发送运行身份缺失，请保留记录')
  return {
    path: command.path.replace(/\/egress-consents$/, '/runs'),
    operation: command.nextOperation,
    body: JSON.stringify({
      operation_id: command.nextOperation,
      client_created_at: command.nextCreatedAt,
      payload: { ...command.nextRun, egress_consent_id: consentID },
    }),
    ...(command.draft ? { draft: command.draft } : {}),
  }
}
function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
export function pendingAgentAction(
  raw: string | null,
): PendingAgentAction | null {
  if (!raw) return null
  const value: unknown = JSON.parse(raw)
  if (
    !record(value) ||
    typeof value.path !== 'string' ||
    typeof value.operation !== 'string' ||
    typeof value.body !== 'string'
  )
    throw new Error('待确认操作记录损坏，请保留记录并重新加载')
  if (
    !/^\/(canvases\/[^/]+\/conversations|conversations\/[^/]+\/(egress-consents|runs)|agent-runs\/[^/]+\/(cancel|close-reconciliation|supplements|retry))$/.test(
      value.path,
    )
  )
    throw new Error('待确认操作路径无效')
  const body: unknown = JSON.parse(value.body)
  if (
    !record(body) ||
    body.operation_id !== value.operation ||
    typeof body.client_created_at !== 'string' ||
    !record(body.payload)
  )
    throw new Error('待确认操作身份不一致')
  const pending: PendingAgentAction = {
    path: value.path,
    operation: value.operation,
    body: value.body,
  }
  if (value.draft !== undefined) {
    const d = value.draft
    if (
      !record(d) ||
      typeof d.conversation !== 'string' ||
      typeof d.model !== 'string' ||
      typeof d.skill !== 'string' ||
      typeof d.text !== 'string'
    )
      throw new Error('待发送草稿损坏')
    pending.draft = {
      conversation: d.conversation,
      model: d.model,
      skill: d.skill,
      text: d.text,
    }
  }
  if (value.nextRun !== undefined) {
    if (
      typeof value.nextOperation !== 'string' ||
      typeof value.nextCreatedAt !== 'string'
    )
      throw new Error('待发送运行身份缺失，请保留记录')
    pending.nextOperation = value.nextOperation
    pending.nextCreatedAt = value.nextCreatedAt
    const next = value.nextRun
    if (
      !record(next) ||
      typeof next.model_key !== 'string' ||
      typeof next.egress_consent_id !== 'string' ||
      !record(next.instruction) ||
      next.instruction.schema_version !== 1 ||
      !Array.isArray(next.instruction.instruction_segments)
    )
      throw new Error('待发送内容无法读取，请保留记录并重新加载')
    const segments: components['schemas']['CreativeAgentInstructionSegment'][] =
      []
    for (const segment of next.instruction.instruction_segments) {
      if (!record(segment)) throw new Error('待发送片段损坏')
      if (segment.type === 'text' && typeof segment.text === 'string')
        segments.push({ type: 'text', text: segment.text })
      else if (
        segment.type === 'skill_ref' &&
        typeof segment.skill_id === 'string' &&
        typeof segment.skill_version_id === 'string'
      )
        segments.push({
          type: 'skill_ref',
          skill_id: segment.skill_id,
          skill_version_id: segment.skill_version_id,
        })
      else throw new Error('待发送片段类型不可恢复')
    }
    pending.nextRun = {
      model_key: next.model_key,
      egress_consent_id: next.egress_consent_id,
      instruction: { schema_version: 1, instruction_segments: segments },
    }
  }
  return pending
}

export function runFailure(code?: string): string {
  if (!code) return ''
  const messages: Record<string, string> = {
    creative_run_cancelled: '已取消后续执行',
    creative_run_deadline_exceeded: '已超过本次运行期限',
    creative_reconciliation_closed: '已结束等待，费用仍会继续核实',
    creative_checkpoint_missing: '恢复记录不完整，已停止自动继续',
    creative_checkpoint_incompatible: '运行版本已变化，无法自动恢复',
    creative_checkpoint_inconsistent: '恢复记录不一致，已停止自动继续',
    creative_llm_unknown: '供应商是否完成调用尚未确认',
    creative_egress_revoked: '外发授权已撤销',
    creative_context_limit: '内容超过本次运行的长度上限',
    creative_tool_limit: '已达到本次工具调用上限',
  }
  return messages[code] ?? '本次执行未能完成，已有结果已保留'
}
export const stepLabels: Record<
  components['schemas']['CreativeAgentStepSummary']['state'],
  string
> = {
  prepared: '等待执行',
  dispatched: '已发送',
  succeeded: '完成',
  failed: '未完成',
  unknown: '待核实',
}
