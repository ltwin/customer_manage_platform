import { EdgesGeometry, MeshBasicMaterial, type BufferGeometry } from 'three'

// The wireframe shading (Blender's X-ray wireframe): meshes are not drawn but
// still picked, and each geometry shows its outline edges. Edges are worked
// out once per geometry, so instances of one model share them.

export const wireBackground = '#3d3f43'
export const wireColor = '#c9c6bf'
/** Edges between faces bending more than this show; smooth curves stay clean. */
const thresholdDeg = 30

/** Not drawn, still hit by the pick ray (three.js checks the object, not the material). */
export const hiddenMaterial = new MeshBasicMaterial({ visible: false })

const edges = new WeakMap<BufferGeometry, EdgesGeometry>()

export function edgesOf(geometry: BufferGeometry): EdgesGeometry {
  let e = edges.get(geometry)
  if (!e) {
    e = new EdgesGeometry(geometry, thresholdDeg)
    edges.set(geometry, e)
  }
  return e
}

/** Frees a geometry's edges along with the geometry. */
export function disposeEdges(geometry: BufferGeometry) {
  edges.get(geometry)?.dispose()
  edges.delete(geometry)
}
