// Opens the spike page's compare view and prints the raw values at some pixels (x, y from the top left, in the
// compare picture): what the reference and the raster read there.
//   node spike/light/measure/probe.mjs "<query>" x,y [x,y …]
import { chromium } from 'playwright'

const [query, ...points] = process.argv.slice(2)
const base = process.env.SPIKE_URL ?? 'http://localhost:5193'
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
try {
  const page = await browser.newPage({ viewport: { width: 1400, height: 1000 } })
  await page.goto(`${base}/spike-light.html?${query}`)
  await page.waitForFunction(() => window.__spike?.done || window.__spike?.error, null, { timeout: 600_000 })
  const out = await page.evaluate((points) => {
    const s = window.__spike
    if (s.error) return s.error
    const c = s.data
    const { width: w, height: h } = c.size
    const at = (a, x, y, rows) => { const i = (rows === 'top' ? y : h - 1 - y) * w + x; return [a[i * 4], a[i * 4 + 1], a[i * 4 + 2], a[i * 4 + 3]].map((v) => +v.toFixed(5)) }
    return points.map((p) => {
      const [x, y] = p.split(',').map(Number)
      return { p, top: { refS: at(c.reference.shadowed, x, y, 'top'), refU: at(c.reference.unshadowed, x, y, 'top'), rasS: at(c.raster.shadowed, x, y, 'top'), rasU: at(c.raster.unshadowed, x, y, 'top') } }
    })
  }, points)
  console.log(JSON.stringify(out, null, 1))
} finally {
  await browser.close()
}
