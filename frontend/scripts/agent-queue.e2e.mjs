// 消息排队与插队：运行中回车排队、同帧清空；成功结束只发出队首一条；停止或
// partial 后暂停，点「继续发送」恢复；插队同帧出现气泡，被拒回到队首；结果
// 未知时重放同一请求；Skill / 图片不能插队并说明原因；刷新和交接中刷新都不丢。
// Run with the frontend Vite server on CREATIVE_AGENT_URL (default :5179).
//   node scripts/agent-queue.e2e.mjs   (AGENT_QUEUE_E2E_ONLY=<part of a name> runs matching checks)
// Everything is mocked: HTTP by route, the SSE stream by a fetch wrapper the
// test feeds frame by frame.
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { writeFile, rm } from 'node:fs/promises'
import { chromium } from 'playwright'

const noticeVersion = /egressNoticeVersion = '([^']+)'/.exec(
  readFileSync(new URL('../src/creative-canvas/editor/agentState.ts', import.meta.url), 'utf8'),
)[1]
const fixture = new URL('../.agent-queue-qa.html', import.meta.url)
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
const url = `${process.env.CREATIVE_AGENT_URL || 'http://localhost:5179'}/.agent-queue-qa.html`
const browser = await chromium.launch({ headless: true })
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
const queueKey = 'creative-agent-queue:qa-account:conversation'
const pendingKey = 'creative-agent-pending:qa-account:canvas'
const terminal = ['succeeded', 'partial', 'failed', 'cancelled']

// A queued message as the panel stores it, for seeding the tab's storage.
const stored = (id, text, extra = {}) => ({
  id,
  parts: [{ kind: 'text', text }],
  draft: { conversation: 'conversation', model: 'model', skill: '', text },
  send: { model: 'model', imageInput: false, revisionIDs: [], segments: [{ type: 'text', text }], scene: null },
  steer: [{ type: 'text', text }],
  hasImage: false,
  ...extra,
})

