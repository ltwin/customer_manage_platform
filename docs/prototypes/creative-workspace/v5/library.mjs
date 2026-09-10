import {
  $,
  esc,
  icon,
  dialog,
  notify,
  paintIcons,
} from "./ui.mjs?connection-menu=1";
import { createAssetShelf } from "./asset-shelf.mjs";
import { photo } from "./model.mjs";
const MARKET = [
  {
    id: "market-coast",
    kind: "image",
    title: "海岸边的呼吸",
    cover: 4,
    label: "素材 · 图片",
    tags: ["海边", "色彩"],
    description:
      "观察蓝色海面与花卉之间的色彩关系。图片沿用原型的 Unsplash 参考素材。",
    access: "free",
  },
  {
    id: "market-mono",
    kind: "style",
    title: "黑白，光的另一面",
    cover: 3,
    label: "风格参考",
    tags: ["黑白", "布光"],
    description:
      "从暗调肖像中观察明暗边界与情绪表达。仅展示风格参考条目的组织方式。",
    access: "free",
  },
  {
    id: "market-space",
    kind: "image",
    title: "建筑里的留白",
    cover: 1,
    label: "素材 · 图片",
    tags: ["建筑", "构图"],
    description: "垂直线条与人物比例的构图参考。",
    access: "free",
  },
  {
    id: "market-light",
    kind: "knowledge",
    title: "一盏灯的人像笔记",
    cover: 6,
    label: "知识包 · 图文",
    tags: ["布光", "人像"],
    description:
      "知识包示例：用主光方向、明暗边界与背景距离组织布光笔记。未来支持带出处的多模态检索。",
    access: "free",
  },
  {
    id: "market-summer",
    kind: "style",
    title: "把夏天留在风里",
    cover: 10,
    label: "风格参考",
    tags: ["海边", "氛围"],
    description: "低饱和蓝与暖色花卉之间的轻盈气氛。",
    access: "follow",
  },
  {
    id: "market-location",
    kind: "knowledge",
    title: "海岸外景勘景手册",
    cover: 12,
    label: "知识包 · 场景",
    tags: ["外景", "自然光"],
    description:
      "从拍摄时段、自然光方向和备选场地组织外景知识。市场与付费获取尚未开放。",
    access: "paid",
  },
  {
    id: "market-line",
    kind: "image",
    title: "空间的温柔秩序",
    cover: 9,
    label: "素材 · 图片",
    tags: ["建筑", "色彩"],
    description: "弧线与植物共同塑造空间节奏。",
    access: "free",
  },
  {
    id: "market-blue",
    kind: "image",
    title: "蓝色时刻",
    cover: 11,
    label: "素材 · 图片",
    tags: ["海边", "自然"],
    description: "记录环境里不同层次的蓝色。",
    access: "free",
  },
];
export function createLibrary(ctx) {
  let assets = [],
    q = "",
    kind = "all",
    composing = false,
    open = false,
    restoreAfterPage = false;
  const shelf = createAssetShelf({
    ...ctx,
    changed: (value) => {
      assets = value;
      renderMarket();
      ctx.onAssets(value);
    },
  });
  function setOpen(value) {
    open = value;
    $("library-panel").hidden = !value;
    document.body.classList.toggle("library-open", value);
    $("library-toggle").setAttribute("aria-expanded", String(value));
    if (value && innerWidth <= 950) ctx.closeAgent();
    if (!value) {
      $("library-panel").classList.remove("expanded");
      $("library-panel").style.width = "";
      $("library-expand").setAttribute("aria-label", "展开资产管理");
      $("library-expand").innerHTML = icon("maximize");
    }
  }
  function pageChanged(page) {
    if (page !== "canvas" && open) {
      restoreAfterPage = true;
      setOpen(false);
    } else if (page === "canvas" && restoreAfterPage) {
      restoreAfterPage = false;
      setOpen(true);
    }
  }
  function setTab(type) {
    const market = type === "market";
    $("personal-tab").setAttribute("aria-pressed", String(!market));
    $("market-tab").setAttribute("aria-pressed", String(market));
    $("personal-panel").hidden = market;
    $("market-panel").hidden = !market;
    if (market) renderMarket();
  }
  function renderMarket() {
    const items = MARKET.filter(
      (a) =>
        (kind === "all" || a.kind === kind) &&
        (!q ||
          [a.title, a.description, ...a.tags]
            .join(" ")
            .toLowerCase()
            .includes(q.toLowerCase())),
    );
    $("market-grid").innerHTML = items
      .map((a) => {
        const collected = assets.some((x) => x.id === a.id);
        return `<article class="market-card"><button class="market-cover" data-market-open="${a.id}" aria-label="查看：${esc(a.title)}"><img src="${photo(a.cover)}" width="170" height="213" alt="${esc(a.title)}" loading="lazy"><span>${a.label}</span></button><h3>${esc(a.title)}</h3><p>示例精选 · ${a.tags.join(" / ")}</p><button data-market-collect="${a.id}" ${a.access !== "free" || collected ? "disabled" : ""}>${icon(collected ? "check" : a.access === "free" ? "plus" : "lock")}${collected ? "已收藏" : a.access === "free" ? "收藏免费示例" : a.access === "follow" ? "关注可用 · 未开放" : "付费获取 · 未开放"}</button></article>`;
      })
      .join("");
    $("market-empty").hidden = items.length > 0;
    $("market-clear").hidden = !q;
    document
      .querySelectorAll("[data-market-kind]")
      .forEach((b) =>
        b.setAttribute("aria-pressed", String(b.dataset.marketKind === kind)),
      );
  }
  async function collect(id) {
    const item = MARKET.find((a) => a.id === id);
    if (!item || item.access !== "free") return;
    const isKnowledge = item.kind === "knowledge";
    const photos = await ctx.photos();
    const p = photos.find((p) => p.id === `asset-${item.cover}`);
    const asset = {
      ...p,
      id,
      title: item.title,
      kind: isKnowledge ? "text" : "image",
      src: isKnowledge ? "" : new URL(photo(item.cover), location.href).href,
      text: isKnowledge
        ? item.description +
          "\n\n1. 记录主光与面部夹角。\n2. 比较补光前后的阴影。\n3. 保存原始参考及适用条件。\n\n此为知识包的文字摘要示例，尚未接入 RAG。"
        : "",
      width: isKnowledge ? 4 : p.width,
      height: isKnowledge ? 5 : p.height,
      tags: item.tags,
      description: item.description,
      source: p.source,
    };
    await shelf.collect(asset);
    notify("示例已收藏到个人资产库");
  }
  $("library-toggle").onclick = () => setOpen(!open);
  $("library-close").onclick = () => {
    setOpen(false);
    $("library-toggle").focus();
  };
  $("library-expand").onclick = () => {
    $("library-panel").style.width = "";
    const expanded = $("library-panel").classList.toggle("expanded");
    $("library-expand").setAttribute(
      "aria-label",
      expanded ? "收起扩展资产管理" : "展开资产管理",
    );
    $("library-expand").innerHTML = icon(expanded ? "minimize" : "maximize");
  };
  const resize = document.createElement("div");
  resize.className = "asset-panel-resize";
  resize.tabIndex = 0;
  resize.setAttribute("role", "separator");
  resize.setAttribute("aria-label", "调整资产库宽度");
  resize.setAttribute("aria-orientation", "vertical");
  resize.setAttribute("aria-valuemin", "318");
  resize.setAttribute("aria-valuemax", "800");
  resize.setAttribute("aria-valuenow", "318");
  $("library-panel").append(resize);
  const setWidth = (value) => {
    const width = Math.max(318, Math.min(innerWidth - 110, 800, value));
    $("library-panel").style.width = `${width}px`;
    $("library-panel").classList.toggle("expanded", width >= 500);
    resize.setAttribute("aria-valuenow", String(Math.round(width)));
    $("library-expand").setAttribute(
      "aria-label",
      width >= 500 ? "收起扩展资产管理" : "展开资产管理",
    );
    $("library-expand").innerHTML = icon(
      width >= 500 ? "minimize" : "maximize",
    );
  };
  let resizing = null;
  resize.onpointerdown = (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    resizing = {
      x: e.clientX,
      width: $("library-panel").getBoundingClientRect().width,
    };
    resize.setPointerCapture(e.pointerId);
  };
  resize.onpointermove = (e) => {
    if (resizing) setWidth(resizing.width + e.clientX - resizing.x);
  };
  resize.onpointerup = resize.onpointercancel = () => (resizing = null);
  resize.onkeydown = (e) => {
    if (["ArrowLeft", "ArrowRight"].includes(e.key)) {
      e.preventDefault();
      setWidth(
        $("library-panel").clientWidth + (e.key === "ArrowRight" ? 40 : -40),
      );
    }
  };
  $("personal-tab").onclick = () => setTab("personal");
  $("market-tab").onclick = () => setTab("market");
  $("market-search").addEventListener("compositionstart", () => {
    composing = true;
  });
  $("market-search").addEventListener("compositionend", () => {
    composing = false;
    q = $("market-search").value;
    renderMarket();
  });
  $("market-search").oninput = () => {
    if (!composing) {
      q = $("market-search").value;
      renderMarket();
    }
  };
  $("market-clear").onclick = () => {
    q = "";
    $("market-search").value = "";
    renderMarket();
    $("market-search").focus();
  };
  $("market-reset").onclick = () => {
    q = "";
    kind = "all";
    $("market-search").value = "";
    renderMarket();
  };
  $("market-types").onclick = (e) => {
    const b = e.target.closest("[data-market-kind]");
    if (b) {
      kind = b.dataset.marketKind;
      renderMarket();
    }
  };
  $("market-grid").onclick = async (e) => {
    const b = e.target.closest("button");
    if (!b || b.disabled) return;
    if (b.dataset.marketCollect) {
      b.disabled = true;
      try {
        await collect(b.dataset.marketCollect);
      } catch (error) {
        notify(error.message);
        b.disabled = false;
      }
      return;
    }
    const item = MARKET.find((a) => a.id === b.dataset.marketOpen);
    if (!item) return;
    dialog({
      title: item.title,
      description: "公共市场 · 内容结构示例",
      wide: true,
      content: `<div class="market-detail"><img src="${photo(item.cover)}" alt="${esc(item.title)}" style="display:block;width:100%;max-height:350px;object-fit:contain;border-radius:12px;background:#14191e"><p>${esc(item.description)}</p><p>${item.tags.map(esc).join(" · ")}</p><p class="demo-note">示例影像沿用 v4 的 Unsplash 来源。这里只演示收藏行为，正式发布与授权规则另行设计。</p></div>`,
      submit: "收藏到个人资产库",
      onSubmit:
        item.access === "free" && !assets.some((a) => a.id === item.id)
          ? () => collect(item.id)
          : undefined,
    });
  };
  document.addEventListener("keydown", (e) => {
    if (
      e.isComposing ||
      e.target.closest("input,textarea,[contenteditable=true],dialog") ||
      !$("menu").hidden
    )
      return;
    if (e.key.toLowerCase() === "l") {
      e.preventDefault();
      setOpen(!open);
    }
  });
  window.addEventListener("resize", () => {
    if (open && innerWidth <= 950) ctx.closeAgent();
  });
  if (innerWidth > 1200) setOpen(true);
  return {
    assets: () => assets,
    addAssets: shelf.add,
    saveNodes: shelf.saveNodes,
    importFiles: shelf.importFiles,
    open: setOpen,
    isOpen: () => open,
    pageChanged,
    setTab,
    render: renderMarket,
  };
}
