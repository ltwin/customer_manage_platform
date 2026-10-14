// Run with the frontend Vite server on CREATIVE_AGENT_URL (default :5179).
// 长会话分段读取：打开即落在最新一轮的提问上，往上滚整轮接上更早的，位置不跳。
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { writeFile, rm } from 'node:fs/promises'
import { chromium } from 'playwright'

const noticeVersion = /egressNoticeVersion = '([^']+)'/.exec(readFileSync(new URL('../src/creative-canvas/editor/agentState.ts', import.meta.url), 'utf8'))[1]
const fixture = new URL('../.agent-history-qa.html', import.meta.url)
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
const url = `${process.env.CREATIVE_AGENT_URL || 'http://localhost:5179'}/.agent-history-qa.html`

// 每轮：提问、若干次工具调用（带调用的 assistant + 工具结果）、最后的回答。
function conversation(turns, calls) {
  const all = []
  const push = (run, role, text) =>
    all.push({ id: `m${all.length + 1}`, ordinal: String(all.length + 1), run_id: run, role, status: 'complete', schema_version: 2, revision: '1', created_at: '2026-10-14T00:00:00Z', body: { blocks: text ? [{ type: 'text', text }] : [] } })
  for (let turn = 0; turn < turns; turn++) {
    const run = `run${turn}`
    push(run, 'user', `第 ${turn + 1} 轮提问`)
    for (let call = 0; call < calls; call++) {
      push(run, 'assistant', '')
      push(run, 'tool', `{"result":"${'x'.repeat(200)}"}`)
    }
    push(run, 'assistant', `第 ${turn + 1} 轮回答\n\n${'这一轮的说明。'.repeat(20)}`)
  }
  return all
}

// hold(before) 返回 'fail' 让这次读取失败，返回 Promise 则等它放行。
async function open(browser, messages, hold = () => undefined) {
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
  page.on('pageerror', (error) => { throw error })
  const reads = []
  await page.route('**/api/v1/creative/**', async (route) => {
    const at = new URL(route.request().url())
    const json = (body) => route.fulfill({ json: body })
    if (at.pathname.endsWith('/agent/catalog')) return json({ limits: { attachments: 10, image_preview_count: 8 }, skills: [], models: [{ model_key: 'model', display_name: '文字模型', available: true, capability: { image_input: false } }] })
    if (at.pathname.endsWith('/conversations')) return json({ items: [{ id: 'long', title: '长会话', title_source: 'generated', updated_at: '2026-10-14T00:00:00Z' }] })
    if (at.pathname.endsWith('/runs')) return json({ items: [] })
    if (at.pathname.endsWith('/messages')) {
      const before = at.searchParams.get('before_ordinal') ?? ''
      const limit = Number(at.searchParams.get('limit') ?? 30)
      const gate = hold(before)
      if (gate === 'fail') return route.fulfill({ status: 500, json: { error: { code: 'internal', message: 'boom' } } })
      await gate
      reads.push({ before, limit })
      const older = messages.filter((m) => !before || Number(m.ordinal) < Number(before))
      const items = older.slice(-limit)
      return json({ items, prev_cursor: older.length > limit ? items[0].ordinal : '', conversation_revision: '1' })
    }
    return json({ items: [] })
  })
  await page.goto(url)
  const log = page.getByRole('log', { name: '对话消息' })
  await log.waitFor()
  await page.waitForFunction(() => document.querySelector('[role=log]')?.getAttribute('aria-busy') === 'false')
  return { page, log, reads }
}

const settle = (page) => page.waitForTimeout(250)
const questions = (page) => page.locator('.cc-agent-history').getByText(/^第 \d+ 轮提问$/).allTextContents()

