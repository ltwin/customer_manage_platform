// R0 spike, group 7 (task 4.3): what read-backs return and how a hidden tab
// treats them. Part one in Playwright's Chromium (Metal): the read-back
// format on both backends and straight alpha on MSAA edges. Part two in real
// Chrome with its window off screen, as hidden.mjs: encoding, a WebGPU shot
// with read-back, and the canvas + toDataURL way, visible and then hidden.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/readback.mjs <out dir>
import { spawn } from 'node:child_process'
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { chromium } from 'playwright'

const base = process.env.SPIKE_URL || 'http://localhost:5291'
const [out] = process.argv.slice(2)
if (!out) throw new Error('usage: readback.mjs <out dir>')
mkdirSync(out, { recursive: true })
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const savePNG = (name, url) => writeFileSync(`${out}/${name}.png`, Buffer.from(url.split(',')[1], 'base64'))
const report = {}

// Part one: format and edges.
{
  const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
  const page = await browser.newPage()
  await page.goto(`${base}/spike-webgpu.html?case=readback`)
  await page.waitForFunction(() => !!window.__readback)
  for (const webgl of [false, true]) {
    const name = webgl ? 'webgl2' : 'webgpu'
    // constant: the WebGL2 builder fails on the card's Fn when it is the whole fragment node (the srgb-target way).
    report[`format-${name}`] = await page.evaluate((gl) => window.__readback.format(gl), webgl)
    report[`format-${name}-constant`] = await page.evaluate((gl) => window.__readback.format(gl, true), webgl)
    for (const unpremultiply of [false, true]) {
      const { png, ...rest } = await page.evaluate(([gl, u]) => window.__readback.edges(gl, u), [webgl, unpremultiply])
      report[`edges-${name}-${unpremultiply ? 'straight' : 'as-is'}`] = rest
      if (!webgl) savePNG(`edges-${unpremultiply ? 'straight' : 'as-is'}`, png)
      console.log(`${name} edges ${unpremultiply ? 'unpremultiplied' : 'as is'}: edge red ${rest.edgeRed.toFixed(0)} at alpha ${rest.edgeAlpha.toFixed(0)}, inside ${rest.insideRed}`)
    }
    const { png: canvasPNG, ...canvas } = await page.evaluate((gl) => window.__readback.canvasShot(gl, true), webgl)
    report[`canvas-${name}`] = canvas
    console.log(`${name} canvas + toDataURL vs target read-back: ${canvas.vsTarget.toFixed(3)} mean difference`)
    const f = report[`format-${name}`]
    console.log(`${name} format: ${Object.entries(f).filter(([k]) => k !== 'backend').map(([k, v]) => `${k} grey ${v.grey} green ${v.green} blue ${v.blue} first raw ${v.firstRawRow}`).join(' | ')}`)
  }
  await browser.close()
}

// Part two: real Chrome, visible then hidden.
const chromePath = process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const port = 9334
const profile = mkdtempSync(join(tmpdir(), 'spike-readback-'))
const chrome = spawn(chromePath, [`--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, '--no-first-run', '--no-default-browser-check', '--window-position=-2400,-2400', '--window-size=1200,800', `${base}/spike-webgpu.html?case=readback`], { stdio: 'ignore' })
const devtools = `http://127.0.0.1:${port}`
try {
  let target
  for (let i = 0; i < 100 && !target; i++) {
    await sleep(200)
    target = await fetch(`${devtools}/json`).then((r) => r.json()).then((list) => list.find((t) => t.type === 'page' && t.url.startsWith(base))).catch(() => null)
  }
  if (!target) throw new Error('no spike tab')
  const ws = new WebSocket(target.webSocketDebuggerUrl)
  await new Promise((resolve, reject) => {
    ws.onopen = resolve
    ws.onerror = reject
  })
  let id = 0
  const pending = new Map()
  ws.onmessage = (m) => {
    const msg = JSON.parse(m.data)
    if (msg.id && pending.has(msg.id)) {
      pending.get(msg.id)(msg)
      pending.delete(msg.id)
    }
  }
  const evaluate = (expression, timeoutMs = 90_000) =>
    new Promise((resolve, reject) => {
      const n = ++id
      const timer = setTimeout(() => {
        pending.delete(n)
        reject(new Error(`timed out after ${timeoutMs} ms`))
      }, timeoutMs)
      pending.set(n, (msg) => {
        clearTimeout(timer)
        if (msg.error) reject(new Error(`CDP: ${msg.error.message}`))
        else if (msg.result?.exceptionDetails) reject(new Error(msg.result.exceptionDetails.exception?.description ?? 'evaluate failed'))
        else resolve(msg.result?.result?.value)
      })
      ws.send(JSON.stringify({ id: n, method: 'Runtime.evaluate', params: { expression, awaitPromise: true, returnByValue: true } }))
    })
  for (let i = 0; i < 100 && !(await evaluate('!!window.__readback')); i++) await sleep(200)
  report.chrome = (await fetch(`${devtools}/json/version`).then((r) => r.json())).Browser
  const cases = {
    encoding: 'window.__readback.encoding()',
    'shot-webgpu': `window.__shooter.run({ scene: 'cornell', control: true, k: 12, times: 3 }).then((r) => ({ backend: r.backend, hashes: r.shots.map((s) => s.hash), ms: r.shots.map((s) => Math.round(s.ms)) }))`,
    'canvas-webgpu': 'window.__readback.canvasShot(false).then(({ png, ...r }) => r)',
    'canvas-webgl2': 'window.__readback.canvasShot(true).then(({ png, ...r }) => r)',
  }
  for (const phase of ['visible', 'hidden']) {
    if (phase === 'hidden') {
      await fetch(`${devtools}/json/new?about:blank`, { method: 'PUT' })
      for (let i = 0; i < 50 && (await evaluate('document.visibilityState')) !== 'hidden'; i++) await sleep(200)
    }
    report[phase] = { visibility: await evaluate('document.visibilityState') }
    for (const [name, expression] of Object.entries(cases)) {
      try {
        report[phase][name] = await evaluate(expression)
      } catch (error) {
        report[phase][name] = { error: String(error.message) }
      }
      console.log(`${phase} ${name}: ${JSON.stringify(report[phase][name])}`)
    }
  }
  ws.close()
} finally {
  chrome.kill()
  await sleep(500)
  rmSync(profile, { recursive: true, force: true })
}
writeFileSync(`${out}/readback.json`, `${JSON.stringify(report, null, 2)}\n`)
