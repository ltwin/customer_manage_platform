// Run with the frontend Vite server on AGENT_SCENE_URL (default :5179):
//   node scripts/agent-scene-mention.e2e.mjs   (AGENT_SCENE_E2E_ONLY=<part of a name> runs matching checks)
// Exercises the real assistant panel with mocked HTTP; never calls a vendor.
// Covers: selection → pending reference → committed reference, the @ menu's
// search and groups, the scene object picker, and cut/paste/undo/redo of
// scene references.
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { writeFile, rm } from 'node:fs/promises'
import { chromium } from 'playwright'

const noticeVersion = /egressPolicyVersion = '([^']+)'/.exec(readFileSync(new URL('../src/creative-canvas/editor/agentState.ts', import.meta.url), 'utf8'))[1]
const fixture = new URL('../.agent-scene-mention-qa.html', import.meta.url)
// The canvas: two scenes, an image, a text, a video; ?many=1 adds 200 texts.
await writeFile(
  fixture,
  `<html><body><div id="root" class="cc-workspace"></div><script type="module">
import React from 'react';
import {createRoot} from 'react-dom/client';
import '/src/index.css';
import '/src/creative-canvas/editor/workspace.css';
import '/src/creative-canvas/editor/generation.css';
import AgentPanel from '/src/creative-canvas/editor/AgentPanel.tsx';
import {sceneContext} from '/src/creative-scene/context.ts';
import {setAuthenticated} from '/src/auth/session.ts';
setAuthenticated({access_token:'local-test',expires_in:3600,token_type:'Bearer'},{id:'qa-account',username:'qa'});
const node = (id, type, title, data, content) => ({id, metadata:{title, type_key:type}, data, ...(content ? {content} : {})});
const nodes = [
  node('n-living', 'core.scene', '客厅', {document_id:'cwdoc_living'}),
  node('n-bed', 'core.scene', '卧室', {document_id:'cwdoc_bed'}),
  node('n-image', 'core.image', '参考图1', {content_revision_id:'ccrv_image'}, {id:'ccrv_image', kind:'image', payload:{}, media:[], truncated:false}),
  node('n-text', 'core.text', '方案', {content_revision_id:'ccrv_text'}, {id:'ccrv_text', kind:'text', payload:{body:'暖色逆光'}, media:[], truncated:false}),
  node('n-video', 'core.video', '片段', {content_revision_id:'ccrv_video'}, {id:'ccrv_video', kind:'video', payload:{}, media:[], truncated:false}),
];
if (location.search.includes('many=1'))
  for (let i = 0; i < 200; i++)
    nodes.push(node('n-t' + i, 'core.text', '文本节点' + i, {content_revision_id:'ccrv_t' + i}, {id:'ccrv_t' + i, kind:'text', payload:{body:'内容' + i}, media:[], truncated:false}));
function App() {
  const [focus, setFocus] = React.useState(null);
  const [editing, setEditing] = React.useState(null);
  window.__select = setFocus;
  window.__editing = setEditing;
  return React.createElement(AgentPanel, {account:'qa-account', canvasID:'canvas', canvas:{nodes, edges:[], node_inputs:[]}, defaultOpen:true, canvasFocus:focus, editingSceneID:editing});
}
window.__scene = (value) => sceneContext.setState(value, true);
createRoot(document.getElementById('root')).render(React.createElement(App));
</script></body></html>`,
)

const base = process.env.AGENT_SCENE_URL || 'http://localhost:5179'
const only = process.env.AGENT_SCENE_E2E_ONLY
const browser = await chromium.launch({ headless: true })
// Scene documents: 客厅 has a group with two lights, a sofa and a table.
const object = (id, name, kind, parent, order) => ({ id, name, kind, parent_id: parent, order, visible: true, locked: false })
const scenes = {
  cwdoc_living: {
    id: 'cwdoc_living', title: '客厅', type_key: 'scene',
    scene: { objects: [
      object('g_lights', '灯组', 'group', null, 1),
      object('lamp', '台灯', 'light', 'g_lights', 1),
      object('key', '主光', 'light', 'g_lights', 2),
      object('sofa', '沙发', 'primitive', null, 2),
      object('table', '茶几', 'primitive', null, 3),
    ] },
  },
  cwdoc_bed: { id: 'cwdoc_bed', title: '卧室', type_key: 'scene', scene: { objects: [object('bed', '', 'primitive', null, 1)] } },
}

