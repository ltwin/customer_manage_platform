import type { AgentMaterial } from './agentReferences.ts'
import type { components } from '../../api/schema'

export type AgentRun = components['schemas']['CreativeAgentRun']
export const runLabels: Record<AgentRun['state'], string> = {
  queued: '排队中',
  running: '正在处理',
  waiting_input: '等待补充',
  waiting_apply: '等待采纳',
  // 页面正在执行助手派出的任务（烘焙模型、截图）。
  waiting_client: '等待页面执行',
  reconciling: '正在核实调用结果',
  succeeded: '已完成',
  partial: '部分完成',
  failed: '未完成',
  cancelled: '已取消',
}
export function isActiveRun(run: AgentRun): boolean {
  return ['queued', 'running', 'reconciling', 'waiting_client'].includes(
    run.state,
  )
}
export function hasUnsettledCost(run: AgentRun): boolean {
  return (
    run.settlement_state === 'unknown' || run.settlement_state === 'pending'
  )
}
// 回到一个会话时沿用它最近一次运行的模型；模型已下架或不可用就不沿用。
export function recalledModel(
  runs: readonly AgentRun[],
  conversation: string,
  models: readonly components['schemas']['CreativeAgentModel'][],
): string {
  let last: AgentRun | undefined
  for (const run of runs) {
    if (run.conversation_id !== conversation) continue
    if (!last || Date.parse(run.created_at) > Date.parse(last.created_at))
      last = run
  }
  return (
    models.find((m) => m.available && m.model_key === last?.model_key)
      ?.model_key ?? ''
  )
}
// 一条待确认链：每一步的请求在发出前就定好并存进本机，结果不确定时只能按
// 原操作身份重放（包括刷新之后），不能再起一次运行。建对话返回的 ID 填进后续
// 路径里的 {conversation}；外发授权返回的 ID 填进紧随其后那次运行的
// egress_consent_id。
export type AgentStep = {
  path: string
  operation: string
  createdAt: string
  payload: Record<string, unknown>
}
export type PendingAgentChain = { steps: AgentStep[]; draft?: AgentDraft }
export type AgentDraft = {
  conversation: string
  model: string
  skill: string
  text: string
  materials?: AgentMaterial[]
}
// 每张画布一个待确认槽位，每个标签页一份。
export const agentPendingKey = (account: string, canvasID: string) =>
  `creative-agent-pending:${account}:${canvasID}`
export const conversationSlot = '{conversation}'
// 新会话先用这个名字；第一次回答完成后由服务端按对话内容改名。
export const untitledConversation = '新对话'
type Segment = components['schemas']['CreativeAgentInstructionSegment']

