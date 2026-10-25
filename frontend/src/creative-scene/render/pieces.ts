import { Box3, BufferAttribute, BufferGeometry, type Mesh, type Object3D, type Sphere } from 'three'
import { mergeGeometries } from 'three/addons/utils/BufferGeometryUtils.js'

// The pieces of a merged geometry (scene-performance「合并 part」): a model's
// parts of one slot are drawn as one geometry, and each part it was made of
// is a piece of it, where it lies in the index (in the vertices without one)
// and the bounds three worked out for it before the merge. Whatever must look
// or act as before the merge works piece by piece: see-through slots and
// ghosts draw a range view per piece (three sorts them by their spheres),
// outlines and wire edges are made per piece, and what read a mesh's box
// (the sun's cascades, framing) reads the pieces' boxes.
//
// Pieces live here, not on geometry.userData: BufferGeometry.copy shares
// userData, so a clone would carry ranges that are no longer its own. A
// geometry with no pieces is one piece, all of it.

/** A piece in its merged geometry: an index range (a vertex range without an index); bounds in model space. */
export type Piece = { start: number; count: number; sphere: Sphere; box: Box3 }

const registry = new WeakMap<BufferGeometry, readonly Piece[]>()
const views = new WeakMap<BufferGeometry, readonly BufferGeometry[]>()

/** Records a geometry's pieces: a merge's, a range view's own, or a shell made piece by piece. */
export function setPieces(geometry: BufferGeometry, pieces: readonly Piece[]) {
  registry.set(geometry, pieces)
}

/** The pieces a geometry was made of; undefined for any other geometry, a clone included: all of it is one piece. */
export function piecesOf(geometry: BufferGeometry): readonly Piece[] | undefined {
  return registry.get(geometry)
}

/**
 * One piece on its own, for what reads a whole geometry (EdgesGeometry,
 * toCreasedNormals): the attributes shared with an index of its range, or
 * its vertices copied out without one.
 */
export function extractPiece(geometry: BufferGeometry, piece: Piece): BufferGeometry {
  const out = new BufferGeometry()
  const index = geometry.index
  if (index) {
    for (const [name, attribute] of Object.entries(geometry.attributes)) out.setAttribute(name, attribute)
    const array = index.array
    out.setIndex(new BufferAttribute(array.slice(piece.start, piece.start + piece.count), 1))
    return out
  }
  for (const [name, attribute] of Object.entries(geometry.attributes)) {
    const a = attribute as BufferAttribute
    out.setAttribute(name, new BufferAttribute(a.array.slice(piece.start * a.itemSize, (piece.start + piece.count) * a.itemSize), a.itemSize, a.normalized))
  }
  return out
}

const countOf = (g: BufferGeometry) => (g.index ? g.index.count : g.attributes.position.count)

/**
 * What make gives for each piece on its own, put back together: an outline's
 * shell or wire edges are made per piece, never across two parts, as before
 * the merge. The result has the pieces too (its own ranges, their boxes), so
 * framing it bounds what the pieces did. A geometry without pieces is made
 * whole.
 */
export function perPiece(geometry: BufferGeometry, make: (piece: BufferGeometry) => BufferGeometry): BufferGeometry {
  const pieces = registry.get(geometry)
  if (!pieces) return make(geometry)
  const made = pieces.map((p) => make(extractPiece(geometry, p)))
  // Made alike from one geometry, they merge.
  const out = made.length === 1 ? made[0] : (mergeGeometries(made, false) as BufferGeometry)
  let start = 0
  registry.set(
    out,
    pieces.map((p, i) => {
      const count = countOf(made[i])
      const q = { ...p, start, count }
      start += count
      return q
    }),
  )
  return out
}

/**
 * A geometry drawn piece by piece: a range view per piece, made once and
 * kept with the geometry. A view shares the geometry's attributes and index
 * (the same GPU buffers) and has its piece's range and bounds; disposing one
 * would free those buffers, so views go only with the geometry
 * (disposePieceViews). A geometry with no pieces is its own one view.
 */
export function pieceViews(geometry: BufferGeometry): readonly BufferGeometry[] {
  const pieces = registry.get(geometry)
  if (!pieces || pieces.length < 2) return [geometry]
  let made = views.get(geometry)
  if (!made) {
    made = pieces.map((p) => {
      const view = new BufferGeometry()
      view.setIndex(geometry.index)
      for (const [name, attribute] of Object.entries(geometry.attributes)) view.setAttribute(name, attribute)
      view.morphAttributes = geometry.morphAttributes
      view.morphTargetsRelative = geometry.morphTargetsRelative
      view.setDrawRange(p.start, p.count)
      // Copies: three computes bounds into the object it finds there.
      view.boundingSphere = p.sphere.clone()
      view.boundingBox = p.box.clone()
      registry.set(view, [p])
      return view
    })
    views.set(geometry, made)
  }
  return made
}

/** Frees a geometry's range views, before the geometry itself. */
export function disposePieceViews(geometry: BufferGeometry) {
  for (const view of views.get(geometry) ?? []) view.dispose()
  views.delete(geometry)
}

const box = /*@__PURE__*/ new Box3()

/**
 * Box3.expandByObject (not precise), with a geometry's pieces' boxes in
 * place of its own: a merged box turned with its object bounds more than
 * the pieces did.
 */
export function expandByPieces(bounds: Box3, object: Object3D): Box3 {
  object.updateWorldMatrix(false, false)
  const geometry = (object as Mesh).geometry as BufferGeometry | undefined
  if (geometry) {
    // An instanced or batched mesh has a box of its own.
    const own = object as Object3D & { boundingBox?: Box3 | null; computeBoundingBox?(): void }
    const pieces = registry.get(geometry)
    if (own.boundingBox !== undefined) {
      if (own.boundingBox === null) own.computeBoundingBox!()
      bounds.union(box.copy(own.boundingBox!).applyMatrix4(object.matrixWorld))
    } else if (pieces) {
      for (const p of pieces) bounds.union(box.copy(p.box).applyMatrix4(object.matrixWorld))
    } else {
      if (geometry.boundingBox === null) geometry.computeBoundingBox()
      bounds.union(box.copy(geometry.boundingBox!).applyMatrix4(object.matrixWorld))
    }
  }
  for (const child of object.children) expandByPieces(bounds, child)
  return bounds
}
