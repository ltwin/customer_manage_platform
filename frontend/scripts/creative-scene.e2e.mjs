// C1 scene editor in a real browser, against an in-memory scene server.
// Run with the frontend Vite server on CREATIVE_SCENE_URL (default :5179):
//   node scripts/creative-scene.e2e.mjs
// The server applies operations with the client's own copy of the semantics
// (state/apply.ts); the Go tests own the real ones. Every check gets a fresh
// browser context.
import assert from 'node:assert/strict'
import { execSync, spawn } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdtempSync, readFileSync } from 'node:fs'
import { rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { chromium } from 'playwright'
import { Box3, Euler, Matrix4, PerspectiveCamera, Quaternion, Ray, Vector3 } from 'three'
import { applyOps } from '../src/creative-scene/state/apply.ts'
import { completeSky } from '../src/creative-scene/world/physicalSky.ts'
import { documentAzimuth, sunVector } from '../src/creative-scene/world/astro.ts'
import { positionToDoc, positionToView } from '../src/creative-scene/space.ts'
import { Refraction } from 'astronomy-engine'
import { changedUnits } from '../src/creative-scene/state/intents.ts'
import { worldOf } from '../src/creative-scene/state/world.ts'
import { defaultModuleCode } from '../src/creative-scene/sandbox/template.ts'

const DOC = 'cwdoc_scene'
const fixture = new URL('../.scene-editor-qa.html', import.meta.url)
const canvasFixture = new URL('../.scene-canvas-qa.html', import.meta.url)
await writeFile(
  fixture,
  `<html><body style="margin:0"><div id="root" class="cc-workspace" style="width:100vw;height:100vh"></div><script>window.__sceneTest = true</script><script type="module">
import React, {StrictMode, useState} from 'react';
import {createRoot} from 'react-dom/client';
import '/src/index.css';
import '/src/creative-canvas/editor/workspace.css';
import SceneEditor from '/src/creative-scene/SceneEditor.tsx';
import SaveModelDialog from '/src/creative-canvas/editor/SaveModelDialog.tsx';
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
// ?library=1 hands the editor the canvas page's 「存入资产库」 dialog; a save makes the asset panel read again.
function Editor({host}) {
  const [open, setOpen] = useState(true);
  const [saving, setSaving] = useState(null);
  const [tick, setTick] = useState(0);
  const [notice, setNotice] = useState('');
  window.__reopen = () => setOpen(true);
  const query = new URLSearchParams(location.search);
  const group = React.createElement('select', {name:'group'}, React.createElement('option', {value:''}, '不归类'), React.createElement('option', {value:'cgrp_qa'}, '柱子'));
  return open
    ? React.createElement(React.Fragment, null,
        React.createElement(SceneEditor, {documentID:'${DOC}', host, onClose:()=>{window.__closed++; setOpen(false)}, ...(query.has('export') ? {canvasID:'ccv_qa', sceneNodeID:'cwnode_qa'} : {}), ...(query.has('library') ? {onSaveModel: setSaving, libraryTick: tick} : {})}),
        saving && React.createElement(SaveModelDialog, {key: saving.target.objectID, request: saving, groupField: group, groupName: () => '柱子', onSaved: (n) => { setNotice(n); setTick((t) => t + 1) }, onClose: () => setSaving(null)}),
        notice && React.createElement('p', {id:'notice'}, notice))
    : React.createElement('p', {id:'closed'}, 'closed');
}
function WithHost() {
  const launcher = useSceneHost('qa-account').launcher;
  window.__launcher = launcher;
  return React.createElement(Editor, {host: launcher});
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
/** JSON with object keys sorted, for comparing material assignments. */
const canonical = (v) => JSON.stringify(v, (_k, x) => (x && typeof x === 'object' && !Array.isArray(x) ? Object.fromEntries(Object.keys(x).sort().map((k) => [k, x[k]])) : x))

/** A scene as the server reads it out: a physical sky completed with the fields it left out (backend sky.go completeSky), the night sky among them. */
const served = (scene) => {
  const out = structuredClone(scene)
  out.environment.sky = completeSky(out.environment.sky)
  return out
}

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
      render: { mode: 'clay', exposure_ev: 0, engine: 'workbench', resolution: '2k' },
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
    // Models saved into the asset library, by source revision and materials; every save request.
    this.assets = new Map()
    this.saves = []
    // Set to make the next new library copy find its thumbnail upload expired.
    this.expireThumbnail = false
    // The asset panel's list, the queries it sent, and the single assets it read.
    this.library = []
    this.assetQueries = []
    this.assetReads = []
  }
  /** A model asset in the library: a library copy of the seeded revision with these materials, without its code. Returns the copy's revision. */
  libraryAsset(rev, title, materials = {}) {
    const m = this.models.get(rev)
    const copy = this.libraryCopy(rev, materials)
    const { content_id } = this.models.get(copy)
    const { code: _code, ...generator } = m.generator
    this.library.push({
      id: `ccas_${this.library.length + 1}`, title, description: '', kind: 'model', revision: '1', content_id, content_revision_id: copy,
      content: { id: copy, content_id, kind: 'model', sequence: '1', truncated: false, payload: { model: { format: 'glb', manifest: m.manifest, generator, name_hint: m.name_hint, materials, source_revision_id: rev } },
        media: [{ role: 'original', blob_id: 'glb', mime: 'model/gltf-binary', byte_size: m.glb.byteLength, width: null, height: null, duration_ms: null }, { role: 'thumbnail', blob_id: 'png', mime: 'image/png', byte_size: 64, width: 256, height: 256, duration_ms: null }] },
      unavailable: false, is_favorite: false, rating: null, palette: [], created_at: '2026-10-20T00:00:00Z', deleted_at: null, purge_after: null, group_ids: [], tag_ids: [],
    })
    return copy
  }
  /** New content copying a model revision with materials, as saving into the library writes it. */
  libraryCopy(rev, materials, thumbnail = null) {
    const m = this.models.get(rev)
    const copy = `ccrv_copy${++this.modelSeq}`
    this.models.set(copy, { ...m, content_id: `ccnt_copy${this.modelSeq}`, materials, source: m.source ?? rev, thumbnail })
    return copy
  }
  /** Whether a content is a library copy, which commit_model never continues. */
  isCopy(contentID) {
    return [...this.models.values()].some((m) => m.content_id === contentID && m.source)
  }
  /** POST /assets/from-scene-model: a copy of the instance's model and materials, if the editor saw its current version. */
  saveModel(body) {
    const p = body.payload
    this.saves.push(body)
    const unit = content(p.object_id)
    const current = this.unitRevs[unit] ?? null
    if (current !== p.expected_object_rev)
      return [409, { error: { code: 'creative_document_conflict', message: '文档已被修改', details: { conflicts: [{ unit, expected_rev: p.expected_object_rev, current_rev: current }] } } }]
    const object = this.object(p.object_id)
    const rev = object.model.content_revision_id
    const src = this.models.get(rev)
    const slots = new Set(src.manifest.slots.map((x) => x.id))
    const materials = Object.fromEntries(Object.entries(object.material_slots ?? {}).filter(([k]) => slots.has(k)))
    const key = `${src.source ?? rev} ${canonical(materials)}`
    const existing = this.assets.get(key)
    if (existing) return [200, { ...existing, reused: true }]
    const u = this.uploads.get(p.thumbnail_upload_id)
    if (this.expireThumbnail || !u || u.state !== 'ready' || u.consumed || u.kind !== 'image') {
      this.expireThumbnail = false
      return [409, { error: { code: 'creative_upload_unavailable', message: '暂存上传已失效' } }]
    }
    u.consumed = true
    const copy = this.libraryCopy(rev, materials, this.uploadBytes.get(p.thumbnail_upload_id))
    const asset = { asset_id: `ccas_${this.assets.size + 1}`, revision: '1', title: p.target.title, description: '', content_revision_id: copy, library_revision: '1', tag_mapping: {}, tags_created: [], tags_reused: [], reused: false }
    this.assets.set(key, asset)
    return [201, asset]
  }
  /** A committed model revision, as if made earlier; returns its revision ID. */
  seedModel({ code = defaultModuleCode, params = templateParams, schema = templateSchema, glb = templateGLB, manifest = templateManifest, content, mount = 'floor' } = {}) {
    const rev = `ccrv_seed${++this.modelSeq}`
    const generator = { language: 'three-module', contract_version: 1, lib_version: 1, code, code_sha256: `sha256-${createHash('sha256').update(code).digest('hex')}`, params_schema: schema, params, mount }
    this.models.set(rev, { content_id: content ?? `ccnt_seed${this.modelSeq}`, glb, generator, manifest, name_hint: '柱' })
    return rev
  }
  /** A model instance placed earlier, at revision 1 like the ground. */
  seedInstance(id, rev, x = 0, name = '柱', slots = {}) {
    this.scene.objects.push({
      id, kind: 'model_instance', name, parent_id: null, order: this.scene.objects.length,
      transform: { ...identity(), position: [x, 0, 0] }, visible: true, locked: false,
      model: { content_revision_id: rev }, material_slots: slots,
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
   * unused, then a new revision under base_content_id (unless it names a
   * library copy) or a new content, and every {ref} in the batch becomes that
   * revision.
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
      const content_id = op.base_content_id && !this.isCopy(op.base_content_id) ? op.base_content_id : `ccnt_m${this.modelSeq}`
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
      scene: served(this.scene),
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
      scene: served(this.scene),
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
        else if (unit.endsWith(':content')) Object.assign(now, { primitive: was.primitive, model: was.model, material_slots: was.material_slots, light: was.light, camera: was.camera, volume: was.volume })
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
    if (path === '/assets' && request.method() === 'GET') {
      server.assetQueries.push(url.searchParams.toString())
      const kind = url.searchParams.get('kind')
      const q = url.searchParams.get('q')
      const items = server.library.filter((a) => (!kind || a.kind === kind) && (!q || a.title.includes(q)))
      return json(200, { items, next_cursor: '', total_count: items.length, library_revision: '1' })
    }
    if (path.startsWith('/assets/ccas_') && request.method() === 'GET') {
      server.assetReads.push(path)
      const asset = server.library.find((a) => a.id === path.slice('/assets/'.length))
      return asset ? json(200, asset) : json(404, { error: { code: 'not_found', message: '资产不存在' } })
    }
    if (path === '/asset-groups') return json(200, { items: [], hierarchy_revision: '1', asset_counts: {} })
    if (path === '/tags' || path === '/tag-categories') return json(200, { items: [], hierarchy_revision: '1' })
    if (path === '/library-usage') return json(200, { asset_count: server.library.length, stored_bytes: 0, limit_bytes: 1, pending_release_bytes: 0 })
    if (path === '/library-settings') return json(200, { retention_days: 30, revision: '1', hierarchy_revision: '1', library_revision: '1' })
    if (path === '/assets/from-scene-model' && request.method() === 'POST') {
      const [status, body] = server.saveModel(request.postDataJSON())
      return json(status, body)
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
  // SCENE_E2E_CONSOLE=1 prints every console message, warnings (WGSL compile errors among them) included.
  if (process.env.SCENE_E2E_CONSOLE) {
    page.on('console', (m) => console.log(`[console ${m.type()}] ${m.text().slice(0, 2000)}`))
    page.on('pageerror', (e) => console.log(`[pageerror] ${e.stack?.slice(0, 2000)}`))
  }
  await route(page, server)
  await routeFiles(page, server)
  await page.goto(`${base}/.scene-editor-qa.html${options.query ?? ''}`)
  // viewport: false for a browser that cannot draw, where there is no canvas to wait for.
  if (options.viewport === false) return { context, page, errors }
  await viewportReady(page)
  await page.getByRole('treeitem').first().waitFor()
  // Let R3F mount the scene and the camera settle.
  await sleep(400)
  return { context, page, errors }
}

/** The renderer is made asynchronously (render/gpu.ts): the viewport takes keys and has its size once it is. */
async function viewportReady(page) {
  const canvas = page.locator('.cc-scene-editor canvas')
  await canvas.waitFor({ timeout: 20_000 })
  await canvas.evaluate((c) => new Promise((resolve) => {
    const sized = () => (c.clientWidth === c.parentElement.clientWidth && c.clientHeight === c.parentElement.clientHeight ? resolve() : requestAnimationFrame(sized))
    sized()
  }))
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

/** The bake's clay thumbnail of the template model, rendered in the page. */
async function clayThumbnail(page) {
  const url = await page.evaluate(async ([glb, manifest]) => {
    const { renderThumbnail } = await import('/src/creative-scene/sandbox/thumbnail.ts')
    const bytes = Uint8Array.from(atob(glb), (c) => c.charCodeAt(0))
    const blob = await renderThumbnail(bytes.buffer, manifest)
    return new Promise((resolve) => {
      const r = new FileReader()
      r.onload = () => resolve(r.result)
      r.readAsDataURL(blob)
    })
  }, [templateGLB.toString('base64'), templateManifest])
  return Buffer.from(url.slice(url.indexOf(',') + 1), 'base64')
}

const pngSize = (bytes) => ({ width: bytes.readUInt32BE(16), height: bytes.readUInt32BE(20) })

await check('C6 save to library: the model with its materials, the card thumbnail in them; the same materials are filed, others a new asset; materials changed meanwhile are confirmed first', async () => {
  const server = new SceneServer()
  const first = server.seedModel()
  const pillar = 'sobj_P00000000000000000001'
  const marble = { stone: { preset: 'marble@1', params: {} } }
  server.seedInstance(pillar, first, 0, '柱', marble)
  const { context, page, errors } = await open(server, { query: '?library=1' })
  await page.getByRole('treeitem', { name: /柱/ }).click()
  const save = page.getByRole('button', { name: '存入资产库…' })
  const dialog = page.getByRole('dialog', { name: '存入个人资产库' })
  const title = dialog.getByRole('textbox', { name: '资产名称' })
  const store = dialog.getByRole('button', { name: '存入', exact: true })
  // The dialog closes on success; the notice may still show the last save's text.
  const notice = async (text) => {
    await dialog.waitFor({ state: 'detached' })
    await page.locator('#notice').filter({ hasText: text }).waitFor()
  }
  const copyOf = (body) => server.models.get([...server.assets.values()].find((a) => a.asset_id === body.asset_id)?.content_revision_id)
  const thumbnailOf = (n) => server.uploadBytes.get(server.saves[n].payload.thumbnail_upload_id)

  await save.click()
  assert.equal(await title.inputValue(), '柱', 'the model name by default')
  await dialog.getByText('保存模型、参数和材质；位置、旋转和缩放不随资产保存', { exact: false }).waitFor()
  await title.fill('大理石柱')
  await store.click()
  await notice('已存入资产库')
  const { thumbnail_upload_id: thumb, ...sent } = server.saves[0].payload
  assert.deepEqual(sent, { document_id: DOC, object_id: pillar, expected_object_rev: '1', target: { title: '大理石柱', group_ids: [] } })
  assert.equal(server.uploads.get(thumb).kind, 'image')
  const marbled = copyOf([...server.assets.values()][0])
  assert.deepEqual(marbled.materials, marble, 'the asset keeps the instance\'s materials')
  assert.equal(marbled.source, first)
  assert.deepEqual(pngSize(marbled.thumbnail), { width: 256, height: 256 })
  assert.ok((await pixelDiff(page, await clayThumbnail(page), marbled.thumbnail)) > 0.02, 'the card shows the materials, not clay')

  await save.click()
  await dialog.locator('select[name="group"]').selectOption('cgrp_qa')
  await store.click()
  await notice('这个模型已在资产库中，已归入「柱子」')
  assert.equal(server.assets.size, 1, 'the same model with the same materials is not saved twice')

  // Another material is another asset, its thumbnail in that material.
  await page.getByLabel('stone 的材质').selectOption('wood@1')
  await waitFor(() => server.object(pillar).material_slots.stone?.preset === 'wood@1', 'material')
  await saved(page)
  await save.click()
  await store.click()
  await notice('已存入资产库')
  assert.equal(server.assets.size, 2, 'other materials, a new asset')
  const wooden = copyOf([...server.assets.values()][1])
  assert.equal(wooden.materials.stone.preset, 'wood@1')
  assert.ok((await pixelDiff(page, marbled.thumbnail, wooden.thumbnail)) > 0.02, 'the new card shows the new material')

  // The assistant paints the pillar while the dialog is open.
  await save.click()
  server.external([{ op: 'update_objects', updates: [{ object_id: pillar, material_slots: { stone: { preset: 'paint@1', params: {} } } }] }], { actor: 'agent' })
  await store.click()
  await dialog.getByText('模型已更新为最新版本，确认后再存').waitFor()
  const conflicted = server.saves.at(-1)
  assert.equal(server.assets.size, 2, 'nothing saved on the conflict')
  await store.click()
  await notice('已存入资产库')
  const last = server.saves.at(-1)
  assert.equal(last.payload.expected_object_rev, server.unitRevs[content(pillar)])
  assert.notEqual(last.operation_id, conflicted.operation_id, 'a new request after the refresh')
  assert.notEqual(last.payload.thumbnail_upload_id, conflicted.payload.thumbnail_upload_id, 'a thumbnail of the new version')
  assert.equal(copyOf([...server.assets.values()][2]).materials.stone.preset, 'paint@1', 'the new materials are what was saved')

  // A thumbnail upload that expired is rendered and sent again, once, unasked.
  await page.getByLabel('stone 的材质').selectOption('metal@1')
  await waitFor(() => server.object(pillar).material_slots.stone?.preset === 'metal@1', 'metal')
  await saved(page)
  await save.click()
  server.expireThumbnail = true
  const before = server.saves.length
  await store.click()
  await notice('已存入资产库')
  assert.equal(server.saves.length, before + 2, 'refused once, then saved')
  assert.notEqual(server.saves.at(-1).payload.thumbnail_upload_id, server.saves.at(-2).payload.thumbnail_upload_id)
  assert.equal(server.assets.size, 4)
  assert.ok(thumbnailOf(server.saves.length - 1))
  assert.deepEqual(errors, [])
  await context.close()
})

await check('asset panel: Shift+A 从资产库添加… opens the 资产 tab; search narrows; a click selects, a double click puts the model with its materials in, selected, one undo; the panel stays; Enter and 最近使用; Shift+F1', async () => {
  const server = new SceneServer()
  const marble = { stone: { preset: 'marble@1', params: { scale: 1 } } }
  const rev = server.libraryAsset(server.seedModel(), '哥特束柱', marble)
  server.libraryAsset(server.seedModel(), '石凳')
  const { context, page, errors } = await open(server, { query: '?library=1' })
  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x + 120, y + 60)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '从资产库添加…' }).click()
  const panel = page.locator('.cc-scene-assets')
  await panel.waitFor()
  assert.equal(await page.getByRole('tab', { name: '资产' }).getAttribute('aria-selected'), 'true', 'the sidebar shows the asset tab')
  const search = panel.getByRole('searchbox', { name: '搜索模型' })
  await waitFor(() => search.evaluate((el) => el === document.activeElement), 'the search box takes the keys')
  await waitFor(() => server.assetQueries.some((q) => new URLSearchParams(q).get('kind') === 'model'), 'only models are listed')
  const tile = (title, list = '模型') => panel.getByRole('listbox', { name: list, exact: true }).getByRole('option', { name: title, exact: true })
  await tile('石凳').waitFor()
  await search.fill('束柱')
  await tile('石凳').waitFor({ state: 'detached' })
  assert.ok(server.assetQueries.some((q) => new URLSearchParams(q).get('q') === '束柱'), 'the search goes to the server')

  const before = server.commands.length
  await tile('哥特束柱').click()
  const detail = panel.locator('.cc-scene-assets-detail')
  await detail.getByText('哥特束柱').waitFor()
  await detail.getByText(/参考点底部中心 · .* 米/).waitFor()
  await shot(page, 'asset-panel')
  await sleep(300)
  assert.equal(server.commands.length, before, 'a click only selects')
  await tile('哥特束柱').dblclick()
  await waitFor(() => server.commands.length === before + 1, 'place command')
  const [add] = server.commands.at(-1).payload.operations
  assert.equal(add.op, 'add_objects')
  const placed = add.objects[0]
  assert.equal(placed.kind, 'model_instance')
  assert.equal(placed.name, '哥特束柱')
  assert.deepEqual(placed.model, { content_revision_id: rev })
  assert.deepEqual(placed.material_slots, marble, 'the asset\'s materials come with it')
  assert.ok(close(placed.transform.position[1], 0, 1e-3), 'on the ground')
  assert(await panel.isVisible(), 'the panel stays open')
  await page.locator('.cc-scene-model-info').filter({ hasText: '代码模型：柱' }).waitFor()
  await saved(page)
  const mod = process.platform === 'darwin' ? 'Meta' : 'Control'
  await page.keyboard.press(`${mod}+KeyZ`)
  await waitFor(() => !server.scene.objects.some((o) => o.id === placed.id), 'keys went back to the editor: one undo removes it')

  // With no search the model is under 最近使用; Enter on a card puts it in too.
  await search.fill('')
  const recent = tile('哥特束柱', '最近使用的模型')
  await recent.waitFor()
  const n = server.commands.length
  await recent.focus()
  await page.keyboard.press('Enter')
  await waitFor(() => server.commands.length === n + 1, 'Enter places')
  await saved(page)

  assert.deepEqual(server.assetReads, [], 'a model already on the list is not read again for 最近使用')

  // The object list shows it selected; Shift+F1 goes back to the assets.
  await search.fill('石')
  await tile('哥特束柱').waitFor({ state: 'detached' })
  const queries = server.assetQueries.length
  await page.getByRole('tab', { name: '大纲' }).click()
  const row = page.getByRole('treeitem', { name: /哥特束柱/ })
  await row.waitFor()
  assert.equal(await row.getAttribute('aria-selected'), 'true', 'the placed model is selected')
  await page.keyboard.press('Shift+F1')
  await panel.waitFor()
  await tile('石凳').waitFor()
  assert.equal(await search.inputValue(), '石', 'the search is kept')
  await sleep(300)
  assert.equal(server.assetQueries.length, queries, 'switching tabs back does not read the library again')
  assert.deepEqual(server.assetReads, [])
  await page.keyboard.press('Escape') // clears the search
  await page.keyboard.press('Escape') // keys go back to the editor
  await page.keyboard.press('Shift+F1')
  await panel.waitFor({ state: 'hidden' })
  assert.deepEqual(errors, [])
  await context.close()
})

await check('asset panel selection: a click on the blank clears; Cmd/Ctrl adds; a drag over the blank boxes; 放入 N 个模型 puts them in a row, all selected, one undo', async () => {
  const server = new SceneServer()
  for (const title of ['哥特束柱', '石凳', '方凳']) server.libraryAsset(server.seedModel(), title)
  const { context, page, errors } = await open(server)
  await page.keyboard.press('Shift+F1')
  const panel = page.locator('.cc-scene-assets')
  const body = panel.locator('.cc-scene-assets-body')
  const tile = (title) => panel.getByRole('listbox', { name: '模型', exact: true }).getByRole('option', { name: title, exact: true })
  const chosen = () => panel.getByRole('listbox', { name: '模型', exact: true }).getByRole('option', { selected: true }).count()
  await tile('方凳').waitFor()
  const area = await body.boundingBox()
  const last = await tile('方凳').boundingBox()
  // Just below the cards, inside the panel.
  const blank = { x: area.x + area.width - 6, y: last.y + last.height + 16 }
  assert.ok(blank.y < area.y + area.height, `room below the cards: ${JSON.stringify({ area, last })}`)

  await tile('石凳').click()
  assert.equal(await chosen(), 1)
  await page.mouse.click(blank.x, blank.y)
  assert.equal(await chosen(), 0, 'a click on the blank clears')

  await tile('哥特束柱').click()
  await tile('石凳').click({ modifiers: ['ControlOrMeta'] })
  assert.equal(await chosen(), 2, 'Cmd/Ctrl adds')
  await panel.getByText('已选 2 个模型').waitFor()
  await tile('石凳').click({ modifiers: ['ControlOrMeta'] })
  assert.equal(await chosen(), 1, 'and takes away')
  await page.keyboard.press('Escape')
  assert.equal(await chosen(), 0, 'Esc on a card clears')

  await page.mouse.move(blank.x, blank.y)
  await page.mouse.down()
  await page.mouse.move(area.x + 4, area.y + 4, { steps: 8 })
  await panel.locator('.cc-scene-assets-marquee').waitFor()
  await page.mouse.up()
  assert.equal(await chosen(), 3, 'the box takes every card it touches')
  await panel.locator('.cc-scene-assets-marquee').waitFor({ state: 'detached' })
  assert.equal(await chosen(), 3, 'letting go of the box does not clear it')
  await shot(page, 'asset-panel-multi')

  const before = server.commands.length
  await panel.getByRole('button', { name: '放入 3 个模型' }).click()
  await waitFor(() => server.commands.length === before + 1, 'one command')
  const ops = server.commands.at(-1).payload.operations
  assert.equal(ops.length, 1)
  assert.deepEqual(ops[0].objects.map((o) => o.name), ['哥特束柱', '石凳', '方凳'])
  const xs = ops[0].objects.map((o) => o.transform.position[0])
  assert.ok(xs[0] < xs[1] && xs[1] < xs[2], `in a row: ${xs}`)
  assert.ok(Math.abs(xs[0] + xs[2]) < 1e-6, 'centred')
  assert.ok(ops[0].objects.every((o) => close(o.transform.position[1], 0, 1e-3)), 'on the ground')
  await page.getByRole('tab', { name: '大纲' }).click()
  for (const title of ['哥特束柱', '石凳', '方凳']) assert.equal(await page.getByRole('treeitem', { name: new RegExp(title) }).getAttribute('aria-selected'), 'true', `${title} selected`)
  await saved(page)
  const mod = process.platform === 'darwin' ? 'Meta' : 'Control'
  await page.keyboard.press(`${mod}+KeyZ`)
  await waitFor(() => !server.scene.objects.some((o) => o.kind === 'model_instance'), 'one undo takes all three')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('C6 drag in: a floor model lands on a box top, the asset panel stays open; a ceiling model with no ceiling stands on what is below; let go elsewhere and nothing changes', async () => {
  const server = new SceneServer()
  const block = 'sobj_B00000000000000000001'
  server.scene.objects.push({ id: block, kind: 'primitive', name: '台子', parent_id: null, order: 1, transform: identity(), visible: true, locked: false, primitive: { builtin: 'box@1', params: { width: 6, height: 1, depth: 6 } } })
  server.unitRevs[placement(block)] = '1'
  server.unitRevs[content(block)] = '1'
  server.history[0] = server.record(['*'], [], null)
  const wood = { stone: { preset: 'wood@1', params: {} } }
  const stool = server.libraryAsset(server.seedModel(), '石凳', wood)
  server.libraryAsset(server.seedModel({ mount: 'ceiling' }), '吊灯')
  const { context, page, errors } = await open(server, { query: '?library=1' })
  const canvas = page.locator('.cc-scene-editor canvas')
  await canvas.click({ position: { x: 20, y: 20 } })
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '从资产库添加…' }).click()
  const panel = page.locator('.cc-scene-assets')
  const tile = (title) => panel.getByRole('listbox', { name: '模型', exact: true }).getByRole('option', { name: title, exact: true })
  await tile('石凳').waitFor()
  const { box } = await viewportCenter(page)
  const centre = { x: box.width / 2, y: box.height / 2 }

  const dropAt = async (title, at) => {
    const before = server.commands.length
    await tile(title).dragTo(canvas, { targetPosition: at })
    await waitFor(() => server.commands.length === before + 1, `${title} placed`)
    const [add] = server.commands.at(-1).payload.operations
    assert.equal(add.op, 'add_objects')
    return add.objects[0]
  }
  const first = await dropAt('石凳', centre)
  assert.equal(first.model.content_revision_id, stool)
  assert.deepEqual(first.material_slots, wood, 'dragged in with the asset\'s materials')
  assert.ok(close(first.transform.position[1], 1, 1e-3), `stands on the block's top: ${first.transform.position}`)
  assert.deepEqual(first.transform.rotation, [0, 0, 0, 1], 'upright')
  assert(await panel.isVisible(), 'the panel stays open after a drag')
  const second = await dropAt('石凳', { x: centre.x + 60, y: centre.y + 20 })
  assert.ok(close(second.transform.position[1], 1, 1e-3))
  assert.notDeepEqual(second.transform.position, first.transform.position, 'each lands where it was let go')
  assert.equal(server.commands.length, 2, 'one revision each')
  assert.ok(close(server.object(first.id).transform.position[1], 1, 1e-3))

  // No ceiling here: the lamp's bounds stand on the block's top, never buried in it.
  const hung = await dropAt('吊灯', { x: centre.x - 40, y: centre.y + 10 })
  assert.ok(close(hung.transform.position[1], 1 - templateManifest.bounds.min[1], 1e-3), `on the block: ${hung.transform.position}`)

  // A drag over the viewport shows the ghost; let go over the panel and nothing is placed.
  const still = await canvas.screenshot()
  const from = await tile('石凳').boundingBox()
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(box.x + centre.x - 80, box.y + centre.y + 40, { steps: 8 })
  await page.mouse.move(box.x + centre.x - 90, box.y + centre.y + 40, { steps: 2 })
  await sleep(300)
  assert.ok((await pixelDiff(page, still, await canvas.screenshot())) > 0.002, 'the ghost follows the pointer')
  await shot(page, 'c6-drag-ghost')
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2, { steps: 8 })
  await page.mouse.up()
  await sleep(300)
  assert.equal(server.commands.length, 3, 'letting go elsewhere places nothing')
  assert.ok((await pixelDiff(page, still, await canvas.screenshot())) < 0.002, 'and the ghost is gone')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('C6 copy semantics: a placed model changes only in this scene; editing it, before or after a reload, forks a model of its own; the asset places as saved', async () => {
  const server = new SceneServer()
  const marble = { stone: { preset: 'marble@1', params: { scale: 1 } } }
  const rev = server.libraryAsset(server.seedModel(), '哥特束柱', marble)
  const copy = server.models.get(rev).content_id
  let { context, page, errors } = await open(server, { query: '?library=1' })
  const canvas = () => page.locator('.cc-scene-editor canvas')
  const place = async () => {
    const before = server.commands.length
    await canvas().click({ position: { x: 20, y: 20 } })
    await page.keyboard.press('Shift+KeyA')
    await page.getByRole('menuitem', { name: '从资产库添加…' }).click()
    await page.locator('.cc-scene-assets').getByRole('option', { name: '哥特束柱', exact: true }).first().dblclick()
    await waitFor(() => server.commands.length === before + 1, 'placed')
    await saved(page)
    return server.commands.at(-1).payload.operations[0].objects[0]
  }
  /** Retunes the selected instance's height; returns the commit it sent and the content it got. */
  const retune = async (id, value) => {
    const before = server.commands.length
    const height = page.getByLabel('高度（m）', { exact: true })
    await height.fill(value)
    await height.press('Enter')
    await waitFor(() => server.commands.length === before + 1, 'retune command', 20_000)
    await saved(page)
    const [commit, set] = server.commands.at(-1).payload.operations
    assert.equal(commit.op, 'commit_model')
    assert.deepEqual(set, { op: 'set_instance_model', object_id: id, model: { ref: commit.ref }, scope: 'this' })
    const forked = server.object(id).model.content_revision_id
    assert.notEqual(forked, rev)
    return { commit, content: server.models.get(forked).content_id }
  }
  const first = await place()

  // A slot's material: the instance only.
  await page.getByLabel('stone 的材质').selectOption('wood@1')
  await waitFor(() => server.object(first.id).material_slots.stone?.preset === 'wood@1', 'material')
  await saved(page)

  // A parameter: the editor names the copy as R4 says, the server forks.
  const one = await retune(first.id, '5')
  assert.equal(one.commit.base_content_id, copy, 'the editor follows R4; it needs to know nothing of copies')
  assert.equal(one.commit.generator.params.height, 5)
  assert.notEqual(one.content, copy, 'a content of its own')
  assert.equal(server.library[0].content_revision_id, rev, 'the asset keeps its revision')
  assert.equal(server.models.get(rev).generator.params.height, 3, 'and that revision is as saved')
  assert.equal([...server.models.values()].filter((m) => m.content_id === copy).length, 1, 'the copy has its one revision')

  const second = await place()
  assert.deepEqual(second.model, { content_revision_id: rev }, 'placing again gives the model as saved')
  assert.deepEqual(second.material_slots, marble, 'with the asset\'s materials, not this scene\'s')
  assert.deepEqual(errors, [])

  // After a reload the editor has no memory of where the model came from: the same result.
  await context.close()
  ;({ context, page, errors } = await open(server, { query: '?library=1' }))
  await page.getByRole('treeitem', { name: '哥特束柱' }).nth(1).click()
  const two = await retune(second.id, '4')
  assert.notEqual(two.content, copy)
  assert.notEqual(two.content, one.content)
  assert.equal([...server.models.values()].filter((m) => m.content_id === copy).length, 1)
  assert.deepEqual(errors, [])
  await context.close()
})

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

await check('fog written as three.js Fog {color, near, far} shows in the rendered view', async () => {
  // Both open rendered, as the scene suggests: clay has the studio light and no fog.
  const plain = new SceneServer()
  plain.scene.render.mode = 'lit'
  const clear = await open(plain)
  const before = await clear.page.locator('.cc-scene-editor canvas').screenshot()
  await clear.context.close()
  const server = new SceneServer()
  server.scene.render.mode = 'lit'
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
  // The far tower's right edge (40 m, out of focus) and the near box's left edge (8 m, in focus),
  // above the horizon: the horizon is out of focus, and in the studio light it is the strongest edge there.
  const tower = [0.62, 0.15, 0.7, 0.5]
  const box = [0.38, 0.38, 0.44, 0.5]
  const whole = [await sharpness(page, sClay, tower), await sharpness(page, bClay, tower)]
  const centre = [await sharpness(page, sClay, box), await sharpness(page, bClay, box)]
  assert.ok(whole[1] < whole[0] * 0.6, `out of focus is softer: ${whole.map((v) => v.toFixed(3))}`)
  // The blurred background spreads a pixel over the in-focus edge: against the studio's grey that
  // is a good share of what the region has, so in focus is told by keeping far more than out of focus.
  assert.ok(centre[1] / centre[0] > (whole[1] / whole[0]) * 4, `in focus stays sharp: ${centre.map((v) => v.toFixed(3))}, out of focus ${whole.map((v) => v.toFixed(3))}`)
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
  // Brighter than the grid lines: only what the spot lights. 177 is where AgX shows the light
  // ACES Filmic showed as 200 (linear 0.53; W1 discovery/research/agx-vs-aces.md).
  const lit = { level: 177 }
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

await check('lights: the rendered view warms its lights in one compile; adding, undoing and switching away and back leave nothing behind', async () => {
  const server = new SceneServer()
  const down = [-Math.SQRT1_2, 0, 0, Math.SQRT1_2]
  const lamp = (name, order, position, light) => ({ id: gid(name), kind: 'light', name, order, transform: { position, rotation: down, scale: [1, 1, 1] }, light })
  seed(server, [
    boxOf('crate', 5, { position: [-1, 0, 0] }, { width: 1, height: 1, depth: 1 }, { preset: 'wood@1', params: {} }),
    boxOf('post', 6, { position: [1, 0, 0] }, { width: 0.4, height: 1.5, depth: 0.4 }),
    lamp('spota', 10, [0, 3, 0], { type: 'spot', color: '#ffffff', intensity: 60, cast_shadow: true, distance: 0, decay: 2, angle: Math.PI / 6, penumbra: 0.2 }),
    lamp('pointa', 11, [-2, 2, 2], { type: 'point', color: '#ffd9b0', intensity: 20, cast_shadow: false, distance: 0, decay: 2 }),
    lamp('pointb', 12, [2, 2, 2], { type: 'point', color: '#ffd9b0', intensity: 20, cast_shadow: false, distance: 0, decay: 2 }),
  ])
  const { context, page, errors } = await open(server)
  const { x, y, box } = await viewportCenter(page)
  const compiles = () => page.evaluate(() => window.__sceneGPU.compiles)
  const compiled = () => waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiling)) === 0, 'compiled', 60_000)
  const before = await compiles()
  await shading(page, '最终渲染')
  await compiled()
  assert.equal((await compiles()) - before, 1, 'the three lights compile together')
  /** What the viewport's renderer holds on the device. */
  const memory = () =>
    page.evaluate(() => {
      const { textures, renderTargets, uniformBuffers, programs } = [...window.__sceneGPU.live].filter((h) => h.role === 'viewport').at(-1).info.memory
      return { textures, renderTargets, uniformBuffers, programs }
    })
  const round = async (n) => {
    await page.mouse.move(x, y)
    await page.keyboard.press('Shift+KeyA')
    await page.getByRole('menuitem', { name: '点光', exact: true }).click()
    await waitFor(() => server.commands.length === 2 * n - 1, 'add a point light')
    await compiled()
    await saved(page)
    await focusEditor(page)
    await page.keyboard.press(`${modKey}+KeyZ`)
    await waitFor(() => server.commands.length === 2 * n, 'undo')
    await saved(page)
    await shading(page, '白模')
    // Past the second after which an unused material is freed (materials/instances.ts).
    await sleep(1500)
    await shading(page, '最终渲染')
    await compiled()
    await settledShot(page, box)
  }
  await round(1)
  const settled = await memory()
  await round(2)
  await round(3)
  assert.deepEqual(await memory(), settled, 'the same resources after two more rounds')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('AC-5: with nothing selected the environment shows; the sun at 5° lengthens rendered shadows and turns the sky to dusk, clay stays; Cmd+Z', async () => {
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
  const clayNoon = await settledShot(page, box)
  await shading(page, '最终渲染')
  const noonLit = await footprint('35-lit')
  const noonSky = await warmth()
  // Set in clay, where the environment tab still edits it.
  await shading(page, '白模')
  const elevation = page.getByRole('spinbutton', { name: '太阳高度（°）' })
  await elevation.fill('5')
  await elevation.press('Enter')
  await waitFor(() => server.scene.environment.sky.sun_elevation_deg === 5, 'the sun goes down')
  assert.deepEqual(server.scene.environment, { sky: { kind: 'procedural', sun_elevation_deg: 5, sun_azimuth_deg: 135, turbidity: 3 }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null }, 'only the elevation changes')
  assert.deepEqual(server.commands.at(-1).payload.operations.map((o) => o.op), ['set_environment'])
  // Solid areas only: the grid's thin lines are a helper and may differ once the view has been rendered.
  const clayDusk = await solidDiff(page, clayNoon, await settledShot(page, box), 6)
  assert.equal(clayDusk, 0, `clay stays: the studio light does not follow the sun (${(clayDusk * 100).toFixed(3)}% off)`)
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
  const top = await lumaStats(page, await settledShot(page, sky))
  assert.ok(top.b > top.r + 60, `the sky is the colour set (rgb ${top.r.toFixed(0)}, ${top.g.toFixed(0)}, ${top.b.toFixed(0)})`)
  // Clay draws the studio's neutral background instead.
  await shading(page, '白模')
  const studio = await lumaStats(page, await settledShot(page, sky))
  assert.ok(Math.abs(studio.r - studio.b) < 20, `clay: a neutral background, not the sky (rgb ${studio.r.toFixed(0)}, ${studio.g.toFixed(0)}, ${studio.b.toFixed(0)})`)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('AC-5: pulled far back, the procedural sky still fills the view', async () => {
  // Over 2 km out, and an orthographic view some 25 km across: the sky's box
  // used to stay 1000 across at the origin, and outside it only its far side
  // showed, a square in the dark.
  const views = { perspective: { position: [1500, -1500, 400], target: [0, 0, 0], ortho: false, zoom: 1 }, ortho: { position: [7, -9, 7], target: [0, 0, 0.5], ortho: true, zoom: 0.05 } }
  for (const [name, camera] of Object.entries(views)) {
    const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
    await context.addInitScript(([key, view]) => localStorage.setItem(key, JSON.stringify(view)), [`creative-scene-view:qa-account:${DOC}`, { camera, selection: { selected: [], active: null }, tool: 'box', toolbar: true, sidebar: true, history: false }])
    const server = new SceneServer()
    seed(server, [boxOf('post', 5, { position: [0, 0, 0] }, { width: 0.4, height: 1.5, depth: 0.4 })])
    const { page, errors } = await open(server, { context })
    const { box } = await viewportCenter(page)
    const corners = [[0, 0], [1, 0], [0, 1], [1, 1]].map(([u, v]) => ({ x: box.x + 4 + u * (box.width - 48), y: box.y + 4 + v * (box.height - 48), width: 40, height: 40 }))
    // Clay has the studio's background and no sky.
    for (const mode of ['最终渲染']) {
      await shading(page, mode)
      await shot(page, `c5-far-sky-${name}-${mode}`)
      for (const corner of corners) {
        const { r, g, b } = await lumaStats(page, await settledShot(page, corner))
        assert.ok(r + g + b > 60, `${name} ${mode}: sky at (${corner.x - box.x}, ${corner.y - box.y}), not the dark backdrop (rgb ${r.toFixed(0)}, ${g.toFixed(0)}, ${b.toFixed(0)})`)
      }
    }
    assert.deepEqual(errors, [])
    await context.close()
  }
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
  // The exposure is on the render tab (R2).
  await page.getByRole('tablist', { name: '属性' }).getByRole('tab', { name: '渲染' }).click()
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

// --- R2 AC-R2-1: the render tab ------------------------------------------------------

await check('render tab: renderer, output resolution, exposure and backend; path tracing planned; selecting shows the object and clearing goes back', async () => {
  const server = new SceneServer()
  seed(server, [boxOf('post', 5, { position: [0, 0, 0] }, { width: 0.4, height: 1.5, depth: 0.4 })])
  const { context, page, errors } = await open(server)
  const tabs = page.getByRole('tablist', { name: '属性' })
  const tab = (name) => tabs.getByRole('tab', { name, exact: true })
  const selectedTab = async () => (await tabs.getByRole('tab', { selected: true }).textContent())?.trim()
  assert.deepEqual(await tabs.getByRole('tab').allTextContents(), ['渲染', '环境', '物件'], 'Blender’s order')
  assert.equal(await selectedTab(), '环境', 'nothing selected: the environment, as before')
  assert.equal(await page.getByRole('spinbutton', { name: '曝光补偿（EV）' }).count(), 0, 'the exposure left the environment tab')

  await tab('渲染').click()
  const engine = page.getByRole('combobox', { name: '渲染器' })
  assert.equal(await engine.inputValue(), 'workbench')
  const planned = engine.locator('option', { hasText: '路径追踪 · 规划中' })
  assert.equal(await planned.count(), 1)
  assert.ok(await planned.evaluate((o) => o.disabled), 'path tracing is shown, not selectable')
  const resolution = page.getByRole('group', { name: '输出分辨率' })
  assert.deepEqual(await resolution.getByRole('button').allTextContents(), ['1K', '2K', '4K', '8K'])
  assert.equal(await resolution.getByRole('button', { name: '2K' }).getAttribute('aria-pressed'), 'true')
  await page.getByText('导出图长边 2048 px').waitFor()
  // SwiftShader has no WebGPU adapter: the WebGL2 backend.
  await waitFor(async () => (await page.locator('output[aria-label="渲染后端"]').textContent()) === '兼容模式', 'the backend shown')

  await engine.selectOption('realtime_gi')
  await waitFor(() => server.scene.render.engine === 'realtime_gi', 'renderer written')
  assert.deepEqual(server.commands.at(-1).payload.operations, [{ op: 'set_render', render: { engine: 'realtime_gi' } }], 'only the renderer is written')
  await page.getByText(/只计算画面里可见的表面/).waitFor()
  await resolution.getByRole('button', { name: '8K' }).click()
  await waitFor(() => server.scene.render.resolution === '8k', 'resolution written')
  assert.deepEqual(server.commands.at(-1).payload.operations, [{ op: 'set_render', render: { resolution: '8k' } }])
  await page.getByText('导出图长边 8192 px').waitFor()
  await page.getByText(/8K 实时 GI 的材质光照每张约需一分钟以上/).waitFor()
  await shot(page, 'r2-render-tab')
  await saved(page)
  await focusEditor(page)
  await page.keyboard.press(`${modKey}+KeyZ`)
  await waitFor(() => server.scene.render.resolution === '2k', 'undo the resolution')
  assert.equal(server.scene.render.engine, 'realtime_gi', 'one step at a time')
  await saved(page)

  // Selecting shows the object; clearing goes back to the render tab stayed on.
  await page.getByRole('treeitem', { name: /post/ }).click()
  await waitFor(async () => (await selectedTab()) === '物件', 'the object tab on selecting')
  await focusEditor(page)
  await page.keyboard.press('Alt+KeyA')
  await waitFor(async () => (await selectedTab()) === '渲染', 'back to the render tab')
  // A tab picked by hand stays while the selection holds, and clearing from it leaves it.
  await page.getByRole('treeitem', { name: /post/ }).click()
  await waitFor(async () => (await selectedTab()) === '物件', 'the object tab again')
  await tab('环境').click()
  await focusEditor(page)
  await page.keyboard.press('Alt+KeyA')
  await sleep(200)
  assert.equal(await selectedTab(), '环境')
  await tab('物件').click()
  await page.getByText('选中物件后在这里调整').waitFor()

  // A revision from the history: every tab shows, read only.
  await page.getByRole('button', { name: '历史' }).click()
  const panel = page.getByRole('complementary', { name: '历史版本' })
  await waitFor(async () => (await panel.locator('li').count()) >= 1, 'history items')
  await panel.locator('li button').last().click()
  await page.getByText(/正在预览第 1 版/).first().waitFor()
  await tab('渲染').click()
  assert.ok(await page.getByRole('combobox', { name: '渲染器' }).isDisabled(), 'read only in a preview')
  assert.ok(await page.getByRole('group', { name: '输出分辨率' }).getByRole('button', { name: '4K' }).isDisabled())
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

// --- W1 AC-W1-1: clay is the studio light, whatever the environment ------------

await check('W1 clay: the sun, the sky, fog, ambient and a shadowing lamp change neither the clay view nor clay exports; every camera is lit from the front', async () => {
  const server = new SceneServer()
  const front = objectID('front'), back = objectID('back'), lamp = objectID('lamp')
  server.external([{ op: 'add_objects', objects: [
    { ...boxObject(objectID('box'), '方块', 0, 1), primitive: { builtin: 'box@1', params: { width: 1.2, height: 1.2, depth: 1.2 } } },
    cameraObject(front, '前', 2, { focal_mm: 50 }, [0, 0.6, 6]),
    // Turned to look back along +Z, at the box's other side.
    { ...cameraObject(back, '后', 3, { focal_mm: 50 }, [0, 0.6, -6]), transform: { position: [0, 0.6, -6], rotation: [0, 1, 0, 0], scale: [1, 1, 1] } },
  ] }])
  const { context, page, errors } = await open(server, { query: '?export=1' })
  const { box } = await viewportCenter(page)
  await shading(page, '白模')
  const change = async (ops) => {
    server.external(ops)
    await page.evaluate(() => window.__invalidate())
    await sleep(600)
  }
  const environment = (patch) => [{ op: 'set_environment', environment: { ...server.scene.environment, ...patch } }]
  const exportClay = async () => {
    const n = server.exports.length
    await page.getByRole('button', { name: '导出' }).first().click()
    const dialog = page.getByRole('dialog', { name: '导出分镜' })
    await dialog.waitFor()
    await dialog.getByRole('button', { name: '导出' }).click()
    await waitFor(() => server.exports.length === n + 1, 'export submitted', 180_000)
    await dialog.waitFor({ state: 'detached' })
    return server.exports.at(-1).payload.shots.map((s) => server.uploadBytes.get(s.images.find((i) => i.pass === 'clay').upload_id))
  }
  /** Pixels off by more than 2 in a channel; 0 is the same picture. */
  const off = async (a, b) => (await compareShots(page, a, b, 2)).over

  const view = await settledShot(page, box)
  const shots = await exportClay()
  const steps = [
    ['a low sun', environment({ sky: { kind: 'procedural', sun_elevation_deg: 5, sun_azimuth_deg: 250, turbidity: 3 } })],
    ['a solid sky', environment({ sky: { kind: 'color', color: '#3355ff' } })],
    ['fog', environment({ fog: { color: '#c0c8d0', near: 2, far: 12 } })],
    ['ambient', environment({ ambient: { color: '#ffb070', intensity: 1.5 } })],
  ]
  for (const [step, ops] of steps) {
    await change(ops)
    assert.equal(await off(view, await settledShot(page, box)), 0, `${step}: the clay view is the same`)
  }
  // Not compared in the viewport, which draws the lamp's icon.
  await change([{ op: 'add_objects', objects: [{ id: lamp, kind: 'light', name: '灯', parent_id: null, order: 4, transform: { ...identity(), position: [1.5, 2, 1.5] }, visible: true, locked: false, light: { type: 'point', color: '#ffb070', intensity: 60, cast_shadow: true } }] }])
  const after = await exportClay()
  for (const [i, png] of after.entries()) assert.equal(await off(shots[i], png), 0, `camera ${i}: the clay export is the same after all of it`)

  // Both cameras see the face towards them lit: the light turns with the view.
  const [a, b] = await Promise.all(after.map((png) => lumaStats(page, png)))
  const centre = await Promise.all(after.map((png) => page.evaluate(async (src) => {
    const i = new Image()
    await new Promise((resolve) => { i.onload = resolve; i.src = src })
    const c = document.createElement('canvas')
    c.width = i.width
    c.height = i.height
    const g = c.getContext('2d')
    g.drawImage(i, 0, 0)
    const p = g.getImageData(i.width >> 1, i.height >> 1, 1, 1).data
    return 0.2126 * p[0] + 0.7152 * p[1] + 0.0722 * p[2]
  }, `data:image/png;base64,${png.toString('base64')}`)))
  console.log(`clay box face, front ${centre[0].toFixed(0)}, back ${centre[1].toFixed(0)}; pictures ${a.mean.toFixed(0)}, ${b.mean.toFixed(0)}`)
  assert.ok(Math.abs(centre[0] - centre[1]) < 6, `the box faces both cameras alike: ${centre.map((v) => v.toFixed(0))}`)
  assert.ok(centre[0] > a.mean, 'the face towards the camera is lit')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('W1 environment tab: outside the rendered view it says the environment shows there, and still edits', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  await page.getByRole('tablist', { name: '属性' }).getByRole('tab', { name: '环境' }).click()
  const tab = page.getByRole('tabpanel', { name: '环境' })
  const hint = tab.getByText('环境在「最终渲染」里生效', { exact: true })
  for (const mode of ['白模', '材质预览', '线框']) {
    await shading(page, mode)
    await hint.waitFor()
  }
  await shading(page, '最终渲染')
  await hint.waitFor({ state: 'detached' })
  await shading(page, '白模')
  const elevation = tab.getByRole('spinbutton', { name: '太阳高度（°）' })
  await elevation.fill('20')
  await elevation.press('Enter')
  await waitFor(() => server.scene.environment.sky.sun_elevation_deg === 20, 'edited in clay')
  await saved(page)
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

/** The calibration scenes' mean brightness before W1 (ACES Filmic, develop f48841a3, SwiftShader): AgX keeps each within 10% (AC-W1-3). */
const calibrationBeforeW1 = { noon: { all: 195, ground: 199 }, dusk: { all: 150, ground: 165 }, room: { all: 64, ground: 58 }, materials: { all: 183, ground: 191 } }

await check('calibration: noon at EV 0, dusk at +2, a room lit by two 40 cd lamps at 0 and the material board render in range', async () => {
  const cases = [
    { name: 'noon', ev: 0, setup: (s) => yard(s, { kind: 'procedural', sun_elevation_deg: 55, sun_azimuth_deg: 135, turbidity: 3 }) },
    { name: 'dusk', ev: 2, setup: (s) => yard(s, { kind: 'procedural', sun_elevation_deg: 5, sun_azimuth_deg: 135, turbidity: 3 }) },
    { name: 'room', ev: 0, setup: (s) => room(s, 40) },
    // R0's material board: 15 presets under a 45° procedural sky (scripts/fixtures/scene-reference).
    { name: 'materials', ev: 0, setup: (s) => referenceScene(s, 'materials') },
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
  const { noon, dusk, room: lamps, materials } = seen
  for (const [name, before] of Object.entries(calibrationBeforeW1)) {
    for (const part of ['all', 'ground']) {
      const change = seen[name][part].mean / before[part] - 1
      assert.ok(Math.abs(change) <= 0.1, `${name} ${part}: ${seen[name][part].mean.toFixed(0)} against ${before[part]} before W1 (${(change * 100).toFixed(1)}%)`)
    }
  }
  assert.ok(noon.ground.mean > 175 && noon.ground.mean < 220, 'sunlit light ground is bright, not blown')
  assert.ok(Math.abs(noon.ground.r - noon.ground.b) < 12, 'daylight is neutral')
  assert.ok(noon.all.bright < 0.01 && dusk.all.bright < 0.01, 'nothing clips outdoors')
  assert.ok(dusk.ground.mean > 130 && dusk.ground.mean < 200, 'dusk at +2 is a little darker than noon')
  assert.ok(dusk.ground.r - dusk.ground.b > 15, 'dusk light is warm')
  assert.ok(lamps.all.mean > 40 && lamps.all.mean < 110 && lamps.all.bright < 0.02, 'the lamps light the room without blowing it out')
  assert.ok(materials.all.bright < 0.01, 'the material board does not clip')
})

// --- WebGPU renderer R0: the reference scenes and their WebGL baseline -----------

const referenceNames = ['noon', 'dusk', 'room', 'materials', 'dof', 'cornell']

/** The editor's saved view (viewState.ts), orthographic from front right above, in view space (Z up). */
const obliqueOrtho = {
  camera: { position: [7, -9, 7], target: [0, 0, 0.5], ortho: true, zoom: 80 },
  selection: { selected: [], active: null }, tool: 'box', toolbar: true, sidebar: true, history: false,
}

/** A reference scene (scripts/fixtures/scene-reference) in place of the server's scene, at revision 1; edit changes it first. */
function referenceScene(server, name, edit) {
  server.scene = JSON.parse(readFileSync(new URL(`fixtures/scene-reference/${name}.json`, import.meta.url), 'utf8'))
  // The exposure the server reads documents from before W2 and W3 with (backend model.go): the default camera at 15 and
  // ambient metering, a stored camera by hand (Epic revision v5).
  server.scene.render.exposure_ev100 ??= 15
  server.scene.render.exposure_mode ??= 'ambient'
  server.scene.render.exposure_compensation ??= 0
  for (const o of server.scene.objects) if (o.camera) Object.assign(o.camera, { exposure_mode: 'manual', exposure_compensation: 0, ...o.camera })
  edit?.(server.scene)
  server.unitRevs = { meta: '1', environment: '1', render: '1', objects: '1' }
  for (const o of server.scene.objects) {
    server.unitRevs[placement(o.id)] = '1'
    server.unitRevs[content(o.id)] = '1'
  }
  server.history[0] = server.record(['*'], [], null)
}

await check('reference scenes: each opens and draws in clay and rendered without errors', async () => {
  for (const name of referenceNames) {
    const server = new SceneServer()
    referenceScene(server, name)
    const { context, page, errors } = await open(server)
    const { box } = await viewportCenter(page)
    await shading(page, '白模')
    const clay = await settledShot(page, box)
    await shading(page, '最终渲染')
    const lit = await settledShot(page, box)
    assert.ok((await pixelDiff(page, clay, lit)) > 0.05, `${name}: the rendered view differs from clay`)
    assert.deepEqual(errors, [], `${name}: ${errors.join('\n')}`)
    await context.close()
  }
})

const metal = ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu']

/**
 * A reference scene exported in all three passes from the rendered view,
 * then the saved orthographic view in clay, on gpu (a Chromium on this
 * machine's GPU); forceWebGL draws the export through the WebGL2 backend.
 */
async function exportReference(gpu, name, { forceWebGL = false, forceCompatibility = false, edit } = {}) {
  const context = await gpu.newContext({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 })
  await context.addInitScript(([key, view]) => localStorage.setItem(key, JSON.stringify(view)), [`creative-scene-view:qa-account:${DOC}`, obliqueOrtho])
  const server = new SceneServer()
  referenceScene(server, name, edit)
  const backends = []
  context.on('console', (m) => m.text().startsWith('scene renderer') && backends.push(m.text()))
  const { page, errors } = await open(server, { context, query: '?export=1' })
  if (forceWebGL) await page.evaluate(() => { window.__sceneGPU.forceWebGL = true })
  if (forceCompatibility) await page.evaluate(() => { window.__sceneGPU.forceCompatibility = true })
  const renderer = await page.evaluate(() => {
    const gl = document.createElement('canvas').getContext('webgl2')
    return gl.getParameter(gl.getExtension('WEBGL_debug_renderer_info').UNMASKED_RENDERER_WEBGL)
  })
  await shading(page, '最终渲染')
  await page.getByRole('button', { name: '导出' }).first().click()
  const dialog = page.getByRole('dialog', { name: '导出分镜' })
  await dialog.waitFor()
  for (const pass of ['白模（clay）', '线稿', '材质光照']) assert.ok(await dialog.getByLabel(pass).isChecked(), `${name}: ${pass} ticked`)
  const started = Date.now()
  await dialog.getByRole('button', { name: '导出' }).click()
  await waitFor(() => server.exports.length === 1, `${name}: export submitted`, 600_000)
  const exportMs = Date.now() - started
  await dialog.waitFor({ state: 'detached' })
  const cameras = new Map(server.scene.objects.map((o) => [o.id, o.name]))
  const images = []
  for (const s of server.exports[0].payload.shots) {
    for (const img of s.images) images.push({ file: `${name}-${cameras.get(s.camera_object_id)}-${img.pass}.png`, pass: img.pass, png: server.uploadBytes.get(img.upload_id) })
  }
  // The saved orthographic view from front right above, in clay: the AO the orthographic GTAO must keep.
  await shading(page, '白模')
  const { x, y, box } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await sleep(600)
  const ortho = { file: `${name}-ortho-clay.png`, pass: 'clay', png: await settledShot(page, box) }
  assert.deepEqual(errors, [], `${name}: ${errors.join('\n')}`)
  await context.close()
  return { renderer, exportMs, images, ortho, backends }
}

// SCENE_E2E_BASELINE=<dir>: every reference scene exported in all three passes
// on this machine's GPU, an orthographic clay view of each, and baseline.json
// with the machine, the versions and the export times. The AC-9 case below
// adds perf.json to the same directory. Run it before the renderer changes.
if (process.env.SCENE_E2E_BASELINE) await check('baseline: the reference scenes exported on this machine’s GPU', async () => {
  const dir = process.env.SCENE_E2E_BASELINE
  const gpu = await chromium.launch({ headless: true, args: metal })
  try {
    const record = { machine: machineInfo(), chromium: gpu.version(), viewport: '1600 × 1000, 2× pixels', scenes: {} }
    for (const name of referenceNames) {
      const out = await exportReference(gpu, name)
      record.renderer ??= out.renderer
      const images = []
      for (const img of [...out.images, out.ortho]) {
        await writeFile(`${dir}/${img.file}`, img.png)
        if (img !== out.ortho) images.push({ file: img.file, ...pngSize(img.png), bytes: img.png.length })
      }
      record.scenes[name] = { export_ms: out.exportMs, images }
      console.log(`baseline ${name}: ${images.length} images in ${out.exportMs} ms`)
    }
    await writeFile(`${dir}/baseline.json`, `${JSON.stringify(record, null, 2)}\n`)
  } finally {
    await gpu.close()
  }
})

// --- W1 scene-display-look: the looks it changes and the ones it keeps ----------

const displayModes = [['线框', 'wireframe'], ['白模', 'clay'], ['材质预览', 'preview'], ['最终渲染', 'lit']]

/** A reference scene's viewport from the editor's default view in each of modes ([label, file name]), on gpu at 1600 × 1000, 2× pixels. */
async function viewportLooks(gpu, name, modes = displayModes, edit) {
  const context = await gpu.newContext({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 })
  const server = new SceneServer()
  referenceScene(server, name, edit)
  const { page, errors } = await open(server, { context })
  const { box } = await viewportCenter(page)
  const ground = { x: box.x, y: box.y + box.height * 0.75, width: box.width, height: box.height * 0.25 }
  await page.mouse.move(1, 1)
  const out = []
  for (const [mode, label] of modes) {
    await shading(page, mode)
    // New lights stay dark until compiled for the viewport's passes (objects/SceneLights.tsx).
    await waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiling)) === 0, `${name} ${mode}: compiled`, 60_000)
    const png = await settledShot(page, box)
    out.push({ label, file: `${name}-view-${label}.png`, png, all: await lumaStats(page, png, { level: 250 }), ground: await lumaStats(page, await settledShot(page, ground), { level: 250 }) })
  }
  assert.deepEqual(errors, [], `${name}: ${errors.join('\n')}`)
  await context.close()
  return out
}

const w1Baseline = new URL('../../.lti/features/2026-10-22-scene-world-environment/features/2026-10-22-scene-display-look/discovery/research/baseline/', import.meta.url)

// SCENE_E2E_LOOKS=<dir>: every reference scene's viewport in the four display
// modes on this machine's GPU, with looks.json measuring each picture and its
// lower quarter (ground). Run once before W1's renderer changes for the
// baseline (w1Baseline) and again after them.
if (process.env.SCENE_E2E_LOOKS) await check('looks: the reference scenes in every display mode on this machine’s GPU', async () => {
  const dir = process.env.SCENE_E2E_LOOKS
  const gpu = await chromium.launch({ headless: true, args: metal })
  try {
    const record = { machine: machineInfo(), chromium: gpu.version(), viewport: '1600 × 1000, 2× pixels', scenes: {} }
    const round = (s) => Object.fromEntries(Object.entries(s).map(([k, v]) => [k, Number(v.toFixed(4))]))
    for (const name of referenceNames) {
      record.scenes[name] = {}
      for (const look of await viewportLooks(gpu, name)) {
        await writeFile(`${dir}/${look.file}`, look.png)
        record.scenes[name][look.label] = { all: round(look.all), ground: round(look.ground) }
        console.log(`looks ${name} ${look.label}: mean ${look.all.mean.toFixed(1)}, clipped ${(look.all.bright * 100).toFixed(2)}%, ground ${look.ground.mean.toFixed(1)} (R−B ${(look.ground.r - look.ground.b).toFixed(1)})`)
      }
    }
    await writeFile(`${dir}/looks.json`, `${JSON.stringify(record, null, 2)}\n`)
  } finally {
    await gpu.close()
  }
})

// SCENE_E2E_W1=<dir> (AC-W1-3): wireframe and line art are what they were
// before W1, pixel for pixel, on this machine's GPU; sheets of any image
// that is not go to the directory.
if (process.env.SCENE_E2E_W1) await check('W1: wireframe and line art are unchanged on this machine’s GPU', async () => {
  const dir = process.env.SCENE_E2E_W1
  const gpu = await chromium.launch({ headless: true, args: metal })
  const off = []
  try {
    const page = await (await gpu.newContext()).newPage()
    const same = async (file, png) => {
      const m = await compareShots(page, readFileSync(new URL(file, w1Baseline)), png, 2)
      if (m.error || m.over > 0) {
        off.push(`${file}: ${m.error ?? `${m.over.toFixed(4)}% of pixels off by more than 2`}`)
        if (m.sheet) await writeFile(`${dir}/${file.replace('.png', '')}.jpg`, m.sheet)
      }
    }
    for (const name of referenceNames) {
      for (const look of await viewportLooks(gpu, name, [['线框', 'wireframe']])) await same(look.file, look.png)
      for (const img of (await exportReference(gpu, name)).images) if (img.pass === 'line') await same(img.file, img.png)
    }
  } finally {
    await gpu.close()
  }
  assert.deepEqual(off, [])
})

// --- R1 AC-R1-1 / AC-R1-4: the same pictures as the WebGL baseline --------------

const r0Baseline = new URL('../../.lti/features/2026-10-20-scene-webgpu-renderer/features/2026-10-21-scene-webgpu-spike/discovery/research/baseline/', import.meta.url)
// W1 (scene-display-look) changed clay (the studio light) and lit (AgX): their references since are W1's
// exports on WebGPU; line art did not change and stays against R0's.
const w1After = new URL('../../.lti/features/2026-10-22-scene-world-environment/features/2026-10-22-scene-display-look/discovery/research/after/', import.meta.url)
const referenceOf = (img) => (img.pass === 'line' ? { dir: r0Baseline, against: 'r0-webgl' } : { dir: w1After, against: 'w1-webgpu' })

/**
 * The thresholds of AC-R1-1, tightened after the first full run to 1.5× the
 * largest difference measured among the images within the Epic's limits
 * (line ≤ 0.1 / 0.05 %, clay and lit ≤ 1.5 / 1 %), never above them. An
 * image over its threshold goes to the owner; the threshold is not raised.
 */
const parityLimits = { line: { mean: 0.0016, over: 0.0026 }, clay: { mean: 1.5, over: 0.62 }, lit: { mean: 0.63, over: 0.18 } }

/** Mean difference per channel (0–255), the share of pixels with a channel off by more than limit (%), and a + b + difference (×8) side by side as a JPEG. */
async function compareShots(page, a, b, limit = 8) {
  const out = await page.evaluate(async ([a, b, limit]) => {
    const load = (src) => new Promise((resolve) => { const i = new Image(); i.onload = () => resolve(i); i.src = src })
    const [x, y] = await Promise.all([load(a), load(b)])
    const w = x.width
    const h = x.height
    if (y.width !== w || y.height !== h) return { error: `${w}×${h} against ${y.width}×${y.height}` }
    const pixels = (img) => {
      const c = document.createElement('canvas')
      c.width = w
      c.height = h
      const g = c.getContext('2d')
      g.drawImage(img, 0, 0)
      return g.getImageData(0, 0, w, h)
    }
    const p = pixels(x).data
    const q = pixels(y).data
    const diff = new ImageData(w, h)
    let sum = 0
    let over = 0
    for (let k = 0; k < p.length; k += 4) {
      let most = 0
      for (let c = 0; c < 3; c++) {
        const d = Math.abs(p[k + c] - q[k + c])
        sum += d
        most = Math.max(most, d)
        diff.data[k + c] = Math.min(255, d * 8)
      }
      diff.data[k + 3] = 255
      if (most > limit) over++
    }
    const sheet = document.createElement('canvas')
    const scale = 0.5
    sheet.width = Math.round(w * scale) * 3
    sheet.height = Math.round(h * scale)
    const g = sheet.getContext('2d')
    const d = document.createElement('canvas')
    d.width = w
    d.height = h
    d.getContext('2d').putImageData(diff, 0, 0)
    for (const [i, img] of [x, y, d].entries()) g.drawImage(img, i * Math.round(w * scale), 0, Math.round(w * scale), Math.round(h * scale))
    return { mean: sum / ((p.length / 4) * 3), over: (over / (p.length / 4)) * 100, sheet: sheet.toDataURL('image/jpeg', 0.85).split(',')[1] }
  }, [`data:image/png;base64,${a.toString('base64')}`, `data:image/png;base64,${b.toString('base64')}`, limit])
  return out.error ? out : { ...out, sheet: Buffer.from(out.sheet, 'base64') }
}

// SCENE_E2E_GPU=<dir>: the reference scenes on this machine's GPU against the
// R0 baseline (AC-R1-1), then the export once more through the WebGL2 backend
// against the WebGPU one (AC-R1-4); side-by-side sheets and parity.json go to
// the directory, and every image must be within its threshold.
if (process.env.SCENE_E2E_GPU) await check('parity: the reference scenes match the WebGL baseline, and the WebGL2 backend matches WebGPU', async () => {
  const dir = process.env.SCENE_E2E_GPU
  const gpu = await chromium.launch({ headless: true, args: metal })
  const rows = []
  const over = []
  const exports = {}
  const record = (against, file, pass, m) => {
    const limit = parityLimits[pass]
    const ok = !m.error && m.mean <= limit.mean && m.over <= limit.over
    rows.push({ against, file, mean: m.error ? null : Number(m.mean.toFixed(4)), over_percent: m.error ? null : Number(m.over.toFixed(4)), ok, ...(m.error ? { error: m.error } : {}) })
    if (!ok) over.push(`${against} ${file}: ${m.error ?? `mean ${m.mean.toFixed(3)}, > 8 ${m.over.toFixed(3)}% (limit ${limit.mean} / ${limit.over}%)`}`)
  }
  try {
    let page = null
    for (const name of referenceNames) {
      const webgpu = await exportReference(gpu, name)
      assert.ok(webgpu.backends.some((b) => b === 'scene renderer (viewport): webgpu'), `${name}: the viewport on WebGPU: ${webgpu.backends}`)
      assert.ok(webgpu.backends.some((b) => b === 'scene renderer (offscreen): webgpu'), `${name}: the export on WebGPU: ${webgpu.backends}`)
      page ??= await (await gpu.newContext()).newPage()
      for (const img of [...webgpu.images, webgpu.ortho]) {
        const reference = referenceOf(img)
        const m = await compareShots(page, readFileSync(new URL(img.file, reference.dir)), img.png)
        if (m.sheet) await writeFile(`${dir}/${img.file.replace('.png', '')}.jpg`, m.sheet)
        record(reference.against, img.file, img.pass, m)
      }
      const webgl = await exportReference(gpu, name, { forceWebGL: true })
      exports[name] = { webgpu_ms: webgpu.exportMs, webgl2_ms: webgl.exportMs }
      assert.ok(webgl.backends.some((b) => b === 'scene renderer (offscreen): webgl2'), `${name}: the export on WebGL2: ${webgl.backends}`)
      for (const img of webgl.images) {
        const m = await compareShots(page, webgpu.images.find((i) => i.file === img.file).png, img.png)
        if (m.sheet) await writeFile(`${dir}/${img.file.replace('.png', '')}-webgl2.jpg`, m.sheet)
        record('webgpu', img.file, img.pass, m)
      }
      console.log(`parity ${name}: ${rows.filter((r) => r.file.startsWith(`${name}-`) && r.ok).length}/${rows.filter((r) => r.file.startsWith(`${name}-`)).length} within`)
    }
    // AC-R1-8: no compatibility-mode device here; the workbench's branch for one (no MSAA, SMAA) against the MSAA picture, for the record.
    const compatibility = []
    for (const name of ['noon', 'materials']) {
      const msaa = await exportReference(gpu, name)
      const smaa = await exportReference(gpu, name, { forceCompatibility: true })
      for (const img of smaa.images) {
        if (img.pass === 'line') continue
        const m = await compareShots(page, msaa.images.find((i) => i.file === img.file).png, img.png)
        if (m.sheet) await writeFile(`${dir}/${img.file.replace('.png', '')}-smaa.jpg`, m.sheet)
        compatibility.push({ file: img.file, mean: Number(m.mean.toFixed(4)), over_percent: Number(m.over.toFixed(4)) })
      }
    }
    await writeFile(`${dir}/parity.json`, `${JSON.stringify({ machine: machineInfo(), chromium: gpu.version(), limits: parityLimits, exports, rows, compatibility }, null, 2)}\n`)
  } finally {
    await gpu.close()
  }
  assert.deepEqual(over, [], 'over the threshold: for the owner to look at')
})

// SCENE_E2E_WORKBENCH=<dir> (R2 AC-R2-3, last part): with the scenes' renderer the workbench, the reference scenes
// export as R1 drew them, within the frozen threshold (Epic AC-1: mean ≤ 0.05, no pixel off by more than 8).
// R1 kept no pictures, so its references are drawn first by R1's code on the same machine and browser:
// SCENE_E2E_WORKBENCH_WRITE=1 against a Vite server of develop writes them to <dir>; without it, this branch's
// exports are compared with them, and a sheet of each image over the threshold goes to <dir>/over.
if (process.env.SCENE_E2E_WORKBENCH) await check('workbench: the reference scenes export as R1 drew them', async () => {
  const dir = process.env.SCENE_E2E_WORKBENCH
  const gpu = await chromium.launch({ headless: true, args: metal })
  const rows = []
  try {
    let page = null
    for (const name of referenceNames) {
      const out = await exportReference(gpu, name)
      for (const img of [...out.images, out.ortho]) {
        if (process.env.SCENE_E2E_WORKBENCH_WRITE) {
          await writeFile(`${dir}/${img.file}`, img.png)
          continue
        }
        page ??= await (await gpu.newContext()).newPage()
        const m = await compareShots(page, readFileSync(`${dir}/${img.file}`), img.png)
        const ok = !m.error && m.mean <= 0.05 && m.over === 0
        if (!ok && m.sheet) await writeFile(`${dir}/over/${img.file.replace('.png', '')}.jpg`, m.sheet)
        rows.push({ file: img.file, mean: m.error ? null : Number(m.mean.toFixed(4)), over_percent: m.error ? null : Number(m.over.toFixed(4)), ok, ...(m.error ? { error: m.error } : {}) })
      }
      console.log(`workbench ${name}: ${out.images.length + 1} images`)
    }
    if (!process.env.SCENE_E2E_WORKBENCH_WRITE) await writeFile(`${dir}/workbench.json`, `${JSON.stringify({ machine: machineInfo(), chromium: gpu.version(), rows }, null, 2)}\n`)
  } finally {
    await gpu.close()
  }
  assert.deepEqual(rows.filter((r) => !r.ok), [])
})

// SCENE_E2E_LOOK=<dir>: the four display modes and an orthographic clay view
// of three reference scenes on this machine's GPU, the ball and the ground
// selected and the move gizmo up, the grid and the axis widget showing. Run
// on both sides of the WebGPU migration and compared by eye (R1 AC-R1-6).
const lookView = (ortho) => ({
  camera: ortho ? { position: [7, -9, 7], target: [0, 0, 0.5], ortho: true, zoom: 80 } : { position: [6, -8, 4], target: [0, 0, 0.6], ortho: false },
  selection: { selected: ['sobj_ball00000000000000000', 'sobj_ground'], active: 'sobj_ball00000000000000000' },
  tool: 'move', toolbar: true, sidebar: true, history: false,
})
if (process.env.SCENE_E2E_LOOK) await check('look: display modes and editor helpers on this machine’s GPU', async () => {
  const dir = process.env.SCENE_E2E_LOOK
  const gpu = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
  try {
    for (const name of ['noon', 'materials', 'room']) {
      for (const ortho of [false, true]) {
        const context = await gpu.newContext({ viewport: { width: 1280, height: 820 }, deviceScaleFactor: 2 })
        await context.addInitScript(([key, view]) => localStorage.setItem(key, JSON.stringify(view)), [`creative-scene-view:qa-account:${DOC}`, lookView(ortho)])
        const server = new SceneServer()
        referenceScene(server, name)
        // The ball of the noon yard; the other scenes get one in front.
        if (!server.scene.objects.some((o) => o.id === 'sobj_ball00000000000000000'))
          seed(server, [{ id: 'sobj_ball00000000000000000', kind: 'primitive', name: 'ball', order: 99, transform: { ...identity(), position: [0, 0, 1.5] }, primitive: { builtin: 'sphere@1', params: { radius: 0.4 } } }])
        const { page, errors } = await open(server, { context })
        const { box } = await viewportCenter(page)
        for (const mode of ortho ? ['白模'] : ['线框', '白模', '材质预览', '最终渲染']) {
          await shading(page, mode)
          // New lights stay dark until compiled for the viewport's passes (objects/SceneLights.tsx).
          await waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiling)) === 0, `${name} ${mode}: compiled`, 60_000)
          await writeFile(`${dir}/${name}-${ortho ? 'ortho' : 'persp'}-${mode}.png`, await settledShot(page, box))
        }
        assert.deepEqual(errors, [], `${name}: ${errors.join('\n')}`)
        await context.close()
      }
    }
  } finally {
    await gpu.close()
  }
})

// --- W2 scene-physical-sky: the other skies stay as they were ---------------------

const w2Baseline = new URL('../../.lti/features/2026-10-22-scene-world-environment/features/2026-10-22-scene-physical-sky/discovery/research/baseline/', import.meta.url)

/**
 * What W1's pictures (w1After) leave out, made from reference scenes: global
 * fog under the procedural and the solid sky, and a camera looking into the
 * procedural sun through depth of field, which the material output clamp must
 * leave alone.
 */
const w2Variants = {
  'noon-fog': {
    from: 'noon',
    edit(scene) {
      scene.environment.fog = { color: '#c8d0d8', near: 4, far: 40 }
    },
  },
  // Without fog: the fog covers the sky, sun and all.
  'noon-sun': {
    from: 'noon',
    edit(scene) {
      const sky = scene.environment.sky
      const facing = new Quaternion().setFromEuler(new Euler((sky.sun_elevation_deg * Math.PI) / 180, (sky.sun_azimuth_deg * Math.PI) / 180 + Math.PI, 0, 'YXZ'))
      scene.objects.push({
        parent_id: null, visible: true, locked: false,
        transform: { position: [0, 1.6, 4], rotation: facing.toArray(), scale: [1, 1, 1] },
        id: 'sobj_sun000000000000000000', kind: 'camera', name: 'sun', order: scene.objects.length,
        camera: { focal_mm: 35, depth_of_field: true, f_stop: 1.4, focus: 2 },
      })
    },
  },
  'room-fog': {
    from: 'room',
    edit(scene) {
      scene.environment.fog = { color: '#202428', near: 1, far: 12 }
    },
  },
}

/** The lit pictures of a scene that W2 must not change: its exports' lit images and the rendered viewport. */
async function litPictures(gpu, name, edit) {
  const pictures = (await exportReference(gpu, name, { edit })).images.filter((img) => img.pass === 'lit')
  const [view] = await viewportLooks(gpu, name, [['最终渲染', 'lit']], edit)
  return [...pictures, view]
}

// SCENE_E2E_W2_BASELINE=<dir>: the variants' lit pictures on this machine's
// GPU, taken before W2 changes the renderer (w2Baseline).
if (process.env.SCENE_E2E_W2_BASELINE) await check('W2 baseline: the variants’ lit pictures on this machine’s GPU', async () => {
  const dir = process.env.SCENE_E2E_W2_BASELINE
  const gpu = await chromium.launch({ headless: true, args: metal })
  try {
    const record = { machine: machineInfo(), chromium: gpu.version(), viewport: '1600 × 1000, 2× pixels', variants: {} }
    for (const [name, { from, edit }] of Object.entries(w2Variants)) {
      const files = []
      for (const img of await litPictures(gpu, from, edit)) {
        const file = img.file.replace(from, name)
        await writeFile(`${dir}/${file}`, img.png)
        files.push(file)
      }
      record.variants[name] = { from, files }
      console.log(`W2 baseline ${name}: ${files.join(', ')}`)
    }
    await writeFile(`${dir}/baseline.json`, `${JSON.stringify(record, null, 2)}\n`)
  } finally {
    await gpu.close()
  }
})

// SCENE_E2E_W2_SKIES=<dir> (AC-W2-6): under the procedural and the solid sky,
// lit exports (render_view draws through the same shoot) and the rendered
// viewport are what they were before W2, pixel for pixel, on this machine's
// GPU: the reference scenes against W1's pictures, the variants against
// w2Baseline. Sheets of any image that is not go to the directory.
if (process.env.SCENE_E2E_W2_SKIES) await check('W2 other skies: lit pictures are unchanged on this machine’s GPU', async () => {
  const dir = process.env.SCENE_E2E_W2_SKIES
  const gpu = await chromium.launch({ headless: true, args: metal })
  const off = []
  try {
    const page = await (await gpu.newContext()).newPage()
    const same = async (base, file, png) => {
      const m = await compareShots(page, readFileSync(new URL(file, base)), png, 2)
      if (m.error || m.over > 0) {
        off.push(`${file}: ${m.error ?? `${m.over.toFixed(4)}% of pixels off by more than 2`}`)
        if (m.sheet) await writeFile(`${dir}/${file.replace('.png', '')}.jpg`, m.sheet)
      }
    }
    for (const name of referenceNames) for (const img of await litPictures(gpu, name)) await same(w1After, img.file, img.png)
    for (const [name, { from, edit }] of Object.entries(w2Variants)) {
      for (const img of await litPictures(gpu, from, edit)) await same(w2Baseline, img.file.replace(from, name), img.png)
    }
  } finally {
    await gpu.close()
  }
  assert.deepEqual(off, [])
})

/**
 * The physical sky's LUTs drawn on the GPU against cpu.ts at the same
 * texels, in the page: two implementations of one atmosphere that share
 * only constants.ts. Errors are relative, over the channels whose value is
 * above floor; WebGL2 reads rows back flipped, so both orders are tried.
 */
async function atmosphereDrift(page, preset, sunElevationDeg) {
  return page.evaluate(async ([preset, sunElevationDeg]) => {
    const { createRenderer } = await import('/src/creative-scene/render/gpu.ts')
    const { atmosphereLuts } = await import('/src/creative-scene/world/atmosphere/luts.ts')
    const { CpuAtmosphere, transmittanceParams, skyViewParams } = await import('/src/creative-scene/world/atmosphere/cpu.ts')
    const { PRESETS } = await import('/src/creative-scene/world/atmosphere/constants.ts')
    const half = (h) => {
      const e = (h & 0x7c00) >> 10
      const f = h & 0x03ff
      const sign = h & 0x8000 ? -1 : 1
      if (e === 0) return sign * 2 ** -14 * (f / 1024)
      if (e === 0x1f) return f ? NaN : sign * Infinity
      return sign * 2 ** (e - 15) * (1 + f / 1024)
    }
    const gpu = await createRenderer({ canvas: document.createElement('canvas'), output: 'direct', role: 'offscreen' })
    const params = PRESETS[preset]
    const luts = atmosphereLuts()
    const sunMu = Math.sin((sunElevationDeg * Math.PI) / 180)
    luts.setAtmosphere(params)
    luts.setSun(sunMu, [1, 1, 1])
    luts.renderAtmosphere(gpu.renderer)
    luts.renderSkyView(gpu.renderer)
    const read = async (target) => {
      const raw = await gpu.renderer.readRenderTargetPixelsAsync(target, 0, 0, target.width, target.height)
      return raw instanceof Uint16Array ? Float32Array.from(raw, half) : Float32Array.from(raw)
    }
    const cpu = new CpuAtmosphere(params)
    /** Errors at an n × n grid of texels: cpuAt(u, v, out) is the CPU's value at a texel centre. */
    const compare = (data, target, n, floor, cpuAt) => {
      const { width: w, height: h } = target
      const out = new Float64Array(3)
      const stats = (flip) => {
        let sum = 0, count = 0, max = 0, nonFinite = 0
        for (let j = 0; j < n; j++) {
          for (let i = 0; i < n; i++) {
            const x = Math.floor(((i + 0.5) / n) * w)
            const y = Math.floor(((j + 0.5) / n) * h)
            cpuAt((x + 0.5) / w, (y + 0.5) / h, out)
            const row = flip ? h - 1 - y : y
            for (let c = 0; c < 3; c++) {
              const g = data[(row * w + x) * 4 + c]
              if (!Number.isFinite(g)) nonFinite++
              else if (Math.abs(out[c]) > floor) {
                const rel = Math.abs(g - out[c]) / Math.abs(out[c])
                sum += rel
                count++
                max = Math.max(max, rel)
              }
            }
          }
        }
        return { mean: sum / Math.max(1, count), max, count, nonFinite }
      }
      const a = stats(false)
      const b = stats(true)
      return a.mean <= b.mean ? { ...a, flipped: false } : { ...b, flipped: true }
    }
    const sun = [Math.sqrt(1 - sunMu * sunMu), sunMu, 0]
    const result = {
      backend: gpu.backend,
      transmittance: compare(await read(luts.transmittance), luts.transmittance, 12, 1e-4, (u, v, out) => {
        const { r, mu } = transmittanceParams(u, v)
        cpu.transmittance(r, mu, out)
      }),
      multiScattering: compare(await read(luts.multiScattering), luts.multiScattering, 6, 1e-6, (u, v, out) => cpu.multiScatteringTexel(u, v, out)),
      skyView: compare(await read(luts.skyView), luts.skyView, 10, 1e-6, (u, v, out) => {
        const { viewMu, lightCos } = skyViewParams(u, v, cpu.view, cpu.ground)
        const s = Math.sqrt(Math.max(0, 1 - viewMu * viewMu))
        cpu.skyRadiance([s * lightCos, viewMu, s * Math.sqrt(Math.max(0, 1 - lightCos * lightCos))], sun, out)
      }),
    }
    // The sky view as the sky draws it (sampleSkyView), toward directions across the sky and the ground:
    // elevation from −10° to 60° down the rows, azimuth from the sun's side to the far one across.
    // three as the app has it: the page cannot import a bare name, and a second copy would not share its renderer's classes.
    const lutsSource = await (await fetch('/src/creative-scene/world/atmosphere/luts.ts')).text()
    const dep = (name) => import(lutsSource.match(new RegExp(`from "(/node_modules/\\.vite/deps/${name}\\.js[^"]*)"`))[1])
    const { FloatType } = await dep('three')
    const tsl = await dep('three_tsl')
    const { QuadMesh, NodeMaterial, RenderTarget } = await dep('three_webgpu')
    const cols = 8, rows = 16
    const elevation = (j) => -10 + (70 * j) / (rows - 1)
    const azimuth = (i) => (180 * i) / (cols - 1)
    const target = new RenderTarget(cols, rows, { type: FloatType, depthBuffer: false })
    const material = new NodeMaterial()
    material.fragmentNode = tsl.Fn(() => {
      const p = tsl.uv()
      const el = tsl.floor(p.y.mul(rows)).mul(70 / (rows - 1)).sub(10).mul(Math.PI / 180)
      const az = tsl.floor(p.x.mul(cols)).mul(180 / (cols - 1)).mul(Math.PI / 180)
      const d = tsl.vec3(tsl.cos(el).mul(tsl.cos(az)), tsl.sin(el), tsl.cos(el).mul(tsl.sin(az)))
      return tsl.vec4(luts.sampleSkyView(d, tsl.vec3(...sun)), 1)
    })()
    const quad = new QuadMesh(material)
    gpu.renderer.setRenderTarget(target)
    quad.render(gpu.renderer)
    gpu.renderer.setRenderTarget(null)
    {
      const data = await read(target)
      const out = new Float64Array(3)
      const stats = (flip) => {
        let sum = 0, count = 0, max = 0, nonFinite = 0
        for (let j = 0; j < rows; j++) {
          for (let i = 0; i < cols; i++) {
            const el = (elevation(j) * Math.PI) / 180
            const az = (azimuth(i) * Math.PI) / 180
            cpu.skyRadiance([Math.cos(el) * Math.cos(az), Math.sin(el), Math.cos(el) * Math.sin(az)], sun, out)
            const row = flip ? rows - 1 - j : j
            for (let c = 0; c < 3; c++) {
              const g = data[(row * cols + i) * 4 + c]
              if (!Number.isFinite(g)) nonFinite++
              else if (Math.abs(out[c]) > 1e-6) {
                const rel = Math.abs(g - out[c]) / Math.abs(out[c])
                sum += rel
                count++
                max = Math.max(max, rel)
              }
            }
          }
        }
        return { mean: sum / Math.max(1, count), max, count, nonFinite }
      }
      const a = stats(false)
      const b = stats(true)
      result.skyDirections = a.mean <= b.mean ? { ...a, flipped: false } : { ...b, flipped: true }
    }
    target.dispose()
    material.dispose()
    luts.dispose()
    gpu.dispose()
    return result
  }, [preset, sunElevationDeg])
}

// W2 (design「大气 LUT 与重算」): the GPU's LUTs and the CPU's tables are two
// implementations of one atmosphere; a constant or a formula changed on one
// side only shows here. With tables of one size the two agree to about
// 0.2 % (half floats, the GPU's filtering); the thresholds leave room for
// that and no more.
await check('W2 atmosphere drift: the GPU LUTs agree with the CPU at their texels', async () => {
  const server = new SceneServer()
  // SCENE_E2E_W2_METAL: on this machine's GPU (the WebGPU backend) rather than SwiftShader's WebGL2.
  const gpu = process.env.SCENE_E2E_W2_METAL ? await chromium.launch({ headless: true, args: metal }) : null
  const { context, page, errors } = await open(server, gpu ? { context: await gpu.newContext() } : {})
  // The multiple scattering's largest relative errors are on texels near 0 (the sun below the horizon), a few 1e-5 off.
  const limits = { transmittance: [0.002, 0.01], multiScattering: [0.005, 0.05], skyView: [0.005, 0.03], skyDirections: [0.02, 0.1] }
  for (const [preset, elevation] of [['clear', 30], ['dust', 8], ['highland', -3]]) {
    const out = await atmosphereDrift(page, preset, elevation)
    console.log(`W2 drift ${preset} at ${elevation}° (${out.backend}): ${['transmittance', 'multiScattering', 'skyView', 'skyDirections'].map((k) => `${k} mean ${(out[k].mean * 100).toFixed(2)}% max ${(out[k].max * 100).toFixed(2)}% (${out[k].count}${out[k].flipped ? ', rows flipped' : ''})`).join(', ')}`)
    for (const [lut, [mean, max]] of Object.entries(limits)) {
      const r = out[lut]
      assert.equal(r.nonFinite, 0, `${preset} ${lut}: no Inf or NaN`)
      assert.ok(r.count > 20, `${preset} ${lut}: compared ${r.count}`)
      assert.ok(r.mean <= mean && r.max <= max, `${preset} at ${elevation}°, ${lut}: mean ${(r.mean * 100).toFixed(2)}%, max ${(r.max * 100).toFixed(2)}%`)
    }
  }
  assert.deepEqual(errors, [])
  await context.close()
  await gpu?.close()
})

// --- W2 physical sky: exposure, the sun and lamps (AC-W2-3, AC-W2-4, AC-W2-5) -------

/** The physical yard (scripts/fixtures/scene-reference): Shanghai on 2026-09-23 at noon, in the golden hour (17:30) and 20 minutes after sunset (18:10); its camera exposed by sunny 16 and its kin. */
const physicalNames = ['physical-noon', 'physical-golden', 'physical-dusk']

/** The yard opened in the rendered view at the editor's default view; change(ops) writes as another session would and waits for the picture. */
async function physicalYard(name, edit) {
  const server = new SceneServer()
  // W2's checks expose the free view by hand at render.exposure_ev100; W3's that want ambient metering say so.
  referenceScene(server, name, (scene) => {
    scene.render.exposure_mode = 'manual'
    edit?.(scene)
  })
  const { context, page, errors } = await open(server, { query: '?export=1' })
  const { box } = await viewportCenter(page)
  await page.mouse.move(1, 1)
  await shading(page, '最终渲染')
  const settle = async () => {
    await waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiling)) === 0, `${name}: compiled`, 60_000)
    // The sky light is made at most every 200 ms.
    await sleep(400)
    return settledShot(page, box)
  }
  const change = async (ops) => {
    server.external(ops)
    await page.evaluate(() => window.__invalidate())
    return settle()
  }
  const sky = (patch) => [{ op: 'set_environment', environment: { ...server.scene.environment, sky: { ...server.scene.environment.sky, ...patch } } }]
  return { server, context, page, errors, box, settle, change, sky }
}

/** The metered EV of a physical sky: incident light on level ground (world/exposure.ts environmentEV), worked out in the page as PhysicalLighting does. */
async function meteredEV(page, sky) {
  return page.evaluate(async (sky) => {
    const { physicalSun, sunVector } = await import('/src/creative-scene/world/astro.ts')
    const { sunAbove } = await import('/src/creative-scene/world/atmosphere/constants.ts')
    const { CpuAtmosphere } = await import('/src/creative-scene/world/atmosphere/cpu.ts')
    const { environmentEV } = await import('/src/creative-scene/world/exposure.ts')
    const where = physicalSun(sky)
    const day = new CpuAtmosphere(sky).daylight(sunVector(where.elevationDeg, where.azimuthDeg).y, sunAbove(sky.sun_strength, sky.sun_temperature_k))
    return environmentEV(day.horizontal)
  }, sky)
}

// SCENE_E2E_W2_PICTURES=<dir>: the physical yard's exports (each camera at its
// own exposure) for a look, on this machine's GPU when SCENE_E2E_W2_METAL is set.
if (process.env.SCENE_E2E_W2_PICTURES) await check('W2 pictures: the physical yard exported at noon, in the golden hour and after sunset', async () => {
  const dir = process.env.SCENE_E2E_W2_PICTURES
  const gpu = await chromium.launch({ headless: true, args: process.env.SCENE_E2E_W2_METAL ? metal : [] })
  try {
    for (const name of physicalNames) {
      const out = await exportReference(gpu, name)
      for (const img of out.images.filter((i) => i.pass === 'lit')) await writeFile(`${dir}/${img.file}`, img.png)
      const [view] = await viewportLooks(gpu, name, [['最终渲染', 'lit']])
      await writeFile(`${dir}/${view.file}`, view.png)
      console.log(`W2 pictures ${name}: viewport mean ${view.all.mean.toFixed(0)}`)
    }
  } finally {
    await gpu.close()
  }
})

await check('W2 sunny 16: noon at EV100 15 exposes the yard; 20 minutes after sunset meters 5 stops lower and comes out dark at the same exposure', async () => {
  const { server, context, page, errors, settle, change, sky } = await physicalYard('physical-noon')
  const noon = await lumaStats(page, await settle(), { level: 250 })
  await shot(page, 'w2-noon-ev15')
  const noonEV = await meteredEV(page, server.scene.environment.sky)
  const dusk = await lumaStats(page, await change(sky({ local_time: '18:10' })), { level: 250 })
  await shot(page, 'w2-dusk-ev15')
  const duskEV = await meteredEV(page, server.scene.environment.sky)
  console.log(`W2 sunny 16: noon meters EV ${noonEV.toFixed(1)}, mean ${noon.mean.toFixed(0)} (rgb ${[noon.r, noon.g, noon.b].map((v) => v.toFixed(0))}), clipped ${(noon.bright * 100).toFixed(2)}%; dusk meters EV ${duskEV.toFixed(1)}, mean ${dusk.mean.toFixed(1)}`)
  assert.ok(Math.abs(noonEV - 15) < 1, 'a clear noon meters about sunny 16')
  assert.ok(noon.mean > 120 && noon.mean < 220 && noon.bright < 0.01, 'exposed: neither dark nor blown')
  assert.ok(Math.abs(noon.r - noon.b) < 15, 'daylight is about neutral')
  assert.ok(noonEV - duskEV >= 5, 'after sunset the light is 5 stops down or more')
  assert.ok(dusk.mean < noon.mean / 4, 'and at noon’s exposure the yard is dark')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('W2 exposure and the sun: the camera view at the camera’s EV100, equal exposures alike, the sun’s strength in stops and its colour temperature', async () => {
  const { server, context, page, errors, settle, change, sky } = await physicalYard('physical-noon')
  const camera = server.scene.objects.find((o) => o.kind === 'camera')
  const lens = (patch) => [{ op: 'update_objects', updates: [{ object_id: camera.id, camera: patch }] }]
  const off = async (a, b) => (await compareShots(page, a, b, 2)).over
  const free = await settle()
  // 0 looks through the scene's camera, at f/16, 1/125 s, ISO 100: EV100 15 like the default exposure.
  await focusEditor(page)
  await page.keyboard.press('0')
  const base = await settle()
  await shot(page, 'w2-camera-f16')
  assert.ok((await pixelDiff(page, free, base)) > 0.05, 'the camera view is the camera’s picture')
  // The free view follows the default exposure; the camera view does not.
  await change([{ op: 'set_render', render: { exposure_ev100: 12 } }])
  assert.ok((await off(base, await settle())) < 0.05, 'the camera view keeps its own exposure')
  const sameEV = await change(lens({ f_stop: 8, shutter_s: 1 / 500 }))
  const two = await change(lens({ shutter_s: 1 / 125 }))
  await shot(page, 'w2-camera-f8')
  const strong = await change([...lens({ f_stop: 16 }), ...sky({ sun_strength: 4 })])
  const warm = await change(sky({ sun_strength: 1, sun_temperature_k: 3000 }))
  await shot(page, 'w2-camera-3000k')
  // Left out, the colour temperature and strength are filled in (state/apply.ts completeSky): 6500 K, 1.
  const { sun_temperature_k: _k, sun_strength: _s, ...unset } = server.scene.environment.sky
  const plain = await change([{ op: 'set_environment', environment: { ...server.scene.environment, sky: unset } }])
  const [b, t, w] = [await lumaStats(page, base), await lumaStats(page, two), await lumaStats(page, warm)]
  const measured = { sameEV: await off(base, sameEV), strongVsTwo: await off(two, strong), unset: await off(base, plain) }
  console.log(`W2 exposure: f/16 mean ${b.mean.toFixed(0)}, f/8 ${t.mean.toFixed(0)}; off by more than 2: same EV ${measured.sameEV.toFixed(3)}%, strength 4 against f/8 ${measured.strongVsTwo.toFixed(3)}%, unset against 6500 K ${measured.unset.toFixed(3)}%; R/B ${(b.r / b.b).toFixed(2)} → ${(w.r / w.b).toFixed(2)} at 3000 K`)
  assert.ok(measured.sameEV < 0.05, 'f/8 at 1/500 s is f/16 at 1/125 s')
  assert.ok(t.mean > b.mean * 1.3, 'two stops more is brighter')
  assert.ok(measured.strongVsTwo < 1, 'a sun 4 × as strong is two stops more light, sky and all')
  assert.equal(measured.unset, 0, 'the colour temperature and strength left out are 6500 K and 1')
  assert.ok(w.r / w.b > (b.r / b.b) * 1.1, 'a lower colour temperature is warmer')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('W2 lamps, emission and fog: invisible at noon, they show after sunset; fog is as bright as the lit ground', async () => {
  // The lamp and the sign stand in the yard throughout (the editor draws the lamp's icon); only their light is turned up.
  const bulb = (intensity) => ({ type: 'point', color: '#ffd9b0', intensity, cast_shadow: false, distance: 0, decay: 2 })
  const lamp = { id: gid('lamp'), kind: 'light', name: 'lamp', order: 30, transform: { ...identity(), position: [-0.5, 1.2, 1] }, light: bulb(0) }
  // A grey sign. (On a near-black board, 2 % of the sun's light, 50 cd/m² is about a tenth more: that shows, as it would.)
  const glow = (intensity) => ({ preset: 'principled@1', params: { color: '#808080', emissive: '#ffffff', emissive_intensity: intensity } })
  const sign = boxOf('sign', 31, { position: [1.8, 0, 1.5] }, { width: 0.5, height: 0.5, depth: 0.05 }, glow(0))
  const { server, context, page, errors, settle, change, sky } = await physicalYard('physical-noon', (scene) => {
    for (const o of [lamp, sign]) scene.objects.push({ parent_id: null, visible: true, locked: false, ...o })
  })
  const off = async (a, b) => (await compareShots(page, a, b, 2)).over
  const bare = await settle()
  const addLamp = [{ op: 'update_objects', updates: [{ object_id: lamp.id, light: bulb(40) }] }]
  const turnUp = [{ op: 'update_objects', updates: [{ object_id: sign.id, material_slots: { main: glow(50) } }] }]
  const add = [...addLamp, ...turnUp]
  const remove = [{ op: 'update_objects', updates: [{ object_id: lamp.id, light: bulb(0) }, { object_id: sign.id, material_slots: { main: glow(0) } }] }]
  const withLamp = await change(addLamp)
  const lampOff = await off(bare, withLamp)
  const signOff = await off(withLamp, await change(turnUp))
  // After sunset at the dusk camera's exposure (EV100 about 6).
  await change([...remove, ...sky({ local_time: '18:10' }), { op: 'set_render', render: { exposure_ev100: 6 } }])
  const night = await settle()
  const lit = await change(add)
  await shot(page, 'w2-dusk-lamp')
  const nightOff = await off(night, lit)
  const [n, l] = [await lumaStats(page, night), await lumaStats(page, lit)]
  console.log(`W2 lamps: at noon the lamp changes ${lampOff.toFixed(4)}% of pixels, the sign's emission ${signOff.toFixed(4)}%; after sunset both ${nightOff.toFixed(2)}%, mean ${n.mean.toFixed(1)} → ${l.mean.toFixed(1)}`)
  assert.ok(lampOff <= 0.01, 'a 40 cd lamp does not show at noon')
  assert.ok(signOff <= 0.01, 'nor 50 cd/m² of emission on a grey sign')
  assert.ok(nightOff > 1 && l.mean > n.mean, 'after sunset they light the yard')

  // Fog at noon over grey ground: the fogged distance against the lit ground near by.
  await change([...remove, ...sky({ local_time: '12:00' }), { op: 'set_render', render: { exposure_ev100: 15 } }])
  const fogged = await change([{ op: 'set_environment', environment: { ...server.scene.environment, fog: { color: '#b4b4b4', near: 3, far: 25 } } }])
  await shot(page, 'w2-noon-fog')
  const { box } = await viewportCenter(page)
  const far = await lumaStats(page, await settledShot(page, { x: box.x, y: box.y, width: box.width, height: box.height * 0.15 }))
  const near = await lumaStats(page, await settledShot(page, { x: box.x, y: box.y + box.height * 0.85, width: box.width, height: box.height * 0.15 }))
  console.log(`W2 fog: the fogged distance ${far.mean.toFixed(0)}, the ground near by ${near.mean.toFixed(0)}`)
  assert.ok(fogged && far.mean > near.mean * 0.75 && far.mean < near.mean * 1.35, 'the fog is lit like the ground under it')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('W2 back to another sky: switching the physical sky off leaves the lit view as it was, fog and all', async () => {
  const fog = { color: '#c8d0d8', near: 4, far: 40 }
  const { server, context, page, errors, settle, change } = await physicalYard('noon', (scene) => {
    scene.environment.fog = fog
  })
  const before = await settle()
  const procedural = server.scene.environment
  const physical = JSON.parse(readFileSync(new URL('fixtures/scene-reference/physical-noon.json', import.meta.url), 'utf8')).environment.sky
  await change([{ op: 'set_environment', environment: { ...procedural, sky: physical } }])
  const back = await change([{ op: 'set_environment', environment: procedural }])
  const m = await compareShots(page, before, back, 0)
  assert.ok(!m.error && m.over === 0, `the procedural sky as before (${m.error ?? `${m.over.toFixed(4)}% differ`})`)
  assert.deepEqual(errors, [])
  await context.close()
})

// SCENE_E2E_W2_GPU=<dir> (AC-W2-8): the physical yard on this machine's GPU,
// exported on WebGPU and again on the WebGL2 backend. Pictures with the scene
// in them are held to R1's lit threshold; a camera looking up at the sky
// alone to a mean of 0.5 and 0.05 % of pixels off by more than 8. Sheets and
// w2-parity.json go to the directory.
const skyOnly = { mean: 0.5, over: 0.05 }
const lookingUp = (scene) => {
  const up = (35 * Math.PI) / 180
  scene.objects.push({
    parent_id: null, visible: true, locked: false, id: 'sobj_skyup00000000000000000', kind: 'camera', name: 'sky', order: scene.objects.length + 20,
    transform: { position: [0, 1.6, 4], rotation: [Math.sin(up / 2), 0, 0, Math.cos(up / 2)], scale: [1, 1, 1] },
    camera: { focal_mm: 35, f_stop: 2.8, shutter_s: scene.objects.find((o) => o.kind === 'camera').camera.shutter_s, iso: scene.objects.find((o) => o.kind === 'camera').camera.iso },
  })
  // The scene's camera's EV100 for the sky camera too: same ISO and shutter, its aperture matched.
  scene.objects.at(-1).camera.f_stop = scene.objects.find((o) => o.kind === 'camera').camera.f_stop
}
if (process.env.SCENE_E2E_W2_GPU) await check('W2 parity: the physical yard on WebGPU and on the WebGL2 backend', async () => {
  const dir = process.env.SCENE_E2E_W2_GPU
  const gpu = await chromium.launch({ headless: true, args: metal })
  const rows = []
  const over = []
  try {
    const page = await (await gpu.newContext()).newPage()
    for (const name of physicalNames) {
      const webgpu = await exportReference(gpu, name, { edit: lookingUp })
      const webgl = await exportReference(gpu, name, { edit: lookingUp, forceWebGL: true })
      assert.ok(webgpu.backends.some((b) => b === 'scene renderer (offscreen): webgpu'), `${name}: ${webgpu.backends}`)
      assert.ok(webgl.backends.some((b) => b === 'scene renderer (offscreen): webgl2'), `${name}: ${webgl.backends}`)
      for (const img of webgl.images.filter((i) => i.pass === 'lit')) {
        const m = await compareShots(page, webgpu.images.find((i) => i.file === img.file).png, img.png)
        const limit = img.file.includes('-sky-') ? skyOnly : parityLimits.lit
        const ok = !m.error && m.mean <= limit.mean && m.over <= limit.over
        if (m.sheet) await writeFile(`${dir}/${img.file.replace('.png', '')}-webgl2.jpg`, m.sheet)
        rows.push({ file: img.file, mean: m.error ? null : Number(m.mean.toFixed(4)), over_percent: m.error ? null : Number(m.over.toFixed(4)), limit, ok })
        if (!ok) over.push(`${img.file}: ${m.error ?? `mean ${m.mean.toFixed(3)}, > 8 ${m.over.toFixed(3)}%`}`)
        console.log(`W2 parity ${img.file}: mean ${m.mean?.toFixed(3)}, > 8 ${m.over?.toFixed(4)}%${ok ? '' : ' (over)'}`)
      }
    }
    await writeFile(`${dir}/w2-parity.json`, `${JSON.stringify({ machine: machineInfo(), chromium: gpu.version(), limits: { scene: parityLimits.lit, sky: skyOnly }, rows }, null, 2)}\n`)
  } finally {
    await gpu.close()
  }
  assert.deepEqual(over, [], 'over the threshold: for the owner to look at')
})

// SCENE_E2E_W2_EVIDENCE=<dir> (W2 tasks 8.2, for the owner), this machine's GPU: the physical yard's lit
// exports, each camera exposed at the EV its light meters (environmentEV), so the pictures compare by
// colour and air rather than by brightness. The four presets at noon and in the golden hour; the plateau
// at a low sun looking level at the horizon; a clear sunset with the sun in the picture at 8K; the sun at
// −6° to −9° (twilight); and noon under the physical sky at EV100 15 against the procedural noon at EV 0.
if (process.env.SCENE_E2E_W2_EVIDENCE) await check('W2 evidence: presets, the plateau’s horizon, the sunset at 8K, twilight, and noon against the procedural sky', async () => {
  const dir = process.env.SCENE_E2E_W2_EVIDENCE
  const gpu = await chromium.launch({ headless: true, args: metal })
  const notes = {}
  try {
    const meter = await (await gpu.newContext()).newPage()
    await meter.goto(`${base}/`)
    const baseSky = JSON.parse(readFileSync(new URL('fixtures/scene-reference/physical-noon.json', import.meta.url), 'utf8')).environment.sky
    const presets = { clear: {}, haze: { dust_density: 12 }, highland: { altitude_m: 4000, dust_density: 0.2 }, dust: { dust_density: 40 } }
    /** The yard with this sky, every camera exposed at f/8 and ISO 100 for the metered EV; only the cameras kept. */
    const shoot = async (file, sky, { cameras, resolution } = {}) => {
      const ev = await meteredEV(meter, sky)
      const edit = (scene) => {
        scene.environment.sky = sky
        if (resolution) scene.render.resolution = resolution
        if (cameras) scene.objects = [...scene.objects.filter((o) => o.kind !== 'camera'), ...cameras]
        for (const o of scene.objects) if (o.kind === 'camera') o.camera = { ...o.camera, f_stop: 8, iso: 100, shutter_s: 64 / 2 ** ev }
      }
      const out = await exportReference(gpu, 'physical-noon', { edit })
      for (const img of out.images.filter((i) => i.pass === 'lit')) await writeFile(`${dir}/${file}-${img.file.split('-').at(-2)}.png`, img.png)
      notes[file] = { metered_ev100: Number(ev.toFixed(2)), sky: { local_time: sky.local_time, altitude_m: sky.altitude_m, dust_density: sky.dust_density } }
      console.log(`W2 evidence ${file}: metered EV100 ${ev.toFixed(1)}`)
      return out
    }
    const level = (name, yawDeg, position = [0, 1.6, 4]) => ({
      parent_id: null, visible: true, locked: false, id: gid(`ev${name}`), kind: 'camera', name, order: 50,
      transform: { position, rotation: [0, Math.sin((yawDeg * Math.PI) / 360), 0, Math.cos((yawDeg * Math.PI) / 360)], scale: [1, 1, 1] },
      camera: { focal_mm: 35 },
    })
    for (const [name, patch] of Object.entries(presets)) {
      await shoot(`preset-${name}-noon`, { ...baseSky, ...patch, local_time: '12:00' })
      await shoot(`preset-${name}-golden`, { ...baseSky, ...patch, local_time: '17:30' })
    }
    // The plateau at a low sun, level with the horizon: no sea-level band between the ground and the sky.
    await shoot('highland-horizon', { ...baseSky, ...presets.highland, local_time: '17:40' }, { cameras: [level('horizon', 0)] })
    // A clear sunset with the sun in the picture (it sets in the west, the scene's −X with north at 180°), at 8K.
    await shoot('sunset-8k', { ...baseSky, local_time: '17:45' }, { cameras: [level('sunset', 90)], resolution: '8k' })
    // Twilight: Shanghai on 2026-09-23 has the sun at about −6° at 18:16 and −9° at 18:30.
    for (const t of ['18:16', '18:23', '18:30']) await shoot(`twilight-${t.replace(':', '')}`, { ...baseSky, local_time: t })
    // Noon: the physical sky at EV100 15 (sunny 16) against the procedural noon at EV 0.
    const physicalNoon = await exportReference(gpu, 'physical-noon')
    const proceduralNoon = await exportReference(gpu, 'noon')
    const lit = (out) => out.images.find((i) => i.pass === 'lit' && i.file.includes('-shot-')).png
    const [p, q] = [await lumaStats(meter, lit(physicalNoon)), await lumaStats(meter, lit(proceduralNoon))]
    await writeFile(`${dir}/noon-physical-ev15.png`, lit(physicalNoon))
    await writeFile(`${dir}/noon-procedural-ev0.png`, lit(proceduralNoon))
    notes.noon = { physical_mean: Number(p.mean.toFixed(1)), procedural_mean: Number(q.mean.toFixed(1)) }
    console.log(`W2 evidence noon: physical at EV100 15 mean ${p.mean.toFixed(1)}, procedural at EV 0 mean ${q.mean.toFixed(1)}`)
    await writeFile(`${dir}/evidence.json`, `${JSON.stringify({ machine: machineInfo(), chromium: gpu.version(), notes }, null, 2)}\n`)
  } finally {
    await gpu.close()
  }
})

// --- W2 the environment tab under a physical sky (AC-W2-1, AC-W2-2, AC-W2-12) ----------

/** The noon yard in the rendered view, its sky switched to physical from the environment tab. */
async function physicalTab(edit) {
  const server = new SceneServer()
  referenceScene(server, 'noon', edit)
  const { context, page, errors } = await open(server)
  const { box } = await viewportCenter(page)
  await page.mouse.move(1, 1)
  await shading(page, '最终渲染')
  await page.getByRole('combobox', { name: '天空类型' }).selectOption('physical')
  await waitFor(() => server.scene.environment.sky.kind === 'physical', 'the physical sky written')
  await saved(page)
  const settle = async () => {
    await waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiling)) === 0, 'compiled', 60_000)
    await sleep(400)
    return settledShot(page, box)
  }
  const sky = () => server.scene.environment.sky
  /** Sets the local time slider to hh:mm and waits for it to be written. */
  const time = async (hhmm) => {
    const [h, m] = hhmm.split(':').map(Number)
    await page.getByRole('slider', { name: '当地时刻' }).fill(String(h * 60 + m))
    await waitFor(() => sky().local_time === hhmm, `local time ${hhmm}`)
  }
  return { server, context, page, errors, box, settle, sky, time }
}

await check('W2 environment tab: the physical sky with its defaults and today’s date, a city from the search with GeoNames credited, the ambient light off', async () => {
  const { server, context, page, errors, sky } = await physicalTab()
  const today = await page.evaluate(() => {
    const d = new Date()
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  })
  assert.deepEqual(server.commands.at(-1).payload.operations.map((o) => o.op), ['set_environment'])
  assert.deepEqual(
    { ...sky(), date: undefined },
    { kind: 'physical', sun_mode: 'location', latitude_deg: 31.2304, longitude_deg: 121.4737, timezone: 'Asia/Shanghai', place: '', date: undefined, local_time: '12:00', north_azimuth_deg: 180, sun_elevation_deg: 35, sun_azimuth_deg: 135, altitude_m: 0, air_density: 1, dust_density: 1, ozone_density: 1, sun_disc: true, sun_size_deg: 0.545, sun_strength: 1, sun_temperature_k: 6500, night: { moon: true, stars: true, milky_way: true, moon_strength: 1, moon_temperature_k: 6500, stars_strength: 1, stars_temperature_k: 6500, night_look: 1 }, aerial_perspective: { enabled: true, distance_scale: 1 } },
  )
  assert.equal(sky().date, today, 'today, on this machine')
  assert.deepEqual(server.scene.environment.ambient, { color: '#ffffff', intensity: 0.4 }, 'the rest of the environment stays')
  const ambient = page.locator('fieldset', { has: page.locator('legend', { hasText: '环境光' }) })
  assert.ok(await ambient.evaluate((f) => f.disabled), 'the ambient light is greyed out')
  await page.getByText('物理天空下天空光由大气算出，环境光不生效').waitFor()

  // The city table is fetched when the search box is first used, not with the editor (gzip about 160 KB).
  const asked = []
  page.on('request', (r) => r.url().includes('cities.json') && asked.push(r.url()))
  assert.equal(await page.evaluate(() => performance.getEntriesByType('resource').some((e) => e.name.includes('cities.json'))), false, 'not loaded with the editor')
  const search = page.getByRole('combobox', { name: '城市' })
  await search.focus()
  await waitFor(() => asked.length > 0, 'loaded on first focus')
  await search.fill('伦敦')
  const list = page.getByRole('listbox', { name: '城市' })
  await waitFor(async () => (await list.getByRole('option').count()) >= 2, 'results')
  assert.deepEqual((await list.getByRole('option').allTextContents()).slice(0, 2), ['伦敦 · 英国', '伦敦 · 加拿大'])
  const credit = page.getByRole('link', { name: 'GeoNames（CC BY 4.0），已修改' })
  assert.equal(await credit.getAttribute('href'), 'https://www.geonames.org/')
  await shot(page, 'w2-city-search')
  await search.fill('成都')
  await list.getByRole('option', { name: '成都 · 中国' }).click()
  await waitFor(() => sky().place === '成都', 'the city written')
  assert.deepEqual([sky().latitude_deg, sky().longitude_deg, sky().timezone], [30.67, 104.07, 'Asia/Shanghai'])
  assert.equal(server.commands.at(-1).payload.operations.length, 1, 'in one write')
  await saved(page)
  // A latitude typed in is no longer that city.
  const latitude = page.getByRole('spinbutton', { name: '纬度（°）' })
  await latitude.fill('30.5')
  await latitude.press('Enter')
  await waitFor(() => sky().latitude_deg === 30.5, 'latitude written')
  assert.equal(sky().place, '', 'the place is cleared')
  await page.getByRole('combobox', { name: '时区' }).selectOption('Asia/Tokyo')
  await waitFor(() => sky().timezone === 'Asia/Tokyo', 'time zone written')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('W2 environment tab: the time of day moves the sun and shows the day’s hours; north turns the shadows; in Sydney the noon sun is in the north', async () => {
  const { context, page, errors, settle, sky, time } = await physicalTab()
  await page.getByLabel('日期').fill('2026-06-21')
  await waitFor(() => sky().date === '2026-06-21', 'date written')
  const evidence = process.env.SCENE_E2E_W2_EVIDENCE
  const elevations = []
  for (const hhmm of ['05:00', '06:00', '08:00', '10:00', '12:00', '14:00', '16:00', '18:00', '19:00', '20:00']) {
    await time(hhmm)
    const png = await settle()
    elevations.push(Number((await page.getByRole('status', { name: '太阳高度' }).textContent()).replace(/[^\d.−-]/g, '').replace('−', '-')))
    if (evidence) await writeFile(`${evidence}/shanghai-solstice-${hhmm.replace(':', '')}.png`, png)
  }
  console.log(`W2 Shanghai on the solstice: elevation ${elevations.join(', ')}`)
  assert.ok(elevations[0] < 2 && elevations[4] > 80 && elevations.at(-1) < 0, 'below the horizon at 05:00 and 20:00, about 82° at noon')
  const day = await page.getByRole('region', { name: '太阳的一天' }).textContent()
  assert.match(day, /日出 04:5\d/)
  assert.match(day, /日落 19:0\d/)
  assert.match(day, /黄金时刻/)
  assert.match(day, /蓝调时刻/)
  // A day's dragging folds into one step per pause (merge key environment.local_time).

  await time('16:00')
  const before = await settle()
  await page.getByRole('slider', { name: '正北方向（°）' }).fill('90')
  await waitFor(() => sky().north_azimuth_deg === 90, 'north written')
  const turned = await settle()
  const turn = await pixelDiff(page, before, turned)
  if (process.env.SCENE_E2E_SHOTS) for (const [n, png] of [['before', before], ['turned', turned]]) await writeFile(`${process.env.SCENE_E2E_SHOTS}/w2-north-${n}.png`, png)
  assert.ok(turn > 0.01, `the shadows turn with north (${(turn * 100).toFixed(2)}% of pixels)`)

  await page.getByRole('combobox', { name: '城市' }).fill('悉尼')
  await page.getByRole('listbox', { name: '城市' }).getByRole('option', { name: '悉尼 · 澳大利亚' }).click()
  await waitFor(() => sky().timezone === 'Australia/Sydney', 'Sydney')
  await time('12:00')
  const bearing = Number((await page.getByRole('status', { name: '太阳方位' }).textContent()).replace(/[^\d.]/g, ''))
  assert.ok(bearing < 20 || bearing > 340, `the noon sun is in the north in Sydney (${bearing}°)`)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('W2 environment tab: manual angles keep the picture, undo goes back to the place; at night they stop at −10°; presets write the atmosphere', async () => {
  const { server, context, page, errors, settle, sky, time } = await physicalTab()
  await time('16:30')
  const located = await settle()
  await saved(page)
  const position = page.getByRole('group', { name: '太阳位置' })
  await position.getByRole('button', { name: '手动角度' }).click()
  await waitFor(() => sky().sun_mode === 'manual', 'manual')
  assert.deepEqual(server.commands.at(-1).payload.operations.length, 1, 'mode and angles in one write')
  assert.ok(sky().sun_elevation_deg > 0 && sky().sun_elevation_deg < 40, `the sun’s angle written (${sky().sun_elevation_deg})`)
  const manual = await settle()
  const m = await compareShots(page, located, manual, 2)
  assert.ok(m.over < 0.01, `the picture stays (${m.over.toFixed(4)}% off)`)
  await page.getByRole('slider', { name: '太阳高度（°）' }).waitFor()
  await saved(page)
  await focusEditor(page)
  await page.keyboard.press(`${modKey}+KeyZ`)
  await waitFor(() => sky().sun_mode === 'location', 'undo goes back to the place')
  await saved(page)
  await time('23:00')
  await position.getByRole('button', { name: '手动角度' }).click()
  await waitFor(() => sky().sun_mode === 'manual', 'manual at night')
  assert.equal(sky().sun_elevation_deg, -10, 'stops at −10°')
  await position.getByRole('button', { name: '地点和时间' }).click()
  await waitFor(() => sky().sun_mode === 'location', 'back')

  const preset = page.getByRole('combobox', { name: '大气预设' })
  assert.equal(await preset.inputValue(), 'clear')
  await preset.selectOption('haze')
  await waitFor(() => sky().dust_density === 12, 'haze written')
  assert.deepEqual([sky().altitude_m, sky().air_density, sky().ozone_density, sky().sun_strength], [0, 1, 1, 1], 'the four fields, not the sun')
  await preset.selectOption('highland')
  await waitFor(() => sky().altitude_m === 4000 && sky().dust_density === 0.2, 'highland written')
  const dust = page.getByRole('spinbutton', { name: '尘埃密度' })
  await dust.fill('3')
  await dust.press('Enter')
  await waitFor(() => sky().dust_density === 3, 'dust written')
  assert.equal(await preset.inputValue(), 'custom', 'custom once a field is changed')
  assert.deepEqual(errors, [])
  await context.close()
})

// --- W2 the exposure's controls (AC-W2-3) --------------------------------------------

const ev100Of = (c) => Math.log2((c.f_stop * c.f_stop) / c.shutter_s) - Math.log2(c.iso / 100)

await check('W2 exposure controls: the default exposure moves the free view only; a camera added takes it, within a third of a stop, at 12 and at −5', async () => {
  const { server, context, page, errors, settle } = await physicalYard('physical-golden')
  const tabs = page.getByRole('tablist', { name: '属性' })
  await tabs.getByRole('tab', { name: '渲染', exact: true }).click()
  assert.equal(await page.getByRole('spinbutton', { name: '曝光补偿（EV）' }).count(), 0, 'no compensation under a physical sky')
  await page.getByText(/晴天 15/).waitFor()
  const free = await settle()
  // Through the scene's camera, before and after the default exposure changes.
  await focusEditor(page)
  await page.keyboard.press('Digit0')
  const through = await settle()
  await page.keyboard.press('Digit0')
  const setDefault = async (ev) => {
    // Adding a camera selects it, which shows the object tab.
    await tabs.getByRole('tab', { name: '渲染', exact: true }).click()
    const field = page.getByRole('spinbutton', { name: '默认曝光（EV100）' })
    await field.fill(String(ev))
    await field.press('Enter')
    await waitFor(() => Math.abs(server.scene.render.exposure_ev100 - ev) < 1e-9, `default exposure ${ev}`)
    assert.deepEqual(server.commands.at(-1).payload.operations, [{ op: 'set_render', render: { exposure_ev100: ev } }])
  }
  await setDefault(11)
  const brighter = await settle()
  assert.ok((await pixelDiff(page, free, brighter)) > 0.05, 'the free view follows the default exposure')
  await focusEditor(page)
  await page.keyboard.press('Digit0')
  assert.ok((await compareShots(page, through, await settle(), 2)).over < 0.05, 'the camera view keeps its own')
  await page.keyboard.press('Digit0')

  for (const ev of [12, -5]) {
    await setDefault(ev)
    const before = await lumaStats(page, await settle())
    const { x, y } = await viewportCenter(page)
    await page.mouse.move(x, y)
    await page.keyboard.press('Shift+KeyA')
    await page.getByRole('menuitem', { name: '摄像机' }).click()
    await waitFor(() => server.commands.at(-1).payload.operations[0].op === 'add_objects', 'camera added')
    const camera = server.commands.at(-1).payload.operations[0].objects[0].camera
    const written = ev100Of({ f_stop: 2.8, ...camera })
    console.log(`W2 new camera at EV100 ${ev}: ISO ${camera.iso}, ${camera.shutter_s} s → EV100 ${written.toFixed(2)}`)
    assert.ok(Math.abs(written - ev) <= 1 / 3, `as bright as the view, within a third of a stop (${written.toFixed(2)})`)
    if (ev === -5) assert.deepEqual([camera.shutter_s, camera.iso > 100], [30, true], 'the shutter at 30 s, the ISO makes up the rest')
    if (ev === 12) {
      await page.mouse.move(1, 1)
      await focusEditor(page)
      await page.keyboard.press('Digit0')
      const after = await lumaStats(page, await settle())
      await page.keyboard.press('Digit0')
      console.log(`W2 new camera at 12: the view ${before.mean.toFixed(0)}, through it ${after.mean.toFixed(0)}`)
      assert.ok(Math.abs(after.mean / before.mean - 1) < 0.15, 'its picture is about as bright')
    }
  }
  assert.deepEqual(errors, [])
  await context.close()
})

await check('W2 exposure controls: a camera’s ISO, shutter and aperture with its EV100; greyed under another sky, like the default exposure', async () => {
  const { server, context, page, errors } = await physicalYard('physical-noon')
  const camera = server.scene.objects.find((o) => o.kind === 'camera')
  await page.getByRole('treeitem', { name: /shot/ }).click()
  const exposure = page.getByRole('group', { name: '曝光', exact: true })
  assert.match(await exposure.getByRole('status', { name: 'EV100' }).textContent(), /15\.0/)
  await exposure.getByRole('combobox', { name: 'ISO' }).selectOption('400')
  await waitFor(() => server.object(camera.id).camera.iso === 400, 'ISO written')
  await exposure.getByRole('combobox', { name: '快门' }).selectOption({ label: '1/500 s' })
  await waitFor(() => server.object(camera.id).camera.shutter_s === 1 / 500, 'shutter written')
  await exposure.getByRole('combobox', { name: '光圈' }).selectOption('8')
  await waitFor(() => server.object(camera.id).camera.f_stop === 8, 'aperture written with the depth of field off')
  // f/8 at 1/500 s, ISO 400: log₂(64 × 500) − 2 = 13.0.
  assert.match(await exposure.getByRole('status', { name: 'EV100' }).textContent(), /13\.0/)
  assert.ok(!(await exposure.evaluate((f) => f.disabled)))

  // Back to the procedural sky: the camera's exposure and the default exposure are greyed.
  server.external([{ op: 'set_environment', environment: { ...server.scene.environment, sky: { kind: 'procedural', sun_elevation_deg: 35, sun_azimuth_deg: 135, turbidity: 3 } } }])
  await page.evaluate(() => window.__invalidate())
  await waitFor(async () => await exposure.evaluate((f) => f.disabled), 'the exposure group greyed')
  await exposure.getByText('只在物理天空下生效').waitFor()
  await page.getByRole('tablist', { name: '属性' }).getByRole('tab', { name: '渲染', exact: true }).click()
  assert.ok(await page.getByRole('spinbutton', { name: '默认曝光（EV100）' }).isDisabled(), 'the default exposure greyed')
  assert.ok(await page.getByRole('spinbutton', { name: '曝光补偿（EV）' }).isEnabled(), 'the compensation as before')
  assert.deepEqual(errors, [])
  await context.close()
})

// --- R1 AC-R1-3: the same pictures in a hidden tab ------------------------------

// The export as the assistant's page draws it, without the editor or the network.
const hiddenFixture = new URL('../.scene-hidden-qa.html', import.meta.url)
await writeFile(
  hiddenFixture,
  `<html><body><script>window.__sceneTest = true</script><script type="module">
import { renderShots } from '/src/creative-scene/host/exportShots.ts';
import { loadModels, openShooter } from '/src/creative-scene/host/render.ts';
window.__kept = {};
// keep: a name to keep the images under, for __differ.
window.__export = async (doc, plan, keep) => {
  const out = await renderShots({ read: async () => doc, loadModels: (d) => loadModels(d.scene, d.models), open: (d, look) => openShooter(d.scene, d.models, look) }, 'doc', plan);
  if (!out.ok) throw new Error(out.message);
  if (keep) window.__kept[keep] = out.images.map((i) => i.blob);
  const hex = async (blob) => [...new Uint8Array(await crypto.subtle.digest('SHA-256', await blob.arrayBuffer()))].map((b) => b.toString(16).padStart(2, '0')).join('');
  return Promise.all(out.images.map(async (i) => ({ camera: i.camera, pass: i.pass, hash: await hex(i.blob) })));
};
// Per image of two kept exports: the largest channel difference and the share of pixels off by more than 8.
window.__differ = async (a, b) => {
  const pixels = async (blob) => {
    const bitmap = await createImageBitmap(blob);
    const c = new OffscreenCanvas(bitmap.width, bitmap.height);
    const g = c.getContext('2d');
    g.drawImage(bitmap, 0, 0);
    return g.getImageData(0, 0, c.width, c.height).data;
  };
  const out = [];
  for (let i = 0; i < window.__kept[a].length; i++) {
    const [p, q] = await Promise.all([pixels(window.__kept[a][i]), pixels(window.__kept[b][i])]);
    let max = 0, over = 0;
    for (let k = 0; k < p.length; k += 4) {
      const d = Math.max(Math.abs(p[k] - q[k]), Math.abs(p[k + 1] - q[k + 1]), Math.abs(p[k + 2] - q[k + 2]));
      if (d > max) max = d;
      if (d > 8) over++;
    }
    out.push({ max, over: over / (p.length / 4) });
  }
  return out;
};
window.__ready = true;
</script></body></html>`,
)

// SCENE_E2E_HIDDEN=1 (needs Google Chrome): Playwright keeps the pages it
// drives visible, so Chrome is started off screen and spoken to over the
// DevTools protocol, as creative-scene-agent-background.e2e.mjs does. The
// material board with glass and two more cameras, clay, line and lit: once in
// front, twice behind a blank tab, then once behind it on the WebGL2 backend.
// R2 AC-R2-5: the real-time GI's exports too, averaged over K frames with the page yielding between them.
if (process.env.SCENE_E2E_HIDDEN) await check('determinism: exports in a hidden tab are the foreground’s on WebGPU and WebGL2, the workbench’s pixel for pixel, the real-time GI’s within a level', async () => {
  const chromePath = process.env.CHROME_PATH ?? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
  const port = 9334
  const profile = mkdtempSync(join(tmpdir(), 'scene-hidden-'))
  const chrome = spawn(chromePath, [`--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, '--no-first-run', '--no-default-browser-check', '--window-position=-2400,-2400', '--window-size=1200,800', `${base}/.scene-hidden-qa.html`], { stdio: 'ignore' })
  const devtools = `http://127.0.0.1:${port}`
  try {
    let target
    for (let i = 0; i < 100 && !target; i++) {
      await sleep(200)
      target = await fetch(`${devtools}/json`).then((r) => r.json()).then((list) => list.find((t) => t.type === 'page' && t.url.includes('.scene-hidden-qa.html'))).catch(() => null)
    }
    assert.ok(target, 'the fixture tab')
    const ws = new WebSocket(target.webSocketDebuggerUrl)
    await new Promise((resolve, reject) => { ws.onopen = resolve; ws.onerror = reject })
    let id = 0
    const pending = new Map()
    ws.onmessage = (m) => {
      const msg = JSON.parse(m.data)
      if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id) }
    }
    const evaluate = (expression, timeoutMs = 300_000) => new Promise((resolve, reject) => {
      const n = ++id
      const timer = setTimeout(() => { pending.delete(n); reject(new Error(`timed out after ${timeoutMs} ms: ${expression.slice(0, 60)}`)) }, timeoutMs)
      pending.set(n, (msg) => {
        clearTimeout(timer)
        if (msg.error) reject(new Error(`CDP: ${msg.error.message}`))
        else if (msg.result?.exceptionDetails) reject(new Error(msg.result.exceptionDetails.exception?.description ?? 'evaluate failed'))
        else resolve(msg.result?.result?.value)
      })
      ws.send(JSON.stringify({ id: n, method: 'Runtime.evaluate', params: { expression, awaitPromise: true, returnByValue: true } }))
    })
    await waitFor(async () => (await evaluate('!!window.__ready')) === true, 'the fixture loaded', 30_000)
    const scene = JSON.parse(readFileSync(new URL('fixtures/scene-reference/materials.json', import.meta.url), 'utf8'))
    const board = scene.objects.find((o) => o.kind === 'camera')
    for (const [i, dx] of [[1, -1.5], [2, 1.5]]) scene.objects.push({ ...structuredClone(board), id: `sobj_cam${String(i).padStart(18, '0')}`, name: `cam${i}`, order: 100 + i, transform: { ...board.transform, position: [board.transform.position[0] + dx, board.transform.position[1], board.transform.position[2]] } })
    const doc = { scene, models: {}, revision: '1' }
    const plan = { cameraObjectIDs: scene.objects.filter((o) => o.kind === 'camera').map((o) => o.id), passes: ['clay', 'line', 'lit'] }
    const run = () => evaluate(`window.__export(${JSON.stringify(doc)}, ${JSON.stringify(plan)})`)
    const gi = { ...doc, scene: { ...scene, render: { ...scene.render, engine: 'realtime_gi' } } }
    const runGI = (keep) => evaluate(`window.__export(${JSON.stringify(gi)}, ${JSON.stringify(plan)}, ${JSON.stringify(keep)})`, 900_000)
    /** AC-R2-5: the real-time GI within one level per channel, and next to no pixel off by more than 8. */
    const near = async (a, b, what) => {
      const diffs = await evaluate(`window.__differ(${JSON.stringify(a)}, ${JSON.stringify(b)})`)
      diffs.forEach((d, i) => assert.ok(d.max <= 1 || d.over <= 0.0001, `${what}, image ${i}: largest difference ${d.max}, ${(d.over * 100).toFixed(4)}% of pixels off by more than 8`))
      return diffs
    }
    assert.equal(await evaluate('document.visibilityState'), 'visible')
    const front = await run()
    assert.equal(front.length, 9)
    assert.equal(new Set(front.map((i) => i.hash)).size, 9, 'every camera and pass its own picture')
    await fetch(`${devtools}/json/new?about:blank`, { method: 'PUT' })
    await waitFor(async () => (await evaluate('document.visibilityState')) === 'hidden', 'the tab went into the background', 10_000)
    const raf = await evaluate('new Promise((res) => { let n = 0; const t = () => { n++; requestAnimationFrame(t) }; requestAnimationFrame(t); setTimeout(() => res(n), 1000) })')
    const first = await run()
    const second = await run()
    assert.equal(await evaluate('document.visibilityState'), 'hidden', 'still in the background')
    assert.deepEqual(first, front, 'hidden as in front')
    assert.deepEqual(second, first, 'twice the same')
    await evaluate('window.__sceneGPU.forceWebGL = true')
    const webgl = await run()
    const again = await run()
    assert.deepEqual(again, webgl, 'the WebGL2 backend, twice the same in the background')
    // W2 AC-W2-9: the physical sky in the background, twice the same on each backend (its LUTs and sky light are made before the first frame).
    const sky = JSON.parse(readFileSync(new URL('fixtures/scene-reference/physical-golden.json', import.meta.url), 'utf8'))
    const skyDoc = { scene: sky, models: {}, revision: '1' }
    const skyPlan = { cameraObjectIDs: sky.objects.filter((o) => o.kind === 'camera').map((o) => o.id), passes: ['clay', 'lit'] }
    const runSky = () => evaluate(`window.__export(${JSON.stringify(skyDoc)}, ${JSON.stringify(skyPlan)})`)
    const skyWebGL = [await runSky(), await runSky()]
    assert.deepEqual(skyWebGL[1], skyWebGL[0], 'the physical sky on WebGL2, twice the same in the background')
    await evaluate('window.__sceneGPU.forceWebGL = false')
    const skyWebGPU = [await runSky(), await runSky()]
    assert.deepEqual(skyWebGPU[1], skyWebGPU[0], 'the physical sky on WebGPU, twice the same in the background')
    console.log(`W2 determinism: the physical sky's lit export ${skyWebGPU[0].find((i) => i.pass === 'lit').hash.slice(0, 12)} on WebGPU, ${skyWebGL[0].find((i) => i.pass === 'lit').hash.slice(0, 12)} on WebGL2`)
    // The real-time GI after the workbench's whole run, so its long export frees no material the workbench reuses:
    // in front again, then twice behind the blank tab, then twice on the WebGL2 backend.
    await evaluate('window.__sceneGPU.forceWebGL = false')
    const blank = await fetch(`${devtools}/json`).then((r) => r.json()).then((list) => list.find((t) => t.type === 'page' && t.url === 'about:blank'))
    await fetch(`${devtools}/json/close/${blank.id}`)
    await waitFor(async () => (await evaluate('document.visibilityState')) === 'visible', 'the tab in front again', 10_000)
    const startedGI = Date.now()
    const frontGI = await runGI('front')
    const giMs = Date.now() - startedGI
    // Clay and line art are the workbench's whatever the renderer (R2 tasks 7.4, engines.ts effectiveEngine).
    assert.deepEqual(frontGI.filter((i) => i.pass !== 'lit'), front.filter((i) => i.pass !== 'lit'), 'clay and line art are the workbench’s')
    assert.ok(frontGI.filter((i) => i.pass === 'lit').every((i) => !front.some((w) => w.hash === i.hash)), 'lit is the real-time GI’s')
    await fetch(`${devtools}/json/new?about:blank`, { method: 'PUT' })
    await waitFor(async () => (await evaluate('document.visibilityState')) === 'hidden', 'the tab in the background again', 10_000)
    await runGI('first')
    await runGI('second')
    assert.equal(await evaluate('document.visibilityState'), 'hidden', 'still in the background')
    const hiddenVsFront = await near('first', 'front', 'real-time GI: hidden as in front')
    const twice = await near('second', 'first', 'real-time GI: twice the same')
    await evaluate('window.__sceneGPU.forceWebGL = true')
    await runGI('webgl1')
    await runGI('webgl2')
    await near('webgl2', 'webgl1', 'real-time GI on the WebGL2 backend, twice the same in the background')
    const largest = (diffs) => Math.max(...diffs.map((d) => d.max))
    console.log(`determinism: the real-time GI export (9 images, K frames each) took ${giMs} ms in front; largest difference hidden/front ${largest(hiddenVsFront)}, twice ${largest(twice)}`)
    console.log(`determinism: hidden (rAF ${raf}/s) = front on WebGPU; WebGL2 in the background repeats itself (${webgl.filter((w, i) => w.hash === front[i].hash).length}/9 equal to WebGPU)`)
  } finally {
    chrome.kill()
    await sleep(500)
    await rm(profile, { recursive: true, force: true })
  }
})

// --- R1 AC-R1-4: without WebGPU ------------------------------------------------

await check('fallback: with no WebGPU adapter (SwiftShader) the editor draws, edits and exports on the WebGL2 backend', async () => {
  const server = new SceneServer()
  referenceScene(server, 'noon')
  const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
  const backends = []
  context.on('console', (m) => m.text().startsWith('scene renderer') && backends.push(m.text()))
  const { page, errors } = await open(server, { context, query: '?export=1' })
  assert.equal(await page.evaluate(async () => (await navigator.gpu?.requestAdapter()) ?? null), null, 'this browser has no adapter')
  await addBox(page, server)
  await page.getByRole('button', { name: '导出' }).first().click()
  const dialog = page.getByRole('dialog', { name: '导出分镜' })
  await dialog.waitFor()
  await dialog.getByRole('button', { name: '导出' }).click()
  await waitFor(() => server.exports.length === 1, 'export submitted', 120_000)
  assert.ok(backends.includes('scene renderer (viewport): webgl2'), backends.join(', '))
  assert.ok(backends.includes('scene renderer (offscreen): webgl2'), backends.join(', '))
  assert.deepEqual(errors, [])
  await context.close()
})

await check('fallback: a browser with neither WebGPU nor WebGL2 says so and claims no jobs', async () => {
  const bare = await chromium.launch({ headless: true })
  try {
    const server = new SceneServer()
    const context = await bare.newContext({ viewport: { width: 1280, height: 820 } })
    const { page } = await open(server, { context, query: '?host=1', viewport: false })
    assert.equal(await page.evaluate(async () => !!document.createElement('canvas').getContext('webgl2') || (await navigator.gpu?.requestAdapter()) != null), false, 'this browser has neither')
    await page.getByRole('alert').filter({ hasText: '当前浏览器无法显示 3D 场景' }).waitFor()
    await page.evaluate(() => window.__invalidate?.())
    await sleep(1500)
    assert.equal(server.claims.length, 0, 'no claims')
    await context.close()
  } finally {
    await bare.close()
  }
})

// --- R1 AC-R1-5: a lost device (render/gpu.ts test hooks) -------------------------

/** host/launcher.ts recoverEveryMs: after a device loss the page tries for a device again no sooner. */
const recoverWaitMs = 10_000

await check('device loss: the viewport draws again on a new device; the selection, the view and the edits stay', async () => {
  const server = new SceneServer()
  const { context, page, errors } = await open(server)
  const id = await addBox(page, server)
  await saved(page)
  // A second box on its way to the server when the device goes: the edit is outside the Canvas and still goes out.
  let release
  server.hold = new Promise((resolve) => (release = resolve))
  const sent = server.commands.length
  const { x, y, box } = await viewportCenter(page)
  await page.mouse.move(x + 140, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '方块' }).click()
  await waitFor(async () => (await page.getByRole('treeitem', { name: /方块/ }).count()) === 2, 'the second box shows')
  const before = await settledShot(page, box)
  const view = await page.evaluate(() => localStorage.getItem(Object.keys(localStorage).find((k) => k.startsWith('creative-scene-view:'))))
  const old = await page.locator('.cc-scene-editor canvas').elementHandle()
  assert.equal(await page.evaluate(() => window.__sceneGPU.loseDevice('viewport')), 1, 'one viewport renderer is live')
  await waitFor(async () => !(await old.evaluate((c) => c.isConnected)), 'a new canvas')
  await viewportReady(page)
  const { box: again } = await viewportCenter(page)
  const after = await settledShot(page, again)
  assert.ok((await pixelDiff(page, before, after)) < 0.003, 'the same picture on the new device')
  assert.equal(await page.getByRole('treeitem', { selected: true }).count(), 1, 'still selected')
  assert.equal(await page.evaluate(() => localStorage.getItem(Object.keys(localStorage).find((k) => k.startsWith('creative-scene-view:')))), view, 'the same view')
  assert.ok(server.object(id), 'the box is in the scene')
  server.hold = null
  release()
  await waitFor(() => server.commands.length === sent + 1, 'the held edit went out')
  await saved(page)
  assert.equal(server.scene.objects.filter((o) => o.primitive?.builtin?.startsWith('box')).length, 2, 'both boxes saved')
  assert.deepEqual(errors.filter((e) => !/lost|Lost/.test(e)), [])
  // A device that keeps failing: the third loss within 10 seconds stops trying.
  for (let i = 0; i < 2; i++) {
    await viewportReady(page)
    const canvas = await page.locator('.cc-scene-editor canvas').elementHandle()
    await page.evaluate(() => window.__sceneGPU.loseDevice('viewport'))
    await waitFor(async () => !(await canvas.evaluate((c) => c.isConnected)), 'gone')
  }
  await page.getByRole('alert').filter({ hasText: '渲染设备不可用，请刷新页面' }).waitFor()
  await context.close()
})

await check('device loss: render_view and export_shots lost mid-way upload nothing, go back to the queue and finish on a later try', async () => {
  const server = new SceneServer()
  referenceScene(server, 'noon')
  const { context, page, errors } = await open(server, { query: '?host=1' })
  await waitFor(() => server.claims.length > 0, 'the page claims', 10_000)
  const cam = 'sobj_shot00000000000000000'
  const jobs = [
    { kind: 'render_view', request: { preset: 'overview', mode: 'clay' }, after: 0 },
    // Lost after the second of three images: none of them may go up.
    { kind: 'export_shots', request: { camera_object_ids: [cam], passes: ['clay', 'line', 'lit'], canvas_id: 'ccv_qa', scene_node_id: 'cwnode_qa' }, after: 2 },
  ]
  for (const [n, j] of jobs.entries()) {
    const id = `csj_lost${n}`
    const job = (attempt) => ({ id, document_id: DOC, kind: j.kind, request: j.request, attempt, claim_token: `tok_${id}_${attempt}`, lease_until: '2100-01-01T00:00:00Z', deadline: '2100-01-01T00:00:00Z' })
    await page.evaluate((after) => window.__sceneGPU.loseNext('offscreen', after), j.after)
    const uploads = server.uploads.size
    server.claimable = [job(0)]
    await page.evaluate(() => window.__invalidate())
    await waitFor(() => server.claimable.length === 0, `${j.kind}: claimed`, 10_000)
    await waitFor(async () => (await page.evaluate(() => window.__launcher.store.getState().holding)) === null, `${j.kind}: given back`, 120_000)
    assert.equal(await page.evaluate(() => window.__sceneGPU.armed.size), 0, `${j.kind}: the device was lost`)
    assert.equal(server.completes.filter((c) => c.id === id).length, 0, `${j.kind}: not reported, the lease runs out`)
    assert.equal(server.uploads.size, uploads, `${j.kind}: nothing went up`)
    // The lease has run out: the job is queued again. The page waits 10 seconds from the loss before it tries for a device.
    server.claimable = [job(1)]
    await page.evaluate(() => window.__invalidate())
    await sleep(1500)
    assert.equal(server.claimable.length, 1, `${j.kind}: not claimed again at once`)
    await sleep(recoverWaitMs)
    await page.evaluate(() => window.__invalidate())
    await waitFor(() => server.completes.some((c) => c.id === id), `${j.kind}: finished on the next try`, 120_000)
    assert.equal(server.completes.find((c) => c.id === id).payload.outcome, 'succeeded')
  }
  assert.deepEqual(errors.filter((e) => !/lost|Lost/.test(e)), [])
  await context.close()
})

await check('device loss: a manual export lost while drawing says so, places nothing, and goes through on a retry', async () => {
  const server = new SceneServer()
  referenceScene(server, 'noon')
  const { context, page, errors } = await open(server, { query: '?export=1' })
  await page.evaluate(() => window.__sceneGPU.loseNext('offscreen', 1))
  await page.getByRole('button', { name: '导出' }).first().click()
  const dialog = page.getByRole('dialog', { name: '导出分镜' })
  await dialog.waitFor()
  await dialog.getByRole('button', { name: '导出' }).click()
  // The editor's notice line, the dialog kept open for the retry.
  await page.getByText('渲染设备暂时不可用，请重试').waitFor({ timeout: 120_000 })
  assert.equal(server.exports.length, 0, 'nothing placed')
  assert.equal(server.uploads.size, 0, 'nothing uploaded')
  await dialog.getByRole('button', { name: '导出' }).click()
  await waitFor(() => server.exports.length === 1, 'the retry goes through', 120_000)
  assert.deepEqual(errors.filter((e) => !/lost|Lost/.test(e)), [])
  await context.close()
})

await check('device loss: the asset card thumbnail drawn on a device that is lost is drawn again on a new one', async () => {
  const server = new SceneServer()
  const model = server.seedModel()
  const pillar = 'sobj_P00000000000000000001'
  server.seedInstance(pillar, model, 0, '柱', { stone: { preset: 'marble@1', params: {} } })
  const { context, page, errors } = await open(server, { query: '?library=1' })
  await page.evaluate(() => window.__sceneGPU.loseNext('thumbnail', 0))
  await page.getByRole('treeitem', { name: /柱/ }).click()
  await page.getByRole('button', { name: '存入资产库…' }).click()
  const dialog = page.getByRole('dialog', { name: '存入个人资产库' })
  await dialog.getByRole('button', { name: '存入', exact: true }).click()
  await dialog.waitFor({ state: 'detached', timeout: 60_000 })
  await page.locator('#notice').filter({ hasText: '已存入资产库' }).waitFor()
  assert.equal(await page.evaluate(() => window.__sceneGPU.armed.size), 0, 'the first device was lost')
  const thumbnail = server.uploadBytes.get(server.saves[0].payload.thumbnail_upload_id)
  assert.deepEqual(pngSize(thumbnail), { width: 256, height: 256 })
  assert.ok((await pixelDiff(page, await clayThumbnail(page), thumbnail)) > 0.02, 'the materials drawn, not an empty card')
  assert.deepEqual(errors.filter((e) => !/lost|Lost/.test(e)), [])
  await context.close()
})

/** The machine the baseline ran on: model, chip and OS, for the record. */
function machineInfo() {
  const run = (cmd) => { try { return execSync(cmd, { encoding: 'utf8' }).trim() } catch { return '' } }
  return { model: run('sysctl -n hw.model'), chip: run('sysctl -n machdep.cpu.brand_string'), os: `${process.platform} ${run('sw_vers -productVersion') || run('uname -r')}` }
}

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
    // SCENE_E2E_PERF_OUT=<dir> keeps the numbers next to the migration's evidence (R1 AC-R1-7).
    const out = process.env.SCENE_E2E_BASELINE ?? process.env.SCENE_E2E_PERF_OUT
    if (out) {
      const perf = { machine: machineInfo(), chromium: gpu.version(), renderer, canvas, modes: results, light_add_ms: add, light_remove_ms: remove }
      await writeFile(`${out}/perf.json`, `${JSON.stringify(perf, null, 2)}\n`)
    }
    assert.ok(results.rendered.mean <= 33, `rendered mean frame ${results.rendered.mean.toFixed(1)} ms`)
    // Both can sit at the display's refresh rate, where they differ only by noise.
    assert.ok(results.preview.mean <= results.rendered.mean + 0.5, 'material preview is no slower')
    assert.ok(add < 1000 && remove < 1000, 'adding or removing a light hitches under a second')
    assert.deepEqual(errors, [], errors.join('\n'))
    await context.close()
  } finally {
    await gpu.close()
  }
})

/** rAF timestamps, each with the compiles under way (SceneGPU.compile), for ms while a swipe turns the view every frame. */
const compileFrames = (page, ms) =>
  page.evaluate((ms) => new Promise((resolve) => {
    const el = document.querySelector('.cc-scene-editor canvas')
    const frames = []
    let sign = 1
    const start = performance.now()
    const tick = (t) => {
      frames.push([t, window.__sceneGPU.compiling])
      sign = -sign
      el.dispatchEvent(new WheelEvent('wheel', { deltaX: 4 * sign, deltaY: 0, deltaMode: 0, bubbles: true, cancelable: true }))
      if (t - start < ms) requestAnimationFrame(tick)
      else resolve(frames)
    }
    requestAnimationFrame(tick)
  }), ms)

if (process.env.SCENE_E2E_PERF) await check('AC-9: switching to the rendered view of the reference scene, on this machine’s GPU', async () => {
  const gpu = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
  try {
    const context = await gpu.newContext({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 })
    const server = new SceneServer()
    perfScene(server)
    const { page, errors } = await open(server, { context })
    const { x, y } = await viewportCenter(page)
    await page.mouse.move(x, y)
    // The longest frame from the click on, and when the scene's lights came on (their compile done).
    const measure = async (from) => {
      await shading(page, from)
      await settledShot(page, { x: x - 50, y: y - 50, width: 100, height: 100 })
      // Past the second after which an unused material is freed (materials/instances.ts).
      await sleep(1500)
      const measuring = compileFrames(page, 6000)
      await sleep(500)
      const [at, before] = await page.evaluate(() => [performance.now(), window.__sceneGPU.compiles])
      await shading(page, '最终渲染')
      const after = (await measuring).filter(([t]) => t >= at)
      const longest = Math.max(...gaps([at, ...after.map(([t]) => t)]))
      const last = after.findLastIndex(([, compiling]) => compiling > 0)
      const lightsOn = (after[last + 1] ?? after[0])[0] - at
      const compiles = (await page.evaluate(() => window.__sceneGPU.compiles)) - before
      return { longest_frame_ms: Math.round(longest), lights_on_ms: Math.round(lightsOn), compiles }
    }
    const switches = {
      'solid → rendered, first': await measure('白模'),
      'solid → rendered, again': await measure('白模'),
      'preview → rendered': await measure('材质预览'),
      'wireframe → rendered': await measure('线框'),
    }
    for (const [name, m] of Object.entries(switches)) console.log(`  ${name}: longest frame ${m.longest_frame_ms} ms, lights on after ${m.lights_on_ms} ms, ${m.compiles} compile(s)`)
    const out = process.env.SCENE_E2E_PERF_OUT
    if (out) await writeFile(`${out}/switch.json`, `${JSON.stringify({ machine: machineInfo(), chromium: gpu.version(), switches }, null, 2)}\n`)
    // The browser keeps compiled shaders between runs: with none kept the first switch's first frame took 1.7 s
    // and its lights 3.7 s; compiling light by light, every switch took 7 to 10 s with stalls of 2.5 s.
    for (const [name, m] of Object.entries(switches)) {
      assert.equal(m.compiles, 1, `${name}: the four lights compile together`)
      assert.ok(m.longest_frame_ms < 2000 && m.lights_on_ms < 5000, `${name}: longest frame ${m.longest_frame_ms} ms, lights on after ${m.lights_on_ms} ms`)
    }
    assert.deepEqual(errors, [], errors.join('\n'))
    await context.close()
  } finally {
    await gpu.close()
  }
})

// SCENE_E2E_W2_PERF=<dir> (AC-W2-11), this machine's GPU at 1600 × 1000, 2× pixels: AC-9's scene under the
// physical sky at noon. The first switch to the rendered view in a fresh browser (shader cache cold) and
// again (warm); frame times against the procedural sky in the same page; dragging the time of day, the
// frame times while dragging and how long after the last move the sky light caught up; the CPU part's
// time (an atmosphere change, a time change). Written to w2-perf.json in the directory.
if (process.env.SCENE_E2E_W2_PERF) await check('W2 performance: the physical sky on this machine’s GPU', async () => {
  const dir = process.env.SCENE_E2E_W2_PERF
  const gpu = await chromium.launch({ headless: true, args: metal })
  try {
    const context = await gpu.newContext({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 })
    const server = new SceneServer()
    perfScene(server)
    const physical = JSON.parse(readFileSync(new URL('fixtures/scene-reference/physical-noon.json', import.meta.url), 'utf8')).environment.sky
    const procedural = server.scene.environment.sky
    server.scene.environment.sky = physical
    server.scene.render.exposure_ev100 = 15
    const { page, errors } = await open(server, { context })
    const { x, y } = await viewportCenter(page)
    await page.mouse.move(x, y)
    const record = { machine: machineInfo(), chromium: gpu.version(), viewport: '1600 × 1000, 2× pixels', scene: 'AC-9 (about 200 objects, 4 lights) under the physical sky, Shanghai at noon' }

    // Switching to the rendered view: until the first frame with the sky light made.
    const switchTo = async () => {
      await shading(page, '白模')
      await settledShot(page, { x: x - 50, y: y - 50, width: 100, height: 100 })
      await sleep(1500)
      const measuring = orbitFrames(page, 6000)
      await sleep(300)
      const at = await page.evaluate(() => performance.now())
      await shading(page, '最终渲染')
      const after = (await measuring).filter((t) => t >= at)
      const lit = await page.evaluate(() => window.__sceneGPU.skyLightAt)
      return { longest_frame_ms: Math.round(Math.max(...gaps([at, ...after]))), sky_light_ms: Math.round(lit - at) }
    }
    record.switch = { cold: await switchTo(), warm: await switchTo() }

    // Standing still (the view turned a little each frame so every frame renders), physical then procedural.
    await orbitFrames(page, 1000)
    record.frames = { physical: frameStats(await orbitFrames(page, 4000)) }
    server.external([{ op: 'set_environment', environment: { ...server.scene.environment, sky: procedural } }])
    await page.evaluate(() => window.__invalidate())
    await sleep(1000)
    await orbitFrames(page, 1000)
    record.frames.procedural = frameStats(await orbitFrames(page, 4000))
    server.external([{ op: 'set_environment', environment: { ...server.scene.environment, sky: physical } }])
    await page.evaluate(() => window.__invalidate())
    await sleep(1000)

    // Dragging the time of day: a minute further each frame for 3 s, from 06:00.
    const drag = await page.evaluate(() => new Promise((resolve) => {
      const slider = document.querySelector('input[type=range][aria-label="当地时刻"]')
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set
      const times = []
      let minute = 360
      const start = performance.now()
      const tick = (t) => {
        times.push(t)
        set.call(slider, String(minute++))
        slider.dispatchEvent(new Event('input', { bubbles: true }))
        if (t - start < 3000) requestAnimationFrame(tick)
        else resolve({ times, last: performance.now() })
      }
      requestAnimationFrame(tick)
    }))
    await sleep(1500)
    const caughtUp = await page.evaluate(() => window.__sceneGPU.skyLightAt)
    record.drag = { ...frameStats(drag.times), sky_light_after_last_ms: Math.round(caughtUp - drag.last) }

    // The CPU part (world/atmosphere/cpu.ts), as PhysicalLighting runs it: tables on an atmosphere change, the day's light on a time change.
    record.cpu = await page.evaluate(async () => {
      const { CpuAtmosphere } = await import('/src/creative-scene/world/atmosphere/cpu.ts')
      const { PRESETS, sunAbove } = await import('/src/creative-scene/world/atmosphere/constants.ts')
      const time = (fn, n) => {
        fn()
        const t0 = performance.now()
        for (let i = 0; i < n; i++) fn()
        return (performance.now() - t0) / n
      }
      let a = new CpuAtmosphere(PRESETS.clear)
      const top = sunAbove(1, 6500)
      return {
        atmosphere_change_ms: Number(time(() => { a = new CpuAtmosphere(PRESETS.haze) }, 10).toFixed(2)),
        time_change_ms: Number(time(() => a.daylight(0.5, top), 50).toFixed(2)),
      }
    })
    record.baseline = JSON.parse(readFileSync(new URL('../../.lti/features/2026-10-22-scene-world-environment/features/2026-10-22-scene-physical-sky/discovery/research/baseline/perf.json', import.meta.url), 'utf8')).modes.rendered
    console.log(`W2 performance: ${JSON.stringify(record, null, 1)}`)
    await writeFile(`${dir}/w2-perf.json`, `${JSON.stringify(record, null, 2)}\n`)
    assert.deepEqual(errors, [])
    await context.close()
  } finally {
    await gpu.close()
  }
})

// --- W3 scene-night-sky: the day stays as it was ----------------------------------

const w3Baseline = new URL('../../.lti/features/2026-10-22-scene-world-environment/features/2026-10-22-scene-night-sky/discovery/research/baseline/', import.meta.url)

/**
 * Physical skies W3 must leave alone (AC-W3-6): the yard at noon (the moon is
 * 49° below the horizon) and the same with the sun at manual angles after
 * sunset, where no night sky is drawn.
 */
// The free view exposed by hand as the baseline was (Epic revision v5 meters it from the ambient light, an intended change of its
// own), and the manual sky's twilight without the night look (it works under manual angles too): what is left is
// what the night sky itself does by day.
const w3Variants = {
  'physical-noon': {
    from: 'physical-noon',
    edit(scene) {
      scene.render.exposure_mode = 'manual'
    },
  },
  'physical-manual': {
    from: 'physical-noon',
    edit(scene) {
      scene.render.exposure_mode = 'manual'
      Object.assign(scene.environment.sky, { sun_mode: 'manual', sun_elevation_deg: -8, sun_azimuth_deg: 250, night: { night_look: 0 } })
    },
  },
}

// SCENE_E2E_W3_BASELINE=<dir>: the variants' lit pictures on this machine's GPU, taken before W3 changes the renderer (w3Baseline).
if (process.env.SCENE_E2E_W3_BASELINE) await check('W3 baseline: the day’s and the manual sky’s lit pictures on this machine’s GPU', async () => {
  const dir = process.env.SCENE_E2E_W3_BASELINE
  const gpu = await chromium.launch({ headless: true, args: metal })
  try {
    const record = { machine: machineInfo(), chromium: gpu.version(), viewport: '1600 × 1000, 2× pixels', variants: {} }
    for (const [name, { from, edit }] of Object.entries(w3Variants)) {
      const files = []
      for (const img of await litPictures(gpu, from, edit)) {
        const file = img.file.replace(from, name)
        await writeFile(`${dir}/${file}`, img.png)
        files.push(file)
      }
      record.variants[name] = { from, files }
      console.log(`W3 baseline ${name}: ${files.join(', ')}`)
    }
    await writeFile(`${dir}/baseline.json`, `${JSON.stringify(record, null, 2)}\n`)
  } finally {
    await gpu.close()
  }
})

// SCENE_E2E_W3_DAY=<dir> (AC-W3-6): the variants' lit exports and rendered viewport are what they were before
// W3, pixel for pixel, on this machine's GPU. Sheets of any image that is not go to the directory.
if (process.env.SCENE_E2E_W3_DAY) await check('W3 day unchanged: noon and the manual sky draw as before W3 on this machine’s GPU', async () => {
  const dir = process.env.SCENE_E2E_W3_DAY
  const gpu = await chromium.launch({ headless: true, args: metal })
  const off = []
  try {
    const page = await (await gpu.newContext()).newPage()
    for (const [name, { from, edit }] of Object.entries(w3Variants)) {
      for (const img of await litPictures(gpu, from, edit)) {
        const file = img.file.replace(from, name)
        const m = await compareShots(page, readFileSync(new URL(file, w3Baseline)), img.png, 2)
        if (m.error || m.over > 0) {
          off.push(`${file}: ${m.error ?? `${m.over.toFixed(4)}% of pixels off by more than 2`}`)
          if (m.sheet) await writeFile(`${dir}/${file.replace('.png', '')}.jpg`, m.sheet)
        }
      }
    }
  } finally {
    await gpu.close()
  }
  assert.deepEqual(off, [])
})

// W3 (design「程序化银河」): the Milky Way's map baked on the GPU against milkyWayAt on the CPU at a grid of
// texels; the two are one model written twice. SCENE_E2E_W3_MILKY_WAY=<dir> also saves the baked map
// (sRGB, its brightest at white) as milky-way-<backend>.png; SCENE_E2E_W2_METAL runs it on this machine's GPU.
await check('W3 milky way drift: the baked map agrees with the CPU model', async () => {
  const gpu = process.env.SCENE_E2E_W2_METAL ? await chromium.launch({ headless: true, args: metal }) : null
  const context = gpu ? await gpu.newContext() : null
  const { page, errors, context: own } = await open(new SceneServer(), context ? { context } : {})
  try {
    const out = await page.evaluate(async () => {
      const { createRenderer } = await import('/src/creative-scene/render/gpu.ts')
      const { bakeMilkyWay, milkyWayAt, MILKY_WAY_SIZE } = await import('/src/creative-scene/world/nightSky/milkyWay.ts')
      const half = (h) => {
        const e = (h & 0x7c00) >> 10
        const f = h & 0x03ff
        const sign = h & 0x8000 ? -1 : 1
        if (e === 0) return sign * 2 ** -14 * (f / 1024)
        if (e === 0x1f) return f ? NaN : sign * Infinity
        return sign * 2 ** (e - 15) * (1 + f / 1024)
      }
      const gpu = await createRenderer({ canvas: document.createElement('canvas'), output: 'direct', role: 'offscreen' })
      const started = performance.now()
      const map = bakeMilkyWay(gpu.renderer)
      const raw = await gpu.renderer.readRenderTargetPixelsAsync(map, 0, 0, map.width, map.height)
      const bakeMs = performance.now() - started
      const data = raw instanceof Uint16Array ? Float32Array.from(raw, half) : Float32Array.from(raw)
      const { w, h } = MILKY_WAY_SIZE
      const lum = (r, g, b) => 0.2126 * r + 0.7152 * g + 0.0722 * b
      const stats = (flip) => {
        let sum = 0, count = 0, max = 0, nonFinite = 0
        for (let j = 0; j < 64; j++) {
          for (let i = 0; i < 128; i++) {
            const x = Math.floor(((i + 0.5) / 128) * w)
            const y = Math.floor(((j + 0.5) / 64) * h)
            const c = milkyWayAt(((x + 0.5) / w) * 360 - 180, ((y + 0.5) / h) * 180 - 90)
            const row = flip ? h - 1 - y : y
            const k = (row * w + x) * 4
            const g = lum(data[k], data[k + 1], data[k + 2])
            if (!Number.isFinite(g)) nonFinite++
            const want = lum(...c)
            // Relative where it shows, absolute against 1 % of the peak where it is dark.
            const err = Math.abs(g - want) / Math.max(want, 0.01)
            sum += err
            count++
            max = Math.max(max, err)
          }
        }
        return { mean: sum / count, max, nonFinite, flipped: flip }
      }
      const a = stats(false)
      const b = stats(true)
      // The picture, l increasing to the left as on sky charts, north up.
      const canvas = document.createElement('canvas')
      canvas.width = w / 2
      canvas.height = h / 2
      const ctx = canvas.getContext('2d')
      const img = ctx.createImageData(w / 2, h / 2)
      const flip = b.mean < a.mean
      for (let y = 0; y < h / 2; y++) {
        for (let x = 0; x < w / 2; x++) {
          const sx = w - 1 - x * 2
          const sy = flip ? y * 2 : h - 1 - y * 2
          for (let c = 0; c < 3; c++) img.data[(y * (w / 2) + x) * 4 + c] = Math.round(255 * Math.min(1, Math.max(0, data[(sy * w + sx) * 4 + c]) ** (1 / 2.2)))
          img.data[(y * (w / 2) + x) * 4 + 3] = 255
        }
      }
      ctx.putImageData(img, 0, 0)
      const result = { backend: gpu.backend, bakeMs, ...(a.mean <= b.mean ? a : b), png: canvas.toDataURL('image/png') }
      map.dispose()
      gpu.dispose()
      return result
    })
    console.log(`W3 milky way drift (${out.backend}): mean ${(out.mean * 100).toFixed(3)}%, max ${(out.max * 100).toFixed(2)}%, baked and read in ${out.bakeMs.toFixed(0)} ms${out.flipped ? ', rows flipped' : ''}`)
    if (process.env.SCENE_E2E_W3_MILKY_WAY) await writeFile(`${process.env.SCENE_E2E_W3_MILKY_WAY}/milky-way-${out.backend}.png`, Buffer.from(out.png.split(',')[1], 'base64'))
    assert.equal(out.nonFinite, 0)
    // On average they agree to about 0.05 %; at the dust lanes' edges float32 noise through exp(−τ) reaches about 12 % (both backends).
    assert.ok(out.mean < 0.005 && out.max < 0.15, `mean ${out.mean}, max ${out.max}`)
    assert.deepEqual(errors, [])
  } finally {
    await (context ?? own).close()
    await gpu?.close()
  }
})

// W3 (design「星星」): Beijing at 22:00 on 2026-10-26, the free view at Polaris where Skyfield puts it
// (scripts/fixtures/astro, refracted as astronomy-engine does): the star is the brightest point by the
// view's centre, within 0.2°. SwiftShader (WebGL2), and this machine's GPU (WebGPU) with SCENE_E2E_W2_METAL;
// SCENE_E2E_W3_STARS=<dir> saves the pictures.
await check('W3 stars: Polaris drawn where the sky puts it, at the view’s centre', async () => {
  const polaris = JSON.parse(readFileSync(new URL('fixtures/astro/skyfield-stars.json', import.meta.url), 'utf8')).points.find((p) => p.star === 'Polaris' && p.location === 'Beijing')
  assert.equal(polaris.utc, '2026-10-26T14:00:00Z')
  const elevation = polaris.elevation + Refraction('normal', polaris.elevation)
  const d = sunVector(elevation, documentAzimuth(polaris.azimuth, 180))
  // View space (Z up): document (x, y, z) is (x, −z, y). The camera stands where the yard's does.
  const position = [0, -4, 1.6]
  const target = [position[0] + d.x * 50, position[1] - d.z * 50, position[2] + d.y * 50]
  const view = { camera: { position, target, ortho: false, zoom: 1 }, selection: { selected: [], active: null }, tool: 'box', toolbar: true, sidebar: true, history: false }
  const gpu = process.env.SCENE_E2E_W2_METAL ? await chromium.launch({ headless: true, args: metal }) : null
  try {
    for (const [backend, b] of [['webgl2', browser], ...(gpu ? [['webgpu', gpu]] : [])]) {
      const context = await b.newContext({ viewport: { width: 1280, height: 820 } })
      await context.addInitScript(([key, v]) => localStorage.setItem(key, JSON.stringify(v)), [`creative-scene-view:qa-account:${DOC}`, view])
      const server = new SceneServer()
      referenceScene(server, 'physical-noon', (scene) => {
        Object.assign(scene.environment.sky, { latitude_deg: 39.9042, longitude_deg: 116.4074, timezone: 'Asia/Shanghai', place: '北京', date: '2026-10-26', local_time: '22:00' })
        scene.environment.sky.night = { moon: false, stars: true, milky_way: true, moon_strength: 1, moon_temperature_k: 6500, stars_strength: 1, stars_temperature_k: 6500, night_look: 1 }
        Object.assign(scene.render, { exposure_mode: 'manual', exposure_ev100: -2 })
      })
      const { page, errors } = await open(server, { context })
      await shading(page, '最终渲染')
      const { box } = await viewportCenter(page)
      let star = null
      await waitFor(async () => {
        const png = await settledShot(page, box)
        star = await page.evaluate(async (src) => {
          const img = await new Promise((resolve) => { const i = new Image(); i.onload = () => resolve(i); i.src = src })
          const c = document.createElement('canvas')
          c.width = img.width
          c.height = img.height
          const g = c.getContext('2d')
          g.drawImage(img, 0, 0)
          const p = g.getImageData(0, 0, c.width, c.height).data
          const at = (x, y) => p[(y * c.width + x) * 4] + p[(y * c.width + x) * 4 + 1] + p[(y * c.width + x) * 4 + 2]
          const [cx, cy] = [c.width / 2, c.height / 2]
          // The brightest pixel within 40 px of the centre, then the light-weighted centre about it over the background.
          let best = { x: 0, y: 0, v: -1 }
          for (let y = Math.floor(cy - 40); y <= cy + 40; y++) for (let x = Math.floor(cx - 40); x <= cx + 40; x++) if (at(x, y) > best.v) best = { x, y, v: at(x, y) }
          let background = 0
          for (let x = -40; x <= 40; x++) background += at(Math.floor(cx) + x, Math.floor(cy) - 40)
          background /= 81
          let [w, sx, sy] = [0, 0, 0]
          for (let y = best.y - 4; y <= best.y + 4; y++) for (let x = best.x - 4; x <= best.x + 4; x++) {
            const v = Math.max(0, at(x, y) - background)
            w += v
            sx += v * x
            sy += v * y
          }
          // Pixel x covers x to x + 1.
          return { x: sx / w + 0.5, y: sy / w + 0.5, cx, cy, peak: best.v, background, height: c.height, png: src }
        }, `data:image/png;base64,${png.toString('base64')}`)
        return star.peak - star.background > 60
      }, `${backend}: a star by the centre`, 30_000)
      if (process.env.SCENE_E2E_W3_STARS) await writeFile(`${process.env.SCENE_E2E_W3_STARS}/polaris-${backend}.png`, Buffer.from(star.png.split(',')[1], 'base64'))
      // The free view's vertical field of view is 45°.
      const pxPerDeg = star.height / (2 * Math.tan((22.5 * Math.PI) / 180)) * (Math.PI / 180)
      const offDeg = Math.hypot(star.x - star.cx, star.y - star.cy) / pxPerDeg
      console.log(`W3 stars ${backend}: Polaris at (${star.x.toFixed(1)}, ${star.y.toFixed(1)}), the centre (${star.cx}, ${star.cy}): ${offDeg.toFixed(3)}° off; peak ${star.peak} over ${star.background.toFixed(0)}`)
      assert.ok(offDeg <= 0.2, `${backend}: ${offDeg.toFixed(3)}° off`)
      assert.deepEqual(errors, [])
      await context.close()
    }
  } finally {
    await gpu?.close()
  }
})

// W3 (design「光照」): Beijing on 2026-10-26, the full moon. From 17:40 (the sun just down, still the shadowing
// light) to 18:40 (the moon takes it) and 22:00 (the moon 54° up): no shader program is built, so nightfall
// compiles nothing; at 22:00 the yard at EV100 −3 is seen, about as noon at sunny 16 (0.22 lux of moonlight).
await check('W3 dusk: by default the picture dims steadily from late afternoon to night, without going dark and jumping back', async () => {
  // The meter and the drawn sky once disagreed by up to 6 stops with the sun 7–11° down: dark at 18:10, bright again at 18:20 (owner 2026-10-23).
  // The low sun's darkening holds from about 4° up until the night's takes over, so the afterglow may lift the picture a little (up to about 4).
  const beijing = { latitude_deg: 39.9042, longitude_deg: 116.4074, timezone: 'Asia/Shanghai', place: '北京', date: '2026-10-26' }
  const { context, page, errors, settle, change, sky } = await physicalYard('physical-noon', (scene) => {
    Object.assign(scene.environment.sky, beijing, { local_time: '17:00' })
    Object.assign(scene.render, { exposure_mode: 'ambient', exposure_compensation: 0 })
  })
  const rows = [[ '17:00', (await lumaStats(page, await settle(), { level: 250 })).mean ]]
  for (let m = 17 * 60 + 5; m <= 19 * 60; m += 5) {
    const t = `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`
    rows.push([t, (await lumaStats(page, await change(sky({ local_time: t })), { level: 250 })).mean])
  }
  console.log(`W3 dusk: ${rows.map(([t, v]) => `${t} ${v.toFixed(0)}`).join(', ')}`)
  for (let i = 1; i < rows.length; i++) {
    const step = rows[i][1] - rows[i - 1][1]
    assert.ok(step > -20 && step < 5, `${rows[i - 1][0]} → ${rows[i][0]}: ${step.toFixed(1)}`)
  }
  assert.ok(Math.min(...rows.map(([, v]) => v)) > 70, 'never dark')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('W3 moonlight: by default the full moon’s yard is seen, darker than noon; by hand at EV100 −3 too; nightfall builds no shader', async () => {
  const beijing = { latitude_deg: 39.9042, longitude_deg: 116.4074, timezone: 'Asia/Shanghai', place: '北京', date: '2026-10-26' }
  // The default camera as a new scene has it (Epic revision v5): ambient metering, no compensation.
  const { context, page, errors, settle, change, sky } = await physicalYard('physical-noon', (scene) => {
    Object.assign(scene.environment.sky, beijing, { local_time: '12:00' })
    Object.assign(scene.render, { exposure_mode: 'ambient', exposure_compensation: 0 })
  })
  const programs = () => page.evaluate(() => [...window.__sceneGPU.live].filter((h) => h.role === 'viewport').reduce((n, h) => n + h.programs, 0))
  const noon = await lumaStats(page, await settle(), { level: 250 })
  await change(sky({ local_time: '17:40' }))
  const before = await programs()
  await change(sky({ local_time: '18:40' }))
  const night = await lumaStats(page, await change(sky({ local_time: '22:00' })), { level: 250 })
  const after = await programs()
  await shot(page, 'w3-full-moon-auto')
  const manual = await lumaStats(page, await change([{ op: 'set_render', render: { exposure_mode: 'manual', exposure_ev100: -3 } }]), { level: 250 })
  await shot(page, 'w3-full-moon-ev-3')
  const rgb = (l) => [l.r, l.g, l.b].map((v) => v.toFixed(0))
  console.log(`W3 moonlight: noon mean ${noon.mean.toFixed(0)}; 22:00 ambient ${night.mean.toFixed(0)} (rgb ${rgb(night)}), clipped ${(night.bright * 100).toFixed(2)}%; by hand at −3 ${manual.mean.toFixed(0)} (rgb ${rgb(manual)}); programs ${before} → ${after}`)
  assert.equal(after, before, 'no shader program built from dusk to night')
  assert.ok(night.mean > 40 && night.mean < noon.mean - 20 && night.bright < 0.02, 'metered from the ambient light the yard is seen, darker than at noon')
  assert.ok(manual.mean > 60 && manual.mean < 220 && manual.bright < 0.02, 'by hand at −3 the yard is seen: neither dark nor blown')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('W3 night look: by a full moon it turns the yard blue at the same brightness; at noon it does nothing', async () => {
  const beijing = { latitude_deg: 39.9042, longitude_deg: 116.4074, timezone: 'Asia/Shanghai', place: '北京', date: '2026-10-26' }
  const { context, page, errors, settle, change, sky, server } = await physicalYard('physical-noon', (scene) => {
    Object.assign(scene.environment.sky, beijing, { local_time: '22:00' })
    Object.assign(scene.render, { exposure_mode: 'ambient', exposure_compensation: 0 })
  })
  const look = (v) => sky({ night: { ...server.scene.environment.sky.night, night_look: v } })
  const on = await lumaStats(page, await settle(), { level: 250 })
  await shot(page, 'w3-night-look-1')
  const off = await lumaStats(page, await change(look(0)), { level: 250 })
  await shot(page, 'w3-night-look-0')
  const noonOff = await change([...sky({ local_time: '12:00' })])
  const noonOn = await change(look(1))
  const noon = await compareShots(page, noonOff, noonOn, 0)
  const rgb = (l) => [l.r, l.g, l.b].map((v) => v.toFixed(0))
  console.log(`W3 night look: 22:00 at 1 mean ${on.mean.toFixed(1)} (rgb ${rgb(on)}), at 0 ${off.mean.toFixed(1)} (rgb ${rgb(off)}); noon off by ${noon.error ?? `${noon.over}%`}`)
  assert.ok(Math.abs(on.mean / off.mean - 1) < 0.03, 'the same brightness')
  assert.ok(on.b / on.r > (off.b / off.r) * 1.1, 'bluer')
  assert.ok(!noon.error && noon.over === 0, 'nothing at noon')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('W3 new moon: by default the moonless yard is darker than the full moon’s yet not black; the same night draws alike twice', async () => {
  const beijing = { latitude_deg: 39.9042, longitude_deg: 116.4074, timezone: 'Asia/Shanghai', place: '北京', local_time: '22:00' }
  const { context, page, errors, settle, change, sky } = await physicalYard('physical-noon', (scene) => {
    Object.assign(scene.environment.sky, beijing, { date: '2026-10-26' })
    Object.assign(scene.render, { exposure_mode: 'ambient', exposure_compensation: 0 })
  })
  const full = await lumaStats(page, await settle(), { level: 250 })
  const first = await change(sky({ date: '2026-11-09' }))
  await shot(page, 'w3-new-moon-auto')
  const again = await change(sky({ local_time: '22:00' }))
  const moonless = await lumaStats(page, first, { level: 250 })
  const twice = await compareShots(page, first, again, 0)
  console.log(`W3 new moon: full moon mean ${full.mean.toFixed(0)}, new moon ${moonless.mean.toFixed(0)}; drawn twice off by ${twice.error ?? `${twice.over}%`}`)
  assert.ok(moonless.mean < full.mean && moonless.mean > 20, 'darker than the full moon, yet not black')
  assert.ok(!twice.error && twice.over === 0, 'the same night draws alike')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('W3 environment tab: the night sky’s switches, strengths and look; the moon’s day with a jump to a new moon and back; the exposure modes of a camera and of the default camera', async () => {
  const { server, context, page, errors, sky, time } = await physicalTab()
  const city = page.getByRole('combobox', { name: '城市' })
  await city.fill('北京')
  await page.getByRole('option', { name: /北京/ }).first().click()
  await waitFor(() => sky().place === '北京', 'Beijing')
  await page.getByLabel('日期').fill('2026-10-26')
  await waitFor(() => sky().date === '2026-10-26', 'the date')
  await time('22:00')
  const moonDay = page.getByRole('region', { name: '月亮的一天' })
  assert.equal(await moonDay.getByRole('status', { name: '月相' }).textContent(), '满月')
  assert.match(await moonDay.getByRole('status', { name: '月亮照亮比例' }).textContent(), /^(9[5-9]|100)%$/)
  await saved(page)
  await moonDay.getByRole('button', { name: /^下一个新月/ }).click()
  await waitFor(() => sky().date === '2026-11-09', 'the next new moon')
  assert.equal(await moonDay.getByRole('status', { name: '月相' }).textContent(), '新月')
  await saved(page)
  await focusEditor(page)
  await page.keyboard.press(`${modKey}+KeyZ`)
  await waitFor(() => sky().date === '2026-10-26', 'undo goes back to the full moon')

  const night = page.getByRole('group', { name: '夜空' })
  await night.getByRole('checkbox', { name: '月亮' }).uncheck()
  await waitFor(() => sky().night.moon === false, 'the moon off')
  await night.getByRole('checkbox', { name: '月亮' }).check()
  await waitFor(() => sky().night.moon === true, 'the moon on')
  for (const [label, key, value] of [['月亮亮度', 'moon_strength', 2], ['星空亮度', 'stars_strength', 0.5], ['夜色', 'night_look', 0.5]]) {
    const field = night.getByRole('spinbutton', { name: label })
    await field.fill(String(value))
    await field.press('Enter')
    await waitFor(() => sky().night[key] === value, `${key} written`)
  }
  await page.getByRole('group', { name: '太阳位置' }).getByRole('button', { name: '手动角度' }).click()
  await waitFor(() => sky().sun_mode === 'manual', 'manual angles')
  assert.ok(await night.getByRole('checkbox', { name: '银河' }).isDisabled(), 'the night sky greyed under manual angles')
  assert.ok(await night.getByRole('spinbutton', { name: '夜色' }).isEnabled(), 'the night look still works')
  await night.getByText('只在「地点和时间」下生效').waitFor()

  // A camera stored before the exposure modes reads as manual; switched to ambient metering it shows the compensation.
  await page.getByRole('treeitem', { name: /shot/ }).click()
  const exposure = page.getByRole('group', { name: '曝光', exact: true })
  const camera = server.scene.objects.find((o) => o.kind === 'camera')
  assert.equal(await exposure.getByRole('combobox', { name: 'ISO' }).count(), 1, 'manual: ISO and shutter')
  await exposure.getByRole('group', { name: '曝光模式' }).getByRole('button', { name: '环境测光' }).click()
  await waitFor(() => server.object(camera.id).camera.exposure_mode === 'ambient', 'the camera metered from the ambient light')
  assert.equal(await exposure.getByRole('combobox', { name: 'ISO' }).count(), 0, 'ambient metering: no ISO')
  const compensation = exposure.getByRole('spinbutton', { name: '曝光补偿（档）' })
  await compensation.fill('1')
  await compensation.press('Enter')
  await waitFor(() => server.object(camera.id).camera.exposure_compensation === 1, 'the compensation written')
  await page.keyboard.press('Escape')
  await page.getByRole('tablist', { name: '属性' }).getByRole('tab', { name: '渲染', exact: true }).click()
  const modes = page.getByRole('group', { name: '默认摄像机曝光' })
  assert.equal(await modes.getByRole('button', { name: '环境测光' }).getAttribute('aria-pressed'), 'true', 'the default camera on ambient metering')
  const fill = async (label, value) => {
    const f = page.getByRole('spinbutton', { name: label })
    await f.fill(String(value))
    await f.press('Enter')
  }
  await fill('环境测光补偿（档）', -1)
  await waitFor(() => server.scene.render.exposure_compensation === -1, 'the default compensation written')
  await modes.getByRole('button', { name: '手动' }).click()
  await waitFor(() => server.scene.render.exposure_mode === 'manual', 'the default camera by hand')
  await page.getByText(/银河 −7/).waitFor()
  assert.ok(await page.getByRole('spinbutton', { name: '默认曝光（EV100）' }).isEnabled())
  assert.deepEqual(errors, [])
  await context.close()
})

// SCENE_E2E_W3_PERF=<dir> (AC-W3-10), this machine's GPU at 1600 × 1000, 2× pixels: AC-9's scene under the
// physical sky at night (Shanghai, 2026-09-23 at 21:00, a gibbous moon up). Frame times standing still, and
// dragging the time of day a minute a frame from 19:00 for 3 s. Written to w3-perf-<SCENE_E2E_W3_PERF_LABEL>.json.
/** Per pixel between two pictures of one view, from the row at share `from` of its height down: how many changed (by more than 2), the changed pixels' mean colour in each, and the top row's largest change. */
async function pixelChange(page, a, b, from = 0) {
  return page.evaluate(async ([a, b, from]) => {
    const load = (src) => new Promise((resolve) => { const i = new Image(); i.onload = () => resolve(i); i.src = src })
    const [x, y] = await Promise.all([load(a), load(b)])
    const pixels = (img) => {
      const c = document.createElement('canvas')
      c.width = img.width
      c.height = img.height
      const g = c.getContext('2d')
      g.drawImage(img, 0, 0)
      return g.getImageData(0, 0, img.width, img.height).data
    }
    const p = pixels(x)
    const q = pixels(y)
    const before = [0, 0, 0]
    const after = [0, 0, 0]
    let changed = 0
    let top = 0
    const start = Math.floor(x.height * from) * x.width * 4
    for (let k = start; k < p.length; k += 4) {
      const most = Math.max(Math.abs(p[k] - q[k]), Math.abs(p[k + 1] - q[k + 1]), Math.abs(p[k + 2] - q[k + 2]))
      if (k < x.width * 4) top = Math.max(top, most)
      if (most <= 2) continue
      changed++
      for (let c = 0; c < 3; c++) {
        before[c] += p[k + c]
        after[c] += q[k + c]
      }
    }
    const n = Math.max(changed, 1)
    return { changed: changed / ((p.length - start) / 4), before: before.map((v) => v / n), after: after.map((v) => v / n), top }
  }, [`data:image/png;base64,${a.toString('base64')}`, `data:image/png;base64,${b.toString('base64')}`, from])
}

// W4 (design「关键机制」): the yard at noon, its surfaces a few metres to some tens away. At the true distance the air
// between does next to nothing; × 1000 they stand kilometres off and go blue, grey in haze, warm at dusk; off, the
// picture is W3's. The sky's background is the same throughout.
await check('W4 far: the aerial perspective blues and pales the yard as the distance scale grows; off it is gone; the sky is left alone', async () => {
  const { context, page, errors, settle, change, sky, server } = await physicalYard('physical-noon', (scene) => {
    Object.assign(scene.render, { exposure_mode: 'ambient', exposure_compensation: 0 })
  })
  const aerial = (patch) => sky({ aerial_perspective: { ...server.scene.environment.sky.aerial_perspective, ...patch } })
  const on1 = await settle()
  const off = await change(aerial({ enabled: false }))
  await shot(page, 'w4-off')
  const on100 = await change(aerial({ enabled: true, distance_scale: 100 }))
  const on1000 = await change(aerial({ distance_scale: 1000 }))
  await shot(page, 'w4-1000')
  // AC-W4-8: the same picture drawn again is the same, pixel for pixel.
  const again = await compareShots(page, on1000, await settle(), 0)
  assert.ok(!again.error && again.over === 0, `drawn again: ${again.error ?? `${again.over}% off`}`)
  const hazy = await change(sky({ dust_density: 12 }))
  const hazyOff = await change(aerial({ enabled: false }))
  const duskOff = await change(sky({ dust_density: 1, local_time: '17:45' }))
  const dusk = await change(aerial({ enabled: true }))
  const at1 = await pixelChange(page, off, on1)
  const at100 = await pixelChange(page, off, on100)
  const at1000 = await pixelChange(page, off, on1000)
  const atHaze = await pixelChange(page, hazyOff, hazy)
  const atDusk = await pixelChange(page, duskOff, dusk)
  const fmt = (c) => `${(c.changed * 100).toFixed(1)}% changed, rgb ${c.before.map((v) => v.toFixed(0))} → ${c.after.map((v) => v.toFixed(0))}, top row ${c.top}`
  console.log(`W4 far: ×1 ${fmt(at1)}; ×100 ${fmt(at100)}; ×1000 ${fmt(at1000)}; hazy ×1000 ${fmt(atHaze)}; dusk ×1000 ${fmt(atDusk)}`)
  const blue = (rgb) => rgb[2] / rgb[0]
  assert.ok(at1.changed < 0.005, 'at the true distance a yard shows next to nothing')
  assert.ok(at100.changed > 0.1 && at1000.changed > 0.6, 'more of the yard changes as the scale grows')
  assert.ok(blue(at1000.after) > blue(at1000.before) * 1.05, 'bluer at noon')
  // Haze scatters little sideways and away from the sun but takes much out: greyer, not bluer (not brighter unless facing the sun).
  assert.ok(blue(atHaze.after) < blue(at1000.after), 'greyer in haze than in clear air')
  assert.ok(blue(atDusk.after) < blue(atDusk.before), 'warmer at dusk')
  for (const c of [at1, at100, at1000, atHaze, atDusk]) assert.equal(c.top, 0, 'the sky at the top is left alone')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('W4 real-time GI: the aerial perspective blues the yard there too, after the GI and the fog', async () => {
  const { context, page, errors, settle, change, sky, server } = await physicalYard('physical-noon', (scene) => {
    Object.assign(scene.render, { exposure_mode: 'ambient', exposure_compensation: 0, engine: 'realtime_gi' })
    scene.environment.sky.aerial_perspective = { enabled: true, distance_scale: 1000 }
  })
  const on = await lumaStats(page, await settle(), { level: 250 })
  await shot(page, 'w4-gi-1000')
  const off = await lumaStats(page, await change(sky({ aerial_perspective: { ...server.scene.environment.sky.aerial_perspective, enabled: false } })), { level: 250 })
  console.log(`W4 real-time GI: ×1000 rgb ${[on.r, on.g, on.b].map((v) => v.toFixed(0))}, off ${[off.r, off.g, off.b].map((v) => v.toFixed(0))}`)
  assert.ok(on.b / on.r > (off.b / off.r) * 1.03, 'bluer with the aerial perspective')
  assert.deepEqual(errors, [])
  await context.close()
})

await check('W4 environment tab: the aerial perspective’s switch and distance scale, the scale greyed while off, undo', async () => {
  const { context, page, errors, sky } = await physicalTab()
  const group = page.getByRole('group', { name: '空气透视' })
  const scale = group.getByRole('spinbutton', { name: '距离倍率' })
  assert.equal(await scale.inputValue(), '1')
  await scale.fill('100')
  await scale.press('Enter')
  await waitFor(() => sky().aerial_perspective.distance_scale === 100, 'the scale written')
  // The slider is on a log scale: its middle, 10^1, is ×10.
  const slider = group.getByRole('slider', { name: '距离倍率' })
  await slider.fill('1')
  await waitFor(() => sky().aerial_perspective.distance_scale === 10, 'the slider writes ×10 at its 10^1')
  await group.getByRole('checkbox', { name: '空气透视' }).uncheck()
  await waitFor(() => sky().aerial_perspective.enabled === false, 'off')
  assert.ok(await scale.isDisabled(), 'the scale greyed while off')
  await saved(page)
  await focusEditor(page)
  await page.keyboard.press(`${modKey}+KeyZ`)
  await waitFor(() => sky().aerial_perspective.enabled === true, 'undo turns it back on')
  assert.deepEqual(errors, [])
  await context.close()
})

// fix/scene-sun-cascaded-shadows: one shadow map over every caster spent its texels on a big box far off, and the
// shadows in the yard went jagged and came off their casters. In cascades, the map by the camera covers the yard
// alone: the box 200 m off changes nothing in the lower half of the view, where the yard's shadows are.
await check('sun shadows: a big box 200 m off leaves the yard’s shadows as they were', async () => {
  const { context, page, errors, settle, change } = await physicalYard('physical-noon', (scene) => {
    Object.assign(scene.render, { exposure_mode: 'ambient', exposure_compensation: 0 })
  })
  const before = await settle()
  await shot(page, 'sun-shadow-yard')
  const tower = { id: gid('tower'), kind: 'primitive', name: '远处大楼', parent_id: null, order: 90, transform: { ...identity(), position: [-150, 0, -180] }, visible: true, locked: false, primitive: { builtin: 'box@1', params: { width: 40, height: 40, depth: 40 } } }
  const after = await change([{ op: 'add_objects', objects: [tower] }])
  await shot(page, 'sun-shadow-tower')
  const lower = await pixelChange(page, before, after, 0.5)
  console.log(`sun shadows: the tower changes ${(lower.changed * 100).toFixed(3)}% of the lower half`)
  assert.ok(lower.changed < 0.001, `the yard's shadows as they were (${(lower.changed * 100).toFixed(3)}% changed)`)
  assert.deepEqual(errors, [])
  await context.close()
})

// AC-W4-5, this machine's GPU: the yard looking up at × 1000 exported on WebGPU and through the WebGL2 backend. W2's
// limits, or where W2's own picture is already over them (physical-noon-shot, accepted in W2), no further off than
// the same picture without the aerial perspective.
if (process.env.SCENE_E2E_W4_GPU) await check('W4 parity: the aerial perspective on WebGPU and on the WebGL2 backend', async () => {
  const dir = process.env.SCENE_E2E_W4_GPU
  const gpu = await chromium.launch({ headless: true, args: metal })
  const rows = []
  const over = []
  const edit = (enabled) => (scene) => {
    lookingUp(scene)
    scene.environment.sky.aerial_perspective = { enabled, distance_scale: 1000 }
  }
  try {
    const page = await (await gpu.newContext()).newPage()
    for (const name of ['physical-noon', 'physical-dusk']) {
      const pair = async (enabled) => {
        const webgpu = await exportReference(gpu, name, { edit: edit(enabled) })
        const webgl = await exportReference(gpu, name, { edit: edit(enabled), forceWebGL: true })
        assert.ok(webgl.backends.some((b) => b === 'scene renderer (offscreen): webgl2'), `${name}: ${webgl.backends}`)
        const out = {}
        for (const img of webgl.images.filter((i) => i.pass === 'lit')) out[img.file] = { m: await compareShots(page, webgpu.images.find((i) => i.file === img.file).png, img.png), file: img.file }
        return out
      }
      const on = await pair(true)
      const off = await pair(false)
      for (const [file, { m }] of Object.entries(on)) {
        const without = off[file].m
        const within = !m.error && m.mean <= parityLimits.lit.mean && m.over <= parityLimits.lit.over
        const noWorse = !m.error && !without.error && m.mean <= without.mean + 0.1 && m.over <= without.over + 0.05
        const ok = within || noWorse
        if (m.sheet) await writeFile(`${dir}/${file.replace('.png', '')}-w4-webgl2.jpg`, m.sheet)
        rows.push({ file, mean: m.error ? null : Number(m.mean.toFixed(4)), over_percent: m.error ? null : Number(m.over.toFixed(4)), without: without.error ? null : { mean: Number(without.mean.toFixed(4)), over_percent: Number(without.over.toFixed(4)) }, ok })
        if (!ok) over.push(`${file}: ${m.error ?? `mean ${m.mean.toFixed(3)}, > 8 ${m.over.toFixed(3)}%`}`)
        console.log(`W4 parity ${file}: mean ${m.mean?.toFixed(3)}, > 8 ${m.over?.toFixed(4)}%; without ${without.mean?.toFixed(3)}, ${without.over?.toFixed(4)}%${ok ? '' : ' (over)'}`)
      }
    }
    await writeFile(`${dir}/w4-parity.json`, `${JSON.stringify({ machine: machineInfo(), chromium: gpu.version(), limits: parityLimits.lit, rows }, null, 2)}\n`)
  } finally {
    await gpu.close()
  }
  assert.deepEqual(over, [], 'over the threshold: for the owner to look at')
})

if (process.env.SCENE_E2E_W3_PERF) await check('W3 performance: the physical sky at night on this machine’s GPU', async () => {
  const dir = process.env.SCENE_E2E_W3_PERF
  const label = process.env.SCENE_E2E_W3_PERF_LABEL ?? 'after'
  const gpu = await chromium.launch({ headless: true, args: metal })
  try {
    const context = await gpu.newContext({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 })
    const server = new SceneServer()
    perfScene(server)
    const physical = JSON.parse(readFileSync(new URL('fixtures/scene-reference/physical-noon.json', import.meta.url), 'utf8')).environment.sky
    server.scene.environment.sky = { ...physical, local_time: '21:00' }
    // W4: SCENE_E2E_W3_PERF_AERIAL=off measures without the aerial perspective (AC-W4-9 against it on, the default).
    if (process.env.SCENE_E2E_W3_PERF_AERIAL === 'off') server.scene.environment.sky.aerial_perspective = { enabled: false, distance_scale: 1 }
    server.scene.render.exposure_ev100 = -3
    const { page, errors } = await open(server, { context })
    const { x, y } = await viewportCenter(page)
    await page.mouse.move(x, y)
    await shading(page, '最终渲染')
    await waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiling)) === 0, 'compiled', 60_000)
    await sleep(1500)
    const record = { machine: machineInfo(), chromium: gpu.version(), viewport: '1600 × 1000, 2× pixels', scene: 'AC-9 (about 200 objects, 4 lights) under the physical sky, Shanghai 2026-09-23 at 21:00, EV100 −3', label }
    await orbitFrames(page, 1000)
    record.frames = frameStats(await orbitFrames(page, 4000))
    const drag = await page.evaluate(() => new Promise((resolve) => {
      const slider = document.querySelector('input[type=range][aria-label="当地时刻"]')
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set
      const times = []
      let minute = 19 * 60
      const start = performance.now()
      const tick = (t) => {
        times.push(t)
        set.call(slider, String(minute++))
        slider.dispatchEvent(new Event('input', { bubbles: true }))
        if (t - start < 3000) requestAnimationFrame(tick)
        else resolve({ times })
      }
      requestAnimationFrame(tick)
    }))
    record.drag = frameStats(drag.times)
    record.requests = await page.evaluate(() => performance.getEntriesByType('resource').map((r) => r.name).filter((n) => /nightSky|stars|moon/.test(n)))
    console.log(`W3 performance: ${JSON.stringify(record, null, 1)}`)
    await writeFile(`${dir}/w3-perf-${label}.json`, `${JSON.stringify(record, null, 2)}\n`)
    assert.deepEqual(errors, [])
    await context.close()
  } finally {
    await gpu.close()
  }
})

// --- R2: the real-time GI ---------------------------------------------------------

// The offscreen shooter on its own: shots in a row, and what its renderer holds after each.
const giFixture = new URL('../.scene-gi-qa.html', import.meta.url)
await writeFile(
  giFixture,
  `<html><body><script>window.__sceneTest = true</script><script type="module">
import { openShooter } from '/src/creative-scene/host/render.ts';
import { cameraFraming } from '/src/creative-scene/host/framing.ts';
window.__shots = async (scene, n, side) => {
  const shooter = await openShooter(scene, {}, 'lit');
  const framing = cameraFraming(scene, scene.objects.find((o) => o.kind === 'camera').id, side);
  const held = [];
  try {
    for (let i = 0; i < n; i++) {
      // render_view's frame count: the pipeline is the same, a quarter of the frames on SwiftShader.
      await shooter.shoot(framing, { kind: 'lit', type: 'image/png', use: 'view' });
      const { textures, renderTargets, uniformBuffers, programs } = [...window.__sceneGPU.live].filter((h) => h.role === 'offscreen').at(-1).info.memory;
      held.push({ textures, renderTargets, uniformBuffers, programs });
    }
  } finally {
    shooter.close();
  }
  return held;
};
// One picture drawn while a background-ticker heartbeat beats every second (the executor's lease renewal is
// one): the beats and the longest gap between them.
window.__heartbeat = async (scene, kind, side) => {
  const { backgroundTicker } = await import('/src/creative-scene/host/ticker.ts');
  const shooter = await openShooter(scene, {}, kind === 'lit' ? 'lit' : 'clay');
  const beats = [performance.now()];
  const stop = backgroundTicker().every(1000, () => beats.push(performance.now()));
  try {
    const framing = cameraFraming(scene, scene.objects.find((o) => o.kind === 'camera').id, side);
    const started = performance.now();
    const blob = await shooter.shoot(framing, { kind, type: 'image/png' });
    const ms = performance.now() - started;
    beats.push(performance.now());
    const gaps = beats.slice(1).map((t, i) => t - beats[i]);
    return { ms, beats: beats.length - 2, longest: Math.max(...gaps), width: framing.width, height: framing.height, bytes: blob.size };
  } finally {
    stop();
    shooter.close();
  }
};
// An export's images as drawn (host/exportShots.ts renderShots): each one's encoding and size.
window.__sizes = async (doc, plan) => {
  const { renderShots } = await import('/src/creative-scene/host/exportShots.ts');
  const { loadModels } = await import('/src/creative-scene/host/render.ts');
  const out = await renderShots({ read: async () => doc, loadModels: (d) => loadModels(d.scene, d.models), open: (d, look) => openShooter(d.scene, d.models, look) }, 'doc', plan);
  if (!out.ok) return out;
  return Promise.all(out.images.map(async (i) => {
    const bitmap = await createImageBitmap(i.blob);
    return { camera: i.camera, pass: i.pass, type: i.blob.type, width: bitmap.width, height: bitmap.height };
  }));
};
// One picture's SHA-256: what the renderer in effect draws for a pass.
window.__hash = async (scene, kind, side) => {
  const shooter = await openShooter(scene, {}, kind === 'lit' ? 'lit' : 'clay');
  try {
    const framing = cameraFraming(scene, scene.objects.find((o) => o.kind === 'camera').id, side);
    const blob = await shooter.shoot(framing, { kind, type: 'image/png', use: 'view' });
    return [...new Uint8Array(await crypto.subtle.digest('SHA-256', await blob.arrayBuffer()))].map((b) => b.toString(16).padStart(2, '0')).join('');
  } finally {
    shooter.close();
  }
};
// One export picture's residual noise (R0's measure, R2 tasks 5.1): mean |grey − its Gaussian blur, σ 6 px at
// 2048| over a box given as fractions of the picture; and how long the picture took.
window.__noise = async (scene, kind, side, box) => {
  const shooter = await openShooter(scene, {}, kind === 'lit' ? 'lit' : 'clay');
  try {
    const framing = cameraFraming(scene, scene.objects.find((o) => o.kind === 'camera').id, side);
    const started = performance.now();
    const blob = await shooter.shoot(framing, { kind, type: 'image/png' });
    const ms = performance.now() - started;
    const bitmap = await createImageBitmap(blob);
    const canvas = new OffscreenCanvas(bitmap.width, bitmap.height);
    const c2d = canvas.getContext('2d');
    c2d.drawImage(bitmap, 0, 0);
    const [x0, y0, x1, y1] = [box[0] * bitmap.width, box[1] * bitmap.height, box[2] * bitmap.width, box[3] * bitmap.height].map(Math.round);
    const w = x1 - x0, h = y1 - y0;
    const rgba = c2d.getImageData(x0, y0, w, h).data;
    const grey = new Float32Array(w * h);
    for (let i = 0; i < w * h; i++) grey[i] = Math.round(0.299 * rgba[4 * i] + 0.587 * rgba[4 * i + 1] + 0.114 * rgba[4 * i + 2]);
    const sigma = (6 * bitmap.width) / 2048, r = Math.ceil(3 * sigma);
    const kernel = Array.from({ length: 2 * r + 1 }, (_, i) => Math.exp(-((i - r) ** 2) / (2 * sigma * sigma)));
    const sum = kernel.reduce((a, b) => a + b);
    const blur = (src, step, n, count, stride) => {
      const out = new Float32Array(src.length);
      for (let line = 0; line < count; line++) for (let i = 0; i < n; i++) {
        let v = 0;
        for (let k = -r; k <= r; k++) v += kernel[k + r] * src[line * stride + Math.min(n - 1, Math.max(0, i + k)) * step];
        out[line * stride + i * step] = v / sum;
      }
      return out;
    };
    const blurred = blur(blur(grey, 1, w, h, w), w, h, w, 1);
    let total = 0;
    for (let i = 0; i < w * h; i++) total += Math.abs(grey[i] - blurred[i]);
    return { noise: total / (w * h), ms, width: framing.width, height: framing.height };
  } finally {
    shooter.close();
  }
};
// One export picture (R2 AC-R2-3): the mean colour of boxes given as fractions of it, and the picture as a JPEG.
window.__regions = async (scene, kind, side, boxes) => {
  const shooter = await openShooter(scene, {}, kind === 'lit' ? 'lit' : 'clay');
  try {
    const framing = cameraFraming(scene, scene.objects.find((o) => o.kind === 'camera').id, side);
    const blob = await shooter.shoot(framing, { kind, type: 'image/png' });
    const bitmap = await createImageBitmap(blob);
    const canvas = new OffscreenCanvas(bitmap.width, bitmap.height);
    const c2d = canvas.getContext('2d');
    c2d.drawImage(bitmap, 0, 0);
    const means = boxes.map((box) => {
      const [x0, y0, x1, y1] = [box[0] * bitmap.width, box[1] * bitmap.height, box[2] * bitmap.width, box[3] * bitmap.height].map(Math.round);
      const d = c2d.getImageData(x0, y0, x1 - x0, y1 - y0).data;
      const sum = [0, 0, 0];
      for (let i = 0; i < d.length; i += 4) for (let c = 0; c < 3; c++) sum[c] += d[i + c];
      return sum.map((v) => v / (d.length / 4));
    });
    const jpeg = await canvas.convertToBlob({ type: 'image/jpeg', quality: 0.9 });
    const bytes = new Uint8Array(await jpeg.arrayBuffer());
    let binary = '';
    for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
    return { means, jpeg: btoa(binary), width: bitmap.width, height: bitmap.height };
  } finally {
    shooter.close();
  }
};
// One picture as a PNG, base64, through the backend asked for (forceWebGL: the WebGL2 one).
window.__png = async (scene, kind, side, use, forceWebGL) => {
  window.__sceneGPU.forceWebGL = forceWebGL;
  const shooter = await openShooter(scene, {}, kind === 'lit' ? 'lit' : 'clay');
  try {
    const framing = cameraFraming(scene, scene.objects.find((o) => o.kind === 'camera').id, side);
    const bytes = new Uint8Array(await (await shooter.shoot(framing, { kind, type: 'image/png', use })).arrayBuffer());
    let binary = '';
    for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
    return btoa(binary);
  } finally {
    shooter.close();
    window.__sceneGPU.forceWebGL = false;
  }
};
// One export image timed as the export draws and encodes it (exportShots.ts), on a shooter of its own; or why not.
window.__timed = async (scene, kind, resolution) => {
  const { imageTypeOf, longSideOf } = await import('/src/creative-scene/render/engines.ts');
  const encoding = imageTypeOf(kind, resolution);
  const shooter = await openShooter(scene, {}, kind === 'lit' ? 'lit' : 'clay');
  try {
    const framing = cameraFraming(scene, scene.objects.find((o) => o.kind === 'camera').id, longSideOf(resolution));
    const started = performance.now();
    const blob = await shooter.shoot(framing, { kind, type: encoding.type, quality: encoding.quality });
    await shooter.confirm();
    return { ms: Math.round(performance.now() - started), bytes: blob.size, type: blob.type, width: framing.width, height: framing.height };
  } catch (e) {
    return { error: String(e?.message ?? e) };
  } finally {
    shooter.close();
  }
};
// W5: one picture through the scene's camera as RGBA (base64), and where document-space points land in it.
window.__picture = async (scene, kind, side, points = []) => {
  const THREE = await import('three');
  const { positionToView } = await import('/src/creative-scene/space.ts');
  const shooter = await openShooter(scene, {}, kind === 'lit' ? 'lit' : 'clay');
  try {
    const framing = cameraFraming(scene, scene.objects.find((o) => o.kind === 'camera').id, side);
    const blob = await shooter.shoot(framing, { kind, type: 'image/png' });
    const bitmap = await createImageBitmap(blob);
    const canvas = new OffscreenCanvas(bitmap.width, bitmap.height);
    const c2d = canvas.getContext('2d');
    c2d.drawImage(bitmap, 0, 0);
    const bytes = c2d.getImageData(0, 0, bitmap.width, bitmap.height).data;
    const camera = new THREE.PerspectiveCamera(framing.fov, framing.width / framing.height, framing.near, framing.far);
    camera.position.fromArray(framing.position);
    camera.quaternion.fromArray(framing.quaternion);
    camera.updateMatrixWorld();
    const at = points.map((p) => {
      const v = new THREE.Vector3(...positionToView(p)).project(camera);
      return [((v.x + 1) / 2) * bitmap.width, ((1 - v.y) / 2) * bitmap.height];
    });
    let binary = '';
    for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
    return { width: bitmap.width, height: bitmap.height, rgba: btoa(binary), at, framing };
  } finally {
    shooter.close();
  }
};
window.__ready = true;
</script></body></html>`,
)

// W5 scene-volumes ---------------------------------------------------------------------------------------------

/** A volume object as add_objects takes it, its fields left out taking their defaults. */
const volumeObject = (name, position, scale, volume = {}, order = 80) => ({ id: gid(name), kind: 'volume', name, parent_id: null, order, transform: { ...identity(), position, scale }, visible: true, locked: false, volume })

/** A document-space camera at eye looking at target, as the scene stores it (three's camera lookAt, Y up). */
function cameraAt(name, eye, target, order = 90) {
  const m = new Matrix4().lookAt(new Vector3(...eye), new Vector3(...target), new Vector3(0, 1, 0))
  const q = new Quaternion().setFromRotationMatrix(m)
  return { id: gid(name), kind: 'camera', name, parent_id: null, order, transform: { position: eye, rotation: [q.x, q.y, q.z, q.w], scale: [1, 1, 1] }, visible: true, locked: false, camera: {} }
}

const roomBox = (name, position, [width, height, depth], order) => ({ id: gid(name), kind: 'primitive', name, parent_id: null, order, transform: { ...identity(), position }, visible: true, locked: false, primitive: { builtin: 'box@1', params: { width, height, depth } } })

/**
 * AC-W5-1's room: 6 m square, 3 m high, a window 2 m wide and 1.4 m high in the +X wall with a bar 0.24 m wide
 * down its middle; the sun 35° up from +X, the camera inside on the bar's plane (z = 0) looking at the window, so
 * the bar's shadow in the beam is a band down the middle of the picture.
 */
function windowRoom(volume) {
  const scene = JSON.parse(readFileSync(new URL('fixtures/scene-reference/physical-noon.json', import.meta.url), 'utf8'))
  scene.environment.sky = completeSky({ ...scene.environment.sky, sun_mode: 'manual', sun_elevation_deg: 35, sun_azimuth_deg: 90 })
  Object.assign(scene.render, { exposure_mode: 'ambient', exposure_compensation: 0, exposure_ev100: 15 })
  scene.objects = [
    scene.objects.find((o) => o.primitive?.builtin === 'ground@1'),
    roomBox('back', [0, 0, -3], [6.4, 3, 0.2], 1),
    roomBox('front', [0, 0, 3], [6.4, 3, 0.2], 2),
    roomBox('left', [-3, 0, 0], [0.2, 3, 6], 3),
    roomBox('sill', [3, 0, 0], [0.2, 1, 6], 4),
    roomBox('lintel', [3, 2.4, 0], [0.2, 0.6, 6], 5),
    roomBox('jamba', [3, 1, -2], [0.2, 1.4, 2], 6),
    roomBox('jambb', [3, 1, 2], [0.2, 1.4, 2], 7),
    roomBox('bar', [3, 1, 0], [0.2, 1.4, 0.24], 8),
    roomBox('ceiling', [0, 3, 0], [6.4, 0.2, 6.4], 9),
    cameraAt('inside', [-2.6, 1.5, 0], [3, 0.9, 0]),
    ...(volume ? [volumeObject('room', [0, 1.5, 0], [5.8, 2.9, 5.8], volume)] : []),
  ]
  return scene
}

/** A picture's mean sRGB luma in a 7 × 7 patch at (x, y). */
function lumaAt(p, [x, y]) {
  const data = Buffer.from(p.rgba, 'base64')
  let sum = 0
  let n = 0
  for (let dy = -3; dy <= 3; dy++)
    for (let dx = -3; dx <= 3; dx++) {
      const px = Math.min(Math.max(Math.round(x) + dx, 0), p.width - 1)
      const py = Math.min(Math.max(Math.round(y) + dy, 0), p.height - 1)
      const k = (py * p.width + px) * 4
      sum += 0.2126 * data[k] + 0.7152 * data[k + 1] + 0.0722 * data[k + 2]
      n++
    }
  return sum / n
}

/** The GI fixture page on gpu (or SwiftShader), for pictures through the export's path. */
async function shooterPage(gpu) {
  const page = await (gpu ?? browser).newPage()
  const errors = []
  page.on('pageerror', (e) => errors.push(String(e)))
  page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
  await page.goto(`${base}/.scene-gi-qa.html`)
  await page.waitForFunction(() => window.__ready === true)
  return { page, errors }
}

/** A picture of window.__picture encoded, as the page encodes it. */
async function encodePicture(page, picture, type = 'image/png') {
  const bytes = await page.evaluate(([w, h, rgba, t]) => {
    const c = new OffscreenCanvas(w, h)
    c.getContext('2d').putImageData(new ImageData(new Uint8ClampedArray(Uint8Array.from(atob(rgba), (ch) => ch.charCodeAt(0))), w, h), 0, 0)
    return c.convertToBlob({ type: t, quality: 0.9 }).then((b) => b.arrayBuffer()).then((a) => [...new Uint8Array(a)])
  }, [picture.width, picture.height, picture.rgba, type])
  return Buffer.from(bytes)
}

/** SCENE_E2E_SHOTS: a picture of window.__picture as a PNG. */
async function savePicture(page, name, picture) {
  if (process.env.SCENE_E2E_SHOTS) await writeFile(`${process.env.SCENE_E2E_SHOTS}/${name}.png`, await encodePicture(page, picture))
}

const w5Metal = () => (process.env.SCENE_E2E_W5_METAL ? chromium.launch({ headless: true, args: metal }) : null)

// AC-W5-1: the sun through the window makes a beam in the room's air, and the window's bar a dark band in it.
// Through the export's path (one workbench frame): what the volume adds where the beam is, against beside it in
// the same air, and in the bar's shadow.
await check('W5 window: a beam through the window, and the bar’s band in it', async () => {
  const gpu = await w5Metal()
  try {
    const { page, errors } = await shooterPage(gpu)
    // In the beam half a metre off the bar's plane, in the bar's shadow, and above the beam: all 1.2 m in from the window.
    const points = [[1.8, 0.84, 0.55], [1.8, 0.84, 0], [1.8, 2.3, 0.55]]
    const picture = (scene) => page.evaluate(([s, p]) => window.__picture(s, 'lit', 1024, p), [scene, points])
    const empty = await picture(windowRoom(null))
    const fogged = await picture(windowRoom({ scatter_density: 0.1, anisotropy: 0.6 }))
    await savePicture(page, `w5-window${gpu ? '-metal' : ''}`, fogged)
    const added = fogged.at.map((at) => lumaAt(fogged, at) - lumaAt(empty, at))
    const [beam, band, beside] = added
    console.log(`W5 window: luma added in the beam ${beam.toFixed(1)}, in the bar's band ${band.toFixed(1)}, beside the beam ${beside.toFixed(1)} (points at ${fogged.at.map(([x, y]) => `${x.toFixed(0)},${y.toFixed(0)}`).join(' ')})`)
    // The room's air takes the sky light too, unshadowed (Epic design 局限), so the beam stands out by about 2.5×.
    assert.ok(beam > 2 * Math.max(beside, 1), 'the beam is brighter than the air beside it')
    assert.ok(band < 0.6 * beam, 'the bar leaves a dark band in the beam')
    assert.deepEqual(errors, [])

    // The viewport through the same eye: the bar's plane is the middle column, the beam on either side of it.
    const context = await (gpu ?? browser).newContext({ viewport: { width: 1280, height: 820 } })
    const eye = [-2.6, 1.5, 0]
    const view = { camera: { position: positionToView(eye), target: positionToView([3, 0.9, 0]), ortho: false, zoom: 1 }, selection: { selected: [], active: null }, tool: 'box', toolbar: true, sidebar: true, history: false }
    await context.addInitScript(([key, v]) => localStorage.setItem(key, JSON.stringify(v)), [`creative-scene-view:qa-account:${DOC}`, view])
    const server = new SceneServer()
    referenceScene(server, 'physical-noon', (scene) => Object.assign(scene, windowRoom(null)))
    const editor = await open(server, { context })
    const { box: area } = await viewportCenter(editor.page)
    await shading(editor.page, '最终渲染')
    const settle = async () => {
      await waitFor(async () => (await editor.page.evaluate(() => window.__sceneGPU.compiling)) === 0, 'compiled', 60_000)
      await sleep(400)
      return settledShot(editor.page, area)
    }
    const before = await settle()
    server.external([{ op: 'add_objects', objects: [volumeObject('room', [0, 1.5, 0], [5.8, 2.9, 5.8], { scatter_density: 0.1, anisotropy: 0.6 })] }])
    await editor.page.evaluate(() => window.__invalidate())
    const after = await settle()
    await shot(editor.page, `w5-window-viewport${gpu ? '-metal' : ''}`)
    if (process.env.SCENE_E2E_SHOTS) for (const [n, png] of [['before', before], ['after', after]]) await writeFile(`${process.env.SCENE_E2E_SHOTS}/w5-window-viewport-${n}.png`, png)
    const rows = await editor.page.evaluate(async ([a, b]) => {
      const load = (src) => new Promise((resolve) => { const i = new Image(); i.onload = () => resolve(i); i.src = src })
      const pixels = (img) => {
        const c = document.createElement('canvas')
        c.width = img.width
        c.height = img.height
        const g = c.getContext('2d')
        g.drawImage(img, 0, 0)
        return g.getImageData(0, 0, img.width, img.height).data
      }
      const [x, y] = await Promise.all([load(a), load(b)])
      const p = pixels(x)
      const q = pixels(y)
      const w = x.width
      const luma = (d, k) => 0.2126 * d[k] + 0.7152 * d[k + 1] + 0.0722 * d[k + 2]
      // What the volume adds, averaged over 7 columns at the middle and a twentieth of the width to each side.
      const added = (row, col) => {
        let sum = 0
        for (let c = col - 3; c <= col + 3; c++) sum += luma(q, (row * w + c) * 4) - luma(p, (row * w + c) * 4)
        return sum / 7
      }
      const mid = Math.round(w / 2)
      const off = Math.round(w / 20)
      return Array.from({ length: x.height }, (_, r) => ({ band: added(r, mid), side: (added(r, mid - off) + added(r, mid + off)) / 2 }))
    }, [`data:image/png;base64,${before.toString('base64')}`, `data:image/png;base64,${after.toString('base64')}`])
    // Below the window and above the floor (half to two thirds of the way down), and the air at the top of the room
    // (the lintel and the ceiling). Medians: the volume's outline and the grid are lines a pixel thin.
    const median = (list) => [...list].sort((m, n) => m - n)[Math.floor(list.length / 2)]
    const span = (from, to) => rows.slice(Math.round(rows.length * from), Math.round(rows.length * to))
    const beamRows = span(0.5, 0.65)
    const brightest = { side: median(beamRows.map((r) => r.side)), band: median(beamRows.map((r) => r.band)) }
    const air = median(span(0, 0.15).map((r) => r.side))
    console.log(`W5 window viewport: luma added in the beam ${brightest.side.toFixed(1)}, in the bar's band there ${brightest.band.toFixed(1)}, in the air at the top ${air.toFixed(1)}`)
    assert.ok(brightest.side > 2 * Math.max(air, 1), 'the viewport: the beam is brighter than the air above it')
    assert.ok(brightest.band < 0.6 * brightest.side, 'the viewport: the bar leaves a dark band in the beam')
    assert.deepEqual(editor.errors, [])
    await context.close()
  } finally {
    await gpu?.close()
  }
})

/** A reference scene (scripts/fixtures/scene-reference) as JSON, with objects added. */
const fixtureScene = (name, ...objects) => {
  const scene = JSON.parse(readFileSync(new URL(`fixtures/scene-reference/${name}.json`, import.meta.url), 'utf8'))
  scene.objects.push(...objects)
  return scene
}

// AC-W5-2: a spot into the volume lights a cone of it; outside the cone the spot adds nothing.
await check('W5 spot: the spot lights a cone of the volume, none outside it', async () => {
  const gpu = await w5Metal()
  try {
    const { page, errors } = await shooterPage(gpu)
    // On the spot's axis, and up by the ceiling outside its 60° cone (the ray there stays outside it all the way).
    const points = [[0, 2.0, -1.0], [-1.6, 2.75, -1.5]]
    const picture = (scene) => page.evaluate(([sc, p]) => window.__picture(sc, 'lit', 1024, p), [scene, points])
    const empty = await picture(fixtureScene('cornell'))
    const fogged = await picture(fixtureScene('cornell', volumeObject('fog', [0, 1.5, -1.5], [4, 3, 3], { scatter_density: 0.15, anisotropy: 0.3 })))
    await savePicture(page, `w5-spot${gpu ? '-metal' : ''}`, fogged)
    const [inside, outside] = fogged.at.map((at) => lumaAt(fogged, at) - lumaAt(empty, at))
    console.log(`W5 spot: luma added in the cone ${inside.toFixed(1)}, outside it ${outside.toFixed(1)} (at ${fogged.at.map(([x, y]) => `${x.toFixed(0)},${y.toFixed(0)}`).join(' ')})`)
    assert.ok(inside > 10, 'the cone shows')
    assert.ok(outside < 0.2 * inside, 'outside the cone the spot adds next to nothing')
    // The real-time GI's composite draws them too, after the lighting pass that draws the spot's shadow map.
    const gi = (scene) => {
      scene.render = { ...scene.render, engine: 'realtime_gi' }
      return page.evaluate(([sc, p]) => window.__picture(sc, 'lit', 512, p), [scene, points])
    }
    const giEmpty = await gi(fixtureScene('cornell'))
    const giFogged = await gi(fixtureScene('cornell', volumeObject('fog', [0, 1.5, -1.5], [4, 3, 3], { scatter_density: 0.15, anisotropy: 0.3 })))
    await savePicture(page, `w5-spot-gi${gpu ? '-metal' : ''}`, giFogged)
    const [giInside, giOutside] = giFogged.at.map((at) => lumaAt(giFogged, at) - lumaAt(giEmpty, at))
    console.log(`W5 spot, real-time GI: luma added in the cone ${giInside.toFixed(1)}, outside it ${giOutside.toFixed(1)}`)
    assert.ok(giInside > 10 && giOutside < 0.2 * giInside, 'the real-time GI: the cone, none outside it')
    assert.deepEqual(errors, [])
  } finally {
    await gpu?.close()
  }
})

/**
 * Which pixels of a picture the camera's rays could see the box volume v through, grown by `grow` pixels: the
 * rest must be the picture without it, pixel for pixel (AC-W5-3).
 */
function volumeCoverage(picture, v, grow = 2) {
  const { width, height, framing } = picture
  const camera = new PerspectiveCamera(framing.fov, width / height, framing.near, framing.far)
  camera.position.fromArray(framing.position)
  camera.quaternion.fromArray(framing.quaternion)
  camera.updateMatrixWorld()
  const local = new Matrix4().compose(new Vector3(...v.transform.position), new Quaternion(...v.transform.rotation), new Vector3(...v.transform.scale)).invert()
  const hit = new Uint8Array(width * height)
  const o = new Vector3()
  const d = new Vector3()
  const ray = new Ray()
  const unit = new Box3(new Vector3(-0.5, -0.5, -0.5), new Vector3(0.5, 0.5, 0.5))
  for (let y = 0; y < height; y++)
    for (let x = 0; x < width; x++) {
      const at = new Vector3(((x + 0.5) / width) * 2 - 1, 1 - ((y + 0.5) / height) * 2, 0.5).unproject(camera)
      o.fromArray(positionToDoc(camera.position.toArray())).applyMatrix4(local)
      d.fromArray(positionToDoc(at.toArray())).applyMatrix4(local).sub(o)
      ray.set(o, d.normalize())
      if (ray.intersectsBox(unit)) hit[y * width + x] = 1
    }
  const grown = new Uint8Array(width * height)
  for (let y = 0; y < height; y++)
    for (let x = 0; x < width; x++) {
      if (!hit[y * width + x]) continue
      for (let dy = -grow; dy <= grow; dy++)
        for (let dx = -grow; dx <= grow; dx++) {
          const px = x + dx
          const py = y + dy
          if (px >= 0 && py >= 0 && px < width && py < height) grown[py * width + px] = 1
        }
    }
  return grown
}

/** Pixels of b unlike a: outside the mask and inside it. */
function changedPixels(a, b, mask) {
  const p = Buffer.from(a.rgba, 'base64')
  const q = Buffer.from(b.rgba, 'base64')
  let outside = 0
  let inside = 0
  for (let i = 0; i < mask.length; i++) {
    const k = i * 4
    if (p[k] === q[k] && p[k + 1] === q[k + 1] && p[k + 2] === q[k + 2]) continue
    if (mask[i]) inside++
    else outside++
  }
  return { outside, inside }
}

// AC-W5-3: moved and scaled, a volume changes only what it covers; beyond its outline the picture is the one
// without it, pixel for pixel. Through the export's path, and the same in the clay and line art picture (AC-W5-6).
await check('W5 range: a volume changes only the pixels it covers; clay and line art never show it', async () => {
  const gpu = await w5Metal()
  try {
    const { page, errors } = await shooterPage(gpu)
    const picture = (scene) => page.evaluate(([sc]) => window.__picture(sc, 'lit', 768), [scene])
    const empty = await picture(fixtureScene('physical-noon'))
    for (const v of [
      volumeObject('haze', [0, 1, 0], [2, 2, 2], { scatter_density: 0.5 }),
      volumeObject('haze', [1.2, 1.5, 1], [1, 3, 1.5], { scatter_density: 0.5, edge_falloff: 0 }),
    ]) {
      const fogged = await picture(fixtureScene('physical-noon', v))
      const c = changedPixels(empty, fogged, volumeCoverage(fogged, v))
      console.log(`W5 range at ${v.transform.position} × ${v.transform.scale}: ${c.inside} pixels changed within the outline, ${c.outside} beyond it`)
      assert.ok(c.inside > 1000, 'the volume shows')
      assert.equal(c.outside, 0, 'beyond the outline the picture is the one without the volume')
    }
    const hash = (scene, kind) => page.evaluate(([sc, k]) => window.__hash(sc, k, 512), [scene, kind])
    const withVolume = fixtureScene('physical-noon', volumeObject('haze', [0, 1, 0], [2, 2, 2], { scatter_density: 2 }))
    for (const kind of ['clay', 'line']) assert.equal(await hash(withVolume, kind), await hash(fixtureScene('physical-noon'), kind), `${kind}: the same as without the volume`)
    // AC-W5-9: the same lit picture twice is the same, pixel for pixel.
    assert.equal(await hash(withVolume, 'lit'), await hash(withVolume, 'lit'), 'lit twice alike')
    assert.deepEqual(errors, [])
  } finally {
    await gpu?.close()
  }
})

// AC-W5-12: under the physical sky with the aerial perspective × 100, dense smoke across the horizon: the air
// behind it is not laid over it, so it reads the same above the horizon (the sky behind) and below (far ground),
// darker than the hazed ground beside it.
await check('W5 aerial: smoke across the horizon has no seam and no haze from behind laid over it', async () => {
  const gpu = await w5Metal()
  try {
    const { page, errors } = await shooterPage(gpu)
    const scene = fixtureScene('physical-noon')
    scene.environment.sky = completeSky({ ...scene.environment.sky, aerial_perspective: { enabled: true, distance_scale: 100 } })
    scene.objects = [
      { ...scene.objects.find((o) => o.primitive?.builtin === 'ground@1'), primitive: { builtin: 'ground@1', params: { size: 1000 } } },
      cameraAt('far', [0, 1.5, 0], [0, 1.5, -100]),
      volumeObject('smoke', [0, 1.5, -40], [12, 8, 4], { scatter_color: '#808080', scatter_density: 1, absorption_color: '#202020', absorption_density: 4, edge_falloff: 0 }),
    ]
    // In the smoke a little above and below the ground's far edge, and the ground beside the smoke.
    const points = [[0, 2.5, -38], [0, 0.6, -38], [-12, 0.3, -30]]
    const fogged = await page.evaluate(([sc, p]) => window.__picture(sc, 'lit', 1024, p), [scene, points])
    const [above, below, ground] = fogged.at.map((at) => lumaAt(fogged, at))
    console.log(`W5 aerial: smoke above the horizon ${above.toFixed(1)}, below ${below.toFixed(1)}; the hazed ground beside ${ground.toFixed(1)}`)
    assert.ok(Math.abs(above - below) < 6, 'no seam at the horizon')
    assert.ok(below < ground - 10, 'the haze behind the smoke is not laid over it')
    assert.deepEqual(errors, [])
  } finally {
    await gpu?.close()
  }
})

// scene-volumes design「选灯与编译」: a scene without volumes builds no march; the first volume builds it, ahead
// of the frame in the viewport. Past the cap of 6 lights and 12 shadow maps the march still builds (WebGPU allows
// 16 textures a stage) and the rest light the surfaces only.
// On this machine's GPU (SCENE_E2E_W5_METAL): nine casting lights are too slow for SwiftShader.
if (process.env.SCENE_E2E_W5_METAL) await check('W5 many lights: no march without volumes; over the light cap the march still builds and draws', async () => {
  const gpu = await w5Metal()
  const context = await (gpu ?? browser).newContext({ viewport: { width: 1280, height: 820 } })
  try {
    const server = new SceneServer()
    referenceScene(server, 'cornell', (scene) => {
      for (let i = 0; i < 8; i++)
        scene.objects.push({ id: gid(`lamp${i}`), kind: 'light', name: `lamp${i}`, parent_id: null, order: 40 + i, transform: { position: [-1.6 + (i % 4) * 1.05, 2.8, i < 4 ? -2.4 : -0.6], rotation: [-0.707107, 0, 0, 0.707107], scale: [1, 1, 1] }, visible: true, locked: false, light: { type: 'spot', color: '#ffffff', intensity: 30, cast_shadow: true, distance: 0, decay: 2, angle: 0.5, penumbra: 0.2 } })
    })
    const { page, errors } = await open(server, { context })
    const { box: area } = await viewportCenter(page)
    await shading(page, '最终渲染')
    const settle = async () => {
      await waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiling)) === 0, 'compiled', 60_000)
      await sleep(400)
      return settledShot(page, area)
    }
    const before = await settle()
    assert.equal(await page.evaluate(() => window.__sceneGPU.marches), 0, 'no march without volumes')
    server.external([{ op: 'add_objects', objects: [volumeObject('fog', [0, 1.5, -1.5], [4, 3, 3], { scatter_density: 0.15 })] }])
    await page.evaluate(() => window.__invalidate())
    const after = await settle()
    await shot(page, `w5-many-lights${gpu ? '-metal' : ''}`)
    const c = await pixelChange(page, before, after)
    const marches = await page.evaluate(() => window.__sceneGPU.marches)
    console.log(`W5 many lights: ${marches} march built, ${(c.changed * 100).toFixed(1)}% changed`)
    assert.ok(marches >= 1, 'the first volume builds the march')
    assert.ok(c.changed > 0.01, 'the volume draws')
    assert.deepEqual(errors, [])
  } finally {
    await context.close()
    await gpu?.close()
  }
})

// The cap's other end: three casting directional lights (12 maps, the sun's and two more) under a sky, the march
// at 14 of WebGPU's 16 textures a stage. The surfaces' material has three lights in cascades: three's
// CSMShadowNode named their uniforms alike and the material did not compile (render/sunShadow.ts ownNames).
if (process.env.SCENE_E2E_W5_METAL) await check('W5 many lights under a sky: the sun and two casting directional lights, the march builds and draws', async () => {
  const gpu = await w5Metal()
  const context = await (gpu ?? browser).newContext({ viewport: { width: 1280, height: 820 } })
  try {
    const server = new SceneServer()
    referenceScene(server, 'physical-noon', (scene) => {
      for (const [i, x] of [[0, -4], [1, 4]])
        scene.objects.push({ id: gid(`key${i}`), kind: 'light', name: `key${i}`, parent_id: null, order: 40 + i, transform: { position: [x, 5, 2], rotation: [0, 0, 0, 1], scale: [1, 1, 1] }, visible: true, locked: false, light: { type: 'directional', color: '#ffffff', intensity: 1, cast_shadow: true } })
    })
    const { page, errors } = await open(server, { context })
    const { box: area } = await viewportCenter(page)
    await shading(page, '最终渲染')
    const settle = async () => {
      await waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiling)) === 0, 'compiled', 60_000)
      await sleep(400)
      return settledShot(page, area)
    }
    const before = await settle()
    server.external([{ op: 'add_objects', objects: [volumeObject('fog', [0, 1.5, 0], [6, 3, 6], { scatter_density: 0.15, cast_shadow: true })] }])
    await page.evaluate(() => window.__invalidate())
    const after = await settle()
    await shot(page, `w5-many-lights-sky${gpu ? '-metal' : ''}`)
    const c = await pixelChange(page, before, after)
    console.log(`W5 many lights under a sky: ${(c.changed * 100).toFixed(1)}% changed`)
    assert.ok(c.changed > 0.01, 'the volume draws')
    assert.deepEqual(errors, [])
  } finally {
    await context.close()
    await gpu?.close()
  }
})

// AC-W5-5: dense smoke casting a shadow darkens the sunlit ground behind it; with cast_shadow off the picture
// beyond the smoke's outline is the one without it (the sun 45° up from +X, the shadow 1.5 m toward −X).
await check('W5 cast shadow: a casting volume darkens the sunlit ground; off, the ground is as without it', async () => {
  const gpu = await w5Metal()
  try {
    const { page, errors } = await shooterPage(gpu)
    const yard = (...objects) => {
      const scene = fixtureScene('physical-noon', ...objects)
      scene.environment.sky = completeSky({ ...scene.environment.sky, sun_mode: 'manual', sun_elevation_deg: 45, sun_azimuth_deg: 90 })
      return scene
    }
    const smoke = (cast) => volumeObject('smoke', [2, 1.5, 3], [1, 1, 1], { scatter_density: 0.5, absorption_density: 8, edge_falloff: 0, cast_shadow: cast })
    // On the ground in the smoke's shadow, and in the sun beyond it (clear of the ball's shadow).
    const points = [[0.5, 0, 3], [0.6, 0, 1.9]]
    const picture = (scene) => page.evaluate(([sc, p]) => window.__picture(sc, 'lit', 768, p), [scene, points])
    const empty = await picture(yard())
    const casting = await picture(yard(smoke(true)))
    const notCasting = await picture(yard(smoke(false)))
    await savePicture(page, `w5-cast-shadow${gpu ? '-metal' : ''}`, casting)
    const [shaded, lit] = casting.at.map((at) => [lumaAt(empty, at), lumaAt(casting, at)])
    console.log(`W5 cast shadow: the ground in the smoke's shadow ${shaded[0].toFixed(0)} → ${shaded[1].toFixed(0)}, in the sun beside it ${lit[0].toFixed(0)} → ${lit[1].toFixed(0)}`)
    assert.ok(shaded[1] < shaded[0] * 0.7, 'the ground behind the smoke darkens')
    assert.ok(Math.abs(lit[1] - lit[0]) < 2, 'the ground beside the shadow stays')
    const c = changedPixels(empty, notCasting, volumeCoverage(notCasting, smoke(false)))
    assert.equal(c.outside, 0, 'with cast_shadow off nothing beyond the outline changes')
    assert.deepEqual(errors, [])
  } finally {
    await gpu?.close()
  }
})

// scene-volumes design「选灯与编译」「表面阴影系数」: a new casting light changes the march's lights, and the march
// compiles ahead of the frame; the frames after it build no more shader than in the same scene without volumes
// (develop's materials compile again on a new light's frames whatever the volumes: three numbers a stand-in's
// shader apart from the real one's). A light whose shadow is turned off frees its map as the scene pass builds:
// the march, rebuilt first, never reads it.
await check('W5 no recompile: a new casting light compiles no more on its frames with volumes than without; a shadow turned off draws on', async () => {
  const gpu = await w5Metal()
  try {
    const run = async (volume) => {
      const server = new SceneServer()
      referenceScene(server, 'cornell', (scene) => volume && scene.objects.push(volumeObject('fog', [0, 1.5, -1.5], [4, 3, 3], { scatter_density: 0.15, cast_shadow: true })))
      const { context, page, errors } = await open(server, gpu ? { context: await gpu.newContext({ viewport: { width: 1280, height: 820 } }) } : {})
      const { box: area } = await viewportCenter(page)
      await shading(page, '最终渲染')
      const settle = async () => {
        await waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiling)) === 0, 'compiled', 60_000)
        await sleep(600)
        return settledShot(page, area)
      }
      const change = async (ops) => {
        server.external(ops)
        await page.evaluate(() => window.__invalidate())
        return settle()
      }
      await settle()
      // The programs the viewport's frames build themselves, outside any compile ahead.
      await page.evaluate(() => {
        window.__built = []
        for (const h of window.__sceneGPU.live) {
          if (h.role !== 'viewport') continue
          const create = h.info.createProgram.bind(h.info)
          h.info.createProgram = (program) => {
            if (window.__sceneGPU.compiling === 0) window.__built.push(program.name || '-')
            create(program)
          }
        }
      })
      const built = () => page.evaluate(() => window.__built.splice(0))
      const light = (name, type, position, order) => ({ id: gid(name), kind: 'light', name, parent_id: null, order, transform: { position, rotation: [-0.707107, 0, 0, 0.707107], scale: [1, 1, 1] }, visible: true, locked: false, light: { type, intensity: type === 'spot' ? 40 : 2, cast_shadow: true } })
      await change([{ op: 'add_objects', objects: [light('lamp', 'spot', [0.8, 2.8, -1], 50)] }])
      const spot = await built()
      await change([{ op: 'add_objects', objects: [light('key', 'directional', [0, 5, 0], 51)] }])
      const directional = await built()
      const off = await change([{ op: 'update_objects', updates: [{ object_id: gid('lamp'), light: { cast_shadow: false } }, { object_id: 'sobj_spot00000000000000000', light: { cast_shadow: false } }] }])
      if (volume) await shot(page, `w5-shadow-off${gpu ? '-metal' : ''}`)
      const lit = await lumaStats(page, off, { level: 250 })
      await context.close()
      return { spot, directional, lit, errors }
    }
    const without = await run(false)
    const withVolume = await run(true)
    const fmt = (r) => `a spot ${r.spot.length} (${r.spot.join(' ')}), a directional light ${r.directional.length}`
    console.log(`W5 no recompile: programs built on the frames without volumes: ${fmt(without)}; with: ${fmt(withVolume)}`)
    assert.ok(![...withVolume.spot, ...withVolume.directional].includes('Volumes'), 'the march compiles ahead, not on a frame')
    assert.ok(withVolume.spot.length <= without.spot.length && withVolume.directional.length <= without.directional.length, 'no more than without volumes')
    assert.ok(withVolume.lit.mean > 5, 'the picture draws on with the shadows off')
    const errors = [...without.errors, ...withVolume.errors]
    assert.deepEqual(errors, [], JSON.stringify(errors).slice(0, 1500))
  } finally {
    await gpu?.close()
  }
})

// AC-W5-11, owner 2026-10-23: the lit materials carry the volumes' shadow only while a volume casts. The first to
// cast compiles them again, ahead, as a new light (objects/SceneLights.tsx VolumesCastLight); then the shadow shows.
await check('W5 first casting volume: the materials compile again ahead of the frames, then the shadow shows', async () => {
  const gpu = await w5Metal()
  try {
    const server = new SceneServer()
    // The yard of「W5 cast shadow」: the sun 45° up from +X.
    referenceScene(server, 'physical-noon', (scene) => {
      scene.environment.sky = completeSky({ ...scene.environment.sky, sun_mode: 'manual', sun_elevation_deg: 45, sun_azimuth_deg: 90 })
      scene.objects.push(volumeObject('smoke', [2, 1.5, 3], [1, 1, 1], { scatter_density: 0.5, absorption_density: 8, edge_falloff: 0, cast_shadow: false }))
    })
    const { context, page, errors } = await open(server, gpu ? { context: await gpu.newContext({ viewport: { width: 1280, height: 820 } }) } : {})
    const { box: area } = await viewportCenter(page)
    await shading(page, '最终渲染')
    const settle = async () => {
      await waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiling)) === 0, 'compiled', 60_000)
      await sleep(600)
      return settledShot(page, area)
    }
    const before = await settle()
    await shot(page, `w5-first-cast-before${gpu ? '-metal' : ''}`)
    await page.evaluate(() => {
      window.__built = []
      for (const h of window.__sceneGPU.live) {
        if (h.role !== 'viewport') continue
        const create = h.info.createProgram.bind(h.info)
        h.info.createProgram = (program) => {
          if (window.__sceneGPU.compiling === 0) window.__built.push(program.name || '-')
          create(program)
        }
      }
    })
    const compiles = await page.evaluate(() => window.__sceneGPU.compiles)
    server.external([{ op: 'update_objects', updates: [{ object_id: gid('smoke'), volume: { cast_shadow: true } }] }])
    await page.evaluate(() => window.__invalidate())
    // The materials compile again for the mark, longer than a frame or two: done once that compile is.
    await waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiles)) > compiles, 'the compile ahead starts', 10_000)
    const after = await settle()
    await shot(page, `w5-first-cast${gpu ? '-metal' : ''}`)
    const built = await page.evaluate(() => window.__built.splice(0))
    const c = await pixelChange(page, before, after)
    // Programs the frames still build are three's, as for a new light (「W5 no recompile」, design「表面阴影系数」).
    console.log(`W5 first casting volume: programs built on the frames ${built.length} (${built.join(' ')}), ${(c.changed * 100).toFixed(2)}% changed`)
    assert.ok(!built.includes('Volumes'), 'the march does not compile again')
    assert.ok(c.changed > 0.003, 'the shadow shows')
    assert.deepEqual(errors, [])
    await context.close()
  } finally {
    await gpu?.close()
  }
})