async function open(query = '', imageInput = true, queued = null, withHistory = false) {
  const page = await browser.newPage()
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  const runs = []
  const previewReads = []
  if (queued) await page.addInitScript((item) => {
    sessionStorage.setItem('creative-agent-queue:qa-account:conversation', JSON.stringify({ items: [item], paused: false }))
  }, queued)
  const finished = {
    id: 'previous', conversation_id: 'conversation', model_key: 'model', state: 'succeeded',
    settlement_state: 'settled', trigger_message_id: 'trigger', revision: '1', last_event_seq: '0',
    pruned_through_seq: '0', snapshot_seq: '0', blocks: [],
  }
  await page.route('**/api/v1/creative/**', async (route) => {
    const req = route.request()
    const path = new URL(req.url()).pathname
    const json = (body) => route.fulfill({ json: body })
    if (req.method() === 'GET' && path.endsWith('/egress-consent'))
      return json({
        policy_version: noticeVersion, effective: true, revision: '1',
        consent: { id: 'ccec_qa', policy_version: noticeVersion, library_access: false, granted_at: '2026-11-13T00:00:00Z' },
      })
    if (withHistory && path.endsWith('/conversations/conversation/messages'))
      return json({ items: [{
        id: 'stored-message', ordinal: '1', role: 'user', status: 'complete',
        body: { blocks: [
          { type: 'content_ref', ref_id: 'ccrv_old_image' },
          { type: 'content_ref', ref_id: 'ccrv_old_text' },
          { type: 'content_ref', ref_id: 'ccrv_missing' },
          {
            type: 'scene_ref',
            scene: { document_id: 'cwdoc_living', title: '原来的客厅', revision: 3, objects: [
              { id: 'lamp', name: '原来的台灯' }, { id: 'key', name: '原来的主光' }, { id: 'gone', name: '', missing: true },
            ] },
          },
          { type: 'scene_ref', scene: { document_id: 'cwdoc_bed', title: '原来的卧室', revision: 1, objects: [] } },
          { type: 'skill_ref', skill: { skill_id: 'skill', skill_version_id: 'old-version', version_number: 2, display_name: '原来的技能' } },
          { type: 'scene_selection', scene: { document_id: 'cwdoc_living', title: '客厅', objects: [{ id: 'sofa', name: '旧沙发' }] } },
        ] },
      }], prev_cursor: '' })
    if (withHistory && path.includes('/content-revisions/')) {
      const id = path.split('/').at(-1)
      previewReads.push(id)
      if (id === 'ccrv_old_image')
        return json({
          id, kind: 'image', truncated: false, payload: {},
          media: [{ role: 'display', mime: 'image/png', width: 1, height: 1, blob_id: 'blob' }],
        })
      if (id === 'ccrv_old_text')
        return json({ id, kind: 'text', payload: { body: '发送时保存的原文' }, media: [], truncated: false })
      return route.fulfill({ status: 404, json: { error: { code: 'not_found', message: '内容不存在' } } })
    }
    if (withHistory && path.endsWith('/agent/skills/skill/versions/old-version')) {
      previewReads.push('old-version')
      return json({ description: '发送时使用的技能版本说明' })
    }
    if (req.method() === 'POST') {
      if (withHistory && path.endsWith('/media-access-tickets')) {
        assert.equal(req.postDataJSON().content_revision_id, 'ccrv_old_image')
        return json({
          url: 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+b8Z0AAAAASUVORK5CYII=',
          expires_at: new Date(Date.now() + 120000).toISOString(),
        })
      }
      if (path.endsWith('/canvases/canvas/conversations')) return json({ id: 'conversation', title: '新对话' })
      if (path.endsWith('/runs')) {
        runs.push(req.postDataJSON().payload)
        if (queued && runs.at(-1).instruction.instruction_segments.some((s) => s.document_id === 'cwdoc_gone'))
          return route.fulfill({ status: 404, json: { error: { code: 'not_found', message: '场景已删除' } } })
        return route.fulfill({ status: 422, json: { error: { code: 'creative_model_unavailable', message: '测试：不真的运行' } } })
      }
    }
    if ((queued || withHistory) && path.endsWith('/canvases/canvas/conversations')) return json({ items: [{ id: 'conversation', title: '测试对话' }] })
    if (queued && path.endsWith('/conversations/conversation/runs')) return json({ items: [finished] })
    if (queued && path.endsWith('/agent-runs/previous')) return json(finished)
    const doc = /\/documents\/([^/]+)$/.exec(path)
    if (doc) return json(scenes[decodeURIComponent(doc[1])])
    if (path.endsWith('/agent/catalog'))
      return json({
        limits: { attachments: 10, image_preview_count: 8 },
        models: [{ model_key: 'model', display_name: 'Test model', capability: { image_input: imageInput }, available: true }],
        skills: [],
      })
    return json({ items: [] })
  })
  await page.goto(`${base}/.agent-scene-mention-qa.html${query}`)
  const editor = page.getByRole('textbox', { name: '创作问题' })
  await editor.waitFor()
  return { page, editor, runs, errors, previewReads }
}
const select = (page, id) => page.evaluate((value) => window.__select(value), id)
const pending = (page) => page.locator('.cc-prompt-editor [data-pending]')
const chips = (page) => page.locator('.cc-prompt-editor [data-ref]')
// The segments the panel sends: submit and read the run request.
async function sent(t, before = t.runs.length) {
  await t.page.getByRole('button', { name: '发送', exact: true }).click()
  for (let i = 0; t.runs.length === before && i < 300; i++) await t.page.waitForTimeout(10)
  assert.equal(t.runs.length, before + 1, 'a run was requested')
  return t.runs.at(-1).instruction.instruction_segments
}
// Leave the editor without touching the selection, as clicking the canvas does.
const blur = (page) => page.evaluate(() => document.activeElement instanceof HTMLElement && document.activeElement.blur())
async function pickFromMenu(t, query) {
  await t.editor.click()
  await t.page.keyboard.type(`@${query}`)
  await t.page.locator('.cc-prompt-picker').waitFor()
}

