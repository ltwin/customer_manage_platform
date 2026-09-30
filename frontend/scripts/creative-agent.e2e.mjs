// Run with the frontend Vite server on CREATIVE_AGENT_URL (default :5179).
// Exercises the real component with delayed HTTP replies; never calls a vendor.
import assert from 'node:assert/strict'
import { writeFile, rm } from 'node:fs/promises'
import { chromium } from 'playwright'
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
createRoot(document.getElementById('root')).render(React.createElement(AgentPanel,{account:'qa-account',canvasID:'canvas'}));
</script></body></html>`,
)
const browser = await chromium.launch({ headless: true })
const url = `${process.env.CREATIVE_AGENT_URL || 'http://127.0.0.1:5179'}/.agent-command-qa.html`
const key = 'creative-agent-pending:qa-account:canvas'
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
          return json({ id: 'consent', vendor_key: 'deepseek' })
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
      if (path.endsWith('/catalog'))
        return json({
          models: [
            {
              model_key: 'model',
              vendor_key: 'deepseek',
              display_name: 'Test model',
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
    assert(!await page.getByLabel('当前对话').isVisible(),'conversation picker starts in the title menu')
    await page.getByLabel('对话菜单').click()
    await page.getByLabel('当前对话').waitFor()
    await page.getByLabel('创作问题').click()
    assert(!await page.getByLabel('当前对话').isVisible(),'outside click closes the title menu')
    await page.getByLabel('创作问题').fill('保留原始问题')
    const metrics=await page.getByRole('complementary',{name:'创作助手'}).evaluate(el=>{const s=getComputedStyle(el),b=el.querySelector('.cc-agent-send'),r=b.getBoundingClientRect();return {width:el.getBoundingClientRect().width,background:s.backgroundColor,send:[r.width,r.height,getComputedStyle(b).borderRadius]}})
    assert.deepEqual(metrics,{width:380,background:'rgb(27, 27, 29)',send:[30,30,'50%']})
    if(order==='old-first') await page.screenshot({path:'/tmp/creative-v6-agent-desktop.png'})
    await page.getByLabel('Skill', { exact: true }).selectOption('version')
    await page.getByLabel('创作问题').press('Enter')
    assert.equal(grants, 0, 'Enter cannot bypass the external-send consent')
    await page.getByRole('checkbox').check()
    await page.getByLabel('创作问题').press('Enter')
    await page.waitForFunction(
      (k) => JSON.parse(sessionStorage.getItem(k)).nextOperation,
      key,
    )
    await page.getByRole('button', { name: '收起创作助手' }).click()
    await page.getByRole('button', { name: '打开创作助手' }).click()
    await page.getByRole('button', { name: '确认原操作' }).click()
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
  let runPosts = 0
  await page.route('**/api/v1/creative/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    const json = (body) => route.fulfill({ json: body })
    if (route.request().method() === 'POST') {
      if (path.endsWith('/egress-consents'))
        return json({ id: 'consent', vendor_key: 'deepseek' })
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
    if (path.endsWith('/catalog'))
      return json({
        models: [
          {
            model_key: 'model',
            vendor_key: 'deepseek',
            display_name: 'Test model',
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
  await page.getByLabel('对话菜单').click()
  await page.getByLabel('当前对话').selectOption('other')
  await page.getByLabel('创作问题').fill('刷新后仍然保留的原文')
  await page.getByLabel('Skill', { exact: true }).selectOption('version')
  await page.getByRole('checkbox').check()
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await page.getByRole('alert').waitFor()
  await page.reload()
  await page.getByRole('button', { name: '打开创作助手' }).click()
  await page.getByRole('button', { name: '确认原操作' }).click()
  await page.waitForFunction((k) => sessionStorage.getItem(k) === null, key)
  assert.equal(
    await page.getByLabel('创作问题').inputValue(),
    '刷新后仍然保留的原文',
  )
  assert.equal(await page.getByLabel('创作问题').isEnabled(), true)
  assert.equal(await page.getByLabel('当前对话').inputValue(), 'other')
  assert.equal(
    await page.getByLabel('模型', { exact: true }).inputValue(),
    'model',
  )
  assert.equal(
    await page.getByLabel('Skill', { exact: true }).inputValue(),
    'version',
  )
  assert.equal(await page.getByRole('checkbox').isChecked(), false)
  assert.equal(runPosts, 2)
  await page.setViewportSize({width:390,height:844})
  await page.screenshot({path:'/tmp/creative-v6-agent-mobile.png'})
  const panelBox=await page.getByRole('complementary',{name:'创作助手'}).boundingBox()
  assert(panelBox.x>=0 && panelBox.x+panelBox.width<=390)
  const compose=await page.locator('.cc-agent-box').boundingBox()
  const footer=await page.locator('.cc-agent-composer-foot').boundingBox()
  assert(footer.x>=compose.x && footer.x+footer.width<=compose.x+compose.width)
  await page.close()
  console.log(
    'PASS: both delayed-consent orders create one run; refresh + 422 restores draft/model/skill/conversation',
  )
} finally {
  await browser.close()
  await rm(fixture, { force: true })
}
