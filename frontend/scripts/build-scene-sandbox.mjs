// Builds the geometry code sandbox page into public/scene-sandbox/ (a build
// output, ignored by git). Two IIFE bundles: first the compile Worker, then
// the page script with the Worker's source embedded as a string, since the
// page creates each Worker from a Blob. Vite copies public/ into dist/, so the
// dev server and the Go static server use the same files.
//
//   node scripts/build-scene-sandbox.mjs
import { createHash } from 'node:crypto'
import { copyFile, mkdir, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { build } from 'vite'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const source = path.join(root, 'src/creative-scene/sandbox')
const out = path.join(root, 'public/scene-sandbox')

async function bundle(entry, name, define = {}) {
  const result = await build({
    configFile: false,
    root,
    publicDir: false,
    logLevel: 'warn',
    define,
    build: {
      write: false,
      minify: true,
      target: 'es2022',
      reportCompressedSize: false,
      lib: { entry: path.join(source, entry), formats: ['iife'], name, fileName: () => `${name}.js` },
    },
  })
  const outputs = Array.isArray(result) ? result : [result]
  const chunks = outputs.flatMap((o) => o.output).filter((o) => o.type === 'chunk')
  if (chunks.length !== 1) throw new Error(`${entry}: expected one chunk, got ${chunks.length}`)
  return chunks[0].code
}

const worker = await bundle('worker.ts', 'sceneSandboxWorker')
if (!worker.includes('import(')) throw new Error('worker.ts: the module import() was rewritten by the bundler')
const page = await bundle('harness.ts', 'sceneSandbox', { __SCENE_SANDBOX_WORKER__: JSON.stringify(worker) })
const script = `sandbox.${createHash('sha256').update(page).digest('hex').slice(0, 12)}.js`

await rm(out, { recursive: true, force: true })
await mkdir(out, { recursive: true })
await writeFile(path.join(out, script), page)
await writeFile(
  path.join(out, 'sandbox.html'),
  `<!doctype html>\n<html><head><meta charset="utf-8"><title>scene sandbox</title></head><body><script src="${script}"></script></body></html>\n`,
)
await copyFile(path.join(source, 'csp.txt'), path.join(out, 'csp.txt'))
console.log(`scene sandbox: ${script} (${(page.length / 1024).toFixed(0)} KiB, worker ${(worker.length / 1024).toFixed(0)} KiB)`)
