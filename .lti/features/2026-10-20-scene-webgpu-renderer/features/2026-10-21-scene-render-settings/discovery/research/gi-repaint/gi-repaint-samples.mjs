// R2 AC-R2-10 source images: C5's AC-10 studio set (its lit-repaint-samples.mjs,
// unchanged but for the renderer), three cameras, clay + line + lit, drawn by
// the export pipeline (renderShots) once with each renderer, on this machine's GPU.
// Run from frontend/ with Vite on CREATIVE_SCENE_URL (default :5179):  node <this> <out dir>
import { mkdir, rm, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import path from 'node:path'

const require = createRequire(path.join(process.cwd(), 'package.json'))
const { chromium } = require('playwright')
const out = process.argv[2]
const fixture = path.join(process.cwd(), '.scene-lit-repaint-qa.html')
const base = process.env.CREATIVE_SCENE_URL || 'http://localhost:5179'

await writeFile(
  fixture,
  `<html><body><script type="module">
import * as THREE from 'three';
import {setAuthenticated} from '/src/auth/session.ts';
import {renderShots} from '/src/creative-scene/host/exportShots.ts';
import {loadModels, openShooter} from '/src/creative-scene/host/render.ts';
setAuthenticated({access_token:'local-test',expires_in:3600,token_type:'Bearer'},{id:'qa',username:'qa'});
const id = () => ({ position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] });
let order = 0;
const slot = (preset, params = {}) => ({ main: { preset, params } });
const prim = (oid, name, builtin, params, position, slots = {}, rotation = [0, 0, 0, 1]) => ({
  id: oid, kind: 'primitive', name, parent_id: null, order: order++, visible: true, locked: false,
  transform: { ...id(), position, rotation }, primitive: { builtin, params }, material_slots: slots,
});
const yaw = (deg) => new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(0, 1, 0), deg * Math.PI / 180).toArray();
const aim = (position, target) => { const o = new THREE.Object3D(); o.position.fromArray(position); o.lookAt(...target); return o.quaternion.toArray(); };
const cam = (oid, name, label, position, target, focal) => {
  const c = new THREE.PerspectiveCamera();
  c.position.fromArray(position);
  c.lookAt(...target);
  return {
    id: oid, kind: 'camera', name, parent_id: null, order: order++, visible: true, locked: false,
    transform: { ...id(), position, rotation: c.quaternion.toArray() },
    camera: { focal_mm: focal, sensor_mm: [36, 24], aspect: '3:2', near: 0.1, far: 200, label, depth_of_field: false, f_stop: 1.8, focus: 5, focus_object_id: '' },
  };
};
const light = (oid, name, position, rotation, l) => ({ id: oid, kind: 'light', name, parent_id: null, order: order++, visible: true, locked: false, transform: { ...id(), position, rotation }, light: l });
const objects = () => { order = 0; return [
  prim('sobj_ground', '地面', 'ground@1', { size: 40 }, [0, 0, 0], slot('concrete@1', { color: '#b8b2a8', stains: 0.15 })),
  prim('sobj_backdrop', '背景墙', 'box@1', { width: 8, height: 3.2, depth: 0.2 }, [0, 0, -3], slot('plaster@1', { color: '#d8d0c2' })),
  prim('sobj_arch', '拱门', 'arch@1', { width: 2.4, height: 3, depth: 0.3, opening_width: 1.3, opening_height: 2.3 }, [-2.6, 0, -2.2], slot('plaster@1', { color: '#f0ece4', mottle: 0.2 }), yaw(30)),
  prim('sobj_stairs', '台阶', 'stairs@1', { width: 1.2, step_count: 5, step_height: 0.18, step_depth: 0.3 }, [2.8, 0, -1.8], slot('wood_plank@1', { color: '#c9a678' }), yaw(-90)),
  prim('sobj_table', '桌子', 'box@1', { width: 1.4, height: 0.75, depth: 0.8 }, [0, 0, 0], slot('wood@1', { color: '#a87a4f' })),
  prim('sobj_chair', '椅子', 'box@1', { width: 0.5, height: 0.9, depth: 0.5 }, [-1.1, 0, 0.2], slot('fabric@1', { color: '#5d6b4a' }), yaw(20)),
  prim('sobj_person', '人物', 'cylinder@1', { radius_top: 0.2, radius_bottom: 0.25, height: 1.45 }, [0.95, 0, 0.5], slot('fabric@1', { color: '#d9c8a9' })),
  prim('sobj_head', '人物头部', 'sphere@1', { radius: 0.13 }, [0.95, 1.45, 0.5], slot('principled@1', { color: '#c99d82', roughness: 0.6, sheen: 0.3 })),
  prim('sobj_lamp_pole', '灯杆', 'cylinder@1', { radius_top: 0.03, radius_bottom: 0.03, height: 1.8 }, [1.9, 0, -0.6], slot('metal@1', { color: '#2a2a2a', rust: 0 })),
  prim('sobj_lamp', '灯罩', 'sphere@1', { radius: 0.22 }, [1.9, 1.75, -0.6], slot('emissive@1', { color: '#ffd9a0', strength: 3 })),
  prim('sobj_vase', '花瓶', 'cylinder@1', { radius_top: 0.08, radius_bottom: 0.12, height: 0.35 }, [-0.3, 0.75, 0], slot('marble@1', { color: '#eeeae2' })),
  light('sobj_lamp_light', '落地灯光', [1.9, 1.75, -0.6], id().rotation, { type: 'point', color: '#ffc58a', intensity: 30, cast_shadow: false, distance: 0, decay: 2 }),
  light('sobj_key', '主光', [3.5, 4.5, 3], aim([3.5, 4.5, 3], [0.3, 0.8, 0]), { type: 'spot', color: '#fff4e6', intensity: 150, cast_shadow: true, distance: 0, decay: 2, angle: Math.PI / 7, penumbra: 0.4 }),
  cam('sobj_cam_a', '正面', 'A 正面全景', [0, 1.6, 6.5], [0, 1.0, 0], 35),
  cam('sobj_cam_b', '左前', 'B 左前中景', [-4.2, 1.4, 3.8], [0.2, 1.0, 0.1], 50),
  cam('sobj_cam_c', '右上', 'C 右上俯拍', [4.2, 4.0, 4.2], [0, 0.6, 0], 28),
]};
const doc = {
  id: 'cwdoc_lit_repaint', title: '棚拍布景', type_key: 'scene', content_revision_id: 'ccrv_1', revision: '1', unit_revs: {}, trashed_at: null,
  created_at: '2026-10-20T00:00:00Z', updated_at: '2026-10-20T00:00:00Z',
  scene: { environment: { sky: { kind: 'procedural', sun_elevation_deg: 35, sun_azimuth_deg: 135, turbidity: 3 }, ambient: { color: '#ffffff', intensity: 0.4 }, fog: null },
    render: { mode: 'lit', exposure_ev: 0, engine: 'workbench', resolution: '2k' }, objects: objects() },
  models: {},
};
const b64 = async (blob) => { const b = new Uint8Array(await blob.arrayBuffer()); let s = ''; for (let i = 0; i < b.length; i += 0x8000) s += String.fromCharCode(...b.subarray(i, i + 0x8000)); return btoa(s); };
window.__export = async (engine) => {
  doc.scene.render.engine = engine;
  const deps = { read: async () => doc, loadModels: (x) => loadModels(x.scene, x.models), open: (x, look) => openShooter(x.scene, x.models, look) };
  const started = performance.now();
  const r = await renderShots(deps, doc.id, { cameraObjectIDs: ['sobj_cam_a', 'sobj_cam_b', 'sobj_cam_c'], passes: ['clay', 'line', 'lit'] });
  if (!r.ok) return r;
  const ms = Math.round(performance.now() - started);
  return { ok: true, ms, images: await Promise.all(r.images.map(async (i) => ({ camera: i.camera, pass: i.pass, data: await b64(i.blob) }))) };
};
window.__ready = true;
</script></body></html>`,
)

const browser = await chromium.launch({ headless: true, args: ['--use-angle=metal', '--ignore-gpu-blocklist', '--enable-gpu'] })
try {
  const page = await (await browser.newContext()).newPage()
  page.on('pageerror', (e) => console.error('pageerror', e.message))
  await page.goto(`${base}/.scene-lit-repaint-qa.html`)
  await page.waitForFunction(() => window.__ready, null, { timeout: 30_000 })
  await mkdir(out, { recursive: true })
  for (const engine of ['workbench', 'realtime_gi']) {
    const r = await page.evaluate((e) => window.__export(e), engine)
    if (!r.ok) throw new Error(r.message)
    console.log(`${engine}: 9 images in ${r.ms} ms`)
    for (const i of r.images) await writeFile(path.join(out, `${engine}-${i.camera.replace('sobj_cam_', '')}-${i.pass}.png`), Buffer.from(i.data, 'base64'))
  }
} finally {
  await browser.close()
  await rm(fixture, { force: true })
}
