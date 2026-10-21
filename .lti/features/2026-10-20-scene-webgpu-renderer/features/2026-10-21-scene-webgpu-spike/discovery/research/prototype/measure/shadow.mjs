// R0 spike, group 3: the sun's shadow, WebGL PCFSoftShadowMap against WebGPU PCF radii and VSM.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/shadow.mjs <out dir>
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
await page.goto(`${base}/spike-webgpu.html?case=shadow`)
await page.waitForFunction(() => !!window.__shadow)
const report = {}
for (const scene of ['noon', 'room']) {
  const legacy = await page.evaluate((s) => window.__shadow.shadowLegacy(s, 1536, 1024), scene)
  savePNG(fs, `${out}/shadow-${scene}-webgl-pcfsoft.png`, legacy)
  for (const [type, radius] of [['pcf', 1], ['pcf', 2], ['pcf', 3], ['pcf', 4], ['vsm', 4]]) {
    const tsl = await page.evaluate((o) => window.__shadow.shadowTSL(o.scene, o.type, o.radius, 1536, 1024), { scene, type, radius })
    savePNG(fs, `${out}/shadow-${scene}-webgpu-${type}${radius}.png`, tsl)
    const r = await compareInPage(page, legacy, tsl)
    report[`${scene} ${type} r${radius}`] = { mean: r.mean, over8: r.over8, max: r.max }
    console.log(`${scene} ${type} r${radius}: mean ${r.mean.toFixed(3)}, ${(r.over8 * 100).toFixed(3)}% over 8, max ${r.max}`)
  }
}
report.errors = errors.slice(0, 10)
if (errors.length) console.log(errors.slice(0, 5).join('\n'))
fs.writeFileSync(`${out}/shadow.json`, `${JSON.stringify(report, null, 2)}\n`)
await browser.close()
