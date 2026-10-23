// W5 spike driver: node measure/run.mjs '<json case or [cases]>' [webgpu|webgl|swiftshader] [outdir]
// Prints each case's numbers; saves its picture as <outdir>/<name>-<backend>.png.
import fs from 'node:fs'
import path from 'node:path'
import { chromium } from '/Users/samson/workspace/my_project/customer_manage_platform/frontend/node_modules/playwright/index.mjs'
const [, , json = '{}', mode = 'webgpu', outdir = 'out'] = process.argv
const base = process.env.SPIKE_URL || 'http://localhost:5294'
const cases = [].concat(JSON.parse(json.startsWith('@') ? fs.readFileSync(json.slice(1), 'utf8') : json))
const args = mode === 'swiftshader' ? ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'] : ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu']
const browser = await chromium.launch({ headless: true, args })
const page = await browser.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message))
page.on('console', (m) => (m.type() === 'error' || m.type() === 'warning') && errors.push(m.text().slice(0, 600)))
await page.goto(`${base}/index.html${mode === 'webgpu' ? '' : '?backend=webgl'}`)
await page.waitForFunction(() => !!window.__spike, null, { timeout: 120000 })
fs.mkdirSync(outdir, { recursive: true })
for (const c of cases) {
  const { name = 'case', ...a } = c
  const r = await page.evaluate((x) => window.__spike.run(x), a)
  if (r.png) {
    fs.writeFileSync(path.join(outdir, `${name}-${mode}.png`), Buffer.from(r.png.split(',')[1], 'base64'))
    delete r.png
  }
  console.log(JSON.stringify({ name, ...r }))
}
if (errors.length) console.error('ERRORS:\n' + [...new Set(errors)].slice(0, 10).join('\n'))
await browser.close()
