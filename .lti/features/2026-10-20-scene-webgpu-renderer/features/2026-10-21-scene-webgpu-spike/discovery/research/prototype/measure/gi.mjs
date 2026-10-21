// R0 spike, group 5 (task 4.1): real-time GI on the box room. Pictures and
// convergence at the export size, frame time at the viewport's 1600 × 1000
// with 2× pixels, and the WebGL2 backend. Pictures are kept as JPEG.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/gi.mjs <out dir> [scratch dir]
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import { chromium } from 'playwright'

const base = process.env.SPIKE_URL || 'http://localhost:5291'
const [out, scratch = out] = process.argv.slice(2)
fs.mkdirSync(out, { recursive: true })
const jpeg = (name, url) => {
  const png = `${scratch}/${name}.png`
  fs.writeFileSync(png, Buffer.from(url.split(',')[1], 'base64'))
  execFileSync('magick', [png, '-quality', '90', `${out}/${name}.jpg`])
  if (scratch !== out) fs.rmSync(png)
}
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const page = await browser.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message))
page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
await page.goto(`${base}/spike-webgpu.html?case=gi`)
await page.waitForFunction(() => !!window.__gi)
const report = { convergence: {}, frameMs: {}, webgl2: null }
const samples = [2, 4, 8, 12, 16, 24, 36, 48, 64, 96]
for (const variant of ['base', 'ssgi', 'full']) {
  const r = await page.evaluate((o) => window.__gi.renderGI(o), { variant, width: 2048, height: 1365, k: 96, samples })
  jpeg(`gi-${variant}-2048`, r.png)
  report.convergence[variant] = r.changes
  console.log(`${variant} at 2048 × 1365: change per frame ${r.changes.map((c) => `${c.frame}:${c.change.toFixed(2)}`).join(' ')}`)
}
for (const variant of ['base', 'ssgi', 'full']) {
  const r = await page.evaluate((o) => window.__gi.renderGI(o).then((x) => x.msPerFrame), { variant, width: 3200, height: 2000, k: 12 })
  report.frameMs[variant] = r
  console.log(`${variant}: ${r.toFixed(2)} ms a frame at 3200 × 2000`)
}
for (const variant of ['ssgi', 'full']) {
  const r = await page.evaluate((o) => window.__gi.renderGI(o).then((x) => x.msPerFrame), { variant, width: 1600, height: 1000, k: 12 })
  report.frameMs[`${variant} 1600x1000`] = r
  console.log(`${variant}: ${r.toFixed(2)} ms a frame at 1600 × 1000`)
}
const before = errors.length
const gl = await page.evaluate((o) => window.__gi.renderGI(o), { variant: 'full', width: 2048, height: 1365, k: 36, forceWebGL: true })
jpeg('gi-full-2048-webgl2', gl.png)
report.webgl2 = { backend: gl.backend, msPerFrame: gl.msPerFrame, errors: errors.slice(before, before + 6) }
console.log(`webgl2 backend (${gl.backend}): ${gl.msPerFrame.toFixed(2)} ms a frame at 2048 × 1365; ${errors.length - before} errors`)
report.errors = errors.slice(0, 10)
if (errors.length) console.log(errors.slice(0, 5).map((e) => e.slice(0, 300)).join('\n'))
fs.writeFileSync(`${out}/gi.json`, `${JSON.stringify(report, null, 2)}\n`)
await browser.close()
