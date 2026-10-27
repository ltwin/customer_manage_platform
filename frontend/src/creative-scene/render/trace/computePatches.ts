import { materialEnvRotation } from 'three/tsl'
import { WGSLNodeBuilder } from 'three/webgpu'

// What three r186 refuses in a compute shader that a probe ray's shading
// needs (Epic「技术选型」, scene-world-space-gi revision v3; S0 hit-shading.md
// 「three r186 在 compute 里的限制」). Each patch changes only what three would
// otherwise refuse outside the fragment stage: the raster's shaders are built
// exactly as before. On upgrading three, check whether upstream does these
// itself, and delete the patch that it does. Upstream issue: drafted, not
// filed (the owner files it).
//
//   - WGSLNodeBuilder.generateTextureGrad: three emits textureSampleGrad in
//     the fragment stage only, and errors elsewhere; WGSL allows it in every
//     stage (only the samplers with implicit derivatives are fragment-only).
//     PMREM sampling (textureCubeUV) uses it, with zero gradients.
//   - materialEnvRotation, which PMREMNode applies to every lookup, reads the
//     frame's material and scene; a compute frame has neither. Without them
//     it is the identity: the probes' rays turn the environment themselves
//     (tables.ts environmentLookup), as the reflections' hits do.

type Builder = { shaderStage: string }
const proto = WGSLNodeBuilder.prototype as unknown as Record<string, (...args: unknown[]) => string>

const grad = proto.generateTextureGrad
proto.generateTextureGrad = function (this: Builder, texture, property, uv, gradient, depth, offset, stage = this.shaderStage) {
  if (stage === 'fragment') return grad.call(this, texture, property, uv, gradient, depth, offset, stage)
  const [dx, dy] = gradient as string[]
  const args = [`${property}`, `${property}_sampler`, uv, ...(depth ? [depth] : []), dx, dy, ...(offset ? [offset] : [])]
  return `textureSampleGrad( ${args.join(', ')} )`
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
