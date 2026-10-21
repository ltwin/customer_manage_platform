// R0 spike, task 2.2: which headless Chromium launches get a WebGPU adapter,
// and what the spike page then runs on. Usage, with the dev server up:
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/env.mjs <out dir>
import { mkdirSync, writeFileSync } from 'node:fs'
import { chromium } from 'playwright'

const base = process.env.SPIKE_URL || 'http://localhost:5291'
const out = process.argv[2]
if (!out) throw new Error('usage: env.mjs <out dir>')
mkdirSync(out, { recursive: true })

const gpu = ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu']
const swift = ['--use-angle=swiftshader', '--enable-unsafe-swiftshader']
const variants = [
  { name: 'shell-default', options: {} },
  { name: 'shell-metal', options: { args: gpu } },
  { name: 'shell-metal-unsafe', options: { args: [...gpu, '--enable-unsafe-webgpu'] } },
  { name: 'shell-swiftshader', options: { args: swift } },
  { name: 'shell-swiftshader-webgpu', options: { args: [...swift, '--enable-unsafe-webgpu', '--use-webgpu-adapter=swiftshader'] } },
  { name: 'new-default', options: { channel: 'chromium' } },
  { name: 'new-metal', options: { channel: 'chromium', args: gpu } },
  { name: 'new-metal-unsafe', options: { channel: 'chromium', args: [...gpu, '--enable-unsafe-webgpu'] } },
  { name: 'new-swiftshader-webgpu', options: { channel: 'chromium', args: [...swift, '--enable-unsafe-webgpu', '--use-webgpu-adapter=swiftshader'] } },
]

const results = []
for (const v of variants) {
  const row = { name: v.name, options: v.options }
  let browser
  try {
    browser = await chromium.launch({ headless: true, ...v.options })
    row.version = browser.version()
    const page = await (await browser.newContext({ viewport: { width: 960, height: 640 } })).newPage()
    const logs = []
    page.on('console', (m) => (m.type() === 'error' || m.type() === 'warning') && logs.push(`${m.type()}: ${m.text()}`))
    page.on('pageerror', (e) => logs.push(`pageerror: ${e.message}`))
    await page.goto(`${base}/spike-webgpu.html?scene=noon`)
    row.adapter = await page.evaluate(async () => {
      if (!('gpu' in navigator)) return { gpu: false }
      const a = await navigator.gpu.requestAdapter()
      if (!a) return { gpu: true, adapter: null }
      const info = a.info ?? {}
      return {
        gpu: true,
        vendor: info.vendor, architecture: info.architecture, description: info.description, fallback: info.isFallbackAdapter ?? a.isFallbackAdapter,
        maxTextureDimension2D: a.limits.maxTextureDimension2D, maxBufferSize: a.limits.maxBufferSize, maxStorageBufferBindingSize: a.limits.maxStorageBufferBindingSize,
        maxColorAttachments: a.limits.maxColorAttachments, maxColorAttachmentBytesPerSample: a.limits.maxColorAttachmentBytesPerSample,
        features: [...a.features].sort(),
      }
    })
    await page.waitForFunction(() => window.__spike?.ready === true, null, { timeout: 30_000 }).catch(() => {})
    row.backend = await page.evaluate(() => window.__spike?.backend ?? null)
    await page.screenshot({ path: `${out}/env-${v.name}.png` })
    row.logs = logs.slice(0, 8)
  } catch (error) {
    row.error = String(error).split('\n')[0]
  } finally {
    await browser?.close()
  }
  console.log(`${v.name}: ${row.error ?? `adapter ${row.adapter?.adapter === null ? 'none' : (row.adapter?.vendor ?? (row.adapter?.gpu ? '?' : 'no navigator.gpu'))}${row.adapter?.architecture ? ` ${row.adapter.architecture}` : ''}, page backend ${row.backend}`}`)
  results.push(row)
}
writeFileSync(`${out}/env.json`, `${JSON.stringify(results, null, 2)}\n`)
