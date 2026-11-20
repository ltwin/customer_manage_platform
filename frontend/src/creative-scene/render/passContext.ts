import { context } from 'three/tsl'

/** three's context data includes the data from any wrapped contexts. */
export type PassContext = ReturnType<typeof context> & { getFlowContextData(): Record<string, unknown> }

export const transparentMark = 'transparentBackground'
const transparentContexts = new WeakMap<PassContext, { version: number; context: PassContext }>()

export function transparentContext(base: PassContext): PassContext {
  let made = transparentContexts.get(base)
  if (!made || made.version !== base.version) {
    made = { version: base.version, context: context({ ...base.getFlowContextData(), [transparentMark]: true }) as PassContext }
    transparentContexts.set(base, made)
  }
  return made.context
}
