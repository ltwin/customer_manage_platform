// R0 spike, group 6 (task 4.2): 2K / 4K / 8K exports in one go and in tiles,
// line art at 1× and 2×, SSGI at a lower internal resolution; time, texture
// memory, device loss, encoded sizes. Previews and crops are kept as JPEG.
// Runs are merged into <out dir>/hires.json by name. [only]: comma-separated
// name prefixes, or exact names ending in $.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/hires.mjs <out dir> [only]
import { execFile, execFileSync } from 'node:child_process'
import fs from 'node:fs'
import { chromium } from 'playwright'

const base = process.env.SPIKE_URL || 'http://localhost:5291'
const [out, only = ''] = process.argv.slice(2)
fs.mkdirSync(out, { recursive: true })
const save = (name, url) => {
  const [head, data] = url.split(',')
  const file = `${out}/${name}.${head.includes('jpeg') ? 'jpg' : 'png'}`
  fs.writeFileSync(file, Buffer.from(data, 'base64'))
  if (file.endsWith('.png')) {
    execFileSync('magick', [file, '-quality', '92', file.replace(/\.png$/, '.jpg')])
    fs.rmSync(file)
  }
}
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const page = await browser.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message))
page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
await page.goto(`${base}/spike-webgpu.html?case=hires`)
await page.waitForFunction(() => !!window.__hires)
// The GPU process's memory footprint (macOS footprint), sampled each second during a run: the peak.
const gpuPid = execFileSync('ps', ['-ax', '-o', 'pid=,command=']).toString().split('\n').map((l) => /^\s*(\d+) (\S*ms-playwright\S*) .*--type=gpu-process/.exec(l)?.[1]).find(Boolean)
const footprintMB = () =>
  new Promise((done) => execFile('footprint', [gpuPid], (_, stdout) => done(Number(/Footprint: ([\d.]+) ([KMG])B/.exec(stdout ?? '')?.slice(1).reduce((v, u) => v * { K: 1 / 1024, M: 1, G: 1024 }[u])) || 0)))
const peakDuring = async (work) => {
  let peak = await footprintMB()
  const idle = peak
  let sampling = true
  const loop = (async () => {
    while (sampling) {
      peak = Math.max(peak, await footprintMB())
      await new Promise((t) => setTimeout(t, 1000))
    }
  })()
  const result = await work()
  sampling = false
  await loop
  return { result, idleMB: idle, peakMB: peak }
}
const json = `${out}/hires.json`
const report = fs.existsSync(json) ? JSON.parse(fs.readFileSync(json, 'utf8')) : { runs: [], materials: [] }
report.adapter = await page.evaluate(() => window.__hires.adapterLimits())
const wanted = only ? only.split(',') : []
const selected = (name) => !only || wanted.some((w) => (w.endsWith('$') ? name === w.slice(0, -1) : name.startsWith(w)))
const keep = (list, entry) => [...list.filter((e) => e.name !== entry.name), entry]
console.log('adapter', report.adapter)

