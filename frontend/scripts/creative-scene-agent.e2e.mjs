// C3 renderer side in a real browser, against mocked endpoints: the
// assistant's screenshots (render_view) taken offscreen, and the workspace
// page's claim loop running jobs with the executor loaded on demand.
// Run with the frontend Vite server on CREATIVE_SCENE_URL (default :5179):
//   node scripts/creative-scene-agent.e2e.mjs
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
  return {
    ok: true, type: shot.image.type, revision: shot.revision, width: bitmap.width, height: bitmap.height,
    center: px(bitmap.width >> 1, bitmap.height >> 1), corner: px(4, 4), canvases: document.querySelectorAll('canvas').length,
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

const base = process.env.CREATIVE_SCENE_URL || 'http://localhost:5179'
const browser = await chromium.launch({ headless: true, args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'] })
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
const testdata = new URL('../../backend/internal/creativeglb/testdata/', import.meta.url)
const pillarGLB = readFileSync(new URL('exported-template.glb', testdata))
const pillarManifest = JSON.parse(readFileSync(new URL('cases.json', testdata), 'utf8')).find((c) => c.name === 'exported-template').manifest
const identity = () => ({ position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] })
// A saturated background, so "the model is there" reads off one pixel.
const background = [51, 85, 255]
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
          camera: { focal_mm: 35, sensor_mm: [36, 24], aspect: '3:2', near_m: 0.1, far_m: 200, label: '' },
        },
      ],
    },
    models: { [MODEL]: { content_id: 'ccnt_pillar', usable: true, manifest: pillarManifest, generator: null, name_hint: '柱' } },
  }
}

/** The job queue, staged uploads and one scene, shared by every tab of a browser context. */
class Server {
  constructor() {
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
    const job = { id, document_id: DOC, kind: 'render_view', attempt: 0, claim_token: `tok_${id}`, lease_until: '2100-01-01T00:00:00Z', deadline: '2100-01-01T00:00:00Z', request: { ...request, mode: 'clay' } }
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
    if (path === `/documents/${DOC}` && request.method() === 'GET') return json(200, sceneDoc())
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

const results = []
async function check(name, fn) {
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
} finally {
  await browser.close()
  await rm(renderFixture, { force: true })
  await rm(hostFixture, { force: true })
}

const failed = results.filter((r) => !r.ok)
console.log(`\n${results.length - failed.length}/${results.length} passed`)
if (failed.length) process.exit(1)
