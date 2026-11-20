import { BackSide } from 'three'
import * as tsl from 'three/tsl'
import { PhysicalLightingModel, type NodeBuilder } from 'three/webgpu'
import type { TSLNode } from '../materials/patterns.ts'
import { transparentMark } from './passContext.ts'

// Transparent-pass transmission coverage and specular highlight floor.
//
// Checked against three 0.186.1:
//   - src/nodes/functions/PhysicalLightingModel.js:
//       lines 28-184  (getVolumeTransmissionRay, applyIorToRoughness, getTransmissionSample, volumeAttenuation, getIBLVolumeRefraction)
//       lines 191-307 (XYZ_TO_REC709, Fresnel0ToIor, IorToFresnel0, evalSensitivity, evalIridescence)
//       lines 330-331 (clearcoatF0, clearcoatF90)
//       lines 500-593 (PhysicalLightingModel.start & transmission setup)
//       lines 904-932 (PhysicalLightingModel.finish & clearcoat/sheen accumulation)
//   - src/nodes/lighting/LightsNode.js:
//       lines 398-423 (backdrop mix & totalSpecularNode + lightingModel.finish)
//
// Why this exists:
// Upstream getIBLVolumeRefraction multiplies (1 - transmittanceFactor) instead of
// folding surface Fresnel (1 - F) and clearcoat attenuation (1 - c_cc * F_cc),
// making pure clear glass (#ffffff, ior 1.5) against empty sky (B_a = 0) drop
// to alpha = 0 and vanish in W3C source-over composition. Specular light
// contributes to coverage so bright highlights survive tone mapping.
// This hook activates only when
// builder.context.transparentBackground === true && this.transmission === true,
// leaving all non-transparent passes byte-for-byte identical to upstream three.

type FnOp = (...args: TSLNode[]) => TSLNode
const {
  DFGLUT,
  EnvironmentBRDF,
  F_Schlick,
  Fn,
  If,
  Loop,
  Schlick_to_F0,
  attenuationColor,
  attenuationDistance,
  cameraPosition,
  cameraProjectionMatrix,
  cameraViewMatrix,
  cameraViewport,
  clamp,
  clearcoat,
  clearcoatNormalView,
  cos,
  diffuseColor,
  diffuseContribution,
  dispersion,
  exp,
  float,
  ior,
  iridescenceIOR,
  iridescenceThickness,
  length,
  log,
  log2,
  mat3,
  metalness,
  mix,
  modelWorldMatrix,
  normalView,
  normalWorld,
  positionViewDirection,
  positionWorld,
  refract,
  roughness,
  screenSize,
  smoothstep,
  specularColor,
  specularColorBlended,
  specularF90,
  textureBicubicLevel,
  thickness,
  transmission,
  vec2,
  vec3,
  vec4,
  viewportMipTexture,
  viewportOpaqueMipTexture,
} = tsl as unknown as Record<string, FnOp & TSLNode>

const clearcoatF0 = vec3(0.04)
const clearcoatF90 = float(1)

const getVolumeTransmissionRay = Fn(([n, v, thick, iorVal, modelMatrix]: TSLNode[]) => {
  const refractionVector = vec3(refract(v.negate(), n.normalize(), float(1.0).div(iorVal)))
  const modelScale = vec3(length(modelMatrix[0].xyz), length(modelMatrix[1].xyz), length(modelMatrix[2].xyz))
  return refractionVector.normalize().mul(thick.mul(modelScale))
}).setLayout({
  name: 'getVolumeTransmissionRay',
  type: 'vec3',
  inputs: [
    { name: 'n', type: 'vec3' },
    { name: 'v', type: 'vec3' },
    { name: 'thickness', type: 'float' },
    { name: 'ior', type: 'float' },
    { name: 'modelMatrix', type: 'mat4' },
  ],
})

const applyIorToRoughness = Fn(([rough, iorVal]: TSLNode[]) => rough.mul(clamp(iorVal.mul(2.0).sub(2.0), 0.0, 1.0))).setLayout({
  name: 'applyIorToRoughness',
  type: 'float',
  inputs: [
    { name: 'roughness', type: 'float' },
    { name: 'ior', type: 'float' },
  ],
})