// scene-volumes design「编辑与拾取」: the add menu puts a 2 m volume on what the view's centre looks at; its bounds
// are picked only near a line, so a click inside it goes to what is behind; the properties write by key; Cmd+Z.
await check('W5 editing: add a volume, a click inside it picks what is behind, its density by number and slider, undo', async () => {
  const server = new SceneServer()
  seed(server, [boxOf('post', 5, { position: [0, 0, 0] }, { width: 0.4, height: 1, depth: 0.4 })])
  const { context, page, errors } = await open(server)
  const { x, y } = await viewportCenter(page)
  await page.mouse.move(x, y)
  await page.keyboard.press('Shift+KeyA')
  await page.getByRole('menuitem', { name: '体积（立方体）' }).click()
  await waitFor(() => server.commands.length === 1, 'add a volume')
  const added = server.commands[0].payload.operations[0].objects[0]
  assert.deepEqual([added.kind, added.name, added.transform.scale, added.volume], ['volume', '体积 1', [2, 2, 2], { shape: 'box' }])
  // Its base on what the centre looks at, the post or the ground by it (a metre below its centre).
  const base = added.transform.position[1] - 1
  assert.ok(base >= -1e-6 && base <= 1 + 1e-6, `on what the centre looks at: ${added.transform.position}`)
  assert.equal(await page.getByRole('treeitem', { name: /体积 1/ }).getAttribute('aria-selected'), 'true', 'the new volume is selected')
  await saved(page)

  const volumeAt = () => server.object(added.id).volume
  const group = page.getByRole('group', { name: '体积' })
  const density = group.getByRole('spinbutton', { name: '散射密度' })
  await density.fill('0.5')
  await density.press('Enter')
  await waitFor(() => volumeAt().scatter_density === 0.5, 'the density by number')
  assert.equal(volumeAt().anisotropy, 0, 'the rest as it was')
  await group.getByRole('slider', { name: '散射密度' }).fill('-3.5')
  await waitFor(() => volumeAt().scatter_density === 0, 'the slider’s leftmost step is 0')
  await group.getByRole('slider', { name: '散射密度' }).fill('0')
  await waitFor(() => volumeAt().scatter_density === 1, 'the slider writes 10^0')
  await group.getByLabel('投射阴影').check()
  await waitFor(() => volumeAt().cast_shadow === true, 'cast shadow on')
  await saved(page)
  await focusEditor(page)
  await page.keyboard.press(`${modKey}+KeyZ`)
  await waitFor(() => volumeAt().cast_shadow === false, 'undo')

  // Inside the bounds, away from their lines: the click picks the post, not the volume around it.
  await page.mouse.click(x, y + 10)
  await waitFor(async () => (await page.getByRole('treeitem', { name: /体积 1/ }).getAttribute('aria-selected')) !== 'true', 'a click inside does not pick the volume')
  assert.deepEqual(errors, [])
  await context.close()
})