// One mocked run, `run`, in `state`. `send` answers every POST that is not a
// cancel; by default it accepts: a consent, a new run, a steer or a supplement.
async function scene({ state = 'running', send, storage } = {}) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 860 } })
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  const server = {
    seq: 0,
    log: [],
    blocks: [],
    sends: [],
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
      created_at: '2026-11-11T00:00:00Z',
    },
  }
  const frame = (event) => `id: ${event.seq}\nevent: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`
  await page.exposeFunction('__replay', (target) => {
    const parsed = new URL(target, 'http://x')
    if (!parsed.pathname.includes('/agent-runs/run/')) return []
    const after = Number(parsed.searchParams.get('after_seq') ?? 0)
    return server.log.filter((event) => Number(event.seq) > after).map(frame)
  })
  if (storage)
    await page.addInitScript((entries) => {
      // Only on the first load: a reload must find what the page itself saved.
      if (sessionStorage.getItem('__seeded')) return
      sessionStorage.setItem('__seeded', '1')
      for (const [key, value] of Object.entries(entries)) sessionStorage.setItem(key, value)
    }, storage)
  await page.addInitScript(() => {
    const real = window.fetch.bind(window)
    const encoder = new TextEncoder()
    window.__sse = { controller: null }
    window.fetch = async (input, init) => {
      const target = typeof input === 'string' ? input : input.url
      if (!/\/agent-runs\/[^/]+\/events/.test(target)) return real(input, init)
      const own = target.includes('/agent-runs/run/')
      const frames = await window.__replay(target)
      const body = new ReadableStream({
        start(controller) {
          if (own) window.__sse.controller = controller
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
    // A finished run's stream is closed; what it would have carried is in the snapshot.
    window.__push = (text) => {
      try {
        window.__sse.controller?.enqueue(encoder.encode(text))
      } catch {}
    }
  })
  const emit = async (type, payload) => {
    const event = { schema_version: 1, run_id: 'run', seq: String(++server.seq), type, created_at: '2026-11-11T00:00:00Z', payload }
    server.log.push(event)
    if (type === 'block.created') server.blocks.push(structuredClone(payload))
    else if (type === 'block.updated') {
      const block = server.blocks.find((item) => item.id === payload.block_id)
      block.revision = payload.revision
      if (payload.state) block.state = payload.state
      if (payload.finished_at) block.finished_at = payload.finished_at
    } else if (payload.state) {
      server.run = { ...server.run, state: payload.state }
      if (terminal.includes(payload.state)) server.run.finished_at = '2026-11-11T00:01:00Z'
    }
    await page.evaluate((text) => window.__push(text), frame(event))
  }
  const finish = (to) => emit('run.state_changed', { state: to })
  // What the server writes when it takes a message into the run.
  const accept = async (segments) => {
    const ordinal = server.blocks.length + 1
    await emit('block.created', {
      id: `ccbk_input_${ordinal}`, run_id: 'run', ordinal: String(ordinal), type: 'input', state: 'pending', revision: '1',
      payload_version: 1, hidden: false, started_at: '2026-11-11T00:00:02Z',
      payload: {
        message_id: `cmsg_input_${ordinal}`,
        body: { schema_version: 1, blocks: segments.map((segment) => ({ type: 'text', text: segment.text })) },
      },
    })
    return { ...server.run, last_event_seq: String(server.seq) }
  }
  const accepting = async ({ path, json, body, server }) => {
    if (path.endsWith('/egress-consents')) return json({ id: `consent-${server.sends.length}` })
    if (path.endsWith('/steers') || path.endsWith('/supplements'))
      return json(await accept(body.payload.instruction_segments), 202)
    const id = `run${Object.keys(server.runs).length + 2}`
    server.runs[id] = { ...server.run, id, state: 'queued', trigger_message_id: `message-${id}`, last_event_seq: '1' }
    delete server.runs[id].finished_at
    return json(server.runs[id])
  }
  await page.route('**/api/v1/creative/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const json = (body, status = 200) => route.fulfill({ status, json: body })
    if (request.method() === 'POST') {
      const body = request.postDataJSON()
      const entry = { path, body, key: request.headers()['idempotency-key'], at: Date.now() }
      server.sends.push(entry)
      if (path.endsWith('/cancel')) {
        await finish('cancelled')
        return json({ ...server.run })
      }
      return (send ?? accepting)({ path, json, body, server, accept, accepting, index: server.sends.length - 1 })
    }
    const other = /\/agent-runs\/([^/]+)$/.exec(path)
    if (other && server.runs[other[1]]) return json({ ...server.runs[other[1]], blocks: [], snapshot_seq: '0' })
    if (path.endsWith('/agent/catalog'))
      return json({
        limits: { attachments: 10, image_preview_count: 8 },
        models: [{ model_key: 'model', display_name: 'Test model', capability: { image_input: false }, available: true }],
        skills: [],
      })
    if (path.endsWith('/conversations')) return json({ items: [{ id: 'conversation', title: '对话', title_source: 'manual', updated_at: '2026-11-11T00:00:00Z' }] })
    if (path.endsWith('/conversations/conversation/runs')) return json({ items: [...Object.values(server.runs).reverse(), server.run] })
    if (path.endsWith('/messages'))
      return json({
        items: [{
          id: 'message', ordinal: '1', role: 'user', status: 'complete', schema_version: 2,
          body: { blocks: [{ type: 'text', text: '帮我拆分镜' }] }, revision: '1', created_at: '2026-11-11T00:00:00Z',
        }],
      })
    if (path.endsWith('/agent-runs/run'))
      return json({ ...server.run, last_event_seq: String(server.seq), blocks: server.blocks, snapshot_seq: String(server.seq) })
    return json({ items: [] })
  })
  await page.goto(url)
  await page.locator('.cc-agent-turn').first().waitFor({ state: 'attached' })
  if (!terminal.includes(state)) await page.waitForFunction(() => window.__sse.controller !== null)
  const editor = () => page.getByRole('textbox', { name: state === 'waiting_input' ? '补充内容' : '创作问题' })
  const type = async (text) => {
    await editor().click()
    await page.keyboard.type(text)
    await page.keyboard.press('Enter')
  }
  const queue = page.getByRole('region', { name: '排队的消息' })
  const queued = () => queue.locator('li').allInnerTexts()
  const button = (name) => page.locator('.cc-agent-composer-foot').getByRole('button', { name, exact: true })
  const until = async (predicate, what) => {
    for (let i = 0; i < 60 && !predicate(); i++) await sleep(50)
    assert(predicate(), what)
  }
  return {
    page, server, emit, finish, accept, editor, type, queue, queued, button, until, errors,
    close: async () => {
      assert.deepEqual(errors, [], 'no page errors')
      await context.close()
    },
  }
}

const paths = (s) => s.server.sends.map((entry) => entry.path.replace('/api/v1/creative', ''))

const results = []
const only = process.env.AGENT_QUEUE_E2E_ONLY
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
  await check('enqueue: Enter while running queues the message and clears the composer in the same frame', async () => {
    const s = await scene()
    await s.button('停止').waitFor()
    await s.editor().click()
    await s.page.keyboard.type('换成侧光')
    await s.button('加入排队').waitFor()
    assert.equal(await s.button('停止').count(), 0, 'the composer has words: no stop')
    await s.page.keyboard.press('Enter')
    await s.queue.getByText('换成侧光').waitFor({ timeout: 300 })
    assert.equal(await s.editor().innerText(), '', 'composer cleared')
    await s.button('停止').waitFor({ timeout: 300 })
    assert.equal(s.server.sends.length, 0, 'nothing sent while the run goes on')
    await s.type('再加一盏轮廓光')
    assert.deepEqual(await s.queued(), ['换成侧光\n插队', '再加一盏轮廓光\n插队'])
    await s.page.locator('.cc-agent-compose').screenshot({ path: '/tmp/agent-queue-list.png' })
    await s.close()
  })

  await check('auto: a run that succeeds sends the head once, the rest waits', async () => {
    const s = await scene()
    await s.type('第一条')
    await s.type('第二条')
    await s.finish('succeeded')
    await s.until(() => s.server.sends.length >= 2, 'the head was sent')
    await sleep(800)
    assert.deepEqual(paths(s), ['/conversations/conversation/egress-consents', '/conversations/conversation/runs'])
    assert.deepEqual(s.server.sends[1].body.payload.instruction.instruction_segments, [{ type: 'text', text: '第一条' }])
    await s.page.locator('.cc-agent-user', { hasText: '第一条' }).waitFor()
    assert.deepEqual(await s.queued(), ['第二条\n插队'], 'the second stays queued behind the new run')
    // The same finished run seen again (a snapshot, a reconnect) sends nothing more.
    await s.emit('run.state_changed', { state: 'succeeded' })
    await sleep(500)
    assert.equal(s.server.sends.length, 2)
    await s.close()
  })

  await check('remove: deleting a queued message sends nothing', async () => {
    const s = await scene()
    await s.type('第一条')
    await s.type('第二条')
    await s.queue.getByRole('button', { name: '删除这条' }).first().click()
    assert.deepEqual(await s.queued(), ['第二条\n插队'])
    await s.finish('succeeded')
    await s.until(() => s.server.sends.length >= 2, 'the remaining one was sent')
    assert.deepEqual(s.server.sends[1].body.payload.instruction.instruction_segments, [{ type: 'text', text: '第二条' }])
    await s.close()
  })

  await check('auto: a refused head goes back, the queue pauses, the composer keeps its words', async () => {
    const s = await scene({
      send: (input) =>
        input.path.endsWith('/runs')
          ? input.json({ error: { code: 'creative_agent_busy', message: '账号已有运行' } }, 429)
          : input.accepting(input),
    })
    await s.type('第一条')
    await s.editor().click()
    await s.page.keyboard.type('还在写')
    await s.finish('succeeded')
    await s.page.getByText('账号已有运行').waitFor()
    await s.queue.getByText('已暂停，不会自动发出').waitFor()
    assert.deepEqual(await s.queued(), ['第一条'], 'back at the head')
    assert.equal(await s.editor().innerText(), '还在写', 'the composer is untouched')
    await s.close()
  })

  for (const ending of ['stop', 'partial']) {
    await check(`pause: after ${ending} the queue holds until 继续发送`, async () => {
      const s = await scene()
      await s.type('第一条')
      if (ending === 'stop') {
        await s.editor().click()
        await s.button('停止').click()
      } else await s.finish('partial')
      await s.queue.getByText('已暂停，不会自动发出').waitFor()
      await sleep(800)
      assert.equal(s.server.sends.filter((entry) => !entry.path.endsWith('/cancel')).length, 0, 'nothing sent')
      await s.queue.getByRole('button', { name: '继续发送' }).click()
      await s.until(() => s.server.sends.some((entry) => entry.path.endsWith('/runs')), 'sent after 继续发送')
      await s.page.locator('.cc-agent-user', { hasText: '第一条' }).waitFor()
      assert.equal(await s.queue.count(), 0, 'queue empty')
      await s.close()
    })
  }

  await check('steer: the bubble shows in the same frame and gives way to the input block', async () => {
    let release
    const gate = new Promise((resolve) => (release = resolve))
    const s = await scene({
      send: async (input) => {
        await gate
        return input.accepting(input)
      },
    })
    await s.type('背景换成黄昏')
    await s.queue.getByRole('button', { name: '插队' }).click()
    const turn = s.page.locator('.cc-agent-exchange').first()
    await turn.locator('.cc-agent-user[aria-busy="true"]', { hasText: '背景换成黄昏' }).waitFor({ timeout: 300 })
    assert.equal(await s.queue.count(), 0, 'out of the queue')
    release()
    await turn.locator('[data-type="input"] .cc-agent-user', { hasText: '背景换成黄昏' }).waitFor()
    await s.page.waitForFunction(() => !document.querySelector('.cc-agent-user[aria-busy="true"]'))
    assert.equal(await s.page.locator('.cc-agent-user', { hasText: '背景换成黄昏' }).count(), 1, 'drawn once')
    assert.deepEqual(paths(s), ['/agent-runs/run/steers'])
    assert.deepEqual(s.server.sends[0].body.payload, { instruction_segments: [{ type: 'text', text: '背景换成黄昏' }] })
    // Ended before the assistant read it: said so.
    await s.emit('block.updated', { block_id: 'ccbk_input_1', revision: '2', state: 'interrupted', finished_at: '2026-11-11T00:01:00Z' })
    await s.page.getByText('助手没有读到这条').waitFor()
    await s.page.locator('.cc-agent-turn').first().screenshot({ path: '/tmp/agent-queue-steer.png' })
    await s.close()
  })

  await check('steer: refused with 409 goes back to the head and says it waits for the round to end', async () => {
    const s = await scene({
      send: ({ json }) => json({ error: { code: 'creative_run_state', message: 'too late' } }, 409),
    })
    await s.type('第一条')
    await s.type('第二条')
    await s.queue.getByRole('button', { name: '插队' }).nth(1).click()
    await s.page.getByText('这一轮不能再插入，会在本轮结束后发送').waitFor()
    assert.deepEqual(await s.queued(), ['第二条\n插队', '第一条\n插队'], 'back at the head')
    assert.equal(await s.page.locator('.cc-agent-user[aria-busy="true"]').count(), 0, 'no bubble left')
    assert.equal(await s.editor().innerText(), '', 'the composer is not filled')
    await s.close()
  })

  await check('steer: an unknown result is sent again as the same request', async () => {
    const s = await scene({
      send: (input) =>
        input.index === 0
          ? input.json({ error: { code: 'creative_unavailable', message: 'down' } }, 503)
          : input.accepting(input),
    })
    await s.type('补一盏灯')
    await s.queue.getByRole('button', { name: '插队' }).click()
    const again = s.page.getByRole('button', { name: '重新发送' })
    await again.waitFor()
    assert.equal(await s.queue.count(), 0, 'the chain owns it now')
    await again.click()
    await s.page.locator('[data-type="input"] .cc-agent-user', { hasText: '补一盏灯' }).waitFor()
    assert.equal(s.server.sends.length, 2)
    assert.equal(s.server.sends[0].key, s.server.sends[1].key, 'same idempotency key')
    assert.deepEqual(s.server.sends[0].body, s.server.sends[1].body, 'same request')
    await s.close()
  })

  await check('waiting: a refused supplement says to stop and continue', async () => {
    const s = await scene({
      state: 'waiting_input',
      storage: { [queueKey]: JSON.stringify({ items: [stored('q1', '用暖色')], paused: false }) },
      send: ({ json }) => json({ error: { code: 'creative_input_limit', message: 'too many' } }, 413),
    })
    await s.queue.getByRole('button', { name: '插队' }).click()
    await s.page.getByText('这一轮不能再补充，停止本轮后点「继续发送」').waitFor()
    assert.deepEqual(paths(s), ['/agent-runs/run/supplements'])
    assert.deepEqual(s.server.sends[0].body.payload, { expected_revision: '1', waiting_token: 'token', instruction_segments: [{ type: 'text', text: '用暖色' }] })
    assert.deepEqual(await s.queued(), ['用暖色\n插队'])
    await s.close()
  })

  await check('blocked: a message with a Skill or an image can only wait, and says why', async () => {
    const s = await scene({
      storage: {
        [queueKey]: JSON.stringify({
          items: [
            stored('q1', '按分镜 Skill', { steer: null }),
            stored('q2', '看这张图', { hasImage: true }),
          ],
          paused: false,
        }),
      },
    })
    const steer = s.queue.getByRole('button', { name: '插队' })
    await steer.first().waitFor()
    assert.equal(await steer.nth(0).getAttribute('aria-disabled'), 'true')
    assert.equal(await steer.nth(0).getAttribute('title'), '选了 Skill 的消息只能排队')
    assert.equal(await steer.nth(1).getAttribute('title'), '当前模型不支持图片，只能排队')
    await steer.nth(0).click({ force: true })
    await sleep(300)
    assert.equal(s.server.sends.length, 0, 'a disabled steer sends nothing')
    await s.close()
  })

  await check('reload: the queue is still there after a reload', async () => {
    const s = await scene()
    await s.type('刷新前写的')
    await s.page.reload()
    await s.queue.getByText('刷新前写的').waitFor()
    assert.equal(s.server.sends.length, 0)
    await s.close()
  })

  await check('reload: mid-handover the message is in the chain or the queue, never both or neither', async () => {
    const chain = {
      steps: [{ path: '/agent-runs/run/steers', operation: 'op-1', createdAt: '2026-11-11T00:00:00Z', payload: { instruction_segments: [{ type: 'text', text: '链里的' }] } }],
      draft: { conversation: 'conversation', model: 'model', skill: '', text: '链里的', queueItem: 'q1' },
    }
    const s = await scene({
      storage: {
        [pendingKey]: JSON.stringify(chain),
        [queueKey]: JSON.stringify({
          items: [stored('q1', '链里的', { sending: true }), stored('q2', '没发出的', { sending: true })],
          paused: false,
        }),
      },
    })
    await s.page.getByRole('button', { name: '重新发送' }).waitFor()
    await s.queue.getByText('没发出的').waitFor()
    assert.deepEqual(await s.queued(), ['没发出的\n插队'], 'the chain owns one, the other is back at the head')
    await s.close()
  })

  await check('same: a queued message is the request a direct send would have made', async () => {
    const direct = await scene({ state: 'succeeded' })
    await direct.type('同一句话')
    await direct.until(() => direct.server.sends.length >= 2, 'sent directly')
    const queued = await scene()
    await queued.type('同一句话')
    await queued.finish('succeeded')
    await queued.until(() => queued.server.sends.length >= 2, 'sent from the queue')
    const shape = (s) => s.server.sends.slice(0, 2).map((entry) => ({ path: entry.path, payload: entry.body.payload }))
    assert.deepEqual(shape(queued), shape(direct))
    await direct.close()
    await queued.close()
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
