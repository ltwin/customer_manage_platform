// Builds src/creative-scene/world/nightSky/stars.bin (W3 design「星表与月面贴图的构建」)
// from the Yale Bright Star Catalogue, 5th revised edition (Hoffleit & Warren
// 1991, CDS V/50, docs/third-party-notices.md): the stars of V ≤ 6.5,
// brightest first, column by column, little-endian:
//
//   'BSC5', uint32 n, float32 ra[n] (J2000, degrees), float32 dec[n],
//   uint8 v[n] (V = −1.5 + 0.05 q), uint8 bv[n] (B−V = −0.5 + 0.02 q; 0.6, sun-like, where the catalogue has none)
//
// Run by hand: node scripts/night-sky/build-stars.mjs <catalog.gz>
// The catalogue is https://cdsarc.cds.unistra.fr/ftp/V/50/catalog.gz (sha256 below).
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { gunzipSync } from 'node:zlib'

const SHA256 = '3dc44b1e90be8fbe5bcc7656032560f51275f985c7e3f783c9028e1838ec7bed'
const LIMIT = 6.5

const gz = readFileSync(process.argv[2])
const sum = createHash('sha256').update(gz).digest('hex')
if (sum !== SHA256) throw new Error(`catalog.gz: sha256 ${sum}, expected ${SHA256}`)
const stars = []
for (const raw of gunzipSync(gz).toString('latin1').split('\n')) {
  const line = raw.padEnd(197)
  const v = line.slice(102, 107).trim()
  // Entries without a J2000 position or a magnitude are novae and the like, dropped from the catalogue.
  if (!line.slice(75, 83).trim() || !v) continue
  const ra = 15 * (Number(line.slice(75, 77)) + Number(line.slice(77, 79)) / 60 + Number(line.slice(79, 83)) / 3600)
  const dec = (line[83] === '-' ? -1 : 1) * (Number(line.slice(84, 86)) + Number(line.slice(86, 88)) / 60 + Number(line.slice(88, 90)) / 3600)
  const bv = line.slice(109, 114).trim()
  if (Number(v) <= LIMIT) stars.push({ ra, dec, v: Number(v), bv: bv ? Number(bv) : 0.6 })
}
stars.sort((a, b) => a.v - b.v)
const n = stars.length
const q = (x, lo, step) => Math.max(0, Math.min(255, Math.round((x - lo) / step)))
const out = Buffer.alloc(8 + n * 10)
out.write('BSC5', 0, 'latin1')
out.writeUInt32LE(n, 4)
stars.forEach((s, i) => {
  out.writeFloatLE(s.ra, 8 + i * 4)
  out.writeFloatLE(s.dec, 8 + n * 4 + i * 4)
  out[8 + n * 8 + i] = q(s.v, -1.5, 0.05)
  out[8 + n * 9 + i] = q(s.bv, -0.5, 0.02)
})
writeFileSync(new URL('../../src/creative-scene/world/nightSky/stars.bin', import.meta.url), out)
console.log(`${n} stars, ${out.length} bytes`)
