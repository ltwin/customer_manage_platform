---
slug: tsl-gpu-readback-measure
status: observed
scope: 3D 场景编辑器：在页面或 e2e 里用 TSL 渲染到纹理再读回像素做数值测量（探针、亮度、法线等）
createdAt: 2026-10-24T10:14:33.025Z
source: fa8a5415-fa2a-4fd8-ba13-5bf72fa67830
---
读回 TSL 出图做测量时有三个坑（S0 组 6 实测）：一是不透明材质的出图 alpha 会被强制为 1，想用 alpha 通道带数据要给材质设 blending: NoBlending；二是负值读回会被截成 0，法线等有符号量先打包成 x*0.5+0.5 再在 JS 里解包；三是 mix(b, a, 0) 在 a 为 NaN 时结果仍是 NaN，不能靠权重 0 屏蔽无效值，要先用 select 之类把 NaN 换掉。测量结果和预期差一个常数倍（例如辐照度差 π 倍）时，先核对这几处和单位约定，再怀疑算法。
