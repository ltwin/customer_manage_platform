import { createStore } from 'zustand/vanilla'

// The open scene editor's scene and selection, for the assistant panel to
// send as the run's scene_context. No three.js: the panel lives on the
// canvas page.

// names holds the selected objects that have one, for the panel to show.
export type SceneContext = { documentID: string; selected: string[]; names?: Record<string, string> }

export const sceneContext = createStore<SceneContext | null>(() => null)

export function setSceneContext(next: SceneContext): void {
  // Edits that leave the selection and its names alone do not re-render the panel.
  if (same(sceneContext.getState(), next)) return
  sceneContext.setState(next, true)
}

/** Clears the context if it is still this editor's. */
export function clearSceneContext(documentID: string): void {
  if (sceneContext.getState()?.documentID === documentID) sceneContext.setState(null, true)
}

/** What the selection reads as, in selection order; an object without a name reads as empty. */
export function selectedNames(context: SceneContext): string[] {
  return context.selected.map((id) => context.names?.[id] ?? '')
}

function same(a: SceneContext | null, b: SceneContext): boolean {
  if (!a || a.documentID !== b.documentID || a.selected.length !== b.selected.length) return false
  const names = selectedNames(b)
  return selectedNames(a).every((name, i) => a.selected[i] === b.selected[i] && name === names[i])
}
