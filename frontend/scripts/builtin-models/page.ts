// The page scripts/builtin-models/convert.mjs drives: a body packed as a GLB
// goes through the import's own conversion (design「素体的生成」): parse,
// convert with the origin at the bottom centre, the mapping found by the UE
// names and checked as the server checks it, written with its node indices
// followed. Gives back the GLB, its manifest and rig, each slot's maps by
// image, and the images as the import would upload them.
import { bundledDecoders } from '../../src/creative-scene/import/decoders.ts'
import { convert } from '../../src/creative-scene/import/convert.ts'
import { browserCodec, prepareImages } from '../../src/creative-scene/import/images.ts'
import { checkMapping, fileConstraints, jointObjects, submittedRig } from '../../src/creative-scene/import/mapping.ts'
import { parseImport } from '../../src/creative-scene/import/parse.ts'
import { writeGLB, writtenRig } from '../../src/creative-scene/import/write.ts'

const base64 = (bytes: Uint8Array) => {
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  return btoa(binary)
}

async function convertBody(glb64: string) {
  const file = Uint8Array.from(atob(glb64), (c) => c.charCodeAt(0))
  const parsed = await parseImport(file.buffer, bundledDecoders)
  const rig = parsed.rig
  if (!rig) throw new Error('没有骨骼')
  const conversion = convert(parsed, { originToBottomCenter: true })
  const check = checkMapping(rig, conversion.scene, rig.detection.humanoid)
  if (check.problem) throw new Error(`人形映射不成立：${check.problem}`)
  const { constraints, notes } = fileConstraints(rig)
  const written = await writeGLB(conversion.scene, jointObjects(rig))
  const fileRig = submittedRig(check.humanoid, constraints)!
  const { images, report } = await prepareImages(conversion.images, parsed.images, browserCodec)
  return {
    glb: base64(new Uint8Array(written.glb)),
    manifest: written.manifest,
    rig: writtenRig(fileRig, rig.objects, written.nodes),
    via: rig.detection.via,
    dropped: check.dropped.map((d) => d.message),
    nonUniform: check.nonUniform,
    notes,
    report: [...conversion.report, ...report].map((r) => `${r.slot ?? '文件'}：${r.text}`),
    slots: Object.fromEntries([...conversion.slots].map(([id, s]) => [id, { params: s.params, maps: s.maps, normalStrength: s.normalStrength, alphaMode: s.alphaMode }])),
    images: images.map((i) => ({ images: i.images, mime: i.mime, bytes: base64(i.bytes) })),
  }
}

Object.assign(window, { convertBody })
document.body.dataset.ready = '1'
