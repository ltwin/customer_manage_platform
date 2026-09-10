import { $, esc, icon, menu, dialog, notify } from "./ui.mjs?connection-menu=1";
import { ORDERS, seed, photo } from "./model.mjs";
export function createAgent(ctx) {
  let contextIds = [],
    attachments = [],
    model = "auto",
    uploading = false,
    skill = null,
    composing = false;
  function setOpen(value) {
    document.body.classList.toggle("agent-closed", !value);
    $("agent-panel").hidden = !value;
    $("agent-reopen").hidden = value;
    if (value && innerWidth <= 950) ctx.closeLibrary();
  }
  const models = [
    { id: "auto", label: "自动选择", note: "按任务选择 · 原型演示" },
    { id: "gpt", label: "GPT", note: "模型系列占位 · 尚未接入" },
    { id: "gemini", label: "Gemini", note: "模型系列占位 · 尚未接入" },
    { id: "claude", label: "Claude", note: "模型系列占位 · 尚未接入" },
  ];
  function syncSelection(ids) {
    contextIds = [...ids];
    renderContext();
  }
  function reference(ids) {
    if (!ids.length) {
      notify("先在画布中选择一个或多个节点。");
      return;
    }
    contextIds = [...ids];
    setOpen(true);
    renderContext();
    $("chat-input").focus();
  }
  function renderContext() {
    contextIds = contextIds.filter((id) =>
      ctx.doc()?.nodes.some((n) => n.id === id),
    );
    $("context-chips").innerHTML = contextIds
      .map((id) => {
        const n = ctx.doc().nodes.find((n) => n.id === id);
        return `<div class="context-chip">${icon(n.type === "group" ? "layers" : n.type === "plan" ? "clapperboard" : n.type === "text" ? "file-text" : "image")}<span>${esc(ctx.nodeTitle(n))}</span><button data-remove-context="${id}" aria-label="移除引用：${esc(ctx.nodeTitle(n))}">${icon("x")}</button></div>`;
      })
      .join("");
  }
  function renderAttachments() {
    $("attachment-chips").innerHTML = attachments
      .map(
        (a) =>
          `<div class="attachment-chip"><img src="${esc(a.src)}" alt="${esc(a.name)}"><button type="button" data-remove-attachment="${a.id}" aria-label="移除附件：${esc(a.name)}">${icon("x")}</button></div>`,
      )
      .join("");
    updateSend();
  }
  function updateSend() {
    $("send-message").disabled =
      uploading ||
      composing ||
      (!$("chat-input").value.trim() && !attachments.length);
  }
  function render() {
    if (!ctx.doc()) return;
    const messages = ctx.doc().messages;
    document.querySelector(".agent-welcome").hidden = messages.length > 0;
    $("messages").innerHTML = messages
      .map(
        (m) =>
          `<article class="message ${m.role}">${m.role === "assistant" ? `<div class="message-label">${icon("sparkles")}创作助手 · 本地示例回复</div>` : ""}<p>${esc(m.text)}</p>${m.attachments?.length ? `<div class="message-attachments">${m.attachments.map((a) => `<img src="${esc(a.src)}" alt="${esc(a.name)}">`).join("")}</div>` : ""}${m.role === "user" ? `<small class="message-meta">${esc(models.find((x) => x.id === m.model)?.label || "自动选择")} · 演示${m.context?.length ? ` · ${m.context.length} 个节点引用` : ""}</small>` : ""}${m.planId ? `<button class="result-card" data-open-plan="${m.planId}"><span class="eyebrow">SHOOTING PLAN</span><strong>${esc(ctx.doc().plans.find((p) => p.id === m.planId)?.title || "策划")}</strong><small>打开策划 · 可以继续手动编辑 ${icon("arrow-up-right")}</small></button>` : m.action === "plan" ? '<button class="pill-button" data-demo-plan>体验拍摄策划 Skill</button>' : ""}</article>`,
      )
      .join("");
    renderContext();
  }
  async function send() {
    const input = $("chat-input"),
      text = input.value.trim();
    if (
      (!text && !attachments.length) ||
      composing ||
      $("send-message").disabled
    )
      return;
    $("send-message").disabled = true;
    const sentAttachments = structuredClone(attachments),
      sentContext = [...contextIds],
      sentModel = model;
    const usePlan = skill === "plan" || /策划|分镜|拍摄/.test(text);
    try {
      await ctx.mutate(
        (doc) => {
          doc.messages.push(
            {
              id: crypto.randomUUID(),
              role: "user",
              text: text || "请参考这些图片附件。",
              context: sentContext,
              attachments: sentAttachments,
              model: sentModel,
            },
            {
              id: crypto.randomUUID(),
              role: "assistant",
              text: usePlan
                ? `可以把这次想法整理成一份独立策划。\n\n先选择本次是否参考订单，再放入可编辑的示例策划。你可以随时修改分镜、布光和场地安排。\n\n当前为演示回复，没有调用生成模型。`
                : contextIds.length
                  ? `已引用 ${contextIds.length} 个节点。\n\n下一步可以比较这些参考的光线、构图与情绪，再把共同方向写成文字节点，或整理为拍摄策划。\n\n当前仅演示上下文与对话交互，尚未进行图像理解或检索。`
                  : "选中画布节点会自动作为上下文；也可以用加号上传图片附件，或直接描述创作意图。\n\n当前仅保存本地演示对话，尚未连接真实模型。",
              action: usePlan ? "plan" : null,
            },
          );
        },
        { history: false },
      );
      attachments = attachments.filter(
        (a) => !sentAttachments.some((sent) => sent.id === a.id),
      );
      renderAttachments();
      input.value = "";
      input.style.height = "";
      $("chat-scroll").scrollTop = $("chat-scroll").scrollHeight;
    } catch {
      /* 保存失败时保留输入，由共享错误条提供重试。 */
    } finally {
      updateSend();
    }
  }
  function planSkill() {
    setOpen(true);
    skill = "plan";
    $("skill-label").textContent = "拍摄策划";
    dialog({
      title: "拍摄策划 Skill",
      description:
        "把本次参考整理成独立策划，并在画布上放入摘要节点。此处使用预置内容演示。",
      content: `<label for="skill-plan-name">策划名称</label><input id="skill-plan-name" name="title" value="${esc(ctx.doc().title + " · 新方向")}" maxlength="80"><fieldset class="order-options"><legend>本次参考订单（可选）</legend><label class="radio-option"><input type="radio" name="order" value="" checked><span><strong>自由创作，不引用订单</strong><small>从当前想法与参考开始</small></span></label>${ORDERS.map((o) => `<label class="radio-option"><input type="radio" name="order" value="${o.id}"><span><strong>${esc(o.title)}</strong><small>${o.number}<br>${esc(o.theme)}</small></span></label>`).join("")}</fieldset><p class="demo-note">引用只作用于本次任务。生成后保持未发布，可以另行选择发布到订单。真实 Agent 与 RAG 暂未接入。</p>`,
      submit: "生成示例策划",
      onSubmit: async (data) => {
        const title = data.get("title").trim();
        if (!title) throw new Error("请填写策划名称。");
        const orderId = data.get("order") || null;
        const p = seed().plans[0];
        p.id = crypto.randomUUID();
        p.title = title;
        p.version = 1;
        p.sourceOrder = orderId;
        p.publication = null;
        p.shots.forEach((s) => {
          s.id = crypto.randomUUID();
        });
        if (orderId === "demo-mono") {
          p.brief = ORDERS[1].theme;
          p.shots = [1, 3, 5].map((n, i) => ({
            id: crypto.randomUUID(),
            title: ["建立人物与空间的关系", "靠近眼神的光", "留下轮廓与留白"][
              i
            ],
            image: photo(n),
            assetId: `asset-${n}`,
            lens: ["35 mm · 全景", "85 mm · 特写", "50 mm · 中景"][i],
            time: "",
            note: "使用一盏主灯，比较不同角度的明暗过渡。先保留自然眼神，再微调姿态。",
          }));
          p.sections = {
            style: "黑白、克制、突出眼神与面部轮廓。",
            location: "室内影棚。选择深灰背景与可控制环境光的空间。",
            lighting: "一盏主灯置于侧前方，白色反光板轻微补光。",
            props: "黑灰服装、椅子、反光板、灯架配重。",
            post: "黑白转换保留灰阶与肤质，控制高光不过曝。",
          };
        }
        const nodeId = crypto.randomUUID();
        await ctx.mutate((doc) => {
          doc.plans.push(p);
          doc.nodes.push({
            id: nodeId,
            type: "plan",
            planId: p.id,
            x: 710,
            y: 575,
            width: 316,
            height: 356,
          });
          doc.messages.push({
            id: crypto.randomUUID(),
            role: "assistant",
            text: `示例策划已放到画布，包含 ${p.shots.length} 个镜头与五个准备章节。\n\n${orderId ? "已记录本次参考订单；策划尚未发布到订单。" : "这次按自由创作处理，没有订单关联。"}\n可以打开策划继续修改。`,
            planId: p.id,
          });
        });
        ctx.navigate({ page: "canvas" });
        requestAnimationFrame(() => ctx.locate(nodeId));
        skill = null;
        $("skill-label").textContent = "选择 Skill";
        notify("示例策划已放入画布，尚未发布");
      },
    });
  }
  $("agent-close").onclick = () => {
    setOpen(false);
    $("agent-reopen").focus();
  };
  $("agent-reopen").onclick = () => setOpen(true);
  $("chat-context").onclick = () => $("chat-attachments").click();
  $("chat-attachments").onchange = async () => {
    const files = [...$("chat-attachments").files];
    $("chat-attachments").value = "";
    if (!files.length) return;
    if (files.length + attachments.length > 6) {
      notify("每次对话最多附加 6 张图片，请减少选择后重试。");
      return;
    }
    uploading = true;
    $("chat-context").disabled = true;
    updateSend();
    try {
      const images = [];
      for (const file of files)
        images.push({
          ...(await ctx.readImage(file)),
          name: file.name,
          id: crypto.randomUUID(),
        });
      attachments.push(...images);
      renderAttachments();
      notify(`已添加 ${images.length} 张附件，仅用于本次对话`);
    } catch (error) {
      notify(error.message);
    } finally {
      uploading = false;
      $("chat-context").disabled = false;
      updateSend();
    }
  };
  $("attachment-chips").onclick = (event) => {
    const button = event.target.closest("[data-remove-attachment]");
    if (!button) return;
    attachments = attachments.filter(
      (a) => a.id !== button.dataset.removeAttachment,
    );
    renderAttachments();
  };
  $("model-select").onclick = () =>
    menu(
      $("model-select"),
      models.map((m) => ({
        ...m,
        icon: m.id === model ? "check" : "sparkles",
        label: m.label + (m.id === model ? " · 当前" : ""),
      })),
      (id) => {
        model = id;
        $("model-label").textContent = models.find((m) => m.id === id).label;
      },
    );
  $("context-chips").onclick = (e) => {
    const b = e.target.closest("[data-remove-context]");
    if (b) {
      ctx.deselect(b.dataset.removeContext);
    }
  };
  $("chat-input").addEventListener("compositionstart", () => {
    composing = true;
  });
  $("chat-input").addEventListener("compositionend", () => {
    composing = false;
    updateSend();
  });
  $("chat-input").addEventListener("input", () => {
    if (!composing) updateSend();
    $("chat-input").style.height = "auto";
    $("chat-input").style.height =
      `${Math.min(170, $("chat-input").scrollHeight)}px`;
  });
  $("chat-input").addEventListener("keydown", (e) => {
    if (
      !e.isComposing &&
      !composing &&
      e.key === "Enter" &&
      (e.metaKey || e.ctrlKey)
    ) {
      e.preventDefault();
      send();
    }
  });
  $("chat-form").onsubmit = (e) => {
    e.preventDefault();
    send();
  };
  $("skill-select").onclick = () =>
    menu(
      $("skill-select"),
      [
        {
          id: "plan",
          icon: "clapperboard",
          label: "拍摄策划",
          note: "体验输入、产出与发布的关系",
        },
        {
          id: "reference",
          icon: "images",
          label: "梳理参考",
          note: "演示引用节点与对话",
        },
        {
          id: "rag",
          icon: "library",
          label: "多模态知识检索",
          note: "暂未开发",
          disabled: true,
        },
        {
          id: "generate",
          icon: "video",
          label: "图片 / 视频创作",
          note: "暂未接入模型",
          disabled: true,
        },
      ],
      (action) => {
        if (action === "plan") planSkill();
        else {
          skill = "reference";
          $("skill-label").textContent = "梳理参考";
          $("chat-input").focus();
        }
      },
    );
  $("suggest-plan").onclick = planSkill;
  $("suggest-reference").onclick = () => {
    const ids = ctx.selected();
    if (ids.length) reference(ids);
    $("chat-input").value = "帮我梳理这些参考里的光线、色彩与情绪。";
    $("send-message").disabled = false;
    $("chat-input").focus();
  };
  $("messages").onclick = (e) => {
    if (e.target.closest("[data-demo-plan]")) planSkill();
  };
  $("agent-history").onclick = () =>
    dialog({
      title: "本地会话",
      description: "当前项目的演示对话保存在此浏览器。",
      content: `<p>${ctx.doc()?.messages.length || 0} 条消息</p><p>切换到策划或收起助手后，对话仍然保留。多会话管理暂未开发。</p>`,
    });
  if (innerWidth <= 950) setOpen(false);
  return { render, reference, syncSelection, open: setOpen, planSkill };
}