export function agentStep(
  path: string,
  payload: Record<string, unknown>,
  createdAt = new Date().toISOString(),
): AgentStep {
  return { path, operation: crypto.randomUUID(), createdAt, payload }
}
export function stepBody(step: AgentStep): string {
  return JSON.stringify({
    operation_id: step.operation,
    client_created_at: step.createdAt,
    payload: step.payload,
  })
}
export function conversationStep(canvasID: string): AgentStep {
  return agentStep(`/canvases/${canvasID}/conversations`, {
    title: untitledConversation,
  })
}
// 发一条消息的两步：发送即授权，清单只放本条引用，另允许按需查库与读取本对话内容；首页和画布内
// 助手都用它组装，两处的外发规则因此不会走样。
export function messageSteps(
  conversation: string,
  message: {
    model: string
    imageInput: boolean
    revisionIDs: string[]
    segments: Segment[]
  },
): AgentStep[] {
  const createdAt = new Date().toISOString()
  return [
    agentStep(
      `/conversations/${conversation}/egress-consents`,
      {
        purpose: 'creative_assistance',
        mode: 'account_library',
        data_classes: [
          'text',
          'canvas_metadata',
          'media_metadata',
          ...(message.imageInput ? ['image_preview'] : []),
        ],
        selected_revision_ids: message.revisionIDs,
      },
      createdAt,
    ),
    agentStep(
      `/conversations/${conversation}/runs`,
      {
        model_key: message.model,
        egress_consent_id: '',
        instruction: {
          schema_version: 1,
          instruction_segments: message.segments,
        },
      },
      createdAt,
    ),
  ]
}
// 第一步完成后剩下的链；走完了返回 null。
export function advanceChain(
  chain: PendingAgentChain,
  reply: unknown,
): PendingAgentChain | null {
  const [done, ...rest] = chain.steps
  if (!rest.length) return null
  if (!record(reply) || typeof reply.id !== 'string' || !reply.id)
    throw new Error('服务端回执缺少编号，请保留记录')
  const id = reply.id
  let steps = rest
  let draft = chain.draft
  if (done.path.endsWith('/conversations')) {
    steps = steps.map((step) => ({
      ...step,
      path: step.path.replace(conversationSlot, id),
    }))
    if (draft) draft = { ...draft, conversation: id }
  } else if (done.path.endsWith('/egress-consents')) {
    const [run, ...more] = steps
    steps = [
      { ...run, payload: { ...run.payload, egress_consent_id: id } },
      ...more,
    ]
  }
  return { steps, ...(draft ? { draft } : {}) }
}
export function ownsPendingChain(
  storage: Pick<Storage, 'getItem'>,
  key: string,
  expected: PendingAgentChain | null,
): boolean {
  const saved = pendingAgentChain(storage.getItem(key))
  // Decode both sides: JSON object property order is not command identity.
  return (
    JSON.stringify(saved) ===
    JSON.stringify(
      pendingAgentChain(expected ? JSON.stringify(expected) : null),
    )
  )
}
export function replacePendingChain(
  storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>,
  key: string,
  expected: PendingAgentChain | null,
  next: PendingAgentChain | null,
): boolean {
  if (!ownsPendingChain(storage, key, expected)) return false
  if (next) storage.setItem(key, JSON.stringify(next))
  else storage.removeItem(key)
  return true
}
// 4xx（408、429 除外）说明服务端明确拒绝了这次操作；其余情况（断网、超时、
// 5xx、408、429）服务端可能已经受理，结果未知。
export function definitelyRejected(
  e: unknown,
): e is Error & { status: number; code?: string } {
  if (!(e instanceof Error) || !('status' in e)) return false
  const status = e.status
  return (
    typeof status === 'number' &&
    status >= 400 &&
    status < 500 &&
    status !== 408 &&
    status !== 429
  )
}
export type ChainSlot = {
  owns(chain: PendingAgentChain): boolean
  save(expected: PendingAgentChain, next: PendingAgentChain | null): boolean
}
export type ChainOutcome =
  | { kind: 'done'; reply: unknown }
  // 槽位里已经不是这条链（换了主人或已经卸载），什么都不做。
  | { kind: 'lost' }
  | {
      kind: 'rejected'
      error: Error & { code?: string }
      chain: PendingAgentChain
    }
  | { kind: 'unknown'; error: unknown; chain: PendingAgentChain }
// 从槽位里已存好的链开始逐步执行：每一步成功后先替换槽位里的链，再发下一步。
// 明确被拒就清掉链；结果未知时链原样留着，只能按原身份重放。
export async function performAgentChain(
  chain: PendingAgentChain,
  slot: ChainSlot,
  send: (path: string, body: string, operation: string) => Promise<unknown>,
): Promise<ChainOutcome> {
  let current = chain
  try {
    for (;;) {
      const step = current.steps[0]
      if (step.path.includes(conversationSlot))
        throw new Error('待发送对话身份缺失，请保留记录')
      const reply = await send(step.path, stepBody(step), step.operation)
      if (!slot.owns(current)) return { kind: 'lost' }
      const next = advanceChain(current, reply)
      if (!slot.save(current, next)) return { kind: 'lost' }
      if (!next) return { kind: 'done', reply }
      current = next
    }
  } catch (e) {
    if (!slot.owns(current)) return { kind: 'lost' }
    if (definitelyRejected(e)) {
      slot.save(current, null)
      return { kind: 'rejected', error: e, chain: current }
    }
    return { kind: 'unknown', error: e, chain: current }
  }
}
function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
const stepPath =
  /^\/(canvases\/[^/]+\/conversations|conversations\/[^/]+\/(egress-consents|runs)|agent-runs\/[^/]+\/(cancel|close-reconciliation|supplements|retry))$/
