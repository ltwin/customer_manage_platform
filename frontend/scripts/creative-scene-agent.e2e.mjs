// C3 renderer side in a real browser, against mocked endpoints: the
// assistant's screenshots (render_view) taken offscreen.
// Run with the frontend Vite server on CREATIVE_SCENE_URL (default :5179):
//   node scripts/creative-scene-agent.e2e.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { rm, writeFile } from 'node:fs/promises'
import { chromium } from 'playwright'

const DOC = 'cwdoc_scene_agent'
const MODEL = 'ccrv_pillar'
const fixture = new URL('../.scene-agent-qa.html', import.meta.url)
await writeFile(
  fixture,
  `<html><body><script type="module">
import {setAuthenticated} from '/src/auth/session.ts';
import {renderView} from '/src/creative-scene/host/render.ts';
setAuthenticated({access_token:'local-test',expires_in:3600,token_type:'Bearer'},{id:'qa-account',username:'qa'});
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

const base = process.env.CREATIVE_SCENE_URL || 'http://localhost:5179'
const browser = await chromium.launch({ headless: true, args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader'] })
const testdata = new URL('../../backend/internal/creativeglb/testdata/', import.meta.url)
const pillarGLB = readFileSync(new URL('exported-template.glb', testdata))
const pillarManifest = JSON.parse(readFileSync(new URL('cases.json', testdata), 'utf8')).find((c) => c.name === 'exported-template').manifest
const identity = () => ({ position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] })
// A saturated background, so "the model is there" reads off one pixel.
const background = [51, 85, 255]

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

async function open() {
  const context = await browser.newContext({ viewport: { width: 1280, height: 820 } })
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('console', (m) => {
    if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(m.text())
    // Chrome drops the oldest context past its cap (16) and says so.
    if (/Too many active WebGL contexts/.test(m.text())) errors.push(m.text())
  })
  const calls = { reads: 0, media: 0 }
  await page.route('**/api/v1/creative/**', async (r) => {
    const request = r.request()
    const path = new URL(request.url()).pathname.replace('/api/v1/creative', '')
    if (path === `/documents/${DOC}` && request.method() === 'GET') {
      calls.reads++
      return r.fulfill({ status: 200, json: sceneDoc() })
    }
    if (path === '/media-access-tickets' && request.method() === 'POST')
      return r.fulfill({ status: 200, json: { url: `${base}/__media/${MODEL}`, expires_at: '2100-01-01T00:00:00Z', mime: 'model/gltf-binary', byte_size: pillarGLB.byteLength } })
    return r.fulfill({ status: 404, json: { error: { code: 'not_found', message: `unmocked ${path}` } } })
  })
  await page.route('**/__media/**', async (r) => {
    calls.media++
    return r.fulfill({ status: 200, contentType: 'model/gltf-binary', body: pillarGLB })
  })
  await page.goto(`${base}/.scene-agent-qa.html`)
  await page.waitForFunction(() => window.__ready, null, { timeout: 30_000 })
  return { page, context, errors, calls, render: (request) => page.evaluate((q) => window.__render(q), request) }
}

const results = []
async function check(name, fn) {
  const started = Date.now()
  try {
    await fn()
    results.push({ name, ok: true })
    console.log(`ok   ${name} (${Date.now() - started} ms)`)
  } catch (error) {
    results.push({ name, ok: false })
    console.log(`FAIL ${name}\n     ${String(error?.stack ?? error).split('\n').slice(0, 6).join('\n     ')}`)
  }
}

const isBackground = (px) => px.every((v, i) => Math.abs(v - background[i]) < 24)

try {
  await check('a screenshot is a JPEG with a 1024 long side, of the revision it read, with the model drawn in its one frame', async () => {
    const t = await open()
    // Level with the pillar's middle, so it stands in the centre against the background.
    const shot = await t.render({ viewpoint: { position: [0, 1.5, 6], target: [0, 1.5, 0], focal_mm: 50 } })
    assert.ok(shot.ok, JSON.stringify(shot))
    assert.equal(shot.type, 'image/jpeg')
    assert.deepEqual([shot.width, shot.height], [1024, 768])
    assert.equal(shot.revision, '7', 'scene_revision is the revision it drew')
    assert.ok(isBackground(shot.corner), `the corner is the scene's background: ${shot.corner}`)
    assert.ok(!isBackground(shot.center), `the pillar is drawn, not its loading outline: ${shot.center}`)
    assert.equal(shot.canvases, 0, 'the canvas never joins the page')
    assert.equal(t.calls.media, 1)
    assert.deepEqual(t.errors, [])
    await t.context.close()
  })

  await check('a scene camera keeps its picture shape; presets are 4:3; a missing camera is reported', async () => {
    const t = await open()
    const camera = await t.render({ camera_object_id: 'sobj_cam' })
    assert.deepEqual([camera.width, camera.height], [1024, 683])
    assert.ok(!isBackground(camera.center), `the camera looks at the pillar: ${camera.center}`)
    for (const preset of ['overview', 'top', 'front', 'side']) {
      const shot = await t.render({ preset })
      assert.deepEqual([shot.width, shot.height], [1024, 768], preset)
      assert.ok(!isBackground(shot.center), `${preset} frames the pillar: ${shot.center}`)
    }
    assert.equal(t.calls.media, 1, 'the GLB is downloaded once for every shot')
    assert.deepEqual(await t.render({ camera_object_id: 'sobj_gone' }), { ok: false, message: '摄像机 sobj_gone 已不在场景里' })
    assert.deepEqual(t.errors, [])
    await t.context.close()
  })

  await check('each screenshot gives its WebGL context back: twenty in a row all draw', async () => {
    const t = await open()
    for (let i = 0; i < 20; i++) {
      const shot = await t.render({ preset: 'front' })
      assert.ok(shot.ok && !isBackground(shot.center), `shot ${i + 1}: ${JSON.stringify(shot)}`)
    }
    assert.deepEqual(t.errors, [], 'no context is left for the browser to drop')
    await t.context.close()
  })
} finally {
  await browser.close()
  await rm(fixture, { force: true })
}

const failed = results.filter((r) => !r.ok)
console.log(`\n${results.length - failed.length}/${results.length} passed`)
if (failed.length) process.exit(1)
