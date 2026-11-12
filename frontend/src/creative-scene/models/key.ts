import type { components } from '../../api/schema'

// What the editor keys a model by (design「前端的模型键」): an account's model
// by its content revision, as before; a built-in model as builtin:<id>. Model
// summaries, the model cache and everything that looks a model up use it.

type ModelRef = components['schemas']['CreativeSceneModelRef']

const prefix = 'builtin:'

/** The key of the model a reference names; undefined for none, or for a ref still waiting on its commit_model. */
export function modelKey(ref: ModelRef | undefined): string | undefined {
  if (ref?.content_revision_id) return ref.content_revision_id
  if (ref?.builtin) return prefix + ref.builtin
  return undefined
}

/** The built-in entry a key names; null for an account's model. */
export const builtinOf = (key: string): string | null => (key.startsWith(prefix) ? key.slice(prefix.length) : null)

export const builtinKey = (id: string) => prefix + id
