// Makes the built-in bodies (scene-skinned-humanoid design「素体的生成」) from
// Quaternius' Universal Base Characters, downloaded by hand: the pack's
// "Base Characters/Godot - UE" folder, CC0.
//
//   node scripts/builtin-models/convert.mjs --source <…/Godot - UE> --out <dir> [--apply]
//
// Each body's .gltf is packed into a GLB with its .bin and textures (the
// pack's wrong "_png" image names corrected), its materials named as the
// slots body, eyes and brows; then the import's own conversion runs on it in
// a browser (scripts/builtin-models/page.ts). Written to <dir>:
//   models/<sha256>.glb     for creativectl models import --dir
//   textures/<sha256>.png   for creativectl textures import --dir, kept as PNG
//   catalog.json            the models.json entries and the materials.json textured entries
// --apply adds them to backend/internal/creativescene/models.json and
// materials.json (with the character category); an ID already there is an
// error, as published entries never change. Run make generate after.
import { createHash } from 'node:crypto'
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parseArgs } from 'node:util'
import { createServer } from 'vite'
import { chromium } from 'playwright'

const { values } = parseArgs({ options: { source: { type: 'string' }, out: { type: 'string' }, apply: { type: 'boolean', default: false } } })
if (!values.source || !values.out) {
  console.error('usage: node scripts/builtin-models/convert.mjs --source <…/Base Characters/Godot - UE> --out <dir> [--apply]')
  process.exit(2)
}

const source = { site: 'quaternius', asset: 'Universal Base Characters', author: 'Quaternius', license: 'CC0' }
const sourceURL = 'https://quaternius.com/packs/universalbasecharacters.html'
const bodies = [
  {
    id: 'body_female@1',
    label: '女素体',
    file: 'Superhero_Female_FullBody.gltf',
    slots: { MI_Superhero_Female: 'body', MI_Eyes: 'eyes', MI_Hair_2: 'brows' },
    materials: { body: ['ubc_female_body@1', '女素体皮肤'], eyes: ['ubc_eyes@1', '素体眼睛'], brows: ['ubc_female_brows@1', '女素体眉毛'] },
  },
  {
    id: 'body_male@1',
    label: '男素体',
    file: 'Superhero_Male_FullBody.gltf',
    slots: { MI_Superhero_Male: 'body', MI_Eyes: 'eyes', MI_Hair_1: 'brows' },
    materials: { body: ['ubc_male_body@1', '男素体皮肤'], eyes: ['ubc_eyes@1', '素体眼睛'], brows: ['ubc_male_brows@1', '男素体眉毛'] },
  },
]

const sha256 = (bytes) => createHash('sha256').update(bytes).digest('hex')
const atStep = (x) => Math.round(x * 100) / 100
const pad4 = (n) => (4 - (n % 4)) % 4

/** The .gltf with its .bin and images in one GLB: images whose file is missing tried without the pack's "_png". */
function packGLB(dir, name, slots) {
  const json = JSON.parse(readFileSync(join(dir, name), 'utf8'))
  if (json.buffers?.length !== 1) throw new Error(`${name}: 须恰好一个 buffer`)
  const parts = [readFileSync(join(dir, json.buffers[0].uri))]
  let length = parts[0].length
  const append = (bytes) => {
    const padding = Buffer.alloc(pad4(length))
    parts.push(padding, bytes)
    length += padding.length
    const view = { buffer: 0, byteOffset: length, byteLength: bytes.length }
    length += bytes.length
    json.bufferViews.push(view)
    return json.bufferViews.length - 1
  }
  for (const image of json.images ?? []) {
    let file = join(dir, image.uri)
    if (!existsSync(file)) file = join(dir, image.uri.replace(/_png\.png$/, '.png'))
    if (!existsSync(file)) throw new Error(`${name}: 找不到贴图 ${image.uri}`)
    image.bufferView = append(readFileSync(file))
    delete image.uri
  }
  for (const m of json.materials ?? []) {
    if (!slots[m.name]) throw new Error(`${name}: 材质 ${m.name} 没有对应的槽位`)
    m.name = slots[m.name]
  }
  const bin = Buffer.concat([...parts, Buffer.alloc(pad4(length))])
  json.buffers = [{ byteLength: bin.length }]
  const text = Buffer.from(JSON.stringify(json))
  const jsonChunk = Buffer.concat([text, Buffer.alloc(pad4(text.length), 0x20)])
  const chunk = (body, type) => {
    const h = Buffer.alloc(8)
    h.writeUInt32LE(body.length, 0)
    h.writeUInt32LE(type, 4)
    return Buffer.concat([h, body])
  }
  const body = Buffer.concat([chunk(jsonChunk, 0x4e4f534a), chunk(bin, 0x004e4942)])
  const header = Buffer.alloc(12)
  header.writeUInt32LE(0x46546c67, 0)
  header.writeUInt32LE(2, 4)
  header.writeUInt32LE(12 + body.length, 8)
  return Buffer.concat([header, body])
}

