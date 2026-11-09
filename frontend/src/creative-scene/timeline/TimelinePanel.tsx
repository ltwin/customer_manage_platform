import { useRef, useState, type PointerEvent } from "react";
import type { Marker, SceneObject, Timeline } from "../api.ts";
import { shotTitle } from "./keys.ts";

// The timeline under the viewport (design「前端」): a frame ruler with the
// shots on it, the current frame, the active object's keys and the auto-key
// switch. Shots are static pictures, so there is no playback.

const steps = [1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000];

/** Drags shorter than this are clicks. */
const dragSlop = 3;

export function TimelinePanel({
  timeline,
  cameras,
  keys,
  frame,
  autoKey,
  readOnly,
  onFrame,
  onShot,
  onAdd,
  onDuplicate,
  onRemove,
  onMove,
  onRename,
  onBind,
  onAutoKey,
}: {
  timeline: Timeline;
  cameras: readonly SceneObject[];
  /** The frames of the active object's keys. */
  keys: readonly number[];
  frame: number;
  autoKey: boolean;
  readOnly: boolean;
  onFrame: (frame: number) => void;
  /** Go to a shot: its frame and its camera's view. */
  onShot: (shot: Marker) => void;
  onAdd: () => void;
  onDuplicate: (id: string) => void;
  onRemove: (id: string) => void;
  onMove: (id: string, frame: number) => void;
  onRename: (id: string, name: string) => void;
  onBind: (id: string, camera: string) => void;
  onAutoKey: (on: boolean) => void;
}) {
  const { markers } = timeline;
  const track = useRef<HTMLDivElement>(null);
  const [drag, setDrag] = useState<{ id: string; frame: number } | null>(null);
  const press = useRef<{ x: number; moved: boolean } | null>(null);
  const end = Math.max(
    60,
    frame + 10,
    ...markers.map((m) => m.frame + 20),
    ...keys.map((k) => k + 10),
  );
  const step = steps.find((s) => end / s <= 12) ?? steps.at(-1)!;
  const at = (f: number) => `${(f / end) * 100}%`;
  const frameAt = (clientX: number) => {
    const r = track.current!.getBoundingClientRect();
    return Math.max(
      0,
      Math.min(100000, Math.round(((clientX - r.left) / r.width) * end)),
    );
  };
  const current = markers.find((m) => m.frame === frame);
  const taken = (f: number, id: string) =>
    markers.some((m) => m.frame === f && m.id !== id);

  const scrub = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    onFrame(frameAt(e.clientX));
  };
  const grab = (e: PointerEvent<HTMLButtonElement>) => {
    if (e.button !== 0) return;
    e.stopPropagation();
    e.currentTarget.setPointerCapture(e.pointerId);
    press.current = { x: e.clientX, moved: false };
  };
  const pull = (e: PointerEvent<HTMLButtonElement>, m: Marker) => {
    const p = press.current;
    if (!p || readOnly) return;
    if (!p.moved && Math.abs(e.clientX - p.x) < dragSlop) return;
    p.moved = true;
    setDrag({ id: m.id, frame: frameAt(e.clientX) });
  };
  const release = (m: Marker) => {
    const p = press.current;
    press.current = null;
    setDrag(null);
    if (!p?.moved) return onShot(m);
    if (drag && drag.frame !== m.frame && !taken(drag.frame, m.id))
      onMove(m.id, drag.frame);
  };

  return (
    <section className="cc-scene-timeline" aria-label="时间线">
      <div className="cc-scene-timeline-bar">
        <button
          type="button"
          className="btn"
          disabled={readOnly}
          onClick={onAdd}
        >
          新建分镜
        </button>
        <button
          type="button"
          className="btn"
          disabled={readOnly || !current}
          onClick={() => current && onDuplicate(current.id)}
        >
          复制分镜
        </button>
        <button
          type="button"
          className="btn"
          disabled={readOnly || !current}
          onClick={() => {
            if (!current) return;
            onRemove(current.id);
            // The button is disabled once its shot is gone, and Chrome sends
            // no blur for that: keep the keys in the editor.
            track.current?.focus();
          }}
        >
          删除分镜
        </button>
        {current && (
          <>
            <input
              key={current.id + current.name}
              className="cc-scene-timeline-name"
              aria-label="分镜名称"
              defaultValue={current.name}
              placeholder={shotTitle(markers, current.id)}
              maxLength={80}
              disabled={readOnly}
              onBlur={(e) =>
                e.target.value !== current.name &&
                onRename(current.id, e.target.value)
              }
              onKeyDown={(e) => {
                if (e.key === "Enter") e.currentTarget.blur();
                if (e.key === "Escape") {
                  e.currentTarget.value = current.name;
                  e.currentTarget.blur();
                }
                e.stopPropagation();
              }}
            />
            <select
              aria-label="分镜摄像机"
              value={current.camera_object_id}
              disabled={readOnly}
              onChange={(e) => onBind(current.id, e.target.value)}
            >
              <option value="">未绑定摄像机</option>
              {current.camera_object_id &&
                !cameras.some((c) => c.id === current.camera_object_id) && (
                  <option value={current.camera_object_id}>（已删除）</option>
                )}
              {cameras.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name || "摄像机"}
                </option>
              ))}
            </select>
          </>
        )}
        <span className="cc-scene-timeline-frame" aria-live="polite">
          第 {frame} 帧
        </span>
        <label className="cc-scene-timeline-auto">
          <input
            type="checkbox"
            checked={autoKey}
            onChange={(e) => onAutoKey(e.target.checked)}
          />
          自动关键帧
        </label>
      </div>
      <div
        ref={track}
        className="cc-scene-timeline-track"
        role="slider"
        aria-label="当前帧"
        aria-valuemin={0}
        aria-valuemax={end}
        aria-valuenow={frame}
        tabIndex={0}
        onPointerDown={scrub}
        onPointerMove={(e) => {
          if (e.currentTarget.hasPointerCapture(e.pointerId))
            onFrame(frameAt(e.clientX));
        }}
        onKeyDown={(e) => {
          if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
          e.preventDefault();
          e.stopPropagation();
          onFrame(Math.max(0, frame + (e.key === "ArrowLeft" ? -1 : 1)));
        }}
      >
        {Array.from({ length: Math.floor(end / step) + 1 }, (_, i) => (
          <span
            key={i}
            className="cc-scene-timeline-tick"
            style={{ left: at(i * step) }}
          >
            {i * step}
          </span>
        ))}
        {keys.map((k) => (
          <span
            key={k}
            className="cc-scene-timeline-key"
            style={{ left: at(k) }}
            aria-hidden
          />
        ))}
        {markers.map((m) => {
          const f = drag?.id === m.id ? drag.frame : m.frame;
          return (
            <button
              key={m.id}
              type="button"
              className="cc-scene-timeline-shot"
              style={{ left: at(f) }}
              aria-pressed={m.frame === frame}
              aria-invalid={drag?.id === m.id && taken(f, m.id)}
              title={`第 ${f} 帧`}
              onClick={(e) => e.detail === 0 && onShot(m)}
              onPointerDown={grab}
              onPointerMove={(e) => pull(e, m)}
              onPointerUp={() => release(m)}
              onPointerCancel={() => {
                press.current = null;
                setDrag(null);
              }}
            >
              {shotTitle(markers, m.id)}
            </button>
          );
        })}
        <span
          className="cc-scene-timeline-head"
          style={{ left: at(frame) }}
          aria-hidden
        />
      </div>
    </section>
  );
}
