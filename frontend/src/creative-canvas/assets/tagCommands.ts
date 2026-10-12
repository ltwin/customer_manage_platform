import type { components } from '../../api/schema'
import type * as api from '../editor/api.ts'

type Schemas = components['schemas']
/** 左栏标签树发出的整理动作，由页面统一提交。 */
export type TagCommand =
  | { kind: 'createTag'; name: string; category: string | null }
  | {
      kind: 'editTag'
      tag: api.Tag
      patch: Partial<Pick<api.Tag, 'name' | 'color' | 'category_id'>>
    }
  | { kind: 'deleteTag'; tag: api.Tag }
  | { kind: 'createCategory'; name: string }
  | { kind: 'renameCategory'; category: api.Category; name: string }
  | { kind: 'deleteCategory'; category: api.Category; tags: number }

export const NEW_TAG_COLOR = '#a1a1a7'
const key = (name: string) => name.normalize('NFKC').trim().toLowerCase()

/** 标签名在整个资产库里唯一（后端按规范化名称复用），分类名在界面上也不重名。 */
export function nameTaken(
  items: { id: string; name: string }[],
  name: string,
  except?: string,
) {
  return items.some((i) => i.id !== except && key(i.name) === key(name))
}

export function tagCommand(c: TagCommand, hierarchy: string) {
  const id = (v: { id: string }) => encodeURIComponent(v.id)
  switch (c.kind) {
    case 'createTag':
      return {
        path: '/tags',
        payload: {
          name: c.name,
          color: NEW_TAG_COLOR,
          category_id: c.category,
          hierarchy_revision: hierarchy,
        } satisfies Schemas['CreativeTagCreatePayload'],
        text: `已新建标签「${c.name}」`,
      }
    case 'editTag': {
      const next = { ...c.tag, ...c.patch }
      return {
        path: `/tags/${id(c.tag)}/edit`,
        payload: {
          name: next.name,
          color: next.color,
          category_id: next.category_id,
          expected_revision: c.tag.revision,
          hierarchy_revision: hierarchy,
        } satisfies Schemas['CreativeTagEditPayload'],
        text:
          c.patch.name !== undefined
            ? '已重命名标签'
            : c.patch.color !== undefined
              ? '已修改标签颜色'
              : '已移动标签',
      }
    }
    case 'deleteTag':
      return {
        path: `/tags/${id(c.tag)}/delete`,
        payload: {
          expected_revision: c.tag.revision,
          hierarchy_revision: hierarchy,
        } satisfies Schemas['CreativeStructureDeletePayload'],
        text: `已删除标签「${c.tag.name}」`,
      }
    case 'createCategory':
      return {
        path: '/tag-categories',
        payload: {
          name: c.name,
          hierarchy_revision: hierarchy,
        } satisfies Schemas['CreativeCategoryCreatePayload'],
        text: `已新建分类「${c.name}」`,
      }
    case 'renameCategory':
      return {
        path: `/tag-categories/${id(c.category)}/edit`,
        payload: {
          name: c.name,
          expected_revision: c.category.revision,
          hierarchy_revision: hierarchy,
        } satisfies Schemas['CreativeCategoryEditPayload'],
        text: '已重命名分类',
      }
    case 'deleteCategory':
      return {
        path: `/tag-categories/${id(c.category)}/delete`,
        payload: {
          expected_revision: c.category.revision,
          hierarchy_revision: hierarchy,
        } satisfies Schemas['CreativeStructureDeletePayload'],
        text: `已删除分类「${c.category.name}」`,
      }
  }
}

/** 删除前的确认文案：说清楚会影响什么、能不能恢复。 */
export function deleteConfirmation(
  c: Extract<TagCommand, { kind: 'deleteTag' | 'deleteCategory' }>,
) {
  if (c.kind === 'deleteCategory')
    return {
      title: `删除分类「${c.category.name}」？`,
      label: '删除分类',
      body: c.tags
        ? `其中 ${c.tags} 个标签会保留，归入「未分类」；素材上的标签不受影响。`
        : '这个分类下没有标签。',
    }
  const n = Number(c.tag.asset_count)
  return {
    title: `删除标签「${c.tag.name}」？`,
    label: '删除标签',
    body: n
      ? `${n} 项素材（含回收站）会去掉这个标签，素材本身保留。删除后无法撤销。`
      : '还没有素材用这个标签。',
  }
}
