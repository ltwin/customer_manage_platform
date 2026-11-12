// The real-time GI's contact shadows (S0 group 6, U4): three's SSSNode traces a short way on screen toward the main
// light, which realtimeGI.ts sets as a unit offset of a stand-in light every frame before the trace. The page wraps
// SSSNode.updateBefore, so after the product has set that way and before the trace:
//   ?contact=off      no contact shadows (shadowIntensity 0)
//   ?contact=jitter   the way turned within the light's angular radius each frame (?contactAngle= radians), a
//                     different turn a frame that the accumulation averages into a soft edge
import { Vector3 } from 'three'
import SSSNode from 'three/addons/tsl/display/SSSNode.js'

const query = new URLSearchParams(location.search)
const way = new Vector3()
const across = new Vector3()
const up = new Vector3()

/** Installs the wrap (once for the page); returns a line for the report, or null with nothing asked. */
export function contactShadows(): string | null {
  const mode = query.get('contact')
  if (!mode) return null
  const angle = Number(query.get('contactAngle') ?? 0.01)
  type Traced = { _mainLight: { matrixWorld: { elements: number[] } }; shadowIntensity: { value: number }; updateBefore(frame: { frameId: number }): unknown }
  const proto = SSSNode.prototype as unknown as Traced
  const before = proto.updateBefore
  let n = 0
  proto.updateBefore = function (this: Traced, frame: { frameId: number }) {
    if (mode === 'off') this.shadowIntensity.value = 0
    if (mode === 'jitter') {
      const e = this._mainLight.matrixWorld.elements
      way.set(e[12], e[13], e[14]).normalize()
      // A point of a disc of the light's angular radius, by the golden angle frame after frame.
      across.set(Math.abs(way.x) < 0.9 ? 1 : 0, Math.abs(way.x) < 0.9 ? 0 : 1, 0).cross(way).normalize()
      up.crossVectors(way, across)
      const k = n++
      const r = Math.tan(angle) * Math.sqrt((k % 64 + 0.5) / 64)
      const phi = k * 2.399963
      way.addScaledVector(across, r * Math.cos(phi)).addScaledVector(up, r * Math.sin(phi)).normalize()
      e[12] = way.x
      e[13] = way.y
      e[14] = way.z
    }
    return before.call(this, frame)
  }
  return mode === 'off' ? 'contact shadows off' : `contact shadows turned within ${angle} rad`
}
