// R0 spike, group 2: GTAO through orthographic and perspective cameras, three's node and the fixed copy.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/ao.mjs <out dir>
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
await page.goto(`${base}/spike-webgpu.html?case=ao`)
await page.waitForFunction(() => !!window.__ao)
const shots = {}
for (const kind of ['ortho', 'persp']) for (const node of ['three', 'fork']) {
  shots[`${kind}-${node}`] = await page.evaluate(([k, n]) => window.__ao.renderAO(k, n, 1200, 800), [kind, node])
  savePNG(fs, `${out}/ao-${kind}-${node}.png`, shots[`${kind}-${node}`])
}
const report = {}
for (const kind of ['ortho', 'persp']) {
  const r = await compareInPage(page, shots[`${kind}-three`], shots[`${kind}-fork`])
  report[`${kind}: three vs fork`] = { mean: r.mean, over8: r.over8, max: r.max }
  console.log(`${kind}: three vs fork mean ${r.mean.toFixed(3)}, ${(r.over8 * 100).toFixed(2)}% over 8`)
}
report.errors = errors.slice(0, 10)
if (errors.length) console.log(errors.slice(0, 5).join('\n'))
fs.writeFileSync(`${out}/ao.json`, `${JSON.stringify(report, null, 2)}\n`)
await browser.close()
