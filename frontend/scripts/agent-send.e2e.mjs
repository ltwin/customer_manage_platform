// Run with the frontend Vite server on CREATIVE_AGENT_URL (default :5179).
// 发送即显示：点发送就画出消息、清空输入框；读回真消息后不重复；
// 往已有会话里发只重读这个会话；被拒时原文回到输入框。
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { writeFile, rm } from 'node:fs/promises'
import { chromium } from 'playwright'

const noticeVersion = /egressNoticeVersion = '([^']+)'/.exec(
  readFileSync(new URL('../src/creative-canvas/editor/agentState.ts', import.meta.url), 'utf8'),
)[1]
const fixture = new URL('../.agent-send-qa.html', import.meta.url)
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
createRoot(document.getElementById('root')).render(React.createElement(AgentPanel,{account:'qa-account',canvasID:'canvas',defaultOpen:true,canvas:{nodes:[]}}));
</script></body></html>`,
)
const url = `${process.env.CREATIVE_AGENT_URL || 'http://localhost:5179'}/.agent-send-qa.html`

const runBase = {
  conversation_id: 'c',
  canvas_id: 'canvas',
  egress_consent_id: 'consent',
  model_key: 'flash',
  kind: 'chat',
  settlement_state: 'settled',
  limits_version: 'creative-agent-8',
  revision: '1',
  last_event_seq: '1',
  pruned_through_seq: '0',
  deadline_at: '2099-01-01T00:00:00Z',
  created_at: '2026-10-15T00:00:00Z',
}
const message = (id, ordinal, run, role, text) => ({
  id,
  ordinal: String(ordinal),
  run_id: run,
  role,
  status: 'complete',
  schema_version: 2,
  revision: '1',
  created_at: '2026-10-15T00:00:00Z',
  body: { blocks: [{ type: 'text', text }] },
})

async function open(browser, { refuse = false } = {}) {
  const page = await browser.newPage({ viewport: { width: 1280, height: 860 } })
  page.on('pageerror', (error) => {
    throw error
  })
  const reads = { catalog: 0, conversations: 0, messages: 0 }
  let accepted = false
  let release
  const gate = new Promise((resolve) => (release = resolve))
  const runs = [{ ...runBase, id: 'run1', trigger_message_id: 'm1', state: 'succeeded', finished_at: '2026-10-15T00:00:10Z' }]
  const messages = [message('m1', 1, 'run1', 'user', '第一轮'), message('m2', 2, 'run1', 'assistant', '第一轮的回答')]
  await page.route('**/api/v1/creative/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const json = (body, status = 200) => route.fulfill({ status, json: body })
    if (request.method() === 'POST') {
      if (path.endsWith('/egress-consents'))
        return json({ id: 'consent-new', conversation_id: 'c', revision: '1', purpose: 'creative_assistance', policy_version: noticeVersion, scope: {}, granted_at: '2026-10-15T00:00:00Z' }, 201)
      if (path.endsWith('/runs')) {
        // Held until the test has looked at the panel mid-send.
        await gate
        if (refuse) return json({ code: 'creative_validation', message: '提交内容无效' }, 422)
        accepted = true
        return json({ ...runBase, id: 'run9', trigger_message_id: 'm9', state: 'queued' }, 202)
      }
      return json({})
    }
    if (path.endsWith('/agent/catalog')) {
      reads.catalog++
      return json({
        limits: { attachments: 10, image_preview_count: 8, auto_compact_percent: 80 },
        skills: [],
        models: [{ model_key: 'flash', display_name: '文字模型', available: true, capability: { image_input: false }, context_window_tokens: 100_000 }],
      })
    }
    if (path.endsWith('/conversations')) {
      reads.conversations++
      return json({ items: [{ id: 'c', title: '秋日人像' }] })
    }
    if (path.endsWith('/conversations/c/runs'))
      return json({ items: accepted ? [{ ...runBase, id: 'run9', trigger_message_id: 'm9', state: 'queued' }, ...runs] : runs })
    if (path.endsWith('/messages')) {
      reads.messages++
      const items = accepted ? [...messages, message('m9', 3, 'run9', 'user', '第二轮：出三版')] : messages
      return json({ items, prev_cursor: '', conversation_revision: '3' })
    }
    const run = /\/agent-runs\/([^/]+)$/.exec(path)?.[1]
    if (run === 'run1') return json({ ...runs[0], blocks: [], snapshot_seq: '1' })
    if (run) return json({ ...runBase, id: run, trigger_message_id: 'm9', state: 'queued', blocks: [], snapshot_seq: '1' })
    return json({ items: [] })
  })
  await page.goto(url)
  await page.getByRole('log', { name: '对话消息' }).getByText('第一轮的回答').waitFor()
  return { page, reads, release }
}

const browser = await chromium.launch({ headless: true })
try {
  // 发送即显示，读回后不重复，且不重读目录和列表。
  {
    const { page, reads, release } = await open(browser)
    const before = { ...reads }
    const editor = page.getByRole('textbox', { name: '创作问题' })
    await editor.click()
    await page.keyboard.type('第二轮：出三版')
    await page.keyboard.press('Enter')
    // The run request is still held: the message is already in the thread.
    const log = page.getByRole('log', { name: '对话消息' })
    await log.locator('.cc-agent-exchange.is-sending').getByText('第二轮：出三版').waitFor()
    assert.equal((await editor.innerText()).trim(), '', 'the composer cleared on send')
    await page.screenshot({ path: '/tmp/creative-agent-send-pending.png' })
    release()
    await log.locator('.cc-agent-exchange.is-sending').waitFor({ state: 'detached' })
    await log.getByText('第二轮：出三版').waitFor()
    assert.equal(await log.getByText('第二轮：出三版').count(), 1, 'the real message replaced the sent one')
    assert.ok(reads.messages > before.messages, 'the thread was reread')
    assert.equal(reads.catalog, before.catalog, 'the catalog was not reread')
    assert.equal(reads.conversations, before.conversations, 'the conversation list was not reread')
    await page.close()
  }
  // 被拒：临时消息撤掉，原文回到输入框。
  {
    const { page, release } = await open(browser, { refuse: true })
    const editor = page.getByRole('textbox', { name: '创作问题' })
    await editor.click()
    await page.keyboard.type('会被拒的一句')
    await page.keyboard.press('Enter')
    const log = page.getByRole('log', { name: '对话消息' })
    await log.locator('.cc-agent-exchange.is-sending').waitFor()
    release()
    await page.getByRole('alert').waitFor()
    await log.locator('.cc-agent-exchange.is-sending').waitFor({ state: 'detached' })
    await page.waitForFunction(() => document.querySelector('[aria-label="创作问题"]')?.textContent?.includes('会被拒的一句'))
    await page.close()
  }
  console.log('agent send e2e passed')
} finally {
  await browser.close()
  await rm(fixture, { force: true })
}
