import assert from "node:assert/strict";
import { groupMembers, moveGroup, normalizeLibrary } from "./asset-store.mjs";
import { trashAssets, restoreAssets } from "../v4/recycle.mjs";
import { saveTag, matchTagFilter } from "../v4/tags.mjs";
let state = normalizeLibrary({
  revision: 0,
  assets: [
    { id: "a", title: "海风", kind: "text", tags: ["海边", "逆光"] },
    { id: "b", title: "另一份", kind: "text", tags: ["海边"] },
  ],
  favorites: ["a"],
  collections: [
    { id: "p", name: "外景", items: ["a"] },
    { id: "q", name: "人像", items: ["a"] },
    { id: "c", name: "海边", parentId: "p", items: [] },
  ],
  projects: [],
});
assert.deepEqual(
  groupMembers(state, "unfiled").map((a) => a.id),
  ["b"],
  "多分组引用不复制素材，收藏不等于归类",
);
const before = structuredClone(state);
assert.throws(() => moveGroup(state, "p", "c"), /自己/);
assert.deepEqual(state, before, "拒绝循环时不能改变树");
moveGroup(state, "c", null, "p");
assert.equal(state.collections[0].id, "c");
assert.equal(state.collections[0].parentId, null);
const removed = trashAssets(state, ["a"]);
assert.equal(removed.assets.length, 1);
assert.equal(
  removed.collections.some((g) => g.items.includes("a")),
  false,
);
const restored = restoreAssets(removed, ["a"]);
assert.equal(restored.assets.length, 2);
assert.equal(
  restored.collections.filter((g) => g.items.includes("a")).length,
  2,
);
assert.deepEqual(restored.favorites, ["a"]);
const tag = state.tagCatalog.find((t) => t.name === "逆光");
state = saveTag(state, { ...tag, name: "夕阳逆光", color: "amber" });
assert.ok(state.assets[0].tags.includes("夕阳逆光"));
assert.ok(matchTagFilter(state.assets[0], [tag.id]));
assert.equal(matchTagFilter(state.assets[1], [tag.id]), false);
console.log(
  "通过：分组防环与排序、多重归类、未归类语义、回收站恢复归类、稳定标签 ID 与检索。",
);
