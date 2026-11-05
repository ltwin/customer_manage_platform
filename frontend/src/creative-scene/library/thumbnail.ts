import * as THREE from 'three'
import type { SceneObject } from '../api.ts'
import { ballBounds, ballGeometry } from '../materials/ball.ts'
import { builtinSource, materialEntry } from '../materials/catalog.ts'
import { imageStatus, loadSlotImages, textureSources, type ImageHold } from '../materials/images.ts'
import { mapInputLabel, mapInputs } from '../materials/slotEdits.ts'
import { ThumbnailError } from '../saveModel.ts'
import { drawnGeometries, keyOf, materialFor } from '../materials/instances.ts'
import { onGeometry, resolvePart, resolveSlot } from '../materials/resolve.ts'
import type { ModelParts } from '../models/cache.ts'
import { roomEnvironment } from '../render/sky.ts'
import { drawThumbnail, frame, onThumbnailGPU } from '../sandbox/thumbnail.ts'
import type { WebGPURenderer } from 'three/webgpu'
import type { Manifest } from '../sandbox/inspect.ts'

// A model asset's card thumbnail: the model with the materials it is saved
// with, in material preview's studio light plus one key light, framed like
// the bake's clay thumbnail, 256 × 256 on a transparent background; a
// material asset's is its slot on the material ball, lit the same. Slots
// without a material resolve as the viewport resolves them. Drawn once the
// slots' images are in (C5c1 design「等图片就绪」); one that does not load
// fails the thumbnail, saying which slot's which texture (ThumbnailError).

type Slots = NonNullable<SceneObject['material_slots']>
type Slot = Slots[string]

/**
 * The slots' images held for drawing; failing, a ThumbnailError naming the
 * textures whose image failed or did not fit the budget (named by the slot
 * when there are several: a model's), not the revisions the cache's own
 * errors name.
 */
function slotImages(slots: Slots): Promise<ImageHold> {
  return loadSlotImages(slots).catch(() => {
    const { failed, over } = imageStatus.getState()
    const gone = new Set([...Object.keys(failed), ...over].flatMap(textureSources))
    const many = Object.keys(slots).length > 1
    const named = Object.entries(slots).flatMap(([id, slot]) => {
      // A built-in textured material has no maps of its own: it is named whole.
      const builtin = materialEntry(slot.preset)
      if (builtin?.kind === 'textured') return Object.values(builtin.maps ?? {}).some((t) => gone.has(builtinSource(t.file))) ? [`内置材质「${builtin.label}」`] : []
      return mapInputs.filter((i) => gone.has(slot.maps?.[i]?.content_revision_id ?? '')).map((i) => (many ? `${id} 的${mapInputLabel[i]}` : mapInputLabel[i]))
    })
    throw new ThumbnailError(`${named.length ? named.join('、') : '部分'}贴图的图片加载失败或超出显存预算`)
  })
}

export async function renderMaterialThumbnail(parts: ModelParts, manifest: Manifest, materials: Slots): Promise<Blob> {
  const images = await slotImages(materials)
  try {
    return await draw(parts, manifest, materials)
  } finally {
    images.release()
  }
}

/** A material asset's card thumbnail: the slot on the material ball, lit as a model's is. */
export async function renderMaterialBall(slot: Slot): Promise<Blob> {
  const images = await slotImages({ main: slot })
  try {
    const scene = lit()
    const drawn = resolveSlot('main', { main: slot }, null, null).material
    for (const g of drawnGeometries(ballGeometry(), drawn)) scene.add(new THREE.Mesh(g, materialFor(keyOf(drawn, true), drawn, true)))
    return await shoot(scene, ballBounds)
  } finally {
    images.release()
  }
}

/** Material preview's studio light plus one key light. */
function lit(): THREE.Scene {
  const scene = new THREE.Scene()
  const key = new THREE.DirectionalLight(0xffffff, 1.5)
  key.position.set(3, 5, 4)
  scene.add(key)
  return scene
}

function shoot(scene: THREE.Scene, bounds: Manifest['bounds']): Promise<Blob> {
  const camera = new THREE.PerspectiveCamera(30, 1)
  frame(camera, { bounds })
  return onThumbnailGPU((gpu) => {
    scene.environment = gpu.cache('room', () => roomEnvironment(gpu.renderer as WebGPURenderer)).texture
    return drawThumbnail(gpu, scene, camera)
  })
}

function draw(parts: ModelParts, manifest: Manifest, materials: Slots): Promise<Blob> {
  const scene = lit()
  const hints = new Map(manifest.slots.map((s) => [s.id, s.hint]))
  for (const m of parts.meshes) {
    const resolved = resolvePart(m, materials, hints).material
    // The geometries belong to the model cache: drawn here, never disposed.
    // See-through: piece by piece, as the editor draws it (objects/ModelMesh.tsx).
    const drawn = onGeometry(resolved, 'uv' in m.geometry.attributes)
    for (const g of drawnGeometries(m.geometry, drawn)) scene.add(new THREE.Mesh(g, materialFor(keyOf(drawn, true), drawn, true)))
  }
  return shoot(scene, manifest.bounds)
}
