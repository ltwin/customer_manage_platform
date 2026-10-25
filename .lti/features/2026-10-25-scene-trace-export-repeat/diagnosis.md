# Diagnosis

> 状态：先记录（owner 2026-10-25「一时半会儿难以解决的问题可以先记issue，然后往后续推进」）。症状每次都能复现，根因未查明，下面的根因是待验证的假设。来源是 `scene-offscreen-reflections`（S1）的未决事项 S1-U5，代码在 `feat/scene-offscreen-reflections`，合入 develop 之前按该分支复现。

## Symptom

S0 的压力场景「大厅」（2001 万面、67 个模型，S1 任务 8.1 加进编辑器）在编辑器里打开，实时 GI 下带着世界空间追踪层连续导出两次，两张材质光照不逐像素相同：

- 最大差 8–45 级；最多 0.15% 的像素差超过 8 级，集中在镜子一带，也就是反射补追的区域；
- 每次运行的差异位置和大小都不一样；
- 连 R2 的「一级以内」（AC-R2-5：最大差不超过 1，或差超过 8 级的像素不超过 0.01%）也过不了。

期望（S1 AC-S1-8 最后一项）：两次导出逐像素相同。参考场景都满足：AC-S1-4 在隐藏标签页里带这一层连续导出两次，9 张图逐像素相同。

## Root Cause

未查明。已排除：

- **关掉这一层**：同一页面、同样的步骤，两次导出逐字节相同（`trace-hall.json` 的 `exportsWithout`）；
- **换成 `mirror-board`**：在编辑器里带这一层导出两次，逐字节相同；
- **离屏层的布局**：两次导出各自的离屏层就绪时，层的每个缓冲和材质、灯光表按 SHA-256 摘要比对相同（`gpuTestHooks.traceDigests`）；
- **库随机生成的 WGSL 变量名**：固定下来之后仍然不同；
- **遍历栈溢出**：`layer.ts` 按顶层和底层的深度检查栈深，超出时换档；库的遍历循环本身也在越界前退出。

待验证的假设：

- **假设 A（中等置信度）**：离屏层在就绪之后、导出的帧画完之前又被同步过。摘要只在就绪那一刻记，如果之后还有网格的矩阵或材质变化，两次导出的时机不同，画面就会不同。验证：导出结束时再记一次摘要，并记下导出期间 `sync()` 的次数。
- **假设 B（低置信度）**：大厅一帧的 GPU 时间约 0.5 s，补追又集中在镜子一带，单个命令缓冲可能过长，被驱动中断或部分执行。目前控制台没有报错。验证：降低分辨率或把镜子缩小，看差异是否消失；打开 Chromium 的 GPU 日志。
- **假设 C（低置信度）**：命中点着色读到了未初始化或越界的数据，比如材质表里某一行、阴影贴图或 PMREM 的采样坐标。参考场景的几何、材质和灯光少，可能读不到这类数据。验证：依次关掉阴影光线、环境采样和预设调用，二分出差异来自哪一项。

## Evidence(红)

```sh
# feat/scene-offscreen-reflections，约 6 分钟；SCENE_E2E_TRACE_SCENES=mirror-board 换成参考场景（通过）
cd frontend && SCENE_E2E_TRACE_PERF=1 SCENE_E2E_ONLY="trace perf: S0’s hall exported twice" \
  CREATIVE_SCENE_URL=http://localhost:<port> node scripts/creative-scene.e2e.mjs
```

- 「AC-S1-8」那项也会导出两次，结果写进 `trace-hall.json` 的 `exports`；
- 一次运行的差异图在 `scene-offscreen-reflections/discovery/research/evidence/after/hall-exports-diff.png`。白色是差超过 8 级的像素，灰色是差 1–8 级的像素。

2026-10-25（Apple M4 Pro，headless Chromium，Metal）各次运行的结果：最大差 23、8、45 级；差超过 8 级的像素 0.015%–0.15%。

## Blast Radius

- 只在带这一层导出超大场景时出现，参考场景不受影响；
- 影响 AC-S1-8 最后一项，S1 按已知问题验收（owner 2026-10-25）；
- 如果根因是假设 C，其他场景也可能偶发，只是参考场景测不到；S2 的探针也会走同一套命中点着色。

## Fix Direction

1. 先验证假设 A。改动最小，只要在导出结束时再记一次摘要。
2. 再二分命中点着色的各项（假设 C）。
3. 找到来源之后，补一个能稳定复现的较小场景作为 e2e 回归用例，不必每次都跑大厅。
