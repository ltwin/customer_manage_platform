import { BufferGeometry, EdgesGeometry, MeshBasicMaterial } from 'three'
import { toCreasedNormals } from 'three/addons/utils/BufferGeometryUtils.js'
import { perPiece } from '../render/pieces.ts'

// The wireframe shading (Blender's X-ray wireframe): meshes are not drawn but
// still picked, and each geometry shows its outline edges. Edges are worked
// out once per geometry, so instances of one model share them. Edges and the
// selection outline's shell are made per piece of a merged model geometry
// (render/pieces.ts): where two parts touch, the edges show as before.

export const wireBackground = '#3d3f43'
export const wireColor = '#c9c6bf'
/** Edges between faces bending more than this show; smooth curves stay clean. */
const thresholdDeg = 30

/** Not drawn, still hit by the pick ray (three.js checks the object, not the material). */
export const hiddenMaterial = new MeshBasicMaterial({ visible: false })

const edges = new WeakMap<BufferGeometry, BufferGeometry>()

export function edgesOf(geometry: BufferGeometry): BufferGeometry {
  let e = edges.get(geometry)
  if (!e) {
    e = perPiece(geometry, (g) => new EdgesGeometry(g, thresholdDeg))
    edges.set(geometry, e)
  }
  return e
}

/**
 * The selection outline's shell (objects/Outline.tsx): a copy with the
 * normals averaged across creases, per piece. Without an index three writes
 * the normals into the geometry it is given, so it gets a copy of the
 * positions alone: freeing the shell frees no buffer of the model's.
 */
export function shellOf(geometry: BufferGeometry): BufferGeometry {
  return perPiece(geometry, (g) => toCreasedNormals(g.index ? g : new BufferGeometry().setAttribute('position', g.attributes.position.clone()), Math.PI))
}

/** Frees a geometry's edges along with the geometry. */
export function disposeEdges(geometry: BufferGeometry) {
  edges.get(geometry)?.dispose()
  edges.delete(geometry)
}
