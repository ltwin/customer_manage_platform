// AC-12: how long the assistant's scene jobs take from creation until their
// result reaches the run, against the real API, media pipeline (local storage,
// in-process worker) and PostgreSQL. The page is the canvas page's renderer
// (useSceneHost) on the canvas's live stream; the run is the harness's
// stand-in, which notes when a result is settled.
// Opt-in, through the Go harness that starts them:
//   CREATIVE_EDITOR_URL=http://localhost:5179 CREATIVE_EDITOR_SCRIPT=creative-scene-agent-latency.e2e.mjs \
//     go test ./internal/platform/httpapi/ -run TestCreativeEditorBrowser -v
// Prints p50 / p95 per stage. The first job of each kind loads the executor
// (and three.js) and is printed on its own. It asserts only that every job
// succeeded: the numbers are for the record, not a budget.
import assert from 'node:assert/strict'
import { rm, writeFile } from 'node:fs/promises'
import { chromium } from 'playwright'
import { defaultModuleCode } from '../src/creative-scene/sandbox/template.ts'

const url = process.env.CREATIVE_EDITOR_URL
const api = process.env.CREATIVE_EDITOR_API
const runs = Number(process.env.SCENE_AGENT_LATENCY_RUNS ?? 10)
assert.ok(url && api, 'run through TestCreativeEditorBrowser')

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

const fixture = new URL('../.scene-agent-latency-qa.html', import.meta.url)
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
// The canvas page's live stream, as useCanvasSnapshot keeps it: job notifications arrive here.
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

const browser = await chromium.launch({ headless: true, args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'] })
const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
// The API answers through the harness; claims and completions are timed here.
const claimed = new Map()
const completes = new Map()
await context.route('**/api/v1/**', async (route) => {
  const request = route.request()
  const u = new URL(request.url())
  if (request.headers().accept === 'text/event-stream') return route.continue({ url: api + u.pathname + u.search })
  const started = Date.now()
  const response = await route.fetch({ url: api + u.pathname + u.search })
  if (u.pathname.endsWith('/scene-jobs/claim') && response.status() === 200) {
    const { job } = await response.json()
    claimed.set(job.id, Date.now())
  }
  const done = u.pathname.match(/\/scene-jobs\/([^/]+)\/complete$/)
  if (done) completes.set(done[1], { ms: Date.now() - started, body: await response.json().catch(() => null) })
  return route.fulfill({ response })
})
const page = await context.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message))
await page.goto(`${url}/.scene-agent-latency-qa.html`)
await page.locator('#host', { hasText: 'idle' }).waitFor({ timeout: 30_000 })
// Let the stream connect before the first job, as an open canvas page would be.
await new Promise((r) => setTimeout(r, 1500))

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
async function waitFor(check, what, timeout = 60_000) {
  const start = Date.now()
  for (;;) {
    const value = await check()
    if (value) return value
    if (Date.now() - start > timeout) throw new Error(`timed out: ${what}`)
    await sleep(20)
  }
}
const identity = { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }
const objectID = (n) => `sobj_lat${String(n).padStart(18, '0')}`
let seen = 0

/** One job from creation until its result reached the run; returns its stages in ms. */
async function measure(kind, request) {
  const created = await call('POST', `/__test/scene-job?document_id=${documentID}&kind=${kind}`, request)
  const settled = await waitFor(async () => (await call('GET', `/__test/scene-job?id=${created.job_id}`)).settled_ms, `${kind} ${created.job_id} settled`)
  const complete = await waitFor(() => completes.get(created.job_id), `${kind} completion seen`)
  assert.equal(complete.body?.state, 'succeeded', `${kind}: ${JSON.stringify(complete.body)}`)
  const all = await page.evaluate(() => window.__measures())
  const stages = Object.fromEntries(all.slice(seen).map(([name, ms]) => [name, ms]))
  seen = all.length
  return { claim: claimed.get(created.job_id) - created.created_ms, ...stages, complete: complete.ms, total: settled - created.created_ms }
}

const results = { bake_model: [], render_view: [] }
const first = {}
for (let i = 0; i <= runs; i++) {
  const height = 2 + (i % 5) * 0.2
  const sample = await measure('bake_model', {
    code: defaultModuleCode, params: { height }, lib_version: 1, name_hint: '时延测量',
    target: { op: 'add', object: { id: objectID(i), name: `测量 ${i}`, parent_id: null, transform: { ...identity, position: [i, 0, 0] } } },
    expected: {},
  })
  if (i === 0) first.bake_model = sample
  else results.bake_model.push(sample)
}
for (let i = 0; i <= runs; i++) {
  const sample = await measure('render_view', { preset: ['overview', 'top', 'front', 'side'][i % 4] })
  if (i === 0) first.render_view = sample
  else results.render_view.push(sample)
}

const pct = (values, p) => {
  const sorted = [...values].sort((a, b) => a - b)
  return sorted[Math.min(sorted.length - 1, Math.ceil((p / 100) * sorted.length) - 1)]
}
const names = {
  claim: 'claim（建任务到页面领到：通知 + 认领）',
  sandbox: 'sandbox（沙箱编译）',
  validate: 'validate（宿主校验）',
  compile: 'compile（编译合计）',
  thumbnail: 'thumbnail（缩略图）',
  upload: 'upload（两个暂存上传 + worker 校验）',
  shot: 'shot（离屏截图）',
  'shot-upload': 'shot-upload（截图暂存上传 + 校验）',
  complete: 'complete（完成接口往返，含写场景）',
  total: 'total（建任务到结果交给运行）',
}
for (const kind of ['bake_model', 'render_view']) {
  const samples = results[kind]
  console.log(`\n${kind}: ${samples.length} jobs (ms)`)
  for (const [key, label] of Object.entries(names)) {
    const values = samples.map((s) => s[key]).filter((v) => typeof v === 'number')
    if (!values.length) continue
    assert.equal(values.length, samples.length, `${kind} ${key}: ${values.length} samples`)
    console.log(`${label.padEnd(36)} p50 ${pct(values, 50).toFixed(0).padStart(6)}  p95 ${pct(values, 95).toFixed(0).padStart(6)}`)
  }
  console.log(`first job (loads the executor): ${JSON.stringify(Object.fromEntries(Object.entries(first[kind]).map(([k, v]) => [k, Math.round(v)])))}`)
}
assert.deepEqual(errors, [])
await browser.close()
await rm(fixture, { force: true })
