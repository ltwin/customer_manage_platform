// S0 group 3: what three r186 refuses in a compute shader that a hit point's shading needs, patched for the spike
// only (hit-shading.md「three r186 在 compute 里的限制」). Each patch changes only what three would otherwise refuse
// outside the fragment stage; the raster's shaders are untouched.
//   - TextureNode.grad(): three emits textureSampleGrad only in the fragment stage; WGSL allows it in every stage
//     (only the implicit-derivative samplers are fragment-only). PMREM sampling (textureCubeUV) uses it with zero
//     gradients.
//   - DepthTexture compare: three emits textureSampleCompare (fragment-only) and refuses elsewhere; WGSL has
//     textureSampleCompareLevel for every stage. The shadow map's lookup uses it.
//   - materialEnvRotation, which PMREMNode applies to every lookup, reads the frame's material and scene; a compute
//     frame has neither. Without them it is the identity (no rotation), as an unrotated scene.environment's.
import { materialEnvRotation } from 'three/tsl'
import { WGSLNodeBuilder } from 'three/webgpu'

type Builder = { shaderStage: string }
const proto = WGSLNodeBuilder.prototype as unknown as Record<string, (...args: unknown[]) => string>

const grad = proto.generateTextureGrad
proto.generateTextureGrad = function (this: Builder, texture, property, uv, gradient, depth, offset, stage = this.shaderStage) {
  if (stage === 'fragment') return grad.call(this, texture, property, uv, gradient, depth, offset, stage)
  const [dx, dy] = gradient as string[]
  const args = [`${property}`, `${property}_sampler`, uv, ...(depth ? [depth] : []), dx, dy, ...(offset ? [offset] : [])]
  return `textureSampleGrad( ${args.join(', ')} )`
}

const compare = proto.generateTextureCompare
proto.generateTextureCompare = function (this: Builder, texture, property, uv, reference, depth, offset, stage = this.shaderStage) {
  if (stage === 'fragment') return compare.call(this, texture, property, uv, reference, depth, offset, stage)
  const args = [`${property}`, `${property}_sampler`, uv, ...(depth ? [depth] : []), reference, ...(offset ? [offset] : [])]
  return `textureSampleCompareLevel( ${args.join(', ')} )`
}

const rotation = materialEnvRotation as unknown as { update(frame: { material: unknown; scene: unknown }): unknown; value: { identity(): void } }
const update = rotation.update
rotation.update = function (frame) {
  if (!frame.material || !frame.scene) {
    rotation.value.identity()
    return
  }
  return update.call(this, frame)
}
