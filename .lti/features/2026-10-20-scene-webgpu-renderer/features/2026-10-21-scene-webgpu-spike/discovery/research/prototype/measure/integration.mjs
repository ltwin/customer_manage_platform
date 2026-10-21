// R0 spike, group 1 (task 3.1): the viewport on demand with the gizmo, and
// whether offscreen shots repeat exactly, visible or hidden, with frames
// stepped by hand or left to three's loop.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/integration.mjs <out dir> [webgpu|webgl]
import { mkdirSync, writeFileSync } from 'node:fs'
import { chromium } from 'playwright'

const base = process.env.SPIKE_URL || 'http://localhost:5291'
const [out, backend = 'webgpu'] = process.argv.slice(2)
if (!out) throw new Error('usage: integration.mjs <out dir> [webgpu|webgl]')
mkdirSync(out, { recursive: true })
const suffix = backend === 'webgl' ? '&backend=webgl' : ''
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const report = { backend }

const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
const context = await browser.newContext({ viewport: { width: 1200, height: 800 } })
const errors = []
const watch = (page, tag) => {
  page.on('pageerror', (e) => errors.push(`${tag} pageerror: ${e.message}`))
  page.on('console', (m) => m.type() === 'error' && errors.push(`${tag} ${m.text()}`))
}

// --- viewport ------------------------------------------------------------------
{
  const page = await context.newPage()
  watch(page, 'viewport')
  await page.goto(`${base}/spike-webgpu.html?case=viewport&scene=noon&aa=0&overlay=direct${suffix}`)
  await page.waitForFunction(() => window.__spike?.ready === true, null, { timeout: 30_000 })
  await sleep(500)
  const idleStart = await page.evaluate(() => window.__drawn)
  await sleep(1500)
  const idleEnd = await page.evaluate(() => window.__drawn)
  await page.screenshot({ path: `${out}/viewport-start.png` })
  // A two-finger swipe for half a second, as the e2e orbit does.
  await page.evaluate(() => new Promise((resolve) => {
    const el = document.querySelector('canvas')
    let n = 0
    const tick = () => {
      el.dispatchEvent(new WheelEvent('wheel', { deltaX: 6, deltaY: 2, deltaMode: 0, bubbles: true, cancelable: true }))
      if (++n < 30) requestAnimationFrame(tick)
      else resolve()
    }
    requestAnimationFrame(tick)
  }))
  await sleep(800)
  const afterOrbit = await page.evaluate(() => window.__drawn)
  await page.screenshot({ path: `${out}/viewport-orbited.png` })
  report.viewport = { idleStart, idleEnd, afterOrbit, onDemand: idleEnd === idleStart && afterOrbit > idleEnd }
  console.log(`viewport: drawn ${idleStart} → ${idleEnd} while idle, ${afterOrbit} after orbiting`)
  await page.close()
}

// --- shooter ---------------------------------------------------------------------
const shooterPage = await context.newPage()
watch(shooterPage, 'shooter')
await shooterPage.goto(`${base}/spike-webgpu.html?case=shooter${suffix}`)
await shooterPage.waitForFunction(() => !!window.__shooter)
const run = (o) => shooterPage.evaluate((o) => window.__shooter.run(o), o)
const save = (name, shot) => writeFileSync(`${out}/${name}.png`, Buffer.from(shot.png.split(',')[1], 'base64'))
const cases = [
  { name: 'controlled', o: { scene: 'cornell', control: true, k: 12 } },
  { name: 'loop-sync', o: { scene: 'cornell', control: false, k: 12 } },
  { name: 'loop-async', o: { scene: 'cornell', control: false, k: 12, wait: 20 } },
]
report.shots = {}
for (const visibility of ['visible', 'hidden']) {
  let cover
  if (visibility === 'hidden') {
    cover = await context.newPage()
    await cover.goto('about:blank')
    await cover.bringToFront()
    await sleep(300)
  }
  const state = await shooterPage.evaluate(() => ({ hidden: document.hidden, visibilityState: document.visibilityState }))
  const rafRuns = await shooterPage.evaluate(() => new Promise((resolve) => {
    let n = 0
    const tick = () => { n++; requestAnimationFrame(tick) }
    requestAnimationFrame(tick)
    setTimeout(() => resolve(n), 500)
  }))
  console.log(`${visibility}: document.hidden=${state.hidden}, rAF callbacks in 500 ms: ${rafRuns}`)
  for (const c of cases) {
    const r = await run(c.o)
    const [a, b] = r.shots
    report.shots[`${visibility}/${c.name}`] = { backend: r.backend, hidden: a.hidden, hashes: [a.hash, b.hash], same: a.hash === b.hash, frameIds: [a.frameIds, b.frameIds], ms: [Math.round(a.ms), Math.round(b.ms)] }
    save(`shot-${visibility}-${c.name}`, a)
    console.log(`  ${c.name}: ${a.hash} ${b.hash} ${a.hash === b.hash ? 'same' : 'DIFFER'}; frameIds ${JSON.stringify(a.frameIds)} / ${JSON.stringify(b.frameIds)}; ${Math.round(a.ms)} ms`)
  }
  report[`${visibility}State`] = { ...state, rafRuns }
  await cover?.close()
}
const v = report.shots['visible/controlled']
const h = report.shots['hidden/controlled']
report.controlledSameAcrossVisibility = v.hashes[0] === h.hashes[0]
console.log(`controlled visible vs hidden: ${report.controlledSameAcrossVisibility ? 'same' : 'DIFFER'}`)
report.errors = errors.slice(0, 20)
if (errors.length) console.log(errors.slice(0, 10).join('\n'))
writeFileSync(`${out}/integration-${backend}.json`, `${JSON.stringify(report, null, 2)}\n`)
await browser.close()
