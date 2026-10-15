// Run with the frontend Vite server on CREATIVE_AGENT_URL (default :5179).
// 上下文压缩与用量小环：分隔条、命令、/ 菜单、小环与浮层、立即压缩。
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { writeFile, rm } from 'node:fs/promises'
import { chromium } from 'playwright'

const noticeVersion = /egressNoticeVersion = '([^']+)'/.exec(
  readFileSync(new URL('../src/creative-canvas/editor/agentState.ts', import.meta.url), 'utf8'),
)[1]
const fixture = new URL('../.agent-context-qa.html', import.meta.url)
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
const url = `${process.env.CREATIVE_AGENT_URL || 'http://localhost:5179'}/.agent-context-qa.html`

const summary = '## 创作目标与当前方向\n秋日人像\n## 已定结论\n暖色调\n## 下一步\n出三版'
const runBase = {
  conversation_id: 'c',
  canvas_id: 'canvas',
  egress_consent_id: 'consent',
  model_key: 'flash',
  settlement_state: 'settled',
  limits_version: 'creative-agent-8',
  revision: '3',
  last_event_seq: '3',
  pruned_through_seq: '0',
  deadline_at: '2099-01-01T00:00:00Z',
  created_at: '2026-10-14T00:00:00Z',
  finished_at: '2026-10-14T00:01:00Z',
}
const runs = [
  { ...runBase, id: 'run1', kind: 'chat', trigger_message_id: 'm1', state: 'succeeded' },
  { ...runBase, id: 'run2', kind: 'compact', trigger_message_id: 'm3', state: 'succeeded' },
]
const block = (run, ordinal, type, state, payload) => ({
  id: `ccbk_${run}_${ordinal}`,
  run_id: run,
  ordinal: String(ordinal),
  type,
  state,
  revision: '2',
  payload_version: 1,
  hidden: false,
  payload,
  started_at: '2026-10-14T00:00:00Z',
  finished_at: '2026-10-14T00:00:10Z',
})
const blocks = {
  run1: [block('run1', 1, 'text', 'complete', { markdown: '第一轮的回答' })],
  run2: [
    block('run2', 1, 'compaction', 'complete', {
      trigger: 'manual',
      focus: '保留配色讨论',
      tokens_before: 90_000,
      tokens_after: 15_000,
      summary,
    }),
  ],
}
const message = (id, ordinal, run, role, blocks) => ({
  id,
  ordinal: String(ordinal),
  run_id: run,
  role,
  status: 'complete',
  schema_version: 2,
  revision: '1',
  created_at: '2026-10-14T00:00:00Z',
  body: { blocks },
})
const messages = [
  message('m1', 1, 'run1', 'user', [{ type: 'text', text: '第一轮：秋日人像' }]),
  message('m2', 2, 'run1', 'assistant', [{ type: 'text', text: '第一轮的回答' }]),
  message('m3', 3, 'run2', 'user', [{ type: 'command', command: 'compact', text: '保留配色讨论' }]),
]
const usage = {
  model_key: 'flash',
  window_tokens: 100_000,
  compact_at_tokens: 80_000,
  used_tokens: 62_000,
  estimated: false,
  parts: [
    { key: 'system', tokens: 6_000 },
    { key: 'tools', tokens: 10_000 },
    { key: 'summary', tokens: 4_000 },
    { key: 'messages', tokens: 42_000 },
  ],
  measured_at: '2026-10-14T00:02:00Z',
}

async function open(browser, { conversations = [{ id: 'c', title: '秋日人像' }], viewport } = {}) {
  const page = await browser.newPage({ viewport: viewport ?? { width: 1280, height: 860 } })
  page.on('pageerror', (error) => {
    throw error
  })
  const sent = []
  await page.route('**/api/v1/creative/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const json = (body, status = 200) => route.fulfill({ status, json: body })
    if (request.method() === 'POST') {
      const body = request.postDataJSON()
      sent.push({ path, body })
      if (path.endsWith('/egress-consents'))
        return json({ id: 'consent-new', conversation_id: 'c', revision: '1', purpose: 'creative_assistance', policy_version: noticeVersion, scope: {}, granted_at: '2026-10-14T00:03:00Z' }, 201)
      if (path.endsWith('/runs'))
        return json({ ...runBase, id: 'run3', kind: 'compact', trigger_message_id: 'm9', state: 'queued', finished_at: undefined }, 202)
      return json({})
    }
    if (path.endsWith('/agent/catalog'))
      return json({
        limits: { attachments: 10, image_preview_count: 8, auto_compact_percent: 80 },
        skills: [],
        models: [
          { model_key: 'flash', display_name: '文字模型', available: true, capability: { image_input: false }, context_window_tokens: 100_000 },
          { model_key: 'pro', display_name: '长窗口模型', available: true, capability: { image_input: false }, context_window_tokens: 200_000 },
        ],
      })
    if (path.endsWith('/conversations')) return json({ items: conversations })
    if (path.endsWith('/conversations/c/runs')) return json({ items: runs })
    if (path.endsWith('/messages'))
      return json({ items: messages, prev_cursor: '', conversation_revision: '3', context_usage: usage })
    const run = /\/agent-runs\/([^/]+)$/.exec(path)?.[1]
    if (run) {
      const known = runs.find((item) => item.id === run) ?? { ...runBase, id: run, kind: 'compact', trigger_message_id: 'm9', state: 'succeeded' }
      return json({ ...known, blocks: blocks[run] ?? [], snapshot_seq: '3' })
    }
    return json({ items: [] })
  })
  await page.goto(url)
  await page.getByRole('log', { name: '对话消息' }).waitFor()
  return { page, sent }
}

