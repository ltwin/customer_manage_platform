import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import type { Key, Scene, SceneObject } from "../src/creative-scene/api.ts";
import {
  channelOf,
  channelUnit,
} from "../src/creative-scene/timeline/channels.ts";
import {
  evaluateAt,
  sampleKeys,
} from "../src/creative-scene/timeline/evaluate.ts";
import type { Sample } from "../src/creative-scene/timeline/channels.ts";

// patterns: 关键帧求值 — the vectors the Go evaluator passes too.
type Vectors = {
  tolerance: number;
  cases: {
    name: string;
    kind: Sample;
    keys: Key[];
    expect: { frame: number; value: unknown }[];
  }[];
};
const vectors = JSON.parse(
  readFileSync(
    new URL(
      "../src/creative-scene/timeline/keyframe_eval.gen.json",
      import.meta.url,
    ),
    "utf8",
  ),
) as Vectors;

test("the evaluator passes the shared vectors", () => {
  for (const c of vectors.cases) {
    for (const e of c.expect) {
      const got = sampleKeys(c.kind, c.keys, e.frame);
      const want = e.value === null ? undefined : e.value;
      if (c.kind === "discrete" || want === undefined) {
        assert.deepEqual(got, want, `${c.name} @${e.frame}`);
        continue;
      }
      const g = ([] as number[]).concat(got as number | number[]);
      const w = ([] as number[]).concat(want as number | number[]);
      assert.equal(g.length, w.length, `${c.name} @${e.frame}`);
      g.forEach((x, i) =>
        assert.ok(
          Math.abs(x - w[i]) <= vectors.tolerance,
          `${c.name} @${e.frame}: ${g} vs ${w}`,
        ),
      );
    }
  }
});

test("channels follow the server list", () => {
  assert.equal(channelOf("camera.focal_mm", "primitive"), undefined);
  assert.equal(channelOf("camera.focal_mm", "camera")?.sample, "number");
  assert.equal(channelOf("name", "primitive"), undefined);
  assert.equal(channelUnit("sobj_1", "transform.position"), "object:sobj_1");
  assert.equal(channelUnit("sobj_1", "camera.aspect"), "object:sobj_1:content");
});

test("evaluateAt writes channels into base fields and keeps the rest", () => {
  const plain = {
    id: "sobj_b",
    kind: "primitive",
    visible: true,
    transform: {
      position: [0, 0, 0],
      rotation: [0, 0, 0, 1],
      scale: [1, 1, 1],
    },
  } as SceneObject;
  const cam = {
    id: "sobj_c",
    kind: "camera",
    visible: true,
    transform: {
      position: [0, 1, 5],
      rotation: [0, 0, 0, 1],
      scale: [1, 1, 1],
    },
    camera: { focal_mm: 35 },
    animation: {
      "transform.position": {
        keys: [
          { frame: 0, value: [0, 1, 5], interp: "linear" },
          { frame: 10, value: [10, 1, 5], interp: "linear" },
        ],
      },
      "camera.focal_mm": {
        keys: [
          { frame: 0, value: 35, interp: "constant" },
          { frame: 10, value: 85, interp: "linear" },
        ],
      },
    },
  } as unknown as SceneObject;
  const scene = {
    timeline: { fps: 24, markers: [] },
    objects: [cam, plain],
  } as unknown as Scene;
  const at5 = evaluateAt(scene, 5);
  assert.deepEqual(at5.objects[0].transform.position, [5, 1, 5]);
  assert.equal(at5.objects[0].camera?.focal_mm, 35);
  assert.equal(at5.objects[0].animation, undefined);
  assert.equal(
    at5.objects[1],
    plain,
    "an object without channels is the same object",
  );
  assert.equal(evaluateAt(scene, 10).objects[0].camera?.focal_mm, 85);
  assert.deepEqual(
    cam.transform.position,
    [0, 1, 5],
    "the input is not changed",
  );
  const still = { ...scene, objects: [plain] };
  assert.equal(
    evaluateAt(still, 3),
    still,
    "a scene without channels is the same scene",
  );
});
