import { existsSync, readFileSync, statSync } from 'node:fs'
import path from 'node:path'
import type { IncomingMessage, ServerResponse } from 'node:http'
import { defineConfig, type Connect, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'

const sandboxDir = path.resolve(import.meta.dirname, 'public/scene-sandbox')

// Serves the geometry code sandbox like the Go static server does: the page
// only with its CSP (the template's {origin} set to the request's origin),
// and nothing under /scene-sandbox/ falls back to the application shell.
// The files are built by scripts/build-scene-sandbox.mjs before dev starts.
function sceneSandbox(): Plugin {
  const serve = (https: boolean): Connect.NextHandleFunction =>
    (req: IncomingMessage, res: ServerResponse, next: Connect.NextFunction) => {
      const url = (req.url ?? '').split('?')[0]
      if (!url.startsWith('/scene-sandbox/')) return next()
      const file = path.join(sandboxDir, path.normalize(url.slice('/scene-sandbox/'.length)))
      const missing = () => {
        res.statusCode = 404
        res.end()
      }
      if (!file.startsWith(sandboxDir + path.sep)) return missing()
      if (url === '/scene-sandbox/sandbox.html') {
        const csp = path.join(sandboxDir, 'csp.txt')
        if (!existsSync(file) || !existsSync(csp) || !req.headers.host) return missing()
        const origin = `${https ? 'https' : 'http'}://${req.headers.host}`
        res.setHeader('Content-Security-Policy', readFileSync(csp, 'utf8').trim().replaceAll('{origin}', origin))
        res.setHeader('Cache-Control', 'no-cache')
        res.setHeader('X-Content-Type-Options', 'nosniff')
        res.setHeader('Content-Type', 'text/html; charset=utf-8')
        res.end(readFileSync(file))
        return
      }
      if (!existsSync(file) || !statSync(file).isFile()) return missing()
      next()
    }
  return {
    name: 'scene-sandbox',
    configureServer(server) {
      server.middlewares.use(serve(Boolean(server.config.server.https)))
    },
    configurePreviewServer(server) {
      server.middlewares.use(serve(Boolean(server.config.preview.https)))
    },
  }
}

// dev 期前端流量走 Vite dev server，/api 代理到后端（design D7）
export default defineConfig({
  plugins: [react(), sceneSandbox()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
    // 浏览器回归在测试开始时才写入 .*-qa.html 夹具；冷启动时监听到这次写入
    // 会把正在测的页面整页刷新，测试随之丢掉状态。夹具照常可访问，只是不监听。
    watch: {
      ignored: ['**/.*-qa.html'],
    },
  },
})
