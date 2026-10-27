// The light size measurement page from the command line: node scripts/light-check/run.mjs '<query>' [panels.png]
// on the dev server at CREATIVE_SCENE_URL (http://localhost:5179 by default). Prints the page's report as JSON.
import { chromium } from 'playwright'
const q = process.argv[2] ?? ''
const out = process.argv[3]
const gpu = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const page = await gpu.newPage({ viewport: { width: 2000, height: 1400 } })
const errors = []
page.on('pageerror', (e) => errors.push(String(e)))
page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
await page.goto(`${process.env.CREATIVE_SCENE_URL || 'http://localhost:5179'}/scripts/light-check/index.html?${q}`)
await page.waitForFunction(() => window.__lightCheck?.done || window.__lightCheck?.error, null, { timeout: 300000 })
const r = await page.evaluate(() => window.__lightCheck)
if (out) await page.locator('#panels').screenshot({ path: out })
console.log(JSON.stringify({ error: r.error, errors: errors.slice(0, 5), report: r.report }, null, 1))
await gpu.close()
