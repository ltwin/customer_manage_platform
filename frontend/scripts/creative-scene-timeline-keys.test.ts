import test from "node:test";
import assert from "node:assert/strict";
import type {
  Marker,
  Scene,
  SceneObject,
  SceneOperation,
} from "../src/creative-scene/api.ts";
import {
  applyOps,
  type SceneState,
} from "../src/creative-scene/state/apply.ts";
import { evaluateAt } from "../src/creative-scene/timeline/evaluate.ts";
import {
  addShot,
  clearChannel,
  duplicateShot,
  firstKey,
  moveShot,
  nextShotFrame,
  onlyInShot,
  removeShot,
} from "../src/creative-scene/timeline/keys.ts";

// Epic design S1–S6 as the editor writes them (scene-timeline-shots task
// 3.3): applied as the server would, every batch leaves each channel keyed
// at every shot (P8).

const id = (n: number) => `sobj_${String(n).padStart(21, "0")}`;
const shot = (n: number, frame: number): Marker => ({
  id: `smk_${String(n).padStart(21, "0")}`,
  name: "",
  frame,
  camera_object_id: cam,
});
const cam = id(1);
const subject = id(2);
const card = id(3);
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

function scene(markers: Marker[]): SceneState {
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
      objects: [
        object(1, "camera", { camera: { focal_mm: 35 } }),
        object(2, "primitive", {
          primitive: { builtin: "box@1", params: {} },
        }),
        object(3, "primitive", {
          primitive: { builtin: "box@1", params: {} },
        }),
      ],
    },
  };
}

/** P8, the TS copy of checkShotKeys: the channels missing a key at a shot. */
function gaps(s: Scene): string[] {
  const out: string[] = [];
  for (const o of s.objects)
    for (const [path, ch] of Object.entries(o.animation ?? {}))
      for (const m of s.timeline.markers)
        if (!ch.keys.some((k) => k.frame === m.frame))
          out.push(`${o.id} ${path} @${m.frame}`);
  return out;
}

/** Applies ops and checks P8, as the server's command does. */
function write(s: SceneState, ops: SceneOperation[]): SceneState {
  assert.ok(ops.length, "the rule wrote nothing");
  const next = applyOps(s, ops);
  assert.deepEqual(gaps(next.scene), [], "P8");
  return next;
}

const shown = (s: SceneState, frame: number, objectID: string) =>
  evaluateAt(s.scene, frame).objects.find((o) => o.id === objectID)!;
const base = (s: SceneState, objectID: string) =>
  s.scene.objects.find((o) => o.id === objectID)!;

const one = shot(1, 0);
const two = shot(2, 10);
const three = shot(3, 20);

test("S1: the first key changes its shot only and keeps the default state", () => {
  let s = scene([one, two, three]);
  s = write(
    s,
    firstKey(s.scene, subject, two.frame, [
      { path: "transform.position", value: at(0, 0, -2), before: at(0) },
    ]),
  );
  assert.deepEqual(shown(s, one.frame, subject).transform.position, at(0));
  assert.deepEqual(
    shown(s, two.frame, subject).transform.position,
    at(0, 0, -2),
  );
  assert.deepEqual(shown(s, three.frame, subject).transform.position, at(0));

  // Off a shot frame, the shots are keyed too; with a baseline the base
  // value goes back to it.
  const moved = applyOps(s, [
    {
      op: "update_objects",
      updates: [{ object_id: cam, camera: { focal_mm: 85 } }],
    },
  ]);
  const keyed = write(
    moved,
    firstKey(moved.scene, cam, 15, [
      { path: "camera.focal_mm", value: 85, before: 35 },
    ]),
  );
  assert.equal(
    base(keyed, cam).camera?.focal_mm,
    35,
    "the base value goes back",
  );
  assert.deepEqual(
    base(keyed, cam).animation?.["camera.focal_mm"].keys.map((k) => [
      k.frame,
      k.value,
    ]),
    [
      [0, 35],
      [10, 35],
      [15, 85],
      [20, 35],
    ],
  );

  // A channel already there just gets the key.
  const again = firstKey(keyed.scene, subject, three.frame, [
    { path: "transform.position", value: at(5), before: at(9) },
  ]);
  assert.deepEqual(again, [
    {
      op: "update_objects",
      updates: [
        {
          object_id: subject,
          animation: {
            set: [{ path: "transform.position", frame: 20, value: at(5) }],
          },
        },
      ],
    },
  ]);
});

