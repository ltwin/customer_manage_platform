---
slug: webgpu-perf-warm-shader-cache
status: observed
scope: 3D 场景编辑器：测 WebGPU 渲染的首帧、切换与编译耗时
createdAt: 2026-10-21T17:16:39.127Z
source: 24a57b62-e69e-4946-9cf4-9a95989b5240
---
Chromium（Metal 后端）会跨页面、跨浏览器进程缓存已编译的着色器，同一场景重复测得的是热缓存数字。R1 切到最终渲染：热缓存第一帧 0.25 s、灯光 0.4–0.65 s；着色器全新时第一帧 1.7 s、灯光 3.7 s。测冷启动的做法：临时往一处 TSL 着色里加一个每次加载都不同的随机常量，让生成的 WGSL 每次不同；测完撤掉，并用 grep 确认已撤掉。报告里热、冷两组数字分开写，并注明是哪一组。另外，three r186 第二次构建同一个材质时可能调整 uniform 顺序，生成的代码不同，所以第二次切换（R1 实测 2.2 s）仍可能是冷编译，第三次起才稳定在热缓存。证据：scene-webgpu-pipeline 的 discovery/research/perf.md「切到最终渲染」。
