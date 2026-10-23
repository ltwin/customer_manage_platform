// Builds src/creative-scene/world/nightSky/moon.webp (W3 design「星表与月面贴图的构建」)
// from NASA's Scientific Visualization Studio CGI Moon Kit (svs.gsfc.nasa.gov/4720,
// public domain, docs/third-party-notices.md): the colour map from LRO LROC
// WAC, grey, 1024 × 512 (equirectangular, longitude −180…180 left to right,
// north up), scaled so that its mean linear albedo is 0.5: the renderer
// multiplies by 2 for a mean of 1, the moon's own albedo coming from its
// magnitude. Needs libvips (`vips`) and cwebp on the path.
//
// Run by hand: node scripts/night-sky/build-moon.mjs <lroc_color_2k.jpg>
// The map is https://svs.gsfc.nasa.gov/vis/a000000/a004700/a004720/lroc_color_2k.jpg (sha256 below).
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const SHA256 = 'f7130a1822681fa7512d7dcfd40db8c10b9ba4f06777910348698260ed7a2170'
const [W, H] = [1024, 512]

const source = process.argv[2]
const sum = createHash('sha256').update(readFileSync(source)).digest('hex')
if (sum !== SHA256) throw new Error(`${source}: sha256 ${sum}, expected ${SHA256}`)
const dir = mkdtempSync(join(tmpdir(), 'moon-'))
try {
  execFileSync('vips', ['resize', source, join(dir, 'half.v'), String(W / 2048)])
  execFileSync('vips', ['colourspace', join(dir, 'half.v'), join(dir, 'grey.v'), 'b-w'])
  execFileSync('vips', ['rawsave', join(dir, 'grey.v'), join(dir, 'grey.raw')])
  const grey = readFileSync(join(dir, 'grey.raw'))
  if (grey.length !== W * H) throw new Error(`grey: ${grey.length} bytes`)
  const toLinear = (c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4)
  const toSrgb = (l) => (l <= 0.0031308 ? 12.92 * l : 1.055 * l ** (1 / 2.4) - 0.055)
  let mean = 0
  for (const v of grey) mean += toLinear(v / 255)
  mean /= grey.length
  const k = 0.5 / mean
  const scaled = Buffer.from(grey.map((v) => Math.round(255 * toSrgb(Math.min(1, toLinear(v / 255) * k)))))
  writeFileSync(join(dir, 'scaled.raw'), scaled)
  execFileSync('vips', ['rawload', join(dir, 'scaled.raw'), join(dir, 'scaled.png'), String(W), String(H), '1'])
  execFileSync('cwebp', ['-quiet', '-q', '80', join(dir, 'scaled.png'), '-o', new URL('../../src/creative-scene/world/nightSky/moon.webp', import.meta.url).pathname])
  let clipped = 0
  for (const v of grey) if (toLinear(v / 255) * k > 1) clipped++
  console.log(`mean linear albedo ${mean.toFixed(4)} scaled by ${k.toFixed(3)}; ${((clipped / grey.length) * 100).toFixed(3)}% clipped`)
} finally {
  rmSync(dir, { recursive: true, force: true })
}
