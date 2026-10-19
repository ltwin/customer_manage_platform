// Open item #1: a tab left in the background keeps running the assistant's
// scene jobs. Real Chrome against the real API, media pipeline and
// PostgreSQL: the canvas page's renderer runs in one tab, a blank tab is
// opened in front of it, and after it has been hidden long enough for
// Chrome's intensive timer throttling (5 minutes) jobs are queued and timed
// against the same jobs run in the foreground.
//
// Playwright keeps every page it drives visible (it emulates focus), so this
// script starts Chrome itself and speaks to the tab over the DevTools
// protocol; the page reaches the harness's API through a Vite server this
// script starts on CREATIVE_EDITOR_URL's port. Chrome's window opens off
// screen.
// Opt-in, through the Go harness:
//   CREATIVE_EDITOR_URL=http://localhost:5181 CREATIVE_EDITOR_SCRIPT=creative-scene-agent-background.e2e.mjs \
//     go test ./internal/platform/httpapi/ -run TestCreativeEditorBrowser -v -timeout 30m
// SCENE_AGENT_HIDDEN_MINUTES (default 6.5) and SCENE_AGENT_BACKGROUND_RUNS
// (default 5 per kind and phase) tune it.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
import { defaultModuleCode } from '../src/creative-scene/sandbox/template.ts'