function readStep(value: unknown): AgentStep {
  if (
    !record(value) ||
    typeof value.path !== 'string' ||
    typeof value.operation !== 'string' ||
    !value.operation ||
    typeof value.createdAt !== 'string' ||
    !record(value.payload)
  )
    throw new Error('待确认操作记录损坏，请保留记录并重新加载')
  if (!stepPath.test(value.path)) throw new Error('待确认操作路径无效')
  return {
    path: value.path,
    operation: value.operation,
    createdAt: value.createdAt,
    payload: value.payload,
  }
}
function readDraft(d: unknown): AgentDraft {
  if (
    !record(d) ||
    typeof d.conversation !== 'string' ||
    typeof d.model !== 'string' ||
    typeof d.skill !== 'string' ||
    typeof d.text !== 'string'
  )
    throw new Error('待发送草稿损坏')
  if (
    d.materials !== undefined &&
    (!Array.isArray(d.materials) ||
      !d.materials.every(
        (m: unknown) =>
          record(m) &&
          typeof m.id === 'string' &&
          typeof m.title === 'string' &&
          (m.kind === 'text' || m.kind === 'image'),
      ))
  )
    throw new Error('待发送素材记录损坏')
  return {
    conversation: d.conversation,
    model: d.model,
    skill: d.skill,
    text: d.text,
    ...(d.materials ? { materials: d.materials as AgentMaterial[] } : {}),
  }
}
// 旧版本存的是「当前一步 + 可选的下一次运行」，转换成链；请求体逐字保留，
// 重放时与原请求一致。
function legacySteps(value: Record<string, unknown>): AgentStep[] {
  if (typeof value.path !== 'string' || typeof value.body !== 'string')
    throw new Error('待确认操作记录损坏，请保留记录并重新加载')
  const body: unknown = JSON.parse(value.body)
  if (
    !record(body) ||
    body.operation_id !== value.operation ||
    typeof body.client_created_at !== 'string' ||
    !record(body.payload)
  )
    throw new Error('待确认操作身份不一致')
  const steps = [
    readStep({
      path: value.path,
      operation: value.operation,
      createdAt: body.client_created_at,
      payload: body.payload,
    }),
  ]
  if (value.nextRun === undefined) return steps
  if (!value.path.endsWith('/egress-consents'))
    throw new Error('待确认操作路径无效')
  steps.push(
    readStep({
      path: value.path.replace(/\/egress-consents$/, '/runs'),
      operation: value.nextOperation,
      createdAt: value.nextCreatedAt,
      payload: value.nextRun,
    }),
  )
  return steps
}
export function pendingAgentChain(
  raw: string | null,
): PendingAgentChain | null {
  if (!raw) return null
  const value: unknown = JSON.parse(raw)
  if (!record(value))
    throw new Error('待确认操作记录损坏，请保留记录并重新加载')
  let steps: AgentStep[]
  if (value.steps === undefined) steps = legacySteps(value)
  else if (Array.isArray(value.steps) && value.steps.length)
    steps = value.steps.map(readStep)
  else throw new Error('待确认操作记录损坏，请保留记录并重新加载')
  // 只有第一步可能还要建对话，之后的步骤才会用到它的编号。
  if (steps[0].path.includes(conversationSlot))
    throw new Error('待发送对话身份缺失，请保留记录')
  return {
    steps,
    ...(value.draft !== undefined ? { draft: readDraft(value.draft) } : {}),
  }
}

