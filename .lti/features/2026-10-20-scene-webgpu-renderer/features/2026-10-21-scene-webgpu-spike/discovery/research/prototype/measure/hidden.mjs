// R0 spike, group 1: shots in a tab that is really hidden. Playwright keeps
// the pages it drives visible (it emulates focus), so, as
// scripts/creative-scene-agent-background.e2e.mjs does, this starts Chrome
// itself with its window off screen, speaks to the tab over the DevTools
// protocol and opens a blank tab in front of it.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/hidden.mjs <out dir> [webgpu|webgl]
import { spawn } from 'node:child_process'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const base = process.env.SPIKE_URL || 'http://localhost:5291'
const [out, backend = 'webgpu'] = process.argv.slice(2)
if (!out) throw new Error('usage: hidden.mjs <out dir> [webgpu|webgl]')
const chromePath = process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const port = 9333
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const profile = mkdtempSync(join(tmpdir(), 'spike-hidden-'))
const page = `${base}/spike-webgpu.html?case=shooter${backend === 'webgl' ? '&backend=webgl' : ''}`
const chrome = spawn(chromePath, [`--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, '--no-first-run', '--no-default-browser-check', '--window-position=-2400,-2400', '--window-size=1200,800', page], { stdio: 'ignore' })
const devtools = `http://127.0.0.1:${port}`

try {
  let target
  for (let i = 0; i < 100 && !target; i++) {
    await sleep(200)
    target = await fetch(`${devtools}/json`).then((r) => r.json()).then((list) => list.find((t) => t.type === 'page' && t.url.startsWith(base))).catch(() => null)
  }
  if (!target) throw new Error('no spike tab')
  const ws = new WebSocket(target.webSocketDebuggerUrl)
  await new Promise((resolve, reject) => { ws.onopen = resolve; ws.onerror = reject })
  let id = 0
  const pending = new Map()
  ws.onmessage = (m) => {
    const msg = JSON.parse(m.data)
    if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id) }
  }
  // A hung promise in the page (a read-back that never settles) shows as a timeout, not a hang here.
  const evaluate = (expression, timeoutMs = 90_000) => new Promise((resolve, reject) => {
    const n = ++id
    const timer = setTimeout(() => { pending.delete(n); reject(new Error(`timed out after ${timeoutMs} ms`)) }, timeoutMs)
    pending.set(n, (msg) => {
      clearTimeout(timer)
      if (msg.error) reject(new Error(`CDP: ${msg.error.message}`))
      else if (msg.result?.exceptionDetails) reject(new Error(msg.result.exceptionDetails.exception?.description ?? 'evaluate failed'))
      else resolve(msg.result?.result?.value)
    })
    ws.send(JSON.stringify({ id: n, method: 'Runtime.evaluate', params: { expression, awaitPromise: true, returnByValue: true } }))
  })
  for (let i = 0; i < 100 && !(await evaluate('!!window.__shooter')); i++) await sleep(200)
  const probe = () => evaluate(`new Promise((res) => { let n = 0; const t = () => { n++; requestAnimationFrame(t) }; requestAnimationFrame(t); setTimeout(() => res({ visibility: document.visibilityState, raf: n }), 1000) })`)
  const run = (o) => evaluate(`window.__shooter.run(${JSON.stringify(o)}).then((r) => ({ backend: r.backend, shots: r.shots.map((s) => ({ hash: s.hash, frameIds: s.frameIds, ms: Math.round(s.ms), hidden: s.hidden, png: s.png })) }))`)
  const cases = [
    { name: 'controlled', o: { scene: 'cornell', control: true, k: 12 } },
    { name: 'loop-async', o: { scene: 'cornell', control: false, k: 12, wait: 20 } },
  ]
  const report = { backend, chrome: (await fetch(`${devtools}/json/version`).then((r) => r.json())).Browser }
  for (const phase of ['visible', 'hidden']) {
    if (phase === 'hidden') {
      await fetch(`${devtools}/json/new?about:blank`, { method: 'PUT' })
      for (let i = 0; i < 50 && (await evaluate('document.visibilityState')) !== 'hidden'; i++) await sleep(200)
    }
    report[phase] = { probe: await probe() }
    console.log(`${phase}: ${JSON.stringify(report[phase].probe)}`)
    for (const c of cases) {
      let r
      try {
        r = await run(c.o)
      } catch (error) {
        report[phase][c.name] = { error: String(error.message) }
        console.log(`  ${c.name}: ${error.message}`)
        continue
      }
      const [a, b] = r.shots
      writeFileSync(`${out}/real-${phase}-${c.name}.png`, Buffer.from(a.png.split(',')[1], 'base64'))
      report[phase][c.name] = { backend: r.backend, hidden: a.hidden, hashes: [a.hash, b.hash], frameIds: [a.frameIds, b.frameIds], ms: [a.ms, b.ms] }
      console.log(`  ${c.name}: ${a.hash} ${b.hash} ${a.hash === b.hash ? 'same' : 'DIFFER'} (hidden=${a.hidden}); frameIds ${JSON.stringify(a.frameIds)}; ${a.ms} ms`)
    }
  }
  report.controlledSame = report.visible.controlled.hashes?.[0] === report.hidden.controlled.hashes?.[0]
  report.loopSame = report.visible['loop-async'].hashes?.[0] === report.hidden['loop-async'].hashes?.[0]
  console.log(`controlled visible vs hidden: ${report.controlledSame ? 'same' : 'DIFFER'}; three's loop visible vs hidden: ${report.loopSame ? 'same' : 'DIFFER'}`)
  writeFileSync(`${out}/hidden-${backend}.json`, `${JSON.stringify(report, null, 2)}\n`)
  ws.close()
} finally {
  chrome.kill()
  await sleep(500)
  rmSync(profile, { recursive: true, force: true })
}
