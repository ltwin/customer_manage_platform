import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useThree } from '@react-three/fiber'
import { EdgesGeometry, type BufferGeometry, type Mesh } from 'three'
import { Line2NodeMaterial } from 'three/webgpu'
import { LineSegments2 } from 'three/addons/lines/webgpu/LineSegments2.js'
import { LineSegmentsGeometry } from 'three/addons/lines/LineSegmentsGeometry.js'
import { ASIDE_LAYER } from '../render/sceneRenderer.ts'

// The edges of the mesh it is a child of, where faces meet at more than
// threshold degrees: drei's Edges on three's WebGPU lines. Widths are CSS
// pixels as drei's were; the WebGPU line material counts device pixels.

const noRaycast = () => {}

export function EdgeLines({ color, threshold = 15, lineWidth = 1 }: { color: string; threshold?: number; lineWidth?: number }) {
  const dpr = useThree((s) => s.viewport.dpr)
  const [line] = useState(() => {
    const l = new LineSegments2(new LineSegmentsGeometry(), new Line2NodeMaterial({ linewidth: 1 }))
    l.raycast = noRaycast
    // In the picture, out of the AO.
    l.layers.set(ASIDE_LAYER)
    return l
  })
  const from = useRef<{ geometry: BufferGeometry; threshold: number } | null>(null)
  useLayoutEffect(() => {
    const geometry = (line.parent as Mesh | null)?.geometry
    if (!geometry || (from.current?.geometry === geometry && from.current.threshold === threshold)) return
    const edges = new EdgesGeometry(geometry, threshold)
    line.geometry.fromEdgesGeometry(edges)
    edges.dispose()
    line.computeLineDistances()
    from.current = { geometry, threshold }
  })
  useLayoutEffect(() => {
    line.material.color.set(color)
    line.material.linewidth = lineWidth * dpr
  }, [line, color, lineWidth, dpr])
  useEffect(
    () => () => {
      line.geometry.dispose()
      line.material.dispose()
    },
    [line],
  )
  return <primitive object={line} />
}