const sizes = { '2k': [2048, 1365], '4k': [4096, 2731], '8k': [8192, 5461], '8k-square': [8192, 8192] }
const runs = [
  ['workbench-2k', { kind: 'workbench', size: '2k' }],
  ['workbench-4k', { kind: 'workbench', size: '4k' }],
  ['workbench-8k', { kind: 'workbench', size: '8k', compare: true }],
  ['workbench-8k-tiles', { kind: 'workbench', size: '8k', tiles: 2, compare: true }],
  ['workbench-8k-tiles-margin', { kind: 'workbench', size: '8k', tiles: 2, margin: 64, compare: true }],
  ['workbench-8k-square', { kind: 'workbench', size: '8k-square' }],
  ['gi-2k', { kind: 'gi', size: '2k', k: 16, cropAt: [0.3, 0.12, 0.125] }],
  ['gi-4k', { kind: 'gi', size: '4k', k: 16, cropAt: [0.3, 0.12, 0.125] }],
  ['gi-8k', { kind: 'gi', size: '8k', k: 16, compare: true, cropAt: [0.3, 0.12, 0.125] }],
  ['gi-8k-half', { kind: 'gi', size: '8k', k: 16, giScale: 0.5, cropAt: [0.3, 0.12, 0.125] }],
  // Tiles against a one-shot picture without TRAA: TRAA's first frames go wrong in the lower tiles (high-resolution.md).
  ['gi-8k-noaa', { kind: 'gi', size: '8k', k: 16, traa: false, compare: true }],
  ['gi-8k-noaa-tiles', { kind: 'gi', size: '8k', k: 16, traa: false, tiles: 2, compare: true }],
  ['gi-8k-noaa-tiles-margin', { kind: 'gi', size: '8k', k: 16, traa: false, tiles: 2, margin: 512, compare: true }],
  ['gi-8k-tiles', { kind: 'gi', size: '8k', k: 16, tiles: 2 }],
  ['line-8k-1x', { kind: 'line', size: '8k', lineScale: 1 }],
  ['line-8k-2x-default-limit', { kind: 'line', size: '8k', lineScale: 2 }],
  ['line-8k-2x', { kind: 'line', size: '8k', lineScale: 2, limit: 16384 }],
  ['line-2k-2x', { kind: 'line', size: '2k', lineScale: 2 }],
]
for (const [name, run] of runs) {
  if (!selected(name)) continue
  const [width, height] = sizes[run.size]
  const before = errors.length
  // cropAt: a crop at a place relative to the picture (x, y, size over the width), the same scene area at every size.
  const crops = run.cropAt ? [[Math.round(run.cropAt[0] * width), Math.round(run.cropAt[1] * height), Math.round(run.cropAt[2] * width)]] : undefined
  const { result: r, idleMB, peakMB } = await peakDuring(() => page.evaluate((o) => window.__hires.renderHires(o), { ...run, width, height, crops }))
  const { preview, crops: cropped, ...rest } = r
  if (preview) save(`${name}-preview`, preview)
  for (const [i, c] of (cropped ?? []).entries()) save(`${name}-crop${i}`, c)
  report.runs = keep(report.runs, { name, ...run, width, height, ...rest, gpuProcessMB: { idle: idleMB, peak: peakMB }, pageErrors: errors.slice(before, before + 4) })
  const f = (x) => (x == null ? '-' : x.toFixed(1))
  console.log(
    `${name}: ${r.ok ? 'ok' : `FAILED ${r.error ?? ''} ${r.lost ?? ''} ${(r.errors ?? []).join(' | ')}`} ${f(r.ms)} ms, tiles ${(r.tiles ?? []).map((t) => `${f(t.gpuMs)}+${f(t.readMs)}ms/${f(t.textureMB)}MB`).join(' ')}; png ${f(r.pngMB)} MB in ${f(r.pngMs)} ms, webp ${f(r.webpMB)} MB in ${f(r.webpMs)} ms, jpeg ${f(r.jpegMB)} / ${f(r.jpeg95MB)} MB` +
      (r.seams ? `; vs one-shot ${f(r.seams.mean)} (seam band ${f(r.seams.band)}, rest ${f(r.seams.rest)})` : '') +
      (r.jump ? `; jump across seams ${r.jump.across.toFixed(2)} vs ${r.jump.control.toFixed(2)} beside` : '') +
      `; GPU process ${idleMB.toFixed(0)} → ${peakMB.toFixed(0)} MB`,
  )
}
if (selected('materials')) {
  for (const size of ['2k', '4k', '8k', '8k-square']) {
    const [width, height] = sizes[size]
    const r = await page.evaluate(([w, h]) => window.__hires.materialSizes(w, h), [width, height])
    report.materials = keep(report.materials, { name: size, width, height, ...r })
    console.log(`materials ${size}: png ${r.pngMB.toFixed(1)} MB in ${r.pngMs.toFixed(0)} ms, webp ${r.webpMB?.toFixed(1)} MB in ${r.webpMs.toFixed(0)} ms, jpeg ${r.jpegMB.toFixed(1)} / ${r.jpeg95MB.toFixed(1)} MB`)
  }
}
if (errors.length) console.log(errors.slice(0, 5).map((e) => e.slice(0, 300)).join('\n'))
fs.writeFileSync(json, `${JSON.stringify(report, null, 2)}\n`)
await browser.close()
