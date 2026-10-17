import type { components } from '../api/schema'
import { objectOfUnit } from './state/apply.ts'

export type RevisionItem = components['schemas']['CreativeDocumentRevisionItem']

export type HistoryGroup = {
  /** Newest first, as the list reads. */
  revisions: string[]
  latest: string
  actor: RevisionItem['actor_kind']
  runID: string | null
  from: string
  to: string
  /** Distinct objects changed or removed across the group. */
  objectCount: number
  label: string
}

const sameWindowMs = 2 * 60 * 1000
const actorName: Record<string, string> = { photographer: '你', agent: '助手', system: '系统' }

const whole = (i: RevisionItem) => i.restored_from !== null || i.changed_units.includes('*')

function joins(group: HistoryGroup, prev: RevisionItem, item: RevisionItem): boolean {
  // Restores and the creation stand alone, so each stays a point to go back to.
  if (whole(prev) || whole(item)) return false
  if (prev.actor_kind !== item.actor_kind) return false
  if (item.actor_kind === 'photographer')
    return Date.parse(prev.created_at) - Date.parse(item.created_at) <= sameWindowMs
  if (item.actor_kind === 'agent') return !!item.run_id && item.run_id === group.runID
  return false
}

function objectsOf(items: RevisionItem[]): number {
  const ids = new Set<string>()
  for (const item of items)
    for (const u of [...item.changed_units, ...item.removed_units]) {
      const unit = objectOfUnit(u)
      if (unit) ids.add(unit.id)
    }
  return ids.size
}

function labelOf(items: RevisionItem[], actor: string): string {
  const only = items.length === 1 ? items[0] : null
  if (only?.restored_from == null && only?.changed_units.includes('*')) return '新建场景'
  if (only?.restored_from != null)
    return only.changed_units.includes('*')
      ? `恢复到第 ${only.restored_from} 版`
      : `恢复了 ${only.changed_units.length + only.removed_units.length} 处`
  return `${actorName[actor] ?? actor}的修改`
}

/**
 * Groups a newest-first revision list: a photographer's revisions within two
 * minutes of each other, or one assistant run's revisions, form one group;
 * every restore and the creation stand alone.
 */
export function groupRevisions(items: readonly RevisionItem[]): HistoryGroup[] {
  const groups: { group: HistoryGroup; items: RevisionItem[] }[] = []
  for (const item of items) {
    const last = groups.at(-1)
    if (last && joins(last.group, last.items.at(-1)!, item)) {
      last.items.push(item)
      last.group.revisions.push(item.revision)
      last.group.from = item.created_at
      continue
    }
    groups.push({
      items: [item],
      group: { revisions: [item.revision], latest: item.revision, actor: item.actor_kind, runID: item.run_id, from: item.created_at, to: item.created_at, objectCount: 0, label: '' },
    })
  }
  return groups.map(({ group, items }) => ({ ...group, objectCount: objectsOf(items), label: labelOf(items, group.actor) }))
}