const viewportBackSideTexture = viewportMipTexture()
const viewportFrontSideTexture = viewportOpaqueMipTexture()

const getTransmissionSample = Fn(([fragCoord, rough, iorVal]: TSLNode[], { material }: { material: { side: number } }) => {
  const vTexture = material.side === BackSide ? viewportBackSideTexture : viewportFrontSideTexture
  const transmissionSample = vTexture.sample(fragCoord.mul(cameraViewport.zw).add(cameraViewport.xy).div(screenSize))
  const lod = log2(cameraViewport.z).mul(applyIorToRoughness(rough, iorVal))
  return textureBicubicLevel(transmissionSample, lod)
})

const volumeAttenuation = Fn(([transmissionDistance, attColor, attDistance]: TSLNode[]) => {
  If(attDistance.notEqual(0), () => {
    const attenuationCoefficient = log(attColor).negate().div(attDistance)
    return exp(attenuationCoefficient.negate().mul(transmissionDistance))
  })
  return vec3(1.0)
}).setLayout({
  name: 'volumeAttenuation',
  type: 'vec3',
  inputs: [
    { name: 'transmissionDistance', type: 'float' },
    { name: 'attenuationColor', type: 'vec3' },
    { name: 'attenuationDistance', type: 'float' },
  ],
})

const XYZ_TO_REC709 = mat3(3.2404542, -1.5371385, -0.4985314, -0.969266, 1.8760108, 0.041556, 0.0556434, -0.2040259, 1.0572252)

const Fresnel0ToIor = (fresnel0: TSLNode) => {
  const sqrtF0 = fresnel0.sqrt()
  return vec3(1.0).add(sqrtF0).div(vec3(1.0).sub(sqrtF0))
}

const IorToFresnel0 = (transmittedIor: TSLNode, incidentIor: TSLNode) => transmittedIor.sub(incidentIor).div(transmittedIor.add(incidentIor)).pow2()

const evalSensitivity = (opd: TSLNode, shift: TSLNode) => {
  const phase = opd.mul(2.0 * Math.PI * 1.0e-9)
  const val = vec3(5.4856e-13, 4.4201e-13, 5.2481e-13)
  const pos = vec3(1.681e6, 1.7953e6, 2.2084e6)
  const varVec = vec3(4.3278e9, 9.3046e9, 6.6121e9)
  const x = float(9.747e-14 * Math.sqrt(2.0 * Math.PI * 4.5282e9))
    .mul(cos(phase.mul(2.2399e6).add(shift.x)))
    .mul(phase.pow2().mul(-4.5282e9).exp())
  let xyz: TSLNode = val
    .mul(varVec.mul(2.0 * Math.PI).sqrt())
    .mul(cos(pos.mul(phase).add(shift)))
    .mul(phase.pow2().negate().mul(varVec).exp())
  xyz = vec3(xyz.x.add(x), xyz.y, xyz.z).div(1.0685e-7)
  return XYZ_TO_REC709.mul(xyz)
}

