import {
  $,
  esc,
  icon,
  iconButton,
  dialog,
  notify,
  download,
  closeDialog,
} from "./ui.mjs?connection-menu=1";
import { SECTIONS, ORDERS } from "./model.mjs";
export function createPlanning(ctx) {
  const root = $("embedded-page");
  function navigate(page, id = "", extra = {}) {
    ctx.navigate({ page, plan: id, ...extra });
  }
  const find = (id) => ctx.doc().plans.find((p) => p.id === id);
  function header(title, right = "") {
    return `<header class="embedded-header"><button class="back-button" data-back-canvas>${icon("minimize")}还原节点</button><span class="embedded-breadcrumb">拍摄策划 <span>·</span> ${esc(title)}</span><div class="embedded-header-actions">${right}</div></header>`;
  }
  function render() {
    const {
      page = "canvas",
      plan: id,
      section = "shots",
      order: orderId,
    } = ctx.route();
    root.hidden = page === "canvas";
    $("canvas").hidden = page !== "canvas";
    if (page === "canvas") return;

    if (page === "order") {
      renderOrder(orderId, id);
      return;
    }
    const plan = find(id);
    if (!plan) {
      root.innerHTML =
        header("策划不可用") +
        '<div class="empty"><h3>没有找到这份策划</h3><p>这份策划仍可从“引用已有策划”重新放到画布。</p><button class="pill-button" data-plan-list>引用已有策划</button></div>';
      return;
    }
    if (page === "live") {
      renderLive(plan);
      return;
    }
    const order = ORDERS.find((o) => o.id === plan.publication?.orderId);
    const lead = plan.shots.find((s) => s.image);
    root.innerHTML =
      header(
        plan.title,
        `${iconButton("download", "导出策划 Markdown", `data-export-plan="${plan.id}"`)}<button class="pill-button" data-live-plan="${plan.id}">${icon("play")}现场模式</button>`,
      ) +
      `
      <div class="plan-scroll"><div class="plan-hero"><div class="plan-hero-copy"><span class="eyebrow">SHOOTING PLAN <span>·</span> 工作版本 v${plan.version}</span><button class="plan-title-button" data-edit-plan="${plan.id}"><h1>${esc(plan.title)}</h1>${icon("pencil")}</button><p>${esc(plan.brief || "还没有写下创作意图。点击标题，为这份策划留下一个起点。")}</p><div class="plan-status-row"><span class="status-badge">${icon(plan.publication ? "file-check" : "file-text")}${plan.publication ? (plan.publication.version === plan.version ? "已发布到订单" : "有未发布修改") : "策划草稿"}</span><span>${plan.shots.length} 个分镜</span></div></div><div class="plan-hero-image">${lead ? `<img src="${esc(lead.image)}" alt="策划参考" loading="lazy">` : `<span>${icon("clapperboard")}</span>`}<span>REFERENCE / 参考画面</span></div></div>
      <div class="order-association"><div>${icon("link")}<span>${order ? `<small>已发布到 · v${plan.publication.version}</small><button data-order="${order.id}" data-plan="${plan.id}">${esc(order.title)} <span>${order.number}</span> ${icon("external-link")}</button>` : "<small>订单关联</small><strong>尚未发布到订单</strong>"}</span></div><button class="pill-button" data-publish-plan="${plan.id}">${order ? "更新发布" : "发布到订单"}</button></div>
      <nav class="plan-tabs" aria-label="策划章节">${Object.entries(SECTIONS)
        .map(
          ([key, text]) =>
            `<button data-section="${key}" data-plan="${plan.id}" ${section === key ? 'aria-current="page"' : ""}>${esc(text)}${key === "shots" ? `<span>${plan.shots.length}</span>` : ""}</button>`,
        )
        .join("")}</nav>
      ${section === "shots" ? shots(plan) : chapter(plan, section)}
      <footer class="plan-foot"><span>${icon("file-check")}策划独立保留，可继续在画布创作</span><span>原型演示 · 本地保存</span></footer></div>`;
  }
  function shots(plan) {
    return `<section class="shot-section"><div class="section-heading"><div><h2>把想法，拆成一个个画面。</h2><p>参考画面、拍摄方式与现场提示，在这里放到一起。</p></div><button class="pill-button" data-new-shot="${plan.id}">${icon("plus")}添加分镜</button></div><div class="shot-table"><div class="shot-table-heading"><span>镜头 / 参考</span><span>画面与拍摄指导</span><span>操作</span></div>${plan.shots.map((shot, i) => `<article class="shot-row" data-shot="${shot.id}"><div class="shot-visual"><span class="shot-number">${String(i + 1).padStart(2, "0")}</span><button class="shot-image" data-edit-shot="${shot.id}" data-plan="${plan.id}" aria-label="插入或替换分镜图片：${esc(shot.title)}">${shot.image ? `<img src="${esc(shot.image)}" alt="${esc(shot.title)}" loading="lazy">` : `${icon("image-plus")}<span>添加参考图</span>`}<span class="shot-image-edit">${icon("image-plus")}替换参考</span></button></div><div class="shot-description"><div class="shot-facts"><span>${esc(shot.lens || "景别待补充")}</span><span>${esc(shot.time || "时间待补充")}</span></div><h3>${esc(shot.title)}</h3><p>${esc(shot.note || "可以补充机位、动作与拍摄提示。")}</p></div><div class="shot-actions">${iconButton("pencil", "编辑分镜：" + shot.title, `data-edit-shot="${shot.id}" data-plan="${plan.id}"`)}${iconButton("trash-2", "移除分镜：" + shot.title, `data-remove-shot="${shot.id}" data-plan="${plan.id}"`)}</div></article>`).join("")}</div>${!plan.shots.length ? `<div class="empty"><h3>从第一个镜头开始</h3><p>可以手写分镜，独立选择参考图。</p><button class="pill-button" data-new-shot="${plan.id}">添加分镜</button></div>` : ""}</section>`;
  }
  function chapter(plan, section) {
    return `<section class="plan-chapter"><div class="section-heading"><h2>${esc(SECTIONS[section] || "拍摄风格")}</h2><button class="pill-button" data-edit-section="${section}" data-plan="${plan.id}">${icon("pencil")}编辑内容</button></div><div class="chapter-text">${esc(plan.sections[section] || "还没有内容。可以手动写下想法，也可以从 Agent 的候选中继续整理。")}</div><div class="chapter-reference-strip">${plan.shots
      .filter((s) => s.image)
      .map(
        (s) =>
          `<img src="${esc(s.image)}" alt="${esc(s.title)}" loading="lazy">`,
      )
      .join("")}</div></section>`;
  }
  function editPlan(id) {
    const plan = find(id);
    dialog({
      title: "编辑策划信息",
      content: `<label for="plan-name">策划名称</label><input id="plan-name" name="title" maxlength="80" value="${esc(plan.title)}"><label for="plan-brief">创作意图</label><textarea id="plan-brief" name="brief" rows="5" maxlength="2000">${esc(plan.brief)}</textarea>`,
      onSubmit: async (data) => {
        if (!data.get("title").trim()) throw new Error("请填写策划名称。");
        await ctx.mutate(
          (doc) => {
            const p = doc.plans.find((p) => p.id === id);
            p.title = data.get("title").trim();
            p.brief = data.get("brief");
            p.version++;
          },
          { history: false },
        );
        notify("策划信息已保存");
      },
    });
  }
  function editShot(planId, shotId) {
    const plan = find(planId),
      shot = plan.shots.find((s) => s.id === shotId) || {
        id: crypto.randomUUID(),
        title: "",
        lens: "",
        time: "",
        note: "",
        image: "",
        assetId: null,
      };
    let selectedImage = { image: shot.image, assetId: shot.assetId };
    dialog({
      title: shotId ? "编辑分镜" : "添加分镜",
      description: "参考图保存在这份策划中，不依赖画布上的图片节点。",
      wide: true,
      content: `<label for="shot-title">镜头名称</label><input id="shot-title" name="title" maxlength="100" value="${esc(shot.title)}"><div class="field-grid"><div><label for="shot-lens">焦段 / 景别</label><input id="shot-lens" name="lens" maxlength="100" value="${esc(shot.lens)}" placeholder="例如：35 mm · 全景"></div><div><label for="shot-time">建议拍摄时间</label><input id="shot-time" name="time" maxlength="40" value="${esc(shot.time)}" placeholder="例如：日落前 40 分钟"></div></div><label for="shot-note">画面与拍摄指导</label><textarea id="shot-note" name="note" rows="3" maxlength="3000">${esc(shot.note)}</textarea><div class="image-field-heading"><label>参考图片</label><button type="button" class="pill-button" id="clear-shot-image">${icon("x")}移除图片</button></div><input id="shot-image-value" name="image" type="hidden" value="${esc(shot.image)}"><div class="shot-editor-preview" id="shot-editor-preview">${shot.image ? `<img src="${esc(shot.image)}" alt="当前分镜参考">` : "<span>从下方资产库选择，或上传一张参考图片</span>"}</div><div class="asset-picker" id="shot-asset-picker">${ctx
        .assets()
        .filter((a) => a.kind === "image")
        .map(
          (a) =>
            `<button type="button" data-shot-image="${a.id}" aria-label="选择参考图：${esc(a.title)}" aria-pressed="${a.id === shot.assetId}"><img src="${esc(a.src)}" alt="${esc(a.title)}" loading="lazy"><span>${esc(a.title)}</span></button>`,
        )
        .join(
          "",
        )}</div><label class="upload-control">${icon("upload")}上传参考图片 · JPG / PNG / WebP · 最大 5 MB<input type="file" id="shot-upload" accept="image/jpeg,image/png,image/webp"></label>`,
      submit: "保存分镜",
      onSubmit: async (data) => {
        if (!data.get("title").trim()) throw new Error("请填写镜头名称。");
        await ctx.mutate(
          (doc) => {
            const p = doc.plans.find((p) => p.id === planId),
              value = {
                ...shot,
                ...selectedImage,
                title: data.get("title").trim(),
                lens: data.get("lens"),
                time: data.get("time"),
                note: data.get("note"),
              },
              index = p.shots.findIndex((s) => s.id === shot.id);
            if (index >= 0) p.shots[index] = value;
            else p.shots.push(value);
            p.version++;
          },
          { history: false },
        );
        notify("分镜已保存");
      },
    });
    const preview = () => {
      $("shot-image-value").value = selectedImage.image || "";
      $("shot-editor-preview").innerHTML = selectedImage.image
        ? `<img src="${esc(selectedImage.image)}" alt="选中的参考图片">`
        : "<span>尚未选择参考图片</span>";
      document
        .querySelectorAll("[data-shot-image]")
        .forEach((b) =>
          b.setAttribute(
            "aria-pressed",
            String(b.dataset.shotImage === selectedImage.assetId),
          ),
        );
    };
    $("shot-asset-picker").onclick = (e) => {
      const button = e.target.closest("[data-shot-image]");
      if (!button) return;
      const asset = ctx.assets().find((a) => a.id === button.dataset.shotImage);
      selectedImage = { image: asset.src, assetId: asset.id };
      preview();
    };
    $("clear-shot-image").onclick = () => {
      selectedImage = { image: "", assetId: null };
      preview();
    };
    $("shot-upload").onchange = async (e) => {
      try {
        const file = e.target.files[0];
        if (!file) return;
        const media = await ctx.readImage(file);
        selectedImage = { image: media.src, assetId: null };
        preview();
      } catch (error) {
        $("dialog-error").textContent = error.message;
      }
    };
  }
  function removeShot(planId, shotId) {
    const shot = find(planId).shots.find((s) => s.id === shotId);
    dialog({
      title: "移除这个分镜？",
      description: `“${shot.title}”将从工作版本中移除。资产库原图、已发布版本及已开始的现场记录保留。`,
      content: "",
      submit: "移除分镜",
      onSubmit: async () => {
        await ctx.mutate(
          (doc) => {
            const p = doc.plans.find((p) => p.id === planId);
            p.shots = p.shots.filter((s) => s.id !== shotId);
            p.version++;
          },
          { history: false },
        );
        notify("分镜已移除，已发布版本保持原样");
      },
    });
    $("dialog-cancel").focus();
  }
  function publish(id) {
    const p = find(id),
      selected = p.publication?.orderId || p.sourceOrder;
    dialog({
      title: p.publication ? "更新订单中的策划" : "发布到订单",
      description: `发布工作版本 v${p.version}。这里仅演示账号内部的订单关联，不发送给客户。`,
      content: `<p class="publication-summary">${esc(p.title)} · ${p.shots.length} 个分镜</p><fieldset class="order-options"><legend>发布目标订单</legend>${ORDERS.map((o) => `<label class="radio-option"><input type="radio" name="order" value="${o.id}" ${selected === o.id ? "checked" : ""}><span><strong>${esc(o.title)}</strong><small>${o.number} · ${o.date}<br>${esc(o.theme)}</small></span></label>`).join("")}</fieldset><p class="demo-note">演示订单。本次参考：${esc(ORDERS.find((o) => o.id === p.sourceOrder)?.title || "未引用订单")}。可以选择不同的发布目标。</p>`,
      submit: "确认发布（演示）",
      onSubmit: async (data) => {
        const orderId = data.get("order");
        if (!ORDERS.some((o) => o.id === orderId))
          throw new Error("请选择发布目标订单。");
        await ctx.mutate(
          (doc) => {
            const plan = doc.plans.find((p) => p.id === id);
            plan.publication = {
              orderId,
              version: plan.version,
              snapshot: structuredClone({ ...plan, publication: null }),
            };
          },
          { history: false },
        );
        notify("演示发布完成，订单关联已显示在策划中");
      },
    });
  }
  async function startLive(id) {
    const p = find(id);
    if (!p) return;
    if (
      !ctx.doc().live ||
      ctx.doc().live.planId !== id ||
      (!ctx.doc().live.snapshot.shots.length && p.shots.length)
    )
      await ctx.mutate(
        (doc) => {
          doc.live = {
            planId: id,
            snapshot: structuredClone(p),
            index: 0,
            done: [],
          };
        },
        { history: false },
      );
    navigate("live", id);
  }
  function renderLive(plan) {
    const live = ctx.doc().live;
    if (!live || live.planId !== plan.id) {
      root.innerHTML =
        header(plan.title) +
        `<div class="empty"><h3>还没有开始现场模式</h3><button class="pill-button" data-live-plan="${plan.id}">使用工作版本 v${plan.version}</button></div>`;
      return;
    }
    const snapshot = live.snapshot,
      index = Math.min(live.index, Math.max(0, snapshot.shots.length - 1)),
      shot = snapshot.shots[index];
    root.innerHTML = `<header class="embedded-header"><button class="back-button" data-open-plan="${plan.id}">${icon("arrow-left")}返回策划</button><span class="embedded-breadcrumb">现场模式 · v${snapshot.version}</span><span class="live-local">本地演示</span></header><div class="field-mode">${shot ? `<div class="field-image">${shot.image ? `<img src="${esc(shot.image)}" alt="${esc(shot.title)}">` : `<div class="empty">${icon("image")}此镜头未设置参考图片</div>`}<span class="field-counter">${String(index + 1).padStart(2, "0")} <span>/ ${String(snapshot.shots.length).padStart(2, "0")}</span></span></div><div class="field-info"><span class="eyebrow">${esc(snapshot.title)}</span><h1>${esc(shot.title)}</h1><div class="shot-facts"><span>${esc(shot.lens)}</span><span>${esc(shot.time)}</span></div><p>${esc(shot.note)}</p><div class="field-tip"><span>${icon("sun")}布光提示</span><p>${esc(snapshot.sections.lighting || "尚未填写布光提示。")}</p></div>${snapshot.version !== plan.version ? `<div class="version-note">策划已更新至 v${plan.version}，本次现场仍使用 v${snapshot.version}。</div>` : ""}<div class="field-controls"><button class="pill-button" data-live-prev ${index === 0 ? "disabled" : ""}>${icon("chevron-left")}上一镜</button><button class="pill-button primary" data-live-done="${shot.id}">${icon("check")}${live.done.includes(shot.id) ? "已拍摄 · 撤销标记" : "标记已拍摄"}</button><button class="pill-button" data-live-next ${index === snapshot.shots.length - 1 ? "disabled" : ""}>下一镜${icon("chevron-right")}</button></div><p class="field-progress">已拍 ${live.done.length} / ${snapshot.shots.length} 个镜头 · 保存在当前浏览器</p></div>` : `<div class="empty"><h3>这份策划还没有分镜</h3><p>返回策划添加镜头，再重新开始现场演示。</p><button class="pill-button" data-open-plan="${plan.id}">返回策划</button></div>`}</div>`;
  }
  function renderOrder(orderId, planId) {
    const order = ORDERS.find((o) => o.id === orderId);
    if (!order) {
      root.innerHTML =
        header("订单不可用") + '<div class="empty">未找到这个演示订单。</div>';
      return;
    }
    const plans = ctx
      .doc()
      .plans.filter((p) => p.publication?.orderId === orderId);
    root.innerHTML = `<header class="embedded-header"><button class="back-button" ${planId ? `data-open-plan="${planId}"` : "data-back-canvas"}>${icon("arrow-left")}${planId ? "返回策划" : "返回画布"}</button><span class="embedded-breadcrumb">影约 CRM <span>/</span>订单详情</span><span class="demo-badge">关联跳转演示</span></header><div class="order-page plan-scroll"><span class="eyebrow">${order.number}</span><h1>${esc(order.title)}</h1><span class="status-badge">${order.status}</span><div class="order-facts">${[
      ["拍摄对象", order.customer],
      ["拍摄日期", order.date],
      ["拍摄时间", order.time],
      ["拍摄地点", order.place],
    ]
      .map(([k, v]) => `<div><span>${k}</span><strong>${esc(v)}</strong></div>`)
      .join(
        "",
      )}</div><section><h2>拍摄需求</h2><p>${esc(order.theme)}</p></section><section><h2>策划交付</h2><p>这里显示已明确发布到此订单的版本。</p>${plans.map((p) => `<button class="order-plan-card" data-published-plan="${p.id}">${icon("clapperboard")}<span><strong>${esc(p.publication.snapshot.title)}</strong><small>已发布 v${p.publication.version} · ${p.publication.snapshot.shots.length} 个分镜${p.publication.version !== p.version ? " · 另有未发布修改" : ""}</small></span>${icon("arrow-up-right")}</button>`).join("") || "<p>还没有已发布策划。</p>"}</section><p class="demo-note">这是虚构订单，用于体验策划与 CRM 的跳转关系，未访问真实业务数据。</p></div>`;
  }
  function chooseExisting() {
    dialog({
      title: "引用已有策划",
      description: "策划文档独立保留。选择一份文档，将摘要节点放回当前画布。",
      content: `<div class="plan-document-list">${
        ctx
          .doc()
          .plans.map(
            (p) =>
              `<article><div><span class="eyebrow">v${p.version} · ${p.shots.length} 个分镜</span><h2>${esc(p.title)}</h2><p>${esc(p.brief || "还没有创作意图")}</p></div><button type="button" class="pill-button" data-put-plan="${p.id}">${icon("plus")}${ctx.doc().nodes.some((n) => n.planId === p.id) ? "定位已有节点" : "放回画布"}</button></article>`,
          )
          .join("") || "<p>还没有独立策划文档。先新建一个策划节点。</p>"
      }</div>`,
      wide: true,
    });
  }
  function exportPlan(id) {
    const p = find(id);
    download(
      `${p.title}.md`,
      `# ${p.title}\n\n工作版本 v${p.version}\n\n${p.brief}\n\n` +
        Object.entries(SECTIONS)
          .map(
            ([key, label]) =>
              `## ${label}\n\n${key === "shots" ? p.shots.map((s, i) => `### ${i + 1}. ${s.title}\n\n${s.lens} · ${s.time}\n\n${s.note}`).join("\n\n") : p.sections[key] || "尚未填写"}`,
          )
          .join("\n\n"),
      "text/markdown;charset=utf-8",
    );
    notify("策划 Markdown 已导出");
  }
  document.addEventListener("click", async (e) => {
    const b = e.target.closest("button");
    if (!b || b.disabled) return;
    const d = b.dataset;
    if (d.backCanvas !== undefined) navigate("canvas");
    if (d.maximizeNode) {
      const node = ctx.doc().nodes.find((n) => n.id === d.maximizeNode);
      if (node) {
        ctx.select(node.id);
        navigate("plan", node.planId, { node: node.id, section: "shots" });
      }
    }
    if (d.openPlan) navigate("plan", d.openPlan, { section: "shots" });
    if (d.livePlan) startLive(d.livePlan);
    if (d.order) navigate("order", d.plan || "", { order: d.order });
    if (d.section) navigate("plan", d.plan, { section: d.section });
    if (d.editPlan) editPlan(d.editPlan);
    if (d.newShot) editShot(d.newShot);
    if (d.editShot) editShot(d.plan, d.editShot);
    if (d.removeShot) removeShot(d.plan, d.removeShot);
    if (d.publishPlan) publish(d.publishPlan);
    if (d.exportPlan) exportPlan(d.exportPlan);
    if (d.planList !== undefined) chooseExisting();
    if (d.putPlan) {
      closeDialog(true);
      const existing = ctx.doc().nodes.find((n) => n.planId === d.putPlan);
      if (existing) {
        navigate("canvas");
        requestAnimationFrame(() => ctx.locate(existing.id));
      } else {
        const id = crypto.randomUUID();
        await ctx.mutate((doc) => {
          doc.nodes.push({
            id,
            type: "plan",
            planId: d.putPlan,
            x: 400,
            y: 200,
            width: 316,
            height: 356,
          });
        });
        navigate("canvas");
        requestAnimationFrame(() => ctx.locate(id));
      }
    }
    if (d.editSection) {
      const p = find(d.plan);
      dialog({
        title: `编辑${SECTIONS[d.editSection]}`,
        content: `<label for="chapter-content">${SECTIONS[d.editSection]}</label><textarea id="chapter-content" rows="10" name="content" maxlength="10000">${esc(p.sections[d.editSection])}</textarea>`,
        onSubmit: async (data) => {
          await ctx.mutate(
            (doc) => {
              const plan = doc.plans.find((p) => p.id === d.plan);
              plan.sections[d.editSection] = data.get("content");
              plan.version++;
            },
            { history: false },
          );
          notify("策划章节已保存");
        },
      });
    }
    if (d.livePrev !== undefined || d.liveNext !== undefined)
      await ctx.mutate(
        (doc) => {
          const live = doc.live;
          live.index = Math.max(
            0,
            Math.min(
              live.snapshot.shots.length - 1,
              live.index + (d.liveNext !== undefined ? 1 : -1),
            ),
          );
        },
        { history: false },
      );
    if (d.liveDone)
      await ctx.mutate(
        (doc) => {
          doc.live.done = doc.live.done.includes(d.liveDone)
            ? doc.live.done.filter((id) => id !== d.liveDone)
            : [...doc.live.done, d.liveDone];
        },
        { history: false },
      );
    if (d.publishedPlan) {
      const p = find(d.publishedPlan),
        published = p.publication.snapshot;
      dialog({
        title: `${published.title} · 已发布 v${p.publication.version}`,
        description: "此处读取发布时固定的内容，工作版本的后续修改不会覆盖它。",
        wide: true,
        content: `<div class="published-preview"><p>${esc(published.brief)}</p>${published.shots.map((s) => `<article>${s.image ? `<img src="${esc(s.image)}" alt="${esc(s.title)}">` : ""}<div><h3>${esc(s.title)}</h3><p>${esc(s.lens)} · ${esc(s.time)}</p><p>${esc(s.note)}</p></div></article>`).join("")}</div>`,
        submit: "关闭",
      });
    }
  });
  return {
    render,
    existing: chooseExisting,
    open: (id, node) => navigate("plan", id, { node, section: "shots" }),
    live: startLive,
    publish,
    editPlan,
  };
}
