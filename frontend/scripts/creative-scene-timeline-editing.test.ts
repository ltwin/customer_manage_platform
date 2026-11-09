import test from "node:test";
import assert from "node:assert/strict";
import { Vector3 } from "three";
import type {
  CommandReceipt,
  Marker,
  Scene,
  SceneDocument,
  SceneObject,
} from "../src/creative-scene/api.ts";
import { localPose } from "../src/creative-scene/editing.ts";
import { clearParent } from "../src/creative-scene/grouping.ts";
import { applyOps, type WireOp } from "../src/creative-scene/state/apply.ts";
import { atFrame } from "../src/creative-scene/state/sceneStore.ts";
import {
  SceneSession,
  type Clock,
  type Transport,
} from "../src/creative-scene/state/sync.ts";
import { worldOf } from "../src/creative-scene/state/world.ts";
import {
  evaluateAt,
  shownAt,
} from "../src/creative-scene/timeline/evaluate.ts";
import {
  forgetUnits,
  insertKeys,
  pruneEdits,
  routeEdit,
  type TimelineEdit,
} from "../src/creative-scene/timeline/keys.ts";

// The editor's edit routing and baselines (scene-timeline-shots task 4.1,
// design「编辑分流」), and the entries that compute from the current
// picture.

const id = (n: number) => `sobj_${String(n).padStart(21, "0")}`;
const shot = (n: number, frame: number): Marker => ({
  id: `smk_${String(n).padStart(21, "0")}`,
  name: "",
  frame,
  camera_object_id: "",
});
const subject = id(2);
const object = (
  n: number,
  kind: SceneObject["kind"],
  extra: Partial<SceneObject> = {},
): SceneObject => ({
  id: id(n),
  kind,
  name: "",
  parent_id: null,
  order: n,
  transform: { position: [0, 0, 0], rotation: [0, 0, 0, 1], scale: [1, 1, 1] },
  visible: true,
  locked: false,
  ...extra,
});
const at = (x: number, y = 0, z = 0) => [x, y, z];
const box = (n: number, extra: Partial<SceneObject> = {}) =>
  object(n, "primitive", {
    primitive: { builtin: "box@1", params: {} },
    ...extra,
  });

function sceneOf(objects: SceneObject[], markers: Marker[]): Scene {
  return {
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
  };
}
const shots = [shot(1, 0), shot(2, 10), shot(3, 20)];
const apply = (s: Scene, ops: WireOp[]) =>
  applyOps({ title: "场景", scene: s }, ops).scene;
const move = (x: number, z = 0): WireOp => ({
  op: "update_objects",
  updates: [
    {
      object_id: subject,
      transform: {
        position: at(x, 0, z),
        rotation: [0, 0, 0, 1],
        scale: [1, 1, 1],
      },
    },
  ],
});
const position = (s: Scene, frame: number) =>
  evaluateAt(s, frame).objects.find((o) => o.id === subject)!.transform
    .position;
const edit = (extra: Partial<TimelineEdit> = {}): TimelineEdit => ({
  frame: 10,
  autoKey: false,
  unkeyed: {},
  baseline: {},
  ...extra,
});

test("AC-2: moved then I in shot 2, or moved with auto keying, changes shot 2 only", () => {
  const s = sceneOf([box(2)], shots);
  // Off: the base value moves in every shot, the value before is kept.
  const off = routeEdit(s, [move(0, -2)], edit());
  assert.deepEqual(off.baseline, {
    [subject]: { "transform.position": at(0) },
  });
  const moved = apply(s, off.ops);
  assert.deepEqual(position(moved, 0), at(0, 0, -2), "all shots move first");
  const i = insertKeys(moved, [subject], { ...edit(), baseline: off.baseline });
  assert.equal(i.adopted, false);
  assert.deepEqual(i.baseline, {}, "I uses the baseline up");
  const keyed = apply(moved, i.ops);
  assert.deepEqual(
    [0, 10, 20].map((f) => position(keyed, f)),
    [at(0), at(0, 0, -2), at(0)],
  );
  assert.deepEqual(
    keyed.objects[0].transform.position,
    at(0),
    "the default state goes back",
  );

  // On: the same keys at once.
  const on = routeEdit(s, [move(0, -2)], edit({ autoKey: true }));
  assert.deepEqual(on.baseline, {});
  const direct = apply(s, on.ops);
  assert.deepEqual(
    [0, 10, 20].map((f) => position(direct, f)),
    [at(0), at(0, 0, -2), at(0)],
  );
  assert.deepEqual(direct.objects[0].animation, keyed.objects[0].animation);
});

