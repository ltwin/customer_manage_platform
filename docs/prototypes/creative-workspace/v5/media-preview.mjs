import { $, notify } from "./ui.mjs?connection-menu=1";

let opener = null,
  sourceVideo = null,
  preview = null,
  outsidePress = false;
const modal = $("media-preview");
export function previewMedia(node, title) {
  opener = document.activeElement;
  sourceVideo =
    [...document.querySelectorAll("[data-node]")]
      .find((el) => el.dataset.node === node.id)
      ?.querySelector("video") || null;
  sourceVideo?.pause();
  modal.setAttribute("aria-label", `媒体预览：${title}`);
  $("media-preview-error").hidden = true;
  preview = document.createElement(node.type === "video" ? "video" : "img");
  const media = preview;
  preview.src = node.src;
  if (node.type === "video") {
    preview.controls = true;
    preview.playsInline = true;
    const time = sourceVideo?.currentTime || 0;
    preview.addEventListener(
      "loadedmetadata",
      () => {
        if (preview === media)
          media.currentTime = Math.min(time, media.duration || time);
      },
      { once: true },
    );
  } else preview.alt = title;
  preview.addEventListener(
    "error",
    () => {
      if (preview === media) $("media-preview-error").hidden = false;
    },
    { once: true },
  );
  $("media-preview-content").replaceChildren(preview);
  modal.showModal();
}
$("media-preview-close").addEventListener("click", () => modal.close());
const outside = (target) =>
  target === modal || target === $("media-preview-content");
modal.addEventListener("pointerdown", (event) => {
  outsidePress = outside(event.target);
});
modal.addEventListener("click", (event) => {
  if (outsidePress && outside(event.target)) modal.close();
  outsidePress = false;
});
modal.addEventListener("close", () => {
  if (preview?.tagName === "VIDEO") {
    preview.pause();
    if (sourceVideo?.isConnected && sourceVideo.readyState > 0)
      sourceVideo.currentTime = preview.currentTime;
    preview.removeAttribute("src");
    preview.load();
  }
  $("media-preview-content").replaceChildren();
  preview = sourceVideo = null;
  outsidePress = false;
  if (opener?.isConnected) opener.focus({ preventScroll: true });
});

export async function downloadMedia(node, title) {
  const button = $("selection-download");
  if (button.disabled) return;
  button.disabled = true;
  button.setAttribute("aria-busy", "true");
  let url;
  try {
    const response = await fetch(node.src);
    if (!response.ok) throw new Error("媒体暂时无法下载，请稍后重试。");
    const blob = await response.blob();
    const extension =
      {
        "image/jpeg": "jpg",
        "image/png": "png",
        "image/webp": "webp",
        "video/mp4": "mp4",
        "video/webm": "webm",
        "audio/mpeg": "mp3",
        "audio/wav": "wav",
        "audio/x-wav": "wav",
        "audio/ogg": "ogg",
      }[blob.type.split(";")[0]] || "";
    const name = (title || "媒体")
      .replace(/[\\/:*?"<>|\x00-\x1f]/g, "_")
      .slice(0, 100);
    url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download =
      extension && !name.toLowerCase().endsWith("." + extension)
        ? name + "." + extension
        : name;
    document.body.append(link);
    link.click();
    link.remove();
    notify("已发起媒体下载");
  } catch (error) {
    notify(
      error.message === "Failed to fetch"
        ? "媒体下载失败，请检查网络或素材来源是否允许下载。"
        : error.message,
    );
  } finally {
    if (url) setTimeout(() => URL.revokeObjectURL(url), 60000);
    button.disabled = false;
    button.removeAttribute("aria-busy");
  }
}