const browser = await chromium.launch({ headless: true })
try {
  // 线上的形态：一轮 37 次工具调用，提问在最前面，默认的一页读不到它。
  {
    const { page, reads } = await open(browser, conversation(1, 37))
    await page.getByText('第 1 轮提问').waitFor()
    await page.getByText('第 1 轮回答').waitFor()
    assert.deepEqual(reads, [{ before: '', limit: 100 }], 'one page of the API maximum reads the whole turn')
    assert.equal(await page.locator('.cc-agent-older').count(), 0, 'nothing older to read')
    await page.close()
  }
  // 很长的会话：打开时贴着底部，往上滚一路接上，直到会话开头。
  {
    const turns = 30
    const { page, log, reads } = await open(browser, conversation(turns, 12))
    await page.getByText(`第 ${turns} 轮回答`).waitFor()
    await settle(page)
    const pinned = await log.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight)
    assert.ok(pinned < 2, `opens at the latest answer (${pinned}px from the bottom)`)
    const opened = reads.length
    assert.equal(reads[0].limit, 100)
    assert.ok(opened <= 2, `opening a long conversation does not page through history (${opened} reads)`)
    // 往上滚到顶：正在看的那一轮不能跳走。
    for (let round = 0; round < 40 && (await page.locator('.cc-agent-older').count()); round++) {
      const anchor = page.getByText((await questions(page))[0], { exact: true })
      const top = await anchor.evaluate((el) => el.getBoundingClientRect().top)
      await log.evaluate((el) => { el.scrollTop = 0 })
      await page.waitForFunction((count) => document.querySelectorAll('.cc-agent-history-content > *').length !== count || !document.querySelector('.cc-agent-older'), await page.locator('.cc-agent-history-content > *').count())
      await settle(page)
      const moved = await anchor.evaluate((el) => el.getBoundingClientRect().top)
      const scrolled = await log.evaluate((el) => el.scrollTop)
      assert.ok(Math.abs(moved - top) > 0 || scrolled > 0, 'older turns were prepended above the reader')
      assert.ok(scrolled > 0 || !(await page.locator('.cc-agent-older').count()), 'the reader is not left stranded at the very top')
    }
    const seen = await questions(page)
    assert.equal(seen.length, turns, 'every turn is shown once')
    assert.deepEqual(seen, Array.from({ length: turns }, (_, i) => `第 ${i + 1} 轮提问`))
    assert.equal(new Set(reads.map((r) => r.before)).size, reads.length, 'no page is read twice')
    await page.screenshot({ path: '/tmp/creative-agent-history-top.png' })
    await page.close()
  }
  // 接上更早的一轮时，正在读的那一行留在原处。
  {
    let release
    const waiting = new Promise((resolve) => { release = resolve })
    let held = false
    const { page, log } = await open(browser, conversation(30, 12), (before) => {
      if (!before || held) return undefined
      held = true
      return waiting
    })
    await page.getByText('第 30 轮回答').waitFor()
    await settle(page)
    const shown = (await questions(page)).length
    const target = page.getByText((await questions(page))[0], { exact: true })
    await log.evaluate((el) => { el.scrollTop = 150 })
    await page.waitForFunction(() => document.querySelector('.cc-agent-older'))
    await settle(page)
    // 读取中那一行的样子留一张图，再回到正在读的位置。
    await log.evaluate((el) => { el.scrollTop = 0 })
    await page.screenshot({ path: '/tmp/creative-agent-history-loading.png' })
    await log.evaluate((el) => { el.scrollTop = 150 })
    await settle(page)
    const before = await target.evaluate((el) => el.getBoundingClientRect().top)
    release()
    await page.waitForFunction((count) => document.querySelectorAll('.cc-agent-history-content').length && [...document.querySelectorAll('.cc-agent-history *')].filter((el) => /^第 \d+ 轮提问$/.test(el.textContent ?? '') && el.children.length === 0).length > count, shown)
    await settle(page)
    const after = await target.evaluate((el) => el.getBoundingClientRect().top)
    assert.ok(Math.abs(after - before) < 2, `the line being read stays put (${before} -> ${after})`)
    await page.close()
  }
  // 读更早的失败：说清楚、能重读，下面的对话不受影响。
  {
    let failed = false
    const { page, log } = await open(browser, conversation(30, 12), (before) => {
      if (!before || failed) return undefined
      failed = true
      return 'fail'
    })
    await page.getByText('第 30 轮回答').waitFor()
    await settle(page)
    const shown = (await questions(page)).length
    await log.evaluate((el) => { el.scrollTop = 0 })
    await page.getByText('更早的对话没有读出来。').waitFor()
    assert.equal(await page.getByText('第 30 轮回答').count(), 1, 'the thread below stays')
    await page.getByRole('button', { name: '重新读取' }).click()
    await page.waitForFunction(() => !document.body.textContent.includes('更早的对话没有读出来'))
    await settle(page)
    assert.ok((await questions(page)).length > shown, 'the retry joined the earlier turns')
    await page.close()
  }
  console.log('PASS: a tool-heavy turn opens on its question; long conversations open at the bottom with one read, page in whole turns on scroll without duplicates or jumps, and an older read that fails can be retried')
} finally {
  await browser.close()
  await rm(fixture, { force: true })
}
