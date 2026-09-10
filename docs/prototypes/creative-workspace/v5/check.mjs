import assert from "node:assert/strict";
import { readFile, access } from "node:fs/promises";
import { seed, connect, removeNodes, bounds } from "./model.mjs";
import { pack } from "../v4/masonry.mjs";

const original = seed(),
  doc = structuredClone(original);
removeNodes(doc, ["n-main", "n-plan"]);
assert.equal(
  doc.nodes.some((n) => n.id === "n-main" || n.id === "n-plan"),
  false,
);
assert.equal(doc.edges.length, 0);
assert.deepEqual(
  doc.plans,
  original.plans,
  "移除节点必须完整保留策划、镜头和发布版本",
);
doc.plans[0].shots[0].note = "新的工作笔记";
assert.notEqual(
  doc.plans[0].publication.snapshot.shots[0].note,
  "新的工作笔记",
  "修改工作版本不能污染发布快照",
);
const live = structuredClone(doc.plans[0]);
doc.plans[0].shots.pop();
assert.equal(live.shots.length, 3, "现场快照应独立于工作版本的镜头集合");
connect(doc, "n-sea", "n-text");
assert.throws(() => connect(doc, "n-sea", "n-text"), /已经连接/);
assert.throws(() => connect(doc, "n-sea", "n-sea"), /另一个节点/);
assert.throws(() => connect(doc, "missing", "n-text"), /不存在/);
assert.equal(
  pack([], 269, { mobile: true }).columns,
  1,
  "保持独立 v4 原有窄屏规则",
);
assert.equal(
  pack([], 269, { mobile: true, minimumMobileWidth: 100 }).columns,
  2,
  "侧栏变体维持双列",
);
assert.ok(bounds(original.nodes).width > 0);
const html = await readFile(new URL("./index.html", import.meta.url), "utf8");
assert.equal(
  /id="(?:plans-open|plan-rail)"/.test(html),
  false,
  "策划不允许独立侧栏或顶部页面入口",
);
for (const name of [
  "app.mjs",
  "canvas.css",
  "planning.css",
  "tokens.css",
  "icons.mjs",
  "LICENSE-lucide",
])
  await access(new URL(name, import.meta.url));
console.log(
  "通过：节点移除与文档独立、发布/现场快照、参考边守卫、侧栏几何兼容、最大化入口与资源清单。",
);
