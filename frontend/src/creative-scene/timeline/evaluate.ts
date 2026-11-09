import type { Key, Scene, SceneObject } from "../api.ts";
import { channelOf, withBaseValue, type Sample } from "./channels.ts";

// patterns: 关键帧求值, the editor's copy. The server's is
// backend/internal/creativescene/evaluate.go; both pass
// keyframe_eval.gen.json, so change the vectors before the rule.

/** The value of keys at frame; undefined for an empty channel, which leaves the base value. */
export function sampleKeys(
  sample: Sample,
  keys: readonly Key[],
  frame: number,
): unknown {
  if (keys.length === 0) return undefined;
  let lo = 0;
  let hi = keys.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (keys[mid].frame < frame) lo = mid + 1;
    else hi = mid;
  }
  if (lo < keys.length && keys[lo].frame === frame) return keys[lo].value;
  if (lo === 0) return keys[0].value;
  if (lo === keys.length) return keys[lo - 1].value;
  const a = keys[lo - 1];
  const b = keys[lo];
  if (sample === "discrete" || a.interp === "constant") return a.value;
  const t = (frame - a.frame) / (b.frame - a.frame);
  if (sample === "number")
    return lerp([a.value as number], [b.value as number], t)[0];
  if (sample === "vector")
    return lerp(a.value as number[], b.value as number[], t);
  return slerp(a.value as number[], b.value as number[], t);
}

function lerp(a: readonly number[], b: readonly number[], t: number): number[] {
  return a.map((x, i) => x + (b[i] - x) * t);
}

// The shorter way round; below 1e-6 rad sin θ loses precision, so a
// normalized lerp instead.
function slerp(
  a: readonly number[],
  b: readonly number[],
  t: number,
): number[] {
  let dot = a[0] * b[0] + a[1] * b[1] + a[2] * b[2] + a[3] * b[3];
  let to = b;
  if (dot < 0) {
    to = b.map((x) => -x);
    dot = -dot;
  }
  const theta = Math.acos(Math.min(dot, 1));
  let q: number[];
  if (theta < 1e-6) {
    q = lerp(a, to, t);
  } else {
    const s = Math.sin(theta);
    const wa = Math.sin((1 - t) * theta) / s;
    const wb = Math.sin(t * theta) / s;
    q = a.map((x, i) => wa * x + wb * to[i]);
  }
  const n = Math.hypot(q[0], q[1], q[2], q[3]);
  return n === 0 ? [0, 0, 0, 1] : q.map((x) => x / n);
}

/** o at frame: each channel's value written into its base field, animation left out. */
export function evaluateObject(o: SceneObject, frame: number): SceneObject {
  const { animation, ...base } = o;
  if (!animation) return o;
  let out: SceneObject = base;
  for (const [path, channel] of Object.entries(animation)) {
    const spec = channelOf(path, o.kind);
    if (!spec) continue;
    const value = sampleKeys(spec.sample, channel.keys, frame);
    if (value !== undefined) out = withBaseValue(out, path, value);
  }
  return out;
}

/**
 * The scene as it stands at frame. Objects without channels are the same
 * objects, so whatever is keyed on object identity keeps working; the
 * timeline stays for the panels that list shots.
 */
export function evaluateAt(scene: Scene, frame: number): Scene {
  if (!scene.objects.some((o) => o.animation)) return scene;
  return {
    ...scene,
    objects: scene.objects.map((o) => evaluateObject(o, frame)),
  };
}
