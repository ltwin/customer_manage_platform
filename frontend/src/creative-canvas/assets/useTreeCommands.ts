import type { components } from '../../api/schema'
import type * as api from '../editor/api.ts'
import {
  deleteGroupCommand,
  groupRestoreCommand,
} from '../editor/groupDeletion.ts'
import { operation } from './operations.ts'
import { enter, type LibraryQuery } from './query.ts'
import {
  deleteConfirmation,
  tagCommand,
  type TagCommand,
} from './tagCommands.ts'
import type { useActions } from './useActions.ts'
import type { LibraryCatalog } from './useLibrary.ts'
import type { useOrganization } from './useOrganization.tsx'

/** 资料库树的整理动作：分组增删改、拖入分组、标签与分类。独立页与画布抽屉共用。 */
export function useTreeCommands({
  account,
  catalog,
  actions,
  organization,
  query,
  go,
  resolve,
}: {
  account: string
  catalog: LibraryCatalog | null
  actions: ReturnType<typeof useActions>
  organization: ReturnType<typeof useOrganization>
  query: LibraryQuery
  go: (next: LibraryQuery, push?: boolean) => void
  resolve: (ids: string[]) => api.Asset[]
}) {
  function onDeleteGroup(group: api.Group) {
    if (!catalog || actions.blocked) return
    const command = deleteGroupCommand(group, catalog.hierarchy)
    organization.setConfirm({
      title: `删除分组「${group.name}」？`,
      body: '素材保留在资产库，子分组移到上一级。',
      label: '删除分组',
      run: () =>
        void actions.execute({
          run: operation(account, command.path, command.payload),
          success: (result) => {
            if (query.group === group.id)
              go(enter(query, 'all', group.parent_id ?? ''), true)
            const undo = groupRestoreCommand(result)
            return {
              text: '已删除分组',
              ...(undo
                ? {
                    action: {
                      label: '撤销',
                      run: () =>
                        void actions.execute({
                          conflictText:
                            '分组已经有新的调整，无法撤销；请刷新后查看',
                          run: operation(account, undo.path, undo.payload),
                          success: () => ({ text: '已恢复分组与归类' }),
                        }),
                    },
                  }
                : {}),
            }
          },
        }),
    })
  }
  function onSaveGroup(
    group: api.Group | null,
    parent: string | null,
    name: string,
    done: () => void,
  ) {
    if (!catalog) return
    const path = group
      ? `/asset-groups/${encodeURIComponent(group.id)}/rename`
      : '/asset-groups'
    const payload = group
      ? ({
          name,
          expected_revision: group.revision,
          hierarchy_revision: catalog.hierarchy,
        } satisfies components['schemas']['CreativeGroupRenamePayload'])
      : ({
          name,
          parent_id: parent,
          hierarchy_revision: catalog.hierarchy,
        } satisfies components['schemas']['CreativeGroupCreatePayload'])
    void actions.execute({
      run: operation(account, path, payload),
      success: () => {
        done()
        return { text: group ? '已重命名分组' : '已新建分组' }
      },
    })
  }
  function onDropAssets(ids: string[], group: string) {
    if (actions.blocked) return
    const assets = resolve(ids)
    if (assets.length !== ids.length || assets.length > 100) {
      actions.setToast({ text: '一次最多处理 100 项，请重新选择素材' })
      return
    }
    organization.organize(assets, 'group', group, true)
  }
  function onTagCommand(command: TagCommand, done: () => void) {
    if (!catalog || actions.blocked) return
    const { path, payload, text } = tagCommand(command, catalog.tagHierarchy)
    const run = () =>
      void actions.execute({
        run: operation(account, path, payload),
        success: () => {
          done()
          if (
            command.kind === 'deleteTag' &&
            query.tags.includes(command.tag.id)
          )
            go({
              ...query,
              tags: query.tags.filter((t) => t !== command.tag.id),
            })
          return { text }
        },
      })
    if (command.kind === 'deleteTag' || command.kind === 'deleteCategory')
      organization.setConfirm({ ...deleteConfirmation(command), run })
    else run()
  }
  return { onDeleteGroup, onSaveGroup, onDropAssets, onTagCommand }
}
