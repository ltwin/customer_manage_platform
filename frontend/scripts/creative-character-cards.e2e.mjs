// Browser smoke test for the role card library flow. All HTTP is mocked.
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const browser = await chromium.launch()
const page = await browser.newPage({
  viewport: process.env.CHARACTER_TEST_MOBILE
    ? { width: 390, height: 844 }
    : { width: 1440, height: 900 },
})
page.setDefaultTimeout(8000)
const errors = []
page.on('pageerror', (error) => errors.push(error.stack ?? error.message))
const content = (id, kind, payload) => ({
  id,
  kind,
  payload,
  content_id: `content-${id}`,
  sequence: '1',
  truncated: false,
})
const image = content('ccrv_illustration', 'image', {})
image.media = [
  {
    role: 'original',
    blob_id: 'b',
    mime: 'image/png',
    byte_size: 20,
    width: 800,
    height: 1200,
    duration_ms: null,
  },
]
const asset = (id, title, body) => ({
  id,
  title,
  kind: body.kind,
  content: body,
  content_id: body.content_id,
  content_revision_id: body.id,
  revision: '1',
  description: '',
  unavailable: false,
  is_favorite: false,
  rating: null,
  created_at: '2026-11-10T00:00:00Z',
  deleted_at: null,
  purge_after: null,
  group_ids: [],
  tag_ids: [],
  palette: [],
})
const illustration = asset('art', '立绘', image)
const otherCharacter = {
  name: '云',
  ip: { name: '星环档案' },
  work: '第一卷',
  views: [{ content_revision_id: image.id, view: 'front' }],
}
const otherCard = {
  ...asset(
    'other-card',
    otherCharacter.name,
    content('ccrv_other', 'character', {
      character: otherCharacter,
    }),
  ),
  character: { ...otherCharacter, view_count: 1 },
}
const canvasNode = (id, type, title, body, x) => ({
  id,
  parent_id: null,
  metadata: {
    type_key: type,
    type_version: 1,
    title,
    intent: '',
    x,
    y: 120,
    width: 300,
    height: 220,
    z_order: 0,
  },
  data: {
    schema_version: 1,
    config: {},
    content_id: body.content_id,
    content_revision_id: body.id,
    selected_version_id: null,
    document_id: null,
  },
  status: {
    content_state: 'ready',
    generation_state: 'idle',
    active_execution_id: null,
    latest_execution_id: null,
    apply_state: null,
    error: null,
    status_revision: '1',
  },
  prompt: null,
  capabilities: {
    actions: [
      'move',
      'resize',
      'rename',
      'duplicate',
      'remove',
      'reference',
      ...(type === 'core.character'
        ? ['update_character']
        : ['edit', 'replace']),
    ],
    prompt_mode: type === 'core.character' ? 'unavailable' : 'draft',
    disabled_reason: null,
  },
  content: body,
  placement_revision: '1',
  data_revision: '1',
})
const canvas = {
  id: 'character-qa',
  project_id: 'qa-project',
  project_name: '角色卡验证',
  project_revision: '1',
  revision: '1',
  topology_revision: '1',
  archived: false,
  edges: [],
  node_inputs: [],
  changes: [],
  object_states: [],
  nodes: [canvasNode('photo', 'core.image', '画布立绘', image, 80)],
}
const graphActions = []
await page.addInitScript(() => {
  const fetch = window.fetch.bind(window)
  window.fetch = (input, init) => {
    if (!String(input).endsWith('/canvases/character-qa/events'))
      return fetch(input, init)
    return Promise.resolve(
      new Response(
        new ReadableStream({
          start(controller) {
            controller.enqueue(
              new TextEncoder().encode('event: invalidate\ndata: {}\n\n'),
            )
            init.signal.addEventListener(
              'abort',
              () => controller.error(new DOMException('Aborted', 'AbortError')),
              { once: true },
            )
          },
        }),
        { headers: { 'Content-Type': 'text/event-stream' } },
      ),
    )
  }
})
let card
const posts = []
const createResult = () => ({
  asset_id: card.id,
  content_revision_id: card.content_revision_id,
  revision: card.revision,
  library_revision: card.revision,
  title: card.title,
  description: '',
  tag_mapping: {},
  reused: false,
})

