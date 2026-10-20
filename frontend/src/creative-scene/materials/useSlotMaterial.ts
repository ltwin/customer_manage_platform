import { useEffect, useMemo } from 'react'
import type { MeshPhysicalMaterial } from 'three'
import { keyOf, materialFor, release, retain } from './instances.ts'
import type { SlotMaterial } from './resolve.ts'

/**
 * The shared material instance for a slot's material. Retained and released
 * in one effect, so StrictMode's extra cleanup and setup leave the count right.
 */
export function useSlotMaterial(m: SlotMaterial, doubleSided: boolean): MeshPhysicalMaterial {
  const key = keyOf(m, doubleSided)
  // eslint-disable-next-line react-hooks/exhaustive-deps -- key stands for m and doubleSided
  const material = useMemo(() => materialFor(key, m, doubleSided), [key])
  useEffect(() => {
    retain(key)
    return () => release(key)
  }, [key])
  return material
}
