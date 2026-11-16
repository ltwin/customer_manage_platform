// Run with the frontend Vite server on CREATIVE_AGENT_URL (default :5179).
// 发送即显示：点发送就画出消息、清空输入框；读回真消息后不重复；
// 往已有会话里发只重读这个会话；被拒时原文回到输入框。
// 发送前说明按账号记在服务端：第一次发送弹出，取消不发、草稿留着；同意后发出；
// 已同意的账号换浏览器（本机什么都没存）不再弹；在「外发说明」撤回后再发会重新弹。
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { writeFile, rm } from 'node:fs/promises'
import { chromium } from 'playwright'

const policyVersion = /egressPolicyVersion = '([^']+)'/.exec(
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
const asset = (i, title) => ({
  id: `a${i}`, title, description: '', palette: [], rating: null, kind: 'image', revision: '1',
  content_id: `c${i}`, content_revision_id: `ccrv_${i}`, unavailable: false, is_favorite: false,
  created_at: '2026-10-15T00:00:00Z', deleted_at: null, purge_after: null, group_ids: [], tag_ids: [],
  content: { id: `ccrv_${i}`, kind: 'image', media: ['original', 'display'].map((role) => ({ role, blob_id: 'b', mime: 'image/svg+xml', byte_size: 1, width: 300, height: 300, duration_ms: null })) },
})
const assets = [asset(0, '秋日样片'), asset(1, '海边逆光'), asset(2, '棚拍布光')]
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

const consentState = (effective, library = true) => ({
  policy_version: policyVersion,
  effective,
  revision: effective ? '1' : '0',
  consent: effective ? { id: 'ccec_1', policy_version: policyVersion, library_access: library, granted_at: '2026-11-13T00:00:00Z' } : null,
})