/** A picture's mean sRGB colour in a 7 × 7 patch at (x, y). */
function rgbAt(p, [x, y]) {
  const data = Buffer.from(p.rgba, 'base64')
  const sum = [0, 0, 0]
  for (let dy = -3; dy <= 3; dy++)
    for (let dx = -3; dx <= 3; dx++) {
      const k = (Math.min(Math.max(Math.round(y) + dy, 0), p.height - 1) * p.width + Math.min(Math.max(Math.round(x) + dx, 0), p.width - 1)) * 4
      for (let c = 0; c < 3; c++) sum[c] += data[k + c]
    }
  return sum.map((v) => v / 49)
}

// AC-W5-4: the fields move the picture as Blender's Volume Scatter and Volume Absorption do: denser is brighter,
// what shows through takes the absorption colour, and forward scattering is brighter looking into the light.
await check('W5 params: density, absorption colour and anisotropy move the picture as in Blender', async () => {
  const gpu = await w5Metal()
  try {
    const { page, errors } = await shooterPage(gpu)
    const beamAt = [[1.8, 0.84, 0.55]]
    const room = (volume) => page.evaluate(([sc, p]) => window.__picture(sc, 'lit', 768, p), [windowRoom(volume), beamAt])
    const empty = await room(null)
    const added = async (volume) => {
      const p = await room(volume)
      return lumaAt(p, p.at[0]) - lumaAt(empty, p.at[0])
    }
    // The camera looks into the window, the sun's light coming toward it.
    const thin = await added({ scatter_density: 0.1, anisotropy: 0 })
    const dense = await added({ scatter_density: 0.3, anisotropy: 0 })
    const forward = await added({ scatter_density: 0.1, anisotropy: 0.6 })
    // The white box seen through an absorber that keeps red, and one that keeps blue. AgX mixes the channels before its
    // curve, so the kept channel dims a little too: the hue is what moves (red over blue up, then down).
    const face = [[-1.5, 0.5, 0.5]]
    const yard = (...objects) => page.evaluate(([sc, p]) => window.__picture(sc, 'lit', 768, p), [fixtureScene('physical-noon', ...objects), face])
    const through = async (color) => {
      const p = color ? await yard(volumeObject('glass', [-1.5, 0.6, 1.2], [1.2, 1.2, 0.6], { scatter_density: 0, absorption_color: color, absorption_density: 3, edge_falloff: 0 })) : await yard()
      const [r, , b] = rgbAt(p, p.at[0])
      return r / b
    }
    const [clear, red, blue] = [await through(null), await through('#ff4040'), await through('#4040ff')]
    console.log(`W5 params: added in the beam at 0.1 ${thin.toFixed(1)}, at 0.3 ${dense.toFixed(1)}, at 0.1 forward (g 0.6) ${forward.toFixed(1)}; the white box's red over blue ${clear.toFixed(2)}, through red ${red.toFixed(2)}, through blue ${blue.toFixed(2)}`)
    assert.ok(dense > thin * 1.3, 'denser is brighter')
    assert.ok(forward > thin * 1.2, 'forward scattering is brighter looking into the light')
    assert.ok(red > clear * 1.1 && blue < clear * 0.9, 'what shows through takes the absorption colour')
    assert.deepEqual(errors, [])
  } finally {
    await gpu?.close()
  }
})

