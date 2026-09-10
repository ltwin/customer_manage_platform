import { normalizeTags } from "../v4/tags.mjs";
import { expiredItems, purgeAssets } from "../v4/recycle.mjs";

function freshState(photos) {
  const notes = [
    {
      id: "note-1",
      kind: "text",
      title: "关于光的一点念头",
      text: "让光停在脸的一侧，\n让故事留在另一侧。",
      category: "文字",
      tags: ["布光", "情绪"],
      width: 4,
      height: 4,
      note: "试试一盏灯，一块黑色背景。",
    },
    {
      id: "note-2",
      kind: "text",
      title: "拍摄前的小纸条",
      text: "不必让画面很满。\n风、光，和一个\n恰好的眼神。",
      category: "文字",
      tags: ["留白", "人像"],
      width: 4,
      height: 5,
      note: "",
    },
    {
      id: "note-3",
      kind: "text",
      title: "下一次，去海边",
      text: "日落前四十分钟。\n白裙，低机位，\n等海风吹过来。",
      category: "文字",
      tags: ["海边", "拍摄想法"],
      width: 4,
      height: 3.7,
      note: "",
    },
    {
      id: "link-1",
      kind: "link",
      title: "寻找光与建筑的关系",
      text: "去看建筑，\n也去看落在\n建筑里的光。",
      category: "链接",
      tags: ["建筑", "空间"],
      width: 4,
      height: 4,
      source: "https://unsplash.com/s/photos/tadao-ando",
      note: "",
    },
  ];
  const all = [
    ...photos.slice(0, 5),
    notes[0],
    ...photos.slice(5, 9),
    notes[1],
    photos[9],
    notes[3],
    photos[10],
    notes[2],
    photos[11],
  ];
  return {
    revision: 0,
    assets: all.map((a, i) => ({ ...a, created: Date.now() - i * 3600000 })),
    favorites: ["asset-1", "asset-3", "asset-9"],
    collections: [
      {
        id: "c-light",
        name: "光的形状",
        items: [
          "asset-1",
          "asset-3",
          "asset-5",
          "asset-6",
          "asset-8",
          "note-1",
        ],
      },
      {
        id: "c-sea",
        name: "把海风收藏",
        items: ["asset-4", "asset-7", "asset-10", "asset-11", "note-3"],
      },
      {
        id: "c-space",
        name: "空间里的秩序",
        items: ["asset-2", "asset-9", "asset-12", "link-1"],
      },
    ],
    projects: [
      {
        id: "p-sea",
        name: "九月，向海而行",
        items: ["asset-4", "asset-10", "asset-12", "note-3"],
      },
      {
        id: "p-mono",
        name: "黑白肖像习作",
        items: ["asset-1", "asset-5", "asset-6", "note-1"],
      },
    ],
  };
}
export function normalizeLibrary(raw) {
  const state = normalizeTags({
    ...raw,
    trash: raw.trash || [],
    projects: raw.projects || [],
  });
  for (const asset of [...state.assets, ...state.trash.map((t) => t.asset)]) {
    if (asset.src)
      asset.src = new URL(asset.src, new URL("../v4/", import.meta.url)).href;
  }
  return state;
}
export function groupMembers(state, scope) {
  if (scope === "trash") return state.trash.map((t) => t.asset);
  if (scope === "favorites")
    return state.assets.filter((a) => state.favorites.includes(a.id));
  if (scope === "unfiled")
    return state.assets.filter(
      (a) => !state.collections.some((g) => g.items.includes(a.id)),
    );
  if (scope === "recent")
    return state.assets
      .filter((a) => a.lastUsed)
      .sort((a, b) => b.lastUsed - a.lastUsed);
  const group = state.collections.find((g) => g.id === scope);
  return group
    ? state.assets.filter((a) => group.items.includes(a.id))
    : state.assets;
}
export function moveGroup(state, id, parentId, beforeId) {
  if (id === beforeId) return;
  const group = state.collections.find((g) => g.id === id);
  if (!group) throw new Error("分组已不存在。");
  let cursor = parentId;
  const seen = new Set();
  while (cursor) {
    if (cursor === id || seen.has(cursor))
      throw new Error("不能把分组放进自己或自己的子分组。");
    seen.add(cursor);
    const parent = state.collections.find((g) => g.id === cursor);
    if (!parent) throw new Error("目标分组已不存在。");
    cursor = parent.parentId;
  }
  group.parentId = parentId || null;
  state.collections = state.collections.filter((g) => g.id !== id);
  const index = state.collections.findIndex((g) => g.id === beforeId);
  state.collections.splice(
    index < 0 ? state.collections.length : index,
    0,
    group,
  );
}
export async function openAssetStore(photos, changed) {
  const db = await new Promise((resolve, reject) => {
    const req = indexedDB.open("creative-canvas-library-v5", 1);
    req.onupgradeneeded = () => req.result.createObjectStore("workspace");
    req.onsuccess = () => resolve(req.result);
    req.onerror = () =>
      reject(new Error("无法打开本地资产库，请允许浏览器存储后重试。"));
  });
  let state = await new Promise((resolve, reject) => {
    const req = db
      .transaction("workspace")
      .objectStore("workspace")
      .get("state");
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(new Error("资产库读取失败，请刷新重试。"));
  });
  const existed = Boolean(state);
  // 首次复制旧侧栏数据，之后独立演进；旧页面的定时清理不会覆盖新界面。
  if (!state) {
    state = await new Promise((resolve, reject) => {
      const req = indexedDB.open("creative-canvas-assets-v5", 1);
      req.onupgradeneeded = () => req.result.createObjectStore("workspace");
      req.onerror = () => reject(new Error("旧资产数据读取失败，请刷新重试。"));
      req.onsuccess = () => {
        const legacy = req.result,
          read = legacy
            .transaction("workspace")
            .objectStore("workspace")
            .get("state");
        read.onsuccess = () => {
          resolve(read.result);
          legacy.close();
        };
        read.onerror = () => {
          reject(new Error("旧资产数据读取失败，请刷新重试。"));
          legacy.close();
        };
      };
    });
  }
  state = normalizeLibrary(state || freshState(await photos()));
  let queue = Promise.resolve();
  function update(change) {
    const task = queue.then(async () => {
      let next = structuredClone(state);
      next = normalizeLibrary(change(next) || next);
      await new Promise((resolve, reject) => {
        const tx = db.transaction("workspace", "readwrite"),
          store = tx.objectStore("workspace");
        const req = store.get("state");
        let message = "保存失败，输入仍保留。请检查本地存储空间后重试。";
        req.onsuccess = () => {
          if (req.result && req.result.revision !== state.revision) {
            message = "另一个页面已更新资产库，请保留输入并刷新后重试。";
            tx.abort();
            return;
          }
          next.revision = (state.revision || 0) + 1;
          store.put(next, "state");
        };
        tx.oncomplete = resolve;
        tx.onerror = tx.onabort = () => reject(new Error(message));
      });
      state = next;
      changed(state);
    });
    queue = task.catch(() => {});
    return task;
  }
  const expired = expiredItems(state);
  if (!existed || expired.length)
    await update((s) =>
      purgeAssets(
        s,
        expired.map((t) => t.asset.id),
      ),
    );
  else changed(state);
  return { get: () => state, update };
}
