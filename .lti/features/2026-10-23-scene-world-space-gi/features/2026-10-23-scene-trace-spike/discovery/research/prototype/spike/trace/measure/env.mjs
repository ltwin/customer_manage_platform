// S0 spike, the page and its device: opens spike-trace.html on this machine's GPU for the box room and the
// stress scene, with the limits the adapter has and with WebGPU's defaults; writes what each was given
// (env.json) and a screenshot of each.
//   node spike/trace/measure/env.mjs <dev server, e.g. http://localhost:5191> <out dir>
import { mkdirSync, writeFileSync } from 'node:fs'
import { chromium } from 'playwright'

const [base, out] = process.argv.slice(2)
if (!base || !out) throw new Error('usage: env.mjs <dev server> <out dir>')
mkdirSync(out, { recursive: true })

// The scene e2e's Metal arguments: a real GPU, WebGPU without flags (R0 environment.md).
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const cases = [
  ['cornell', '?scene=cornell'],
  ['cornell-default-limits', '?scene=cornell&limits=default'],
  ['stress', '?scene=stress'],
  ['stress-default-limits', '?scene=stress&limits=default'],
]
const results = { chromium: browser.version(), cases: {} }
try {
  for (const [label, q] of cases) {
    const page = await browser.newPage({ viewport: { width: 1500, height: 1000 } })
    const errors = []
    page.on('pageerror', (e) => errors.push(e.message))
    page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
    const started = Date.now()
    await page.goto(`${base}/spike-trace.html${q}`)
    await page.waitForFunction(() => window.__spike?.ready || window.__spike?.error, null, { timeout: 180_000 })
    const error = await page.evaluate(() => window.__spike.error)
    const report = error ? null : await page.evaluate(() => window.__spike.report())
    await page.screenshot({ path: `${out}/${label}.png` })
    results.cases[label] = { readyMs: Date.now() - started, error, errors, report }
    console.log(`${label}: ${error ?? `ready in ${Date.now() - started} ms`}`)
    await page.close()
  }
} finally {
  await browser.close()
}
writeFileSync(`${out}/env.json`, `${JSON.stringify(results, null, 2)}\n`)