const evalIridescence = Fn((params: Record<string, TSLNode>) => {
  const { outsideIOR, eta2, cosTheta1, thinFilmThickness, baseF0 } = params
  const iridescenceIORVal = mix(outsideIOR, eta2, smoothstep(0.0, 0.03, thinFilmThickness))
  const sinTheta2Sq = outsideIOR.div(iridescenceIORVal).pow2().mul(cosTheta1.pow2().oneMinus())
  const cosTheta2Sq = sinTheta2Sq.oneMinus().toVar()
  If(cosTheta2Sq.lessThan(0), () => vec3(1.0))
  const cosTheta2 = cosTheta2Sq.sqrt()
  const r0 = IorToFresnel0(iridescenceIORVal, outsideIOR)
  const r12 = F_Schlick({ f0: r0, f90: 1.0, dotVH: cosTheta1 } as unknown as TSLNode)
  const t121 = r12.oneMinus()
  const phi12 = iridescenceIORVal.lessThan(outsideIOR).select(Math.PI, 0.0)
  const phi21 = float(Math.PI).sub(phi12)
  const baseIOR = Fresnel0ToIor(baseF0.clamp(0.0, 0.9999))
  const r1 = IorToFresnel0(baseIOR, iridescenceIORVal.toVec3())
  const r23 = F_Schlick({ f0: r1, f90: 1.0, dotVH: cosTheta2 } as unknown as TSLNode)
  const phi23 = vec3(
    baseIOR.x.lessThan(iridescenceIORVal).select(Math.PI, 0.0),
    baseIOR.y.lessThan(iridescenceIORVal).select(Math.PI, 0.0),
    baseIOR.z.lessThan(iridescenceIORVal).select(Math.PI, 0.0),
  )
  const opd = iridescenceIORVal.mul(thinFilmThickness, cosTheta2, 2.0)
  const phi = vec3(phi21).add(phi23)
  const r123 = r12.mul(r23).clamp(1e-5, 0.9999)
  const sqrtr123 = r123.sqrt()
  const rs = t121.pow2().mul(r23).div(vec3(1.0).sub(r123))
  const c0 = r12.add(rs)
  const out = c0.toVar()
  const cm = rs.sub(t121).toVar()
  Loop({ start: 1, end: 2, condition: '<=', name: 'm' } as unknown as TSLNode, ({ m }: { m: TSLNode }) => {
    cm.mulAssign(sqrtr123)
    const sm = evalSensitivity(float(m).mul(opd), float(m).mul(phi)).mul(2.0)
    out.addAssign(cm.mul(sm))
  })
  return out.max(vec3(0.0))
}).setLayout({
  name: 'evalIridescence',
  type: 'vec3',
  inputs: [
    { name: 'outsideIOR', type: 'float' },
    { name: 'eta2', type: 'float' },
    { name: 'cosTheta1', type: 'float' },
    { name: 'thinFilmThickness', type: 'float' },
    { name: 'baseF0', type: 'vec3' },
  ],
})

const backdropAlphaKey = '_transparentBackdropAlpha'
const materialOpacityKey = '_transparentMaterialOpacity'

type Context = Record<string, unknown> & {
  backdrop?: TSLNode
  backdropAlpha?: TSLNode
  outgoingLight: TSLNode
  reflectedLight: { directSpecular: TSLNode; indirectSpecular: TSLNode }
  [backdropAlphaKey]?: TSLNode
  [materialOpacityKey]?: TSLNode
}

type Builder = NodeBuilder & {
  context: Context
  material: { transparent: boolean }
}

type LightingModelInstance = {
  clearcoat: boolean
  sheen: boolean
  iridescence: boolean
  transmission: boolean
  dispersion: boolean
  clearcoatRadiance?: TSLNode
  clearcoatSpecularDirect?: TSLNode
  clearcoatSpecularIndirect?: TSLNode
  sheenSpecularDirect?: TSLNode
  sheenSpecularIndirect?: TSLNode
  iridescenceFresnel?: TSLNode
  iridescenceF0Dielectric?: TSLNode
  iridescenceF0Metallic?: TSLNode
  dfg?: TSLNode
  multiScatteringCompensation?: TSLNode
}

type LightingModelProto = {
  start(this: LightingModelInstance, builder: NodeBuilder): void
  finish(this: LightingModelInstance, builder: NodeBuilder): void
  transparentTransmission?: true
}

/**
 * Computes backdrop refraction with single sampling of B_a and stores B_a on
 * builder.context for finish() specular coverage lower bound.
 */
