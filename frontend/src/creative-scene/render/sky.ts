import { BackSide, BoxGeometry, Mesh, Scene, Vector3, type Texture } from 'three'
import * as tsl from 'three/tsl'
import { NodeMaterial, PMREMGenerator, type RenderTarget, type WebGPURenderer } from 'three/webgpu'
import { ColorEnvironment } from 'three/addons/environments/ColorEnvironment.js'
import { RoomEnvironment } from 'three/addons/environments/RoomEnvironment.js'
import type { SceneEnvironment } from '../api.ts'
import type { TSLNode } from '../materials/patterns.ts'
import { MIE_COEFFICIENT, MIE_DIRECTIONAL_G, RAYLEIGH } from './atmosphere.ts'
import { SKY_GAIN } from './litLook.ts'

// The editor's two skies, in TSL (patterns「场景着色写法」: compare with the
// sources on every three upgrade). Before R1 they were two shaders:
//  - the lit look's: three's Sky (r186 examples/jsm/objects/Sky.js; its TSL
//    twin is SkyMesh.js, where this is taken from), with the view direction
//    turned to document space (view (x, y, z) → document (x, z, −y)), no
//    clouds (they drift with time, and an export must not depend on when it
//    is taken), the sun disc on or off, and the output times skyGain so
//    background and sky light sit beside the sun light;
//  - the clay look's: drei's Sky, which is three-stdlib's older Sky shader,
//    with up = (0, 0, 1) as ClayLighting set it: its own sun disc, rayleigh
//    0.5, sunfade from the view-space y, directions from the world origin and
//    a final curve pow(c, 1 / (1.2 + 1.2·sunfade)). Kept as it was since C4.
// R0 (scene-webgpu-spike, sky-shadow-outline.md): both within 1/255 of the
// GLSL skies.

type Op = (...args: TSLNode[]) => TSLNode
const { acos, clamp, cameraPosition, cos, dot, exp, float, max, min, mix, normalize, positionWorld, pow, smoothstep, uniform, vec3, vec4 } = tsl as unknown as Record<string, Op> & {
  cameraPosition: TSLNode
  positionWorld: TSLNode
}
const { modelViewProjection } = tsl as unknown as { modelViewProjection: TSLNode }
const Fn = tsl.Fn as unknown as (fn: () => TSLNode) => () => TSLNode

const e = 2.718281828459045
const totalRayleigh = vec3(5.804542996261093e-6, 1.3562911419845635e-5, 3.0265902468824876e-5)
const MieConst = vec3(1.8399918514433978e14, 2.7798023919660528e14, 4.0790479543861094e14)
const cutoffAngle = 1.6110731556870734
const steepness = 1.5
const EE = 1000
const pi = 3.141592653589793
const rayleighZenithLength = 8.4e3
const mieZenithLength = 1.25e3
const sunAngularDiameterCos = 0.9999566769464484
const THREE_OVER_SIXTEENPI = 0.05968310365946075
const ONE_OVER_FOURPI = 0.07957747154594767

const sunIntensity = (zenithAngleCos: TSLNode): TSLNode =>
  float(EE).mul(max(0, float(1).sub(pow(e, float(cutoffAngle).sub(acos(clamp(zenithAngleCos, -1, 1))).div(steepness).negate()))))

/** In-scattering shared by both skies: Lin before the night sky and the disc. */
function scatter(direction: TSLNode, sunDirection: TSLNode, sunE: TSLNode, betaR: TSLNode, betaM: TSLNode, g: TSLNode, zenithCos: TSLNode, sunZenithCos: TSLNode) {
  const zenithAngle = acos(max(0, zenithCos))
  const inverse = float(1).div(cos(zenithAngle).add(float(0.15).mul(pow(float(93.885).sub(zenithAngle.mul(180 / pi)), -1.253))))
  const Fex = exp(betaR.mul(inverse.mul(rayleighZenithLength)).add(betaM.mul(inverse.mul(mieZenithLength))).negate()).toVar()
  const cosTheta = dot(direction, sunDirection).toVar()
  const c = cosTheta.mul(0.5).add(0.5)
  const betaRTheta = betaR.mul(float(THREE_OVER_SIXTEENPI).mul(float(1).add(pow(c, 2))))
  const g2 = pow(g, 2)
  const mPhase = float(ONE_OVER_FOURPI).mul(float(1).sub(g2)).mul(float(1).div(pow(float(1).sub(float(2).mul(g).mul(cosTheta)).add(g2), 1.5)))
  const betaMTheta = betaM.mul(mPhase)
  const ratio = sunE.mul(betaRTheta.add(betaMTheta).div(betaR.add(betaM))).toVar()
  const Lin = pow(ratio.mul(float(1).sub(Fex)), vec3(1.5)).mul(mix(vec3(1), pow(ratio.mul(Fex), vec3(0.5)), clamp(pow(float(1).sub(sunZenithCos), 5), 0, 1)))
  return { Fex, cosTheta, Lin }
}

/** A box drawn inside out at the far plane, coloured by direction. */
function skyMesh(colorNode: TSLNode): Mesh {
  const material = new NodeMaterial()
  material.side = BackSide
  material.depthWrite = false
  material.vertexNode = Fn(() => {
    const position = modelViewProjection.toVar()
    position.z.assign(position.w)
    return position
  })()
  material.colorNode = colorNode
  const mesh = new Mesh(new BoxGeometry(1, 1, 1), material)
  mesh.frustumCulled = false
  return mesh
}

type Uniform<T> = { value: T }

export type LitSky = Mesh & { sunPosition: Uniform<Vector3>; turbidity: Uniform<number>; showSunDisc: Uniform<number> }

