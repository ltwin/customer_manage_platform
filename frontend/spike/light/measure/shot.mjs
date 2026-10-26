// Opens the spike page with the given query on this machine's GPU, waits for its picture and saves a screenshot of it
// with the page's report beside it.
//   node spike/light/measure/shot.mjs <out.png> "<query>"   (SPIKE_URL, default http://localhost:5193)
import { writeFile } from 'node:fs/promises'
import { chromium } from 'playwright'

const [out, query = ''] = process.argv.slice(2)
const base = process.env.SPIKE_URL ?? 'http://localhost:5193'
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
try {
  const page = await browser.newPage({ viewport: { width: 1400, height: 1000 } })
  // SPIKE_LOG=1 prints every console message, warnings (WGSL compile errors among them) included.
  page.on('console', (m) => (process.env.SPIKE_LOG || m.type() === 'error') && console.log(`[console ${m.type()}]`, m.text().slice(0, 2000)))
  await page.goto(`${base}/spike-light.html?${query}`)
  await page.waitForFunction(() => window.__spike?.done || window.__spike?.error, null, { timeout: Number(process.env.SPIKE_TIMEOUT ?? 600_000) })
  const spike = await page.evaluate(() => window.__spike)
  if (spike.error) throw new Error(spike.error)
  await page.locator('#info').evaluate((e) => (e.style.display = 'none'))
  const panels = page.locator('#panels')
  await ((await panels.count()) ? panels : page.locator('#view')).screenshot({ path: out })
  await writeFile(out.replace(/\.png$/, '.json'), `${JSON.stringify(spike.report, null, 2)}\n`)
  console.log(JSON.stringify(spike.report))
} finally {
  await browser.close()
}
