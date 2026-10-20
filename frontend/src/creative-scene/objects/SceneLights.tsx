import { useEffect, useLayoutEffect, useMemo } from 'react'
import { useThree } from '@react-three/fiber'
import type { SceneObject } from '../api.ts'
import { applyLight, buildLight, disposeLight } from './lights.ts'

type Light = NonNullable<SceneObject['light']>

/**
 * A light object as a three.js light, in its object's document-space group;
 * mounted only in the lit look. A new type builds a new light; other fields
 * are written onto the same one.
 */
export function SceneLight({ light }: { light: Light }) {
  const invalidate = useThree((s) => s.invalidate)
  const built = useMemo(() => buildLight(light.type), [light.type])
  useLayoutEffect(() => {
    applyLight(built, light)
    invalidate()
  }, [built, light, invalidate])
  useEffect(() => () => disposeLight(built), [built])
  return (
    <>
      <primitive object={built.light} />
      {built.target && <primitive object={built.target} />}
    </>
  )
}