async function enterScene(t, selected = [], documentID = 'cwdoc_living') {
  await t.page.evaluate(async ({ documentID, selected }) => {
    window.__editing(documentID)
    window.__scene(null)
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))
    window.__scene({ documentID, selected })
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))
  }, { documentID, selected })
}

const checks = {
  async 'historical reference chips preview the exact saved revision and snapshot'() {
    const t = await open('', true, null, true)
    const refs = t.page.locator('.cc-agent-user .cc-agent-chip')
    const tooltip = t.page.getByRole('tooltip')
    await refs.first().waitFor()
    assert.deepEqual(t.previewReads, [], 'history does not read previews in advance')
    await refs.nth(0).hover()
    const image = tooltip.getByRole('img')
    await image.waitFor()
    await image.evaluate((el) => el.decode())
    const box = await tooltip.boundingBox()
    const viewport = t.page.viewportSize()
    assert.ok(box.x >= 0 && box.x + box.width <= viewport.width && box.y >= 0 && box.y + box.height <= viewport.height)
    await refs.nth(1).hover()
    await tooltip.getByText('发送时保存的原文').waitFor()
    await refs.nth(2).hover()
    await tooltip.getByText('素材暂不可预览').waitFor()
    await refs.nth(3).hover()
    await tooltip.getByText('原来的台灯、原来的主光、已删除的物件').waitFor()
    await tooltip.getByText('原来的客厅', { exact: true }).waitFor()
    await refs.nth(4).hover()
    await tooltip.getByText('整个场景', { exact: true }).waitFor()
    await refs.nth(5).hover()
    await tooltip.getByText('Skill · 第 2 版').waitFor()
    await tooltip.getByText('发送时使用的技能版本说明').waitFor()
    await t.page.locator('.cc-agent-user-scene .cc-agent-scene-tag').hover()
    await tooltip.getByText('旧沙发', { exact: true }).waitFor()
    await t.editor.hover()
    await tooltip.waitFor({ state: 'hidden' })
    await refs.nth(1).focus()
    await tooltip.getByText('发送时保存的原文').waitFor()
    await t.page.keyboard.press('Escape')
    await tooltip.waitFor({ state: 'hidden' })
    assert.deepEqual(t.previewReads, ['ccrv_old_image', 'ccrv_old_text', 'ccrv_missing', 'old-version'])

    await pickFromMenu(t, '客')
    await t.page.getByRole('option', { name: /客厅/ }).click()
    await chips(t.page).first().hover()
    await tooltip.getByText('场景引用 · 点击修改物件').waitFor()
    return t
  },
  async 'keyboard navigation scrolls options into view and inserts the searched object'() {
    const t = await open('?many=1')
    await pickFromMenu(t, '')
    const menu = t.page.locator('.cc-prompt-options')
    await t.editor.press('ArrowUp')
    const last = menu.locator('[aria-selected=true]')
    assert.ok(await menu.evaluate((el) => el.scrollTop > 0))
    const bounds = await menu.boundingBox()
    const active = await last.boundingBox()
    assert.ok(active.y >= bounds.y && active.y + active.height <= bounds.y + bounds.height + 1)
    await t.editor.press('Escape')
    await t.editor.fill('')
    await pickFromMenu(t, '客')
    await t.editor.press('ArrowRight')
    const search = t.page.getByRole('searchbox', { name: '搜索物件' })
    await search.fill('台灯')
    await search.press('ArrowDown')
    assert.equal(await t.page.locator('[data-keyboard-active=true]').getAttribute('data-object'), 'lamp')
    await search.press('Enter')
    await chips(t.page).first().waitFor()
    assert.deepEqual((await sent(t)).filter((s) => s.type === 'scene_ref'), [
      { type: 'scene_ref', document_id: 'cwdoc_living', object_ids: ['lamp'] },
    ])
    return t
  },
  async 'two chips for the same scene edit their own object selections'() {
    const t = await open()
    const row = (id) => t.page.locator(`.cc-scene-picker [data-object="${id}"]`)
    const insert = t.page.getByRole('button', { name: '插入', exact: true })
    for (const id of ['lamp', 'sofa']) {
      await pickFromMenu(t, '客')
      await t.page.getByRole('button', { name: '选择「客厅」里的物件' }).click()
      await row(id).click()
      await insert.click()
      await insert.waitFor({ state: 'hidden' })
      await t.page.keyboard.type(' ')
    }
    await chips(t.page).filter({ hasText: '台灯' }).click()
    assert.equal(await row('lamp').getAttribute('aria-selected'), 'true')
    await chips(t.page).filter({ hasText: '沙发' }).click()
    assert.equal(await row('sofa').getAttribute('aria-selected'), 'true')
    assert.equal(await row('lamp').getAttribute('aria-selected'), 'false')
    await insert.click()
    assert.deepEqual((await sent(t)).filter((s) => s.type === 'scene_ref'), [
      { type: 'scene_ref', document_id: 'cwdoc_living', object_ids: ['lamp'] },
      { type: 'scene_ref', document_id: 'cwdoc_living', object_ids: ['sofa'] },
    ])
    return t
  },
  async 'a restored selection and a reordered set do not place pending references'() {
    const t = await open()
    await enterScene(t, ['lamp', 'key'])
    assert.equal(await pending(t.page).count(), 0)
    await t.page.evaluate(() => window.__scene({ documentID: 'cwdoc_living', selected: ['key', 'lamp'] }))
    await t.page.waitForTimeout(80)
    assert.equal(await pending(t.page).count(), 0)
    await t.editor.fill('改成蓝色')
    assert.deepEqual(await sent(t), [{ type: 'text', text: '改成蓝色' }])
    await blur(t.page)
    await t.page.evaluate(() => window.__scene({ documentID: 'cwdoc_living', selected: ['sofa'] }))
    await pending(t.page).waitFor()
    await enterScene(t, ['bed'], 'cwdoc_bed')
    assert.equal(await pending(t.page).count(), 0, 'entering another scene just sets its baseline')
    return t
  },
  async 'a missing queued scene can be replaced without overwriting another draft'() {
    const t = await open('', true, {
      id: 'queued', parts: [{ kind: 'text', text: '修改 ' }, { kind: 'chip', title: '已删除场景' }, { kind: 'text', text: ' 为蓝色' }],
      draft: { conversation: 'conversation', model: 'model', skill: '', text: '修改 @[sref_gone] 为蓝色', scenes: {
        sref_gone: { documentID: 'cwdoc_gone', title: '已删除场景', objectIDs: ['gone'], names: ['旧台灯'] },
      } },
      send: { model: 'model', segments: [{ type: 'text', text: '修改 ' }, { type: 'scene_ref', document_id: 'cwdoc_gone', object_ids: ['gone'] }, { type: 'text', text: ' 为蓝色' }], scene: null },
      steer: null, hasImage: false,
    })
    const restore = t.page.getByRole('button', { name: '重新选择引用' })
    await restore.waitFor()
    assert.equal(t.runs.length, 1)
    assert.equal(await t.page.getByRole('button', { name: '继续发送' }).count(), 0)
    await t.editor.fill('现在正在写的内容')
    assert.equal(await restore.getAttribute('aria-disabled'), 'true')
    await t.editor.fill('')
    await restore.click()
    await restore.waitFor({ state: 'hidden' })
    await chips(t.page).first().waitFor()
    assert.match(await t.editor.textContent(), /修改 .*旧台灯.* 为蓝色/)
    await t.editor.evaluate((el) => {
      el.focus()
      const range = document.createRange()
      range.selectNode(el.querySelector('[data-ref]'))
      getSelection().removeAllRanges()
      getSelection().addRange(range)
    })
    await t.editor.press('Backspace')
    await t.page.keyboard.type('@')
    await t.page.locator('.cc-prompt-picker').waitFor()
    await t.page.getByRole('button', { name: '选择「客厅」里的物件' }).click()
    const search = t.page.getByRole('searchbox', { name: '搜索物件' })
    await search.fill('台灯')
    await search.press('ArrowDown')
    await search.press('Enter')
    await chips(t.page).filter({ hasText: '客厅' }).waitFor()
    const sentAgain = await sent(t, 1)
    assert.deepEqual(sentAgain.filter((s) => s.type === 'scene_ref'), [
      { type: 'scene_ref', document_id: 'cwdoc_living', object_ids: ['lamp'] },
    ])
    assert.match(sentAgain.map((s) => s.text ?? '').join(' '), /修改\s+为蓝色/)
    return t
  },
  async 'a canvas pick becomes a grey pending reference, is replaced, and turns real on focus'() {
    const t = await open()
    await t.editor.click()
    await t.page.keyboard.type('把 ')
    await blur(t.page)
    await select(t.page, 'n-image')
    await pending(t.page).waitFor()
    assert.match(await pending(t.page).textContent(), /参考图1/)
    assert.equal(await chips(t.page).count(), 0, 'pending is not part of the text')
    // Picking another node replaces it.
    await select(t.page, 'n-living')
    await t.page.waitForFunction(() => document.querySelector('.cc-prompt-editor [data-pending]')?.textContent.includes('客厅'))
    assert.equal(await pending(t.page).count(), 1)
    // A video cannot be referenced: the pending reference goes.
    await select(t.page, 'n-video')
    await t.page.waitForFunction(() => !document.querySelector('.cc-prompt-editor [data-pending]'))
    await select(t.page, 'n-living')
    await pending(t.page).waitFor()
    await t.editor.click()
    assert.equal(await pending(t.page).count(), 0)
    assert.match(await chips(t.page).first().textContent(), /客厅/)
    // The caret sits after it: typing continues the sentence.
    await t.page.keyboard.type('的灯光改暖')
    assert.match(await t.editor.textContent(), /把 ◫客厅\s*的灯光改暖/)
    // Leaving and coming back with the same selection adds nothing.
    await blur(t.page)
    await select(t.page, 'n-living')
    await t.page.waitForTimeout(100)
    assert.equal(await pending(t.page).count(), 0, 'unchanged selection places nothing')
    // Deselecting then selecting again is a change.
    await select(t.page, null)
    await select(t.page, 'n-text')
    await pending(t.page).waitFor()
    await t.editor.click()
    const segments = await sent(t)
    assert.equal(segments[0].text, '把 ')
    assert.deepEqual(segments.filter((s) => s.type !== 'text'), [
      { type: 'scene_ref', document_id: 'cwdoc_living' },
      { type: 'content_ref', content_revision_id: 'ccrv_text' },
    ], 'references in place')
    assert.match(segments[2].text, /的灯光改暖/)
    return t
  },
  async 'only a pending reference in an empty draft can be sent, committed first'() {
    const t = await open()
    await select(t.page, 'n-bed')
    await pending(t.page).waitFor()
    assert.equal(await t.page.getByRole('button', { name: '发送', exact: true }).isDisabled(), false, 'send is open')
    const segments = await sent(t)
    assert.deepEqual(segments.filter((s) => s.type !== 'text'), [{ type: 'scene_ref', document_id: 'cwdoc_bed' }])
    return t
  },
  async 'committing a pending image re-checks the model before sending'() {
    const t = await open('', false)
    await select(t.page, 'n-image')
    await pending(t.page).waitFor()
    await t.page.getByRole('button', { name: '发送', exact: true }).click()
    await t.page.getByText('当前模型不支持图片理解').waitFor()
    assert.equal(t.runs.length, 0, 'nothing is sent')
    assert.match(await chips(t.page).first().textContent(), /参考图1/, 'the draft keeps the reference')
    return t
  },
  async 'the @ menu searches 200 nodes, groups them and inserts the highlighted one'() {
    const t = await open('?many=1')
    await select(t.page, 'n-t150')
    await blur(t.page)
    await pickFromMenu(t, '')
    const groups = await t.page.locator('.cc-prompt-group').allTextContents()
    assert.deepEqual(groups.map((g) => g.replace(/\d+$/, '')), ['场景', '图片', '文本'])
    assert.match(await t.page.locator('.cc-prompt-options [role=group][aria-label=文本] [role=option]').first().textContent(), /文本节点150.*已选中/)
    const elapsed = await t.page.evaluate(async () => {
      const input = document.querySelector('.cc-prompt-search')
      input.focus()
      const start = performance.now()
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set
      set.call(input, '文本节点19')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      await new Promise((resolve) => requestAnimationFrame(() => resolve()))
      return performance.now() - start
    })
    assert.ok(elapsed < 100, `search took ${elapsed.toFixed(1)} ms`)
    assert.equal(await t.page.locator('.cc-prompt-options [role=option]').count(), 11)
    // Text content matches too.
    await t.page.locator('.cc-prompt-search').fill('暖色')
    assert.deepEqual(await t.page.locator('.cc-prompt-options [role=option] > span:nth-child(2)').allTextContents(), ['方案'])
    await t.page.locator('.cc-prompt-search').press('Enter')
    await t.page.waitForFunction(() => !document.querySelector('.cc-prompt-picker'))
    // The canvas pick became a reference when the editor was clicked; the search result follows it.
    assert.deepEqual(await chips(t.page).allTextContents(), ['T文本节点150', 'T方案'])
    console.log(`  search over 200 nodes: ${elapsed.toFixed(1)} ms`)
    return t
  },
  async 'the scene picker selects by click, modifier and drag, and inserts in list order'() {
    const t = await open()
    await pickFromMenu(t, '客')
    await t.page.getByRole('button', { name: '选择「客厅」里的物件' }).click()
    const row = (name) => t.page.locator('.cc-scene-picker-list [data-object]', { hasText: name })
    await row('沙发').waitFor()
    await row('沙发').click()
    await row('台灯').click({ modifiers: ['ControlOrMeta'] })
    const picked = () => t.page.locator('.cc-scene-picker-list [aria-selected=true] span:last-child').allTextContents()
    assert.deepEqual(await picked(), ['台灯', '沙发'])
    // Drag from 台灯 down to 茶几: the run replaces the selection.
    const from = await row('台灯').boundingBox()
    const to = await row('茶几').boundingBox()
    await t.page.mouse.move(from.x + 20, from.y + from.height / 2)
    await t.page.mouse.down()
    await t.page.mouse.move(to.x + 20, to.y + to.height / 2, { steps: 6 })
    await t.page.mouse.up()
    assert.deepEqual(await picked(), ['台灯', '主光', '沙发', '茶几'])
    // Search narrows the list but keeps its parents; hidden selections stay.
    await t.page.locator('.cc-scene-picker .cc-prompt-search').fill('主')
    assert.deepEqual(await t.page.locator('.cc-scene-picker-list [data-object] span:last-child').allTextContents(), ['灯组', '主光'])
    assert.equal(await t.page.locator('.cc-scene-picker-list [data-object][data-matched=false]').count(), 1, 'the parent is shown but not pickable')
    await t.page.locator('.cc-scene-picker .cc-prompt-search').fill('')
    // Mod-click drops 沙发 from the dragged run.
    await row('沙发').click({ modifiers: ['ControlOrMeta'] })
    assert.deepEqual(await picked(), ['台灯', '主光', '茶几'])
    await t.page.getByRole('button', { name: '插入' }).click()
    await t.page.waitForFunction(() => !document.querySelector('.cc-prompt-picker'))
    assert.equal(await chips(t.page).first().textContent(), '◫客厅 · 台灯、主光 +1')
    const segments = await sent(t)
    assert.deepEqual(segments.find((s) => s.type === 'scene_ref'), { type: 'scene_ref', document_id: 'cwdoc_living', object_ids: ['lamp', 'key', 'table'] })
    return t
  },
  async 'the insert button and selected scene rows get brighter on hover'() {
    const t = await open()
    await pickFromMenu(t, '客')
    await t.page.getByRole('button', { name: '选择「客厅」里的物件' }).click()
    const picker = t.page.locator('.cc-scene-picker')
    const search = picker.getByRole('searchbox', { name: '搜索物件' })
    const insert = picker.getByRole('button', { name: '插入' })
    const whole = picker.getByRole('option', { name: '整个场景' })
    const lamp = picker.locator('[data-object="lamp"]')
    await lamp.waitFor()

    const colors = (locator) => locator.evaluate((element) => {
      const canvas = document.createElement('canvas')
      canvas.width = canvas.height = 1
      const ctx = canvas.getContext('2d')
      const luminance = () => {
        const [r, g, b] = [...ctx.getImageData(0, 0, 1, 1).data].map((byte) => {
          const channel = byte / 255
          return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
        })
        return 0.2126 * r + 0.7152 * g + 0.0722 * b
      }
      const style = getComputedStyle(element)
      for (const color of [getComputedStyle(element.closest('.cc-prompt-picker')).backgroundColor, style.backgroundColor]) {
        ctx.fillStyle = color
        ctx.fillRect(0, 0, 1, 1)
      }
      const background = luminance()
      ctx.fillStyle = style.color
      ctx.fillRect(0, 0, 1, 1)
      const foreground = luminance()
      return { background, fill: style.backgroundColor, contrast: (Math.max(background, foreground) + 0.05) / (Math.min(background, foreground) + 0.05) }
    })

    async function assertBrighterOnHover(locator, name) {
      await search.hover()
      const rest = await colors(locator)
      await locator.hover()
      const hovered = await colors(locator)
      assert.ok(hovered.background > rest.background, `${name} hover should be brighter (${rest.fill} → ${hovered.fill})`)
      assert.ok(hovered.contrast >= 4.5, `${name} hover contrast: ${hovered.contrast.toFixed(2)}:1`)
    }

    await assertBrighterOnHover(insert, 'Insert button')
    await assertBrighterOnHover(whole, 'Whole scene')
    await lamp.click()
    assert.equal(await lamp.getAttribute('aria-selected'), 'true')
    await assertBrighterOnHover(lamp, 'Selected object')
    return t
  },
  async 'an unnamed object can be found and referenced by its type label'() {
    const t = await open()
    await pickFromMenu(t, '卧')
    await t.page.getByRole('button', { name: '选择「卧室」里的物件' }).click()
    await t.page.locator('.cc-scene-picker .cc-prompt-search').fill('基础体')
    const row = t.page.locator('.cc-scene-picker-list [data-object="bed"]')
    await row.getByText('基础体', { exact: true }).click()
    await t.page.getByRole('button', { name: '插入' }).click()
    assert.equal(await chips(t.page).first().textContent(), '◫卧室 · 基础体')
    assert.deepEqual((await sent(t)).filter((segment) => segment.type !== 'text'), [
      { type: 'scene_ref', document_id: 'cwdoc_bed', object_ids: ['bed'] },
    ])
    return t
  },
  async 'cut and paste, delete and undo keep the scene reference'() {
    const t = await open()
    await pickFromMenu(t, '卧')
    await t.page.locator('.cc-prompt-search').press('Enter')
    await t.page.keyboard.type('换成木地板')
    // Cut everything and paste it back.
    await t.page.evaluate(() => {
      const el = document.querySelector('.cc-prompt-editor')
      el.focus()
      const range = document.createRange()
      range.selectNodeContents(el)
      getSelection().removeAllRanges()
      getSelection().addRange(range)
      const data = new DataTransfer()
      el.dispatchEvent(new ClipboardEvent('cut', { clipboardData: data, bubbles: true, cancelable: true }))
      window.__clip = data.getData('text/plain')
    })
    assert.equal(await chips(t.page).count(), 0, 'cut removes it')
    await t.page.evaluate(() => {
      const el = document.querySelector('.cc-prompt-editor')
      const data = new DataTransfer()
      data.setData('text/plain', window.__clip)
      el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }))
    })
    assert.match(await chips(t.page).first().textContent(), /卧室/, 'paste restores it')
    // Delete it with Backspace, then undo.
    await t.editor.press('End')
    for (let i = 0; i < '换成木地板'.length + 1; i++) await t.editor.press('ArrowLeft')
    await t.editor.press('Backspace')
    await t.page.waitForFunction(() => !document.querySelector('.cc-prompt-editor [data-ref]'))
    await t.editor.press('ControlOrMeta+z')
    await chips(t.page).first().waitFor()
    const segments = await sent(t)
    assert.deepEqual(segments.filter((s) => s.type === 'scene_ref'), [{ type: 'scene_ref', document_id: 'cwdoc_bed' }])
    return t
  },
  async 'inserting can be undone and redone, and an edit undoes to the old objects'() {
    const t = await open()
    await pickFromMenu(t, '客')
    await t.page.locator('.cc-prompt-search').press('Enter')
    await chips(t.page).first().waitFor()
    await t.editor.press('ControlOrMeta+z')
    await t.page.waitForFunction(() => !document.querySelector('.cc-prompt-editor [data-ref]'))
    await t.editor.press('ControlOrMeta+Shift+z')
    await chips(t.page).first().waitFor()
    assert.equal(await chips(t.page).first().textContent(), '◫客厅')
    // Click the chip: pick 台灯 and replace it.
    await chips(t.page).first().click()
    const row = (name) => t.page.locator('.cc-scene-picker-list [data-object]', { hasText: name })
    await row('台灯').click()
    await t.page.getByRole('button', { name: '插入' }).click()
    await t.page.waitForFunction(() => document.querySelector('.cc-prompt-editor [data-ref]')?.textContent.includes('台灯'))
    await t.editor.press('ControlOrMeta+z')
    await t.page.waitForFunction(() => document.querySelector('.cc-prompt-editor [data-ref]')?.textContent === '◫客厅')
    const segments = await sent(t)
    assert.deepEqual(segments.filter((s) => s.type === 'scene_ref'), [{ type: 'scene_ref', document_id: 'cwdoc_living' }])
    return t
  },
  async 'committing a pending reference can be undone and redone without it coming back grey'() {
    const t = await open()
    await select(t.page, 'n-living')
    await pending(t.page).waitFor()
    await t.editor.click()
    await chips(t.page).first().waitFor()
    await t.editor.press('ControlOrMeta+z')
    await t.page.waitForFunction(() => !document.querySelector('.cc-prompt-editor [data-ref], .cc-prompt-editor [data-pending]'))
    assert.equal(await t.page.getByRole('button', { name: '发送', exact: true }).isDisabled(), true, 'nothing left to send')
    // Focusing again does not bring it back.
    await blur(t.page)
    await t.editor.click()
    assert.equal(await chips(t.page).count() + await pending(t.page).count(), 0)
    await t.editor.press('ControlOrMeta+Shift+z')
    await chips(t.page).first().waitFor()
    const segments = await sent(t)
    assert.deepEqual(segments.filter((s) => s.type === 'scene_ref'), [{ type: 'scene_ref', document_id: 'cwdoc_living' }])
    return t
  },
  async 'tab-focusing a pending reference does not hijack a later click inside typed text'() {
    const t = await open()
    await select(t.page, 'n-living')
    await pending(t.page).waitFor()
    await t.page.getByRole('button', { name: '收起创作助手' }).focus()
    for (let i = 0; i < 10; i++) {
      if (await t.editor.evaluate((el) => el === document.activeElement)) break
      await t.page.keyboard.press('Tab')
    }
    assert.equal(await t.editor.evaluate((el) => el === document.activeElement), true, 'Tab focused the editor')
    assert.equal(await pending(t.page).count(), 0)
    await chips(t.page).first().waitFor()
    await t.page.keyboard.type('改成暖色灯光')
    const target = await t.editor.evaluate((el) => {
      const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT)
      let node
      while ((node = walker.nextNode())) {
        const at = (node.textContent ?? '').indexOf('暖色')
        if (at >= 0) {
          const range = document.createRange()
          range.setStart(node, at)
          range.setEnd(node, at + 1)
          const rect = range.getBoundingClientRect()
          return { x: rect.left + 1, y: rect.top + rect.height / 2 }
        }
      }
      return null
    })
    assert.ok(target, 'found target text coordinate')
    await t.page.mouse.click(target.x, target.y)
    await t.page.keyboard.type('柔和')
    assert.match(await t.editor.textContent(), /客厅\s*改成柔和暖色灯光/)
    return t
  },
  async 'a scene click inserts the whole scene; re-editing keeps an object that is gone'() {
    const t = await open()
    await pickFromMenu(t, '客')
    await t.page.locator('.cc-prompt-options [role=option]', { hasText: '客厅' }).click()
    await t.page.waitForFunction(() => !document.querySelector('.cc-prompt-picker'))
    assert.equal(await chips(t.page).first().textContent(), '◫客厅', 'a click inserts, no object list')
    // Pick 台灯 and 主光, then 台灯 is deleted from the scene.
    await chips(t.page).first().click()
    const row = (name) => t.page.locator('.cc-scene-picker-list [data-object]', { hasText: name })
    await row('台灯').click()
    await row('主光').click({ modifiers: ['ControlOrMeta'] })
    await t.page.getByRole('button', { name: '插入' }).click()
    await t.page.waitForFunction(() => document.querySelector('.cc-prompt-editor [data-ref]')?.textContent === '◫客厅 · 台灯、主光')
    const objects = scenes.cwdoc_living.scene.objects
    const lamp = objects.findIndex((o) => o.id === 'lamp')
    const [removed] = objects.splice(lamp, 1)
    try {
      await chips(t.page).first().click()
      await row('已不在场景里').waitFor()
      assert.deepEqual(await t.page.locator('.cc-scene-picker-list [aria-selected=true] span:last-child').allTextContents(), ['已不在场景里的物件', '主光'])
      assert.equal(await t.page.locator('.cc-scene-picker footer [role=status]').textContent(), '已选 2 个物件')
      await t.page.getByRole('button', { name: '插入' }).click()
      await t.page.waitForFunction(() => !document.querySelector('.cc-prompt-picker'))
      const segments = await sent(t)
      assert.deepEqual(segments.filter((s) => s.type === 'scene_ref'), [{ type: 'scene_ref', document_id: 'cwdoc_living', object_ids: ['key', 'lamp'] }])
    } finally {
      objects.splice(lamp, 0, removed)
    }
    return t
  },
  async 'more than 50 objects selected in the editor says so and places nothing'() {
    const t = await open()
    await enterScene(t)
    await t.page.evaluate(() => window.__scene({ documentID: 'cwdoc_living', selected: Array.from({ length: 50 }, (_, i) => 'o' + i), names: {} }))
    await pending(t.page).waitFor()
    await t.page.evaluate(() => window.__scene({ documentID: 'cwdoc_living', selected: Array.from({ length: 51 }, (_, i) => 'o' + i), names: {} }))
    await t.page.getByText('一次最多引用 50 个物件').waitFor()
    assert.equal(await pending(t.page).count(), 0)
    return t
  },
  async 'the scene editor selection is referenced in batches, in order'() {
    const t = await open()
    await enterScene(t)
    await t.page.evaluate(() => window.__scene({ documentID: 'cwdoc_living', selected: ['lamp', 'key'], names: { lamp: '台灯', key: '主光' } }))
    await pending(t.page).waitFor()
    await t.editor.click()
    await t.page.keyboard.type('改成金属，')
    await blur(t.page)
    await t.page.evaluate(() => window.__scene({ documentID: 'cwdoc_living', selected: ['sofa'], names: { sofa: '沙发' } }))
    await pending(t.page).waitFor()
    await t.page.evaluate(() => window.__scene({ documentID: 'cwdoc_living', selected: ['sofa', 'table'], names: { sofa: '沙发', table: '茶几' } }))
    await t.page.waitForFunction(() => document.querySelector('.cc-prompt-editor [data-pending]')?.textContent.includes('茶几'))
    await t.editor.click()
    await t.page.keyboard.type('改成胡桃木')
    // Leaving the editor view drops a pending reference and places none.
    await blur(t.page)
    await t.page.evaluate(() => window.__scene({ documentID: 'cwdoc_living', selected: ['bed'], names: {} }))
    await pending(t.page).waitFor()
    await t.page.evaluate(() => window.__editing(null))
    await t.page.waitForFunction(() => !document.querySelector('.cc-prompt-editor [data-pending]'))
    const segments = await sent(t)
    assert.deepEqual(segments.filter((s) => s.type === 'scene_ref'), [
      { type: 'scene_ref', document_id: 'cwdoc_living', object_ids: ['lamp', 'key'] },
      { type: 'scene_ref', document_id: 'cwdoc_living', object_ids: ['sofa', 'table'] },
    ])
    assert.equal(await t.page.locator('.cc-agent-box .cc-agent-scene-tag').count(), 0, 'no selection tag floats over the composer')
    return t
  },
  async 'a picker of 500 objects scrolls and drags without long frames'() {
    const objects = scenes.cwdoc_bed.scene.objects
    for (let i = 0; i < 500; i++) objects.push(object(`o${i}`, `物件${i}`, 'primitive', null, i + 2))
    const t = await open()
    await pickFromMenu(t, '卧')
    await t.page.getByRole('button', { name: '选择「卧室」里的物件' }).click()
    await t.page.locator('.cc-scene-picker-list [data-object]').nth(400).waitFor({ state: 'attached' })
    const frames = await t.page.evaluate(async () => {
      const list = document.querySelector('.cc-scene-picker-list')
      const longest = []
      let last = performance.now()
      for (let i = 0; i < 60; i++) {
        list.scrollTop += 120
        await new Promise((resolve) => requestAnimationFrame(() => resolve()))
        const now = performance.now()
        longest.push(now - last)
        last = now
      }
      return Math.max(...longest)
    })
    const first = await t.page.locator('.cc-scene-picker-list [data-object]').nth(1).boundingBox()
    await t.page.mouse.move(first.x + 20, first.y + first.height / 2)
    await t.page.mouse.down()
    const start = Date.now()
    await t.page.mouse.move(first.x + 20, first.y + 300, { steps: 30 })
    const dragMs = (Date.now() - start) / 30
    await t.page.mouse.up()
    console.log(`  500 objects: longest scroll frame ${frames.toFixed(1)} ms, ${dragMs.toFixed(1)} ms per drag step`)
    assert.ok(frames < 50, `scroll frame ${frames} ms`)
    assert.ok(dragMs < 50, `drag step ${dragMs} ms`)
    objects.splice(1)
    return t
  },
}

let failed = 0
try {
  for (const [name, check] of Object.entries(checks)) {
    if (only && !name.includes(only)) continue
    let t
    try {
      t = await check()
      assert.deepEqual(t.errors, [], 'no page errors')
      console.log(`ok - ${name}`)
    } catch (e) {
      failed++
      console.error(`not ok - ${name}\n`, e)
    } finally {
      await t?.page.close()
    }
  }
} finally {
  await browser.close()
  await rm(fixture, { force: true })
}
if (failed) process.exit(1)
