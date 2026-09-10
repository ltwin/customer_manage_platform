import type * as api from './api.ts'
import type { NewTag } from './api.ts'
export type Catalog = {
  groups: api.Group[]
  tags: api.Tag[]
  categories: api.Category[]
  hierarchy: string
  settings: api.LibrarySettings | null
}
export const emptyCatalog = (): Catalog => ({
  groups: [],
  tags: [],
  categories: [],
  hierarchy: '1',
  settings: null,
})
export type Editor = {
  kind:
    | 'group'
    | 'move'
    | 'tag'
    | 'category'
    | 'metadata'
    | 'organize'
    | 'settings'
  id?: string
  name?: string
  description?: string
  parent?: string
  position?: number
  color?: string
  revision?: string
  hierarchy: string
  selected?: api.Asset[]
}
export type Organization = {
  groupIDs: string[]
  tagIDs: string[]
  newTags: NewTag[]
}
export const emptyOrganization = (): Organization => ({
  groupIDs: [],
  tagIDs: [],
  newTags: [],
})
