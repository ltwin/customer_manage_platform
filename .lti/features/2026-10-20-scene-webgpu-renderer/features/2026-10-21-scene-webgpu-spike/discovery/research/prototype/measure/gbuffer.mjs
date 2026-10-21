// R0 spike, group 4: opaque-only normals and depth by a pre-pass (workbench, MSAA) or one MRT pass
// (real-time GI, TRAA); switching them; SMAA in compatibility mode.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/gbuffer.mjs <out dir>
import fs from 'node:fs'
import { chromium } from 'playwright'
import { savePNG } from './compare.mjs'

const base = process.env.SPIKE_URL || 'http://localhost:5291'
const out = process.argv[2]
fs.mkdirSync(out, { recursive: true })
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const page = await browser.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message))
page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
await page.goto(`${base}/spike-webgpu.html?case=gbuffer`)
await page.waitForFunction(() => !!window.__gbuffer)
const report = { frameMs: {}, switch: null, compatibility: null }
for (const variant of (process.env.ONLY_SWITCH ? [] : ['prepass', 'mrt'])) {
  const r = await page.evaluate((v) => window.__gbuffer.renderGBuffer(v, 'picture', 3200, 2000, 60).then((x) => x.ms), variant)
  report.frameMs[variant] = r
  console.log(`${variant}: ${r.toFixed(2)} ms a frame at 3200 × 2000 (picture, 60 frames)`)
}
for (const variant of (process.env.ONLY_SWITCH ? [] : ['prepass', 'mrt', 'mrt-unmasked'])) for (const show of ['picture', 'normal', 'ao']) {
  if (variant === 'mrt-unmasked' && show !== 'normal') continue
  const r = await page.evaluate((o) => window.__gbuffer.renderGBuffer(o.variant, o.show, 1600, 1000, 1), { variant, show })
  savePNG(fs, `${out}/gbuffer-${variant}-${show}.png`, r.png)
}
report.switch = await page.evaluate(() => window.__gbuffer.switchPipelines(12, 1600, 1000))
console.log(`textures after each of 12 switches: ${report.switch.join(' ')}`)
const c = await page.evaluate(() => window.__gbuffer.compatibilitySMAA(1600, 1000))
if (c.error) console.log(`compatibility: ${c.error}`)
else {
  savePNG(fs, `${out}/compat-none.png`, c.none)
  savePNG(fs, `${out}/compat-smaa.png`, c.smaa)
  console.log(`compatibility device: compatibilityMode=${c.compatibilityMode}, samples=${c.samples}`)
}
report.compatibility = c.error ? { error: c.error } : { compatibilityMode: c.compatibilityMode, samples: c.samples }
report.errors = errors.slice(0, 10)
if (errors.length) console.log(errors.slice(0, 5).join('\n'))
fs.writeFileSync(`${out}/gbuffer${process.env.ONLY_SWITCH ? '-switch' : ''}.json`, `${JSON.stringify(report, null, 2)}\n`)
await browser.close()
