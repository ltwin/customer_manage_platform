// C1 scene editor in a real browser, against an in-memory scene server.
// Run with the frontend Vite server on CREATIVE_SCENE_URL (default :5179):
//   node scripts/creative-scene.e2e.mjs
// The server applies operations with the client's own copy of the semantics
// (state/apply.ts); the Go tests own the real ones. Every check gets a fresh
// browser context.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { rm, writeFile } from 'node:fs/promises'
import { chromium } from 'playwright'
import { applyOps } from '../src/creative-scene/state/apply.ts'
import { changedUnits } from '../src/creative-scene/state/intents.ts'
import { worldOf } from '../src/creative-scene/state/world.ts'
import { defaultModuleCode } from '../src/creative-scene/sandbox/template.ts'

const DOC = 'cwdoc_scene'
const fixture = new URL('../.scene-editor-qa.html', import.meta.url)
const canvasFixture = new URL('../.scene-canvas-qa.html', import.meta.url)
await writeFile(
  fixture,
  `<html><body style="margin:0"><div id="root" class="cc-workspace" style="width:100vw;height:100vh"></div><script type="module">
import React, {StrictMode, useState} from 'react';
import {createRoot} from 'react-dom/client';
import '/src/index.css';
import '/src/creative-canvas/editor/workspace.css';
import SceneEditor from '/src/creative-scene/SceneEditor.tsx';
import {useSceneHost} from '/src/creative-scene/host/launcher.ts';
import {sceneContext} from '/src/creative-scene/context.ts';
import {notifyCanvasInvalidate} from '/src/creative-canvas/editor/canvasEvents.ts';
import {setAuthenticated} from '/src/auth/session.ts';
setAuthenticated({access_token:'local-test',expires_in:3600,token_type:'Bearer'},{id:'qa-account',username:'qa'});
try { localStorage.setItem('creative-scene:first-run-seen', '1') } catch {}
window.__invalidate = notifyCanvasInvalidate;
window.__context = () => sceneContext.getState();
window.__closed = 0;
// Like main.tsx: StrictMode mounts, unmounts and mounts again in development.
// 「还原节点」 closes the editor; window.__reopen() opens it again in the same page.
// ?host=1 mounts the page's job launcher too, as WorkspacePage does, and hands it to the editor.
function Editor({host}) {
  const [open, setOpen] = useState(true);
  window.__reopen = () => setOpen(true);
  return open
    ? React.createElement(SceneEditor, {documentID:'${DOC}', host, onClose:()=>{window.__closed++; setOpen(false)}, ...(new URLSearchParams(location.search).has('export') ? {canvasID:'ccv_qa', sceneNodeID:'cwnode_qa'} : {})})
    : React.createElement('p', {id:'closed'}, 'closed');
}
function WithHost() {
  return React.createElement(Editor, {host: useSceneHost('qa-account').launcher});
}
const App = new URLSearchParams(location.search).has('host') ? WithHost : Editor;
createRoot(document.getElementById('root')).render(React.createElement(StrictMode, null, React.createElement(App)));
</script></body></html>`,
)
// The canvas page module graph, without the editor: three must not load.
await writeFile(
  canvasFixture,
  `<html><body><script type="module">
import '/src/creative-canvas/editor/WorkspacePage.tsx';
window.__loaded = true;
</script></body></html>`,
)
const base = process.env.CREATIVE_SCENE_URL || 'http://localhost:5179'
const browser = await chromium.launch({ headless: true, args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'] })
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
const close = (a, b, eps = 1e-6) => Math.abs(a - b) < eps

const identity = () => ({ position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] })

// The editor's default template as the sandbox exports it (the shared fixture).
const testdata = new URL('../../backend/internal/creativeglb/testdata/', import.meta.url)
const templateGLB = readFileSync(new URL('exported-template.glb', testdata))
const templateManifest = JSON.parse(readFileSync(new URL('cases.json', testdata), 'utf8')).find((c) => c.name === 'exported-template').manifest
const templateSchema = {
  height: { type: 'number', default: 3, min: 1, max: 12, step: 0.1, unit: 'm', label: '高度' },
  radius: { type: 'number', default: 0.25, min: 0.1, max: 1, step: 0.01, unit: 'm', label: '半径' },
  style: { type: 'enum', default: 'round', options: ['round', 'square'], label: '柱身' },
  broken: { type: 'boolean', default: false, label: '断裂' },
}
const templateParams = { height: 3, radius: 0.25, style: 'round', broken: false }
const placement = (id) => `object:${id}`
const content = (id) => `object:${id}:content`

/** One scene document with revisions, conflict checks and trash. */
class SceneServer {
  constructor() {
    this.title = '棚拍布景'
    this.scene = {
      environment: {
        sky: { kind: 'procedural', sun_elevation_deg: 35, sun_azimuth_deg: 135, turbidity: 3 },
        ambient: { color: '#ffffff', intensity: 0.4 },
        fog: null,
      },
      render: { mode: 'clay' },
      objects: [
        {
          id: 'sobj_ground',
          kind: 'primitive',
          name: '地面',
          parent_id: null,
          order: 0,
          transform: identity(),
          visible: true,
          locked: true,
          primitive: { builtin: 'ground@1', params: { size: 40 } },
        },
      ],
    }
    this.revision = 1
    this.unitRevs = { meta: '1', environment: '1', render: '1', objects: '1', [placement('sobj_ground')]: '1', [content('sobj_ground')]: '1' }
    this.trashedAt = null
    this.receipts = new Map()
    this.commands = []
    this.history = [this.record(['*'], [], null)]
    this.reads = 0
    this.hold = null
    // Code models: staged uploads, committed revisions and their GLBs.
    this.uploads = new Map()
    this.uploadBytes = new Map()
    this.models = new Map()
    this.discarded = []
    this.mediaMissing = new Set()
    this.modelSeq = 0
    // Set to a promise to keep uploads from completing until it settles.
    this.uploadHold = null
    // The assistant's client jobs: what the editor lists, and what a page claims.
    this.jobs = []
    this.claimable = []
    this.claims = []
    this.completes = []
    // Exports the editor submitted.
    this.exports = []
  }
  /** A committed model revision, as if made earlier; returns its revision ID. */
  seedModel({ code = defaultModuleCode, params = templateParams, schema = templateSchema, glb = templateGLB, manifest = templateManifest, content } = {}) {
    const rev = `ccrv_seed${++this.modelSeq}`
    const generator = { language: 'three-module', contract_version: 1, lib_version: 1, code, code_sha256: `sha256-${createHash('sha256').update(code).digest('hex')}`, params_schema: schema, params, mount: 'floor' }
    this.models.set(rev, { content_id: content ?? `ccnt_seed${this.modelSeq}`, glb, generator, manifest, name_hint: '柱' })
    return rev
  }
  /** A model instance placed earlier, at revision 1 like the ground. */
  seedInstance(id, rev, x = 0, name = '柱') {
    this.scene.objects.push({
      id, kind: 'model_instance', name, parent_id: null, order: this.scene.objects.length,
      transform: { ...identity(), position: [x, 0, 0] }, visible: true, locked: false,
      model: { content_revision_id: rev }, material_slots: {},
    })
    this.unitRevs[placement(id)] = '1'
    this.unitRevs[content(id)] = '1'
    this.history[0] = this.record(['*'], [], null)
  }
  modelSummaries(scene) {
    const out = {}
    for (const o of scene.objects) {
      const rev = o.model?.content_revision_id
      if (!rev || out[rev]) continue
      const m = this.models.get(rev)
      if (!m) {
        out[rev] = { content_id: '', usable: false, manifest: null, generator: null, name_hint: null }
        continue
      }
      const { code: _code, ...generator } = m.generator
      out[rev] = { content_id: m.content_id, usable: true, manifest: m.manifest, generator, name_hint: m.name_hint ?? null }
    }
    return out
  }
  /**
   * commit_model the way the server does it here: both uploads ready and
   * unused, then a new revision under base_content_id or a new content, and
   * every {ref} in the batch becomes that revision.
   */
  commitModels(ops) {
    const commits = ops.filter((op) => op.op === 'commit_model')
    for (const op of commits) {
      for (const id of [op.glb_upload_id, op.thumbnail_upload_id]) {
        const u = this.uploads.get(id)
        if (!u || u.state !== 'ready' || u.consumed) return { error: [409, { error: { code: 'creative_upload_unavailable', message: '暂存上传已失效' } }] }
      }
      assert.ok(!op.base_content_id || [...this.models.values()].some((m) => m.content_id === op.base_content_id), `base content ${op.base_content_id} is real`)
    }
    const models = {}
    for (const op of commits) {
      for (const id of [op.glb_upload_id, op.thumbnail_upload_id]) this.uploads.get(id).consumed = true
      const rev = `ccrv_m${++this.modelSeq}`
      const content_id = op.base_content_id ?? `ccnt_m${this.modelSeq}`
      this.models.set(rev, { content_id, glb: this.uploadBytes.get(op.glb_upload_id), generator: op.generator, manifest: op.manifest, name_hint: op.name_hint })
      models[op.ref] = { content_id, content_revision_id: rev }
    }
    const real = (m) => {
      if (m?.ref !== undefined) return { content_revision_id: models[m.ref].content_revision_id }
      if (m) assert.ok(this.models.has(m.content_revision_id) || m.content_revision_id.startsWith('ccrv_gone'), `a model sent is real: ${m.content_revision_id}`)
      return m
    }
    const resolved = ops.map((op) =>
      op.op === 'add_objects'
        ? { ...op, objects: op.objects.map((o) => (o.model ? { ...o, model: real(o.model) } : o)) }
        : op.op === 'set_instance_model'
          ? { ...op, model: real(op.model) }
          : op,
    )
    return { ops: resolved, models }
  }
  uploadView(u) {
    const { consumed: _consumed, ...view } = u
    return view
  }
  record(changed, removed, restoredFrom, actor = 'photographer', at = null) {
    return {
      actor,
      revision: String(this.revision),
      title: this.title,
      scene: structuredClone(this.scene),
      unit_revs: { ...this.unitRevs },
      changed_units: changed,
      removed_units: removed,
      restored_from: restoredFrom,
      created_at: new Date(at ?? Date.UTC(2026, 9, 16, 9, 0, this.revision)).toISOString(),
    }
  }
  doc() {
    return {
      id: DOC,
      title: this.title,
      type_key: 'scene',
      content_revision_id: `ccrv_${this.revision}`,
      revision: String(this.revision),
      unit_revs: { ...this.unitRevs },
      trashed_at: this.trashedAt,
      created_at: '2026-10-16T09:00:00Z',
      updated_at: '2026-10-16T09:00:00Z',
      scene: structuredClone(this.scene),
      models: this.modelSummaries(this.scene),
    }
  }
  object(id) {
    return this.scene.objects.find((o) => o.id === id)
  }
  objectUnits() {
    return new Set(this.scene.objects.flatMap((o) => [placement(o.id), content(o.id)]))
  }
  /** Applies one command body; returns [status, body]. */
  command(body) {
    const seen = this.receipts.get(body.operation_id)
    if (seen) return [200, seen]
    if (this.trashedAt) return [409, { error: { code: 'creative_state_conflict', message: '文档在回收站' } }]
    const { payload } = body
    const ops = payload.operations
    if (ops[0]?.op === 'restore') return this.restoreWhole(body, payload.expected_revision, ops[0].revision)
    const conflicts = []
    for (const [unit, rev] of Object.entries(payload.expected)) {
      const current = this.unitRevs[unit] ?? null
      if (current !== rev) conflicts.push({ unit, expected_rev: rev, current_rev: current })
    }
    if (conflicts.length && process.env.SCENE_E2E_DEBUG) console.log('CONFLICT', JSON.stringify(payload), JSON.stringify(conflicts))
    if (conflicts.length)
      return [409, { error: { code: 'creative_document_conflict', message: '文档已被修改', details: { conflicts } } }]
    const committed = this.commitModels(ops)
    if (committed.error) return committed.error
    const before = { title: this.title, scene: this.scene }
    const beforeUnits = this.objectUnits()
    let changed
    let restoredFrom = null
    if (ops[0]?.op === 'restore_units') {
      restoredFrom = ops[0].from_revision
      const after = this.restoreUnits(ops[0])
      this.title = after.title
      this.scene = after.scene
      changed = [...ops[0].units]
    } else {
      const after = applyOps(before, committed.ops)
      this.title = after.title
      this.scene = after.scene
      changed = changedUnits({ ops: committed.ops }, before)
    }
    const afterUnits = this.objectUnits()
    const removed = [...beforeUnits].filter((u) => !afterUnits.has(u))
    const existenceChanged = removed.length || [...afterUnits].some((u) => !beforeUnits.has(u))
    if (existenceChanged && !changed.includes('objects')) changed.push('objects')
    changed = changed.filter((u) => !removed.includes(u) && (!u.startsWith('object:') || afterUnits.has(u)))
    this.revision++
    for (const u of changed) this.unitRevs[u] = String(this.revision)
    for (const u of removed) delete this.unitRevs[u]
    this.history.push(this.record(changed, removed, restoredFrom))
    const receipt = { revision: String(this.revision), changed_units: changed, annotated_units: [], removed_units: removed, reset_required: false }
    if (Object.keys(committed.models).length) receipt.models = committed.models
    this.receipts.set(body.operation_id, receipt)
    this.commands.push({ ...body, receipt })
    return [200, receipt]
  }
  restoreUnits(op) {
    const source = this.history.find((h) => h.revision === op.from_revision)
    assert.ok(source, `restore from unknown revision ${op.from_revision}`)
    let objects = structuredClone(this.scene.objects)
    let { environment, render } = this.scene
    let title = this.title
    for (const unit of op.units) {
      if (unit === 'meta') title = source.title
      else if (unit === 'environment') environment = source.scene.environment
      else if (unit === 'render') render = source.scene.render
      else if (unit.startsWith('object:')) {
        const id = unit.split(':')[1]
        const was = source.scene.objects.find((o) => o.id === id)
        const now = objects.find((o) => o.id === id)
        if (!was) objects = objects.filter((o) => o.id !== id)
        else if (!now) objects.push(structuredClone(was))
        else if (unit.endsWith(':content')) Object.assign(now, { primitive: was.primitive, model: was.model, material_slots: was.material_slots, light: was.light, camera: was.camera })
        else Object.assign(now, { name: was.name, transform: was.transform, visible: was.visible, locked: was.locked })
      }
    }
    return { title, scene: { ...this.scene, environment, render, objects } }
  }
  restoreWhole(body, expected, revision) {
    if (expected !== String(this.revision))
      return [409, { error: { code: 'creative_document_conflict', message: '文档已被修改', details: { conflicts: [{ unit: '*', expected_rev: expected, current_rev: String(this.revision) }] } } }]
    const source = this.history.find((h) => h.revision === revision)
    this.revision++
    this.title = source.title
    this.scene = structuredClone(source.scene)
    this.unitRevs = Object.fromEntries(Object.keys(source.unit_revs).map((u) => [u, String(this.revision)]))
    this.history.push(this.record(['*'], [], revision))
    const receipt = { revision: String(this.revision), changed_units: [], annotated_units: [], removed_units: [], reset_required: true }
    this.receipts.set(body.operation_id, receipt)
    this.commands.push({ ...body, receipt })
    return [200, receipt]
  }
  /** Someone else's edit, straight on the server; the assistant's with `actor: 'agent'`, dated `at`. */
  external(ops, { actor = 'photographer', at = null } = {}) {
    const before = { title: this.title, scene: this.scene }
    const after = applyOps(before, ops)
    const changed = changedUnits({ ops }, before)
    if (ops.some((op) => op.op === 'add_objects')) changed.push('objects')
    this.title = after.title
    this.scene = after.scene
    this.revision++
    for (const u of changed) this.unitRevs[u] = String(this.revision)
    this.history.push(this.record(changed, [], null, actor, at))
  }
  revisionItem(h) {
    return {
      revision: h.revision,
      actor_kind: h.actor,
      run_id: h.actor === 'agent' ? 'car_qa' : null,
      restored_from: h.restored_from,
      changed_units: h.changed_units,
      annotated_units: [],
      removed_units: h.removed_units,
      title: h.title,
      created_at: h.created_at,
    }
  }
}

async function route(page, server) {
  await page.route('**/api/v1/creative/**', async (r) => {
    const request = r.request()
    const url = new URL(request.url())
    const path = url.pathname.replace('/api/v1/creative', '')
    const json = (status, body) => r.fulfill({ status, json: body })
    if (path === `/documents/${DOC}` && request.method() === 'GET') {
      server.reads++
      return json(200, server.doc())
    }
    if (path === `/documents/${DOC}/commands` && request.method() === 'POST') {
      if (server.hold) await server.hold
      const [status, body] = server.command(request.postDataJSON())
      return json(status, body)
    }
    if (path === `/documents/${DOC}/revisions`) {
      const since = Number(url.searchParams.get('since_revision') ?? 0)
      const limit = Number(url.searchParams.get('limit') ?? 50)
      const items = [...server.history].reverse().filter((h) => Number(h.revision) > since).slice(0, limit).map((h) => server.revisionItem(h))
      return json(200, { items, next_cursor: null })
    }
    if (path === `/documents/${DOC}/scene-jobs`) return json(200, { items: server.jobs })
    if (path === `/documents/${DOC}/scene-exports` && request.method() === 'POST') {
      const body = request.postDataJSON()
      server.exports.push(body)
      const nodes = body.payload.shots.flatMap((s) => s.images.map((img, i) => ({ camera_object_id: s.camera_object_id, pass: img.pass, node_id: `cwnode_${s.camera_object_id}_${i}`, content_revision_id: `ccrv_${img.upload_id}` })))
      return json(200, { change_id: 'cwch_qa', scene_revision: body.payload.scene_revision, nodes })
    }
    if (path === '/scene-jobs/claim' && request.method() === 'POST') {
      const body = request.postDataJSON()
      server.claims.push(body)
      const i = server.claimable.findIndex((j) => !body.document_id || j.document_id === body.document_id)
      if (i < 0) return r.fulfill({ status: 204, body: '' })
      const [job] = server.claimable.splice(i, 1)
      return json(200, { job })
    }
    const jobAction = path.match(/^\/scene-jobs\/([^/]+)\/(lease|complete)$/)
    if (jobAction) {
      if (jobAction[2] === 'lease') return json(200, { job_id: jobAction[1], lease_until: '2100-01-01T00:00:00Z' })
      const { payload } = request.postDataJSON()
      server.completes.push({ id: jobAction[1], payload })
      return json(200, { job_id: jobAction[1], state: payload.outcome === 'failed' ? 'failed' : 'succeeded', result: {} })
    }
    const one = path.match(new RegExp(`^/documents/${DOC}/revisions/(\\d+)$`))
    if (one) {
      const h = server.history.find((x) => x.revision === one[1])
      if (!h) return json(404, { error: { code: 'not_found', message: '修订不存在' } })
      return json(200, { ...server.revisionItem(h), unit_revs: h.unit_revs, view: { scene: h.scene, models: server.modelSummaries(h.scene) } })
    }
    if (path === '/uploads' && request.method() === 'POST') {
      const { payload } = request.postDataJSON()
      const id = `ccup_${server.uploads.size + 1}`
      const upload = {
        id, state: 'uploading', io_phase: 'none', revision: '1', kind: payload.kind, mime: payload.mime, size: payload.size,
        file_name: payload.file_name, part_size: payload.size, part_count: 1, uploaded_parts: [], target_kind: payload.target.kind,
        expires_at: '2100-01-01T00:00:00Z', error_code: '', publication: null, binding: null, content_revision_id: null, created_at: '2026-10-17T00:00:00Z',
      }
      server.uploads.set(id, upload)
      return json(201, server.uploadView(upload))
    }
    const upload = path.match(/^\/uploads\/([^/]+)(\/part-authorizations|\/complete)?$/)
    if (upload) {
      const u = server.uploads.get(upload[1])
      if (upload[2] === '/part-authorizations')
        return json(200, { upload_id: u.id, parts: [{ part_number: 1, method: 'PUT', url: `${base}/__upload/${u.id}`, headers: {}, expires_at: '2100-01-01T00:00:00Z' }] })
      if (upload[2] === '/complete') {
        if (server.uploadHold) await server.uploadHold
        Object.assign(u, { state: 'ready', publication: { kind: 'candidate', id: `ccuc_${u.id}`, revision: '1' } })
        u.binding = { status: 'staged', target_kind: null, target_id: null, candidate_id: `ccuc_${u.id}`, expires_at: '2100-01-01T00:00:00Z' }
      }
      return json(200, server.uploadView(u))
    }
    const discard = path.match(/^\/upload-candidates\/([^/]+)\/discard$/)
    if (discard) {
      server.discarded.push(discard[1])
      return json(200, { id: discard[1], state: 'discarded' })
    }
    if (path === '/media-access-tickets' && request.method() === 'POST') {
      const { content_revision_id: rev } = request.postDataJSON()
      const m = server.models.get(rev)
      if (!m) return json(404, { error: { code: 'not_found', message: '内容不存在' } })
      return json(200, { url: `${base}/__media/${rev}`, expires_at: '2100-01-01T00:00:00Z', mime: 'model/gltf-binary', byte_size: m.glb.byteLength })
    }
    const revision = path.match(/^\/content-revisions\/([^/]+)$/)
    if (revision && server.models.has(revision[1])) {
      const m = server.models.get(revision[1])
      return json(200, { truncated: false, id: revision[1], content_id: m.content_id, kind: 'model', sequence: '1', payload: { model: { format: 'glb', generator: m.generator, manifest: m.manifest, name_hint: m.name_hint } } })
    }
    return json(404, { error: { code: 'not_found', message: `unmocked ${path}` } })
  })
}

async function routeFiles(page, server) {
  await page.route('**/__upload/**', async (r) => {
    server.uploadBytes.set(r.request().url().split('/').at(-1), r.request().postDataBuffer())
    return r.fulfill({ status: 200, body: '' })
  })
  await page.route('**/__media/**', async (r) => {
    const rev = r.request().url().split('/').at(-1)
    const m = server.models.get(rev)
    if (!m || server.mediaMissing.has(rev)) return r.fulfill({ status: 404, body: '' })
    return r.fulfill({ status: 200, contentType: 'model/gltf-binary', body: m.glb })
  })
}

async function open(server, options = {}) {
  const context = options.context ?? (await browser.newContext({ viewport: { width: 1280, height: 820 } }))
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('console', (m) => m.type() === 'error' && !/Failed to load resource/.test(m.text()) && errors.push(m.text()))
  await route(page, server)
  await routeFiles(page, server)
  await page.goto(`${base}/.scene-editor-qa.html${options.query ?? ''}`)
  await page.locator('.cc-scene-editor canvas').waitFor({ timeout: 20_000 })
  await page.getByRole('treeitem').first().waitFor()
  // Let R3F mount the scene and the camera settle.
  await sleep(400)
  return { context, page, errors }
}

async function viewportCenter(page) {
  const box = await page.locator('.cc-scene-editor canvas').boundingBox()
  return { x: box.x + box.width / 2, y: box.y + box.height / 2, box }
}

/** The share of pixels that differ visibly (summed RGB difference over min) between two screenshots of one size. */
async function pixelDiff(page, a, b, min = 48) {
  return page.evaluate(async ([a, b, min]) => {
    const load = (src) => new Promise((resolve) => { const i = new Image(); i.onload = () => resolve(i); i.src = src })
    const [x, y] = await Promise.all([load(a), load(b)])
    const c = document.createElement('canvas')
    c.width = x.width
    c.height = x.height
    const g = c.getContext('2d')
    g.drawImage(x, 0, 0)
    const p = g.getImageData(0, 0, c.width, c.height).data
    g.clearRect(0, 0, c.width, c.height)
    g.drawImage(y, 0, 0)
    const q = g.getImageData(0, 0, c.width, c.height).data
    let n = 0
    for (let i = 0; i < p.length; i += 4) if (Math.abs(p[i] - q[i]) + Math.abs(p[i + 1] - q[i + 1]) + Math.abs(p[i + 2] - q[i + 2]) > min) n++
    return n / (p.length / 4)
  }, [`data:image/png;base64,${a.toString('base64')}`, `data:image/png;base64,${b.toString('base64')}`, min])
}

async function waitFor(check, message, timeout = 5000) {
  const start = Date.now()
  for (;;) {
    if (await check()) return
    if (Date.now() - start > timeout) throw new Error(`timed out: ${message}`)
    await sleep(25)
  }
}

const saveText = (page) => page.locator('.cc-scene-save').textContent()
const saved = (page) => waitFor(async () => (await saveText(page)) === '已保存', 'saved')

async function addBox(page, server) {
  const before = server.commands.length
  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '方块' }).click()
  await waitFor(() => server.commands.length === before + 1, 'add command')
  await saved(page)
  const add = server.commands.at(-1).payload.operations[0]
  assert.equal(add.op, 'add_objects')
  return add.objects[0].id
}

