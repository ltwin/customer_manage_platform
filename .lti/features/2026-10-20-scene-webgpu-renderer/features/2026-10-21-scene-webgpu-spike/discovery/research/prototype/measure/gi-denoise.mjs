// R0 spike, group 5: the GI denoised (temporal reprojection + recurrent denoise) and, for exports, every
// frame averaged; convergence at 2048 × 1365, frame time at 1600 × 1000, and the WebGL2 backend.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/gi-denoise.mjs <out dir> <scratch dir>
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import { chromium } from 'playwright'

const base = process.env.SPIKE_URL || 'http://localhost:5291'
const [out, scratch] = process.argv.slice(2)
const jpeg = (name, url) => {
  const png = `${scratch}/${name}.png`
  fs.writeFileSync(png, Buffer.from(url.split(',')[1], 'base64'))
  execFileSync('magick', [png, '-quality', '90', `${out}/${name}.jpg`])
}
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const page = await browser.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message))
page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
await page.goto(`${base}/spike-webgpu.html?case=gi`)
await page.waitForFunction(() => !!window.__gi)
const report = { convergence: {}, frameMs: {} }
const samples = [2, 4, 8, 12, 16, 24, 36, 48, 64, 96, 128]
for (const [name, o] of [['full-dn', { variant: 'full-dn' }], ['full-dn-acc', { variant: 'full-dn', accumulate: true }], ['full-acc', { variant: 'full', accumulate: true }]]) {
  for (const k of [12, 36, 128]) {
    const r = await page.evaluate((o) => window.__gi.renderGI(o), { ...o, width: 2048, height: 1365, k, samples: k === 128 ? samples : [] })
    jpeg(`gi-${name}-${k}`, r.png)
    if (k === 128) {
      report.convergence[name] = r.changes
      console.log(`${name}: change per frame ${r.changes.map((c) => `${c.frame}:${c.change.toFixed(2)}`).join(' ')}`)
    }
  }
}
for (const [name, o] of [['full-dn', { variant: 'full-dn' }], ['full-dn-acc', { variant: 'full-dn', accumulate: true }]]) {
  for (const [w, h] of [[1600, 1000], [2048, 1365]]) {
    const r = await page.evaluate((o) => window.__gi.renderGI(o).then((x) => x.msPerFrame), { ...o, width: w, height: h, k: 12 })
    report.frameMs[`${name} ${w}x${h}`] = r
    console.log(`${name}: ${r.toFixed(2)} ms a frame at ${w} × ${h}`)
  }
}
const before = errors.length
const gl = await page.evaluate((o) => window.__gi.renderGI(o), { variant: 'full-dn', accumulate: true, width: 2048, height: 1365, k: 36, forceWebGL: true })
jpeg('gi-full-dn-acc-36-webgl2', gl.png)
report.webgl2 = { backend: gl.backend, msPerFrame: gl.msPerFrame, errors: errors.slice(before, before + 6) }
console.log(`webgl2 (${gl.backend}): ${gl.msPerFrame.toFixed(2)} ms a frame at 2048 × 1365; ${errors.length - before} errors`)
report.errors = errors.slice(0, 10)
if (errors.length) console.log(errors.slice(0, 5).map((e) => e.slice(0, 300)).join('\n'))
fs.writeFileSync(`${out}/gi-denoise.json`, `${JSON.stringify(report, null, 2)}\n`)
await browser.close()
