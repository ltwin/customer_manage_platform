import { SphereGeometry } from 'three'

// The material ball a material asset is shown on (C5c1 design「材质资产的存入
// 与使用」): its card thumbnail and the library viewer's 3D preview. A sphere
// one metre across with UVs, so a triplanar map shows at its real size and a
// UV one wraps it once.

// Framing takes a box's half diagonal as the radius to fit (sandbox/thumbnail.ts
// frame): the box whose half diagonal is the ball's radius fits the ball itself.
const half = 0.5 / Math.sqrt(3)
export const ballBounds = { min: [-half, -half, -half], max: [half, half, half] }

let shared: SphereGeometry | null = null

/** The ball's geometry, made once and never disposed: every ball is the same. */
export function ballGeometry(): SphereGeometry {
  shared ??= new SphereGeometry(0.5, 96, 48)
  return shared
}
