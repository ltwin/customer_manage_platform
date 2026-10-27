// Area lights' shadows (S0 group 5, U3 and U5): three's RectAreaLight casts none, so the page gives the softbox a
// shadow of a kind three has and a node that uses it, and filters it with PCSS as a disc of the area's size:
//   ?areaShadow=cube    a cube map from the area's centre (PointShadowNode), as a point light's
//   ?areaShadow=front   one perspective map from the centre along the way the area faces, ?areaFov= degrees across
//   ?areaShadow=none    unshadowed, three's as it is
// ?areaR= the disc's radius in metres (default √(w h / π), the area's own); ?areaMap= the map's size (1024).
// The cube's kernel is the rectangle's own (its axes and half sizes), ?areaKernel=disc a disc of areaR.
import { PointLight, RectAreaLight, SpotLight, Vector2, Vector3, type LightShadow, type Object3D } from 'three'
import * as tsl from 'three/tsl'
import { RectAreaLightNode, type WebGPURenderer } from 'three/webgpu'
import { pcssFor } from './soft.ts'

const query = new URLSearchParams(location.search)
const { pointShadow } = tsl as unknown as { pointShadow: (light: unknown) => unknown }

class CubeShadowedAreaNode extends RectAreaLightNode {
  setupShadowNode() {
    return pointShadow((this as unknown as { light: unknown }).light)
  }
}

/** Gives softbox (made by main.tsx's addSoftbox) its shadow as ?areaShadow= asks; returns a line for the report. */
export function areaShadows(renderer: WebGPURenderer, softbox: RectAreaLight): string {
  const mode = query.get('areaShadow') ?? 'none'
  if (mode === 'none') return 'area light unshadowed'
  const box = softbox as RectAreaLight & { shadow: LightShadow; castShadow: boolean; angle?: number; distance?: number; penumbra?: number; target?: Object3D }
  const r = Number(query.get('areaR') ?? Math.sqrt((softbox.width * softbox.height) / Math.PI))
  const size = Number(query.get('areaMap') ?? 1024)
  const { lightNodes } = (renderer as unknown as { library: { lightNodes: WeakMap<object, unknown> } }).library
  if (mode === 'cube') {
    // A point light's shadow: its camera stands at the light's place and turns to each face (PointLightShadow).
    box.shadow = new PointLight().shadow
    lightNodes.set(RectAreaLight, CubeShadowedAreaNode)
  } else {
    // A spot light's: SpotLightShadow reads angle, distance and target off the light it shades.
    box.shadow = new SpotLight().shadow
    box.angle = (Number(query.get('areaFov') ?? 150) * Math.PI) / 360
    box.distance = 0
    box.penumbra = 0
    const target = new PointLight()
    target.visible = false
    // Ahead of the softbox's face: RectAreaLight shines down its −Z.
    target.position.set(0, 0, -1)
    softbox.add(target)
    box.target = target
  }
  box.castShadow = true
  box.shadow.mapSize.set(size, size)
  box.shadow.camera.near = 0.05
  box.shadow.camera.far = 50
  box.shadow.bias = 0
  // The area's axes and half sizes for a rectangle's kernel (?areaKernel=disc: a disc of r instead).
  const rect = mode === 'cube' && query.get('areaKernel') !== 'disc'
  softbox.updateMatrixWorld()
  const e = softbox.matrixWorld.elements
  Object.assign(box.shadow, {
    lightAxisX: new Vector3(e[0], e[1], e[2]).normalize(),
    lightAxisY: new Vector3(e[4], e[5], e[6]).normalize(),
    lightHalf: new Vector2(softbox.width / 2, softbox.height / 2),
  })
  pcssFor(box.shadow, rect ? 'cube-rect' : mode === 'cube' ? 'cube' : 'spot', r)
  return `area light ${softbox.width}×${softbox.height} m shadowed by a ${mode === 'cube' ? 'cube' : `${query.get('areaFov') ?? 150}° front`} map ${size}, PCSS ${rect ? 'with the rectangle\'s kernel' : `as a disc of r ${r.toFixed(3)} m`}`
}
