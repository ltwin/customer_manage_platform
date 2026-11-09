import type {
  AnimationPatch,
  Key,
  Marker,
  ObjectUpdate,
  Scene,
  SceneObject,
  SceneOperation,
} from "../api.ts";
import {
  baseValue,
  channelOf,
  channelPaths,
  inContentUnit,
  withBaseValue,
  type ChannelSpec,
} from "./channels.ts";
import {
  evaluateObject,
  sampleKeys,
  withUnkeyed,
  type PathValues,
} from "./evaluate.ts";
import { objectOfUnit, type WireOp } from "../state/apply.ts";

// The editor's side of Epic design S1–S6: each shot or channel edit as the
// one batch that keeps every channel keyed at every shot (P8); the server
// only checks. The assistant's tool layer writes the same keys
// (backend/internal/creativescene/agent_shots.go).

/** A shot's title: its name, or 「分镜 N」 by frame order, as the server's markerTitle. */
export function shotTitle(markers: readonly Marker[], id: string): string {
  const i = markers.findIndex((m) => m.id === id);
  if (i < 0) return "";
  return markers[i].name || `分镜 ${i + 1}`;
}

/** How far after the last shot a new one goes, leaving frames between for animation later. */
export const shotFrameStep = 10;

/** The frame a new shot takes by default: shotFrameStep after the last, or 0. */
export const nextShotFrame = (markers: readonly Marker[]) =>
  markers.length ? Math.max(...markers.map((m) => m.frame)) + shotFrameStep : 0;

type Patch = {
  clear: string[];
  delete: NonNullable<AnimationPatch["delete"]>;
  set: NonNullable<AnimationPatch["set"]>;
};
const emptyPatch = (): Patch => ({ clear: [], delete: [], set: [] });

const asPatch = (p: Patch): AnimationPatch => ({
  ...(p.clear.length ? { clear: p.clear } : {}),
  ...(p.delete.length ? { delete: p.delete } : {}),
  ...(p.set.length ? { set: p.set } : {}),
});

const updates = (items: ObjectUpdate[]): SceneOperation[] =>
  items.length ? [{ op: "update_objects", updates: items }] : [];

/**
 * One update per object whose channels visit writes to, the channels
 * visited in object and path order so the keys come out the same every
 * time.
 */
function eachChannel(
  scene: Scene,
  visit: (
    o: SceneObject,
    path: string,
    spec: ChannelSpec,
    keys: readonly Key[],
    patch: Patch,
  ) => void,
): ObjectUpdate[] {
  const out: ObjectUpdate[] = [];
  for (const o of scene.objects) {
    if (!o.animation) continue;
    const patch = emptyPatch();
    for (const path of Object.keys(o.animation).sort()) {
      const spec = channelOf(path, o.kind);
      if (spec) visit(o, path, spec, o.animation[path].keys, patch);
    }
    if (patch.delete.length || patch.set.length)
      out.push({ object_id: o.id, animation: asPatch(patch) });
  }
  return out;
}

const withMarkers = (markers: Marker[]): SceneOperation => ({
  op: "set_timeline",
  timeline: { markers },
});

/** The base fields of next that differ from o's, as an update writes them. */
function baseFields(o: SceneObject, next: SceneObject): Partial<ObjectUpdate> {
  const out: Partial<ObjectUpdate> = {};
  if (JSON.stringify(next.transform) !== JSON.stringify(o.transform))
    out.transform = next.transform;
  if (next.visible !== o.visible) out.visible = next.visible;
  const camera = Object.fromEntries(
    Object.entries(next.camera ?? {}).filter(
      ([k, v]) =>
        JSON.stringify(v) !==
        JSON.stringify(o.camera?.[k as keyof typeof o.camera]),
    ),
  );
  if (Object.keys(camera).length) out.camera = camera;
  return out;
}

/** A value written at frame, and the value from before the change for a property without a channel yet. */
export type KeyWrite = { path: string; value: unknown; before: unknown };