/** Two RGBA pictures apart: the mean difference per channel (0–255) and the share of pixels a channel off by more than 8 (%). */
function pictureDiff(a, b) {
  const p = Buffer.from(a.rgba, 'base64')
  const q = Buffer.from(b.rgba, 'base64')
  let sum = 0
  let over = 0
  for (let k = 0; k < p.length; k += 4) {
    let most = 0
    for (let c = 0; c < 3; c++) {
      const d = Math.abs(p[k + c] - q[k + c])
      sum += d
      most = Math.max(most, d)
    }
    if (most > 8) over++
  }
  return { mean: sum / ((p.length / 4) * 3), over: (over / (p.length / 4)) * 100 }
}

// AC-W5-7 (SCENE_E2E_W5_METAL): the same volumes on WebGPU (this machine's GPU) and on the WebGL2 backend under
// SwiftShader differ by no more than the Epic's lit threshold (W2 parity), or no more than the same pictures
// without the volumes do. SCENE_E2E_W5_EVIDENCE=<dir> keeps the numbers.
if (process.env.SCENE_E2E_W5_METAL) await check('W5 backends: the volumes on WebGPU and on WebGL2 under SwiftShader alike', async () => {
  const gpu = await w5Metal()
  try {
    const metalPage = await shooterPage(gpu)
    const swiftPage = await shooterPage(null)
    const backends = []
    for (const p of [metalPage.page, swiftPage.page]) p.on('console', (m) => m.text().startsWith('scene renderer') && backends.push(m.text()))
    const shoot = (p, scene) => p.evaluate(([sc]) => window.__picture(sc, 'lit', 1024), [scene])
    const scenes = {
      window: [windowRoom({ scatter_density: 0.1, anisotropy: 0.6 }), windowRoom(null)],
      spot: [fixtureScene('cornell', volumeObject('fog', [0, 1.5, -1.5], [4, 3, 3], { scatter_density: 0.15, anisotropy: 0.3 })), fixtureScene('cornell')],
    }
    const rows = {}
    for (const [name, [withVolume, without]] of Object.entries(scenes)) {
      const m = pictureDiff(await shoot(metalPage.page, withVolume), await shoot(swiftPage.page, withVolume))
      const o = pictureDiff(await shoot(metalPage.page, without), await shoot(swiftPage.page, without))
      const within = m.mean <= parityLimits.lit.mean && m.over <= parityLimits.lit.over
      const noWorse = m.mean <= o.mean + 0.1 && m.over <= o.over + 0.05
      rows[name] = { mean: Number(m.mean.toFixed(4)), over_percent: Number(m.over.toFixed(4)), without: { mean: Number(o.mean.toFixed(4)), over_percent: Number(o.over.toFixed(4)) }, ok: within || noWorse }
      console.log(`W5 backends ${name}: mean ${m.mean.toFixed(3)}, > 8 ${m.over.toFixed(4)}%; without the volume ${o.mean.toFixed(3)}, ${o.over.toFixed(4)}%`)
    }
    console.log(`W5 backends: ${[...new Set(backends)].join('; ')}`)
    if (process.env.SCENE_E2E_W5_EVIDENCE) await writeFile(`${process.env.SCENE_E2E_W5_EVIDENCE}/w5-parity.json`, `${JSON.stringify({ machine: machineInfo(), chromium: gpu.version(), limits: parityLimits.lit, backends: [...new Set(backends)], rows }, null, 2)}\n`)
    assert.ok(backends.includes('scene renderer (offscreen): webgpu') && backends.some((b) => b.includes('webgl2')), `both backends drew: ${backends}`)
    for (const [name, r] of Object.entries(rows)) assert.ok(r.ok, `${name}: mean ${r.mean}, > 8 ${r.over_percent}%`)
    assert.deepEqual([...metalPage.errors, ...swiftPage.errors], [])
  } finally {
    await gpu?.close()
  }
})

