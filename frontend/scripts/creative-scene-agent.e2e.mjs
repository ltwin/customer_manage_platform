// C3 in a real browser, against mocked endpoints: the assistant's
// screenshots (render_view) taken offscreen, the workspace page's claim loop
// running jobs with the executor loaded on demand, and the assistant panel's
// screenshots and change list.
// Run with the frontend Vite server on CREATIVE_SCENE_URL (default :5179):
//   node scripts/creative-scene-agent.e2e.mjs   (SCENE_AGENT_E2E_ONLY=<part of a name> runs matching checks)
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { rm, writeFile } from 'node:fs/promises'
import { chromium } from 'playwright'
import { defaultModuleCode } from '../src/creative-scene/sandbox/template.ts'

const DOC = 'cwdoc_scene_agent'
const MODEL = 'ccrv_pillar'
const auth = `setAuthenticated({access_token:'local-test',expires_in:3600,token_type:'Bearer'},{id:'qa-account',username:'qa'});`
const renderFixture = new URL('../.scene-agent-qa.html', import.meta.url)
const hostFixture = new URL('../.scene-agent-host-qa.html', import.meta.url)
const panelFixture = new URL('../.scene-agent-panel-qa.html', import.meta.url)
const noticeVersion = /egressNoticeVersion = '([^']+)'/.exec(readFileSync(new URL('../src/creative-canvas/editor/agentState.ts', import.meta.url), 'utf8'))[1]
await writeFile(
  renderFixture,
  `<html><body><script type="module">
import {setAuthenticated} from '/src/auth/session.ts';
import {renderView} from '/src/creative-scene/host/render.ts';
${auth}
window.__render = async (request) => {
  const shot = await renderView('${DOC}', request);
  if (!shot.ok) return shot;
  const bitmap = await createImageBitmap(shot.image);
  const canvas = new OffscreenCanvas(bitmap.width, bitmap.height);
  const ctx = canvas.getContext('2d');
  ctx.drawImage(bitmap, 0, 0);
  const px = (x, y) => Array.from(ctx.getImageData(x, y, 1, 1).data.slice(0, 3));
  // Squared luminance gradient: blur spreads edges thin and lowers it.
  const d = ctx.getImageData(0, 0, bitmap.width, bitmap.height).data;
  const lum = (i) => d[i] * 0.299 + d[i + 1] * 0.587 + d[i + 2] * 0.114;
  let energy = 0;
  for (let y = 0; y < bitmap.height - 1; y++) for (let x = 0; x < bitmap.width - 1; x++) {
    const i = (y * bitmap.width + x) * 4;
    energy += (lum(i) - lum(i + 4)) ** 2 + (lum(i) - lum(i + bitmap.width * 4)) ** 2;
  }
  return {
    energy: energy / (bitmap.width * bitmap.height),
    ok: true, type: shot.image.type, revision: shot.revision, width: bitmap.width, height: bitmap.height,
    center: px(bitmap.width >> 1, bitmap.height >> 1), corner: px(4, 4), canvases: document.querySelectorAll('canvas').length,
    // Points the request names in fractions of the picture.
    probes: (request.probes ?? []).map(([x, y]) => px(Math.floor(x * bitmap.width), Math.floor(y * bitmap.height))),
  };
};
window.__ready = true;
</script></body></html>`,
)
// The workspace page's renderer as WorkspacePage mounts it (useSceneHost), without the rest of the page.
await writeFile(
  hostFixture,
  `<html><body><div id="root"></div><script type="module">
import React from 'react';
import {createRoot} from 'react-dom/client';
import {setAuthenticated} from '/src/auth/session.ts';
import {notifyCanvasInvalidate} from '/src/creative-canvas/editor/canvasEvents.ts';
import {useSceneHost} from '/src/creative-scene/host/launcher.ts';
${auth}
window.__invalidate = notifyCanvasInvalidate;
function Host() {
  const {state} = useSceneHost('qa-account');
  return React.createElement('p', {id: 'host'}, state.holding ? 'holding:' + state.holding.kind : state.capable ? 'idle' : 'incapable');
}
createRoot(document.getElementById('root')).render(React.createElement(Host));
window.__ready = true;
</script></body></html>`,
)

// The assistant panel on a canvas with the scene's node; window.__scene() stands in for an open scene editor.
await writeFile(
  panelFixture,
  `<html><body><div id="root" class="cc-workspace"></div><script type="module">
import React from 'react';
import {createRoot} from 'react-dom/client';
import '/src/index.css';
import '/src/creative-canvas/editor/workspace.css';
import AgentPanel from '/src/creative-canvas/editor/AgentPanel.tsx';
import {setSceneContext} from '/src/creative-scene/context.ts';
import {setAuthenticated} from '/src/auth/session.ts';
${auth}
localStorage.setItem('creative-egress-notice:qa-account', '${noticeVersion}');
window.__scene = setSceneContext;
const nodes = [{id: 'node-scene', metadata: {title: '棚拍布景'}, data: {document_id: '${DOC}'}}];
createRoot(document.getElementById('root')).render(React.createElement(AgentPanel, {account: 'qa-account', canvasID: 'canvas', defaultOpen: true, canvas: {nodes}}));
window.__ready = true;
</script></body></html>`,
)

