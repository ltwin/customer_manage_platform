// Types of three r186 SSGINode (@types/three 0.186.0), for the fork beside it.
import { Node, PerspectiveCamera, TempNode, TextureNode, UniformNode } from "three/webgpu";

declare class SSGINode extends TempNode<"vec4"> {
    beautyNode: Node;
    depthNode: Node;
    normalNode: Node;

    sliceCount: UniformNode<"uint", number>;
    stepCount: UniformNode<"uint", number>;
    aoIntensity: UniformNode<"float", number>;
    giIntensity: UniformNode<"float", number>;
    radius: UniformNode<"float", number>;
    useScreenSpaceSampling: UniformNode<"bool", boolean>;
    expFactor: UniformNode<"float", number>;
    thickness: UniformNode<"float", number>;
    useLinearThickness: UniformNode<"bool", boolean>;
    backfaceLighting: UniformNode<"float", number>;
    useTemporalFiltering: boolean;

    constructor(beautyNode: Node, depthNode: Node, normalNode: Node, camera: PerspectiveCamera);

    /** The fork's one texture: vec4(GI, AO). */
    getTextureNode(): TextureNode<"vec4">;
    getAONode(): Node<"float">;
    getGINode(): Node<"vec3">;

    dispose(): void;
}

export default SSGINode;

export const ssgi: (
    beautyNode: Node,
    depthNode: Node,
    normalNode: Node,
    camera: PerspectiveCamera,
) => SSGINode;