// SCENE_E2E_W5_SAMPLES=<dir> (scene-volumes design「验证与交付」): the owner's four samples at 2K on this machine's
// GPU, through the export's path: the beam through the window, the spot's cone, dense smoke shadowing the yard, and
// the room's two point lights in its air (the uniform steps' 1/d² near a point light: look for bright spots or bands).
if (process.env.SCENE_E2E_W5_SAMPLES) await check('W5 samples: the window, the spot, the smoke’s shadow and point lights in the air, for the owner', async () => {
  const dir = process.env.SCENE_E2E_W5_SAMPLES
  const gpu = await chromium.launch({ headless: true, args: metal })
  try {
    const { page, errors } = await shooterPage(gpu)
    const window = windowRoom({ scatter_density: 0.1, anisotropy: 0.6 })
    window.objects = window.objects.map((o) => (o.kind === 'camera' ? cameraAt('inside', [-2.6, 1.4, 1.6], [3, 1.0, -0.4]) : o))
    const smoke = fixtureScene('physical-noon', volumeObject('smoke', [2, 1.5, 3], [1.4, 1.2, 1.4], { scatter_color: '#9a9a9a', scatter_density: 1.5, absorption_density: 1, edge_falloff: 0.4, cast_shadow: true }))
    smoke.environment.sky = completeSky({ ...smoke.environment.sky, sun_mode: 'manual', sun_elevation_deg: 45, sun_azimuth_deg: 90 })
    smoke.objects = smoke.objects.map((o) => (o.kind === 'camera' ? cameraAt('side', [3.5, 2.2, 8], [1, 0.7, 2.6]) : o))
    const samples = {
      'w5-sample-window': window,
      'w5-sample-spot': fixtureScene('cornell', volumeObject('fog', [0, 1.5, -1.5], [4, 3, 3], { scatter_density: 0.15, anisotropy: 0.3 })),
      'w5-sample-smoke-shadow': smoke,
      'w5-sample-point-lights': fixtureScene('room', volumeObject('air', [0, 1.5, 0], [6, 3, 4], { scatter_density: 0.15, anisotropy: 0.2 })),
      ...(process.env.SCENE_E2E_W5_DEBUG ? { 'w5-sample-point-lights-empty': fixtureScene('room') } : {}),
    }
    for (const [name, scene] of Object.entries(samples)) {
      const picture = await page.evaluate(([sc]) => window.__picture(sc, 'lit', 2048), [scene])
      await writeFile(`${dir}/${name}.jpg`, await encodePicture(page, picture, 'image/jpeg'))
      console.log(`W5 samples: ${name}.jpg ${picture.width} × ${picture.height}`)
    }
    assert.deepEqual(errors, [])
  } finally {
    await gpu.close()
  }
})