mkdirSync(join(values.out, 'models'), { recursive: true })
mkdirSync(join(values.out, 'textures'), { recursive: true })

const server = await createServer({
  root: fileURLToPath(new URL('../../', import.meta.url)),
  server: { host: '127.0.0.1', port: 0 },
  logLevel: 'error',
  plugins: [{ name: 'builtin-models', configureServer(vite) {
    vite.middlewares.use('/__builtin_models__', async (_req, res) => {
      res.setHeader('Content-Type', 'text/html')
      res.end(await vite.transformIndexHtml('/__builtin_models__', '<html><head></head><body><script type="module" src="/scripts/builtin-models/page.ts"></script></body></html>'))
    })
  } }],
})
await server.listen()
const browser = await chromium.launch({ headless: true })
const models = []
const materials = new Map()
try {
  const page = await browser.newPage()
  page.on('pageerror', (e) => console.error('page:', e.message))
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__builtin_models__`)
  await page.waitForFunction(() => document.body.dataset.ready === '1', null, { timeout: 120_000 })
  for (const b of bodies) {
    const r = await page.evaluate((glb) => window.convertBody(glb), packGLB(values.source, b.file, b.slots).toString('base64'))
    for (const line of [...r.report, ...r.notes, ...r.dropped]) console.log(`${b.id}: ${line}`)
    if (r.nonUniform.length) console.log(`${b.id}: 非均匀缩放的骨骼 ${r.nonUniform.join('、')}`)
    const glb = Buffer.from(r.glb, 'base64')
    const glbName = `${sha256(glb)}.glb`
    writeFileSync(join(values.out, 'models', glbName), glb)
    // Each image under its content's name, as the built-in textures are.
    const fileOf = new Map()
    for (const image of r.images) {
      const bytes = Buffer.from(image.bytes, 'base64')
      const name = `${sha256(bytes)}.${image.mime === 'image/jpeg' ? 'jpg' : 'png'}`
      writeFileSync(join(values.out, 'textures', name), bytes)
      for (const i of image.images) fileOf.set(i, { name, bytes: bytes.length })
    }
    const slotValues = {}
    for (const [slot, [id, label]] of Object.entries(b.materials)) {
      const s = r.slots[slot]
      if (!s) throw new Error(`${b.id}: 没有槽位 ${slot}`)
      const maps = {}
      for (const [kind, ref] of Object.entries(s.maps)) {
        // glTF multiplies the map by its factor: at 0 metalness the map does nothing.
        if (kind === 'metalness' && s.params.metalness === 0) continue
        maps[kind] = { file: fileOf.get(ref.image).name, ...(ref.channel ? { channel: ref.channel } : {}) }
      }
      const files = [...new Set(Object.values(maps).map((m) => m.file))].map((name) => ({ name, bytes: [...fileOf.values()].find((f) => f.name === name).bytes }))
      const entry = {
        id, kind: 'textured', label, category: 'character', size_m: 1, listed: false, maps,
        // At the parameters' step: the factors come from Blender as 0.8227272629737854.
        defaults: { color: s.params.color, roughness: atStep(s.params.roughness), metalness: atStep(s.params.metalness) },
        files, source,
      }
      const was = materials.get(id)
      if (was && JSON.stringify(was) !== JSON.stringify(entry)) throw new Error(`${id}: 两个素体的同名材质不一样`)
      materials.set(id, entry)
      slotValues[slot] = {
        preset: id, params: {},
        mapping: { kind: 'uv', ...(s.normalStrength !== 1 ? { normal_strength: s.normalStrength } : {}) },
        ...(s.alphaMode !== 'opaque' ? { alpha_mode: s.alphaMode } : {}),
      }
    }
    models.push({ id: b.id, label: b.label, category: 'humanoid', mount: 'floor', glb: { name: glbName, bytes: glb.length }, manifest: r.manifest, rig: r.rig, materials: slotValues, source: { ...source, url: sourceURL } })
    console.log(`${b.id}: ${glbName}，${r.manifest.triangles} 个三角形，${Object.keys(r.rig.humanoid).length} 根标准骨骼（${r.via}），${r.rig.constraints?.length ?? 0} 条约束`)
  }
} finally {
  await browser.close()
  await server.close()
}
writeFileSync(join(values.out, 'catalog.json'), `${JSON.stringify({ models, materials: [...materials.values()] }, null, 2)}\n`)

if (values.apply) {
  const backend = fileURLToPath(new URL('../../../backend/internal/creativescene/', import.meta.url))
  const modelsFile = join(backend, 'models.json')
  const catalog = JSON.parse(readFileSync(modelsFile, 'utf8'))
  for (const m of models) if (catalog.models.some((x) => x.id === m.id)) throw new Error(`models.json 已有 ${m.id}：发布过的条目不改，发新版本`)
  catalog.models.push(...models)
  writeFileSync(modelsFile, `${JSON.stringify(catalog, null, 2)}\n`)

  // materials.json is laid out by hand: entries go in as text, in its layout, before the categories.
  const materialsFile = join(backend, 'materials.json')
  let text = readFileSync(materialsFile, 'utf8')
  for (const id of materials.keys()) if (text.includes(`"id": "${id}"`)) throw new Error(`materials.json 已有 ${id}：发布过的条目不改，发新版本`)
  const inline = (o) => JSON.stringify(o).replace(/":/g, '": ').replace(/,"/g, ', "').replace(/^\{/, '{ ').replace(/\}$/, ' }')
  const laid = [...materials.values()].map((m) => [
    '    {',
    ...['id', 'kind', 'label', 'category', 'size_m', 'listed'].map((k) => `      "${k}": ${JSON.stringify(m[k])},`),
    '      "maps": {',
    Object.entries(m.maps).map(([k, v]) => `        "${k}": ${inline(v)}`).join(',\n') ,
    '      },',
    `      "defaults": ${inline(m.defaults)},`,
    '      "files": [',
    m.files.map((f) => `        ${inline(f)}`).join(',\n'),
    '      ],',
    `      "source": ${inline(m.source)}`,
    '    }',
  ].join('\n'))
  const end = '\n  ],\n  "categories": ['
  if (!text.includes(end)) throw new Error('materials.json 的排版变了：找不到 categories 前的位置')
  text = text.replace(end, `,\n${laid.join(',\n')}${end}`)
  if (!text.includes('{ "id": "character"')) text = text.replace('    { "id": "leather", "label": "皮革" }\n', '    { "id": "leather", "label": "皮革" },\n    { "id": "character", "label": "角色" }\n')
  if (!text.includes('{ "id": "character"')) throw new Error('materials.json 的排版变了：没能加上 character 品类')
  writeFileSync(materialsFile, text)
  console.log('已写入 models.json 与 materials.json；再跑 make generate')
}
