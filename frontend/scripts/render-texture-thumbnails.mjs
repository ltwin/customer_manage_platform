// Renders the built-in textured materials' thumbnails (C5c2 design「缩略图」)
// on the editor's own material ball, into
// backend/internal/creativescene/builtin_thumbnails/<id with @ as _>.webp;
// make generate copies them to the frontend. Run again after the material
// build changes, and commit what it writes.
//
//   node scripts/render-texture-thumbnails.mjs --files <dir>
//
// <dir> holds the texture files: <sha256>.jpg as downloaded, or an object
// store's local root after creativectl textures import
// (scene-textures/<sha256>.jpg/content). No backend: the page's reads of
// /api/v1/creative/scene-textures/* are answered from <dir>.
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { parseArgs } from 'node:util'
import { createServer } from 'vite'
import { chromium } from 'playwright'

const { values } = parseArgs({ options: { files: { type: 'string' }, only: { type: 'string' } } })
if (!values.files) {
  console.error('usage: node scripts/render-texture-thumbnails.mjs --files <dir> [--only <id>]')
  process.exit(2)
}
const out = fileURLToPath(new URL('../../backend/internal/creativescene/builtin_thumbnails/', import.meta.url))
mkdirSync(out, { recursive: true })

function textureFile(name) {
  for (const p of [join(values.files, name), join(values.files, 'scene-textures', name, 'content')]) if (existsSync(p)) return readFileSync(p)
  return null
}

const server = await createServer({
  root: fileURLToPath(new URL('../', import.meta.url)),
  server: { host: '127.0.0.1', port: 0 },
  logLevel: 'error',
  plugins: [{ name: 'texture-thumbnails', configureServer(vite) {
    vite.middlewares.use('/__thumbnails__', async (_req, res) => {
      res.setHeader('Content-Type', 'text/html')
      res.end(await vite.transformIndexHtml('/__thumbnails__', '<html><head></head><body><script type="module" src="/scripts/fixtures/texture-thumbnails.ts"></script></body></html>'))
    })
  } }],
})
await server.listen()
const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
let failed = 0
try {
  const page = await browser.newPage()
  page.on('pageerror', (e) => console.error('page:', e.message))
  await page.route('**/api/v1/creative/scene-textures/*', (route) => {
    const body = textureFile(decodeURIComponent(new URL(route.request().url()).pathname.split('/').pop()))
    return body ? route.fulfill({ body, contentType: 'image/jpeg' }) : route.fulfill({ status: 503 })
  })
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__thumbnails__`)
  await page.waitForFunction(() => document.body.dataset.ready === '1', null, { timeout: 120_000 })
  const ids = (await page.evaluate(() => window.builtinIDs)).filter((id) => !values.only || id === values.only)
  for (const id of ids) {
    try {
      const b64 = await page.evaluate((id) => window.renderBuiltin(id), id)
      const file = join(out, `${id.replace('@', '_')}.webp`)
      writeFileSync(file, Buffer.from(b64, 'base64'))
      console.log(`${id} → ${file}`)
    } catch (e) {
      failed++
      console.error(`${id}: ${e.message}`)
    }
  }
} finally {
  await browser.close()
  await server.close()
}
process.exit(failed ? 1 : 0)
