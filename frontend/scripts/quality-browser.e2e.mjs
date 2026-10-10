// Self-contained browser regression: npm dependencies only; no backend/accounts.
// Run with: node --test scripts/quality-browser.e2e.mjs
import assert from 'node:assert/strict'
import { before, after, test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
import { chromium } from 'playwright'

let server, browser, base
before(async () => {
  server = await createServer({
    root: fileURLToPath(new URL('../', import.meta.url)),
    server: { host: '127.0.0.1', port: 0 },
    plugins: [{ name: 'quality-harness', configureServer(vite) {
      vite.middlewares.use('/__quality__', async (_req, res) => {
        res.setHeader('Content-Type', 'text/html')
        res.end(await vite.transformIndexHtml('/__quality__', '<html><head></head><body><div id="root"></div><script type="module" src="/scripts/fixtures/quality-harness.tsx"></script></body></html>'))
      })
    } }],
  })
  await server.listen()
  base = `http://127.0.0.1:${server.httpServer.address().port}/__quality__`
  browser = await chromium.launch({ headless: true })
})
after(async () => { await browser?.close(); await server?.close() })

test('profile page mutation replaces the profile observed through the real provider', async () => {
  const page = await browser.newPage()
  const previous = { display_name: '旧名', profile_revision: 'pr-1', avatar_revision: 'ar-1', avatar_url: null, avatar_version: null }
  const next = { ...previous, display_name: '新名', profile_revision: 'pr-2', avatar_revision: 'ar-2', avatar_url: '/api/v1/account/profile/avatar/content?v=next', avatar_version: 'next' }
  let patches = 0
  await page.route('**/api/v1/account/profile', route => {
    if (route.request().method() === 'PATCH') { patches++; return route.fulfill({ json: next }) }
    return route.fulfill({ json: previous })
  })
  await page.route('**/api/v1/account/profile/avatar/content*', route => route.fulfill({ status: 404 }))
  try {
    await page.goto(base)
    await page.getByRole('textbox').fill('新名')
    await page.getByRole('button', { name: '保存展示名称' }).click()
    await page.waitForFunction(() => document.querySelector('[data-testid="profile"]')?.textContent?.includes('pr-2'))
    assert.deepEqual(JSON.parse(await page.getByTestId('profile').textContent()), next)
    assert.equal(patches, 1)
  } finally { await page.close() }
})

test('one failed node read leaves other nodes and creation editable; retry recovers', async () => {
  const page = await browser.newPage({ viewport: { width: 1500, height: 900 } })
  page.setDefaultTimeout(15000)
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  const makeNode = (id, x) => ({
    id, parent_id: null,
    metadata: { type_key: 'core.text', type_version: 1, title: id, intent: '', x, y: 120, width: 280, height: 180, z_order: 0 },
    data: { schema_version: 1, config: {}, content_id: `content-${id}`, content_revision_id: `revision-${id}`, selected_version_id: null, document_id: null },
    status: { content_state: 'ready', generation_state: 'idle', active_execution_id: null, latest_execution_id: null, apply_state: null, error: null, status_revision: '1' },
    prompt: null, capabilities: { actions: ['edit', 'rename', 'move', 'resize'], prompt_mode: 'draft', disabled_reason: null }, placement_revision: '1', data_revision: '1',
    content: { id: `revision-${id}`, content_id: `content-${id}`, kind: 'text', sequence: '1', truncated: false, payload: { body: `${id}内容` } },
  })
  const snapshot = { id: 'test-canvas', project_id: 'p', project_name: '读取隔离', project_revision: '1', revision: '1', topology_revision: '1', archived: false, nodes: [makeNode('bad', 80), makeNode('good', 480)], edges: [], node_inputs: [], changes: [], object_states: [] }
  let failing = true
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/events')) return route.fulfill({ contentType: 'text/event-stream', body: ': heartbeat\n\n' })
    if (path.endsWith('/canvases/test-canvas')) return route.fulfill({ json: snapshot })
    if (path.endsWith('/content-revisions/revision-bad') && failing) return route.fulfill({ status: 500, json: { error: { code: 'read_failed', message: '单个节点读取失败' } } })
    if (path.includes('/content-revisions/')) return route.fulfill({ json: snapshot.nodes.find(n => path.endsWith(n.data.content_revision_id)).content })
    if ((path.endsWith('/agent/catalog') || path.endsWith('/generation/catalog'))) return route.fulfill({ json: { models: [], skills: [] } })
    if (path.endsWith('/library-settings')) return route.fulfill({ json: { hierarchy_revision: '1', library_revision: '1', revision: '1', retention_days: 30 } })
    if (path.endsWith('/media-capabilities')) return route.fulfill({ status: 503, json: { error: { code: 'disabled', message: 'fixture' } } })
    return route.fulfill({ json: { items: [], total_count: 0, next_cursor: '', hierarchy_revision: '1', library_revision: '1' } })
  })
  try {
    await page.goto(`${base}?canvas`)
    const create = page.getByRole('button', { name: '新建节点', exact: true })
    await page.waitForFunction(() => !document.querySelector('button[aria-label="新建节点"]')?.disabled)
    await page.locator('.react-flow__node[data-id="bad"]').click()
    await page.getByRole('alert').filter({ hasText: '单个节点读取失败' }).waitFor()
    assert.equal(await create.isEnabled(), true)
    await page.locator('.react-flow__node[data-id="good"]').dblclick()
    await page.getByRole('textbox', { name: '正文', exact: true }).waitFor()
    assert.equal(await page.getByRole('textbox', { name: '正文', exact: true }).isEditable(), true)
    await page.keyboard.press('Escape')
    await page.locator('.react-flow__node[data-id="bad"]').click()
    await page.getByRole('alert').filter({ hasText: '单个节点读取失败' }).waitFor()
    const layers = await page.evaluate(() => ({
      notice: Number(getComputedStyle(document.querySelector('.cc-save-status')).zIndex),
      panel: Number(getComputedStyle(document.querySelector('.cc-generation')).zIndex),
    }))
    assert.ok(layers.notice > layers.panel, 'recovery actions must remain above the generation panel')
    failing = false
    const recovered = page.waitForResponse(response => response.url().endsWith('/content-revisions/revision-bad') && response.status() === 200, { timeout: 15000 }).catch(error => error)
    await page.getByRole('alert').filter({ hasText: '单个节点读取失败' }).getByRole('button', { name: '重试' }).click()
    const recovery = await recovered
    if (recovery instanceof Error) throw recovery
    await page.getByRole('alert').filter({ hasText: '单个节点读取失败' }).waitFor({ state: 'hidden' })
    await page.waitForFunction(() => !document.querySelector('button[aria-label="新建节点"]')?.disabled)
    assert.equal(await create.isEnabled(), true)
    assert.deepEqual(errors, [])
  } finally { await page.close() }
})
