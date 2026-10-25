# Diagnosis

> 状态：先记录（owner 2026-10-25「一时半会儿难以解决的问题可以先记issue，然后往后续推进」）。症状已实测，原因已知，修法改动较大，留给后续。来源是 `scene-offscreen-reflections`（S1）的未决事项 S1-U3。S1 只做 design A5，即离屏底座上传后释放 CPU 侧数组；视口这一份在本 issue 处理。代码在 `feat/scene-offscreen-reflections`，合入 develop 之前按该分支复现。

## Symptom

大厅（2001 万面、67 个模型）在编辑器里以实时 GI 打开，世界空间追踪层就绪后：

- 只开视口时，JS 堆加类型化数组约 1.6–1.9 GiB；
- 视口开着时再导出，两份底座并存，峰值约 2.8 GiB（S1 做 A5 之前测，做完后的数字以 S1 tasks 8.2 为准）；
- 这一层每个设备约 697 MiB（731,289,072 B），显存一份，视口还在 CPU 侧另留一份镜像。

期望（S1 design S1-U3）：视口的 JS 堆超过 1 GB 时，改为按区段直接写 GPU 缓冲，不保留 CPU 镜像。

## Root Cause

已知：

- 视口的底座是增量的（design「增量打包」）。区段的追加、扩容复制、压缩、拖动时按区间上传变换，都通过 three 的 `StorageBufferAttribute` 完成，而它从 CPU 数组上传。所以 `layer.ts` 为每个缓冲保留整份 CPU 数组（`layer.arrays`）；
- 判档用的层字节数和实例更新也读这些数组；
- 1.6–1.9 GiB 里另有模型几何本身。光栅、拾取和构建都要用它，不在本 issue 的范围内。

## Evidence(红)

```sh
# feat/scene-offscreen-reflections，约 12 分钟；JS 堆写进 trace-hall.json 的 heap.viewport 和 heap.peak（MiB）
cd frontend && SCENE_E2E_TRACE_PERF=<目录> SCENE_E2E_ONLY="trace perf: AC-S1-8" \
  CREATIVE_SCENE_URL=http://localhost:<port> node scripts/creative-scene.e2e.mjs
```

2026-10-25（Apple M4 Pro）：`heap.viewport` 1909 MiB，`heap.peak` 2835 MiB；`open.bytes` 731,289,072。

## Blast Radius

- 低内存设备打开超大场景时可能被浏览器杀掉标签页；
- 两个设备各自按 1 GiB 判档，互相不知道对方（S1-U3），视口和导出并存时总量没有约束；
- 中小场景的层只有几 MiB 到几十 MiB，不受影响。

## Fix Direction

1. 视口的层改为直接写 GPU 缓冲：追加和区间更新用 `queue.writeBuffer`，扩容和压缩用 `copyBufferToBuffer`，取 three 后端的 `GPUBuffer`。CPU 侧只保留分配表、变换槽之类的小数据。需要先确认 three r186 有没有稳定的入口，必要时加限定范围的补丁（`patterns` 已允许）。
2. 判档和实例更新改为读分配表，不读数组。
3. 两个设备共用一份内存账，按总量判档；或者在视口占用较大时，让导出降档。降档会影响画质，交 owner 决定。