function transparentVolumeRefraction(model: LightingModelInstance, context: Context): TSLNode {
  const position = positionWorld.toVar()
  const v = cameraPosition.sub(positionWorld).normalize().toVar()
  const n = normalWorld.toVar()
  const rough = roughness.toVar()
  const baseIor = ior.toVar()
  const thick = thickness.toVar()
  const attColor = attenuationColor.toVar()
  const attDist = attenuationDistance.toVar()
  const diff = diffuseContribution.toVar()

  let transmittedLight: TSLNode
  let transmittance: TSLNode

  if (model.dispersion) {
    transmittedLight = vec4().toVar()
    transmittance = vec3().toVar()
    const halfSpread = baseIor.sub(1.0).mul(dispersion.mul(0.025))
    const iors = vec3(baseIor.sub(halfSpread), baseIor, baseIor.add(halfSpread))
    Loop({ start: 0, end: 3 } as unknown as TSLNode, ({ i }: { i: TSLNode }) => {
      const iorEl = iors.element(i)
      const ray = getVolumeTransmissionRay(n, v, thick, iorEl, modelWorldMatrix)
      const exit = position.add(ray)
      const ndc = cameraProjectionMatrix.mul(cameraViewMatrix.mul(vec4(exit, 1.0)))
      const coords = vec2(ndc.xy.div(ndc.w)).toVar()
      coords.addAssign(1.0)
      coords.divAssign(2.0)
      coords.assign(vec2(coords.x, coords.y.oneMinus()))
      const sample = getTransmissionSample(coords, rough, iorEl)
      transmittedLight.element(i).assign(sample.element(i))
      transmittedLight.a.addAssign(sample.a)
      transmittance.element(i).assign(diff.element(i).mul(volumeAttenuation(length(ray), attColor, attDist).element(i)))
    })
    transmittedLight.a.divAssign(3.0)
  } else {
    const ray = getVolumeTransmissionRay(n, v, thick, baseIor, modelWorldMatrix)
    const exit = position.add(ray)
    const ndc = cameraProjectionMatrix.mul(cameraViewMatrix.mul(vec4(exit, 1.0)))
    const coords = vec2(ndc.xy.div(ndc.w)).toVar()
    coords.addAssign(1.0)
    coords.divAssign(2.0)
    coords.assign(vec2(coords.x, coords.y.oneMinus()))
    transmittedLight = getTransmissionSample(coords, rough, baseIor).toVar()
    transmittance = diff.mul(volumeAttenuation(length(ray), attColor, attDist)).toVar()
  }

  const rawAlpha = transmittedLight.a.toVar('transparentBackdropAlpha')
  context[backdropAlphaKey] = rawAlpha

  const attenuatedColor = transmittance.rgb.mul(transmittedLight.rgb)
  const dotNV = n.dot(v).clamp()
  const f = vec3(EnvironmentBRDF({ dotNV, specularColor: specularColorBlended, specularF90, roughness: rough } as unknown as TSLNode)).toVar()

  const ccAtten =
    model.clearcoat === true
      ? clearcoat
          .mul(F_Schlick({ dotVH: clearcoatNormalView.dot(positionViewDirection).clamp(), f0: clearcoatF0, f90: clearcoatF90 } as unknown as TSLNode))
          .oneMinus()
      : float(1.0)
  const tChan = f.oneMinus().mul(ccAtten).mul(transmittance.rgb)
  const tEff = clamp(tChan.r.add(tChan.g).add(tChan.b).div(3.0), 0.0, 1.0)
  const alphaBackdrop = mix(float(1.0), rawAlpha, tEff)

  return vec4(f.oneMinus().mul(attenuatedColor), alphaBackdrop)
}

/**
 * Wraps PhysicalLightingModel.start and finish once. Only activates when
 * builder.context.transparentBackground === true and this.transmission === true.
 */
