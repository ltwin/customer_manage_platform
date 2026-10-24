// S0 group 4, reflections: spike-trace.html?case=reflections over the scenes on this machine's GPU; reflections.json
// with each scene's comparison, timing, retrace counts and the stages' limits, and mirror-board's pictures (ssr, A,
// |A − B| × 8, the pitch strip).
//   node spike/trace/measure/reflections.mjs <dev server> <out dir> [scene,…]
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { chromium } from 'playwright'

const [base, out, only] = process.argv.slice(2)
if (!base || !out) throw new Error('usage: reflections.mjs <dev server> <out dir> [scenes]')
mkdirSync(out, { recursive: true })
const scenes = only ? only.split(',') : ['mirror-board', 'backlit-room', 'materials']
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
let results = { chromium: browser.version(), scenes: {} }
try {
  results = { ...JSON.parse(readFileSync(`${out}/reflections.json`, 'utf8')), chromium: browser.version() }
} catch {}
const save = (file, url) => writeFileSync(`${out}/${file}`, Buffer.from(url.slice(url.indexOf(',') + 1), 'base64'))
try {
  for (const scene of scenes) {
    const page = await browser.newPage({ viewport: { width: 1536, height: 1024 } })
    const errors = []
    page.on('pageerror', (e) => errors.push(e.message))
    page.on('console', (m) => (m.type() === 'error' || m.type() === 'warning') && errors.push(m.text().slice(0, 500)))
    const started = Date.now()
    await page.goto(`${base}/spike-trace.html?scene=${scene}&case=reflections${process.env.EXTRA ?? ""}`)
    const settled = await page.waitForFunction(() => window.__spike?.ready || window.__spike?.error, null, { timeout: 600_000, polling: 500 }).then(() => true, () => false)
    const error = settled ? await page.evaluate(() => window.__spike.error) : 'timed out after 600 s'
    if (error) results.scenes[scene] = { error, errors: [...new Set(errors)].slice(0, 20) }
    else {
      const { pictures, ...rest } = await page.evaluate(() => window.__spike.result)
      if (scene === 'mirror-board' || process.env.SAVE_ALL) for (const [name, url] of Object.entries(pictures)) save(`${scene}-${name}.${url.startsWith('data:image/png') ? 'png' : 'jpg'}`, url)
      results.scenes[scene] = { pageMs: Date.now() - started, ...rest, errors: [...new Set(errors)].slice(0, 20) }
    }
    console.log(`${scene}: ${error ? error.split('\n').slice(0, 4).join(' | ') : `done in ${Date.now() - started} ms`}`)
    await page.close()
    writeFileSync(`${out}/reflections.json`, `${JSON.stringify(results, null, 2)}\n`)
  }
} finally {
  await browser.close()
}
