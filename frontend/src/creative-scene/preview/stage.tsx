import { useEffect, type ComponentRef, type RefObject } from 'react'
import { useFrame } from '@react-three/fiber'
import { OrbitControls } from '@react-three/drei'
import { gpuOf } from '../render/gpu.ts'
import { aimStudio } from '../render/studio.ts'

// What the asset viewer's 3D stages share: a model's (ModelPreview.tsx) and
// a material's on the material ball (MaterialPreview.tsx). Their view and
// image wait are in stageView.ts.

export type Orbit = ComponentRef<typeof OrbitControls>

/** Steps the renderer's frame before R3F draws one (render/gpu.ts owns the count); clay's key light turns with the view, as in the editor. */
export function FrameStep() {
  useFrame((state) => {
    gpuOf(state.gl).advanceFrame()
    aimStudio(state.scene, state.camera)
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
