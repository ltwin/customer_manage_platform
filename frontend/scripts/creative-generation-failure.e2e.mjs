// Browser regression for the generation failure state (owner 2026-10-16):
// the latest failed generation shows on the card instead of an empty node,
// following-latest cards cover old content, pinned cards only mark the version
// strip, retry reuses the panel's generate path, and a user stop is not a failure.
// All API traffic is mocked. Run against the Vite dev server:
// node scripts/creative-generation-failure.e2e.mjs
// CREATIVE_QA_CHECK=<名称片段> 只跑部分检查。
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const base = process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5173'
const only = process.env.CREATIVE_QA_CHECK ?? ''
const browser = await chromium.launch({ headless: true })
const errors = []
let context, page, server

const art = `data:image/svg+xml,${encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="400" height="300"><rect width="400" height="300" fill="#369"/></svg>')}`
const status = (extra = {}) => ({ content_state: 'empty', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1', ...extra })
const failed = (error, extra = {}) => status({ generation_state: 'failed', latest_execution_id: 'e-old', apply_state: 'discarded', error, ...extra })
const caps = ['move', 'resize', 'rename', 'duplicate', 'remove', 'edit', 'reference', 'maximize', 'download', 'replace', 'save_to_library']
const prompt = (id, model) => ({ draft_id: `d-${id}`, draft_revision: '1', action_key: 'gateway.generation', text: '秋日森林里的摄影布景', model_key: model, parameters: {}, references: [] })
const textNode = (id, x, nodeStatus) => ({
  id, parent_id: null,
  metadata: { type_key: 'core.text', type_version: 1, title: id, intent: '', x, y: 160, width: 300, height: 180, z_order: 0 },
  prompt: prompt(id, 'qa-text'),
  capabilities: { actions: caps, prompt_mode: 'draft', disabled_reason: null },
  status: nodeStatus,
  placement_revision: '1', data_revision: '1',
  data: { schema_version: 1, config: {}, content_id: null, content_revision_id: null, selected_version_id: null, document_id: null },
})
const imageContent = (rev) => ({ id: rev, content_id: 'c-photo', kind: 'image', sequence: '1', payload: {}, truncated: false,
  media: [{ role: 'original', blob_id: `b-${rev}`, mime: 'image/png', byte_size: 2000, width: 400, height: 300, duration_ms: null },
    { role: 'display', blob_id: `d-${rev}`, mime: 'image/jpeg', byte_size: 1000, width: 400, height: 300, duration_ms: null }] })
const imageNode = (id, x, nodeStatus, { pinned = false } = {}) => ({
  id, parent_id: null,
  metadata: { type_key: 'core.image', type_version: 1, title: id, intent: '', x, y: 160, width: 400, height: 300, z_order: 0 },
  prompt: prompt(id, 'qa-image'),
  capabilities: { actions: caps, prompt_mode: 'draft', disabled_reason: null },
  status: { ...nodeStatus, content_state: 'ready' },
  placement_revision: '1', data_revision: '1',
  data: { schema_version: 1, config: pinned ? { main_version_mode: 'pinned' } : {}, content_id: 'c-photo', content_revision_id: 'r-1', selected_version_id: pinned ? 'v-1' : null, document_id: null },
  content: imageContent('r-1'),
  version_summary: { count: 1, main_ordinal: 1, main_model_key: 'qa-image' },
})
const version = { id: 'v-1', node_id: 'photo', version_no: '1', origin_kind: 'generation', content_revision_id: 'r-1', execution_id_snapshot: 'e-0', output_ordinal: 0, generation_provenance: {}, created_at: '2026-10-16T00:00:00Z', revision: '1', inputs: [] }

async function fresh(nodes) {
  await context?.close()
  context = await browser.newContext({ viewport: { width: 1600, height: 1000 } })
  page = await context.newPage()
  page.setDefaultTimeout(8000)
  page.on('pageerror', (error) => errors.push(error.message))
  const canvas = { id: 'qa', project_id: 'qa-project', project_name: '生成失败回归', project_revision: '1', revision: '1', topology_revision: '1', archived: false, nodes, edges: [], node_inputs: [], changes: [], object_states: [] }
  server = { canvas, posts: [] }
  await page.addInitScript(() => {
    const original = window.fetch.bind(window)
    window.fetch = async (input, init) => {
      if (!String(input).endsWith('/canvases/qa/events')) return original(input, init)
      return new Response(new ReadableStream({
        start(controller) {
          controller.enqueue(new TextEncoder().encode('event: invalidate\ndata: {}\n\n'))
          init.signal.addEventListener('abort', () => controller.error(new DOMException('Aborted', 'AbortError')), { once: true })
        },
      }), { headers: { 'Content-Type': 'text/event-stream' } })
    }
  })
  const execution = (id, node, state, apply, error = '') => ({ execution_id: id, node_id: node, canvas_id: 'qa', action_key: 'gateway.generation', executor_version: '1', state, apply_state: apply, execution_epoch: 1, deadline: new Date(Date.now() + 3600000).toISOString(), change_id: null, error, created_at: new Date().toISOString() })
  await page.route('**/api/v1/**', async (route) => {
    const req = route.request()
    const path = new URL(req.url()).pathname
    const json = (value, code = 200) => route.fulfill({ status: code, json: value })
    if (path.endsWith('/auth/refresh')) return json({ access_token: 'qa', token_type: 'Bearer', expires_in: 600 })
    if (path.endsWith('/me')) return json({ id: 'qa-fail', email: 'qa@example.test', created_at: '2026-01-01T00:00:00Z', timezone: 'Asia/Shanghai' })
    if (path.endsWith('/generation/catalog')) return json({ models: [
      { key: 'qa-text', label: '文字模型', kind: 'text', configuration_revision: '1', parameters: [] },
      { key: 'qa-image', label: '图片模型', kind: 'image', configuration_revision: '1', parameters: [] }], disabled_reason: '' })
    if (path.endsWith('/media-access-tickets')) return json({ url: art, expires_at: new Date(Date.now() + 3600000).toISOString() })
    if (path.endsWith('/content-revisions/r-1')) return json(imageContent('r-1'))
    if (path.endsWith('/content-revisions/out-2')) return json({ id: 'out-2', content_id: 'c-out', kind: 'text', sequence: '1', payload: { body: '重试后的作品' }, truncated: false, media: [] })
    const versionsMatch = path.match(/\/nodes\/([^/]+)\/versions$/)
    if (versionsMatch) {
      const n = canvas.nodes.find((x) => x.id === versionsMatch[1])
      // 版本页的 selected_version_id 是生效主版本：自动跟随时也指向最新一版。
      return json({ items: n?.content && n.metadata.type_key === 'core.image' ? [version] : [], selected_version_id: n?.content && n.metadata.type_key === 'core.image' ? 'v-1' : null, data_revision: n?.data_revision ?? '1' })
    }
    if (path.endsWith('/executions') && req.method() === 'POST') {
      const v = req.postDataJSON().payload
      server.posts.push(v)
      const n = canvas.nodes.find((x) => x.id === v.node_id)
      assert.equal(v.draft_revision, n.prompt.draft_revision, 'retry uses the current prompt draft')
      // 重试成功：节点换成新作品，失败的那次不再是最近一次。
      n.status = status({ content_state: 'ready', generation_state: 'succeeded', latest_execution_id: 'e-2', apply_state: 'applied', status_revision: '2' })
      n.data = { ...n.data, content_id: 'c-out', content_revision_id: 'out-2' }
      n.content = { id: 'out-2', content_id: 'c-out', kind: 'text', sequence: '1', payload: { body: '重试后的作品' }, truncated: false, media: [] }
      canvas.revision = String(Number(canvas.revision) + 1)
      return json(execution('e-2', n.id, 'succeeded', 'applied'))
    }
    if (path.endsWith('/executions/e-2')) return json(execution('e-2', 'draft', 'succeeded', 'applied'))
    if (path.endsWith('/executions/e-old')) {
      const n = canvas.nodes.find((x) => x.status.latest_execution_id === 'e-old')
      return json(execution('e-old', n?.id ?? 'x', n?.status.generation_state ?? 'failed', n?.status.apply_state ?? 'discarded', n?.status.error ?? ''))
    }
    if (path.endsWith('/canvases/qa')) return json(canvas)
    if (path.endsWith('/library-settings')) return json({ revision: '1', view_mode: 'grid', sort: 'updated_desc', thumbnail_size: 'medium' })
    if (path.endsWith('/media-capabilities')) return json({ formats: [], image_max_bytes: 1000000, av_max_bytes: 10000000, batch_limit: 10 })
    return json({ items: [], next_cursor: '', total_count: 0, library_revision: '1' })
  })
  await page.goto(`${base}/creative/canvases/qa`)
  await page.locator('.react-flow__node').first().waitFor()
}

const card = (id) => page.locator(`.react-flow__node[data-id="${id}"]`)
const overlay = (id) => card(id).locator('.cc-generation-failed')
const strip = (id) => card(id).getByRole('group', { name: '作品版本' })

const checks = {
  async '首次生成失败显示失败态并可在卡片上重试'() {
    await fresh([textNode('draft', 100, failed('generation_content_rejected'))])
    await overlay('draft').waitFor()
    assert.match(await overlay('draft').innerText(), /内容未通过审核/)
    assert.equal(await card('draft').locator('.cc-generating').count(), 0)
    await page.screenshot({ path: '/tmp/creative-generation-failure-text.png' })
    await overlay('draft').getByRole('button', { name: '重试' }).click()
    const panel = page.getByRole('complementary', { name: '节点生成' })
    await panel.waitFor()
    await page.waitForFunction(() => !document.querySelector('.react-flow__node[data-id="draft"] .cc-generation-failed'))
    assert.equal(server.posts.length, 1, 'one retry is one generation request')
    assert.equal(server.posts[0].node_id, 'draft')
    await card('draft').getByText('重试后的作品').waitFor()
  },
  async '面板状态与失败原因'() {
    await fresh([textNode('draft', 100, failed('generation_timeout'))])
    await card('draft').locator('.cc-node-header').click()
    const panel = page.getByRole('complementary', { name: '节点生成' })
    await panel.getByRole('status').filter({ hasText: '生成失败' }).first().waitFor()
    await panel.getByText('生成超时，可以重试').waitFor()
    assert.equal(server.posts.length, 0, 'opening the panel never retries by itself')
  },
  async '跟随最新时失败态盖住旧版本，版本条可切回'() {
    await fresh([imageNode('photo', 100, failed('generation_failed'))])
    await overlay('photo').waitFor()
    await card('photo').locator('.cc-node-header').click()
    const tiles = strip('photo').getByRole('button')
    await strip('photo').getByRole('button', { name: '最近一次生成失败' }).waitFor()
    assert.equal(await strip('photo').getByRole('button', { name: '最近一次生成失败' }).getAttribute('aria-pressed'), 'true')
    await strip('photo').getByText('最近一次生成失败').waitFor()
    assert.equal(await card('photo').getByRole('button', { name: '重试' }).count(), 1, 'one retry entry per card')
    await page.screenshot({ path: '/tmp/creative-generation-failure-image.png' })
    await strip('photo').getByRole('button', { name: '版本 1（主版本）' }).click()
    await overlay('photo').waitFor({ state: 'detached' })
    assert.equal(await strip('photo').getByRole('button', { name: '最近一次生成失败' }).getAttribute('aria-pressed'), 'false')
    assert(await tiles.count() >= 2)
    await strip('photo').getByRole('button', { name: '最近一次生成失败' }).click()
    await overlay('photo').waitFor()
  },
  async '固定主版本时卡片不变，只在版本条标出失败'() {
    await fresh([imageNode('photo', 100, failed('generation_failed'), { pinned: true })])
    await card('photo').locator('.cc-node-header').click()
    await strip('photo').getByRole('button', { name: '最近一次生成失败' }).waitFor()
    assert.equal(await overlay('photo').count(), 0, 'pinned main version keeps the card')
    assert.equal(await strip('photo').getByRole('button', { name: '最近一次生成失败' }).getAttribute('aria-pressed'), 'false')
    await strip('photo').getByRole('button', { name: '最近一次生成失败' }).click()
    await overlay('photo').waitFor()
  },
  async '主动停止不算失败'() {
    await fresh([textNode('stopped', 100, status({ generation_state: 'cancelled', latest_execution_id: 'e-old', apply_state: 'discarded' }))])
    await card('stopped').locator('.cc-node-empty').waitFor()
    assert.equal(await overlay('stopped').count(), 0)
  },
}

let failedCheck = ''
try {
  for (const [name, run] of Object.entries(checks)) {
    if (only && !name.includes(only)) continue
    failedCheck = name
    await run()
    console.log(`ok - ${name}`)
  }
  failedCheck = ''
  assert.deepEqual(errors, [])
} catch (e) {
  await page?.screenshot({ path: `/tmp/creative-generation-failure-${failedCheck}.png` }).catch(() => {})
  throw e
} finally {
  await browser.close()
}
