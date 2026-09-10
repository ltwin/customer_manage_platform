import {
  $,
  esc,
  icon,
  iconButton,
  dialog,
  notify,
  menu,
} from "./ui.mjs?connection-menu=1";
import { openAssetStore, groupMembers, moveGroup } from "./asset-store.mjs";
import { matchTagFilter, tagColor } from "../v4/tags.mjs";
import {
  trashAssets,
  restoreAssets,
  purgeAssets,
  setRetention,
  retentionDays,
} from "../v4/recycle.mjs";
import { createAssetEditors, readAssetFile } from "./asset-editors.mjs";
import { previewMedia, downloadMedia } from "./media-preview.mjs";
const TYPES = {
  all: "全部类型",
  image: "图片",
  text: "文字",
  link: "链接",
  video: "视频",
  audio: "音频",
};
const SYSTEM = [
  ["all", "全部资产", "images"],
  ["recent", "最近使用", "clock"],
  ["favorites", "我的最爱", "heart"],
  ["unfiled", "未归类", "folder"],
];
export function createAssetShelf(ctx) {
  let store,
    state,
    scope = "all",
    query = "",
    type = "all",
    mode = "all",
    order = "recent",
    selected = new Set(),
    tags = [],
    collapsed = new Set(),
    items = [],
    composing = false;
  const root = $("personal-panel");
  root.innerHTML = `<div class="asset-search-row"><label class="search-field">${icon("search")}<input id="asset-search" type="search" aria-label="搜索个人资产" placeholder="搜索名称、描述或标签"></label>${iconButton("plus", "导入或新增资产", 'id="asset-import" aria-haspopup="menu"')}</div>
  <div id="asset-suggestions" class="asset-suggestions" hidden></div><div class="asset-browser"><nav id="asset-groups" class="asset-groups" aria-label="资产分组"></nav><section class="asset-results"><div class="asset-scope-row"><button id="asset-scope" aria-expanded="false">${icon("folder")}<span>全部资产</span>${icon("chevron-down")}</button><div>${iconButton("tag", "标签筛选", 'id="asset-tag-toggle" aria-expanded="false"')}${iconButton("settings-2", "素材类型与排序", 'id="asset-filter" aria-haspopup="menu"')}</div></div>
  <div id="asset-tag-popover" class="asset-tag-popover" hidden><header><strong>标签筛选 <small id="asset-tag-live"></small></strong><button id="tag-mode">同时满足</button></header><input id="asset-tag-search" aria-label="查找筛选标签" placeholder="查找标签"><div id="asset-tag-options"></div></div>
  <div id="asset-conditions" class="asset-conditions"></div><div class="asset-result-line"><span id="asset-count" role="status" aria-live="polite">正在读取…</span><button id="asset-search-all" hidden>搜索全部资产</button><button id="asset-sort-label">最近入库</button></div>
  <div id="asset-import-queue" class="asset-import-queue" hidden></div><div id="asset-trash-info" class="asset-trash-info" hidden></div><div class="asset-scroll"><div id="asset-grid" class="asset-grid" tabindex="-1"></div></div><div id="asset-empty" class="asset-empty" hidden></div>
  <div id="asset-bulk" class="asset-bulk glass" hidden><span id="asset-selected-count"></span><button id="asset-bulk-add" title="添加到画布">${icon("plus")}画布</button><button id="asset-bulk-edit" title="批量归类与标签">${icon("folder")}整理</button>${iconButton("ellipsis", "所选资产更多操作", 'id="asset-bulk-more"')}${iconButton("x", "取消资产选择", 'id="asset-deselect"')}</div></section></div>
  <input type="file" id="asset-file-input" hidden multiple accept="image/jpeg,image/png,image/webp,video/mp4,video/webm,audio/mpeg,audio/wav,audio/ogg"><div id="asset-load-error" role="alert" hidden></div>`;
  const editors = createAssetEditors({
    state: () => state,
    update: (fn) => store.update(fn),
  });
  const safe = (fn) => async (e) => {
    try {
      await fn(e);
    } catch (error) {
      notify(error.message);
    }
  };
  const name = () =>
    SYSTEM.find((x) => x[0] === scope)?.[1] ||
    state?.collections.find((g) => g.id === scope)?.name ||
    (scope === "trash" ? "回收站" : "全部资产");
  const currentGroup = () => state?.collections.find((g) => g.id === scope);
  const isTrash = () => scope === "trash";
  function setScope(id) {
    scope = id;
    selected.clear();
    root.classList.remove("show-groups");
    $("asset-scope").setAttribute("aria-expanded", "false");
    render();
  }
  function renderGroups() {
    function row(id, label, glyph, depth = 0, group = null) {
      const children = state.collections.some((g) => g.parentId === id),
        count = groupMembers(state, id).length;
      return `<div class="asset-group-row ${id === scope ? "active" : ""}" ${group ? `draggable="true" data-group-drag="${esc(id)}"` : ""} style="--depth:${depth}">${group && children ? `<button class="group-disclosure" data-collapse="${esc(id)}" aria-label="${collapsed.has(id) ? "展开" : "收起"}${esc(label)}" aria-expanded="${!collapsed.has(id)}">${icon(collapsed.has(id) ? "chevron-right" : "chevron-down")}</button>` : ""}<button data-scope="${esc(id)}" ${id === scope ? 'aria-current="page"' : ""}>${icon(glyph)}<span>${esc(label)}</span><small>${count}</small></button>${group ? iconButton("ellipsis", `管理分组：${label}`, `data-group-menu="${esc(id)}"`) : ""}</div>`;
    }
    function tree(parent = null, depth = 0, seen = new Set()) {
      return state.collections
        .filter((g) => (g.parentId || null) === parent && !seen.has(g.id))
        .map((g) => {
          const next = new Set([...seen, g.id]);
          return (
            row(g.id, g.name, "folder", depth, g) +
            (collapsed.has(g.id) ? "" : tree(g.id, depth + 1, next))
          );
        })
        .join("");
    }
    $("asset-groups").innerHTML =
      SYSTEM.map(([id, n, i]) => row(id, n, i)).join("") +
      `<header class="asset-group-heading"><span>我的分组</span>${iconButton("plus", "新建资产分组", 'id="asset-group-new"')}</header><div id="asset-group-tree">${tree() || '<p class="asset-nav-hint">新建分组，整理可复用的参考。</p>'}</div><button id="asset-group-root" class="asset-nav-hint">${icon("arrow-left")}移到最外层</button><div class="asset-nav-footer"><button id="asset-manage-tags">${icon("tag")}标签管理</button><button data-scope="trash" ${scope === "trash" ? 'aria-current="page"' : ""}>${icon("trash-2")}回收站 <small>${state.trash.length}</small></button></div>`;
  }
  function renderTags() {
    const q = $("asset-tag-search").value.trim().toLowerCase();
    const pool = groupMembers(state, scope);
    $("asset-tag-options").innerHTML =
      [{ id: null, name: "未分类" }, ...state.tagGroups]
        .map((g) => {
          const options = state.tagCatalog.filter(
            (t) =>
              (t.groupId || null) === g.id && t.name.toLowerCase().includes(q),
          );
          return options.length
            ? `<section><small>${esc(g.name)}</small>${options.map((t) => `<button data-filter-tag="${esc(t.id)}" aria-pressed="${tags.includes(t.id)}"><i style="--tag-color:${tagColor(t.color)}"></i><span>${esc(t.name)}</span><small>${pool.filter((a) => (a.tagIds || []).includes(t.id)).length}</small>${tags.includes(t.id) ? icon("check") : ""}</button>`).join("")}</section>`
            : "";
        })
        .join("") || '<p class="asset-nav-hint">没有匹配的标签</p>';
    $("tag-mode").textContent = mode === "all" ? "同时满足" : "满足任一";
    $("asset-tag-live").textContent = `${items.length} 份`;
  }
  function suggestions() {
    const el = $("asset-suggestions");
    if (!query || document.activeElement !== $("asset-search")) {
      el.hidden = true;
      return;
    }
    const q = query.toLowerCase();
    el.innerHTML =
      state.tagCatalog
        .filter((t) => t.name.toLowerCase().includes(q))
        .slice(0, 4)
        .map(
          (t) =>
            `<button data-suggest-tag="${esc(t.id)}"><i style="--tag-color:${tagColor(t.color)}"></i><span>${esc(t.name)}</span><small>标签</small></button>`,
        )
        .join("") +
      state.collections
        .filter((g) => g.name.toLowerCase().includes(q))
        .slice(0, 3)
        .map(
          (g) =>
            `<button data-suggest-group="${esc(g.id)}">${icon("folder")}<span>${esc(g.name)}</span><small>分组</small></button>`,
        )
        .join("");
    el.hidden = !el.innerHTML;
  }
  function render() {
    if (!state) return;
    if (
      !SYSTEM.some((x) => x[0] === scope) &&
      scope !== "trash" &&
      !currentGroup()
    )
      scope = "all";
    tags = tags.filter((id) => state.tagCatalog.some((t) => t.id === id));
    items = groupMembers(state, scope).filter(
      (a) =>
        (type === "all" || a.kind === type) &&
        matchTagFilter(a, tags, mode) &&
        (!query ||
          [a.title, a.description, a.text, a.source, ...(a.tags || [])]
            .join(" ")
            .toLowerCase()
            .includes(query.toLowerCase())),
    );
    if (scope !== "recent")
      items.sort(
        order === "name"
          ? (a, b) => a.title.localeCompare(b.title, "zh-CN")
          : order === "old"
            ? (a, b) => (a.created || 0) - (b.created || 0)
            : (a, b) => (b.created || 0) - (a.created || 0),
      );
    const visible = new Set(items.map((a) => a.id));
    selected = new Set([...selected].filter((id) => visible.has(id)));
    renderGroups();
    renderTags();
    suggestions();
    $("asset-scope").innerHTML =
      icon(isTrash() ? "trash-2" : "folder") +
      `<span>${esc(name())}</span>` +
      icon("chevron-down");
    $("asset-search").placeholder =
      scope === "all" ? "搜索名称、描述或标签" : `在「${name()}」中搜索`;
    $("asset-count").textContent = `${items.length} 份资产`;
    $("asset-search-all").hidden =
      scope === "all" || isTrash() || !(query || tags.length || type !== "all");
    $("asset-sort-label").textContent =
      scope === "recent"
        ? "最近使用"
        : { recent: "最近入库", old: "最早入库", name: "名称排序" }[order];
    $("asset-conditions").innerHTML =
      tags
        .map((id) => {
          const t = state.tagCatalog.find((t) => t.id === id);
          return `<button data-remove-tag="${esc(id)}"><i style="--tag-color:${tagColor(t.color)}"></i>${esc(t.name)}${icon("x")}</button>`;
        })
        .join("") +
      (type !== "all"
        ? `<button id="asset-clear-type">${TYPES[type]}${icon("x")}</button>`
        : "") +
      (tags.length || type !== "all" || query
        ? '<button id="asset-clear-filters">清除</button>'
        : "");
    $("asset-tag-toggle").classList.toggle("has-filter", tags.length > 0);
    $("asset-grid").innerHTML = items
      .map(
        (a) =>
          `<article class="shelf-card ${selected.has(a.id) ? "selected" : ""}" data-asset="${esc(a.id)}" draggable="${!isTrash()}"><button class="shelf-card-content" data-select-asset="${esc(a.id)}" aria-label="选择资产：${esc(a.title)}" aria-pressed="${selected.has(a.id)}"><div class="shelf-thumbnail ${a.kind}" style="aspect-ratio:${a.width || 4}/${a.height || 4}">${a.kind === "image" ? `<img src="${esc(a.src)}" width="${a.width || 4}" height="${a.height || 4}" loading="lazy" decoding="async" alt="${esc(a.title)}" draggable="false">` : a.kind === "video" ? `<video src="${esc(a.src)}" preload="metadata" muted playsinline></video><span class="shelf-media-symbol">${icon("play")}</span>` : `<div class="shelf-note">${icon(a.kind === "audio" ? "audio-lines" : a.kind === "link" ? "link" : "file-text")}<span>${esc(a.text || a.description || a.title)}</span></div>`}<span class="shelf-kind">${TYPES[a.kind] || "素材"}</span></div><span class="shelf-card-title">${esc(a.title)}</span></button><div class="shelf-quick">${iconButton("maximize", `预览：${a.title}`, `data-preview="${esc(a.id)}"`)}${!isTrash() ? iconButton("plus", `添加到画布：${a.title}`, `data-add-asset="${esc(a.id)}"`) : ""}</div>${iconButton("ellipsis", `资产菜单：${a.title}`, `data-asset-menu="${esc(a.id)}"`)}</article>`,
      )
      .join("");
    // 图片失败保留卡片占位和可识别名称，避免布局坍塌。
    $("asset-grid")
      .querySelectorAll("img")
      .forEach(
        (img) =>
          (img.onerror = () => {
            img.style.opacity = "0";
            img.parentElement.classList.add("load-failed");
          }),
      );
    $("asset-empty").hidden = items.length > 0;
    $("asset-empty").innerHTML =
      `${icon("search")}<h3>${query || tags.length || type !== "all" ? "没有匹配的资产" : isTrash() ? "回收站是空的" : scope === "recent" ? "还没有使用记录" : "这里还没有资产"}</h3><p>${query || tags.length ? "试试移除标签，或扩大搜索范围。" : scope === "recent" ? "放到画布的资产会出现在这里。" : "导入文件，或从画布存入值得复用的内容。"}</p><button id="asset-empty-action" class="pill-button">${query || tags.length || type !== "all" ? "清除筛选" : "导入文件"}</button>`;
    $("asset-trash-info").hidden = !isTrash();
    $("asset-trash-info").innerHTML =
      `<span>${retentionDays(state) === null ? "不自动清理" : `保留 ${retentionDays(state)} 天，到期清理`} · 画布中的内容保留</span><button id="asset-retention">设置</button>`;
    updateSelection();
    layout();
  }
  function layout() {
    const grid = $("asset-grid"),
      width = grid.clientWidth;
    if (!width) return;
    const columns = width >= 420 ? 3 : width >= 220 ? 2 : 1,
      gap = 10,
      cardWidth = (width - gap * (columns - 1)) / columns,
      heights = Array(columns).fill(0);
    [...grid.children].forEach((card, index) => {
      const a = items[index];
      if (!a) return;
      const ratio =
        a.kind === "video"
          ? 10 / 16
          : a.kind === "audio"
            ? 3 / 4
            : (a.height || 4) / (a.width || 4);
      const height = Math.max(70, cardWidth * ratio) + 32;
      const col = heights.indexOf(Math.min(...heights));
      Object.assign(card.style, {
        width: `${cardWidth}px`,
        left: `${col * (cardWidth + gap)}px`,
        top: `${heights[col]}px`,
      });
      heights[col] += height + gap;
    });
    grid.style.height = `${Math.max(0, ...heights) + 70}px`;
  }
  new ResizeObserver(layout).observe($("asset-grid"));
  function updateSelection() {
    $("asset-bulk").hidden = !selected.size;
    $("asset-selected-count").textContent = `已选 ${selected.size}`;
    $("asset-bulk-add").hidden = isTrash();
    $("asset-bulk-edit").hidden = isTrash();
    $("asset-grid")
      .querySelectorAll("[data-asset]")
      .forEach((el) => {
        const yes = selected.has(el.dataset.asset);
        el.classList.toggle("selected", yes);
        el.querySelector("[data-select-asset]").setAttribute(
          "aria-pressed",
          String(yes),
        );
      });
  }
  async function add(ids, at) {
    const assets = ids
      .map((id) => state.assets.find((a) => a.id === id))
      .filter(Boolean);
    await ctx.addAssets(assets, at);
    try {
      await store.update((s) => {
        s.assets
          .filter((a) => ids.includes(a.id))
          .forEach((a) => (a.lastUsed = Date.now()));
      });
    } catch (e) {
      notify(`已添加到画布，但使用记录保存失败：${e.message}`);
    }
  }
  function preview(a) {
    if (["image", "video"].includes(a.kind)) {
      previewMedia({ ...a, type: a.kind }, a.title);
      return;
    }
    dialog({
      title: a.title,
      description: TYPES[a.kind],
      content:
        a.kind === "audio"
          ? `<audio src="${esc(a.src)}" controls style="width:100%"></audio>`
          : `<p class="shelf-text-preview">${esc(a.text || a.description)}</p>${a.kind === "link" && /^https?:\/\//i.test(a.source) ? `<a href="${esc(a.source)}" target="_blank" rel="noopener noreferrer">${esc(a.source)}</a>` : ""}`,
    });
  }
  function groupForm(group, parentId = null) {
    dialog({
      title: group ? "重命名分组" : "新建分组",
      description: parentId
        ? `创建在「${state.collections.find((g) => g.id === parentId)?.name}」中。`
        : "用分组整理可反复使用的参考。",
      content: `<label class="field-label">分组名称<input name="name" required maxlength="60" value="${esc(group?.name || "")}" placeholder="例如：人像 / 外景参考"></label>`,
      onSubmit: async (f) => {
        const n = String(f.get("name")).trim();
        if (!n) throw new Error("请填写分组名称。");
        await store.update((s) => {
          if (
            s.collections.some(
              (g) =>
                g.id !== group?.id &&
                (g.parentId || null) === (group?.parentId || parentId) &&
                g.name === n,
            )
          )
            throw new Error("同一层级已存在这个名称。");
          if (group) s.collections.find((g) => g.id === group.id).name = n;
          else
            s.collections.push({
              id: crypto.randomUUID(),
              name: n,
              parentId,
              items: [],
            });
        });
        notify("分组已保存");
      },
    });
  }
  function groupMenu(b, id) {
    const g = state.collections.find((g) => g.id === id);
    menu(
      b,
      [
        { id: "rename", icon: "pencil", label: "重命名" },
        { id: "child", icon: "folder", label: "新建子分组" },
        {
          id: "root",
          icon: "arrow-left",
          label: "移到最外层",
          disabled: !g.parentId,
        },
        { separator: true },
        {
          id: "delete",
          icon: "trash-2",
          label: "删除分组",
          note: "保留素材，子分组提升一层",
          danger: true,
        },
      ],
      async (action) => {
        if (action === "rename") groupForm(g);
        if (action === "child") groupForm(null, id);
        if (action === "root")
          await store.update((s) => moveGroup(s, id, null));
        if (action === "delete")
          dialog({
            title: `删除「${g.name}」？`,
            description:
              "只移除这个分组。素材继续保留，子分组提升到上一层；不再属于任何分组的素材进入未归类。",
            content: "",
            submit: "删除分组",
            onSubmit: async () => {
              await store.update((s) => {
                s.collections = s.collections.filter((x) => x.id !== id);
                s.collections.forEach((x) => {
                  if (x.parentId === id) x.parentId = g.parentId || null;
                });
              });
              notify("分组已删除，素材已保留");
            },
          });
      },
    );
  }
  function assetMenu(b, ids) {
    const a = items.find((a) => a.id === ids[0]);
    if (!a) return;
    const trash = isTrash();
    const list = trash
      ? [
          { id: "restore", icon: "undo-2", label: "恢复素材" },
          { id: "purge", icon: "trash-2", label: "彻底删除…", danger: true },
        ]
      : [
          { id: "add", icon: "plus", label: "添加到画布" },
          {
            id: "edit",
            icon: "tag",
            label: ids.length > 1 ? "批量归类与标签" : "编辑 / 分组 / 标签",
          },
          {
            id: "favorite",
            icon: "heart",
            label: ids.every((id) => state.favorites.includes(id))
              ? "移出我的最爱"
              : "加入我的最爱",
          },
          ...(currentGroup()
            ? [{ id: "remove", icon: "folder", label: "从当前分组移除" }]
            : []),
          ...(ids.length === 1 && a.src
            ? [{ id: "download", icon: "download", label: "下载" }]
            : []),
          { separator: true },
          { id: "trash", icon: "trash-2", label: "移入回收站", danger: true },
        ];
    menu(b, list, async (action) => {
      if (action === "add") await add(ids);
      if (action === "edit")
        editors.edit(state.assets.filter((a) => ids.includes(a.id)));
      if (action === "download")
        await downloadMedia({ ...a, type: a.kind }, a.title);
      if (action === "favorite")
        await store.update((s) => {
          const remove = ids.every((id) => s.favorites.includes(id));
          s.favorites = remove
            ? s.favorites.filter((id) => !ids.includes(id))
            : [...new Set([...s.favorites, ...ids])];
        });
      if (action === "remove") {
        const groupId = scope;
        await store.update((s) => {
          const g = s.collections.find((g) => g.id === groupId);
          g.items = g.items.filter((id) => !ids.includes(id));
        });
        notify("已移出分组，素材仍在资产库");
      }
      if (action === "trash") {
        await store.update((s) => trashAssets(s, ids));
        notify("已移入回收站，可恢复；画布中的内容保留");
      }
      if (action === "restore") {
        await store.update((s) => restoreAssets(s, ids));
        notify("素材及仍存在的归类已恢复");
      }
      if (action === "purge")
        dialog({
          title: `彻底删除 ${ids.length} 份素材？`,
          description: "无法恢复。已经放入画布的内容快照保留。",
          content: "",
          submit: "彻底删除",
          onSubmit: async () => {
            await store.update((s) => purgeAssets(s, ids));
            notify("素材已彻底删除");
          },
        });
    });
  }
  function filters(b) {
    menu(
      b,
      [
        ...Object.entries(TYPES).map(([id, n]) => ({
          id,
          icon:
            id === type
              ? "check"
              : id === "all"
                ? "images"
                : id === "text"
                  ? "file-text"
                  : id === "audio"
                    ? "audio-lines"
                    : id,
          label: n,
        })),
        { separator: true },
        ...Object.entries({
          recent: "最近入库",
          old: "最早入库",
          name: "名称排序",
        }).map(([id, n]) => ({
          id: `sort:${id}`,
          icon: order === id ? "check" : "settings-2",
          label: n,
        })),
      ],
      (id) => {
        if (id.startsWith("sort:")) order = id.slice(5);
        else type = id;
        render();
      },
    );
  }
  const imports = [];
  function renderImports() {
    const el = $("asset-import-queue");
    el.hidden = !imports.length;
    el.innerHTML =
      `<header><span>导入文件</span><button id="asset-import-dismiss" ${imports.some((x) => x.busy) ? "disabled" : ""}>收起</button></header>` +
      imports
        .map(
          (x, i) =>
            `<div><span>${esc(x.file.name)}</span><small>${esc(x.status)}</small>${x.failed ? `<button data-retry-import="${i}">重试</button>` : ""}</div>`,
        )
        .join("");
  }
  async function importOne(job) {
    job.busy = true;
    job.failed = false;
    job.status = "读取中…";
    renderImports();
    try {
      const media = await readAssetFile(job.file, ctx.readImage);
      job.status = "保存中…";
      renderImports();
      await store.update((s) => {
        const id = crypto.randomUUID();
        s.assets.unshift({
          ...media,
          id,
          tagIds: [],
          tags: [],
          created: Date.now(),
        });
        s.collections.find((g) => g.id === job.group)?.items.push(id);
      });
      job.status = "已入库";
    } catch (e) {
      job.failed = true;
      job.status = e.message;
    } finally {
      job.busy = false;
      renderImports();
    }
  }
  async function importFiles(files) {
    if (!store) return;
    const batch = [...files].slice(0, 50).map((file) => ({
      file,
      group: currentGroup()?.id,
      status: "等待中",
      busy: true,
    }));
    imports.push(...batch);
    renderImports();
    for (const job of batch) await importOne(job);
    const failed = batch.filter((job) => job.failed).length;
    notify(
      failed
        ? `已入库 ${batch.length - failed} 份，${failed} 份失败，可逐项重试。`
        : `已入库 ${batch.length} 份，可补充分组和标签。`,
    );
  }
  $("asset-search").onfocus = () => {
    if (state) suggestions();
  };
  $("asset-search").oncompositionstart = () => (composing = true);
  $("asset-search").oncompositionend = () => {
    composing = false;
    query = $("asset-search").value.trim();
    render();
  };
  $("asset-search").oninput = () => {
    if (!composing) {
      query = $("asset-search").value.trim();
      render();
    }
  };
  $("asset-scope").onclick = () => {
    const open = root.classList.toggle("show-groups");
    $("asset-scope").setAttribute("aria-expanded", String(open));
  };
  $("asset-tag-toggle").onclick = () => {
    if (!state) return;
    const el = $("asset-tag-popover");
    el.hidden = !el.hidden;
    $("asset-tag-toggle").setAttribute("aria-expanded", String(!el.hidden));
    if (!el.hidden) {
      renderTags();
      $("asset-tag-search").focus();
    }
  };
  $("asset-tag-search").oninput = renderTags;
  $("tag-mode").onclick = () => {
    mode = mode === "all" ? "any" : "all";
    render();
  };
  $("asset-filter").onclick = (e) => filters(e.currentTarget);
  $("asset-sort-label").onclick = (e) => filters(e.currentTarget);
  $("asset-import").onclick = (e) =>
    menu(
      e.currentTarget,
      [
        {
          id: "file",
          icon: "upload",
          label: "导入文件",
          note: "图片、视频、音频",
        },
        { id: "text", icon: "file-text", label: "新建文字" },
        { id: "link", icon: "link", label: "保存链接" },
      ],
      (id) => {
        if (id === "file") $("asset-file-input").click();
        else editors.newAsset(id, currentGroup()?.id);
      },
    );
  $("asset-file-input").onchange = (e) => {
    importFiles(e.target.files);
    e.target.value = "";
  };
  function clearFilters() {
    tags = [];
    type = "all";
    query = "";
    $("asset-search").value = "";
    render();
  }
  root.addEventListener(
    "click",
    safe(async (e) => {
      const b = e.target.closest("button");
      if (!b || !state) return;
      if (b.dataset.suggestTag || b.dataset.suggestGroup) {
        query = "";
        $("asset-search").value = "";
        $("asset-suggestions").hidden = true;
        if (b.dataset.suggestTag) {
          tags = [...new Set([...tags, b.dataset.suggestTag])];
          render();
        } else setScope(b.dataset.suggestGroup);
        $("asset-search").focus();
      }
      if (b.dataset.scope) setScope(b.dataset.scope);
      if (b.dataset.collapse) {
        collapsed.has(b.dataset.collapse)
          ? collapsed.delete(b.dataset.collapse)
          : collapsed.add(b.dataset.collapse);
        renderGroups();
      }
      if (b.dataset.groupMenu) groupMenu(b, b.dataset.groupMenu);
      if (b.id === "asset-group-new") groupForm();
      if (b.id === "asset-manage-tags") editors.manageTags();
      if (b.dataset.filterTag) {
        tags = tags.includes(b.dataset.filterTag)
          ? tags.filter((t) => t !== b.dataset.filterTag)
          : [...tags, b.dataset.filterTag];
        render();
      }
      if (b.dataset.removeTag) {
        tags = tags.filter((t) => t !== b.dataset.removeTag);
        render();
      }
      if (b.id === "asset-clear-type") {
        type = "all";
        render();
      }
      if (b.id === "asset-clear-filters") clearFilters();
      if (b.id === "asset-search-all") setScope("all");
      if (b.id === "asset-empty-action") {
        if (query || tags.length || type !== "all") clearFilters();
        else $("asset-file-input").click();
      }
      if (b.dataset.selectAsset) {
        const id = b.dataset.selectAsset;
        if (e.shiftKey || e.metaKey || e.ctrlKey) {
          selected.has(id) ? selected.delete(id) : selected.add(id);
        } else
          selected =
            selected.has(id) && selected.size === 1 ? new Set() : new Set([id]);
        updateSelection();
      }
      if (b.dataset.addAsset) await add([b.dataset.addAsset]);
      if (b.dataset.preview)
        preview(items.find((a) => a.id === b.dataset.preview));
      if (b.dataset.assetMenu)
        assetMenu(
          b,
          selected.has(b.dataset.assetMenu)
            ? [...selected]
            : [b.dataset.assetMenu],
        );
      if (b.id === "asset-deselect") {
        selected.clear();
        updateSelection();
      }
      if (b.id === "asset-bulk-add") await add([...selected]);
      if (b.id === "asset-bulk-edit")
        editors.edit(state.assets.filter((a) => selected.has(a.id)));
      if (b.id === "asset-bulk-more") assetMenu(b, [...selected]);
      if (b.dataset.retryImport !== undefined)
        await importOne(imports[Number(b.dataset.retryImport)]);
      if (b.id === "asset-import-dismiss") {
        imports.length = 0;
        renderImports();
      }
      if (b.id === "asset-retention")
        menu(
          b,
          [7, 30, 90, null].map((d) => ({
            id: String(d),
            icon: retentionDays(state) === d ? "check" : "clock",
            label: d === null ? "不自动清理" : `保留 ${d} 天`,
          })),
          (id) => {
            const days = id === "null" ? null : Number(id);
            dialog({
              title: "更改回收站保留时长？",
              description:
                "已经到期的素材会立即彻底删除。原型在打开资产库时检查到期内容。",
              content: "",
              submit: "保存设置",
              onSubmit: () => store.update((s) => setRetention(s, days)),
            });
          },
        );
    }),
  );
  root.addEventListener("keydown", (e) => {
    if (e.target.closest("input,textarea,select") || !$("menu").hidden) return;
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "a") {
      e.preventDefault();
      e.stopPropagation();
      selected = new Set(items.map((a) => a.id));
      updateSelection();
      return;
    }
    if (e.code === "Space" && selected.size === 1) {
      e.preventDefault();
      e.stopPropagation();
      preview(items.find((a) => selected.has(a.id)));
    }
    if (e.key === "Escape") {
      $("asset-tag-popover").hidden = true;
      $("asset-suggestions").hidden = true;
      root.classList.remove("show-groups");
      $("asset-tag-toggle").setAttribute("aria-expanded", "false");
      $("asset-scope").setAttribute("aria-expanded", "false");
      selected.clear();
      updateSelection();
      e.stopPropagation();
    }
  });
  document.addEventListener("pointerdown", (e) => {
    if (!e.target.closest("#asset-search,#asset-suggestions"))
      $("asset-suggestions").hidden = true;
    if (!e.target.closest("#asset-tag-popover,#asset-tag-toggle")) {
      $("asset-tag-popover").hidden = true;
      $("asset-tag-toggle").setAttribute("aria-expanded", "false");
    }
    if (!root.contains(e.target)) {
      root.classList.remove("show-groups");
      $("asset-scope").setAttribute("aria-expanded", "false");
    }
  });
  root.addEventListener("dragend", () => {
    root.classList.remove("group-dragging");
    root
      .querySelectorAll(".drop-target")
      .forEach((el) => el.classList.remove("drop-target"));
  });
  root.addEventListener("dragstart", (e) => {
    const g = e.target.closest("[data-group-drag]"),
      a = e.target.closest("[data-asset]");
    if (g) {
      e.dataTransfer.setData(
        "application/x-creative-group",
        g.dataset.groupDrag,
      );
      e.dataTransfer.effectAllowed = "move";
      root.classList.add("group-dragging");
    } else if (a && !isTrash()) {
      const ids = selected.has(a.dataset.asset)
        ? [...selected]
        : [a.dataset.asset];
      e.dataTransfer.setData(
        "application/x-creative-asset",
        JSON.stringify({ ids }),
      );
      e.dataTransfer.effectAllowed = "copy";
    }
  });
  root.addEventListener("dragover", (e) => {
    const target = e.target.closest("[data-group-drag],#asset-group-root");
    if (e.dataTransfer.types.includes("Files") || target) {
      e.preventDefault();
      e.dataTransfer.dropEffect = e.dataTransfer.types.includes(
        "application/x-creative-group",
      )
        ? "move"
        : "copy";
      root
        .querySelectorAll(".drop-target")
        .forEach((el) => el.classList.remove("drop-target"));
      target?.classList.add("drop-target");
    }
  });
  root.addEventListener("dragleave", (e) => {
    if (!root.contains(e.relatedTarget))
      root
        .querySelectorAll(".drop-target")
        .forEach((el) => el.classList.remove("drop-target"));
  });
  root.addEventListener(
    "drop",
    safe(async (e) => {
      e.preventDefault();
      root
        .querySelectorAll(".drop-target")
        .forEach((el) => el.classList.remove("drop-target"));
      if (e.dataTransfer.files.length) {
        await importFiles(e.dataTransfer.files);
        return;
      }
      const target = e.target.closest("[data-group-drag],#asset-group-root");
      if (!target) return;
      const groupId = target.dataset.groupDrag || null,
        moving = e.dataTransfer.getData("application/x-creative-group");
      if (moving) {
        const before =
          groupId && e.clientY - target.getBoundingClientRect().top < 10;
        await store.update((s) =>
          moveGroup(
            s,
            moving,
            before
              ? s.collections.find((g) => g.id === groupId).parentId
              : groupId,
            before ? groupId : null,
          ),
        );
        notify("分组位置已更新");
      } else if (groupId) {
        const raw = e.dataTransfer.getData("application/x-creative-asset");
        if (!raw) return;
        const payload = JSON.parse(raw),
          ids = payload.ids || [payload.id];
        await store.update((s) => {
          const g = s.collections.find((g) => g.id === groupId);
          g.items = [
            ...new Set([
              ...g.items,
              ...ids.filter((id) => s.assets.some((a) => a.id === id)),
            ]),
          ];
        });
        notify("已加入分组，原有归类保留");
      }
    }),
  );
  const ready = openAssetStore(ctx.photos, (next) => {
    state = next;
    ctx.changed(next.assets);
    render();
  })
    .then((value) => (store = value))
    .catch((e) => {
      $("asset-load-error").hidden = false;
      $("asset-load-error").textContent = e.message;
    });
  return {
    assets: () => state?.assets || [],
    ready,
    add,
    importFiles,
    collect: async (asset) => {
      await ready;
      if (!store) throw new Error("资产库尚未就绪。");
      await store.update((s) => {
        if (!s.assets.some((a) => a.id === asset.id))
          s.assets.unshift({ ...asset, created: Date.now() });
      });
    },
    saveNodes: (nodes) => {
      if (!store) return notify("资产库尚未就绪。");
      const supported = nodes.filter(
        (n) =>
          ["image", "text", "video", "audio"].includes(n.type) &&
          (n.src || n.text),
      );
      if (!supported.length)
        return notify("请选择包含内容的图片、文字、视频或音频节点。");
      const seen = new Set();
      const assets = supported
        .map(
          (n) =>
            state.assets.find((a) => a.id === n.assetId) || {
              id: crypto.randomUUID(),
              kind: n.type,
              title: n.title,
              text: n.text,
              src: n.src,
              width: n.type === "image" ? n.width : 4,
              height: n.type === "image" ? Math.max(1, n.height - 74) : 4,
              tagIds: [],
              sourceNodeId: n.id,
            },
        )
        .filter((a) => !seen.has(a.id) && seen.add(a.id));
      editors.edit(assets, { fresh: true, groupId: currentGroup()?.id });
    },
  };
}
