// Run with the frontend Vite server on CREATIVE_AGENT_URL (default :5179).
// Exercises the real component with delayed HTTP replies; never calls a vendor.
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { writeFile, rm } from 'node:fs/promises'
import { chromium } from 'playwright'
// 默认已看过首次外发说明；地址带 ?notice=0 时从没看过开始（V6-029）。
const noticeVersion = /egressNoticeVersion = '([^']+)'/.exec(readFileSync(new URL('../src/creative-canvas/editor/agentState.ts', import.meta.url), 'utf8'))[1]
const fixture = new URL('../.agent-command-qa.html', import.meta.url)
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
if (!location.search.includes('notice=0')) localStorage.setItem('creative-egress-notice:qa-account', '${noticeVersion}');
createRoot(document.getElementById('root')).render(React.createElement(AgentPanel,{account:'qa-account',canvasID:'canvas',canvas:{nodes:[{id:'node-image',metadata:{title:'主图'},data:{content_revision_id:'ccrv_image'},content:{id:'ccrv_image',kind:'image',payload:{},media:[],truncated:false}}]}}));
</script></body></html>`,
)
const browser = await chromium.launch({ headless: true })
// 画布下拉是 combobox + 浮在工作区顶层的 listbox，不是原生 select。
async function pick(page, label, option) {
  await page.getByRole('combobox', { name: label, exact: true }).click()
  await page.getByRole('listbox', { name: label, exact: true }).getByRole('option', { name: option }).click()
}
const url = `${process.env.CREATIVE_AGENT_URL || 'http://localhost:5179'}/.agent-command-qa.html`
const key = 'creative-agent-pending:qa-account:canvas'
const finishedRun = {
  id: 'run',
  conversation_id: 'conversation',
  canvas_id: 'canvas',
  trigger_message_id: 'message',
  egress_consent_id: 'consent',
  model_key: 'model',
  state: 'succeeded',
  settlement_state: 'settled',
  limits_version: '1',
  revision: '1',
  last_event_seq: '0',
  pruned_through_seq: '0',
  deadline_at: '2026-10-09T01:00:00Z',
  created_at: '2026-10-09T00:00:00Z',
  finished_at: '2026-10-09T00:00:10Z',
  blocks: [],
  snapshot_seq: '0',
}
const delay = () => {
  let release
  const promise = new Promise((resolve) => {
    release = resolve
  })
  return { promise, release }
}
try {
  for (const order of ['old-first', 'new-first']) {
    const page = await browser.newPage()
    page.on('pageerror', (error) => console.error('page error:', error.message))
    const gates = [delay(), delay()]
    let grants = 0
    const posts = []
    await page.route('**/api/v1/creative/**', async (route) => {
      const req = route.request(),
        path = new URL(req.url()).pathname
      const json = (body) => route.fulfill({ json: body })
      if (req.method() === 'POST') {
        const body = req.postDataJSON()
        if (path.endsWith('/egress-consents')) {
          assert.equal(body.payload.mode, 'selected_revisions')
          assert.deepEqual(body.payload.selected_revision_ids, [])
          const index = grants++
          await gates[index].promise
          return json({ id: 'consent', approved_revisions: 0 })
        }
        if (path.endsWith('/runs')) {
          posts.push(body)
          return json({
            id: 'run',
            trigger_message_id: 'message',
            conversation_id: 'conversation',
            state: 'succeeded',
            settlement_state: 'settled',
          })
        }
      }
      if (path.endsWith('/agent-runs/run')) return json(finishedRun)
      if (path.endsWith('/agent/catalog'))
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
          skills: [
            {
              skill_id: 'skill',
              skill_version_id: 'version',
              display_name: 'Test skill',
              available: true,
            },
          ],
        })
      if (path.endsWith('/conversations'))
        return json({ items: [{ id: 'conversation', title: 'First' }] })
      return json({ items: [] })
    })
    await page.goto(url)
    await page.getByRole('button', { name: '打开创作助手' }).click()
    assert.equal(await page.locator('.cc-agent-title').textContent(), 'First', 'the header names the current conversation')
    assert.equal(await page.getByRole('listbox', { name: '历史对话' }).count(), 0, 'history starts closed')
    await page.getByRole('combobox', { name: '历史对话' }).click()
    await page.getByRole('listbox', { name: '历史对话' }).waitFor()
    await page.getByLabel('创作问题').click()
    assert.equal(await page.getByRole('listbox', { name: '历史对话' }).count(), 0, 'outside click closes the history list')
    await page.getByLabel('创作问题').fill('保留原始问题')
    const metrics=await page.getByRole('complementary',{name:'创作助手'}).evaluate(el=>{const s=getComputedStyle(el),b=el.querySelector('.cc-agent-send'),r=b.getBoundingClientRect();return {width:el.getBoundingClientRect().width,background:s.backgroundColor,send:[r.width,r.height,getComputedStyle(b).borderRadius]}})
    assert.deepEqual(metrics,{width:380,background:'rgb(27, 27, 29)',send:[30,30,'50%']})
    if(order==='old-first') await page.screenshot({path:'/tmp/creative-v6-agent-desktop.png'})
    await pick(page, 'Skill', 'Test skill')
    assert.equal(await page.getByRole('checkbox').count(), 0, 'sending needs no consent tick')
    await page.getByLabel('创作问题').press('Enter')
    await page.waitForFunction(
      (k) => JSON.parse(sessionStorage.getItem(k))?.steps?.length === 2,
      key,
    )
    await page.getByRole('button', { name: '收起创作助手' }).click()
    await page.getByRole('button', { name: '打开创作助手' }).click()
    await page.getByRole('button', { name: '重新发送' }).click()
    for (let i = 0; grants < 2 && i < 500; i++)
      await new Promise((resolve) => setTimeout(resolve, 10))
    assert.equal(grants, 2)
    gates[order === 'old-first' ? 0 : 1].release()
    if (order === 'old-first') {
      await new Promise((resolve) => setTimeout(resolve, 100))
      assert.equal(posts.length, 0, 'unmounted owner advanced the chain')
    } else
      await page.waitForFunction((k) => sessionStorage.getItem(k) === null, key)
    gates[order === 'old-first' ? 1 : 0].release()
    await page.waitForFunction((k) => sessionStorage.getItem(k) === null, key)
    await new Promise((resolve) => setTimeout(resolve, 100))
    assert.equal(posts.length, 1, 'same grant created multiple runs')
    await page.close()
  }
  // A dropped response, refresh, and definitive rejection must retain the editable draft.
  const page = await browser.newPage()
    page.on('pageerror', (error) => console.error('page error:', error.message))
  let runPosts = 0
  await page.route('**/api/v1/creative/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    const json = (body) => route.fulfill({ json: body })
    if (route.request().method() === 'POST') {
      if (path.endsWith('/egress-consents'))
        return json({ id: 'consent', approved_revisions: 0 })
      if (path.endsWith('/runs')) {
        if (++runPosts === 1) return route.abort('failed')
        return route.fulfill({
          status: 422,
          json: {
            error: {
              code: 'creative_model_unavailable',
              message: 'Model unavailable',
            },
          },
        })
      }
    }
    if (path.endsWith('/agent/catalog'))
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
        skills: [
          {
            skill_id: 'skill',
            skill_version_id: 'version',
            display_name: 'Test skill',
            available: true,
          },
        ],
      })
    if (path.endsWith('/conversations'))
      return json({
        items: [
          { id: 'conversation', title: 'First' },
          { id: 'other', title: 'Other' },
        ],
      })
    return json({ items: [] })
  })
  await page.goto(url)
  await page.getByRole('button', { name: '打开创作助手' }).click()
  await pick(page, '历史对话', 'Other')
  await page.getByLabel('创作问题').fill('刷新后仍然保留的原文')
  await pick(page, 'Skill', 'Test skill')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await page.getByRole('alert').waitFor()
  await page.reload()
  await page.getByRole('button', { name: '打开创作助手' }).click()
  await page.getByRole('button', { name: '重新发送' }).click()
  await page.waitForFunction((k) => sessionStorage.getItem(k) === null, key)
  assert.equal(
    await page.getByLabel('创作问题').textContent(),
    '刷新后仍然保留的原文',
  )
  assert.equal(await page.getByLabel('创作问题').isEnabled(), true)
  assert.equal(await page.locator('.cc-agent-title').textContent(), 'Other')
  assert.equal(
    await page.getByRole('combobox', { name: '模型', exact: true }).textContent(),
    'Test model',
  )
  assert.equal(
    await page.getByRole('combobox', { name: 'Skill', exact: true }).textContent(),
    'Test skill',
  )
  assert.equal(runPosts, 2)
  await page.setViewportSize({width:390,height:844})
  await page.screenshot({path:'/tmp/creative-v6-agent-mobile.png'})
  const panelBox=await page.getByRole('complementary',{name:'创作助手'}).boundingBox()
  assert(panelBox.x>=0 && panelBox.x+panelBox.width<=390)
  const compose=await page.locator('.cc-agent-box').boundingBox()
  const footer=await page.locator('.cc-agent-composer-foot').boundingBox()
  assert(footer.x>=compose.x && footer.x+footer.width<=compose.x+compose.width)
  await page.close()
  const materialPage = await browser.newPage()
  const submissions = []
  const approvals = []
  let failOnce = true
  materialPage.on('pageerror', (error) => { throw error })
  await materialPage.route('**/api/v1/creative/**', async (route) => {
    const req = route.request(), path = new URL(req.url()).pathname
    const json = (body) => route.fulfill({json: body})
    if (req.method() === 'POST') {
      if (path.endsWith('/egress-consents')) { approvals.push(req.postDataJSON()); return json({id:'consent',approved_revisions:0}) }
      if (path.endsWith('/runs')) {
        submissions.push(req.postDataJSON())
        if (failOnce) { failOnce = false; return route.abort('failed') }
        return json({id:'run',trigger_message_id:'message',conversation_id:'conversation',state:'succeeded',settlement_state:'settled'})
      }
    }
    if(path.endsWith('/agent-runs/run')) return json(finishedRun)
    if(path.endsWith('/agent/catalog')) return json({limits:{attachments:10,image_preview_count:8},skills:[],models:[{model_key:'text',display_name:'文字模型',available:true,capability:{image_input:false}},{model_key:'vision',display_name:'视觉模型',available:true,capability:{image_input:true}}]})
    if(path.endsWith('/conversations')) return json({items:[{id:'conversation',title:'素材讨论'}]})
    if(path.endsWith('/assets')) return json({items:[{id:'asset',title:'拍摄方案',content_revision_id:'ccrv_text',content:{id:'ccrv_text',kind:'text',payload:{body:'方案'},truncated:false}}],next_cursor:''})
    // 会话上次用的是文字模型；刷新后待确认草稿里的视觉模型必须胜出。
    if(req.method()==='GET' && path.endsWith('/conversations/conversation/runs')) return json({items:[{...finishedRun,model_key:'text'}]})
    return json({items:[]})
  })
  await materialPage.goto(url)
  await materialPage.getByRole('button',{name:'打开创作助手'}).click()
  await materialPage.getByLabel('创作问题').fill('参考 @')
  await materialPage.getByRole('option',{name:/主图/}).click()
  await materialPage.getByText('当前模型不支持图片理解，请切换视觉模型或移除图片引用。').waitFor()
  assert(await materialPage.getByRole('button',{name:'发送',exact:true}).isDisabled())
  await pick(materialPage, '模型', '视觉模型')
  await materialPage.getByRole('button',{name:'引用素材',exact:true}).click()
  await materialPage.getByRole('dialog',{name:'引用素材库'}).getByRole('button',{name:/拍摄方案/}).click()
  assert.equal(await materialPage.locator('.cc-agent-box [data-ref]').count(),2)
  await materialPage.getByRole('button',{name:'发送',exact:true}).click()
  await materialPage.getByRole('alert').waitFor()
  await materialPage.reload()
  await materialPage.getByRole('button',{name:'打开创作助手'}).click()
  await materialPage.locator('.cc-agent-box [data-ref]').nth(1).waitFor()
  assert.equal(await materialPage.locator('.cc-agent-box [data-ref]').count(),2,'reference chips survive refresh')
  await new Promise((resolve) => setTimeout(resolve, 100))
  assert.equal(await materialPage.getByRole('combobox',{name:'模型',exact:true}).textContent(),'视觉模型','a pending draft keeps the model it was written with')
  await materialPage.getByRole('button',{name:'重新发送'}).click()
  await materialPage.waitForFunction((k)=>sessionStorage.getItem(k)===null,key)
  assert.equal(submissions.length,2)
  assert.deepEqual(submissions[0],submissions[1],'retry keeps original reference payload')
  assert.deepEqual(approvals[0].payload.selected_revision_ids,['ccrv_image','ccrv_text'])
  assert(approvals[0].payload.data_classes.includes('image_preview'))
  assert(approvals[0].payload.data_classes.includes('canvas_metadata'))
  assert.deepEqual(submissions[0].payload.instruction.instruction_segments.filter((s)=>s.type==='content_ref').map((s)=>s.content_revision_id),['ccrv_image','ccrv_text'])
  await materialPage.close()
  // 回到一个会话沿用它最近一次运行的模型；已下架的不沿用。
  const recallPage = await browser.newPage()
  recallPage.on('pageerror', (error) => { throw error })
  const history = {
    a: [{ ...finishedRun, id: 'a1', conversation_id: 'a', model_key: 'vision' }],
    b: [
      { ...finishedRun, id: 'b1', conversation_id: 'b', model_key: 'vision', created_at: '2026-10-09T01:00:00Z' },
      { ...finishedRun, id: 'b2', conversation_id: 'b', model_key: 'text', created_at: '2026-10-09T02:00:00Z' },
    ],
    c: [{ ...finishedRun, id: 'c1', conversation_id: 'c', model_key: 'gone' }],
  }
  await recallPage.route('**/api/v1/creative/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    const json = (body) => route.fulfill({ json: body })
    const runs = path.match(/\/conversations\/(\w+)\/runs$/)
    if (runs) return json({ items: history[runs[1]] ?? [] })
    const run = path.match(/\/agent-runs\/(\w+)$/)
    if (run) return json(Object.values(history).flat().find((item) => item.id === run[1]))
    if (path.endsWith('/agent/catalog')) return json({limits:{attachments:10,image_preview_count:8},skills:[],models:[{model_key:'text',display_name:'文字模型',available:true,capability:{image_input:false}},{model_key:'vision',display_name:'视觉模型',available:true,capability:{image_input:true}}]})
    if (path.endsWith('/conversations')) return json({ items: [{ id: 'a', title: '会话甲' }, { id: 'b', title: '会话乙' }, { id: 'c', title: '会话丙' }] })
    return json({ items: [] })
  })
  const chosen = () => recallPage.getByRole('combobox', { name: '模型', exact: true }).textContent()
  const settle = () => new Promise((resolve) => setTimeout(resolve, 150))
  await recallPage.goto(url)
  await recallPage.getByRole('button', { name: '打开创作助手' }).click()
  await settle()
  assert.equal(await chosen(), '视觉模型', 'the conversation opened on arrival recalls its model')
  await pick(recallPage, '历史对话', '会话乙')
  await settle()
  assert.equal(await chosen(), '文字模型', 'the most recent run decides, not the list order')
  await pick(recallPage, '历史对话', '会话丙')
  await settle()
  assert.equal(await chosen(), '文字模型', 'a withdrawn model is not recalled')
  await pick(recallPage, '模型', '视觉模型')
  await pick(recallPage, '历史对话', '会话乙')
  await settle()
  assert.equal(await chosen(), '文字模型')
  await recallPage.close()
  // 读取中：列表、消息、运行详情依次到达，每一步都不能先露出空状态或旧答复。
  const loadPage = await browser.newPage()
  loadPage.on('pageerror', (error) => { throw error })
  const list = delay(), thread = delay(), snapshot = delay()
  let failThread = true
  await loadPage.route('**/api/v1/creative/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    const json = (body) => route.fulfill({ json: body })
    if (path.endsWith('/agent/catalog')) return json({limits:{attachments:10,image_preview_count:8},skills:[],models:[{model_key:'model',display_name:'文字模型',available:true,capability:{image_input:false}}]})
    if (path.endsWith('/conversations')) {
      await list.promise
      return json({ items: [{ id: 'conversation', title: '雪山婚纱' }, { id: 'broken', title: '读不出的对话' }] })
    }
    if (path.startsWith('/api/v1/creative/conversations/broken/') && failThread)
      return route.fulfill({ status: 500, json: { error: { code: 'internal', message: '对话暂时无法更新' } } })
    if (path.endsWith('/runs')) {
      await thread.promise
      return json({ items: path.includes('/conversation/') ? [finishedRun] : [] })
    }
    if (path.endsWith('/messages')) {
      await thread.promise
      return json({ items: path.includes('/conversation/') ? [
        { id: 'message', role: 'user', ordinal: '1', status: 'complete', run_id: null, body: { blocks: [{ type: 'text', text: '布光怎么安排' }] } },
        { id: 'answer', role: 'assistant', ordinal: '2', status: 'complete', run_id: 'run', body: { blocks: [{ type: 'text', text: '侧逆光为主' }] } },
      ] : [] })
    }
    if (path.endsWith('/agent-runs/run')) {
      await snapshot.promise
      return json(finishedRun)
    }
    return json({ items: [] })
  })
  const log = loadPage.getByRole('log', { name: '对话消息' })
  const welcome = loadPage.getByText('让创作助手陪你把想法落到画布上')
  await loadPage.goto(url)
  await loadPage.getByRole('button', { name: '打开创作助手' }).click()
  await loadPage.locator('.cc-agent-loading').waitFor()
  assert.equal(await log.getAttribute('aria-busy'), 'true')
  assert.equal(await welcome.count(), 0, 'no welcome before the list is read')
  assert.equal(await loadPage.locator('.cc-agent-title').textContent(), '正在读取', 'no placeholder title before the list is read')
  await loadPage.screenshot({ path: '/tmp/creative-v6-agent-loading.png', animations: 'disabled' })
  list.release()
  await loadPage.locator('.cc-agent-title', { hasText: '雪山婚纱' }).waitFor()
  assert.equal(await loadPage.locator('.cc-agent-loading').count(), 1, 'still reading the opened conversation')
  assert.equal(await welcome.count(), 0, 'no welcome while the thread is read')
  thread.release()
  await loadPage.getByText('布光怎么安排').waitFor()
  assert.equal(await loadPage.locator('.cc-agent-loading').count(), 0)
  assert.equal(await loadPage.locator('.cc-agent-loading-answer').count(), 1, 'the answer is outlined until its run is read')
  assert.equal(await loadPage.getByText('侧逆光为主').count(), 0, 'the plain answer waits for the run')
  snapshot.release()
  await loadPage.getByText('侧逆光为主').waitFor()
  assert.equal(await loadPage.locator('.cc-agent-loading-answer').count(), 0)
  assert.equal(await log.getAttribute('aria-busy'), 'false')
  await pick(loadPage, '历史对话', '读不出的对话')
  await loadPage.getByText('对话没有读出来。').waitFor()
  assert.equal(await welcome.count(), 0, 'a failed read is not an empty conversation')
  failThread = false
  await loadPage.getByRole('button', { name: '重新读取' }).click()
  await welcome.waitFor()
  assert.equal(await loadPage.getByText('对话没有读出来。').count(), 0)
  await loadPage.close()
  // 改名、归档、查看已归档、恢复：服务端状态在这里模拟，面板只看回执与列表。
  const managePage = await browser.newPage()
  managePage.on('pageerror', (error) => { throw error })
  const store = [
    { id: 'a', title: '会话甲', title_source: 'generated', archived: false, updated_at: '2026-10-09T02:00:00Z' },
    { id: 'b', title: '会话乙', title_source: 'generated', archived: false, updated_at: '2026-10-09T01:00:00Z' },
  ]
  const commands = []
  await managePage.route('**/api/v1/creative/**', async (route) => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname
    const json = (body) => route.fulfill({ json: body })
    const command = path.match(/\/conversations\/(\w+)\/(rename|archive|restore)$/)
    if (req.method() === 'POST' && command) {
      const body = req.postDataJSON()
      commands.push([command[2], command[1], body.payload])
      const item = store.find((c) => c.id === command[1])
      if (body.payload.title === '冲突') return route.fulfill({ status: 409, json: { error: { code: 'creative_conversation_archived', message: '这个对话已归档，恢复后才能继续' } } })
      if (command[2] === 'rename') Object.assign(item, { title: body.payload.title, title_source: 'manual' })
      else item.archived = command[2] === 'archive'
      return json(item)
    }
    if (path.endsWith('/agent/catalog')) return json({limits:{attachments:10,image_preview_count:8},skills:[],models:[{model_key:'text',display_name:'文字模型',available:true,capability:{image_input:false}}]})
    if (path.endsWith('/conversations')) {
      const archived = url.searchParams.get('archived') === 'true'
      return json({ items: store.filter((c) => c.archived === archived) })
    }
    return json({ items: [] })
  })
  const title = () => managePage.locator('.cc-agent-title').textContent()
  // 菜单项名字互相包含（归档 / 查看已归档），按全名点。
  const menu = async (option) => {
    await managePage.getByRole('combobox', { name: '对话操作', exact: true }).click()
    await managePage.getByRole('listbox', { name: '对话操作', exact: true }).getByRole('option', { name: option, exact: true }).click()
  }
  const options = async (label) => {
    await managePage.getByRole('combobox', { name: label, exact: true }).click()
    const names = await managePage.getByRole('listbox', { name: label, exact: true }).getByRole('option').allTextContents()
    await managePage.keyboard.press('Escape')
    return names
  }
  await managePage.goto(url)
  await managePage.getByRole('button', { name: '打开创作助手' }).click()
  await managePage.locator('.cc-agent-title', { hasText: '会话甲' }).waitFor()
  await managePage.getByRole('combobox', { name: '对话操作', exact: true }).click()
  await managePage.getByRole('listbox', { name: '对话操作', exact: true }).waitFor()
  await managePage.screenshot({ path: '/tmp/creative-v6-agent-manage.png', animations: 'disabled' })
  await managePage.keyboard.press('Escape')
  await menu('重命名')
  const name = managePage.getByRole('textbox', { name: '对话名称' })
  assert.equal(await name.inputValue(), '会话甲')
  await managePage.screenshot({ path: '/tmp/creative-v6-agent-rename.png', clip: { x: 880, y: 60, width: 400, height: 70 } })
  await name.fill('  雪山婚纱 · 二稿 ')
  await name.press('Enter')
  await managePage.locator('.cc-agent-title', { hasText: '雪山婚纱 · 二稿' }).waitFor()
  assert.deepEqual(commands.at(-1), ['rename', 'a', { title: '雪山婚纱 · 二稿' }])
  await menu('重命名')
  await name.fill('不要了')
  await name.press('Escape')
  assert.equal(await title(), '雪山婚纱 · 二稿', 'Escape keeps the old name')
  assert.equal(commands.length, 1, 'Escape sends nothing')
  await menu('重命名')
  await name.fill('冲突')
  await name.press('Enter')
  await managePage.getByRole('alert').waitFor()
  assert.equal(await title(), '雪山婚纱 · 二稿', 'a refused rename puts the old name back')

  await menu('归档')
  await managePage.locator('.cc-agent-title', { hasText: '会话乙' }).waitFor()
  assert.deepEqual(commands.at(-1), ['archive', 'a', {}])
  assert.equal((await options('历史对话')).some((text) => text.includes('雪山婚纱')), false, 'an archived conversation leaves the list')

  await menu('查看已归档')
  await managePage.locator('.cc-agent-title', { hasText: '雪山婚纱 · 二稿' }).waitFor()
  await managePage.getByText('这个对话已归档，恢复后可以继续。').waitFor()
  await managePage.screenshot({ path: '/tmp/creative-v6-agent-archived.png' })
  assert.equal(await managePage.getByLabel('创作问题').count(), 0, 'an archived conversation takes no new message')
  assert.deepEqual(await options('对话操作'), ['返回对话列表'])
  await managePage.getByRole('button', { name: '恢复', exact: true }).click()
  await managePage.getByLabel('创作问题').waitFor()
  assert.deepEqual(commands.at(-1), ['restore', 'a', {}])
  assert.equal(await title(), '雪山婚纱 · 二稿', 'restoring stays on the conversation')

  await menu('查看已归档')
  await managePage.getByText('没有已归档的对话').waitFor()
  await menu('返回对话列表')
  await managePage.getByLabel('创作问题').waitFor()
  await managePage.close()

  // 第一条消息顺带建对话：建对话的响应丢了，刷新后「重新发送」重放同一个操作，
  // 不会多出一个空对话；旧版本存下的待确认记录照样能重新发送。
  const chainPage = await browser.newPage()
  chainPage.on('pageerror', (error) => { throw error })
  const chainPosts = []
  const made = new Map()
  let dropConversation = true
  await chainPage.route('**/api/v1/creative/**', async (route) => {
    const req = route.request(), path = new URL(req.url()).pathname
    const json = (body) => route.fulfill({ json: body })
    if (req.method() === 'POST') {
      const id = req.headers()['idempotency-key']
      chainPosts.push([path.replace('/api/v1/creative', ''), id])
      if (path.endsWith('/canvases/canvas/conversations')) {
        if (!made.has(id)) made.set(id, `conv${made.size + 1}`)
        if (dropConversation) { dropConversation = false; return route.abort('failed') }
        return json({ id: made.get(id), title: '新对话' })
      }
      if (path.endsWith('/egress-consents')) return json({ id: 'consent', approved_revisions: 0 })
      if (path.endsWith('/runs')) return json({ ...finishedRun, conversation_id: path.split('/').at(-2) })
    }
    if (path.endsWith('/agent/catalog')) return json({ limits: { attachments: 10, image_preview_count: 8 }, skills: [], models: [{ model_key: 'model', display_name: 'Test model', available: true, capability: { image_input: false } }] })
    if (path.endsWith('/agent-runs/run')) return json(finishedRun)
    return json({ items: [] })
  })
  // 从没看过首次外发说明：新消息先弹窗，取消什么都不发、草稿留着。
  await chainPage.goto(`${url}?notice=0`)
  await chainPage.getByRole('button', { name: '打开创作助手' }).click()
  await chainPage.getByLabel('创作问题').fill('第一条消息')
  const notice = chainPage.getByRole('alertdialog', { name: '发送前说明' })
  await chainPage.getByRole('button', { name: '发送', exact: true }).click()
  await notice.getByText('平台签约的模型服务商', { exact: false }).waitFor()
  await notice.getByRole('button', { name: '取消' }).click()
  await notice.waitFor({ state: 'detached' })
  assert.deepEqual(chainPosts, [], 'cancelling the notice sends nothing')
  assert.equal(await chainPage.getByLabel('创作问题').textContent(), '第一条消息', 'the draft stays')
  await chainPage.getByRole('button', { name: '发送', exact: true }).click()
  await notice.getByRole('button', { name: '知道了，发送' }).click()
  await chainPage.getByRole('alert').waitFor()
  assert.equal(made.size, 1, 'the server created the conversation')
  assert.equal(await chainPage.evaluate(() => localStorage.getItem('creative-egress-notice:qa-account')), noticeVersion)
  // 重放是此前确认过的发送，不再弹窗：清掉本机记录也一样。
  await chainPage.evaluate(() => localStorage.clear())
  await chainPage.reload()
  await chainPage.getByRole('button', { name: '打开创作助手' }).click()
  await chainPage.getByRole('button', { name: '重新发送' }).click()
  await chainPage.waitForFunction((k) => sessionStorage.getItem(k) === null, key)
  const conversationPosts = chainPosts.filter(([p]) => p.endsWith('/conversations'))
  assert.equal(conversationPosts.length, 2)
  assert.equal(conversationPosts[0][1], conversationPosts[1][1], 'the retry replays the same conversation operation')
  assert.equal(made.size, 1, 'no second, empty conversation')
  assert.deepEqual(chainPosts.slice(2).map(([p]) => p), ['/conversations/conv1/egress-consents', '/conversations/conv1/runs'])
  // 旧格式：当前一步加上下一次运行的身份。
  chainPosts.length = 0
  await chainPage.evaluate((k) => sessionStorage.setItem(k, JSON.stringify({
    path: '/conversations/conv1/egress-consents',
    operation: 'legacy-consent',
    body: JSON.stringify({ operation_id: 'legacy-consent', client_created_at: '2026-10-01T00:00:00Z', payload: { purpose: 'creative_assistance', mode: 'selected_revisions', data_classes: ['text', 'canvas_metadata'], selected_revision_ids: [] } }),
    nextOperation: 'legacy-run',
    nextCreatedAt: '2026-10-01T00:00:00Z',
    nextRun: { model_key: 'model', egress_consent_id: '', instruction: { schema_version: 1, instruction_segments: [{ type: 'text', text: '旧记录' }] } },
    draft: { conversation: 'conv1', model: 'model', skill: '', text: '旧记录' },
  })), key)
  await chainPage.reload()
  await chainPage.getByRole('button', { name: '打开创作助手' }).click()
  await chainPage.getByRole('button', { name: '重新发送' }).click()
  await chainPage.waitForFunction((k) => sessionStorage.getItem(k) === null, key)
  assert.deepEqual(chainPosts, [
    ['/conversations/conv1/egress-consents', 'legacy-consent'],
    ['/conversations/conv1/runs', 'legacy-run'],
  ])
  assert.equal(await notice.count(), 0, 'replays never ask again')
  await chainPage.close()
  console.log(
    'PASS: both delayed-consent orders create one run; refresh + 422 restores draft/model/skill/conversation; image/text references, model capability and retry identity verified; conversations recall their last model; rename, archive, read-only archive view and restore; a lost conversation reply replays one operation; legacy pending records resend; the first-send notice gates new messages but not replays',
  )
} finally {
  await browser.close()
  await rm(fixture, { force: true })
}