// SCENE_E2E_W5_PERF=<dir> (AC-W5-11), this machine's GPU: the viewport at 1280 × 720 CSS pixels, 2× pixels, WebGPU,
// the window room under the sun with two casting spots (three casting lights). Throughput as the spike measured it
// (scene-volumes discovery/research/volume-lighting.md 第 2 节): the frame drawn 30 times in a row and waited for once,
// the least of three medians of 9 rounds, without volumes, with 1, 8 and 8 all casting; and how long a new casting spot takes from
// the edit to its compile being done. SCENE_E2E_W5_PERF_NOVOLUME=1 measures the scene without volumes only (and runs
// on develop with the same hook, for the comparison). Written to w5-perf-<SCENE_E2E_W5_PERF_LABEL>.json.
if (process.env.SCENE_E2E_W5_PERF) await check('W5 performance: the volumes’ frame time in the viewport on this machine’s GPU', async () => {
  const dir = process.env.SCENE_E2E_W5_PERF
  const label = process.env.SCENE_E2E_W5_PERF_LABEL ?? 'after'
  const gpu = await chromium.launch({ headless: true, args: metal })
  try {
    const context = await gpu.newContext({ viewport: { width: 1356, height: 874 }, deviceScaleFactor: 2 })
    await context.addInitScript(([key, v]) => localStorage.setItem(key, JSON.stringify(v)), [`creative-scene-view:qa-account:${DOC}`, { camera: { position: positionToView([-2.6, 1.5, 0]), target: positionToView([3, 0.9, 0]), ortho: false, zoom: 1 }, selection: { selected: [], active: null }, tool: 'box', toolbar: false, sidebar: false, history: false }])
    const server = new SceneServer()
    const spot = (name, position, order) => ({ id: gid(name), kind: 'light', name, parent_id: null, order, transform: { position, rotation: [-0.707107, 0, 0, 0.707107], scale: [1, 1, 1] }, visible: true, locked: false, light: { type: 'spot', intensity: 40, cast_shadow: true, angle: 0.6, penumbra: 0.3 } })
    referenceScene(server, 'physical-noon', (scene) => {
      Object.assign(scene, windowRoom(null))
      scene.objects.push(spot('spota', [-1.5, 2.8, -1.5], 30), spot('spotb', [-1.5, 2.8, 1.5], 31))
    })
    const { page, errors } = await open(server, { context, viewport: false })
    await viewportReady(page)
    await sleep(400)
    await shading(page, '最终渲染')
    const compiled = () => waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiling)) === 0, 'compiled', 60_000)
    const measure = async () => {
      await compiled()
      await sleep(800)
      await page.evaluate(() => window.__sceneGPU.redrawViewport(30))
      // The least of three medians: other work on the machine only ever slows a round down.
      const medians = []
      for (let k = 0; k < 3; k++) {
        const rounds = []
        for (let i = 0; i < 9; i++) rounds.push((await page.evaluate(() => window.__sceneGPU.redrawViewport(30))) / 30)
        rounds.sort((a, b) => a - b)
        medians.push(rounds[4])
      }
      return Number(Math.min(...medians).toFixed(2))
    }
    const change = async (ops) => {
      server.external(ops)
      await page.evaluate(() => window.__invalidate())
    }
    const canvas = await page.evaluate(() => { const r = document.querySelector('.cc-scene-editor canvas').getBoundingClientRect(); return `${Math.round(r.width)} × ${Math.round(r.height)}` })
    const record = { machine: machineInfo(), chromium: gpu.version(), viewport: `${canvas} CSS pixels, 2× pixels, WebGPU`, scene: 'AC-W5-1 window room, the sun and two spots casting', label, ms: {} }
    record.ms.none = await measure()
    // A new casting spot: from the edit to its compile done (warm shader cache: the same scene has run before).
    const newLight = async () => {
      const started = await page.evaluate(() => performance.now())
      await change([{ op: 'add_objects', objects: [spot('spotc', [1.5, 2.8, 0], 32)] }])
      await sleep(50)
      await compiled()
      const ms = Math.round((await page.evaluate(() => performance.now())) - started)
      await change([{ op: 'remove_objects', object_ids: [gid('spotc')] }])
      await compiled()
      return ms
    }
    record.new_light_compile_ms = await newLight()
    if (!process.env.SCENE_E2E_W5_PERF_NOVOLUME) {
      await change([{ op: 'add_objects', objects: [volumeObject('room', [0, 1.5, 0], [5.8, 2.9, 5.8], { scatter_density: 0.1, anisotropy: 0.6 })] }])
      record.ms.one = await measure()
      const eight = Array.from({ length: 7 }, (_, i) => volumeObject(`v${i}`, [-2 + (i % 4) * 1.3, 0.8 + Math.floor(i / 4) * 1.2, -1.5 + (i % 3) * 1.4], [1.2, 1, 1.2], { scatter_density: 0.3 }, 81 + i))
      await change([{ op: 'add_objects', objects: eight }])
      record.ms.eight = await measure()
      await change([{ op: 'update_objects', updates: [gid('room'), ...eight.map((v) => v.id)].map((id) => ({ object_id: id, volume: { cast_shadow: true } })) }])
      record.ms.eight_casting = await measure()
      // With volumes casting the materials carry their shadow (AC-W5-11, owner 2026-10-23): what a new light costs then.
      record.new_light_compile_casting_ms = await newLight()
      record.added_ms = { one: Number((record.ms.one - record.ms.none).toFixed(2)), eight: Number((record.ms.eight - record.ms.none).toFixed(2)), eight_casting_over_eight: Number((record.ms.eight_casting - record.ms.eight).toFixed(2)) }
    }
    console.log(`W5 performance: ${JSON.stringify(record, null, 1)}`)
    await writeFile(`${dir}/w5-perf-${label}.json`, `${JSON.stringify(record, null, 2)}\n`)
    assert.deepEqual(errors, [], errors.join('\n'))
    await context.close()
  } finally {
    await gpu.close()
  }
})

