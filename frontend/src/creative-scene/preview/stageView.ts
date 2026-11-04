import { useEffect, useMemo, useState } from 'react'
import { Camera, MathUtils, Vector3 } from 'three'
import type { components } from '../../api/schema'
import { loadSlotImages, slotTextureKeys, type ImageHold } from '../materials/images.ts'

// The asset viewer's 3D stages (stage.tsx): where the camera opens and when
// the slots' images are in.

type Manifest = components['schemas']['CreativeModelManifest']
type Materials = components['schemas']['CreativeSceneMaterialSlots']

export const fov = 30

/** The thumbnail's view: three-quarter from the front (+Z), centred on the bounds. */
export function framing(manifest: Pick<Manifest, 'bounds'>) {
  const min = new Vector3().fromArray(manifest.bounds.min)
  const max = new Vector3().fromArray(manifest.bounds.max)
  const center = min.clone().add(max).multiplyScalar(0.5)
  const radius = Math.max(min.distanceTo(max) / 2, 0.01)
  // Looser than the thumbnail's 1.05: the stage has room for the buttons below.
  const distance = (radius / Math.sin(MathUtils.degToRad(fov / 2))) * 1.35
  const position = center.clone().add(new Vector3(0.9, 0.7, 1.4).normalize().multiplyScalar(distance))
  // How the camera is turned there: the room's light is set as it falls from this view (stage.tsx FrameStep).
  // A camera, not a plain object: lookAt turns a camera's -Z to the target.
  const eye = new Camera()
  eye.position.copy(position)
  eye.lookAt(center)
  return { position: position.toArray(), target: center.toArray(), near: distance / 100, far: distance * 20, turn: eye.quaternion.clone() }
}

/**
 * Whether the slots' images are in, held while on (C5c1 design「等图片就绪」).
 * One that did not load draws as its placeholder: the preview still shows
 * the rest, and the editor's material panel says which.
 */
export function useSlotImages(materials: Materials, on: boolean): boolean {
  const keys = useMemo(() => slotTextureKeys([materials]).join(' '), [materials])
  const [ready, setReady] = useState<string | null>(null)
  useEffect(() => {
    if (!on || keys === '') return
    let live = true
    let hold: ImageHold | null = null
    loadSlotImages(materials).then(
      (h) => {
        if (!live) return h.release()
        hold = h
        setReady(keys)
      },
      (err: unknown) => {
        console.warn('model preview: an image did not load', err)
        if (live) setReady(keys)
      },
    )
    return () => {
      live = false
      hold?.release()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keys are what of the slots the images depend on
  }, [keys, on])
  return !on || keys === '' || ready === keys
}
