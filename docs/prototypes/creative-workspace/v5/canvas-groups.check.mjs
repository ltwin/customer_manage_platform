import assert from "node:assert/strict";
import { seed, removeNodes, connect } from "./model.mjs";
import {
  worldNodes,
  groupNodes,
  ungroupNodes,
  selectionRoots,
  descendantIds,
  fitGroupFrames,
  copyGroupedNodes,
} from "./canvas-groups.mjs";
const pos = (doc, id) => worldNodes(doc.nodes).find((n) => n.id === id);
const xy = (n) => ({ x: n.x, y: n.y });
const d = seed(),
  original = structuredClone(d),
  before = new Map(worldNodes(d.nodes).map((n) => [n.id, xy(n)]));
const g = groupNodes(d, ["n-brief", "n-sea"]);
fitGroupFrames(d.nodes);
for (const id of before.keys())
  assert.deepEqual(xy(pos(d, id)), before.get(id), "成组不能改变世界坐标");
const group = d.nodes.find((n) => n.id === g),
  child = d.nodes.find((n) => n.id === "n-sea");
assert.equal(child.parentId, g);
assert.deepEqual(xy(pos(d, child.id)), {
  x: group.x + child.x,
  y: group.y + child.y,
});
const childLocal = xy(child);
group.x += 125;
group.y -= 60;
fitGroupFrames(d.nodes);
assert.deepEqual(xy(child), childLocal, "整组拖动只改变父组坐标");
assert.deepEqual(xy(pos(d, child.id)), {
  x: before.get(child.id).x + 125,
  y: before.get(child.id).y - 60,
});
assert.deepEqual(
  xy(pos(d, "n-main")),
  before.get("n-main"),
  "组外节点不能跟随",
);
assert.deepEqual(
  selectionRoots(d.nodes, [g, child.id]).map((n) => n.id),
  [g],
  "同时选父子只能移动一次",
);
const stableSibling = xy(pos(d, "n-brief"));
child.x -= 180;
const movedChild = xy(pos(d, child.id));
fitGroupFrames(d.nodes);
assert.deepEqual(
  xy(pos(d, "n-brief")),
  stableSibling,
  "组边框自适应不能挪动其他子节点",
);
assert.deepEqual(xy(pos(d, child.id)), movedChild);
const outer = groupNodes(d, [g, "n-main"]);
fitGroupFrames(d.nodes);
assert.equal(d.nodes.find((n) => n.id === g).parentId, outer);
const nestedBefore = new Map(worldNodes(d.nodes).map((n) => [n.id, xy(n)]));
const copies = copyGroupedNodes(d, [outer, g, child.id]);
fitGroupFrames(d.nodes);
assert.equal(copies.length, 1);
const copied = descendantIds(d.nodes, copies);
assert.equal([...copied].length, 5);
assert.equal(
  d.edges.filter((e) => copied.has(e.from) && copied.has(e.to)).length,
  2,
);
assert.throws(() => connect(d, outer, "n-text"), /组内/);
removeNodes(d, copies);
assert.ok(d.nodes.some((n) => n.id === outer));
assert.equal(d.edges.length, 2);
ungroupNodes(d, [outer, g]);
fitGroupFrames(d.nodes);
for (const id of original.nodes.map((n) => n.id))
  assert.deepEqual(
    xy(pos(d, id)),
    nestedBefore.get(id),
    "嵌套解组保留世界坐标",
  );
assert.deepEqual(d.edges, original.edges);
assert.deepEqual(d.plans, original.plans);
const planGroup = groupNodes(d, ["n-plan", "n-text"]);
removeNodes(d, [planGroup]);
assert.ok(!d.nodes.some((n) => ["n-plan", "n-text", planGroup].includes(n.id)));
assert.deepEqual(d.plans, original.plans, "移除分组不能删除策划文档");
const invalid = [
  { id: "a", type: "group", parentId: "b", x: 0, y: 0 },
  { id: "b", type: "group", parentId: "a", x: 0, y: 0 },
];
assert.throws(() => worldNodes(invalid), /循环/);
console.log(
  "通过：父组相对坐标、成组/嵌套解组位置不变、整组移动、父子去重、子节点移动与边框补偿、整组复制连线、级联移除与独立策划保护。",
);
