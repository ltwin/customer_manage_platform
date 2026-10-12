// 首页一句话开始：建项目 → 建对话 → 外发授权 → 建运行。
// 后三步是助手面板的同一条待确认链，存进新画布自己的槽位；首页另存一条
// 本机记录，刷新后据此找回这次提交（design §4.3）。
import type { components } from '../../api/schema'
import {
  conversationSlot,
  conversationStep,
  messageSteps,
  type PendingAgentChain,
} from '../editor/agentState.ts'
import type { SavedAttachment } from './chatAttachments.ts'

type Segment = components['schemas']['CreativeAgentInstructionSegment']

export type HomeStart = {
  text: string
  model: string
  // Skill 的版本 ID；空串是普通对话。
  skill: string
  attachments: SavedAttachment[]
  // 项目回执已保存、journal 尚未清除时，仍可认出同一次创建。
  projectOperation?: string
  projectID?: string
  canvasID?: string
  name?: string
  // 被明确拒绝前已经建好的对话：改了再发时接着用，不再建一个。
  conversation?: string
  // 只记这条链的操作身份，用来认出画布槽位里是不是同一条链。
  chain?: { operations: string[] }
}

export const homeStartKey = (account: string) =>
  `creative-home-start:${account}`

const nameLimit = 30
// 项目名取这句话的第一行，去掉首尾空白后截到 30 个字，超出加「…」。
export function projectNameFrom(text: string): string {
  const line = text.trim().split('\n')[0].trim()
  const chars = [...line]
  if (!chars.length) return '未命名项目'
  return chars.length > nameLimit
    ? `${chars.slice(0, nameLimit).join('')}…`
    : line
}

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
const optional = (value: unknown) =>
  value === undefined || typeof value === 'string'
// 读不懂的记录当作没有：首页只是少了一次找回，画布槽位里的链不受影响。
export function readHomeStart(raw: string | null): HomeStart | null {
  if (!raw) return null
  let value: unknown
  try {
    value = JSON.parse(raw)
  } catch {
    return null
  }
  if (
    !record(value) ||
    typeof value.text !== 'string' ||
    typeof value.model !== 'string' ||
    typeof value.skill !== 'string' ||
    !Array.isArray(value.attachments) ||
    !value.attachments.every(
      (item: unknown) =>
        record(item) &&
        typeof item.id === 'string' &&
        typeof item.title === 'string' &&
        item.kind === 'image' &&
        optional(item.digest) &&
        (item.source === 'upload' || item.source === 'library'),
    ) ||
    !optional(value.projectOperation) ||
    !optional(value.projectID) ||
    !optional(value.canvasID) ||
    !optional(value.name) ||
    !optional(value.conversation) ||
    (value.projectID !== undefined && typeof value.canvasID !== 'string') ||
    (value.chain !== undefined &&
      (!record(value.chain) ||
        !Array.isArray(value.chain.operations) ||
        !value.chain.operations.every((op: unknown) => typeof op === 'string')))
  )
    return null
  return value as HomeStart
}

export type HomeRecovery =
  | { kind: 'none' }
  // 项目还没建：恢复输入框，像什么都没发生过。
  | { kind: 'draft'; start: HomeStart }
  // 项目已建好，消息还没发出：再发送时用这个项目。
  | { kind: 'project'; start: HomeStart }
  // 槽位里还是同一条链：结果未知，只能原样重放。
  | { kind: 'unknown'; start: HomeStart; chain: PendingAgentChain }
  // 链已经在别处处理掉了（画布里重发成功，或被拒后成了草稿）：清掉记录。
  | { kind: 'settled' }
export function recoverHomeStart(
  start: HomeStart | null,
  slot: PendingAgentChain | null,
): HomeRecovery {
  if (!start) return { kind: 'none' }
  if (!start.projectID) return { kind: 'draft', start }
  if (!start.chain) return { kind: 'project', start }
  // 链往前走时前面的步骤会被摘掉，最后一步（建运行）始终在。
  const last = slot?.steps[slot.steps.length - 1]?.operation
  if (slot && last && start.chain.operations.includes(last))
    return { kind: 'unknown', start, chain: slot }
  return { kind: 'settled' }
}

// 组装这次提交的待确认链。指令：有 Skill 时先放 skill_ref，然后是文字，
// 参考图按添加顺序作为 content_ref 追加在后面。
export function homeChain(
  start: HomeStart & { canvasID: string },
  model: { imageInput: boolean },
  skill?: { skill_id: string; skill_version_id: string },
): PendingAgentChain {
  const ids = start.attachments.map((item) => item.id)
  const segments: Segment[] = [
    ...(skill
      ? [
          {
            type: 'skill_ref' as const,
            skill_id: skill.skill_id,
            skill_version_id: skill.skill_version_id,
          },
        ]
      : []),
    { type: 'text', text: start.text },
    ...ids.map((id) => ({
      type: 'content_ref' as const,
      content_revision_id: id,
    })),
  ]
  return {
    steps: [
      ...(start.conversation ? [] : [conversationStep(start.canvasID)]),
      ...messageSteps(start.conversation || conversationSlot, {
        model: start.model,
        imageInput: model.imageInput,
        revisionIDs: ids,
        segments,
      }),
    ],
    // 在画布里被拒后，面板把这份草稿放回输入框；参考图以 @ 引用的形式带上。
    draft: {
      conversation: start.conversation ?? '',
      model: start.model,
      skill: start.skill,
      text: [start.text, ...ids.map((id) => `@[${id}]`)].join(' '),
      materials: start.attachments.map((item) => ({
        id: item.id,
        title: item.title,
        kind: 'image' as const,
      })),
    },
  }
}
