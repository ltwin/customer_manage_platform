# 几何代码沙箱穿刺（epic 未决 #2）

日期：2026-10-17。环境：macOS，Playwright 1.58.2，Chromium（Chrome for Testing 145）与 WebKit 26.0（Playwright webkit-2248）。穿刺代码在会话 scratchpad，不入库。

## 搭法

- 宿主页面（不设 CSP）嵌入 `<iframe sandbox="allow-scripts">`，iframe 文档由同一服务器提供，带 HTTP 头 CSP：
  `default-src 'none'; script-src <站点源> blob:; worker-src blob:; connect-src 'none'; img-src blob: data:; style-src 'none'; base-uri 'none'; form-action 'none'`。
- 沙箱页面脚本内嵌编译 Worker 的源码（esbuild 打包的 IIFE：`three@0.186.1`、`GLTFExporter`、`three-bvh-csg@0.0.18` + `three-mesh-bvh@0.9.16`、一个 `lib` 雏形），用 Blob URL 创建 Worker。
- Worker 收到模块文本后，用 Blob URL 加 `import()` 加载用户模块，调用 `build({params, THREE, lib, rng})`，用 `GLTFExporter.parseAsync(..., {binary: true})` 导出 GLB，连同 manifest 回传。
- 另起一个「攻击者」端口记录收到的一切请求；宿主页面写入 cookie 和 `localStorage` 作为诱饵。

## 结论

1. **CSP 的 `script-src 'self'` 在 WebKit 下不匹配**：opaque origin 的文档里，WebKit 把 `'self'` 当成 opaque origin，连沙箱自己的脚本都被拒绝；Chromium 按 URL 的源匹配。必须写明确的站点源（可带路径）。
2. **Chromium 不允许 opaque origin 里从 Blob 创建 module Worker**（启动即报无信息的 error 事件）；classic Worker 可以。WebKit 两种都行。采用 classic Worker；classic Worker 里 `import(blobURL)` 加载用户模块在两个浏览器都可用。
3. **正常编译**：圆柱减方块的 CSG、槽位材质、`part:` / `anchor:` 命名，两边都在 20–22 ms 内产出 16.8 KB 的 GLB，`magic = glTF`，manifest 的包围盒、三角形数、槽位、部件、挂点正确。12 次 CSG 减法的模块 23–30 ms。
4. **新建沙箱的代价**：iframe + Worker 从创建到就绪 20–47 ms（首次含脚本解析）。每次编译都用新 Worker 的代价可以接受。
5. **阻断情况**（两个浏览器一致，攻击者端口收到 0 个请求）：

| 尝试 | Worker 内 | iframe 主线程 |
|---|---|---|
| `fetch`（攻击者 / 本站）、XHR、WebSocket、EventSource | 拦截 | 拦截 |
| `import()` 攻击者脚本 / 本站脚本、`importScripts` | 拦截 | — |
| `eval`、`new Function` | 拦截（CSP 无 `unsafe-eval`） | — |
| IndexedDB | 拦截 | 拦截 |
| `document.cookie`、`localStorage`、`parent.document`、`top.location` | 无此对象 | 拦截（SecurityError） |
| 图片外联、弹窗、表单提交 | — | 拦截 |
| 嵌套 Worker 里 `fetch` | 拦截（继承 CSP） | — |
| `BroadcastChannel` | 可发，但宿主收不到（不同源） | — |
| Cache API | Chromium 无此接口；WebKit 可打开，但只属于这次的 opaque origin，碰不到站点数据 | — |

6. **死循环**：宿主 5 秒（穿刺用 3 秒）超时后移除 iframe，宿主主线程计时器全程正常（3 秒内 57–61 次 50 ms 心跳）；重建后下一次编译正常。
7. **内存炸弹**：Worker 内抛出分配失败（Chromium `Array buffer allocation failed`，WebKit `Out of memory`），页面没有崩溃。
8. **超大输出**：200 万三角形、72 MB 的 GLB 在 250–290 ms 内产出并传回。上限必须由宿主检查。
9. **manifest 不可信**：用户代码与打包的 three.js、`lib`、导出器在同一个 Worker 全局里，可以改写原型或 `postMessage` 伪造 manifest，也可能污染同一 Worker 的下一次编译。因此：每次编译用新 Worker；宿主自己解析 GLB、重新计算 manifest 并检查上限；服务端再算一次。
10. **`Math.random`**：three.js 创建对象时用它生成 UUID，不能让它抛错。改为每次编译从固定种子开始的确定性序列；`Date` 可以禁用。
11. iframe 文档请求本身会带站点 cookie（由宿主页面发起的导航），WebKit 下沙箱脚本请求也带。沙箱读不到响应头和 cookie，但 `script-src` 应限定到沙箱资源路径，避免用户代码对其他路径发起带 cookie 的 GET。

## 未覆盖

- 真实 Safari、Edge 与 Windows 未测；Firefox 不是目标浏览器，未测。
- 宿主侧 `GLTFLoader` 解析、缩略图渲染、暂存上传与 worker 校验链路的时延，留给实施时测量。