test("S2a and S2b: a new shot takes the default state, a copy its shot", () => {
  let s = scene([one, two]);
  s = write(
    s,
    firstKey(s.scene, subject, two.frame, [
      { path: "transform.position", value: at(0, 0, -2), before: at(0) },
    ]),
  );
  assert.equal(nextShotFrame(s.scene.timeline.markers), 20);
  const fresh = write(s, addShot(s.scene, three));
  assert.deepEqual(
    shown(fresh, three.frame, subject).transform.position,
    at(0),
  );
  const copy = write(s, duplicateShot(s.scene, two.id, three));
  assert.deepEqual(
    copy.scene.timeline.markers.map((m) => m.frame),
    [0, 10, 20],
  );
  assert.deepEqual(
    shown(copy, three.frame, subject).transform.position,
    at(0, 0, -2),
  );
});

test("S3 and S4: a moved shot keeps its picture, a removed one leaves no gap", () => {
  let s = scene([one, two, three]);
  s = write(
    s,
    firstKey(s.scene, subject, two.frame, [
      { path: "transform.position", value: at(0, 0, -2), before: at(0) },
    ]),
  );
  // Frame 15 had a key of its own: the moved shot's replaces it.
  s = applyOps(s, [
    {
      op: "update_objects",
      updates: [
        {
          object_id: subject,
          animation: {
            set: [{ path: "transform.position", frame: 15, value: at(7) }],
          },
        },
      ],
    },
  ]);
  const moved = write(s, moveShot(s.scene, two.id, 15));
  assert.deepEqual(shown(moved, 15, subject).transform.position, at(0, 0, -2));
  assert.deepEqual(
    base(moved, subject).animation?.["transform.position"].keys.map(
      (k) => k.frame,
    ),
    [0, 15, 20],
  );
  const removed = write(moved, removeShot(moved.scene, two.id));
  assert.deepEqual(
    base(removed, subject).animation?.["transform.position"].keys.map(
      (k) => k.frame,
    ),
    [0, 20],
  );
  assert.equal(removed.scene.timeline.markers.length, 2);
});

test("S5: clearing a channel keeps what the frame shows", () => {
  let s = scene([one, two]);
  s = write(
    s,
    firstKey(s.scene, subject, two.frame, [
      { path: "transform.position", value: at(4), before: at(0) },
    ]),
  );
  const cleared = write(
    s,
    clearChannel(s.scene, subject, "transform.position", 5),
  );
  assert.equal(base(cleared, subject).animation, undefined);
  assert.deepEqual(base(cleared, subject).transform.position, at(2));
});

test("S6: an object only in a shot stays out of new shots and goes with a copy", () => {
  let s = scene([one, two]);
  s = write(s, onlyInShot(s.scene, card, two.id));
  assert.equal(base(s, card).visible, false);
  assert.equal(shown(s, one.frame, card).visible, false);
  assert.equal(shown(s, two.frame, card).visible, true);
  const fresh = write(s, addShot(s.scene, three));
  assert.equal(shown(fresh, three.frame, card).visible, false);
  const copy = write(s, duplicateShot(s.scene, two.id, three));
  assert.equal(shown(copy, three.frame, card).visible, true);
  // Again on another shot: the old channel is replaced.
  const moved = write(copy, onlyInShot(copy.scene, card, one.id));
  assert.deepEqual(
    [one, two, three].map((m) => shown(moved, m.frame, card).visible),
    [true, false, false],
  );
});
