// V6-030: ordered blocks and the run stream, against the real panel.
// Run with the frontend Vite server on CREATIVE_AGENT_URL (default :5179).
// Everything is mocked: HTTP by route, the SSE stream by a fetch wrapper the
// test feeds frame by frame. Each acceptance criterion gets a fresh context.
import assert from 'node:assert/strict'
import { writeFile, rm } from 'node:fs/promises'
import { chromium } from 'playwright'

const fixture = new URL('../.agent-blocks-qa.html', import.meta.url)
const pixel =
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='
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
const media=[{role:'display',blob_id:'blob',mime:'image/png',byte_size:1,width:1,height:1,duration_ms:null}];
createRoot(document.getElementById('root')).render(React.createElement(AgentPanel,{account:'qa-account',canvasID:'canvas',canvas:{nodes:[{id:'node-image',metadata:{title:'分镜 01'},data:{content_revision_id:'ccrv_image'},content:{id:'ccrv_image',kind:'image',payload:{},media,truncated:false}}]}}));
</script></body></html>`,
)
const url = `${process.env.CREATIVE_AGENT_URL || 'http://localhost:5179'}/.agent-blocks-qa.html`
const browser = await chromium.launch({ headless: true })
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

// One mocked run: the server-side truth (blocks + event log) lives here, and
// every emit both records it and pushes it down the open stream.
async function scene(options = {}) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 860 } })
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  const server = {
    seq: 0,
    log: [],
    blocks: [],
    run: {
      id: 'run',
      conversation_id: 'conversation',
      canvas_id: 'canvas',
      trigger_message_id: 'message',
      egress_consent_id: 'consent',
      model_key: 'model',
      state: 'running',
      settlement_state: 'pending',
      limits_version: '1',
      revision: '1',
      last_event_seq: '0',
      pruned_through_seq: '0',
      deadline_at: '2026-10-09T01:00:00Z',
      created_at: '2026-10-09T00:00:00Z',
    },
    snapshots: 0,
    connects: [],
    schema: 1,
  }
  const frame = (event) =>
    `id: ${event.seq}\nevent: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`
  await page.exposeFunction('__replay', (target) => {
    const after = Number(new URL(target, 'http://x').searchParams.get('after_seq') ?? 0)
    server.connects.push(after)
    return server.log.filter((event) => Number(event.seq) > after).map(frame)
  })
  await page.addInitScript(() => {
    const real = window.fetch.bind(window)
    const encoder = new TextEncoder()
    window.__sse = { down: false, controller: null }
    window.fetch = async (input, init) => {
      const target = typeof input === 'string' ? input : input.url
      if (!/\/agent-runs\/[^/]+\/events/.test(target)) return real(input, init)
      if (window.__sse.down) throw new TypeError('network down')
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
    window.__drop = () => {
      try {
        window.__sse.controller?.error(new TypeError('connection lost'))
      } catch {}
      window.__sse.controller = null
    }
  })
  await page.route('**/api/v1/creative/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const json = (body) => route.fulfill({ json: body })
    if (path.endsWith('/media-access-tickets'))
      return json({ url: pixel, expires_at: '2099-01-01T00:00:00Z' })
    if (path.endsWith('/catalog'))
      return json({
        limits: { attachments: 10, image_preview_count: 8 },
        models: [
          {
            model_key: 'model',
            display_name: 'Test model',
            capability: { image_input: false },
            available: true,
          },
        ],
        skills: [],
      })
    if (path.endsWith('/conversations'))
      return json({ items: [{ id: 'conversation', title: '对话' }] })
    if (path.endsWith('/conversations/conversation/runs'))
      return json({ items: [server.run] })
    if (path.endsWith('/messages'))
      return json({
        items: [
          {
            id: 'message',
            ordinal: '1',
            role: 'user',
            status: 'complete',
            schema_version: 2,
            body: { blocks: [{ type: 'text', text: '帮我拆分镜' }] },
            revision: '1',
            created_at: '2026-10-09T00:00:00Z',
          },
        ],
      })
    if (path.endsWith('/agent-runs/run')) {
      server.snapshots++
      return json({
        ...server.run,
        last_event_seq: String(server.seq),
        blocks: server.blocks,
        snapshot_seq: String(server.seq),
      })
    }
    if (path.endsWith('/detail'))
      return json({
        block_id: path.split('/').at(-2),
        step_id: 'step',
        tool_key: 'read_canvas',
        arguments: { scope: 'selection' },
        result: '画布上有 3 个节点',
      })
    return json({ items: [] })
  })
  const emit = async (type, payload, { deliver = true } = {}) => {
    const event = {
      schema_version: server.schema,
      run_id: 'run',
      seq: String(++server.seq),
      type,
      created_at: '2026-10-09T00:00:00Z',
      payload,
    }
    server.log.push(event)
    if (type === 'block.created') server.blocks.push(structuredClone(payload))
    else if (type === 'block.updated') {
      const block = server.blocks.find((item) => item.id === payload.block_id)
      block.revision = payload.revision
      if (payload.state) block.state = payload.state
      if (payload.finished_at) block.finished_at = payload.finished_at
      if (payload.patch?.set) block.payload = payload.patch.set
      if (payload.patch?.append)
        block.payload[payload.patch.append.path] =
          (block.payload[payload.patch.append.path] ?? '') + payload.patch.append.text
    } else if (payload.state) {
      server.run = { ...server.run, state: payload.state }
      if (['succeeded', 'failed', 'cancelled'].includes(payload.state))
        server.run.finished_at = '2026-10-09T00:01:00Z'
    }
    if (deliver) await page.evaluate((text) => window.__push(text), frame(event))
    return event
  }
  let ordinal = 0
  const revisions = new Map()
  const create = (type, state, payload, more = {}) => {
    const id = `ccbk_${++ordinal}`
    revisions.set(id, 1)
    return emit(
      'block.created',
      {
        id,
        run_id: 'run',
        ordinal: String(ordinal),
        type,
        state,
        revision: '1',
        payload_version: 1,
        hidden: false,
        payload,
        started_at: `2026-10-09T00:00:${String(ordinal * 2).padStart(2, '0')}Z`,
        ...more,
      },
      more.delivery,
    ).then(() => id)
  }
  const update = (id, change, delivery) => {
    revisions.set(id, revisions.get(id) + 1)
    return emit(
      'block.updated',
      { block_id: id, revision: String(revisions.get(id)), ...change },
      delivery,
    )
  }
  const append = (id, text, delivery) =>
    update(id, { patch: { append: { path: 'markdown', text } } }, delivery)
  const tool = async (title, payload = {}) =>
    create('tool', 'running', {
      step_id: `step-${ordinal + 1}`,
      tool_key: 'read_canvas',
      title,
      started_at: '2026-10-09T00:00:00Z',
      detail_ref: 'step',
      node_refs: [],
      ...payload,
    })
  const finishTool = (id, seconds, state = 'succeeded') => {
    const block = server.blocks.find((item) => item.id === id)
    return update(id, {
      state,
      finished_at: new Date(Date.parse(block.started_at) + seconds * 1000).toISOString(),
      payload_version: 1,
      patch: { set: { ...block.payload, duration_ms: seconds * 1000 } },
    })
  }
  const open = async () => {
    await page.getByRole('button', { name: '打开创作助手' }).click()
    await page.locator('.cc-agent-turn').waitFor({ state: 'attached' })
  }
  if (options.prepare) await options.prepare({ server, emit, create })
  await page.goto(url)
  await open()
  // The stream is attached once the first connect has been replayed.
  await page.waitForFunction(() => window.__sse.controller !== null)
  const order = () =>
    page.locator('.cc-agent-turn [data-block]').evaluateAll((nodes) =>
      nodes.map((node) => `${node.dataset.type}:${node.dataset.block}:${node.dataset.state}`),
    )
  return {
    page, server, emit, create, update, append, tool, finishTool, open, order, errors,
    close: async () => {
      assert.deepEqual(errors, [], 'no page errors')
      await context.close()
    },
  }
}

try {
  // AC-01: blocks arrive in order under stable ids; the answer streams as
  // Markdown; what the photographer expanded stays expanded.
  {
    const s = await scene()
    const read = await s.tool('读取画布')
    const row = s.page.locator(`[data-block="${read}"]`)
    await row.getByText('读取画布').waitFor()
    assert.equal(await row.locator('.cc-agent-spin').count(), 1, 'a running tool spins')
    await s.finishTool(read, 2)
    await row.getByText('2 秒').waitFor()
    await row.getByRole('button').click()
    await row.getByText('画布上有 3 个节点').waitFor()
    await row.evaluate((node) => (node.__mark = 'same element'))
    const answer = await s.create('text', 'streaming', { markdown: '' })
    const body = s.page.locator(`[data-block="${answer}"]`)
    await s.append(answer, '先定 **主')
    await body.getByText('先定 **主').waitFor()
    assert.equal(await body.locator('b').count(), 0, 'an unclosed marker is plain text')
    assert.equal(await body.locator('.cc-agent-caret').count(), 1)
    await s.append(answer, '光**，再看：\n\n1. 清晨\n2. 逆光')
    await body.locator('b', { hasText: '主光' }).waitFor()
    assert.deepEqual(await body.locator('ol li').allTextContents(), ['清晨', '逆光'])
    await row.hover()
    for (const piece of ['\n3. 侧', '光', '\n\n用 `f/2.8`。']) await s.append(answer, piece)
    await body.locator('code', { hasText: 'f/2.8' }).waitFor()
    assert.equal(await row.getByRole('button').getAttribute('aria-expanded'), 'true')
    assert.equal(await row.getByText('画布上有 3 个节点').count(), 1, 'detail survives the stream')
    assert.equal(await row.evaluate((node) => node.__mark), 'same element', 'the row was not rebuilt')
    assert.deepEqual(await s.order(), [
      `tool:${read}:succeeded`,
      `text:${answer}:streaming`,
    ])
    await s.page.screenshot({ path: '/tmp/creative-v6-agent-blocks-stream.png' })
    await s.close()
  }

  // AC-02: tool state comes from the server; a stretch of process blocks folds
  // into time and operation count once something follows it, and reopens.
  {
    const s = await scene()
    const first = await s.tool('读取画布')
    const second = await s.tool('读取素材库')
    const rows = s.page.locator('.cc-agent-turn [data-type="tool"]')
    await rows.nth(1).waitFor()
    assert.equal(await s.page.locator('.cc-agent-fold').count(), 0, 'the live tail has no header')
    assert.equal(await s.page.locator('.cc-agent-status').textContent(), '读取素材库停止')
    await s.finishTool(first, 3)
    await s.finishTool(second, 4, 'failed')
    await s.page.locator('.cc-agent-tool.is-failed').getByText('失败').waitFor()
    const answer = await s.create('text', 'streaming', { markdown: '看完了。' })
    const fold = s.page.locator('.cc-agent-fold')
    await fold.waitFor()
    assert.equal(await fold.textContent(), '已处理 6 秒 · 2 次操作 · 1 项失败')
    assert.equal(await rows.count(), 0, 'the segment folds when a conclusion follows')
    await fold.click()
    assert.equal(await rows.count(), 2)
    await rows.first().getByRole('button').click()
    await rows.first().getByText('画布上有 3 个节点').waitFor()
    await s.append(answer, '继续。')
    await s.update(answer, { state: 'complete', finished_at: '2026-10-09T00:00:20Z' })
    await s.emit('run.finished', { state: 'succeeded', settlement_state: 'settled' })
    await s.page.locator('.cc-agent-turn[data-state="succeeded"]').waitFor()
    assert.equal(await rows.count(), 2, 'the photographer opened it; it stays open')
    assert.equal(await s.page.locator('.cc-agent-status').count(), 0, 'no status once finished')
    await s.page.screenshot({ path: '/tmp/creative-v6-agent-blocks-fold.png' })
    await s.close()
  }

  // AC-03: the plan shows current, done and skipped steps; a tool's results
  // hang under its own row.
  {
    const s = await scene()
    const plan = await s.create('plan', 'active', {
      steps: [
        { key: 'a', title: '读画布', state: 'done' },
        { key: 'b', title: '拆分镜', state: 'doing' },
        { key: 'c', title: '补道具', state: 'skipped' },
        { key: 'd', title: '写提示词', state: 'todo' },
      ],
    })
    const card = s.page.locator(`[data-block="${plan}"]`)
    await card.getByText('1/4').waitFor()
    assert.deepEqual(
      await card.locator('li').evaluateAll((items) => items.map((item) => item.className)),
      ['is-done', 'is-doing', 'is-skipped', 'is-todo'],
    )
    assert.equal(await card.locator('li.is-doing .cc-agent-spin').count(), 1)
    await s.update(plan, {
      payload_version: 1,
      patch: {
        set: {
          steps: [
            { key: 'a', title: '读画布', state: 'done' },
            { key: 'b', title: '拆分镜', state: 'done' },
            { key: 'c', title: '补道具', state: 'skipped' },
            { key: 'd', title: '写提示词', state: 'doing' },
          ],
        },
      },
    })
    await card.getByText('2/4').waitFor()
    const made = await s.tool('生成分镜草图', { node_refs: [{ node_id: 'node-image' }] })
    const other = await s.tool('读取画布')
    const thumb = s.page.locator(`[data-block="${made}"] .cc-agent-thumbs img`)
    await thumb.waitFor()
    assert.equal(await thumb.getAttribute('alt'), '分镜 01')
    assert.equal(await s.page.locator(`[data-block="${other}"] .cc-agent-thumbs`).count(), 0)
    await s.page.screenshot({ path: '/tmp/creative-v6-agent-blocks-plan.png' })
    await s.close()
  }

  // AC-04: a refresh rebuilds the same list in the same order, and nothing
  // stands in for reasoning the vendor did not publish.
  {
    const s = await scene()
    const read = await s.tool('读取画布')
    await s.finishTool(read, 2)
    const answer = await s.create('text', 'streaming', { markdown: '第一段' })
    const again = await s.tool('读取素材库')
    await s.page.locator(`[data-block="${again}"]`).waitFor()
    const before = await s.order()
    assert.equal(before.length, 3)
    assert.equal(await s.page.locator('[data-type="thinking"]').count(), 0, 'no invented thinking block')
    const words = await s.page.locator(`[data-block="${answer}"]`).textContent()
    await s.page.reload()
    await s.open()
    await s.page.locator(`[data-block="${again}"]`).waitFor()
    assert.deepEqual(await s.order(), before)
    assert.equal(await s.page.locator(`[data-block="${answer}"]`).textContent(), words)
    // A block type from a later release degrades alone.
    await s.create('proposal', 'waiting', { node_id: 'x' })
    await s.page.getByText('此内容需要更新页面后查看').waitFor()
    assert.equal((await s.order()).length, 4)
    await s.close()
  }

  // A dropped stream resumes after the last seq it held: nothing missing,
  // nothing twice.
  {
    const s = await scene()
    const answer = await s.create('text', 'streaming', { markdown: '' })
    await s.append(answer, '一')
    await s.page.locator(`[data-block="${answer}"]`).getByText('一').waitFor()
    await s.page.evaluate(() => window.__drop())
    const offline = { deliver: false }
    await s.append(answer, '二', offline)
    await s.append(answer, '三', offline)
    const late = await s.create(
      'tool',
      'running',
      { step_id: 's', tool_key: 'read_canvas', title: '读取画布', started_at: '2026-10-09T00:00:00Z', detail_ref: 's', node_refs: [] },
      { delivery: offline },
    )
    await s.page.locator(`[data-block="${late}"]`).waitFor({ timeout: 8000 })
    assert.equal(await s.page.locator(`[data-block="${answer}"]`).textContent(), '一二三')
    assert.deepEqual(s.server.connects, [0, 2], 'the second connect resumes after seq 2')
    // An event the page cannot place sends it back to the snapshot.
    const snapshots = s.server.snapshots
    s.server.seq += 3
    await s.append(answer, '四')
    await s.page.waitForFunction(() => window.__sse.controller !== null)
    for (let i = 0; i < 80 && s.server.snapshots === snapshots; i++) await sleep(50)
    assert(s.server.snapshots > snapshots, 'a gap re-reads the snapshot')
    await s.page.locator(`[data-block="${answer}"]`).getByText('一二三四').waitFor({ timeout: 8000 })
    assert.equal((await s.order()).length, 2, 'no block twice')
    await s.close()
  }

  // The run is followed with the panel closed: the launcher shows it, and
  // reopening finds everything, including what was expanded.
  {
    const s = await scene()
    const read = await s.tool('读取画布')
    await s.finishTool(read, 2)
    const answer = await s.create('text', 'streaming', { markdown: '开头' })
    const row = s.page.locator(`[data-block="${read}"]`)
    await row.getByRole('button').click()
    await row.getByText('画布上有 3 个节点').waitFor()
    await s.page.getByRole('button', { name: '收起创作助手' }).click()
    const launcher = s.page.getByRole('button', { name: '打开创作助手' })
    assert.equal(await launcher.getAttribute('data-state'), 'run')
    const snapshots = s.server.snapshots
    await s.append(answer, '，中间')
    await s.append(answer, '，结尾。')
    await s.update(answer, { state: 'complete', finished_at: '2026-10-09T00:00:30Z' })
    await s.emit('run.finished', { state: 'succeeded', settlement_state: 'settled' })
    await s.page.waitForFunction(
      () => !document.querySelector('.cc-agent-launch').dataset.state,
    )
    await launcher.click()
    await s.page.locator(`[data-block="${answer}"]`).getByText('开头，中间，结尾。').waitFor()
    assert.equal(await row.getByRole('button').getAttribute('aria-expanded'), 'true')
    assert(s.server.snapshots <= snapshots + 1, 'content came from the stream, not from re-reading')
    await s.close()
  }

  // When the stream cannot connect at all, the snapshot is polled instead.
  {
    const s = await scene()
    await s.page.evaluate(() => {
      window.__sse.down = true
      window.__drop()
    })
    const offline = { deliver: false }
    const answer = await s.create('text', 'streaming', { markdown: '轮询也能看到' }, { delivery: offline })
    await s.page.locator(`[data-block="${answer}"]`).getByText('轮询也能看到').waitFor({ timeout: 8000 })
    await s.close()
  }
  console.log(
    'PASS: ordered blocks stream as Markdown with stable rows; segments fold and reopen; plan and thumbnails; refresh, reconnect, gap recovery, closed-panel tracking and polling fallback',
  )
} finally {
  await browser.close()
  await rm(fixture, { force: true })
}
