---
slug: tsl-sample-in-divergent-branch-nan
status: observed
scope: 3D 场景编辑器：在按像素分叉的 TSL 循环或分支（select、If、Loop）里采样纹理，尤其是阴影贴图的比较采样
createdAt: 2026-10-26T08:00:14.949Z
source: develop f3c4807f（owner 2026-10-26，体积雾纯白）
---
TSL 的 select 在分支不是常量时会编成 if/else，three 又在 WGSL 开头写了 diagnostic(off, derivative_uniformity)，所以分支里的 textureSampleCompare、texture().sample() 能编过，但它们靠 2×2 像素块求隐式导数，分叉时结果未定义。W5 体积步进里点光的立方体阴影查找写成 select(within, cube.sample(dir).compare(depth), 1)：没进分支的像素，采样方向停在 var<private> 的零向量，立方体坐标的导数成了 NaN，整块像素的光照变 NaN，被 6e4 的上限截成纯白（截止 3 m 的投影点光贴地放、从后方看时出现白色穹顶）。修法：采样放在分支外，每一步都做，范围改用 step 相乘的掩码 mix 进去；参与除法的量先夹住，例如深度换算里的 m 不小于 near。2D 贴图的零坐标是有限值，暂时不出 NaN，但属于同一类未定义行为；透视投影的 clip.w 可能为 0，改成无条件采样前先确认不会引入新的 NaN。项目约定不写 wgslFn，也就用不了显式级别的 textureSampleCompareLevel，three 的生成器也不输出它。这类问题 SwiftShader 不复现，只在本机 GPU（Metal）上出现，防回退用例要固定在 GPU 上跑，见 e2e「W5 point shadow」。
