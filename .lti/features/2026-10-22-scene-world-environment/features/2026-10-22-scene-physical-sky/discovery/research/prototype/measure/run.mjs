// W2 spike driver: node measure/run.mjs <case> '<json args>' [webgpu|webgl|swiftshader]
import { chromium } from '/Users/samson/workspace/my_project/customer_manage_platform/frontend/node_modules/playwright/index.mjs'
const [, , name, argJson = '{}', mode = 'webgpu'] = process.argv
const base = process.env.SPIKE_URL || 'http://localhost:5292'
const args = mode === 'swiftshader' ? ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'] : ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu']
const browser = await chromium.launch({ headless: true, args })
const page = await browser.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message))
page.on('console', (m) => (m.type() === 'error' || m.type() === 'warning') && errors.push(m.text().slice(0, 400)))
await page.goto(`${base}/index.html${mode === 'webgpu' ? '' : '?backend=webgl'}`)
await page.waitForFunction(() => !!window.__spike, null, { timeout: 120000 })
const result = await page.evaluate(([n, a]) => window.__spike[n](JSON.parse(a)), [name, argJson])
console.log(JSON.stringify(result, null, 1))
if (errors.length) console.error('ERRORS:\n' + errors.slice(0, 8).join('\n'))
await browser.close()
