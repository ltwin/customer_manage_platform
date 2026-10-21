import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import { chromium } from 'playwright'
const [out, scratch] = process.argv.slice(2)
const b = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const p = await b.newPage()
const errors = []
p.on('console', (m) => m.type() === 'error' && errors.push(m.text().slice(0, 300)))
await p.goto('http://localhost:5291/spike-webgpu.html?case=gi')
await p.waitForFunction(() => !!window.__gi)
const report = {}
for (const [name, o] of [['full-frame-acc', { variant: 'full-frame', accumulate: true }]]) {
  const r = await p.evaluate((o) => window.__gi.renderGI(o), { ...o, width: 2048, height: 1365, k: 128, samples: [4, 8, 12, 24, 36, 64, 128] })
  fs.writeFileSync(`${scratch}/${name}.png`, Buffer.from(r.png.split(',')[1], 'base64'))
  execFileSync('magick', [`${scratch}/${name}.png`, '-quality', '90', `${out}/gi-${name}-128.jpg`])
  const t = await p.evaluate((o) => window.__gi.renderGI(o).then((x) => x.msPerFrame), { ...o, width: 1600, height: 1000, k: 8 })
  report[name] = { changes: r.changes, ms1600: t }
  console.log(name, r.changes.map((c) => `${c.frame}:${c.change.toFixed(2)}`).join(' '), `| ${t.toFixed(1)} ms at 1600x1000`)
}
console.log(errors.slice(0, 3))
fs.writeFileSync(`${out}/gi-try-frame.json`, JSON.stringify(report, null, 2) + '\n')
await b.close()
