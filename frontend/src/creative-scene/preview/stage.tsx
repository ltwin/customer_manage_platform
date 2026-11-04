import { useEffect, type ComponentRef, type RefObject } from 'react'
import { useFrame } from '@react-three/fiber'
import { OrbitControls } from '@react-three/drei'
import { Quaternion } from 'three'
import { gpuOf } from '../render/gpu.ts'
import { aimStudio } from '../render/studio.ts'

// What the asset viewer's 3D stages share: a model's (ModelPreview.tsx) and
// a material's on the material ball (MaterialPreview.tsx). Their view and
// image wait are in stageView.ts.

export type Orbit = ComponentRef<typeof OrbitControls>

const turned = /*@__PURE__*/ new Quaternion()
const back = /*@__PURE__*/ new Quaternion()

/**
 * Steps the renderer's frame before R3F draws one (render/gpu.ts owns the
 * count). The light stays with the viewer, as in a material preview: clay's
 * key light turns with the view, as in the editor, and the room's light
 * turns by as much as the camera has from opening, where it falls as on the
 * card thumbnail. Dragging so looks round the object under the same light.
 */
export function FrameStep({ opening }: { opening: Quaternion }) {
  useFrame((state) => {
    gpuOf(state.gl).advanceFrame()
    aimStudio(state.scene, state.camera)
    // The PMREM is looked up with y flipped (three PMREMNode), so the turn goes in mirrored across the xz plane: x and z negated.
    turned.copy(state.camera.quaternion).multiply(back.copy(opening).invert())
    state.scene.environmentRotation.setFromQuaternion(turned.set(-turned.x, turned.y, -turned.z, turned.w))
  }, -1)
  return null
}

/** Orbit around the model's centre; the opening view is what a double-click returns to. */
export function Controls({ controls, target }: { controls: RefObject<Orbit | null>; target: number[] }) {
  useEffect(() => {
    controls.current?.saveState()
  }, [controls, target])
  return <OrbitControls ref={controls} makeDefault target={target as [number, number, number]} enablePan={false} />
}