await page.route('**/api/v1/**', async (route) => {
  const request = route.request()
  const url = new URL(request.url())
  const path = url.pathname
  const json = (value, status = 200) => route.fulfill({ status, json: value })
  if (path.endsWith('/auth/refresh'))
    return json({
      access_token: 'character-test',
      token_type: 'Bearer',
      expires_in: 600,
    })
  if (path.endsWith('/me'))
    return json({
      id: 'qa-character',
      email: 'qa@example.test',
      created_at: '2026-11-10T00:00:00Z',
      timezone: 'Asia/Shanghai',
    })
  if (path.endsWith('/library-usage')) return json({}, 404)
  if (path.endsWith('/character-ips')) {
    return json({
      items: [
        {
          key: 'fate',
          name: 'Fate',
          builtin: true,
          character_count: card ? 1 : 0,
          works: ['Fate/Grand Order'],
        },
      ],
    })
  }
  if (path.endsWith('/content-revisions/ccrv_illustration')) return json(image)
  if (path.endsWith('/media-access-tickets')) {
    return json({
      url: 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/1Z4AAAAASUVORK5CYII=',
      expires_at: new Date(Date.now() + 60_000).toISOString(),
    })
  }
  if (path.endsWith('/generation/catalog'))
    return json({ models: [], disabled_reason: '' })
  if (path.endsWith('/versions'))
    return json({ items: [], selected_version_id: null, data_revision: '1' })
  if (path.endsWith('/canvases/character-qa')) {
    if (card && canvas.nodes.length === 1) {
      const pinned = content('ccrv_card1', 'character', {
        character: {
          ...card.content.payload.character,
          appearance: '青绿色短发',
        },
      })
      canvas.nodes.push({
        ...canvasNode('character-node', 'core.character', '云', pinned, 460),
        character: {
          name: '云',
          cover_revision_id: image.id,
          view_count: 1,
          asset_id: card.id,
          latest_revision_id: card.content_revision_id,
        },
      })
    }
    return json(canvas)
  }
  if (path.endsWith('/canvases/character-qa/commands')) {
    const actions = request.postDataJSON()?.payload?.actions ?? []
    graphActions.push(...actions)
    const update = actions.find((action) => action.type === 'update_character')
    if (update) {
      assert.equal(update.content_revision_id, card.content_revision_id)
      const node = canvas.nodes.find((node) => node.id === update.node_id)
      node.data.content_revision_id = update.content_revision_id
      node.content = card.content
      node.data_revision = '2'
      delete node.character.latest_revision_id
    }
    canvas.revision = '2'
    return json({
      change_id: 'cwch-test',
      result_revision: canvas.revision,
      result_topology_revision: canvas.topology_revision,
      object_results: [],
      created_ids: [],
      removed_ids: [],
      omitted_reference_ids: [],
      id_mapping: {},
      before_topology_revision: canvas.topology_revision,
    })
  }
  if (path.endsWith('/assets/characters')) {
    const { operation_id, payload } = request.postDataJSON()
    assert.equal(request.headers()['idempotency-key'], operation_id)
    posts.push(payload)
    const { character } = payload
    card = {
      ...asset(
        'card',
        character.name,
        content('ccrv_card1', 'character', { character }),
      ),
      character: {
        name: character.name,
        ip: character.ip,
        work: character.work,
        view_count: character.views.length,
      },
    }
    return json(createResult(), 201)
  }
  if (path.endsWith('/assets/card/character-revisions')) {
    const { payload } = request.postDataJSON()
    assert.equal(payload.expected_revision, '1')
    posts.push(payload)
    card = {
      ...card,
      revision: '2',
      content_revision_id: 'ccrv_card2',
      content: {
        ...card.content,
        id: 'ccrv_card2',
        sequence: '2',
        payload: { character: payload.character },
      },
    }
    return json(createResult())
  }
  if (path.endsWith('/assets/card')) return json(card)
  if (path.endsWith('/assets/art')) return json(illustration)
  if (path.endsWith('/assets')) {
    const kind = url.searchParams.get('kind')
    const ip = url.searchParams.get('character_ip')
    const work = url.searchParams.get('character_work')
    const items = [illustration, otherCard, card].filter(
      (item) =>
        item &&
        (!kind || item.kind === kind) &&
        (!ip || item.character?.ip?.name === ip) &&
        (!work || item.character?.work === work),
    )
    return json({
      items,
      next_cursor: '',
      total_count: items.length,
      library_revision: card?.revision ?? '1',
      facets: { kinds: { image: 1, character: card ? 2 : 1 }, tags: [] },
    })
  }
  return json({
    items: [],
    formats: [],
    asset_counts: {},
    hierarchy_revision: '1',
    library_revision: '1',
    next_cursor: '',
    total_count: 0,
  })
})

