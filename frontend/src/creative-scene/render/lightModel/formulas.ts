import * as tsl from 'three/tsl'
import type { TSLNode } from '../../materials/patterns.ts'

// The light model's formulas (Epic design「光栅与命中点一致」, I4): the
// raster's light nodes and lighting model, and the world-space layer's hits
// (render/trace/hit.ts), draw sized lights by these alone.

type Op = (...args: TSLNode[]) => TSLNode
const { max } = tsl as unknown as Record<string, Op>

/**
 * What three's falloff at d becomes at the effective distance max(d, r)
 * (Epic design I2): 1 / max(d^decay, 0.01) made 1 / max(max(d, r)^decay,
 * 0.01), as a factor on three's. The cutoff window stays three's, at d. Not
 * three's getDistanceAttenuation run again: S0 found the ratio came out wrong
 * that way (integration.md「灯光节点」).
 */
export function effectiveFalloff(d: TSLNode, r: TSLNode, decay: TSLNode): TSLNode {
  return max(d.pow(decay), 0.01).div(max(max(d, r).pow(decay), 0.01))
}