test("I without a baseline keys the current value in every shot and says so", () => {
  const s = sceneOf(
    [
      box(2, {
        transform: {
          position: at(3),
          rotation: [0, 0, 0, 1],
          scale: [1, 1, 1],
        },
      }),
    ],
    shots,
  );
  const i = insertKeys(s, [subject], edit());
  assert.equal(i.adopted, true);
  const keyed = apply(s, i.ops);
  assert.deepEqual(
    Object.keys(keyed.objects[0].animation ?? {}).sort(),
    ["transform.position", "transform.rotation", "transform.scale"],
    "an object changed nowhere keys its transform",
  );
  assert.deepEqual(
    [0, 10, 20].map((f) => position(keyed, f)),
    [at(3), at(3), at(3)],
  );
});

test("a keyed property waits unkeyed with auto keying off and takes a key on", () => {
  let s = sceneOf([box(2)], shots);
  s = apply(s, insertKeys(s, [subject], edit()).ops);
  const off = routeEdit(s, [move(5)], edit());
  assert.deepEqual(off.ops, [], "nothing is sent");
  assert.deepEqual(off.unkeyed, { [subject]: { "transform.position": at(5) } });
  assert.deepEqual(
    shownAt(s, 10, off.unkeyed).objects[0].transform.position,
    at(5),
    "the editor shows it",
  );
  assert.deepEqual(
    atFrame(20),
    { frame: 20, unkeyed: {}, baseline: {} },
    "another frame drops it",
  );
  const i = insertKeys(s, [subject], { ...edit(), unkeyed: off.unkeyed });
  assert.deepEqual(i.unkeyed, {});
  assert.deepEqual(position(apply(s, i.ops), 10), at(5));

  const on = routeEdit(
    s,
    [move(6)],
    edit({ autoKey: true, unkeyed: off.unkeyed }),
  );
  assert.deepEqual(on.unkeyed, {});
  assert.deepEqual(on.ops, [
    {
      op: "update_objects",
      updates: [
        {
          object_id: subject,
          animation: {
            set: [{ path: "transform.position", frame: 10, value: at(6) }],
          },
        },
      ],
    },
  ]);
});

test("a move of a keyed object writes no part of its stored transform", () => {
  let s = sceneOf([box(2)], shots);
  // Rotation keyed, position not: the gizmo hands back the shown rotation.
  s = apply(s, [
    {
      op: "update_objects",
      updates: [
        {
          object_id: subject,
          animation: {
            set: shots.map((m) => ({
              path: "transform.rotation",
              frame: m.frame,
              value:
                m.frame === 10
                  ? [0, 0.7071067811865476, 0, 0.7071067811865476]
                  : [0, 0, 0, 1],
            })),
          },
        },
      ],
    },
  ]);
  const shown = evaluateAt(s, 10).objects[0].transform;
  const r = routeEdit(
    s,
    [
      {
        op: "update_objects",
        updates: [
          { object_id: subject, transform: { ...shown, position: at(4) } },
        ],
      },
    ],
    edit(),
  );
  const moved = apply(s, r.ops);
  assert.deepEqual(
    moved.objects[0].transform.rotation,
    [0, 0, 0, 1],
    "the stored rotation stays",
  );
  assert.deepEqual(moved.objects[0].transform.position, at(4));
});

