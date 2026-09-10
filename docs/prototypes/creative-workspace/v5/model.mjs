import { descendantIds } from "./canvas-groups.mjs";
// 画布布局与独立策划分开保存；移除节点不删除策划或素材。
export const TYPES = {
  text: "文字",
  image: "图片",
  video: "视频",
  audio: "音频",
  scene: "3D 导演台",
  plan: "拍摄策划",
  group: "分组",
};
export const SECTIONS = {
  shots: "分镜表",
  style: "拍摄风格",
  location: "场地与空间",
  lighting: "布光方案",
  props: "道具准备",
  post: "后期思路",
};
export const ORDERS = [
  {
    id: "demo-sea",
    number: "PO-2026-0918",
    title: "海边情绪人像",
    date: "2026.09.18",
    time: "16:30–18:30",
    customer: "林女士",
    theme: "海风、白裙与日落；自然、松弛，保留环境的呼吸感。",
    place: "海岸外景 · 待确认",
    status: "待拍摄",
  },
  {
    id: "demo-mono",
    number: "PO-2026-0926",
    title: "黑白肖像习作",
    date: "2026.09.26",
    time: "14:00–16:00",
    customer: "陈先生",
    theme: "黑白肖像，一盏灯，突出面部轮廓与眼神。",
    place: "室内影棚",
    status: "待拍摄",
  },
];
export const photo = (number) =>
  `../v4/assets/photo-${String(number).padStart(2, "0")}.jpg`;