// SCENE_E2E_SHOTS=<dir> saves screenshots at the main steps for a look.
const shot = (page, name) => process.env.SCENE_E2E_SHOTS && page.screenshot({ path: `${process.env.SCENE_E2E_SHOTS}/${name}.png` })

const results = []
const only = process.env.SCENE_E2E_ONLY
async function check(name, fn) {
  if (only && !name.includes(only)) return
  const started = Date.now()
  try {
    await fn()
    results.push(`ok   ${name} (${Date.now() - started} ms)`)
  } catch (error) {
    results.push(`FAIL ${name}\n     ${error.stack?.split('\n').slice(0, 4).join('\n     ')}`)
  }
}

await check('keyboard flow: Shift+A, G Z 2 Enter, R cancelled, A G confirmed', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  assert.equal(server.reads, 1)
  const id = await addBox(page, server)
  const added = server.object(id)
  assert.equal(added.primitive.builtin, 'box@1')
  assert.equal(added.parent_id, null)
  assert.equal(added.order, 1, 'after the ground')
  assert.ok(close(added.transform.position[1], 0), 'placed on the ground')
  await page.getByRole('treeitem', { name: /方块/ }).waitFor()
  assert.equal(await page.getByRole('treeitem', { name: /方块/ }).getAttribute('aria-selected'), 'true', 'a new object is selected')

  await shot(page, '1-added')
  const start = [...added.transform.position]
  await page.keyboard.press('KeyG')
  await waitFor(async () => /移动/.test(await page.locator('.cc-scene-hint').textContent()), 'grab running')
  await page.keyboard.press('KeyZ')
  await page.keyboard.press('Digit2')
  assert.match(await page.locator('.cc-scene-hint').textContent(), /^移动 · Z 轴 · .*2/, 'the status bar shows mode, axis and the typed value')
  await page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === 2, 'move command')
  await saved(page)
  const move = server.commands[1].payload
  assert.deepEqual(Object.keys(move.expected), [placement(id)], 'a move names only the placement unit')
  const moved = server.object(id).transform.position
  // The editor's Z is up (Blender); the document stores Y up.
  assert.ok(close(moved[0], start[0]) && close(moved[1], start[1] + 2) && close(moved[2], start[2]), `G Z 2 raises it: ${moved} from ${start}`)

  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('KeyR')
  await page.mouse.move(x + 90, y + 30, { steps: 4 })
  await shot(page, '2-rotating')
  await page.mouse.click(x + 90, y + 30, { button: 'right' })
  await sleep(300)
  assert.equal(server.commands.length, 2, 'a cancelled rotate sends nothing')
  assert.match(await page.locator('.cc-scene-hint').textContent(), /G 移动/, 'back to idle')
  assert.deepEqual(server.object(id).transform.rotation, [0, 0, 0, 1])

  await page.keyboard.press('KeyA')
  await page.mouse.move(x, y)
  await page.keyboard.press('KeyG')
  await page.mouse.move(x + 120, y, { steps: 6 })
  await page.mouse.click(x + 120, y)
  await waitFor(() => server.commands.length === 3, 'select-all move command')
  await saved(page)
  const all = server.commands[2].payload.operations[0]
  assert.equal(all.op, 'update_objects')
  assert.deepEqual(all.updates.map((u) => u.object_id), [id], 'the locked ground stays put')
  const final = server.object(id).transform.position
  assert.ok(!close(final[0], moved[0], 1e-3), 'moved along the view')
  assert.ok(close(final[1], moved[1], 1e-6), 'a sideways drag moves parallel to the screen, without vertical drift')
  assert.deepEqual(server.object('sobj_ground').transform, identity())
  await shot(page, '3-final')
  assert.equal(server.commands.length, 3)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('new objects go where the menu opened, not where its item is', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '方块' }).waitFor()
  assert.equal(await page.getByRole('menuitem').first().textContent(), '方块', 'the first item is a box, not a second ground')
  await page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === 1, 'keyboard add')
  const viaKeyboard = server.commands[0].payload.operations[0].objects[0].transform.position
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '方块' }).click()
  await waitFor(() => server.commands.length === 2, 'mouse add')
  const viaMouse = server.commands[1].payload.operations[0].objects[0].transform.position
  assert.deepEqual(viaMouse, viaKeyboard, 'the pointer travelling to the item does not move the spot')

  await page.mouse.click(x + 160, y + 90, { button: 'right' })
  await page.getByRole('menuitem', { name: '球体' }).click()
  await waitFor(() => server.commands.length === 3, 'context-menu add')
  const sphere = server.commands[2].payload.operations[0].objects[0].transform.position
  assert.ok(Math.hypot(sphere[0] - viaMouse[0], sphere[2] - viaMouse[2]) > 0.5, 'a right click elsewhere adds elsewhere')

  await page.getByRole('button', { name: '添加' }).click()
  await page.getByRole('menuitem', { name: '圆柱' }).click()
  await waitFor(() => server.commands.length === 4, 'menu-bar add')
  const cylinder = server.commands[3].payload.operations[0].objects[0].transform.position
  assert.ok(cylinder.every(Number.isFinite) && cylinder[1] === 0, 'the menu bar adds at the view centre on the ground')
  await saved(page)
  await shot(page, '0-placed')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('closing the editor right after adding and opening it again keeps the box', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '方块' }).click()
  // Close at once, as a photographer would, without waiting for 「已保存」.
  await page.getByRole('button', { name: '还原节点' }).click()
  await page.locator('#closed').waitFor()
  await waitFor(() => server.commands.length === 1, 'the add is saved after the editor closed')
  await page.evaluate(() => window.__reopen())
  await page.getByRole('treeitem', { name: /方块/ }).waitFor()
  assert.equal(server.scene.objects.length, 2)
  // Undo still follows the session after StrictMode's remount.
  const mod = process.platform === 'darwin' ? 'Meta' : 'Control'
  await page.locator('.cc-scene-editor canvas').click({ position: { x: 20, y: 20 } })
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '球体' }).click()
  await waitFor(() => server.commands.length === 2, 'second add')
  await saved(page)
  await page.keyboard.press(`${mod}+KeyZ`)
  await waitFor(() => server.commands.length === 3, 'undo')
  assert.equal(server.commands[2].payload.operations[0].op, 'restore_units')
  assert.equal(server.scene.objects.length, 2, 'the sphere is undone, the box stays')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('reopening shows exactly what the server has', async () => {
  const server = new SceneServer()
  const first = await open(server)
  const id = await addBox(first.page, server)
  await first.page.keyboard.press('KeyG')
  await first.page.keyboard.press('KeyX')
  await first.page.keyboard.press('Minus')
  await first.page.keyboard.press('Digit3')
  await first.page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === 2, 'move')
  await saved(first.page)
  await first.context.close()

  const again = await open(server)
  await again.page.getByRole('treeitem', { name: /方块/ }).click()
  const fields = again.page.locator('.cc-scene-props input[type=number]')
  const [px, py, pz] = server.object(id).transform.position
  const value = async (i) => Number(await fields.nth(i).inputValue())
  const shown = (v) => Math.round(v * 1000) / 1000 || 0
  // Fields read in Blender's axes: Y is depth (document -Z), Z is up (document Y).
  assert.ok(close(await value(0), shown(px)), 'x')
  assert.ok(close(await value(1), shown(-pz)), 'y')
  assert.ok(close(await value(2), shown(py)), 'z')
  assert.equal(server.reads, 2)
  assert.deepEqual([...first.errors, ...again.errors], [])
  await again.context.close()
})

await check('fog written as three.js Fog {color, near, far} shows in the view', async () => {
  const clear = await open(new SceneServer())
  const before = await clear.page.locator('.cc-scene-editor canvas').screenshot()
  await clear.context.close()
  const server = new SceneServer()
  server.scene.environment.fog = { color: '#ff00ff', near: 1, far: 8 }
  const foggy = await open(server)
  const after = await foggy.page.locator('.cc-scene-editor canvas').screenshot()
  const diff = await pixelDiff(foggy.page, before, after)
  assert.ok(diff > 0.2, `fog changes ${diff} of the picture`)
  assert.deepEqual([...clear.errors, ...foggy.errors], [])
  await foggy.context.close()
})

await check('undo and redo go through restore_units', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  const start = [...server.object(id).transform.position]
  await page.keyboard.press('KeyG')
  await page.keyboard.press('KeyX')
  await page.keyboard.press('Digit4')
  await page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === 2, 'move')
  await saved(page)
  const mod = process.platform === 'darwin' ? 'Meta' : 'Control'
  await page.keyboard.press(`${mod}+KeyZ`)
  await waitFor(() => server.commands.length === 3, 'undo')
  const undo = server.commands[2].payload.operations[0]
  assert.equal(undo.op, 'restore_units')
  assert.ok(close(server.object(id).transform.position[0], start[0]), 'back where it was')
  await saved(page)
  await page.keyboard.press(`${mod}+KeyZ`)
  await waitFor(() => server.commands.length === 4, 'undo the add')
  assert.equal(server.object(id), undefined, 'undoing the add removes the box')
  await saved(page)
  await page.keyboard.press(`${mod}+Shift+KeyZ`)
  await waitFor(() => server.commands.length === 5, 'redo the add')
  assert.ok(server.object(id), 'redo brings the same object back')
  await saved(page)
  await page.keyboard.press(`${mod}+Shift+KeyZ`)
  await waitFor(() => server.commands.length === 6, 'redo the move')
  assert.ok(close(server.object(id).transform.position[0], start[0] + 4))
  await saved(page)
  assert.equal(await page.getByRole('treeitem', { name: /方块/ }).count(), 1)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('slow saves: add then S stays two steps, and Cmd+Z waits for the save instead of failing', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const { x, y } = await viewportCenter(page)
  const mod = process.platform === 'darwin' ? 'Meta' : 'Control'
  let release
  server.hold = new Promise((resolve) => (release = resolve))
  const add = async (dx) => {
    await page.mouse.move(x + dx, y)
    await page.keyboard.press('Shift+KeyA')
    await page.getByRole('menuitem', { name: '方块' }).click()
  }
  const boxes = () => page.getByRole('treeitem', { name: /方块/ }).count()
  const scaleX = () => page.getByRole('group', { name: '缩放' }).getByLabel('X').inputValue()
  await add(-160) // goes out and waits on the server
  await add(160) // waits behind it
  await page.mouse.move(x + 200, y + 20)
  await page.keyboard.press('KeyS')
  await page.mouse.move(x + 320, y + 80, { steps: 5 })
  await page.mouse.down()
  await page.mouse.up()
  await waitFor(async () => (await scaleX()) !== '1', 'scaled')
  await page.keyboard.press(`${mod}+KeyZ`)
  await waitFor(async () => (await scaleX()) === '1', 'the first Cmd+Z drops the scale')
  assert.equal(await boxes(), 2, 'and keeps the box')
  await page.keyboard.press(`${mod}+KeyZ`)
  await waitFor(async () => (await boxes()) === 1, 'the second drops the unsent box')
  await page.keyboard.press(`${mod}+KeyZ`)
  await waitFor(async () => (await page.locator('.cc-scene-hint').textContent()) === '正在保存，保存后撤销', 'waiting notice')
  assert.equal(await boxes(), 1, 'the box being saved waits for its receipt')
  await page.keyboard.press(`${mod}+KeyZ`)
  assert.equal(await page.locator('.cc-scene-save').count(), 1, 'the same notice again keeps the editor up')
  server.hold = null
  release()
  await waitFor(() => server.commands.length === 2, 'saved, then undone')
  assert.deepEqual(server.commands.map((c) => c.payload.operations[0].op), ['add_objects', 'restore_units'])
  assert.equal(server.scene.objects.length, 1, 'only the ground is left')
  await saved(page)
  assert.equal(await boxes(), 0)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('7 then 5 at once gives a top orthographic view framed like the perspective one', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  await addBox(page, server)
  const { box } = await viewportCenter(page)
  await page.keyboard.press('Digit7')
  await page.keyboard.press('Digit5')
  await sleep(1200)
  const ortho = await page.screenshot({ clip: box })
  await shot(page, 'c4-ortho-top')
  await page.keyboard.press('Digit5')
  await sleep(800)
  const persp = await page.screenshot({ clip: box })
  // A view still turning, or an orthographic camera left at zoom 1, differs wholesale.
  const diff = await pixelDiff(page, ortho, persp)
  assert.ok(diff < 0.05, `top orthographic vs perspective differ on ${(diff * 100).toFixed(1)}% of pixels`)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('leaving and coming back, even after a reload, shows the editor as it was left', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  await page.getByRole('button', { name: '移动', exact: true }).click()
  await page.keyboard.press('Digit7')
  await page.keyboard.press('Digit5')
  await sleep(1200)
  const { box } = await viewportCenter(page)
  const left = await page.screenshot({ clip: box })
  await page.getByRole('button', { name: '还原节点' }).click()
  await page.locator('#closed').waitFor()
  await page.evaluate(() => window.__reopen())
  await page.locator('.cc-scene-editor canvas').waitFor()
  await page.getByRole('treeitem', { name: /方块/ }).waitFor()
  const restored = async (when) => {
    assert.equal(await page.getByRole('button', { name: '移动', exact: true }).getAttribute('aria-pressed'), 'true', `${when}: the tool`)
    assert.equal(await page.getByRole('treeitem', { name: /方块/ }).getAttribute('aria-selected'), 'true', `${when}: the selection`)
    await sleep(1200)
    const diff = await pixelDiff(page, await page.screenshot({ clip: box }), left)
    assert.ok(diff < 0.005, `${when}: the same top orthographic view (${(diff * 100).toFixed(2)}% of pixels differ)`)
  }
  await restored('reopened')
  await page.reload()
  await page.locator('.cc-scene-editor canvas').waitFor({ timeout: 20_000 })
  await page.getByRole('treeitem', { name: /方块/ }).waitFor()
  await restored('reloaded')
  // A perspective view comes back too, from an angle no number key gives.
  await page.keyboard.press('Digit5')
  await sleep(600)
  const { x, y } = await viewportCenter(page)
  await page.keyboard.down('Alt')
  await page.mouse.move(x + 200, y + 150)
  await page.mouse.down()
  await page.mouse.move(x + 120, y + 60, { steps: 8 })
  await page.mouse.up()
  await page.keyboard.up('Alt')
  await sleep(1200)
  const turned = await page.screenshot({ clip: box })
  assert.ok((await pixelDiff(page, turned, left)) > 0.02, 'the turned view is a different picture')
  await page.reload()
  await page.locator('.cc-scene-editor canvas').waitFor({ timeout: 20_000 })
  await page.getByRole('treeitem', { name: /方块/ }).waitFor()
  // The first frames after a reload can come late under load: compare until it settles.
  let back = 1
  for (const started = Date.now(); back >= 0.005 && Date.now() - started < 5000; await sleep(200))
    back = await pixelDiff(page, await page.screenshot({ clip: box }), turned)
  assert.ok(back < 0.005, `the turned perspective view comes back (${(back * 100).toFixed(2)}% of pixels differ)`)
  assert.equal(server.commands.length, 1, 'the view is not an edit')
  assert.ok(server.object(id))
  assert.deepEqual(errors, [])
  await context.close()
})

await check("someone else's edit: a re-read shows it, a stale edit asks first", async () => {
  const server = new SceneServer()
  const a = await open(server)
  const id = await addBox(a.page, server)
  const b = await open(server, { context: await browser.newContext({ viewport: { width: 1280, height: 820 } }) })
  await b.page.getByRole('treeitem', { name: /方块/ }).click()

  // A moves the box; B learns of it from the canvas page's notification.
  await a.page.keyboard.press('KeyG')
  await a.page.keyboard.press('KeyX')
  await a.page.keyboard.press('Digit1')
  await a.page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === 2, 'A moves')
  await b.page.evaluate(() => window.__invalidate())
  const bx = b.page.locator('.cc-scene-props input[type=number]').first()
  await waitFor(async () => close(Number(await bx.inputValue()), Math.round(server.object(id).transform.position[0] * 1000) / 1000), 'B shows the move')

  // A moves again; B, not yet told, moves the same box and is asked.
  server.external([{ op: 'update_objects', updates: [{ object_id: id, transform: { ...server.object(id).transform, position: [5, 0, 5] } }] }])
  await b.page.keyboard.press('KeyG')
  await b.page.keyboard.press('KeyZ')
  await b.page.keyboard.press('Digit1')
  await b.page.keyboard.press('Enter')
  await b.page.getByRole('alert').filter({ hasText: '重新应用' }).waitFor()
  await shot(b.page, '5-conflict')
  assert.equal(await saveText(b.page), '有冲突待处理')
  assert.deepEqual(server.object(id).transform.position, [5, 0, 5], 'the conflicting edit did not land')
  await b.page.getByRole('button', { name: '还原节点' }).click()
  await b.page.getByText('有修改因冲突没有保存').waitFor()
  await b.page.getByRole('button', { name: '返回处理' }).click()
  assert.equal(await b.page.evaluate(() => window.__closed), 0, 'closing waits for a decision')
  await b.page.getByRole('button', { name: '重新应用我的修改' }).click()
  await waitFor(() => close(server.object(id).transform.position[1], 1, 1e-6), 'reapplied on top of the latest')
  await saved(b.page)
  assert.deepEqual([...a.errors, ...b.errors], [])
  await a.context.close()
  await b.context.close()
})