async function open(browser, { refuse = false, agreed = true, revokedElsewhere = false } = {}) {
  const page = await browser.newPage({ viewport: { width: 1280, height: 860 } })
  page.on('pageerror', (error) => {
    throw error
  })
  const reads = { catalog: 0, conversations: 0, messages: 0 }
  const consent = { state: consentState(agreed), posts: [], runs: 0 }
  let accepted = false
  let release
  const gate = new Promise((resolve) => (release = resolve))
  const runs = [{ ...runBase, id: 'run1', trigger_message_id: 'm1', state: 'succeeded', finished_at: '2026-10-15T00:00:10Z' }]
  const messages = [message('m1', 1, 'run1', 'user', '第一轮'), message('m2', 2, 'run1', 'assistant', '第一轮的回答')]
  await page.route('**/qa-img/**', (route) =>
    route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="300" height="300"><rect width="300" height="300" fill="#a86"/></svg>' }),
  )
  await page.route('**/api/v1/creative/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const json = (body, status = 200) => route.fulfill({ status, json: body })
    if (path.endsWith('/media-access-tickets'))
      return json({ url: `${new URL(url).origin}/qa-img/${request.postDataJSON().content_revision_id}`, expires_at: '2099-01-01T00:00:00Z' })
    if (path.endsWith('/assets'))
      return json({ items: assets, next_cursor: '', total_count: assets.length, library_revision: '1', facets: { kinds: { image: 3, text: 0, link: 0, video: 0, audio: 0 }, tags: [] } })
    const one = /\/assets\/([^/]+)$/.exec(path)?.[1]
    if (one) return json(assets.find((a) => a.id === one))
    if (path.endsWith('/asset-groups')) return json({ items: [], asset_counts: {}, hierarchy_revision: '1' })
    if (path.endsWith('/egress-consent') && request.method() === 'GET') return json(consent.state)
    if (request.method() === 'POST') {
      if (path.endsWith('/egress-consent')) {
        const payload = request.postDataJSON().payload
        // 与服务端一致：按摄影师看到的状态版本有条件地写。
        if (payload.expected_revision !== consent.state.revision)
          return json({ error: { code: 'creative_revision_conflict', message: '内容已被其他窗口修改，本机这次修改未保存' } }, 409)
        consent.posts.push(payload)
        consent.state = { ...consentState(true, payload.library_access), revision: String(Number(consent.state.revision) + 1) }
        return json(consent.state)
      }
      if (path.endsWith('/egress-consent/revoke')) {
        consent.posts.push('revoke')
        consent.state = { ...consentState(false), revision: '2', revoked_at: '2026-11-13T01:00:00Z' }
        return json(consent.state)
      }
      if (path.endsWith('/runs')) {
        consent.runs++
        assert.equal('egress_consent_id' in request.postDataJSON().payload, false, 'a run carries no authorisation')
        // Held until the test has looked at the panel mid-send.
        await gate
        // 这个页面读过「已同意」、发出之前，另一台设备撤回了。
        if (revokedElsewhere && consent.runs === 1)
          consent.state = { ...consentState(false), revision: '2', revoked_at: '2026-11-13T01:00:00Z' }
        if (!consent.state.effective)
          return json({ error: { code: 'creative_egress_revoked', message: '外发同意已撤回，重新同意发送前说明后才能继续；已发出的内容无法召回' } }, 409)
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
  return { page, reads, release, consent }
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
  // 第一次发送弹出发送前说明：取消不发，草稿留着；同意后发出，记在服务端。
  {
    const { page, release, consent } = await open(browser, { agreed: false })
    release()
    const editor = page.getByRole('textbox', { name: '创作问题' })
    await editor.click()
    await page.keyboard.type('第一次发送')
    await page.keyboard.press('Enter')
    const notice = page.getByRole('alertdialog', { name: '发送前说明' })
    await notice.waitFor()
    await notice.getByText('自动开始回复处理结果').waitFor()
    const library = notice.getByRole('checkbox')
    assert.equal(await library.isChecked(), true, 'library search is on by default')
    await page.screenshot({ path: '/tmp/creative-agent-egress-notice.png' })
    await notice.getByRole('button', { name: '取消' }).click()
    await notice.waitFor({ state: 'detached' })
    assert.equal(consent.runs, 0, 'nothing was sent after cancelling')
    assert.deepEqual(consent.posts, [], 'no consent was recorded after cancelling')
    assert.ok((await editor.innerText()).includes('第一次发送'), 'the draft stays')
    await page.keyboard.press('Enter')
    await notice.waitFor()
    await notice.getByRole('button', { name: '同意并发送' }).click()
    // 发送中的气泡一闪而过，等受理后读回的那条。
    await page.getByRole('log', { name: '对话消息' }).getByText('第二轮：出三版').waitFor()
    assert.deepEqual(consent.posts, [{ policy_version: policyVersion, library_access: true, expected_revision: '0' }])
    assert.equal(consent.runs, 1)
    await page.close()
  }
  // 在「外发说明」里撤回：再发送时重新弹出说明。
  {
    const { page, release, consent } = await open(browser)
    release()
    await page.getByRole('combobox', { name: '对话操作' }).click()
    await page.getByRole('option', { name: /外发说明/ }).click()
    const settings = page.getByRole('dialog', { name: '外发说明' })
    await settings.getByText(/已同意/).waitFor()
    await page.screenshot({ path: '/tmp/creative-agent-egress-settings.png' })
    await settings.getByRole('button', { name: '撤回同意' }).click()
    const confirm = page.getByRole('alertdialog', { name: '撤回外发同意' })
    await confirm.getByText('已经发出的内容无法召回').waitFor()
    await confirm.getByRole('button', { name: '撤回同意' }).click()
    await settings.getByText(/已于 .* 撤回/).waitFor()
    assert.deepEqual(consent.posts, ['revoke'])
    await settings.getByRole('button', { name: '关闭' }).click()
    const editor = page.getByRole('textbox', { name: '创作问题' })
    await editor.click()
    await page.keyboard.type('撤回后再发')
    await page.keyboard.press('Enter')
    await page.getByRole('alertdialog', { name: '发送前说明' }).waitFor()
    assert.equal(consent.runs, 0)
    await page.close()
  }
  // 另一台设备撤回后，本页以为还同意着：发送被拒，重新弹出说明，同意后用同一份
  // 草稿自动再发一次，不用摄影师重打。
  {
    const { page, release, consent } = await open(browser, { revokedElsewhere: true })
    release()
    const editor = page.getByRole('textbox', { name: '创作问题' })
    await editor.click()
    await page.keyboard.type('别处撤回后发')
    await page.keyboard.press('Enter')
    const notice = page.getByRole('alertdialog', { name: '发送前说明' })
    await notice.waitFor()
    assert.equal(consent.runs, 1, 'the first send was refused by the server')
    await notice.getByRole('button', { name: '同意并发送' }).click()
    await page.getByRole('log', { name: '对话消息' }).getByText('第二轮：出三版').waitFor()
    assert.equal(consent.runs, 2, 'the same draft went again after agreeing')
    assert.deepEqual(consent.posts, [{ policy_version: policyVersion, library_access: true, expected_revision: '2' }])
    assert.equal((await editor.innerText()).trim(), '', 'the resent draft left the composer')
    await page.close()
  }
  // 面板打开后另一台设备撤回：切换素材库开关不能把同意恢复回来，显示最新状态。
  {
    const { page, release, consent } = await open(browser)
    release()
    await page.getByRole('combobox', { name: '对话操作' }).click()
    await page.getByRole('option', { name: /外发说明/ }).click()
    const settings = page.getByRole('dialog', { name: '外发说明' })
    await settings.getByText(/已同意/).waitFor()
    consent.state = { ...consentState(false), revision: '2', revoked_at: '2026-11-13T01:00:00Z' }
    await settings.getByRole('checkbox').click()
    await settings.getByText(/已于 .* 撤回/).waitFor()
    await settings.getByText('外发同意刚在其他窗口或设备上改过，已显示最新状态').waitFor()
    assert.deepEqual(consent.posts, [], 'a stale switch recorded nothing')
    assert.equal(consent.state.effective, false, 'the withdrawal stands')
    await page.close()
  }
  // 「+」一处收齐：上传、资产库、画布引用和命令，不再另有回形针。
  {
    const { page, release } = await open(browser)
    release()
    assert.equal(await page.getByRole('button', { name: '上传参考图' }).count(), 0)
    const menu = () => page.getByRole('listbox', { name: '添加', exact: true })
    await page.getByRole('combobox', { name: '添加', exact: true }).click()
    const names = await menu().getByRole('option').evaluateAll((items) => items.map((item) => item.querySelector('.cc-picker-label')?.textContent))
    assert.deepEqual(names, ['上传图片', '从资产库选', '引用画布节点', '压缩上下文'])
    assert.deepEqual(await menu().locator('.cc-picker-group').allTextContents(), ['添加', '命令'])
    await page.screenshot({ path: '/tmp/creative-agent-add-menu.png' })
    const chooser = page.waitForEvent('filechooser')
    await menu().getByRole('option', { name: /上传图片/ }).click()
    await chooser
    await page.getByRole('combobox', { name: '添加', exact: true }).click()
    await menu().getByRole('option', { name: /从资产库选/ }).click()
    // 勾选不挤压浏览区，取消勾选不留痕；放大按钮打开只读的快速查看，不改勾选。
    const library = page.getByRole('dialog', { name: '引用资产库素材' })
    const tile = library.getByRole('button', { name: '海边逆光', exact: true })
    await tile.locator('img').waitFor()
    const board = library.locator('.al-board')
    const width = (await board.boundingBox()).width
    await tile.click()
    assert.equal(await tile.getAttribute('aria-pressed'), 'true')
    assert.equal((await board.boundingBox()).width, width, 'picking keeps the layout')
    await library.getByRole('button', { name: '引用 1 项' }).waitFor()
    await tile.click()
    assert.equal(await tile.getAttribute('aria-pressed'), 'false')
    assert.equal((await board.boundingBox()).width, width)
    await library.getByRole('button', { name: '查看：秋日样片' }).click()
    const quick = page.getByRole('dialog', { name: '快速查看：秋日样片' })
    await quick.locator('img').first().waitFor()
    await page.screenshot({ path: '/tmp/creative-agent-library-quicklook.png' })
    assert.equal(await library.locator('.al-tile[aria-pressed="true"]').count(), 0, 'looking does not pick')
    // The quick look offers only the picker's own action, not library edits.
    assert.equal(await quick.getByRole('button', { name: /移到回收站|放入项目/ }).count(), 0)
    await quick.getByRole('button', { name: '选用', exact: true }).click()
    await quick.getByRole('button', { name: '取消选用', exact: true }).waitFor()
    assert.equal(await library.getByRole('button', { name: '秋日样片', exact: true }).getAttribute('aria-pressed'), 'true')
    await page.keyboard.press('Escape')
    await quick.waitFor({ state: 'detached' })
    assert.ok(await library.isVisible(), 'Escape closes only the quick look')
    await library.getByRole('button', { name: '引用 1 项' }).waitFor()
    await library.getByRole('button', { name: '关闭', exact: true }).click()
    await page.getByRole('combobox', { name: '添加', exact: true }).click()
    await menu().getByRole('option', { name: /引用画布节点/ }).click()
    await page.getByText('先连接画布上的节点，或从资产库添加参考').waitFor()
    await page.close()
  }
  console.log('agent send e2e passed')
} finally {
  await browser.close()
  await rm(fixture, { force: true })
}
