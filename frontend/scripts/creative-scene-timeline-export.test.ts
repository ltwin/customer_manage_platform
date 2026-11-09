import test from "node:test";
import assert from "node:assert/strict";
import type { Scene, SceneObject } from "../src/creative-scene/api.ts";
import {
  renderShots,
  shotSceneKey,
  shotsOf,
} from "../src/creative-scene/host/exportShots.ts";
import type { Shooter } from "../src/creative-scene/host/render.ts";
import { evaluateAt } from "../src/creative-scene/timeline/evaluate.ts";

// Export by shot (scene-timeline-shots task 5.1): each shot drawn at its
// frame through its camera, shots of one scene key on one mounted scene.

const id = (n: number) => `sobj_${String(n).padStart(21, "0")}`;
const marker = (n: number) => `smk_${String(n).padStart(21, "0")}`;
const object = (
  n: number,
  kind: SceneObject["kind"],
  extra: Partial<SceneObject> = {},
): SceneObject => ({
  id: id(n),
  kind,
  name: `物件 ${n}`,
  parent_id: null,
  order: n,
  transform: { position: [0, 1, 5], rotation: [0, 0, 0, 1], scale: [1, 1, 1] },
  visible: true,
  locked: false,
  ...extra,
});
const camA = object(1, "camera", { camera: { focal_mm: 35 } });
const camB = object(2, "camera", {
  camera: { focal_mm: 85 },
  transform: { position: [4, 1, 5], rotation: [0, 0, 0, 1], scale: [1, 1, 1] },
});
const subject = object(3, "primitive", {
  primitive: { builtin: "box@1", params: {} },
  transform: { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] },
  animation: {
    "transform.position": {
      keys: [
        { frame: 0, value: [0, 0, 0], interp: "linear" },
        { frame: 10, value: [0, 0, -2], interp: "linear" },
        { frame: 20, value: [0, 0, -2], interp: "linear" },
      ],
    },
  },
});
const lamp = object(4, "light", {
  light: { type: "point", color: "#ffffff", intensity: 100 },
});

const scene = (objects: SceneObject[]): Scene => ({
  environment: {
    sky: {
      kind: "procedural",
      sun_elevation_deg: 35,
      sun_azimuth_deg: 135,
      turbidity: 3,
    },
    ambient: { color: "#ffffff", intensity: 0.4 },
    fog: null,
  },
  render: { mode: "clay", resolution: "2k" } as Scene["render"],
  timeline: {
    fps: 24,
    markers: [
      { id: marker(1), name: "回头", frame: 0, camera_object_id: camA.id },
      { id: marker(2), name: "", frame: 10, camera_object_id: camA.id },
      { id: marker(3), name: "", frame: 20, camera_object_id: camB.id },
    ],
  },
  objects,
});

test("the scene key: two cameras at one moment share it; a light, a volume or a carrying camera's move does not", () => {
  const s = scene([camA, camB, subject, lamp]);
  const at10 = evaluateAt(s, 10);
  const moved = (o: SceneObject, x: number) => ({
    ...o,
    transform: { ...o.transform, position: [x, 1, 5] },
  });
  assert.equal(
    shotSceneKey({
      ...at10,
      objects: [moved(camA, 9), camB, ...at10.objects.slice(2)],
    }),
    shotSceneKey(at10),
    "a camera on its own does not count, nor its lens",
  );
  assert.notEqual(shotSceneKey(evaluateAt(s, 0)), shotSceneKey(at10));
  assert.equal(
    shotSceneKey(evaluateAt(s, 20)),
    shotSceneKey(at10),
    "the same picture at frames 10 and 20",
  );
  assert.equal(
    shotSceneKey({
      ...at10,
      render: { ...at10.render, resolution: "4k" } as Scene["render"],
    }),
    shotSceneKey(at10),
    "the output size is not the scene",
  );
  const dimmer = {
    ...at10,
    objects: at10.objects.map((o) =>
      o.light ? { ...o, light: { ...o.light, intensity: 10 } } : o,
    ),
  };
  assert.notEqual(shotSceneKey(dimmer), shotSceneKey(at10), "a light counts");
  const fog = object(5, "volume", { volume: { scatter_density: 0.1 } });
  assert.notEqual(
    shotSceneKey({ ...at10, objects: [...at10.objects, fog] }),
    shotSceneKey({
      ...at10,
      objects: [...at10.objects, { ...fog, volume: { scatter_density: 0.5 } }],
    }),
    "a volume counts",
  );
  const rig = object(6, "primitive", {
    parent_id: camA.id,
    primitive: { builtin: "box@1", params: {} },
  });
  const carried = { ...at10, objects: [...at10.objects, rig] };
  assert.notEqual(
    shotSceneKey({
      ...carried,
      objects: carried.objects.map((o) => (o.id === camA.id ? moved(o, 9) : o)),
    }),
    shotSceneKey(carried),
    "a camera with an object under it moves that object",
  );
});

test("an export by shot draws each at its frame through its camera, mounting another scene once per key", async () => {
  const doc = {
    scene: scene([camA, camB, subject]),
    models: {},
    revision: "7",
  };
  const drawn: string[] = [];
  const mounted: number[][] = [];
  const position = (s: Scene) =>
    s.objects.find((o) => o.id === subject.id)!.transform.position;
  const shooter: Shooter = {
    maxTextureSize: 8192,
    world: false,
    shoot: async (f, o) => {
      drawn.push(`${o.kind}@${f.fov.toFixed(1)}`);
      return new Blob([o.kind]);
    },
    setLook: async () => {},
    setScene: async (s) => {
      mounted.push(position(s));
    },
    confirm: async () => {},
    close: () => {},
  };
  const out = await renderShots(
    {
      read: async () => doc as never,
      loadModels: async () => {},
      // No timer left behind: the switches here never run late.
      sleep: () => new Promise<void>(() => {}),
      open: async (d) => {
        mounted.push(position(d.scene));
        return shooter;
      },
    },
    "doc",
    {
      cameraObjectIDs: [],
      markerIDs: [marker(2), marker(1), marker(3)],
      passes: ["clay"],
    },
  );
  assert.ok(out.ok);
  assert.deepEqual(
    mounted,
    [
      [0, 0, -2],
      [0, 0, 0],
    ],
    "frames 10 and 20 share a scene, frame 0 is the other",
  );
  assert.equal(drawn.length, 3);
  assert.deepEqual(
    out.images.map((i) => [i.marker, i.camera]),
    [
      [marker(2), camA.id],
      [marker(1), camA.id],
      [marker(3), camB.id],
    ],
    "images in the plan's order",
  );
  const shots = shotsOf(
    out.images,
    out.images.map((_, i) => ({ id: `up${i}` }) as never),
  );
  assert.deepEqual(shots, [
    { marker_id: marker(2), images: [{ pass: "clay", upload_id: "up0" }] },
    { marker_id: marker(1), images: [{ pass: "clay", upload_id: "up1" }] },
    { marker_id: marker(3), images: [{ pass: "clay", upload_id: "up2" }] },
  ]);

  const unbound = {
    ...doc,
    scene: {
      ...doc.scene,
      timeline: {
        fps: 24,
        markers: [{ id: marker(1), name: "", frame: 0, camera_object_id: "" }],
      },
    },
  };
  const refused = await renderShots(
    {
      read: async () => unbound as never,
      loadModels: async () => {},
      open: async () => shooter,
    },
    "doc",
    { cameraObjectIDs: [], markerIDs: [marker(1)], passes: ["clay"] },
  );
  assert.deepEqual(refused, { ok: false, message: "「分镜 1」没有绑定摄像机" });
});
