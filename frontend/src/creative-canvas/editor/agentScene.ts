import type { components } from '../../api/schema'
import { ApiError } from '../../api/transport.ts'
import type { RestoreBatch, RunChange } from '../../creative-scene/api.ts'
import * as api from './api.ts'
import type { AgentBlock } from './agentBlocks.ts'

// What the panel shows of the scene tools: screenshots under render_view,
// and after a run, what it changed in each scene with a way back. Both come
// from tool details, read once per finished block.

export type Detail = components['schemas']['CreativeAgentBlockDetail']

/** The tools that write scenes; their arguments name the scene. */
export const sceneWriteTools: ReadonlySet<string> = new Set([
  'edit_scene',
  'write_model',
])

const details = new Map<string, Promise<Detail>>()

/** A finished tool block's arguments and result; a failed read is tried again next time. */
export function finishedDetail(
  block: Pick<AgentBlock, 'id' | 'run_id'>,
  read: <T>(path: string) => Promise<T> = api.read,
): Promise<Detail> {
  let detail = details.get(block.id)
  if (!detail) {
    detail = read<Detail>(
      `/agent-runs/${encodeURIComponent(block.run_id)}/blocks/${encodeURIComponent(block.id)}/detail`,
    )
    details.set(block.id, detail)
    detail.catch(() => details.delete(block.id))
  }
  return detail
}

function resultObject(detail: Detail): Record<string, unknown> | null {
  if (!detail.result) return null
  try {
    const value: unknown = JSON.parse(detail.result)
    return value && typeof value === 'object'
      ? (value as Record<string, unknown>)
      : null
  } catch {
    return null
  }
}

/** The screenshot render_view handed back, as a content revision. */
export function screenshotRevision(detail: Detail): string | null {
  const id = resultObject(detail)?.content_revision_id
  return typeof id === 'string' && id ? id : null
}

/** The scene a tool call worked on. */
export function documentOf(detail: Detail): string | null {
  const id = detail.arguments.document_id
  return typeof id === 'string' && id ? id : null
}

export const changeLabels: Record<RunChange['change'], string> = {
  added: '新增',
  removed: '删除',
  modified: '修改',
}

const actorNames = {
  photographer: '你',
  agent: '助手的其他运行',
  system: '系统',
} as const

/** Why a change cannot be put back on its own. */
export function blockedText(
  blocked: NonNullable<RunChange['blocked']>,
): string {
  switch (blocked.reason) {
    case 'changed_by_others':
      return `之后${actorNames[blocked.actor_kind]}又改过（第 ${blocked.revision} 版）`
    case 'restored_whole':
      return `之后整份恢复过（第 ${blocked.revision} 版）`
    case 'hierarchy_not_restorable':
      return '层级调整不能单独恢复，可以在历史版本里整份恢复'
  }
}

/** One restore_units command. */
export function restorePayload(batch: RestoreBatch) {
  return {
    expected: batch.expected,
    operations: [
      {
        op: 'restore_units',
        from_revision: batch.from_revision,
        units: batch.units,
      },
    ],
  }
}

export type RestoreOutcome = {
  /** Revisions written, one per batch that went through. */
  written: string[]
  total: number
  /** A batch met someone's newer edit; the rest were not sent. */
  conflict: boolean
  error: string | null
}

/**
 * Sends the batches in order and stops at the first that fails: undoing a
 * run in several batches is not atomic, and what is left must be read again
 * before anything else goes out.
 */
export async function submitBatches(
  batches: readonly RestoreBatch[],
  send: (batch: RestoreBatch) => Promise<{ revision: string }>,
): Promise<RestoreOutcome> {
  const written: string[] = []
  for (const batch of batches) {
    try {
      written.push((await send(batch)).revision)
    } catch (err) {
      const conflict = err instanceof ApiError && err.status === 409
      return {
        written,
        total: batches.length,
        conflict,
        error: conflict
          ? null
          : err instanceof Error
            ? err.message
            : '请稍后再试',
      }
    }
  }
  return { written, total: batches.length, conflict: false, error: null }
}

/** What the list says after restoring one part (with its name) or undoing the run. */
export function outcomeText(outcome: RestoreOutcome, name?: string): string {
  const part = outcome.written.length > 0
  if (name !== undefined) {
    if (outcome.error) return `没有恢复：${outcome.error}`
    if (outcome.conflict) return `「${name}」刚被修改，没有覆盖，清单已重新读取`
    return `已恢复「${name}」`
  }
  if (outcome.error)
    return part
      ? `撤销了一部分，其余没有完成：${outcome.error}`
      : `没有撤销：${outcome.error}`
  if (outcome.conflict)
    return part
      ? '撤销了一部分：其余的地方刚被修改，没有覆盖，清单已重新读取'
      : '这些地方刚被修改，没有撤销，清单已重新读取'
  return '已撤销本轮的改动'
}
