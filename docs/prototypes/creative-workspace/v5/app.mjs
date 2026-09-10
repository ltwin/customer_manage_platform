import { fitGroupFrames } from "./canvas-groups.mjs";
import {
  $,
  esc,
  icon,
  paintIcons,
  menu,
  dialog,
  closeDialog,
  notify,
  download,
  dialogHasChanges,
} from "./ui.mjs?connection-menu=1";
import { seed, openStore, ORDERS } from "./model.mjs";
import { createCanvas } from "./canvas.mjs?groups=1";
import { createPlanning } from "./planning.mjs";
import { createLibrary } from "./library.mjs?native-shelf=1";
import { readAssetFile } from "./asset-editors.mjs";
import { createAgent } from "./agent.mjs";
paintIcons();
let doc,
  store,
  canvas,
  planning,
  library,
  agent,
  failedDraft = null,
  chain = Promise.resolve();
const past = [],
  future = [];
let historyBusy = false;
let photoPromise,
  lastRouteKey = "",
  lastNodeId = "";
function readCamera() {
  try {
    const value = JSON.parse(sessionStorage.getItem("creative-canvas-v5-view"));
    return value && ["x", "y", "z"].every((key) => Number.isFinite(value[key]))
      ? value
      : null;
  } catch {
    return null;
  }
}
function saveCamera(value) {
  try {
    sessionStorage.setItem("creative-canvas-v5-view", JSON.stringify(value));
  } catch {
    /* 视口记忆失败不阻断内容保存。 */
  }
}
const photos = () =>
  (photoPromise ||= fetch("../v4/photos.json")
    .then((r) => {
      if (!r.ok) throw new Error("示例影像列表未能加载，请重试。");
      return r.json();
    })
    .catch((error) => {
      photoPromise = null;
      throw error;
    }));
const projection = (value) =>
  structuredClone({ nodes: value.nodes, edges: value.edges });
