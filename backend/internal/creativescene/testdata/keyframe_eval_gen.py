"""Reference evaluator for patterns: 关键帧求值. Regenerate: python3 -I keyframe_eval_gen.py keyframe_eval.json"""
import json, math, sys

def sample(kind, keys, frame):
    if not keys:
        return None  # an empty channel gives no value; the base value stays
    if frame <= keys[0]["frame"]:
        return keys[0]["value"]
    if frame >= keys[-1]["frame"]:
        return keys[-1]["value"]
    for a, b in zip(keys, keys[1:]):
        if a["frame"] <= frame < b["frame"]:
            break
    if frame == a["frame"] or a["interp"] == "constant" or kind == "discrete":
        return a["value"]
    t = (frame - a["frame"]) / (b["frame"] - a["frame"])
    if kind == "number":
        return a["value"] + (b["value"] - a["value"]) * t
    if kind == "vector":
        return [x + (y - x) * t for x, y in zip(a["value"], b["value"])]
    qa, qb = a["value"], list(b["value"])
    dot = sum(x * y for x, y in zip(qa, qb))
    if dot < 0:
        qb = [-x for x in qb]
        dot = -dot
    theta = math.acos(min(dot, 1.0))
    if theta < 1e-6:
        q = [x + (y - x) * t for x, y in zip(qa, qb)]
    else:
        s = math.sin(theta)
        wa, wb = math.sin((1 - t) * theta) / s, math.sin(t * theta) / s
        q = [wa * x + wb * y for x, y in zip(qa, qb)]
    n = math.sqrt(sum(x * x for x in q))
    return [x / n for x in q]

def k(frame, value, interp="linear"):
    return {"frame": frame, "value": value, "interp": interp}

h = math.sqrt(0.5)
cases = [
    ("单个关键帧在所有帧取它的值", "number", [k(10, 3.5)], [0, 10, 50]),
    ("首前与尾后取端点值", "number", [k(10, 1), k(20, 3)], [0, 9, 10, 20, 21, 100]),
    ("线性插值", "number", [k(0, 0), k(10, 10), k(30, -10)], [0, 3, 10, 15, 25, 30]),
    ("阶跃保持左值", "number", [k(0, 1, "constant"), k(10, 5)], [0, 5, 9, 10]),
    ("混合插值按左关键帧", "number", [k(0, 0), k(10, 10, "constant"), k(20, 0)], [5, 10, 15, 20]),
    ("向量逐分量插值", "vector", [k(0, [0, 0, 0]), k(4, [4, -8, 2])], [1, 2, 3]),
    ("四元数球面插值", "quaternion", [k(0, [0, 0, 0, 1]), k(10, [0, h, 0, h])], [0, 2, 5, 8, 10]),
    ("四元数走最短路径", "quaternion", [k(0, [0, 0, 0, 1]), k(10, [0, -h, 0, -h])], [5]),
    ("四元数几乎相同时退化为线性", "quaternion", [k(0, [0, 0, 0, 1]), k(10, [0, 1e-9, 0, 1])], [5]),
    ("四元数接近 180 度", "quaternion", [k(0, [0, 0, 0, 1]), k(10, [0, 1, 0, 1e-4])], [5]),
    ("离散布尔只取左值", "discrete", [k(0, True, "constant"), k(10, False, "constant")], [0, 9, 10, 11]),
    ("离散字符串只取左值", "discrete", [k(0, "3:2", "constant"), k(5, "16:9", "constant")], [4, 5]),
    ("离散数组只取左值", "discrete", [k(0, [36, 24], "constant"), k(5, [23.5, 15.6], "constant")], [4, 5]),
    ("空通道不给值", "number", [], [0, 10]),
]
out = []
for name, kind, keys, frames in cases:
    out.append({"name": name, "kind": kind, "keys": keys,
                "expect": [{"frame": f, "value": sample(kind, keys, f)} for f in frames]})
doc = {"note": "patterns: 关键帧求值 的共用测试向量；由 Python 参考实现生成，Go 与 TS 求值器都必须逐项通过（数值容差 1e-9）。value 为 null 表示通道不给值，保留基础值。改规则时先改这里。",
       "tolerance": 1e-9, "cases": out}
json.dump(doc, open(sys.argv[1], "w"), ensure_ascii=False, indent=1)
open(sys.argv[1], "a").write("\n")
