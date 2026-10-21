import { acos, clamp, cameraPosition, cos, dot, exp, float, Fn, max, min, mix, modelViewProjection, normalize, positionWorld, pow, smoothstep, uniform, vec3, vec4 } from 'three/tsl'
import { BackSide, BoxGeometry, Mesh, NodeMaterial, Vector3 } from 'three/webgpu'
import { MIE_COEFFICIENT, MIE_DIRECTIONAL_G, RAYLEIGH } from '../../src/creative-scene/render/atmosphere.ts'
import { SKY_GAIN } from '../../src/creative-scene/render/litLook.ts'

// Group 3 (R0 task 3.3): the editor's two skies in TSL.
//  - litSky: three's SkyMesh (the TSL twin of Sky.js) as render/sky.ts
//    viewSky uses it: the view direction turned to document space
//    (view (x, y, z) → document (x, z, −y)), no clouds, the sun disc on or
//    off, the output times SKY_GAIN.
//  - claySky: drei's Sky, which is three-stdlib's older Sky shader, with
//    up = (0, 0, 1) as ClayLighting sets it: its own sun disc, rayleigh 0.5,
//    sunfade from the view-space y, directions from the world origin and a
//    final curve pow(c, 1 / (1.2 + 1.2·sunfade)).

/* eslint-disable @typescript-eslint/no-explicit-any -- TSL nodes are loosely typed in three's typings */
type N = any

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

const sunIntensity = (zenithAngleCos: N): N => float(EE).mul(max(0, float(1).sub(pow(e, float(cutoffAngle).sub(acos(clamp(zenithAngleCos, -1, 1))).div(steepness).negate()))))

/** In-scattering shared by both skies: Lin before the night sky and the disc. */
function scatter(direction: N, sunDirection: N, sunE: N, betaR: N, betaM: N, g: N, zenithCos: N, sunZenithCos: N) {
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

function skyMesh(colorNode: N): Mesh {
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

export type LitSky = Mesh & { sunPosition: N; turbidity: N; showSunDisc: N }

/** render/sky.ts viewSky in TSL; sunPosition is in document space (Y up). */
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

export type ClaySky = Mesh & { sunPosition: N; turbidity: N }

/** drei's Sky (three-stdlib) with up = (0, 0, 1); sunPosition in view space, as ClayLighting gives it. */
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
