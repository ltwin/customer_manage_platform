import type { Object3D } from 'three'

// Meshes with a see-through material (Alpha below 1, transmission). AO works
// from the scene's depth and normals, where glass would hide what is behind
// it; the renderer hides these while AO draws (sceneRenderer.ts).

export const transparentMeshes = new Set<Object3D>()
