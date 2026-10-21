// R0 spike, group 3: the selection outline, back-face hull (drei Outlines in TSL) against OutlineNode, with timings.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/outline.mjs <out dir>
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
await page.goto(`${base}/spike-webgpu.html?case=outline`)
await page.waitForFunction(() => !!window.__outline)
const report = {}
for (const variant of ['none', 'hull', 'node']) {
  const r = await page.evaluate((v) => window.__outline.renderOutline(v, 2400, 1600), variant)
  savePNG(fs, `${out}/outline-${variant}.png`, r.png)
  report[variant] = { msPerFrame: r.msPerFrame }
  console.log(`${variant}: ${r.msPerFrame.toFixed(2)} ms a frame at 2400 × 1600`)
}
report.errors = errors.slice(0, 10)
if (errors.length) console.log(errors.slice(0, 5).join('\n'))
fs.writeFileSync(`${out}/outline.json`, `${JSON.stringify(report, null, 2)}\n`)
await browser.close()
