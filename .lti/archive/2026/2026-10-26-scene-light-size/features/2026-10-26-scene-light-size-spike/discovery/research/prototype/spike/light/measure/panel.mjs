// Saves the gloss panel's light as the compare view reads it, unshadowed, reference and raster side by side
// (AC-4's pictures): a box around the panel's face, each pixel's luminance, as JSON next to out.
//   node spike/light/measure/panel.mjs <out.json> "<query>"   (the query with view=compare, scene=occluder-wall)
import { writeFile } from 'node:fs/promises'
import { chromium } from 'playwright'

const [out, query = ''] = process.argv.slice(2)
const base = process.env.SPIKE_URL ?? 'http://localhost:5193'
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
try {
  const page = await browser.newPage()
  await page.goto(`${base}/spike-light.html?${query}`)
  await page.waitForFunction(() => window.__spike?.done || window.__spike?.error, null, { timeout: 600_000 })
  const panel = await page.evaluate(() => {
    const s = window.__spike
    if (s.error) throw new Error(s.error)
    const c = s.data
    const { width: w, height: h } = c.size
    const lum = (a, i) => 0.2126 * a[i * 4] + 0.7152 * a[i * 4 + 1] + 0.0722 * a[i * 4 + 2]
    // The panel's face (doc x −3.74…−2.74, height 2.36…3.36, z −2.93; three's world is (x, −z, y)), and a margin.
    let x0 = w, x1 = 0, y0 = h, y1 = 0
    for (let i = 0; i < w * h; i++) {
      const [x, y, z] = [c.position[i * 4], c.position[i * 4 + 2], -c.position[i * 4 + 1]]
      if (Math.abs(z + 2.93) > 0.004 || x < -3.74 || x > -2.74 || y < 2.36 || y > 3.36) continue
      const px = i % w, py = Math.floor(i / w)
      x0 = Math.min(x0, px); x1 = Math.max(x1, px); y0 = Math.min(y0, py); y1 = Math.max(y1, py)
    }
    const m = 6
    x0 = Math.max(0, x0 - m); y0 = Math.max(0, y0 - m); x1 = Math.min(w - 1, x1 + m); y1 = Math.min(h - 1, y1 + m)
    const pick = (a) => { const o = []; for (let y = y1; y >= y0; y--) for (let x = x0; x <= x1; x++) o.push(lum(a, y * w + x)); return o }
    return { width: x1 - x0 + 1, height: y1 - y0 + 1, reference: pick(c.reference.unshadowed), raster: pick(c.raster.unshadowed), report: s.report }
  })
  await writeFile(out, JSON.stringify(panel))
  console.log(JSON.stringify({ width: panel.width, height: panel.height, highlight: panel.report.compare.highlight }))
} finally {
  await browser.close()
}
