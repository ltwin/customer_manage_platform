// 停止即停：运行中发送键就是停止键；点击立刻显示已停止、文字不再增长，
// 停止落定后显示服务端保存的内容；失败回滚；旧回执回放出未结束时换 key 重发；
// 停止落定前发出的新消息先等它。
// Run with the frontend Vite server on CREATIVE_AGENT_URL (default :5179).
//   node scripts/agent-stop.e2e.mjs   (AGENT_STOP_E2E_ONLY=<part of a name> runs matching checks)
// Everything is mocked: HTTP by route, the SSE stream by a fetch wrapper the
// test feeds frame by frame.
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { writeFile, rm } from 'node:fs/promises'
import { chromium } from 'playwright'

const noticeVersion = /egressNoticeVersion = '([^']+)'/.exec(
  readFileSync(new URL('../src/creative-canvas/editor/agentState.ts', import.meta.url), 'utf8'),
)[1]
const fixture = new URL('../.agent-stop-qa.html', import.meta.url)
await writeFile(
  fixture,
  `<html><body><div id="root" class="cc-workspace"></div><script type="module">
import React from 'react';
import {createRoot} from 'react-dom/client';
import '/src/index.css';
import '/src/creative-canvas/editor/workspace.css';
import AgentPanel from '/src/creative-canvas/editor/AgentPanel.tsx';
import {setAuthenticated} from '/src/auth/session.ts';
setAuthenticated({access_token:'local-test',expires_in:3600,token_type:'Bearer'},{id:'qa-account',username:'qa'});
localStorage.setItem('creative-egress-notice:qa-account', '${noticeVersion}');
createRoot(document.getElementById('root')).render(React.createElement(React.StrictMode,null,React.createElement(AgentPanel,{account:'qa-account',canvasID:'canvas',defaultOpen:true,canvas:{nodes:[]}})));
</script></body></html>`,
)
const url = `${process.env.CREATIVE_AGENT_URL || 'http://localhost:5179'}/.agent-stop-qa.html`
const browser = await chromium.launch({ headless: true })
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

