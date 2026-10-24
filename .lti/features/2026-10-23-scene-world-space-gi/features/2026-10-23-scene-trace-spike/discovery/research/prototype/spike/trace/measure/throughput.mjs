// S0 group 2, throughput and limits: spike-trace.html?case=throughput on this machine's GPU. For each scene and each
// way of asking for limits, in one browser context (its IndexedDB and shader caches kept between pages):
//   cold  the geometry cache cleared: prepare, pack and the first traced frame
//   warm  the page again, from the cache: the same, then the ray timings and the edits
// throughput.json gathers them, written after each page.
//   node spike/trace/measure/throughput.mjs <dev server> <out dir> [scene,…] [adapter|default,…] [ε in metres,…] [warm parts]
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { chromium } from 'playwright'

const [base, out, only, limitsArg, epsArg, partsArg] = process.argv.slice(2)
if (!base || !out) throw new Error('usage: throughput.mjs <dev server> <out dir> [scenes] [limits]')
mkdirSync(out, { recursive: true })
const scenes = only ? only.split(',') : ['cornell', 'backlit-room', 'mirror-board', 'stress']
const limitsList = limitsArg ? limitsArg.split(',') : ['adapter', 'default']
const epsList = epsArg ? epsArg.split(',') : ['0.01']
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
let results = { chromium: browser.version(), runs: {} }
// Runs add to an existing throughput.json: one scene or one ε at a time.
try {
  results = { ...JSON.parse(readFileSync(`${out}/throughput.json`, 'utf8')), chromium: browser.version() }
} catch {}
const write = () => writeFileSync(`${out}/throughput.json`, `${JSON.stringify(results, null, 2)}\n`)
try {
  for (const scene of scenes) {
    const context = await browser.newContext({ viewport: { width: 1500, height: 1000 } })
    for (const limits of limitsList) for (const eps of epsList) {
      for (const [label, q] of [['cold', 'cache=clear&part=none'], ['warm', `cache=use&part=${partsArg ?? 'rays,edit'}`]]) {
        const page = await context.newPage()
        const errors = []
        page.on('pageerror', (e) => errors.push(e.message))
        page.on('console', (m) => (m.type() === 'error' || m.type() === 'warning') && errors.push(m.text().slice(0, 400)))
        const started = Date.now()
        await page.goto(`${base}/spike-trace.html?scene=${scene}&case=throughput&${q}&eps=${eps}${limits === 'default' ? '&limits=default' : ''}`)
        const settled = await page.waitForFunction(() => window.__spike?.ready || window.__spike?.error, null, { timeout: 600_000, polling: 500 }).then(() => true, () => false)
        const error = settled ? await page.evaluate(() => window.__spike.error) : 'timed out after 600 s'
        const key = `${scene}/${limits}/${eps}/${label}`
        results.runs[key] = error ? { error, errors } : { pageMs: Date.now() - started, ...(await page.evaluate(() => ({ report: window.__spike.report(), result: window.__spike.result }))), errors: [...new Set(errors)].slice(0, 20) }
        console.log(`${key}: ${error ? error.split('\n')[0] : `done in ${Date.now() - started} ms`}`)
        await page.close()
        write()
      }
    }
    await context.close()
  }
} finally {
  await browser.close()
}
write()