try {
  await page.goto(
    `${process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5188'}/creative/library`,
  )
  await page.getByRole('button', { name: '立绘', exact: true }).click()
  await page.getByRole('button', { name: '创建角色卡', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '新建角色卡' })
  await dialog.getByLabel('角色名').fill('云')
  await dialog.getByLabel('外貌描述').fill('青绿色短发')
  await dialog.getByRole('combobox', { name: '选择所属 IP' }).press('ArrowDown')
  await page.getByRole('option', { name: 'Fate', exact: true }).click()
  await dialog.getByLabel('作品', { exact: true }).fill('Fate/Grand Order')
  await dialog
    .getByRole('combobox', { name: '参考图 1 的视角' })
    .press('ArrowDown')
  await page.getByRole('option', { name: '正面', exact: true }).click()
  if (process.env.CHARACTER_TEST_SCREENSHOT)
    await page.screenshot({ path: process.env.CHARACTER_TEST_SCREENSHOT })
  await dialog.getByRole('button', { name: '创建角色卡', exact: true }).click()
  await page.getByText('Fate · Fate/Grand Order', { exact: true }).waitFor()
  assert.equal(
    posts[0].character.views[0].content_revision_id,
    'ccrv_illustration',
  )
  assert.equal(posts[0].character.views[0].view, 'front')
  await page.getByText('星环档案 · 第一卷', { exact: true }).waitFor()
  assert.equal(
    await page.getByRole('button', { name: '查看：云', exact: true }).count(),
    2,
  )

  const filters = page.getByRole('form', { name: '角色卡筛选' })
  await filters.getByLabel('所属 IP', { exact: true }).fill('Fate')
  await filters.getByLabel('作品', { exact: true }).fill('Fate/Grand Order')
  const filtered = page.waitForRequest((request) => {
    const url = new URL(request.url())
    return (
      url.pathname.endsWith('/assets') &&
      url.searchParams.get('character_ip') === 'Fate' &&
      url.searchParams.get('character_work') === 'Fate/Grand Order'
    )
  })
  await filters.getByRole('button', { name: '筛选', exact: true }).click()
  await filtered
  await page.getByRole('button', { name: '查看：云', exact: true }).click()
  await page.getByText('青绿色短发', { exact: true }).waitFor()
  await page.getByRole('button', { name: '编辑角色卡', exact: true }).click()
  const edit = page.getByRole('dialog', { name: '编辑角色卡' })
  await edit.getByLabel('外貌描述').fill('青绿色短发，银色胸针')
  const edited = page.waitForResponse((response) =>
    response.url().endsWith('/assets/card/character-revisions'),
  )
  await edit.getByRole('button', { name: '存为新版本', exact: true }).click()
  assert.equal((await edited).status(), 200)
  assert.equal(posts.at(-1).character.appearance, '青绿色短发，银色胸针')
  await page.getByText('青绿色短发，银色胸针', { exact: true }).waitFor()
  if (!process.env.CHARACTER_TEST_MOBILE) {
    await page.goto(
      `${process.env.CREATIVE_EDITOR_URL ?? 'http://localhost:5188'}/creative/canvases/character-qa`,
    )
    const characterNode = page.locator(
      '.react-flow__node[data-id="character-node"]',
    )
    await characterNode.getByText('角色卡有新版本').waitFor()
    await characterNode.click({ button: 'right' })
    const updated = page.waitForResponse((response) =>
      response.url().endsWith('/canvases/character-qa/commands'),
    )
    await page.getByRole('menuitem', { name: '更新角色卡版本' }).click()
    // The hint goes in the click's frame, before the queued command is sent.
    await characterNode
      .getByText('角色卡有新版本')
      .waitFor({ state: 'hidden', timeout: 500 })
    await updated
    assert.equal(graphActions[0].type, 'update_character')
    const imageNode = page.locator('.react-flow__node[data-id="photo"]')
    await imageNode.locator('.cc-node-header').click({ button: 'right' })
    await page.getByRole('menuitem', { name: '创建角色卡' }).click()
    const fromCanvas = page.getByRole('dialog', { name: '新建角色卡' })
    await fromCanvas
      .getByRole('combobox', { name: '参考图 1 的视角' })
      .waitFor()
  }
  assert.deepEqual(errors, [])
} catch (error) {
  console.error({
    errors,
    graphActions,
    posts,
    text: await page.locator('body').innerText(),
  })
  if (process.env.CHARACTER_TEST_SCREENSHOT)
    await page.screenshot({ path: '/tmp/character-cards-e2e-failure.png' })
  throw error
} finally {
  await browser.close()
}
