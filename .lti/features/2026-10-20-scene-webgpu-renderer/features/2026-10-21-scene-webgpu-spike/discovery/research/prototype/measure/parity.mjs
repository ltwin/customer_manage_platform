// R0 spike, task 5.1: the editor's WebGL workbench against the WebGPU one on
// the same scene, size and timing; line art against its TSL port.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/parity.mjs <out dir>
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import { chromium } from 'playwright'

const base = process.env.SPIKE_URL || 'http://localhost:5291'
const [out] = process.argv.slice(2)
fs.mkdirSync(out, { recursive: true })
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const page = await browser.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message))
page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
await page.goto(`${base}/spike-webgpu.html?case=parity`)
await page.waitForFunction(() => !!window.__parity)
const report = { workbench: [], line: null }
// The baseline's viewport canvas (2394 × 1692 device pixels) and group 4's 3200 × 2000; each pipeline twice, alternating.
for (const [width, height] of [[2394, 1692], [3200, 2000]]) {
  for (let round = 0; round < 2; round++) {
    for (const which of ['legacyWorkbench', 'webgpuWorkbench']) {
      const { ms } = await page.evaluate(([f, w, h]) => window.__parity[f](w, h, 60), [which, width, height])
      report.workbench.push({ which, width, height, round, ms })
      console.log(`${which} ${width} × ${height} round ${round}: ${ms.toFixed(2)} ms a frame`)
    }
  }
}
const line = await page.evaluate(() => window.__parity.lineCompare(2048, 1365))
const { legacy, tsl, ...numbers } = line
report.line = numbers
console.log(`line art, linePass.ts vs TSL at 2048 × 1365: mean ${numbers.mean.toFixed(3)}, > 8: ${(numbers.overShare * 100).toFixed(3)}%, max ${numbers.max}`)
for (const [name, url] of [['line-legacy', legacy], ['line-tsl', tsl]]) fs.writeFileSync(`${out}/${name}.png`, Buffer.from(url.split(',')[1], 'base64'))
execFileSync('magick', [`${out}/line-legacy.png`, `${out}/line-tsl.png`, '-compose', 'difference', '-composite', '-negate', `${out}/line-diff.png`])
execFileSync('magick', [`${out}/line-legacy.png`, `${out}/line-tsl.png`, `${out}/line-diff.png`, '-gravity', 'center', '-crop', '640x640+0+0', '+repage', '+append', `${out}/line-compare.png`])
for (const name of ['line-legacy', 'line-tsl', 'line-diff']) fs.rmSync(`${out}/${name}.png`)
report.errors = errors.slice(0, 6)
if (errors.length) console.log(errors.slice(0, 4).map((e) => e.slice(0, 300)).join('\n'))
fs.writeFileSync(`${out}/parity.json`, `${JSON.stringify(report, null, 2)}\n`)
await browser.close()