/**
 * I at frame (any frame, a shot's or not). A property with a channel gets
 * the key at frame. One without gets its first channel (S1): value at
 * frame, before at every other shot, and before as the base value, the
 * default state new shots take.
 */
export function firstKey(
  scene: Scene,
  objectID: string,
  frame: number,
  writes: readonly KeyWrite[],
): SceneOperation[] {
  const o = scene.objects.find((x) => x.id === objectID);
  if (!o) return [];
  const patch = emptyPatch();
  let base = o;
  for (const w of writes) {
    if (!channelOf(w.path, o.kind)) continue;
    if (!o.animation?.[w.path]) {
      for (const m of scene.timeline.markers)
        if (m.frame !== frame)
          patch.set.push({ path: w.path, frame: m.frame, value: w.before });
      base = withBaseValue(base, w.path, w.before);
    }
    patch.set.push({ path: w.path, frame, value: w.value });
  }
  if (!patch.set.length) return [];
  return updates([
    { object_id: o.id, ...baseFields(o, base), animation: asPatch(patch) },
  ]);
}

/** S2a: a new shot, every channel keyed there with its base value (the default state). */
export function addShot(scene: Scene, shot: Marker): SceneOperation[] {
  return [
    withMarkers([...scene.timeline.markers, shot]),
    ...updates(
      eachChannel(scene, (o, path, _spec, _keys, patch) => {
        patch.set.push({ path, frame: shot.frame, value: baseValue(o, path) });
      }),
    ),
  ];
}

/** S2b: a new shot exactly as shot from shows, the objects only in it included. */
export function duplicateShot(
  scene: Scene,
  from: string,
  shot: Marker,
): SceneOperation[] {
  const source = scene.timeline.markers.find((m) => m.id === from);
  if (!source) return [];
  return [
    withMarkers([...scene.timeline.markers, shot]),
    ...updates(
      eachChannel(scene, (_o, path, spec, keys, patch) => {
        patch.set.push({
          path,
          frame: shot.frame,
          value: sampleKeys(spec.sample, keys, source.frame),
        });
      }),
    ),
  ];
}

/** S3: the shot to another frame, each channel's key there moving with it and replacing what that frame had. */
export function moveShot(
  scene: Scene,
  id: string,
  frame: number,
): SceneOperation[] {
  const shot = scene.timeline.markers.find((m) => m.id === id);
  if (!shot || shot.frame === frame) return [];
  return [
    withMarkers(
      scene.timeline.markers.map((m) => (m.id === id ? { ...m, frame } : m)),
    ),
    ...updates(
      eachChannel(scene, (_o, path, _spec, keys, patch) => {
        const k = keys.find((x) => x.frame === shot.frame);
        if (!k) return;
        patch.delete.push({ path, frame: shot.frame });
        patch.set.push({ path, frame, value: k.value, interp: k.interp });
      }),
    ),
  ];
}

/** S4: the shot and each channel's key at its frame. */
export function removeShot(scene: Scene, id: string): SceneOperation[] {
  const shot = scene.timeline.markers.find((m) => m.id === id);
  if (!shot) return [];
  return [
    withMarkers(scene.timeline.markers.filter((m) => m.id !== id)),
    ...updates(
      eachChannel(scene, (_o, path, _spec, keys, patch) => {
        if (keys.some((k) => k.frame === shot.frame))
          patch.delete.push({ path, frame: shot.frame });
      }),
    ),
  ];
}

/**
 * S5: no channel for path any more, its base value what frame shows, so
 * nothing jumps there; value, when given, is what the editor shows instead
 * (a change not keyed yet).
 */
export function clearChannel(
  scene: Scene,
  objectID: string,
  path: string,
  frame: number,
  value?: unknown,
): SceneOperation[] {
  const o = scene.objects.find((x) => x.id === objectID);
  const ch = o?.animation?.[path];
  const spec = o && channelOf(path, o.kind);
  if (!o || !ch || !spec) return [];
  const shown = withBaseValue(
    o,
    path,
    value !== undefined ? value : sampleKeys(spec.sample, ch.keys, frame),
  );
  return updates([
    { object_id: o.id, ...baseFields(o, shown), animation: { clear: [path] } },
  ]);
}

