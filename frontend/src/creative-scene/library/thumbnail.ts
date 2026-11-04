import * as THREE from 'three'
import type { SceneObject } from '../api.ts'
import { loadSlotImages } from '../materials/images.ts'
import { drawnGeometries, keyOf, materialFor } from '../materials/instances.ts'
import { onGeometry, resolvePart } from '../materials/resolve.ts'
import type { ModelParts } from '../models/cache.ts'
import { roomEnvironment } from '../render/sky.ts'
import { drawThumbnail, frame, onThumbnailGPU } from '../sandbox/thumbnail.ts'
import type { WebGPURenderer } from 'three/webgpu'
import type { Manifest } from '../sandbox/inspect.ts'

// A model asset's card thumbnail: the model with the materials it is saved
// with, in material preview's studio light plus one key light, framed like
// the bake's clay thumbnail, 256 × 256 on a transparent background. Slots
// without a material resolve as the viewport resolves them. Drawn once the
// slots' images are in (C5c1 design「等图片就绪」); one that does not load
// fails the thumbnail, saying which.

type Slots = NonNullable<SceneObject['material_slots']>

export async function renderMaterialThumbnail(parts: ModelParts, manifest: Manifest, materials: Slots): Promise<Blob> {
  const images = await loadSlotImages(materials)
  try {
    return await draw(parts, manifest, materials)
  } finally {
    images.release()
  }
}

function draw(parts: ModelParts, manifest: Manifest, materials: Slots): Promise<Blob> {
  const scene = new THREE.Scene()
  const key = new THREE.DirectionalLight(0xffffff, 1.5)
  key.position.set(3, 5, 4)
  scene.add(key)
  const hints = new Map(manifest.slots.map((s) => [s.id, s.hint]))
  for (const m of parts.meshes) {
    const resolved = resolvePart(m, materials, hints).material
    // The geometries belong to the model cache: drawn here, never disposed.
    // See-through: piece by piece, as the editor draws it (objects/ModelMesh.tsx).
    const drawn = onGeometry(resolved, 'uv' in m.geometry.attributes)
    for (const g of drawnGeometries(m.geometry, drawn)) scene.add(new THREE.Mesh(g, materialFor(keyOf(drawn, true), drawn, true)))
  }
  const camera = new THREE.PerspectiveCamera(30, 1)
  frame(camera, manifest)
  return onThumbnailGPU((gpu) => {
    scene.environment = gpu.cache('room', () => roomEnvironment(gpu.renderer as WebGPURenderer)).texture
    return drawThumbnail(gpu, scene, camera)
  })
}
