"""Writes the occluder-wall reference scene, checking that every board, its shadow and the highlight are seen apart."""
import json
import sys

import numpy as np

C = np.array([0.0, 1.8, 7.0])  # camera
WZ = -2.95  # wall's front face
L = np.array([-5.5, 3.6, 4.0])  # lights
aim = np.array([0.0, 1.8, WZ])
specs = [("board005", "遮挡板 0.05 m", 0.05, -2.8, 1.0), ("board050", "遮挡板 0.5 m", 0.5, -1.4, 2.6), ("board300", "遮挡板 3 m", 3.0, float(sys.argv[1]), float(sys.argv[2]))]


def onwall(p, frm):
    t = (WZ - frm[2]) / (p[2] - frm[2])
    return frm + (p - frm) * t


def rect(cx, cy, h):
    return (cx - h, cx + h, cy - h, cy + h)


def overlap(a, b):
    return not (a[1] < b[0] or b[1] < a[0] or a[3] < b[2] or b[3] < a[2])


regions = {}
for n, name, D, x, y in specs:
    z = WZ + D + 0.01
    c = np.array([x, y, z])
    s, v = onwall(c, L), onwall(c, C)
    k, kc = (WZ - L[2]) / (z - L[2]), (WZ - C[2]) / (z - C[2])
    regions[(name, "板")] = rect(v[0], v[1], 0.2 * kc)
    regions[(name, "影")] = rect(s[0], s[1], 0.2 * k)
Cm = C.copy()
Cm[2] = 2 * WZ - C[2]
h = L + (Cm - L) * ((WZ - L[2]) / (Cm[2] - L[2]))
regions[("高光板", "")] = rect(h[0], h[1], 0.5)
for k, r in regions.items():
    print(k, [round(float(t), 2) for t in r])
    assert r[0] > -3.9 and r[1] < 3.9 and r[2] > 0.15 and r[3] < 3.9, k
keys = list(regions)
for i in range(len(keys)):
    for j in range(i + 1, len(keys)):
        if keys[i][0] != keys[j][0]:
            assert not overlap(regions[keys[i]], regions[keys[j]]), (keys[i], keys[j])

# The lights' turn: -Z onto the way to the aim.
d = (aim - L) / np.linalg.norm(aim - L)
f = np.array([0, 0, -1.0])
ax = np.cross(f, d)
ang = np.arctan2(np.linalg.norm(ax), f @ d)
ax /= np.linalg.norm(ax)
q = [round(float(t), 6) for t in [*(ax * np.sin(ang / 2)), np.cos(ang / 2)]]

I = [0, 0, 0, 1]
sid = lambda s: "sobj_" + s.ljust(21, "0")
grey = {"preset": "principled@1", "params": {"color": "#bdbdb8", "roughness": 0.6, "metalness": 0}}
gloss = {"preset": "principled@1", "params": {"color": "#bdbdb8", "roughness": 0.15, "metalness": 0}}


def box(n, name, order, pos, w, hh, dd, mat):
    return {"id": sid(n), "kind": "primitive", "name": name, "parent_id": None, "order": order,
            "transform": {"position": [round(float(t), 4) for t in pos], "rotation": I, "scale": [1, 1, 1]}, "visible": True, "locked": False,
            "primitive": {"builtin": "box@1", "params": {"width": w, "height": hh, "depth": dd}}, "material_slots": {"main": mat}}


def light(n, name, order, visible, l):
    return {"id": sid(n), "kind": "light", "name": name, "parent_id": None, "order": order,
            "transform": {"position": [float(t) for t in L], "rotation": q, "scale": [1, 1, 1]}, "visible": visible, "locked": False, "light": l}


objs = [{"id": "sobj_ground", "kind": "primitive", "name": "地面", "parent_id": None, "order": 0, "transform": {"position": [0, 0, 0], "rotation": I, "scale": [1, 1, 1]},
         "visible": True, "locked": True, "primitive": {"builtin": "ground@1", "params": {"size": 40}}},
        box("wall", "墙", 1, [0, 0, -3], 8, 4, 0.1, grey)]
# A box stands on its base: y is the bottom.
for i, (n, name, D, x, y) in enumerate(specs):
    objs.append(box(n, name, 2 + i, [x, y - 0.2, WZ + D + 0.01], 0.4, 0.4, 0.02, grey))
objs.append(box("gloss", "高光板", 5, [h[0], h[1] - 0.5, WZ + 0.01], 1.0, 1.0, 0.02, gloss))
objs.append({"id": sid("ball"), "kind": "primitive", "name": "球", "parent_id": None, "order": 6, "transform": {"position": [2.6, 0, -1.4], "rotation": I, "scale": [1, 1, 1]},
             "visible": True, "locked": False, "primitive": {"builtin": "sphere@1", "params": {"radius": 0.25, "segments": 64}}, "material_slots": {"main": grey}})
objs.append(light("spot", "聚光", 7, True, {"type": "spot", "color": "#ffffff", "intensity": 250, "cast_shadow": True, "distance": 0, "decay": 2, "angle": 0.9, "penumbra": 0}))
objs.append(light("point", "点光", 8, False, {"type": "point", "color": "#ffffff", "intensity": 250, "cast_shadow": True, "distance": 0, "decay": 2}))
objs.append(light("sun", "平行光", 9, False, {"type": "directional", "color": "#ffffff", "intensity": 3, "cast_shadow": True}))
objs.append({"id": sid("shot"), "kind": "camera", "name": "shot", "parent_id": None, "order": 10, "transform": {"position": [float(t) for t in C], "rotation": I, "scale": [1, 1, 1]},
             "visible": True, "locked": False, "camera": {"focal_mm": 35}})
scene = {"environment": {"sky": {"kind": "color", "color": "#0b0c0e"}, "ambient": {"color": "#ffffff", "intensity": 0.05}, "fog": None},
         "render": {"mode": "lit", "exposure_ev": 0, "engine": "workbench", "resolution": "2k"}, "objects": objs}
json.dump(scene, open(sys.argv[3], "w"), ensure_ascii=False, indent=2)
print("lights' turn", q, "wall centre", round(250 / float(np.linalg.norm(aim - L)) ** 2, 2), "lux")