/**
 * S6 on an object already there: hidden by default and visible only at
 * shot, so a new shot leaves it out and a copy of shot takes it along.
 */
export function onlyInShot(
  scene: Scene,
  objectID: string,
  shot: string,
): SceneOperation[] {
  const o = scene.objects.find((x) => x.id === objectID);
  if (!o || !scene.timeline.markers.some((m) => m.id === shot)) return [];
  return updates([
    {
      object_id: o.id,
      visible: false,
      animation: {
        ...(o.animation?.visible ? { clear: ["visible"] } : {}),
        set: scene.timeline.markers.map((m) => ({
          path: "visible",
          frame: m.frame,
          value: m.id === shot,
        })),
      },
    },
  ]);
}

// --- the editor's edits (design「编辑分流」) ------------------------------

/** What an edit or I needs from the editor besides the scene (sceneStore). */
export type TimelineEdit = {
  frame: number;
  autoKey: boolean;
  unkeyed: PathValues;
  baseline: PathValues;
};

const transformPaths = [
  "transform.position",
  "transform.rotation",
  "transform.scale",
];

/** Equal as stored values, numbers within float noise: a gizmo hands back what it did not move recomputed. */
function same(a: unknown, b: unknown): boolean {
  if (typeof a === "number" && typeof b === "number")
    return Math.abs(a - b) <= 1e-9 * Math.max(1, Math.abs(a), Math.abs(b));
  if (Array.isArray(a) && Array.isArray(b))
    return a.length === b.length && a.every((x, i) => same(x, b[i]));
  return JSON.stringify(a) === JSON.stringify(b);
}

const withValue = (
  values: PathValues,
  id: string,
  path: string,
  value: unknown,
): PathValues => ({
  ...values,
  [id]: { ...values[id], [path]: value },
});

function without(
  values: PathValues,
  id: string,
  paths: readonly string[],
): PathValues {
  const left = Object.fromEntries(
    Object.entries(values[id] ?? {}).filter(([p]) => !paths.includes(p)),
  );
  if (Object.keys(left).length === Object.keys(values[id] ?? {}).length)
    return values;
  const { [id]: _, ...rest } = values;
  return Object.keys(left).length ? { ...rest, [id]: left } : rest;
}

/** The keyable values u writes that differ from what shown shows. */
function written(shown: SceneObject, u: ObjectUpdate): Map<string, unknown> {
  const out = new Map<string, unknown>();
  for (const path of channelPaths(shown.kind)) {
    const [head, field] = path.split(".");
    const value =
      head === "transform"
        ? u.transform?.[field as keyof NonNullable<ObjectUpdate["transform"]>]
        : head === "visible"
          ? u.visible
          : u.camera?.[field as keyof NonNullable<ObjectUpdate["camera"]>];
    if (value !== undefined && !same(value, baseValue(shown, path)))
      out.set(path, value);
  }
  return out;
}

/** u writing value as path's base value instead of what it wrote; a field left as it is goes. */
function withBase(
  u: ObjectUpdate,
  o: SceneObject,
  path: string,
  value: unknown,
): ObjectUpdate {
  const [head, field] = path.split(".");
  if (head === "visible") {
    const { visible: _, ...rest } = u;
    return same(value, o.visible)
      ? rest
      : { ...rest, visible: value as boolean };
  }
  if (head === "transform") {
    const transform = { ...(u.transform ?? o.transform), [field]: value };
    const { transform: _, ...rest } = u;
    return same(transform, o.transform) ? rest : { ...rest, transform };
  }
  const camera: Record<string, unknown> = { ...u.camera };
  delete camera[field];
  const { camera: _, ...rest } = u;
  const keep = same(value, baseValue(o, path))
    ? camera
    : { ...camera, [field]: value };
  return Object.keys(keep).length ? { ...rest, camera: keep } : rest;
}

