// W2 spike: run a list of shots, save each PNG and the stats.
//   node measure/shots.mjs <list.json> <out dir> [webgpu|webgl|swiftshader]
import fs from 'node:fs'
import { chromium } from '/Users/samson/workspace/my_project/customer_manage_platform/frontend/node_modules/playwright/index.mjs'
const [, , listPath, out, mode = 'webgpu'] = process.argv
const list = JSON.parse(fs.readFileSync(listPath, 'utf8'))
fs.mkdirSync(out, { recursive: true })
const base = process.env.SPIKE_URL || 'http://localhost:5292'
const args = mode === 'swiftshader' ? ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'] : ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu']
const browser = await chromium.launch({ headless: true, args })
const page = await browser.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message))
page.on('console', (m) => m.type() === 'error' && errors.push(m.text().slice(0, 400)))
await page.goto(`${base}/index.html${mode === 'webgpu' ? '' : '?backend=webgl'}`)
await page.waitForFunction(() => !!window.__spike, null, { timeout: 120000 })
const report = {}
for (const { name, args: a, fn = 'shot' } of list) {
  const r = await page.evaluate(([f, x]) => window.__spike[f](x), [fn, a])
  if (r.png) fs.writeFileSync(`${out}/${name}-${mode}.png`, Buffer.from(r.png.split(',')[1], 'base64'))
  delete r.png
  report[name] = r
  const s = r.stats ?? {}
  console.log(`${name}: mean ${s.mean?.toFixed(1)} ground ${s.ground?.toFixed(1)} clipped ${((s.clipped ?? 0) * 100).toFixed(2)}% ev ${r.ev?.toFixed?.(2)} metered ${r.meteredEv?.toFixed?.(2)} E ${r.E?.toFixed?.(1)} hdrMax ${r.hdr?.max?.toFixed?.(1)} nonFinite ${r.hdr?.nonFinite} sub ${r.hdr?.subnormal?.toFixed?.(4)} resid ${r.residualEv?.toFixed?.(2)} t ${JSON.stringify(r.timings ?? {})}`)
}
fs.writeFileSync(`${out}/report-${mode}.json`, `${JSON.stringify({ errors: errors.slice(0, 10), report }, null, 1)}\n`)
if (errors.length) console.error('ERRORS:\n' + errors.slice(0, 6).join('\n'))
await browser.close()
