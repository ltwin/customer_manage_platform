// S0 group 1, integration: spike-trace.html?case=integration over the reference scenes and the stress scene on
// this machine's GPU; integration.json with each scene's pack and trace times, determinism and the pixels that
// disagree with the raster, and the traced normals and the disagreement mask of each as PNGs.
//   node spike/trace/measure/integration.mjs <dev server> <out dir> [scene,…]
import { mkdirSync, writeFileSync } from 'node:fs'
import { chromium } from 'playwright'

const [base, out, only] = process.argv.slice(2)
if (!base || !out) throw new Error('usage: integration.mjs <dev server> <out dir> [scenes]')
mkdirSync(out, { recursive: true })
// The stress scene's pack is group 2's (throughput.mjs): built on the main thread it outlasts any page timeout.
const scenes = only ? only.split(',') : ['cornell', 'materials', 'backlit-room', 'thin-wall', 'mirror-board', 'noon']
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const results = { chromium: browser.version(), scenes: {} }
const save = (file, url) => writeFileSync(`${out}/${file}`, Buffer.from(url.slice(url.indexOf(',') + 1), 'base64'))
try {
  for (const scene of scenes) {
    const page = await browser.newPage({ viewport: { width: 1500, height: 1000 } })
    const errors = []
    page.on('pageerror', (e) => errors.push(e.message))
    page.on('console', (m) => (m.type() === 'error' || m.type() === 'warning') && errors.push(m.text()))
    await page.goto(`${base}/spike-trace.html?scene=${scene}&case=integration`)
    const settled = await page.waitForFunction(() => window.__spike?.ready || window.__spike?.error, null, { timeout: 300_000 }).then(() => true, () => false)
    const error = settled ? await page.evaluate(() => window.__spike.error) : 'timed out after 300 s'
    if (error) {
      results.scenes[scene] = { error, errors }
      console.log(`${scene}: ${error.split('\n')[0]}`)
    } else {
      const { maskPNG, normalsPNG, ...result } = await page.evaluate(() => window.__spike.result)
      save(`${scene}-mask.png`, maskPNG)
      save(`${scene}-normals.png`, normalsPNG)
      results.scenes[scene] = { ...result, errors }
      console.log(`${scene}: pack ${result.packMs} ms, trace ${result.traceMs} ms (GPU ${result.gpuMs.map((v) => v?.toFixed?.(2))} ms), ${(100 * result.pixels.offFraction).toFixed(3)}% off, deterministic ${result.deterministic}`)
    }
    await page.close()
    writeFileSync(`${out}/integration.json`, `${JSON.stringify(results, null, 2)}\n`)
  }
} finally {
  await browser.close()
}
writeFileSync(`${out}/integration.json`, `${JSON.stringify(results, null, 2)}\n`)
