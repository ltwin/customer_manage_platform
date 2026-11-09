import test from "node:test";
import assert from "node:assert/strict";
import type {
  CommandReceipt,
  Marker,
  SceneDocument,
  SceneObject,
} from "../src/creative-scene/api.ts";
import {
  applyOps,
  type SceneState,
} from "../src/creative-scene/state/apply.ts";
import {
  enqueue,
  project,
  touchedUnits,
  type Intent,
} from "../src/creative-scene/state/intents.ts";
import {
  SceneSession,
  type Clock,
  type Transport,
} from "../src/creative-scene/state/sync.ts";
import { UndoStack } from "../src/creative-scene/state/undo.ts";

// The state layer with a timeline (scene-timeline-shots task 3.2): keys go
// to the units of the fields they key, folding keeps the server's order, and
// undo brings back each unit's channels.

const id = (n: number) => `sobj_${String(n).padStart(21, "0")}`;
const marker = (n: number, frame: number, cam = ""): Marker => ({
  id: `smk_${String(n).padStart(21, "0")}`,
  name: "",
  frame,
  camera_object_id: cam,
});
const identity = {
  position: [0, 0, 0],
  rotation: [0, 0, 0, 1],
  scale: [1, 1, 1],
};
const cam = id(1);
const camera = (animation?: SceneObject["animation"]): SceneObject => ({
  id: cam,
  kind: "camera",
  name: "A 机",
  parent_id: null,
  order: 1,
  transform: identity,
  visible: true,
  locked: false,
  camera: { focal_mm: 35 },
  ...(animation ? { animation } : {}),
});
const keys = (path: string, ...pairs: [number, unknown][]) => ({
  [path]: {
    keys: pairs.map(([frame, value]) => ({
      frame,
      value,
      interp: typeof value === "boolean" ? "constant" : "linear",
    })),
  },
});
const shots = [marker(1, 0, cam), marker(2, 10, cam)];
const keyed = {
  ...keys("transform.position", [0, [0, 0, 0]], [10, [0, 0, -2]]),
  ...keys("camera.focal_mm", [0, 35], [10, 85]),
} as NonNullable<SceneObject["animation"]>;

function sceneState(objects: SceneObject[], markers: Marker[]): SceneState {
  return {
    title: "场景",
    scene: {
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
      render: { mode: "clay" },
      timeline: { fps: 24, markers },
      objects,
    },
  };
}

const update = (animation: object) => ({
  op: "update_objects" as const,
  updates: [{ object_id: cam, animation }],
});
const channel = (s: SceneState, path: string) =>
  s.scene.objects[0].animation?.[path]?.keys.map((k) => [k.frame, k.value]);

let seq = 0;
const intent = (ops: Intent["ops"], merge?: string): Intent => ({
  id: `i${++seq}`,
  createdAt: "2026-11-08T00:00:00Z",
  ops,
  expected: {},
  state: "pending",
  label: "关键帧",
  ...(merge ? { merge } : {}),
});

test("keys count in the units of the fields they key", () => {
  const s = sceneState([camera(keyed)], shots);
  const units = touchedUnits(
    update({
      clear: ["camera.focal_mm"],
      set: [{ path: "visible", frame: 0, value: false }],
    }),
    s,
  );
  assert.deepEqual(units, [`object:${cam}`, `object:${cam}:content`]);
  assert.deepEqual(
    touchedUnits(
      update({ delete: [{ path: "camera.focal_mm", frame: 10 }] }),
      s,
    ),
    [`object:${cam}:content`],
  );
});

test("a set then a clear does not fold, and the channel ends cleared", () => {
  const base = sceneState([camera(keyed)], shots);
  const set = intent(
    [update({ set: [{ path: "camera.focal_mm", frame: 10, value: 50 }] })],
    "focal",
  );
  const clear = intent([update({ clear: ["camera.focal_mm"] })], "focal");
  let { queue } = enqueue([], set);
  ({ queue } = enqueue(queue, clear));
  assert.equal(queue.length, 2, "folding would run the clear first");
  const saved = applyOps(
    base,
    queue.flatMap((i) => i.ops),
  );
  assert.equal(channel(saved, "camera.focal_mm"), undefined);
  assert.deepEqual(channel(project(base, queue), "camera.focal_mm"), undefined);

  // Sets alone fold; the later write of a frame wins.
  const again = intent(
    [
      update({
        set: [
          { path: "camera.focal_mm", frame: 10, value: 70 },
          { path: "camera.focal_mm", frame: 0, value: 24 },
        ],
      }),
    ],
    "focal",
  );
  ({ queue } = enqueue([set], again));
  assert.equal(queue.length, 1);
  assert.deepEqual(channel(project(base, queue), "camera.focal_mm"), [
    [0, 24],
    [10, 70],
  ]);
});

