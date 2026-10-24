// S0 group 2: spike-trace.html?case=parallel, once as the product serves pages and once cross-origin isolated (COOP
// and COEP added to every response), so three-mesh-bvh's ParallelMeshBVHWorker can run; parallel.json.
//   node spike/trace/measure/parallel.mjs <dev server> <out dir>
import { mkdirSync, writeFileSync } from 'node:fs'
import { chromium } from 'playwright'

const [base, out] = process.argv.slice(2)
if (!base || !out) throw new Error('usage: parallel.mjs <dev server> <out dir>')
mkdirSync(out, { recursive: true })
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const results = { chromium: browser.version() }
try {
  for (const isolated of [false, true]) {
    const context = await browser.newContext()
    if (isolated) {
      await context.route('**/*', async (route) => {
        const response = await route.fetch()
        await route.fulfill({ response, headers: { ...response.headers(), 'cross-origin-opener-policy': 'same-origin', 'cross-origin-embedder-policy': 'require-corp' } })
      })
    }
    const page = await context.newPage()
    const errors = []
    page.on('pageerror', (e) => errors.push(e.message))
    page.on('console', (m) => (m.type() === 'error' || m.type() === 'warning') && errors.push(m.text().slice(0, 400)))
    // The smallest scene: the case builds its own geometry.
    await page.goto(`${base}/spike-trace.html?scene=thin-wall&case=parallel`)
    await page.waitForFunction(() => window.__spike?.ready || window.__spike?.error, null, { timeout: 600_000, polling: 500 })
    const error = await page.evaluate(() => window.__spike.error)
    results[isolated ? 'isolated' : 'asServed'] = error ? { error, errors } : { result: await page.evaluate(() => window.__spike.result), errors }
    console.log(isolated ? 'isolated' : 'as served', JSON.stringify(results[isolated ? 'isolated' : 'asServed']).slice(0, 600))
    await context.close()
  }
} finally {
  await browser.close()
}
writeFileSync(`${out}/parallel.json`, `${JSON.stringify(results, null, 2)}\n`)