export function installTransparentTransmission(model: { prototype: unknown } = PhysicalLightingModel): void {
  const proto = model.prototype as LightingModelProto
  if (proto.transparentTransmission) return
  const baseStart = Object.getPrototypeOf(proto).start as (this: LightingModelInstance, builder: NodeBuilder) => void
  const origStart = proto.start
  const origFinish = proto.finish

  proto.start = function (this: LightingModelInstance, builder: NodeBuilder) {
    const { context } = builder as Builder
    if (context[transparentMark] !== true || this.transmission !== true) {
      return origStart.call(this, builder)
    }

    if (this.clearcoat === true) {
      this.clearcoatRadiance = vec3().toVar('clearcoatRadiance')
      this.clearcoatSpecularDirect = vec3().toVar('clearcoatSpecularDirect')
      this.clearcoatSpecularIndirect = vec3().toVar('clearcoatSpecularIndirect')
    }
    if (this.sheen === true) {
      this.sheenSpecularDirect = vec3().toVar('sheenSpecularDirect')
      this.sheenSpecularIndirect = vec3().toVar('sheenSpecularIndirect')
    }
    if (this.iridescence === true) {
      const dotNVi = normalView.dot(positionViewDirection).clamp()
      const iridescenceFresnelDielectric = evalIridescence({
        outsideIOR: float(1.0),
        eta2: iridescenceIOR,
        cosTheta1: dotNVi,
        thinFilmThickness: iridescenceThickness,
        baseF0: specularColor,
      } as unknown as TSLNode)
      const iridescenceFresnelMetallic = evalIridescence({
        outsideIOR: float(1.0),
        eta2: iridescenceIOR,
        cosTheta1: dotNVi,
        thinFilmThickness: iridescenceThickness,
        baseF0: diffuseColor.rgb,
      } as unknown as TSLNode)
      this.iridescenceFresnel = mix(iridescenceFresnelDielectric, iridescenceFresnelMetallic, metalness)
      this.iridescenceF0Dielectric = Schlick_to_F0({ f: iridescenceFresnelDielectric, f90: 1.0, dotVH: dotNVi } as unknown as TSLNode)
      this.iridescenceF0Metallic = Schlick_to_F0({ f: iridescenceFresnelMetallic, f90: 1.0, dotVH: dotNVi } as unknown as TSLNode)
    }

    context.backdrop = transparentVolumeRefraction(this, context)
    context.backdropAlpha = transmission
    context[materialOpacityKey] = diffuseColor.a.toVar('transparentMaterialOpacity')
    diffuseColor.a.mulAssign(mix(1, context.backdrop.a, transmission))

    const dotNV = normalView.dot(positionViewDirection).clamp()
    this.dfg = DFGLUT({ roughness, dotNV } as unknown as TSLNode).toConst('dfg')
    const ess = this.dfg.x.add(this.dfg.y)
    this.multiScatteringCompensation = specularColorBlended.mul(ess.reciprocal().sub(1.0)).add(1.0).toConst('multiScatteringCompensation')

    baseStart.call(this, builder)
  }

  proto.finish = function (this: LightingModelInstance, builder: NodeBuilder) {
    origFinish.call(this, builder)
    const { context, material } = builder as Builder
    const backdropAlpha = context[backdropAlphaKey]
    const opacity = context[materialOpacityKey]
    if (context[transparentMark] !== true || this.transmission !== true || !backdropAlpha || !opacity) {
      return
    }
    const { reflectedLight } = context
    let spec: TSLNode = reflectedLight.directSpecular.add(reflectedLight.indirectSpecular)
    if (this.clearcoat === true) {
      const dotNVcc = clearcoatNormalView.dot(positionViewDirection).clamp()
      const fcc = F_Schlick({ dotVH: dotNVcc, f0: clearcoatF0, f90: clearcoatF90 } as unknown as TSLNode)
      spec = spec
        .mul(clearcoat.mul(fcc).oneMinus())
        .add(this.clearcoatSpecularDirect!.add(this.clearcoatSpecularIndirect!).mul(clearcoat))
    }
    if (this.sheen === true) {
      spec = spec.add(this.sheenSpecularDirect!, this.sheenSpecularIndirect!)
    }
    const mSpec = spec.r.max(spec.g).max(spec.b).max(0.0).toVar()
    const cSpec = clamp(mSpec.div(mSpec.add(1.0)), 0.0, 1.0)
    diffuseColor.a.assign(diffuseColor.a.max(opacity.mul(backdropAlpha.oneMinus()).mul(transmission).mul(cSpec)))
    if (material.transparent) {
      // The blend state multiplies RGB by opacity times optical coverage.
      // The glass's radiance should be weighted by material opacity alone.
      context.outgoingLight.mulAssign(opacity.div(diffuseColor.a.max(1e-6)))
    }
  }

  proto.transparentTransmission = true
}
