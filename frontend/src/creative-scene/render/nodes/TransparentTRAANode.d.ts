import type { Camera } from 'three';
import type { Node, TextureNode } from 'three/webgpu';
import TRAANode from 'three/addons/tsl/display/TRAANode.js';

export const clipAABB4D: (historyColor: Node, minColor: Node, maxColor: Node) => Node;

export class TransparentTRAANode extends TRAANode {}

export default TransparentTRAANode;

export const transparentTRAA: (
  beautyNode: Node,
  depthNode: TextureNode,
  velocityNode: TextureNode,
  camera: Camera,
) => TransparentTRAANode;
