import list from "./channels.gen.json" with { type: "json" };
import type { SceneObject } from "../api.ts";

// The keyframe channels (data: 场景时间线), generated from
// backend/internal/creativescene/channels.json; the server validates against
// the same file. A channel lives in the unit of the base field it keys.

export type Sample = "number" | "vector" | "quaternion" | "discrete";

export type ChannelSpec = {
  path: string;
  unit: "placement" | "content";
  camera_only: boolean;
  sample: Sample;
};

export const channelSpecs: readonly ChannelSpec[] =
  list as unknown as ChannelSpec[];

const byPath = new Map(channelSpecs.map((c) => [c.path, c]));

/** The spec of path on an object of kind, or undefined when it cannot be keyed there. */
export function channelOf(path: string, kind: string): ChannelSpec | undefined {
  const spec = byPath.get(path);
  return spec && (!spec.camera_only || kind === "camera") ? spec : undefined;
}

/** The paths an object of kind can key, in the list's order. */
export function channelPaths(kind: string): string[] {
  return channelSpecs
    .filter((c) => !c.camera_only || kind === "camera")
    .map((c) => c.path);
}

/** Whether path's channel lives in the content unit rather than the placement one. */
export const inContentUnit = (path: string) =>
  byPath.get(path)?.unit === "content";

/** The unit holding path's channel on object id. */
export function channelUnit(id: string, path: string): string {
  return inContentUnit(path) ? `object:${id}:content` : `object:${id}`;
}

/** o's base value of path; undefined when o has no such field. */
export function baseValue(o: SceneObject, path: string): unknown {
  const [head, field] = path.split(".");
  switch (head) {
    case "transform":
      return o.transform[field as keyof SceneObject["transform"]];
    case "visible":
      return o.visible;
    case "camera":
      return o.camera?.[field as keyof NonNullable<SceneObject["camera"]>];
  }
  return undefined;
}

/** o with the base value of path replaced; o itself is not changed. */
export function withBaseValue(
  o: SceneObject,
  path: string,
  value: unknown,
): SceneObject {
  const [head, field] = path.split(".");
  switch (head) {
    case "transform":
      return { ...o, transform: { ...o.transform, [field]: value } };
    case "visible":
      return { ...o, visible: value as boolean };
    case "camera":
      return o.camera ? { ...o, camera: { ...o.camera, [field]: value } } : o;
  }
  return o;
}