// One mocked run. `cancel` decides how each cancel request is answered.
async function scene({ state = 'running', cancel, send } = {}) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 860 } })
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  const server = {
    seq: 0,
    log: [],
    blocks: [],
    cancels: [],
    sends: [],
    // Runs created during the test, by id; their streams stay empty.
    runs: {},
    run: {
      id: 'run',
      conversation_id: 'conversation',
      canvas_id: 'canvas',
      trigger_message_id: 'message',
      egress_consent_id: 'consent',
      model_key: 'model',
      state,
      ...(state === 'waiting_input' ? { waiting_token: 'token', wait_reason: 'question' } : {}),
      settlement_state: 'pending',
      limits_version: '1',
      revision: '1',
      last_event_seq: '0',
      pruned_through_seq: '0',
      deadline_at: '2099-01-01T00:00:00Z',
      created_at: '2026-10-09T00:00:00Z',
    },
  }
  const frame = (event) => `id: ${event.seq}\nevent: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`
  await page.exposeFunction('__replay', (target) => {
    const parsed = new URL(target, 'http://x')
    if (!parsed.pathname.includes('/agent-runs/run/')) return []
    const after = Number(parsed.searchParams.get('after_seq') ?? 0)
    return server.log.filter((event) => Number(event.seq) > after).map(frame)
  })
  await page.addInitScript(() => {
    const real = window.fetch.bind(window)
    const encoder = new TextEncoder()
    window.__sse = { controller: null }
    window.fetch = async (input, init) => {
      const target = typeof input === 'string' ? input : input.url
      if (!/\/agent-runs\/[^/]+\/events/.test(target)) return real(input, init)
      const frames = await window.__replay(target)
      const body = new ReadableStream({
        start(controller) {
          window.__sse.controller = controller
          for (const item of frames) controller.enqueue(encoder.encode(item))
          init?.signal?.addEventListener('abort', () => {
            try {
              controller.error(new DOMException('aborted', 'AbortError'))
            } catch {}
          })
        },
      })
      return new Response(body, { headers: { 'content-type': 'text/event-stream' } })
    }
    window.__push = (text) => window.__sse.controller?.enqueue(encoder.encode(text))
  })
  const emit = async (type, payload) => {
    const event = { schema_version: 1, run_id: 'run', seq: String(++server.seq), type, created_at: '2026-10-09T00:00:00Z', payload }
    server.log.push(event)
    if (type === 'block.created') server.blocks.push(structuredClone(payload))
    else if (type === 'block.updated') {
      const block = server.blocks.find((item) => item.id === payload.block_id)
      block.revision = payload.revision
      if (payload.state) block.state = payload.state
      if (payload.finished_at) block.finished_at = payload.finished_at
      if (payload.patch?.append)
        block.payload[payload.patch.append.path] = (block.payload[payload.patch.append.path] ?? '') + payload.patch.append.text
    } else if (payload.state) {
      server.run = { ...server.run, state: payload.state }
      if (['succeeded', 'partial', 'failed', 'cancelled'].includes(payload.state)) server.run.finished_at = '2026-10-09T00:01:00Z'
    }
    await page.evaluate((text) => window.__push(text), frame(event))
  }
  let revision = 1
  const answer = async (markdown) => {
    await emit('block.created', {
      id: 'ccbk_answer', run_id: 'run', ordinal: '1', type: 'text', state: 'streaming', revision: '1',
      payload_version: 1, hidden: false, payload: { markdown }, started_at: '2026-10-09T00:00:01Z',
    })
  }
  const append = (text) =>
    emit('block.updated', { block_id: 'ccbk_answer', revision: String(++revision), patch: { append: { path: 'markdown', text } } })
  // What the server does when the stop commits: open blocks interrupted, run ended.
  const end = async () => {
    if (server.blocks.length)
      await emit('block.updated', { block_id: 'ccbk_answer', revision: String(++revision), state: 'interrupted', finished_at: '2026-10-09T00:01:00Z' })
    await emit('run.state_changed', { state: 'cancelled', reason: 'creative_run_cancelled' })
  }
  await page.route('**/api/v1/creative/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const json = (body, status = 200) => route.fulfill({ status, json: body })
    const cancelled = /\/agent-runs\/([^/]+)\/cancel$/.exec(path)
    if (request.method() === 'POST' && cancelled) {
      const entry = { run: cancelled[1], key: request.headers()['idempotency-key'], at: Date.now() }
      server.cancels.push(entry)
      return cancel({ route, json, entry, server, end, index: server.cancels.length - 1 })
    }
    if (request.method() === 'POST') {
      server.sends.push({ path, at: Date.now() })
      if (send) return send({ path, json, server })
      return json({ error: { code: 'creative_test_stop', message: 'stop here' } }, 409)
    }
    const other = /\/agent-runs\/([^/]+)$/.exec(path)
    if (other && server.runs[other[1]])
      return json({ ...server.runs[other[1]], blocks: [], snapshot_seq: '0' })
    if (path.endsWith('/agent/catalog'))
      return json({
        limits: { attachments: 10, image_preview_count: 8 },
        models: [{ model_key: 'model', display_name: 'Test model', capability: { image_input: false }, available: true }],
        skills: [],
      })
    if (path.endsWith('/conversations')) return json({ items: [{ id: 'conversation', title: '对话' }] })
    if (path.endsWith('/conversations/conversation/runs')) return json({ items: [server.run] })
    if (path.endsWith('/messages'))
      return json({
        items: [{
          id: 'message', ordinal: '1', role: 'user', status: 'complete', schema_version: 2,
          body: { blocks: [{ type: 'text', text: '帮我拆分镜' }] }, revision: '1', created_at: '2026-10-09T00:00:00Z',
        }],
      })
    if (path.endsWith('/agent-runs/run'))
      return json({ ...server.run, last_event_seq: String(server.seq), blocks: server.blocks, snapshot_seq: String(server.seq) })
    return json({ items: [] })
  })
  await page.goto(url)
  await page.locator('.cc-agent-turn').waitFor({ state: 'attached' })
  // A finished run is read once and never streamed.
  if (!['succeeded', 'partial', 'failed', 'cancelled'].includes(state))
    await page.waitForFunction(() => window.__sse.controller !== null)
  const text = () => page.locator('[data-block="ccbk_answer"]').innerText()
  const button = (name) => page.locator('.cc-agent-composer-foot').getByRole('button', { name, exact: true })
  return {
    page, server, emit, answer, append, end, text, button, errors,
    close: async () => {
      assert.deepEqual(errors, [], 'no page errors')
      await context.close()
    },
  }
}