test("applying keys completes them and drops emptied channels", () => {
  const base = sceneState([camera()], shots);
  const s = applyOps(base, [
    update({
      set: [
        { path: "transform.rotation", frame: 0, value: [0, 0, 0, 2] },
        { path: "visible", frame: 0, value: true },
      ],
    }),
  ]);
  const anim = s.scene.objects[0].animation!;
  assert.deepEqual(anim["transform.rotation"].keys[0], {
    frame: 0,
    value: [0, 0, 0, 1],
    interp: "linear",
  });
  assert.equal(anim.visible.keys[0].interp, "constant");
  const emptied = applyOps(s, [
    update({
      delete: [
        { path: "transform.rotation", frame: 0 },
        { path: "visible", frame: 0 },
      ],
    }),
  ]);
  assert.equal(emptied.scene.objects[0].animation, undefined);
});

// --- undo through a session ---------------------------------------------

const clock: Clock = {
  now: () => 0,
  setTimeout: () => 0,
  clearTimeout: () => {},
  online: () => true,
  hidden: () => false,
};

type Sent = {
  payload: { expected: Record<string, unknown>; operations: unknown[] };
  resolve(r: CommandReceipt): void;
};

async function started(objects: SceneObject[], markers: Marker[]) {
  const s = sceneState(objects, markers);
  const unit_revs: Record<string, string> = {
    meta: "1",
    environment: "1",
    render: "1",
    timeline: "3",
    objects: "3",
  };
  for (const o of objects) {
    unit_revs[`object:${o.id}`] = "3";
    unit_revs[`object:${o.id}:content`] = "3";
  }
  const doc: SceneDocument = {
    id: "cwdoc_1",
    type_key: "scene",
    title: s.title,
    revision: "3",
    content_revision_id: "ccrv_1",
    unit_revs,
    trashed_at: null,
    created_at: "",
    updated_at: "",
    scene: s.scene,
    models: {},
  };
  const sent: Sent[] = [];
  const transport: Transport = {
    read: () => Promise.resolve(structuredClone(doc)),
    command: (_doc, _intent, payload) =>
      new Promise<CommandReceipt>((resolve) => {
        sent.push({ payload: payload as Sent["payload"], resolve });
      }),
  };
  const session = new SceneSession("cwdoc_1", transport, clock);
  await session.start();
  const undo = new UndoStack(session);
  const confirm = async (revision: number, changed: string[]) => {
    sent.at(-1)!.resolve({
      revision: String(revision),
      changed_units: changed,
      annotated_units: [],
      removed_units: [],
      reset_required: false,
    });
    await new Promise((r) => setImmediate(r));
  };
  const view = () => session.state.view!;
  return { session, undo, sent, confirm, view };
}

test("undoing a removed shot brings its keys back", async () => {
  const { session, undo, sent, confirm, view } = await started(
    [camera(keyed)],
    shots,
  );
  session.edit(
    [
      { op: "set_timeline", timeline: { markers: [shots[0]] } },
      update({
        delete: [
          { path: "transform.position", frame: 10 },
          { path: "camera.focal_mm", frame: 10 },
        ],
      }),
    ],
    "删除分镜",
  );
  const units = ["timeline", `object:${cam}`, `object:${cam}:content`];
  assert.deepEqual(
    Object.keys(sent.at(-1)!.payload.expected).sort(),
    [...units].sort(),
  );
  await confirm(4, units);
  assert.equal(view().scene.timeline.markers.length, 1);
  assert.deepEqual(channel(view(), "camera.focal_mm"), [[0, 35]]);

  assert.equal(undo.undo(), "sent");
  const restore = sent.at(-1)!.payload.operations[0] as { units: string[] };
  assert.deepEqual([...restore.units].sort(), [...units].sort());
  assert.deepEqual(
    view().scene.timeline.markers,
    shots,
    "the shot shows at once",
  );
  assert.deepEqual(channel(view(), "camera.focal_mm"), [
    [0, 35],
    [10, 85],
  ]);
  assert.deepEqual(channel(view(), "transform.position"), [
    [0, [0, 0, 0]],
    [10, [0, 0, -2]],
  ]);
});

test("each unit's channels come back on their own", async () => {
  const { session, undo, confirm, view } = await started(
    [camera(keyed)],
    shots,
  );
  session.edit(
    [
      update({
        set: [{ path: "transform.position", frame: 10, value: [1, 0, 0] }],
      }),
    ],
    "关键帧",
  );
  await confirm(4, [`object:${cam}`]);
  session.edit(
    [update({ set: [{ path: "camera.focal_mm", frame: 10, value: 50 }] })],
    "关键帧",
  );
  await confirm(5, [`object:${cam}:content`]);

  assert.equal(undo.undo(), "sent");
  assert.deepEqual(channel(view(), "camera.focal_mm"), [
    [0, 35],
    [10, 85],
  ]);
  assert.deepEqual(
    channel(view(), "transform.position")?.[1],
    [10, [1, 0, 0]],
    "undoing the lens keeps the move",
  );
  await confirm(6, [`object:${cam}:content`]);
  assert.equal(undo.undo(), "sent");
  assert.deepEqual(channel(view(), "transform.position")?.[1], [
    10,
    [0, 0, -2],
  ]);
  assert.deepEqual(
    channel(view(), "camera.focal_mm")?.[1],
    [10, 85],
    "undoing the move keeps the lens",
  );
});
