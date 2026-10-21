// Types of three r186 GTAONode (@types/three 0.186.0), for the fork beside it.
import { Camera, Node, TempNode, TextureNode, UniformNode } from "three/webgpu";

declare class GTAONode extends TempNode<"float"> {
    depthNode: Node;
    normalNode: Node;

    resolutionScale: number;

    radius: UniformNode<"float", number>;
    thickness: UniformNode<"float", number>;
    /** Used again by the fork, which keeps GTAOShader's distance model. */
    distanceExponent: UniformNode<"float", number>;
    /** Used again by the fork, which keeps GTAOShader's distance model. */
    distanceFallOff: UniformNode<"float", number>;
    scale: UniformNode<"float", number>;
    samples: UniformNode<"float", number>;

    useTemporalFiltering: boolean;

    constructor(depthNode: Node, normalNode: Node, camera: Camera);

    getTextureNode(): TextureNode;

    setSize(width: number, height: number): void;
}

export default GTAONode;

export const ao: (
    depthNode: Node,
    normalNode: Node,
    camera: Camera,
) => GTAONode;