const base = process.env.CREATIVE_SCENE_URL || 'http://localhost:5179'
const browser = await chromium.launch({ headless: true, args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'] })
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
const testdata = new URL('../../backend/internal/creativeglb/testdata/', import.meta.url)
const pillarGLB = readFileSync(new URL('exported-template.glb', testdata))
const pillarManifest = JSON.parse(readFileSync(new URL('cases.json', testdata), 'utf8')).find((c) => c.name === 'exported-template').manifest
const identity = () => ({ position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] })
// A saturated background (#3355ff), so "the model is there" reads off one
// pixel. Screenshots draw in the clay look, whose tone mapping takes the
// background with everything else: #3355ff comes out about (62, 85, 227).
const background = [62, 85, 227]
const isBackground = (px) => px.every((v, i) => Math.abs(v - background[i]) < 24)
const threeURL = (u) => /\/(three|@react-three)[/_.]/.test(u) || /deps\/(three|@react-three)/.test(u)

function sceneDoc() {
  return {
    id: DOC, title: '棚拍布景', type_key: 'scene', content_revision_id: 'ccrv_scene_7', revision: '7', unit_revs: {}, trashed_at: null,
    created_at: '2026-10-18T00:00:00Z', updated_at: '2026-10-18T00:00:00Z',
    scene: {
      environment: { sky: { kind: 'color', color: '#3355ff' }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null },
      render: { mode: 'clay' },
      objects: [
        { id: 'sobj_pillar', kind: 'model_instance', name: '柱', parent_id: null, order: 0, transform: identity(), visible: true, locked: false, model: { content_revision_id: MODEL }, material_slots: {} },
        {
          id: 'sobj_cam', kind: 'camera', name: '机位', parent_id: null, order: 1, transform: { ...identity(), position: [0, 1.5, 8] }, visible: true, locked: false,
          camera: { focal_mm: 35, sensor_mm: [36, 24], aspect: '3:2', near: 0.1, far: 200, label: '' },
        },
        // Depth of field on, focused 2 m ahead: the pillar 8 m away is out of focus.
        {
          id: 'sobj_dof', kind: 'camera', name: '景深机位', parent_id: null, order: 2, transform: { ...identity(), position: [0, 1.5, 8] }, visible: true, locked: false,
          camera: { focal_mm: 85, sensor_mm: [36, 24], aspect: '3:2', near: 0.1, far: 200, label: '', depth_of_field: true, f_stop: 1.4, focus: 2, focus_object_id: '' },
        },
      ],
    },
    models: { [MODEL]: { content_id: 'ccnt_pillar', usable: true, manifest: pillarManifest, generator: null, name_hint: '柱' } },
  }
}

/**
 * What the assistant writes for 「把这个场景改成黄昏、墙用石砖、窗户用彩色玻璃、
 * 加两盏暖色壁灯」(AC-8): a low sun, a brick wall with a tinted glass pane in
 * front of it, two warm wall lights and the exposure dusk asks for.
 */
function duskDoc() {
  const doc = sceneDoc()
  const box = (id, name, order, position, size, slot) => ({
    id, kind: 'primitive', name, parent_id: null, order, transform: { ...identity(), position }, visible: true, locked: false,
    primitive: { builtin: 'box@1', params: { width: size[0], height: size[1], depth: size[2] } }, material_slots: { main: slot },
  })
  const lamp = (id, order, x) => ({
    id, kind: 'light', name: '壁灯', parent_id: null, order, transform: { ...identity(), position: [x, 2, -1.6] }, visible: true, locked: false,
    light: { type: 'point', color: '#ffb46b', intensity: 40, cast_shadow: false, distance: 0, decay: 2 },
  })
  doc.scene.environment = { sky: { kind: 'procedural', sun_elevation_deg: 4, sun_azimuth_deg: 250, turbidity: 4 }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null }
  doc.scene.render = { mode: 'lit', exposure_ev: 2 }
  doc.scene.objects = [
    ...doc.scene.objects.filter((o) => o.kind === 'camera'),
    { id: 'sobj_ground', kind: 'primitive', name: '地面', parent_id: null, order: 3, transform: identity(), visible: true, locked: true, primitive: { builtin: 'ground@1', params: { size: 40 } }, material_slots: {} },
    box('sobj_wall', '墙', 4, [0, 0, -2], [6, 3, 0.2], { preset: 'stone_brick@1', params: {} }),
    box('sobj_window', '窗', 5, [0, 1, -1.8], [1.2, 1.2, 0.05], { preset: 'principled@1', params: { color: '#5a8fd0', transmission: 0.9, roughness: 0.05, thickness: 0.01 } }),
    lamp('sobj_lampa', 6, -2),
    lamp('sobj_lampb', 7, 2),
  ]
  doc.models = {}
  return doc
}

/** The job queue, staged uploads and one scene, shared by every tab of a browser context. */
class Server {
  constructor() {
    this.doc = sceneDoc()
    this.queue = []
    this.claims = []
    this.leases = []
    this.completes = []
    this.uploads = new Map()
    this.media = 0
    // Set to a promise to keep uploads from completing until it settles.
    this.uploadHold = null
  }
  bake(id) {
    const job = {
      id, document_id: DOC, kind: 'bake_model', attempt: 0, claim_token: `tok_${id}`, lease_until: '2100-01-01T00:00:00Z', deadline: '2100-01-01T00:00:00Z',
      request: { code: defaultModuleCode, params: { height: 2.4 }, lib_version: 1, target: { op: 'add', object: { id: 'sobj_new', name: '柱', parent_id: null, transform: identity() } }, expected: {} },
    }
    this.queue.push(job)
    return job
  }
  render(id, request) {
    const job = { id, document_id: DOC, kind: 'render_view', attempt: 0, claim_token: `tok_${id}`, lease_until: '2100-01-01T00:00:00Z', deadline: '2100-01-01T00:00:00Z', request: { mode: 'clay', ...request } }
    this.queue.push(job)
    return job
  }
  export(id, request) {
    const job = { id, document_id: DOC, kind: 'export_shots', attempt: 0, claim_token: `tok_${id}`, lease_until: '2100-01-01T00:00:00Z', deadline: '2100-01-01T00:00:00Z', request }
    this.queue.push(job)
    return job
  }
  async completed(id) {
    for (let i = 0; i < 300; i++) {
      const done = this.completes.find((c) => c.id === id)
      if (done) return done
      await sleep(100)
    }
    throw new Error(`${id} was not completed`)
  }
}

async function route(context, server) {
  await context.route('**/api/v1/creative/**', async (r) => {
    const request = r.request()
    const path = new URL(request.url()).pathname.replace('/api/v1/creative', '')
    const json = (status, body) => r.fulfill({ status, json: body })
    if (path === '/scene-jobs/claim') {
      const { document_id: documentID, kinds } = request.postDataJSON()
      server.claims.push(documentID ?? null)
      const i = server.queue.findIndex((j) => (!documentID || j.document_id === documentID) && kinds.includes(j.kind))
      if (i < 0) return r.fulfill({ status: 204, body: '' })
      return json(200, { job: server.queue.splice(i, 1)[0] })
    }
    const job = path.match(/^\/scene-jobs\/([^/]+)\/(lease|complete)$/)
    if (job && job[2] === 'lease') {
      server.leases.push(job[1])
      return json(200, { lease_until: '2100-01-01T00:00:00Z' })
    }
    if (job) {
      const { payload } = request.postDataJSON()
      server.completes.push({ id: job[1], payload })
      return json(200, { job_id: job[1], state: payload.outcome, result: {} })
    }
    if (path === `/documents/${DOC}` && request.method() === 'GET') return json(200, server.doc)
    if (path === '/media-access-tickets' && request.method() === 'POST')
      return json(200, { url: `${base}/__media/${MODEL}`, expires_at: '2100-01-01T00:00:00Z', mime: 'model/gltf-binary', byte_size: pillarGLB.byteLength })
    if (path === '/uploads' && request.method() === 'POST') {
      const { payload } = request.postDataJSON()
      const id = `ccup_${server.uploads.size + 1}`
      const upload = {
        id, state: 'uploading', io_phase: 'none', revision: '1', kind: payload.kind, mime: payload.mime, size: payload.size,
        file_name: payload.file_name, part_size: payload.size, part_count: 1, uploaded_parts: [], target_kind: payload.target.kind,
        expires_at: '2100-01-01T00:00:00Z', error_code: '', publication: null, binding: null, content_revision_id: null, created_at: '2026-10-18T00:00:00Z',
      }
      server.uploads.set(id, upload)
      return json(201, upload)
    }
    const upload = path.match(/^\/uploads\/([^/]+)(\/part-authorizations|\/complete)?$/)
    if (upload) {
      const u = server.uploads.get(upload[1])
      if (upload[2] === '/part-authorizations')
        return json(200, { upload_id: u.id, parts: [{ part_number: 1, method: 'PUT', url: `${base}/__upload/${u.id}`, headers: {}, expires_at: '2100-01-01T00:00:00Z' }] })
      if (upload[2] === '/complete') {
        if (server.uploadHold) await server.uploadHold
        Object.assign(u, { state: 'ready', publication: { kind: 'candidate', id: `ccuc_${u.id}`, revision: '1' } })
      }
      return json(200, u)
    }
    return json(404, { error: { code: 'not_found', message: `unmocked ${path}` } })
  })
  await context.route('**/__upload/**', (r) => r.fulfill({ status: 200, body: '' }))
  await context.route('**/__media/**', (r) => {
    server.media++
    return r.fulfill({ status: 200, contentType: 'model/gltf-binary', body: pillarGLB })
  })
}

async function tab(context, fixture) {
  const page = await context.newPage()
  const errors = []
  const requested = []
  page.on('request', (r) => requested.push(r.url()))
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('console', (m) => {
    if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(m.text())
    // Chrome drops the oldest context past its cap (16) and says so.
    if (/Too many active WebGL contexts/.test(m.text())) errors.push(m.text())
  })
  await page.goto(`${base}/${fixture}`)
  await page.waitForFunction(() => window.__ready, null, { timeout: 30_000 })
  return { page, errors, requested }
}

async function openRender() {
  const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
  const server = new Server()
  await route(context, server)
  const t = await tab(context, '.scene-agent-qa.html')
  return { ...t, context, server, render: (request) => t.page.evaluate((q) => window.__render(q), request) }
}


// --- the panel's mocked conversation: one finished scene run ---------------

const pixel = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='
const chairBatch = { from_revision: '1', units: ['objects', 'object:sobj_chair000000000000000000', 'object:sobj_chair000000000000000000:content'], expected: { objects: '4', 'object:sobj_chair000000000000000000': '3', 'object:sobj_chair000000000000000000:content': '3' } }
const tableBatch = { from_revision: '2', units: ['object:sobj_table000000000000000000'], expected: { 'object:sobj_table000000000000000000': '4' } }
const lampBatch = { from_revision: '2', units: ['object:sobj_lamp0000000000000000000'], expected: { 'object:sobj_lamp0000000000000000000': '4' } }
const runBase = {
  conversation_id: 'c', canvas_id: 'canvas', egress_consent_id: 'consent', model_key: 'vision', kind: 'chat', settlement_state: 'settled',
  limits_version: '1', revision: '1', last_event_seq: '9', pruned_through_seq: '0', deadline_at: '2099-01-01T00:00:00Z', created_at: '2026-10-18T00:00:00Z',
}
const toolBlock = (n, key, title) => ({
  id: `ccbk_${n}`, run_id: 'run', ordinal: String(n), type: 'tool', state: 'succeeded', revision: '2', payload_version: 1, hidden: false,
  payload: { step_id: `step${n}`, tool_key: key, title, started_at: '2026-10-18T00:00:00Z', duration_ms: 1200, detail_ref: `step${n}`, node_refs: [] },
  started_at: '2026-10-18T00:00:00Z', finished_at: '2026-10-18T00:00:02Z',
})
const panelBlocks = [
  toolBlock(1, 'write_model', '生成模型'),
  toolBlock(2, 'edit_scene', '修改场景'),
  toolBlock(3, 'render_view', '截图回看'),
  { id: 'ccbk_4', run_id: 'run', ordinal: '4', type: 'text', state: 'complete', revision: '2', payload_version: 1, hidden: false, payload: { markdown: '搭好了：椅子、桌子和台灯。' }, started_at: '2026-10-18T00:00:03Z', finished_at: '2026-10-18T00:00:04Z' },
]
const panelDetails = {
  ccbk_1: { tool_key: 'write_model', arguments: { document_id: DOC, code: '…' }, result: '{"status":"ok"}' },
  ccbk_2: { tool_key: 'edit_scene', arguments: { document_id: DOC, based_on_revision: 2, operations: [] }, result: '{"written_revision":4}' },
  ccbk_3: { tool_key: 'render_view', arguments: { document_id: DOC, preset: 'overview' }, result: '{"image_revision_id":"ccrv_shot","content_revision_id":"ccrv_shot","scene_revision":4}' },
}

class PanelServer {
  constructor() {
    this.tickets = []
    this.ticketMissing = false
    this.commands = []
    this.created = []
    this.changeReads = 0
    this.restored = new Set()
    this.undoAll = [tableBatch]
    // Statuses for the next commands, in order; past the end, 200.
    this.answers = []
    this.revision = 7
  }
  changes() {
    const chair = this.restored.has('chair')
      ? { restore: null, blocked: { reason: 'changed_by_others', revision: '8', actor_kind: 'photographer' } }
      : { restore: chairBatch, blocked: null }
    return {
      run_id: 'run', document_id: DOC, revisions: ['3', '4'],
      entries: [
        { target: { kind: 'object', object_id: 'sobj_chair000000000000000000' }, name: '椅子', change: 'added', ...chair },
        { target: { kind: 'object', object_id: 'sobj_table000000000000000000' }, name: '桌子', change: 'modified', restore: tableBatch, blocked: null },
        { target: { kind: 'object', object_id: 'sobj_lamp0000000000000000000' }, name: '台灯', change: 'modified', restore: null, blocked: { reason: 'changed_by_others', revision: '5', actor_kind: 'photographer' } },
      ],
      undo_all: this.undoAll,
    }
  }
}

async function routePanel(context, panel) {
  await context.route('**/api/v1/creative/**', async (r) => {
    const request = r.request()
    const url = new URL(request.url())
    const path = url.pathname.replace('/api/v1/creative', '')
    const json = (body, status = 200) => r.fulfill({ status, json: body })
    if (path === '/media-access-tickets') {
      const { content_revision_id, role, purpose } = request.postDataJSON()
      panel.tickets.push({ content_revision_id, role, purpose })
      if (panel.ticketMissing) return json({ error: { code: 'not_found', message: '内容不存在' } }, 404)
      return json({ url: pixel, expires_at: '2099-01-01T00:00:00Z', mime: 'image/jpeg', byte_size: 1 })
    }
    if (path === '/agent/catalog')
      return json({ limits: { attachments: 10, image_preview_count: 8 }, skills: [], models: [{ model_key: 'vision', display_name: '看图模型', available: true, capability: { image_input: true }, context_window_tokens: 100_000 }] })
    if (path.endsWith('/conversations')) return json({ items: [{ id: 'c', title: '搭景' }] })
    if (path.endsWith('/conversations/c/egress-consents') && request.method() === 'POST')
      return json({ id: 'consent-new', conversation_id: 'c', revision: '1', purpose: 'creative_assistance', policy_version: noticeVersion, scope: {}, granted_at: '2026-10-18T00:00:00Z' }, 201)
    if (path.endsWith('/conversations/c/runs') && request.method() === 'POST') {
      panel.created.push(request.postDataJSON())
      return json({ ...runBase, id: 'run9', trigger_message_id: 'm9', state: 'queued' }, 202)
    }
    if (path.endsWith('/conversations/c/runs')) return json({ items: [{ ...runBase, id: 'run', trigger_message_id: 'm1', state: 'succeeded', finished_at: '2026-10-18T00:00:05Z' }] })
    if (path.endsWith('/conversations/c/messages'))
      return json({ items: [{ id: 'm1', ordinal: '1', run_id: 'run', role: 'user', status: 'complete', schema_version: 2, revision: '1', created_at: '2026-10-18T00:00:00Z', body: { blocks: [{ type: 'text', text: '搭一个棚拍场景' }] } }], prev_cursor: '', conversation_revision: '1' })
    if (path === '/agent-runs/run')
      return json({ ...runBase, id: 'run', trigger_message_id: 'm1', state: 'succeeded', finished_at: '2026-10-18T00:00:05Z', blocks: panelBlocks, snapshot_seq: '9' })
    const detail = path.match(/^\/agent-runs\/run\/blocks\/([^/]+)\/detail$/)
    if (detail) return json({ block_id: detail[1], step_id: 'step', ...panelDetails[detail[1]] })
    if (path === `/documents/${DOC}/run-changes`) {
      assert.equal(url.searchParams.get('run_id'), 'run')
      panel.changeReads++
      return json(panel.changes())
    }
    if (path === `/documents/${DOC}/commands` && request.method() === 'POST') {
      const body = request.postDataJSON()
      panel.commands.push(body)
      const status = panel.answers.shift() ?? 200
      if (status === 409) return json({ error: { code: 'creative_document_conflict', message: '文档已被修改', details: { conflicts: [] } } }, 409)
      const revision = String(++panel.revision)
      if (body.payload.operations[0].units.includes('object:sobj_chair000000000000000000') && body.payload.operations[0].units.length === 3) panel.restored.add('chair')
      return json({ revision, changed_units: body.payload.operations[0].units, annotated_units: [], removed_units: [], reset_required: false })
    }
    return json({ items: [] })
  })
}

async function openPanel() {
  const context = await browser.newContext({ viewport: { width: 1280, height: 860 } })
  const panel = new PanelServer()
  await routePanel(context, panel)
  const t = await tab(context, '.scene-agent-panel-qa.html')
  await t.page.getByRole('log', { name: '对话消息' }).getByText('搭好了：椅子、桌子和台灯。').waitFor()
  return { ...t, context, panel }
}

async function waitUntil(test, message, timeout = 5000) {
  const start = Date.now()
  while (!(await test())) {
    if (Date.now() - start > timeout) throw new Error(`timed out: ${message}`)
    await sleep(25)
  }
}

const results = []
const only = process.env.SCENE_AGENT_E2E_ONLY
async function check(name, fn) {
  if (only && !name.includes(only)) return
  const started = Date.now()
  try {
    await Promise.race([fn(), sleep(90_000).then(() => Promise.reject(new Error('timed out after 90 s')))])
    results.push({ name, ok: true })
    console.log(`ok   ${name} (${Date.now() - started} ms)`)
  } catch (error) {
    results.push({ name, ok: false })
    console.log(`FAIL ${name}\n     ${String(error?.stack ?? error).split('\n').slice(0, 6).join('\n     ')}`)
  }
}

try {
  // --- render_view: offscreen screenshots ---------------------------------

  await check('a screenshot is a JPEG with a 1024 long side, of the revision it read, with the model drawn in its one frame', async () => {
    const t = await openRender()
    // Level with the pillar's middle, so it stands in the centre against the background.
    const shot = await t.render({ viewpoint: { position: [0, 1.5, 6], target: [0, 1.5, 0], focal_mm: 50 } })
    assert.ok(shot.ok, JSON.stringify(shot))
    assert.equal(shot.type, 'image/jpeg')
    assert.deepEqual([shot.width, shot.height], [1024, 768])
    assert.equal(shot.revision, '7', 'scene_revision is the revision it drew')
    assert.ok(isBackground(shot.corner), `the corner is the scene's background: ${shot.corner}`)
    assert.ok(!isBackground(shot.center), `the pillar is drawn, not its loading outline: ${shot.center}`)
    assert.equal(shot.canvases, 0, 'the canvas never joins the page')
    assert.equal(t.server.media, 1)
    assert.deepEqual(t.errors, [])
    await t.context.close()
  })

  await check('a scene camera keeps its picture shape; presets are 4:3; a missing camera is reported', async () => {
    const t = await openRender()
    const camera = await t.render({ camera_object_id: 'sobj_cam' })
    assert.deepEqual([camera.width, camera.height], [1024, 683])
    assert.ok(!isBackground(camera.center), `the camera looks at the pillar: ${camera.center}`)
    for (const preset of ['overview', 'top', 'front', 'side']) {
      const shot = await t.render({ preset })
      assert.deepEqual([shot.width, shot.height], [1024, 768], preset)
      assert.ok(!isBackground(shot.center), `${preset} frames the pillar: ${shot.center}`)
    }
    assert.equal(t.server.media, 1, 'the GLB is downloaded once for every shot')
    assert.deepEqual(await t.render({ camera_object_id: 'sobj_gone' }), { ok: false, message: '摄像机 sobj_gone 已不在场景里' })
    assert.deepEqual(t.errors, [])
    await t.context.close()
  })

  await check('render_view with depth_of_field blurs through the camera’s aperture, at render_view’s size', async () => {
    const t = await openRender()
    let started = Date.now()
    const plain = await t.render({ camera_object_id: 'sobj_dof' })
    const plainMs = Date.now() - started
    started = Date.now()
    const blurred = await t.render({ camera_object_id: 'sobj_dof', depth_of_field: true })
    console.log(`render_view 1024 JPEG: sharp ${plainMs} ms, depth of field ${Date.now() - started} ms; gradient energy ${plain.energy.toFixed(2)} → ${blurred.energy.toFixed(2)}`)
    assert.ok(plain.ok && blurred.ok, JSON.stringify(blurred))
    assert.equal(blurred.type, 'image/jpeg')
    assert.deepEqual([blurred.width, blurred.height], [1024, 683])
    assert.ok(!isBackground(blurred.center), `the pillar is still there: ${blurred.center}`)
    assert.ok(blurred.energy < plain.energy * 0.6, `out of focus is softer: ${plain.energy.toFixed(2)} → ${blurred.energy.toFixed(2)}`)
    assert.deepEqual(t.errors, [])
    await t.context.close()
  })

  await check('the dusk sample (AC-8): render_view mode lit draws the brick, the glass and the warm lamps the clay leaves out', async () => {
    const t = await openRender()
    t.server.doc = duskDoc()
    // The wall below the window, between the lamps.
    const wall = [[0.5, 0.62]]
    const clay = await t.render({ camera_object_id: 'sobj_cam', probes: wall })
    const lit = await t.render({ camera_object_id: 'sobj_cam', mode: 'lit', probes: wall })
    assert.ok(clay.ok && lit.ok, JSON.stringify(lit))
    assert.equal(lit.type, 'image/jpeg')
    assert.deepEqual([lit.width, lit.height], [1024, 683])
    console.log(`dusk sample at the camera: clay wall ${clay.probes[0]}, lit wall ${lit.probes[0]}; corner ${clay.corner} → ${lit.corner}`)
    const [r, g, b] = lit.probes[0]
    assert.ok(r > b + 20 && g > b, `the brick wall in warm lamp light: ${lit.probes[0]}`)
    const [cr, , cb] = clay.probes[0]
    assert.ok(Math.abs(cr - cb) < 20, `the clay wall stays grey: ${clay.probes[0]}`)
    assert.ok(lit.corner[2] < clay.corner[2] - 40, `a darker dusk sky than the clay's: ${clay.corner} → ${lit.corner}`)
    assert.deepEqual(t.errors, [])
    await t.context.close()
  })

  await check('each screenshot gives its WebGL context back: twenty in a row all draw', async () => {
    const t = await openRender()
    for (let i = 0; i < 20; i++) {
      const shot = await t.render({ preset: 'front' })
      assert.ok(shot.ok && !isBackground(shot.center), `shot ${i + 1}: ${JSON.stringify(shot)}`)
    }
    assert.deepEqual(t.errors, [], 'no context is left for the browser to drop')
    await t.context.close()
  })

  // --- the workspace page's claim loop ------------------------------------

  await check('with only the canvas page open, the executor loads on the first job and bakes it; three.js loads only then', async () => {
    const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
    const server = new Server()
    await route(context, server)
    const t = await tab(context, '.scene-agent-host-qa.html')
    await t.page.locator('#host', { hasText: 'idle' }).waitFor()
    await sleep(300)
    assert.ok(server.claims.length >= 1, 'it looked for work on mount')
    assert.deepEqual(t.requested.filter(threeURL), [], 'no three.js before a job')

    server.bake('csj_bake')
    await t.page.evaluate(() => window.__invalidate())
    const done = await server.completed('csj_bake')
    assert.equal(done.payload.outcome, 'succeeded', JSON.stringify(done.payload))
    assert.equal(done.payload.claim_token, 'tok_csj_bake')
    const { bake } = done.payload
    assert.equal(server.uploads.get(bake.glb_upload_id).mime, 'model/gltf-binary')
    assert.equal(server.uploads.get(bake.thumbnail_upload_id).mime, 'image/png')
    assert.deepEqual(Object.keys(bake.module).sort(), ['mount', 'params', 'params_schema'])
    assert.equal(bake.module.params.height, 2.4, 'the job’s parameters')
    assert.ok(bake.manifest.bounds, 'the checked manifest')
    assert.ok(t.requested.some(threeURL), 'three.js came with the executor')
    await t.page.locator('#host', { hasText: 'idle' }).waitFor()

    server.render('csj_shot', { preset: 'overview' })
    await t.page.evaluate(() => window.__invalidate())
    const shot = await server.completed('csj_shot')
    assert.equal(shot.payload.outcome, 'succeeded', JSON.stringify(shot.payload))
    assert.equal(shot.payload.render.scene_revision, '7')
    assert.equal(server.uploads.get(shot.payload.render.image_upload_id).mime, 'image/jpeg')
    assert.deepEqual(t.errors, [])
    await context.close()
  })

  await check('the canvas page runs export_shots: clay and line art per camera, completed with the revision it drew', async () => {
    const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
    const server = new Server()
    await route(context, server)
    const t = await tab(context, '.scene-agent-host-qa.html')
    await t.page.locator('#host', { hasText: 'idle' }).waitFor()
    server.export('csj_export', { camera_object_ids: ['sobj_cam', 'sobj_dof'], passes: ['clay', 'line'], canvas_id: 'canvas', scene_node_id: 'node-scene' })
    const started = Date.now()
    await t.page.evaluate(() => window.__invalidate())
    const done = await server.completed('csj_export')
    assert.equal(done.payload.outcome, 'succeeded', JSON.stringify(done.payload))
    const { export: out } = done.payload
    assert.equal(out.scene_revision, '7')
    assert.deepEqual(out.shots.map((s) => [s.camera_object_id, s.images.map((i) => i.pass)]), [['sobj_cam', ['clay', 'line']], ['sobj_dof', ['clay', 'line']]])
    for (const s of out.shots) for (const i of s.images) assert.equal(server.uploads.get(i.upload_id).mime, 'image/png')
    console.log(`export_shots of 4 images completed in ${Date.now() - started} ms`)
    assert.deepEqual(t.errors, [])
    await context.close()
  })

  await check('the canvas page runs the dusk sample’s lit render_view and a lit export', async () => {
    const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
    const server = new Server()
    server.doc = duskDoc()
    await route(context, server)
    const t = await tab(context, '.scene-agent-host-qa.html')
    await t.page.locator('#host', { hasText: 'idle' }).waitFor()
    server.render('csj_lit', { camera_object_id: 'sobj_cam', mode: 'lit' })
    server.export('csj_lit_export', { camera_object_ids: ['sobj_cam'], passes: ['clay', 'line', 'lit'], canvas_id: 'canvas', scene_node_id: 'node-scene' })
    await t.page.evaluate(() => window.__invalidate())
    const shot = await server.completed('csj_lit')
    assert.equal(shot.payload.outcome, 'succeeded', JSON.stringify(shot.payload))
    const done = await server.completed('csj_lit_export')
    assert.equal(done.payload.outcome, 'succeeded', JSON.stringify(done.payload))
    assert.deepEqual(done.payload.export.shots.map((s) => [s.camera_object_id, s.images.map((i) => i.pass)]), [['sobj_cam', ['clay', 'line', 'lit']]])
    assert.deepEqual(t.errors, [])
    await context.close()
  })

  await check('closing asks first only in the browser’s only workspace tab, and only while it holds a job', async () => {
    const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
    const server = new Server()
    await route(context, server)
    let release
    server.uploadHold = new Promise((resolve) => (release = resolve))
    const a = await tab(context, '.scene-agent-host-qa.html')
    // Chrome shows the leave prompt only after the person has used the page.
    await a.page.mouse.click(10, 10)
    const dialogs = []
    a.page.on('dialog', (d) => {
      dialogs.push(d.type())
      void d.dismiss()
    })
    server.bake('csj_held')
    await a.page.evaluate(() => window.__invalidate())
    await a.page.locator('#host', { hasText: 'holding:bake_model' }).waitFor({ timeout: 20_000 })
    await a.page.close({ runBeforeUnload: true })
    await sleep(500)
    assert.deepEqual(dialogs, ['beforeunload'], 'alone and holding: closing asks')
    assert.ok(!a.page.isClosed(), 'dismissed: the page stays')

    const b = await tab(context, '.scene-agent-host-qa.html')
    await b.page.locator('#host', { hasText: 'idle' }).waitFor()
    await sleep(200)
    const closed = new Promise((resolve) => a.page.once('close', () => resolve(true)))
    await a.page.close({ runBeforeUnload: true })
    assert.ok(await Promise.race([closed, sleep(3000).then(() => false)]), 'the page closed')
    assert.deepEqual(dialogs, ['beforeunload'], 'another tab can take the job over: no prompt')
    release()
    await context.close()
  })

  await check('a device that fails the probe claims nothing', async () => {
    const context = await browser.newContext({ viewport: { width: 800, height: 900 } })
    const server = new Server()
    await route(context, server)
    const t = await tab(context, '.scene-agent-host-qa.html')
    await t.page.locator('#host', { hasText: 'incapable' }).waitFor()
    server.bake('csj_narrow')
    await t.page.evaluate(() => window.__invalidate())
    await sleep(800)
    assert.deepEqual(server.claims, [], 'no claim from a narrow window')
    assert.equal(server.queue.length, 1, 'the job waits for a page that can run it')
    await context.close()
  })

  // --- the assistant panel: screenshots and the run's change list ---------

  await check('the panel shows the assistant’s screenshot under its tool row, and a click opens it large', async () => {
    const p = await openPanel()
    await p.page.locator('.cc-agent-fold').first().click()
    const thumb = p.page.getByRole('button', { name: '查看截图' })
    await thumb.locator('img').waitFor()
    assert.deepEqual(p.panel.tickets, [{ content_revision_id: 'ccrv_shot', role: 'original', purpose: 'display' }])
    await thumb.click()
    const view = p.page.getByRole('dialog', { name: '截图' })
    await view.locator('img').waitFor()
    await p.page.keyboard.press('Escape')
    await view.waitFor({ state: 'detached' })
    // A screenshot that has gone away says so instead of a broken image.
    p.panel.ticketMissing = true
    await p.page.reload()
    await p.page.locator('.cc-agent-fold').first().click()
    await p.page.getByText('截图已不可用').waitFor()
    assert.deepEqual(p.errors, [])
    await p.context.close()
  })

  await check('「恢复这一处」 and 「撤销本轮」 each write one revision; a conflict mid-undo says what was done and reads the list again', async () => {
    const p = await openPanel()
    const list = p.page.getByRole('region', { name: '本轮对「棚拍布景」的改动' })
    const row = (name) => list.getByRole('listitem').filter({ hasText: name })
    await row('椅子').waitFor()
    assert.equal(await row('台灯').textContent(), '台灯修改不可恢复：之后你又改过（第 5 版）')
    // One part.
    await row('椅子').getByRole('button', { name: '恢复这一处' }).click()
    await list.getByRole('status').filter({ hasText: '已恢复「椅子」' }).waitFor()
    assert.equal(p.panel.commands.length, 1)
    assert.deepEqual(p.panel.commands[0].payload, { expected: chairBatch.expected, operations: [{ op: 'restore_units', from_revision: '1', units: chairBatch.units }] })
    assert.equal(p.panel.changeReads, 2, 'the list was read again')
    // What it put back reads as put back, not as someone's later edit.
    await row('椅子').getByText('已恢复').waitFor()
    // The whole run: one batch, one revision. The list read after it has three batches.
    p.panel.undoAll = [chairBatch, tableBatch, lampBatch]
    await list.getByRole('button', { name: '撤销本轮' }).click()
    await list.getByRole('status').filter({ hasText: '已撤销本轮的改动' }).waitFor()
    assert.equal(p.panel.commands.length, 2)
    assert.deepEqual(p.panel.commands[1].payload.operations[0].units, tableBatch.units)
    await waitUntil(() => p.panel.changeReads === 3, 'the list was read again')
    // The second of three batches meets a newer edit: the first stays done, the third is not sent.
    p.panel.answers = [200, 409]
    await list.getByRole('button', { name: '撤销本轮' }).click()
    await list.getByRole('status').filter({ hasText: '撤销了一部分：其余的地方刚被修改，没有覆盖，清单已重新读取' }).waitFor()
    assert.equal(p.panel.commands.length, 4, 'the third batch was not sent')
    assert.deepEqual(p.errors, [])
    await p.context.close()
  })

  await check('a message sent from the scene editor carries its scene and selection', async () => {
    const p = await openPanel()
    await p.page.evaluate((doc) => window.__scene({ documentID: doc, selected: ['sobj_chair000000000000000000'] }), DOC)
    const editor = p.page.getByRole('textbox', { name: '创作问题' })
    await editor.click()
    await p.page.keyboard.type('把椅子换成木头的')
    await p.page.keyboard.press('Enter')
    await waitUntil(() => p.panel.created.length === 1, 'the run was created')
    assert.deepEqual(p.panel.created[0].payload.scene_context, { document_id: DOC, selected_object_ids: ['sobj_chair000000000000000000'] })
    await p.context.close()
  })
} finally {
  await browser.close()
  await rm(renderFixture, { force: true })
  await rm(hostFixture, { force: true })
  await rm(panelFixture, { force: true })
}

const failed = results.filter((r) => !r.ok)
console.log(`\n${results.length - failed.length}/${results.length} passed`)
if (failed.length) process.exit(1)