/**
 * Routes the editor's ops by what the timeline says of each property they
 * write, against the stored scene. A property with a channel takes a key at
 * the frame with 自动关键帧 on, else the value waits unkeyed (no command). One
 * without a channel takes its first key (S1) with 自动关键帧 on and shots in
 * the scene; otherwise its base value changes in every shot, and the value
 * before is kept as the baseline for I. Everything else passes as it is.
 */
export function routeEdit(
  scene: Scene,
  ops: readonly WireOp[],
  t: TimelineEdit,
): { ops: WireOp[]; unkeyed: PathValues; baseline: PathValues } {
  const byID = new Map(scene.objects.map((o) => [o.id, o]));
  const markers = scene.timeline.markers;
  let { unkeyed, baseline } = t;
  const out: WireOp[] = [];
  for (const op of ops) {
    if (op.op !== "update_objects") {
      out.push(op);
      continue;
    }
    const items: ObjectUpdate[] = [];
    for (const u of op.updates) {
      const o = byID.get(u.object_id);
      if (!o) {
        items.push(u);
        continue;
      }
      const shown = withUnkeyed(evaluateObject(o, t.frame), unkeyed[o.id]);
      // The transform is written whole: the parts with channels stay as stored.
      let next = u;
      if (u.transform)
        for (const path of transformPaths)
          if (o.animation?.[path])
            next = withBase(next, o, path, baseValue(o, path));
      const set: NonNullable<AnimationPatch["set"]> = [];
      for (const [path, value] of written(shown, u)) {
        const keyed = !!o.animation?.[path];
        if (!keyed && !(t.autoKey && markers.length)) {
          if (markers.length && baseline[o.id]?.[path] === undefined)
            baseline = withValue(baseline, o.id, path, baseValue(o, path));
          continue;
        }
        if (!t.autoKey) {
          next = withBase(next, o, path, baseValue(o, path));
          unkeyed = withValue(unkeyed, o.id, path, value);
          continue;
        }
        if (keyed) {
          next = withBase(next, o, path, baseValue(o, path));
          unkeyed = without(unkeyed, o.id, [path]);
        } else {
          // S1; the base value goes back to the baseline, if any.
          const before = baseline[o.id]?.[path] ?? baseValue(o, path);
          next = withBase(next, o, path, before);
          baseline = without(baseline, o.id, [path]);
          for (const m of markers)
            if (m.frame !== t.frame)
              set.push({ path, frame: m.frame, value: before });
        }
        set.push({ path, frame: t.frame, value });
      }
      if (set.length)
        next = {
          ...next,
          animation: {
            ...next.animation,
            set: [...(next.animation?.set ?? []), ...set],
          },
        };
      if (Object.keys(next).length > 1) items.push(next);
    }
    if (items.length) out.push({ ...op, updates: items });
  }
  return { ops: out, unkeyed, baseline };
}

/**
 * I at the frame on these objects: the paths given, else those changed here
 * (unkeyed or with a baseline), else the transform as Blender keys it. A
 * property with a channel takes its shown value at the frame; one without
 * its first key (firstKey), the other shots taking the baseline. adopted
 * says some other shot took the current value for want of a baseline
 * (「其他分镜也采用了这个值」).
 */
