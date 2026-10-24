// S0 group 3, hit shading: spike-trace.html?scene=materials&case=shading on this machine's GPU, three loads in one
// browser: cold (?cold=1, a constant new to every pipeline cache), then twice warm. shading.json holds the raster's
// sameness, the compile times and the comparison per mesh of each load; the pictures of the last load as PNGs.
//   node spike/trace/measure/shading.mjs <dev server> <out dir> [scene]
import { mkdirSync, writeFileSync } from 'node:fs'
import { chromium } from 'playwright'

const [base, out, scene = 'materials'] = process.argv.slice(2)
if (!base || !out) throw new Error('usage: shading.mjs <dev server> <out dir> [scene]')
mkdirSync(out, { recursive: true })
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const results = { chromium: browser.version(), scene, loads: {} }
const save = (file, url) => writeFileSync(`${out}/${file}`, Buffer.from(url.slice(url.indexOf(',') + 1), 'base64'))
try {
  const context = await browser.newContext({ viewport: { width: 1500, height: 1000 } })
  for (const [label, q] of [['cold', '&cold=1'], ['warm1', ''], ['warm2', '']]) {
    const page = await context.newPage()
    const errors = []
    page.on('pageerror', (e) => errors.push(e.message))
    page.on('console', (m) => (m.type() === 'error' || m.type() === 'warning') && errors.push(m.text().slice(0, 600)))
    const started = Date.now()
    await page.goto(`${base}/spike-trace.html?scene=${scene}&case=shading${q}`)
    const settled = await page.waitForFunction(() => window.__spike?.ready || window.__spike?.error, null, { timeout: 600_000, polling: 500 }).then(() => true, () => false)
    const error = settled ? await page.evaluate(() => window.__spike.error) : 'timed out after 600 s'
    if (error) results.loads[label] = { error, errors: [...new Set(errors)].slice(0, 20) }
    else {
      const { pictures, ...rest } = await page.evaluate(() => window.__spike.result)
      if (label === 'warm2') for (const [name, url] of Object.entries(pictures)) save(`${scene}-${name}.png`, url)
      results.loads[label] = { pageMs: Date.now() - started, ...rest, errors: [...new Set(errors)].slice(0, 20) }
    }
    console.log(`${label}: ${error ? error.split('\n').slice(0, 3).join(' | ') : `done in ${Date.now() - started} ms`}`)
    await page.close()
    writeFileSync(`${out}/shading.json`, `${JSON.stringify(results, null, 2)}\n`)
  }
} finally {
  await browser.close()
}
