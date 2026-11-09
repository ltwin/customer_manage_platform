import type { components } from '../../api/schema'

// A model's generator comes in two forms (data: 模型内容): a code model's,
// with its code and parameters, and an imported GLB's, with neither.

export type CodeGenerator = components['schemas']['CreativeModelCodeGenerator']

/** Whether g is an imported GLB's: no code, no parameters to change. */
export const isImported = (g: { language: string } | null | undefined): boolean => g?.language === 'imported'

/** g when it is a code model's, else null. */
export function codeGenerator<G extends { language: string }>(g: G | null | undefined): Extract<G, { language: 'three-module' }> | null {
  return g?.language === 'three-module' ? (g as Extract<G, { language: 'three-module' }>) : null
}