const browser = await chromium.launch({ headless: true })
try {
  // 有用量的会话：小环、浮层、分隔条和命令消息。
  {
    const { page } = await open(browser)
    const ring = page.locator('.cc-agent-ring')
    await ring.getByText('62%').waitFor()
    assert.equal(await ring.getAttribute('data-level'), 'warm', 'past 60% the ring takes the accent')
    await ring.hover()
    const panel = page.getByRole('dialog', { name: '上下文用量' })
    await panel.waitFor()
    const text = await panel.innerText()
    for (const want of ['上下文 6.2 万 / 10.0 万 · 62%', '系统指令', '工具定义', '交接摘要', '对话消息', '剩余 3.8 万', '再用约 1.8 万 后自动压缩'])
      assert.ok(text.includes(want), `panel shows ${want}:\n${text}`)
    assert.ok(!text.includes('总数是估算'), 'a measured total is not called an estimate')
    await page.screenshot({ path: '/tmp/creative-agent-context-ring.png' })
    await page.mouse.move(10, 10)
    await panel.waitFor({ state: 'detached' })

    // 分隔条：手动压缩、前后 token，点开看摘要。
    const divider = page.locator('.cc-agent-compaction button')
    await divider.getByText('上下文已压缩 · 手动 · 约 9.0 万 → 1.5 万 token').waitFor()
    await divider.click()
    await page.locator('.cc-agent-compaction-summary').getByText('秋日人像').waitFor()
    await page.locator('.cc-agent-compaction-summary').getByText('保留重点：保留配色讨论').waitFor()
    // 命令消息：/compact 标记加上关注点。
    await page.locator('.cc-agent-user .cc-agent-command').getByText('/compact').waitFor()
    await page.screenshot({ path: '/tmp/creative-agent-context-divider.png' })

    // 换成另一个模型：窗口按它算，总数改为估算。
    await page.getByRole('button', { name: '模型' }).click().catch(async () => {
      await page.locator('.cc-agent-model').click()
    })
    await page.getByText('长窗口模型').click()
    await ring.getByText('31%').waitFor()
    await ring.hover()
    await page.getByRole('dialog', { name: '上下文用量' }).getByText('总数是估算，下一次回复后更新为实际用量').waitFor()
    await page.mouse.move(10, 10)
    await page.close()
  }
  // / 菜单：补全命令，带关注点发送，只发 command 一段。
  {
    const { page, sent } = await open(browser)
    const editor = page.getByRole('textbox', { name: '创作问题' })
    await editor.click()
    await page.keyboard.type('/co')
    const menu = page.getByRole('list', { name: '命令' })
    await menu.getByText('/compact').waitFor()
    await page.screenshot({ path: '/tmp/creative-agent-context-menu.png' })
    await page.keyboard.press('Enter')
    await page.getByText('发送后会把较早的对话整理成交接摘要').waitFor()
    assert.equal(sent.length, 0, 'Enter completed the command instead of sending it')
    await page.keyboard.type('保留配色讨论')
    await page.keyboard.press('Enter')
    await page.waitForFunction(() => true)
    for (let i = 0; i < 40 && !sent.some((item) => item.path.endsWith('/runs')); i++) await page.waitForTimeout(100)
    const run = sent.find((item) => item.path.endsWith('/runs'))
    assert.ok(run, `a run was created: ${JSON.stringify(sent)}`)
    assert.deepEqual(run.body.payload.instruction.instruction_segments, [{ type: 'command', command: 'compact', text: '保留配色讨论' }])
    await page.close()
  }
  // 立即压缩：不动输入框里正在写的草稿。
  {
    const { page, sent } = await open(browser)
    const editor = page.getByRole('textbox', { name: '创作问题' })
    await editor.click()
    await page.keyboard.type('还在写的想法')
    await page.locator('.cc-agent-ring').hover()
    await page.getByRole('button', { name: '立即压缩' }).click()
    for (let i = 0; i < 40 && !sent.some((item) => item.path.endsWith('/runs')); i++) await page.waitForTimeout(100)
    const run = sent.find((item) => item.path.endsWith('/runs'))
    assert.deepEqual(run?.body.payload.instruction.instruction_segments, [{ type: 'command', command: 'compact' }])
    await page.waitForTimeout(300)
    assert.ok((await editor.innerText()).includes('还在写的想法'), 'the draft stayed')
    await page.close()
  }
  // 新对话：空环，不给预估值；立即压缩不可用。
  {
    const { page } = await open(browser, { conversations: [] })
    const ring = page.locator('.cc-agent-ring')
    await ring.waitFor()
    assert.equal(await ring.getAttribute('data-level'), 'empty')
    assert.equal(await page.locator('.cc-agent-ring-text').count(), 0, 'no percentage on an empty ring')
    await ring.hover()
    const panel = page.getByRole('dialog', { name: '上下文用量' })
    await panel.getByText('发送第一条消息后显示上下文用量').waitFor()
    assert.ok(await panel.getByRole('button', { name: '立即压缩' }).isDisabled())
    await page.screenshot({ path: '/tmp/creative-agent-context-empty.png' })
    await page.close()
  }
  // 窄屏：浮层不出界。
  {
    const { page } = await open(browser, { viewport: { width: 390, height: 780 } })
    await page.locator('.cc-agent-ring').click()
    const box = await page.getByRole('dialog', { name: '上下文用量' }).boundingBox()
    assert.ok(box && box.x >= 0 && box.x + box.width <= 390, `panel stays on screen: ${JSON.stringify(box)}`)
    await page.screenshot({ path: '/tmp/creative-agent-context-narrow.png' })
    await page.close()
  }
  console.log('agent context e2e passed')
} finally {
  await browser.close()
  await rm(fixture, { force: true })
}