export function newPlan(title = "未命名拍摄策划") {
  return {
    id: crypto.randomUUID(),
    title,
    brief: "",
    version: 1,
    sourceOrder: null,
    publication: null,
    shots: [],
    sections: { style: "", location: "", lighting: "", props: "", post: "" },
  };
}
export function seed() {
  const plan = newPlan("九月，向海而行");
  plan.id = "plan-sea";
  plan.brief =
    "以海风为线索，记录人与海岸之间松弛、安静的片刻。保留自然光，也保留一点偶然。";
  plan.sourceOrder = "demo-sea";
  plan.shots = [
    {
      id: "shot-1",
      title: "沿着海岸，慢慢走",
      image: photo(12),
      assetId: "asset-12",
      lens: "35 mm · 全景",
      time: "16:40",
      note: "低机位，人物放在画面右侧。让海岸线把视线引向远处，留出天空。",
    },
    {
      id: "shot-2",
      title: "等海风吹过来",
      image: photo(10),
      assetId: "asset-10",
      lens: "85 mm · 近景",
      time: "17:10",
      note: "逆光，让头发自然散开。观察眼神和呼吸，避免持续指挥动作。",
    },
    {
      id: "shot-3",
      title: "蓝色时刻的留白",
      image: photo(11),
      assetId: "asset-11",
      lens: "50 mm · 中景",
      time: "17:45",
      note: "日落后保留冷色环境光，降低反差。人物看向海面，结束在一个安静的背影。",
    },
  ];
  plan.sections = {
    style:
      "自然、松弛、略带电影叙事感。\n低饱和蓝与暖肤色形成轻微对照，保留海风和衣料的动态。\n参考重点是留白与光线，不照搬人物造型。",
    location:
      "海岸步道 → 礁石边缘 → 开阔沙滩。\n勘景确认日落方向、潮汐与可站立区域；避免湿滑礁石。\n雨天备选：有大窗的室内空间，保留阴天漫射光。",
    lighting:
      "主光：日落前的侧逆光。\n补光：白色反光板，保持阴影层次；无须把脸补得过亮。\n蓝色时刻：按环境曝光，再用小功率常亮灯补眼神光。",
    props:
      "白色棉麻裙 / 备用外搭\n透明雨伞 / 小毛巾 / 平底鞋\n反光板 / 灯架配重 / 备用电池\n饮水与防晒用品",
    post: "统一白平衡，肤色保留暖意。\n蓝色轻微向青偏移，压低绿色饱和度。\n保留自然皮肤纹理与微弱颗粒，不过度磨皮。\n选片按“到达—靠近—告别”组织叙事。",
  };
  plan.publication = {
    orderId: "demo-sea",
    version: 1,
    snapshot: structuredClone({ ...plan, publication: null }),
  };
  return {
    revision: 0,
    title: "九月，向海而行",
    camera: null,
    nodes: [
      {
        id: "n-brief",
        type: "text",
        title: "留一点空间，给海风",
        text: "不是看向镜头，\n是走进风里。\n\n自然光，松弛的瞬间。",
        x: 42,
        y: 60,
        width: 230,
        height: 208,
      },
      {
        id: "n-sea",
        type: "image",
        title: "海岸的色彩与呼吸",
        src: photo(4),
        assetId: "asset-4",
        x: 42,
        y: 322,
        width: 230,
        height: 214,
      },
      {
        id: "n-main",
        type: "image",
        title: "寻找人与空间的关系",
        src: photo(12),
        assetId: "asset-12",
        x: 354,
        y: 90,
        width: 266,
        height: 387,
      },
      {
        id: "n-plan",
        type: "plan",
        planId: plan.id,
        x: 711,
        y: 146,
        width: 316,
        height: 356,
      },
      {
        id: "n-text",
        type: "text",
        title: "拍摄前的观察",
        text: "日落前四十分钟。\n让风先经过，再按下快门。",
        x: 354,
        y: 545,
        width: 266,
        height: 174,
      },
    ],
    edges: [
      { id: "e-1", from: "n-brief", to: "n-main" },
      { id: "e-2", from: "n-sea", to: "n-main" },
    ],
    plans: [plan],
    messages: [],
    live: null,
  };
}
export function referenceLabel(from, to) {
  if (to.type === "video" && to.mode === "frames" && from.type === "image")
    return "画面参考";
  return (
    {
      text: "参考文本",
      image: "参考图片",
      video: "参考视频",
      audio: "参考音频",
      scene: "场景参考",
    }[from.type] || "参考输入"
  );
}
export function connect(doc, fromId, toId) {
  const from = doc.nodes.find((n) => n.id === fromId),
    to = doc.nodes.find((n) => n.id === toId);
  if (!from || !to) throw new Error("节点已不存在，请重新选择。");
  if (from.id === to.id) throw new Error("请选择另一个节点作为参考对象。");
  if (from.type === "group" || to.type === "group")
    throw new Error("请连接组内的具体节点，分组本身不是参考输入。");
  if (from.type === "plan" || to.type === "plan")
    throw new Error("策划通过独立文档使用参考，当前不连接输入端口。");
  if (doc.edges.some((e) => e.from === fromId && e.to === toId))
    throw new Error("这两个节点已经连接。");
  doc.edges.push({ id: crypto.randomUUID(), from: fromId, to: toId });
}
export function removeNodes(doc, ids) {
  ids = [...descendantIds(doc.nodes, ids)];
  doc.nodes = doc.nodes.filter((n) => !ids.includes(n.id));
  doc.edges = doc.edges.filter(
    (e) => !ids.includes(e.from) && !ids.includes(e.to),
  );
}
export function bounds(nodes) {
  if (!nodes.length) return { x: 0, y: 0, width: 900, height: 600 };
  const x = Math.min(...nodes.map((n) => n.x)),
    y = Math.min(...nodes.map((n) => n.y));
  return {
    x,
    y,
    width: Math.max(...nodes.map((n) => n.x + n.width)) - x,
    height: Math.max(...nodes.map((n) => n.y + n.height)) - y,
  };
}
export async function openStore() {
  const db = await new Promise((resolve, reject) => {
    const req = indexedDB.open("creative-canvas-prototype-v5", 1);
    req.onupgradeneeded = () => req.result.createObjectStore("documents");
    req.onsuccess = () => resolve(req.result);
    req.onerror = () =>
      reject(new Error("本地存储未能打开，请检查浏览器权限后重试。"));
  });
  const read = () =>
    new Promise((resolve, reject) => {
      const req = db
        .transaction("documents")
        .objectStore("documents")
        .get("canvas");
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(new Error("读取画布失败，请重试。"));
    });
  const save = (next, expected) =>
    new Promise((resolve, reject) => {
      const tx = db.transaction("documents", "readwrite"),
        store = tx.objectStore("documents");
      let message =
        "本地保存失败，请释放浏览器存储空间后重试。当前画布仍保留在页面中。";
      const req = store.get("canvas");
      req.onsuccess = () => {
        if (req.result && req.result.revision !== expected) {
          message =
            "另一个窗口已更新画布。请先导出本地副本，再刷新读取最新版本。";
          tx.abort();
          return;
        }
        store.put({ ...next, revision: expected + 1 }, "canvas");
      };
      tx.oncomplete = () => resolve(expected + 1);
      tx.onerror = tx.onabort = () => reject(new Error(message));
    });
  return { read, save };
}
