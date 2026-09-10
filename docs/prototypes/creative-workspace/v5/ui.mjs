import { icons } from "./icons.mjs";
export const $ = (id) => document.getElementById(id);
export const esc = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
export const icon = (name) => icons[name] || icons.image;
export const iconButton = (name, label, attrs = "") =>
  `<button class="icon-button" aria-label="${esc(label)}" title="${esc(label)}" ${attrs}>${icon(name)}</button>`;
export function paintIcons(root = document) {
  root.querySelectorAll("[data-icon]").forEach((el) => {
    el.innerHTML = icon(el.dataset.icon);
  });
}
let toastTimer;
export function notify(message) {
  clearTimeout(toastTimer);
  $("toast").textContent = message;
  $("toast").classList.add("show");
  toastTimer = setTimeout(() => $("toast").classList.remove("show"), 4200);
}
let menuAnchor = null,
  menuClose = null;
export function closeMenu(restore = false) {
  $("menu").hidden = true;
  menuAnchor?.setAttribute("aria-expanded", "false");
  if (restore && menuAnchor?.isConnected)
    menuAnchor.focus({ preventScroll: true });
  menuAnchor = null;
  const cleanup = menuClose;
  menuClose = null;
  cleanup?.();
}
export function menu(anchor, items, action, point, onClose) {
  closeMenu();
  menuAnchor = anchor;
  menuClose = onClose;
  anchor?.setAttribute("aria-expanded", "true");
  const el = $("menu");
  el.innerHTML = items
    .map((item) =>
      item.separator
        ? '<div class="menu-separator" role="separator"></div>'
        : `<button role="menuitem" data-command="${esc(item.id)}" ${item.disabled ? "disabled" : ""} class="${item.danger ? "danger-text" : ""}">${icon(item.icon || "plus")}<span>${esc(item.label)}${item.note ? `<small>${esc(item.note)}</small>` : ""}</span>${item.key ? `<kbd>${esc(item.key)}</kbd>` : ""}</button>`,
    )
    .join("");
  el.hidden = false;
  const rect = anchor?.getBoundingClientRect(),
    x = point?.x ?? rect?.left ?? 80,
    y = point?.y ?? (rect ? rect.bottom + 7 : 80);
  el.style.left = `${Math.max(8, Math.min(x, innerWidth - el.offsetWidth - 8))}px`;
  el.style.top = `${Math.max(8, Math.min(y, innerHeight - el.offsetHeight - 8))}px`;
  el.onclick = (event) => {
    const button = event.target.closest("[data-command]");
    if (!button || button.disabled) return;
    const id = button.dataset.command;
    closeMenu(true);
    Promise.resolve(action(id)).catch((error) => notify(error.message));
  };
  el.querySelector("button:not(:disabled)")?.focus({ preventScroll: true });
}
$("menu").addEventListener("keydown", (event) => {
  const buttons = [...$("menu").querySelectorAll("button:not(:disabled)")],
    i = buttons.indexOf(document.activeElement);
  if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
    event.preventDefault();
    buttons[
      event.key === "Home"
        ? 0
        : event.key === "End"
          ? buttons.length - 1
          : (i + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) %
            buttons.length
    ]?.focus();
  }
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation();
    closeMenu(true);
  }
  if (event.key === "Tab") closeMenu();
});
document.addEventListener("pointerdown", (e) => {
  if (
    !$("menu").hidden &&
    !$("menu").contains(e.target) &&
    !menuAnchor?.contains(e.target)
  )
    closeMenu();
});
window.addEventListener("resize", () => closeMenu());
let dialogOpener,
  initialForm = "",
  submitting = false,
  closeHandler,
  trackChanges = false;
function fingerprint() {
  return [...$("dialog").querySelectorAll("input,textarea")]
    .map((e) => `${e.type}:${e.value}:${e.checked}`)
    .join("|");
}
export function dialogHasChanges() {
  return $("dialog").open && trackChanges && fingerprint() !== initialForm;
}
export function closeDialog(force = false) {
  if (submitting) return;
  if (!force && trackChanges && fingerprint() !== initialForm) {
    $("discard").showModal();
    return;
  }
  $("dialog").close();
  closeHandler?.();
  if (dialogOpener?.isConnected) dialogOpener.focus({ preventScroll: true });
}
export function dialog({
  title,
  description = "",
  content,
  submit = "保存",
  onSubmit,
  onClose,
  wide = false,
}) {
  closeMenu();
  dialogOpener = document.activeElement;
  closeHandler = onClose;
  trackChanges = Boolean(onSubmit);
  $("dialog").classList.toggle("wide-dialog", wide);
  $("dialog-title").textContent = title;
  $("dialog-description").textContent = description;
  $("dialog-body").innerHTML = content;
  $("dialog-error").textContent = "";
  $("dialog-submit").textContent = submit;
  $("dialog-submit").hidden = !onSubmit;
  $("dialog-cancel").textContent = onSubmit ? "取消" : "关闭";
  initialForm = fingerprint();
  $("dialog").showModal();
  $("dialog-form").onsubmit = async (event) => {
    event.preventDefault();
    if (submitting || !onSubmit) return;
    submitting = true;
    $("dialog-submit").disabled = true;
    try {
      await onSubmit(new FormData($("dialog-form")));
      submitting = false;
      closeDialog(true);
    } catch (error) {
      $("dialog-error").textContent = error.message;
      $("dialog-error").focus();
    } finally {
      submitting = false;
      $("dialog-submit").disabled = false;
    }
  };
}
$("dialog-close").addEventListener("click", () => closeDialog());
$("dialog-cancel").addEventListener("click", () => closeDialog());
$("dialog").addEventListener("cancel", (e) => {
  e.preventDefault();
  closeDialog();
});
$("discard-keep").addEventListener("click", () => $("discard").close());
$("discard-leave").addEventListener("click", () => {
  $("discard").close();
  closeDialog(true);
});
export function download(name, data, type = "application/json") {
  const url = URL.createObjectURL(new Blob([data], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 5000);
}
