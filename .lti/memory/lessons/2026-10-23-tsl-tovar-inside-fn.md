---
slug: tsl-tovar-inside-fn
status: observed
scope: 3D 场景编辑器：写用到 toVar() 的 TSL 节点图（天空、LUT 等 pass）
createdAt: 2026-10-23T06:12:29.567Z
source: 188beed6-152e-49c5-ab86-602f1e946382
---
用到 toVar() 的 TSL 图要在 Fn(() => { … })() 里构建。W2 物理天空实施时有一处没包进 Fn，Metal（WebGPU）上天空颜色每个像素都一样；包进 Fn 后恢复。防漂移的 e2e「W2 atmosphere drift」读回天空视图的多个方向，能抓到这类回归。