function route() {
  const p = new URLSearchParams(location.hash.slice(1)),
    mode = p.get("mode"),
    nodeId = p.get("node");
  const node = doc?.nodes.find((n) => n.id === nodeId);
  if (mode === "maximized")
    return {
      page: "plan",
      node: nodeId,
      plan: node?.planId || "",
      section: p.get("tab") || "shots",
    };
  return {
    page: ["live", "order", "plan"].includes(p.get("page"))
      ? p.get("page")
      : "canvas",
    plan: p.get("plan") || "",
    section: p.get("section") || "shots",
    order: p.get("order") || "",
  };
}
function navigate(value) {
  if (value.page === "plan") {
    const node =
      doc.nodes.find((n) => n.id === value.node) ||
      doc.nodes.find((n) => n.planId === value.plan);
    if (!node) {
      const id = crypto.randomUUID();
      mutate((next) => {
        next.nodes.push({
          id,
          type: "plan",
          planId: value.plan,
          x: 420,
          y: 200,
          width: 316,
          height: 356,
        });
      }).then(() => navigate({ ...value, node: id }));
      return;
    }
    canvas.select(node.id);
    value = { node: node.id, mode: "maximized", tab: value.section || "shots" };
  }
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(value))
    if (v && !(k === "page" && v === "canvas")) p.set(k, v);
  const hash = p.toString();
  if (location.hash.slice(1) !== hash) location.hash = hash;
  else routeChanged();
}
function routeChanged() {
  if (!doc) return;
  const next = route(),
    key = JSON.stringify(next),
    changed = key !== lastRouteKey;
  const scroll = changed
    ? 0
    : $("embedded-page").querySelector(".plan-scroll")?.scrollTop || 0;
  library.pageChanged(next.page);
  planning.render();
  const scroller = $("embedded-page").querySelector(".plan-scroll");
  if (scroller) scroller.scrollTop = scroll;
  document.title = `${next.page === "canvas" ? doc.title : next.page === "order" ? "演示订单" : doc.plans.find((p) => p.id === next.plan)?.title || "策划"} · 创意空间`;
  if (next.node) lastNodeId = next.node;
  if (changed && next.page === "canvas" && lastNodeId)
    requestAnimationFrame(() =>
      document
        .querySelector(`[data-node="${lastNodeId}"]`)
        ?.focus({ preventScroll: true }),
    );
  lastRouteKey = key;
}
function render() {
  if (!doc) return;
  $("project-name").innerHTML = `${esc(doc.title)}${icon("chevron-down")}`;
  $("undo").disabled = !past.length;
  $("redo").disabled = !future.length;
  canvas.render();
  agent.render();
  routeChanged();
}
function mutate(change, options = {}) {
  const task = chain.then(async () => {
    if (!doc || !store) throw new Error("画布尚未打开，请稍后重试。");
    if (failedDraft)
      throw new Error("请先处理当前的保存失败，再继续修改画布。");
    const next = structuredClone(doc);
    change(next);
    fitGroupFrames(next.nodes);
    const previous = projection(doc);
    $("save-status").textContent = "正在保存…";
    try {
      next.revision = await store.save(next, doc.revision);
      if (options.history !== false) {
        past.push(previous);
        if (past.length > 40) past.shift();
        future.length = 0;
      }
      doc = next;
      $("save-status").textContent = "已保存到本地";
      $("save-error").hidden = true;
      if (options.render !== false) render();
    } catch (error) {
      failedDraft = next;
      $("save-status").textContent = "尚未保存";
      $("save-error").hidden = false;
      $("save-error-text").textContent = error.message;
      throw error;
    }
  });
  chain = task.catch(() => {});
  task.catch((error) => {
    if (!failedDraft) notify(error.message);
  });
  return task;
}
async function historyMove(direction) {
  const source = direction === "undo" ? past : future,
    target = direction === "undo" ? future : past;
  if (!source.length || !doc || failedDraft || historyBusy) return;
  historyBusy = true;
  const snapshot = source[source.length - 1],
    current = projection(doc);
  try {
    await mutate(
      (next) => {
        next.nodes = structuredClone(snapshot.nodes);
        next.edges = structuredClone(snapshot.edges);
      },
      { history: false },
    );
    source.pop();
    target.push(current);
    render();
    notify(direction === "undo" ? "已撤销画布操作" : "已重做画布操作");
  } catch {
    /* 错误条保留恢复入口，历史栈不前移。 */
  } finally {
    historyBusy = false;
  }
}
async function readImage(file) {
  if (!["image/jpeg", "image/png", "image/webp"].includes(file.type))
    throw new Error("请选择 JPG、PNG 或 WebP 图片。");
  if (file.size > 5 * 1024 * 1024)
    throw new Error("图片超过 5 MB，请选择更小的图片。");
  const src = await readFile(file);
  const image = new Image();
  image.src = src;
  try {
    await image.decode();
  } catch {
    throw new Error("这张图片无法读取，请换一张图片。");
  }
  return {
    src,
    width: image.naturalWidth,
    height: image.naturalHeight,
    assetId: null,
  };
}
function readFile(file) {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(r.result);
    r.onerror = () => reject(new Error("文件读取失败，请重新选择。"));
    r.readAsDataURL(file);
  });
}
function pickMedia(node, save) {
  let draft = null,
    reading = false;
  const image = node.type === "image",
    type = image ? "图片" : node.type === "video" ? "视频" : "音频";
  dialog({
    title: `选择${type}`,
    description: image
      ? "从个人资产库选择图片，或上传本地参考。"
      : `${type}文件仅保存在此浏览器，最大 12 MB。生成能力暂未接入。`,
    wide: image,
    content: `<div id="node-media-preview" class="shot-editor-preview">${image && node.src ? `<img src="${esc(node.src)}" alt="当前图片">` : `<span>选择一份${type}参考</span>`}</div><input name="selected-media" id="selected-media" type="hidden" value="">${
      image
        ? `<div class="asset-picker" id="node-image-picker">${library
            .assets()
            .filter((a) => a.kind === "image")
            .map(
              (a) =>
                `<button type="button" data-pick-image="${a.id}" aria-label="选择图片：${esc(a.title)}" aria-pressed="false"><img src="${esc(a.src)}" alt="${esc(a.title)}" loading="lazy"><span>${esc(a.title)}</span></button>`,
            )
            .join("")}</div>`
        : ""
    }<label class="upload-control">${icon("upload")}上传${type} · ${image ? "JPG / PNG / WebP · 最大 5 MB" : node.type === "video" ? "MP4 / WebM · 最大 12 MB" : "MP3 / WAV / OGG · 最大 12 MB"}<input id="media-file" type="file" accept="${image ? "image/jpeg,image/png,image/webp" : node.type === "video" ? "video/mp4,video/webm" : "audio/mpeg,audio/wav,audio/ogg"}"></label>`,
    submit: `使用这份${type}`,
    onSubmit: async () => {
      if (reading) throw new Error("正在读取文件，请稍后再保存。");
      if (!draft) throw new Error(`请先选择${type}。`);
      await save(draft);
      notify(`${type}已替换`);
    },
  });
  if (image)
    $("node-image-picker").onclick = (e) => {
      const b = e.target.closest("[data-pick-image]");
      if (!b) return;
      const a = library.assets().find((a) => a.id === b.dataset.pickImage);
      draft = { src: a.src, assetId: a.id };
      $("selected-media").value = a.id;
      $("node-media-preview").innerHTML =
        `<img src="${esc(a.src)}" alt="${esc(a.title)}">`;
      document
        .querySelectorAll("[data-pick-image]")
        .forEach((el) => el.setAttribute("aria-pressed", String(el === b)));
    };
  $("media-file").onchange = async (e) => {
    const file = e.target.files[0];
    if (!file) return;
    reading = true;
    $("dialog-submit").disabled = true;
    try {
      if (image) draft = await readImage(file);
      else {
        const allowed =
          node.type === "video"
            ? ["video/mp4", "video/webm"]
            : ["audio/mpeg", "audio/wav", "audio/x-wav", "audio/ogg"];
        if (!allowed.includes(file.type))
          throw new Error("文件类型不支持，请选择提示中的格式。");
        if (file.size > 12 * 1024 * 1024)
          throw new Error("文件超过 12 MB，请选择更小的参考片段。");
        draft = { src: await readFile(file), title: file.name, assetId: null };
      }
      $("selected-media").value = file.name;
      $("node-media-preview").innerHTML = image
        ? `<img src="${esc(draft.src)}" alt="上传的参考图片">`
        : `<span>${esc(file.name)} · 已读取</span>`;
      $("dialog-error").textContent = "";
    } catch (error) {
      draft = null;
      $("dialog-error").textContent = error.message;
    } finally {
      reading = false;
      $("dialog-submit").disabled = false;
    }
  };
}
const base = {
  doc: () => doc,
  mutate,
  route,
  navigate,
  order: (id) => ORDERS.find((o) => o.id === id),
  assets: () => library?.assets() || [],
  readImage,
  photos,
  readCamera,
  saveCamera,
  locate: (id) => canvas.locate(id),
  select: (id) => canvas.select(id),
};
canvas = createCanvas({
  ...base,
  openPlan: (id, node) => planning.open(id, node),
  referencePlans: () => planning.existing(),
  reference: (ids) => agent.reference(ids),
  selectionChanged: (ids) => agent?.syncSelection(ids),
  pickMedia,
  saveAssets: (nodes) => library.saveNodes(nodes),
  dropAssets: (ids, at) => library.addAssets(ids, at),
  dropFiles: async (files, at) => {
    const assets = [];
    for (const file of [...files].slice(0, 50))
      assets.push(await readAssetFile(file, readImage));
    await canvas.addAssets(assets, at);
  },
});
library = createLibrary({
  ...base,
  closeAgent: () => agent?.open(false),
  onAssets: () => {},
  addAssets: (assets, at) => canvas.addAssets(assets, at),
});
planning = createPlanning(base);
agent = createAgent({
  ...base,
  closeLibrary: () => library.open(false),
  nodeTitle: (n) => canvas.title(n),
  selected: () => canvas.selected(),
  deselect: (id) => canvas.deselect(id),
});
window.addEventListener("hashchange", routeChanged);
$("undo").onclick = () => historyMove("undo");
$("redo").onclick = () => historyMove("redo");
$("project-name").onclick = () => {
  if (!doc) return;
  menu(
    $("project-name"),
    [
      { id: "rename", icon: "pencil", label: "重命名项目" },
      { id: "fit", icon: "scan", label: "查看整个画布" },
      { id: "plans", icon: "clapperboard", label: "引用已有策划" },
    ],
    (id) => {
      if (id === "rename") rename();
      if (id === "fit") {
        navigate({ page: "canvas" });
        canvas.fit();
      }
      if (id === "plans") planning.existing();
    },
  );
};
function rename() {
  dialog({
    title: "重命名创作项目",
    content: `<label for="project-title">项目名称</label><input id="project-title" name="title" value="${esc(doc.title)}" maxlength="80">`,
    onSubmit: async (data) => {
      const title = data.get("title").trim();
      if (!title) throw new Error("请填写项目名称。");
      await mutate(
        (next) => {
          next.title = title;
        },
        { history: false },
      );
      notify("项目名称已保存");
    },
  });
}
function exportLocal() {
  download(
    `${doc?.title || "创意空间"}-本地副本.json`,
    JSON.stringify(failedDraft || doc, null, 2),
  );
}
$("project-menu").onclick = () =>
  menu(
    $("project-menu"),
    [
      { id: "export", icon: "download", label: "导出画布本地副本" },
      { id: "help", icon: "circle-help", label: "原型说明" },
      { separator: true },
      {
        id: "share",
        icon: "external-link",
        label: "在线协作与分享",
        note: "暂未开发",
        disabled: true,
      },
    ],
    (id) => {
      if (id === "export") exportLocal();
      if (id === "help") about();
    },
  );
