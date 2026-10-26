---
slug: three-specular-denoise-mirror-speckles
status: observed
scope: 3D 场景编辑器：实时 GI 的反射或漫反射用 three 的 RecurrentDenoiseNode 降噪，尤其是镜面、长光线或屏幕外补追的结果
createdAt: 2026-10-26T08:12:44.571Z
source: 679ca314-5a9a-49ef-bb72-691f46cd716b
---
RecurrentDenoiseNode 在 specular 模式下，取样半径 = 0.05 m × 光线长度系数 × 视深 × max(√粗糙度, 0.01) × 收敛系数，对镜面也有下限，而且随光线长度放大。镜子的预通道粗糙度被 getRoughness 抬到约 0.05，补追得到的光线又长，8 个 Vogel 取样点从几个像素外取到别的物体的颜色，镜中出现颗粒斑点；取样旋转来自 frameId，所以每次导出斑点位置不同，两次导出也不一致（S1 design A11、issue scene-trace-export-repeat）。镜面的反射波瓣没有随机噪声，S1 让预通道粗糙度 ≤ 0.06 的像素不做空间降噪，0.06–0.12 渐变（realtimeGI.ts 的 MIRROR_ROUGHNESS）。排查时把降噪半径临时设为 0：斑点消失就是降噪的问题；还可以按距离、实例编号着色出图（gpuTestHooks.traceShade），排除求交本身。上游草稿在 S1 discovery/research/upstream-drafts.md 第 5 份。