// The orthographic views draw lit with the workbench (render/engines.ts): rays from the near plane, the volume
// where it stands, nothing beyond its outline.
await check('W5 ortho: a volume draws in an orthographic view', async () => {
  const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
  await context.addInitScript(([key, view]) => localStorage.setItem(key, JSON.stringify(view)), [`creative-scene-view:qa-account:${DOC}`, obliqueOrtho])
  const server = new SceneServer()
  referenceScene(server, 'physical-noon', (scene) => Object.assign(scene.render, { exposure_mode: 'ambient', exposure_compensation: 0 }))
  const { page, errors } = await open(server, { context })
  const { box: area } = await viewportCenter(page)
  await shading(page, '最终渲染')
  const settle = async () => {
    await waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiling)) === 0, 'compiled', 60_000)
    await sleep(400)
    return settledShot(page, area)
  }
  const before = await settle()
  assert.equal(await page.evaluate(() => window.__sceneGPU.marches), 0, 'no march without volumes')
  server.external([{ op: 'add_objects', objects: [volumeObject('haze', [0, 1, 0], [2, 2, 2], { scatter_density: 0.8 })] }])
  await page.evaluate(() => window.__invalidate())
  const after = await settle()
  await shot(page, 'w5-ortho')
  const c = await pixelChange(page, before, after)
  console.log(`W5 ortho: ${(c.changed * 100).toFixed(1)}% changed`)
  assert.ok(c.changed > 0.01 && c.changed < 0.3, 'the volume shows, and only where it stands')
  assert.deepEqual(errors, [])
  await context.close()
})

// The real-time GI's viewport composites the helpers' pass over the picture too: with the volumes' textures in the
// same pass it sampled 17, over WebGPU's 16 a stage, and drew black (realtimeGI.ts). SwiftShader allows more.
await check('W5 real-time GI viewport: the rendered view draws, with and without a volume, the helpers over it', async () => {
  const gpu = await w5Metal()
  try {
    for (const volume of [false, true]) {
      const server = new SceneServer()
      referenceScene(server, 'cornell', (scene) => {
        scene.render = { ...scene.render, engine: 'realtime_gi' }
        if (volume) scene.objects.push(volumeObject('fog', [0, 1.5, -1.5], [4, 3, 3], { scatter_density: 0.15, cast_shadow: true }))
      })
      const { context, page, errors } = await open(server, gpu ? { context: await gpu.newContext({ viewport: { width: 1280, height: 820 } }) } : {})
      const { box: area } = await viewportCenter(page)
      await shading(page, '最终渲染')
      await waitFor(async () => (await page.evaluate(() => window.__sceneGPU.compiling)) === 0, 'compiled', 60_000)
      await sleep(1000)
      const lit = await lumaStats(page, await settledShot(page, area), { level: 250 })
      console.log(`W5 real-time GI viewport${volume ? ' with a volume' : ''}: mean ${lit.mean.toFixed(1)}`)
      assert.ok(lit.mean > 10, `the picture draws${volume ? ' with a volume' : ''}: mean ${lit.mean.toFixed(1)}`)
      assert.deepEqual(errors, [])
      await context.close()
    }
  } finally {
    await gpu?.close()
  }
})

