import {
  compileError,
  isCompileRequest,
  readSandboxMessage,
  type CompileResultMessage,
} from './protocol.ts'

// The sandbox page's script. It runs in an opaque origin under the sandbox
// CSP and never runs module code itself: each compile gets a new classic
// Worker made from the embedded source (Chromium refuses module Workers from
// Blob URLs in an opaque origin), destroyed after its first message.

declare const __SCENE_SANDBOX_WORKER__: string

const workerURL = URL.createObjectURL(new Blob([__SCENE_SANDBOX_WORKER__], { type: 'text/javascript' }))
let running: Worker | null = null

// The Worker runs module code, so its message is reshaped here: the id is
// always the request's, and the host still checks every field.
function reshape(data: unknown, id: number): CompileResultMessage {
  const m = readSandboxMessage(data)
  if (m && m.type === 'result') return { ...m, id }
  return { type: 'result', id, ok: false, error: compileError('runtime', '编译结果格式不正确') }
}

window.addEventListener('message', (event) => {
  if (event.source !== window.parent || !isCompileRequest(event.data)) return
  const request = event.data
  const origin = event.origin
  running?.terminate()
  const worker = new Worker(workerURL)
  running = worker
  const finish = (result: CompileResultMessage) => {
    if (running !== worker) return
    running = null
    worker.terminate()
    window.parent.postMessage(result, origin, result.ok ? [result.glb] : [])
  }
  worker.onmessage = (e) => finish(reshape(e.data, request.id))
  worker.onmessageerror = () => finish(reshape(null, request.id))
  worker.onerror = (e) => {
    e.preventDefault()
    finish({ type: 'result', id: request.id, ok: false, error: compileError('runtime', e.message || '编译出错') })
  }
  worker.postMessage(request)
})

window.parent.postMessage({ type: 'ready' }, '*')
