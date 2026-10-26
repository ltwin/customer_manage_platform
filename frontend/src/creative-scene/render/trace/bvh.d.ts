// three-mesh-bvh 0.9.15's parts the world-space layer packs with that its
// typings leave out (S0 integration.md「打包辅助函数」): the deep imports and
// the WebGPU module's structs. Only what layer.ts and build.worker.ts use;
// compared with upstream on every upgrade (AC-S1-5's depth check is the one
// to run).

declare module 'three-mesh-bvh/src/webgpu/utils/packBVHBufferUtils.js' {
  /** Nodes along the deepest path of a root's tree (a lone leaf is 1). */
  export function getMaxNodeDepth(root: ArrayBuffer, start?: number): number
  /**
   * The top level's nodes written at nodeWriteOffset, each leaf naming its
   * transform slot and the node its tree starts at; null rewrites the bounds
   * alone (a refit).
   */
  export function appendBVHData(bvh: { _roots: ArrayBuffer[] }, primitiveInfo: readonly { transformSlot: number; nodeOffset: number }[] | null, nodeWriteOffset: number, target: ArrayBuffer): void
}

declare module 'three-mesh-bvh/src/webgpu/ClusteredBVH.js' {
  import type { Matrix4, Object3D } from 'three'

  /** The top level over objects: with shouldCluster always true, one leaf per root of each object's tree. */
  export class ClusteredBVH {
    constructor(
      root: Object3D,
      options: {
        strategy: number
        getBVH: (object: Object3D, instance: number) => { _roots: ArrayBuffer[] } | null
        shouldCluster: (object: Object3D) => boolean
      },
    )
    _roots: ArrayBuffer[]
    primitiveBuffer: Uint32Array
    primitiveBufferStride: number
    objects: Object3D[]
    matrixWorld: Matrix4
    getObjectId(compositeId: number): number
    getInstanceId(compositeId: number): number
    getBVHRootIndex(compositeNodeId: number): number
    refit(): void
  }
}
