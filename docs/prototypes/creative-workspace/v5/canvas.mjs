import {
  worldNodes,
  selectionRoots,
  descendantIds,
  groupNodes,
  ungroupNodes,
  copyGroupedNodes,
  fitGroupFrames,
} from "./canvas-groups.mjs";
import { createMagneticPorts } from "./magnetic-ports.mjs?fixed-anchors=1";
import { previewMedia, downloadMedia } from "./media-preview.mjs";
import {
  $,
  esc,
  icon,
  iconButton,
  menu,
  closeMenu,
  notify,
  dialog,
} from "./ui.mjs?connection-menu=1";
import {
  TYPES,
  bounds,
  referenceLabel,
  connect,
  removeNodes,
  newPlan,
} from "./model.mjs";
export const typeIcon = (type) =>
  ({
    text: "file-text",
    image: "image",
    video: "video",
    audio: "audio-lines",
    scene: "box",
    plan: "clapperboard",
    group: "layers",
  })[type] || "image";
export function createCanvas(ctx) {
  const selected = new Set();
  let camera = { x: 0, y: 0, z: 1 },
    initialized = false,
    tool = "select",
    gesture,
    space = false,
    suppressClick = false,
    cameraTimer,
    activePreview = null;
  const title = (n) =>
    n.type === "plan"
      ? ctx.doc().plans.find((p) => p.id === n.planId)?.title || "策划已不可用"
      : n.title;
  const point = (event) => {
    const r = $("canvas").getBoundingClientRect();
    return {
      x: (event.clientX - r.left - camera.x) / camera.z,
      y: (event.clientY - r.top - camera.y) / camera.z,
    };
  };
  const magnets = createMagneticPorts(
    $("canvas"),
    () => camera.z,
    () => Boolean(gesture) || !$("menu").hidden,
  );
  function nodeAnchor(id, side) {
    const node = document.querySelector(`[data-node="${id}"]`);
    if (!node) return null;
    const rect = node.getBoundingClientRect();
    return {
      clientX: side === "input" ? rect.left : rect.right,
      clientY: rect.top + rect.height / 2,
    };
  }
  function render() {
    const doc = ctx.doc();
    if (!doc) return;
    for (const id of selected)
      if (!doc.nodes.some((n) => n.id === id)) selected.delete(id);
    magnets.reset();
    $("nodes").innerHTML = worldNodes(doc.nodes)
      .sort(
        (a, b) =>
          (a.type === "group" ? 0 : 1) - (b.type === "group" ? 0 : 1) ||
          (a.type === "group"
            ? descendantIds(doc.nodes, [b.id]).size -
              descendantIds(doc.nodes, [a.id]).size
            : 0),
      )
      .map((n) => {
        if (n.type === "group")
          return `<article class="canvas-node canvas-group ${selected.has(n.id) ? "selected" : ""}" data-node="${n.id}" data-type="group" tabindex="0" aria-label="分组：${esc(n.title)}" style="transform:translate(${n.x}px,${n.y}px);width:${n.width}px;height:${n.height}px"><header class="node-header group-header">${icon("layers")}<strong>${esc(n.title)}</strong><small>${doc.nodes.filter((child) => child.parentId === n.id).length} 个子节点</small>${iconButton("ellipsis", "分组操作：" + n.title, `data-node-menu="${n.id}" aria-haspopup="menu"`)}</header></article>`;
        const plan = doc.plans.find((p) => p.id === n.planId);
        let content = "";
        if (n.type === "text")
          content = `<div class="node-text">${esc(n.text || "写下一点想法…")}</div>`;
        else if (n.type === "image" && n.src)
          content = `<img class="node-image" src="${esc(n.src)}" alt="${esc(title(n))}" draggable="false"><div class="node-footer">${icon("image")}<span>${n.assetId ? "资产库参考" : "图片节点"}</span><span>IMAGE</span></div>`;
        else if (n.type === "video" && n.src)
          content = `<video class="node-video" src="${esc(n.src)}" controls preload="metadata"></video><div class="node-footer"><span>本地视频</span><span>VIDEO</span></div>`;
        else if (n.type === "audio" && n.src)
          content = `<audio class="node-audio" src="${esc(n.src)}" controls preload="metadata"></audio><div class="node-footer"><span>本地音频</span><span>AUDIO</span></div>`;
        else if (n.type === "plan" && plan)
          content = `<div class="node-plan-cover">${plan.shots.find((s) => s.image) ? `<img src="${esc(plan.shots.find((s) => s.image).image)}" alt="" draggable="false">` : ""}<span>${icon("clapperboard")}SHOOTING PLAN</span></div><div class="node-plan-body"><h3>${esc(plan.title)}</h3><p>${esc(plan.brief || "从创作意图开始，逐步补充分镜、场地、布光与准备事项。")}</p><div class="node-plan-meta"><span>${plan.shots.length} 个分镜</span><span>工作版本 v${plan.version}</span><span>${plan.publication ? (plan.publication.version === plan.version ? "已发布" : "有未发布修改") : "未发布"}</span></div><div class="node-plan-actions"><button class="pill-button primary" data-maximize-node="${n.id}">展开策划 ${icon("arrow-up-right")}</button><button class="pill-button" data-live-plan="${plan.id}">${icon("play")}现场模式</button></div>${plan.publication ? `<button class="plan-order-link" data-order="${plan.publication.orderId}" data-plan="${plan.id}">${icon("link")}已关联演示订单 · ${esc(ctx.order(plan.publication.orderId)?.number)} ${icon("external-link")}</button>` : ""}</div>`;
        else
          content = `<div class="node-placeholder">${icon(typeIcon(n.type))}<p>${n.type === "scene" ? "在空间里推敲机位与灯光" : `放入一份${TYPES[n.type]}参考`}</p>${n.type === "scene" ? '<button class="pill-button" disabled>3D 场景编辑 · 暂未开发</button>' : `<button class="pill-button" data-fill-node="${n.id}">${icon("upload")}选择${TYPES[n.type]}</button>`}<small>${n.type === "scene" ? "未来可展开专用导演台" : "生成能力暂未接入"}</small></div>`;
        return `<article class="canvas-node ${selected.has(n.id) ? "selected" : ""}" data-node="${n.id}" data-type="${n.type}" tabindex="0" aria-label="${esc(TYPES[n.type] + "节点：" + title(n))}" style="transform:translate(${n.x}px,${n.y}px);width:${n.width}px;height:${n.height}px"><header class="node-header">${icon(typeIcon(n.type))}<strong>${esc(n.type === "plan" ? "拍摄策划" : title(n))}</strong>${n.type === "plan" ? iconButton("maximize", "最大化拍摄策划：" + title(n), `data-maximize-node="${n.id}"`) : ""}${iconButton("ellipsis", "节点操作：" + title(n), `data-node-menu="${n.id}" aria-haspopup="menu"`)}</header><div class="node-content">${content}</div>${n.type === "plan" ? "" : `<button class="node-port input" data-input="${n.id}" aria-label="添加上游参考：${esc(title(n))}" title="添加参考 · 点击或拖动" aria-haspopup="menu">${icon("plus")}</button><button class="node-port output" data-output="${n.id}" aria-label="添加下游节点：${esc(title(n))}" title="继续创作 · 点击或拖动" aria-haspopup="menu">${icon("plus")}</button>`}</article>`;
      })
      .join("");
    drawEdges(doc.nodes);
    $("canvas-empty").hidden = doc.nodes.length > 0;
    $("node-count").textContent =
      `${doc.nodes.filter((n) => n.type !== "group").length} 个节点${doc.nodes.some((n) => n.type === "group") ? ` · ${doc.nodes.filter((n) => n.type === "group").length} 个分组` : ""} · ${doc.edges.length} 条连线`;
    if (!initialized) {
      initialized = true;
      if (ctx.readCamera() || doc.camera) {
        camera = { ...(ctx.readCamera() || doc.camera) };
        transform();
      } else requestAnimationFrame(fit);
    } else transform();
    syncSelection();
  }
  function drawEdges(positions) {
    positions = worldNodes(positions);
    const doc = ctx.doc();
    $("edges").innerHTML = doc.edges
      .map((e) => {
        const from = positions.find((n) => n.id === e.from),
          to = positions.find((n) => n.id === e.to);
        if (!from || !to) return "";
        const x1 = from.x + from.width,
          y1 = from.y + from.height / 2,
          x2 = to.x,
          y2 = to.y + to.height / 2,
          bend = Math.max(55, Math.abs(x2 - x1) * 0.45);
        const d = `M${x1} ${y1} C${x1 + bend} ${y1},${x2 - bend} ${y2},${x2} ${y2}`;
        return `<g class="edge-group" data-from="${e.from}" data-to="${e.to}"><path class="edge-path" d="${d}"/><path class="edge-flow" d="${d}" pathLength="100" aria-hidden="true"/><path class="edge-hit" d="${d}" data-edge="${e.id}" role="button" tabindex="0" aria-label="${esc(title(from) + " → " + title(to))}的${referenceLabel(from, to)}，打开连线操作"/><text class="edge-label" x="${(x1 + x2) / 2}" y="${(y1 + y2) / 2 - 9}" text-anchor="middle">${referenceLabel(from, to)}</text></g>`;
      })
      .join("");
    syncEdgeSelection();
  }
  function syncEdgeSelection() {
    const referenced = descendantIds(ctx.doc().nodes, selected);
    $("edges")
      .querySelectorAll(".edge-group")
      .forEach((group) => {
        group.classList.toggle(
          "is-related",
          referenced.has(group.dataset.from) ||
            referenced.has(group.dataset.to),
        );
      });
  }
  function transform(save = false) {
    $("world").style.transform =
      `translate(${camera.x}px,${camera.y}px) scale(${camera.z})`;
    $("canvas").style.backgroundPosition = `${camera.x}px ${camera.y}px`;
    $("canvas").style.backgroundSize = `${22 * camera.z}px ${22 * camera.z}px`;
    $("zoom-level").textContent = `${Math.round(camera.z * 100)}%`;
    drawMap();
    drawEdges(ctx.doc().nodes);
    if (activePreview)
      drawConnection(activePreview.reference, activePreview.end);
    positionTools();
    if (save) {
      clearTimeout(cameraTimer);
      cameraTimer = setTimeout(() => ctx.saveCamera(camera), 250);
    }
  }
  function fit() {
    magnets.release();
    const b = bounds(worldNodes(ctx.doc().nodes)),
      w = $("canvas").clientWidth,
      h = $("canvas").clientHeight;
    camera.z = Math.max(
      0.22,
      Math.min(1, (w - 68) / b.width, (h - 170) / b.height),
    );
    camera.x = (w - b.width * camera.z) / 2 - b.x * camera.z;
    camera.y = 65 + (h - 170 - b.height * camera.z) / 2 - b.y * camera.z;
    transform(true);
  }
  function zoom(z, screen) {
    magnets.release();
    const w = $("canvas").clientWidth,
      h = $("canvas").clientHeight,
      x = screen?.x ?? w / 2,
      y = screen?.y ?? h / 2,
      old = camera.z;
    camera.z = Math.max(0.2, Math.min(2, z));
    camera.x = x - ((x - camera.x) * camera.z) / old;
    camera.y = y - ((y - camera.y) * camera.z) / old;
    transform(true);
  }
  function drawMap() {
    const doc = ctx.doc();
    if (!doc || $("minimap").hidden) return;
    const positions = worldNodes(doc.nodes);
    const b = bounds(positions),
      width = $("minimap").clientWidth,
      height = $("minimap").clientHeight,
      s = Math.min((width - 12) / b.width, (height - 12) / b.height),
      dx = (width - b.width * s) / 2,
      dy = (height - b.height * s) / 2;
    $("minimap").innerHTML =
      positions
        .map(
          (n) =>
            `<button class="map-node ${n.type === "plan" ? "plan" : ""}" data-locate="${n.id}" aria-label="定位：${esc(title(n))}" title="${esc(title(n))}" style="left:${dx + (n.x - b.x) * s}px;top:${dy + (n.y - b.y) * s}px;width:${n.width * s}px;height:${n.height * s}px"></button>`,
        )
        .join("") +
      `<span class="map-view" style="left:${dx + (-camera.x / camera.z - b.x) * s}px;top:${dy + (-camera.y / camera.z - b.y) * s}px;width:${($("canvas").clientWidth / camera.z) * s}px;height:${($("canvas").clientHeight / camera.z) * s}px"></span>`;
  }
  function positionTools(positions = ctx.doc()?.nodes || []) {
    const nodes = worldNodes(positions).filter((n) => selected.has(n.id));
    const bar = $("selection-tools");
    if (!nodes.length) return;
    const b = bounds(nodes),
      surface = $("canvas");
    const width = bar.offsetWidth;
    const center = camera.x + (b.x + b.width / 2) * camera.z;
    bar.style.left =
      Math.max(
        8,
        Math.min(center - width / 2, surface.clientWidth - width - 8),
      ) + "px";
    bar.style.top =
      Math.max(8, camera.y + b.y * camera.z - bar.offsetHeight - 12) + "px";
  }
  function syncSelection() {
    document
      .querySelectorAll(".canvas-node")
      .forEach((el) =>
        el.classList.toggle("selected", selected.has(el.dataset.node)),
      );
    $("selection-tools").hidden = !selected.size;
    $("selection-label").textContent =
      selected.size === 1
        ? title(ctx.doc().nodes.find((n) => n.id === [...selected][0]))
        : `已选 ${selected.size} 个节点`;
    const node =
      selected.size === 1
        ? ctx.doc().nodes.find((n) => selected.has(n.id))
        : null;
    $("selection-group").hidden =
      selectionRoots(ctx.doc().nodes, selected).length < 2;
    $("selection-ungroup").hidden = !ctx
      .doc()
      .nodes.some((n) => selected.has(n.id) && n.type === "group");
    $("selection-delete").setAttribute(
      "aria-label",
      ctx.doc().nodes.some((n) => selected.has(n.id) && n.type === "group")
        ? "移除分组及其子节点"
        : "移除所选节点",
    );
    $("selection-preview").hidden =
      !node?.src || !["image", "video"].includes(node.type);
    $("selection-download").hidden =
      !node?.src || !["image", "video", "audio"].includes(node.type);
    $("selection-save-asset").hidden = !ctx
      .doc()
      .nodes.some(
        (n) =>
          selected.has(n.id) &&
          ["image", "text", "video", "audio"].includes(n.type) &&
          (n.src || n.text),
      );
    positionTools();
    syncEdgeSelection();
    ctx.selectionChanged([...selected]);
  }
  function select(id, additive = false) {
    if (!additive) selected.clear();
    if (additive && selected.has(id)) selected.delete(id);
    else selected.add(id);
    syncSelection();
  }
  function locate(id) {
    const n = worldNodes(ctx.doc().nodes).find((n) => n.id === id);
    if (!n) return;
    camera.z = Math.min(
      1,
      ($("canvas").clientWidth - 50) / n.width,
      ($("canvas").clientHeight - 160) / n.height,
    );
    camera.x = $("canvas").clientWidth / 2 - (n.x + n.width / 2) * camera.z;
    camera.y = $("canvas").clientHeight / 2 - (n.y + n.height / 2) * camera.z;
    transform(true);
    select(id);
    $("canvas").focus({ preventScroll: true });
  }
  function addMenu(anchor = $("add-node"), at) {
    menu(
      anchor,
      [
        ...Object.entries(TYPES)
          .filter(([type]) => type !== "group")
          .map(([id, label]) => ({
            id,
            label,
            icon: typeIcon(id),
            note:
              id === "scene"
                ? "节点预览 · 编辑器暂未开发"
                : id === "plan"
                  ? "独立策划文档的摘要入口"
                  : "",
            key: id === "text" ? "T" : undefined,
          })),
        { separator: true },
        {
          id: "existing-plan",
          icon: "file-check",
          label: "引用已有策划",
          note: "重新放入已保留的策划文档",
        },
      ],
      (type) =>
        type === "existing-plan" ? ctx.referencePlans() : add(type, at),
      at ? { x: at.clientX, y: at.clientY } : undefined,
    );
  }
  async function add(type, at, asset, reference) {
    const p = at
      ? point(at)
      : {
          x: ($("canvas").clientWidth / 2 - camera.x) / camera.z - 130,
          y: ($("canvas").clientHeight / 2 - camera.y) / camera.z - 125,
        };
    const id = crypto.randomUUID();
    await ctx.mutate((doc) => {
      const node = {
        id,
        type,
        title: asset?.title || `新的${TYPES[type]}`,
        x: p.x,
        y: p.y,
        width: type === "plan" ? 316 : 266,
        height: type === "text" ? 210 : type === "plan" ? 356 : 320,
      };
      if (type === "text") {
        node.text = asset?.text || "";
      }
      if (asset) {
        node.src = asset.src;
        node.assetId = asset.id;
        if (asset.width && asset.height && type === "image")
          node.height = Math.min(
            500,
            (node.width * asset.height) / asset.width + 74,
          );
      }
      if (type === "plan") {
        const plan = newPlan();
        doc.plans.push(plan);
        node.planId = plan.id;
      }
      if (reference) {
        node.x = reference.side === "input" ? p.x - node.width - 20 : p.x + 20;
        node.y = p.y - node.height / 2;
      }
      doc.nodes.push(node);
      if (reference)
        connect(
          doc,
          reference.side === "input" ? id : reference.id,
          reference.side === "input" ? reference.id : id,
        );
    });
    select(id);
    notify(`已添加${TYPES[type]}节点`);
    if (!asset && type === "text") edit(id);
    return id;
  }
  async function addAssets(assets, at) {
    if (!assets.length) return;
    const origin = at
      ? point(at)
      : {
          x: ($("canvas").clientWidth / 2 - camera.x) / camera.z - 133,
          y: ($("canvas").clientHeight / 2 - camera.y) / camera.z - 160,
        };
    const nodes = assets.map((asset, index) => {
      const type = asset.kind === "link" ? "text" : asset.kind;
      return {
        id: crypto.randomUUID(),
        type,
        title: asset.title,
        text:
          asset.kind === "link"
            ? [asset.source, asset.text || asset.description]
                .filter(Boolean)
                .join("\n\n")
            : asset.text || "",
        src: asset.src,
        assetId: asset.id || null,
        x: origin.x + (index % 3) * 300,
        y: origin.y + Math.floor(index / 3) * 540,
        width: 266,
        height:
          type === "image" && asset.width && asset.height
            ? Math.min(500, (266 * asset.height) / asset.width + 74)
            : type === "text"
              ? 210
              : 320,
      };
    });
    await ctx.mutate((doc) => {
      doc.nodes.push(...nodes);
    });
    selected.clear();
    nodes.forEach((n) => selected.add(n.id));
    syncSelection();
    notify(`已放入画布 · ${nodes.length} 份素材`);
  }
  function edit(id) {
    const n = ctx.doc().nodes.find((n) => n.id === id);
    if (!n) return;
    if (n.type === "group") {
      renameNode(n.id);
      return;
    }
    if (n.type === "plan") {
      ctx.openPlan(n.planId, n.id);
      return;
    }
    if (n.type === "scene") {
      notify("3D 导演台的场景编辑暂未开发。当前可以移动、复制与连接节点。");
      return;
    }
    if (n.type !== "text") {
      ctx.pickMedia(n, async (media) =>
        ctx.mutate((doc) => {
          Object.assign(
            doc.nodes.find((item) => item.id === id),
            media,
          );
        }),
      );
      return;
    }
    dialog({
      title: "编辑文字节点",
      content: `<label for="node-title">标题</label><input id="node-title" name="title" value="${esc(n.title)}" maxlength="80"><label for="node-text">正文</label><textarea id="node-text" name="text" rows="7" maxlength="10000">${esc(n.text)}</textarea>`,
      submit: "保存文字",
      onSubmit: async (data) => {
        if (!data.get("title").trim()) throw new Error("请填写节点标题。");
        await ctx.mutate((doc) => {
          const node = doc.nodes.find((item) => item.id === id);
          node.title = data.get("title").trim();
          node.text = data.get("text");
        });
        notify("文字已保存");
      },
    });
  }
  function nodeMenu(id, anchor) {
    if (!selected.has(id)) select(id);
    const n = ctx.doc().nodes.find((n) => n.id === id);
    menu(
      anchor,
      [
        {
          id: "edit",
          icon: n.type === "plan" ? "external-link" : "pencil",
          label:
            n.type === "plan"
              ? "最大化策划节点"
              : n.type === "image"
                ? "替换图片"
                : "编辑节点",
        },
        {
          id: "rename",
          icon: "file-text",
          label: "重命名",
          disabled: n.type === "plan",
        },
        ...(selectionRoots(ctx.doc().nodes, selected).length >= 2
          ? [
              {
                id: "group",
                icon: "layers",
                label: "将所选节点打组",
                key: "⌘G",
              },
            ]
          : []),
        ...(ctx
          .doc()
          .nodes.some((n) => selected.has(n.id) && n.type === "group")
          ? [
              {
                id: "ungroup",
                icon: "unlink",
                label: "解散分组",
                note: "保留子节点的位置与连线",
                key: "⌘⇧G",
              },
            ]
          : []),
        { id: "agent", icon: "sparkles", label: "打开 Agent" },
        {
          id: "copy",
          icon: "copy",
          label: "复制节点",
          note: n.type === "plan" ? "引用同一份策划" : "",
        },
        { separator: true },
        {
          id: "delete",
          icon: "trash-2",
          label: n.type === "group" ? "移除整组与子节点" : "移除节点",
          danger: true,
          note: n.type === "plan" ? "保留策划文档" : "保留资产库原素材",
        },
      ],
      (action) => {
        if (action === "edit") edit(id);
        if (action === "agent") ctx.reference([...selected]);
        if (action === "copy") copySelected();
        if (action === "delete") removeSelected();
        if (action === "group") groupSelected();
        if (action === "ungroup") ungroupSelected();
        if (action === "rename") renameNode(id);
      },
    );
  }
  function renameNode(id) {
    const n = ctx.doc().nodes.find((n) => n.id === id);
    if (!n) return;
    dialog({
      title: n.type === "group" ? "重命名分组" : "重命名节点",
      content: `<label for="rename-node">名称</label><input id="rename-node" name="title" value="${esc(n.title)}" maxlength="80">`,
      onSubmit: async (data) => {
        const name = data.get("title").trim();
        if (!name) throw new Error("请填写名称。");
        await ctx.mutate((doc) => {
          doc.nodes.find((n) => n.id === id).title = name;
        });
      },
    });
  }
  async function groupSelected() {
    try {
      let id;
      const ids = [...selected];
      await ctx.mutate((doc) => {
        id = groupNodes(doc, ids);
      });
      select(id);
      notify("已打组 · 拖动组标题移动全部子节点");
    } catch (error) {
      notify(error.message);
    }
  }
  async function ungroupSelected() {
    const ids = [...selected];
    if (!ctx.doc().nodes.some((n) => ids.includes(n.id) && n.type === "group"))
      return;
    try {
      let children;
      await ctx.mutate((doc) => {
        children = ungroupNodes(doc, ids);
      });
      selected.clear();
      children.forEach((id) => selected.add(id));
      syncSelection();
      notify("已解组，子节点位置与连线保留");
    } catch (error) {
      notify(error.message);
    }
  }
  async function removeSelected() {
    const ids = [...selected];
    if (!ids.length) return;
    await ctx.mutate((doc) => removeNodes(doc, ids));
    selected.clear();
    syncSelection();
    notify(`已移除 ${ids.length} 个节点，可撤销。素材与策划仍保留。`);
    $("canvas").focus();
  }
  async function copySelected() {
    const ids = [...selected];
    let copies;
    await ctx.mutate((doc) => {
      copies = copyGroupedNodes(doc, ids);
    });
    selected.clear();
    copies.forEach((id) => selected.add(id));
    syncSelection();
    notify("已复制节点与组内连线");
  }
  function setTool(value) {
    tool = value;
    $("canvas").dataset.tool = value;
    $("select-tool").setAttribute("aria-pressed", String(value === "select"));
    $("pan-tool").setAttribute("aria-pressed", String(value === "pan"));
  }
  function clearConnection() {
    activePreview = null;
    magnets.release();
    $("connection-preview").innerHTML = "";
    $("canvas").classList.remove("linking");
    document
      .querySelectorAll(".connecting")
      .forEach((el) => el.classList.remove("connecting"));
  }
  function drawConnection(reference, end) {
    const center = nodeAnchor(reference.id, reference.side);
    if (!center) return;
    activePreview = {
      reference,
      end: { clientX: end.clientX, clientY: end.clientY },
    };
    const sign = reference.side === "input" ? -1 : 1;
    const rect = $("connection-preview").getBoundingClientRect();
    const x = center.clientX - rect.left,
      y = center.clientY - rect.top;
    const ex = end.clientX - rect.left,
      ey = end.clientY - rect.top;
    $("connection-preview").innerHTML =
      `<path d="M${x} ${y} C${x + 60 * sign} ${y},${ex - 60 * sign} ${ey},${ex} ${ey}"/>`;
  }
  function connectedMenu(reference, end, anchor) {
    const n = ctx.doc().nodes.find((n) => n.id === reference.id);
    if (!n) return clearConnection();
    if (!end) {
      const center = nodeAnchor(reference.id, reference.side);
      if (!center) return clearConnection();
      end = {
        clientX:
          center.clientX + (reference.side === "input" ? -150 : 150) * camera.z,
        clientY: center.clientY,
      };
    }
    const position = { clientX: end.clientX, clientY: end.clientY };
    const world = point(position);
    menu(
      anchor,
      Object.entries(TYPES)
        .filter(([type]) => type !== "plan" && type !== "group")
        .map(([id, label]) => ({
          id,
          label,
          icon: typeIcon(id),
          note:
            reference.side === "input"
              ? "添加为当前节点的参考"
              : "使用当前节点作为参考",
        })),
      (type) => {
        // Convert the frozen endpoint using the current camera when the menu commits.
        const rect = $("canvas").getBoundingClientRect();
        return add(
          type,
          {
            clientX: rect.left + camera.x + world.x * camera.z,
            clientY: rect.top + camera.y + world.y * camera.z,
          },
          null,
          reference,
        );
      },
      { x: position.clientX, y: position.clientY },
      clearConnection,
    );
    $("canvas").classList.add("linking");
    document
      .querySelector(`[data-${reference.side}="${reference.id}"]`)
      ?.classList.add("connecting");
    drawConnection(reference, position);
  }
  async function finishConnection(reference, targetId) {
    clearConnection();
    try {
      await ctx.mutate((doc) =>
        connect(
          doc,
          reference.side === "input" ? targetId : reference.id,
          reference.side === "input" ? reference.id : targetId,
        ),
      );
      notify("已建立参考连接");
    } catch (error) {
      notify(error.message);
    }
  }
  function edgeMenu(id, target) {
    menu(
      target,
      [{ id: "remove", icon: "unlink", label: "移除参考连线" }],
      async () => {
        await ctx.mutate((doc) => {
          doc.edges = doc.edges.filter((e) => e.id !== id);
        });
        notify("连线已移除，可撤销");
      },
    );
  }
  $("canvas").addEventListener("click", (event) => {
    if (suppressClick) {
      suppressClick = false;
      return;
    }
    const target = event.target;
    const port = target.closest("[data-output],[data-input]");
    if (port) {
      connectedMenu(
        {
          id: port.dataset.output || port.dataset.input,
          side: port.dataset.output ? "output" : "input",
        },
        null,
        port,
      );
      return;
    }
    const edge = target.closest("[data-edge]");
    if (edge) {
      edgeMenu(edge.dataset.edge, edge);
      return;
    }
    const more = target.closest("[data-node-menu]");
    if (more) {
      nodeMenu(more.dataset.nodeMenu, more);
      return;
    }
    const fill = target.closest("[data-fill-node]");
    if (fill) {
      edit(fill.dataset.fillNode);
      return;
    }
    if (
      target.closest(
        "[data-maximize-node],[data-open-plan],[data-live-plan],[data-order],video,audio,.canvas-bottom,.selection-tools",
      )
    )
      return;
    const node = target.closest("[data-node]");
    if (node)
      select(
        node.dataset.node,
        event.shiftKey || event.metaKey || event.ctrlKey,
      );
  });
  $("canvas").addEventListener("dblclick", (event) => {
    if (event.target.closest("button,video,audio,.canvas-bottom")) return;
    const node = event.target.closest("[data-node]");
    if (node) edit(node.dataset.node);
    else addMenu(null, event);
  });
  $("canvas").addEventListener("contextmenu", (event) => {
    if (event.target.closest("video,audio")) return;
    event.preventDefault();
    const node = event.target.closest("[data-node]");
    if (node) nodeMenu(node.dataset.node, node);
    else addMenu(null, event);
  });
  $("canvas").addEventListener("pointerdown", (event) => {
    if (event.button !== 0 && event.button !== 1) return;
    const port = event.target.closest("[data-output],[data-input]");
    if (port && event.button === 0 && !space && tool === "select") {
      closeMenu();
      const reference = {
        id: port.dataset.output || port.dataset.input,
        side: port.dataset.output ? "output" : "input",
      };
      gesture = {
        kind: "connect",
        startX: event.clientX,
        startY: event.clientY,
        reference,
        anchor: port,
      };
      port.classList.add("connecting");
      $("canvas").classList.add("linking");
      event.preventDefault();
      $("canvas").setPointerCapture(event.pointerId);
      return;
    }
    const panIntent = space || tool === "pan" || event.button === 1;
    if (
      event.target.closest(".canvas-bottom,.selection-tools") ||
      (!panIntent &&
        event.target.closest("button,a,input,textarea,video,audio,[data-edge]"))
    )
      return;
    closeMenu();
    const node = event.target.closest("[data-node]");
    if (node && !space && tool === "select" && event.button === 0) {
      const initialSelection = [...selected];
      if (!selected.has(node.dataset.node))
        select(
          node.dataset.node,
          event.shiftKey || event.metaKey || event.ctrlKey,
        );
      const p = point(event);
      gesture = {
        kind: "node",
        nodeId: node.dataset.node,
        initialSelection,
        additive: event.shiftKey || event.metaKey || event.ctrlKey,
        startX: event.clientX,
        startY: event.clientY,
        p,
        original: selectionRoots(ctx.doc().nodes, selected).map((n) => ({
          id: n.id,
          x: n.x,
          y: n.y,
        })),
      };
      node.classList.add("dragging");
    } else if (!panIntent) {
      gesture = {
        kind: "marquee",
        startX: event.clientX,
        startY: event.clientY,
        p: point(event),
        originalSelection: [...selected],
        additive: event.shiftKey || event.metaKey || event.ctrlKey,
      };
      if (!gesture.additive) selected.clear();
      syncSelection();
    } else {
      gesture = {
        kind: "pan",
        startX: event.clientX,
        startY: event.clientY,
        x: camera.x,
        y: camera.y,
      };
      $("canvas").classList.add("panning");
    }
    event.preventDefault();
    $("canvas").setPointerCapture(event.pointerId);
  });
  window.addEventListener("pointermove", (event) => {
    if (!gesture) return;
    const dx = event.clientX - gesture.startX,
      dy = event.clientY - gesture.startY;
    if (Math.hypot(dx, dy) < 3) return;
    gesture.moved = true;
    if (gesture.kind === "pan") {
      camera.x = gesture.x + dx;
      camera.y = gesture.y + dy;
      transform();
    }
    if (gesture.kind === "marquee") {
      const end = point(event),
        start = gesture.p;
      const x = Math.min(start.x, end.x),
        y = Math.min(start.y, end.y);
      const right = Math.max(start.x, end.x),
        bottom = Math.max(start.y, end.y);
      const box = $("selection-box");
      box.hidden = false;
      Object.assign(box.style, {
        left: camera.x + x * camera.z + "px",
        top: camera.y + y * camera.z + "px",
        width: (right - x) * camera.z + "px",
        height: (bottom - y) * camera.z + "px",
      });
      selected.clear();
      if (gesture.additive)
        gesture.originalSelection.forEach((id) => selected.add(id));
      worldNodes(ctx.doc().nodes)
        .filter((n) =>
          n.type === "group"
            ? n.x >= x &&
              n.y >= y &&
              n.x + n.width <= right &&
              n.y + n.height <= bottom
            : n.x < right &&
              n.x + n.width > x &&
              n.y < bottom &&
              n.y + n.height > y,
        )
        .forEach((n) => selected.add(n.id));
      syncSelection();
    }
    if (gesture.kind === "node") {
      const moved = ctx.doc().nodes.map((n) => {
        const origin = gesture.original.find((x) => x.id === n.id);
        return origin
          ? { ...n, x: origin.x + dx / camera.z, y: origin.y + dy / camera.z }
          : { ...n };
      });
      fitGroupFrames(moved);
      positionTools(moved);
      for (const n of worldNodes(moved)) {
        const el = document.querySelector(`[data-node="${n.id}"]`);
        Object.assign(el.style, {
          transform: `translate(${n.x}px,${n.y}px)`,
          width: `${n.width}px`,
          height: `${n.height}px`,
        });
      }
      drawEdges(moved);
    }
    if (gesture.kind === "connect") drawConnection(gesture.reference, event);
  });
  window.addEventListener("pointerup", async (event) => {
    if (!gesture) return;
    const g = gesture;
    gesture = null;
    $("selection-box").hidden = true;
    if ($("canvas").hasPointerCapture(event.pointerId))
      $("canvas").releasePointerCapture(event.pointerId);
    $("canvas").classList.remove("panning");
    if (g.kind === "connect") {
      suppressClick = true;
      setTimeout(() => {
        suppressClick = false;
      }, 0);
      if (!g.moved) connectedMenu(g.reference, null, g.anchor);
      else {
        const hit = document.elementFromPoint(event.clientX, event.clientY);
        const target = hit?.closest("[data-node]");
        if (target) await finishConnection(g.reference, target.dataset.node);
        else if (
          hit?.closest("#canvas") &&
          !hit.closest(".canvas-bottom,.selection-tools")
        )
          connectedMenu(g.reference, event, g.anchor);
        else clearConnection();
      }
      return;
    }
    if (!g.moved) {
      if (g.kind === "node") {
        selected.clear();
        g.initialSelection.forEach((id) => selected.add(id));
        select(g.nodeId, g.additive);
        document
          .querySelectorAll(".dragging")
          .forEach((el) => el.classList.remove("dragging"));
        // Pointer capture retargets the click; the completed gesture owns selection.
        suppressClick = true;
        setTimeout(() => {
          suppressClick = false;
        }, 0);
      }
      return;
    }
    suppressClick = true;
    setTimeout(() => {
      suppressClick = false;
    }, 0);
    if (g.kind === "pan") transform(true);
    if (g.kind === "node") {
      try {
        await ctx.mutate((doc) => {
          for (const old of g.original) {
            const n = doc.nodes.find((n) => n.id === old.id);
            if (n) {
              n.x = Math.round(old.x + (event.clientX - g.startX) / camera.z);
              n.y = Math.round(old.y + (event.clientY - g.startY) / camera.z);
            }
          }
        });
      } catch {
        render();
      }
    }
  });
  window.addEventListener("pointercancel", () => {
    gesture = null;
    clearConnection();
    $("selection-box").hidden = true;
    $("connection-preview").innerHTML = "";
    $("canvas").classList.remove("panning");
    render();
  });
  $("canvas").addEventListener(
    "wheel",
    (event) => {
      if (event.target.closest("video,audio,.node-text,.minimap")) return;
      event.preventDefault();
      if (event.ctrlKey || event.metaKey) {
        const r = $("canvas").getBoundingClientRect();
        zoom(camera.z * Math.exp(-event.deltaY * 0.007), {
          x: event.clientX - r.left,
          y: event.clientY - r.top,
        });
      } else {
        camera.x -= event.deltaX;
        camera.y -= event.deltaY;
        transform(true);
      }
    },
    { passive: false },
  );
  $("canvas").addEventListener("dragover", (event) => {
    if (
      event.dataTransfer.types.includes("application/x-creative-asset") ||
      event.dataTransfer.types.includes("Files")
    ) {
      event.preventDefault();
      event.dataTransfer.dropEffect = "copy";
    }
  });
  $("canvas").addEventListener("drop", async (event) => {
    const raw = event.dataTransfer.getData("application/x-creative-asset");
    if (!raw && !event.dataTransfer.files.length) return;
    event.preventDefault();
    const at = { clientX: event.clientX, clientY: event.clientY };
    try {
      if (event.dataTransfer.files.length)
        await ctx.dropFiles(event.dataTransfer.files, at);
      else {
        const data = JSON.parse(raw);
        await ctx.dropAssets(data.ids || [data.id], at);
      }
    } catch (error) {
      notify(error.message || "素材未能放入画布，请重试。");
    }
  });
  $("minimap").addEventListener("click", (event) => {
    const target = event.target.closest("[data-locate]");
    if (target) locate(target.dataset.locate);
  });
  $("minimap-toggle").addEventListener("click", () => {
    $("minimap").hidden = !$("minimap").hidden;
    $("minimap-toggle").setAttribute(
      "aria-pressed",
      String(!$("minimap").hidden),
    );
    drawMap();
  });
  $("add-node").addEventListener("click", () => addMenu());
  $("empty-add").addEventListener("click", () => addMenu($("empty-add")));
  $("zoom-in").addEventListener("click", () => zoom(camera.z * 1.2));
  $("zoom-out").addEventListener("click", () => zoom(camera.z / 1.2));
  $("zoom-level").addEventListener("click", () => zoom(1));
  $("fit-view").addEventListener("click", fit);
  $("select-tool").addEventListener("click", () => setTool("select"));
  $("pan-tool").addEventListener("click", () => setTool("pan"));
  $("selection-agent").addEventListener("click", () =>
    ctx.reference([...selected]),
  );
  $("selection-preview").addEventListener("click", () => {
    const node =
      selected.size === 1
        ? ctx.doc().nodes.find((n) => selected.has(n.id))
        : null;
    if (node?.src && ["image", "video"].includes(node.type))
      previewMedia(node, title(node));
  });
  $("selection-download").addEventListener("click", () => {
    const node =
      selected.size === 1
        ? ctx.doc().nodes.find((n) => selected.has(n.id))
        : null;
    if (node?.src && ["image", "video", "audio"].includes(node.type))
      downloadMedia(node, title(node));
  });
  $("selection-save-asset").addEventListener("click", () =>
    ctx.saveAssets(ctx.doc().nodes.filter((n) => selected.has(n.id))),
  );
  $("selection-group").addEventListener("click", groupSelected);
  $("selection-ungroup").addEventListener("click", ungroupSelected);
  $("selection-copy").addEventListener("click", copySelected);
  $("selection-delete").addEventListener("click", removeSelected);
  document.addEventListener("keydown", (event) => {
    if (
      event.isComposing ||
      event.target.closest(
        "input,textarea,select,[contenteditable=true],dialog,.library-panel",
      ) ||
      !$("menu").hidden ||
      !$("embedded-page").hidden
    )
      return;
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "g") {
      event.preventDefault();
      if (event.shiftKey) ungroupSelected();
      else groupSelected();
      return;
    }
    if (event.key === "Escape") {
      gesture = null;
      clearConnection();
      document
        .querySelectorAll(".connecting")
        .forEach((el) => el.classList.remove("connecting"));
      selected.clear();
      syncSelection();
    }
    if (event.code === "Space") {
      event.preventDefault();
      space = true;
      $("canvas").classList.add("panning");
    }
    if (event.key.toLowerCase() === "n") addMenu();
    if (event.key.toLowerCase() === "f") fit();
    if (event.key.toLowerCase() === "v") setTool("select");
    if (event.key.toLowerCase() === "h") setTool("pan");
    if (
      (event.key === "Delete" || event.key === "Backspace") &&
      selected.size
    ) {
      event.preventDefault();
      removeSelected();
    }
    if (
      ["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(event.key) &&
      selected.size
    ) {
      event.preventDefault();
      const step = event.shiftKey ? 50 : 10;
      ctx.mutate((doc) => {
        for (const n of selectionRoots(doc.nodes, selected)) {
          n.x +=
            event.key === "ArrowLeft"
              ? -step
              : event.key === "ArrowRight"
                ? step
                : 0;
          n.y +=
            event.key === "ArrowUp"
              ? -step
              : event.key === "ArrowDown"
                ? step
                : 0;
        }
      });
    }
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "d") {
      event.preventDefault();
      copySelected();
    }
    if (
      event.target.matches("[data-edge]") &&
      (event.key === "Enter" || event.key === " ")
    ) {
      event.preventDefault();
      edgeMenu(event.target.dataset.edge, event.target);
    }
  });
  document.addEventListener("keyup", (event) => {
    if (event.code === "Space") {
      space = false;
      $("canvas").classList.remove("panning");
    }
  });
  window.addEventListener("blur", () => {
    space = false;
    gesture = null;
    closeMenu();
    clearConnection();
    $("selection-box").hidden = true;
    $("canvas").classList.remove("panning");
    render();
  });
  new ResizeObserver(() => {
    if (!ctx.doc()) return;
    drawMap();
    drawEdges(ctx.doc().nodes);
    if (activePreview)
      drawConnection(activePreview.reference, activePreview.end);
    positionTools();
  }).observe($("canvas"));
  return {
    render,
    fit,
    locate,
    add,
    addAssets,
    selected: () => [...selected],
    select,
    deselect: (id) => {
      selected.delete(id);
      syncSelection();
    },
    title,
  };
}