export function insertKeys(
  scene: Scene,
  ids: readonly string[],
  t: Omit<TimelineEdit, "autoKey">,
  paths?: readonly string[],
): {
  ops: WireOp[];
  unkeyed: PathValues;
  baseline: PathValues;
  adopted: boolean;
} {
  let { unkeyed, baseline } = t;
  let adopted = false;
  const updates: ObjectUpdate[] = [];
  const others = scene.timeline.markers.some((m) => m.frame !== t.frame);
  for (const id of ids) {
    const o = scene.objects.find((x) => x.id === id);
    if (!o) continue;
    const changed = [
      ...new Set([
        ...Object.keys(unkeyed[id] ?? {}),
        ...Object.keys(baseline[id] ?? {}),
      ]),
    ];
    const keyed = (paths ?? (changed.length ? changed : transformPaths)).filter(
      (p) => channelOf(p, o.kind),
    );
    const shown = withUnkeyed(evaluateObject(o, t.frame), unkeyed[id]);
    const writes = keyed.map((path) => {
      const before = baseline[id]?.[path];
      if (!o.animation?.[path] && before === undefined && others)
        adopted = true;
      return {
        path,
        value: baseValue(shown, path),
        before: before ?? baseValue(o, path),
      };
    });
    for (const op of firstKey(scene, id, t.frame, writes))
      if (op.op === "update_objects") updates.push(...op.updates);
    unkeyed = without(unkeyed, id, keyed);
    baseline = without(baseline, id, keyed);
  }
  return {
    ops: updates.length ? [{ op: "update_objects", updates }] : [],
    unkeyed,
    baseline,
    adopted,
  };
}

/**
 * The editor's values after the scene changed: unkeyed ones of objects or
 * channels gone go, and so does a baseline once the property has a channel
 * or its base value is back to it (an undo). The same objects when nothing
 * goes.
 */
export function pruneEdits(
  scene: Scene,
  unkeyed: PathValues,
  baseline: PathValues,
): { unkeyed: PathValues; baseline: PathValues } {
  const byID = new Map(scene.objects.map((o) => [o.id, o]));
  const prune = (
    values: PathValues,
    gone: (o: SceneObject, path: string, value: unknown) => boolean,
  ) => {
    let out = values;
    for (const [id, paths] of Object.entries(values)) {
      const o = byID.get(id);
      const drop = Object.entries(paths)
        .filter(([path, value]) => !o || gone(o, path, value))
        .map(([path]) => path);
      if (drop.length) out = without(out, id, drop);
    }
    return out;
  };
  return {
    unkeyed: prune(unkeyed, (o, path) => !o.animation?.[path]),
    baseline: prune(
      baseline,
      (o, path, value) =>
        !!o.animation?.[path] || same(baseValue(o, path), value),
    ),
  };
}

/**
 * The baselines kept in these units dropped: another tab or the assistant
 * wrote them, so the value before is no longer what the base held. The same
 * object when none were kept.
 */
export function forgetUnits(
  values: PathValues,
  units: readonly string[],
): PathValues {
  let out = values;
  for (const u of units) {
    const unit = objectOfUnit(u);
    const paths = unit && out[unit.id];
    if (paths)
      out = without(
        out,
        unit.id,
        Object.keys(paths).filter((p) => inContentUnit(p) === unit.content),
      );
  }
  return out;
}

/** How the properties show path on o at frame (Blender's colours): keyed here, keyed elsewhere, or changed without a key. */
export type KeyState = "key" | "animated" | "changed";

export function keyStateOf(
  o: SceneObject,
  path: string,
  frame: number,
  unkeyed: PathValues,
): KeyState | undefined {
  if (unkeyed[o.id]?.[path] !== undefined) return "changed";
  const ch = o.animation?.[path];
  if (!ch) return undefined;
  return ch.keys.some((k) => k.frame === frame) ? "key" : "animated";
}

/**
 * Alt+I off the shots: each channel's key at frame goes; a channel left
 * without keys goes with it. A shot's keys stay (P8): null says so, for
 * 「清除动画」 instead.
 */
export function deleteKeysAt(
  scene: Scene,
  ids: readonly string[],
  frame: number,
): SceneOperation[] | null {
  if (scene.timeline.markers.some((m) => m.frame === frame)) return null;
  const items: ObjectUpdate[] = [];
  for (const id of ids) {
    const o = scene.objects.find((x) => x.id === id);
    const del = Object.entries(o?.animation ?? {})
      .filter(([, ch]) => ch.keys.some((k) => k.frame === frame))
      .map(([path]) => ({ path, frame }));
    if (o && del.length)
      items.push({ object_id: o.id, animation: { delete: del } });
  }
  return updates(items);
}
