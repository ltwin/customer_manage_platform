// R0 spike, group 3: the two skies, the editor's on WebGL against the TSL ports on WebGPU.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/sky.mjs <out dir>
import fs from 'node:fs'
import { chromium } from 'playwright'
import { compareInPage, savePNG } from './compare.mjs'

const base = process.env.SPIKE_URL || 'http://localhost:5291'
const out = process.argv[2]
fs.mkdirSync(out, { recursive: true })
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const page = await browser.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message))
page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
await page.goto(`${base}/spike-webgpu.html?case=sky`)
await page.waitForFunction(() => !!window.__sky)
const report = {}
for (const scene of ['noon', 'dusk']) for (const mode of ['clay', 'lit']) for (const toward of [true, false]) {
  const name = `${scene}-${mode}-${toward ? 'toward' : 'away'}`
  const a = await page.evaluate((o) => window.__sky.skyLegacy(o.scene, o.mode, o.toward, 800, 600), { scene, mode, toward })
  const b = await page.evaluate((o) => window.__sky.skyTSL(o.scene, o.mode, o.toward, 800, 600), { scene, mode, toward })
  savePNG(fs, `${out}/sky-${name}-glsl.png`, a)
  savePNG(fs, `${out}/sky-${name}-tsl.png`, b)
  const r = await compareInPage(page, a, b)
  report[name] = { mean: r.mean, over8: r.over8, max: r.max }
  console.log(`${name}: mean ${r.mean.toFixed(3)}, ${(r.over8 * 100).toFixed(3)}% over 8, max ${r.max}`)
}
for (const scene of ['noon', 'dusk']) {
  const a = await page.evaluate((s) => window.__sky.envLegacy(s, 1200, 600), scene)
  const b = await page.evaluate((s) => window.__sky.envTSL(s, 1200, 600), scene)
  savePNG(fs, `${out}/env-${scene}-glsl.png`, a)
  savePNG(fs, `${out}/env-${scene}-tsl.png`, b)
  const r = await compareInPage(page, a, b)
  savePNG(fs, `${out}/env-${scene}-diff.png`, r.diff)
  report[`env ${scene}`] = { mean: r.mean, over8: r.over8, max: r.max }
  console.log(`env ${scene}: mean ${r.mean.toFixed(3)}, ${(r.over8 * 100).toFixed(3)}% over 8, max ${r.max}`)
}
report.errors = errors.slice(0, 10)
if (errors.length) console.log(errors.slice(0, 5).join('\n'))
fs.writeFileSync(`${out}/sky.json`, `${JSON.stringify(report, null, 2)}\n`)
await browser.close()