$("retry-save").onclick = async () => {
  if (!failedDraft) return;
  const next = failedDraft;
  $("retry-save").disabled = true;
  try {
    next.revision = await store.save(next, doc.revision);
    failedDraft = null;
    doc = next;
    $("save-error").hidden = true;
    $("save-status").textContent = "已保存到本地";
    render();
    notify("本地保存已恢复");
  } catch (error) {
    $("save-error-text").textContent = error.message;
  } finally {
    $("retry-save").disabled = false;
  }
};
$("export-recovery").onclick = exportLocal;
function about() {
  dialog({
    title: "一间夜色里的创作室",
    description: "创意空间 v5 · 画布交互原型",
    content: `<p>通用画布承载创作过程，资产库与 Agent 提供辅助，策划作为独立文档呈现在画布上。</p><p>可体验节点新建、拖拽、连线、缩放、撤销，个人资产管理、市场示例收藏、手动编辑策划及现场演示。</p><p>Agent 使用预置回复与示例策划。多模态 RAG、模型生成、3D 场景编辑、真实市场交易、CRM 数据连接暂未开发。</p><p>内容仅保存在当前浏览器。v4 原型与 v5 使用独立存储，不会互相修改。</p><p><a class="pill-button" href="README.md" target="_blank" rel="noopener">查看设计与验证说明 ${icon("arrow-up-right")}</a></p>`,
  });
}
$("about-open").onclick = about;
$("help-open").onclick = () =>
  dialog({
    title: "留在创作里的快捷操作",
    content: `<div class="shortcut-list">${[
      ["新建节点", "N / 双击空白"],
      ["所选节点打组", "⌘ / Ctrl + G"],
      ["解散所选分组", "⌘ / Ctrl + Shift + G"],
      ["整组移动", "拖动组标题栏"],
      ["框选节点", "左键拖动空白 · Shift 追加"],
      ["移动画布", "中键 / 空格 + 拖动 / H"],
      ["选择节点", "V / 单击"],
      ["多选节点", "Shift + 单击"],
      ["微调位置", "方向键 / Shift 快速移动"],
      ["缩放", "⌘ / Ctrl + 滚轮"],
      ["适应全部节点", "F"],
      ["撤销 / 重做", "⌘Z / ⌘⇧Z"],
      ["打开资产库", "L"],
      ["建立参考连线", "点击输出端口，再点输入端口"],
    ]
      .map(
        ([a, b]) =>
          `<div style="display:flex;justify-content:space-between;gap:15px;border-bottom:1px solid var(--line);padding:10px 0;font-size:11px"><span>${a}</span><span style="color:var(--muted)">${b}</span></div>`,
      )
      .join("")}</div>`,
  });