/** The lit look's sky; sunPosition is in document space (Y up). */
export function litSky(): LitSky {
  const sunPosition = uniform(new Vector3(0, 1, 0))
  const turbidity = uniform(3)
  const showSunDisc = uniform(1)
  const rayleigh = float(RAYLEIGH)
  const mieCoefficient = float(MIE_COEFFICIENT)
  const g = float(MIE_DIRECTIONAL_G)
  const color = Fn(() => {
    const sunDirection = normalize(sunPosition).toVar()
    const sunE = sunIntensity(sunDirection.y).toVar()
    const sunfade = float(1).sub(clamp(float(1).sub(exp(sunPosition.y.div(450000))), 0, 1))
    const betaR = totalRayleigh.mul(rayleigh.sub(float(1).sub(sunfade))).toVar()
    const betaM = float(0.434).mul(float(0.2).mul(turbidity).mul(10e-18)).mul(MieConst).mul(mieCoefficient).toVar()
    const v = normalize(positionWorld.sub(cameraPosition))
    const direction = vec3(v.x, v.z, v.y.negate()).toVar()
    const { Fex, cosTheta, Lin } = scatter(direction, sunDirection, sunE, betaR, betaM, g, direction.y, sunDirection.y)
    const L0 = vec3(0.1).mul(Fex)
    const sundisc = clamp(cosTheta.sub(sunAngularDiameterCos).mul(50000), 0, 1).mul(showSunDisc)
    const sundiscColor = min(sunE.mul(Fex), 80).mul(760).mul(sundisc)
    const texColor = Lin.add(L0).mul(0.04).add(sundiscColor).add(vec3(0, 0.0003, 0.00075))
    return vec4(texColor.mul(vec3(...SKY_GAIN)), 1)
  })()
  return Object.assign(skyMesh(color), { sunPosition, turbidity, showSunDisc }) as LitSky
}

export type ClaySky = Mesh & { sunPosition: Uniform<Vector3>; turbidity: Uniform<number> }

/** The clay look's sky; sunPosition in view space (Z up). */
export function claySky(): ClaySky {
  const sunPosition = uniform(new Vector3(0, 0, 1))
  const turbidity = uniform(3)
  const up = vec3(0, 0, 1)
  const rayleigh = float(0.5)
  const mieCoefficient = float(0.005)
  const g = float(0.8)
  const color = Fn(() => {
    const sunDirection = normalize(sunPosition).toVar()
    const sunE = sunIntensity(dot(sunDirection, up)).toVar()
    const sunfade = float(1).sub(clamp(float(1).sub(exp(sunPosition.y.div(450000))), 0, 1)).toVar()
    const betaR = totalRayleigh.mul(rayleigh.sub(float(1).sub(sunfade))).toVar()
    const betaM = float(0.434).mul(float(0.2).mul(turbidity).mul(10e-18)).mul(MieConst).mul(mieCoefficient).toVar()
    const direction = normalize(positionWorld).toVar()
    const { Fex, cosTheta, Lin } = scatter(direction, sunDirection, sunE, betaR, betaM, g, dot(up, direction), dot(up, sunDirection))
    const sundisk = smoothstep(sunAngularDiameterCos, sunAngularDiameterCos + 0.00002, cosTheta)
    const L0 = vec3(0.1).mul(Fex).add(sunE.mul(19000).mul(Fex).mul(sundisk))
    const texColor = Lin.add(L0).mul(0.04).add(vec3(0, 0.0003, 0.00075))
    return vec4(pow(texColor, vec3(float(1).div(float(1.2).add(sunfade.mul(1.2))))), 1)
  })()
  return Object.assign(skyMesh(color), { sunPosition, turbidity }) as ClaySky
}

export function disposeSky(sky: Mesh) {
  sky.geometry.dispose()
  ;(sky.material as NodeMaterial).dispose()
}

/** The sun's direction in document space (Y up) from the sky's angles. */
export function sunDirection(env: SceneEnvironment): Vector3 {
  const sky = env.sky
  const el = ((sky.sun_elevation_deg ?? 35) * Math.PI) / 180
  const az = ((sky.sun_azimuth_deg ?? 135) * Math.PI) / 180
  return new Vector3(Math.sin(az) * Math.cos(el), Math.sin(el), Math.cos(az) * Math.cos(el))
}

export function setSky(sky: LitSky, env: SceneEnvironment, sunDisc: boolean) {
  sky.sunPosition.value.copy(sunDirection(env))
  sky.turbidity.value = env.sky.turbidity ?? 3
  sky.showSunDisc.value = sunDisc ? 1 : 0
}

/** What lights a lit scene besides its lights: the sky without the sun disc, or the sky colour. */
export function skyEnvironment(pmrem: PMREMGenerator, env: SceneEnvironment): RenderTarget {
  if (env.sky.kind === 'procedural') {
    const scene = new Scene()
    const sky = litSky()
    setSky(sky, env, false)
    sky.scale.setScalar(10)
    scene.add(sky)
    const target = pmrem.fromScene(scene, 0, 0.1, 100)
    disposeSky(sky)
    return target
  }
  const colour = new ColorEnvironment(env.sky.kind === 'color' ? (env.sky.color ?? '#8a8f98') : '#8a8f98')
  const target = pmrem.fromScene(colour, 0)
  colour.dispose()
  return target
}

/** Material preview's neutral studio light: the same for every scene. Callers keep it on the device (gpu.cache). */
export function roomEnvironment(renderer: WebGPURenderer): { texture: Texture; dispose(): void } {
  const pmrem = new PMREMGenerator(renderer)
  const room = new RoomEnvironment()
  const target = pmrem.fromScene(room, 0.04)
  room.dispose()
  pmrem.dispose()
  return { texture: target.texture, dispose: () => target.dispose() }
}