await check('a trashed scene turns read-only and keeps the edit for after untrash', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  server.trashedAt = '2026-10-16T10:00:00Z'
  await page.keyboard.press('KeyG')
  await page.keyboard.press('KeyX')
  await page.keyboard.press('Digit2')
  await page.keyboard.press('Enter')
  await page.getByText('场景在回收站，请先在画布上恢复后再编辑').waitFor()
  assert.equal(await saveText(page), '只读')
  await page.keyboard.press('Shift+KeyA')
  assert.equal(await page.getByRole('menu').count(), 0, 'no add menu while trashed')
  await page.getByRole('alert').filter({ hasText: '回收站' }).waitFor()
  server.trashedAt = null
  await page.evaluate(() => window.__invalidate())
  await waitFor(async () => (await page.getByText('场景在回收站，请先在画布上恢复后再编辑').count()) === 0, 'untrashed')
  await page.getByRole('button', { name: '重新应用我的修改' }).click()
  await waitFor(() => server.commands.length === 2, 'kept edit saved')
  assert.ok(close(server.object(id).transform.position[0], 2, 1e-6) || server.object(id).transform.position[0] > 1.5)
  await saved(page)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('history lists revisions and restores one as a new revision', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  await addBox(page, server)
  await page.getByRole('button', { name: '历史' }).click()
  const panel = page.getByRole('complementary', { name: '历史版本' })
  await panel.waitFor()
  await waitFor(async () => (await panel.locator('li').count()) >= 1, 'history items')
  assert.match(await panel.locator('li').last().textContent(), /新建场景/, 'the creation lists on its own')
  await panel.locator('li button').last().click()
  await page.getByText(/正在预览第 1 版/).first().waitFor()
  await shot(page, '4-history-preview')
  assert.equal(await page.getByRole('treeitem', { name: /方块/ }).count(), 0, 'revision 1 has only the ground')
  await page.getByRole('button', { name: '恢复到此版本' }).click()
  await page.getByRole('button', { name: '恢复', exact: true }).click()
  await waitFor(() => server.commands.length === 2, 'whole restore')
  assert.equal(server.history.at(-1).restored_from, '1')
  await waitFor(async () => (await page.getByRole('treeitem', { name: /方块/ }).count()) === 0, 'box gone after restore')
  assert.equal(server.scene.objects.length, 1)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('selection edits: box select, H / Alt+H, Shift+D, X', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const { x, y, box } = await viewportCenter(page)
  const first = await addBox(page, server)
  const second = await addBox(page, server)

  // Box select from empty ground around both; the locked ground stays out.
  await page.keyboard.press('Alt+KeyA')
  await page.mouse.move(box.x + 40, box.y + 40)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width - 40, box.y + box.height - 40, { steps: 8 })
  await page.mouse.up()
  const selectedRows = async () => page.locator('[role=treeitem][aria-selected=true]').allTextContents()
  assert.deepEqual(await selectedRows(), ['方块', '方块'], 'box select takes both boxes, not the locked ground')

  await page.keyboard.press('KeyH')
  await waitFor(() => server.commands.length === 3, 'hide')
  assert.deepEqual([server.object(first).visible, server.object(second).visible], [false, false])
  await page.keyboard.press('Alt+KeyH')
  await waitFor(() => server.commands.length === 4, 'reveal')
  assert.deepEqual([server.object(first).visible, server.object(second).visible], [true, true])
  await saved(page)

  // Shift+D copies and moves in one go: one add, nothing in between.
  await page.getByRole('treeitem').nth(1).click()
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyD')
  await page.mouse.move(x + 60, y + 20, { steps: 5 })
  await sleep(150)
  assert.equal(server.commands.length, 4, 'the copy waits until the move ends')
  await page.mouse.click(x + 60, y + 20)
  await waitFor(() => server.commands.length === 5, 'duplicate')
  const dup = server.commands[4].payload.operations
  assert.equal(dup.length, 1)
  assert.equal(dup[0].op, 'add_objects')
  const copy = dup[0].objects[0]
  assert.notEqual(copy.id, first)
  assert.notDeepEqual(copy.transform.position, server.object(first).transform.position, 'the copy lands where it was moved')
  await saved(page)

  // X asks first; the menu's single item deletes.
  await page.keyboard.press('KeyX')
  await page.getByRole('menuitem', { name: '删除 1 个物件' }).click()
  await waitFor(() => server.commands.length === 6, 'delete')
  assert.equal(server.object(copy.id), undefined)
  await saved(page)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('a held key acts once, and clearing what is already clear saves nothing', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  await page.keyboard.press('KeyG')
  await page.keyboard.press('KeyX')
  await page.keyboard.press('Digit2')
  await page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === 2, 'move off the origin')
  await saved(page)

  // Keydowns after the first are repeats while the key stays down.
  const hold = async (code, modifier) => {
    if (modifier) await page.keyboard.down(modifier)
    for (let i = 0; i < 4; i++) await page.keyboard.down(code)
    await page.keyboard.up(code)
    if (modifier) await page.keyboard.up(modifier)
  }
  await hold('KeyG', 'Alt')
  await waitFor(() => server.commands.length === 3, 'clear location')
  await saved(page)
  assert.deepEqual(server.object(id).transform.position, [0, 0, 0])
  await page.keyboard.press('Alt+KeyG')
  await page.keyboard.press('Alt+KeyS')
  await sleep(300)
  assert.equal(server.commands.length, 3, 'a held Alt+G clears once; clearing again changes nothing and saves nothing')

  assert.ok(await page.getByRole('treeitem').count() > 0)
  await hold('KeyN')
  assert.equal(await page.getByRole('treeitem').count(), 0, 'a held N hides the sidebar once instead of flickering')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('the move tool drags the selection with its handle: one update', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  const before = [...server.object(id).transform.position]
  await page.getByRole('button', { name: '移动', exact: true }).click()
  const { x, y } = await viewportCenter(page)
  // The handle's centre sits on the box's origin, the middle of the view.
  await page.mouse.move(x - 3, y + 2)
  await page.mouse.move(x, y)
  await page.mouse.down()
  await page.mouse.move(x + 90, y, { steps: 10 })
  await sleep(100)
  assert.equal(server.commands.length, 1, 'nothing goes out mid-drag')
  await page.mouse.up()
  await waitFor(() => server.commands.length === 2, 'gizmo move')
  const op = server.commands[1].payload.operations[0]
  assert.equal(op.op, 'update_objects')
  assert.deepEqual(op.updates.map((u) => u.object_id), [id])
  assert.ok(Math.hypot(...server.object(id).transform.position.map((v, i) => v - before[i])) > 0.2, 'it moved')
  await saved(page)
  await shot(page, '6-gizmo')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('properties commit 300 ms after typing stops; focusing alone writes nothing', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  const width = page.getByLabel('宽（m）')
  await width.click()
  await width.fill('3')
  const typed = Date.now()
  await waitFor(() => server.commands.length === 2, 'debounced commit')
  assert.ok(Date.now() - typed >= 250, 'not before the pause')
  assert.equal(server.object(id).primitive.params.width, 3)
  assert.deepEqual(server.commands[1].payload.expected, { [content(id)]: '2' }, 'a parameter is a content edit')
  await page.keyboard.press('Enter')
  await sleep(400)
  assert.equal(server.commands.length, 2, 'Enter after the commit sends nothing more')
  // Rotation shows rounded degrees; focusing and leaving must not write them back.
  const rx = page.locator('.cc-scene-props fieldset').nth(1).locator('input').first()
  await rx.click()
  await page.getByLabel('名称').click()
  await sleep(400)
  assert.equal(server.commands.length, 2)
  await saved(page)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('key boundaries: fields, Esc, Cmd/Ctrl+S, ?, nothing leaks to the page', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  await addBox(page, server)
  await page.evaluate(() => {
    window.__leaked = 0
    window.addEventListener('keydown', () => window.__leaked++)
  })
  const mod = process.platform === 'darwin' ? 'Meta' : 'Control'
  assert.match(await page.locator('.cc-scene-hint').textContent(), /G 移动 · R 旋转/)
  await page.getByRole('button', { name: '物件' }).click()
  assert.equal(await page.getByRole('menuitem', { name: /复制/ }).locator('kbd').textContent(), process.platform === 'darwin' ? '⇧D' : 'Shift+D')
  await page.keyboard.press('Escape')

  const name = page.getByLabel('名称')
  await name.click()
  await name.fill('grsx 方块')
  await page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === 2, 'rename')
  assert.deepEqual(Object.keys(server.commands[1].payload.operations[0].updates[0]), ['object_id', 'name'], 'letters typed in a field are text, not shortcuts')

  for (let i = 0; i < 3; i++) await page.keyboard.press('Escape')
  assert.equal(await page.evaluate(() => window.__closed), 0, 'Esc never closes the view')
  assert.equal(await page.locator('[role=treeitem][aria-selected=true]').count(), 0, 'Esc clears the selection')

  await page.keyboard.press(`${mod}+KeyS`)
  await waitFor(async () => (await page.locator('.cc-scene-hint').textContent()) === '场景会自动保存，不需要手动保存', 'save notice')
  await page.keyboard.press('Shift+Slash')
  await page.getByRole('dialog', { name: /快捷键/ }).waitFor()
  await page.keyboard.press('Escape')
  // From a field, a click in the viewport gives the keys back to the editor.
  await page.getByRole('treeitem', { name: /方块/ }).click()
  await page.getByLabel('名称').click()
  const { box } = await viewportCenter(page)
  await page.mouse.click(box.x + 30, box.y + 30)
  await page.keyboard.press('KeyA')
  assert.equal(await page.locator('[role=treeitem][aria-selected=true]').count(), 1, 'A selects the box after a viewport click')
  await page.keyboard.press('KeyG')
  await page.keyboard.press('KeyA')
  assert.equal(await page.evaluate(() => window.__leaked), 0, 'no key reaches listeners behind the editor')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('leaving with unsaved edits is intercepted; saved, it is not', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  let release
  server.hold = new Promise((r) => (release = r))
  await page.keyboard.press('KeyG')
  await page.keyboard.press('KeyX')
  await page.keyboard.press('Digit1')
  await page.keyboard.press('Enter')
  await waitFor(async () => (await saveText(page)) === '保存中', 'saving')
  const dialogs = []
  page.on('dialog', (d) => {
    dialogs.push(d.type())
    void d.dismiss()
  })
  await page.close({ runBeforeUnload: true })
  await waitFor(() => dialogs.length === 1, 'beforeunload prompt')
  assert.deepEqual(dialogs, ['beforeunload'])
  assert.equal(page.isClosed(), false, 'dismissing keeps the page')
  release()
  server.hold = null
  await waitFor(() => server.commands.length === 2, 'saved after all')
  await saved(page)
  assert.ok(server.object(id).transform.position[0] > 0.5)
  await page.close({ runBeforeUnload: true })
  await waitFor(() => page.isClosed(), 'closes without asking once saved')
  assert.equal(dialogs.length, 1)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('without WebGL2 the view explains instead of going blank', async () => {
  const server = new SceneServer()
  const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
  await context.addInitScript(() => {
    const real = HTMLCanvasElement.prototype.getContext
    HTMLCanvasElement.prototype.getContext = function (type, ...rest) {
      return type === 'webgl2' ? null : real.call(this, type, ...rest)
    }
  })
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  await route(page, server)
  await page.goto(`${base}/.scene-editor-qa.html`)
  await page.getByText('当前浏览器无法显示 3D 场景，请使用最新版 Chrome、Edge 或 Safari').waitFor()
  assert.equal(await page.locator('.cc-scene-editor canvas').count(), 0)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('trackpad swipes and pinches move the view at once, without a click', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  await addBox(page, server)
  await page.getByRole('button', { name: '视图' }).click()
  await page.getByRole('menuitem', { name: '触控板' }).click()
  const { x, y, box } = await viewportCenter(page)
  const view = () => page.screenshot({ clip: box })
  await page.mouse.move(x, y)
  // The first wheel settles the device guess, a re-render that would hide the bug.
  await page.mouse.wheel(1, 0)
  await sleep(300)
  let before = await view()
  // Two-finger swipe orbits; the frame must change with no click after it.
  for (let i = 0; i < 5; i++) await page.mouse.wheel(40, 0)
  await sleep(300)
  let after = await view()
  assert.ok(!before.equals(after), 'a two-finger swipe redraws the view by itself')
  before = after
  // A pinch arrives as a wheel with ctrlKey.
  await page.keyboard.down('Control')
  for (let i = 0; i < 5; i++) await page.mouse.wheel(0, -30)
  await page.keyboard.up('Control')
  await sleep(300)
  after = await view()
  assert.ok(!before.equals(after), 'a pinch redraws the view by itself')
  assert.equal(server.commands.length, 1, 'moving the view saves nothing')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('number keys and the axis widget turn the view as in Blender', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  const { x, y, box } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('Digit1')
  await sleep(900)
  // Front view: the widget's Z head sits straight above its centre.
  await page.mouse.click(box.x + box.width - 64, box.y + 64 - 40)
  await sleep(900)
  assert.equal(await page.getByRole('treeitem', { name: /方块/ }).getAttribute('aria-selected'), 'true', 'a press on the widget is not a selection click')
  // Top view: right on screen is +X, up on screen is +Y (document -Z).
  const at = () => [...server.object(id).transform.position]
  let before = at()
  await page.mouse.move(x, y)
  await page.keyboard.press('KeyG')
  await page.mouse.move(x + 100, y, { steps: 5 })
  await page.mouse.click(x + 100, y)
  await waitFor(() => server.commands.length === 2, 'move right')
  let after = at()
  assert.ok(after[0] - before[0] > 0.3 && close(after[1], before[1], 1e-6) && close(after[2], before[2], 1e-3), `right is +X: ${before} → ${after}`)
  await saved(page)
  before = after
  await page.mouse.move(x, y)
  await page.keyboard.press('KeyG')
  await page.mouse.move(x, y - 100, { steps: 5 })
  await page.mouse.click(x, y - 100)
  await waitFor(() => server.commands.length === 3, 'move up')
  after = at()
  assert.ok(before[2] - after[2] > 0.3 && close(after[0], before[0], 1e-3), `up is +Y, document -Z: ${before} → ${after}`)
  await saved(page)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('the editor makes room for the open assistant panel', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const editor = page.locator('.cc-scene-editor')
  const before = await editor.boundingBox()
  await page.evaluate(() => {
    const panel = document.createElement('aside')
    panel.className = 'cc-agent-panel'
    document.getElementById('root').appendChild(panel)
  })
  await sleep(100)
  const after = await editor.boundingBox()
  assert.ok(after.x + after.width <= 1280 - 380 - 24 + 1, `editor ends at ${after.x + after.width}`)
  assert.ok(after.width < before.width)
  await page.setViewportSize({ width: 700, height: 820 })
  await sleep(100)
  const narrow = await editor.boundingBox()
  assert.ok(narrow.x + narrow.width >= 700 - 12 - 1, 'a bottom-sheet panel takes no width from the editor')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('opening the canvas page loads no three.js', async () => {
  const context = await browser.newContext()
  const page = await context.newPage()
  const requested = []
  page.on('request', (r) => requested.push(r.url()))
  await page.goto(`${base}/.scene-canvas-qa.html`)
  await page.waitForFunction(() => window.__loaded === true, null, { timeout: 20_000 })
  await sleep(300)
  const three = requested.filter((u) => /\/(three|@react-three)[/_.]/.test(u) || /deps\/(three|@react-three)/.test(u))
  assert.deepEqual(three, [], 'the editor and three load only when a scene opens')
  assert.ok(requested.some((u) => u.includes('WorkspacePage')), 'the canvas page itself loaded')
  await context.close()
})

// --- code models (C2): the real sandbox, mocked uploads and tickets -------

const mod = process.platform === 'darwin' ? 'Meta' : 'Control'
const objectID = (tag) => `sobj_${tag.padEnd(21, '0')}`
const codePanel = (page) => page.getByRole('complementary', { name: '代码' })

/** Replaces the whole text in the code panel's editor. */
async function replaceCode(page, code) {
  await page.locator('.cc-scene-code .cm-content').click()
  await page.keyboard.press(`${mod}+KeyA`)
  await page.keyboard.insertText(code)
}

async function generateButton(page, name) {
  const button = codePanel(page).getByRole('button', { name, exact: true })
  await waitFor(() => button.isEnabled(), `${name} ready`, 15_000)
  return button
}

await check('a code model: 代码模型… then 生成 is one command at the menu point, and it is there after reopening', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const boxAt = server.object(await addBox(page, server)).transform.position
  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '代码模型…' }).click()
  const generate = await generateButton(page, '生成')
  assert.match(await codePanel(page).locator('.cm-content').textContent(), /export const params/, 'a new model starts from the template')
  await generate.click()
  await waitFor(() => server.commands.length === 2, 'create command', 20_000)
  await saved(page)
  const { operations, expected } = server.commands[1].payload
  assert.deepEqual(operations.map((op) => op.op), ['commit_model', 'add_objects'], 'one command: the model and its instance')
  const [commit, add] = operations
  assert.equal(commit.generator.code, defaultModuleCode)
  assert.equal(commit.base_content_id, undefined)
  assert.deepEqual(commit.manifest, templateManifest, 'the host computed the manifest')
  assert.equal(server.uploads.get(commit.glb_upload_id).kind, 'model')
  assert.equal(server.uploads.get(commit.thumbnail_upload_id).mime, 'image/png')
  assert.ok(server.uploadBytes.get(commit.glb_upload_id).equals(templateGLB), 'the uploaded GLB is the template baked by the real sandbox')
  assert.deepEqual(add.objects[0].transform.position, boxAt, 'it lands where the menu opened, like a box')
  assert.deepEqual(add.objects[0].model, { ref: commit.ref })
  assert.deepEqual(Object.keys(expected), ['objects'])
  const item = page.getByRole('treeitem', { name: /代码模型/ })
  await item.waitFor()
  assert.equal(await item.getAttribute('aria-selected'), 'true', 'the new model is selected')
  await generateButton(page, '重新生成')
  assert.match(server.object(add.objects[0].id).model.content_revision_id, /^ccrv_m/)
  await sleep(500)
  await shot(page, 'model-created')
  assert.deepEqual(errors, [])
  await context.close()

  const again = await open(server)
  await again.page.getByRole('treeitem', { name: /代码模型/ }).click()
  await again.page.getByLabel('高度（m）', { exact: true }).waitFor()
  await sleep(500)
  assert.equal(await again.page.getByText('模型不可用').count(), 0, 'the saved model loads by ticket')
  assert.deepEqual(again.errors, [])
  await again.context.close()
})

await check('a parameter change rebakes into a new revision; Cmd+Z and redo switch the model back and forth', async () => {
  const server = new SceneServer()
  const rev = server.seedModel()
  const pillar = objectID('pillar')
  server.seedInstance(pillar, rev)
  const { context, page, errors } = await open(server)
  await page.getByRole('treeitem', { name: /柱/ }).click()
  const height = page.getByLabel('高度（m）', { exact: true })
  await height.fill('5')
  await height.press('Enter')
  await waitFor(() => server.commands.length === 1, 'retune command', 20_000)
  await saved(page)
  const [commit, set] = server.commands[0].payload.operations
  assert.equal(commit.base_content_id, server.models.get(rev).content_id, 'an unshared model gets a new revision of the same content')
  assert.equal(commit.generator.params.height, 5)
  assert.equal(commit.manifest.bounds.max[1], 5, 'the bake used the new height')
  assert.deepEqual(set, { op: 'set_instance_model', object_id: pillar, model: { ref: commit.ref }, scope: 'this' })
  assert.deepEqual(Object.keys(server.commands[0].payload.expected), [content(pillar)])
  const next = server.object(pillar).model.content_revision_id
  assert.notEqual(next, rev)

  await page.locator('.cc-scene-editor canvas').click({ position: { x: 20, y: 20 } })
  await page.keyboard.press(`${mod}+KeyZ`)
  await waitFor(() => server.commands.length === 2, 'undo')
  assert.deepEqual(server.commands[1].payload.operations, [{ op: 'restore_units', from_revision: '1', units: [content(pillar)] }])
  assert.equal(server.object(pillar).model.content_revision_id, rev, 'undo brings the old model back')
  await saved(page)
  await page.keyboard.press(`${mod}+Shift+KeyZ`)
  await waitFor(() => server.commands.length === 3, 'redo')
  assert.equal(server.object(pillar).model.content_revision_id, next)
  await saved(page)
  await page.getByRole('treeitem', { name: /柱/ }).click()
  await waitFor(async () => (await page.getByLabel('高度（m）', { exact: true }).inputValue()) === '5', 'the panel shows the redone value')

  // Past the definition's max, the field sends the max.
  await height.fill('99')
  await height.press('Enter')
  await waitFor(() => server.commands.length === 4, 'clamped retune', 20_000)
  await saved(page)
  assert.equal(server.commands[3].payload.operations[0].generator.params.height, 12, 'clamped to max')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('a shared model forks by default; 应用到全部 switches every instance', async () => {
  const server = new SceneServer()
  const rev = server.seedModel()
  const [a, b, c] = ['a', 'b', 'c'].map((t) => objectID(`pillar${t}`))
  server.seedInstance(a, rev, -2, '柱 A')
  server.seedInstance(b, rev, 0, '柱 B')
  server.seedInstance(c, rev, 2, '柱 C')
  const { context, page, errors } = await open(server)
  await page.getByRole('treeitem', { name: /柱 A/ }).click()
  await page.getByText('与 2 个实例共用').waitFor()
  const height = page.getByLabel('高度（m）', { exact: true })
  await height.fill('4')
  await height.press('Enter')
  await waitFor(() => server.commands.length === 1, 'fork command', 20_000)
  await saved(page)
  const [fork, forkSet] = server.commands[0].payload.operations
  assert.equal(fork.base_content_id, undefined, 'only this one: a new model')
  assert.equal(forkSet.scope, 'this')
  assert.equal(server.object(b).model.content_revision_id, rev, 'the others keep theirs')
  assert.equal(server.object(c).model.content_revision_id, rev)
  await sleep(500)
  await shot(page, 'model-forked')

  await page.getByRole('treeitem', { name: /柱 B/ }).click()
  await page.getByText('与 1 个实例共用').waitFor()
  await page.getByLabel('应用到全部').check()
  const heightB = page.getByLabel('高度（m）', { exact: true })
  await heightB.fill('6')
  await heightB.press('Enter')
  await waitFor(() => server.commands.length === 2, 'all-instances command', 20_000)
  await saved(page)
  const [all, allSet] = server.commands[1].payload.operations
  assert.equal(all.base_content_id, server.models.get(rev).content_id)
  assert.equal(allSet.scope, 'all_instances')
  assert.deepEqual(allSet.object_ids, [b, c])
  assert.deepEqual(Object.keys(server.commands[1].payload.expected).sort(), [content(b), content(c)].sort())
  assert.equal(server.object(b).model.content_revision_id, server.object(c).model.content_revision_id)
  assert.notEqual(server.object(b).model.content_revision_id, rev)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('someone else changing the model while it bakes and uploads: the save asks first', async () => {
  const server = new SceneServer()
  const rev = server.seedModel()
  const theirs = server.seedModel({ params: { ...templateParams, height: 2 } })
  const pillar = objectID('pillar')
  server.seedInstance(pillar, rev)
  const { context, page, errors } = await open(server)
  await page.getByRole('treeitem', { name: /柱/ }).click()
  let release
  server.uploadHold = new Promise((resolve) => (release = resolve))
  const height = page.getByLabel('高度（m）', { exact: true })
  await height.fill('5')
  await height.press('Enter')
  await waitFor(() => server.uploads.size === 2, 'both uploads started', 20_000)

  // While the uploads wait, another tab switches the same instance's model,
  // and this tab reads it.
  server.external([{ op: 'set_instance_model', object_id: pillar, model: { content_revision_id: theirs }, scope: 'this' }])
  const reads = server.reads
  await page.evaluate(() => window.__invalidate())
  await waitFor(() => server.reads > reads, 're-read')
  await sleep(300)
  server.uploadHold = null
  release()

  await page.getByRole('alert').filter({ hasText: '重新应用' }).waitFor({ timeout: 20_000 })
  assert.equal(await saveText(page), '有冲突待处理')
  assert.equal(server.commands.length, 0, 'nothing was saved over theirs')
  assert.equal(server.object(pillar).model.content_revision_id, theirs)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('a syntax error shows its line and changes nothing; new parameter definitions come through on 重新生成', async () => {
  const server = new SceneServer()
  const rev = server.seedModel()
  const pillar = objectID('pillar')
  server.seedInstance(pillar, rev)
  const { context, page, errors } = await open(server)
  await page.getByRole('treeitem', { name: /柱/ }).click()
  await page.getByRole('button', { name: '编辑代码' }).click()
  let regenerate = await generateButton(page, '重新生成')
  assert.match(await codePanel(page).locator('.cm-content').textContent(), /export const params/, 'the model\'s own code is loaded')
  await replaceCode(page, 'export const mount = "floor"\nexport default function build({ THREE }) {\n  return new THREE.Group(\n}\n')
  await regenerate.click()
  await codePanel(page).getByText(/^第 4 行：/).waitFor({ timeout: 10_000 })
  assert.equal(await page.locator('.cc-code-error-line').count(), 1, 'the line is marked')
  assert.equal(server.commands.length, 0, 'the scene is unchanged')
  assert.equal(server.uploads.size, 0, 'nothing was uploaded')

  await replaceCode(page, defaultModuleCode.replace('  broken: {', "  width: { type: 'number', default: 1, min: 0.5, max: 3, unit: 'm', label: '宽度' },\n  broken: {"))
  regenerate = await generateButton(page, '重新生成')
  await regenerate.click()
  await waitFor(() => server.commands.length === 1, 'regenerate command', 20_000)
  await saved(page)
  const [commit] = server.commands[0].payload.operations
  assert.deepEqual(commit.generator.params_schema.width, { type: 'number', default: 1, min: 0.5, max: 3, unit: 'm', label: '宽度' })
  assert.equal(commit.generator.params.width, 1, 'a new parameter starts at its default')
  assert.equal(await page.locator('.cc-code-error-line').count(), 0)
  await page.getByLabel('宽度（m）', { exact: true }).waitFor({ timeout: 10_000 })
  assert.deepEqual(errors, [])
  await context.close()
})

await check('a model over the limit is refused before anything is uploaded', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '代码模型…' }).click()
  await generateButton(page, '生成')
  await replaceCode(page, `export const mount = 'free'
export default function build({ THREE, lib }) {
  const g = new THREE.BufferGeometry()
  g.setAttribute('position', new THREE.BufferAttribute(new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]), 3))
  g.setIndex(new THREE.BufferAttribute(new Uint32Array(300001 * 3).map((_, i) => i % 3), 1))
  return new THREE.Mesh(g, lib.material('a'))
}
`)
  await (await generateButton(page, '生成')).click()
  await codePanel(page).getByText(/模型超出上限/).waitFor({ timeout: 15_000 })
  assert.equal(server.uploads.size, 0)
  assert.equal(server.commands.length, 0)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('with the focus in the code panel, Cmd+Z undoes text and leaves the scene alone', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  await addBox(page, server)
  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '代码模型…' }).click()
  await generateButton(page, '生成')
  const text = codePanel(page).locator('.cm-content')
  await text.click()
  await page.keyboard.press(`${mod}+End`)
  await page.keyboard.type('// 草稿')
  await waitFor(async () => (await text.textContent()).includes('// 草稿'), 'typed')
  await page.keyboard.press(`${mod}+KeyZ`)
  await waitFor(async () => !(await text.textContent()).includes('// 草稿'), 'text undone')
  await sleep(300)
  assert.equal(server.commands.length, 1, 'no restore_units went out')
  assert.equal(server.scene.objects.length, 2, 'the box is still there')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('a model that cannot be shown is a grey block and says so', async () => {
  const server = new SceneServer()
  const missing = server.seedModel()
  server.mediaMissing.add(missing)
  server.seedInstance(objectID('gone'), 'ccrv_gone1', -1, '已删模型')
  server.seedInstance(objectID('broken'), missing, 1, '坏模型')
  const { context, page, errors } = await open(server)
  await page.getByRole('treeitem', { name: /已删模型/ }).click()
  await page.getByText('模型不可用').waitFor()
  await page.getByRole('treeitem', { name: /坏模型/ }).click()
  await page.getByText('模型不可用').waitFor({ timeout: 10_000 })
  assert.deepEqual(errors, [])
  await context.close()
})

// --- the assistant's work, as the editor shows it ---------------------------

/** Pixels in the assistant's outline colour (look.ts agentEdge, #17a397). */
async function agentPixels(page, png) {
  return page.evaluate(async (src) => {
    const i = await new Promise((resolve) => { const x = new Image(); x.onload = () => resolve(x); x.src = src })
    const c = document.createElement('canvas')
    c.width = i.width
    c.height = i.height
    const g = c.getContext('2d')
    g.drawImage(i, 0, 0)
    const p = g.getImageData(0, 0, c.width, c.height).data
    let n = 0
    for (let k = 0; k < p.length; k += 4) if (Math.abs(p[k] - 23) < 30 && Math.abs(p[k + 1] - 163) < 30 && Math.abs(p[k + 2] - 151) < 30) n++
    return n
  }, `data:image/png;base64,${png.toString('base64')}`)
}
const boxObject = (id, name, x, order) => ({ id, kind: 'primitive', name, parent_id: null, order, transform: { ...identity(), position: [x, 0, 0] }, visible: true, locked: false, primitive: { builtin: 'box@1' } })
const agentBadge = (page, name) => page.getByRole('treeitem', { name: new RegExp(name) }).locator('.cc-scene-row-agent')
const bakeSummary = (id, target) => ({ id, kind: 'bake_model', state: 'queued', run_id: 'car_qa', created_at: '2026-10-18T00:00:00Z', target })

await check('the assistant’s changes are outlined and labelled for a minute; touching them warns without blocking', async () => {
  const server = new SceneServer()
  const chair = objectID('chair')
  const table = objectID('table')
  server.external([{ op: 'add_objects', objects: [boxObject(table, '桌子', 2.5, 1)] }])
  // The assistant added the chair 48 seconds ago: its outline has about 12 seconds left.
  server.external([{ op: 'add_objects', objects: [boxObject(chair, '椅子', -2.5, 2)] }], { actor: 'agent', at: Date.now() - 48_000 })
  const { context, page, errors } = await open(server)
  await agentBadge(page, '椅子').waitFor()
  assert.equal(await agentBadge(page, '桌子').count(), 0)
  const { box: clip } = await viewportCenter(page)
  assert.ok((await agentPixels(page, await page.screenshot({ clip }))) > 50, 'the chair is outlined in the assistant’s colour')
  // Selecting it says so; hiding it still saves.
  await page.getByRole('treeitem', { name: /椅子/ }).click()
  await waitFor(async () => (await page.locator('.cc-scene-hint').textContent()) === '助手正在修改此物件，你的修改可能让它重做', 'soft notice on select')
  await page.locator('.cc-scene-hint').filter({ hasNotText: '助手' }).waitFor({ timeout: 6000 })
  const before = server.commands.length
  await page.getByRole('treeitem', { name: /椅子/ }).getByRole('button', { name: '隐藏' }).click()
  await waitFor(() => server.commands.length === before + 1, 'the edit went out')
  await saved(page)
  assert.equal(await page.locator('.cc-scene-hint').textContent(), '助手正在修改此物件，你的修改可能让它重做', 'soft notice on edit')
  assert.equal(server.object(chair).visible, false)
  // The assistant moves the table now: its outline comes with the notification.
  server.external([{ op: 'update_objects', updates: [{ object_id: table, transform: { ...identity(), position: [3, 0, 0] } }] }], { actor: 'agent' })
  await page.evaluate(() => window.__invalidate())
  await agentBadge(page, '桌子').waitFor({ timeout: 4000 })
  // The chair's minute runs out; the table's has just begun.
  await agentBadge(page, '椅子').waitFor({ state: 'detached', timeout: 20_000 })
  assert.equal(await agentBadge(page, '桌子').count(), 1)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('a model the assistant is adding stands as a placeholder with the status line until its job ends', async () => {
  const server = new SceneServer()
  // The chair goes under the shelf, 3 m along from it: at the origin, in the middle of the view.
  const shelf = objectID('shelf')
  server.external([{ op: 'add_objects', objects: [boxObject(shelf, '架子', -3, 1)] }])
  server.jobs = [bakeSummary('csj_add', { op: 'add', object: { name: '椅子', parent_id: shelf, transform: { ...identity(), position: [3, 0, 0] } } })]
  const { context, page, errors } = await open(server)
  const activity = page.locator('.cc-scene-activity')
  await waitFor(async () => (await activity.count()) > 0 && (await activity.textContent()) === '助手正在生成「椅子」…', 'status line')
  await sleep(300)
  const { box: clip, x, y } = await viewportCenter(page)
  const drawn = await agentPixels(page, await page.screenshot({ clip }))
  assert.ok(drawn > 30, `the placeholder is drawn (${drawn} px)`)
  const middle = await agentPixels(page, await page.screenshot({ clip: { x: x - 70, y: y - 70, width: 140, height: 140 } }))
  assert.ok(middle > 10, `it stands where its parent puts it (${middle} px in the middle)`)
  // It cannot be picked: a click on it selects neither it nor the shelf it hangs from.
  await page.mouse.click(x, y)
  await sleep(200)
  assert.equal(await page.locator('[role=treeitem][aria-selected=true]').count(), 0)
  server.jobs = []
  await page.evaluate(() => window.__invalidate())
  await activity.waitFor({ state: 'detached', timeout: 4000 })
  await sleep(300)
  assert.ok((await agentPixels(page, await page.screenshot({ clip }))) < drawn / 10, 'the placeholder is gone with its job')
  // A bake replacing an object outlines it; two bakes say how many.
  server.jobs = [bakeSummary('csj_add', { op: 'add', object: { name: '椅子' } }), bakeSummary('csj_rep', { op: 'replace', object_ids: ['sobj_ground'] })]
  await page.evaluate(() => window.__invalidate())
  await agentBadge(page, '地面').waitFor({ timeout: 4000 })
  assert.equal(await activity.textContent(), '助手正在生成「椅子」等 2 个模型…')
  assert.equal(server.commands.length, 0)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('the editor view claims its own scene’s jobs before the account’s older ones', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server, { query: '?host=1' })
  await waitFor(() => server.claims.some((c) => c.document_id === DOC), 'the editor asks for its scene', 10_000)
  const job = (id, doc) => ({ id, document_id: doc, kind: 'render_view', request: { preset: 'overview' }, attempt: 0, claim_token: `tok_${id}`, lease_until: '2100-01-01T00:00:00Z', deadline: '2100-01-01T00:00:00Z' })
  server.claimable = [job('csj_other', 'cwdoc_other'), job('csj_this', DOC)]
  await page.evaluate(() => window.__invalidate())
  await waitFor(() => server.completes.length === 2, 'both jobs reported', 30_000)
  assert.deepEqual(server.completes.map((c) => c.id), ['csj_this', 'csj_other'])
  assert.equal(server.completes[0].payload.outcome, 'succeeded')
  assert.equal(server.completes[0].payload.render.scene_revision, '1')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('the editor hands its scene and selection to the assistant panel, and takes them back when trashed or closed', async () => {
  const server = new SceneServer()
  const chair = objectID('chair')
  server.external([{ op: 'add_objects', objects: [boxObject(chair, '椅子', 1, 1)] }])
  const { context, page, errors } = await open(server)
  const shared = async () => JSON.stringify(await page.evaluate(() => window.__context()))
  await waitFor(async () => (await shared()) === JSON.stringify({ documentID: DOC, selected: [], names: {} }), 'the scene, nothing selected')
  await page.getByRole('treeitem', { name: /椅子/ }).click()
  await waitFor(async () => (await shared()) === JSON.stringify({ documentID: DOC, selected: [chair], names: { [chair]: '椅子' } }), 'the selection')
  // A trashed scene cannot be worked on: a run that named it would be refused.
  server.trashedAt = '2026-10-18T10:00:00Z'
  await page.evaluate(() => window.__invalidate())
  await waitFor(async () => (await shared()) === 'null', 'nothing while trashed')
  server.trashedAt = null
  await page.evaluate(() => window.__invalidate())
  await waitFor(async () => (await shared()) !== 'null', 'back after untrash')
  await page.getByRole('button', { name: '还原节点' }).click()
  await page.locator('#closed').waitFor()
  assert.equal(await shared(), 'null')
  assert.deepEqual(errors, [])
  await context.close()
})


// --- cameras and export (C4) ------------------------------------------------

const pngSize = (bytes) => ({ width: bytes.readUInt32BE(16), height: bytes.readUInt32BE(20) })

/** Gradient energy of a PNG inside a rectangle given as fractions [x0, y0, x1, y1]: blur lowers it. */
async function sharpness(page, png, rect) {
  return page.evaluate(async ([src, rect]) => {
    const img = await new Promise((resolve) => { const i = new Image(); i.onload = () => resolve(i); i.src = src })
    const [x0, y0, x1, y1] = rect
    const w = Math.round(img.width * (x1 - x0)), h = Math.round(img.height * (y1 - y0))
    const c = document.createElement('canvas')
    c.width = w
    c.height = h
    const g = c.getContext('2d')
    g.drawImage(img, img.width * x0, img.height * y0, w, h, 0, 0, w, h)
    const d = g.getImageData(0, 0, w, h).data
    const lum = (i) => d[i] * 0.299 + d[i + 1] * 0.587 + d[i + 2] * 0.114
    let e = 0
    for (let y = 0; y < h - 1; y++) for (let x = 0; x < w - 1; x++) {
      const i = (y * w + x) * 4
      // Squared: blurring an edge keeps its summed gradient but spreads it thin.
      e += (lum(i) - lum(i + 4)) ** 2 + (lum(i) - lum(i + w * 4)) ** 2
    }
    return e / (w * h)
  }, [`data:image/png;base64,${png.toString('base64')}`, rect])
}

const cameraObject = (id, name, order, camera, position = [0, 1.6, 8]) => ({ id, kind: 'camera', name, parent_id: null, order, transform: { ...identity(), position }, visible: true, locked: false, camera })
const emptyObject = (id, name, order, position) => ({ id, kind: 'group', name, parent_id: null, order, transform: { ...identity(), position }, visible: true, locked: false })

await check('Add > 摄像机 stands at the view; 0 looks through it in a frame with guides; 1 leaves', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '摄像机' }).click()
  await waitFor(() => server.commands.length === 1, 'add camera')
  const cam = server.commands[0].payload.operations[0].objects[0]
  assert.deepEqual([cam.kind, cam.name, cam.camera], ['camera', '摄像机 1', {}])
  assert.ok(Math.hypot(...cam.transform.position) > 5, `stands where the view looks from: ${cam.transform.position}`)
  await page.keyboard.press('Digit0')
  const frame = page.locator('.cc-scene-camframe')
  await frame.waitFor()
  assert.match(await frame.textContent(), /摄像机 1 · 35 mm/)
  const box = await frame.boundingBox()
  const view = await viewportCenter(page)
  assert.ok(Math.abs(box.width / box.height - 1.5) < 0.02, `3:2 frame: ${box.width}×${box.height}`)
  assert.ok(Math.abs(Math.max(box.width / view.box.width, box.height / view.box.height) - 0.9) < 0.02, 'fills 90% of the viewport')
  await page.getByRole('button', { name: '构图辅助线' }).click()
  await page.getByRole('menu').getByLabel('三分线').check()
  await page.getByRole('menu').getByLabel('对角线').check()
  assert.equal(await frame.locator('line').count(), 6)
  await shot(page, 'c4-camera-view')
  await page.keyboard.press('Digit1')
  await frame.waitFor({ state: 'detached' })
  assert.deepEqual(errors, [])
  await context.close()
})

await check('0 again leaves the camera view for the view it came from, orthographic too', async () => {
  const server = new SceneServer()
  server.external([{ op: 'add_objects', objects: [cameraObject(objectID('cam'), '机位', 1, {})] }])
  const { context, page, errors } = await open(server)
  const { box } = await viewportCenter(page)
  const frame = page.locator('.cc-scene-camframe')
  const roundTrip = async (view) => {
    await sleep(1200)
    const before = await page.screenshot({ clip: box })
    await page.keyboard.press('Digit0')
    await frame.waitFor()
    await sleep(1200)
    assert.ok((await pixelDiff(page, await page.screenshot({ clip: box }), before)) > 0.02, `${view}: the camera view is a different picture`)
    await page.keyboard.press('Digit0')
    await frame.waitFor({ state: 'detached' })
    let back = 1
    for (const started = Date.now(); back >= 0.005 && Date.now() - started < 5000; await sleep(200))
      back = await pixelDiff(page, await page.screenshot({ clip: box }), before)
    assert.ok(back < 0.005, `${view}: back to the view it came from (${(back * 100).toFixed(2)}% of pixels differ)`)
  }
  await roundTrip('perspective')
  await page.keyboard.press('Digit7')
  await page.keyboard.press('Digit5')
  await roundTrip('top orthographic')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('Add > 空物体 shows a marker; the camera focuses on it by 拾取, its sharp range shows', async () => {
  const server = new SceneServer()
  const cam = objectID('cam')
  server.external([{ op: 'add_objects', objects: [cameraObject(cam, '机位', 1, { depth_of_field: true, f_stop: 2.8, focal_mm: 50 })] }])
  const { context, page, errors } = await open(server)
  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '空物体' }).click()
  await waitFor(() => server.commands.length === 1, 'add empty')
  const empty = server.commands[0].payload.operations[0].objects[0]
  assert.deepEqual([empty.kind, empty.name], ['group', '空物体 1'])
  await saved(page)
  await page.getByRole('treeitem', { name: /机位/ }).click()
  const props = page.getByRole('group', { name: '摄像机' })
  await props.getByLabel('物件').check()
  await waitFor(() => server.commands.length === 2, 'focus by object')
  await saved(page)
  await props.getByRole('button', { name: '拾取' }).click()
  // The empty sits where the view's centre hit; a click there picks it.
  await page.mouse.click(x, y)
  await waitFor(() => server.commands.length === 3, 'picked')
  const update = server.commands[2].payload.operations[0].updates[0]
  assert.deepEqual([update.object_id, update.camera], [cam, { focus_object_id: empty.id }])
  await saved(page)
  assert.match(await props.textContent(), /清晰范围 [\d.]+ m – [\d.]+ m/)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('a sensor preset changes only sensor_mm and the equivalent focal length', async () => {
  const server = new SceneServer()
  const cam = objectID('cam')
  server.external([{ op: 'add_objects', objects: [cameraObject(cam, '机位', 1, { focal_mm: 35 })] }])
  const { context, page, errors } = await open(server)
  await page.getByRole('treeitem', { name: /机位/ }).click()
  const props = page.getByRole('group', { name: '摄像机' })
  await props.getByRole('button', { name: '更多' }).click()
  await props.getByLabel('传感器预设').selectOption('APS-C')
  await waitFor(() => server.commands.length === 1, 'preset')
  assert.deepEqual(server.commands[0].payload.operations[0].updates[0].camera, { sensor_mm: [23.5, 15.6] })
  await saved(page)
  assert.match(await props.textContent(), /等效全画幅 53\.6 mm/)
  assert.deepEqual(errors, [])
  await context.close()
})

/** A scene to export: a box in focus 8 m ahead, a far tower, two cameras. */
function exportScene(dof) {
  const server = new SceneServer()
  const near = objectID('near'), far = objectID('far'), aim = objectID('aim'), a = objectID('cama'), b = objectID('camb')
  server.external([{ op: 'add_objects', objects: [
    { ...boxObject(near, '近处方块', 0, 1), transform: { ...identity(), position: [0, 0, 0] } },
    { ...boxObject(far, '远处塔', 0, 2), transform: { position: [4, 0, -40], rotation: [0, 0, 0, 1], scale: [3, 8, 3] } },
    emptyObject(aim, '空物体 1', 3, [0, 0.5, 0]),
    cameraObject(a, '机位 A', 4, { focal_mm: 50, depth_of_field: dof, f_stop: 1.4, focus_object_id: aim }, [0, 0.5, 8]),
    cameraObject(b, '机位 B', 5, { focal_mm: 35, aspect: '2:3' }, [6, 1.6, 6]),
  ] }])
  return { server, a, b }
}

async function runExport(page, server) {
  await page.getByRole('button', { name: '导出' }).first().click()
  const dialog = page.getByRole('dialog', { name: '导出分镜' })
  await dialog.waitFor()
  assert.match(await dialog.textContent(), /共 4 张/)
  await dialog.getByRole('button', { name: '导出' }).click()
  await waitFor(() => server.exports.length === 1, 'export submitted', 180_000)
  await dialog.waitFor({ state: 'detached' })
  const body = server.exports[0]
  return body.payload.shots.map((s) => ({ camera: s.camera_object_id, images: s.images.map((img) => ({ pass: img.pass, png: server.uploadBytes.get(img.upload_id) })) }))
}

await check('export: two cameras × clay and line, 2048 PNGs at each camera’s shape; depth of field blurs clay out of focus only', async () => {
  const started = Date.now()
  const off = exportScene(false)
  const first = await open(off.server, { query: '?export=1' })
  const sharp = await runExport(first.page, off.server)
  const ms = Date.now() - started
  const body = off.server.exports[0]
  assert.equal(body.operation_id.length, 36)
  assert.equal(body.payload.canvas_id, 'ccv_qa')
  assert.equal(body.payload.scene_node_id, 'cwnode_qa')
  assert.deepEqual(sharp.map((s) => [s.camera, s.images.map((i) => i.pass)]), [[off.a, ['clay', 'line']], [off.b, ['clay', 'line']]])
  assert.deepEqual(pngSize(sharp[0].images[0].png), { width: 2048, height: 1365 })
  assert.deepEqual(pngSize(sharp[1].images[1].png), { width: 1365, height: 2048 }, 'portrait camera, long side 2048')
  await first.page.locator('.cc-scene-hint').filter({ hasText: '已导出 4 张图到画布' }).waitFor()
  assert.deepEqual(first.errors, [])
  await first.context.close()

  const on = exportScene(true)
  const second = await open(on.server, { query: '?export=1' })
  const blurred = await runExport(second.page, on.server)
  const page = second.page
  const [sClay, bClay] = [sharp[0].images[0].png, blurred[0].images[0].png]
  if (process.env.SCENE_E2E_SHOTS) {
    for (const [name, png] of [['clay', sClay], ['clay-dof', bClay], ['line', sharp[0].images[1].png], ['portrait', sharp[1].images[0].png]]) await writeFile(`${process.env.SCENE_E2E_SHOTS}/c4-export-${name}.png`, png)
  }
  // The far tower's right edge (40 m, out of focus) and the near box's left edge (8 m, in focus).
  const tower = [0.62, 0.15, 0.7, 0.5]
  const box = [0.37, 0.4, 0.45, 0.6]
  const whole = [await sharpness(page, sClay, tower), await sharpness(page, bClay, tower)]
  const centre = [await sharpness(page, sClay, box), await sharpness(page, bClay, box)]
  assert.ok(whole[1] < whole[0] * 0.6, `out of focus is softer: ${whole.map((v) => v.toFixed(3))}`)
  assert.ok(centre[1] > centre[0] * 0.85, `in focus stays sharp: ${centre.map((v) => v.toFixed(3))}`)
  const lineDiff = await pixelDiff(page, sharp[0].images[1].png, blurred[0].images[1].png)
  assert.ok(lineDiff < 0.002, `line art is not blurred: ${lineDiff}`)
  console.log(`export 4 images ${ms} ms; sharpness out of focus ${whole.map((v) => v.toFixed(3))} centre ${centre.map((v) => v.toFixed(3))}`)
  assert.deepEqual(second.errors, [])
  await second.context.close()
})

await check('guides stay in the editor: an export with guides on matches one without', async () => {
  const one = exportScene(false)
  const a = await open(one.server, { query: '?export=1' })
  await a.page.getByRole('treeitem', { name: /机位 A/ }).click()
  await a.page.keyboard.press('Digit0')
  await a.page.getByRole('button', { name: '构图辅助线' }).click()
  for (const name of ['三分线', '黄金分割', '对角线', '中心十字']) await a.page.getByRole('menu').getByLabel(name).check()
  const withGuides = await runExport(a.page, one.server)
  await a.context.close()
  const two = exportScene(false)
  const b = await open(two.server, { query: '?export=1' })
  const without = await runExport(b.page, two.server)
  const diff = await pixelDiff(b.page, withGuides[0].images[0].png, without[0].images[0].png)
  assert.ok(diff < 0.001, `the guides are not in the export: ${diff}`)
  await b.context.close()
})

await check('景深预览 is a switch: the camera view blurs live, keeps blurring through scene changes, and off leaves the same AO picture', async () => {
  const { server, a } = exportScene(true)
  const { context, page, errors } = await open(server)
  await page.getByRole('treeitem', { name: /机位 A/ }).click()
  await page.keyboard.press('Digit0')
  const frame = page.locator('.cc-scene-camframe')
  await frame.waitFor()
  await page.mouse.move(5, 5)
  await sleep(400)
  const box = await frame.boundingBox()
  // The picture without the switch and guides button at its top right.
  const clip = { x: box.x, y: box.y + 40, width: Math.round(box.width * 0.75), height: Math.round(box.height - 40) }
  const picture = () => page.screenshot({ clip })
  // The far tower (out of focus) as a share of the clip.
  const tower = [(0.62 * box.width) / clip.width, (0.15 * box.height - 40) / clip.height, (0.7 * box.width) / clip.width, (0.5 * box.height - 40) / clip.height]
  const before = await picture()
  const sharp = await sharpness(page, before, tower)
  const toggle = page.getByRole('button', { name: '景深预览' })
  assert.equal(await toggle.getAttribute('aria-pressed'), 'false')
  await toggle.click()
  await page.mouse.move(5, 5)
  assert.equal(await toggle.getAttribute('aria-pressed'), 'true')
  let blurred = 0
  await waitFor(async () => (blurred = await sharpness(page, await picture(), tower)) < sharp * 0.6, 'the camera view blurs', 30_000)
  await shot(page, 'c4-dof-preview')
  // A change from elsewhere: the switch stays on and the blur follows the new aperture.
  server.external([{ op: 'update_objects', updates: [{ object_id: a, camera: { f_stop: 8 } }] }])
  await page.evaluate(() => window.__invalidate())
  await frame.filter({ hasText: 'f/8' }).waitFor()
  let stopped = 0
  await waitFor(async () => (stopped = await sharpness(page, await picture(), tower)) > blurred * 1.5, 'f/8 blurs less', 30_000)
  assert.equal(await toggle.getAttribute('aria-pressed'), 'true', 'a scene change keeps the switch on')
  await toggle.click()
  await page.mouse.move(5, 5)
  assert.equal(await toggle.getAttribute('aria-pressed'), 'false')
  let diff = 1
  await waitFor(async () => (diff = await pixelDiff(page, before, await picture())) < 0.002, 'off is the picture before the switch', 30_000)
  console.log(`camera view sharpness: off ${sharp.toFixed(3)}, f/1.4 ${blurred.toFixed(3)}, f/8 ${stopped.toFixed(3)}; off again differs ${diff}`)
  assert.deepEqual(errors, [])
  await context.close()
})

// --- AC-7 grouping -------------------------------------------------------------

/** Objects placed earlier, at revision 1 like the ground. */
function seed(server, objects) {
  for (const o of objects) {
    server.scene.objects.push({ parent_id: null, visible: true, locked: false, transform: identity(), ...o })
    server.unitRevs[placement(o.id)] = '1'
    server.unitRevs[content(o.id)] = '1'
  }
  server.history[0] = server.record(['*'], [], null)
}

const gid = (name) => `sobj_${name.padEnd(21, '0')}`
// A group turned by Euler(0, 0.8, 0) and scaled 2, a child tilted and stretched.
const turned = { position: [2, 0, -3], rotation: [0, 0.3894183423086505, 0, 0.9210609940028851], scale: [2, 2, 2] }
const tilted = { position: [1, 0.5, 2], rotation: [0.06407134770607115, 0.09115754934299071, 0.15343930202422257, 0.9818561728660807], scale: [1, 1.5, 1] }

function groupingScene(server) {
  seed(server, [
    { id: gid('p'), kind: 'group', name: '支架', order: 1, transform: turned },
    { id: gid('a'), kind: 'primitive', name: '箱 A', parent_id: gid('p'), order: 0, transform: tilted, primitive: { builtin: 'box@1', params: { width: 1, height: 1, depth: 1 } } },
    { id: gid('q'), kind: 'group', name: '台', order: 2, transform: { ...identity(), position: [-3, 0, 0] } },
    { id: gid('b'), kind: 'primitive', name: '箱 B', parent_id: gid('q'), order: 0, transform: { ...identity(), position: [0, 0, 2] }, primitive: { builtin: 'box@1', params: { width: 1, height: 1, depth: 1 } } },
    { id: gid('c'), kind: 'primitive', name: '箱 C', order: 3, transform: { ...identity(), position: [4, 0, 4] }, primitive: { builtin: 'box@1', params: { width: 1, height: 1, depth: 1 } } },
  ])
}

const worlds = (server) => {
  const world = worldOf(server.scene.objects)
  return new Map(server.scene.objects.map((o) => [o.id, world(o.id).elements]))
}
function sameWorlds(before, after, ids) {
  for (const id of ids) {
    const a = before.get(id)
    const b = after.get(id)
    assert.ok(a && b && a.every((v, i) => Math.abs(v - b[i]) < 1e-6), `${id} moved: ${a} → ${b}`)
  }
}
const treeOf = (server) => Object.fromEntries(server.scene.objects.map((o) => [o.id, `${o.parent_id ?? '-'}#${o.order}`]))
/** Parents are groups and siblings never share an order, as the real server checks. */
function validTree(server) {
  const kinds = new Map(server.scene.objects.map((o) => [o.id, o.kind]))
  const taken = new Set()
  for (const o of server.scene.objects) {
    if (o.parent_id) assert.equal(kinds.get(o.parent_id), 'group', `${o.name} under a ${kinds.get(o.parent_id)}`)
    const key = `${o.parent_id}#${o.order}`
    assert.ok(!taken.has(key), `order clash ${key}`)
    taken.add(key)
  }
}
const modKey = process.platform === 'darwin' ? 'Meta' : 'Control'
async function pick(page, names) {
  for (const [i, name] of names.entries()) await page.getByRole('treeitem', { name: new RegExp(name) }).click({ modifiers: i ? [modKey] : [] })
}

await check('AC-7: Ctrl+G groups objects of three parents in place, one revision; Cmd+Z puts them back; Ctrl+Alt+G ungroups in place', async () => {
  const server = new SceneServer()
  groupingScene(server)
  const { context, page, errors } = await open(server)
  const start = worlds(server)
  const startTree = treeOf(server)
  const startTransforms = new Map(server.scene.objects.map((o) => [o.id, o.transform]))
  const moved = [gid('a'), gid('b'), gid('c')]

  await pick(page, ['箱 A', '箱 B', '箱 C'])
  await page.keyboard.press(`${modKey}+KeyG`)
  await waitFor(() => server.commands.length === 1, 'group')
  await saved(page)
  const op = server.commands[0].payload.operations
  assert.equal(op.length, 1)
  assert.equal(op[0].op, 'reparent_objects')
  assert.equal(server.revision, 2, 'one revision')
  const g = op[0].create_groups[0].id
  assert.deepEqual(moved.map((id) => server.object(id).parent_id), [g, g, g])
  assert.equal(server.object(g).parent_id, null, 'parents differ: the group goes to the root')
  validTree(server)
  sameWorlds(start, worlds(server), moved)
  await page.getByRole('treeitem', { name: /分组 1/ }).waitFor()

  await page.keyboard.press(`${modKey}+KeyZ`)
  await waitFor(() => server.commands.length === 2, 'undo')
  await saved(page)
  assert.equal(server.commands[1].payload.operations[0].op, 'reparent_objects')
  assert.deepEqual(treeOf(server), startTree, 'back under their old parents, in their old order')
  for (const id of moved) {
    const [t, u] = [server.object(id).transform, startTransforms.get(id)]
    assert.ok([...t.position, ...t.scale].every((v, i) => Math.abs(v - [...u.position, ...u.scale][i]) < 1e-6), `${id} transform restored`)
  }

  // Redo, then ungroup with Ctrl+Alt+G.
  await page.keyboard.press(`${modKey}+Shift+KeyZ`)
  await waitFor(() => server.commands.length === 3, 'redo')
  await saved(page)
  await page.getByRole('treeitem', { name: /分组 1/ }).click()
  await page.keyboard.press(`${modKey}+Alt+KeyG`)
  await waitFor(() => server.commands.length === 4, 'ungroup')
  await saved(page)
  assert.equal(server.object(g), undefined, 'the group is gone')
  assert.deepEqual(moved.map((id) => server.object(id).parent_id), [null, null, null])
  validTree(server)
  sameWorlds(start, worlds(server), moved)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('AC-7: Ctrl+P onto a primitive says why and changes nothing; dragging into a turned, scaled group keeps the object in place', async () => {
  const server = new SceneServer()
  groupingScene(server)
  const { context, page, errors } = await open(server)
  await pick(page, ['台', '箱 C'])
  await page.keyboard.press(`${modKey}+KeyP`)
  await page.locator('.cc-scene-hint').filter({ hasText: '只能挂到分组或空物体下' }).waitFor()
  await sleep(300)
  assert.equal(server.commands.length, 0, 'the scene is unchanged')

  const start = worlds(server)
  const row = page.getByRole('treeitem', { name: /箱 C/ })
  // Dragging a selected row takes the whole selection; this one goes alone.
  await row.click()
  const target = page.getByRole('treeitem', { name: /支架/ })
  const box = await target.boundingBox()
  await row.dragTo(target, { targetPosition: { x: 60, y: box.height / 2 } })
  await waitFor(() => server.commands.length === 1, 'drop')
  await saved(page)
  assert.equal(server.object(gid('c')).parent_id, gid('p'))
  validTree(server)
  sameWorlds(start, worlds(server), [gid('c')])
  // The list shows it inside 支架, indented under it.
  let names = []
  await waitFor(async () => {
    names = await page.getByRole('treeitem').allTextContents()
    return names.findIndex((n) => n.includes('箱 C')) === names.findIndex((n) => n.includes('箱 A')) + 1
  }, 'the list shows the new hierarchy').catch(() => assert.fail(`list order ${names}`))
  const pad = async (name) => parseFloat(await page.getByRole('treeitem', { name: new RegExp(name) }).evaluate((e) => e.style.paddingLeft))
  assert.ok((await pad('箱 C')) > (await pad('支架')))
  assert.deepEqual(errors, [])
  await context.close()
})

await check('AC-7: grouping while the assistant moves one of the objects asks first, and 重新应用 keeps it where the assistant put it', async () => {
  const server = new SceneServer()
  groupingScene(server)
  const { context, page, errors } = await open(server)
  await pick(page, ['箱 B', '箱 C'])
  // The assistant moves C; the editor has not heard yet.
  server.external([{ op: 'update_objects', updates: [{ object_id: gid('c'), transform: { ...identity(), position: [6, 1, -2] } }] }], { actor: 'agent' })
  const moved = worlds(server)
  await page.keyboard.press(`${modKey}+KeyG`)
  await page.getByRole('alert').filter({ hasText: '重新应用' }).waitFor()
  assert.equal(server.scene.objects.some((o) => o.kind === 'group' && o.name.startsWith('分组')), false, 'the grouping did not land')
  assert.deepEqual(server.object(gid('c')).transform.position, [6, 1, -2], 'nothing jumped or went missing')
  await page.getByRole('button', { name: '重新应用我的修改' }).click()
  await waitFor(() => server.scene.objects.some((o) => o.name === '分组 1'), 'grouped on the latest')
  await saved(page)
  validTree(server)
  sameWorlds(moved, worlds(server), [gid('b'), gid('c')])
  assert.deepEqual(errors, [])
  await context.close()
})

// --- AC-1 display modes --------------------------------------------------------

/** The share of pixels within a small distance of one colour. */
async function colorShare(page, png, [r, g, b]) {
  return page.evaluate(async ([src, r, g, b]) => {
    const img = await new Promise((resolve) => { const i = new Image(); i.onload = () => resolve(i); i.src = src })
    const c = document.createElement('canvas')
    c.width = img.width
    c.height = img.height
    const x = c.getContext('2d')
    x.drawImage(img, 0, 0)
    const p = x.getImageData(0, 0, c.width, c.height).data
    let n = 0
    for (let i = 0; i < p.length; i += 4) if (Math.abs(p[i] - r) + Math.abs(p[i + 1] - g) + Math.abs(p[i + 2] - b) < 12) n++
    return n / (p.length / 4)
  }, [`data:image/png;base64,${png.toString('base64')}`, r, g, b])
}
const wireGrey = [0x3d, 0x3f, 0x43]
const pressed = (page, name) => page.getByRole('group', { name: '显示模式' }).getByRole('button', { name, exact: true }).getAttribute('aria-pressed')

await check('AC-1: Z switches the shading, Shift+Z goes to wireframe and back; it is kept here, never an edit', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  await addBox(page, server)
  const { box } = await viewportCenter(page)
  assert.equal(await pressed(page, '白模'), 'true', 'a clay scene opens solid')
  await page.mouse.move(box.x + 300, box.y + 200)
  await page.keyboard.press('KeyZ')
  await page.getByRole('menuitem', { name: '线框', exact: true }).click()
  await waitFor(async () => (await colorShare(page, await page.screenshot({ clip: box }), wireGrey)) > 0.5, 'a wireframe view')
  assert.equal(await pressed(page, '线框'), 'true')
  await page.keyboard.press('Shift+KeyZ')
  await waitFor(async () => (await pressed(page, '白模')) === 'true', 'Shift+Z goes back')
  assert.ok((await colorShare(page, await page.screenshot({ clip: box }), wireGrey)) < 0.05, 'solid again')
  await page.getByRole('group', { name: '显示模式' }).getByRole('button', { name: '最终渲染' }).click()
  await page.keyboard.press('Shift+KeyZ')
  await page.keyboard.press('Shift+KeyZ')
  assert.equal(await pressed(page, '最终渲染'), 'true', 'Shift+Z comes back to the mode before wireframe')

  // Cmd+Z undoes the add, not the shading.
  await page.keyboard.press(`${modKey}+KeyZ`)
  await waitFor(() => server.commands.length === 2, 'undo the add')
  await saved(page)
  assert.equal(await pressed(page, '最终渲染'), 'true', 'undo leaves the shading alone')
  assert.ok(server.commands.every((c) => c.payload.operations.every((op) => op.op !== 'set_render')), 'never written to the scene')
  assert.equal(server.scene.render.mode, 'clay')

  await page.reload()
  await page.locator('.cc-scene-editor canvas').waitFor({ timeout: 20_000 })
  assert.equal(await pressed(page, '最终渲染'), 'true', 'kept in this browser')
  assert.deepEqual(errors, [])
  await context.close()

  // Another browser has no choice yet: the scene's suggestion decides.
  const lit = new SceneServer()
  lit.scene.render.mode = 'lit'
  const other = await open(lit)
  assert.equal(await pressed(other.page, '最终渲染'), 'true', 'a lit scene opens rendered')
  await other.context.close()
})

/** A screenshot once two in a row agree: the view has stopped changing. */
async function settledShot(page, clip) {
  let png = await page.screenshot({ clip })
  for (let i = 0; i < 30; i++) {
    await sleep(250)
    const next = await page.screenshot({ clip })
    const still = (await pixelDiff(page, png, next)) < 0.0005
    png = next
    if (still) return png
  }
  return png
}

await check('AC-1: lights light the rendered view; material preview ignores lights and the sun', async () => {
  const server = new SceneServer()
  groupingScene(server)
  const lamp = gid('lamp')
  seed(server, [{ id: lamp, kind: 'light', name: '灯', order: 9, transform: { ...identity(), position: [-1, 2.5, 1] }, light: { type: 'point', color: '#ffb070', intensity: 60, cast_shadow: true, distance: 0, decay: 2 } }])
  const { context, page, errors } = await open(server)
  const { box } = await viewportCenter(page)
  const shading = (name) => page.getByRole('group', { name: '显示模式' }).getByRole('button', { name }).click()
  const change = async (ops) => {
    server.external(ops)
    await page.evaluate(() => window.__invalidate())
    await sleep(600)
  }
  const dim = [{ op: 'update_objects', updates: [{ object_id: lamp, light: { intensity: 0 } }] }]
  const bright = [{ op: 'update_objects', updates: [{ object_id: lamp, light: { intensity: 60 } }] }]
  const sunset = (deg) => [{ op: 'set_environment', environment: { ...server.scene.environment, sky: { ...server.scene.environment.sky, sun_azimuth_deg: deg } } }]

  await shading('最终渲染')
  const lit = await settledShot(page, box)
  await change(dim)
  const dark = await settledShot(page, box)
  const byLight = await pixelDiff(page, lit, dark)
  assert.ok(byLight > 0.01, `the lamp lights the rendered view (${(byLight * 100).toFixed(2)}% differ)`)
  await change(bright)

  await shading('材质预览')
  const before = await settledShot(page, box)
  await change(dim)
  await change(sunset(300))
  const after = await settledShot(page, box)
  const preview = await pixelDiff(page, before, after)
  assert.ok(preview < 0.002, `material preview does not change (${(preview * 100).toFixed(3)}% differ)`)

  await shading('白模')
  const clay = await settledShot(page, box)
  await change(bright)
  await change([{ op: 'update_objects', updates: [{ object_id: gid('c'), material_slots: { main: { preset: 'stone_brick@1', params: {} } } }] }])
  const clayAfter = await settledShot(page, box)
  const byClay = await pixelDiff(page, clay, clayAfter)
  assert.ok(byClay < 0.002, `clay ignores lights and materials (${(byClay * 100).toFixed(3)}% differ)`)
  assert.deepEqual(errors, [])
  await context.close()
})

// --- AC-2 materials ------------------------------------------------------------

const boxOf = (name, order, transform, params, slot) => ({
  id: gid(name), kind: 'primitive', name, order, transform: { ...identity(), ...transform },
  primitive: { builtin: 'box@1', params }, material_slots: slot ? { main: slot } : {},
})
const shading = (page, name) => page.getByRole('group', { name: '显示模式' }).getByRole('button', { name }).click()

/** Blobs of pixels brighter than a level in all channels, 4-connected, at least min pixels. */
async function brightBlobs(page, png, level, min) {
  return page.evaluate(async ([src, level, min]) => {
    const img = await new Promise((resolve) => { const i = new Image(); i.onload = () => resolve(i); i.src = src })
    const c = document.createElement('canvas')
    c.width = img.width
    c.height = img.height
    const x = c.getContext('2d')
    x.drawImage(img, 0, 0)
    const p = x.getImageData(0, 0, c.width, c.height).data
    const w = c.width
    const h = c.height
    const seen = new Uint8Array(w * h)
    const bright = (i) => p[i * 4] >= level && p[i * 4 + 1] >= level && p[i * 4 + 2] >= level
    let blobs = 0
    for (let i = 0; i < w * h; i++) {
      if (seen[i] || !bright(i)) continue
      let size = 0
      const stack = [i]
      seen[i] = 1
      while (stack.length) {
        const j = stack.pop()
        size++
        const jx = j % w
        for (const k of [j - 1, j + 1, j - w, j + w]) {
          if (k < 0 || k >= w * h || seen[k] || (Math.abs((k % w) - jx) > 1) || !bright(k)) continue
          seen[k] = 1
          stack.push(k)
        }
      }
      if (size >= min) blobs++
    }
    return blobs
  }, [`data:image/png;base64,${png.toString('base64')}`, level, min])
}

await check('AC-2: every preset compiles and draws in both material looks', async () => {
  const server = new SceneServer()
  const ids = JSON.parse(readFileSync(new URL('../src/creative-scene/materials.gen.json', import.meta.url), 'utf8')).materials.map((m) => m.id)
  seed(server, ids.map((id, i) => boxOf(`m${String.fromCharCode(97 + i)}`, 10 + i, { position: [(i % 5) * 1.6 - 3.2, 0, Math.floor(i / 5) * 1.6 - 1.6] }, { width: 1.2, height: 1.2, depth: 1.2 }, { preset: id, params: {} })))
  // Looking down 35.5° from 5 m up and 7 m back: all fifteen in the frame.
  seed(server, [{ id: gid('cam'), kind: 'camera', name: '样张', order: 40, transform: { position: [0, 5, 7], rotation: [-0.30508, 0, 0, 0.95233], scale: [1, 1, 1] }, camera: {} }])
  const { context, page, errors } = await open(server)
  await page.getByRole('treeitem', { name: /样张/ }).click()
  await page.keyboard.press('Digit0')
  await page.locator('.cc-scene-camframe').waitFor()
  await page.mouse.move(5, 5)
  const box = await page.locator('.cc-scene-camframe').boundingBox()
  const solid = await settledShot(page, box)
  for (const name of ['材质预览', '最终渲染']) {
    await shading(page, name)
    const png = await settledShot(page, box)
    assert.ok((await pixelDiff(page, solid, png)) > 0.05, `${name} draws the materials`)
    if (process.env.SCENE_E2E_SHOTS) (await import('node:fs')).writeFileSync(`${process.env.SCENE_E2E_SHOTS}/c5-materials-${name}.png`, png)
  }
  assert.deepEqual(errors, [], 'no shader failed to compile')
  await context.close()
})

await check('AC-2: a brick wall scaled 2× along X looks like one built twice as wide: no stretching', async () => {
  const look = async (params, scale) => {
    const server = new SceneServer()
    seed(server, [boxOf('wall', 5, { position: [0, 0, 0], scale }, params, { preset: 'stone_brick@1', params: {} })])
    const { context, page, errors } = await open(server)
    await page.keyboard.press('Digit1')
    await shading(page, '材质预览')
    const { box } = await viewportCenter(page)
    const png = await settledShot(page, box)
    assert.deepEqual(errors, [])
    return { png, page, context }
  }
  const wide = await look({ width: 4, height: 1.5, depth: 0.3 }, [1, 1, 1])
  const scaled = await look({ width: 2, height: 1.5, depth: 0.3 }, [2, 1, 1])
  const diff = await pixelDiff(wide.page, wide.png, scaled.png)
  assert.ok(diff < 0.003, `the same bricks (${(diff * 100).toFixed(3)}% of pixels differ)`)
  await wide.context.close()
  await scaled.context.close()
})

await check('AC-2: polished metal mirrors the sky with one sun highlight, not two', async () => {
  const server = new SceneServer()
  server.scene.render.exposure_ev = -2
  seed(server, [{
    id: gid('ball'), kind: 'primitive', name: '球', order: 5, transform: { ...identity(), position: [0, 0, 0] },
    primitive: { builtin: 'sphere@1', params: { radius: 2 } }, material_slots: { main: { preset: 'principled@1', params: { metalness: 1, roughness: 0.2 } } },
  }])
  const { context, page, errors } = await open(server)
  await shading(page, '最终渲染')
  await page.getByRole('treeitem', { name: /球/ }).click()
  await page.keyboard.press('Period')
  await page.keyboard.press('Escape')
  const { box } = await viewportCenter(page)
  const png = await settledShot(page, box)
  await shot(page, 'c5-metal')
  const blobs = await brightBlobs(page, png, 250, 4)
  assert.equal(blobs, 1, `sun highlights on the ball: ${blobs}`)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('AC-2: a far brick wall barely changes when the camera moves half a pixel', async () => {
  const server = new SceneServer()
  const cam = gid('cam')
  seed(server, [
    boxOf('wall', 5, { position: [0, 0, -60] }, { width: 60, height: 20, depth: 0.5 }, { preset: 'stone_brick@1', params: {} }),
    { id: cam, kind: 'camera', name: '机位', order: 6, transform: { ...identity(), position: [0, 6, 0] }, camera: {} },
  ])
  const { context, page, errors } = await open(server)
  await shading(page, '最终渲染')
  await page.getByRole('treeitem', { name: /机位/ }).click()
  await page.keyboard.press('Digit0')
  const frame = page.locator('.cc-scene-camframe')
  await frame.waitFor()
  await page.mouse.move(5, 5)
  const fb = await frame.boundingBox()
  const clip = { x: Math.round(fb.x + 10), y: Math.round(fb.y + 40), width: Math.round(fb.width * 0.7), height: Math.round(fb.height * 0.5) }
  const before = await settledShot(page, clip)
  // 35 mm on a 36 mm sensor: the frame is 2 × 60 × 18/35 m wide at the wall.
  const halfPixel = (0.5 * 2 * 60 * (18 / 35)) / fb.width
  server.external([{ op: 'update_objects', updates: [{ object_id: cam, transform: { ...identity(), position: [halfPixel, 6, 0] } }] }])
  await page.evaluate(() => window.__invalidate())
  await sleep(800)
  const after = await settledShot(page, clip)
  const diff = await pixelDiff(page, before, after)
  console.log(`far brick wall, half a pixel over: ${(diff * 100).toFixed(2)}% of pixels differ visibly`)
  assert.ok(diff < 0.02, `the far wall shimmers (${(diff * 100).toFixed(2)}% differ)`)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('AC-2: a see-through box shows what is behind it', async () => {
  const look = async (opacity) => {
    const server = new SceneServer()
    seed(server, [
      boxOf('back', 5, { position: [0, 0, -2] }, { width: 3, height: 3, depth: 0.2 }, { preset: 'principled@1', params: { color: '#c02020' } }),
      boxOf('glass', 6, { position: [0, 0, 0] }, { width: 1.5, height: 1.5, depth: 1.5 }, { preset: 'principled@1', params: { color: '#ffffff', opacity } }),
    ])
    const { context, page, errors } = await open(server)
    await page.keyboard.press('Digit1')
    await shading(page, '材质预览')
    const { x, y } = await viewportCenter(page)
    await shot(page, `c5-see-through-${opacity}`)
    // Inside the front box, seen straight on.
    const png = await settledShot(page, { x: x - 20, y: y - 55, width: 40, height: 30 })
    assert.deepEqual(errors, [])
    await context.close()
    return png
  }
  const opaque = await look(1)
  const clear = await look(0.25)
  const page = (await open(new SceneServer())).page
  const diff = await pixelDiff(page, opaque, clear)
  assert.ok(diff > 0.5, `the red wall shows through (${(diff * 100).toFixed(1)}% differ)`)
  await page.context().close()
})

// --- AC-2 / AC-3 material editing ----------------------------------------------

/** Two pillars of the template model (slots stone and trim) and a box. */
function materialScene(server) {
  const rev = server.seedModel()
  server.seedInstance(gid('pa'), rev, -2, '柱 A')
  server.seedInstance(gid('pb'), rev, 2, '柱 B')
  server.object(gid('pa')).material_slots = { stone: { preset: 'marble@1', params: { scale: 2 } }, trim: { preset: 'paint@1', params: { color: '#203040' } } }
  seed(server, [{ ...boxOf('box', 9, { position: [0, 0, 3] }, { width: 1, height: 1, depth: 1 }, { preset: 'wood@1', params: {} }), name: '箱' }])
}
const objectMenu = async (page, item) => {
  await page.locator('.cc-scene-menubar').getByRole('button', { name: '物件' }).click()
  await page.getByRole('menuitem', { name: item }).click()
}

await check('AC-3: 复制材质到选中物件 copies by slot name in one revision, one Cmd+Z; a one-slot box fills every slot', async () => {
  const server = new SceneServer()
  materialScene(server)
  const { context, page, errors } = await open(server)
  await pick(page, ['柱 B', '箱', '柱 A'])
  await objectMenu(page, '复制材质到选中物件')
  await waitFor(() => server.commands.length === 1, 'copy')
  await saved(page)
  const ops = server.commands[0].payload.operations
  assert.equal(ops.length, 1)
  assert.deepEqual(ops[0].updates.map((u) => u.object_id), [gid('pb')], 'the box has no stone or trim slot: left alone')
  assert.deepEqual(server.object(gid('pb')).material_slots, server.object(gid('pa')).material_slots)
  await page.keyboard.press(`${modKey}+KeyZ`)
  await waitFor(() => server.commands.length === 2, 'undo')
  await saved(page)
  assert.deepEqual(server.object(gid('pb')).material_slots, {}, 'one Cmd+Z takes it back')

  await pick(page, ['柱 A', '柱 B', '箱'])
  await objectMenu(page, '复制材质到选中物件')
  await waitFor(() => server.commands.length === 3, 'copy from the box')
  await saved(page)
  for (const id of [gid('pa'), gid('pb')]) assert.deepEqual(server.object(id).material_slots, { stone: { preset: 'wood@1', params: {} }, trim: { preset: 'wood@1', params: {} } })
  assert.deepEqual(errors, [])
  await context.close()
})

await check('AC-2: a material chosen with two boxes selected goes to both in one update', async () => {
  const server = new SceneServer()
  seed(server, [boxOf('b1', 5, { position: [-1, 0, 0] }, { width: 1, height: 1, depth: 1 }), boxOf('b2', 6, { position: [1, 0, 0] }, { width: 1, height: 1, depth: 1 })])
  const { context, page, errors } = await open(server)
  await pick(page, ['b1', 'b2'])
  await page.getByLabel('main 的材质').selectOption('stone_brick@1')
  await waitFor(() => server.commands.length === 1, 'set')
  await saved(page)
  const op = server.commands[0].payload.operations[0]
  assert.deepEqual(op.updates.map((u) => u.object_id), [gid('b1'), gid('b2')])
  for (const id of [gid('b1'), gid('b2')]) assert.equal(server.object(id).material_slots.main.preset, 'stone_brick@1')
  // One field edit goes to both too.
  await page.getByLabel('砖块尺寸（m）').fill('0.5')
  await page.getByLabel('砖块尺寸（m）').press('Enter')
  await waitFor(() => server.commands.length === 2, 'param')
  assert.ok([gid('b1'), gid('b2')].every((id) => server.object(id).material_slots.main.params.brick_size === 0.5))
  assert.deepEqual(errors, [])
  await context.close()
})

await check('AC-3: 复制材质 in one scene, 粘贴材质 on an object of another', async () => {
  const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
  const one = new SceneServer()
  materialScene(one)
  const a = await open(one, { context })
  await a.page.getByRole('treeitem', { name: /柱 A/ }).click()
  await a.page.getByLabel('槽位 stone').getByRole('button', { name: '复制材质' }).click()
  await a.page.close()
  const two = new SceneServer()
  seed(two, [boxOf('other', 5, { position: [0, 0, 0] }, { width: 1, height: 1, depth: 1 })])
  const b = await open(two, { context })
  await b.page.getByRole('treeitem', { name: /other/ }).click()
  await b.page.getByLabel('槽位 main').getByRole('button', { name: '粘贴材质' }).click()
  await waitFor(() => two.commands.length === 1, 'paste')
  await saved(b.page)
  assert.deepEqual(two.object(gid('other')).material_slots.main, { preset: 'marble@1', params: { scale: 2 } })
  assert.deepEqual([...a.errors, ...b.errors], [])
  await context.close()
})

// --- AC-4 / AC-5 lights and environment ---------------------------------------

/**
 * Mean luma and colour of a screenshot, the share of pixels brighter than
 * a level, and the share darker than ratio × the median: shadow, relative
 * to how lit the picture is overall.
 */
async function lumaStats(page, png, { level = 60, ratio = 0.6 } = {}) {
  return page.evaluate(async ([src, level, ratio]) => {
    const img = await new Promise((resolve) => { const i = new Image(); i.onload = () => resolve(i); i.src = src })
    const c = document.createElement('canvas')
    c.width = img.width
    c.height = img.height
    const x = c.getContext('2d')
    x.drawImage(img, 0, 0)
    const p = x.getImageData(0, 0, c.width, c.height).data
    const n = p.length / 4
    const luma = new Float32Array(n)
    let r = 0, g = 0, b = 0
    for (let i = 0; i < n; i++) {
      r += p[i * 4]
      g += p[i * 4 + 1]
      b += p[i * 4 + 2]
      luma[i] = 0.2126 * p[i * 4] + 0.7152 * p[i * 4 + 1] + 0.0722 * p[i * 4 + 2]
    }
    const sorted = Float32Array.from(luma).sort()
    const median = sorted[n >> 1]
    let bright = 0, dark = 0, sum = 0
    for (const v of luma) {
      sum += v
      if (v > level) bright++
      if (v < ratio * median) dark++
    }
    return { mean: sum / n, bright: bright / n, dark: dark / n, r: r / n, g: g / n, b: b / n }
  }, [`data:image/png;base64,${png.toString('base64')}`, level, ratio])
}
/** Like pixelDiff, counting only solid areas: a pixel whose neighbours 2 px away differ too, so thin lines do not count. */
async function solidDiff(page, a, b, min) {
  return page.evaluate(async ([a, b, min]) => {
    const load = (src) => new Promise((resolve) => { const i = new Image(); i.onload = () => resolve(i); i.src = src })
    const [x, y] = await Promise.all([load(a), load(b)])
    const c = document.createElement('canvas')
    c.width = x.width
    c.height = x.height
    const g = c.getContext('2d')
    g.drawImage(x, 0, 0)
    const p = g.getImageData(0, 0, c.width, c.height).data
    g.clearRect(0, 0, c.width, c.height)
    g.drawImage(y, 0, 0)
    const q = g.getImageData(0, 0, c.width, c.height).data
    const w = c.width
    const h = c.height
    const differs = new Uint8Array(w * h)
    for (let i = 0; i < w * h; i++) differs[i] = Math.abs(p[i * 4] - q[i * 4]) + Math.abs(p[i * 4 + 1] - q[i * 4 + 1]) + Math.abs(p[i * 4 + 2] - q[i * 4 + 2]) > min ? 1 : 0
    let n = 0
    for (let y = 2; y < h - 2; y++) for (let x = 2; x < w - 2; x++) {
      const i = y * w + x
      if (differs[i] && differs[i - 2] && differs[i + 2] && differs[i - 2 * w] && differs[i + 2 * w]) n++
    }
    return n / (w * h)
  }, [`data:image/png;base64,${a.toString('base64')}`, `data:image/png;base64,${b.toString('base64')}`, min])
}
const night = { sky: { kind: 'color', color: '#0b0c0e' }, ambient: { color: '#ffffff', intensity: 0 }, fog: null }
const lightAt = (server, id) => server.object(id).light
const focusEditor = (page) => page.locator('.cc-scene-editor').focus()

await check('AC-4: Shift+A adds a spot above the pointer; a 30° cone stores π/12 and narrows the light; shadows show; spot → point keeps the rest; Cmd+Z', async () => {
  const server = new SceneServer()
  server.scene.environment = structuredClone(night)
  seed(server, [boxOf('post', 5, { position: [0, 0, 0] }, { width: 0.4, height: 1, depth: 0.4 })])
  const { context, page, errors } = await open(server)
  const { x, y, box } = await viewportCenter(page)
  await shading(page, '最终渲染')
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '聚光', exact: true }).click()
  await waitFor(() => server.commands.length === 1, 'add a spot')
  const spot = server.commands[0].payload.operations[0].objects[0]
  assert.deepEqual([spot.kind, spot.name, spot.light.type], ['light', '聚光 1', 'spot'])
  assert.ok(Math.abs(spot.transform.position[1] - 2) < 1e-9, `above the pointed ground: ${spot.transform.position}`)
  await saved(page)
  // From the left, 30° down at the post, so its shadow falls in the light.
  server.external([{ op: 'update_objects', updates: [{ object_id: spot.id, transform: { ...spot.transform, position: [-1.5, 1.2, 0], rotation: [-0.1830127018922193, -0.6830127018922193, -0.18301270189221933, 0.6830127018922194] } }] }])
  await page.evaluate(() => window.__invalidate())
  await sleep(600)
  // Brighter than the grid lines: only what the spot lights.
  const lit = { level: 200 }
  const wide = await lumaStats(page, await settledShot(page, box), lit)
  assert.ok(wide.bright > 0.01, `the spot lights the ground (${(wide.bright * 100).toFixed(2)}% lit)`)

  const cone = page.getByLabel('光锥角（°）')
  await cone.fill('30')
  await cone.press('Enter')
  await waitFor(() => Math.abs(lightAt(server, spot.id).angle - Math.PI / 12) < 1e-9, 'a 30° cone is a π/12 half angle')
  const narrow = await lumaStats(page, await settledShot(page, box), lit)
  await shot(page, 'c5-spot-30')
  assert.ok(narrow.bright < wide.bright * 0.6, `a narrower cone lights less (${(wide.bright * 100).toFixed(2)}% → ${(narrow.bright * 100).toFixed(2)}%)`)

  await page.getByLabel('投射阴影').check()
  await waitFor(() => lightAt(server, spot.id).cast_shadow === true, 'shadows on')
  const shadow = await lumaStats(page, await settledShot(page, box), lit)
  await shot(page, 'c5-spot-shadow')
  assert.ok(shadow.bright < narrow.bright * 0.8, `the post casts a shadow in the light (${(narrow.bright * 100).toFixed(2)}% → ${(shadow.bright * 100).toFixed(2)}% lit)`)

  await page.getByLabel('灯光类型').selectOption('point')
  await waitFor(() => lightAt(server, spot.id).type === 'point', 'to a point light')
  assert.deepEqual(lightAt(server, spot.id), { type: 'point', color: '#ffffff', intensity: spot.light.intensity, cast_shadow: true, distance: 0, decay: 2 })
  assert.equal(await page.getByLabel('光锥角（°）').count(), 0, 'the cone fields go')
  await saved(page)
  await focusEditor(page)
  await page.keyboard.press(`${modKey}+KeyZ`)
  await waitFor(() => lightAt(server, spot.id).type === 'spot', 'undo')
  assert.ok(Math.abs(lightAt(server, spot.id).angle - Math.PI / 12) < 1e-9, 'the spot comes back with its cone')
  await saved(page)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('AC-5: with nothing selected the environment shows; the sun at 5° lengthens shadows and turns the sky to dusk; Cmd+Z', async () => {
  const server = new SceneServer()
  const post = gid('post')
  seed(server, [boxOf('post', 5, { position: [0, 0, 0] }, { width: 0.4, height: 1.5, depth: 0.4 })])
  const { context, page, errors } = await open(server)
  const { box } = await viewportCenter(page)
  await page.getByLabel('环境', { exact: true }).waitFor()
  const show = async (visible) => {
    server.external([{ op: 'update_objects', updates: [{ object_id: post, visible }] }])
    await page.evaluate(() => window.__invalidate())
    await sleep(600)
  }
  /** The post and its shadow: what hiding the post changes. */
  const footprint = async (name) => {
    const standing = await settledShot(page, box)
    await shot(page, `c5-sun-${name}`)
    await show(false)
    const gone = await settledShot(page, box)
    await show(true)
    return solidDiff(page, standing, gone, 12)
  }
  const skyBand = { x: box.x, y: box.y + 4, width: box.width, height: 30 }
  const warmth = async () => {
    const band = await lumaStats(page, await settledShot(page, skyBand))
    return band.r / Math.max(1, band.b)
  }
  const noon = await footprint('35')
  await shading(page, '最终渲染')
  const noonLit = await footprint('35-lit')
  const noonSky = await warmth()
  await shading(page, '白模')
  const elevation = page.getByRole('spinbutton', { name: '太阳高度（°）' })
  await elevation.fill('5')
  await elevation.press('Enter')
  await waitFor(() => server.scene.environment.sky.sun_elevation_deg === 5, 'the sun goes down')
  assert.deepEqual(server.scene.environment, { sky: { kind: 'procedural', sun_elevation_deg: 5, sun_azimuth_deg: 135, turbidity: 3 }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null }, 'only the elevation changes')
  assert.deepEqual(server.commands.at(-1).payload.operations.map((o) => o.op), ['set_environment'])
  const dusk = await footprint('5')
  assert.ok(dusk > noon * 1.5, `a longer clay shadow (${(noon * 100).toFixed(2)}% → ${(dusk * 100).toFixed(2)}% of the view)`)
  await shading(page, '最终渲染')
  const duskLit = await footprint('5-lit')
  assert.ok(duskLit > noonLit * 1.5, `a longer rendered shadow (${(noonLit * 100).toFixed(2)}% → ${(duskLit * 100).toFixed(2)}% of the view)`)
  const duskSky = await warmth()
  assert.ok(duskSky > noonSky * 1.3, `a dusk sky (red/blue ${noonSky.toFixed(2)} → ${duskSky.toFixed(2)})`)
  await shading(page, '白模')
  await saved(page)
  await focusEditor(page)
  await page.keyboard.press(`${modKey}+KeyZ`)
  await waitFor(() => server.scene.environment.sky.sun_elevation_deg === 35, 'undo')
  await saved(page)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('AC-5: a colour sky and fog show as set', async () => {
  const server = new SceneServer()
  seed(server, [boxOf('post', 5, { position: [0, 0, 0] }, { width: 0.4, height: 1.5, depth: 0.4 })])
  const { context, page, errors } = await open(server)
  const { box } = await viewportCenter(page)
  await page.getByLabel('天空类型').selectOption('color')
  await waitFor(() => server.scene.environment.sky.kind === 'color', 'a colour sky')
  assert.deepEqual(server.scene.environment.sky, { kind: 'color', color: '#a7b6c6' }, 'only the colour sky\'s fields')
  await page.getByLabel('天空颜色').fill('#2050c0')
  await waitFor(() => server.scene.environment.sky.color === '#2050c0', 'sky colour')
  await shading(page, '最终渲染')
  const clear = await settledShot(page, box)
  await page.getByLabel('雾', { exact: true }).check()
  await waitFor(() => !!server.scene.environment.fog, 'fog on')
  const near = page.getByRole('spinbutton', { name: '起始（m）' })
  await near.fill('1')
  await near.press('Enter')
  const far = page.getByRole('spinbutton', { name: '结束（m）' })
  await far.fill('12')
  await far.press('Enter')
  await waitFor(() => server.scene.environment.fog?.far === 12 && server.scene.environment.fog.near === 1, 'fog range')
  const foggy = await settledShot(page, box)
  await shot(page, 'c5-fog')
  const diff = await pixelDiff(page, clear, foggy)
  assert.ok(diff > 0.2, `the ground fades into fog (${(diff * 100).toFixed(1)}% differ)`)
  // Level with the horizon, the sky fills the top of the view; fog leaves the background alone.
  await page.keyboard.press('Digit1')
  const sky = { x: box.x, y: box.y + 10, width: box.width, height: 40 }
  for (const mode of ['白模', '最终渲染']) {
    await shading(page, mode)
    const top = await lumaStats(page, await settledShot(page, sky))
    assert.ok(top.b > top.r + 60, `${mode}: the sky is the colour set (rgb ${top.r.toFixed(0)}, ${top.g.toFixed(0)}, ${top.b.toFixed(0)})`)
  }
  assert.deepEqual(errors, [])
  await context.close()
})

await check('AC-5: exposure +2 brightens the rendered view of a room lit by two wall lights, not the clay', async () => {
  const server = new SceneServer()
  server.scene.environment = structuredClone(night)
  server.scene.render.exposure_ev = 0
  seed(server, [
    boxOf('wall', 5, { position: [0, 0, -1.5] }, { width: 6, height: 3, depth: 0.2 }),
    { id: gid('lampa'), kind: 'light', name: '壁灯 A', order: 6, transform: { ...identity(), position: [-1.5, 2, -1.2] }, light: { type: 'point', color: '#ffd2a0', intensity: 40, cast_shadow: false, distance: 0, decay: 2 } },
    { id: gid('lampb'), kind: 'light', name: '壁灯 B', order: 7, transform: { ...identity(), position: [1.5, 2, -1.2] }, light: { type: 'point', color: '#ffd2a0', intensity: 40, cast_shadow: false, distance: 0, decay: 2 } },
  ])
  const { context, page, errors } = await open(server)
  const { box } = await viewportCenter(page)
  const clayBefore = await settledShot(page, box)
  await shot(page, 'c5-room-clay')
  await shading(page, '最终渲染')
  const before = await lumaStats(page, await settledShot(page, box))
  const ev = page.getByRole('spinbutton', { name: '曝光补偿（EV）' })
  await ev.fill('2')
  await ev.press('Enter')
  await waitFor(() => server.scene.render.exposure_ev === 2, 'exposure')
  assert.deepEqual(server.commands.at(-1).payload.operations, [{ op: 'set_render', render: { exposure_ev: 2 } }], 'only the exposure is written')
  assert.equal(server.scene.render.mode, 'clay', 'the mode stays')
  const after = await lumaStats(page, await settledShot(page, box))
  assert.ok(after.mean > before.mean * 1.3, `brighter (${before.mean.toFixed(1)} → ${after.mean.toFixed(1)})`)
  await shading(page, '白模')
  const clayAfter = await settledShot(page, box)
  await shot(page, 'c5-room-clay-after')
  const clay = await pixelDiff(page, clayBefore, clayAfter)
  // An exposure leaking into the clay would change nearly every pixel; far grid lines flicker a little.
  assert.ok(clay < 0.005, `the clay does not change (${(clay * 100).toFixed(3)}% differ)`)
  assert.deepEqual(errors, [])
  await context.close()
})

// --- AC-6 lit export ------------------------------------------------------------

/** A picture as a grid of block means (RGB, 0–255), cols × rows; clip insets each side by a share. */
async function blocks(page, png, cols, rows, inset = 0) {
  return page.evaluate(async ([src, cols, rows, inset]) => {
    const img = await new Promise((resolve) => { const i = new Image(); i.onload = () => resolve(i); i.src = src })
    const c = document.createElement('canvas')
    c.width = img.width
    c.height = img.height
    const x = c.getContext('2d')
    x.drawImage(img, 0, 0)
    const p = x.getImageData(0, 0, c.width, c.height).data
    const x0 = Math.round(c.width * inset), y0 = Math.round(c.height * inset)
    const w = c.width - 2 * x0, h = c.height - 2 * y0
    const out = new Float64Array(cols * rows * 3)
    const n = new Float64Array(cols * rows)
    for (let y = 0; y < h; y++) for (let xx = 0; xx < w; xx++) {
      const b = Math.min(rows - 1, Math.floor((y * rows) / h)) * cols + Math.min(cols - 1, Math.floor((xx * cols) / w))
      const i = ((y + y0) * c.width + xx + x0) * 4
      out[b * 3] += p[i]
      out[b * 3 + 1] += p[i + 1]
      out[b * 3 + 2] += p[i + 2]
      n[b]++
    }
    return Array.from(out, (v, i) => v / n[Math.floor(i / 3)])
  }, [`data:image/png;base64,${png.toString('base64')}`, cols, rows, inset])
}
const meanAbs = (a, b) => a.reduce((sum, v, i) => sum + Math.abs(v - b[i]), 0) / a.length

await check('AC-6: in the rendered view the dialog ticks all three passes; 3 cameras give 9 images, and lit matches the camera view', async () => {
  const { server, a, b } = exportScene(false)
  const c = objectID('camc')
  const lamp = objectID('lamp')
  server.scene.render.exposure_ev = 1
  server.external([
    { op: 'add_objects', objects: [
      cameraObject(c, '机位 C', 6, { focal_mm: 28 }, [-5, 2, 7]),
      { id: lamp, kind: 'light', name: '灯', parent_id: null, order: 7, transform: { ...identity(), position: [1.5, 2, 1.5] }, visible: true, locked: false, light: { type: 'point', color: '#ffb070', intensity: 30, cast_shadow: true } },
    ] },
    { op: 'update_objects', updates: [
      { object_id: objectID('near'), material_slots: { main: { preset: 'stone_brick@1', params: {} } } },
      { object_id: objectID('far'), material_slots: { main: { preset: 'paint@1', params: { color: '#3060a0' } } } },
    ] },
  ])
  const { context, page, errors } = await open(server, { query: '?export=1' })
  await shading(page, '最终渲染')
  await page.getByRole('button', { name: '导出' }).first().click()
  const dialog = page.getByRole('dialog', { name: '导出分镜' })
  await dialog.waitFor()
  for (const name of ['白模（clay）', '线稿', '材质光照']) assert.ok(await dialog.getByLabel(name).isChecked(), `${name} ticked`)
  assert.match(await dialog.textContent(), /共 9 张/)
  await dialog.getByRole('button', { name: '导出' }).click()
  await waitFor(() => server.exports.length === 1, 'export submitted', 180_000)
  await dialog.waitFor({ state: 'detached' })
  const shots = server.exports[0].payload.shots.map((s) => ({ camera: s.camera_object_id, images: s.images.map((img) => ({ pass: img.pass, png: server.uploadBytes.get(img.upload_id) })) }))
  assert.deepEqual(shots.map((s) => [s.camera, s.images.map((i) => i.pass)]), [a, b, c].map((id) => [id, ['clay', 'line', 'lit']]))
  const [clay, , lit] = shots[0].images.map((i) => i.png)
  assert.deepEqual(pngSize(lit), { width: 2048, height: 1365 })
  if (process.env.SCENE_E2E_SHOTS) for (const [name, png] of [['clay', clay], ['lit', lit]]) await writeFile(`${process.env.SCENE_E2E_SHOTS}/c5-export-${name}.png`, png)

  // Through camera A in the rendered view: the same picture, smaller.
  await page.getByRole('treeitem', { name: /机位 A/ }).click()
  await page.keyboard.press('Digit0')
  const frame = page.locator('.cc-scene-camframe')
  await frame.waitFor()
  const fb = await frame.boundingBox()
  const view = await settledShot(page, fb)
  await shot(page, 'c5-camera-view-lit')
  const [cols, rows] = [24, 16]
  const want = await blocks(page, view, cols, rows, 0.03)
  const fromLit = meanAbs(await blocks(page, lit, cols, rows, 0.03), want)
  const fromClay = meanAbs(await blocks(page, clay, cols, rows, 0.03), want)
  console.log(`lit export vs camera view: ${fromLit.toFixed(1)}; clay export vs camera view: ${fromClay.toFixed(1)}`)
  assert.ok(fromLit < 8, `the lit export looks like the rendered camera view (mean difference ${fromLit.toFixed(1)})`)
  assert.ok(fromClay > fromLit * 2, `and unlike the clay export (${fromClay.toFixed(1)})`)
  assert.deepEqual(errors, [])
  await context.close()
})

// --- 3.4 calibration: the reference scenes ----------------------------------

/** A yard: a white box, a concrete wall, a wooden crate and a metal ball on the ground. */
const yard = (server, sky) => {
  server.scene.environment = { sky, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null }
  seed(server, [
    boxOf('white', 5, { position: [-1.5, 0, 0] }, { width: 1, height: 1, depth: 1 }),
    boxOf('wall', 6, { position: [0.5, 0, -2] }, { width: 4, height: 2.5, depth: 0.2 }, { preset: 'concrete@1', params: {} }),
    boxOf('crate', 7, { position: [1.2, 0, 0.5] }, { width: 0.8, height: 0.8, depth: 0.8 }, { preset: 'wood@1', params: {} }),
    { id: gid('ball'), kind: 'primitive', name: 'ball', order: 8, transform: { ...identity(), position: [0, 0, 1.5] }, primitive: { builtin: 'sphere@1', params: { radius: 0.4 } }, material_slots: { main: { preset: 'metal@1', params: { rust: 0 } } } },
  ])
}
/** A room 6 × 4 m, 3 m high, open towards the view, lit by two wall lights. */
const room = (server, intensity) => {
  server.scene.environment = { sky: { kind: 'color', color: '#0b0c0e' }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null }
  const wall = (name, order, position, size) => boxOf(name, order, { position }, { width: size[0], height: size[1], depth: size[2] }, { preset: 'plaster@1', params: {} })
  const lamp = (name, order, x) => ({ id: gid(name), kind: 'light', name, order, transform: { ...identity(), position: [x, 2, -1.75] }, light: { type: 'point', color: '#ffd9b0', intensity, cast_shadow: true, distance: 0, decay: 2 } })
  seed(server, [
    wall('back', 5, [0, 0, -2], [6, 3, 0.2]),
    wall('left', 6, [-3, 0, 0], [0.2, 3, 4]),
    wall('right', 7, [3, 0, 0], [0.2, 3, 4]),
    boxOf('sofa', 8, { position: [0, 0, -1] }, { width: 2, height: 0.8, depth: 0.9 }, { preset: 'fabric@1', params: {} }),
    boxOf('table', 9, { position: [0, 0, 0.5] }, { width: 1, height: 0.45, depth: 0.6 }, { preset: 'wood@1', params: {} }),
    lamp('lampa', 10, -1.8),
    lamp('lampb', 11, 1.8),
  ])
}

await check('calibration: noon at EV 0, dusk at +2 and a room lit by two 40 cd lamps at 0 render in range', async () => {
  const cases = [
    { name: 'noon', ev: 0, setup: (s) => yard(s, { kind: 'procedural', sun_elevation_deg: 55, sun_azimuth_deg: 135, turbidity: 3 }) },
    { name: 'dusk', ev: 2, setup: (s) => yard(s, { kind: 'procedural', sun_elevation_deg: 5, sun_azimuth_deg: 135, turbidity: 3 }) },
    { name: 'room', ev: 0, setup: (s) => room(s, 40) },
  ]
  const seen = {}
  for (const c of cases) {
    const server = new SceneServer()
    c.setup(server)
    server.scene.render.exposure_ev = c.ev
    const { context, page, errors } = await open(server)
    const { box } = await viewportCenter(page)
    await shading(page, '最终渲染')
    const all = await lumaStats(page, await settledShot(page, box), { level: 250 })
    await shot(page, `c5-calibration-${c.name}`)
    // The lower quarter is ground in the yard and the floor before the room.
    const ground = await lumaStats(page, await settledShot(page, { x: box.x, y: box.y + box.height * 0.75, width: box.width, height: box.height * 0.25 }))
    seen[c.name] = { all, ground }
    console.log(`${c.name} at EV ${c.ev}: mean ${all.mean.toFixed(0)}, clipped ${(all.bright * 100).toFixed(1)}%, ground ${ground.mean.toFixed(0)} (rgb ${[ground.r, ground.g, ground.b].map((v) => v.toFixed(0))})`)
    assert.deepEqual(errors, [])
    await context.close()
  }
  const { noon, dusk, room: lamps } = seen
  assert.ok(noon.ground.mean > 175 && noon.ground.mean < 220, 'sunlit light ground is bright, not blown')
  assert.ok(Math.abs(noon.ground.r - noon.ground.b) < 12, 'daylight is neutral')
  assert.ok(noon.all.bright < 0.01 && dusk.all.bright < 0.01, 'nothing clips outdoors')
  assert.ok(dusk.ground.mean > 130 && dusk.ground.mean < 200, 'dusk at +2 is a little darker than noon')
  assert.ok(dusk.ground.r - dusk.ground.b > 15, 'dusk light is warm')
  assert.ok(lamps.all.mean > 40 && lamps.all.mean < 110 && lamps.all.bright < 0.02, 'the lamps light the room without blowing it out')
})

// --- AC-9 performance (opt-in: SCENE_E2E_PERF=1, needs the machine's GPU) ------

/** About 200 objects in 8 materials, the sun and 4 lights of which 2 spots cast shadows (AC-9). */
function perfScene(server) {
  const presets = [
    { preset: 'stone_brick@1', params: {} }, { preset: 'wood@1', params: {} }, { preset: 'metal@1', params: {} }, { preset: 'fabric@1', params: {} },
    { preset: 'marble@1', params: {} }, { preset: 'tile@1', params: {} }, { preset: 'plaster@1', params: {} }, { preset: 'principled@1', params: { color: '#c04040', roughness: 0.3 } },
  ]
  const builtins = [['box@1', { width: 0.8, height: 0.8, depth: 0.8 }], ['cylinder@1', { radius_top: 0.35, radius_bottom: 0.35, height: 1.2, radial_segments: 24 }], ['sphere@1', { radius: 0.45, segments: 32 }]]
  const objects = []
  for (let i = 0; i < 196; i++) {
    const [builtin, params] = builtins[i % 3]
    // Zero-padded and closed with x: gid pads with zeros, so p1 and p10 would collide.
    objects.push({ id: gid(`p${String(i).padStart(3, '0')}x`), kind: 'primitive', name: `件 ${i}`, order: 10 + i, transform: { ...identity(), position: [(i % 14) * 1.3 - 8.5, 0, Math.floor(i / 14) * 1.3 - 8.5] }, primitive: { builtin, params }, material_slots: { main: presets[i % 8] } })
  }
  const light = (name, order, position, rotation, light) => ({ id: gid(name), kind: 'light', name, order, transform: { position, rotation, scale: [1, 1, 1] }, light })
  const down = [-Math.SQRT1_2, 0, 0, Math.SQRT1_2]
  objects.push(
    light('spota', 300, [-3, 5, 0], down, { type: 'spot', color: '#ffffff', intensity: 150, cast_shadow: true, distance: 0, decay: 2, angle: Math.PI / 6, penumbra: 0.2 }),
    light('spotb', 301, [3, 5, 0], down, { type: 'spot', color: '#ffe0c0', intensity: 150, cast_shadow: true, distance: 0, decay: 2, angle: Math.PI / 6, penumbra: 0.2 }),
    light('pointa', 302, [-5, 2, 4], identity().rotation, { type: 'point', color: '#ffd9b0', intensity: 40, cast_shadow: false, distance: 0, decay: 2 }),
    light('pointb', 303, [5, 2, 4], identity().rotation, { type: 'point', color: '#ffd9b0', intensity: 40, cast_shadow: false, distance: 0, decay: 2 }),
  )
  seed(server, objects)
}

/** rAF timestamps for ms while a two-finger swipe turns the view a little every frame, so every frame renders. */
const orbitFrames = (page, ms) =>
  page.evaluate((ms) => new Promise((resolve) => {
    const el = document.querySelector('.cc-scene-editor canvas')
    const times = []
    let sign = 1
    const start = performance.now()
    const tick = (t) => {
      times.push(t)
      sign = -sign
      el.dispatchEvent(new WheelEvent('wheel', { deltaX: 4 * sign, deltaY: 0, deltaMode: 0, bubbles: true, cancelable: true }))
      if (t - start < ms) requestAnimationFrame(tick)
      else resolve(times)
    }
    requestAnimationFrame(tick)
  }), ms)
const gaps = (times) => times.slice(1).map((t, i) => t - times[i])
const frameStats = (times) => {
  const g = gaps(times).sort((a, b) => a - b)
  return { mean: g.reduce((a, b) => a + b, 0) / g.length, p95: g[Math.floor(g.length * 0.95)], max: g.at(-1), frames: g.length }
}

if (process.env.SCENE_E2E_PERF) await check('AC-9: frame times of the reference scene at 1600 × 1000, 2× pixels, on this machine’s GPU', async () => {
  const gpu = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
  try {
    const context = await gpu.newContext({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 })
    const server = new SceneServer()
    perfScene(server)
    const { page, errors } = await open(server, { context })
    const renderer = await page.evaluate(() => {
      const gl = document.createElement('canvas').getContext('webgl2')
      return gl.getParameter(gl.getExtension('WEBGL_debug_renderer_info').UNMASKED_RENDERER_WEBGL)
    })
    const canvas = await page.locator('.cc-scene-editor canvas').evaluate((c) => [c.width, c.height])
    console.log(`AC-9 on ${renderer}, canvas ${canvas.join(' × ')} device pixels`)
    const { x, y } = await viewportCenter(page)
    await page.mouse.move(x, y)
    const results = {}
    for (const [mode, label] of [['线框', 'wireframe'], ['白模', 'solid'], ['材质预览', 'preview'], ['最终渲染', 'rendered']]) {
      await shading(page, mode)
      await settledShot(page, { x: x - 50, y: y - 50, width: 100, height: 100 })
      await orbitFrames(page, 1000) // shaders compiled, caches warm
      results[label] = frameStats(await orbitFrames(page, 4000))
      console.log(`  ${label}: mean ${results[label].mean.toFixed(1)} ms, p95 ${results[label].p95.toFixed(1)} ms, max ${results[label].max.toFixed(1)} ms over ${results[label].frames} frames`)
    }
    // A light added and taken away in the rendered view: the longest frame while three recompiles.
    const hitch = async (act) => {
      const measuring = orbitFrames(page, 3000)
      await sleep(800)
      const at = await page.evaluate(() => performance.now())
      await act()
      const times = await measuring
      const after = times.filter((t) => t >= at)
      return Math.max(...gaps([at, ...after]))
    }
    const add = await hitch(async () => {
      await page.keyboard.press('Shift+KeyA')
      await page.getByRole('menuitem', { name: '点光', exact: true }).click()
    })
    await page.locator('.cc-scene-editor').focus()
    const remove = await hitch(() => page.keyboard.press(`${modKey}+KeyZ`))
    console.log(`  adding a light: longest frame ${add.toFixed(0)} ms; removing it: ${remove.toFixed(0)} ms`)
    assert.ok(results.rendered.mean <= 33, `rendered mean frame ${results.rendered.mean.toFixed(1)} ms`)
    assert.ok(results.preview.mean <= results.rendered.mean, 'material preview is no slower')
    assert.ok(add < 1000 && remove < 1000, 'adding or removing a light hitches under a second')
    assert.deepEqual(errors, [], errors.join('\n'))
    await context.close()
  } finally {
    await gpu.close()
  }
})

await browser.close()
await rm(fixture, { force: true })
await rm(canvasFixture, { force: true })
console.log(results.join('\n'))
if (results.some((r) => r.startsWith('FAIL'))) process.exit(1)
