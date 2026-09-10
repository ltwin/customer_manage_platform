import { $, esc, icon, dialog, notify, menu } from "./ui.mjs?connection-menu=1";
import {
  TAG_COLORS,
  tagColor,
  saveTag,
  deleteTag,
  saveTagGroup,
  deleteTagGroup,
} from "../v4/tags.mjs";

export async function readAssetFile(file, readImage) {
  if (file.type.startsWith("image/"))
    return { kind: "image", ...(await readImage(file)), title: file.name };
  const kind = ["video/mp4", "video/webm"].includes(file.type)
    ? "video"
    : ["audio/mpeg", "audio/wav", "audio/x-wav", "audio/ogg"].includes(
          file.type,
        )
      ? "audio"
      : null;
  if (!kind) throw new Error("支持 JPG、PNG、WebP、MP4、WebM、MP3、WAV、OGG。");
  if (file.size > 12 * 1024 * 1024)
    throw new Error("视频或音频请控制在 12 MB 内。");
  const src = await new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(r.result);
    r.onerror = () => reject(new Error("文件读取失败。"));
    r.readAsDataURL(file);
  });
  return { kind, src, title: file.name, width: 16, height: 9 };
}
export function createAssetEditors(ctx) {
  function organization(groupIds = [], tagIds = []) {
    const s = ctx.state();
    return `<details class="asset-field-picker"><summary>${icon("folder")}归入分组 <span id="edit-group-count">${groupIds.length || "可选"}</span></summary><div class="asset-check-list">${s.collections.map((g) => `<label><input type="checkbox" name="groups" value="${esc(g.id)}" ${groupIds.includes(g.id) ? "checked" : ""}>${esc(g.name)}</label>`).join("") || "<p>尚无分组，可稍后整理。</p>"}</div></details>
    <details class="asset-field-picker" open><summary>${icon("tag")}标签 <span id="edit-tag-count">${tagIds.length || "可选"}</span></summary><input id="edit-tag-search" placeholder="查找或新建标签" aria-label="查找或新建标签"><div class="asset-check-list" id="edit-tags">${tagOptions(s, tagIds)}</div><div id="inline-tag-new" class="inline-tag-new"><input id="inline-tag-name" placeholder="新标签名称" aria-label="新标签名称" maxlength="40"><select id="inline-tag-color" aria-label="标签颜色">${TAG_COLORS.map(([id, name]) => `<option value="${id}">${name}</option>`).join("")}</select><select id="inline-tag-group" aria-label="标签分类"><option value="">未分类</option>${s.tagGroups.map((g) => `<option value="${esc(g.id)}">${esc(g.name)}</option>`).join("")}</select><button type="button" id="inline-tag-create" class="pill-button">新建并选择</button><p id="inline-tag-error" role="alert"></p></div></details>`;
  }
  function tagOptions(s, selected) {
    return [{ id: null, name: "未分类" }, ...s.tagGroups]
      .map((g) => {
        const tags = s.tagCatalog.filter((t) => (t.groupId || null) === g.id);
        return tags.length
          ? `<div class="tag-option-group"><small>${esc(g.name)}</small>${tags.map((t) => `<label data-tag-name="${esc(t.name.toLowerCase())}"><input type="checkbox" name="tags" value="${esc(t.id)}" ${selected.includes(t.id) ? "checked" : ""}><i style="--tag-color:${tagColor(t.color)}"></i>${esc(t.name)}</label>`).join("")}</div>`
          : "";
      })
      .join("");
  }
  function bindOrganization() {
    const updateCounts = () => {
      $("edit-tag-count").textContent =
        `${document.querySelectorAll("#dialog input[name=tags]:checked").length} 已选`;
      $("edit-group-count").textContent =
        `${document.querySelectorAll("#dialog input[name=groups]:checked").length} 已选`;
    };
    $("dialog-body").addEventListener("change", updateCounts);
    $("edit-tag-search").oninput = () => {
      const q = $("edit-tag-search").value.trim().toLowerCase();
      $("edit-tags")
        .querySelectorAll("[data-tag-name]")
        .forEach((el) => (el.hidden = !el.dataset.tagName.includes(q)));
      $("inline-tag-name").value = $("edit-tag-search").value;
    };
    $("inline-tag-create").onclick = async () => {
      const b = $("inline-tag-create");
      b.disabled = true;
      try {
        const id = crypto.randomUUID(),
          selected = [
            ...document.querySelectorAll("#dialog input[name=tags]:checked"),
          ].map((e) => e.value);
        await ctx.update((s) =>
          saveTag(s, {
            id,
            name: $("inline-tag-name").value,
            color: $("inline-tag-color").value,
            groupId: $("inline-tag-group").value,
          }),
        );
        $("edit-tags").innerHTML = tagOptions(ctx.state(), [...selected, id]);
        $("edit-tag-search").value = "";
        $("inline-tag-name").value = "";
        $("inline-tag-error").textContent = "";
        updateCounts();
      } catch (e) {
        $("inline-tag-error").textContent = e.message;
      } finally {
        b.disabled = false;
      }
    };
  }
  function edit(assets, { fresh = false, groupId = null } = {}) {
    const single = assets.length === 1,
      a = assets[0],
      s = ctx.state();
    const existingGroups = s.collections
      .filter((g) => assets.every((a) => g.items.includes(a.id)))
      .map((g) => g.id);
    const groups = fresh
      ? [...new Set([...existingGroups, ...(groupId ? [groupId] : [])])]
      : existingGroups;
    const tags = single ? a.tagIds || [] : [];
    dialog({
      title: fresh
        ? single
          ? `新增${{ text: "文字", link: "链接" }[a.kind] || "资产"}`
          : `存入资产库 · ${assets.length} 份`
        : single
          ? "编辑资产"
          : "批量整理",
      description: single
        ? "分组和标签可以稍后补充。"
        : "所选分组与标签将追加到全部所选素材，原有归类保留。",
      content: `${single ? `<label class="field-label">名称<input name="title" value="${esc(a.title)}" maxlength="200" required></label>${a.kind === "text" ? `<label class="field-label">正文<textarea name="text" rows="5" required>${esc(a.text)}</textarea></label>` : ""}${a.kind === "link" ? `<label class="field-label">链接地址<input name="source" type="url" value="${esc(a.source)}" placeholder="https://" required></label>` : ""}<label class="field-label">${a.kind === "link" ? "链接说明" : "描述"}<textarea name="description" rows="2">${esc(a.description || "")}</textarea></label>` : ""}${organization(groups, tags)}`,
      submit: fresh ? "存入资产库" : "保存",
      onSubmit: async (f) => {
        const tagIds = f.getAll("tags"),
          groupIds = f.getAll("groups");
        if (single && !String(f.get("title")).trim())
          throw new Error("请填写名称。");
        if (
          single &&
          a.kind === "link" &&
          !/^https?:\/\//i.test(f.get("source"))
        )
          throw new Error("请填写 http 或 https 链接。");
        await ctx.update((next) => {
          for (const original of assets) {
            let target = next.assets.find((x) => x.id === original.id);
            if (fresh && !target) {
              target = { ...original, created: Date.now(), tags: [] };
              next.assets.unshift(target);
            }
            if (!target) throw new Error("素材已不存在，请刷新后重试。");
            if (single) {
              target.title = String(f.get("title")).trim();
              target.description = String(f.get("description") || "");
              if (a.kind === "text") target.text = String(f.get("text"));
              if (a.kind === "link") target.source = String(f.get("source"));
            }
            target.tagIds = single
              ? tagIds
              : [...new Set([...(target.tagIds || []), ...tagIds])];
            for (const g of next.collections) {
              if (single) g.items = g.items.filter((id) => id !== target.id);
              if (groupIds.includes(g.id) && !g.items.includes(target.id))
                g.items.push(target.id);
            }
          }
        });
        notify(fresh ? "已存入个人资产库" : "资产信息已保存");
      },
    });
    bindOrganization();
  }
  function newAsset(kind, groupId) {
    edit(
      [
        {
          id: crypto.randomUUID(),
          kind,
          title: "",
          text: "",
          source: "",
          width: 4,
          height: 4,
        },
      ],
      { fresh: true, groupId },
    );
  }
  function manageTags() {
    dialog({
      title: "标签管理",
      description: "标签分类帮助整理词汇，标签颜色帮助快速识别。",
      wide: true,
      content: `<div class="asset-manager-tools"><button type="button" id="tag-new" class="pill-button">${icon("plus")}新建标签</button><button type="button" id="tag-group-new" class="pill-button">新建分类</button></div><div id="managed-tags"></div>`,
    });
    const render = () => {
      const s = ctx.state();
      $("managed-tags").innerHTML = [
        { id: null, name: "未分类" },
        ...s.tagGroups,
      ]
        .map(
          (g) =>
            `<section class="managed-tag-section"><header><strong>${esc(g.name)}</strong>${g.id ? `<button type="button" class="icon-button" data-tag-group="${esc(g.id)}" aria-label="管理分类：${esc(g.name)}">${icon("ellipsis")}</button>` : ""}</header><div>${
              s.tagCatalog
                .filter((t) => (t.groupId || null) === g.id)
                .map(
                  (t) =>
                    `<button type="button" data-manage-tag="${esc(t.id)}"><i style="--tag-color:${tagColor(t.color)}"></i>${esc(t.name)}${icon("ellipsis")}</button>`,
                )
                .join("") || "<small>暂无标签</small>"
            }</div></section>`,
        )
        .join("");
    };
    render();
    $("tag-new").onclick = () => tagForm();
    $("tag-group-new").onclick = () => groupForm();
    $("managed-tags").onclick = (e) => {
      const t = e.target.closest("[data-manage-tag]"),
        g = e.target.closest("[data-tag-group]");
      if (t)
        tagForm(
          ctx.state().tagCatalog.find((x) => x.id === t.dataset.manageTag),
        );
      if (g)
        groupForm(
          ctx.state().tagGroups.find((x) => x.id === g.dataset.tagGroup),
        );
    };
  }
  function groupForm(group) {
    dialog({
      title: group ? "编辑标签分类" : "新建标签分类",
      content: `<label class="field-label">分类名称<input name="name" required maxlength="40" value="${esc(group?.name || "")}"></label>${group ? '<button type="button" id="remove-tag-group" class="pill-button danger-text">删除分类，保留标签</button>' : ""}`,
      onSubmit: async (f) => {
        await ctx.update((s) =>
          saveTagGroup(s, { id: group?.id, name: f.get("name") }),
        );
        notify("标签分类已保存");
      },
    });
    if (group)
      $("remove-tag-group").onclick = async () => {
        try {
          await ctx.update((s) => deleteTagGroup(s, group.id));
          manageTags();
        } catch (e) {
          $("dialog-error").textContent = e.message;
        }
      };
  }
  function tagForm(tag) {
    const s = ctx.state();
    dialog({
      title: tag ? "编辑标签" : "新建标签",
      content: `<label class="field-label">名称<input name="name" required maxlength="40" value="${esc(tag?.name || "")}"></label><label class="field-label">颜色<select name="color">${TAG_COLORS.map(([id, n]) => `<option value="${id}" ${tag?.color === id ? "selected" : ""}>${n}</option>`).join("")}</select></label><label class="field-label">分类<select name="group"><option value="">未分类</option>${s.tagGroups.map((g) => `<option value="${esc(g.id)}" ${tag?.groupId === g.id ? "selected" : ""}>${esc(g.name)}</option>`).join("")}</select></label>${tag ? '<button type="button" id="remove-tag" class="pill-button danger-text">删除标签…</button>' : ""}`,
      onSubmit: async (f) => {
        await ctx.update((s) =>
          saveTag(s, {
            id: tag?.id,
            name: f.get("name"),
            color: f.get("color"),
            groupId: f.get("group"),
          }),
        );
        notify("标签已保存");
      },
    });
    if (tag)
      $("remove-tag").onclick = () =>
        dialog({
          title: "删除标签？",
          description: `“${tag.name}”将从所有素材中移除，素材保留。`,
          content: "",
          submit: "删除标签",
          onSubmit: async () => {
            await ctx.update((s) => deleteTag(s, tag.id));
            notify("标签已删除");
          },
        });
  }
  return { edit, newAsset, manageTags };
}