// Every code the run can end with has its own words; the fallback is only for
// a server newer than this page.
const failures: Record<string, string> = {
  creative_run_cancelled: '已取消后续执行',
  creative_run_deadline_exceeded: '已超过本次运行期限',
  creative_run_interrupted: '运行被中断，请重新发送',
  creative_run_superseded: '这次运行已被新的操作取代',
  creative_reconciliation_closed: '已结束等待，费用仍会继续核实',
  creative_checkpoint_missing: '恢复记录不完整，已停止自动继续',
  creative_checkpoint_incompatible: '运行版本已变化，无法自动恢复',
  creative_checkpoint_inconsistent: '恢复记录不一致，已停止自动继续',
  creative_llm_unknown: '供应商是否完成调用尚未确认',
  creative_model_result_unknown: '供应商是否完成调用尚未确认',
  creative_model_providers_exhausted: '当前负载较高，请稍后尝试',
  creative_model_unavailable: '当前模型暂无可用的服务渠道，请换一个模型',
  creative_model_rejected: '模型服务拒绝了这次请求，请重试或换一个模型',
  creative_model_balance_exhausted: '模型服务额度不足，请稍后再试或换一个模型',
  creative_model_capability_missing: '当前模型不支持这次请求，请换一个模型',
  creative_model_empty_result: '模型没有给出回答，请重试',
  creative_model_turn_limit: '已达到本次思考轮数上限',
  creative_tool_limit: '模型一次发起的工具调用过多，已停止',
  creative_tool_not_allowed: '模型调用了未授权的工具，已停止',
  creative_egress_revoked: '外发授权已撤销',
  creative_egress_required: '这次请求需要新的外发授权',
  creative_skill_unavailable: '所选 Skill 已不可用',
  creative_llm_budget_exceeded: '本月模型额度已用完',
  creative_context_limit: '内容超过本次运行的长度上限',
  creative_input_unavailable: '引用的内容已不可读',
  creative_canvas_integrity: '画布数据校验未通过，已停止修改，请联系我们处理',
  creative_tool_failing: '同一工具多次出错，已停止执行，请稍后再试',
  creative_compaction_failed: '上下文整理没有完成，会话保持原样',
}
export function runFailure(code?: string): string {
  if (!code) return ''
  return failures[code] ?? '本次执行未能完成，请重试'
}
// 首页发送被明确拒绝时说明原因。参考图过期在服务端看来就是找不到。
const refusals: Record<string, string> = {
  archived_read_only: '项目已归档，恢复后才能发送',
  creative_segment_unsupported: '所选 Skill 或引用的内容暂不能使用',
}
export function sendRefusal(
  error: Error & { code?: string },
  hasImages: boolean,
): string {
  const code = error.code ?? ''
  if (code === 'not_found' && hasImages) return '参考图已过期，请重新添加'
  return failures[code] ?? refusals[code] ?? error.message
}

// 首次外发说明（V6-029）：第一次发送前弹窗说明一次，按账号和说明版本记在本机。
// 这条记录只决定弹不弹窗；每次发送照样按本条消息的范围建授权，由后端核对。
// 版本与后端 creativeagent.policyVersion 一致，两边一起改（单测会核对）。
// 助手要发送新的数据类型时改写说明并升级版本，所有账号会再看一次。
export const egressNoticeVersion = 'egress-2026-10-2'
const noticeKey = (account: string) => `creative-egress-notice:${account}`
// 读不到本机存储时当作没看过：宁可多弹一次，也不漏掉说明。
export function noticeSeen(storage: Storage | null, account: string): boolean {
  try {
    return storage?.getItem(noticeKey(account)) === egressNoticeVersion
  } catch {
    return false
  }
}
export function rememberNotice(storage: Storage | null, account: string) {
  try {
    storage?.setItem(noticeKey(account), egressNoticeVersion)
  } catch {
    // 记不住只是下次再弹一次。
  }
}
