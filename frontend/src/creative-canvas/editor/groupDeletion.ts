import type { components } from '../../api/schema'
import type { Group } from './api.ts'
export function deleteGroupCommand(group: Group, hierarchy: string) {
  return {
    path: `/asset-groups/${encodeURIComponent(group.id)}/delete`,
    payload: {
      expected_revision: group.revision,
      hierarchy_revision: hierarchy,
    } satisfies components['schemas']['CreativeStructureDeletePayload'],
  }
}
export function groupRestoreCommand(result: unknown) {
  if (
    !result ||
    typeof result !== 'object' ||
    !('deletion_id' in result) ||
    typeof result.deletion_id !== 'string'
  )
    return null
  return {
    path: `/asset-groups/deletions/${encodeURIComponent(result.deletion_id)}/restore`,
    payload: {} satisfies components['schemas']['CreativeEmptyPayload'],
  }
}
