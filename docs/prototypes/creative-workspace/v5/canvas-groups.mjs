// 存储坐标属于父组；绘制、命中和连线统一使用解析后的世界坐标。
export function worldNodes(nodes) {
  const byId = new Map(nodes.map((n) => [n.id, n])),
    cache = new Map(),
    visiting = new Set();
  function resolve(n) {
    if (cache.has(n.id)) return cache.get(n.id);
    if (visiting.has(n.id)) throw new Error("画布分组存在循环。");
    visiting.add(n.id);
    const parent = n.parentId ? byId.get(n.parentId) : null;
    if (n.parentId && (!parent || parent.type !== "group"))
      throw new Error("节点的父组已不存在。");
    const p = parent ? resolve(parent) : { x: 0, y: 0 };
    const result = { ...n, x: n.x + p.x, y: n.y + p.y };
    cache.set(n.id, result);
    visiting.delete(n.id);
    return result;
  }
  return nodes.map(resolve);
}
export function descendantIds(nodes, ids) {
  const result = new Set(ids);
  let changed = true;
  while (changed) {
    changed = false;
    for (const n of nodes)
      if (n.parentId && result.has(n.parentId) && !result.has(n.id)) {
        result.add(n.id);
        changed = true;
      }
  }
  return result;
}
export function selectionRoots(nodes, ids) {
  const chosen = new Set(ids),
    byId = new Map(nodes.map((n) => [n.id, n]));
  return nodes.filter((n) => {
    if (!chosen.has(n.id)) return false;
    let parent = n.parentId;
    const seen = new Set();
    while (parent && !seen.has(parent)) {
      if (chosen.has(parent)) return false;
      seen.add(parent);
      parent = byId.get(parent)?.parentId;
    }
    return true;
  });
}
function rectangle(nodes) {
  const x = Math.min(...nodes.map((n) => n.x)),
    y = Math.min(...nodes.map((n) => n.y));
  return {
    x,
    y,
    width: Math.max(...nodes.map((n) => n.x + n.width)) - x,
    height: Math.max(...nodes.map((n) => n.y + n.height)) - y,
  };
}
export function groupNodes(doc, ids) {
  const roots = selectionRoots(doc.nodes, ids);
  if (roots.length < 2) throw new Error("请框选或多选至少两个节点后再打组。");
  const world = worldNodes(doc.nodes),
    byId = new Map(world.map((n) => [n.id, n]));
  const parents = new Set(roots.map((n) => n.parentId || null));
  const parentId = parents.size === 1 ? [...parents][0] : null;
  const parent = byId.get(parentId) || { x: 0, y: 0 },
    box = rectangle(roots.map((n) => byId.get(n.id)));
  const id = crypto.randomUUID(),
    gx = box.x - 28,
    gy = box.y - 52;
  const group = {
    id,
    type: "group",
    title: "未命名分组",
    parentId,
    x: gx - parent.x,
    y: gy - parent.y,
    width: box.width + 56,
    height: box.height + 80,
  };
  for (const n of roots) {
    const w = byId.get(n.id);
    n.parentId = id;
    n.x = w.x - gx;
    n.y = w.y - gy;
  }
  doc.nodes.push(group);
  return id;
}
export function ungroupNodes(doc, ids) {
  const selected = new Set(ids),
    released = [];
  // 外层先解组，随后解内层仍能保持绝对位置。
  const groups = doc.nodes.filter(
    (n) => selected.has(n.id) && n.type === "group",
  );
  function depth(n) {
    let d = 0,
      p = n.parentId;
    while (p) {
      d++;
      p = doc.nodes.find((n) => n.id === p)?.parentId;
    }
    return d;
  }
  groups.sort((a, b) => depth(a) - depth(b));
  for (const group of groups) {
    for (const child of doc.nodes.filter((n) => n.parentId === group.id)) {
      child.x += group.x;
      child.y += group.y;
      child.parentId = group.parentId || null;
      released.push(child.id);
    }
    doc.nodes = doc.nodes.filter((n) => n.id !== group.id);
  }
  return [...new Set(released)].filter((id) =>
    doc.nodes.some((n) => n.id === id),
  );
}
export function fitGroupFrames(nodes) {
  const seen = new Set();
  function fit(group) {
    if (seen.has(group.id)) return;
    seen.add(group.id);
    const children = nodes.filter((n) => n.parentId === group.id);
    children.filter((n) => n.type === "group").forEach(fit);
    if (!children.length) return;
    const box = rectangle(children),
      dx = box.x - 28,
      dy = box.y - 52;
    // 调整组边框时补偿子坐标，子节点世界位置不变。
    if (Math.abs(dx) > 1e-8) {
      group.x += dx;
      children.forEach((n) => (n.x -= dx));
    }
    if (Math.abs(dy) > 1e-8) {
      group.y += dy;
      children.forEach((n) => (n.y -= dy));
    }
    group.width = box.width + 56;
    group.height = box.height + 80;
  }
  nodes.filter((n) => n.type === "group").forEach(fit);
}
export function copyGroupedNodes(doc, ids) {
  const roots = selectionRoots(doc.nodes, ids),
    rootIds = new Set(roots.map((n) => n.id));
  const all = descendantIds(doc.nodes, rootIds),
    mapping = new Map([...all].map((id) => [id, crypto.randomUUID()]));
  const copies = doc.nodes
    .filter((n) => all.has(n.id))
    .map((n) => ({
      ...structuredClone(n),
      id: mapping.get(n.id),
      parentId: mapping.get(n.parentId) || n.parentId || null,
      x: n.x + (rootIds.has(n.id) ? 35 : 0),
      y: n.y + (rootIds.has(n.id) ? 35 : 0),
    }));
  const edges = doc.edges
    .filter((e) => all.has(e.from) && all.has(e.to))
    .map((e) => ({
      ...e,
      id: crypto.randomUUID(),
      from: mapping.get(e.from),
      to: mapping.get(e.to),
    }));
  doc.nodes.push(...copies);
  doc.edges.push(...edges);
  return roots.map((n) => mapping.get(n.id));
}