const answered = (run) => ({ ...run, state: 'cancelled', settlement_state: 'unknown', finished_at: '2026-10-09T00:01:00Z' })

const results = []
const only = process.env.AGENT_STOP_E2E_ONLY
async function check(name, fn) {
  if (only && !name.includes(only)) return
  const started = Date.now()
  try {
    await Promise.race([fn(), sleep(60_000).then(() => Promise.reject(new Error('timed out after 60 s')))])
    results.push({ name, ok: true })
    console.log(`ok   ${name} (${Date.now() - started} ms)`)
  } catch (error) {
    results.push({ name, ok: false })
    console.log(`FAIL ${name}\n     ${String(error?.stack ?? error).split('\n').slice(0, 6).join('\n     ')}`)
  }
}

try {
  await check('button: running shows stop in place of send, the status row has no buttons', async () => {
    const s = await scene({ cancel: async ({ json, server, end }) => { await end(); return json(answered(server.run)) } })
    await s.button('停止').waitFor()
    assert.equal(await s.button('发送').count(), 0)
    assert.equal(await s.page.locator('.cc-agent-status button').count(), 0, 'no buttons in the status row')
    await s.page.locator('.cc-agent-compose').screenshot({ path: '/tmp/agent-stop-button.png' })
    await s.emit('run.state_changed', { state: 'succeeded' })
    await s.button('发送').waitFor()
    assert.equal(await s.button('停止').count(), 0)
    await s.close()
  })

  await check('button: waiting for input stops when empty and continues once written', async () => {
    const s = await scene({ state: 'waiting_input', cancel: async ({ json, server }) => json(answered(server.run)) })
    await s.button('停止').waitFor()
    await s.page.locator('#creative-agent-draft').fill('用暖色')
    await s.button('补充并继续').waitFor()
    assert.equal(await s.button('停止').count(), 0)
    await s.page.locator('#creative-agent-draft').fill('')
    await s.button('停止').waitFor()
    await s.close()
  })

  await check('freeze: the stop shows at once and the text stops growing; the saved text shows once it ends', async () => {
    let respond
    const s = await scene({ cancel: ({ json, server, end }) => new Promise((resolve) => { respond = async () => { await end(); resolve(json(answered(server.run))) } }) })
    await s.answer('先定主光，')
    await s.page.getByText('先定主光，').waitFor()
    const before = await s.text()
    await s.button('停止').click()
    // The next frame already shows the stop.
    await s.button('发送').waitFor({ timeout: 300 })
    await s.page.locator('.cc-agent-note', { hasText: '已停止' }).waitFor({ timeout: 300 })
    assert.equal(await s.page.locator('.cc-agent-status').count(), 0, 'no status row')
    // The model keeps writing until the stop commits; none of it shows.
    for (const piece of ['再看', '逆光']) {
      await s.append(piece)
      await sleep(300)
    }
    await sleep(2400 - 600)
    assert.equal(await s.text(), before, 'text froze at the click')
    assert.equal(s.server.cancels.length, 1)
    await respond()
    await s.page.getByText('先定主光，再看逆光').waitFor()
    assert.equal(await s.page.locator('.cc-agent-note', { hasText: '已停止' }).count(), 1)
    assert.equal(await s.page.getByText(/费用/).count(), 0, 'no cost wording')
    await s.button('发送').waitFor()
    await s.page.screenshot({ path: '/tmp/agent-stop-frozen.png' })
    await s.close()
  })

  await check('failure: a failed stop goes back to running with the latest text and says so', async () => {
    let respond
    const s = await scene({ cancel: ({ json }) => new Promise((resolve) => { respond = () => resolve(json({ error: { code: 'creative_unavailable', message: 'down' } }, 503)) }) })
    await s.answer('先定主光，')
    await s.page.getByText('先定主光，').waitFor()
    await s.button('停止').click()
    await s.button('发送').waitFor({ timeout: 300 })
    await s.append('再看逆光')
    await sleep(300)
    assert.equal(await s.text(), '先定主光，', 'frozen while stopping')
    respond()
    await s.page.getByText('停止没有成功，请重试').waitFor()
    await s.button('停止').waitFor()
    await s.page.getByText('先定主光，再看逆光').waitFor()
    assert.equal(await s.page.locator('.cc-agent-note', { hasText: '已停止' }).count(), 0)
    await s.close()
  })

  await check('replay: an answer from an older stop is sent again under a new key', async () => {
    const s = await scene({
      cancel: async ({ json, server, end, index }) => {
        if (index === 0) return json({ ...server.run, state: 'reconciling' })
        await end()
        return json(answered(server.run))
      },
    })
    await s.button('停止').click()
    await s.page.locator('.cc-agent-note', { hasText: '已停止' }).waitFor()
    for (let i = 0; i < 20 && s.server.cancels.length < 2; i++) await sleep(50)
    assert.equal(s.server.cancels.length, 2, 'stopped again')
    assert.notEqual(s.server.cancels[0].key, s.server.cancels[1].key, 'under a new key')
    await sleep(300)
    assert.equal(await s.page.getByText('停止没有成功，请重试').count(), 0)
    await s.close()
  })

  await check('send: a message written during the stop waits for it', async () => {
    let respond
    const s = await scene({ cancel: ({ json, server, end }) => new Promise((resolve) => { respond = async () => { await end(); resolve(json(answered(server.run))) } }) })
    await s.button('停止').click()
    const editor = s.page.getByRole('textbox', { name: '创作问题' })
    await editor.click()
    await s.page.keyboard.type('换成侧光')
    await s.page.keyboard.press('Enter')
    await sleep(800)
    assert.equal(s.server.sends.length, 0, 'nothing sent before the stop committed')
    const answeredAt = Date.now()
    await respond()
    for (let i = 0; i < 40 && s.server.sends.length === 0; i++) await sleep(50)
    assert(s.server.sends.length > 0, 'sent after the stop')
    assert(s.server.sends[0].at >= answeredAt, 'sent after the stop answered')
    assert(s.server.sends[0].path.endsWith('/egress-consents'))
    await s.close()
  })

  // 建运行被拖住时：发出去的同一帧就是运行中样式，可以停；建好后立刻被取消。
  const startScene = (runs) => {
    let release
    const gate = new Promise((resolve) => (release = resolve))
    const scenePromise = scene({
      state: 'succeeded',
      cancel: ({ json, server, entry }) => {
        server.runs[entry.run] = answered(server.runs[entry.run])
        return json(server.runs[entry.run])
      },
      send: async ({ path, json, server }) => {
        if (path.endsWith('/egress-consents')) return json({ id: 'consent-2' })
        await gate
        return runs({ json, server })
      },
    })
    return scenePromise.then((s) => ({ ...s, release }))
  }
  const sendMessage = async (s) => {
    await s.page.getByRole('textbox', { name: '创作问题' }).click()
    await s.page.keyboard.type('换成侧光')
    await s.page.keyboard.press('Enter')
  }

  await check('start: a message just sent shows as running and can be stopped before its run exists', async () => {
    const s = await startScene(({ json, server }) => {
      server.runs.run2 = { ...server.run, id: 'run2', state: 'queued', trigger_message_id: 'message-2', last_event_seq: '1' }
      delete server.runs.run2.finished_at
      return json(server.runs.run2)
    })
    await s.button('发送').waitFor()
    await sendMessage(s)
    await s.button('停止').waitFor({ timeout: 300 })
    await s.page.locator('.cc-agent-status', { hasText: '正在处理' }).waitFor({ timeout: 300 })
    await s.page.locator('.is-sending .cc-agent-loading-answer').waitFor({ timeout: 300 })
    await s.page.locator('.cc-agent-compose').screenshot({ path: '/tmp/agent-stop-starting.png' })
    await sleep(500)
    assert.equal(await s.button('停止').count(), 1, 'still stoppable while the run is being made')
    await s.button('停止').click()
    await s.page.locator('.is-sending .cc-agent-note', { hasText: '已停止' }).waitFor({ timeout: 300 })
    assert.equal(await s.page.locator('.is-sending .cc-agent-loading-answer').count(), 0)
    assert.equal(await s.page.locator('.cc-agent-status').count(), 0)
    assert.equal(s.server.cancels.length, 0, 'nothing to cancel yet')
    s.release()
    for (let i = 0; i < 40 && !s.server.cancels.some((entry) => entry.run === 'run2'); i++) await sleep(50)
    assert(s.server.cancels.some((entry) => entry.run === 'run2'), 'the new run was cancelled once it existed')
    await sleep(300)
    assert.equal(await s.page.getByText('停止没有成功，请重试').count(), 0)
    assert.equal(await s.page.locator('.is-sending .cc-agent-note', { hasText: '已停止' }).count(), 1)
    await s.close()
  })

  await check('start: a stopped message that is refused puts the draft back without an error', async () => {
    const s = await startScene(({ json }) => json({ error: { code: 'creative_test_refused', message: '拒绝' } }, 409))
    await sendMessage(s)
    await s.button('停止').waitFor({ timeout: 300 })
    await s.button('停止').click()
    s.release()
    await s.page.getByRole('textbox', { name: '创作问题' }).getByText('换成侧光').waitFor()
    await sleep(300)
    assert.equal(await s.page.locator('.cc-agent-error, [role="alert"]').count(), 0, 'no error')
    assert.equal(await s.page.getByText('拒绝').count(), 0)
    assert.equal(s.server.cancels.length, 0)
    await s.close()
  })

  await check('start: a stopped message whose result is unknown keeps the stop when sent again', async () => {
    let calls = 0
    const s = await startScene(({ json, server }) => {
      if (++calls === 1) return json({ error: { code: 'creative_unavailable', message: 'down' } }, 503)
      server.runs.run2 = { ...server.run, id: 'run2', state: 'queued', trigger_message_id: 'message-2', last_event_seq: '1' }
      delete server.runs.run2.finished_at
      return json(server.runs.run2)
    })
    await sendMessage(s)
    await s.button('停止').waitFor({ timeout: 300 })
    await s.button('停止').click()
    s.release()
    const again = s.page.getByRole('button', { name: '重新发送' })
    await again.waitFor()
    assert.equal(s.server.cancels.length, 0)
    await again.click()
    for (let i = 0; i < 40 && !s.server.cancels.some((entry) => entry.run === 'run2'); i++) await sleep(50)
    assert.equal(calls, 2)
    assert(s.server.cancels.some((entry) => entry.run === 'run2'), 'the run made by sending again was cancelled')
    await s.close()
  })

  await check('send: while waiting for a stop the composer is held, so no message is dropped', async () => {
    let respond
    const s = await scene({ cancel: ({ json, server, end }) => new Promise((resolve) => { respond = async () => { await end(); resolve(json(answered(server.run))) } }) })
    await s.button('停止').click()
    const editor = s.page.getByRole('textbox', { name: '创作问题' })
    await editor.click()
    await s.page.keyboard.type('第二条')
    await s.page.keyboard.press('Enter')
    await s.page.waitForFunction(() => document.querySelector('[aria-label="创作问题"][aria-disabled="true"]'))
    await s.page.keyboard.type('第三条')
    await s.page.keyboard.press('Enter')
    assert.equal(await editor.innerText(), '', 'nothing typed while held')
    await respond()
    for (let i = 0; i < 40 && s.server.sends.length === 0; i++) await sleep(50)
    await sleep(300)
    assert.deepEqual(s.server.sends.map((send) => send.path.split('/').at(-1)), ['egress-consents'], 'only the second message was sent')
    // The mock refuses it, so its words come back to the composer.
    await editor.getByText('第二条').waitFor()
    await s.close()
  })

  const failed = results.filter((result) => !result.ok)
  if (failed.length) {
    console.log(`${failed.length} of ${results.length} failed`)
    process.exitCode = 1
  } else console.log(`PASS: ${results.length} checks`)
} finally {
  await browser.close()
  await rm(fixture, { force: true })
}
