import { useEffect, useLayoutEffect, useMemo } from 'react'
import { useThree } from '@react-three/fiber'
import { AmbientLight, type Camera, type Object3D } from 'three'
import type { SceneObject } from '../api.ts'
import { gpuOf, type SceneGPU } from '../render/gpu.ts'
import { applyLight, buildLight, disposeLight } from './lights.ts'

type Light = NonNullable<SceneObject['light']>

/** A light waiting for its compile; show() turns it on once that is done. */
type Warming = { light: Object3D; live: boolean; show: () => void }

/** The lights that started warming in the commit under way, per device. */
const batches = new Map<SceneGPU, { scene: Object3D; camera: Camera; lights: Warming[] }>()

/**
 * Warms the lights of one commit together, after it: switching to the rendered
 * view, or an undo bringing several back, mounts them all at once. Compiled
 * one by one, each compile had only its own light among the new ones, a set no
 * frame draws, and every light coming on made the next set compile on a
 * frame: 7 to 10 seconds on the AC-9 scene.
 */
function warm(gpu: SceneGPU, scene: Object3D, camera: Camera, w: Warming) {
  let batch = batches.get(gpu)
  if (!batch) {
    batch = { scene, camera, lights: [] }
    batches.set(gpu, batch)
    // After the commit's layout effects: the look's other lights, sky and fog are in place.
    queueMicrotask(() => {
      batches.delete(gpu)
      const lights = batch!.lights.filter((l) => l.live)
      if (!lights.length) return
      // compile() only gathers lights and starts compiling, so they count for it and no frame sees them.
      for (const l of lights) l.light.visible = true
      const ready = gpu.compile(batch!.scene, batch!.camera)
      for (const l of lights) l.light.visible = false
      void ready.finally(() => {
        for (const l of lights) if (l.live) l.show()
      })
    })
  }
  batch.lights.push(w)
}

/** With warm, light stays dark until the viewport's passes have compiled for it, again whenever shadows changes. */
function useWarm(light: Object3D, warmed: boolean, shadows: boolean) {
  const { gl, scene, camera, invalidate } = useThree()
  useLayoutEffect(() => {
    if (!warmed) return
    const w: Warming = {
      light,
      live: true,
      show: () => {
        light.visible = true
        invalidate()
      },
    }
    light.visible = false
    warm(gpuOf(gl), scene, camera, w)
    return () => {
      w.live = false
      light.visible = true
    }
  }, [warmed, light, shadows, gl, scene, camera, invalidate])
}

/**
 * A light object as a three.js light, in its object's document-space group;
 * mounted only in the lit look. A new type builds a new light; other fields
 * are written onto the same one.
 *
 * A new light, or one that starts or stops casting shadows, makes three.js
 * compile every lit material again, at once, on the next frame: about two
 * seconds on the AC-9 scene. With warm (the viewport), the light stays dark
 * while the viewport's passes compile for it on the driver's threads
 * (SceneGPU.compile), with the other lights of its commit, then shows. A
 * shot compiles its whole scene before shooting and passes warm false.
 */
export function SceneLight({ light, warm: warmed }: { light: Light; warm: boolean }) {
  const invalidate = useThree((s) => s.invalidate)
  const built = useMemo(() => buildLight(light.type), [light.type])
  useLayoutEffect(() => {
    applyLight(built, light)
    invalidate()
  }, [built, light, invalidate])
  useWarm(built.light, warmed, light.cast_shadow ?? false)
  useEffect(() => () => disposeLight(built), [built])
  return (
    <>
      <primitive object={built.light} />
      {built.target && <primitive object={built.target} />}
    </>
  )
}

/**
 * While a volume casts shadows on surfaces, a light of no intensity that the
 * lit materials' shadows look for (render/volumes/shadow.ts volumesCast):
 * only then do they carry the volumes' shadow, and a new casting light
 * compiles no more than without volumes (AC-W5-11, owner 2026-10-23). It
 * changes the scene's lights, so three compiles every lit material again:
 * warmed as a new light is, once as the first volume starts casting.
 */
export function VolumesCastLight({ warm: warmed }: { warm: boolean }) {
  const light = useMemo(() => {
    const l = new AmbientLight(0xffffff, 0)
    l.userData.volumesCast = true
    return l
  }, [])
  useWarm(light, warmed, false)
  useEffect(() => () => light.dispose(), [light])
  return <primitive object={light} />
}
