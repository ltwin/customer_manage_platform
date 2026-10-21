import { useEffect, useLayoutEffect, useMemo } from 'react'
import { useThree } from '@react-three/fiber'
import type { SceneObject } from '../api.ts'
import { gpuOf } from '../render/gpu.ts'
import { applyLight, buildLight, disposeLight } from './lights.ts'

type Light = NonNullable<SceneObject['light']>

/**
 * A light object as a three.js light, in its object's document-space group;
 * mounted only in the lit look. A new type builds a new light; other fields
 * are written onto the same one.
 *
 * A new light, or one that starts or stops casting shadows, makes three.js
 * compile every lit material again, at once, on the next frame: about two
 * seconds on the AC-9 scene. With warm (the viewport), the light stays dark
 * while the viewport's passes compile for it on the driver's threads
 * (SceneGPU.compile), then shows. A
 * shot compiles its whole scene before shooting and passes warm false.
 */
export function SceneLight({ light, warm }: { light: Light; warm: boolean }) {
  const { gl, scene, camera, invalidate } = useThree()
  const built = useMemo(() => buildLight(light.type), [light.type])
  useLayoutEffect(() => {
    applyLight(built, light)
    invalidate()
  }, [built, light, invalidate])
  const shadows = light.cast_shadow ?? false
  useLayoutEffect(() => {
    if (!warm) return
    let live = true
    // compile() only gathers lights and starts compiling, so the light counts for it and no frame sees it.
    built.light.visible = true
    const ready = gpuOf(gl).compile(scene, camera)
    built.light.visible = false
    void ready.finally(() => {
      if (!live) return
      built.light.visible = true
      invalidate()
    })
    return () => {
      live = false
      built.light.visible = true
    }
  }, [warm, built, shadows, gl, scene, camera, invalidate])
  useEffect(() => () => disposeLight(built), [built])
  return (
    <>
      <primitive object={built.light} />
      {built.target && <primitive object={built.target} />}
    </>
  )
}
