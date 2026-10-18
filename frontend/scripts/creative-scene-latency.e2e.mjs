// AC-11: how long a code model's parameter change takes against the real
// API, media pipeline (local storage, in-process worker) and PostgreSQL.
// Opt-in, through the Go harness that starts them:
//   CREATIVE_EDITOR_URL=http://localhost:5179 CREATIVE_EDITOR_SCRIPT=creative-scene-latency.e2e.mjs \
//     go test ./internal/platform/httpapi/ -run TestCreativeEditorBrowser -v
// Prints p50 / p95 per stage. It asserts only that every change was saved:
// the numbers are for the record, not a budget.
import assert from 'node:assert/strict'
import { rm, writeFile } from 'node:fs/promises'
import { chromium } from 'playwright'

const url = process.env.CREATIVE_EDITOR_URL
const api = process.env.CREATIVE_EDITOR_API
const runs = Number(process.env.SCENE_LATENCY_RUNS ?? 20)
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
const { document_id: documentID } = await call('POST', '/__test/scene')

const fixture = new URL('../.scene-latency-qa.html', import.meta.url)
await writeFile(
  fixture,
  `<html><body style="margin:0"><div id="root" class="cc-workspace" style="width:100vw;height:100vh"></div><script type="module">
import React from 'react';
import {createRoot} from 'react-dom/client';
import '/src/index.css';
import '/src/creative-canvas/editor/workspace.css';
import SceneEditor from '/src/creative-scene/SceneEditor.tsx';
import {setAuthenticated} from '/src/auth/session.ts';
setAuthenticated(${JSON.stringify(login)}, ${JSON.stringify(account)});
try { localStorage.setItem('creative-scene:first-run-seen', '1') } catch {}
createRoot(document.getElementById('root')).render(React.createElement(SceneEditor, {documentID:${JSON.stringify(documentID)}, onClose(){}}));
</script></body></html>`,
)

const browser = await chromium.launch({ headless: true, args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'] })
const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
// The API answers through the harness; a commit's round trip is timed here.
const commits = []
await context.route('**/api/v1/**', async (route) => {
  const request = route.request()
  const u = new URL(request.url())
  if (request.headers().accept === 'text/event-stream') return route.continue({ url: api + u.pathname + u.search })
  const started = performance.now()
  const response = await route.fetch({ url: api + u.pathname + u.search })
  const body = request.method() === 'POST' && u.pathname.endsWith('/commands') ? request.postDataJSON() : null
  if (body?.payload?.operations?.some((op) => op.op === 'commit_model')) commits.push(performance.now() - started)
  return route.fulfill({ response })
})
const page = await context.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message))
await page.goto(`${url}/.scene-latency-qa.html`)
await page.locator('.cc-scene-editor canvas').waitFor({ timeout: 30_000 })
await page.getByRole('treeitem').first().waitFor()

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
async function waitFor(check, what, timeout = 30_000) {
  const start = Date.now()
  while (!(await check())) {
    if (Date.now() - start > timeout) throw new Error(`timed out: ${what}`)
    await sleep(20)
  }
}
const saved = () => waitFor(async () => (await page.locator('.cc-scene-save').textContent()) === '已保存', 'saved')

// One code model from the template, then the parameter changes.
const box = await page.locator('.cc-scene-editor canvas').boundingBox()
await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
await page.keyboard.press('Shift+KeyA')
await page.getByRole('menuitem', { name: '代码模型…' }).click()
const generate = page.getByRole('complementary', { name: '代码' }).getByRole('button', { name: '生成', exact: true })
await waitFor(() => generate.isEnabled(), 'code panel')
await generate.click()
await waitFor(() => commits.length === 1, 'model created', 60_000)
await saved()
await page.getByRole('button', { name: '关闭代码面板' }).click()
const height = page.getByLabel('高度（m）', { exact: true })
await height.waitFor()

const totals = []
for (let i = 1; i <= runs; i++) {
  await height.fill((3 + i * 0.1).toFixed(1))
  const started = performance.now()
  await height.press('Enter')
  await waitFor(() => commits.length === i + 1, `change ${i} committed`, 60_000)
  await saved()
  totals.push(performance.now() - started)
  // Let the read that follows a new model settle before the next change.
  await sleep(300)
}

const measures = await page.evaluate(() =>
  performance.getEntriesByType('measure').filter((m) => m.name.startsWith('scene-model ')).map((m) => [m.name.slice(12), m.duration]),
)
const stage = (name) => measures.filter(([n]) => n === name).map(([, d]) => d).slice(1)
const pct = (values, p) => {
  const sorted = [...values].sort((a, b) => a - b)
  return sorted[Math.min(sorted.length - 1, Math.ceil((p / 100) * sorted.length) - 1)]
}
const rows = [
  ['sandbox（沙箱编译）', stage('sandbox')],
  ['validate（宿主校验）', stage('validate')],
  ['thumbnail（缩略图）', stage('thumbnail')],
  ['upload（两个暂存上传 + worker 校验）', stage('upload')],
  ['commit（提交命令往返）', commits.slice(1)],
  ['total（回车到已保存）', totals],
]
for (const [name, values] of rows) assert.equal(values.length, runs, `${name}: ${values.length} samples`)
console.log(`scene model latency, ${runs} parameter changes (ms)`)
for (const [name, values] of rows) console.log(`${name.padEnd(28)} p50 ${pct(values, 50).toFixed(0).padStart(6)}  p95 ${pct(values, 95).toFixed(0).padStart(6)}`)
assert.deepEqual(errors, [])
await browser.close()
await rm(fixture, { force: true })