test("a baseline goes when its property is keyed elsewhere, undone, or its object changed elsewhere", () => {
  const s = sceneOf([box(2), box(3)], shots);
  const off = routeEdit(s, [move(7)], edit());
  const moved = apply(s, off.ops);
  assert.equal(
    pruneEdits(moved, {}, off.baseline).baseline,
    off.baseline,
    "nothing to drop",
  );

  // Keyed for another reason (the assistant, another tab).
  const keyed = apply(moved, insertKeys(moved, [subject], edit()).ops);
  assert.deepEqual(pruneEdits(keyed, {}, off.baseline).baseline, {});
  // An undo takes the base value back to it.
  assert.deepEqual(pruneEdits(s, {}, off.baseline).baseline, {});
  // Another tab or the assistant changed the object's placement unit; its content unit leaves it.
  assert.equal(
    forgetUnits(off.baseline, [`object:${subject}:content`]),
    off.baseline,
  );
  assert.deepEqual(forgetUnits(off.baseline, [`object:${subject}`]), {});
  // Unkeyed values of a channel gone go too.
  assert.deepEqual(
    pruneEdits(s, { [subject]: { "transform.position": at(1) } }, {}).unkeyed,
    {},
  );
});

// --- the session tells the editor which units a read moved -----------------

test("a read reports the units someone else moved", async () => {
  const s = sceneOf([box(2)], shots);
  const revs = (r: string) => ({
    meta: "1",
    environment: "1",
    render: "1",
    timeline: "3",
    objects: "3",
    [`object:${subject}`]: r,
    [`object:${subject}:content`]: "3",
  });
  let doc: SceneDocument = {
    id: "cwdoc_1",
    type_key: "scene",
    title: "场景",
    revision: "3",
    content_revision_id: "ccrv_1",
    unit_revs: revs("3"),
    trashed_at: null,
    created_at: "",
    updated_at: "",
    scene: s,
    models: {},
  };
  const transport: Transport = {
    read: () => Promise.resolve(structuredClone(doc)),
    command: () => new Promise<CommandReceipt>(() => {}),
  };
  // Reads go out at most every 500 ms.
  let now = 0;
  const clock: Clock = {
    now: () => now,
    setTimeout: () => 0,
    clearTimeout: () => {},
    online: () => true,
    hidden: () => false,
  };
  const session = new SceneSession("cwdoc_1", transport, clock);
  await session.start();
  const moved: string[][] = [];
  session.observe({ moved: (units) => moved.push(units) });
  doc = { ...doc, revision: "4", unit_revs: revs("4") };
  now = 1000;
  session.invalidate();
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(moved, [[`object:${subject}`]]);
});

// --- entries computed from the current picture ----------------------------

const rig = () => {
  const parent = object(1, "group", {
    animation: {
      "transform.position": {
        keys: [
          { frame: 0, value: at(0), interp: "linear" },
          { frame: 10, value: at(10), interp: "linear" },
        ],
      },
    },
  });
  const child = box(2, {
    parent_id: id(1),
    transform: { position: at(1), rotation: [0, 0, 0, 1], scale: [1, 1, 1] },
  });
  return sceneOf([parent, child], [shot(1, 0), shot(2, 10)]);
};
const worldPosition = (objects: readonly SceneObject[], objectID: string) =>
  new Vector3().setFromMatrixPosition(worldOf(objects)(objectID)).toArray();

test("cameraToView under an animated parent lands where the view is in this shot", () => {
  const s = rig();
  const shown = evaluateAt(s, 10).objects;
  const pose = { position: [12, 1, 0], rotation: [0, 0, 0, 1] };
  const local = localPose(shown, id(1), pose, [1, 1, 1]);
  assert.deepEqual(
    local.position.map((x) => Math.round(x * 1e6) / 1e6),
    [2, 1, 0],
  );
});

test("clearing the parent keeps the world position this shot shows", () => {
  const s = rig();
  const shown = evaluateAt(s, 10).objects;
  const r = clearParent(shown, { selected: [subject], active: subject });
  assert.ok(r && !("error" in r));
  const after = evaluateAt(apply(s, [r.op]), 10).objects;
  assert.deepEqual(
    worldPosition(after, subject),
    worldPosition(shown, subject),
  );
  assert.deepEqual(worldPosition(after, subject), at(11));
});
