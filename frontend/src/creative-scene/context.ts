import { createStore } from 'zustand/vanilla'

// The open scene editor's scene and selection, for the assistant panel to
// send as the run's scene_context. No three.js: the panel lives on the
// canvas page.

export type SceneContext = { documentID: string; selected: string[] }

export const sceneContext = createStore<SceneContext | null>(() => null)

export function setSceneContext(next: SceneContext): void {
  sceneContext.setState(next, true)
}

/** Clears the context if it is still this editor's. */
export function clearSceneContext(documentID: string): void {
  if (sceneContext.getState()?.documentID === documentID) sceneContext.setState(null, true)
}
