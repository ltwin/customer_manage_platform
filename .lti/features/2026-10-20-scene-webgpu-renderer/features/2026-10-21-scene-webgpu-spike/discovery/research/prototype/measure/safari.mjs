// R0 spike, group 7 (task 4.3): Safari. No script here drives Safari's WebGPU
// (Playwright's WebKit is not Safari, safaridriver needs remote automation
// turned on by hand), so this opens the page in Safari with ?report= and
// waits for the page to post its results: adapter, read-back format, group 1
// shots, a group 5 GI picture.
//   SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/safari.mjs <out dir>
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import http from 'node:http'

const base = process.env.SPIKE_URL || 'http://localhost:5291'
const [out] = process.argv.slice(2)
if (!out) throw new Error('usage: safari.mjs <out dir>')
fs.mkdirSync(out, { recursive: true })
const port = 5399
const received = new Promise((resolve) => {
  const server = http.createServer((req, res) => {
    let body = ''
    req.on('data', (c) => (body += c))
    req.on('end', () => {
      res.writeHead(200, { 'access-control-allow-origin': '*' })
      res.end('ok')
      if (req.method === 'POST') {
        server.close()
        resolve(JSON.parse(body))
      }
    })
  })
  server.listen(port, '127.0.0.1')
})
const version = execFileSync('defaults', ['read', '/Applications/Safari.app/Contents/Info', 'CFBundleShortVersionString']).toString().trim()
execFileSync('open', ['-a', 'Safari', `${base}/spike-webgpu.html?case=readback&report=${encodeURIComponent(`http://127.0.0.1:${port}/`)}`])
const timeout = new Promise((_, reject) => setTimeout(() => reject(new Error('no report from Safari in 5 minutes')), 300_000))
const result = await Promise.race([received, timeout])
result.safari = version
for (const name of ['shooter', 'gi']) {
  const png = result[name]?.png
  if (!png) continue
  const file = `${out}/safari-${name}.png`
  fs.writeFileSync(file, Buffer.from(png.split(',')[1], 'base64'))
  execFileSync('magick', [file, '-quality', '90', file.replace(/\.png$/, '.jpg')])
  fs.rmSync(file)
  delete result[name].png
}
fs.writeFileSync(`${out}/safari.json`, `${JSON.stringify(result, null, 2)}\n`)
console.log(JSON.stringify(result, null, 1).slice(0, 3000))