$("node-search").onclick = () => {
  dialog({
    title: "在画布里找一个念头",
    content: `<div class="search-field"><span>${icon("search")}</span><input id="find-node-input" type="search" placeholder="搜索节点名称或文字" aria-label="搜索节点"><button type="button" id="clear-node-search" class="icon-button" aria-label="清除节点搜索" hidden>${icon("x")}</button></div><div id="node-search-results"></div>`,
  });
  let composing = false;
  const update = () => {
    const q = $("find-node-input").value.trim().toLowerCase();
    const nodes = doc.nodes.filter((n) =>
      [canvas.title(n), n.text].join(" ").toLowerCase().includes(q),
    );
    $("clear-node-search").hidden = !q;
    $("node-search-results").innerHTML =
      nodes
        .map(
          (n) =>
            `<button type="button" class="order-plan-card" data-search-node="${n.id}"><span><strong>${esc(canvas.title(n))}</strong></span>${icon("scan")}</button>`,
        )
        .join("") ||
      '<div class="empty">没有匹配的节点，试试其他关键词。</div>';
  };
  $("find-node-input").oncompositionstart = () => {
    composing = true;
  };
  $("find-node-input").oncompositionend = () => {
    composing = false;
    update();
  };
  $("find-node-input").oninput = () => {
    if (!composing) update();
  };
  $("clear-node-search").onclick = () => {
    $("find-node-input").value = "";
    update();
    $("find-node-input").focus();
  };
  $("node-search-results").onclick = (e) => {
    const b = e.target.closest("[data-search-node]");
    if (b) {
      closeDialog(true);
      navigate({ page: "canvas" });
      requestAnimationFrame(() => canvas.locate(b.dataset.searchNode));
    }
  };
  update();
};
document.addEventListener("keydown", (e) => {
  if (
    e.isComposing ||
    e.target.closest(
      "input,textarea,select,[contenteditable=true],dialog,.library-panel",
    ) ||
    !$("menu").hidden
  )
    return;
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "z") {
    e.preventDefault();
    historyMove(e.shiftKey ? "redo" : "undo");
  }
});
window.addEventListener("beforeunload", (e) => {
  if (failedDraft || dialogHasChanges()) {
    e.preventDefault();
    e.returnValue = "";
  }
});
window.addEventListener("offline", () =>
  notify("当前离线。本地编辑仍可使用，尚未加载的图片需联网后重试。"),
);
async function boot() {
  $("page-error").hidden = true;
  try {
    store ||= await openStore();
    doc = await store.read();
    if (!doc) {
      doc = seed();
      doc.revision = await store.save(doc, 0);
    }
    $("save-status").textContent = "已保存到本地";
    render();
  } catch (error) {
    $("page-error-message").textContent = error.message;
    $("page-error").hidden = false;
    $("save-status").textContent = "打开失败";
  }
}
$("retry-boot").onclick = boot;
void boot();
