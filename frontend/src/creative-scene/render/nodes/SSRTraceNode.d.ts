// Types of the fork of three r186 SSRNode (SSRTraceNode.js beside it): upstream's (@types/three 0.186.0) with the retrace's options.
import type SSRNode from 'three/addons/tsl/display/SSRNode.js'
import type { SSRNodeOptions } from 'three/addons/tsl/display/SSRNode.js'
import type { Node, UniformNode } from 'three/webgpu'
import type { HitShading } from '../trace/hit.ts'

export interface SSRTraceNodeOptions extends SSRNodeOptions {
  /** What traces and shades a retraced ray through the world-space layer (render/trace/hit.ts). */
  trace: HitShading
  /** The roughness below which a ray is retraced (Epic U3); 0.5 unless said. */
  rMax?: number
}

declare class SSRTraceNode extends SSRNode {
  trace: HitShading | null
  rMax: UniformNode<'float', number>
}

export default SSRTraceNode

export function ssrTrace(colorNode: Node<'vec4'>, depthNode: Node<'float'> | Node<'vec4'>, normalNode: Node<'vec3'>, options: SSRTraceNodeOptions): SSRTraceNode