// R2 tasks 1.2: every picture builds and frees a whole GI pipeline (prepass, lighting pass, SSGI, SSR, contact
// shadows, denoisers, TRAA). Freed whole, the renderer holds as much after the 48th picture as after the first.
await check('real-time GI: 48 pictures in a row leave nothing behind on the device', async () => {
  const context = await browser.newContext()
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (e) => errors.push(e.message))
  page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
  await page.goto(`${base}/.scene-gi-qa.html`)
  await page.waitForFunction(() => window.__ready === true)
  const scene = JSON.parse(readFileSync(new URL('fixtures/scene-reference/materials.json', import.meta.url), 'utf8'))
  scene.render = { ...scene.render, engine: 'realtime_gi' }
  const held = await page.evaluate(([s]) => window.__shots(s, 48, 128), [scene])
  assert.equal(held.length, 48)
  assert.deepEqual(held.at(-1), held[0], `after the first: ${JSON.stringify(held[0])}, after the 48th: ${JSON.stringify(held.at(-1))}`)
  assert.deepEqual(errors, [], errors.join('\n'))
  await context.close()
})

// The sky is the picture's (render/sceneRenderer.ts SKY_LAYER), drawn behind the scene by the real-time GI's lighting
// pass as by the workbench. On the helpers' layer it had gone with them into the viewport's own pass, which a picture to
// keep has not: the procedural and the physical sky were black behind every real-time GI export (R2 c5b302eb).
await check('real-time GI: a lit picture draws the sky behind the scene, procedural and physical, as the workbench does', async () => {
  const context = await browser.newContext()
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (e) => errors.push(e.message))
  page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
  await page.goto(`${base}/.scene-gi-qa.html`)
  await page.waitForFunction(() => window.__ready === true)
  // The top left of both yards is sky (scripts/fixtures/scene-reference).
  const sky = [0.02, 0.02, 0.2, 0.15]
  for (const name of ['noon', 'physical-noon']) {
    const scene = JSON.parse(readFileSync(new URL(`fixtures/scene-reference/${name}.json`, import.meta.url), 'utf8'))
    // As the server reads it out (referenceScene): the sky completed, the stored camera exposed by hand.
    scene.environment.sky = completeSky(scene.environment.sky)
    for (const o of scene.objects) if (o.camera) Object.assign(o.camera, { exposure_mode: 'manual', exposure_compensation: 0, ...o.camera })
    const seen = {}
    for (const engine of ['workbench', 'realtime_gi']) {
      scene.render = { ...scene.render, engine }
      const { means } = await page.evaluate(([s, box]) => window.__regions(s, 'lit', 256, [box]), [scene, sky])
      seen[engine] = means[0]
    }
    const [r, , b] = seen.realtime_gi
    assert.ok(b > 60 && b >= r, `${name}: the real-time GI's sky is a sky: ${seen.realtime_gi.map(Math.round)}`)
    for (let c = 0; c < 3; c++) assert.ok(Math.abs(seen.realtime_gi[c] - seen.workbench[c]) < 12, `${name}: the same sky as the workbench's: ${seen.realtime_gi.map(Math.round)} against ${seen.workbench.map(Math.round)}`)
  }
  assert.deepEqual(errors, [], errors.join('\n'))
  await context.close()
})

// R2 AC-R2-2 and AC-R2-4: the renderer in effect (render/engines.ts effectiveEngine). The staleness of exports is
// the server's fingerprint (creativescene TestEngineFingerprintsOnlyClayAndLit); the in-memory server has none.
await check('renderer in effect: real-time GI converges in the rendered view and stops; clay, wireframe, material preview, line art and orthographic views stay the workbench’s', async () => {
  const server = new SceneServer()
  seed(server, [
    boxOf('post', 5, { position: [0, 0, 0] }, { width: 0.6, height: 1.5, depth: 0.6 }),
    boxOf('wall', 6, { position: [1.2, 0, -1] }, { width: 3, height: 2, depth: 0.2 }, { preset: 'paint@1', params: { color: '#c03020' } }),
  ])
  const { context, page, errors } = await open(server)
  const { box } = await viewportCenter(page)
  const frames = () => page.evaluate(() => window.__sceneGPU.viewportFrames)
  const settledFrames = async (n, what) => {
    await waitFor(async () => (await frames()) === n, what, 60_000)
    await sleep(500)
    assert.equal(await frames(), n, `${what}: stays at ${n}`)
  }
  const engine = (value) => page.getByRole('combobox', { name: '渲染器' }).selectOption(value)
  await page.getByRole('tablist', { name: '属性' }).getByRole('tab', { name: '渲染' }).click()
  const look = {}
  for (const mode of ['线框', '材质预览', '白模', '最终渲染']) {
    await shading(page, mode)
    await settledFrames(1, `${mode}, workbench`)
    look[mode] = await settledShot(page, box)
  }
  await engine('realtime_gi')
  await waitFor(() => server.scene.render.engine === 'realtime_gi', 'renderer written')
  // The rendered view with the real-time GI draws on by itself, to its frame count (engines.ts convergeFrames), then stops.
  const giFrames = 12
  await settledFrames(giFrames, 'rendered, real-time GI')
  // By more than 24 levels: the GI's bounce on a post and a wall is gentle. More than 48 once held only because the sky
  // was drawn with the helpers, and the grid laid over it took its colour (2.3% of pixels past 24, 0.8% past 48).
  assert.ok((await pixelDiff(page, look['最终渲染'], await settledShot(page, box), 24)) > 0.01, 'the real-time GI changes the rendered view')
  for (const mode of ['白模', '线框', '材质预览']) {
    await shading(page, mode)
    await settledFrames(1, `${mode}, real-time GI chosen`)
    assert.equal(await pixelDiff(page, look[mode], await settledShot(page, box), 0), 0, `${mode} is the workbench’s, pixel for pixel`)
  }
  // An orthographic view is drawn by the workbench and says so.
  await shading(page, '最终渲染')
  await focusEditor(page)
  await page.keyboard.press('Digit5')
  await page.getByRole('status').filter({ hasText: '正交视图按工作台显示' }).waitFor()
  await settledFrames(1, 'orthographic')
  await page.keyboard.press('Digit5')
  await waitFor(async () => (await page.getByRole('status').filter({ hasText: '正交视图按工作台显示' }).count()) === 0, 'the badge goes in perspective')

  // Another tab of the same scene follows; Cmd+Z takes the renderer back.
  const other = await open(server)
  await other.page.getByRole('tablist', { name: '属性' }).getByRole('tab', { name: '渲染' }).click()
  assert.equal(await other.page.getByRole('combobox', { name: '渲染器' }).inputValue(), 'realtime_gi', 'the other tab reads the renderer')
  await saved(page)
  await focusEditor(page)
  await page.keyboard.press(`${modKey}+KeyZ`)
  await waitFor(() => server.scene.render.engine === 'workbench', 'undo')
  await other.page.evaluate(() => window.__invalidate())
  await waitFor(async () => (await other.page.getByRole('combobox', { name: '渲染器' }).inputValue()) === 'workbench', 'the other tab follows the undo')
  assert.deepEqual(other.errors, [])
  await other.context.close()
  assert.deepEqual(errors, [])
  await context.close()

  // Exports: lit is drawn by the scene's renderer, clay and line art by the workbench whatever it says.
  const shots = await browser.newContext()
  const shooter = await shots.newPage()
  await shooter.goto(`${base}/.scene-gi-qa.html`)
  await shooter.waitForFunction(() => window.__ready === true)
  const scene = JSON.parse(readFileSync(new URL('fixtures/scene-reference/cornell.json', import.meta.url), 'utf8'))
  const hashes = {}
  for (const engineName of ['workbench', 'realtime_gi']) {
    scene.render = { ...scene.render, engine: engineName }
    for (const kind of ['clay', 'line', 'lit']) hashes[`${engineName}:${kind}`] = await shooter.evaluate(([s, k]) => window.__hash(s, k, 256), [scene, kind])
  }
  assert.equal(hashes['realtime_gi:line'], hashes['workbench:line'], 'line art is the workbench’s')
  assert.equal(hashes['realtime_gi:clay'], hashes['workbench:clay'], 'clay is the workbench’s')
  assert.notEqual(hashes['realtime_gi:lit'], hashes['workbench:lit'], 'lit follows the renderer')
  await shots.close()
})

/** A JPEG's size, from its start-of-frame marker. */
function jpegSize(bytes) {
  for (let i = 2; i < bytes.length;) {
    const marker = bytes[i + 1]
    const length = bytes.readUInt16BE(i + 2)
    if (marker >= 0xc0 && marker <= 0xc3) return { width: bytes.readUInt16BE(i + 7), height: bytes.readUInt16BE(i + 5) }
    i += 2 + length
  }
  return null
}

// R2 AC-R2-6: the output resolution. Every level and three shapes through the export's own drawing; then the
// editor's export at 8K, a device too small for 4K, and a device lost at 4K. 8K on this machine's GPU (the
// reference device): SwiftShader loses its context drawing 8K, which is the failure checked at 4K below.
await check('output resolution: 1K to 8K long sides at 1:1, 3:2 and portrait, 4K and 8K clay as JPEG; a device too small or lost at 4K fails and says so', async () => {
  const gpu = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
  try {
  const shapes = { sq: '1:1', wide: '3:2', tall: '2:3' }
  const cameras = Object.entries(shapes).map(([name, aspect], i) => cameraObject(objectID(`cam${name}`), name, 10 + i, { aspect }, [i * 2 - 2, 1.6, 8]))
  const model = new SceneServer()
  model.external([{ op: 'add_objects', objects: [{ ...boxObject(objectID('box'), '方块', 0, 1), transform: { ...identity(), position: [0, 0, 0] } }, ...cameras] }])
  const shots = await gpu.newContext()
  const shooter = await shots.newPage()
  await shooter.goto(`${base}/.scene-gi-qa.html`)
  await shooter.waitForFunction(() => window.__ready === true)
  const plan = { cameraObjectIDs: cameras.map((c) => c.id), passes: ['clay', 'line'] }
  for (const [resolution, side] of [['1k', 1024], ['2k', 2048], ['4k', 4096], ['8k', 8192]]) {
    const doc = { scene: { ...structuredClone(model.scene), render: { ...model.scene.render, resolution } }, models: {}, revision: '1' }
    const images = await shooter.evaluate(([d, p]) => window.__sizes(d, p), [doc, plan])
    assert.ok(Array.isArray(images), `${resolution}: ${JSON.stringify(images)}`)
    for (const img of images) {
      const aspect = shapes[cameras.find((c) => c.id === img.camera).name]
      const [w, h] = aspect.split(':').map(Number)
      const want = w >= h ? { width: side, height: Math.round((side * h) / w) } : { width: Math.round((side * w) / h), height: side }
      assert.deepEqual({ width: img.width, height: img.height }, want, `${resolution} ${aspect} ${img.pass}`)
      const jpeg = img.pass === 'clay' && (resolution === '4k' || resolution === '8k')
      assert.equal(img.type, jpeg ? 'image/jpeg' : 'image/png', `${resolution} ${img.pass}`)
    }
  }
  await shots.close()

  // The editor at 8K: the dialog says so, and an 8K square goes up as JPEG with line art as PNG.
  const server = new SceneServer()
  server.external([{ op: 'add_objects', objects: [{ ...boxObject(objectID('box'), '方块', 0, 1), transform: { ...identity(), position: [0, 0, 0] } }, cameraObject(objectID('camsq'), 'sq', 10, { aspect: '1:1' }, [0, 1.6, 8])] }])
  server.scene.render.resolution = '8k'
  const metal = await open(server, { query: '?export=1', context: await gpu.newContext({ viewport: { width: 1280, height: 820 } }) })
  await metal.page.getByRole('button', { name: '导出' }).first().click()
  const big = metal.page.getByRole('dialog', { name: '导出分镜' })
  await big.waitFor()
  assert.match(await big.textContent(), /长边 8192（8K）/)
  await big.getByRole('button', { name: '导出' }).click()
  await waitFor(() => server.exports.length === 1, 'the 8K export submitted', 600_000)
  const [shot] = server.exports[0].payload.shots
  const bytes = Object.fromEntries(shot.images.map((img) => [img.pass, server.uploadBytes.get(img.upload_id)]))
  assert.deepEqual(jpegSize(bytes.clay), { width: 8192, height: 8192 }, 'clay: an 8K JPEG')
  assert.deepEqual(pngSize(bytes.line), { width: 8192, height: 8192 }, 'line art: an 8K PNG')
  console.log(`8K square: clay JPEG ${(bytes.clay.length / 1e6).toFixed(1)} MB, line PNG ${(bytes.line.length / 1e6).toFixed(1)} MB`)
  assert.deepEqual(metal.errors, [])
  await metal.context.close()

  // A device too small for 4K fails before drawing; a device lost at 4K is not retried.
  server.scene.render.resolution = '4k'
  const { context, page, errors } = await open(server, { query: '?export=1' })
  const dialog = page.getByRole('dialog', { name: '导出分镜' })
  await page.evaluate(() => { window.__sceneGPU.textureLimit = 2048 })
  await page.getByRole('button', { name: '导出' }).first().click()
  await dialog.waitFor()
  await dialog.getByRole('button', { name: '导出' }).click()
  await page.getByText('当前设备最大只能导出长边 2048 px 的图，请选更低的输出分辨率').waitFor({ timeout: 120_000 })
  await page.evaluate(() => { window.__sceneGPU.textureLimit = null; window.__sceneGPU.loseNext('offscreen', 1) })
  await dialog.getByRole('button', { name: '导出' }).click()
  await page.getByText('渲染设备在按 4K 出图时丢失，当前设备可能画不了这个分辨率，可以换低一档再导出').waitFor({ timeout: 300_000 })
  assert.equal(server.exports.length, 1, 'nothing more placed')
  assert.deepEqual(errors.filter((e) => !/lost|Lost/.test(e)), [])
  await context.close()
  } finally {
    await gpu.close()
  }
})

// R2 AC-R2-5 (tasks 5.1): an export's residual noise in the box room, where only bounced light reaches the upper
// back wall, the hardest of the reference scenes. This machine's GPU: SwiftShader takes minutes for K frames at 2K.
await check('real-time GI: a 2K export of the box room is within its residual noise, K frames averaged after the warm-up', async () => {
  const gpu = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
  try {
    const page = await gpu.newPage()
    await page.goto(`${base}/.scene-gi-qa.html`)
    await page.waitForFunction(() => window.__ready === true)
    const scene = JSON.parse(readFileSync(new URL('fixtures/scene-reference/cornell.json', import.meta.url), 'utf8'))
    scene.render = { ...scene.render, engine: 'realtime_gi' }
    // The upper back wall, R0's box.
    const wall = [0.22, 0.183, 0.781, 0.513]
    const out = await page.evaluate(([s, b]) => window.__noise(s, 'lit', 2048, b), [scene, wall])
    console.log(`residual noise, box room lit ${out.width}×${out.height}: ${out.noise.toFixed(3)} (limit 1.5), ${(out.ms / 1000).toFixed(1)} s`)
    assert.ok(out.noise <= 1.5, `lit: ${out.noise}`)
  } finally {
    await gpu.close()
  }
})

// The real-time GI's ambient fill (litLook.ts AMBIENT_FILL_GI), again under AgX (W1 tasks 5.1): how much
// brighter or darker each reference's lit export is with the real-time GI than with the workbench stays what
// it was before W1, within 5 points. Measured the same way before W1 on develop f48841a3 (ACES Filmic), this
// machine's GPU. Opt-in, SCENE_E2E_GI_FILL=1: about a minute.
const giAgainstWorkbenchBeforeW1 = { room: 0.068, noon: -0.191, cornell: 0.329 }
if (process.env.SCENE_E2E_GI_FILL) await check('real-time GI: against the workbench, the reference lit exports are as much brighter or darker as before W1', async () => {
  const gpu = await chromium.launch({ headless: true, args: metal })
  try {
    const page = await gpu.newPage()
    await page.goto(`${base}/.scene-gi-qa.html`)
    await page.waitForFunction(() => window.__ready === true)
    const grey = ([r, g, b]) => 0.2126 * r + 0.7152 * g + 0.0722 * b
    const off = []
    for (const [name, before] of Object.entries(giAgainstWorkbenchBeforeW1)) {
      const reference = JSON.parse(readFileSync(new URL(`fixtures/scene-reference/${name}.json`, import.meta.url), 'utf8'))
      const mean = {}
      for (const engine of ['workbench', 'realtime_gi']) {
        const r = await page.evaluate(([s]) => window.__regions(s, 'lit', 2048, [[0, 0, 1, 1]]), [{ ...reference, render: { ...reference.render, engine } }])
        mean[engine] = grey(r.means[0])
      }
      const now = mean.realtime_gi / mean.workbench - 1
      console.log(`${name}: workbench ${mean.workbench.toFixed(1)}, real-time GI ${mean.realtime_gi.toFixed(1)} (${(now * 100).toFixed(1)}%, before W1 ${(before * 100).toFixed(1)}%)`)
      if (Math.abs(now - before) > 0.05) off.push(`${name} ${(now * 100).toFixed(1)}% against ${(before * 100).toFixed(1)}%`)
    }
    assert.deepEqual(off, [])
  } finally {
    await gpu.close()
  }
})

// R2 AC-R2-3 (tasks 7.1): what the real-time GI adds to the box room, measured in boxes of its 2K export against the
// workbench's: the floor by the red wall and the cube's face towards it turn red, the metal ball and the glossy
// floor show the walls, the cube's front takes the floor's bounce. The contact is recorded, not compared: the spot
// light clips the floor at the cube's foot to white in both, and the workbench's shadows are already black. Clay
// is the workbench's whatever the renderer (engines.ts effectiveEngine). SCENE_E2E_EVIDENCE=<dir> keeps the two
// pictures and the numbers for the owner. This machine's GPU.
await check('real-time GI: the box room bleeds red beside the red wall, mirrors the walls in the ball and darkens contacts, against the workbench', async () => {
  const gpu = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
  try {
    const page = await gpu.newPage()
    await page.goto(`${base}/.scene-gi-qa.html`)
    await page.waitForFunction(() => window.__ready === true)
    const cornell = JSON.parse(readFileSync(new URL('fixtures/scene-reference/cornell.json', import.meta.url), 'utf8'))
    const boxes = {
      floor_by_red_wall: [0.11, 0.94, 0.22, 0.99],
      cube_face_to_red_wall: [0.243, 0.7, 0.257, 0.88],
      ball: [0.61, 0.7, 0.77, 0.9],
      floor_by_green_wall: [0.8, 0.94, 0.88, 0.99],
      cube_front_foot: [0.3, 0.9, 0.42, 0.925],
      cube_contact: [0.3, 0.955, 0.42, 0.975],
    }
    const names = Object.keys(boxes)
    const out = {}
    for (const engine of ['workbench', 'realtime_gi']) {
      const scene = { ...cornell, render: { ...cornell.render, engine } }
      const r = await page.evaluate(([s, b]) => window.__regions(s, 'lit', 2048, b), [scene, Object.values(boxes)])
      out[`${engine}-lit`] = Object.fromEntries(names.map((n, i) => [n, r.means[i].map((v) => Number(v.toFixed(2)))]))
      if (process.env.SCENE_E2E_EVIDENCE) await writeFile(`${process.env.SCENE_E2E_EVIDENCE}/cornell-${engine}-lit.jpg`, Buffer.from(r.jpeg, 'base64'))
    }
    const redGreen = ([r, g]) => r / g
    const grey = ([r, g, b]) => 0.299 * r + 0.587 * g + 0.114 * b
    const meanDiff = (a, b) => (Math.abs(a[0] - b[0]) + Math.abs(a[1] - b[1]) + Math.abs(a[2] - b[2])) / 3
    const wb = out['workbench-lit']
    const gi = out['realtime_gi-lit']
    const measured = {
      lit_floor_by_red_wall_red_green: [redGreen(wb.floor_by_red_wall), redGreen(gi.floor_by_red_wall)],
      lit_cube_face_red_green: [redGreen(wb.cube_face_to_red_wall), redGreen(gi.cube_face_to_red_wall)],
      lit_ball_difference: meanDiff(wb.ball, gi.ball),
      lit_floor_by_green_wall_green_red: [1 / redGreen(wb.floor_by_green_wall), 1 / redGreen(gi.floor_by_green_wall)],
      lit_cube_front_foot_grey: [grey(wb.cube_front_foot), grey(gi.cube_front_foot)],
      lit_cube_contact_grey: [grey(wb.cube_contact), grey(gi.cube_contact)],
    }
    console.log(JSON.stringify(measured, (_, v) => (typeof v === 'number' ? Number(v.toFixed(3)) : v)))
    if (process.env.SCENE_E2E_EVIDENCE) await writeFile(`${process.env.SCENE_E2E_EVIDENCE}/cornell-regions.json`, `${JSON.stringify({ machine: machineInfo(), chromium: gpu.version(), boxes, means: out, measured }, null, 2)}\n`)
    // 2026-10-21, M4 Pro: 1.005 → 1.105, 0.951 → 1.606, 22.2; the floor's red was 1.21 with the recurrent denoiser (tasks 1.1).
    assert.ok(measured.lit_floor_by_red_wall_red_green[1] >= measured.lit_floor_by_red_wall_red_green[0] + 0.07, 'the floor by the red wall turns red')
    assert.ok(measured.lit_cube_face_red_green[1] >= measured.lit_cube_face_red_green[0] + 0.3, 'the cube’s face to the red wall turns red')
    assert.ok(measured.lit_ball_difference >= 10, 'the ball mirrors more than the workbench’s environment')
    assert.ok(measured.lit_floor_by_green_wall_green_red[1] > measured.lit_floor_by_green_wall_green_red[0], 'the glossy floor mirrors the green wall')
    assert.ok(measured.lit_cube_front_foot_grey[1] > measured.lit_cube_front_foot_grey[0], 'the cube’s front takes the floor’s bounce')
  } finally {
    await gpu.close()
  }
})

// R2 AC-R2-8: the real-time GI on the WebGL2 backend. On SwiftShader (no WebGPU there) the box room's lit draws
// without an error, at render_view's frames; on this machine's GPU, forced onto WebGL2, its 2K export is within
// AC-R1-1's limits of the WebGPU one (Epic: mean ≤ 1.5, pixels off by more than 8 ≤ 1 %). Clay is the workbench's.
async function giOnWebGL2(target, side, use, forceWebGL) {
  const page = await target.newPage()
  const errors = []
  const backends = []
  page.on('pageerror', (e) => errors.push(e.message))
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push(m.text())
    if (m.text().startsWith('scene renderer')) backends.push(m.text())
  })
  await page.goto(`${base}/.scene-gi-qa.html`)
  await page.waitForFunction(() => window.__ready === true)
  const cornell = JSON.parse(readFileSync(new URL('fixtures/scene-reference/cornell.json', import.meta.url), 'utf8'))
  const scene = { ...cornell, render: { ...cornell.render, engine: 'realtime_gi' } }
  const pictures = {}
  for (const force of forceWebGL) pictures[force] = Buffer.from(await page.evaluate(([s, n, u, f]) => window.__png(s, 'lit', n, u, f), [scene, side, use, force]), 'base64')
  return { page, errors, backends, pictures }
}

await check('real-time GI on WebGL2: SwiftShader draws the box room’s lit without an error', async () => {
  const context = await browser.newContext()
  const { errors, backends, pictures } = await giOnWebGL2(context, 512, 'view', [false])
  assert.ok(backends.length > 0 && backends.every((b) => b === 'scene renderer (offscreen): webgl2'), backends.join(', '))
  assert.ok(pictures.false.length > 10_000)
  assert.deepEqual(errors, [])
  await context.close()
})

await check('real-time GI on WebGL2: the box room’s 2K export forced onto WebGL2 is within AC-R1-1 of WebGPU’s', async () => {
  const gpu = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
  try {
    const { page, errors, backends, pictures } = await giOnWebGL2(gpu, 2048, 'export', [false, true])
    assert.ok(backends.includes('scene renderer (offscreen): webgpu') && backends.includes('scene renderer (offscreen): webgl2'), backends.join(', '))
    const m = await compareShots(page, pictures.false, pictures.true)
    const measured = { lit: { mean: Number(m.mean.toFixed(3)), over_percent: Number(m.over.toFixed(3)) } }
    if (process.env.SCENE_E2E_EVIDENCE) await writeFile(`${process.env.SCENE_E2E_EVIDENCE}/cornell-gi-lit-webgpu-webgl2.jpg`, m.sheet)
    console.log(`real-time GI, WebGPU against WebGL2: ${JSON.stringify(measured)}`)
    if (process.env.SCENE_E2E_EVIDENCE) await writeFile(`${process.env.SCENE_E2E_EVIDENCE}/cornell-gi-webgpu-webgl2.json`, `${JSON.stringify({ machine: machineInfo(), chromium: gpu.version(), measured }, null, 2)}\n`)
    assert.ok(measured.lit.mean <= 1.5 && measured.lit.over_percent <= 1, JSON.stringify(measured.lit))
    assert.deepEqual(errors, [])
  } finally {
    await gpu.close()
  }
})

// R2 tasks 7.4: a camera's frustum and an empty's cross are drawn in the real-time GI's rendered view, but are not
// in the scene it lights: they once stood in its prepass, and the contact shadows and the SSGI darkened the ground
// around them in grain. With and without them, the view differs by little more than their lines.
await check('real-time GI: camera frustums and empties are drawn in the rendered view but neither shade nor occlude', async () => {
  const gpu = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
  try {
    const shots = []
    for (const helpers of [false, true]) {
      const server = new SceneServer()
      server.scene.render = { ...server.scene.render, engine: 'realtime_gi' }
      if (helpers) seed(server, [cameraObject(objectID('cam'), '机位', 5, {}, [0, 0.15, 0]), emptyObject(objectID('empty'), '空物体', 6, [0.8, 0.1, 0.4])])
      const { page, errors } = await open(server, { context: await gpu.newContext() })
      const { box } = await viewportCenter(page)
      await shading(page, '最终渲染')
      shots.push(await settledShot(page, box))
      assert.deepEqual(errors, [])
      if (helpers) {
        if (process.env.SCENE_E2E_EVIDENCE) for (const [i, png] of shots.entries()) await writeFile(`${process.env.SCENE_E2E_EVIDENCE}/helpers-${i}.png`, png)
        const diff = await pixelDiff(page, shots[0], shots[1])
        console.log(`real-time GI with a camera and an empty: ${(diff * 100).toFixed(2)}% of the view differs`)
        assert.ok(diff < 0.01, `${(diff * 100).toFixed(2)}% of the view differs`)
      }
    }
  } finally {
    await gpu.close()
  }
})

// R2 tasks 7.5: the fog is laid over the real-time GI's composite, the indirect light and the reflections with
// the rest. With every surface past the fog's far end, nothing of a box's colour shows; the materials' own fog
// once left the GI and the AO it took apart and put back unfogged, and a red box showed through.
await check('real-time GI: the fog covers the indirect light as it covers the rest', async () => {
  const gpu = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
  try {
    const shots = []
    for (const color of ['#ffffff', '#ff2020']) {
      const server = new SceneServer()
      server.scene.render = { ...server.scene.render, engine: 'realtime_gi' }
      server.scene.environment.fog = { color: '#808080', near: 0.01, far: 0.02 }
      seed(server, [{ id: objectID('box'), kind: 'primitive', name: '盒子', order: 5, transform: { ...identity(), scale: [4, 4, 4] }, primitive: { builtin: 'box@1' }, material_slots: { main: { preset: 'paint@1', params: { color } } } }])
      const { page, errors } = await open(server, { context: await gpu.newContext() })
      const { box } = await viewportCenter(page)
      await shading(page, '最终渲染')
      shots.push(await settledShot(page, box))
      assert.deepEqual(errors, [])
      if (shots.length === 2) {
        if (process.env.SCENE_E2E_EVIDENCE) for (const [i, png] of shots.entries()) await writeFile(`${process.env.SCENE_E2E_EVIDENCE}/fog-${i}.png`, png)
        const diff = await pixelDiff(page, shots[0], shots[1])
        console.log(`real-time GI in full fog, a white box against a red one: ${(diff * 100).toFixed(2)}% of the view differs`)
        assert.ok(diff < 0.005, `${(diff * 100).toFixed(2)}% of the view differs`)
      }
    }
  } finally {
    await gpu.close()
  }
})

// R2 tasks 7.2: a light added in the real-time GI's rendered view is compiled first for both its passes, as the
// workbench's is (R1): no pipeline refused (the prepass and the lighting pass once shared a stand-in's material),
// no frame held while the lighting pass compiles (5 s before). R0's performance scene, this machine's GPU.
await check('real-time GI: a light added in the rendered view compiles first, without an error or a held frame', async () => {
  const gpu = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
  try {
    const context = await gpu.newContext({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 })
    const server = new SceneServer()
    perfScene(server)
    server.scene.render = { ...server.scene.render, engine: 'realtime_gi' }
    const { page, errors } = await open(server, { context })
    const { x, y } = await viewportCenter(page)
    await page.mouse.move(x, y)
    await shading(page, '最终渲染')
    await settledShot(page, { x: x - 50, y: y - 50, width: 100, height: 100 })
    await orbitFrames(page, 1000)
    const measuring = orbitFrames(page, 3000)
    await sleep(800)
    const at = await page.evaluate(() => performance.now())
    await page.keyboard.press('Shift+KeyA')
    await page.getByRole('menuitem', { name: '点光', exact: true }).click()
    const longest = Math.max(...gaps([at, ...(await measuring).filter((t) => t >= at)]))
    console.log(`real-time GI: adding a light, longest frame ${longest.toFixed(0)} ms`)
    assert.ok(longest < 1000, `longest frame ${longest} ms`)
    assert.deepEqual(errors, [], errors.join('\n'))
    await context.close()
  } finally {
    await gpu.close()
  }
})

// SCENE_E2E_PERF_R2=<dir> (R2 AC-R2-9, tasks 7.2), this machine's GPU, 1600 × 1000 at 2× pixels as R1's AC-9:
// the viewport's frame times with the real-time GI (its 1× pixels) in the box room and R0's performance scene, and
// the workbench's in the latter against R1's perf.json; how long the viewport goes on drawing after the last
// input; the longest frame when a light is added in the real-time GI's rendered view; each resolution's export
// image time for both renderers (the box room, clay and lit, each on a fresh shooter after a warm one). perf.json
// goes to <dir>; the limits are judged in the record, not here, except that nothing errs.
if (process.env.SCENE_E2E_PERF_R2) await check('R2 performance: viewport frames, settling and export times on this machine’s GPU', async () => {
  const dir = process.env.SCENE_E2E_PERF_R2
  const gpu = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
  const record = { machine: machineInfo(), chromium: gpu.version(), viewport: '1600 × 1000, 2× pixels', viewport_frames: {}, settle: {}, exports: {} }
  // rAF times and the viewport's frame count after the last of the input, until the count has not moved for 1.5 s.
  const settle = (page) => page.evaluate(() => new Promise((resolve) => {
    const el = document.querySelector('.cc-scene-editor canvas')
    el.dispatchEvent(new WheelEvent('wheel', { deltaX: 4, deltaY: 0, deltaMode: 0, bubbles: true, cancelable: true }))
    const start = performance.now()
    let count = window.__sceneGPU.viewportFrames
    let changed = start
    const tick = (t) => {
      const now = window.__sceneGPU.viewportFrames
      if (now !== count) {
        count = now
        changed = t
      }
      if (t - changed < 1500) requestAnimationFrame(tick)
      else resolve({ ms: Math.round(changed - start), frames: count })
    }
    requestAnimationFrame(tick)
  }))
  try {
    for (const [name, engine] of [['perf', 'realtime_gi'], ['cornell', 'realtime_gi'], ['perf', 'workbench']]) {
      const context = await gpu.newContext({ viewport: { width: 1600, height: 1000 }, deviceScaleFactor: 2 })
      const server = new SceneServer()
      if (name === 'perf') perfScene(server)
      else referenceScene(server, name)
      server.scene.render = { ...server.scene.render, engine }
      const { page, errors } = await open(server, { context })
      const { x, y } = await viewportCenter(page)
      await page.mouse.move(x, y)
      const label = `${name}-${engine}`
      record.viewport_frames[label] = {}
      record.settle[label] = {}
      const modes = engine === 'workbench' ? [['线框', 'wireframe'], ['白模', 'solid'], ['材质预览', 'preview'], ['最终渲染', 'rendered']] : [['最终渲染', 'rendered']]
      for (const [mode, key] of modes) {
        await shading(page, mode)
        await settledShot(page, { x: x - 50, y: y - 50, width: 100, height: 100 })
        await orbitFrames(page, 1000)
        const stats = frameStats(await orbitFrames(page, 4000))
        record.viewport_frames[label][key] = Object.fromEntries(Object.entries(stats).map(([k, v]) => [k, Number(v.toFixed(1))]))
        record.settle[label][key] = await settle(page)
        console.log(`  ${label} ${key}: mean ${stats.mean.toFixed(1)} ms, p95 ${stats.p95.toFixed(1)} ms; settles in ${record.settle[label][key].ms} ms after ${record.settle[label][key].frames} frames`)
      }
      if (name === 'perf' && engine === 'realtime_gi') {
        // The rendered view is up: a point light added, the longest frame after it.
        const measuring = orbitFrames(page, 3000)
        await sleep(800)
        const at = await page.evaluate(() => performance.now())
        await page.keyboard.press('Shift+KeyA')
        await page.getByRole('menuitem', { name: '点光', exact: true }).click()
        const times = await measuring
        record.gi_light_add_ms = Math.round(Math.max(...gaps([at, ...times.filter((t) => t >= at)])))
        console.log(`  ${label}: adding a light, longest frame ${record.gi_light_add_ms} ms`)
      }
      assert.deepEqual(errors, [], `${label}: ${errors.join('\n')}`)
      await context.close()
    }
    const page = await gpu.newPage()
    await page.goto(`${base}/.scene-gi-qa.html`)
    await page.waitForFunction(() => window.__ready === true)
    const cornell = JSON.parse(readFileSync(new URL('fixtures/scene-reference/cornell.json', import.meta.url), 'utf8'))
    for (const engine of ['workbench', 'realtime_gi']) {
      const scene = { ...cornell, render: { ...cornell.render, engine } }
      // Clay is the workbench's whatever the renderer.
      for (const kind of engine === 'workbench' ? ['clay', 'lit'] : ['lit']) {
        await page.evaluate(([s, k]) => window.__timed(s, k, '1k'), [scene, kind]) // compiled, caches warm
        for (const resolution of ['1k', '2k', '4k', '8k']) {
          const out = await page.evaluate(([s, k, r]) => window.__timed(s, k, r), [scene, kind, resolution])
          record.exports[`${engine}-${kind}-${resolution}`] = out
          console.log(`  export ${engine} ${kind} ${resolution}: ${out.error ?? `${out.width}×${out.height} ${out.type} in ${(out.ms / 1000).toFixed(1)} s, ${(out.bytes / 1e6).toFixed(2)} MB`}`)
        }
      }
    }
  } finally {
    await writeFile(`${dir}/perf.json`, `${JSON.stringify(record, null, 2)}\n`)
    await gpu.close()
  }
})

// SCENE_E2E_GPU_8K=1 (this machine's GPU): R2 tasks 4.1, an 8K real-time GI export does not starve the page.
// The frames yield between them (SceneGPU.settle); only the last frame and its encoding run in one task.
if (process.env.SCENE_E2E_GPU_8K) await check('real-time GI: the page keeps its heartbeat through an 8K picture, as the lease renewal needs', async () => {
  const gpu = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
  try {
    const page = await gpu.newPage()
    await page.goto(`${base}/.scene-gi-qa.html`)
    await page.waitForFunction(() => window.__ready === true)
    const scene = JSON.parse(readFileSync(new URL('fixtures/scene-reference/cornell.json', import.meta.url), 'utf8'))
    scene.render = { ...scene.render, engine: 'realtime_gi' }
    const out = await page.evaluate(([s]) => window.__heartbeat(s, 'lit', 8192), [scene])
    console.log(`8K real-time GI: ${out.width}×${out.height} in ${(out.ms / 1000).toFixed(1)} s, ${out.beats} beats, longest gap ${(out.longest / 1000).toFixed(1)} s, ${(out.bytes / 1e6).toFixed(1)} MB`)
    // The lease runs 30 s and is renewed every 10 s (host/jobs.ts).
    assert.ok(out.longest < 10_000, `longest gap ${out.longest} ms`)
  } finally {
    await gpu.close()
  }
})

await browser.close()
await rm(fixture, { force: true })
await rm(canvasFixture, { force: true })
await rm(hiddenFixture, { force: true })
await rm(giFixture, { force: true })
console.log(results.join('\n'))
if (results.some((r) => r.startsWith('FAIL'))) process.exit(1)
