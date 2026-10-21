// R0 spike, group 2: the editor's DofPass on WebGL against its TSL port on
// WebGPU (and the WebGL2 backend), the dof reference scene through camera A.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/dof.mjs <out dir>
import fs from 'node:fs'
import { chromium } from 'playwright'
import { compareInPage, savePNG } from './compare.mjs'

const base = process.env.SPIKE_URL || 'http://localhost:5291'
const out = process.argv[2]
if (!out) throw new Error('usage: dof.mjs <out dir>')
fs.mkdirSync(out, { recursive: true })
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const page = await browser.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message))
page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
await page.goto(`${base}/spike-webgpu.html?case=dof`)
await page.waitForFunction(() => !!window.__dof)
const [w, h] = [1536, 1024]
const legacy = await page.evaluate(([w, h]) => window.__dof.dofLegacy(w, h), [w, h])
const tsl = await page.evaluate(([w, h]) => window.__dof.dofTSL(w, h, false), [w, h])
const tslGL = await page.evaluate(([w, h]) => window.__dof.dofTSL(w, h, true), [w, h])
savePNG(fs, `${out}/dof-glsl-webgl.png`, legacy)
savePNG(fs, `${out}/dof-tsl-webgpu.png`, tsl)
savePNG(fs, `${out}/dof-tsl-webgl2.png`, tslGL)
const report = {}
for (const [name, a, b] of [['glsl vs tsl(webgpu)', legacy, tsl], ['glsl vs tsl(webgl2)', legacy, tslGL]]) {
  const r = await compareInPage(page, a, b)
  savePNG(fs, `${out}/dof-diff-${name.replace(/[^a-z0-9]+/gi, '-')}.png`, r.diff)
  report[name] = { mean: r.mean, over8: r.over8, max: r.max }
  console.log(`${name}: mean ${r.mean.toFixed(3)}, ${(r.over8 * 100).toFixed(3)}% over 8, max ${r.max}`)
}
report.errors = errors.slice(0, 10)
if (errors.length) console.log(errors.slice(0, 5).join('\n'))
fs.writeFileSync(`${out}/dof.json`, `${JSON.stringify(report, null, 2)}\n`)
await browser.close()
