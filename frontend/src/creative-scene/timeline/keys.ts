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
  withBaseValue,
  type ChannelSpec,
} from "./channels.ts";
import { sampleKeys } from "./evaluate.ts";

// The editor's side of Epic design S1–S6: each shot or channel edit as the
// one batch that keeps every channel keyed at every shot (P8); the server
// only checks. The assistant's tool layer writes the same keys
// (backend/internal/creativescene/agent_shots.go).

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

/** S5: no channel for path any more, its base value what frame shows, so nothing jumps there. */
export function clearChannel(
  scene: Scene,
  objectID: string,
  path: string,
  frame: number,
): SceneOperation[] {
  const o = scene.objects.find((x) => x.id === objectID);
  const ch = o?.animation?.[path];
  const spec = o && channelOf(path, o.kind);
  if (!o || !ch || !spec) return [];
  const shown = withBaseValue(o, path, sampleKeys(spec.sample, ch.keys, frame));
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