const url = process.env.CREATIVE_EDITOR_URL
const api = process.env.CREATIVE_EDITOR_API
const hiddenMinutes = Number(process.env.SCENE_AGENT_HIDDEN_MINUTES ?? 6.5)
const runs = Number(process.env.SCENE_AGENT_BACKGROUND_RUNS ?? 5)
const chromePath = process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
assert.ok(url && api, 'run through TestCreativeEditorBrowser')
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function call(method, path, body, token) {
  const response = await fetch(api + path, {
    method,
    headers: { 'Content-Type': 'application/json', Origin: url, ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!response.ok) throw new Error(`${method} ${path}: ${response.status} ${await response.text()}`)
  return response.json()
}

const login = await call('POST', '/api/v1/auth/login', { email: process.env.CREATIVE_EDITOR_EMAIL, password: process.env.CREATIVE_EDITOR_PASSWORD })
const account = await call('GET', '/api/v1/me', undefined, login.access_token)
const { canvas_id: canvasID } = await call('POST', '/__test/document')
const { document_id: documentID } = await call('POST', '/__test/scene')

const root = fileURLToPath(new URL('..', import.meta.url))
const fixture = new URL('../.scene-agent-background-qa.html', import.meta.url)
await writeFile(
  fixture,
  `<html><body><div id="root"></div><script type="module">
import React from 'react';
import {createRoot} from 'react-dom/client';
import {setAuthenticated} from '/src/auth/session.ts';
import {connectCanvasEvents} from '/src/creative-canvas/editor/api.ts';
import {notifyCanvasInvalidate, watchCanvasEvents} from '/src/creative-canvas/editor/canvasEvents.ts';
import {useSceneHost} from '/src/creative-scene/host/launcher.ts';
setAuthenticated(${JSON.stringify(login)}, ${JSON.stringify(account)});
// The canvas page's live stream; with a run waiting on the page it stays open while hidden (keepAlive).
const stream = new AbortController();
void watchCanvasEvents((signal) => connectCanvasEvents(${JSON.stringify(canvasID)}, signal), stream.signal, notifyCanvasInvalidate, () => {});
function Host() {
  const {state} = useSceneHost(${JSON.stringify(account.id)});
  return React.createElement('p', {id: 'host'}, state.holding ? 'holding' : state.capable ? 'idle' : 'incapable');
}
createRoot(document.getElementById('root')).render(React.createElement(Host));
window.__measures = () => performance.getEntriesByType('measure').filter((m) => m.name.startsWith('scene-model ')).map((m) => [m.name.slice(12), m.duration]);
</script></body></html>`,
)

const vite = await createServer({ root, logLevel: 'error', server: { port: Number(new URL(url).port), strictPort: true, proxy: { '/api': { target: api } } } })
await vite.listen()
const profile = await mkdtemp(join(process.env.TMPDIR ?? tmpdir(), 'scene-background-'))
const debugPort = 9300 + Math.floor(Math.random() * 500)
const chrome = spawn(
  chromePath,
  [`--remote-debugging-port=${debugPort}`, `--user-data-dir=${profile}`, '--no-first-run', '--no-default-browser-check', '--window-position=-3000,-3000', '--window-size=1280,820', `${url}/.scene-agent-background-qa.html`],
  { stdio: 'ignore' },
)
const devtools = `http://127.0.0.1:${debugPort}`

try {
  let tab
  for (let i = 0; i < 100 && !tab; i++, await sleep(200)) {
    try {
      tab = (await (await fetch(`${devtools}/json`)).json()).find((t) => t.type === 'page' && t.url.includes('.scene-agent-background-qa.html'))
    } catch {
      // Chrome is still starting.
    }
  }
  assert.ok(tab, 'the page opened')
  // A bare DevTools session on that one tab: evaluating scripts does not make it visible.
  const ws = new WebSocket(tab.webSocketDebuggerUrl)
  await new Promise((resolve, reject) => {
    ws.onopen = resolve
    ws.onerror = reject
  })
  let seq = 0
  const replies = new Map()
  ws.onmessage = (e) => {
    const m = JSON.parse(e.data)
    replies.get(m.id)?.(m)
    replies.delete(m.id)
  }
  const evaluate = (expression) =>
    new Promise((resolve, reject) => {
      const id = ++seq
      replies.set(id, (m) => (m.result?.exceptionDetails ? reject(new Error(JSON.stringify(m.result.exceptionDetails))) : resolve(m.result?.result?.value)))
      ws.send(JSON.stringify({ id, method: 'Runtime.evaluate', params: { expression, awaitPromise: true, returnByValue: true } }))
    })
  async function waitFor(check, what, timeout = 120_000) {
    const start = Date.now()
    for (;;) {
      const value = await check()
      if (value) return value
      if (Date.now() - start > timeout) throw new Error(`timed out: ${what}`)
      await sleep(50)
    }
  }
  await waitFor(async () => (await evaluate(`document.getElementById('host')?.textContent`)) === 'idle', 'the renderer is idle', 60_000)
  await sleep(1500)

  const identity = { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }
  let objects = 0
  let seen = 0
  async function measure(kind) {
    const n = objects++
    const request =
      kind === 'bake_model'
        ? { code: defaultModuleCode, params: { height: 2 + (n % 5) * 0.2 }, lib_version: 1, target: { op: 'add', object: { id: `sobj_bg${String(n).padStart(19, '0')}`, name: `后台 ${n}`, parent_id: null, transform: { ...identity, position: [n, 0, 0] } } }, expected: {} }
        : { preset: ['overview', 'top', 'front', 'side'][n % 4] }
    const created = await call('POST', `/__test/scene-job?document_id=${documentID}&kind=${kind}`, request)
    const done = await waitFor(async () => {
      const r = await call('GET', `/__test/scene-job?id=${created.job_id}`)
      return r.settled_ms ? r : null
    }, `${kind} settled`, 180_000)
    assert.equal(done.failure, '', `${kind} ${created.job_id}: ${done.failure}`)
    const all = await evaluate('window.__measures()')
    const stages = Object.fromEntries(all.slice(seen))
    seen = all.length
    return { ...stages, total: done.settled_ms - created.created_ms }
  }
  async function phase() {
    const out = { bake_model: [], render_view: [] }
    for (const kind of ['bake_model', 'render_view']) for (let i = 0; i < runs; i++) out[kind].push(await measure(kind))
    return out
  }

  // The first job of each kind loads the executor and three.js; it is not counted.
  await measure('bake_model')
  await measure('render_view')
  assert.equal(await evaluate('document.visibilityState'), 'visible')
  const foreground = await phase()

  await fetch(`${devtools}/json/new?about:blank`, { method: 'PUT' })
  await waitFor(async () => (await evaluate('document.visibilityState')) === 'hidden', 'the tab went into the background', 10_000)
  // The page's own timers are throttled now; the renderer's run on a Worker.
  const pageTimers = await evaluate(`new Promise((resolve) => { const t0 = performance.now(); let n = 0; const step = () => (++n < 6 ? setTimeout(step, 10) : resolve(performance.now() - t0)); setTimeout(step, 10) })`)
  console.log(`hidden for ${hiddenMinutes} minutes (six chained 10 ms page timers took ${Math.round(pageTimers)} ms)`)
  await sleep(hiddenMinutes * 60_000)
  assert.equal(await evaluate('document.visibilityState'), 'hidden', 'still in the background')
  const hidden = await phase()

  const pct = (values, p) => {
    const sorted = [...values].sort((a, b) => a - b)
    return sorted[Math.min(sorted.length - 1, Math.ceil((p / 100) * sorted.length) - 1)]
  }
  const stagesOf = { bake_model: ['compile', 'thumbnail', 'upload', 'total'], render_view: ['shot', 'shot-upload', 'total'] }
  for (const kind of ['bake_model', 'render_view']) {
    console.log(`\n${kind}: ${runs} jobs each, foreground vs hidden ${hiddenMinutes} min (ms)`)
    for (const stage of stagesOf[kind]) {
      const f = foreground[kind].map((s) => s[stage])
      const h = hidden[kind].map((s) => s[stage])
      console.log(`${stage.padEnd(12)} foreground p50 ${pct(f, 50).toFixed(0).padStart(6)} p95 ${pct(f, 95).toFixed(0).padStart(6)}   hidden p50 ${pct(h, 50).toFixed(0).padStart(6)} p95 ${pct(h, 95).toFixed(0).padStart(6)}`)
    }
    const f = pct(foreground[kind].map((s) => s.total), 50)
    const h = pct(hidden[kind].map((s) => s.total), 50)
    console.log(`total p50 ratio hidden / foreground: ${(h / f).toFixed(2)}`)
    // The design's bar: within twice the foreground time.
    assert.ok(h <= 2 * f, `${kind}: hidden p50 ${h} ms is over twice the foreground ${f} ms`)
  }
  ws.close()
} finally {
  chrome.kill()
  await vite.close()
  await sleep(500)
  await rm(profile, { recursive: true, force: true })
  await rm(fixture, { force: true })
}
